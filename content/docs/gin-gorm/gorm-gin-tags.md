---
title: GORM 多对多实战：书籍与标签
description: 系列第 5 篇：兑现伏笔给图书加 tags（多对多）——many2many 声明与连接表、Preload、按标签筛选、关联增删，以及删父记录与带字段连接表两个进阶。
date: 2026-09-05
series: gin-gorm
order: 5
tags:
  - Go
  - PostgreSQL
  - ORM
---

## 一、模型与迁移：many2many 声明

**目标：** 用 `many2many` 声明 `Book` ↔ `Tag`，让 `AutoMigrate` 自动建出 `tags` 与连接表 `book_tags`。

### 1.1 Tag 模型

新建 `models/tag.go`：

```go
package models

import "gorm.io/gorm"

type Tag struct {
    gorm.Model
    // 部分唯一索引：只约束"未软删除"的行
    Name string `json:"name" gorm:"index:idx_tags_name,unique,where:deleted_at IS NULL;not null"`
}
```

- `Name` 带唯一索引：同名标签全局只有一个——这是"标签去重"的基础（`FirstOrCreate` 依赖它，见第三节）；
- **为什么是"部分"唯一索引（`where deleted_at IS NULL`）而不是普通唯一索引？** 入门篇第八章讲过一个坑：软删除的行仍占着唯一索引，同名重建会撞约束。那个例子当时是假设的 `ISBN`，在这里是**真实存在的**——`tags` 自带 `gorm.Model`（含 `deleted_at`），用普通 `uniqueIndex` 的话，软删过 `Go` 之后再建 `Go` 会直接报唯一约束冲突；加上 `where` 后，只有未删除的行参与唯一性，软删的标签不占名字；

> **自由标签 vs 受控词表（设计决策）：** 本篇按「用户自定义标签」讲（豆瓣式）——任何人可随手打标签，`Name` 唯一索引 + `FirstOrCreate` 自动去重，标签随用随建。若你的产品是官方预置的**受控词表**（分类式），改法很简单：
> 去掉「按名 `FirstOrCreate` + 按名筛选」，改为预置标签表 + 前端只传已有的 `tagId`，handler 先校验标签存在（404）再 `Append`——关系从「按名找或建」变成「按 id 校验」。词表模式没有去重问题，但灵活性低；自由标签需要用归一化兜底（见 3.1）。

### 1.2 Book 增加关联字段

`models/book.go` 在 `CoverPath` 之后追加（`Comments` 等原有字段保持不变）：

```go
Tags []Tag `json:"tags,omitempty" gorm:"many2many:book_tags;constraint:OnDelete:CASCADE"` // 多对多：经连接表 book_tags
```

`constraint:OnDelete:CASCADE` 让连接表的两条外键都带级联删除——物理删书时数据库顺手清连接行（为什么必须有它，3.4 会讲）。

> **与一对多的本质区别：** 一对多关系写在**子表**上（`comments.book_id`）；多对多关系写在**标签声明**上（`gorm:"many2many:book_tags;"`），连接表本身**不需要写模型**——AutoMigrate 会自动建。你声明的是"关系"，不是"表"。

### 1.3 迁移与验证

`main.go` 的 `AutoMigrate` 改为同时建三张表（`tags` 首次迁移会建表；`book_tags` 连接表也会自动建）：

```go
if err := db.DB.AutoMigrate(&models.Book{}, &models.Comment{}, &models.Tag{}); err != nil {
    log.Fatal("迁移失败：", err)
}
```

**验证：**

```text
\d book_tags
-- 应看到 book_id、tag_id 两列，主键是 (book_id, tag_id) 复合主键（默认唯一）

\d tags
-- idx_tags_name 是带 WHERE 的部分唯一索引：UNIQUE, btree (name) WHERE deleted_at IS NULL
```

> 连接表复合主键 = 同一对 (书, 标签) 最多一行，"天然去重"就来自它。注意重复追加**不会报错**——`Append` 插入连接表时自带 `ON CONFLICT DO NOTHING`（3.1 有实测 SQL）。

