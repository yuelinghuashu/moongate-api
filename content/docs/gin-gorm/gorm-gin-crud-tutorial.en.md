---
title: "GORM Crash Course: Building a Book Management API with Gin + GORM"
description: "Build a complete book-management API from scratch: environment, models, and the CRUD mainline in one pass — with soft delete, the zero-value trap, and error checks explained along the way."
date: 2026-09-01
series: gin-gorm
order: 1
tags:
  - Go
  - PostgreSQL
  - ORM
---

## Before You Start

For readers who know basic Go syntax and want to get a GORM project running from scratch. Three things to confirm first:

- **Go 1.22+** (this tutorial uses `errors.Is`; 1.24+ recommended)
- **A local PostgreSQL** (or Docker: `docker run --name pg -e POSTGRES_PASSWORD=123456 -p 5432:5432 -d postgres:16`)
- **Create the database by hand**: `CREATE DATABASE library;` — `AutoMigrate` can create tables, not databases

MySQL / SQLite work too: the code is portable, only the driver and DSN change (see Part 1). Note that **from Part 2 on the series uses PostgreSQL-specific features** (`ILIKE`, functional dependency when grouping by primary key, partial unique indexes); differences are flagged where they appear.

The project layout starts flat and evolves later (see Part 6):

```text
gin-demo/
├── main.go           # entry point
├── db/
│   └── db.go         # database connection
├── models/
│   └── book.go       # data model
├── handlers/
│   └── book.go       # business logic (CRUD)
└── go.mod
```

> **Why flat?** Three packages (`db` / `models` / `handlers`) are enough to get running and easy to map chapter by chapter. Growing into `internal/` + a Service / Repository split is [Engineering (Part 1)](./gorm-gin-engineering-layering)'s job.

## Build a Mental Model First

GORM's odd one out isn't Spring/Django — it's the **hand-written SQL** camp (JDBC/MyBatis): there you assemble SQL and map result rows into objects by hand, and GORM is the opposite.

> **One sentence:** Gin turns an HTTP request into a Go function call; GORM translates a Go struct into database SQL. The whole article follows one line: **request in → fill the struct → hand it to GORM → fill the result back → return JSON**.

### You're Not Writing SQL — You're Operating on Structs

You describe **intent** (`Create`, `Find`, `Updates`, `Delete`); translating it into SQL is the framework's job. Want to see what it generated? Turn on GORM logging.

### Four Mental Models You Must Build

1. **A struct plays three roles at once.** The same struct is the table definition (`gorm` tags), the request/response carrier (`json` tags), and the argument to database operations (`&book`). The type is the contract — change it in one place and everything follows.
2. **Querying is "filling in the blank", not "returning a value".** `Find(&books)` and `First(&book, id)` must receive a **pointer** to the destination; GORM fills it via reflection, and forgetting `&` fills a copy nobody can see. Same for `ShouldBindJSON(&x)`, `Delete(&models.Book{})`, `AutoMigrate(&models.Book{})` — to fill data in, or to act on the object itself, pass the address. Conversely, a **pointer type** like `*gorm.DB` / `*gin.Context` means "one shared instance, no copy", while `*string` / `*int` means "may not be sent" (`nil`).
3. **Zero value means "not provided".** `Updates(struct)` decides what to update by zero values (`0`, `""`, `false`), so setting a field to `0` is **silently skipped**. Part 7's zero-value trap is rooted here.
4. **Errors are part of the result.** There are no exceptions: every operation returns a `result`, and you check `result.Error` and `result.RowsAffected` yourself.

### Soft Delete: Another Example of GORM Rewriting SQL

Part 8's `Delete` never removes the row — GORM rewrites it into a soft delete, and deleted rows stop appearing in queries. The mechanics wait until Part 8; just remember: **never assume `Delete` emits a `DELETE` statement**.

This article stops at single-table CRUD. Comments and pagination, cover uploads, request DTOs, tag associations, layering, and reliability unfold in the following parts.

## Part 1: Project Setup

Create the directory and initialize the module:

