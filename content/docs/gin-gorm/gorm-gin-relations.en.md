---
title: "GORM Relations in Practice: Comment Model, CRUD & Preload"
description: "On top of the crash course's single-table CRUD, this part introduces a second table `comments` (one-to-many): the model & migration, comment create/list/delete, and using Preload to load a book's comments on demand in the detail endpoint. Full code and verification commands in every section."
date: 2026-09-02
series: gin-gorm
level: P3
tags:
  - Go
  - PostgreSQL
  - ORM
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

> **Prerequisites:** finish the crash course — the project with a single `books` table and the five CRUD handlers (plus an optional hard-delete route `DELETE /books/:id/permanent`; register it or not, per Part 9's final `main.go`). Conventions are the same as the crash course (`WithContext` on every DB call, `errors.Is` for 404, and the 400 · 404 · 201 semantics).

A real book API can't live with only a `books` table. This part adds a second table, `comments`, and turns "one-to-many" into a real model.

---

## 1. Adding a Second Table: comments (One-to-Many)

**Goal:** model the one-to-many relationship between `Book` and `Comment`, so "a book has many comments" becomes a real model.

Why comments for the second table? In the book domain, "book → comments" is the most natural and easiest to relate to; one-to-many + foreign key + `Preload` is the first rung on the multi-table ladder. The third table (`tags`, many-to-many) gets its own dedicated part — [GORM Many-to-Many in Practice](./gorm-gin-tags). This part teaches only one new table.

### 1.1 The Comment Model

Create `models/comment.go`:

```go
package models

import "gorm.io/gorm"

type Comment struct {
    gorm.Model
    BookID   uint   `json:"bookId" gorm:"not null;index"` // foreign key: which book this comment belongs to
    Nickname string `json:"nickname"`                     // commenter name (no users/auth table yet)
    Content  string `json:"content" gorm:"not null"`
}
```

Field notes:

- `BookID` is the foreign key; `index` keeps it indexed — looking up comments by book is the hottest query in this domain;
- The commenter is a plain `Nickname` string — deliberately no user table or auth (that is another topic);
- `gorm.Model` brings soft delete for free — comments are soft-deletable too, behaving exactly like the crash course.

### 1.2 Adding the Relationship Field to Book

Append to `models/book.go`:

```go
type Book struct {
    gorm.Model
    Title    string    `json:"title" gorm:"not null"`
    Author   string    `json:"author" gorm:"not null"`
    Price    int       `json:"price"`
    Comments []Comment `json:"comments,omitempty"` // relationship declaration, not a table column; only used by Preload
}
```

> **Why doesn't this field need a `foreignKey` tag?** GORM's has-many default convention is "parent type name + parent primary-key field name" (`Book` + `ID` = `BookID`) — `Comment.BookID` hits that convention, so the association resolves automatically. **When must you write `gorm:"foreignKey:..."` explicitly?** When the child's foreign-key field drifts from the convention (say, you name it `BookRef`). And note: **this field must not be removed** — it's the relationship declaration that `Preload("Comments")` resolves by name.

> **Why doesn't `Comments` appear in list responses?** If list endpoints loaded every book's comments by default, payloads would balloon and you'd invite N+1 queries. `json:"comments,omitempty"` plus **no default Preload** — comments load only in the **detail endpoint, on demand** (Section 3). That's the standard practice in real projects.

### 1.3 Migration Update & Verification

Change `main.go`'s `AutoMigrate` to create both tables at once:

```go
if err := db.DB.AutoMigrate(&models.Book{}, &models.Comment{}); err != nil {
    log.Fatal("migration failed:", err)
}
```

- `books` already exists, and this part **adds no columns to it** — `Comments []Comment` is a *relationship declaration*, not a column, so AutoMigrate leaves the `books` table alone;
- `comments` is created on its first migration, together with the foreign key (`book_id → books.id`) and the `index`.

**Verify:**

```text
\d comments
-- you should see book_id indexed, a foreign-key constraint pointing at books(id),
-- and the usual gorm.Model columns (deleted_at etc.)
```

> **⚠️ Soft-deleting the parent does NOT cascade to children:** soft-deleting a `books` row only stamps `books.deleted_at` — **`comments.deleted_at` is untouched**, so the comments stay visible and queryable. The crash course taught "soft delete = the framework rewrites your SQL"; here's the other side: **soft delete on the parent never cascades to the child table**. If you want "deleting a book hides its comments", write it yourself (e.g. `db.Model(&models.Comment{}).Where("book_id = ?", id).Update("deleted_at", time.Now())` — illustrative only, `WithContext` and error checks omitted) — this part doesn't cover it.

---

## 2. Comment Management: CRUD on the Second Table

**Goal:** write the CRUD for comments (create, list per book with pagination, delete) and register the routes — a faithful replay of the crash course's six-step skeleton on a second table. No Preload yet (that's Section 3).

