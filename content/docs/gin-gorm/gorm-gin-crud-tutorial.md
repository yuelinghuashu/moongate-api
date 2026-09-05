---
title: GORM 入门实战：用 Gin + GORM 写一个图书管理 API
description: 从零搭建一个完整的图书管理 API，涵盖 GORM 的 CRUD、软删除、零值陷阱等核心知识点，附带完整代码和测试命令
date: 2026-09-01
series: gin-gorm
level: P2
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

## 适合读者

- 已掌握 Go 基础语法
- 想学 GORM 但不知道从哪开始
- 想看到一个能直接运行的完整项目
- 有其他语言（Java/Python/Node 等）Web 开发经验更佳——本文会顺带对比常见框架的写法差异

## 环境准备

动手前确认三样东西：

- **Go 1.22+**（本教程用到 `errors.Is`；泛型只在系列工程化篇出现，1.21+ 均可，推荐 1.24+）；
- **本地 PostgreSQL**（无安装可用 Docker：`docker run --name pg -e POSTGRES_PASSWORD=123456 -p 5432:5432 -d postgres:16`）；
- **手动创建数据库**：`CREATE DATABASE library;`——GORM 的 `AutoMigrate` 只能建表，不能建库（见第二章注意事项）。

想换 MySQL / SQLite 也可以——本篇的代码本身跨库通用，差别只在驱动安装与 DSN（见第一章末尾对照表）。但要提前知道：**入门篇之后的篇目会用 PostgreSQL 专属特性**（`ILIKE` 模糊搜索、聚合查询按主键分组的"函数依赖"、软删除的部分唯一索引），正文会在用到处就地标注差异，并把它们集中收在媒体篇末尾的「PostgreSQL 特性速查」表里。

## 完整项目结构

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

> **学习级结构：** `db` / `models` / `handlers` 平铺适合小项目与入门。业务复杂后建议按职责演进——用 Go 的 `internal/` 包约束可见性、抽出 Service 层放业务逻辑、Repository 层收拢数据访问。教程保持平铺以聚焦 GORM，先跑通再谈分层。想对 handler 做单元测试、需要 mock 数据库时，再把数据访问收拢为接口（如 `BookRepository`）注入（见第十章进阶方向）。

## 先建立心智模型：GORM 的核心理念

在动手写代码之前，先用三十秒建立正确的「心智模型」——它回答的是"GORM 和别的路子哪里不一样"。GORM 最不一样的对手不是 Spring/Django 这类框架，而是 JDBC/MyBatis 这种手写 SQL 的路子：后者要你自己拼 SQL、再把结果集一行行映射成对象，GORM 恰好反过来。先理解下面这套思维方式，再动手，比照抄代码重要得多。

### 一句话总纲

> Gin 负责把「HTTP 请求」变成「Go 函数调用」，GORM 负责把「Go 结构体」翻译成「数据库 SQL」。整篇文章的思维主线只有一条：**请求进来 → 装进结构体 → 交给 GORM → 结果填回结构体 → 返回 JSON**。

### 不是写 SQL，是操作结构体

Java 的 JDBC / MyBatis、PHP 手写 PDO 这类技术里，你需要自己拼 SQL 字符串，再把结果集一行行手动映射成对象。GORM 反过来：你只描述**意图**（`Create`、`Find`、`Updates`、`Delete`），翻译成 SQL 是框架的事——

| GORM 方法（节选）                | 对应 SQL 意图                                       |
| -------------------------------- | --------------------------------------------------- |
| `db.Create(&book)`               | `INSERT INTO books ...`                             |
| `db.Model(&book).Updates(input)` | `UPDATE books SET ...`                              |
| `db.Delete(&models.Book{}, id)`  | `UPDATE books SET deleted_at = NOW() ...`（软删除） |

想确认 GORM 到底生成了什么 SQL？开启 GORM 日志就能看到（见第十章）。带着「意图」写代码，不要试图在脑子里逐条翻译 SQL；完整的方法 ↔ SQL 对照见第十章总结。