```bash
mkdir gin-demo
cd gin-demo
go mod init gin-demo
```

Install the dependencies:

```bash
go get github.com/gin-gonic/gin
go get gorm.io/gorm
go get gorm.io/driver/postgres
```

> `go get` resolves the latest compatible version. The tutorial avoids `go get -u` — `-u` upgrades every indirect dependency too, adding uncertainty for no reason.

Switching databases means switching the driver and DSN:

| Database   | Driver                           | DSN                                                                           |
| ---------- | -------------------------------- | ----------------------------------------------------------------------------- |
| PostgreSQL | `go get gorm.io/driver/postgres` | `host=localhost user=postgres password=123456 dbname=library sslmode=disable` |
| MySQL      | `go get gorm.io/driver/mysql`    | `user:pass@tcp(localhost:3306)/library?charset=utf8mb4&parseTime=True`        |
| SQLite     | `go get gorm.io/driver/sqlite`   | `./data.db`                                                                   |

## Part 2: Connecting to the Database

At startup call `db.InitDB()`: build the DSN, open the connection with `gorm.Open`, and hand back a `*gorm.DB` other packages can use; exit on failure.

A DSN is one line of `key=value` pairs: `host` / `port` / `user` / `password` / `dbname`, plus `sslmode=disable` for local development (enable SSL in production).

Create `db/db.go`:

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
        log.Fatal("failed to connect to database: ", err)
    }
    log.Println("database connected")
}
```

> ⚠️ In production, manage sensitive config with environment variables — don't hardcode it. The companion repo accepts `DATABASE_DSN` to override the whole DSN, which is how its verification runs point at a scratch database.

> **Note:** PostgreSQL needs a **manually created database**; `AutoMigrate` creates tables but not databases; `sslmode=disable` is for local development only.

## Part 3: Defining the Data Model

Create `models/book.go` and describe the table with one struct:

```go
package models

import "gorm.io/gorm"

