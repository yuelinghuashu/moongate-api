---
title: GORM 工程化实战（二）：可靠性与生产化
description: 系列第 7 篇（收尾）：统一错误中间件与 ok/fail（slog、超时 504）、排序白名单、文件头嗅探与 Uploader 抽象、连接池；末尾附系列承诺兑现映射表。
date: 2026-09-06 02:00:00
series: gin-gorm
tags:
  - Go
  - PostgreSQL
  - ORM
  - Engineering
---

## 一、统一错误处理：中间件 + ok/fail + slog

**目标：** 把散落在各 handler 的 `c.JSON(status, gin.H{"error": ...})` 样板收拢成「handler 只挂错误、中间件统一翻译」，并用 `slog` 记录服务端日志，超时错误映射为 504。

### 1.1 统一响应形状 ok/fail

前五篇的响应有两种：成功裸数据、失败 `{"error": ...}`。前端每次都要猜。统一成显式形状。先把系列的**响应形状演进**收在一张表里讲清——按顺序读的读者会看到它是"摊开 → 收拢"的教学过程；跳读的读者请以最新一篇的契约为准：

| 阶段 | 篇目 | 成功形状 | 错误形状 | 为什么变 |
| --- | --- | --- | --- | --- |
| 单表入门 | 入门篇 | 裸对象 / 裸数组 | `{"error": string}` | 教学：先看清"返回什么就是什么" |
| 分页化 | 媒体篇（第 3 篇） | `{items, total, page, pageSize}` | 仍是 `{"error": string}` | 列表需要分页元数据——**破坏性变更**，契约以本篇为准 |
| 多条校验错误 | 数据工程篇（第 4 篇） | 不变 | 可选新增 `{"errors": []}` | 想一次告诉客户端所有字段问题（新形态，接口自选） |
| 统一收拢 | 本篇（第 7 篇） | `{"ok": true, "data": …}` | `{"ok": false, "error": …}` | 成败显式化 + 收拢样板——**最终契约** |

于是从本篇起，成功走 `ok(...)`、失败走 `fail(...)`：

```go
// internal/handler/respond.go
package handler

import (
    "net/http"

    "github.com/gin-gonic/gin"
)

func ok(c *gin.Context, status int, data any) {
    c.JSON(status, gin.H{"ok": true, "data": data})
}

func fail(c *gin.Context, status int, msg string) {
    c.JSON(status, gin.H{"ok": false, "error": msg})
}
```

### 1.2 handler 只挂错误，不再自己写响应

分层篇的 handler 已经见到雏形（`_ = c.Error(err)` 后直接 return）。这里把它变成约定：

```go
// handler 里的错误路径，只做一件事：把错误挂到 Gin 上下文
if err := h.svc.DeleteBook(c.Request.Context(), id); err != nil {
    _ = c.Error(err) // 具体状态码与日志由错误中间件统一处理
    return
}
ok(c, http.StatusOK, gin.H{"message": "删除成功"})
```

### 1.3 错误中间件：翻译 + 分类 + 日志

`c.Next()` 之后统一看挂了多少错误，按类型翻译状态码、写 `slog`：

```go
// internal/handler/middleware.go
package handler

import (
    "context"
    "errors"
    "log/slog"
    "net/http"

    "github.com/gin-gonic/gin"
    "gorm.io/gorm"
)

func errorMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        c.Next()

        logList := c.Errors.ByType(gin.ErrorTypePrivate)
        if len(logList) == 0 {
            return // 没有挂错（或成功响应），不用兜底
        }

        // 1. 服务端日志：真实原因进日志，不进响应
        for _, e := range logList {
            slog.Error("request failed",
                "method", c.Request.Method,
                "path", c.Request.URL.Path,
                "err", e.Err,
            )
        }

        // 2. 状态码分类：404（查无记录）/ 504（超时）/ 其余 500
        status := http.StatusInternalServerError
        switch {
        case errors.Is(logList.Last().Err, gorm.ErrRecordNotFound):
            status = http.StatusNotFound // 404
        case errors.Is(logList.Last().Err, context.DeadlineExceeded):
            status = http.StatusGatewayTimeout // 504：服务端超时到期（requestTimeout）。客户端断开是 context.Canceled，不命中这里 → 500
        }

        c.AbortWithStatusJSON(status, gin.H{"ok": false, "error": http.StatusText(status)})
    }
}
```