### 四个必须建立的心智

1. **结构体一物三用**：同一个 struct 同时扮演三个角色——数据库表结构定义（`gorm` 标签）、请求/响应的数据载体（`json` 标签）、数据库操作的参数（`&book`）。类型即契约，改一处全联动。这与 Java 中 Entity / DTO 分离、再配一套 XML 映射的写法完全不同。
2. **查数据是「填空」，不是「返回值」**：Go 是值传递，所以 `Find(&books)`、`First(&book, id)` 必须传目标变量的**指针**，GORM 靠反射把结果填进去。漏写 `&` 等于填了一个副本，函数外拿不到数据——这是新手最容易犯、也最反直觉的一处，因为 Java/Python 的对象引用天然是共享的。
3. **零值即「未提供」**：Go 规定每个变量都有零值（数字 `0`、字符串 `""`、布尔 `false`）。GORM 的 `Updates(结构体)` 正是根据零值判断「这个字段要不要更新」——所以把字段更新成 `0` 或 `""` 会被**静默跳过**（不报错，也不更新）。第七章的「零值陷阱」根就在这里。对比 Java 的 `null`、Python 的 `None`——它们表示"没有值"；Go 的零值却是一个真实的值，`0` 明明是"想把字段更新成 0"的意图，却被 GORM 当成"未提供"。这种差异正是零值陷阱对新手最反直觉的地方。
4. **错误是结果的一部分**：Go 没有异常机制。GORM 把每次操作的结果封装成 `result`，你要自己检查 `result.Error` 有没有错、`result.RowsAffected` 影响了几行。整篇文章你会反复看到这个模式——它取代了其他语言里的 `try/catch`。

### 小注：为什么到处是 `&` 和 `*`？

新手最容易卡在这一处——GORM 和 Gin 的代码里满是 `&` 与 `*`，却说不上来为什么。其实它们各管一件事：

- **`&x`（取地址传参）=「这个变量归你填，改完要带回来」**：Go 默认值传递，传下去的是拷贝。GORM 的 `First(&book, id)`、`Find(&books)` 和 Gin 的 `c.ShouldBindJSON(&book)` 都要往里**填**数据，`Delete(&models.Book{}, id)`、`AutoMigrate(&models.Book{})` 要拿到**对象本身**——不传 `&`，函数外拿不到结果（这就是心智点 2「填空」的通用版：不止查询，绑定/删除/迁移全在用）。
- **`*gorm.DB` / `*gin.Context`（指针类型声明）=「引用同一个实例，不拷贝」**：这两个类型本身就被声明成指针。`db.DB` 是数据库连接句柄、`*gin.Context` 是请求上下文——全局只有一份，到处传递的是**指向它的地址**，省拷贝且保证操作的是同一实例。
- **`*string` / `*int`（DTO 指针字段）=「可能没传」**：`nil` 表示"这个字段没出现"，与空串/0 区分开——第七章正文用的是结构体与 map，指针 DTO 的正式落地在[《数据工程实战》](./gorm-gin-dto-batch)与第十章进阶方向。

一句话：**`&` 是「填这里」，`*` 是「这就是引用/可能没有」**——它们不是 GORM 的魔法，是 Go 传值与引用语义的体现，所有框架都一样。

### 软删除：框架改写 SQL 的又一个例子

第八章的 `Delete` 不会真的删除数据——GORM 会把它改写成「软删除」，删掉的记录以后也不会再出现在查询里。具体机制留到第八章展开，先记住：**别指望 `Delete` 一定生成 `DELETE` 语句**（上面对照表中的「（软删除）」就是伏笔）。

### 和本文各章的关系

- 第二、三、四章（连接、模型、迁移）——「结构体 ↔ 表」的地基；
- 第五~八章（增删改查）——上面四个心智点的实战演练；
- 第九、十章（路由、总结）——把一切串起来并回顾。

现在带着这套心智进入第一章，边敲代码边印证。

## 第一章：项目初始化

### 目标：创建项目目录，安装依赖