type Book struct {
    gorm.Model
    Title  string `json:"title" gorm:"not null"`
    Author string `json:"author" gorm:"not null"`
    Price  int    `json:"price" gorm:"not null;check:price >= 0"` // cents — 5990 means 59.90
}
```

- `gorm.Model` embeds `ID`, `CreatedAt`, `UpdatedAt`, and `DeletedAt`
- `gorm:"not null"` maps to the database's `NOT NULL` constraint
- `gorm:"check:price >= 0"` adds a database-level CHECK — negative values are rejected outright (validation can't live only in the DTO)
- `json:"title"` names the field in JSON serialization — the response body uses it too

**Three things newcomers most often miss:**

- **`gorm.Model` fields appear in JSON under their raw Go names.** It carries no `json` tags, so responses contain `"ID"` and `"DeletedAt": null`. To unify the naming, declare your own fields or use a DTO (see Part 10, "Next Steps").
- **Money is an `int`, in cents.** Floating point has precision loss (`0.1 + 0.2 != 0.3`), so `price: 5990` means 59.90; the frontend divides by 100 when it needs whole units.
- **`binding:"required"` goes on the DTO, not the model.** The model is reused by the update endpoint, and `required` would turn partial updates into 400s (you'll use this once in Part 5).

**Naming conventions:**

- Type names are singular (`Book`, the Go convention); GORM pluralizes the table name to `books` for you, so a plural type name buys nothing at the database level. File names are up to you — this tutorial uses `book.go`.
- The same field has three names in three layers: Go field (PascalCase), database column (snake_case, converted by GORM), JSON key (camelCase, via the `json:` tag). Rename a column with `gorm:"column:..."`, rename the JSON key with `json:` — two independent switches.
- To use a custom table name, add one line: `func (Book) TableName() string { return "my_books" }`.

## Part 4: Auto Migration

Create the table from `main.go` (no routes yet):

```go
func main() {
    db.InitDB()

    if err := db.DB.AutoMigrate(&models.Book{}); err != nil {
        log.Fatal("migration failed: ", err)
    }

    // ... server & routes in Part 9
}
```

- **It only adds.** It never drops columns and **never removes existing constraints** — delete a `not null` tag and the column stays `NOT NULL`; change an association's `OnDelete` behaviour and the existing foreign key stays as it was. Constraint changes need an explicit migration (`db.Migrator().DropConstraint()` or hand-written SQL)
- Changing a field's `size`, `precision`, or nullability makes GORM try to alter the column type
- Renaming a field adds a new column; use `db.Migrator().RenameColumn()` to actually rename

## The Formula First: The Uniform CRUD Flow

The five handlers in Parts 5–8 are instances of one flow. The six-step skeleton:

1. **Grab parameters**: `id := c.Param("id")` or `c.Query("keyword")`
2. **Bind the request**: `c.ShouldBindJSON(&xxx)`, failure means a straight 400
3. **Check existence**: `db.DB.First(&xxx, id)`; no record (`errors.Is` hits `gorm.ErrRecordNotFound`) → 404
4. **Do the operation**: `Create` / `Find` / `Updates` / `Delete` — each returns a `result`
5. **Check the result**: a non-"not found" `result.Error` → 500; `RowsAffected == 0` → 404
6. **Respond**: `c.JSON(200/201, ...)`

**Every part is an instance of the formula:**

| Operation           | Params | Bind | Existence     | Operation | Result check                          | Status |
| ------------------- | ------ | ---- | ------------- | --------- | ------------------------------------- | ------ |
| `POST /books`       | –      | ✓    | –             | `Create`  | `Error` → 500                         | 201    |
| `GET /books`        | –      | –    | –             | `Find`    | `Error` → 500                         | 200    |
| `GET /books/:id`    | ✓      | –    | `First` → 404 | –         | `ErrRecordNotFound` → 404             | 200    |
| `PUT /books/:id`    | ✓      | ✓    | `First` → 404 | `Updates` | `Error` → 500                         | 200    |
| `DELETE /books/:id` | ✓      | –    | –             | `Delete`  | `Error` → 500; `RowsAffected`=0 → 404 | 200    |

Two things that differ most from other frameworks:

- **Gin has exactly one data entry point, `*gin.Context`**: path parameters, query parameters, and the request body all come from it — no controller classes, one function signature serves every request.
- **Every GORM step returns the same `result`**: `*gorm.DB` is both the chain carrier and the holder of `Error` / `RowsAffected` — that is mental model 4 landing at the API layer.

Also, every DB call in this article carries the request context: `db.DB.WithContext(c.Request.Context()).Xxx(...)` (why: Part 5).

## Part 5: Creating a Book

Parts 6–8 append their CRUD functions to the same file, `handlers/book.go`, whose header is:

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

The first endpoint, creating a book:

```go
// CreateBook creates a book (POST /books)
func CreateBook(c *gin.Context) {
    var book models.Book

    // 1. Bind the JSON request body
    if err := c.ShouldBindJSON(&book); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
        return
    }

    // 2. Insert into the database
    result := db.DB.WithContext(c.Request.Context()).Create(&book)
    if result.Error != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create book"})
        return
    }

    // 3. Return the created data (this is "one struct, three roles": body in, DB in, out as-is)
    c.JSON(http.StatusCreated, book)
}
```

> ⚠️ Routes are only registered in Part 9. Jump ahead, copy the `main.go` there, get the server running, then come back and test Parts 5–8 chapter by chapter.

Testing:

```bash
curl -X POST http://localhost:8080/books \
  -H "Content-Type: application/json" \
  -d '{"title":"Go in Action","author":"Alice","price":5990}'
