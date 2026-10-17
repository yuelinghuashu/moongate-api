---
title: GORM 工程化实战（一）：分层、注入与可测性
description: 系列第 6 篇：把直连 db.DB 的五篇代码重构为 Repository / Service / Handler 三层（internal/ + 构造注入），fake repository + httptest 表驱动测试，并用泛型 GetPaginated[T]（选读）收拢分页样板。
date: 2026-09-06 00:00:00
series: gin-gorm
order: 6
tags:
  - Go
  - PostgreSQL
  - ORM
  - Engineering
---

## 一、分层重构：从 handler 直连 db.DB 到三层

**目标：** 把五篇的平铺代码重构为 `internal/repository` + `internal/service` + `internal/handler`，handler 不再碰数据库，依赖全部由 `main.go` 构造注入。

### 1.1 问题的本质：handler 为什么不可测

回顾入门篇的 `GetBook`：

```go
func GetBook(c *gin.Context) {
    // ...直接调 db.DB.WithContext(...).Preload(...).First(&book, id)
}
```

handler 和"具体拿到哪条数据"绑死了。要测试它，就得让测试环境里有一个真的 PostgreSQL——慢、脆、还得造数据。用户可见的行为（状态码、JSON 形状）测不了，这也是前五篇用 curl 验证的原因。**可测性 = 数据访问可替换**，这是分层的全部动机。

> **先撞一次墙（为什么前五篇只能 curl）：** 假设想给入门篇的 `GetBook` 写单元测试——handler 内部直接 `db.DB.First(&book, id)`，测试就必须：连上一个真 PostgreSQL、建表、准备好"存在 / 不存在 / 数据库故障"三种数据，还要保证测试之间互不污染。这已经不是"单元测试"，是"起一套环境再手动演戏"；故障分支（连接断开）在测试里几乎模拟不出来。所以前五篇用 curl 验证不是偷懒，而是**当时 handler 没有可替换的数据入口**——正文的下一步就从给数据访问安一个"可替换的入口"下手。

### 1.2 目标包结构

项目从平铺演进为（入门篇「学习级结构」注记里早就埋过这个方向）：

```text
go-learning/                    # gin-demo
├── main.go                     # 组装容器：构造依赖、注册路由
├── db/db.go
├── models/
├── internal/                   # internal/ 包约束：外部 import 即编译错误
│   ├── repository/             # 数据访问（GORM 的活只在这里出现）
│   │   └── book.go
│   ├── service/                # 业务逻辑（跨表、事务在此）
│   │   └── book.go
│   └── handler/                # HTTP 层（参数、绑定、响应）
│       ├── book.go
│       └── pagination.go
└── seed/books.json
```

`internal/` 的语义：包路径含 `internal` 时，只有其父目录内的代码能 import 它——外部模块一引用就编译报错。它把"这是本项目内部实现"变成编译期事实，而不是约定。

### 1.3 BookRepository 接口

`internal/repository/book.go`——接口定义"数据访问需要什么"，实现藏在后面。下面直接给出迁移完成后的**最终接口**；但动手时别一次写全：**第一刀只为实现一条链路立 2~3 个方法**（先从 `FindByID` 起步），跑通 1.6 的注入与测试后，再按这份清单补齐。先读全量形态，是为了看清"数据访问需要什么"这件事的完整边界：

```go
package repository

import (
    "context"

    "go-learning/models"

    "gorm.io/gorm"
)

// BookRepository 数据访问契约：handler/service 只看接口，不看实现
type BookRepository interface {
    Create(ctx context.Context, book *models.Book) error
    FirstBook(ctx context.Context, id uint) (*models.Book, error) // 存在性检查（不带关联）
    FindByID(ctx context.Context, id uint) (*models.Book, error)  // 带 Comments 与 Tags 的完整详情
    Query(ctx context.Context, q string) *gorm.DB                              // 搭好条件的链（篇7 的排序白名单要用）
    ListOrdered(ctx context.Context, query *gorm.DB, order string, page, pageSize int) ([]models.Book, int64, error)
    List(ctx context.Context, q string, page, pageSize int) ([]models.Book, int64, error) // 便捷版：ListOrdered 固定倒序
    ListByTag(ctx context.Context, tagName string) ([]models.Book, error)
    UpdateStruct(ctx context.Context, id uint, input interface{}) error // Updates：零值不更新
    SetCover(ctx context.Context, id uint, url string) error
    SoftDelete(ctx context.Context, id uint) error
    HardDelete(ctx context.Context, id uint) error
    CreateComment(ctx context.Context, comment *models.Comment) error
    ListComments(ctx context.Context, bookID uint, page, pageSize int) ([]models.Comment, int64, error)
    DeleteComment(ctx context.Context, bookID, cid uint) error // 越权防护：book_id + id
    AddTag(ctx context.Context, bookID, tagID uint) error
    RemoveTag(ctx context.Context, bookID, tagID uint) error
    FirstOrCreateTag(ctx context.Context, name string) (*models.Tag, error)
    CountBookTag(ctx context.Context, bookID, tagID uint) (int64, error)
}
```