---

## 二、读取：Preload 与按标签筛选

**目标：** 读取一侧：`Preload` 按需带出标签、按标签筛选图书。

### 2.1 详情接口带标签

多表关联篇的 `GetBook` 已经 `Preload("Comments")`（带 `Limit(5)` 的边界写法），这里链上第二个 `Preload`：

```go
result := db.DB.WithContext(c.Request.Context()).
    Preload("Comments", func(tx *gorm.DB) *gorm.DB { // 篇 2 的边界原样保留：详情只带最新 5 条
        return tx.Order("created_at DESC").Limit(5)
    }).
    Preload("Tags"). // 标签数量天然有限，不需要边界
    First(&book, id)
```

- `Preload` 可以链多个：子查询各自批量取（`comments` 一条、`book_tags ⋈ tags` 一条），一次组装，**都不触发 N+1**；
- **要不要加边界，看基数**：评论是无界的（一本书可能上万条），所以详情里只带最新 5 条预览；标签是有界的（几个到几十个），直接 `Preload("Tags")` 就够——边界写法的完整说明见多表关联篇 §3.1；
- **列表不 Preload**：契约同多表关联篇（列表轻、详情重）；`json:"tags,omitempty"` + 不默认加载。

**测试：**

```bash
curl http://localhost:8080/books/1
# 详情里出现 "tags":[...]（先给书打上标签再测，见第三节）
```

### 2.2 按标签筛选图书（分页）

"列出所有带『Go』标签的书"——连接表 JOIN 两次，分页解析复用篇 3 收拢的 `parsePagination`（写在 `handlers/tag.go`；import 见 3.1，`parsePagination` 是同包函数）：

```go
// GetBooksByTag 列出带指定标签的图书（GET /tags/:name/books）：分页 + 创建时间倒序。
// 响应形状与篇 3 的图书列表一致：{items, total, page, pageSize}。
func GetBooksByTag(c *gin.Context) {
    // 标签名用与写入端同一套归一化（3.1），否则 "Go" / "go" 查不到同一个标签
    tagName := strings.ToLower(strings.TrimSpace(c.Param("name")))
    page, pageSize := parsePagination(c) // 篇 3 的分页帮助函数（handlers/pagination.go）

    query := db.DB.WithContext(c.Request.Context()).
        Model(&models.Book{}). // Count(&total) 的 dest 是 int64，没有 Model 时 GORM 不知道表名
        Joins("JOIN book_tags ON book_tags.book_id = books.id").
        // 手工 JOIN 进来的表不会自动带软删条件：tags 一侧要自己写
        Joins("JOIN tags ON tags.id = book_tags.tag_id AND tags.deleted_at IS NULL").
        Where("tags.name = ?", tagName)

    var total int64
    if err := query.Count(&total).Error; err != nil {
        _ = c.Error(err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
        return
    }

    var books []models.Book
    if err := query.
        Order("books.created_at DESC"). // JOIN 后 created_at 有歧义，必须带表名
        Offset((page - 1) * pageSize).
        Limit(pageSize).
        Find(&books).Error; err != nil {
        _ = c.Error(err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
        return
    }

    c.JSON(http.StatusOK, gin.H{"items": books, "total": total, "page": page, "pageSize": pageSize})
}
```

> **⚠️ 破坏性契约变更：** 这个接口的响应从裸数组变成 `{items,total,page,pageSize}` 对象（与篇 3 的 `GET /books` 对齐）——已有前端需要同步适配。

> **`Count` 之前必须让 GORM 知道表。** `Find(&books)` 能从 dest 的 `models.Book` 推断出 `books`，但 `Count(&total)` 的 dest 是 `int64`——不写 `Model(&models.Book{})` 就会直接报 `Table not set, please set it like: db.Model(&user) or db.Table("users")`（实测），接口表现为 500。这也是"什么时候必须写 `Model()`"最典型的一次：**终结方法的 dest 不是模型结构体时，必须显式指定**。