```

Response example (note the key casing):

```json
{
  "ID": 1,
  "CreatedAt": "2026-09-05T13:05:46+08:00",
  "UpdatedAt": "2026-09-05T13:05:46+08:00",
  "DeletedAt": null,
  "title": "Go in Action",
  "author": "Alice",
  "price": 5990
}
```

> `ID` / `CreatedAt` / `DeletedAt` come out capitalised because `gorm.Model` has no `json` tags (see Part 3); `title` / `author` / `price` are lowercase because we tagged them.

### Going further: adding validation

The code above validates nothing — `{"price":5990}` (no `title` / `author`) inserts fine. Put the validation tags on the **request struct**, not on `models.Book`:

**Step one: declare the request struct (DTO).** Keep it in `handlers/book.go`, just above `CreateBook` (lowercase is fine within the package; export it as `CreateBookInput` only if another package will ever reference it):

```go
// createBookInput: request-only DTO for creating a book; validation tags belong to it
type createBookInput struct {
    Title  string `json:"title" binding:"required"`
    Author string `json:"author" binding:"required"`
    Price  int    `json:"price" binding:"gte=0"`
}
```

**Why not put `binding:"required"` on `models.Book`?** The same model is reused by Part 7's update endpoint — updates are partial (only `{"price":6990}`), and `required` would turn them into 400s. So: **the model owns the schema and JSON naming; validation is an interface contract and belongs to the request struct (DTO)**. It lands for real in the [DTO article](./gorm-gin-dto-batch).

**Step two: make `CreateBook` actually bind it.** Declaring the DTO changes nothing on its own — if the binding target stays `models.Book`, requests missing `title` still insert fine. Replace the `CreateBook` from the top of Part 5 with this version:

```go
// CreateBook creates a book (POST /books): bind the DTO to validate, then translate into the model
func CreateBook(c *gin.Context) {
    // 1. Bind into the DTO — this is where the binding tags take effect, and missing fields 400 right here
    var input createBookInput
    if err := c.ShouldBindJSON(&input); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON (title and author are required, price must be >= 0)"})
        return
    }

    // 2. DTO → model: copy only the fields a client is allowed to decide.
    //    Fields like ID / CreatedAt / DeletedAt simply don't exist on the DTO, so they can't be smuggled in.
    book := models.Book{
        Title:  input.Title,
        Author: input.Author,
        Price:  input.Price,
    }

    // 3. Insert (note we pass book to Create, not input)
    result := db.DB.WithContext(c.Request.Context()).Create(&book)
    if result.Error != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "create failed"})
        return
    }

    // 4. Return the persisted model — same response shape as the top of Part 5, with ID/CreatedAt filled by GORM
    c.JSON(http.StatusCreated, book)
}
```

> **Why not just `Create(&input)`?** Because `input` is the DTO and `book` is the model: the model embeds `gorm.Model` (primary key, timestamps, soft delete) and will later grow columns the client has no say over, like `cover_path`. That DTO-to-model translation is a deliberate safety check — only fields you explicitly list inside `models.Book{...}` get persisted, while extra keys the client sends (say `"deletedAt"`) are dropped during binding. This is the "what you accept and what you store are two different things" rule the rest of the series keeps coming back to.

Only now does a request with missing fields take the 400 branch:

```bash
# Missing a required field → 400 (the original code returned 201 here)
curl -s -o /dev/null -w '%{http_code}\n' -X POST http://localhost:8080/books \
  -H "Content-Type: application/json" -d '{"price":5990}'

# Full payload → 201, same response as the example at the top of Part 5
curl -s -X POST http://localhost:8080/books \
  -H "Content-Type: application/json" -d '{"title":"Go in Action","author":"Zhang San","price":5990}'
```

### Going further: why you must carry the context

`WithContext(ctx)` hangs a Go `context.Context` on the query chain; when the query executes, it travels through `database/sql` → the driver and becomes the source of **cancellation and timeout signals**.

`c.Request.Context()` is tied to the request lifecycle: **when the client disconnects, it is cancelled**. Carry it and the query is interrupted too, returning the connection to the pool instead of hanging; the bare form (`db.DB.Create(&book)`) also runs, but is blind to the request's lifecycle.

That is why every DB call here is written as `db.DB.WithContext(c.Request.Context()).Xxx(...)`. The server-side timeout middleware that guards against slow queries lives in Part 9's `main.go`; when it expires, GORM returns `context.DeadlineExceeded`, which falls through to 500 — map it to 504 in production (see Part 7).

## Part 6: Querying Books

The two read endpoints:

```go
// GetBooks lists all books (GET /books)
func GetBooks(c *gin.Context) {
    books := []models.Book{} // empty slice, not nil: an empty table returns [] instead of null
    result := db.DB.WithContext(c.Request.Context()).Find(&books)
    if result.Error != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
        return
    }
    c.JSON(http.StatusOK, books)
}