> **半接口的诚实说明：** `Query` / `ListOrdered` 的签名里出现了 `*gorm.DB`——调用方（Service，以及篇 7 的排序白名单）拿到的仍是一条 GORM 链。这说明"接口把 GORM 完全挡住"是理想态；现实里为了省样板，常让查询链透出。教学上接受这个妥协（想彻底隔离，要把查询也抽象成自己的 DSL，那是另一层话题），但要清楚：**这个接口挡住的是"数据库在哪里、怎么连"，不是"用 GORM 查"**。

GORM 实现（`NewBookRepository` 返回接口，调用方永远不需要知道具体类型）：

```go
type gormBookRepository struct {
    db *gorm.DB
}

// NewBookRepository 构造 GORM 实现并向上转型为接口
func NewBookRepository(db *gorm.DB) BookRepository {
    return &gormBookRepository{db: db}
}

func (r *gormBookRepository) FindByID(ctx context.Context, id uint) (*models.Book, error) {
    var book models.Book
    if err := r.db.WithContext(ctx).Preload("Comments").Preload("Tags").First(&book, id).Error; err != nil {
        return nil, err
    }
    return &book, nil
}

func (r *gormBookRepository) Query(ctx context.Context, q string) *gorm.DB {
    query := r.db.WithContext(ctx).Model(&models.Book{})
    if q != "" {
        like := "%" + q + "%"
        query = query.Where("title ILIKE ? OR author ILIKE ?", like, like)
    }
    return query
}

func (r *gormBookRepository) ListOrdered(ctx context.Context, query *gorm.DB, order string, page, pageSize int) ([]models.Book, int64, error) {
    var total int64
    if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
        return nil, 0, err
    }
    var books []models.Book
    if err := query.Order(order).Offset((page - 1) * pageSize).Limit(pageSize).Find(&books).Error; err != nil {
        return nil, 0, err
    }
    return books, total, nil
}

// List 便捷版：固定倒序（无排序参数场景直接用）
func (r *gormBookRepository) List(ctx context.Context, q string, page, pageSize int) ([]models.Book, int64, error) {
    return r.ListOrdered(ctx, r.Query(ctx, q), "created_at DESC", page, pageSize)
}
```

> **接口该多大？** 上面是**仓库最终形态**（18 个方法，与配套仓库逐字一致）——本篇只把 Create / Get 两条链路端到端走通，其余方法按 §1.7 清单逐个补；接口一次给全，是为了让你先看清"数据访问需要什么"的完整边界。教学上按聚合根收拢成少数接口、写法同构；真实项目常按聚合根拆多个接口，原则一样。
>
> 另一个口径要提前对齐：`AddTag(bookID, tagID uint)` 是**按 id** 的底层操作——而路由/前端语义是**按名字**打标签（多对多篇 §3.1）。"把名字解析成 id、按名建标签、幂等去重"是 Service 的活（见 1.4），接口层只暴露最原子的数据操作。
>
> **真实项目会按聚合根拆开。** 这里给 18 个方法的单接口，是为了让 §1.7 的迁移清单一张表讲完；实际项目通常拆成三个：`BookRepository`（Create / FirstBook / FindByID / List / Update / SoftDelete / SetCover）、`CommentRepository`（CreateComment / ListComments / DeleteComment）、`TagRepository`（FirstOrCreateTag / AddTag / RemoveTag / CountBookTag / ListByTag）。拆开后 handler 多注入两个字段，换来每个接口都能独立替换和 fake——按自己的聚合边界挑一种，别把 18 个方法当模板。

### 1.4 Service 层与事务

