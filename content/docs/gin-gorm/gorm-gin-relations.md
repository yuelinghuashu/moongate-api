---
title: GORM 多表关联实战：评论模型、增删查与 Preload
description: 在入门篇单表 CRUD 的基础上引入第二张表 comments（一对多）：模型与迁移、评论的增删查、以及用 Preload 在详情接口按需加载关联评论。每节附完整代码与验证命令。
date: 2026-09-02
series: gin-gorm
order: 2
tags:
  - Go
  - PostgreSQL
  - ORM
---

## 一、新增第二张表 comments（一对多）

**目标：** 建立 `Book` 与 `Comment` 的一对多关系，让"某本书下有多条评论"成为一个真实模型。

第二张表选评论：一对多 + 外键 + `Preload` 是多表关联的第一级台阶；多对多（`tags`）留到[《GORM 多对多实战》](./gorm-gin-tags)，一课只上一张新表。

### 1.1 Comment 模型

新建 `models/comment.go`：

```go
package models

import "gorm.io/gorm"

type Comment struct {
    gorm.Model
    BookID   uint   `json:"bookId" gorm:"not null;index"` // 外键：属于哪本书
    Nickname string `json:"nickname"`                     // 评论者名（不引入用户/鉴权）
    Content  string `json:"content" gorm:"not null"`
}
```

字段说明：

- `BookID` 是外键，`index` 让它带索引——按书查评论是最高频查询；
- 评论者的处理：用 `Nickname` 字符串，刻意不引入用户表和鉴权（那是另一个话题）；
- `gorm.Model` 自带软删除——评论同样支持软删除，行为与入门篇一致。

### 1.2 Book 增加关联字段

`models/book.go` 只加这一行（其余字段同入门篇第三章）：

```go
Comments []Comment `json:"comments,omitempty" gorm:"constraint:OnDelete:CASCADE"` // 关系声明，不是表列；仅用于 Preload
```

`constraint:OnDelete:CASCADE` 让 AutoMigrate 把外键建成 `ON DELETE CASCADE`：物理删书时数据库连带删评论。不加的话外键默认 `NO ACTION`，删有评论的书会直接报错（入门篇第八章的 ⚠️ 讲的就是这个坑）。

> **为什么这个字段连 `foreignKey` 标签都不用写？** 因为 GORM 的 has-many 默认约定是"父类型名 + 父主键字段名"（`Book` + `ID` = `BookID`）——`Comment.BookID` 恰好命中约定，关联自动解析。**什么时候必须显式写 `gorm:"foreignKey:..."`**：子表外键字段偏离约定时（比如改成 `BookRef`）；同时注意**这个字段不能删**——它是关系声明，`Preload("Comments")` 靠它按名字加载。

> **为什么 `Comments` 不直接出现在列表响应里？** 列表接口如果默认带全部评论，响应体量会膨胀、还会诱发 N+1 查询。`json:"comments,omitempty"` + **不默认 Preload**——评论只在**详情接口按需加载**（第三节），而且"按需"是**带边界**的：只取最新几条预览，完整列表走 2.2 的分页接口。这是真实项目的通行做法。

### 1.3 迁移更新与验证

`main.go` 的 `AutoMigrate` 改为同时建两张表：

```go
if err := db.DB.AutoMigrate(&models.Book{}, &models.Comment{}); err != nil {
    log.Fatal("迁移失败：", err)
}
```

- `books` 已存在，本篇**不会给它加任何列**——`Comments []Comment` 是「关系声明」不是列，AutoMigrate 对 `books` 本表没有动作；
- `comments` 首次迁移会建表 + 建外键（`book_id → books.id`）+ `index`。

**验证：**

```text
\d comments
-- 应看到 book_id 带索引、外键约束指向 books(id)，deleted_at 等 gorm.Model 字段齐全
```

> **⚠️ 软删除不联动从表：** 软删除 `books` 里的一本书，只是给 `books.deleted_at` 打时间戳，**`comments.deleted_at` 不受影响**——评论依然可见、依然可查。入门篇讲过"软删除 = 框架改写 SQL"，这里看到它的另一面：**主表软删不会级联到子表**。要"删书连带隐藏评论"，得自己写——**2.4 会给出两种正式做法**（写侧事务联动软删 + 读侧父资源校验）。注意别用 `Update("deleted_at", time.Now())` 这类手写改列的方式：它绕过了 GORM 的软删逻辑（不触发钩子、不维护 `updated_at`），标准写法是 `Delete(&models.Comment{})`。