注册：

```go
r.Use(gin.Logger(), gin.Recovery())
r.Use(errorMiddleware())
```

> **错误中间件的两个好处**：一是**状态码语义集中**——`errors.Is` 的区分逻辑（404 vs 500 vs 504）从每个 handler 收进一处，前五篇正文保持显式检查是为了看清它，工程篇就该收拢；二是**日志与响应分离**——`slog` 记真实原因，客户端只拿到 `"Internal Server Error"`，不泄露内部细节（呼应[《多表关联实战》](./gorm-gin-relations)的「错误消息为什么统一」注记）。

**测试：**

```bash
curl -i http://localhost:8080/books/999   # 404：{"ok":false,"error":"Not Found"}
curl -i -X DELETE http://localhost:8080/books/1/permanent

# 演示 504（服务端超时）：把 main.go 的 requestTimeout 临时调短（如 200ms），
# 再对 books 造一个慢查询（psql 里执行 SELECT pg_sleep(2); 后再发请求），日志里应有
# request failed + err=context deadline exceeded，响应 504。
# 注意：停掉数据库是"连接拒绝"，走 500 而不是 504；本篇没有产生 502 的路径。
```

> **slog 是标准库的**（`log/slog`，Go 1.21+）：结构化日志自带 `key=value` 输出，工程里不再 `fmt.Println` 凑数。这也是[《多表关联实战》](./gorm-gin-relations)注记里"生产环境升级为 slog + 统一错误中间件"预告的兑现。

---

## 二、请求安全补强：排序白名单与文件头嗅探

**目标：** 兑现两处安全预告——GetBooks 接受外部排序字段但必须先过白名单；上传校验从"扩展名白名单"升级为"文件内容嗅探"。

### 2.1 GetBooks 排序白名单

媒体篇 §2.2 的注释承诺过它（"接受外部排序字段需先白名单化"，见[《文件与查询增强实战》](./gorm-gin-media-query)§2.2）。现在兑现：`sort` 参数进来，先映射进白名单，查不到的拒绝，绝不直接拼进 `Order()`：

```go
// internal/service/book.go —— 排序字段白名单：key 是外部参数，value 是列名
var bookSortWhitelist = map[string]string{
    "createdAt": "created_at",
    "updatedAt": "updated_at",
    "price":     "price",
    "title":     "title",
}

// ListBooks 分页 + 搜索 + 白名单排序
func (s *BookService) ListBooks(ctx context.Context, q, sort, dir string, page, pageSize int) ([]models.Book, int64, error) {
    query := s.repo.Query(ctx, q) // 组装好 Where 的链（见分层篇 repository.List 的原型）

    order := "created_at DESC" // 默认
    if col, ok := bookSortWhitelist[sort]; ok {
        if dir == "asc" {
            order = col + " ASC"
        } else {
            order = col + " DESC"
        }
    }
    return s.repo.ListOrdered(ctx, query, order, page, pageSize)
}
```

```bash
curl "http://localhost:8080/books?sort=price&dir=asc&page=1&pageSize=10"   # 按价格升序 ✓
curl "http://localhost:8080/books?sort=created_at;DROP%20TABLE%20books--"   # 白名单外 → 回落默认排序，永不进 SQL ✓
```

> **关键在"永不拼接"：** 白名单让"用户的字面输入"与"SQL 片段"之间永远隔着一张映射表——查不到就直接忽略（用默认排序），而不是报错或猜测。`order` 变量里除了白名单查出的列名，不可能出现其它字符串。

### 2.2 上传文件头嗅探

扩展名谁都能伪造（`hack.jpg` 里放个 exe）。`http.DetectContentType` 读文件前 512 字节按内容判定"真类型"：

```go
// internal/repository 同层或独立：上传校验收进 handler 边界
func sniffImage(r io.Reader) (string, error) {
    buf := make([]byte, 512)
    n, _ := r.Read(buf)

    ctype, _, _ := mime.ParseMediaType(http.DetectContentType(buf[:n]))
    switch ctype {
    case "image/jpeg", "image/png", "image/webp":
        return ctype, nil
    default:
        return "", errors.New("仅允许 jpg/png/webp 图片")
    }
}
```