// GetBook returns one book by ID (GET /books/:id)
func GetBook(c *gin.Context) {
    id := c.Param("id")

    var book models.Book
    result := db.DB.WithContext(c.Request.Context()).First(&book, id)
    if errors.Is(result.Error, gorm.ErrRecordNotFound) {
        c.JSON(http.StatusNotFound, gin.H{"error": "book not found"})
        return
    }
    if result.Error != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
        return
    }

    c.JSON(http.StatusOK, book)
}
```

Testing:

```bash
curl http://localhost:8080/books
curl http://localhost:8080/books/1
```

> **Why does `First` check errors in two layers?** "Not found" surfaces as `gorm.ErrRecordNotFound`; only when `errors.Is` matches it do we return 404, while errors like a dropped connection fall through to 500 instead of being misreported as "book not found". `GetBooks` uses `[]models.Book{}` rather than `var books []models.Book` because the latter serializes as `null` on an empty table — later parts' paginated lists keep that habit.

## Part 7: Updating a Book

```go
// UpdateBook updates a book (PUT /books/:id)
func UpdateBook(c *gin.Context) {
    id := c.Param("id")

    // 1. Check whether the book exists (the same two-layer check as Part 6's GetBook — write it yourself this time)
    var book models.Book
    result := db.DB.WithContext(c.Request.Context()).First(&book, id)
    if errors.Is(result.Error, gorm.ErrRecordNotFound) {
        c.JSON(http.StatusNotFound, gin.H{"error": "book not found"})
        return
    }
    if result.Error != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
        return
    }

    // 2. Bind the JSON request body
    var input models.Book
    if err := c.ShouldBindJSON(&input); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
        return
    }

    // 3. Update the fields (only non-zero fields of input; the WHERE condition comes from the First above)
    result = db.DB.WithContext(c.Request.Context()).Model(&book).Updates(input)
    if result.Error != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "update failed"})
        return
    }

    // 4. Return the updated data (GORM writes the updated non-zero fields back to book, matching the DB; zero-value fields keep the values loaded by First above)
    c.JSON(http.StatusOK, book)
}
```

### PUT or PATCH?

`PUT` strictly means "replace the whole resource"; partial updates belong to `PATCH`. `Updates` here only touches the non-zero fields sent in, so it is effectively PATCH semantics — `PUT` is fine for an introductory example; for stricter semantics, switch the route to `r.PATCH("/books/:id", handlers.UpdateBook)`.

### The zero-value trap

When `Updates` receives a **struct**, GORM ignores zero-value fields (`0`, `""`, `false`), so `{"price":0}` is silently skipped. That is by design, but it makes "set the price to 0" impossible.

The fix is a **pointer DTO**: `nil` means "not sent", `&0` means "sent as 0":

```go
type updateBookInput struct {
    Title  *string `json:"title"`
    Author *string `json:"author"`
    Price  *int    `json:"price" binding:"omitempty,gte=0,lte=1000000"`
}

var in updateBookInput
if err := c.ShouldBindJSON(&in); err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
    return
}
// only fields that are non-nil go into the update
fields := map[string]any{}
if in.Title != nil { fields["title"] = *in.Title }
if in.Author != nil { fields["author"] = *in.Author }
if in.Price != nil { fields["price"] = *in.Price }
db.DB.WithContext(c.Request.Context()).Model(book).Updates(fields)
```

> **⚠️ Don't use the model as the request body.** `var input models.Book` above hands the request straight to the model: JSON binding is case-insensitive, so `{"deletedAt":"2026-01-01T00:00:00Z"}` soft-deletes the book and `{"coverUrl":"/uploads/x.jpg"}` rewrites the cover without going through the upload endpoint — that is mass assignment. The fix is exactly the DTO above: **what you accept and what you store are two different things**. The main text keeps the model version so the zero-value trap stays visible; production code should use the pointer DTO (the [DTO article](./gorm-gin-dto-batch) lands it for real).
>
> You can also get away with `map[string]any` plus a key whitelist, but numbers inside a map become `float64` and every type guarantee is gone — not recommended.

Testing:

```bash
curl -X PUT http://localhost:8080/books/1 \
  -H "Content-Type: application/json" \
  -d '{"price":6990}'