Service 放"跨表的业务逻辑"。前五篇 handler 里最像业务的其实是批量导入（数据文件 → 分批插入），但它没有跨表语义。给 Service 找一个真正的立足点：**建书 + 按名打标签**是两步（标签不存在先建、再写连接表），用 `db.Transaction()` 包成原子操作——按名标签的 `FirstOrCreate` 去重语义在[多对多篇 §3.1](./gorm-gin-tags)已讲，这里只看事务怎么把它们包成一步。

> ⚠️ **这是新增行为，不是纯重构：** 前五篇的 `POST /books` 只建书；本篇起顺带接收 `tags` 数组、一步完成"建书 + 打标签"。教程用"给已有接口加一个真实存在的跨表场景"来给 Service 找立足点——如果你不想改变接口行为，也可以跳过本节、让 Service 先只做透传，等遇到真事务再回来。这里演示的是"当需要事务时，代码该长在哪一层"。

```go
package service

import (
    "context"

    "go-learning/models"
    "gorm.io/gorm"
)

// BookService 业务逻辑：依赖接口，不依赖数据库
type BookService struct {
    repo BookRepository
    db   *gorm.DB // 事务天然跨仓库，需要直接拿连接（见下方注记）
}

func NewBookService(repo BookRepository, db *gorm.DB) *BookService {
    return &BookService{repo: repo, db: db}
}

// CreateBookWithTags 一步完成「建书 + 打标签」，任一失败整体回滚
func (s *BookService) CreateBookWithTags(ctx context.Context, book *models.Book, tagNames []string) (*models.Book, error) {
    err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
        // 1. 建书
        if err := tx.Create(book).Error; err != nil {
            return err
        }
        // 2. 每个标签：不存在则先建（FirstOrCreate），再写连接表
        for _, name := range tagNames {
            var tag models.Tag
            if err := tx.Where("name = ?", name).FirstOrCreate(&tag, models.Tag{Name: name}).Error; err != nil {
                return err
            }
            if err := tx.Model(book).Association("Tags").Append(&tag); err != nil {
                return err
            }
        }
        return nil
    })
    if err != nil {
        return nil, err
    }
    return book, nil
}
```

> **事务与分层的诚实取舍：** 事务天然"跨仓库"（一个事务里要操作书、标签、连接表三处），塞进 repository 接口会让接口爆炸（`WithTx(func(repo) error)` 的 Unit of Work 风格）。本篇务实：`*gorm.DB` 直接注入 Service 做事务，其余数据访问走 `repo`。接口仍挡得住**单条数据操作**（`Create` / `FindByID` / `SoftDelete` 可被 fake，handler 分支可测）；事务是**跨操作的编排**，不属于任何单个 repo 方法。三种摆法：
>
> 1. **Service 持 `*gorm.DB` 做事务**（本篇路线）：最直白，代价是事务路径靠真库集成验证。
> 2. **Unit of Work**：`repo.Transaction(ctx, func(tx BookRepository) error)`——可测性最好，接口与实现成本都涨。
> 3. **注入 `TxRunner` 接口**：是 ① 的可测升级版，接口只多一个方法。
>
> 选型判据：事务是否真的跨表、事务路径要不要进自动化测试、repo 方法是否超过 ~10 个或事务点超过 ~3 处——后两条命中就尽早从 ① 升 ③。这也是系列没有"工程化（三）"的原因：七篇闭环，剩下的交给真实规模。

### 1.5 Handler 只剩三件事

Handler 现在只做：从 Gin 拿参数、调 Service、写响应。数据从哪来？Service 的事：