### 2.1 Creating a Comment

`handlers/comment.go`:

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

// CreateComment creates a comment for the given book
func CreateComment(c *gin.Context) {
    id := c.Param("id")

    // 1. The book must exist
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

    // 2. Bind the comment body (nickname optional, content required)
    var input struct {
        Nickname string `json:"nickname"`
        Content  string `json:"content" binding:"required"`
    }
    if err := c.ShouldBindJSON(&input); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "please send valid JSON"})
        return
    }

    // 3. Insert, with the foreign key pointing at the current book
    comment := models.Comment{
        BookID:   book.ID,
        Nickname: input.Nickname,
        Content:  input.Content,
    }
    if err := db.DB.WithContext(c.Request.Context()).Create(&comment).Error; err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create comment"})
        return
    }

    c.JSON(http.StatusCreated, comment)
}
```

> Notice step 3 does not `Create(&input)` directly — `input` is the request DTO, `comment` is the model. **Separating request structs from models** graduates here from the crash course's "local experiment" (its Part 5 advanced section already used `createBookInput`, back when the two structs mostly overlapped) into the main-line rule of this series; the dedicated DTO article formalizes it ([GORM Data Engineering](./gorm-gin-dto-batch)).

**Testing:**

```bash
curl -X POST http://localhost:8080/books/1/comments \
  -H "Content-Type: application/json" \
  -d '{"nickname":"Alice","content":"Very clearly written!"}'