```

## Part 8: Deleting a Book

Because `Book` embeds `gorm.Model`, GORM performs a **soft delete** by default: the row is never removed, only `deleted_at` is set, and queries filter it out from then on.

```go
// DeleteBook soft-deletes a book (DELETE /books/:id)
func DeleteBook(c *gin.Context) {
    id := c.Param("id")

    // Perform the soft delete
    result := db.DB.WithContext(c.Request.Context()).Delete(&models.Book{}, id)
    if result.Error != nil { // check Error before RowsAffected — order matters
        c.JSON(http.StatusInternalServerError, gin.H{"error": "delete failed"})
        return
    }
    if result.RowsAffected == 0 {
        c.JSON(http.StatusNotFound, gin.H{"error": "book not found"})
        return
    }

    c.JSON(http.StatusOK, gin.H{"message": "book deleted"})
}
```

### How soft delete behaves

What GORM actually runs is `UPDATE ... SET deleted_at=NOW() WHERE id=? AND deleted_at IS NULL`:

| Operation                   | GORM behaviour                                                                    |
| --------------------------- | --------------------------------------------------------------------------------- |
| First DELETE                | sets `deleted_at = NOW()`; the row stops appearing in queries                     |
| Deleting the same row again | `deleted_at IS NOT NULL` means no match, `RowsAffected == 0`, so "book not found" |

After a soft delete, `First` / `Find` add `deleted_at IS NULL` automatically; to read deleted rows use `db.DB.WithContext(c.Request.Context()).Unscoped().First(&book, id)`.

> **⚠️ The unique-index trap**: soft-deleted rows still occupy a unique index — re-creating a field with a unique index (say ISBN) after a soft delete violates the constraint. PostgreSQL's answer is a **partial unique index**: `CREATE UNIQUE INDEX ... WHERE deleted_at IS NULL`, so only live rows participate. Part 5 puts that to work on a real `tags.name`.

### Hard deletion (remove for real)

> This function is **optional**. Part 9's full `main.go` registers it; delete that line if you don't want to expose it.

```go
// DeleteBookPermanently hard-deletes a book (DELETE /books/:id/permanent)
func DeleteBookPermanently(c *gin.Context) {
    id := c.Param("id")
    // Unscoped() bypasses soft delete and removes the row for real
    result := db.DB.WithContext(c.Request.Context()).Unscoped().Delete(&models.Book{}, id)
    if result.Error != nil { // check Error before RowsAffected — order matters
        c.JSON(http.StatusInternalServerError, gin.H{"error": "delete failed"})
        return
    }
    if result.RowsAffected == 0 {
        c.JSON(http.StatusNotFound, gin.H{"error": "book not found"})
        return
    }
    c.JSON(http.StatusOK, gin.H{"message": "book permanently deleted"})
}
```

> **⚠️ It fails once child rows exist**: from Part 2 on, `Book` has a comments association, and the foreign key GORM creates defaults to `NO ACTION` — deleting a book that has comments is rejected by PostgreSQL (`violates foreign key constraint`). The fix is `constraint:OnDelete:CASCADE` on the association (Part 2's model already carries it), or delete the comments first in a transaction. Soft delete is unaffected.

Testing: `curl -X DELETE http://localhost:8080/books/1`.

## Part 9: Registering Routes

Wire everything together — the route section looks like this:

