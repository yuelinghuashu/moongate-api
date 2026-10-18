---
title: "GORM Many-to-Many in Practice: Books & Tags"
description: "Part 5: delivers on an earlier promise — adding tags to books (many-to-many): the many2many declaration & join table, Preload, filtering by tag, adding/removing associations, plus two advanced topics: deleting the parent record, and a join model with extra fields."
date: 2026-09-05
series: gin-gorm
order: 5
tags:
  - Go
  - PostgreSQL
  - ORM
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
    // partial unique index: only rows that are not soft-deleted are constrained
    Name string `json:"name" gorm:"index:idx_tags_name,unique,where:deleted_at IS NULL;not null"`
}
```

- `Name` carries a unique index: the same tag name exists exactly once globally — this is the foundation of "tag deduplication" (`FirstOrCreate` relies on it; see Section 3);
- **Why a *partial* unique index (`where deleted_at IS NULL`) instead of a plain one?** The crash course, Part 8, flagged a trap: soft-deleted rows still occupy a unique index, so re-creating the same name hits the constraint. That example used a hypothetical `ISBN`; here it is **real** — `tags` embeds `gorm.Model` (including `deleted_at`), so with a plain `uniqueIndex`, soft-deleting `Go` and then creating `Go` again fails on the unique constraint. With `where`, only live rows take part in uniqueness, and soft-deleted tags don't hold the name;

> **Free-form tags vs. a controlled vocabulary (a design decision):** this part follows **user-defined tags** (the Douban-style model) — anyone can tag a book on the fly; the `Name` unique index plus `FirstOrCreate` deduplicates automatically, and tags are created on first use.
> If your product ships a **controlled vocabulary** of official, preset tags (the category model), the change is simple: drop the "create-by-name `FirstOrCreate` + filter-by-name" logic, and instead preset the tag table with the front end only sending existing `tagId`s; the handler validates the tag first (404) and then `Append`s — the relationship goes from "find-or-create by name" to "validate by id". The vocabulary model has no dedup problem, but it is less flexible; free-form tags need normalization as a safety net (see 3.1).

### 1.2 Adding the Association Field to Book

In `models/book.go`, append after `CoverPath` (existing fields such as `Comments` stay unchanged):

```go
Tags []Tag `json:"tags,omitempty" gorm:"many2many:book_tags;constraint:OnDelete:CASCADE"` // many-to-many: through the join table book_tags
```

`constraint:OnDelete:CASCADE` gives both join-table foreign keys cascade deletes — a physical book delete takes the join rows with it (why this is mandatory: Section 3.4).

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

\d tags
-- idx_tags_name is a partial unique index: UNIQUE, btree (name) WHERE deleted_at IS NULL
```

> The join table's composite primary key means the same (book, tag) pair can exist at most once — that is where "natural deduplication" comes from. Note a duplicate append does **not** error: `Append` inserts into the join table with `ON CONFLICT DO NOTHING` built in (measured SQL in 3.1).

---

## 2. Reading: Preload & Filtering by Tag

**Goal:** on the reading side — `Preload` pulls tags along on demand, and books can be filtered by tag.

### 2.1 The Detail Endpoint Carries Tags

The relations part's `GetBook` already does `Preload("Comments")` (the bounded `Limit(5)` form); chain a second `Preload` here:

```go
result := db.DB.WithContext(c.Request.Context()).
    Preload("Comments", func(tx *gorm.DB) *gorm.DB { // Part 2's bound, kept as-is: 5 latest in the detail
        return tx.Order("created_at DESC").Limit(5)
    }).
    Preload("Tags"). // a book's tags are naturally bounded — no cap needed
    First(&book, id)
```

- `Preload` chains: each association runs its own bulk query (`comments` in one, `book_tags ⋈ tags` in one), assembled once — **neither path triggers N+1**;
- **Bound or not by cardinality:** comments are unbounded (a book can have tens of thousands), so the detail previews the latest 5; tags are bounded (a handful to dozens), so plain `Preload("Tags")` is fine — the bounded form is explained in full in the relations part §3.1;
- **No Preload on the list:** same contract as the relations part (lists stay light, the detail is heavy); `json:"tags,omitempty"` plus no default loading.