```bash
mkdir gin-demo
cd gin-demo
go mod init gin-demo
```

### 安装依赖

```bash
# Web 框架
go get github.com/gin-gonic/gin

# ORM 库 + PostgreSQL 驱动
go get gorm.io/gorm
go get gorm.io/driver/postgres
```

> `go get` 会按当前环境解析最新兼容版本（等价于 `@latest`）。教程不用 `go get -u`——`-u` 会连坐升级所有间接依赖，没必要时反而引入不确定性。

### 数据库驱动说明

本文使用 PostgreSQL 作为示例数据库。如果你使用的是其他数据库，替换对应的驱动即可：

| 数据库     | 安装命令                         | DSN 格式                                                                      |
| ---------- | -------------------------------- | ----------------------------------------------------------------------------- |
| PostgreSQL | `go get gorm.io/driver/postgres` | `host=localhost user=postgres password=123456 dbname=library sslmode=disable` |
| MySQL      | `go get gorm.io/driver/mysql`    | `user:pass@tcp(localhost:3306)/library?charset=utf8mb4&parseTime=True`        |
| SQLite     | `go get gorm.io/driver/sqlite`   | `./data.db`                                                                   |

## 第二章：连接数据库

### 目标：建立数据库连接，在项目启动时初始化

### 连接流程

1. `main.go` 启动时调用 `db.InitDB()`
2. `db.InitDB()` 构造 DSN 字符串，通过 `gorm.Open()` 建立连接
3. 连接成功返回 `*gorm.DB` 实例，失败则退出程序

---

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

> ⚠️ 生产环境请用环境变量（如 `os.Getenv` 或 `godotenv`）管理敏感配置，不要硬编码。

### 代码说明

| 代码                                            | 说明                                 |
| ----------------------------------------------- | ------------------------------------ |
| `var DB *gorm.DB`                               | 声明全局 DB 变量，供其他包使用       |
| `gorm.Open(postgres.Open(dsn), &gorm.Config{})` | 建立数据库连接                       |
| `log.Fatal`                                     | 连接失败时终止程序，避免后续代码执行 |

### DSN 参数说明

| 参数       | 示例        | 说明                |
| ---------- | ----------- | ------------------- |
| `host`     | `localhost` | 数据库主机地址      |
| `port`     | `5432`      | PostgreSQL 默认端口 |
| `user`     | `postgres`  | 数据库用户名        |
| `password` | `123456`    | 数据库密码          |
| `dbname`   | `library`   | 数据库名称          |
| `sslmode`  | `disable`   | 本地开发禁用 SSL    |

> **注意事项：**
>
> - PostgreSQL 需要**手动创建数据库**：`CREATE DATABASE library;`
> - GORM 的 `AutoMigrate` 能自动创建表，但**不能自动创建数据库**
> - `sslmode=disable` 仅用于本地开发，生产环境应开启 SSL

## 第三章：定义数据模型

### 目标：用 Go 结构体定义数据库表结构

创建 `models/book.go`：

```go
package models

import "gorm.io/gorm"

type Book struct {
    gorm.Model
    Title  string  `json:"title" gorm:"not null"`
    Author string  `json:"author" gorm:"not null"`
    Price  int     `json:"price"` // 单位：分（5990 表示 59.90 元）
}
```

### 字段说明

- `gorm.Model`：内置了 `ID`、`CreatedAt`、`UpdatedAt`、`DeletedAt` 四个字段
- `gorm:"not null"`：对应数据库的 `NOT NULL` 约束
- `json:"title"`：指定 JSON 序列化时的字段名，响应体里也用它