把它接进 `UploadCover` 时有一个**必须处理的细节**：`sniffImage` 会从流里读走前 512 字节，如果接着把同一个 `fh` 直接交给保存，落盘文件会**丢掉文件头而损坏**（`multipart.File` 支持 `Seek`，保存前要 `fh.Seek(0, 0)` 拨回开头）。这一步会与扩展名白名单、2MB 上限、`Uploader.Save` 组合在一起——§3.2 会给出一个完整可编译的 `UploadCover`（嗅探 → 倒回 → 保存一条龙），这里先记住"读了要倒回去"这个坑。

```bash
# 伪造扩展名的非图片：以前 400 靠后缀，现在 400 靠内容
curl -X POST http://localhost:8080/books/1/cover -F "cover=@/etc/hosts;filename=pic.jpg"
# {"ok":false,"error":"仅允许 jpg/png/webp 图片"}
```

> **两种校验的定位：** 扩展名校验是"用户体验层"（快、能给出友好提示）；内容嗅探是"安全层"——它验的是**文件头魔数**，能挡掉"改名换后缀"的伪造，但并非"可解码性"保证（PNG 文件头 + 任意尾随数据也能通过）。生产里两层都留；本篇的完整函数同样保留媒体篇的 2MB 大小上限（见 §3.2）。

---

## 三、上传器抽象：从磁盘到对象存储

**目标：** 兑现"对象存储"预告——用 `Uploader` 接口把"图片存哪"从业务里摘出去，Disk 与 S3 只是两个实现。**接口契约：给 key、返回可展示的 URL/路径**——数据库存的是 `Save` 的返回值（Disk 下是 `/uploads/...` 相对地址，S3 下是完整 https URL），字段沿用媒体篇的 `cover_path`；"是否另存原始 key"的取舍见 3.3 注记。

### 3.1 为什么需要接口

媒体篇的 `UploadCover` 里 `c.SaveUploadedFile(file, filepath.Join("uploads", ...))` 把存储钉死在本地磁盘。换成 S3 要改 handler——而 handler 不该知道存储细节。抽接口：

```go
// internal/storage/uploader.go —— 文件头一次给全（interface + Disk + S3 三段拼成同一个文件）
package storage

import (
    "context"
    "fmt"
    "io"
    "os"
    "path/filepath"
)

type Uploader interface {
    // Save 保存 r 的内容到 key，返回可展示的 URL/路径（Disk：/uploads/key；S3：完整 https URL）
    Save(ctx context.Context, key string, r io.Reader) (string, error)
}
```

Disk 实现（沿用现在的目录语义，封装成接口实现）：

```go
type DiskUploader struct {
    Dir string // ./uploads
    BaseURL string // /uploads
}

func (d *DiskUploader) Save(ctx context.Context, key string, r io.Reader) (string, error) {
    dst := filepath.Join(d.Dir, key)
    if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
        return "", err
    }
    f, err := os.Create(dst)
    if err != nil {
        return "", err
    }
    defer f.Close()
    if _, err := io.Copy(f, r); err != nil {
        return "", err
    }
    return d.BaseURL + "/" + key, nil
}
```

S3 骨架（不接真实 AWS，只留形状与调用点）：

```go
type S3Uploader struct {
    Bucket  string
    Region  string
}

func (s *S3Uploader) Save(ctx context.Context, key string, r io.Reader) (string, error) {
    // 构造真实实现时：aws-sdk-go-v2 的 s3.PutObject 到这里
    return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", s.Bucket, s.Region, key), nil
}
```

### 3.2 handler 只认接口

`UploadCover` 改造后只依赖 `storage.Uploader`——业务与存储解耦。先补齐三处"接线"（篇 6 的 `BookHandler` 只有 `svc` 一个字段）：结构体加 `up`、构造器变双参；Service 加 `SetCover`（先验书存在、再更新 `cover_path`）；repository 加对应的单字段更新。