```go
package handler

import (
    "errors"
    "net/http"
    "strconv"

    "go-learning/internal/service"
    "go-learning/models"

    "github.com/gin-gonic/gin"
    "github.com/go-playground/validator/v10"
)

// createBookInput：在数据工程篇的 DTO 上只加这一行（Title / Author / Price 与校验规则不变；
// 完整结构见数据工程篇 §2 或配套仓库 internal/handler/book.go）
type createBookInput struct {
    Tags []string `json:"tags"`
}

// bindBookInput 绑定 + 校验错误翻译：把数据工程篇 §2.1 的逻辑收进一个函数，所有 handler 复用
func bindBookInput(c *gin.Context) (*createBookInput, bool) {
    var input createBookInput
    if err := c.ShouldBindJSON(&input); err != nil {
        var ve validator.ValidationErrors
        if errors.As(err, &ve) && len(ve) > 0 {
            e := ve[0]
            var msg string
            switch e.Tag() {
            case "required":
                msg = "缺少必填字段 " + e.Field()
            case "gte":
                msg = e.Field() + " 不能小于 " + e.Param()
            case "lte":
                msg = e.Field() + " 不能大于 " + e.Param()
            default:
                msg = "参数不合法"
            }
            c.JSON(http.StatusBadRequest, gin.H{"error": msg})
            return nil, false
        }
        c.JSON(http.StatusBadRequest, gin.H{"error": "请发送合法的 JSON"})
        return nil, false
    }
    return &input, true
}

// BookHandler 只依赖 Service；测试想换假实现就是一行的事
type BookHandler struct {
    svc *service.BookService
}

func NewBookHandler(svc *service.BookService) *BookHandler {
    return &BookHandler{svc: svc}
}

func (h *BookHandler) Create(c *gin.Context) {
    input, bound := bindBookInput(c)
    if !bound {
        return // 400 已由 bindBookInput 写好
    }
    book := models.Book{Title: input.Title, Author: input.Author, Price: *input.Price}
    result, err := h.svc.CreateBookWithTags(c.Request.Context(), &book, input.Tags)
    if err != nil {
        _ = c.Error(err) // handler 不写错误响应——挂上去，交给错误中间件
        return
    }
    c.JSON(http.StatusCreated, result)
}

func (h *BookHandler) Get(c *gin.Context) {
    id, err := strconv.ParseUint(c.Param("id"), 10, 64)
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "id 非法"})
        return
    }
    book, err := h.svc.GetBook(c.Request.Context(), uint(id))
    if err != nil {
        _ = c.Error(err) // gorm.ErrRecordNotFound → 404、其余 → 500，由中间件翻译
        return
    }
    c.JSON(http.StatusOK, book)
}
```

> **为什么 DTO 变了？** 前文若写"DTO 与校验规则不动"是错的：`POST /books` 要顺带收 `tags`，`createBookInput` 必须新增 `Tags []string`（1.4 注记已说明这是新增行为）；数据工程篇的校验翻译也收成了 `bindBookInput`，不会退回通用文案（import 见本片段头部：`errors` / `net/http` / `strconv` 与新增的 `validator/v10` 一次列全）。绑定/参数类 400 由 handler 就地返回——绑定错误不属于"数据错误"，不进错误中间件。

**错误怎么变成响应？（最小版错误中间件）** handler 只 `c.Error(err)` 不写响应——翻译由 handler 包提供的错误中间件完成；`gin.Engine` 上挂一个它，所有 handler 的错误路径就统一了。这里给最小版（404/500），篇 7 会升级为 ok/fail + `slog` + 504：

```go
// internal/handler/middleware.go
func errorMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        c.Next()
        logList := c.Errors // c.Error(err) 挂的就是 ErrorTypePrivate，不用再按类型过滤
        if len(logList) == 0 {
            return
        }
        status := http.StatusInternalServerError
        if errors.Is(logList.Last().Err, gorm.ErrRecordNotFound) {
            status = http.StatusNotFound
        }
        c.AbortWithStatusJSON(status, gin.H{"error": http.StatusText(status)})
    }
}

// ErrorMiddleware main 包注册用（handler 包导出）
func ErrorMiddleware() gin.HandlerFunc { return errorMiddleware() }
```

> **"删了 0 行"怎么变成 404？** 前五篇删除类接口的 404 来自 `RowsAffected == 0`（那不是 error）。收进 error 模型后，约定改为：**repository 实现把"0 行"转成 `gorm.ErrRecordNotFound` 返回**（删除/移除不存在 → 中间件 404；真错误 → 500）。这样"handler 只挂 error"才覆盖得住删除语义，不会出现"删不存在的书却返回 200"。代价是消息收敛为统一的 `Not Found`——前文的「图书不存在/评论不存在」等具体文案在中间件模型下不再透出，要区分需自定义错误类型（篇 7 的错误分类会处理）。

### 1.6 main.go：构造注入

依赖全部在 `main.go` 组装一次，从上到下"注入"，handler 永远不知道数据库长什么样：