> **为什么 `tags` 一侧要手写 `deleted_at IS NULL`？** GORM 的软删条件只加在**根模型**的表上：实测这条查询的 WHERE 是 `tags.name = ? AND books.deleted_at IS NULL`，`tags.deleted_at` 必须自己补（上面写在 JOIN 条件里），否则软删过的标签仍会列出它的书。同理，`ORDER BY` 里的 `created_at` 在 JOIN 之后有歧义（`books` 和 `tags` 都有这一列），必须写成 `books.created_at`。

> **路由为什么不是 `/books/by-tag`？（实战坑）** 入门篇已注册 `GET /books/:id`，而 Gin 的路由树**不允许同一位置静态段与通配段并存**——再注册 `/books/by-tag` 会直接 panic（"conflicts with existing wildcard"）。所以按标签筛选放在 `tags` 前缀下：`GET /tags/:name/books`，语义也更 REST。

**路由注册：**

```go
r.GET("/tags/:name/books", handlers.GetBooksByTag)
```

**测试：**

```bash
curl "http://localhost:8080/tags/go/books?page=1&pageSize=10"
# {"items":[...],"total":1,"page":1,"pageSize":10}
curl "http://localhost:8080/tags/GO/books"   # 归一化后与 /tags/go/books 等价
```

---

## 三、写入与维护：关联的建立与删除

**目标：** 写关系：给书加标签（幂等）、移除、整组替换，以及删除父记录时连接表如何处理。

### 3.1 给书加标签（Append 本身就幂等）

`FirstOrCreate` 保证标签唯一（`Name` 唯一索引），`Association("Tags").Append` 写连接表（`handlers/tag.go` 的 import：`strings`，以及 `db`/`models`/`net/http`/`gin`）。这条路由挂在 2.4 的 `bookGroup` 上，书的存在性由 `requireBook` 兜底：

```go
// addTagInput：给书加标签的请求 DTO（与 createBookInput / createCommentInput 同一约定：
// 一个写接口一个具名 DTO，校验规则只写在 DTO 上）
type addTagInput struct {
    Name string `json:"name" binding:"required"`
}

// AddBookTag 给书追加一个标签：标签不存在则先创建，再写连接表（POST /books/:id/tags）
func AddBookTag(c *gin.Context) {
    // 1. 书的存在性已由 2.4 的 requireBook 中间件保证，直接取实体（路由必须挂在 bookGroup 上）
    book := c.MustGet("book").(*models.Book)

    // 2. 绑定标签名并归一化：同一个语义只留一种写法
    var input addTagInput
    if err := c.ShouldBindJSON(&input); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "请发送合法的 JSON"})
        return
    }
    name := strings.ToLower(strings.TrimSpace(input.Name))

    // 3. 标签不存在则先创建（Name 唯一索引保证去重）；第二个参数同时决定 WHERE 与新建记录的字段
    var tag models.Tag
    if err := db.DB.WithContext(c.Request.Context()).
        FirstOrCreate(&tag, models.Tag{Name: name}).Error; err != nil {
        _ = c.Error(err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "标签处理失败"})
        return
    }

    // 4. 写连接表：Append 的插入自带 ON CONFLICT DO NOTHING，重复追加天然幂等
    if err := db.DB.WithContext(c.Request.Context()).
        Model(book).Association("Tags").Append(&tag); err != nil {
        _ = c.Error(err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "添加标签失败"})
        return
    }

    c.JSON(http.StatusOK, gin.H{"message": "标签已添加"})
}
```

> **`FirstOrCreate(&tag, models.Tag{Name: name})` 的两个参数不是重复。** 第一个是**目标**：查询结果填它，找不到时它就是被插入的那条记录；第二个是**条件**：GORM 用它生成 `WHERE name = ?`，并在找不到时把这条等值条件**回填进目标**，新建的标签才带上名字。
>
> 少了第二个会怎样？`Where("name = ?", name).FirstOrCreate(&tag)` 生成的 INSERT 是 `... (name) VALUES ("")`——字符串形式的 Where 是 `clause.Expr`，不会被回填；而 `name` 只约束了 `not null`，空串合法，于是库里静默多出一个匿名标签（v1.31.2 实测）。所以**结构体条件不能省**；反过来，既然它已经生成了 `WHERE name = ?`，外面再套一层 `Where("name = ?")` 就是多余的。