```go
// internal/handler/book.go —— BookHandler 结构体与构造器（对照篇 6 的单参版本）
type BookHandler struct {
    svc *service.BookService
    up  storage.Uploader // 篇 6 只有 svc；本篇补存储抽象（见 3.1）
}

func NewBookHandler(svc *service.BookService, up storage.Uploader) *BookHandler {
    return &BookHandler{svc: svc, up: up}
}
```

```go
// internal/service/book.go —— BookService 增加 SetCover：先确认书存在（404），再更新封面字段
func (s *BookService) SetCover(ctx context.Context, id uint, url string) error {
    if _, err := s.repo.FirstBook(ctx, id); err != nil {
        return err
    }
    return s.repo.SetCover(ctx, id, url)
}

// internal/repository/book.go —— repo.SetCover 单字段更新（示意）：
//   r.db.WithContext(ctx).Model(&models.Book{Model: gorm.Model{ID: id}}).Update("cover_path", url).Error
```

完整的新 `UploadCover`（§2.2 嗅探 + 媒体篇大小上限 + 倒回流头 + 3.1 存储抽象一次合体）：

```go
func (h *BookHandler) UploadCover(c *gin.Context) {
    // 0. 解析 id（非法 → 400）
    id, err := strconv.ParseUint(c.Param("id"), 10, 64)
    if err != nil {
        fail(c, http.StatusBadRequest, "id 非法")
        return
    }
    // 1. 书必须存在（404 由错误中间件翻译）
    book, err := h.svc.GetBook(c.Request.Context(), uint(id))
    if err != nil {
        _ = c.Error(err)
        return
    }
    // 2. 取文件
    file, err := c.FormFile("cover")
    if err != nil {
        fail(c, http.StatusBadRequest, "缺少文件字段 cover")
        return
    }
    fh, err := file.Open()
    if err != nil {
        fail(c, http.StatusBadRequest, "文件读取失败")
        return
    }
    defer fh.Close()

    // 3. 内容嗅探：扩展名不可信，按文件头判"真类型"（见 §2.2）
    ctype, err := sniffImage(fh)
    if err != nil {
        fail(c, http.StatusBadRequest, err.Error())
        return
    }
    // 4. 大小上限保留（媒体篇的 2MB 检查）
    if file.Size > 2<<20 {
        fail(c, http.StatusBadRequest, "文件过大（上限 2MB）")
        return
    }
    // 5. 关键：嗅探已消费前 512 字节——保存前必须把流倒回文件头，否则落盘文件损坏
    if _, err := fh.Seek(0, 0); err != nil {
        _ = c.Error(err)
        return
    }
    // 6. 交给 Uploader 落盘：扩展名由内容给出，不信任用户文件名后缀
    ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}[ctype]
    key := fmt.Sprintf("%d_%d%s", book.ID, time.Now().UnixNano(), ext)
    url, err := h.up.Save(c.Request.Context(), key, fh)
    if err != nil {
        _ = c.Error(err)
        return
    }
    // 7. 入库的是 Save 返回的可展示 URL/路径（不是原始 key），并响应
    if err := h.svc.SetCover(c.Request.Context(), book.ID, url); err != nil {
        _ = c.Error(err)
        return
    }
    ok(c, http.StatusOK, gin.H{"coverUrl": url})
}
```

`main.go` 一行切换存储实现：

```go
// up := &storage.S3Uploader{Bucket: "my-books", Region: "ap-southeast-1"}  // 切 S3 只改这里
up := &storage.DiskUploader{Dir: uploadDir, BaseURL: "/uploads"}
bookH := handler.NewBookHandler(bookSvc, up)
```

> **命名决策回顾（存 key 还是存 URL？）：** 媒体篇对照表埋过两个候选字段——`cover_path`（存相对地址/URL）与 `cover_key`（存对象存储的原始 key）。本篇实现的取舍是：**`Save` 直接返回可展示地址并存入 `cover_path`**（Disk 与 S3 只是 URL 形态不同，列语义都是"可展示地址"），省掉"先存 key、读时再拼 URL"的一次换算。只有当你要在服务端对原始 key 做二次操作（迁移、CDN 签名、批量删除）时，才值得另开 `cover_key` 列存原始 key、响应时再拼——那是把"存储语义"与"展示契约"彻底拆开的进阶，本篇点到为止。