```go
func main() {
    db.InitDB()
    if err := db.DB.AutoMigrate(&models.Book{}, &models.Comment{}, &models.Tag{}); err != nil {
        log.Fatal("迁移失败：", err)
    }

    // 组装依赖：db → repository → service → handler
    bookRepo := repository.NewBookRepository(db.DB)
    bookSvc := service.NewBookService(bookRepo, db.DB)
    bookH := handler.NewBookHandler(bookSvc)

    r := gin.Default()
    r.Use(requestTimeout(5 * time.Second))
    r.Use(handler.ErrorMiddleware()) // handler 只挂 c.Error，统一翻译 404/500（见 1.5）
    r.Static("/uploads", uploadDir)

    r.POST("/books", bookH.Create)
    r.GET("/books", bookH.List)
    // ...其余路由同构迁移（Get/Update/Delete/评论/标签/上传）
    r.Run(":8080")
}
```

> **顺序即契约：** 依赖方向永远从上往下——`db` 最底层、`handler` 最顶层。谁也不能反过来。想替换实现（比如切到 SQLite 驱动、切到 fake），只在 `main.go` 的组装行改一行。
>
> **跑通点（第一刀完成）：** 到这里书维度的两条链路（Create / Get）端到端通了——`go build ./...` 通过、`curl http://localhost:8080/books/1` 正常返回，说明注入链路成立。其余端点与这个骨架**完全同构**（接口加方法 → gorm 实现 → service 透传 → handler 挂错 → main 注册），逐个补齐即可；**上传与 cover 存储先保持直连 `db.DB`**（篇 7 的 Uploader 一起收）。

### 1.7 其余端点的迁移清单（同构模板）

下表把"同构"摊开成一行一个端点，差异只在参数与校验。顺带一个交接点：前五篇里每个 handler 开头那段"书必须存在"的检查，到这里正式退场——它下沉成 `repo.FirstBook`，handler 连 `db` 都不再 import。

| 端点（旧直连 handler）                  | repository 要补的方法                                                      | service 方法                 | handler 改造点        | 事务？                                                            |
| --------------------------------------- | -------------------------------------------------------------------------- | ---------------------------- | --------------------- | ----------------------------------------------------------------- |
| `PUT /books/:id`（部分更新）            | `FirstBook`（存在性→404）+ `UpdateStruct`（零值不更新，篇 1 语义）         | `UpdateBook`                 | parseID + bind        | 否                                                                |
| `DELETE /books/:id`（软删）             | `SoftDelete`（0 行 → `gorm.ErrRecordNotFound`）                            | `SoftDelete`                 | parseID + 挂错        | 否                                                                |
| `DELETE /books/:id/permanent`（物理删） | `HardDelete`                                                               | `HardDelete`                 | parseID + 挂错        | 不需要：关联已声明 `OnDelete:CASCADE`（篇 2 / 篇 5），数据库层级联清理 |
| 评论：增 / 分页查 / 删                  | `CreateComment` / `ListComments` / `DeleteComment`                         | 同名透传                     | parseID（`cid` 同理） | 否                                                                |
| 标签：加 / 删 / 按标签查书              | `FirstOrCreateTag` + `CountBookTag` + `AddTag` / `RemoveTag` / `ListByTag` | 按名解析 + 幂等（§1.3 口径） | parseID / `:name`     | 否                                                                |
| 上传封面                                | 篇 7 收口（`storage.Uploader` + `SetCover`）                               | `SetCover`                   | 见篇 7 完整代码       | 否                                                                |

> 提示：每个新 handler 的测试照 2.2 的模板补自己的分支表即可（查/删路径可用 fake，事务路径靠真库集成验证）。想对照最终实现，可直接看配套代码库中 `internal/{repository,service,handler}` 的完整形态（模块路径 `go-learning`）。

---

## 二、可测试性：fake repository + httptest 表驱动

**目标：** 用内存版 repository 把 database 从测试里摘掉，`httptest` 起一个真 Gin 路由打请求，表驱动覆盖所有状态码分支，然后 `go test`。

### 2.1 fake repository：接口的回报

`internal/handler/book_test.go`——接口定义时的许诺在这里兑现：

