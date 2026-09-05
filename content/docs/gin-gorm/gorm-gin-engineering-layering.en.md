---
title: "GORM Engineering in Practice (Part 1): Layering, Dependency Injection & Testability"
description: "Part 6: refactors the flat db.DB code of the first five parts into Repository / Service / Handler layers (internal/ + constructor injection), tests handlers with a fake repository + httptest table-driven tests, and (optional reading) wraps the pagination boilerplate into a generic GetPaginated[T]."
date: 2026-09-06 00:00:00
series: gin-gorm
level: P4
tags:
  - Go
  - PostgreSQL
  - ORM
  - Engineering
---

## 📚 Series Navigation

This series has seven parts:

1. [**GORM Crash Course: Building a Book Management API with Gin + GORM**](./gorm-gin-crud-tutorial) — single-table CRUD, soft delete, the zero-value trap, a complete runnable project
2. [**GORM Relations in Practice: Comment Model, CRUD & Preload**](./gorm-gin-relations) — second table `comments`, comment CRUD, on-demand detail loading
3. [**GORM Media & Query Enhancement: Cover Upload, Pagination/Search & Comment Count**](./gorm-gin-media-query) — uploads & static serving, pagination/search/sort, comment-count aggregation
4. [**GORM Data Engineering: Batch Import, Request DTOs & Validation-Error Translation**](./gorm-gin-dto-batch) — batch import, request DTOs, validation-error translation
5. [**GORM Many-to-Many in Practice: Books & Tags**](./gorm-gin-tags) — many2many join table, tag filtering, association add/remove
6. [**GORM Engineering in Practice (Part 1): Layering, Dependency Injection & Testability**](./gorm-gin-engineering-layering) — Repository/Service layering, constructor injection, table-driven tests (P4, optional reading)
7. [**GORM Engineering in Practice (Part 2): Reliability & Production Readiness**](./gorm-gin-engineering-reliability) — unified errors, security hardening, object storage, connection pool (P4, optional reading)

---

> 🎓 **Engineering optional reading:** this part does not sit on the same track as the first five parts (the P2/P3 main line) — read it when you need layering, dependency injection, and testing skills beyond GORM itself; skipping it does not break the closed loop of the first five parts.

> **Prerequisites:** finish the first five parts of the series — the project with the full endpoint set across the `books` + `comments` + `tags` tables, cover images, pagination/search, and batch import. Code conventions match the first five parts (`WithContext` / `errors.Is` for 404 / the 400 · 404 · 201 semantics). This part sits one level above them: its center of gravity shifts from "how to use GORM" to "how to organize a program".

The first five parts kept wiring `db.DB` straight into the handlers — it ran, but it couldn't be tested and was hard to maintain. This part makes good on the engineering this series has been pointing toward all along: split the code into `Repository / Service / Handler` layers, use constructor injection to take the database out of the handlers, then use a fake repository + `httptest` to turn handlers into something `go test` can run.

> **Map of this part (read it before you walk in):** several new concepts the first five parts never used appear here all at once: **interfaces** (`BookRepository` — first declare "what data access needs", with the implementation hidden behind it), **constructor injection** (`main.go` "feeds" the concrete implementation to the upper layers), **the `internal/` package rule**, **test doubles** (a fake repository) and **httptest table-driven tests**, and finally a **generic wrapper** (optional reading). Don't let the sheer number of new terms scare you — the body proceeds in the order "hit the pain point first → migrate just one path and get it running → then talk about scale and consolidation", and every step explains the "why" first. The generics section can be skipped.
>
> Also note the **module path**: the import prefixes in this article's code blocks are `go-learning/` (the module name of the companion sample project). The project you built in the crash course is named `gin-demo` — before you start, replace every `go-learning/` prefix in this article with your own module name (or run `go mod edit -module go-learning` to rename yours to match), and leave the rest of the code unchanged. We won't repeat this reminder below.

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
    FindByID(ctx context.Context, id uint) (*models.Book, error) // full detail, with Comments and Tags loaded
    Query(ctx context.Context, q string) *gorm.DB                // the chain with filters applied (Part 7's sort whitelist will use it)
    ListOrdered(ctx context.Context, query *gorm.DB, order string, page, pageSize int) ([]models.Book, int64, error)
    List(ctx context.Context, q string, page, pageSize int) ([]models.Book, int64, error) // convenience version: ListOrdered with a fixed descending order
    UpdateBook(ctx context.Context, book *models.Book, fields map[string]interface{}) error
    SoftDelete(ctx context.Context, id uint) error
    AddTag(ctx context.Context, bookID, tagID uint) error
    RemoveTag(ctx context.Context, bookID, tagID uint) error
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

