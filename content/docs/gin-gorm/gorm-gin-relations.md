---
title: GORM 多表关联实战：评论模型、增删查与 Preload
description: 在入门篇单表 CRUD 的基础上引入第二张表 comments（一对多）：模型与迁移、评论的增删查、以及用 Preload 在详情接口按需加载关联评论。每节附完整代码与验证命令。
date: 2026-09-02
series: gin-gorm
level: P3
tags:
  - Go
  - PostgreSQL
  - ORM
---

## 📚 系列导航

本系列共七篇：

1. [**GORM 入门实战：用 Gin + GORM 写一个图书管理 API**](./gorm-gin-crud-tutorial) —— 单表 CRUD、软删除、零值陷阱，从零跑通完整项目
2. [**GORM 多表关联实战：评论模型、增删查与 Preload**](./gorm-gin-relations) —— 第二张表 comments、评论增删查、详情按需加载
3. [**GORM 文件与查询增强实战：封面上传、分页搜索与评论数聚合**](./gorm-gin-media-query) —— 上传与静态服务、分页/搜索/排序、评论数聚合
4. [**GORM 数据工程实战：批量导入、请求 DTO 与校验错误翻译**](./gorm-gin-dto-batch) —— CreateInBatches、DTO 与模型分离、校验错误翻译
5. [**GORM 多对多实战：书籍与标签**](./gorm-gin-tags) —— many2many 连接表、按标签筛选与关联增删
6. [**GORM 工程化实战（一）：分层、注入与可测性（选读）**](./gorm-gin-engineering-layering) —— Repository/Service 分层、构造注入、表驱动测试
7. [**GORM 工程化实战（二）：可靠性与生产化（选读）**](./gorm-gin-engineering-reliability) —— 统一错误处理、安全补强、对象存储、连接池

---

> **前置阅读**：完成入门篇（[《GORM 入门实战》](./gorm-gin-crud-tutorial)）——`books` 单表 + 五个 CRUD handler 的项目（另有一条可选的物理删除路由 `DELETE /books/:id/permanent`，按第九章完整版注册与否皆可）。代码约定与入门篇一致（`WithContext` / `errors.Is` 判 404 / 400·404·201）。

真实的图书 API 不能只有一张 `books` 表。本篇补上第二张表 `comments`（评论），把「一对多」变成真实模型。

---

## 一、新增第二张表 comments（一对多）

**目标：** 建立 `Book` 与 `Comment` 的一对多关系，让"某本书下有多条评论"成为一个真实模型。

为什么第二张表选评论？图书场景里"书 → 评论"最自然、最容易被读者代入；一对多 + 外键 + `Preload` 是多表关联的第一级台阶。第三张表（`tags`，多对多）在[《GORM 多对多实战》](./gorm-gin-tags)兑现，一课只上一张新表。

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

`models/book.go` 追加：

```go
type Book struct {
    gorm.Model
    Title    string    `json:"title" gorm:"not null"`
    Author   string    `json:"author" gorm:"not null"`
    Price    int       `json:"price"`
    Comments []Comment `json:"comments,omitempty"` // 关系声明，不是表列；仅用于 Preload
}
```

> **为什么这个字段连 `foreignKey` 标签都不用写？** 因为 GORM 的 has-many 默认约定是"父类型名 + 父主键字段名"（`Book` + `ID` = `BookID`）——`Comment.BookID` 恰好命中约定，关联自动解析。**什么时候必须显式写 `gorm:"foreignKey:..."`**：子表外键字段偏离约定时（比如改成 `BookRef`）；同时注意**这个字段不能删**——它是关系声明，`Preload("Comments")` 靠它按名字加载。

> **为什么 `Comments` 不直接出现在列表响应里？** 列表接口如果默认带全部评论，响应体量会膨胀、还会诱发 N+1 查询。`json:"comments,omitempty"` + **不默认 Preload**——评论只在**详情接口按需加载**（第三节），这是真实项目的通行做法。

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

> **⚠️ 软删除不联动从表：** 软删除 `books` 里的一本书，只是给 `books.deleted_at` 打时间戳，**`comments.deleted_at` 不受影响**——评论依然可见、依然可查。入门篇讲过"软删除 = 框架改写 SQL"，这里看到它的另一面：**主表软删不会级联到子表**。要"删书连带隐藏评论"，得自己写（比如 `db.Model(&models.Comment{}).Where("book_id = ?", id).Update("deleted_at", time.Now())`，示意代码，省略了 `WithContext` 与错误检查），本篇不展开。

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

    // 2. 绑定评论内容（昵称可空、评论正文必填）
    var input struct {
        Nickname string `json:"nickname"`
        Content  string `json:"content" binding:"required"`
    }
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

> 注意第 3 步没有用整个 `input` 直接 `Create(&input)`——`input` 是请求 DTO，`comment` 才是模型。**请求结构体和模型分离**在这里从入门篇的"局部做法"升级为主线规则（入门篇第五章进阶已用 `createBookInput` 尝过鲜，当时两者字段还基本重合），[《数据工程实战》](./gorm-gin-dto-batch)会把它正式落地。

**测试：**

```bash
curl -X POST http://localhost:8080/books/1/comments \
  -H "Content-Type: application/json" \
  -d '{"nickname":"Alice","content":"写得很清楚"}'
# → 201，返回带 id（`gorm.Model` 无 json 标签，实际键是大写 `ID`/`CreatedAt`，见入门篇）的 comment
```

