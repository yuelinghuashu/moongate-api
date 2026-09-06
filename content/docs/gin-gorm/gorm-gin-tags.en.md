---
title: "GORM Many-to-Many in Practice: Books & Tags"
description: "Part 5: delivers on an earlier promise — adding tags to books (many-to-many): the many2many declaration & join table, Preload, filtering by tag, adding/removing associations, plus two advanced topics: deleting the parent record, and a join model with extra fields."
date: 2026-09-05
series: gin-gorm
tags:
  - Go
  - PostgreSQL
  - ORM
---

> **Prerequisites:** finish [the crash course](./gorm-gin-crud-tutorial), [the relations part](./gorm-gin-relations), [the media part](./gorm-gin-media-query), and [the data engineering part](./gorm-gin-dto-batch) — the project with `books` + `comments` + cover images + paginated search. Conventions are the same as the crash course (`WithContext` on every DB call, `errors.Is` for 404, and the 400 · 404 · 201 semantics).

When the relations part finished laying out "book → comments" (one-to-many), it left a teaser — "the third table, `tags` (many-to-many), is left for later". This part pays it off: adding tags to books, and covering GORM's many2many in one go — the join-table declaration, loading, filtering, and association add/remove.

---

## 1. Model & Migration: Declaring the many2many

**Goal:** declare `Book` ↔ `Tag` with `many2many`, so `AutoMigrate` automatically creates `tags` and the join table `book_tags`.

### 1.1 The Tag Model

Create `models/tag.go`:

```go
package models

import "gorm.io/gorm"

type Tag struct {
    gorm.Model
    Name string `json:"name" gorm:"uniqueIndex;not null"`
}
```

- `Name` carries a unique index: the same tag name exists exactly once globally — this is the foundation of "tag deduplication" (`FirstOrCreate` relies on it; see Section 3);

> **Free-form tags vs. a controlled vocabulary (a design decision):** this part follows **user-defined tags** (the Douban-style model) — anyone can tag a book on the fly; the `Name` unique index plus `FirstOrCreate` deduplicates automatically, and tags are created on first use. If your product ships a **controlled vocabulary** of official, preset tags (the category model), the change is simple: drop the "create-by-name `FirstOrCreate` + filter-by-name" logic, and instead preset the tag table with the front end only sending existing `tagId`s; the handler validates the tag first (404) and then `Append`s — the relationship goes from "find-or-create by name" to "validate by id". The vocabulary model has no dedup problem, but it is less flexible; free-form tags need normalization as a safety net (see 3.1).

### 1.2 Adding the Association Field to Book

In `models/book.go`, append after `CoverPath` (existing fields such as `Comments` stay unchanged):

```go
Tags []Tag `json:"tags,omitempty" gorm:"many2many:book_tags;"` // many-to-many: through the join table book_tags
```

> **The essential difference from one-to-many:** a one-to-many relationship is written on the **child table** (`comments.book_id`); a many-to-many is written on the **association declaration** (`gorm:"many2many:book_tags;"`), and the join table itself **needs no model** — AutoMigrate creates it for you. What you declare is the "relationship", not a "table".

### 1.3 Migration & Verification

Change `main.go`'s `AutoMigrate` to create all three tables at once (`tags` is created on its first migration; the `book_tags` join table is created automatically too):

```go
if err := db.DB.AutoMigrate(&models.Book{}, &models.Comment{}, &models.Tag{}); err != nil {
    log.Fatal("migration failed:", err)
}
```

**Verify:**

```text
\d book_tags
-- you should see two columns, book_id and tag_id, whose primary key is the
-- composite key (book_id, tag_id) (unique by default)
```

> The join table's composite primary key means the same (book, tag) pair can exist at most once — both "a duplicate append errors out" and "natural deduplication", covered later, stem from it.

---

## 2. Reading: Preload & Filtering by Tag

**Goal:** on the reading side — `Preload` pulls tags along on demand, and books can be filtered by tag.

### 2.1 The Detail Endpoint Carries Tags

The relations part's `GetBook` already does `Preload("Comments")`; chain a second `Preload` here:

```go
result := db.DB.WithContext(c.Request.Context()).
    Preload("Comments").
    Preload("Tags").
    First(&book, id)
```

- `Preload` chains: each association runs its own bulk query (`comments` in one, `book_tags ⋈ tags` in one), assembled once — **neither path triggers N+1**;
- **No Preload on the list:** same contract as the relations part (lists stay light, the detail is heavy); `json:"tags,omitempty"` plus no default loading.

**Testing:**

```bash
curl http://localhost:8080/books/1
# the detail response should include "tags":[...] (tag the book first, then test — see Section 3)
```

### 2.2 Filtering Books by Tag

"List all books tagged Go" — JOIN the join table twice:

```go
// GetBooksByTag lists the books that carry the given tag (GET /tags/:name/books)
func GetBooksByTag(c *gin.Context) {
    tagName := c.Param("name")

    var books []models.Book
    if err := db.DB.WithContext(c.Request.Context()).
        Joins("JOIN book_tags ON book_tags.book_id = books.id").
        Joins("JOIN tags ON tags.id = book_tags.tag_id").
        Where("tags.name = ?", tagName).
        Find(&books).Error; err != nil {
        _ = c.Error(err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
        return
    }

    c.JSON(http.StatusOK, books)
}
```

