---
title: GORM 入门实战：用 Gin + GORM 写一个图书管理 API
description: 从零搭建一个完整的图书管理 API：环境、模型、CRUD 主线一次跑通，顺带讲清软删除、零值陷阱与错误检查。
date: 2026-09-01
series: gin-gorm
order: 1
tags:
  - Go
  - PostgreSQL
  - ORM
---

## 开始之前

适合已经会 Go 基础语法、想从零跑通一个 GORM 项目的读者。动手前确认三件事：

- **Go 1.22+**（本教程用到 `errors.Is`；推荐 1.24+）
- **本地 PostgreSQL**（没有就 Docker：`docker run --name pg -e POSTGRES_PASSWORD=123456 -p 5432:5432 -d postgres:16`）
- **手动建库**：`CREATE DATABASE library;`——`AutoMigrate` 只能建表，不能建库

换 MySQL / SQLite 也可以，代码跨库通用，差别只在驱动与 DSN（见第一章）。但第 2 篇起会用到 PostgreSQL 专属特性（`ILIKE`、按主键分组的函数依赖、部分唯一索引），差异会在用到处就地标注。

项目结构先平铺，业务复杂后再演进（见第 6 篇）：

```text
gin-demo/
├── main.go           # 入口文件
├── db/
│   └── db.go         # 数据库连接
├── models/
│   └── book.go       # 数据模型
├── handlers/
│   └── book.go       # 业务逻辑（CRUD）
└── go.mod
```

> **为什么平铺？** `db` / `models` / `handlers` 三个包足够跑通，也方便逐章对照；用 `internal/` + Service / Repository 分层是[《工程化（一）》](./gorm-gin-engineering-layering)的事。

## 先建立心智模型

GORM 最不一样的对手不是 Spring/Django，而是 JDBC/MyBatis 这类**手写 SQL** 的路子：后者要你拼 SQL、再把结果集一行行映射成对象，GORM 恰好反过来。

> **一句话总纲：** Gin 把「HTTP 请求」变成「Go 函数调用」，GORM 把「Go 结构体」翻译成「数据库 SQL」。主线只有一条：**请求进来 → 装进结构体 → 交给 GORM → 结果填回结构体 → 返回 JSON**。

### 不是写 SQL，是操作结构体

你只描述**意图**（`Create`、`Find`、`Updates`、`Delete`），翻译成 SQL 是框架的事。想确认它生成了什么 SQL，开 GORM 日志就能看到。

### 四个必须建立的心智

1. **结构体一物三用**：同一个 struct 既是表结构（`gorm` 标签）、又是请求/响应载体（`json` 标签）、还是数据库操作的参数（`&book`）。类型即契约，改一处全联动。
2. **查数据是「填空」，不是「返回值」**：`Find(&books)`、`First(&book, id)` 必须传目标变量的**指针**，GORM 靠反射填进去；漏写 `&` 等于填了个副本，函数外拿不到。`ShouldBindJSON(&x)`、`Delete(&models.Book{})`、`AutoMigrate(&models.Book{})` 同理——要往里填、或要对象本身，都得传地址。反过来，`*gorm.DB` / `*gin.Context` 这种**指针类型声明**表示「引用同一个实例、不拷贝」，`*string` / `*int` 表示「可能没传」（`nil`）。
3. **零值即「未提供」**：`Updates(结构体)` 按零值（`0`、`""`、`false`）判断要不要更新，所以把字段改成 `0` 会被**静默跳过**。第七章的零值陷阱根就在这里。
4. **错误是结果的一部分**：Go 没有异常，每次操作返回 `result`，错误要看 `result.Error`、影响行数看 `result.RowsAffected`。

### 软删除：框架改写 SQL 的又一个例子

第八章的 `Delete` 不会真的删除数据——GORM 把它改写成「软删除」，删掉的记录以后也不会再出现在查询里。机制留到第八章，先记住：**别指望 `Delete` 一定生成 `DELETE` 语句**。

本篇只到单表 CRUD；评论与分页、封面上传、请求 DTO、标签关联、分层与可靠性，依次在后续篇目展开。

## 第一章：项目初始化

建目录、初始化模块：

