---
title: GORM 入门实战：用 Gin + GORM 写一个图书管理 API
description: 从零搭建一个完整的图书管理 API，涵盖 GORM 的 CRUD、软删除、零值陷阱等核心知识点，附带完整代码和测试命令
permalink: 96d254d9-5123-4e7f-8518-2c7b46c37018
date: 2026-07-03
series: 
level: P2
tags:
  - Go
  - PostgreSQL
  - ORM
---

## 适合读者

- 已掌握 Go 基础语法
- 想学 GORM 但不知道从哪开始
- 想看到一个能直接运行的完整项目

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

## 第一章：项目初始化

**目标：** 创建项目目录，安装依赖。

```bash
mkdir gin-demo
cd gin-demo
go mod init gin-demo
```

**安装依赖：**

```bash
# Web 框架
go get -u github.com/gin-gonic/gin

# ORM 库 + PostgreSQL 驱动
go get -u gorm.io/gorm
go get -u gorm.io/driver/postgres
```

**数据库驱动说明：**

本文使用 PostgreSQL 作为示例数据库。如果你使用的是其他数据库，替换对应的驱动即可：

| 数据库 | 安装命令 | DSN 格式 |
| --- | --- | --- |
| PostgreSQL | `go get -u gorm.io/driver/postgres` | `host=localhost user=postgres dbname=books sslmode=disable` |
| MySQL | `go get -u gorm.io/driver/mysql` | `user:pass@tcp(localhost:3306)/dbname?charset=utf8mb4&parseTime=True` |
| SQLite | `go get -u gorm.io/driver/sqlite` | `./data.db` |

> Gin 负责处理 HTTP 请求和路由，GORM 负责数据库操作，两者各司其职，缺一不可。后续所有代码都依赖这两个库。

## 第二章：连接数据库

**目标：** 建立数据库连接，在项目启动时初始化。

**连接流程：**

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
    dbname := "books"

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

**代码说明：**

| 代码 | 说明 |
| --- | --- |
| `var DB *gorm.DB` | 声明全局 DB 变量，供其他包使用 |
| `gorm.Open(postgres.Open(dsn), &gorm.Config{})` | 建立数据库连接 |
| `log.Fatal` | 连接失败时终止程序，避免后续代码执行 |

**DSN 参数说明：**

| 参数 | 示例 | 说明 |
| --- | --- | --- |
| `host` | `localhost` | 数据库主机地址 |
| `port` | `5432` | PostgreSQL 默认端口 |
| `user` | `postgres` | 数据库用户名 |
| `password` | `123456` | 数据库密码 |
| `dbname` | `books` | 数据库名称 |
| `sslmode` | `disable` | 本地开发禁用 SSL |

> **注意事项：**
>
> - PostgreSQL 需要**手动创建数据库**：`CREATE DATABASE books;`
> - GORM 的 `AutoMigrate` 能自动创建表，但**不能自动创建数据库**
> - `sslmode=disable` 仅用于本地开发，生产环境应开启 SSL

## 第三章：定义数据模型

**目标：** 用 Go 结构体定义数据库表结构。

创建 `models/book.go`：

```go
package models

import "gorm.io/gorm"

type Book struct {
    gorm.Model
    Title  string  `json:"title" gorm:"not null"`
    Author string  `json:"author" gorm:"not null"`
    Price  float64 `json:"price"`
}
```

**字段说明：**

- `gorm.Model`：内置了 `ID`、`CreatedAt`、`UpdatedAt`、`DeletedAt` 四个字段
- `gorm:"not null"`：对应数据库的 `NOT NULL` 约束
- `json:"title"`：指定 JSON 序列化时的字段名

## 第四章：自动迁移

**目标：** 程序启动时自动创建或更新表结构。

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

**注意事项：**

- `AutoMigrate` 只会创建不存在的表，不会删除已有字段
- 字段类型变更时，GORM 不会自动修改已有字段类型

## 第五章：创建图书

**目标：** 实现 `POST /books` 接口，接收 JSON 请求并存入数据库。

> **注：** 后续第六、七、八章的所有 CRUD 函数均追加至同一个文件 `handlers/book.go` 中。开头统一为：

```go
package handlers

import (
    "gin-demo/db"
    "gin-demo/models"
    "net/http"

    "github.com/gin-gonic/gin"
)
```

**创建图书：**

```go
func CreateBook(c *gin.Context) {
    var book models.Book

    // 1. 绑定 JSON 请求体
    if err := c.ShouldBindJSON(&book); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "请发送合法的 JSON"})
        return
    }

    // 2. 插入数据库
    result := db.DB.Create(&book)
    if result.Error != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "创建失败"})
        return
    }

    // 3. 返回创建的数据
    c.JSON(http.StatusCreated, book)
}
```

**测试：**

```bash
curl -X POST http://localhost:8080/books \
  -H "Content-Type: application/json" \
  -d '{"title":"Go语言实战","author":"张三","price":59.9}'
```

## 第六章：查询图书

**目标：** 实现查询列表和查询单条两个接口。

```go
// 查询所有图书
func GetBooks(c *gin.Context) {
    var books []models.Book
    db.DB.Find(&books)
    c.JSON(http.StatusOK, books)
}

// 查询单条图书
func GetBook(c *gin.Context) {
    id := c.Param("id")

    var book models.Book
    result := db.DB.First(&book, id)
    if result.Error != nil {
        c.JSON(http.StatusNotFound, gin.H{"error": "图书不存在"})
        return
    }

    c.JSON(http.StatusOK, book)
}
```

**测试：**

```bash
# 查询所有
curl http://localhost:8080/books

# 查询单条
curl http://localhost:8080/books/1
```