> **三个新手最容易忽略的点：**
>
> - **`gorm.Model` 的字段会以大写的 Go 字段名出现在 JSON 里**：`gorm.Model` 没有 json 标签，所以响应里是 `"ID"`、`"CreatedAt"`、`"DeletedAt": null` 这样的原名（注意大写）。想隐藏或统一命名，就用**自定义字段声明**替代内嵌 `gorm.Model`（字段上加 `json:"-"` 或小写 tag），或定义专门的 DTO（数据传输对象）作为响应结构。
> - **金额统一用 `int`（单位：分）**：浮点数有精度误差（`0.1 + 0.2 != 0.3`），金额字段直接用整数（单位：分）规避——接口里 `price: 5990` 表示 59.90 元。前端需要"元"时自行除以 100（或自定义 `MarshalJSON`）；涉及汇率/费率等需要精确小数的业务才引入 `decimal` 库（见第十章进阶方向）。
> - **`binding:"required"` 自动校验（加在 DTO 上，不是模型上）**：给请求结构（DTO）的字段加上 `binding:"required"` 后，Gin 在 `ShouldBindJSON` 阶段就会校验，缺少必填字段直接返回 400——第五章进阶会实际用到。

### 命名规则（重要）

- **结构体类型名用单数**：`Book` 而不是 `Books`。这是 Go 的惯例（标准库里的 `http.Server`、`time.Time` 都是单数），语义也更清晰——一个结构体实例就是一条记录。写成 `Books` 不会让表名变成别的，反而会和 `CreateBook`、`GetBook` 这类单数函数名冲突。
- **表名由 GORM 自动复数化**：类型 `Book` 对应的表名是 `books`（由 inflection 库处理）；即使把类型写成 `Books`，表名也还是 `books`，所以复数类型名没有任何数据库上的收益。
- **文件名可以用复数，但类型名必须是单数**：`book.go` 和 `books.go` 都合法（一个文件放一组相关类型时复数更常见），本教程统一用单数 `book.go`。
- 需要自定义表名时，用 `TableName()` 方法：

```go
func (Book) TableName() string { return "my_books" }
```

**三套命名：Go 字段、数据库列、JSON 各说各话：**

同一个逻辑字段在三个层有三个名字，互不冲突——翻译者是 GORM（列名）和 `json:` 标签（JSON）：

| 层        | 名字                    | 由谁决定                                                      |
| --------- | ----------------------- | ------------------------------------------------------------- |
| Go 字段   | `BookID`（PascalCase）  | Go 标识符惯例                                                 |
| 数据库列  | `book_id`（snake_case） | GORM 的 `NamingStrategy` 从字段名自动转换，一般无需 `column:` |
| JSON 输出 | `bookId`（camelCase）   | `json:"bookId"` 标签，只影响序列化                            |

列名与 JSON 名互不干扰——以表中假设的 `BookID` 为例（你项目里换成 `Title`/`Author` 等真实字段同理）：库里永远是 `book_id`，前端看到的是 `bookId`。想改列名用 `gorm:"column:..."`，想让 JSON 叫别的用 `json:` 标签——两个开关各管各的。

> **补充：没有 `json:` 标签时呢？** 上面表格讲的是"有 json 标签"的字段。没标签的（比如内嵌 `gorm.Model` 的 `ID`、`CreatedAt`）会**直接输出 Go 字段原名**——所以第一次 `curl` 时你会看到 `"ID"`、`"DeletedAt"` 这种大写键（见第三章「三个新手最容易忽略的点」）。

## 第四章：自动迁移

### 目标：程序启动时自动创建或更新表结构

在 `main.go` 中添加：

```go
package main

import (
    "log"
    "gin-demo/db"
    "gin-demo/models"
    "github.com/gin-gonic/gin"
)

func main() {
    // 1. 连接数据库
    db.InitDB()

    // 2. 自动迁移（建表）
    if err := db.DB.AutoMigrate(&models.Book{}); err != nil {
        log.Fatal("迁移失败：", err)
    }

    // 3. 启动 Gin 服务
    r := gin.Default()
    // ... 路由
    r.Run(":8080")
}
```

> 本章的 `main.go` 是骨架版本，第九章会给出注册完所有路由的最终完整版。

### 注意事项