```bash
mkdir gin-demo
cd gin-demo
go mod init gin-demo
```

安装依赖：

```bash
go get github.com/gin-gonic/gin
go get gorm.io/gorm
go get gorm.io/driver/postgres
```

> `go get` 会解析最新的兼容版本。教程不用 `go get -u`——`-u` 会连坐升级所有间接依赖，没必要。

换数据库就换驱动与 DSN：

| 数据库     | 驱动                             | DSN                                                                           |
| ---------- | -------------------------------- | ----------------------------------------------------------------------------- |
| PostgreSQL | `go get gorm.io/driver/postgres` | `host=localhost user=postgres password=123456 dbname=library sslmode=disable` |
| MySQL      | `go get gorm.io/driver/mysql`    | `user:pass@tcp(localhost:3306)/library?charset=utf8mb4&parseTime=True`        |
| SQLite     | `go get gorm.io/driver/sqlite`   | `./data.db`                                                                   |

## 第二章：连接数据库

启动时调用 `db.InitDB()`：拼出 DSN，用 `gorm.Open` 建立连接，成功后返回 `*gorm.DB` 供其他包使用；失败直接退出。

DSN 就是一行 `key=value`：`host` / `port` / `user` / `password` / `dbname`，本地开发再加 `sslmode=disable`（生产要开 SSL）。

创建 `db/db.go`：

```go
package db

import (
    "fmt"
    "log"

    "gorm.io/driver/postgres"
    "gorm.io/gorm"
)

var DB *gorm.DB

func InitDB() {
    host := "localhost"
    port := 5432
    user := "postgres"
    password := "123456"
    dbname := "library"

    dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
        host, port, user, password, dbname)

    var err error
    DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("数据库连接失败：", err)
    }
    log.Println("数据库连接成功")
}
```

> ⚠️ 生产环境请用环境变量管理敏感配置，不要硬编码——配套仓库用 `DATABASE_DSN` 覆盖整个 DSN，验证/测试靠它切到另一套库。

> **注意：** PostgreSQL 要**手动建库**；`AutoMigrate` 能自动建表但不能建库；`sslmode=disable` 仅限本地开发。

## 第三章：定义数据模型

创建 `models/book.go`，用一个结构体描述表：

```go
package models

import "gorm.io/gorm"

type Book struct {
    gorm.Model
    Title  string `json:"title" gorm:"not null"`
    Author string `json:"author" gorm:"not null"`
    Price  int    `json:"price" gorm:"not null;check:price >= 0"` // 单位：分（5990 表示 59.90 元）
}
```

- `gorm.Model`：内置 `ID`、`CreatedAt`、`UpdatedAt`、`DeletedAt` 四个字段
- `gorm:"not null"`：对应数据库的 `NOT NULL` 约束
- `gorm:"check:price >= 0"`：数据库级 CHECK，负数直接写不进去（校验不能只靠 DTO）
- `json:"title"`：指定 JSON 序列化时的字段名，响应体里也用它

**三个新手最容易忽略的点：**

- **`gorm.Model` 的字段在 JSON 里是大写原名**：它没有 json 标签，所以响应里是 `"ID"`、`"DeletedAt": null`。想统一命名就用自定义字段声明或 DTO（见第十章「响应精简」）。
- **金额统一用 `int`（单位：分）**：浮点数有精度误差（`0.1 + 0.2 != 0.3`），`price: 5990` 表示 59.90 元，前端需要"元"时自行除以 100。
- **`binding:"required"` 加在 DTO 上，不加在模型上**：模型会被更新接口复用，`required` 会把部分更新拦成 400（第五章会用一次）。

**命名规则：**

- 类型名用单数 `Book`（Go 惯例）；表名由 GORM 自动复数化成 `books`，写成 `Books` 没有任何数据库上的收益。文件名随意，本教程统一用 `book.go`。
- 同一个字段在三个层有三个名字：Go 字段 PascalCase、数据库列 snake_case（GORM 自动转换）、JSON camelCase（`json:` 标签）。改列名用 `gorm:"column:..."`，改 JSON 名用 `json:` 标签，两个开关各管各的。
- 需要自定义表名时加一行：`func (Book) TableName() string { return "my_books" }`。

## 第四章：自动迁移