---

## 二、评论管理：第二张表的增删查

**目标：** 写出评论这批资源的增删查（创建、按书分页查询、删除），并注册对应路由——入门篇的六步公式在第二张表上的完整复刻。本节暂不涉及 Preload（那是第三节的事）。

### 2.1 创建评论

`handlers/comment.go`：

```go
package handlers

import (
    "errors"
    "gin-demo/db"
    "gin-demo/models"
    "net/http"

    "github.com/gin-gonic/gin"
    "gorm.io/gorm"
)

// createCommentInput：创建评论的请求 DTO。命名与职责沿用入门篇的 createBookInput——
// 一个写接口一个具名 DTO，字段与校验规则只在这里声明，模型只管表结构。
type createCommentInput struct {
    Nickname string `json:"nickname"`
    Content  string `json:"content" binding:"required"`
}

// CreateComment 为指定图书创建评论
func CreateComment(c *gin.Context) {
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

    // 2. 绑定评论内容（昵称可空、评论正文必填——规则写在 DTO 上）
    var input createCommentInput
    if err := c.ShouldBindJSON(&input); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "请发送合法的 JSON"})
        return
    }

    // 3. 插入，外键指向当前书
    comment := models.Comment{
        BookID:   book.ID,
        Nickname: input.Nickname,
        Content:  input.Content,
    }
    if err := db.DB.WithContext(c.Request.Context()).Create(&comment).Error; err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "创建评论失败"})
        return
    }

    c.JSON(http.StatusCreated, comment)
}
```

> 注意第 3 步没有用整个 `input` 直接 `Create(&input)`——`input` 是请求 DTO，`comment` 才是模型。**请求结构体和模型分离**在这里承接入门篇第五章的 `createBookInput`（同一套约定：一个写接口一个具名 DTO，本篇是 `createCommentInput`），[《数据工程实战》](./gorm-gin-dto-batch)会把它正式落地，含更新接口的 `updateBookInput` 与校验错误翻译。

**测试：**

```bash
curl -X POST http://localhost:8080/books/1/comments \
  -H "Content-Type: application/json" \
  -d '{"nickname":"Alice","content":"写得很清楚"}'
# → 201，返回带 id（`gorm.Model` 无 json 标签，实际键是大写 `ID`/`CreatedAt`，见入门篇）的 comment
```

### 2.2 按书分页查询评论

`ListComments`：解析分页参数、只查当前书的评论（`handlers/comment.go` 的 import 需补 `strconv`）：

```go
// ListComments 按书分页查评论（默认创建时间倒序），返回 {items,total,page,pageSize}。
func ListComments(c *gin.Context) {
    id := c.Param("id")

    // 1. 解析并校验分页参数：默认 page=1 / pageSize=10，非法值回落，pageSize 上限 100
    page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
    pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "10"))
    if page < 1 {
        page = 1
    }
    if pageSize < 1 {
        pageSize = 10
    }
    if pageSize > 100 {
        pageSize = 100
    }

    // 2. 组装查询：只查当前书的评论
    var comments []models.Comment
    query := db.DB.WithContext(c.Request.Context()).
        Model(&models.Comment{}).
        Where("book_id = ?", id)

    // 3. 先 Count 过滤后的总条数
    var total int64
    if err := query.Count(&total).Error; err != nil {
        _ = c.Error(err) // 具体错误交给 Gin 日志；客户端消息保持统一
        c.JSON(http.StatusInternalServerError, gin.H{"error": "查询评论失败"})
        return
    }

    // 4. 再取当前页（Order + Offset + Limit）
    if err := query.
        Order("created_at DESC").
        Offset((page - 1) * pageSize).
        Limit(pageSize).
        Find(&comments).Error; err != nil {
        _ = c.Error(err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "查询评论失败"})
        return
    }

    c.JSON(http.StatusOK, gin.H{
        "items":    comments,
        "total":    total,
        "page":     page,
        "pageSize": pageSize,
    })
}
```

