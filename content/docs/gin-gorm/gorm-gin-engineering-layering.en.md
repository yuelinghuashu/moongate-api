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

In the crash course's `GetBook`, the data entry point is just that one line — `db.DB.WithContext(...).Preload(...).First(&book, id)` — and the handler is welded to "which specific row gets fetched". To test it you would need a real PostgreSQL in the test environment: slow, brittle, still needs seeded data, and failure branches like "connection dropped" are practically impossible to simulate; the user-visible behavior (status codes, JSON shape) is exactly what you cannot test — which is why the first five parts could only curl. **Testability = data access that can be swapped out**, and that is the whole motive for layering.

### 1.2 The Target Package Structure

The project evolves from the flat layout into (a direction the crash course's "learning-grade structure" note already planted long ago):

```text
go-learning/                    # the crash course's gin-demo module
├── main.go                     # assembly root: builds dependencies, registers routes
├── db/db.go
├── models/
├── internal/                   # internal/ package rule: importing it from outside is a compile error
│   ├── service/                # business logic (cross-table work, transactions) + the data-access interface
│   │   ├── store.go            # BookStore interface + ListParams (consumer side)
│   │   └── book.go
│   ├── repository/             # data-access implementation (GORM work happens only here; no interface)
│   │   ├── book.go
│   │   └── pagination.go       # generic GetPaginated[T] (section 3)
│   └── handler/                # HTTP layer (params, binding, responses)
│       ├── book.go
│       └── pagination.go
└── seed/books.json
```

The semantics of `internal/`: when a package path contains `internal`, only code inside its parent directory can import it — an external module referencing it fails to compile. It turns "this is this project's internal implementation" from a convention into a compile-time fact.

### 1.3 The Data-Access Contract: Put the Interface on the Consumer Side

First decide where the interface lives: **in `internal/service`, declaring only the methods the Service actually calls** (Go's convention is "accept interfaces, return structs", with the interface defined on the consumer side); `internal/repository` only supplies the implementation and defines no interface.

```go
// internal/service/store.go — consumer-side interface: only the 4 methods the Service calls
type BookStore interface {
    FindByID(ctx context.Context, id uint) (*models.Book, error)  // detail: comment preview + tags
    FirstBook(ctx context.Context, id uint) (*models.Book, error) // existence check, no associations
    List(ctx context.Context, p ListParams) ([]models.Book, int64, error)
    SetCover(ctx context.Context, id uint, url string) error
}

// ListParams carries the list query: the sort is mapped through the whitelist in the Service.
// A string, not a *gorm.DB — that is what makes the interface actually shield GORM.
type ListParams struct {
    Q              string // fuzzy search over title/author
    Order          string // whitelisted order fragment, e.g. "created_at DESC"
    Page, PageSize int
}
```

> **Why only 4 methods?** Because these are exactly what this part and the next one actually call: detail, existence check, list, set cover. Creating a book goes through the transaction connection (`tx.Create`, see 1.4), and the comment/tag/update/delete endpoints are not migrated here — their pending methods live in §1.7's checklist and get added when you really migrate that endpoint. **Let the interface grow on demand instead of laying out 18 methods for a fake to fill.**
>
> **Why no more `Query(ctx, q) *gorm.DB`?** Leaking a GORM chain through the interface makes the Service depend on GORM's concrete types, and "swap the implementation" becomes empty talk. With conditions and sorting folded into `ListParams`, only the repository touches `*gorm.DB` — the interface shields "where the database is and how it is queried".

The repository only supplies the implementation (`internal/repository/book.go`) and returns a concrete type:

```go
type gormBookRepository struct {
    db *gorm.DB
}

// NewBookRepository returns the concrete implementation (Go: return structs, accept interfaces)
func NewBookRepository(db *gorm.DB) *gormBookRepository {
    return &gormBookRepository{db: db}
}

// Compile-time assertion: the implementation must satisfy the Service's needs
var _ service.BookStore = (*gormBookRepository)(nil)

func (r *gormBookRepository) FindByID(ctx context.Context, id uint) (*models.Book, error) {
    var book models.Book
    if err := r.db.WithContext(ctx).
        Preload("Comments", func(tx *gorm.DB) *gorm.DB {
            // the detail carries only the 5 latest comments (bounded form: relations part §3.1)
            return tx.Order("created_at DESC").Limit(5)
        }).
        Preload("Tags").
        First(&book, id).Error; err != nil {
        return nil, err
    }
    return &book, nil
}

func (r *gormBookRepository) FirstBook(ctx context.Context, id uint) (*models.Book, error) {
    var book models.Book
    if err := r.db.WithContext(ctx).First(&book, id).Error; err != nil {
        return nil, err
    }
    return &book, nil
}

func (r *gormBookRepository) List(ctx context.Context, p service.ListParams) ([]models.Book, int64, error) {
    query := r.db.WithContext(ctx).Model(&models.Book{})
    if p.Q != "" {
        like := "%" + p.Q + "%"
        query = query.Where("title ILIKE ? OR author ILIKE ?", like, like)
    }
    var books []models.Book
    total, err := GetPaginated(query, p.Order, p.Page, p.PageSize, &books) // generic helper: section 3
    if err != nil {
        return nil, 0, err
    }
    return books, total, nil
}

// SetCover single-field update: 0 rows → ErrRecordNotFound (matches the "0 rows → 404" rule in 1.5)
func (r *gormBookRepository) SetCover(ctx context.Context, id uint, url string) error {
    res := r.db.WithContext(ctx).Model(&models.Book{}).Where("id = ?", id).Update("cover_path", url)
    if res.Error != nil {
        return res.Error
    }
    if res.RowsAffected == 0 {
        return gorm.ErrRecordNotFound
    }
    return nil
}
```

> Real projects split this by aggregate root into several small interfaces (`BookStore` / `CommentStore` / `TagStore`), each holding only its consumer's methods — the complete data-access surface is the §1.7 checklist.

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
    repo BookStore
    db   *gorm.DB // transactions naturally span repositories, so it needs a raw connection (see the note below)
}

func NewBookService(repo BookStore, db *gorm.DB) *BookService {
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

// GetBook detail: pass through to the repository (Comments preview + Tags, see 1.3's FindByID).
// Return errors untouched: gorm.ErrRecordNotFound is attached by the handler and mapped to 404
// by the error middleware.
func (s *BookService) GetBook(ctx context.Context, id uint) (*models.Book, error) {
    return s.repo.FindByID(ctx, id)
}

// ListBooks list (pagination + search). Part 7 §2.1 extends the signature to (q, sort, dir)
// once the sort whitelist lands.
func (s *BookService) ListBooks(ctx context.Context, q string, page, pageSize int) ([]models.Book, int64, error) {
    return s.repo.List(ctx, q, page, pageSize)
}
```

> **Why transactions live in the Service:** one transaction touches books, tags, and the join table, so it is naturally cross-repository; stuffing it into the repository interface means Unit of Work (`WithTx(func(repo) error)`) and an exploding interface. This part stays pragmatic: `*gorm.DB` goes into the Service for transactions, everything else goes through `repo` — the cost is that transactional paths are verified against a real database. Need them testable? Upgrade to a `TxRunner` (one extra interface method). "Delete the book + soft-delete its comments" (Part 2 §2.4's alternative) is the same kind of cross-table transaction and likewise belongs in the Service.

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

// createBookInput: the data-engineering part's §2 DTO moved over verbatim, plus one Tags line
// (Title / Author / Price and their rules are unchanged; Price stays *int + required)
type createBookInput struct {
    Title  string   `json:"title" binding:"required"`
    Author string   `json:"author" binding:"required"`
    Price  *int     `json:"price" binding:"required,gte=0,lte=1000000"`
    Tags   []string `json:"tags"`
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

// List: params → Service → response (pagination parsing reuses the media part's parsePagination;
// details omitted here)
func (h *BookHandler) List(c *gin.Context) {
    page, pageSize := parsePagination(c)
    books, total, err := h.svc.ListBooks(c.Request.Context(), c.Query("q"), page, pageSize)
    if err != nil {
        _ = c.Error(err)
        return
    }
    c.JSON(http.StatusOK, gin.H{"items": books, "total": total, "page": page, "pageSize": pageSize})
}
```

> **Why did the DTO change?** `POST /books` must now also accept `tags`, so `createBookInput` gains `Tags []string` on top of the data-engineering part version — every other field and rule (`Title`/`Author` required, `Price *int` + range) is untouched (the 1.4 note explains why the DTO changes and the model does not); the data-engineering part's validation translation is also folded into `bindBookInput`, so it won't regress to generic copy (the imports are listed all at once at the head of this snippet: `errors` / `net/http` / `strconv`, plus the newly added `validator/v10`). Binding/parameter 400s are returned by the handler on the spot — a binding error isn't a "data error" and doesn't go through the error middleware.

**How does an error become a response:** handlers only attach the error and never write a response; translation is one middleware, mounted once on the `gin.Engine`, and every handler's error path is unified:

```go
// internal/handler/middleware.go — the smallest version that runs
// (this file's imports: errors / net/http / gorm.io/gorm)
// slog, 504 mapping, and the ok/fail shape are upgraded together in Part 7 §1.3
func errorMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        c.Next()
        if len(c.Errors) == 0 {
            return
        }
        status := http.StatusInternalServerError
        if errors.Is(c.Errors.Last().Err, gorm.ErrRecordNotFound) {
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

The table below spreads the "isomorphic" claim into one row per endpoint; the differences are only parameters and validation. **Its second column is the complete map of "which data operations this project needs", not an interface definition** — the interface (`service.BookStore`) holds only what the Service currently calls, and migrating an endpoint adds its methods to the interface and the repo. One hand-off note: the "book must exist" check at the top of every handler in the first five parts now retires — it sinks into `repo.FirstBook`, and the handler doesn't even import `db` any more.

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

### 2.1 fake store: the payoff of putting the interface in the right place

`internal/handler/book_test.go` — because the interface has only 4 methods, the fake has only 4, and three of them carry real behavior:

```go
package handler

import (
    "context"
    "errors"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "go-learning/internal/service"
    "go-learning/models"

    "github.com/gin-gonic/gin"
    "gorm.io/gorm"
)

// fakeStore is an in-memory implementation: only service.BookStore's 4 methods
type fakeStore struct {
    books   map[uint]*models.Book
    findErr error
}

func (f *fakeStore) FindByID(ctx context.Context, id uint) (*models.Book, error) {
    if f.findErr != nil {
        return nil, f.findErr
    }
    if b, ok := f.books[id]; ok {
        return b, nil
    }
    return nil, gorm.ErrRecordNotFound
}

func (f *fakeStore) FirstBook(ctx context.Context, id uint) (*models.Book, error) {
    return f.FindByID(ctx, id)
}

func (f *fakeStore) List(ctx context.Context, p service.ListParams) ([]models.Book, int64, error) {
    return nil, 0, nil
}

func (f *fakeStore) SetCover(ctx context.Context, id uint, url string) error { return nil }

// Compile-time assertion: a missing method fails the build, not the test run
var _ service.BookStore = (*fakeStore)(nil)
```

> **This is what "let the interface grow on demand" buys you:** with 18 methods laid out up front, the fake needed 16 `return nil, nil` stubs — noisy, and dangerous: if the Service ever called one of the unimplemented methods, the fake would **silently return an empty result** and the test could "pass" without testing anything. Of the 4 methods here, only `List` is a stub (this round does not exercise lists); the rest behave.

### 2.2 Table-Driven Tests with httptest

`httptest.NewRecorder` plus a real `gin.Engine` to mount the route — fire the request in, assert the status code and the response body:

```go
func TestBookHandler_Get(t *testing.T) {
    tests := []struct {
        name       string
        store      *fakeStore
        wantStatus int
        wantBody   string
    }{
        {"found", &fakeStore{books: map[uint]*models.Book{1: {Model: gorm.Model{ID: 1}, Title: "Go"}}}, 200, `"title":"Go"`},
        {"not found", &fakeStore{books: map[uint]*models.Book{}}, 404, `"error"`},
        {"query failed", &fakeStore{books: map[uint]*models.Book{}, findErr: errors.New("db down")}, 500, `"error"`},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            svc := service.NewBookService(tt.store, nil) // the fake doesn't need a real db
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

> **Every migrated endpoint** can fill in its own branch table from this template (the example demonstrates Get; the rest are isomorphic). Two boundaries worth being honest about: (1) only methods that go through the `repo` interface can be tested with the fake (read/delete paths like Get); methods that go through `s.db` transactions (like 1.4's `CreateBookWithTags`) would panic under a fake + `nil db`, so their transactional correctness is verified by integration tests against a real database; (2) what's tested here is the handler's behavior branches — whether the repository's GORM implementation emits the right SQL — see 2.3's conclusion (sqlmock is not recommended).

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