```go
r := gin.Default()
r.Use(requestTimeout(5 * time.Second)) // per-request timeout: slow queries are interrupted by the context

r.POST("/books", handlers.CreateBook)
r.GET("/books", handlers.GetBooks)
r.GET("/books/:id", handlers.GetBook)
r.PUT("/books/:id", handlers.UpdateBook) // strict REST semantics would use PATCH for partial updates
r.DELETE("/books/:id", handlers.DeleteBook)
r.DELETE("/books/:id/permanent", handlers.DeleteBookPermanently) // optional

r.Run(":8080")
```

`requestTimeout` is the "timeout fallback for every request" from Part 5: it derives a `context.WithTimeout` ctx and writes it back onto the request so downstream `WithContext` calls can see the deadline.

Full `main.go` (imports, database init, migration, middleware, and the routes above):

<details>
<summary>Full main.go (click to expand)</summary>

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

// Timeout fallback for every request (see Part 5, "Going further")
func requestTimeout(d time.Duration) gin.HandlerFunc {
    return func(c *gin.Context) {
        ctx, cancel := context.WithTimeout(c.Request.Context(), d)
        defer cancel()
        c.Request = c.Request.WithContext(ctx)
        c.Next()
    }
}

func main() {
    // Connect to the database
    db.InitDB()

    // Auto migrate
    if err := db.DB.AutoMigrate(&models.Book{}); err != nil {
        log.Fatal("migration failed: ", err)
    }

    r := gin.Default()
    // Per-request timeout: slow queries are interrupted by the context → 500 (map to 504 in production)
    r.Use(requestTimeout(5 * time.Second))

    // RESTful API routes
    r.POST("/books", handlers.CreateBook)
    r.GET("/books", handlers.GetBooks)
    r.GET("/books/:id", handlers.GetBook)
    r.PUT("/books/:id", handlers.UpdateBook) // strict REST semantics would use PATCH for partial updates
    r.DELETE("/books/:id", handlers.DeleteBook)
    // Optional: the hard-delete example from Part 8 (soft delete is enough by default)
    r.DELETE("/books/:id/permanent", handlers.DeleteBookPermanently)

    r.Run(":8080")
}
```

</details>

With the server up you can test every chapter with curl, including the hard delete: `curl -X DELETE http://localhost:8080/books/1/permanent`.

## Part 10: Summary

| Operation    | GORM call                                  | SQL emitted                                 |
| ------------ | ------------------------------------------ | ------------------------------------------- |
| Create       | `db.Create(&book)`                         | `INSERT INTO ...`                           |
| List all     | `db.Find(&books)`                          | `SELECT * FROM ...`                         |
| Get one      | `db.First(&book, id)`                      | `SELECT * FROM ... WHERE id = ?`            |
| Update       | `db.Model(&book).Updates(input)`           | `UPDATE ... SET ...`                        |
| Soft delete  | `db.Delete(&models.Book{}, id)`            | `UPDATE ... SET deleted_at = NOW()`         |
| Hard delete  | `db.Unscoped().Delete(&models.Book{}, id)` | `DELETE FROM ...`                           |
| Read deleted | `db.Unscoped().First(&models.Book{}, id)`  | `SELECT * FROM ... WHERE id = ?` (unscoped) |

> All five handlers are instances of the six-step skeleton from "The Formula First": parameters → binding → existence → operation → result → response; every DB call carries `WithContext(c.Request.Context())`, and queries use `errors.Is` to tell "not found" (404) from other errors (500).

### Next Steps

- Transactions: `db.Transaction()`
- Hooks: `BeforeCreate`, `AfterUpdate`
- Slimmer responses: how to unify `gorm.Model`'s capitalised keys — declare your own fields, or use a DTO as the response shape
- Connection pool: grab the underlying `*sql.DB` with `sqlDB, _ := db.DB.DB()`, then tune `SetMaxOpenConns()` / `SetConnMaxLifetime()`
- Timeout mapping: return 504 when `errors.Is(err, context.DeadlineExceeded)`, instead of a blanket 500
- SQL debugging: `&gorm.Config{Logger: logger.Default.LogMode(logger.Info)}`