这是系列第一个分页接口，骨架就是 `Count` → `Order` → `Offset` → `Limit` 这四步。下一篇[《文件与查询增强实战》](./gorm-gin-media-query)把同一骨架用在图书列表上时，会把这段参数解析收拢成 `parsePagination` 帮助函数。

> **错误消息为什么统一？** `Count` 与 `Find` 失败都返回「查询评论失败」是刻意的——客户端看到的是 500，不需要知道是哪一步挂了（向外暴露内部细节也不安全）；要区分的是**服务端日志**：`_ = c.Error(err)`（入门篇第五章起，每个 500 分支都这么写）把具体错误交给 Gin——`gin.Default()` 的 Logger 会把它拼进访问日志；生产环境升级为 `slog` + 统一错误中间件，见[《工程化（二）》](./gorm-gin-engineering-reliability)。

**测试：**

```bash
curl "http://localhost:8080/books/1/comments?page=1&pageSize=10"
# {"items":[...],"total":1,"page":1,"pageSize":10}
```

> **与创建/删除的语义差异：** `CreateComment` / `DeleteComment` 都会先校验书存在（书软删后这些操作 404），`ListComments` 却不校验——书软删后 `GET /books/:id/comments` 依然 200 返回历史评论。这是**现状而不是最终答案**：同一本书，`GET /books/:id` 已经 404，`GET /books/:id/comments` 却还是 200。2.4 会给出统一做法；在此之前先记住这条默认语义——**列表接口只按条件取数，不判定父资源是否存在**。

### 2.3 删除评论

```go
// DeleteComment 软删除评论：删除条件 = 评论主键 + 归属的书一致。
// 只按 cid 删会越权——URL 是 /books/:id/comments/:cid，必须确保评论属于这
// 本书，否则 /books/1/comments/99 也能删掉书 2 的评论（水平越权）。
func DeleteComment(c *gin.Context) {
    id := c.Param("id")   // 书的 id（归属校验用）
    cid := c.Param("cid") // 评论的 id（主键）

    // 主键 + Where 条件叠加：DELETE ... WHERE id = cid AND book_id = id
    result := db.DB.WithContext(c.Request.Context()).
        Where("book_id = ?", id).
        Delete(&models.Comment{}, cid)

    // 先查错误、后查影响行数：cid 不是合法数字时主键转换会报错（500）；
    // 条件没命中（评论不存在 / 不属于这本书）才是 404 —— 与查询接口的双层检查一致
    if result.Error != nil {
        _ = c.Error(result.Error)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "删除评论失败"})
        return
    }
    if result.RowsAffected == 0 {
        c.JSON(http.StatusNotFound, gin.H{"error": "评论不存在"})
        return
    }
    c.JSON(http.StatusOK, gin.H{"message": "评论已删除"})
}
```

`Delete(&models.Comment{}, cid)` 与入门篇删书同构——六步骨架里"做操作 + 验 `RowsAffected`"的又一次实例化。

**测试：**

```bash
curl -X DELETE http://localhost:8080/books/1/comments/1
# → 200；再删同一条 → 404
```

### 2.4 父资源软删之后，子资源怎么办

先把现状摆清楚——同一本书被软删后，四个接口给出的答案并不一致：

| 接口 | 书软删后的响应 | 原因 |
| ---- | -------------- | ---- |
| `GET /books/:id` | 404 | `First(&book, id)` 自带 `deleted_at IS NULL` |
| `POST /books/:id/comments` | 404 | 先查书是否存在 |
| `DELETE /books/:id/comments/:cid` | 404 | 同上 |
| `GET /books/:id/comments` | **200 + 历史评论** | 只查 `comments`，压根没碰 `books`（见 2.2 的注记） |

要让"书没了，评论也看不见"，有两条路。**先决定选哪条，再写代码。**

#### 怎么选：四条判据

- **子资源有独立生命周期吗？** 评论有——作者能删自己的评论，平台也可能因举报下架某一条。有 → 别让它跟着书一起消失；
- **恢复父资源的概率高吗？** 高（下架后重新上架、误删恢复）→ 别级联，否则"恢复书"要变成"全量补偿评论"；
- **子表规模有上限吗？** 评论无界（一本书可能上万条）→ 级联会把一次删除变成上万行 UPDATE；订单项这种几十行、父删即作废的 → 级联很划算；
- **你能保证每个读路径都做校验吗？** 如果消费方很多（老接口、内部报表、导出任务）且你控制不住 → 写侧级联反而是更稳的兜底。