---

## 四、连接池配置与收尾

**目标：** 兑现 ch10 预告的连接池条目，把 `db.InitDB()` 补上池参数；顺手回顾超时 504（已在中间件里落地）。

### 4.1 连接池三行

`gorm.Open` 返回的 `*gorm.DB` 包着 `database/sql` 连接池，用 `db.DB()` 拿到底层后配置：

```go
// db/db.go —— InitDB 末尾追加（db 是包名，包级变量叫 DB，类型 *gorm.DB）
sqlDB, err := DB.DB() // 取底层 *sql.DB
if err != nil {
    log.Fatal("获取底层连接失败：", err)
}
sqlDB.SetMaxOpenConns(50)           // 并发上限：防止数据库被打穿
sqlDB.SetConnMaxLifetime(time.Hour) // 生命上限：连接不能永生（数据库侧一般也有超时）
sqlDB.SetMaxIdleConns(10)           // 空闲上限：池中最多保留 10 条空闲连接
```

> **三个都是"上限"，别记成"下限"：** `SetMaxOpenConns` 是并发上限（超了排队等连接）；`SetConnMaxLifetime` 是生命上限（到期换新，规避数据库/中间件层的连接回收问题）；`SetMaxIdleConns` 是**空闲连接数上限**——database/sql 的空闲连接超过该数会被关闭，它不会主动建连"保活"。想要"预热保温"效果，得应用自己在启动时主动发起几个查询，这是另一个话题。

### 4.2 本篇收尾：全部承诺兑现

系列七篇至此闭环——把五篇处处的预告逐条对账：

| 承诺                                                 | 出处                                                                                                         | 兑现位置                           |
| ---------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ | ---------------------------------- |
| `BookRepository` 接口 + Service + 注入 + `internal/` | [入门篇](./gorm-gin-crud-tutorial)「学习级结构」注记 / ch10「工程化沉淀」/ [dto 篇](./gorm-gin-dto-batch)§四 | 工程化（一）一                     |
| `httptest` 表驱动测试（+ sqlmock）                   | [dto 篇](./gorm-gin-dto-batch)§四 / 入门篇注记                                                               | 工程化（一）二                     |
| 泛型 `GetPaginated[T]`                               | 入门篇 ch10 / [dto 篇](./gorm-gin-dto-batch)§四                                                              | 工程化（一）三                     |
| 聚合分页收拢（`GetPaginatedScan`，选读）             | [媒体篇](./gorm-gin-media-query)§2.3                                                                         | 工程化（一）三（选读注记）         |
| 事务 `db.Transaction()`                              | 入门篇 ch10                                                                                                  | 工程化（一）一（Service 原子操作） |
| 超时映射 504                                         | 入门篇 ch10                                                                                                  | 本篇一（错误中间件）               |
| 统一错误处理 / ok-fail / slog                        | [多表关联篇](./gorm-gin-relations)「错误消息为什么统一」注记 / 入门篇 ch10                                   | 本篇一                             |
| 排序白名单                                           | [媒体篇](./gorm-gin-media-query)§2.2 注释与要点                                                              | 本篇二                             |
| 文件头嗅探                                           | [媒体篇](./gorm-gin-media-query)§1.3 要点                                                                    | 本篇二                             |
| 对象存储抽象                                         | [媒体篇](./gorm-gin-media-query)§1.3（cover_path/cover_key 对照表）                                          | 本篇三                             |
| 连接池                                               | 入门篇 ch10                                                                                                  | 本篇四                             |

**你现在的项目**：三层架构（book 维度链路端到端迁移，其余按同构模板补齐）+ handler 层表驱动测试 + 统一错误中间件（404/500/504 语义集中 + slog 日志）+ 排序白名单 + 文件头嗅探（含流倒回与 2MB 上限）+ 可切换的存储抽象 + 连接池配置——可以放心上线的骨架齐了。

回头看，前几篇里每一处 `WithContext`、每一次 `errors.Is` 的铺垫，最后都汇进了本篇的一张错误中间件和一张兑现映射表——它们的意义，在收拢的那一刻才完全显现。