> **How big should an interface be?** What's above is the final shape **for this part's migration scope** — it only collects the "book + tag" dimension of data access (the comment and upload handlers aren't migrated in this part yet; they still hit `db.DB` directly, and Part 7's `Uploader` abstraction will fold them in together). For teaching, access is consolidated into a small number of interfaces around aggregate roots with identical shape; real projects often split into multiple interfaces by aggregate root — the principle is the same.
>
> One more point to align on up front: `AddTag(bookID, tagID uint)` is a low-level operation **by id** — whereas the route/front-end semantics tag **by name** (see the many-to-many part, §3.1). "Resolving names to ids, creating tags by name, deduping idempotently" is the Service's job (see 1.4); the interface layer exposes only the most atomic data operations.

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

> **The honest trade-off between transactions and layering:** a transaction is naturally "cross-repository" — one transaction touches the book, the tags, and the join table in three places. Stuffing transactions into the repository interface would make it explode immediately (the Unit-of-Work style of `WithTx(func(repo) error)`). This part takes the pragmatic route: `*gorm.DB` is injected into Service directly for transactions, and all other data access goes through the `repo` interface. When it grows bigger, evolve — this is the same kind of trade-off as the "teaching gradient" pacing used in every earlier part (difficulty is deliberately staged across parts).
>
> **Then if Service grabs `db` directly, what is the Repository interface still shielding?**
>
> The interface shields **single data operations** — one-shot "data accesses" like `Create` / `FindByID` / `SoftDelete` can be swapped out, can be faked, and the branching logic of handlers / services therefore becomes testable.
>
> Transactions are another matter: they are **orchestrations across multiple operations** and by nature don't belong to any single repo method. The three common arrangements:
>
> 1. **Service holds `*gorm.DB` for transactions** (this part's route): the most straightforward; the cost is that transactional paths can't be unit-tested with a fake — they need integration verification against a real database.
> 2. **Unit of Work**: `repo.Transaction(ctx, func(tx BookRepository) error)` pulls the transaction boundary into the interface — the best testability, but interface size, nesting, and implementation cost all go up; it suits medium-to-large projects dense with transactions.
> 3. **Inject a "transaction runner"**: Service depends on a `TxRunner` interface rather than a bare `*gorm.DB`, and you swap the runner in tests — ①'s testable upgrade, at the cost of just one extra interface method.
>
> This part picks ① for **teaching first**: lay "how a transaction is written" out plainly to see it clearly, and leave the engineering trade-offs to real projects; ② and ③ are its evolution directions, not "more correct" options.
>
> This is also why the series has no "Engineering (Part 3)" — the seven parts close the loop, and what remains is for readers to make up at real-world scale.
>
> **How to choose (run the three criteria and you have your answer):** ① is the transaction really "cross-table/cross-aggregate" (operating on a single row needs no transaction at all); ② should transactional paths be in automated tests (if yes → don't stop at ①); ③ the scale signals — more than ~10 repo interface methods, or more than ~3 transaction sites (if hit → move up from ① to ③). Stopping at ① is perfectly reasonable for small projects and teaching; once ② or ③ triggers, switch to ③ early (② fits transaction-dense scenarios where the team is already used to Unit of Work).

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

// createBookInput: adds Tags on top of the data-engineering part's DTO (see the 1.4 note: tagging on POST /books is new behavior)
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

func (h *BookHandler) GetByID(c *gin.Context) {
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
// internal/handler/error.go
func errorMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        c.Next()
        logList := c.Errors.ByType(gin.ErrorTypePrivate)
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
> **The runnable checkpoint (first slice done):** by now you've only migrated two book-dimension paths (Create / GetByID) — verify immediately: `go build ./...` passes, and `curl http://localhost:8080/books/1` returns normally, which proves the injection chain is wired. The remaining endpoints (Update / Delete / comments / tags / find books by tag) are **fully isomorphic** to this skeleton: add a method to the interface → pass it through in service → attach the error in the handler → register in main; just fill them in one by one. **Uploads and cover storage stay wired straight to `db.DB` for now** (Part 7's `Uploader` will fold them in together).

### 1.7 A Migration Checklist for the Remaining Endpoints (the Isomorphic Template)

The first slice demonstrated the skeleton; every remaining endpoint is just filled in line by line — each row walks the same drill of "add a method to the interface → GORM implementation → pass-through in service → attach the error in the handler → register in main", differing from the Create / GetByID above only in parameters and validation:

| Endpoint (old direct-`db.DB` handler) | Repository methods to add | Service method | Handler changes | Transaction? |
| ------------------------------------- | ------------------------- | -------------- | --------------- | ------------ |
| `PUT /books/:id` (partial update) | `FirstBook` (existence check → 404) + `UpdateStruct` (zero values not updated — the crash course's semantics) | `UpdateBook` | parseID + bind | No |
| `DELETE /books/:id` (soft delete) | `SoftDelete` (0 rows → `gorm.ErrRecordNotFound`) | `SoftDelete` | parseID + attach error | No |
| `DELETE /books/:id/permanent` (hard delete) | `HardDelete` | `HardDelete` | parseID + attach error | Suggested: clear the associations with `Select(clause.Associations)` first, then delete (tags part §3.4) |
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
// ...implement the remaining methods as needed; ones you don't need can just return dummy data
```

> **The difference between a fake and a mock (one sentence):** a fake is an in-memory implementation that actually works (the one above really returns data); a mock is a double of the sqlmock kind that "asserts what you called". For testing handlers, a fake is the most comfortable — you're testing "behavior", not "call sequences".

### 2.2 Table-Driven Tests with httptest

`httptest.NewRecorder` plus a real `gin.Engine` to mount the route — fire the request in, assert the status code and the response body:

```go
func TestBookHandler_GetByID(t *testing.T) {
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
            h := NewBookHandler(svc)

            r := gin.New()
            r.Use(errorMiddleware()) // same as in main: after the handler attaches the error, the middleware translates it into 404/500
            r.GET("/books/:id", h.GetByID)

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
# === RUN   TestBookHandler_GetByID/found
# === RUN   TestBookHandler_GetByID/not_found
# === RUN   TestBookHandler_GetByID/query_failed
# --- PASS
```

> **What table-driven tests are for:** one test case = one row of data + one row of assertion. The three branches "found / not found / query failed" lie side by side; adding a branch is adding a row, not copying a test function. **Every migrated endpoint** can fill in its own branch table from this template (the example demonstrates GetByID; the rest are isomorphic). Two boundaries worth being honest about: (1) only methods that go through the `repo` interface can be tested with the fake (read/delete paths like GetByID); methods that go through `s.db` transactions (like 1.4's `CreateBookWithTags`) would panic under a fake + `nil db`, so their transactional correctness is verified by integration tests against a real database; (2) what's tested here is the handler's behavior branches — whether the repository's GORM implementation emits the right SQL is verified with the sqlmock idea in 2.3 (kept brief).

### 2.3 Does the Repository Layer Need Tests? sqlmock in One Paragraph

Handlers are covered — but should the GORM implementation itself (`gormBookRepository`'s SQL behavior) be tested? It can be, with go-sqlmock (`github.com/DATA-DOG/go-sqlmock`):

```go
db, mockSQL, _ := sqlmock.New()
gormDB, _ := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})

mockSQL.ExpectQuery(`SELECT .* FROM "books" WHERE id = .*`).
    WillReturnRows(sqlmock.NewRows([]string{"id", "title"}).AddRow(1, "Go"))

repo := NewBookRepository(gormDB)
book, err := repo.FindByID(context.Background(), 1)
```

This snippet carries value and cost in equal measure: what it asserts is "GORM emitted the SQL you expected" — **which is exactly what GORM guarantees for you**. Teaching conclusion: **what layering must test is "our code" (handler branches, service business logic); GORM's correctness is GORM's own responsibility**. So this part's main act is the fake + httptest; sqlmock is touched only briefly.

---

## 3. [Optional Reading] Wrapping the Boilerplate: Generic GetPaginated[T]

**Goal:** use generics to fold `Count + Order + Offset + Limit + error handling` into one function, leaving each list endpoint with only "filling in the arguments" to do.

The first five parts' body deliberately wrote the pagination skeleton out explicitly (that was teaching); this engineering part keeps the promise and wraps it up. The generic fetch function lives in the **repository package** (it needs the `*gorm.DB` chain; if it were in handler, repository would depend back on handler and form a cycle):

> **Optional reading:** this section is syntactic sugar — skipping it doesn't affect this part's main line. And it only works for "pure-model pagination" — lists like `GetPaginated[models.Book]` that `Find` directly into the model; aggregate lists like the media part's `BookListItem` with a JOIN / `commentCount` need a dedicated query shape (`Scan` into a custom carrier), so this generic doesn't apply there.

```go
// internal/repository/pagination.go — the generic fetch: Count + Order + Offset + Limit wrapped in one go
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

`ListOrdered` (see 1.3)'s three pagination lines can now be swapped for this function — the type parameter lets `[]models.Book`, `[]models.Comment` share one piece of logic:

```go
// inside repository.ListOrdered (the equivalent rewrite)
var books []models.Book
total, err := GetPaginated(query, order, page, pageSize, &books)
```

**The Scan variant: aggregate lists wrap up too.** The media part's "each book with its comment count" goes through `Scan` into `BookListItem` (a custom carrier) rather than `Find` into the model — with the same skeleton, only the last step changes from `Find` to `Scan`, and aggregate lists get pagination as well:

```go
// the Scan variant of the same skeleton: dest becomes a custom query carrier (BookListItem, etc.)
func GetPaginatedScan[T any](query *gorm.DB, order string, page, pageSize int, dest *[]T) (int64, error) {
    var total int64
    if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
        return 0, err
    }
    if err := query.Order(order).
        Offset((page - 1) * pageSize).
        Limit(pageSize).
        Scan(dest).Error; err != nil {
        return 0, err
    }
    return total, nil
}
```

Usage (the media part's §2.3 aggregate query — the chain already carries `Select/Joins/Group`; here we only add pagination):

```go
query := db.DB.WithContext(ctx).Model(&models.Book{}).
    Select("books.*, COUNT(comments.id) AS comment_count").
    Joins("LEFT JOIN comments ON comments.book_id = books.id AND comments.deleted_at IS NULL").
    Group("books.id")
var items []BookListItem
total, err := GetPaginatedScan(query, "books.created_at DESC", page, pageSize, &items)
```

In one sentence: the generic wraps the **pagination skeleton** (Count / ordering / Offset / Limit), not the way rows are fetched — pure-model lists use the `Find` version (`GetPaginated`), aggregate carriers use the `Scan` version (`GetPaginatedScan`), and the two differ only in the last line. Both count as optional reading.

The response shape belongs on the **handler** side, unified with `PageResult` (the return structure of the list endpoints):

```go
// internal/handler/pagination.go
type PageResult[T any] struct {
    Items    []T   `json:"items"`
    Total    int64 `json:"total"`
    Page     int   `json:"page"`
    PageSize int   `json:"pageSize"`
}
```

> **Why consolidate only now?** Echoing the opening line of this section — "the engineering part keeps its promises": generics fold the "pagination skeleton" into a black-box function — it was written explicitly in the body twice first ([GORM Relations in Practice](./gorm-gin-relations) §2.2, [GORM Media & Query Enhancement](./gorm-gin-media-query) §2.2), and only in the engineering part is encapsulation allowed; the timeout middleware follows the same idea ("hand-written in the body, and in the engineering parts it can be a one-line library swap").

---

## Wrap-Up

- **The new package structure**: `main.go` + `db/` + `models/` + `internal/{repository,service,handler}`, with dependencies flowing `db → repository → service → handler` and `main.go` constructor-injecting everything in one place;
- **The migration scope this part demonstrates**: the book dimension's two paths, Create / GetByID, run end to end (interface → GORM implementation → Service → handler → injection → tests); Update / Delete / comments / tags / find books by tag are **isomorphic** to that skeleton and just need filling in from the same template; **the upload handler still talks to `db.DB` directly**, waiting to be folded in together with Part 7's `Uploader` abstraction;
- **Your project now**: three-layer architecture + fake/httptest table-driven tests (the example covers GetByID's 404/500 branches, and the template is reusable for every migrated endpoint), `go test ./internal/handler/ -v` passing, and the pagination boilerplate wrapped into the generic `GetPaginated[T]` (optional reading);
- Next part, [GORM Engineering in Practice (Part 2): Reliability & Production Readiness](./gorm-gin-engineering-reliability): a unified error middleware with ok/fail responses (`slog` takes over logging, timeouts map to 504), the `GetBooks` sort whitelist, upload header sniffing and an object-storage abstraction, and connection-pool settings — settling every reliability item the first five parts foreshadowed in one go.