在 `main.go` 里建表（此时还没有路由）：

```go
func main() {
    db.InitDB()

    if err := db.DB.AutoMigrate(&models.Book{}); err != nil {
        log.Fatal("迁移失败：", err)
    }

    // ... 服务与路由见第九章
}
```

- **只加不删**：不删列，也**不撤销已有约束**——去掉 `not null` 标签列仍是 `NOT NULL`，换关联的 `OnDelete` 行为已有外键也不会变；约束变更要显式迁移（`db.Migrator().DropConstraint()` / 手写 SQL）
- 字段的 `size`、`precision`、可空性变化时，GORM 会尝试改列类型
- 重命名字段等于新增一列；要改名用 `db.Migrator().RenameColumn()`

## 先看公式：CRUD 的统一流程

第五到第八章的五个 handler 是同一套流程的实例化。六步骨架：

1. **抓参数**：`id := c.Param("id")` 或 `c.Query("keyword")`
2. **绑请求**：`c.ShouldBindJSON(&xxx)`，失败直接 400
3. **查存在**：`db.DB.First(&xxx, id)`，查无记录（`errors.Is` 命中 `gorm.ErrRecordNotFound`）→ 404
4. **做操作**：`Create` / `Find` / `Updates` / `Delete`——每次操作都返回 `result`
5. **验结果**：非「查无记录」的 `result.Error` → 500；`RowsAffected == 0` → 404
6. **回响应**：`c.JSON(200/201, ...)`

**每章都是公式的一个实例：**

| 操作                | 抓参数 | 绑请求 | 查存在        | 做操作    | 验结果                                | 响应 |
| ------------------- | ------ | ------ | ------------- | --------- | ------------------------------------- | ---- |
| `POST /books`       | –      | ✓      | –             | `Create`  | `Error` → 500                         | 201  |
| `GET /books`        | –      | –      | –             | `Find`    | `Error` → 500                         | 200  |
| `GET /books/:id`    | ✓      | –      | `First` → 404 | –         | `ErrRecordNotFound` → 404             | 200  |
| `PUT /books/:id`    | ✓      | ✓      | `First` → 404 | `Updates` | `Error` → 500                         | 200  |
| `DELETE /books/:id` | ✓      | –      | –             | `Delete`  | `Error` → 500；`RowsAffected`=0 → 404 | 200  |

两个和其他框架最不一样的地方：

- **Gin 的数据入口只有一个 `*gin.Context`**：路径参数、查询参数、请求体都从它身上拿，没有控制器类，一个函数签名通吃所有请求。
- **GORM 每一步都返回同一个 `result`**：`*gorm.DB` 既是链式调用的承接者，也是 `Error` 和 `RowsAffected` 的载体——这就是心智模型 4 在 API 层的落点。

另外，本篇所有 db 调用都链上请求上下文：`db.DB.WithContext(c.Request.Context()).Xxx(...)`（原因见第五章）。

## 第五章：创建图书

第六、七、八章的 CRUD 函数都追加到同一个文件 `handlers/book.go`，文件头统一为：

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
```

第一个接口，创建图书：

```go
// CreateBook 创建图书（POST /books）
func CreateBook(c *gin.Context) {
    var book models.Book

    // 1. 绑定 JSON 请求体
    if err := c.ShouldBindJSON(&book); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "请发送合法的 JSON"})
        return
    }

    // 2. 插入数据库
    result := db.DB.WithContext(c.Request.Context()).Create(&book)
    if result.Error != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "创建失败"})
        return
    }

    // 3. 返回创建的数据（这就是「结构体一物三用」：请求装进来、DB 插进去、原样返回）
    c.JSON(http.StatusCreated, book)
}
```

> ⚠️ 路由到第九章才注册。建议先跳到第九章把 `main.go` 抄下来跑起服务，再回头逐章测试第五~八章的接口。

测试：

```bash
curl -X POST http://localhost:8080/books \
  -H "Content-Type: application/json" \
  -d '{"title":"Go语言实战","author":"张三","price":5990}'