> **Why isn't the route `/books/by-tag`? (a real-world pitfall)** The crash course already registered `GET /books/:id`, and Gin's route tree **doesn't allow a static segment and a wildcard segment at the same position** — registering `/books/by-tag` too makes it panic outright ("conflicts with existing wildcard"). So filtering by tag lives under the `tags` prefix, `GET /tags/:name/books` — more RESTful in spirit, too.

**Route registration:**

```go
r.GET("/tags/:name/books", handlers.GetBooksByTag)
```

**Testing:**

```bash
curl "http://localhost:8080/tags/Go/books"
# returns the books that carry the Go tag (tag a book first, then test)
```

---

## 3. Writing & Maintenance: Creating and Removing Associations

**Goal:** write relationships — add tags to a book (idempotently), remove them, replace the whole set, and what happens to the join table when the parent record is deleted.

### 3.1 Adding a Tag to a Book (Check First, Then Insert — Idempotent)

`FirstOrCreate` keeps tags unique (the `Name` unique index); `Association("Tags").Append` writes the join table:

```go
// AddBookTag appends a tag to a book: the tag is created first if it doesn't
// exist, then the join table is written (POST /books/:id/tags)
func AddBookTag(c *gin.Context) {
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

    // 2. Bind the tag name
    var input struct {
        Name string `json:"name" binding:"required"`
    }
    if err := c.ShouldBindJSON(&input); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "please send valid JSON"})
        return
    }

    // 3. Create the tag first if it doesn't exist (Name's unique index keeps it deduplicated)
    var tag models.Tag
    if err := db.DB.WithContext(c.Request.Context()).
        Where("name = ?", input.Name).
        FirstOrCreate(&tag, models.Tag{Name: input.Name}).Error; err != nil {
        _ = c.Error(err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process tag"})
        return
    }

    // 4. Write the join table: check first, then insert — idempotent
    var count int64
    if err := db.DB.WithContext(c.Request.Context()).
        Table("book_tags").
        Where("book_id = ? AND tag_id = ?", book.ID, tag.ID).
        Count(&count).Error; err != nil {
        _ = c.Error(err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check tag relation"})
        return
    }
    if count == 0 {
        if err := db.DB.WithContext(c.Request.Context()).
            Model(&book).Association("Tags").Append(&tag); err != nil {
            _ = c.Error(err)
            c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to add tag"})
            return
        }
    }

    c.JSON(http.StatusOK, gin.H{"message": "tag added"})
}
```

> **The price of free-form tags: name normalization.** `FirstOrCreate`'s "deduplication" only works on **byte-identical strings** — `Go`, `golang`, and `Go ` (trailing space) are three different tags in the database.
> In production you normalize before it hits the database: `strings.ToLower` + `strings.TrimSpace`, plus alias mapping when needed (`Go` → `golang`). This part doesn't implement it, but keep it in mind: **a unique index guarantees "string uniqueness", not "semantic uniqueness".**

> **Why check before inserting in step 4?** `Association("Tags").Append(&tag)` is a straight `INSERT` into the join table. `book_tags`'s composite primary key `(book_id, tag_id)` guarantees each pair appears at most once — **appending the same tag twice hits the unique constraint and errors out; it is not idempotent**. That's why the example `Count`s first and inserts after. If your project accepts "duplicate requests error out" semantics, dropping step 4's `Count` works too — this part demonstrates the idempotent version.

**Route registration:**

```go
r.POST("/books/:id/tags", handlers.AddBookTag)
```

**Testing:**

```bash
curl -X POST http://localhost:8080/books/1/tags \
  -H "Content-Type: application/json" \
  -d '{"name":"Go"}'
# → 200, tag added; run the exact same command again and it is still 200
# (idempotent — no duplicate row in the join table)
```

### 3.2 Removing a Tag (Deleting the Join-Table Row Directly)

`Association("Tags").Delete(&tag)` needs the Tag fetched by ID first; this example operates on the join table directly instead, which is more straightforward — the delete condition is exactly the composite primary key:

```go
// RemoveBookTag removes one of a book's tags (DELETE /books/:id/tags/:tid)
func RemoveBookTag(c *gin.Context) {
    id := c.Param("id")
    tid := c.Param("tid")

    result := db.DB.WithContext(c.Request.Context()).
        Table("book_tags").
        Where("book_id = ? AND tag_id = ?", id, tid).
        Delete(nil)

    if result.Error != nil {
        _ = c.Error(result.Error)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to remove tag"})
        return
    }
    if result.RowsAffected == 0 {
        c.JSON(http.StatusNotFound, gin.H{"error": "tag not found or not attached to this book"})
        return
    }
    c.JSON(http.StatusOK, gin.H{"message": "tag removed"})
}
```

