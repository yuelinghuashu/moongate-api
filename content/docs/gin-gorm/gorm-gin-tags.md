---
title: GORM 多对多实战：书籍与标签
description: 系列第 5 篇：兑现伏笔给图书加 tags（多对多）——many2many 声明与连接表、Preload、按标签筛选、关联增删，以及删父记录与带字段连接表两个进阶。
date: 2026-09-05
series: gin-gorm
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
    Name string `json:"name" gorm:"uniqueIndex;not null"`
}
```

- `Name` 带唯一索引：同名标签全局只有一个——这是"标签去重"的基础（`FirstOrCreate` 依赖它，见第三节）；

> **自由标签 vs 受控词表（设计决策）：** 本篇按「用户自定义标签」讲（豆瓣式）——任何人可随手打标签，`Name` 唯一索引 + `FirstOrCreate` 自动去重，标签随用随建。若你的产品是官方预置的**受控词表**（分类式），改法很简单：
> 去掉「按名 `FirstOrCreate` + 按名筛选」，改为预置标签表 + 前端只传已有的 `tagId`，handler 先校验标签存在（404）再 `Append`——关系从「按名找或建」变成「按 id 校验」。词表模式没有去重问题，但灵活性低；自由标签需要用归一化兜底（见 3.1）。

### 1.2 Book 增加关联字段

`models/book.go` 在 `CoverPath` 之后追加（`Comments` 等原有字段保持不变）：

```go
Tags []Tag `json:"tags,omitempty" gorm:"many2many:book_tags;"` // 多对多：经连接表 book_tags
```

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
```

> 连接表复合主键 = 同一对 (书, 标签) 最多一行——后面"重复追加报错"和"天然去重"都源于它。

---

## 二、读取：Preload 与按标签筛选

**目标：** 读取一侧：`Preload` 按需带出标签、按标签筛选图书。

### 2.1 详情接口带标签

多表关联篇的 `GetBook` 已经 `Preload("Comments")`，这里链上第二个 `Preload`：

```go
result := db.DB.WithContext(c.Request.Context()).
    Preload("Comments").
    Preload("Tags").
    First(&book, id)
```

- `Preload` 可以链多个：子查询各自批量取（`comments` 一条、`book_tags ⋈ tags` 一条），一次组装，**都不触发 N+1**；
- **列表不 Preload**：契约同多表关联篇（列表轻、详情重）；`json:"tags,omitempty"` + 不默认加载。

**测试：**

```bash
curl http://localhost:8080/books/1
# 详情里出现 "tags":[...]（先给书打上标签再测，见第三节）
```

### 2.2 按标签筛选图书

"列出所有带『Go』标签的书"——连接表 JOIN 两次：

```go
// GetBooksByTag 列出带指定标签的图书（GET /tags/:name/books）
func GetBooksByTag(c *gin.Context) {
    tagName := c.Param("name")

    var books []models.Book
    if err := db.DB.WithContext(c.Request.Context()).
        Joins("JOIN book_tags ON book_tags.book_id = books.id").
        Joins("JOIN tags ON tags.id = book_tags.tag_id").
        Where("tags.name = ?", tagName).
        Find(&books).Error; err != nil {
        _ = c.Error(err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
        return
    }

    c.JSON(http.StatusOK, books)
}
```

> **路由为什么不是 `/books/by-tag`？（实战坑）** 入门篇已注册 `GET /books/:id`，而 Gin 的路由树**不允许同一位置静态段与通配段并存**——再注册 `/books/by-tag` 会直接 panic（"conflicts with existing wildcard"）。所以按标签筛选放在 `tags` 前缀下：`GET /tags/:name/books`，语义也更 REST。

**路由注册：**

```go
r.GET("/tags/:name/books", handlers.GetBooksByTag)
```

**测试：**

```bash
curl "http://localhost:8080/tags/Go/books"
# 返回带 Go 标签的书（先打标签再测）
```

---

## 三、写入与维护：关联的建立与删除

**目标：** 写关系：给书加标签（幂等）、移除、整组替换，以及删除父记录时连接表如何处理。

### 3.1 给书加标签（先查后插，保证幂等）

`FirstOrCreate` 保证标签唯一（Name 唯一索引），`Association("Tags").Append` 写连接表：

```go
// AddBookTag 给书追加一个标签：标签不存在则先创建，再写连接表（POST /books/:id/tags）
func AddBookTag(c *gin.Context) {
    id := c.Param("id")

    // 1. 书必须存在
    var book models.Book
    result := db.DB.WithContext(c.Request.Context()).First(&book, id)
    if errors.Is(result.Error, gorm.ErrRecordNotFound) {
        c.JSON(http.StatusNotFound, gin.H{"error": "图书不存在"})
        return
    }
    if result.Error != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
        return
    }

    // 2. 绑定标签名
    var input struct {
        Name string `json:"name" binding:"required"`
    }
    if err := c.ShouldBindJSON(&input); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "请发送合法的 JSON"})
        return
    }

    // 3. 标签不存在则先创建（Name 唯一索引保证去重）
    var tag models.Tag
    if err := db.DB.WithContext(c.Request.Context()).
        Where("name = ?", input.Name).
        FirstOrCreate(&tag, models.Tag{Name: input.Name}).Error; err != nil {
        _ = c.Error(err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "标签处理失败"})
        return
    }

    // 4. 写连接表：先查后插，保证幂等
    var count int64
    if err := db.DB.WithContext(c.Request.Context()).
        Table("book_tags").
        Where("book_id = ? AND tag_id = ?", book.ID, tag.ID).
        Count(&count).Error; err != nil {
        _ = c.Error(err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "查询标签关系失败"})
        return
    }
    if count == 0 {
        if err := db.DB.WithContext(c.Request.Context()).
            Model(&book).Association("Tags").Append(&tag); err != nil {
            _ = c.Error(err)
            c.JSON(http.StatusInternalServerError, gin.H{"error": "添加标签失败"})
            return
        }
    }

    c.JSON(http.StatusOK, gin.H{"message": "标签已添加"})
}
```