```

响应示例（注意键名大小写）：

```json
{
  "ID": 1,
  "CreatedAt": "2026-09-05T13:05:46+08:00",
  "UpdatedAt": "2026-09-05T13:05:46+08:00",
  "DeletedAt": null,
  "title": "Go语言实战",
  "author": "张三",
  "price": 5990
}
```

> `ID` / `CreatedAt` / `DeletedAt` 大写是 `gorm.Model` 没有 json 标签导致的（见第三章）；`title` / `author` / `price` 是我们自己用 tag 定义的。

### 进阶：加参数校验

上面的代码不校验请求内容，`{"price":5990}`（没有 `title` / `author`）也能插入成功。校验标签加在**收请求的结构**上，而不是 `models.Book` 上：

**第一步：定义请求结构（DTO）。** 放在 `handlers/book.go` 里、`CreateBook` 上方即可（小写开头够用；若以后要跨包引用，改成导出的 `CreateBookInput`）：

```go
// createBookInput：创建图书专用的请求结构（DTO），校验标签只属于它
type createBookInput struct {
    Title  string `json:"title" binding:"required"`
    Author string `json:"author" binding:"required"`
    Price  int    `json:"price" binding:"gte=0"`
}
```

**为什么不在 `models.Book` 上加 `binding:"required"`？** 同一个模型还被第七章的更新接口复用——更新是部分更新（只传 `{"price":6990}`），模型上有 `required` 会让它被 400 拦下。所以：**模型管数据库结构与 JSON 命名，校验是接口契约、属于请求结构（DTO）**；正式落地见[《数据工程实战》](./gorm-gin-dto-batch)。

**第二步：让 `CreateBook` 真的绑这个结构。** 只定义 DTO 不换绑定目标，代码一个字都不会变（缺字段照样插库成功）——把第五章开头那版 `CreateBook` 整体替换成下面这版：

```go
// CreateBook 创建图书（POST /books）——先绑 DTO 校验，再翻译成模型入库
func CreateBook(c *gin.Context) {
    // 1. 绑定到 DTO：binding 标签在这一层生效，缺字段当场 400
    var input createBookInput
    if err := c.ShouldBindJSON(&input); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "请发送合法的 JSON（title、author 必填，price 不能为负）"})
        return
    }

    // 2. DTO → 模型：只搬「客户端可以决定」的字段
    //    DeletedAt / ID / CreatedAt 这些不许客户端碰的字段，DTO 里根本没有，自然搬不过来
    book := models.Book{
        Title:  input.Title,
        Author: input.Author,
        Price:  input.Price,
    }

    // 3. 插入数据库（注意传给 Create 的是 book，不是 input）
    result := db.DB.WithContext(c.Request.Context()).Create(&book)
    if result.Error != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "创建失败"})
        return
    }

    // 4. 返回落库后的模型（响应形状和第五章开头完全一致，多出来的 ID/CreatedAt 由 GORM 填）
    c.JSON(http.StatusCreated, book)
}
```

> **为什么不直接 `Create(&input)`？** 因为 `input` 是 DTO、`book` 是模型：模型嵌了 `gorm.Model`（主键、时间戳、软删），将来还会加 `cover_path` 这类不归客户端管的列。DTO 到模型的这一段「翻译」是刻意的安全检查——只有你显式写进 `models.Book{...}` 的字段才会落库，客户端多传的键（比如 `"deletedAt"`）在绑定时就丢了。这就是后文反复提到的「收什么和存什么是两件事」。

改完之后，缺字段的请求才真的会走 400 分支：

```bash
# 缺少必填字段 → 400（原代码这里是 201）
curl -s -o /dev/null -w '%{http_code}\n' -X POST http://localhost:8080/books \
  -H "Content-Type: application/json" -d '{"price":5990}'

# 完整字段 → 201，响应与第五章开头的示例一致
curl -s -X POST http://localhost:8080/books \
  -H "Content-Type: application/json" -d '{"title":"Go语言实战","author":"张三","price":5990}'