- `Table("book_tags")...Delete(nil)` runs `DELETE ... WHERE book_id = ? AND tag_id = ?` straight against the join table — reading, writing, and deleting can all ride on the "the join table is just a table" mindset;
- `RowsAffected == 0` → the book doesn't carry this tag → 404 (the same ownership-check logic as deleting comments).

**Route registration:**

```go
r.DELETE("/books/:id/tags/:tid", handlers.RemoveBookTag)
```

**Testing:**

```bash
curl -X DELETE http://localhost:8080/books/1/tags/1
# → 200; deleting the same row again → 404
```

### 3.3 Replace: Whole-Set Replacement vs. Incremental Append

`Append` is **incremental** (adds on top of the existing relationships); `Replace` is **whole-set replacement** (clears all of the book's tags first, then writes):

```go
// The edit page's "save all tags" scenario: the front end sends the whole set,
// and old tags no longer in it are removed
db.DB.WithContext(c.Request.Context()).Model(&book).Association("Tags").Replace(&tags)
```

> **Don't mix the two semantics:** using `Replace` where `Append` belongs — on an "append one tag" endpoint — silently wipes the book's other tags. Add/remove single tags with Append/Delete; only whole-set saves use Replace.

---

### 3.4 When the Parent Record Is Deleted, What About the Join Table?

`DELETE /books/:id` (soft delete) and `/books/:id/permanent` (physical delete) already exist — many-to-many gives each of the two deletes a layer of nuance:

- **Soft delete:** stamps `books.deleted_at`, and the join-table rows stay **exactly as they are**; queries and Preloads on the book can't see it (the book itself is filtered out) — the same logic as the relations part's comments;
- **Physical delete:** GORM by default does **not clean up the join-table rows**! Orphan `book_tags` rows don't block queries (the book is already gone from any book-based filter), but they waste table space, and ID reuse can splice data across records. Two solutions:

```go
// Solution one: when deleting, explicitly cascade the delete to the associations (Select(clause.Associations))
db.DB.WithContext(c.Request.Context()).
    Select(clause.Associations).
    Unscoped().Delete(&models.Book{}, id)
```

```sql
-- Solution two: give the join table a foreign-key constraint at table-creation time,
-- and let the database cascade the cleanup
ALTER TABLE book_tags
  ADD CONSTRAINT fk_book_tags_book FOREIGN KEY (book_id) REFERENCES books(id) ON DELETE CASCADE;
```

> Use solution one for teaching (no change to the database structure); production often does both — GORM's explicit cascade plus the database foreign key as the safety net.

---

## 4. Advanced: A Join Model with Extra Fields

**Goal:** attach fields to "the relationship itself" — when a join model with extra fields is called for, and how to declare one.

By default the join table holds only `book_id` / `tag_id`. To attach attributes to a relationship (say, "where does this book's close-reading tag rank" or "when was the tag attached"), you need an **explicit join model**:

```go
// models/book_tag.go — custom join table: composite primary key + extra fields
type BookTag struct {
    BookID    uint      `gorm:"primaryKey"`
    TagID     uint      `gorm:"primaryKey"`
    Position  int       // sort value: manual order of a book's tags
    CreatedAt time.Time // when the tag was attached
}
```

Each of the two models hangs a has-many pointing at the join model:

```go
type Book struct {
    // ...existing fields
    BookTags []BookTag `gorm:"foreignKey:BookID"`
}

type Tag struct {
    // ...existing fields
    BookTags []BookTag `gorm:"foreignKey:TagID"`
}
```

Highlights after the upgrade (sketched, not exhaustive):

- The association no longer rides on "`Tags []Tag` + the many2many tag" — instead **two has-manys point at `BookTag`**, and reads and writes go through the join records directly (`db.Create(&models.BookTag{BookID: 1, TagID: 2, Position: 1})`);
- `Association("Tags")`'s convenient automatic writes no longer apply; "a book's tags" becomes `Preload("BookTags")` plus your own mapping;
- **the default join table covers 90% of scenarios** — upgrade to a join model only when the relationship itself needs stored fields (teaching order: default first, upgrade on demand).

---

## Wrap-Up

- **New routes in this part:**

| Method | Path                   | Handler       |
| ------ | ---------------------- | ------------- |
| POST   | `/books/:id/tags`      | AddBookTag    |
| DELETE | `/books/:id/tags/:tid` | RemoveBookTag |
| GET    | `/tags/:name/books`    | GetBooksByTag |

(`main.go` additions: add `&models.Tag{}` to `AutoMigrate`, plus the three routes above.)

- Your project now: three tables `books` + `comments` + `tags`, cover-image upload with static serving, paginated search lists, comment CRUD, tags with their join table, batch import and DTO validation;
- The series now covers both association shapes — has-many and many2many — along with querying, aggregation, and engineering practices; an optional extension (optional reading): [**GORM Engineering in Practice (Part 1): Layering, Dependency Injection & Testability**](./gorm-gin-engineering-layering) — refactor the always-direct `db.DB` into a `BookRepository` interface + a Service layer + table-driven tests. The tests and transactions that layering unlocks will put every `WithContext` in this part to good use.