### 2.2 按书分页查询评论

`ListComments`——一个值得背下来的分页骨架（它用 `strconv.Atoi` 解析分页参数：若 2.1 之后你的 `handlers/comment.go` import 里还没有 `strconv`，记得补上）：

```go
// ListComments 按书分页查评论，默认按创建时间倒序。
// 返回 {items, total, page, pageSize}，total 是过滤后的总条数。
func ListComments(c *gin.Context) {
    id := c.Param("id")

    // 1. 解析分页参数
    // 1.1 page：第几页，默认 1
    page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
    // 1.2 pageSize：每页条数，默认 10
    pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "10"))

    // 2. 校验分页参数（防御式，非法值回落到默认）
    // 2.1 page 最小为 1
    if page < 1 {
        page = 1
    }
    // 2.2 pageSize 回落默认 10
    if pageSize < 1 {
        pageSize = 10
    }
    // 2.3 pageSize 上限 100：防止客户端一次拉取过量数据
    if pageSize > 100 {
        pageSize = 100
    }

    // 3. 组装查询：只查当前书的评论
    var comments []models.Comment
    query := db.DB.WithContext(c.Request.Context()).
        Model(&models.Comment{}).
        Where("book_id = ?", id)

    // 4. 先 Count 过滤后的总条数
    var total int64
    if err := query.Count(&total).Error; err != nil {
        _ = c.Error(err) // 具体错误交给 Gin 日志；客户端消息保持统一
        c.JSON(http.StatusInternalServerError, gin.H{"error": "查询评论失败"})
        return
    }

    // 5. 再取当前页（评论按创建时间倒序）
    if err := query.
        Order("created_at DESC").
        Offset((page - 1) * pageSize).
        Limit(pageSize).
        Find(&comments).Error; err != nil {
        _ = c.Error(err) // 具体错误交给 Gin 日志；客户端消息保持统一
        c.JSON(http.StatusInternalServerError, gin.H{"error": "查询评论失败"})
        return
    }

    // 6. 返回分页结果
    c.JSON(http.StatusOK, gin.H{
        "items":    comments,
        "total":    total,
        "page":     page,
        "pageSize": pageSize,
    })
}
```

这是系列里第一个分页接口，先看完整形态——记住 `Count + Order + Offset + Limit` 的组合。（下一篇[《文件与查询增强实战》](./gorm-gin-media-query)会把同一骨架用在图书列表上，并把这段解析收拢成 `parsePagination` 帮助函数。）

另外留意一个编码细节：`strconv.Atoi` 出错时用 `_` 丢弃，非法参数直接回落到默认值——防御式解析。

> **错误消息为什么统一？** `Count` 与 `Find` 失败都返回「查询评论失败」是刻意的——客户端看到的是 500，不需要知道是哪一步挂了（向外暴露内部细节也不安全）；真正要区分的是**服务端日志**：`_ = c.Error(err)` 把具体错误交给 Gin 的日志中间件记录。生产环境会升级为 `slog` + 统一错误中间件（落地见[《GORM 工程化实战（二）：可靠性与生产化》](./gorm-gin-engineering-reliability)）。

**测试：**

```bash
curl "http://localhost:8080/books/1/comments?page=1&pageSize=10"
# {"items":[...],"total":1,"page":1,"pageSize":10}
```

> **与创建/删除的语义差异：** `CreateComment` / `DeleteComment` 都会先校验书存在（书软删后这些操作 404），`ListComments` 却不校验——书软删后 `GET /books/:id/comments` 依然 200 返回历史评论。这是刻意的：子表数据随主表软删仍可查（呼应 1.3 的 ⚠️ 框），列表接口只负责"按条件取数"，不负责判定资源存在。

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

### 2.4 路由注册

以下三行放进 `main.go` 的路由区（其余路由沿用入门篇）：

```go
r.POST("/books/:id/comments", handlers.CreateComment)
r.GET("/books/:id/comments", handlers.ListComments)
r.DELETE("/books/:id/comments/:cid", handlers.DeleteComment)
```

---

## 三、关联查询实战：Preload 一对多

**目标：** 用 `Preload("Comments")` 让详情接口按需加载关联评论——这是"一对多读取"的重头戏，也是本篇真正的新知识点。

### 3.1 详情接口按需加载评论

`handlers/book.go` 的 `GetBook` 加上一行 `Preload`：

```go
// 查询单条图书（带评论，按需加载）
func GetBook(c *gin.Context) {
    id := c.Param("id")

    var book models.Book
    result := db.DB.WithContext(c.Request.Context()).Preload("Comments").First(&book, id)
    if errors.Is(result.Error, gorm.ErrRecordNotFound) {
        c.JSON(http.StatusNotFound, gin.H{"error": "图书不存在"})
        return
    }
    if result.Error != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
        return
    }

    c.JSON(http.StatusOK, book)
}
```

`Preload("Comments")` 让一次 `First` 顺带查出该书全部评论：GORM 先查 `books` 再按 `book_id` 批量查 `comments` 组装——**它不会触发 N+1**（是两条 SQL 一次组装，不是逐行查询）。

> **列表接口不 Preload：** `GetBooks` 保持原样（不带评论）——详情才带。**接口契约由数据用途决定，不由 ORM 能力决定**。

**测试：**

```bash
curl http://localhost:8080/books/1
# 详情里应包含 "comments":[...]
```

---

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
