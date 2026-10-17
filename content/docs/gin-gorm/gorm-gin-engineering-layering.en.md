---
title: "GORM Engineering in Practice (Part 1): Layering, Dependency Injection & Testability"
description: "Part 6: refactors the flat db.DB code of the first five parts into Repository / Service / Handler layers (internal/ + constructor injection), tests handlers with a fake repository + httptest table-driven tests, and (optional reading) wraps the pagination boilerplate into a generic GetPaginated[T]."
date: 2026-09-06 00:00:00
series: gin-gorm
order: 6
tags:
  - Go
  - PostgreSQL
  - ORM
  - Engineering
---

## 1. The Layering Refactor: From Handlers Wired to db.DB to Three Layers

**Goal:** refactor the flat code of the five parts into `internal/repository` + `internal/service` + `internal/handler`; handlers stop touching the database, and every dependency is constructor-injected in `main.go`.

### 1.1 The Essence of the Problem: Why Handlers Can't Be Tested

Recall the crash course's `GetBook`:

```go
func GetBook(c *gin.Context) {
    // ...directly calls db.DB.WithContext(...).Preload(...).First(&book, id)
}
```

The handler is welded to "which specific row gets fetched". To test it, the test environment would need a real PostgreSQL — slow, brittle, and you'd still have to seed data. The user-visible behavior (status codes, JSON shape) can't be tested, which is exactly why the first five parts verified with curl. **Testability = data access that can be swapped out**, and that is the whole motive for layering.

> **Hit the wall first (why the first five parts could only curl):** suppose you want to write a unit test for the crash course's `GetBook` — the handler calls `db.DB.First(&book, id)` directly inside, so the test would have to: connect to a real PostgreSQL, create the tables, prepare data for the three states "exists / missing / database down", and make sure tests don't pollute each other. That is no longer a "unit test" — it's "stand up an environment and act out scenarios by hand"; and the failure branch (a dropped connection) is nearly impossible to simulate in a test. So the first five parts verifying with curl was not laziness — back then **the handlers had no swappable data entry point**. The next step in the body starts by giving data access exactly such a "swappable entry point".

### 1.2 The Target Package Structure