```

### 为什么必须带 Context

`WithContext(ctx)` 把 Go 的 `context.Context` 挂到本次查询链上；执行时它会贯穿 `database/sql` → 数据库驱动，成为**取消与超时的信号来源**。

`c.Request.Context()` 的生命周期绑定在请求上：**客户端断开连接时它会被取消**。带上它，查询会随之中断、连接归还连接池而不是挂死；不带的裸写法（`db.DB.Create(&book)`）也能跑，但对请求生命周期毫无感知。

所以本篇所有 db 调用都写成 `db.DB.WithContext(c.Request.Context()).Xxx(...)`。服务端防慢查询的超时中间件在第九章的 `main.go` 里；超时后 GORM 返回 `context.DeadlineExceeded`，会走 500，生产环境可映射成 504（见第 7 篇）。

## 第六章：查询图书

两个查询接口：

```go
// GetBooks 查询所有图书（GET /books）
func GetBooks(c *gin.Context) {
    books := []models.Book{} // 空切片而非 nil：表为空时 JSON 输出 []，而不是 null
    result := db.DB.WithContext(c.Request.Context()).Find(&books)
    if result.Error != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
        return
    }
    c.JSON(http.StatusOK, books)
}

// GetBook 查询单条图书（GET /books/:id）
func GetBook(c *gin.Context) {
    id := c.Param("id")

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

    c.JSON(http.StatusOK, book)
}
```

测试：

```bash
curl http://localhost:8080/books
curl http://localhost:8080/books/1
```

> **为什么 `First` 的检查分两层？** 查无记录时 GORM 返回 `gorm.ErrRecordNotFound`，用 `errors.Is` 精确命中它才返回 404；数据库断开这类错误落到 500，不会误报成"图书不存在"。`GetBooks` 用 `[]models.Book{}` 而非 `var books []models.Book`，因为后者在空表时输出 `null` 而不是 `[]`。

## 第七章：更新图书

```go
// UpdateBook 更新图书（PUT /books/:id）
func UpdateBook(c *gin.Context) {
    id := c.Param("id")

    // 1. 检查图书是否存在（和第六章 GetBook 是同一套两层检查——这次自己写出来）
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

    // 2. 绑定 JSON 请求体
    var input models.Book
    if err := c.ShouldBindJSON(&input); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "请发送合法的 JSON"})
        return
    }

    // 3. 更新字段（仅更新 input 中的非零字段，WHERE 条件由上方 First 决定）
    result = db.DB.WithContext(c.Request.Context()).Model(&book).Updates(input)
    if result.Error != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "更新失败"})
        return
    }

    // 4. 返回更新后的数据（GORM 会把更新的非零字段回写 book，与落库一致；零值字段保持上方 First 加载的值）
    c.JSON(http.StatusOK, book)
}
```

### PUT 还是 PATCH？

`PUT` 严格语义是「整体替换」，部分更新该用 `PATCH`。本章的 `Updates` 只改传入的非零字段，本质是 PATCH 语义——入门示例用 `PUT` 没问题，想更严谨就把路由改成 `r.PATCH("/books/:id", handlers.UpdateBook)`。

### 零值陷阱

`Updates` 传入**结构体**时，GORM 默认忽略零值字段（`0`、`""`、`false`），所以 `{"price":0}` 会被静默跳过。这是设计如此，但"把价格改成 0"就做不到。

解法是**指针 DTO**：`nil` 表示"没传"，`&0` 表示"传了 0"：

```go
type updateBookInput struct {
    Title  *string `json:"title"`
    Author *string `json:"author"`
    Price  *int    `json:"price" binding:"omitempty,gte=0,lte=1000000"`
}

var in updateBookInput
if err := c.ShouldBindJSON(&in); err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": "请发送合法的 JSON"})
    return
}
// 只把非 nil 的字段放进更新
fields := map[string]any{}
if in.Title != nil { fields["title"] = *in.Title }
if in.Author != nil { fields["author"] = *in.Author }
if in.Price != nil { fields["price"] = *in.Price }
db.DB.WithContext(c.Request.Context()).Model(book).Updates(fields)
```

> **⚠️ 别把模型当请求体。** 上面 `var input models.Book` 直接拿模型收请求：JSON 绑定不区分大小写，`{"deletedAt":"2026-01-01T00:00:00Z"}` 能把书软删、`{"coverUrl":"/uploads/x.jpg"}` 能绕过上传接口改封面——等于开放了批量赋值。修法就是上面这个 DTO：**收什么和存什么是两件事**。本篇保留模型版本只为让零值陷阱看得见；上线代码请用指针 DTO（[《数据工程实战》](./gorm-gin-dto-batch)会把它正式落地）。
>
> 想少写一点也可以用 `map[string]any` + 键白名单，但 map 里的数字会变成 `float64`、类型保证全丢，不推荐。

测试：

```bash
curl -X PUT http://localhost:8080/books/1 \
  -H "Content-Type: application/json" \
  -d '{"price":6990}'