- `AutoMigrate` 会创建缺失的表、列和索引，但**不会删除已有字段**（保护数据）
- 当字段的 `size`、`precision`、可空性（nullable）等属性变化时，GORM 会**尝试修改已有列的类型**
- 字段重命名不会同步改列名：比如把 `Title` 改成 `Name`，GORM 会新增一列而不是改名，此时需要手动迁移或用 `db.Migrator().RenameColumn()`

## 先看公式：CRUD 的统一流程

第五到第八章的五个 handler，看起来各写各的，实际全是**同一套流程的实例化**。先看公式，再进代码——和心智模型一样，先拿到地图再进迷宫。

**六步骨架：**

1. **抓参数**：`id := c.Param("id")` 或 `c.Query("keyword")`——Gin 的数据入口之一
2. **绑请求**：`c.ShouldBindJSON(&xxx)`，失败直接返回 400
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

**Gin + GORM 特化说明（与其他框架最不一样的地方）：**

- **Gin 的数据入口只有一个 `*gin.Context`**：路径参数（`c.Param`）、查询参数（`c.Query`）、请求体（`c.ShouldBindJSON`）都从它身上拿——没有控制器类、没有依赖注入，一个函数签名通吃所有请求。
- **GORM 每一步都返回同一个 `result`**：`*gorm.DB` 既是链式调用的承接者，也是 `Error` 和 `RowsAffected` 的载体——「验结果」这一步，就是心智模型里「错误是结果的一部分」落到 API 层的形态。
- **传指针的分野**：要往变量里填数据就必须传 `&`（`Create(&book)`、`First(&book, id)`）；删除操作不需要填数据，传类型即可（`Delete(&models.Book{}, id)`）。
- **状态码即约定**：400 参数问题 / 404 不存在 / 500 服务端错误 / 201 创建成功，整套文章都用这套映射。
- **生产惯例：每个 db 调用都链上请求上下文**：`db.WithContext(c.Request.Context()).Xxx(...)`——机制与超时中间件见第五章进阶。

现在进入第五章，对照公式看 `CreateBook` 是怎么实例化第 2、4、5、6 步的。

## 第五章：创建图书

### 目标：实现 `POST /books` 接口，接收 JSON 请求并存入数据库

> **注：** 后续第六、七、八章的所有 CRUD 函数均追加至同一个文件 `handlers/book.go` 中。开头统一为：

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

### 创建图书

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

> ⚠️ **测试时机：** 路由到第九章才注册，当前 `main.go` 只是骨架（`// ... 路由`）。建议先跳到第九章、把完整版 `main.go` 抄下来跑起服务，再回头逐章测试第五~八章接口（handler 代码按各章顺序照写即可）。

**测试**：

```bash
curl -X POST http://localhost:8080/books \
  -H "Content-Type: application/json" \
  -d '{"title":"Go语言实战","author":"张三","price":5990}'
```

**响应示例（注意键名大小写）：**

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

> 注意键名大小写：`ID` / `CreatedAt` / `DeletedAt` 是大写，因为 `gorm.Model` 没有 json 标签，直接输出 Go 字段原名（见第三章「三个新手最容易忽略的点」）；`title` / `author` / `price` 是我们自己用小写 tag 定义的。想统一成全小写（`id`、`createdAt`），有两条路：改用自定义字段声明，或定义 DTO 作为统一响应结构（见第十章进阶方向「响应精简」）。

### 进阶：为什么必须带 Context

注意上面的写法里有一个容易忽略的生产细节：所有数据库调用都链上了 `WithContext(c.Request.Context())`。下面拆开讲它是什么、为什么必须带、怎么配超时。

**WithContext 是什么？**

`WithContext(ctx)` 是 GORM 链式 API 的一环，把 Go 的 `context.Context` 挂到本次查询链上；执行 SQL 时，这个 ctx 会贯穿 `database/sql` → 数据库驱动（Postgres 驱动底层是 pgx），成为**取消与超时的信号来源**。它和 `Where` / `Order` 一样只是链上的一个方法——不带它查询也能跑：