两条默认路线：

- **内容型子资源**（评论、标签、点赞）：父记录软删只动自己一行，可见性由**读侧**判定。删除成本 O(1)、恢复父资源等于恢复全部子资源、子资源保留自己的生命周期；
- **组合型子资源**（订单项、草稿行、账单明细）：子记录离开父记录没有存在意义 → 写侧联动软删，并且**必须**包事务、成对实现恢复、读侧再留一道 gate。

本篇的评论属于前者，所以**主线走读侧**；写侧联动作为备选，放在本节最后。

#### 主线：读侧校验父资源

`ListComments` 开头补一次存在性校验，与 `CreateComment` / `DeleteComment` 对齐：

```go
// 1. 父资源必须存在：书被软删后这里直接 404，子资源自然"看不见"
var book models.Book
if err := db.DB.WithContext(c.Request.Context()).First(&book, id).Error; err != nil {
    if errors.Is(err, gorm.ErrRecordNotFound) {
        c.JSON(http.StatusNotFound, gin.H{"error": "图书不存在"})
        return
    }
    _ = c.Error(err)
    c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
    return
}
```

> 若你更愿意"200 + 空列表"，就把这条校验换成一次 join：`Joins("JOIN books ON books.id = comments.book_id AND books.deleted_at IS NULL")`——一条 SQL 搞定、省一次往返，代价是"书不存在"和"书存在但没有评论"在响应上无法区分。

#### 把 gate 做成中间件：一处强制，覆盖所有子资源路由

逐个 handler 抄校验的问题很现实：今天记得给 `ListComments` 加，明天新加 `/books/:id/tags` 就可能忘。这类"父资源必须存在"的规则属于路由组，做成中间件只需写一次。

**第一步：写中间件**（`handlers/middleware.go`）：

```go
// requireBook：/books/:id 组上所有子资源路由的父资源校验。
// 书不存在（含软删）就地 404 并中止；存在则放进 context 供 handler 复用。
func requireBook(c *gin.Context) {
    var book models.Book
    if err := db.DB.WithContext(c.Request.Context()).First(&book, c.Param("id")).Error; err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            c.JSON(http.StatusNotFound, gin.H{"error": "图书不存在"})
            c.Abort()
            return
        }
        _ = c.Error(err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
        c.Abort()
        return
    }
    c.Set("book", &book) // 后面要用这本书时直接取，省一次查询：book := c.MustGet("book").(*models.Book)
    c.Next()
}
```

**第二步：把子资源路由挂到组上**（`main.go`；组路径是 `/books/:id`，所以组内只写剩下的段，右边注释是拼出来的完整路径）：

```go
// —— ① 集合资源与单本书本身：保持入门篇的写法，不进组 ——
r.POST("/books", handlers.CreateBook)      // 创建
r.GET("/books", handlers.GetBooks)         // 列表
r.GET("/books/:id", handlers.GetBook)      // 详情（自己做 First + Preload 与 404）
r.PUT("/books/:id", handlers.UpdateBook)   // 更新（自己查存在）
r.DELETE("/books/:id", handlers.DeleteBook) // 删除（404 来自 RowsAffected）

// —— ② 书下面的子资源：挂到组上，统一由 requireBook 校验父资源 ——
bookGroup := r.Group("/books/:id")
bookGroup.Use(requireBook)

// 评论（本篇）
bookGroup.GET("/comments", handlers.ListComments)          // GET    /books/:id/comments
bookGroup.POST("/comments", handlers.CreateComment)        // POST   /books/:id/comments
bookGroup.DELETE("/comments/:cid", handlers.DeleteComment) // DELETE /books/:id/comments/:cid

// 标签（第五篇，同样受父资源校验保护）
bookGroup.POST("/tags", handlers.AddBookTag)               // POST   /books/:id/tags
bookGroup.DELETE("/tags/:tid", handlers.RemoveBookTag)     // DELETE /books/:id/tags/:tid

// 物理删除（入门篇第八章的可选项）
bookGroup.DELETE("/permanent", handlers.DeleteBookPermanently) // DELETE /books/:id/permanent
```

四个要点：