```

## 第八章：删除图书

因为 `Book` 使用了 `gorm.Model`，GORM 默认执行**软删除**：记录不真正移除，只是 `deleted_at` 被设为当前时间，查询时默认被过滤掉。

```go
// DeleteBook 软删除图书（DELETE /books/:id）
func DeleteBook(c *gin.Context) {
    id := c.Param("id")

    // 执行软删除
    result := db.DB.WithContext(c.Request.Context()).Delete(&models.Book{}, id)
    if result.Error != nil { // 先验错误，再判行数——顺序不能反
        c.JSON(http.StatusInternalServerError, gin.H{"error": "删除失败"})
        return
    }
    if result.RowsAffected == 0 {
        c.JSON(http.StatusNotFound, gin.H{"error": "图书不存在"})
        return
    }

    c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}
```

### 软删除行为解析

GORM 实际执行的是 `UPDATE ... SET deleted_at=NOW() WHERE id=? AND deleted_at IS NULL`：

| 操作               | GORM 行为                                                                             |
| ------------------ | ------------------------------------------------------------------------------------- |
| 第一次 DELETE      | 设置 `deleted_at = NOW()`，不再出现在查询中                                           |
| 再次 DELETE 同一条 | 由于 `deleted_at IS NOT NULL`，GORM 找不到记录，`RowsAffected == 0`，返回"图书不存在" |

软删除后，默认的 `First` / `Find` 会自动加上 `deleted_at IS NULL`；要查已删除的记录，用 `db.DB.WithContext(c.Request.Context()).Unscoped().First(&book, id)`。

> **⚠️ 唯一约束的坑**：软删除的行仍占着唯一索引——带唯一索引的字段（比如 ISBN）在软删后重建同名记录会撞约束。PostgreSQL 的解法是**部分唯一索引**：`CREATE UNIQUE INDEX ... WHERE deleted_at IS NULL`，只让未删除的行参与唯一性。第 5 篇会在真实的 `tags.name` 上用一遍。

### 如果需要物理删除（彻底删除）

> 该函数属于**可选项**。第九章的完整版 `main.go` 默认注册了它；不想对外开放就把那行删掉。

```go
// DeleteBookPermanently 物理删除图书（DELETE /books/:id/permanent）
func DeleteBookPermanently(c *gin.Context) {
    id := c.Param("id")
    // Unscoped() 绕过软删除，执行物理删除
    result := db.DB.WithContext(c.Request.Context()).Unscoped().Delete(&models.Book{}, id)
    if result.Error != nil { // 先验错误，再判行数（与 DeleteBook 同一套顺序）
        c.JSON(http.StatusInternalServerError, gin.H{"error": "删除失败"})
        return
    }
    if result.RowsAffected == 0 {
        c.JSON(http.StatusNotFound, gin.H{"error": "图书不存在"})
        return
    }
    c.JSON(http.StatusOK, gin.H{"message": "物理删除成功"})
}
```

> **⚠️ 有子记录时会失败**：第 2 篇给 `Book` 加上评论关联后，GORM 建的外键默认 `NO ACTION`——删一本有评论的书会被 PostgreSQL 拒绝（`violates foreign key constraint`）。解法是给关联声明加 `constraint:OnDelete:CASCADE`（第 2 篇的模型已带），或在事务里先删评论。软删除不受影响。

测试：`curl -X DELETE http://localhost:8080/books/1`。

## 第九章：注册路由

把前面的东西串起来——路由部分是：