> **自由标签的代价：名称归一化。** `FirstOrCreate` 的「去重」只对**完全相同的字符串**生效——`Go`、`golang`、`Go `（尾随空格）在数据库里是三个标签。
> 生产要在入库前归一化：`strings.ToLower` + `strings.TrimSpace`，必要时再做别名映射（`Go` → `golang`）。本篇不展开实现，但要记住：**唯一索引保证的是「字符串唯一」，不是「语义唯一」。**

> **为什么第 4 步要先查后插？** `Association("Tags").Append(&tag)` 是直接往连接表 `INSERT`。`book_tags` 的复合主键 `(book_id, tag_id)` 保证同一对关系最多一行——**重复追加同一标签会撞唯一约束报错，不是幂等**。所以示例先 `Count` 检查再插。若你的项目接受"重复请求报错"的语义，省掉第 4 步的 Count 也行——本篇按幂等演示。

**路由注册：**

```go
r.POST("/books/:id/tags", handlers.AddBookTag)
```

**测试：**

```bash
curl -X POST http://localhost:8080/books/1/tags \
  -H "Content-Type: application/json" \
  -d '{"name":"Go"}'
# → 200 标签已添加；再执行一次同样命令，仍是 200（幂等，连接表无重复行）
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

- `Table("book_tags")...Delete(nil)` 直接对连接表执行 `DELETE ... WHERE book_id = ? AND tag_id = ?`——读、写、删三路都能走"连接表即普通表"的思路；
- `RowsAffected == 0` → 该书没有这个标签 → 404（与评论删除的归属校验同一套逻辑）。

**路由注册：**

```go
r.DELETE("/books/:id/tags/:tid", handlers.RemoveBookTag)
```

**测试：**

```bash
curl -X DELETE http://localhost:8080/books/1/tags/1
# → 200；再删同一条 → 404
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
- **物理删除**：GORM 默认**不会清理连接表行**！孤儿 `book_tags` 行不挡查询（按书过滤时书已不存在），但会占表空间，ID 复用还会串数据。两种解法：

```go
// 解法一：删除时显式连坐删除关联（Select(clause.Associations)）
db.DB.WithContext(c.Request.Context()).
    Select(clause.Associations).
    Unscoped().Delete(&models.Book{}, id)
```

```sql
-- 解法二：建表时给连接表加外键约束，让数据库层级联清理
ALTER TABLE book_tags
  ADD CONSTRAINT fk_book_tags_book FOREIGN KEY (book_id) REFERENCES books(id) ON DELETE CASCADE;
```

> 教学用解法一（不动数据库结构）；生产常两手都做：GORM 显式连坐 + 数据库外键兜底。

---

## 四、进阶：带字段的连接表（JoinModel）

**目标：** 给"关系本身"附加字段——带字段的连接表（JoinModel）什么时候用、怎么声明。

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

两个模型各挂一个 has-many 指向连接模型：

```go
type Book struct {
    // ...原有字段
    BookTags []BookTag `gorm:"foreignKey:BookID"`
}

type Tag struct {
    // ...原有字段
    BookTags []BookTag `gorm:"foreignKey:TagID"`
}
```

升级后要点（点到为止）：

- 关联不再靠"`Tags []Tag` + many2many 标签"，而是**两个 has-many 指向 `BookTag`**——读写要直接操作连接记录（`db.Create(&models.BookTag{BookID: 1, TagID: 2, Position: 1})`）；
- `Association("Tags")` 的便捷自动写不再适用；取"某本书的标签"变成 `Preload("BookTags")` 后自行映射；
- **默认连接表覆盖 90% 场景**——只有需要给"关系本身"存字段时才升级 JoinModel（教学顺序：先默认，再按需升级）。

---

## 本篇小结

- **本篇新增路由：**

| 方法   | 路径                   | handler       |
| ------ | ---------------------- | ------------- |
| POST   | `/books/:id/tags`      | AddBookTag    |
| DELETE | `/books/:id/tags/:tid` | RemoveBookTag |
| GET    | `/tags/:name/books`    | GetBooksByTag |

（`main.go` 增补：`AutoMigrate` 加 `&models.Tag{}`、上述三条路由。）

- 你现在的项目：`books` + `comments` + `tags` 三表、封面图上传与静态服务、分页搜索列表、评论 CRUD、标签与连接表、批量导入与 DTO 校验；
- 系列至此覆盖 has-many / many2many 两种关联形态与查询、聚合、工程方法；可选延伸（选读）：[《GORM 工程化实战（一）：分层、注入与可测性》](./gorm-gin-engineering-layering)，把一直直连的 `db.DB` 重构为 `BookRepository` 接口 + Service 层 + 表驱动测试——分层后的测试与事务，会让这篇的每次 `WithContext` 都派上用场。