```go
package handler

import (
    "context"
    "errors"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "go-learning/internal/repository"
    "go-learning/internal/service"
    "go-learning/models"

    "github.com/gin-gonic/gin"
    "gorm.io/gorm"
)

// fakeBookRepo 内存实现：不碰数据库，就能演完所有分支
type fakeBookRepo struct {
    books     map[uint]*models.Book
    findErr   error
    createErr error
}

func (f *fakeBookRepo) Create(ctx context.Context, book *models.Book) error {
    if f.createErr != nil {
        return f.createErr
    }
    book.ID = uint(len(f.books) + 1)
    f.books[book.ID] = book
    return nil
}

func (f *fakeBookRepo) FindByID(ctx context.Context, id uint) (*models.Book, error) {
    if f.findErr != nil {
        return nil, f.findErr
    }
    if b, ok := f.books[id]; ok {
        return b, nil
    }
    return nil, gorm.ErrRecordNotFound
}
// 接口必须**全部**实现才能赋给 repository.BookRepository——漏一个方法就编译不过。
// 下面这行是编译期断言：go build 就会红，不用等测试。
var _ repository.BookRepository = (*fakeBookRepo)(nil)

// 用不到的方法给空实现（其余 16 个同形，为省篇幅折叠成单行）
func (f *fakeBookRepo) FirstBook(context.Context, uint) (*models.Book, error) { return nil, nil }
func (f *fakeBookRepo) Query(context.Context, string) *gorm.DB                { return nil }
func (f *fakeBookRepo) ListOrdered(context.Context, *gorm.DB, string, int, int) ([]models.Book, int64, error) {
    return nil, 0, nil
}
func (f *fakeBookRepo) List(context.Context, string, int, int) ([]models.Book, int64, error) {
    return nil, 0, nil
}
func (f *fakeBookRepo) ListByTag(context.Context, string) ([]models.Book, error) { return nil, nil }
func (f *fakeBookRepo) UpdateStruct(context.Context, uint, interface{}) error    { return nil }
func (f *fakeBookRepo) SetCover(context.Context, uint, string) error             { return nil }
func (f *fakeBookRepo) SoftDelete(context.Context, uint) error                   { return nil }
func (f *fakeBookRepo) HardDelete(context.Context, uint) error                   { return nil }
func (f *fakeBookRepo) CreateComment(context.Context, *models.Comment) error     { return nil }
func (f *fakeBookRepo) ListComments(context.Context, uint, int, int) ([]models.Comment, int64, error) {
    return nil, 0, nil
}
func (f *fakeBookRepo) DeleteComment(context.Context, uint, uint) error { return nil }
func (f *fakeBookRepo) AddTag(context.Context, uint, uint) error        { return nil }
func (f *fakeBookRepo) RemoveTag(context.Context, uint, uint) error     { return nil }
func (f *fakeBookRepo) FirstOrCreateTag(context.Context, string) (*models.Tag, error) {
    return nil, nil
}
func (f *fakeBookRepo) CountBookTag(context.Context, uint, uint) (int64, error) { return 0, nil }
```

> **fake 与 mock 的差别（一句话）：** fake 是"能干活的内存实现"（上面这个真能返回数据）；mock 是 sqlmock 那种"断言你调用了什么"的替身。测 handler 用 fake 最顺手——你在测"行为"，不是测"调用序列"。

### 2.2 httptest 表驱动

`httptest.NewRecorder` + 真 `gin.Engine` 起路由，请求打进去，断言状态码与响应体：

```go
func TestBookHandler_Get(t *testing.T) {
    tests := []struct {
        name       string
        repo       *fakeBookRepo
        wantStatus int
        wantBody   string
    }{
        {"找到", &fakeBookRepo{books: map[uint]*models.Book{1: {Model: gorm.Model{ID: 1}, Title: "Go"}}}, 200, `"title":"Go"`},
        {"不存在", &fakeBookRepo{books: map[uint]*models.Book{}}, 404, `"error"`},
        {"查询失败", &fakeBookRepo{books: map[uint]*models.Book{}, findErr: errors.New("db down")}, 500, `"error"`},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            svc := service.NewBookService(tt.repo, nil) // fake 不需要真 db
            h := NewBookHandler(svc) // 篇 7 构造器变双参后写成 NewBookHandler(svc, nil)

            r := gin.New()
            r.Use(errorMiddleware()) // 与 main 同款：handler 挂错后由中间件翻译 404/500
            r.GET("/books/:id", h.Get)

            req := httptest.NewRequest(http.MethodGet, "/books/1", nil)
            w := httptest.NewRecorder()
            r.ServeHTTP(w, req)

            if w.Code != tt.wantStatus {
                t.Errorf("status = %d, want %d (body: %s)", w.Code, tt.wantStatus, w.Body.String())
            }
            if !strings.Contains(w.Body.String(), tt.wantBody) {
                t.Errorf("body = %s, want contains %s", w.Body.String(), tt.wantBody)
            }
        })
    }
}
```