```go
// 不带：查询照跑，但无法感知请求是否已终止
db.DB.First(&book, id)

// 带：ctx 的取消 / 超时信号会传导到数据库驱动层
db.DB.WithContext(c.Request.Context()).First(&book, id)
```

**为什么必须带？**

`c.Request.Context()` 的生命周期绑定在请求上：**客户端断开连接时它会被取消**。带上了它，数据库查询会随之中断、连接归还连接池而不是挂死；不带 Context 的裸写法（如 `db.DB.Create(&book)`）也能跑，但查询对请求生命周期毫无感知——所以从第五章起，正文代码统一使用带 Context 的写法（`Create` / `Find` / `Updates` / `Delete` 同理）。

**相关配置：请求级超时中间件**

客户端断开由框架自动取消，但服务端还要防"慢查询拖垮连接"——用中间件给每个请求挂一个超时兜底（函数放在 `main.go`，第九章的完整版会注册它）：

```go
// 完整函数与注册见第九章 main.go；核心就是四行：
ctx, cancel := context.WithTimeout(c.Request.Context(), d)
defer cancel()                         // 释放计时器，防止泄漏
c.Request = c.Request.WithContext(ctx) // 不写回则下游 WithContext 感知不到超时
c.Next()
```

超时后 GORM 返回 `context.DeadlineExceeded`，会走现有的 500 分支（生产环境可进一步映射 504，见第十章进阶方向）。

### 进阶：加参数校验

上面的代码不校验请求内容，`{"price":5990}`（没有 `title` / `author`）也能插入成功。校验标签加在哪？——**加在"收请求的结构"上，而不是 `models.Book` 上**：

```go
// createBookInput：创建图书专用的请求结构（DTO），校验标签只属于它
type createBookInput struct {
    Title  string `json:"title" binding:"required"`
    Author string `json:"author" binding:"required"`
    Price  int    `json:"price" binding:"gte=0"`
}
```

**为什么不在 `models.Book` 上加 `binding:"required"`？** 同一个模型还被第七章的更新接口复用——更新是部分更新（只传 `{"price":6990}`，见零值陷阱），模型上有 `required` 会让它被 400 拦下。所以：**模型管数据库结构与 JSON 命名（`gorm` / `json` 标签），校验是接口契约，属于请求结构（DTO）**；系列正篇[《GORM 数据工程实战》](./gorm-gin-dto-batch)会正式展开 DTO。

`CreateBook` 的绑定目标换成这个结构，再映射成模型：

```go
var input createBookInput
if err := c.ShouldBindJSON(&input); err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": "请发送合法的 JSON"})
    return
}
book := models.Book{Title: input.Title, Author: input.Author, Price: input.Price}
```

加上后，请求体缺少必填字段时 `ShouldBindJSON` 会直接返回校验错误，自动走现有的 400 分支：

```bash
# 缺少必填字段，返回 400
curl -X POST http://localhost:8080/books \
  -H "Content-Type: application/json" \
  -d '{"price":5990}'
```

## 第六章：查询图书

### 目标：实现查询列表和查询单条两个接口

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

**测试**：

```bash
# 查询所有
curl http://localhost:8080/books

# 查询单条
curl http://localhost:8080/books/1
```

> **提示：** 为什么 `First` 的检查分两层？GORM 查不到记录时返回的是 `gorm.ErrRecordNotFound`，用 `errors.Is` 精确命中它才返回 404（"图书不存在"）；其它错误——比如数据库连接断开——会落到 500，不会误报成"图书不存在"。这就是心智模型里「错误是结果的一部分」在 API 层的落点——查无记录与连接断开是两种不同的 `error`，值得用 `errors.Is` 区分。
>
> （`GetBooks` 为什么用 `[]models.Book{}` 初始化而不是 `var books []models.Book`？`Find` 查不到行时不会给 `var` 声明的切片分配内存，它保持为 nil，JSON 会输出 `null` 而不是 `[]`——空表场景下前端解析就会踩坑，初始化成空切片是列表接口的标准姿势，后续文章的分页列表会沿用。）

## 第七章：更新图书

### 目标：实现 `PUT /books/:id` 接口