# → 201, returns the comment. Keys are uppercase (ID / CreatedAt — gorm.Model has no
# json tags; see the crash course), not lowercase id
```

### 2.2 Listing a Book's Comments with Pagination

`ListComments` — a pagination skeleton worth memorizing (it parses params with `strconv.Atoi`; if `handlers/comment.go` doesn't import `strconv` yet after 2.1, add it):

```go
// ListComments lists a book's comments page by page, newest first.
// Returns {items, total, page, pageSize}; total is the filtered count.
func ListComments(c *gin.Context) {
    id := c.Param("id")

    // 1. Parse the pagination params
    // 1.1 page: which page, default 1
    page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
    // 1.2 pageSize: items per page, default 10
    pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "10"))

    // 2. Validate (defensive: bad values fall back to defaults)
    // 2.1 page starts at 1
    if page < 1 {
        page = 1
    }
    // 2.2 pageSize falls back to 10
    if pageSize < 1 {
        pageSize = 10
    }
    // 2.3 pageSize caps at 100: stop a client pulling too much at once
    if pageSize > 100 {
        pageSize = 100
    }

    // 3. Build the query: only this book's comments
    var comments []models.Comment
    query := db.DB.WithContext(c.Request.Context()).
        Model(&models.Comment{}).
        Where("book_id = ?", id)

    // 4. Count the filtered total first
    var total int64
    if err := query.Count(&total).Error; err != nil {
        _ = c.Error(err) // specific error goes to Gin's log; the client message stays uniform
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query comments"})
        return
    }

    // 5. Then fetch the current page (newest first)
    if err := query.
        Order("created_at DESC").
        Offset((page - 1) * pageSize).
        Limit(pageSize).
        Find(&comments).Error; err != nil {
        _ = c.Error(err) // specific error goes to Gin's log; the client message stays uniform
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query comments"})
        return
    }

    // 6. Return the page result
    c.JSON(http.StatusOK, gin.H{
        "items":    comments,
        "total":    total,
        "page":     page,
        "pageSize": pageSize,
    })
}
```

This is the series' first paginated endpoint — read the full shape and memorize the `Count + Order + Offset + Limit` combination. (The next part, [GORM Media & Query Enhancement](./gorm-gin-media-query), reuses the same skeleton on the book list and folds this parsing into a `parsePagination` helper.)

One coding detail worth noticing: `strconv.Atoi` errors are dropped with `_`, and invalid values fall back to defaults — defensive parsing.

> **Why are the error messages uniform?** Both `Count` and `Find` returning "failed to query comments" is deliberate — the client sees a 500 and doesn't need to know which step died (and leaking internals outward is unsafe). What must be distinguished is the **server-side log**: `_ = c.Error(err)` hands the specific error to Gin's logging middleware. In production this upgrades to `slog` + a unified error middleware (landed in [GORM Engineering in Practice (Part 2)](./gorm-gin-engineering-reliability)).

**Testing:**

```bash
curl "http://localhost:8080/books/1/comments?page=1&pageSize=10"
# {"items":[...],"total":1,"page":1,"pageSize":10}
```

> **The semantic difference from create/delete:** `CreateComment` / `DeleteComment` first check that the book exists (they return 404 once the book is soft-deleted), but `ListComments` doesn't — after a book is soft-deleted, `GET /books/:id/comments` still returns 200 with the historical comments. Deliberate: child data stays queryable after the parent's soft delete (echoing the ⚠️ box in 1.3). A list endpoint fetches rows by condition; it doesn't decide whether the resource exists.

### 2.3 Deleting a Comment

```go
// DeleteComment soft-deletes a comment. The delete condition is the comment's
// primary key AND an owning book that matches — deleting by cid alone would be
// a horizontal-privilege hole: the URL is /books/:id/comments/:cid, so we must
// ensure the comment belongs to THIS book, otherwise /books/1/comments/99 could
// delete book 2's comment.
func DeleteComment(c *gin.Context) {
    id := c.Param("id")   // the book's id (ownership check)
    cid := c.Param("cid") // the comment's id (primary key)

    // Primary key + Where combine into: DELETE ... WHERE id = cid AND book_id = id
    result := db.DB.WithContext(c.Request.Context()).
        Where("book_id = ?", id).
        Delete(&models.Comment{}, cid)

    // Check the error first, then the row count: a non-numeric cid errors during
    // primary-key conversion (500); a condition that matches nothing (no such
    // comment, or not this book's) is the 404 — same two-layer check as the reads
    if result.Error != nil {
        _ = c.Error(result.Error)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete comment"})
        return
    }
    if result.RowsAffected == 0 {
        c.JSON(http.StatusNotFound, gin.H{"error": "comment not found"})
        return
    }
    c.JSON(http.StatusOK, gin.H{"message": "comment deleted"})
}
```

`Delete(&models.Comment{}, cid)` is isomorphic to deleting a book in the crash course — another instantiation of "run the operation + check `RowsAffected`" from the six-step skeleton.

**Testing:**

```bash
curl -X DELETE http://localhost:8080/books/1/comments/1
# → 200; deleting the same comment again → 404
```

### 2.4 Route Registration

Put these three lines into `main.go`'s route block (all other routes stay as in the crash course):

```go
r.POST("/books/:id/comments", handlers.CreateComment)
r.GET("/books/:id/comments", handlers.ListComments)
r.DELETE("/books/:id/comments/:cid", handlers.DeleteComment)
```

---

## 3. Reading Relationships: Preload for One-to-Many

**Goal:** make the detail endpoint load a book's comments on demand with `Preload("Comments")` — the heart of reading one-to-many, and the genuinely new knowledge of this part.

### 3.1 Loading Comments On Demand in the Detail Endpoint

`GetBook` in `handlers/book.go` gains one `Preload` line:

```go
// Returns one book with its comments, loaded on demand
func GetBook(c *gin.Context) {
    id := c.Param("id")

    var book models.Book
    result := db.DB.WithContext(c.Request.Context()).Preload("Comments").First(&book, id)
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

`Preload("Comments")` makes a single `First` also fetch all of that book's comments: GORM queries `books` first, then fetches `comments` by `book_id` in one bulk query and assembles the result — **no N+1** (two SQL statements assembled once, not row-by-row queries).

> **List endpoints don't Preload:** `GetBooks` stays exactly as it is (no comments); only the detail loads them. **The API contract is decided by what the data is for, not by what the ORM can do.**

**Testing:**

```bash
curl http://localhost:8080/books/1
# the detail response should include "comments":[...]
```

---

## Wrap-Up

- **New routes in this part:**

| Method | Path                              | Handler       |
| ------ | --------------------------------- | ------------- |
| POST   | `/books/:id/comments`             | CreateComment |
| GET    | `/books/:id/comments`             | ListComments  |
| DELETE | `/books/:id/comments/:cid`        | DeleteComment |
| GET    | `/books/:id` (modified, now loads Comments) | GetBook       |

- Your project now: `books` + `comments` two tables, comment CRUD, and a detail endpoint that loads comments on demand;
- Next part, [GORM Media & Query Enhancement](./gorm-gin-media-query): add book covers (upload + static serving), then upgrade the list into pagination / search / sort, plus a comment count per book.