运行：

```bash
go test ./internal/handler/ -v
# === RUN   TestBookHandler_Get/找到
# === RUN   TestBookHandler_Get/不存在
# === RUN   TestBookHandler_Get/查询失败
# --- PASS
```

> **表驱动的意义：** 一个用例 = 一行数据 + 一行断言。"找到 / 不存在 / 查询失败"三个分支并排躺着，新增分支就是加一行，不用复制测试函数。**被迁移的每个接口**都能照这个模板补自己的分支表（示例演示了 Get，其余同构）。两点边界要诚实：(1) 只有走 `repo` 接口的方法能被 fake 测（Get 这类查/删路径）；走 `s.db` 事务的方法（如 1.4 的 `CreateBookWithTags`）在 fake + `nil db` 下会 panic，它们的事务正确性靠对真库的集成验证；(2) 这里测的是 handler 的行为分支，repository 的 GORM 实现是否发出正确 SQL，见 2.3 的结论（不推荐用 sqlmock）。

### 2.3 repository 层要测吗：不建议 sqlmock

GORM 实现本身（`gormBookRepository` 的 SQL 行为）要不要单测？**不推荐用 sqlmock**：它断言的是"GORM 发出了你预想的 SQL"——而这正是 GORM 替你保证的事；SQL 还会随 GORM 版本变化，正则断言维护成本高、收益低。分层要测的是"我们的代码"（handler 分支、service 业务）；repository 的正确性交给集成测试（真库或 testcontainers），需要替身时优先在接口边界用 fake，而不是在 SQL 层。

---

## 三、[选读] 提效封装：泛型 GetPaginated[T]

前五篇正文故意把分页骨架写显式（教学）；工程篇把它收拢成一个泛型函数，放在 repository 包（要拿 `*gorm.DB` 链；放 handler 会让 repository 反向依赖 handler）：

```go
// internal/repository/pagination.go —— Count + Order + Offset + Limit 一次收拢
func GetPaginated[T any](query *gorm.DB, order string, page, pageSize int, dest *[]T) (int64, error) {
    var total int64
    if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
        return 0, err
    }
    if err := query.Order(order).
        Offset((page - 1) * pageSize).
        Limit(pageSize).
        Find(dest).Error; err != nil {
        return 0, err
    }
    return total, nil
}
```

> **`Session(&gorm.Session{})` 才是真知识**：`Count` 与 `Find` 复用同一条链时，GORM 会把 `SELECT count(*)` 的状态带下去；先克隆一条干净链，才能拿到"过滤后的总数 + 当前页数据"。就算不封装泛型，也建议照这个写法来。

用法就是把分页三行换掉：

```go
var books []models.Book
total, err := GetPaginated(query, order, page, pageSize, &books)
```

> **选读，且只对纯模型列表成立**：带 JOIN/聚合的列表（媒体篇的 `BookListItem`）`Find` 不进模型，再写一个 `Scan` 版只会多出一个几乎重复的函数——那种情况直接手写 `Count + Scan` 更清楚。响应形状（`{items,total,page,pageSize}`）归 handler，普通结构体或 `PageResult[T]` 都行，不展开。

---

## 本篇小结

- **新包结构**：`main.go` + `db/` + `models/` + `internal/{repository,service,handler}`；依赖方向 `db → repository → service → handler`，`main.go` 一次性构造注入；
- **本篇示范的迁移范围**：书维度的 Create / Get 两条链路端到端走完（接口 → gorm 实现 → Service → handler → 注入 → 测试）；Update / Delete / 评论 / 标签 / 按标签查书与该骨架**同构**，按同一模板补齐即可；**上传 handler 仍直连 `db.DB`**，等篇 7 的 Uploader 抽象一起收；
- **你现在的项目**：三层架构 + fake/httptest 表驱动测试（示例覆盖 Get 的 404/500 分支，模板可复用到每个被迁移接口）、`go test ./internal/handler/ -v` 通过、分页样板已收拢进泛型 `GetPaginated[T]`（选读）；
- 下一篇[《GORM 工程化实战（二）：可靠性与生产化》](./gorm-gin-engineering-reliability)：统一错误中间件与 ok/fail 响应（`slog` 接管日志、超时映射 504）、GetBooks 排序白名单、上传文件头嗅探与对象存储抽象、连接池配置——把前五篇预告的可靠性条目一次结清。