```go
// UpdateBook 更新图书（PUT /books/:id）
func UpdateBook(c *gin.Context) {
    id := c.Param("id")

    // 1. 检查图书是否存在
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

严格按照 REST 语义，`PUT /books/:id` 表示「整体替换」，只更新部分字段应该用 `PATCH /books/:id`。本章的 `Updates` 只更新传入的非零字段，本质上是部分更新（PATCH）语义。入门示例用 `PUT` 没问题，但你应该知道两者的区别——如果希望语义更严谨，把路由改成 `r.PATCH("/books/:id", handlers.UpdateBook)`，测试命令相应改为 `curl -X PATCH ...` 即可。

### 零值陷阱与进阶思考

`Updates` 传入**结构体**时，GORM 默认会忽略零值字段（`0`、`""`、`false` 等）。这是设计如此，通常能满足 90% 的业务场景。但如果你确实需要将某个字段更新为 `0` 或 `""`，有两种方案：

#### 方案一：用 `Select` 强制指定字段

```go
db.DB.WithContext(c.Request.Context()).Model(&book).Select("Price").Updates(input)
```

#### 方案二：用 `map[string]interface{}`（更通用，推荐）

```go
// 前端只传需要更新的字段，零值也能正常更新
var inputMap map[string]interface{}
if err := c.ShouldBindJSON(&inputMap); err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": "请发送合法的 JSON"})
    return
}
db.DB.WithContext(c.Request.Context()).Model(&book).Updates(inputMap)
```

方案二的优势在于：前端传什么就更新什么，不会因为零值问题导致意外行为，且在字段较多的场景下更灵活。

> **方案二的代价（两个坑）：**
>
> - **JSON 数字会变成 `float64`**：`ShouldBindJSON` 解析到 `map[string]interface{}` 时，任何数字键值都是 `float64`（`{"price":6990}` → `float64(6990)`）。实测（pgx + PostgreSQL）：float64 能写进整数列，整数没问题；遇到非整数值（如 `6990.5`）**不会报错，而是被静默舍入成 6990**——数据悄悄变了。要点是：**map 里的值失去了 Go 的类型保证**，这正是后面要加白名单的第二个原因；
> - **请求里的任意键都会进 UPDATE SET**：客户端误传的键名如果恰好是模型字段（如 `ID`、`DeletedAt`），GORM 会把它们解析成 `id`、`deleted_at` 列**真的拼进 SET**——最危险的是条件匹配时**悄悄改写主键或软删除时间戳**，比报错更可怕；只有解析不到任何字段的键（如 `Comments`）才会原样进 SET 报"列不存在"。
>
> 所以 map 方案要配**键白名单**（只挑允许的键进更新）——既挡住多余键，也顺带挡掉上面的类型与主键风险；更类型安全的做法是用指针字段 DTO（`*string` / `*int`，见第十章进阶方向「请求 DTO 与指针字段」与[《数据工程实战》](./gorm-gin-dto-batch)）。

**测试**：

```bash
curl -X PUT http://localhost:8080/books/1 \
  -H "Content-Type: application/json" \
  -d '{"price":6990}'