- **组内路径是相对段**：`bookGroup.GET("/comments", ...)` 最终是 `/books/:id/comments`，和 2.5 的清单一一对应——别把它写成 `"/books/:id/comments"`，那会变成 `/books/:id/books/:id/comments`；
- **中间件里必须 `c.Abort()`**：已经写了响应就要中止链路，否则 handler 会接着往下跑（再写一次响应，日志里会出现 `headers already written`）；
- **`/books/:id` 自己不进组**：详情接口自己做查询与 404；`DELETE /books/:id` 也靠 `RowsAffected` 判 404，不需要父资源预检。**也别把 `GET /books/:id` 塞进组**（`bookGroup.GET("", ...)`）——那和上面第 ① 段重复注册，Gin 启动就会 panic；
- **参数名必须处处一致**：组是 `:id`，所以原有单条路由也必须是 `:id`。哪一处写成 `:bookId`，启动时会直接 panic：`':bookId' in new path … conflicts with existing wildcard ':id'`。

上了中间件之后，`CreateComment` / `DeleteComment` / `ListComments` 里那几次手工校验都可以删掉——这正是中间件版比逐个 handler 写更值的地方；路由树也不受影响：组路径与已有的 `/books/:id` 是同一段参数名（`:id`），可以共存。

#### 备选：写侧联动软删（组合型子资源才选它）

如果你判定该书属于"组合"语义（评论跟着书一起作废），就在事务里把子表也软删掉。下面是完整实现，替换入门篇的 `DeleteBook`：

```go
// DeleteBook 软删除图书，并联动软删它的评论（DELETE /books/:id）
func DeleteBook(c *gin.Context) {
    id := c.Param("id")

    result := db.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
        // 1. 软删书：0 行 = 书不存在（或已删），转成 ErrRecordNotFound 交给下面的 404
        r := tx.Delete(&models.Book{}, id)
        if r.Error != nil {
            return r.Error
        }
        if r.RowsAffected == 0 {
            return gorm.ErrRecordNotFound
        }
        // 2. 联动软删它的评论：Comment 也嵌了 gorm.Model，所以这句同样是软删
        return tx.Where("book_id = ?", id).Delete(&models.Comment{}).Error
    })

    if errors.Is(result.Error, gorm.ErrRecordNotFound) {
        c.JSON(http.StatusNotFound, gin.H{"error": "图书不存在"})
        return
    }
    if result.Error != nil {
        _ = c.Error(result.Error)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "删除失败"})
        return
    }
    c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}
```

落到数据库就是两条 UPDATE，要么都成功、要么一起回滚：

```sql
UPDATE books    SET deleted_at = now() WHERE id = 3 AND deleted_at IS NULL;
UPDATE comments SET deleted_at = now() WHERE book_id = 3 AND comments.deleted_at IS NULL;
```

选它之前，先把代价认下来：

- **删除成本随子表规模增长**：一本书 1 万条评论 = 一次删书 1 万行 UPDATE，长事务和锁的风险要自己兜（必要时分页删）；
- **恢复要成对实现**：恢复书时必须同步恢复评论，否则会出现"书回来了、评论还是删的"；
- **数据状态被污染**：审计/报表里"下架期间的评论"会变成"从未存在"，而内容型数据通常需要保留可追溯；
- **写法上用 `Delete(&models.Comment{})`，不要手写 `Update("deleted_at", time.Now())`**：后者绕过 GORM 的软删逻辑（不触发钩子、不维护 `updated_at`）；
- **物理删除仍走另一条路**：`/books/:id/permanent` 的 `Unscoped().Delete` 由外键 `ON DELETE CASCADE` 真删评论行（见第五篇 §3.4）——**真正的"删除"才该用数据库级联**。

**测试：**

```bash
# 主线（读侧校验）：书软删后子资源 404
curl -X DELETE http://localhost:8080/books/3
curl "http://localhost:8080/books/3/comments"   # 404（join 版则是空列表）
psql -c "SELECT id, deleted_at FROM comments WHERE book_id = 3"  # NULL，评论数据完好

# 备选（写侧联动）：书软删时评论一起被软删
curl -X POST http://localhost:8080/books/3/comments \
  -H "Content-Type: application/json" -d '{"content":"验证联动软删"}'
curl -X DELETE http://localhost:8080/books/3
psql -c "SELECT id, deleted_at FROM comments WHERE book_id = 3"  # deleted_at 已带时间戳
```