The project evolves from the flat layout into (a direction the crash course's "learning-grade structure" note already planted long ago):

```text
go-learning/                    # the crash course's gin-demo module
├── main.go                     # assembly root: builds dependencies, registers routes
├── db/db.go
├── models/
├── internal/                   # internal/ package rule: importing it from outside is a compile error
│   ├── repository/             # data access (GORM work happens only here)
│   │   └── book.go
│   ├── service/                # business logic (cross-table logic, transactions)
│   │   └── book.go
│   └── handler/                # HTTP layer (params, binding, responses)
│       ├── book.go
│       └── pagination.go
└── seed/books.json
```

The semantics of `internal/`: when a package path contains `internal`, only code inside its parent directory can import it — an external module referencing it fails to compile. It turns "this is this project's internal implementation" from a convention into a compile-time fact.

### 1.3 The BookRepository Interface

`internal/repository/book.go` — the interface declares "what data access needs"; the implementation stays hidden behind it. Below is the **final interface** as it looks after the migration; but don't write it all at once when you start: **the first slice only stands up 2–3 methods for a single path** (start with `FindByID`), and once 1.6's injection and tests run, fill in the rest from this checklist. Reading the full shape first is so you can see the complete boundary of "what data access needs":

```go
package repository

import (
    "context"

    "go-learning/models"

    "gorm.io/gorm"
)

// BookRepository is the data-access contract: handler/service see only the interface, never the implementation
type BookRepository interface {
    Create(ctx context.Context, book *models.Book) error
    FirstBook(ctx context.Context, id uint) (*models.Book, error) // existence check (no associations)
    FindByID(ctx context.Context, id uint) (*models.Book, error)  // full detail, with Comments and Tags loaded
    Query(ctx context.Context, q string) *gorm.DB                 // the chain with filters applied (Part 7's sort whitelist will use it)
    ListOrdered(ctx context.Context, query *gorm.DB, order string, page, pageSize int) ([]models.Book, int64, error)
    List(ctx context.Context, q string, page, pageSize int) ([]models.Book, int64, error) // convenience version: ListOrdered with a fixed descending order
    ListByTag(ctx context.Context, tagName string) ([]models.Book, error)
    UpdateStruct(ctx context.Context, id uint, input interface{}) error // Updates: zero values are skipped
    SetCover(ctx context.Context, id uint, url string) error
    SoftDelete(ctx context.Context, id uint) error
    HardDelete(ctx context.Context, id uint) error
    CreateComment(ctx context.Context, comment *models.Comment) error
    ListComments(ctx context.Context, bookID uint, page, pageSize int) ([]models.Comment, int64, error)
    DeleteComment(ctx context.Context, bookID, cid uint) error // cross-tenant guard: book_id + id
    AddTag(ctx context.Context, bookID, tagID uint) error
    RemoveTag(ctx context.Context, bookID, tagID uint) error
    FirstOrCreateTag(ctx context.Context, name string) (*models.Tag, error)
    CountBookTag(ctx context.Context, bookID, tagID uint) (int64, error)
}
```

> **An honest note on the half-interface:** `*gorm.DB` shows up in the signatures of `Query` / `ListOrdered` — the callers (Service — and Part 7's sort whitelist) still get a GORM chain back. This shows that "the interface fully shields GORM" is the ideal; in reality, to save boilerplate, query chains are often allowed to leak through. We accept this compromise for teaching (total isolation would mean abstracting queries into your own DSL, which is another topic entirely), but be clear about it: **what this interface shields is "where the database is and how it's connected" — not "querying with GORM"**.

The GORM implementation (`NewBookRepository` returns the interface, so callers never need to know the concrete type):

```go
type gormBookRepository struct {
    db *gorm.DB
}

// NewBookRepository builds the GORM implementation and upcasts it to the interface
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

// List is the convenience version: fixed descending order (call this directly when there's no sort parameter)
func (r *gormBookRepository) List(ctx context.Context, q string, page, pageSize int) ([]models.Book, int64, error) {
    return r.ListOrdered(ctx, r.Query(ctx, q), "created_at DESC", page, pageSize)
}
```

> **How big should an interface be?** What's above is the **repository's final shape** (18 methods, verbatim the companion repo) — this part only walks the Create / Get paths end to end and fills in the rest from the §1.7 checklist. Handing over the whole interface up front is so you can see the complete boundary of "what data access needs". For teaching, access is consolidated into a small number of interfaces around aggregate roots; real projects often split into multiple interfaces by aggregate root — the principle is the same.
>
> One more point to align on up front: `AddTag(bookID, tagID uint)` is a low-level operation **by id** — whereas the route/front-end semantics tag **by name** (see the many-to-many part, §3.1). "Resolving names to ids, creating tags by name, deduping idempotently" is the Service's job (see 1.4); the interface layer exposes only the most atomic data operations.
>
> **Real projects split this by aggregate root.** The single 18-method interface here exists so §1.7's migration checklist fits in one table; in practice you'd split it into three: `BookRepository` (Create / FirstBook / FindByID / List / Update / SoftDelete / SetCover), `CommentRepository` (CreateComment / ListComments / DeleteComment), `TagRepository` (FirstOrCreateTag / AddTag / RemoveTag / CountBookTag / ListByTag). The handler then injects two more fields and gains an interface per aggregate that can be swapped and faked independently — pick the boundary that fits, don't copy the 18-method version as a template.

### 1.4 The Service Layer & Transactions

Service holds "cross-table business logic". The closest thing to business logic in the first five parts' handlers was actually the batch import (data file → chunked inserts), but it has no cross-table semantics. Let's give Service a real foothold: **creating a book + tagging it by name** is two steps (create the tag first if it doesn't exist, then write the join table) — wrap them into one atomic operation with `db.Transaction()`. The `FirstOrCreate` dedup semantics of name-based tagging were already covered in [§3.1 of the many-to-many part](./gorm-gin-tags); here we only look at how a transaction fuses them into one step.

> ⚠️ **This is new behavior, not a pure refactor:** the first five parts' `POST /books` only created a book; from this part on it additionally accepts a `tags` array and completes "create the book + apply the tags" in one step. The tutorial uses "adding a genuinely cross-table scenario to an existing endpoint" to give Service a foothold — if you'd rather not change the endpoint's behavior, you can skip this section and let Service start as pure pass-through, coming back when you meet a real transaction. What's demonstrated here is "when a transaction is needed, which layer the code should live in".

```go
package service

import (
    "context"

    "go-learning/models"
    "gorm.io/gorm"
)

// BookService holds business logic: depends on the interface, not on the database
type BookService struct {
    repo BookRepository
    db   *gorm.DB // transactions naturally span repositories, so it needs a raw connection (see the note below)
}

func NewBookService(repo BookRepository, db *gorm.DB) *BookService {
    return &BookService{repo: repo, db: db}
}

// CreateBookWithTags does "create the book + apply the tags" in one step; any failure rolls everything back
func (s *BookService) CreateBookWithTags(ctx context.Context, book *models.Book, tagNames []string) (*models.Book, error) {
    err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
        // 1. Create the book
        if err := tx.Create(book).Error; err != nil {
            return err
        }
        // 2. For each tag: create it first if missing (FirstOrCreate), then write the join table
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

> **The honest trade-off between transactions and layering:** a transaction is naturally "cross-repository" (one transaction touches books, tags, and the join table), and stuffing it into the repository interface explodes the interface (the Unit-of-Work style of `WithTx(func(repo) error)`). This part stays pragmatic: `*gorm.DB` goes into Service for transactions, everything else goes through `repo`. The interface still shields **single data operations** (`Create` / `FindByID` / `SoftDelete` can be faked, so handler branches are testable); a transaction is **orchestration across operations** and belongs to no single repo method. Three arrangements:
>
> 1. **Service holds `*gorm.DB` for transactions** (this part): simplest, but transactional paths are verified against a real database.
> 2. **Unit of Work**: `repo.Transaction(ctx, func(tx BookRepository) error)` — best testability, higher interface and implementation cost.
> 3. **Inject a `TxRunner` interface**: ①'s testable upgrade, one extra interface method.
>
> Choosing: is the transaction genuinely cross-table, do transactional paths need automated tests, does the repo interface exceed ~10 methods or the transaction sites ~3 — once the last two trigger, move from ① to ③ early. That is also why the series has no "Engineering (Part 3)": seven parts close the loop, and the rest is for real-world scale.

### 1.5 Handlers Are Left with Only Three Jobs

Handlers now only do: take parameters from Gin, call the Service, write the response. Where does the data come from? That's the Service's business:

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

// createBookInput: one extra line on top of the data-engineering part's DTO
// (Title / Author / Price and their rules are unchanged; the full struct is in
// that part's §2 or the companion repo's internal/handler/book.go)
type createBookInput struct {
    Tags []string `json:"tags"`
}

// bindBookInput does binding + validation-error translation: folds the data-engineering part's §2.1 logic into one function every handler reuses
func bindBookInput(c *gin.Context) (*createBookInput, bool) {
    var input createBookInput
    if err := c.ShouldBindJSON(&input); err != nil {
        var ve validator.ValidationErrors
        if errors.As(err, &ve) && len(ve) > 0 {
            e := ve[0]
            var msg string
            switch e.Tag() {
            case "required":
                msg = "missing required field " + e.Field()
            case "gte":
                msg = e.Field() + " cannot be less than " + e.Param()
            case "lte":
                msg = e.Field() + " cannot be greater than " + e.Param()
            default:
                msg = "invalid parameter"
            }
            c.JSON(http.StatusBadRequest, gin.H{"error": msg})
            return nil, false
        }
        c.JSON(http.StatusBadRequest, gin.H{"error": "please send valid JSON"})
        return nil, false
    }
    return &input, true
}

// BookHandler depends only on the Service; swapping in a fake for tests is a one-line change
type BookHandler struct {
    svc *service.BookService
}

func NewBookHandler(svc *service.BookService) *BookHandler {
    return &BookHandler{svc: svc}
}

func (h *BookHandler) Create(c *gin.Context) {
    input, bound := bindBookInput(c)
    if !bound {
        return // the 400 was already written by bindBookInput
    }
    book := models.Book{Title: input.Title, Author: input.Author, Price: *input.Price}
    result, err := h.svc.CreateBookWithTags(c.Request.Context(), &book, input.Tags)
    if err != nil {
        _ = c.Error(err) // the handler doesn't write the error response — attach it and leave it to the error middleware
        return
    }
    c.JSON(http.StatusCreated, result)
}

func (h *BookHandler) Get(c *gin.Context) {
    id, err := strconv.ParseUint(c.Param("id"), 10, 64)
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
        return
    }
    book, err := h.svc.GetBook(c.Request.Context(), uint(id))
    if err != nil {
        _ = c.Error(err) // gorm.ErrRecordNotFound → 404, anything else → 500, translated by the middleware
        return
    }
    c.JSON(http.StatusOK, book)
}
```

> **Why did the DTO change?** It would have been wrong for the earlier text to claim "the DTO and validation rules stay untouched": `POST /books` must now also accept `tags`, so `createBookInput` has to add `Tags []string` (the 1.4 note already explained this is new behavior); the data-engineering part's validation translation is also folded into `bindBookInput`, so it won't regress to generic copy (the imports are listed all at once at the head of this snippet: `errors` / `net/http` / `strconv`, plus the newly added `validator/v10`). Binding/parameter 400s are returned by the handler on the spot — a binding error isn't a "data error" and doesn't go through the error middleware.

**How does an error become a response? (the minimal error middleware)** Handlers only call `c.Error(err)` and never write a response — the translation is done by an error middleware provided by the handler package; mount one on the `gin.Engine` and every handler's error path is unified. Here's the minimal version (404/500); Part 7 upgrades it to ok/fail + `slog` + 504:

```go
// internal/handler/middleware.go
func errorMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        c.Next()
        logList := c.Errors // c.Error(err) already attaches ErrorTypePrivate; no need to filter by type
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

// ErrorMiddleware is what the main package registers (exported by the handler package)
func ErrorMiddleware() gin.HandlerFunc { return errorMiddleware() }
```

> **How does "deleted 0 rows" become a 404?** In the first five parts, 404s on delete endpoints came from `RowsAffected == 0` (which isn't an error). Once folded into the error model, the convention changes: **repository implementations translate "0 rows" into a returned `gorm.ErrRecordNotFound`** (deleting/removing something that doesn't exist → the middleware returns 404; a real error → 500). That's how "handlers only attach errors" can cover delete semantics — you won't see "deleting a nonexistent book returns 200". The cost: messages converge to a uniform `Not Found` — the specific copy from earlier parts, like "book not found" / "comment not found", no longer surfaces under the middleware model; telling them apart requires custom error types (Part 7's error taxonomy will handle that).

### 1.6 main.go: Constructor Injection

Every dependency is assembled once in `main.go`, "injected" from the top down; handlers never learn what the database looks like:

```go
func main() {
    db.InitDB()
    if err := db.DB.AutoMigrate(&models.Book{}, &models.Comment{}, &models.Tag{}); err != nil {
        log.Fatal("migration failed:", err)
    }

    // assemble dependencies: db → repository → service → handler
    bookRepo := repository.NewBookRepository(db.DB)
    bookSvc := service.NewBookService(bookRepo, db.DB)
    bookH := handler.NewBookHandler(bookSvc)

    r := gin.Default()
    r.Use(requestTimeout(5 * time.Second))
    r.Use(handler.ErrorMiddleware()) // handlers only attach c.Error; this translates 404/500 uniformly (see 1.5)
    r.Static("/uploads", uploadDir)

    r.POST("/books", bookH.Create)
    r.GET("/books", bookH.List)
    // ...the remaining routes migrate isomorphically (Get/Update/Delete/comments/tags/upload)
    r.Run(":8080")
}
```

> **The order is the contract:** dependencies always point top-down — `db` at the bottom, `handler` at the top. Nobody may reverse it. To swap an implementation (say, switch to the SQLite driver or to a fake), change one line in `main.go`'s assembly.
>
> **The runnable checkpoint (first slice done):** the two book-dimension paths (Create / Get) now run end to end — `go build ./...` passes and `curl http://localhost:8080/books/1` returns normally, which proves the injection chain is wired. The remaining endpoints are **fully isomorphic** (add a method to the interface → GORM implementation → pass-through in service → attach the error in the handler → register in main); just fill them in one by one. **Uploads and cover storage stay wired straight to `db.DB` for now** (Part 7's `Uploader` folds them in).

### 1.7 A Migration Checklist for the Remaining Endpoints (the Isomorphic Template)

The table below spreads the "isomorphic" claim into one row per endpoint; the differences are only parameters and validation. One hand-off note: the "book must exist" check at the top of every handler in the first five parts now retires — it sinks into `repo.FirstBook`, and the handler doesn't even import `db` any more.

| Endpoint (old direct-`db.DB` handler) | Repository methods to add | Service method | Handler changes | Transaction? |
| ------------------------------------- | ------------------------- | -------------- | --------------- | ------------ |
| `PUT /books/:id` (partial update) | `FirstBook` (existence check → 404) + `UpdateStruct` (zero values not updated — the crash course's semantics) | `UpdateBook` | parseID + bind | No |
| `DELETE /books/:id` (soft delete) | `SoftDelete` (0 rows → `gorm.ErrRecordNotFound`) | `SoftDelete` | parseID + attach error | No |
| `DELETE /books/:id/permanent` (hard delete) | `HardDelete` | `HardDelete` | parseID + attach error | Not needed: associations declare `OnDelete:CASCADE` (Parts 2 and 5), so the database cascades |
| Comments: create / paginated list / delete | `CreateComment` / `ListComments` / `DeleteComment` | same-name pass-through | parseID (`cid` likewise) | No |
| Tags: add / remove / list books by tag | `FirstOrCreateTag` + `CountBookTag` + `AddTag` / `RemoveTag` / `ListByTag` | name resolution + idempotency (§1.3's convention) | parseID / `:name` | No |
| Cover upload | rolled up in Part 7 (`storage.Uploader` + `SetCover`) | `SetCover` | see Part 7's full code | No |

> Tip: for each new handler, write its own branch table following the 2.2 template (read/delete paths can use the fake; transactional paths are verified by integration tests against the real database). To compare against the finished implementation, read the complete `internal/{repository,service,handler}` in the companion sample repo (module path `go-learning`).

---

## 2. Testability: A Fake Repository + Table-Driven httptest

**Goal:** take the database out of tests with an in-memory repository, start a real Gin route with `httptest` and fire requests at it, cover every status-code branch table-driven, then `go test`.

### 2.1 The Fake Repository: The Payoff of the Interface

`internal/handler/book_test.go` — this is where the promise made when the interface was defined gets kept:

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

// fakeBookRepo is an in-memory implementation: it can play out every branch without touching the database
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
// The interface must be **fully** implemented before it can be assigned to
// repository.BookRepository — miss one method and it won't compile.
// This line is a compile-time assertion: go build fails, no test run needed.
var _ repository.BookRepository = (*fakeBookRepo)(nil)

// Unused methods get empty implementations (the other 16, folded to one line each for space)
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

> **The difference between a fake and a mock (one sentence):** a fake is an in-memory implementation that actually works (the one above really returns data); a mock is a double of the sqlmock kind that "asserts what you called". For testing handlers, a fake is the most comfortable — you're testing "behavior", not "call sequences".

### 2.2 Table-Driven Tests with httptest

`httptest.NewRecorder` plus a real `gin.Engine` to mount the route — fire the request in, assert the status code and the response body:

```go
func TestBookHandler_Get(t *testing.T) {
    tests := []struct {
        name       string
        repo       *fakeBookRepo
        wantStatus int
        wantBody   string
    }{
        {"found", &fakeBookRepo{books: map[uint]*models.Book{1: {Model: gorm.Model{ID: 1}, Title: "Go"}}}, 200, `"title":"Go"`},
        {"not found", &fakeBookRepo{books: map[uint]*models.Book{}}, 404, `"error"`},
        {"query failed", &fakeBookRepo{books: map[uint]*models.Book{}, findErr: errors.New("db down")}, 500, `"error"`},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            svc := service.NewBookService(tt.repo, nil) // the fake doesn't need a real db
            h := NewBookHandler(svc) // once Part 7 makes the constructor two-arg: NewBookHandler(svc, nil)

            r := gin.New()
            r.Use(errorMiddleware()) // same as in main: after the handler attaches the error, the middleware translates it into 404/500
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

Run it:

```bash
go test ./internal/handler/ -v
# === RUN   TestBookHandler_Get/found
# === RUN   TestBookHandler_Get/not_found
# === RUN   TestBookHandler_Get/query_failed
# --- PASS
```

> **What table-driven tests are for:** one test case = one row of data + one row of assertion. The three branches "found / not found / query failed" lie side by side; adding a branch is adding a row, not copying a test function. **Every migrated endpoint** can fill in its own branch table from this template (the example demonstrates Get; the rest are isomorphic). Two boundaries worth being honest about: (1) only methods that go through the `repo` interface can be tested with the fake (read/delete paths like Get); methods that go through `s.db` transactions (like 1.4's `CreateBookWithTags`) would panic under a fake + `nil db`, so their transactional correctness is verified by integration tests against a real database; (2) what's tested here is the handler's behavior branches — whether the repository's GORM implementation emits the right SQL — see 2.3's conclusion (sqlmock is not recommended).

### 2.3 Should the Repository Layer Be Tested? Don't Use sqlmock

Should the GORM implementation itself (`gormBookRepository`'s SQL behavior) have unit tests? **sqlmock is not recommended**: it asserts "GORM emitted the SQL you expected" — which is exactly what GORM guarantees for you; the SQL also shifts between GORM versions, so regex assertions are costly to maintain and yield little. What layering must test is "our code" (handler branches, service business logic); the repository's correctness is covered by integration tests (a real database or testcontainers), and when you do need a double, prefer a fake at the interface boundary rather than at the SQL layer.

---

## 3. [Optional Reading] Wrapping the Boilerplate: Generic GetPaginated[T]

The first five parts deliberately wrote the pagination skeleton out explicitly (teaching); the engineering part folds it into one generic function, placed in the repository package (it needs the `*gorm.DB` chain; putting it in handler would make repository depend on handler):

```go
// internal/repository/pagination.go — Count + Order + Offset + Limit in one place
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

> **`Session(&gorm.Session{})` is the real point.** When `Count` and `Find` reuse the same chain, GORM carries the `SELECT count(*)` state into the next query; clone a clean chain first to get "filtered total + current page". Follow this even if you don't wrap the generic.

Usage is just replacing the three pagination lines:

```go
var books []models.Book
total, err := GetPaginated(query, order, page, pageSize, &books)
```

> **Optional, and only for pure-model lists**: aggregate lists with JOINs (the media part's `BookListItem`) don't `Find` into the model — writing a `Scan` variant would just add a nearly identical function; write `Count + Scan` by hand there. The response shape (`{items,total,page,pageSize}`) belongs to the handler; a plain struct or `PageResult[T]` both work, not expanded here.

---

## Wrap-Up

- **The new package structure**: `main.go` + `db/` + `models/` + `internal/{repository,service,handler}`, with dependencies flowing `db → repository → service → handler` and `main.go` constructor-injecting everything in one place;
- **The migration scope this part demonstrates**: the book dimension's two paths, Create / Get, run end to end (interface → GORM implementation → Service → handler → injection → tests); Update / Delete / comments / tags / find books by tag are **isomorphic** to that skeleton and just need filling in from the same template; **the upload handler still talks to `db.DB` directly**, waiting to be folded in together with Part 7's `Uploader` abstraction;
- **Your project now**: three-layer architecture + fake/httptest table-driven tests (the example covers Get's 404/500 branches, and the template is reusable for every migrated endpoint), `go test ./internal/handler/ -v` passing, and the pagination boilerplate wrapped into the generic `GetPaginated[T]` (optional reading);
- Next part, [GORM Engineering in Practice (Part 2): Reliability & Production Readiness](./gorm-gin-engineering-reliability): a unified error middleware with ok/fail responses (`slog` takes over logging, timeouts map to 504), the `GetBooks` sort whitelist, upload header sniffing and an object-storage abstraction, and connection-pool settings — settling every reliability item the first five parts foreshadowed in one go.