**Testing:**

```bash
curl http://localhost:8080/books/1
# the detail response should include "tags":[...] (tag the book first, then test — see Section 3)
```

### 2.2 Filtering Books by Tag (Paginated)

"List all books tagged Go" — JOIN the join table twice, with pagination parsed by the `parsePagination` helper folded out in Part 3 (the handler lives in `handlers/tag.go`; imports are listed in 3.1, and `parsePagination` is a same-package function):

```go
// GetBooksByTag lists the books carrying the given tag (GET /tags/:name/books),
// paginated and ordered newest-first. Response shape matches Part 3's book list:
// {items, total, page, pageSize}.
func GetBooksByTag(c *gin.Context) {
    // The tag name goes through the same normalization as the write path (3.1);
    // otherwise "Go" / "go" would not find the same tag
    tagName := strings.ToLower(strings.TrimSpace(c.Param("name")))
    page, pageSize := parsePagination(c) // Part 3's helper (handlers/pagination.go)

    query := db.DB.WithContext(c.Request.Context()).
        Model(&models.Book{}). // Count(&total)'s dest is an int64 — without Model, GORM has no table
        Joins("JOIN book_tags ON book_tags.book_id = books.id").
        // Tables joined by hand do not get the soft-delete condition automatically:
        // the tags side must spell it out
        Joins("JOIN tags ON tags.id = book_tags.tag_id AND tags.deleted_at IS NULL").
        Where("tags.name = ?", tagName)

    var total int64
    if err := query.Count(&total).Error; err != nil {
        _ = c.Error(err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
        return
    }

    var books []models.Book
    if err := query.
        Order("books.created_at DESC"). // created_at is ambiguous after the JOIN — qualify it
        Offset((page - 1) * pageSize).
        Limit(pageSize).
        Find(&books).Error; err != nil {
        _ = c.Error(err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
        return
    }

    c.JSON(http.StatusOK, gin.H{"items": books, "total": total, "page": page, "pageSize": pageSize})
}
```

> **⚠️ Breaking contract change:** this endpoint's response turns from a bare array into a `{items,total,page,pageSize}` object (aligned with Part 3's `GET /books`) — existing front ends must adapt.

> **`Count` needs GORM to know the table.** `Find(&books)` can infer `books` from the `models.Book` dest, but `Count(&total)`'s dest is an `int64` — leave out `Model(&models.Book{})` and it fails with `Table not set, please set it like: db.Model(&user) or db.Table("users")` (measured), which the endpoint reports as a 500. This is the textbook case for the "when must you write `Model()`" rule: **whenever the terminal method's dest is not a model struct, specify it explicitly**.

> **Why does the `tags` side need an explicit `deleted_at IS NULL`?** GORM only adds the soft-delete condition to the **root model's** table: the measured WHERE for this query is `tags.name = ? AND books.deleted_at IS NULL`, so `tags.deleted_at` has to be supplied by hand (as above, in the JOIN condition) — otherwise a soft-deleted tag keeps listing its books. For the same reason, `created_at` in `ORDER BY` is ambiguous after the JOIN (`books` and `tags` both have that column), so it must be written as `books.created_at`.

> **Why isn't the route `/books/by-tag`? (a real-world pitfall)** The crash course already registered `GET /books/:id`, and Gin's route tree **doesn't allow a static segment and a wildcard segment at the same position** — registering `/books/by-tag` too makes it panic outright ("conflicts with existing wildcard"). So filtering by tag lives under the `tags` prefix, `GET /tags/:name/books` — more RESTful in spirit, too.

**Route registration:**

```go
r.GET("/tags/:name/books", handlers.GetBooksByTag)
```

**Testing:**

```bash
curl "http://localhost:8080/tags/go/books?page=1&pageSize=10"
# {"items":[...],"total":1,"page":1,"pageSize":10}
curl "http://localhost:8080/tags/GO/books"   # normalized, so identical to /tags/go/books
```

---

## 3. Writing & Maintenance: Creating and Removing Associations

**Goal:** write relationships — add tags to a book (idempotently), remove them, replace the whole set, and what happens to the join table when the parent record is deleted.

