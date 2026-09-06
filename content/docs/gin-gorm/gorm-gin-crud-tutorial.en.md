---
title: "GORM Crash Course: Building a Book Management API with Gin + GORM"
description: "Build a complete book-management API from scratch — GORM CRUD, soft delete, the zero-value trap, request contexts, and error handling — with the full project code and test commands."
date: 2026-09-01
series: gin-gorm
tags:
  - Go
  - PostgreSQL
  - ORM
---

## Who This Is For

- You already know Go basics
- You want to learn GORM but don't know where to start
- You want a complete, runnable project to follow along
- Experience in another web stack (Java/Python/Node) helps — this post compares common framework conventions as we go

## Environment Setup

Three things to check before you start:

- **Go 1.22+** (this post uses `errors.Is`; generics appear only in the series' engineering articles — anything 1.21+ works, 1.24+ recommended);
- **A local PostgreSQL** (no install? use Docker: `docker run --name pg -e POSTGRES_PASSWORD=123456 -p 5432:5432 -d postgres:16`);
- **Create the database manually**: `CREATE DATABASE library;` — GORM's `AutoMigrate` only creates tables, never databases (see the note in Part 2).

MySQL / SQLite work too — see the driver table at the end of Part 1.

## Project Layout

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

> **Learning-grade structure:** a flat `db` / `models` / `handlers` layout is fine for small projects and for learning. As the business grows, evolve it by responsibility — use a Go `internal/` package to constrain visibility, extract a Service layer for business logic, and a Repository layer for data access. This post stays flat to keep the focus on GORM: get it running first, talk about layering later. When you want to unit-test handlers with a mocked database, collect data access behind an interface (e.g. `BookRepository`) and inject it (see Part 10, "Next Steps").

## Build a Mental Model First: The Core Ideas of GORM

Before writing any code, take thirty seconds to build the right **mental model** — understanding the way of thinking below matters more than copying code, because this is exactly where Gin + GORM differs most from other languages and frameworks (Java/Spring, Python/Django, Node/Express, and especially hand-written-SQL stacks like JDBC/MyBatis).

### The One-Sentence Overview

> Gin turns an **HTTP request** into a **Go function call**; GORM translates **Go structs** into **database SQL**. The entire post runs on one storyline: **request comes in → loaded into a struct → handed to GORM → results filled back into the struct → returned as JSON**.

### You're Not Writing SQL — You're Operating on Structs

With Java's JDBC/MyBatis or hand-written PHP PDO, you assemble SQL strings yourself and manually map result rows into objects. GORM flips that: you only describe your **intent** (`Create`, `Find`, `Updates`, `Delete`), and translating it to SQL is the framework's job:

| GORM method (excerpt)            | Equivalent SQL                                          |
| -------------------------------- | ------------------------------------------------------- |
| `db.Create(&book)`               | `INSERT INTO books ...`                                 |
| `db.Model(&book).Updates(input)` | `UPDATE books SET ...`                                  |
| `db.Delete(&models.Book{}, id)`  | `UPDATE books SET deleted_at = NOW() ...` (soft delete) |

Want to see the SQL GORM actually generates? Turn on GORM logging (see Part 10). Write with **intent**, don't try to hand-translate every query in your head; the complete method ↔ SQL map is in the Part 10 summary.

### Four Mental Models You Must Build

1. **A struct plays three roles at once.** The same struct is the database table definition (`gorm` tags), the request/response data carrier (`json` tags), and the argument to database operations (`&book`). The type is the contract — change it in one place and everything follows. This is nothing like Java's Entity/DTO split plus a separate XML mapping layer.
2. **Querying is "filling in the blank", not "returning a value".** Go passes by value, so `Find(&books)` and `First(&book, id)` must receive a **pointer** to the destination variable — GORM fills it via reflection. Forget the `&` and you've filled a copy that no one outside the call can see. This is the most counter-intuitive spot for newcomers, because Java/Python object references are inherently shared.
3. **Zero value means "not provided".** Go gives every variable a zero value (`0`, `""`, `false`). GORM's `Updates(struct)` uses exactly that to decide "should this field be updated" — which is why updating a field to `0` or `""` is silently skipped (no error, no update). The "zero-value trap" in Part 7 is rooted right here. Java's `null` and Python's `None` have no such "zero value participates in business logic" semantics — that's what makes GORM's behavior most surprising to newcomers.
4. **Errors are part of the result.** Go has no exceptions. GORM packages the outcome of every operation into a `result`, and you check `result.Error` and `result.RowsAffected` yourself. You'll see this pattern over and over in this post — it's what replaces `try/catch` in other languages.

### A Note: Why All the `&` and `*`?

This trips newcomers up most — GORM and Gin code is full of `&` and `*`, and nobody explains why. Each has one job:

- **`&x` (taking an address) = "this variable is yours to fill; bring the changes back"**. Go passes by value, so what goes down is a copy. GORM's `First(&book, id)`, `Find(&books)` and Gin's `c.ShouldBindJSON(&book)` need to **fill** data in; `Delete(&models.Book{}, id)` and `AutoMigrate(&models.Book{})` need the **object itself** — without `&`, nothing comes back out of the call (this is mental model #2 "filling in, not returning" generalised: it applies to binding, deletion, and migration, not just queries).
- **`*gorm.DB` / `*gin.Context` (pointer type declarations) = "reference the one instance, don't copy it"**. These types are declared as pointers: `db.DB` is the database connection handle, `*gin.Context` is the request context — there is exactly one of each, and passing around the **address** avoids copies while guaranteeing you operate on the same instance.
- **`*string` / `*int` (pointer fields in DTOs) = "may not have been sent"**. `nil` means "this field never appeared", distinct from empty string / zero — Part 7 itself uses a struct and a map; the full pointer-DTO rollout is in the series' DTO article and in the Next Steps section.

One sentence: **`&` means "fill in here", `*` means "this is a reference / may be absent"** — not GORM magic, just Go's value-vs-reference semantics, the same in every framework.

### Soft Delete: Another Example of GORM Rewriting SQL

`Delete` in Part 8 doesn't really delete data — GORM rewrites it into a "soft delete", and deleted records stop showing up in queries. The detailed mechanics are left for Part 8. For now, remember: **don't assume `Delete` always generates a `DELETE` statement** (the "(soft delete)" hint in the table above is the foreshadowing).

### How the Chapters Fit Together

- Parts 2–4 (connection, model, migration) — the "struct ↔ table" foundation;
- Parts 5–8 (CRUD) — putting the four mental models into practice;
- Parts 9–10 (routing, summary) — wiring it all together and looking back.

Now go into Part 1 with this mindset, and verify as you type.

## Part 1: Project Setup

### Goal: Create the project directory and install dependencies

```bash
mkdir gin-demo
cd gin-demo
go mod init gin-demo
```

### Installing dependencies

```bash
# Web framework
go get github.com/gin-gonic/gin

# ORM + PostgreSQL driver
go get gorm.io/gorm
go get gorm.io/driver/postgres
```

> `go get` resolves the latest compatible version for your environment (same as `@latest`). This post avoids `go get -u` — the `-u` flag also upgrades every indirect dependency, which adds needless churn.

### Database driver options

This post uses PostgreSQL as the example database. Using something else? Swap in the matching driver:

| Database   | Install command                  | DSN format                                                                    |
| ---------- | -------------------------------- | ----------------------------------------------------------------------------- |
| PostgreSQL | `go get gorm.io/driver/postgres` | `host=localhost user=postgres password=123456 dbname=library sslmode=disable` |
| MySQL      | `go get gorm.io/driver/mysql`    | `user:pass@tcp(localhost:3306)/library?charset=utf8mb4&parseTime=True`        |
| SQLite     | `go get gorm.io/driver/sqlite`   | `./data.db`                                                                   |

## Part 2: Connecting to the Database

### Goal: Establish the database connection and initialize it at startup

### Connection flow

1. `main.go` calls `db.InitDB()` at startup
2. `db.InitDB()` builds the DSN string and connects via `gorm.Open()`
3. On success it returns a `*gorm.DB` instance; on failure the program exits

---

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

> ⚠️ In production, manage sensitive config with environment variables (`os.Getenv` or `godotenv`) — don't hardcode it.

### Code walkthrough

| Code                                            | What it does                                               |
| ----------------------------------------------- | ---------------------------------------------------------- |
| `var DB *gorm.DB`                               | Declares the global DB handle for other packages           |
| `gorm.Open(postgres.Open(dsn), &gorm.Config{})` | Opens the database connection                              |
| `log.Fatal`                                     | Terminates the program on failure, so no further code runs |

### DSN parameters

| Parameter  | Example     | Meaning                   |
| ---------- | ----------- | ------------------------- |
| `host`     | `localhost` | Database host             |
| `port`     | `5432`      | PostgreSQL default port   |
| `user`     | `postgres`  | Database user             |
| `password` | `123456`    | Database password         |
| `dbname`   | `library`   | Database name             |
| `sslmode`  | `disable`   | Disable SSL for local dev |

> **Notes:**
>
> - PostgreSQL requires **creating the database manually**: `CREATE DATABASE library;`
> - GORM's `AutoMigrate` can create tables, but it **cannot create the database itself**
> - `sslmode=disable` is for local development only — enable SSL in production

## Part 3: Defining the Data Model

### Goal: Define the table structure with a Go struct

Create `models/book.go`:

```go
package models

import "gorm.io/gorm"

type Book struct {
    gorm.Model
    Title  string  `json:"title" gorm:"not null"`
    Author string  `json:"author" gorm:"not null"`
    Price  int     `json:"price"` // cents — 5990 means 59.90
}
```

### Field notes

- `gorm.Model` embeds `ID`, `CreatedAt`, `UpdatedAt`, and `DeletedAt`
- `gorm:"not null"` maps to the database's `NOT NULL` constraint
- `json:"title"` names the field in JSON serialization — the response body uses it too

> **Three things newcomers most often miss:**
>
> - **`gorm.Model` fields appear in JSON under their raw Go names.** `gorm.Model` carries no `json` tags, so responses contain `"ID"`, `"CreatedAt"`, and `"DeletedAt": null` — capitalised field names. To hide or rename them, declare your own fields instead of embedding `gorm.Model` (add `json:"-"` or lowercase tags), or use a dedicated DTO (data transfer object) as the response shape.
> - **Money is an `int`, in cents.** Floating point has precision loss (`0.1 + 0.2 != 0.3`), so amounts are stored as integers in cents — in the API, `price: 5990` means 59.90. The frontend divides by 100 when it needs whole units (or you provide a custom `MarshalJSON`); reach for a `decimal` library only for rates / fees that need exact decimals (see Part 10, "Next Steps").
> - **`binding:"required"` gives you automatic validation (on the DTO, not the model).** Add the tag to a request struct's fields and Gin validates during `ShouldBindJSON` — a missing required field returns 400 directly. You'll use it in Part 5, "Going further".

### Naming conventions (important)

- **Struct type names are singular:** `Book`, not `Books`. That's the Go convention (think `http.Server`, `time.Time`), and it reads better — one struct instance is one record. Writing `Books` won't change the table name; it will just clash with singular function names like `CreateBook` and `GetBook`.
- **GORM pluralizes the table name for you:** type `Book` maps to table `books` (handled by the inflection library); even `Books` maps to `books`. A plural type name buys you nothing at the database level.
- **File names can be plural, type names must be singular:** both `book.go` and `books.go` are fine (plural file names are common when a file groups several related types); this post consistently uses `book.go`.
- Want a custom table name? Use a `TableName()` method:

```go
func (Book) TableName() string { return "my_books" }
```

**Three naming layers: Go field, DB column, and JSON each speak their own language**

The same logical field has a different name in each layer — nothing conflicts, because GORM (DB columns) and `json:` tags (JSON keys) do the translating:

| Layer       | Name                   | Decided by                                                                   |
| ----------- | ---------------------- | ---------------------------------------------------------------------------- |
| Go field    | `BookID` (PascalCase)  | Go identifier convention                                                     |
| DB column   | `book_id` (snake_case) | GORM's `NamingStrategy`, derived automatically — usually no `column:` needed |
| JSON output | `bookId` (camelCase)   | The `json:"bookId"` tag; serialization only                                  |

Column names and JSON keys are independent: the database always sees `book_id`, the frontend always sees `bookId`. Want a different column name? Use `gorm:"column:..."`. Want a different JSON key? Use a `json:` tag — two switches, each in charge of its own layer.

> **And when there is no `json:` tag?** The table above covers fields with tags. Tagless fields — like `gorm.Model`'s embedded `ID` and `CreatedAt` — are **serialized under their raw Go names**. That's why your first `curl` shows capitalised keys like `"ID"` and `"DeletedAt"` (see Part 3, "Three things newcomers most often miss").

## Part 4: Auto Migration

### Goal: Create or update the schema automatically at startup

Add this to `main.go`:

```go
package main

import (
    "log"
    "gin-demo/db"
    "gin-demo/models"
    "github.com/gin-gonic/gin"
)

func main() {
    // 1. Connect to the database
    db.InitDB()

    // 2. Auto migrate (create tables)
    if err := db.DB.AutoMigrate(&models.Book{}); err != nil {
        log.Fatal("migration failed: ", err)
    }

    // 3. Start the Gin server
    r := gin.Default()
    // ... routes
    r.Run(":8080")
}
```

> This `main.go` is the skeleton version — Part 9 will show the final, complete version with all routes registered.

### Notes

- `AutoMigrate` creates missing tables, columns, and indexes, but it will **not delete existing fields** (protecting your data)
- When a field's `size`, `precision`, or nullability changes, GORM will **attempt to alter the existing column type**
- Renaming a field does **not** rename the column: rename `Title` to `Name` and GORM adds a new column instead of renaming. Handle that with a manual migration or `db.Migrator().RenameColumn()`

## The Formula First: The Uniform CRUD Flow

The five handlers in Parts 5–8 look different on the surface, but every one of them is an **instance of the same flow**. See the formula first, then the code — same as the mental model: get the map before entering the maze.

**The six-step skeleton:**

1. **Read the parameter:** `id := c.Param("id")` or `c.Query("keyword")` — one of Gin's data entry points
2. **Bind the body:** `c.ShouldBindJSON(&xxx)`, return 400 on failure
3. **Check existence:** `db.DB.First(&xxx, id)`; if there's no record (`errors.Is` hits `gorm.ErrRecordNotFound`) → 404
4. **Run the operation:** `Create` / `Find` / `Updates` / `Delete` — every operation returns a `result`
5. **Check the result:** a `result.Error` that isn't "record not found" → 500; `RowsAffected == 0` → 404
6. **Send the response:** `c.JSON(200/201, ...)`

**Each chapter is one instance of the formula:**

| Operation           | Read param | Bind body | Check exists  | Run op    | Check result                          | Status |
| ------------------- | ---------- | --------- | ------------- | --------- | ------------------------------------- | ------ |
| `POST /books`       | –          | ✓         | –             | `Create`  | `Error` → 500                         | 201    |
| `GET /books`        | –          | –         | –             | `Find`    | `Error` → 500                         | 200    |
| `GET /books/:id`    | ✓          | –         | `First` → 404 | –         | `ErrRecordNotFound` → 404             | 200    |
| `PUT /books/:id`    | ✓          | ✓         | `First` → 404 | `Updates` | `Error` → 500                         | 200    |
| `DELETE /books/:id` | ✓          | –         | –             | `Delete`  | `Error` → 500; `RowsAffected`=0 → 404 | 200    |

**Gin + GORM specifics (where it differs most from other frameworks):**

- **Gin has a single data entry point: `*gin.Context`.** Path parameters (`c.Param`), query parameters (`c.Query`), and the request body (`c.ShouldBindJSON`) all come from it — no controller classes, no dependency injection, one function signature for every request.
- **Every GORM step returns the same `result`.** `*gorm.DB` is both the receiver for method chaining and the carrier of `Error` / `RowsAffected` — the "check the result" step is mental model #4 (errors are part of the result) in its API shape.
- **The pointer divide:** to fill data into a variable you must pass `&` (`Create(&book)`, `First(&book, id)`); deletions don't need data filled in, so pass the type (`Delete(&models.Book{}, id)`).
- **Status codes are a convention:** 400 parameter problem / 404 not found / 500 server error / 201 created — the whole post uses this mapping.
- **Production convention: chain the request context onto every db call:** `db.WithContext(c.Request.Context()).Xxx(...)` — the mechanism and the timeout middleware are covered in Part 5, "Going further".

Now on to Part 5 — watch how `CreateBook` instantiates steps 2, 4, 5, and 6 of the formula.

## Part 5: Creating a Book

### Goal: Implement the `POST /books` endpoint — accept JSON and store it

> **Note:** All CRUD functions in Parts 6–8 are appended to the same file, `handlers/book.go`. It starts with:

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

### Creating a book

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

> ⚠️ **When to test:** routes are registered only in Part 9 — the `main.go` you have right now is a skeleton (`// ... routes`). Jump ahead to Part 9, copy its final `main.go` and run the server, then come back and test the endpoints of Parts 5–8 (write the handler code in order as you go).

**Testing:**

```bash
curl -X POST http://localhost:8080/books \
  -H "Content-Type: application/json" \
  -d '{"title":"Go in Action","author":"Alice","price":5990}'
```

**Response example (note the key casing):**

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

> `ID` / `CreatedAt` / `DeletedAt` come out capitalised — `gorm.Model` carries no `json` tags, so the raw Go field names are used (see Part 3, "Three things newcomers most often miss"); `title` / `author` / `price` are lowercase because we tagged them ourselves. To make everything lowercase (`id`, `createdAt`), declare your own fields or use a DTO (see Part 10, "Next Steps" — slim responses).

### Going further: why you must carry the context

There's an easy-to-miss production detail in the code above: every database call chains `WithContext(c.Request.Context())`. Let's unpack what it is, why it's required, and how to configure a timeout.

**What is `WithContext`?**

`WithContext(ctx)` is one link in GORM's chainable API: it attaches Go's `context.Context` to the current query chain. When the SQL executes, that context flows through `database/sql` → the database driver (pgx under the hood for Postgres) and becomes the **source of cancel and timeout signals**. It's just a chain method like `Where` / `Order` — the query runs fine without it:

```go
// Without it: the query runs, but is blind to whether the request has ended
db.DB.First(&book, id)

// With it: cancel / timeout signals reach the driver layer
db.DB.WithContext(c.Request.Context()).First(&book, id)
```

**Why you must carry it?**

`c.Request.Context()` is tied to the request lifecycle: **when the client disconnects, it gets cancelled**. Carry it and the query is interrupted too — the connection returns to the pool instead of hanging. The bare form (e.g. `db.DB.Create(&book)`) also runs, but the query has no awareness of the request's lifecycle — which is why, from Part 5 on, the main code uniformly uses the context-carrying form (`Create` / `Find` / `Updates` / `Delete` alike).

**The related config: a per-request timeout middleware**

Client disconnects are cancelled automatically by the framework, but the server also needs protection against slow queries tying up connections — a middleware that attaches a timeout fallback to every request (the function lives in `main.go`; the full version in Part 9 registers it):

```go
// The full function and its registration live in Part 9's main.go; the core is four lines:
ctx, cancel := context.WithTimeout(c.Request.Context(), d)
defer cancel()                         // required: releases the timer, prevents leaks
c.Request = c.Request.WithContext(ctx) // required: without writing it back, the downstream WithContext never sees the timeout
c.Next()
```

On timeout, GORM returns `context.DeadlineExceeded`, which falls into the existing 500 branch (production can map it to 504 — see Part 10, "Next Steps").

### Going further: adding validation

The code above doesn't validate the request body — `{"price":5990}` (no `title` / `author`) inserts fine. Where do validation tags go? — **on the structure that accepts the request, not on `models.Book`**:

```go
// createBookInput: request-only DTO for creating a book; validation tags belong to it
type createBookInput struct {
    Title  string `json:"title" binding:"required"`
    Author string `json:"author" binding:"required"`
    Price  int    `json:"price" binding:"gte=0"`
}
```

**Why not put `binding:"required"` on `models.Book`?** The same model is reused by the update endpoint in Part 7 — updates are partial (only `{"price":6990}`, see the zero-value trap), and `required` on the model would make that request fail with 400. So: **the model owns the database schema and JSON naming (`gorm` / `json` tags); validation is an interface contract and belongs to the request structure (DTO)** — the series' dedicated DTO article covers them properly.

Point `CreateBook`'s binding target at this struct, then map it to the model:

```go
var input createBookInput
if err := c.ShouldBindJSON(&input); err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
    return
}
book := models.Book{Title: input.Title, Author: input.Author, Price: input.Price}
```

With the validation in place, a body missing a required field fails `ShouldBindJSON` with a validation error, going straight to the existing 400 branch:

```bash
# Missing a required field → 400
curl -X POST http://localhost:8080/books \
  -H "Content-Type: application/json" \
  -d '{"price":5990}'
```

## Part 6: Querying Books

### Goal: Implement the list and single-get endpoints

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

**Testing:**

```bash
# List all
curl http://localhost:8080/books

# Get one
curl http://localhost:8080/books/1
```

> **Note:** Why does `First` check errors in two layers? "Not found" surfaces as `gorm.ErrRecordNotFound`; only when `errors.Is` matches it do we return 404 ("book not found"). Any other error — like the database connection dropping — falls through to 500 instead of being misreported as "book not found". That's mental model #4 in action: an error is data too, and worth treating seriously with `errors.Is`.
>
> (Why does `GetBooks` initialize `books := []models.Book{}` instead of `var books []models.Book`? When `Find` matches nothing it never allocates the slice, so a `var`-declared slice stays nil and serializes as JSON `null`. Initializing an empty slice is the standard move for list endpoints; the paginated lists in later articles keep that habit.)

## Part 7: Updating a Book

### Goal: Implement the `PUT /books/:id` endpoint

```go
// UpdateBook updates a book (PUT /books/:id)
func UpdateBook(c *gin.Context) {
    id := c.Param("id")

    // 1. Check whether the book exists
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

Strictly speaking, REST's `PUT /books/:id` means "replace the whole resource"; partial updates should use `PATCH /books/:id`. The `Updates` in this chapter only touches the non-zero fields sent in — it's effectively a partial update (PATCH semantics). Using `PUT` is fine for an introductory example, but you should know the difference — if you want stricter semantics, switch the route to `r.PATCH("/books/:id", handlers.UpdateBook)` and the test command to `curl -X PATCH ...`.

### The zero-value trap and going further

When `Updates` receives a **struct**, GORM ignores zero-value fields by default (`0`, `""`, `false`, ...). That's by design and covers 90% of real business cases. But when you genuinely need to set a field to `0` or `""`, there are two approaches:

#### Approach 1: Pin the field with `Select`

```go
db.DB.WithContext(c.Request.Context()).Model(&book).Select("Price").Updates(input)
```

#### Approach 2: Use `map[string]interface{}` (more general, recommended)

```go
// The frontend sends only the fields to update; zero values update fine
var inputMap map[string]interface{}
if err := c.ShouldBindJSON(&inputMap); err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
    return
}
db.DB.WithContext(c.Request.Context()).Model(&book).Updates(inputMap)
```

The advantage of approach 2: whatever the frontend sends gets updated, with no surprises from zero values — and it's more flexible when there are many fields.

> **The cost of approach 2 (two gotchas):**
>
> - **JSON numbers become `float64`**: when `ShouldBindJSON` decodes into `map[string]interface{}`, every numeric value is `float64` (`{"price":6990}` → `float64(6990)`). Verified against pgx + PostgreSQL: a float64 can update an integer column — whole numbers are fine, but a non-integral value like `6990.5` does **not** error out; it is silently rounded to `6990` (data changes without a sound). The point: **values in the map have lost Go's type guarantees**, another reason for the whitelist below;
> - **Every key in the request ends up in the UPDATE SET**: a key that happens to match a model field (`ID`, `DeletedAt`) is resolved by GORM to the `id` / `deleted_at` columns and genuinely lands in the SET clause — worst case, the WHERE matches and you **silently rewrite the primary key or the soft-delete timestamp**; only keys that resolve to no field (like `Comments`) go in raw and produce "column does not exist".
>
> So pair the map approach with a key whitelist (it blocks both stray keys and the type/PK hazards above); the more type-safe alternative is a pointer-field DTO (`*string` / `*int`) — see "Request DTOs & pointer fields" in the Next Steps section and the series' DTO article.

**Testing:**

```bash
curl -X PUT http://localhost:8080/books/1 \
  -H "Content-Type: application/json" \
  -d '{"price":6990}'
```

## Part 8: Deleting a Book

### Goal: Implement the `DELETE /books/:id` endpoint

Because `Book` embeds `gorm.Model`, GORM runs a **soft delete** by default. The record isn't actually removed — `deleted_at` is set to the current time and the record is filtered out of queries.

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

| Operation       | What GORM does                                                                                 |
| --------------- | ---------------------------------------------------------------------------------------------- |
| First DELETE    | Sets `deleted_at = NOW()`; the record stops appearing in queries                               |
| DELETE it again | Because `deleted_at IS NOT NULL`, GORM finds no record; `RowsAffected == 0` → "book not found" |

Note: GORM executes `UPDATE ... SET deleted_at=NOW() WHERE id=? AND deleted_at IS NULL`; when the condition doesn't match, `RowsAffected` is 0 — it won't soft-delete twice.

> **Note:** After a soft delete, the default `First` / `Find` queries automatically add `deleted_at IS NULL`, so soft-deleted records never show up in lists.
>
> **⚠️ The unique-constraint trap:** if a field has a unique index (like `ISBN` — assuming you added that field and its index yourself; the `Book` in this post has only `Title` / `Author` / `Price`), a soft-deleted record still occupies the index — inserting the same ISBN again violates the constraint. The PostgreSQL fix is a **partial unique index**, so only non-deleted rows participate in uniqueness:

```sql
CREATE UNIQUE INDEX idx_books_isbn ON books (isbn) WHERE deleted_at IS NULL;
```

### Querying soft-deleted records

```go
db.DB.WithContext(c.Request.Context()).Unscoped().First(&book, id)
```

### Hard deletion (remove for real)

> This function is **optional**. Part 9's final `main.go` registers it by default (marked "optional"); remove that line if you don't want to expose hard deletion.

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

**Testing:**

```bash
curl -X DELETE http://localhost:8080/books/1
```

## Part 9: Registering Routes

### Goal: Register every route on the Gin engine

Update `main.go`:

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

### Testing (the hard-delete example)

```bash
curl -X DELETE http://localhost:8080/books/1/permanent
```

> Once it's running, the hard-delete endpoint from Part 8 is reachable through the route above, alongside the curl tests.

## Part 10: Summary

### What You Learned

| Operation     | GORM method                                | Equivalent SQL                                         |
| ------------- | ------------------------------------------ | ------------------------------------------------------ |
| Create        | `db.Create(&book)`                         | `INSERT INTO ...`                                      |
| List all      | `db.Find(&books)`                          | `SELECT * FROM ...`                                    |
| Get one       | `db.First(&book, id)`                      | `SELECT * FROM ... WHERE id = ?`                       |
| Update        | `db.Model(&book).Updates(input)`           | `UPDATE ... SET ...`                                   |
| Soft delete   | `db.Delete(&models.Book{}, id)`            | `UPDATE ... SET deleted_at = NOW()`                    |
| Hard delete   | `db.Unscoped().Delete(&models.Book{}, id)` | `DELETE FROM ...`                                      |
| Query deleted | `db.Unscoped().First(&models.Book{}, id)`  | `SELECT * FROM ... WHERE id = ?` (ignores soft delete) |

> Every handler in this post is an instance of the six-step skeleton from "The Formula First": parameter → bind → existence → operation → result → response; every db operation chains `WithContext(c.Request.Context())`, and queries use `errors.Is` to tell "record not found" (404) apart from other errors (500).

### Next Steps

- Transactions: `db.Transaction()`
- Pagination & filtering: `Where` / `Order` / `Offset` / `Limit` are covered hands-on in [GORM Media & Query Enhancement](./gorm-gin-media-query)
- Hooks: `BeforeCreate`, `AfterUpdate`
- Request DTOs & pointer fields: replacing `map[string]interface{}` with `*string` / `*int` is implemented in [GORM Data Engineering](./gorm-gin-dto-batch) — `Price *int` is exactly this
- Slim responses: `gorm.Model` fields carry no `json` tags, so responses show the capitalised Go names (`ID` / `CreatedAt` / `DeletedAt`); to unify the output style, declare your own fields (`json:"-"` / lowercase tags) or use a DTO as the unified response shape
- Connection pool: `sqlDB, _ := db.DB.DB()` then configure with `SetMaxOpenConns()` / `SetConnMaxLifetime()`
- Engineering pay-off (P4 optional): Repository/Service layering & tests (`BookRepository` + sqlmock) and a generic `GetPaginated[T]` wrapper — landed in [GORM Engineering in Practice (Part 1)](./gorm-gin-engineering-layering) and [GORM Engineering in Practice (Part 2)](./gorm-gin-engineering-reliability)
- Timeout mapping: return 504 on `errors.Is(err, context.DeadlineExceeded)` instead of a blanket 500
- One-line timeout: to write less code you can plug in gin-contrib/timeout via `timeout.New(...)`, but it turns the ctx derivation / write-back mechanics into a black box — this post keeps the hand-written version so you can see the mechanism
- SQL debugging: enable GORM logging with `&gorm.Config{Logger: logger.Default.LogMode(logger.Info)}` to inspect SQL
- Unified error handling: use an error middleware / a unified response wrapper (ok/fail shape) to remove the repeated 500 boilerplate — the main code keeps the explicit checks so you can see how `errors.Is` distinguishes error types

> **Advanced content lives in the series follow-ups:** relationships (`Preload`) and aggregate queries get their full treatment in the following series posts: [GORM Relations in Practice](./gorm-gin-relations) and [GORM Media & Query Enhancement](./gorm-gin-media-query); Repository / Service layering and testability land in [GORM Engineering in Practice (Part 1)](./gorm-gin-engineering-layering) and [GORM Engineering in Practice (Part 2)](./gorm-gin-engineering-reliability) — this post stays flat and direct, focused on GORM itself.