## 第七章：更新图书

**目标：** 实现 `PUT /books/:id` 接口。

```go
func UpdateBook(c *gin.Context) {
    id := c.Param("id")

    // 1. 检查图书是否存在
    var book models.Book
    if result := db.DB.First(&book, id); result.Error != nil {
        c.JSON(http.StatusNotFound, gin.H{"error": "图书不存在"})
        return
    }

    // 2. 绑定 JSON 请求体
    var input models.Book
    if err := c.ShouldBindJSON(&input); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "请发送合法的 JSON"})
        return
    }

    // 3. 更新字段
    db.DB.Model(&book).Updates(input)

    // 4. 返回更新后的数据
    c.JSON(http.StatusOK, book)
}
```

**零值陷阱与进阶思考：**

`Updates` 传入**结构体**时，GORM 默认会忽略零值字段（`0`、`""`、`false` 等）。这是设计如此，通常能满足 90% 的业务场景。但如果你确实需要将某个字段更新为 `0` 或 `""`，有两种方案：

**方案一：用 `Select` 强制指定字段**

```go
db.DB.Model(&book).Select("Price").Updates(input)
```

**方案二：用 `map[string]interface{}`（更通用，推荐）**

```go
// 前端只传需要更新的字段，零值也能正常更新
var inputMap map[string]interface{}
if err := c.ShouldBindJSON(&inputMap); err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": "请发送合法的 JSON"})
    return
}
db.DB.Model(&book).Updates(inputMap)
```

方案二的优势在于：前端传什么就更新什么，不会因为零值问题导致意外行为，且在字段较多的场景下更灵活。

**测试：**

```bash
curl -X PUT http://localhost:8080/books/1 \
  -H "Content-Type: application/json" \
  -d '{"price":69.9}'
```

## 第八章：删除图书

**目标：** 实现 `DELETE /books/:id` 接口。

因为 `Book` 使用了 `gorm.Model`，GORM 默认执行**软删除**。这意味着记录不会真正从数据库中移除，只是 `deleted_at` 字段被设为当前时间，查询时默认被过滤掉。

```go
func DeleteBook(c *gin.Context) {
    id := c.Param("id")

    // 执行软删除
    result := db.DB.Delete(&models.Book{}, id)
    if result.RowsAffected == 0 {
        c.JSON(http.StatusNotFound, gin.H{"error": "图书不存在"})
        return
    }

    c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}
```

**软删除行为解析：**

| 操作               | GORM 行为                                                                             |
| ------------------ | ------------------------------------------------------------------------------------- |
| 第一次 DELETE      | 设置 `deleted_at = NOW()`，不再出现在查询中                                           |
| 再次 DELETE 同一条 | 由于 `deleted_at IS NOT NULL`，GORM 找不到记录，`RowsAffected == 0`，返回"图书不存在" |

> **注意：** 软删除后，默认的 `First` / `Find` 查询会自动加上 `deleted_at IS NULL` 条件，所以被软删除的记录不会出现在列表中。

**如果需要查询已删除的记录：**

```go
db.DB.Unscoped().First(&book, id)
```

**如果需要物理删除（彻底删除）：**

```go
func DeleteBookPermanently(c *gin.Context) {
    id := c.Param("id")
    // Unscoped() 绕过软删除，执行物理删除
    result := db.DB.Unscoped().Delete(&models.Book{}, id)
    if result.RowsAffected == 0 {
        c.JSON(http.StatusNotFound, gin.H{"error": "图书不存在"})
        return
    }
    c.JSON(http.StatusOK, gin.H{"message": "物理删除成功"})
}
```

**测试：**

```bash
curl -X DELETE http://localhost:8080/books/1
```

## 第九章：注册路由

**目标：** 把所有路由注册到 Gin 引擎。

更新 `main.go`：

```go
package main

import (
    "log"
    "gin-demo/db"
    "gin-demo/handlers"
    "gin-demo/models"
    "github.com/gin-gonic/gin"
)

func main() {
    // 连接数据库
    db.InitDB()

    // 自动迁移
    if err := db.DB.AutoMigrate(&models.Book{}); err != nil {
        log.Fatal("迁移失败：", err)
    }

    r := gin.Default()

    // RESTful API 路由
    r.POST("/books", handlers.CreateBook)
    r.GET("/books", handlers.GetBooks)
    r.GET("/books/:id", handlers.GetBook)
    r.PUT("/books/:id", handlers.UpdateBook)
    r.DELETE("/books/:id", handlers.DeleteBook)

    r.Run(":8080")
}
```

## 第十章：总结

**你学到的核心知识：**

| 操作       | GORM 方法                        | 对应 SQL                                       |
| ---------- | -------------------------------- | ---------------------------------------------- |
| 创建       | `db.Create(&book)`               | `INSERT INTO ...`                              |
| 查询所有   | `db.Find(&books)`                | `SELECT * FROM ...`                            |
| 查询单条   | `db.First(&book, id)`            | `SELECT * FROM ... WHERE id = ?`               |
| 更新       | `db.Model(&book).Updates(input)` | `UPDATE ... SET ...`                           |
| 软删除     | `db.Delete(&book)`               | `UPDATE ... SET deleted_at = NOW()`            |
| 物理删除   | `db.Unscoped().Delete(&book)`    | `DELETE FROM ...`                              |
| 查询已删除 | `db.Unscoped().First(&book, id)` | `SELECT * FROM ... WHERE id = ?`（不限软删除） |

**进阶方向：**

- 关联查询：`Preload`、`Joins`
- 事务：`db.Transaction()`
- 查询条件：`Where`、`Order`、`Limit`、`Offset`
- 钩子函数：`BeforeCreate`、`AfterUpdate`