### 3.1 Adding a Tag to a Book (`Append` Is Already Idempotent)

`FirstOrCreate` keeps tags unique (the `Name` unique index); `Association("Tags").Append` writes the join table (the imports in `handlers/tag.go`: `strings`, plus `db` / `models` / `net/http` / `gin`). This route hangs off 2.4's `bookGroup`, so `requireBook` covers the parent check:

```go
// addTagInput: the request DTO for tagging a book (same convention as
// createBookInput / createCommentInput: one named DTO per write endpoint,
// with validation rules living on the DTO)
type addTagInput struct {
    Name string `json:"name" binding:"required"`
}

// AddBookTag appends a tag to a book: the tag is created first if it doesn't
// exist, then the join table is written (POST /books/:id/tags)
func AddBookTag(c *gin.Context) {
    // 1. Existence is already guaranteed by 2.4's requireBook middleware — just take the entity
    book := c.MustGet("book").(*models.Book)

    // 2. Bind the tag name and normalize it: one meaning, one spelling
    var input addTagInput
    if err := c.ShouldBindJSON(&input); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "please send valid JSON"})
        return
    }
    name := strings.ToLower(strings.TrimSpace(input.Name))

    // 3. Create the tag first if it doesn't exist (Name's unique index dedupes it);
    // the second argument drives both the WHERE and the fields of the new record
    var tag models.Tag
    if err := db.DB.WithContext(c.Request.Context()).
        FirstOrCreate(&tag, models.Tag{Name: name}).Error; err != nil {
        _ = c.Error(err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process tag"})
        return
    }

    // 4. Write the join table: Append's insert carries ON CONFLICT DO NOTHING, so it is idempotent
    if err := db.DB.WithContext(c.Request.Context()).
        Model(book).Association("Tags").Append(&tag); err != nil {
        _ = c.Error(err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to add tag"})
        return
    }

    c.JSON(http.StatusOK, gin.H{"message": "tag added"})
}
```

> **The two arguments of `FirstOrCreate(&tag, models.Tag{Name: name})` are not a duplicate.** The first is the **target**: query results fill it, and when nothing is found it is the very record that gets inserted. The second is the **condition**: GORM turns it into `WHERE name = ?` and, on a miss, copies those equalities **back into the target** so the new tag carries its name.
>
> What happens without the second one? `Where("name = ?", name).FirstOrCreate(&tag)` produces `... (name) VALUES ("")` — a string `Where` becomes a `clause.Expr` and is never copied back; and `name` only declares `not null`, where the empty string is perfectly legal, so you silently end up with an anonymous tag in the table (measured on v1.31.2). So the **struct condition is not optional**; conversely, since it already generates `WHERE name = ?`, wrapping another `Where("name = ?")` around it is redundant.

> **Why no `Count` before inserting in step 4?** The folklore is that appending the same tag twice "hits `book_tags`'s composite primary key and errors out" — but GORM builds conflict handling into that insert (measured on v1.31.2):
>
> ```sql
> INSERT INTO book_tags (book_id, tag_id) VALUES (1,7) ON CONFLICT DO NOTHING
> ```
>
> So a duplicate append neither errors nor creates a second row — **idempotency is `Append`'s own behavior**. Checking first then inserting only buys an extra round trip and a wider race window. A check is worth keeping only when you need to tell "this really was added" (say, to return 201 vs 200) — and then the reason is response semantics, not avoiding an error.