> **为什么第 4 步不用先 `Count` 再插？** 有种说法是"重复 `Append` 会撞 `book_tags` 的复合主键报错"，但 GORM 插连接表的那一刻就带了冲突处理（v1.31.2 实测）：
>
> ```sql
> INSERT INTO book_tags (book_id, tag_id) VALUES (1,7) ON CONFLICT DO NOTHING
> ```
>
> 所以重复追加既不报错、也不会多出第二行——**幂等是 `Append` 自带的**。先查再插只多了一次往返和一个并发窗口，没有收益。只有当你需要区分"这次真的新增了"（比如据此返回 201 / 200）时，才值得再查一次；那时的理由是响应语义，不是防报错。

> **归一化：自由标签的命门。** `FirstOrCreate` 只能对**完全相同的字符串**去重——`Go`、`golang`、`Go ` 在库里是三个标签。第 2 步的 `strings.ToLower` + `strings.TrimSpace` 不是可选项：**唯一索引保证「字符串唯一」，不保证「语义唯一」**。需要别名（`Go` → `golang`）就在这里做映射表。读取端（2.2 的 `GetBooksByTag`）用同一套归一化，否则写进去的 `go` 用 `Go` 查不到。

> **两个已知边界：**（1）`FirstOrCreate` 是"先查后插"，并发下两个请求可能同时查不到、同时插入，撞唯一索引返回 500——要更稳可加 `clause.OnConflict{DoNothing: true}` 或失败重试一次；（2）`FirstOrCreate` 与 `Append` 没有包在事务里，`Append` 失败会留下一个没人用的标签。[《工程化（一）》](./gorm-gin-engineering-layering)把它收进服务层用 `db.Transaction()` 一并解决。

**路由注册：**

```go
bookGroup.POST("/tags", handlers.AddBookTag)
```

**测试：**

```bash
curl -X POST http://localhost:8080/books/1/tags \
  -H "Content-Type: application/json" \
  -d '{"name":"Go"}'
# → 200，落库的标签名是归一化后的 go；再执行一次同样命令，仍是 200
# （幂等，连接表无重复行）；-d '{"name":" GO "}' 也会命中同一个标签

curl -X POST http://localhost:8080/books/999/tags \
  -H "Content-Type: application/json" -d '{"name":"Go"}'
# → 404：书不存在。这条 404 由 requireBook 中间件给出，handler 不会执行
```

### 3.2 移除标签（直接删连接表行）

`Association("Tags").Delete(&tag)` 需要先按 ID 拿到 Tag；本例直接操作连接表更直白——删除条件就是复合主键：

```go
// RemoveBookTag 移除书的某个标签（DELETE /books/:id/tags/:tid）
func RemoveBookTag(c *gin.Context) {
    id := c.Param("id")
    tid := c.Param("tid")

    result := db.DB.WithContext(c.Request.Context()).
        Table("book_tags").
        Where("book_id = ? AND tag_id = ?", id, tid).
        Delete(nil)

    if result.Error != nil {
        _ = c.Error(result.Error)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "移除标签失败"})
        return
    }
    if result.RowsAffected == 0 {
        c.JSON(http.StatusNotFound, gin.H{"error": "标签不存在或不属于此书"})
        return
    }
    c.JSON(http.StatusOK, gin.H{"message": "标签已移除"})
}
```

- **为什么叫 `RemoveBookTag` 而不是 `DeleteBookTag`？** 它删的是**关系**（`book_tags` 里的一行），`tags` 表里那个标签本身原封不动——别的书还在用它。系列里的动词分工是：**Create/Delete 作用于实体行**（`DeleteBook`、`DeleteComment`），**Add/Remove 作用于关系**（`AddBookTag`、`RemoveBookTag`）。叫 `DeleteBookTag` 会让人以为标签被删了；
- `Table("book_tags")...Delete(nil)` 直接对连接表执行 `DELETE ... WHERE book_id = ? AND tag_id = ?`——读、写、删三路都能走"连接表即普通表"的思路；
- `RowsAffected == 0` → 该书没有这个标签 → 404（与评论删除的归属校验同一套逻辑）。