```go
r := gin.Default()
r.Use(requestTimeout(5 * time.Second)) // 请求级超时：慢查询被 context 中断

r.POST("/books", handlers.CreateBook)
r.GET("/books", handlers.GetBooks)
r.GET("/books/:id", handlers.GetBook)
r.PUT("/books/:id", handlers.UpdateBook) // 严格 REST 语义下部分更新用 PATCH
r.DELETE("/books/:id", handlers.DeleteBook)
r.DELETE("/books/:id/permanent", handlers.DeleteBookPermanently) // 可选

r.Run(":8080")
```

`requestTimeout` 就是第五章说的"给每个请求挂超时兜底"：把 `context.WithTimeout` 派生出的 ctx 写回请求，下游的 `WithContext` 才能感知超时。

完整 `main.go`（含 import、数据库初始化、迁移、中间件与上面这些路由）：

<details>
<summary>完整 main.go（点击展开）</summary>

```go
package main

import (
    "context"
    "log"
    "time"

    "gin-demo/db"
    "gin-demo/handlers"
    "gin-demo/models"
    "github.com/gin-gonic/gin"
)

// 给每个请求挂一个超时兜底（第五章）
func requestTimeout(d time.Duration) gin.HandlerFunc {
    return func(c *gin.Context) {
        ctx, cancel := context.WithTimeout(c.Request.Context(), d)
        defer cancel()
        c.Request = c.Request.WithContext(ctx)
        c.Next()
    }
}

func main() {
    // 连接数据库
    db.InitDB()

    // 自动迁移
    if err := db.DB.AutoMigrate(&models.Book{}); err != nil {
        log.Fatal("迁移失败：", err)
    }

    r := gin.Default()
    // 请求级超时：慢查询会被 context 中断，走 500（生产可映射 504）
    r.Use(requestTimeout(5 * time.Second))

    // RESTful API 路由
    r.POST("/books", handlers.CreateBook)
    r.GET("/books", handlers.GetBooks)
    r.GET("/books/:id", handlers.GetBook)
    r.PUT("/books/:id", handlers.UpdateBook) // 严格 REST 语义下部分更新用 PATCH
    r.DELETE("/books/:id", handlers.DeleteBook)
    // 可选：第八章的物理删除示例（默认软删除即可满足需求）
    r.DELETE("/books/:id/permanent", handlers.DeleteBookPermanently)

    r.Run(":8080")
}
```

</details>

跑起来后就可以用 curl 逐章测试，包括物理删除：`curl -X DELETE http://localhost:8080/books/1/permanent`。

## 第十章：总结

| 操作       | GORM 方法                                  | 对应 SQL                                       |
| ---------- | ------------------------------------------ | ---------------------------------------------- |
| 创建       | `db.Create(&book)`                         | `INSERT INTO ...`                              |
| 查询所有   | `db.Find(&books)`                          | `SELECT * FROM ...`                            |
| 查询单条   | `db.First(&book, id)`                      | `SELECT * FROM ... WHERE id = ?`               |
| 更新       | `db.Model(&book).Updates(input)`           | `UPDATE ... SET ...`                           |
| 软删除     | `db.Delete(&models.Book{}, id)`            | `UPDATE ... SET deleted_at = NOW()`            |
| 物理删除   | `db.Unscoped().Delete(&models.Book{}, id)` | `DELETE FROM ...`                              |
| 查询已删除 | `db.Unscoped().First(&models.Book{}, id)`  | `SELECT * FROM ... WHERE id = ?`（不限软删除） |

> 全文的 5 个 handler 都是「先看公式」节六步骨架的实例化：参数 → 绑定 → 存在 → 操作 → 结果 → 响应；所有 db 操作均链上 `WithContext(c.Request.Context())`，查询用 `errors.Is` 区分「查无记录」（404）与其它错误（500）。

### 进阶方向

- 事务：`db.Transaction()`
- 钩子函数：`BeforeCreate`、`AfterUpdate`
- 响应精简：`gorm.Model` 的大写键怎么统一——自定义字段声明，或 DTO 做统一响应结构
- 连接池：`sqlDB, _ := db.DB.DB()` 拿到 `*sql.DB` 后，用 `SetMaxOpenConns()` / `SetConnMaxLifetime()` 配置
- 超时映射：`errors.Is(err, context.DeadlineExceeded)` 时返回 504，而不是笼统的 500
- SQL 调试：`&gorm.Config{Logger: logger.Default.LogMode(logger.Info)}`