> **Normalization: the make-or-break for free-form tags.** `FirstOrCreate` deduplicates only **byte-identical strings** — `Go`, `golang`, and `Go ` are three tags in the database. The `strings.ToLower` + `strings.TrimSpace` in step 2 is not optional: **a unique index guarantees "string uniqueness", not "semantic uniqueness"**. Alias mapping (`Go` → `golang`) belongs here too. The read path (2.2's `GetBooksByTag`) uses the same normalization, otherwise a stored `go` cannot be found by querying `Go`.

> **Two known limits:** (1) `FirstOrCreate` is "check then insert", so under concurrency two requests can both miss and both insert, hitting the unique index with a 500 — add `clause.OnConflict{DoNothing: true}` or retry once to harden it; (2) `FirstOrCreate` and `Append` are not wrapped in a transaction, so a failed `Append` leaves an unused tag behind. [Engineering (Part 1)](./gorm-gin-engineering-layering) lifts this into a service layer and wraps it in `db.Transaction()`.

**Route registration:**

```go
bookGroup.POST("/tags", handlers.AddBookTag)
```

**Testing:**

```bash
curl -X POST http://localhost:8080/books/1/tags \
  -H "Content-Type: application/json" \
  -d '{"name":"Go"}'
# → 200; the stored tag name is the normalized go. Run the same command again: still 200
# (idempotent, no duplicate join row); -d '{"name":" GO "}' hits the same tag

curl -X POST http://localhost:8080/books/999/tags \
  -H "Content-Type: application/json" -d '{"name":"Go"}'
# → 404: the book does not exist. This 404 comes from the requireBook middleware — the handler never runs
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

- **Why `RemoveBookTag` and not `DeleteBookTag`?** It deletes a **relationship** (one row in `book_tags`); the tag itself in `tags` stays untouched — other books still use it. The series splits the verbs accordingly: **Create/Delete act on entity rows** (`DeleteBook`, `DeleteComment`), while **Add/Remove act on relationships** (`AddBookTag`, `RemoveBookTag`). Calling it `DeleteBookTag` would suggest the tag itself was deleted;
- `Table("book_tags")...Delete(nil)` runs `DELETE ... WHERE book_id = ? AND tag_id = ?` straight against the join table — reading, writing, and deleting can all ride on the "the join table is just a table" mindset;
- `RowsAffected == 0` → the book doesn't carry this tag → 404 (the same ownership-check logic as deleting comments).

**Route registration:**

```go
bookGroup.DELETE("/tags/:tid", handlers.RemoveBookTag)
```

**Testing:**

```bash
curl -X DELETE http://localhost:8080/books/1/tags/1
# → 200; deleting the same row again → 404 (the tag is not on this book)
curl -X DELETE http://localhost:8080/books/999/tags/1
# → 404 (the book does not exist — this one comes from the requireBook middleware, the handler never runs)
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
- **Physical delete:** GORM **creates the join table with two foreign keys** (`book_tags.book_id → books.id`, `book_tags.tag_id → tags.id`), default **`NO ACTION`** — so "deleting the book leaves orphan join rows" is *not* the default behaviour; the default is a hard **error**: `DELETE FROM books` hits `fk_book_tags_book` and PostgreSQL returns `violates foreign key constraint` (the endpoint 500s). For a physical delete to work the foreign keys must cascade. Two solutions:

```go
// Solution one: cascade at the database level from the association declaration
// (the §1.2 form; a freshly AutoMigrated database picks it up directly)
Tags []Tag `json:"tags,omitempty" gorm:"many2many:book_tags;constraint:OnDelete:CASCADE"`
```

```sql
-- Solution two: patch an existing database (AutoMigrate won't change a foreign key
-- that already exists, so this has to be an explicit migration)
ALTER TABLE book_tags DROP CONSTRAINT fk_book_tags_book;
ALTER TABLE book_tags ADD CONSTRAINT fk_book_tags_book
  FOREIGN KEY (book_id) REFERENCES books(id) ON DELETE CASCADE;
```

> The same trap exists on `comments` (the crash course, Part 8 ⚠️): only after both foreign keys are `ON DELETE CASCADE` can `/permanent` delete a book that has comments and tags. Soft delete is unaffected — it only writes `deleted_at`.

---

## 4. Advanced: A Join Model with Extra Fields

**Goal:** attach fields to the relationship itself — when a join model earns its keep, and which of the two routes to take.

The default join table only has `book_id` / `tag_id`. To give the relationship properties (say "this book puts the ★favorite tag third" or "when was this tag applied"), you need an **explicit join model**:

```go
// models/book_tag.go — a custom join table: composite primary key + extra fields
type BookTag struct {
    BookID    uint      `gorm:"primaryKey"`
    TagID     uint      `gorm:"primaryKey"`
    Position  int       // sort value: the manual order of tags within a book
    CreatedAt time.Time // when the tag was applied
}
```

### 4.1 Preferred: `SetupJoinTable` (keep the many2many API)

Instead of tearing the relationship apart, the smallest change is to tell GORM "the `book_tags` join table is modeled by `BookTag`" — the `many2many:book_tags` declaration on `Book.Tags` stays **exactly as it is**:

```go
// end of db.InitDB in db/db.go, or in main.go before AutoMigrate (runs once at startup)
if err := db.DB.SetupJoinTable(&models.Book{}, "Tags", &models.BookTag{}); err != nil {
    log.Fatal("failed to register the join-table model: ", err)
}
// Include the join model in the migration so position / created_at get created
if err := db.DB.AutoMigrate(&models.Book{}, &models.Comment{}, &models.Tag{}, &models.BookTag{}); err != nil {
    log.Fatal("migration failed: ", err)
}
```

The association API keeps working and the extra columns come along — the `Append` SQL measured on GORM v1.31.2:

```sql
INSERT INTO book_tags (book_id, tag_id, position) VALUES (1,7,0) ON CONFLICT DO NOTHING
```

- `Preload("Tags")`, `Association("Tags").Append/Replace/Delete` all still work; reading through `Tags []Tag` is unchanged;
- extra columns take zero values on `Append` — to write `position`, operate on the join model directly: `db.DB.Save(&models.BookTag{BookID: 1, TagID: 7, Position: 1})` (or `FirstOrCreate` then `Update`);
- `constraint:OnDelete:CASCADE` on the `Tags` field still applies, so physical book deletion still cascades (3.4's conclusion holds).

### 4.2 Alternative: two has-many relations pointing at the join model

Only when the join table carries **its own business** (its own endpoints, a state machine, an audit trail) is it worth splitting the relationship — at that point it is simply another business table:

```go
type Book struct {
    // ...existing fields
    BookTags []BookTag `gorm:"foreignKey:BookID;constraint:OnDelete:CASCADE"`
}

type Tag struct {
    // ...existing fields
    BookTags []BookTag `gorm:"foreignKey:TagID;constraint:OnDelete:CASCADE"`
}
```

- Reads and writes operate on join records directly: `db.Create(&models.BookTag{BookID: 1, TagID: 7, Position: 1})`, then `Preload("BookTags")` and map to `[]Tag` yourself;
- the convenient `Association("Tags")` writes no longer apply — "a book's tags" has to be joined or mapped by hand;
- **⚠️ `constraint:OnDelete:CASCADE` must be put back**: these has-many relations introduce fresh foreign keys, and without CASCADE they default to `NO ACTION` (measured) — which resurrects the very "physical delete hits a foreign key" failure 3.4 solved.

> **How to choose:** the default join table covers about 90% of cases; to add fields to the relationship, try `SetupJoinTable` first (smallest diff, `Tags []Tag` reads unchanged); only split into two has-many relations when the join table needs a lifecycle of its own.

## Wrap-Up

- **New routes in this part:**

| Method | Path                   | Handler       |
| ------ | ---------------------- | ------------- |
| POST   | `/books/:id/tags`      | AddBookTag    |
| DELETE | `/books/:id/tags/:tid` | RemoveBookTag |
| GET    | `/tags/:name/books`    | GetBooksByTag |

(`main.go` additions: add `&models.Tag{}` to `AutoMigrate`; the `POST /tags` and `DELETE /tags/:tid` lines hang off 2.4's `bookGroup`, while `GET /tags/:name/books` stays a standalone route.)

- Your project now: three tables `books` + `comments` + `tags`, cover-image upload with static serving, paginated search lists, comment CRUD, tags with their join table, batch import and DTO validation;
- The series now covers both association shapes — has-many and many2many — along with querying, aggregation, and engineering practices; an optional extension (optional reading): [**GORM Engineering in Practice (Part 1): Layering, Dependency Injection & Testability**](./gorm-gin-engineering-layering) — refactor the always-direct `db.DB` into a `BookRepository` interface + a Service layer + table-driven tests. The tests and transactions that layering unlocks will put every `WithContext` in this part to good use.