**路由注册：**

```go
bookGroup.DELETE("/tags/:tid", handlers.RemoveBookTag)
```

**测试：**

```bash
curl -X DELETE http://localhost:8080/books/1/tags/1
# → 200；再删同一条 → 404（标签不在这本书上）
curl -X DELETE http://localhost:8080/books/999/tags/1
# → 404（书不存在——这是 requireBook 中间件给的，handler 根本没执行）
```

### 3.3 Replace：整组替换 vs 增量追加

`Append` 是**增量**（在已有关系上加），`Replace` 是**整组替换**（先清空该书的全部标签再写入）：

```go
// 编辑页"全量保存标签"场景：前端传整组标签，旧的不在列表里的会被移除
db.DB.WithContext(c.Request.Context()).Model(&book).Association("Tags").Replace(&tags)
```

> **两种语义别混：** 把 `Replace` 当 `Append` 用在"追加一个标签"的接口上，会静默清掉该书其它标签。增删单条用 Append/Delete，整组保存才用 Replace。

---

### 3.4 删除父记录时，连接表怎么办？

`DELETE /books/:id`（软删除）与 `/books/:id/permanent`（物理删除）已存在——多对多让两种删除各有一层讲究：

- **软删除**：给 `books.deleted_at` 打时间戳，连接表行**原样保留**；书的查询/Preload 都看不见它（书本身被过滤）——与多表关联篇的 comments 同理；
- **物理删除**：GORM 建连接表时会**自带两条外键**（`book_tags.book_id → books.id`、`book_tags.tag_id → tags.id`），默认 **`NO ACTION`**——所以"删书留孤儿连接行"不是默认行为，**默认是直接报错**：`DELETE FROM books` 会撞 `fk_book_tags_book`，PostgreSQL 返回 `violates foreign key constraint`（接口 500）。想让物理删除成立，只能把外键改成级联。两种解法：

```go
// 解法一：在关联声明上让数据库级联删除（§1.2 的写法，新库 AutoMigrate 直接生效）
Tags []Tag `json:"tags,omitempty" gorm:"many2many:book_tags;constraint:OnDelete:CASCADE"`
```

```sql
-- 解法二：已有库上补约束（AutoMigrate 不会改已有外键，只能显式迁移）
ALTER TABLE book_tags DROP CONSTRAINT fk_book_tags_book;
ALTER TABLE book_tags ADD CONSTRAINT fk_book_tags_book
  FOREIGN KEY (book_id) REFERENCES books(id) ON DELETE CASCADE;
```

> 同样的坑在 `comments` 上也有一份（入门篇第八章的 ⚠️）：两条外键都改成 `ON DELETE CASCADE` 后，`/permanent` 才能删掉"有评论、有标签"的书。软删除不受外键影响——它只改 `deleted_at`。

---

## 四、进阶：带字段的连接表（JoinModel）

**目标：** 给"关系本身"附加字段——带字段的连接表什么时候用、两条实现路线怎么选。

默认连接表只有 `book_id` / `tag_id` 两列。要给关系附属性（如"这本书的『精读』标签排第几""何时打上的标签"），需要**显式连接模型**：

```go
// models/book_tag.go —— 自定义连接表：复合主键 + 附加字段
type BookTag struct {
    BookID    uint      `gorm:"primaryKey"`
    TagID     uint      `gorm:"primaryKey"`
    Position  int       // 排序值：书内标签的手动顺序
    CreatedAt time.Time // 打标时间
}
```

### 4.1 首选：`SetupJoinTable`（保留 many2many API）

比起把关系拆开，改动最小的一步是告诉 GORM "`book_tags` 这张连接表用 `BookTag` 这个模型"——`Book.Tags` 上的 `many2many:book_tags` 声明**一个字都不用改**：