### 2.5 路由注册

以下三行放进 `main.go` 的路由区（其余路由沿用入门篇）：

```go
r.POST("/books/:id/comments", handlers.CreateComment)
r.GET("/books/:id/comments", handlers.ListComments)
r.DELETE("/books/:id/comments/:cid", handlers.DeleteComment)
```

> 若采用 2.4 的 `requireBook` 中间件，这三行改成挂在 `r.Group("/books/:id")` 组下即可（组内只写 `/comments`、`/comments/:cid` 这样的相对段）——最终路径不变，只是多了父资源校验。第五篇的标签路由也一样并进这个组。

---

## 三、关联查询实战：Preload 一对多

**目标：** 用 `Preload("Comments")` 让详情接口按需加载关联评论——"一对多读取"的重头戏，也是本篇真正的新知识点。

### 3.1 详情接口按需加载评论（带边界）

`handlers/book.go` 的 `GetBook` 加上 `Preload`：详情只带**最新 5 条**评论预览，完整列表走 2.2 的分页接口。

```go
// 查询单条图书（带最新 5 条评论预览）
func GetBook(c *gin.Context) {
    id := c.Param("id")

    var book models.Book
    result := db.DB.WithContext(c.Request.Context()).
        Preload("Comments", func(tx *gorm.DB) *gorm.DB {
            return tx.Order("created_at DESC").Limit(5) // 详情只预览最新 5 条，不做全量导出
        }).
        First(&book, id)
    if errors.Is(result.Error, gorm.ErrRecordNotFound) {
        c.JSON(http.StatusNotFound, gin.H{"error": "图书不存在"})
        return
    }
    if result.Error != nil {
        _ = c.Error(result.Error)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
        return
    }

    c.JSON(http.StatusOK, book)
}
```

`Preload("Comments")` 让一次 `First` 顺带查出该书评论：GORM 先查 `books` 再按 `book_id` 批量查 `comments` 组装——**它不会触发 N+1**（两条 SQL 一次组装，不是逐行查询）。`Preload` 的第二个参数是**给子查询加条件**的地方：这里让它按时间倒序只取 5 条。

> **三个接口的分工别混：** 详情 `GET /books/:id` = 最新 5 条预览；完整的分页列表 = 2.2 的 `GET /books/:id/comments`（带 `total`，才谈得上翻页）；列表页上每本书的评论数 = 下一篇的 JOIN + GROUP BY 聚合。**详情不做全量内嵌**——评论是无界的（一本书可能上万条），内嵌全量会让响应随评论数线性膨胀；而且这种契约一旦发布，收紧就是破坏性变更。

> **`Limit` 放在 `Preload` 里的坑：** 它作用于**这条子查询整体**，不是"每本书各取 5 条"。所以这个写法只适合单条详情（`First` 一本）；若对多本书列表也写 `Preload("Comments", ...Limit(5))`，拿到的会是全表前 5 条，而不是每本书 5 条。

> **列表接口不 Preload：** `GetBooks` 保持原样（不带评论）——详情才带（理由见 1.2 的「为什么 `Comments` 不直接出现在列表响应里」），而且带的是**有上限的预览**，不是全量。

**测试：**

```bash
curl http://localhost:8080/books/1
# 详情里出现 "comments":[...]，最多 5 条（按创建时间倒序）

curl "http://localhost:8080/books/1/comments?page=2&pageSize=10"
# 要翻页、要总数，走这个接口
```

## 本篇小结

- **本篇新增路由：**

| 方法   | 路径                              | handler       |
| ------ | --------------------------------- | ------------- |
| POST   | `/books/:id/comments`             | CreateComment |
| GET    | `/books/:id/comments`             | ListComments  |
| DELETE | `/books/:id/comments/:cid`        | DeleteComment |
| GET    | `/books/:id`（改造，带 Comments） | GetBook       |

- 你现在的项目：`books` + `comments` 双表、评论增删查、详情按需加载评论；
- 下一篇[《文件与查询增强实战》](./gorm-gin-media-query)：给书加封面图（上传 + 静态服务），并把列表升级为分页、搜索、排序，附加每本书的评论数。