```

## 第八章：删除图书

### 目标：实现 `DELETE /books/:id` 接口

因为 `Book` 使用了 `gorm.Model`，GORM 默认执行**软删除**。这意味着记录不会真正从数据库中移除，只是 `deleted_at` 字段被设为当前时间，查询时默认被过滤掉。

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

| 操作               | GORM 行为                                                                             |
| ------------------ | ------------------------------------------------------------------------------------- |
| 第一次 DELETE      | 设置 `deleted_at = NOW()`，不再出现在查询中                                           |
| 再次 DELETE 同一条 | 由于 `deleted_at IS NOT NULL`，GORM 找不到记录，`RowsAffected == 0`，返回"图书不存在" |

注意：GORM 执行的是 `UPDATE ... SET deleted_at=NOW() WHERE id=? AND deleted_at IS NULL`，条件不匹配时 `RowsAffected` 为 0，并不会重复软删除。

> **注意：** 软删除后，默认的 `First` / `Find` 查询会自动加上 `deleted_at IS NULL` 条件，所以被软删除的记录不会出现在列表中。
>
> **⚠️ 唯一约束的坑：** 若某字段带唯一索引（如 `ISBN`——假设你已经给模型加了该字段与唯一索引；本章 `Book` 只有 `Title`/`Author`/`Price`），软删除的记录仍占用索引——再次插入相同 ISBN 会违反唯一约束。PostgreSQL 的解法是**部分唯一索引**：只让未删除行参与唯一性。

```sql
CREATE UNIQUE INDEX idx_books_isbn ON books (isbn) WHERE deleted_at IS NULL;
```

### 如果需要查询已删除的记录

```go
db.DB.WithContext(c.Request.Context()).Unscoped().First(&book, id)
```

### 如果需要物理删除（彻底删除）

> 该函数属于**可选项**。第九章的完整版 `main.go` 默认注册了它（带「可选」注释）；如果你不想对外开放物理删除，把那一行删掉即可。

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

**测试**：

```bash
curl -X DELETE http://localhost:8080/books/1
```

## 第九章：注册路由

### 目标：把所有路由注册到 Gin 引擎

更新 `main.go`：

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

// 给每个请求挂一个超时兜底（第五章进阶）
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

### 测试（物理删除示例）

```bash
curl -X DELETE http://localhost:8080/books/1/permanent
```

> 完整运行后，除 `curl` 手动测试外，第八章的「物理删除」接口也可通过上面的路由调用。

## 第十章：总结

### 你学到的核心知识

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
- 分页与筛选：`Where` / `Order` / `Offset` / `Limit` 的实战已在[《文件与查询增强实战》](./gorm-gin-media-query)覆盖
- 钩子函数：`BeforeCreate`、`AfterUpdate`
- 请求 DTO 与指针字段：`*string` / `*int` 替代 `map[string]interface{}` 的实战已在[《数据工程实战》](./gorm-gin-dto-batch)落地（`Price *int` 就是它）
- 响应精简：响应键名为什么是大写（`ID` / `CreatedAt`），以及想统一风格怎么办——见第三章「三个新手最容易忽略的点」与第五章响应示例的提示（自定义字段声明 `json:"-"`/小写 tag，或 DTO 做统一响应结构）
- 连接池：`sqlDB, _ := db.DB.DB()` 获取底层连接后，用 `SetMaxOpenConns()` / `SetConnMaxLifetime()` 配置连接池
- 工程化沉淀（选读）：Repository / Service 分层与测试（`BookRepository` + sqlmock）、泛型 `GetPaginated[T]` 封装——落地见[《工程化（一）》](./gorm-gin-engineering-layering)与[《工程化（二）》](./gorm-gin-engineering-reliability)
- 超时映射：`errors.Is(err, context.DeadlineExceeded)` 时返回 504，而不是笼统的 500
- 超时一行库：想少写代码可用 gin-contrib/timeout 的 `timeout.New(...)` 接入，但会把 ctx 派生/回写机制变成黑盒，教程保持手写以便看清原理
- SQL 调试：开启 GORM 日志 `&gorm.Config{Logger: logger.Default.LogMode(logger.Info)}`，排查 SQL 问题
- 统一错误处理：用错误中间件 / 统一响应包装（ok/fail 结构）收敛重复的 500 样板——正文保持显式检查以便看清 `errors.Is` 的区分逻辑

> **进阶内容请移步系列正篇：** 关联查询（`Preload`）、分页聚合等实战在[《GORM 多表关联实战》](./gorm-gin-relations)与[《GORM 文件与查询增强实战》](./gorm-gin-media-query)；Repository / Service 分层与可测试性落地于[《GORM 工程化实战（一）》](./gorm-gin-engineering-layering)与[《GORM 工程化实战（二）》](./gorm-gin-engineering-reliability)——正文保持平铺直连，聚焦 GORM 本体。