```go
// db/db.go 的 InitDB 末尾，或 main.go 的 AutoMigrate 之前（启动时执行一次）
if err := db.DB.SetupJoinTable(&models.Book{}, "Tags", &models.BookTag{}); err != nil {
    log.Fatal("注册连接表模型失败：", err)
}
// 迁移时把连接模型一起列上（让 position / created_at 两列被创建）
if err := db.DB.AutoMigrate(&models.Book{}, &models.Comment{}, &models.Tag{}, &models.BookTag{}); err != nil {
    log.Fatal("迁移失败：", err)
}
```

关联 API 全部照旧，附加列自动带上——GORM v1.31.2 实测 `Append` 生成的 SQL：

```sql
INSERT INTO book_tags (book_id, tag_id, position) VALUES (1,7,0) ON CONFLICT DO NOTHING
```

- `Preload("Tags")`、`Association("Tags").Append/Replace/Delete` 都可继续用，`Tags []Tag` 的读法不变；
- 附加列在 `Append` 里取零值——要写 `position` 就直接对连接模型操作：`db.DB.Save(&models.BookTag{BookID: 1, TagID: 7, Position: 1})`（或先 `FirstOrCreate` 再 `Update`）；
- `Tags` 字段上的 `constraint:OnDelete:CASCADE` 仍然生效，物理删书照样级联（3.4 的结论不变）。

### 4.2 备选：两个 has-many 指向连接模型

只有当连接表要承担**独立业务**（自己的接口、状态机、审计）时，才值得把关系拆开——那时它已经是一张普通业务表：

```go
type Book struct {
    // ...原有字段
    BookTags []BookTag `gorm:"foreignKey:BookID;constraint:OnDelete:CASCADE"`
}

type Tag struct {
    // ...原有字段
    BookTags []BookTag `gorm:"foreignKey:TagID;constraint:OnDelete:CASCADE"`
}
```

- 读写都直接操作连接记录：`db.Create(&models.BookTag{BookID: 1, TagID: 7, Position: 1})`、`Preload("BookTags")` 后自行映射成 `[]Tag`；
- `Association("Tags")` 的便捷写不再适用，"某本书的标签"要自己 join 或映射；
- **⚠️ `constraint:OnDelete:CASCADE` 必须补回来**：这组 has-many 是新的外键，不给它 CASCADE 就是默认 `NO ACTION`（实测），3.4 解决过的"物理删书撞外键"会原地复活。

> **选择建议：** 90% 的场景用默认连接表就够；要给关系加字段，先试 `SetupJoinTable`（改动最小、`Tags []Tag` 的读法不变）；连接表需要独立生命周期时，才拆成两个 has-many。

## 本篇小结

- **本篇新增路由：**

| 方法   | 路径                   | handler       |
| ------ | ---------------------- | ------------- |
| POST   | `/books/:id/tags`      | AddBookTag    |
| DELETE | `/books/:id/tags/:tid` | RemoveBookTag |
| GET    | `/tags/:name/books`    | GetBooksByTag |

（`main.go` 增补：`AutoMigrate` 加 `&models.Tag{}`；`POST /tags`、`DELETE /tags/:tid` 两行挂到 2.4 的 `bookGroup` 上，`GET /tags/:name/books` 仍单独注册；第四节按需再加 `&models.BookTag{}` 与 `SetupJoinTable`。）

- 你现在的项目：`books` + `comments` + `tags` 三表、封面图上传与静态服务、分页搜索列表、评论 CRUD、标签与连接表、批量导入与 DTO 校验；
- 系列至此覆盖 has-many / many2many 两种关联形态与查询、聚合、工程方法；可选延伸（选读）：[《GORM 工程化实战（一）：分层、注入与可测性》](./gorm-gin-engineering-layering)，把一直直连的 `db.DB` 重构为 `BookRepository` 接口 + Service 层 + 表驱动测试——分层后的测试与事务，会让这篇的每次 `WithContext` 都派上用场。
