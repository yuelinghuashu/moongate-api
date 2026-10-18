---
title: "GORM Relations in Practice: Comment Model, CRUD & Preload"
description: "On top of the crash course's single-table CRUD, this part introduces a second table `comments` (one-to-many): the model & migration, comment create/list/delete, and using Preload to load a book's comments on demand in the detail endpoint. Full code and verification commands in every section."
date: 2026-09-02
series: gin-gorm
order: 2
tags:
  - Go
  - PostgreSQL
  - ORM
---

## 1. Adding a Second Table: comments (One-to-Many)

**Goal:** model the one-to-many relationship between `Book` and `Comment`, so "a book has many comments" becomes a real model.

Why comments for the second table: one-to-many + foreign key + `Preload` is the first rung on the multi-table ladder; many-to-many (`tags`) waits for [GORM Many-to-Many in Practice](./gorm-gin-tags). One new table per part.

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

Add just this one line to `models/book.go` (everything else is unchanged from Part 3 of the crash course):

```go
Comments []Comment `json:"comments,omitempty" gorm:"constraint:OnDelete:CASCADE"` // relationship declaration, not a table column; only used by Preload
```

`constraint:OnDelete:CASCADE` makes AutoMigrate build the foreign key as `ON DELETE CASCADE`: hard-deleting a book takes its comments with it. Without it the foreign key defaults to `NO ACTION` and deleting a book that has comments errors out (exactly the ⚠️ in Part 8 of the crash course).

> **Why doesn't this field need a `foreignKey` tag?** GORM's has-many default convention is "parent type name + parent primary-key field name" (`Book` + `ID` = `BookID`) — `Comment.BookID` hits that convention, so the association resolves automatically. **When must you write `gorm:"foreignKey:..."` explicitly?** When the child's foreign-key field drifts from the convention (say, you name it `BookRef`). And note: **this field must not be removed** — it's the relationship declaration that `Preload("Comments")` resolves by name.

> **Why doesn't `Comments` appear in list responses?** If list endpoints loaded every book's comments by default, payloads would balloon and you'd invite N+1 queries. `json:"comments,omitempty"` plus **no default Preload** — comments load only in the **detail endpoint, on demand** (Section 3), and "on demand" means **bounded**: a few latest comments as a preview, with the full list on 2.2's paginated endpoint. That's the standard practice in real projects.

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

> **⚠️ Soft-deleting the parent does NOT cascade to children:** soft-deleting a `books` row only stamps `books.deleted_at` — **`comments.deleted_at` is untouched**, so the comments stay visible and queryable. The crash course taught "soft delete = the framework rewrites your SQL"; here's the other side: **soft delete on the parent never cascades to the child table**. If you want "deleting a book hides its comments", it has to be written — **§2.4 gives the two proper ways** (a write-side transactional cascade plus a read-side parent check). Note: don't hand-write `Update("deleted_at", time.Now())`; that bypasses GORM's soft-delete logic (no hooks, no `updated_at` upkeep). The standard form is `Delete(&models.Comment{})`.

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

// createCommentInput: the request DTO for creating a comment — same naming and
// responsibility as the crash course's createBookInput: one named DTO per write
// endpoint, with field and validation rules living here while the model owns only the schema.
type createCommentInput struct {
    Nickname string `json:"nickname"`
    Content  string `json:"content" binding:"required"`
}

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

    // 2. Bind the comment body (nickname optional, content required — rules live on the DTO)
    var input createCommentInput
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

> Notice step 3 does not `Create(&input)` directly — `input` is the request DTO, `comment` is the model. **Separating request structs from models** continues the crash course's Part 5 `createBookInput` convention here (one named DTO per write endpoint — this part's is `createCommentInput`), and the dedicated DTO article formalizes it, including the update side's `updateBookInput` and validation-error translation ([GORM Data Engineering](./gorm-gin-dto-batch)).

**Testing:**

```bash
curl -X POST http://localhost:8080/books/1/comments \
  -H "Content-Type: application/json" \
  -d '{"nickname":"Alice","content":"Very clearly written!"}'
# → 201, returns the comment. Keys are uppercase (ID / CreatedAt — gorm.Model has no
# json tags; see the crash course), not lowercase id
```

### 2.2 Listing a Book's Comments with Pagination

`ListComments`: parses the pagination params and fetches only this book's comments (`handlers/comment.go` needs `strconv` in its imports):

```go
// ListComments lists a book's comments page by page (newest first), returning
// {items,total,page,pageSize}.
func ListComments(c *gin.Context) {
    id := c.Param("id")

    // 1. Parse and validate: page defaults to 1, pageSize to 10, bad values fall back, pageSize caps at 100
    page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
    pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "10"))
    if page < 1 {
        page = 1
    }
    if pageSize < 1 {
        pageSize = 10
    }
    if pageSize > 100 {
        pageSize = 100 // stop a client pulling too much at once
    }

    // 2. Build the query: only this book's comments
    var comments []models.Comment
    query := db.DB.WithContext(c.Request.Context()).
        Model(&models.Comment{}).
        Where("book_id = ?", id)

    // 3. Count the filtered total first
    var total int64
    if err := query.Count(&total).Error; err != nil {
        _ = c.Error(err) // specific error goes to Gin's log; the client message stays uniform
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query comments"})
        return
    }

    // 4. Then fetch the current page (Order + Offset + Limit)
    if err := query.
        Order("created_at DESC").
        Offset((page - 1) * pageSize).
        Limit(pageSize).
        Find(&comments).Error; err != nil {
        _ = c.Error(err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query comments"})
        return
    }

    c.JSON(http.StatusOK, gin.H{
        "items":    comments,
        "total":    total,
        "page":     page,
        "pageSize": pageSize,
    })
}
```

This is the series' first paginated endpoint; the skeleton is `Count` → `Order` → `Offset` → `Limit`. When the next part, [GORM Media & Query Enhancement](./gorm-gin-media-query), reuses it on the book list, it folds that parsing into a `parsePagination` helper.

> **Why are the error messages uniform?** Both `Count` and `Find` returning "failed to query comments" is deliberate — the client sees a 500 and doesn't need to know which step died (and leaking internals outward is unsafe). What must be distinguished is the **server-side log**: `_ = c.Error(err)` (used on every 500 branch since Part 5) hands the specific error to Gin — the `Logger` inside `gin.Default()` renders it into the access log; in production this upgrades to `slog` + a unified error middleware, see [Engineering (Part 2)](./gorm-gin-engineering-reliability).

**Testing:**

```bash
curl "http://localhost:8080/books/1/comments?page=1&pageSize=10"
# {"items":[...],"total":1,"page":1,"pageSize":10}
```

> **The semantic difference from create/delete:** `CreateComment` / `DeleteComment` first check that the book exists (they return 404 once the book is soft-deleted), but `ListComments` doesn't — after a book is soft-deleted, `GET /books/:id/comments` still returns 200 with the historical comments. That is **the current state, not the final answer**: for the same book, `GET /books/:id` already returns 404 while `GET /books/:id/comments` still returns 200. §2.4 settles it; until then, remember this default semantics — **a list endpoint only fetches rows by condition; it does not decide whether the parent resource exists**.

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

### 2.4 After the Parent Is Soft-Deleted, What Happens to the Children?

Let's lay out the current state — once a book is soft-deleted, the four endpoints disagree:

| Endpoint | Response after the book is soft-deleted | Why |
| -------- | --------------------------------------- | --- |
| `GET /books/:id` | 404 | `First(&book, id)` carries `deleted_at IS NULL` |
| `POST /books/:id/comments` | 404 | checks that the book exists first |
| `DELETE /books/:id/comments/:cid` | 404 | same |
| `GET /books/:id/comments` | **200 + historical comments** | it only queries `comments` and never touches `books` (see the 2.2 note) |

To make "the book is gone, so are its comments", there are two routes. **Pick one first, then write code.**

#### How to choose: four questions

- **Do the children have a lifecycle of their own?** Comments do — authors delete their own, and the platform may hide one after a report. If yes, don't let them vanish with the book;
- **Is restoring the parent likely?** If so (unpublish and republish, undo a mistaken delete), don't cascade — otherwise "restore the book" turns into "compensate every comment";
- **Is the child table bounded?** Comments aren't (a popular book can have tens of thousands), so cascading turns one delete into ten thousand UPDATEs; order items (a few dozen rows that are meaningless without the order) cascade very well;
- **Can you guarantee every read path checks the parent?** If the consumers are many and beyond your control (legacy endpoints, internal reports, exports), the write-side cascade is the sturdier net.

Two default routes:

- **Content-type children** (comments, tags, likes): soft-deleting the parent touches only its own row, and visibility is decided on the **read side**. Deletion is O(1), restoring the parent restores all children, and children keep their own lifecycle;
- **Composition-type children** (order items, draft lines, invoice lines): a child record has no meaning without its parent → cascade on the write side, and you **must** wrap it in a transaction, implement restore in pairs, and keep a read-side gate anyway.

This part's comments are the former, so the **main line is read-side**; the write-side cascade is the alternative, at the end of this section.

#### Main line: check the parent on the read side

Add one existence check at the top of `ListComments`, aligned with `CreateComment` / `DeleteComment`:

```go
// 1. The parent must exist: once the book is soft-deleted this 404s, so the children are invisible
var book models.Book
if err := db.DB.WithContext(c.Request.Context()).First(&book, id).Error; err != nil {
    if errors.Is(err, gorm.ErrRecordNotFound) {
        c.JSON(http.StatusNotFound, gin.H{"error": "book not found"})
        return
    }
    _ = c.Error(err)
    c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
    return
}
```

> If you prefer "200 with an empty list", swap this check for a join instead: `Joins("JOIN books ON books.id = comments.book_id AND books.deleted_at IS NULL")` — one statement, one round trip saved, at the cost of not being able to tell "the book does not exist" from "the book has no comments".

#### Turn the gate into middleware: one place, every child route

Copying the check into each handler fails predictably: today you remember `ListComments`, tomorrow a new `/books/:id/tags` may miss it. "The parent must exist" is a rule for a route group, so express it once as middleware.

**Step one: write the middleware** (`handlers/middleware.go`):

```go
// requireBook: the parent check for every child route on the /books/:id group.
// A missing (or soft-deleted) book 404s right here and aborts; otherwise the book is
// stashed in the context for the handler to reuse.
func requireBook(c *gin.Context) {
    var book models.Book
    if err := db.DB.WithContext(c.Request.Context()).First(&book, c.Param("id")).Error; err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            c.JSON(http.StatusNotFound, gin.H{"error": "book not found"})
            c.Abort()
            return
        }
        _ = c.Error(err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
        c.Abort()
        return
    }
    c.Set("book", &book) // grab it later instead of querying again: book := c.MustGet("book").(*models.Book)
    c.Next()
}
```

**Step two: mount the child routes on the group** (`main.go`; the group path is `/books/:id`, so inside it you write only the remaining segment — the comment on the right is the full path it produces):

```go
// -- (1) Collection routes and the book itself: keep the crash course's form, stay out of the group --
r.POST("/books", handlers.CreateBook)       // create
r.GET("/books", handlers.GetBooks)          // list
r.GET("/books/:id", handlers.GetBook)       // detail (does its own First + Preload and 404)
r.PUT("/books/:id", handlers.UpdateBook)    // update (checks existence itself)
r.DELETE("/books/:id", handlers.DeleteBook) // delete (404 comes from RowsAffected)

// -- (2) Child resources of a book: mounted on the group, checked by requireBook --
bookGroup := r.Group("/books/:id")
bookGroup.Use(requireBook)

// Comments (this part)
bookGroup.GET("/comments", handlers.ListComments)          // GET    /books/:id/comments
bookGroup.POST("/comments", handlers.CreateComment)        // POST   /books/:id/comments
bookGroup.DELETE("/comments/:cid", handlers.DeleteComment) // DELETE /books/:id/comments/:cid

// Tags (Part 5, protected by the same parent check)
bookGroup.POST("/tags", handlers.AddBookTag)               // POST   /books/:id/tags
bookGroup.DELETE("/tags/:tid", handlers.RemoveBookTag)     // DELETE /books/:id/tags/:tid

// Hard delete (the optional endpoint from the crash course's Part 8)
bookGroup.DELETE("/permanent", handlers.DeleteBookPermanently) // DELETE /books/:id/permanent
```

Three points:

- **Inside a group the path is a relative segment**: `bookGroup.GET("/comments", ...)` ends up as `/books/:id/comments`, matching the list in 2.5 one to one — do not write `"/books/:id/comments"` there, or you get `/books/:id/books/:id/comments`;
- **The middleware must `c.Abort()`**: once it has written a response it has to stop the chain, or the handler runs anyway and writes a second response (`headers already written` in the log);
- **`/books/:id` itself stays out of the group**: the detail endpoint does its own query and 404; `DELETE /books/:id` decides 404 from `RowsAffected`, so neither needs the parent pre-check. **Do not put `GET /books/:id` into the group either** (`bookGroup.GET("", ...)`) — that re-registers block (1) above and Gin panics at startup;
- **The parameter name must match everywhere**: the group uses `:id`, so the original single routes must too. Write `:bookId` anywhere and startup panics with `':bookId' in new path … conflicts with existing wildcard ':id'`.
- **`PUT /books/:id` is optional here**: to save one query, move it into the group too — the middleware has already loaded the book, so `book := c.MustGet("book").(*models.Book)` feeds `Model(&book).Updates(...)` directly and that whole `First` + 404 block goes away (delete the `r.PUT(...)` from block (1) as well, or you register the route twice). `GET /books/:id` should not join (it needs `Preload("Comments")`; the middleware's plain `First` cannot provide it, so using it means querying twice), and neither should `DELETE /books/:id` (its 404 comes from `RowsAffected`, so joining the group adds a SELECT to every delete). The trade-off: inside the group the middleware queries the book **before** the body is bound, so a bad request that ends in 400 still costs one wasted SELECT.

With the middleware in place, the manual checks inside `CreateComment` / `DeleteComment` / `ListComments` can all go — that is what makes the middleware form worth it. The route tree is unaffected too: the group path shares the same parameter name (`:id`) as the existing `/books/:id`, so they coexist.

#### Alternative: cascade the soft delete (composition-type children only)

If you decide this book's comments vanish with it, soft-delete the child table inside a transaction. Here is the full implementation, replacing the crash course's `DeleteBook`:

```go
// DeleteBook soft-deletes a book and soft-deletes its comments along with it (DELETE /books/:id)
func DeleteBook(c *gin.Context) {
    id := c.Param("id")

    result := db.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
        // 1. Soft-delete the book: 0 rows means it does not exist (or is already deleted) — map to 404 below
        r := tx.Delete(&models.Book{}, id)
        if r.Error != nil {
            return r.Error
        }
        if r.RowsAffected == 0 {
            return gorm.ErrRecordNotFound
        }
        // 2. Soft-delete its comments: Comment embeds gorm.Model too, so this is a soft delete as well
        return tx.Where("book_id = ?", id).Delete(&models.Comment{}).Error
    })

    if errors.Is(result.Error, gorm.ErrRecordNotFound) {
        c.JSON(http.StatusNotFound, gin.H{"error": "book not found"})
        return
    }
    if result.Error != nil {
        _ = c.Error(result.Error)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "delete failed"})
        return
    }
    c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}
```

On the database this is two UPDATEs — either both commit or both roll back:

```sql
UPDATE books    SET deleted_at = now() WHERE id = 3 AND deleted_at IS NULL;
UPDATE comments SET deleted_at = now() WHERE book_id = 3 AND comments.deleted_at IS NULL;
```

Accept the costs before choosing it:

- **Deletion cost grows with the child table**: a book with 10,000 comments means 10,000 UPDATEs per delete, and long transactions/locks are your problem (batch it if needed);
- **Restore must be implemented in pairs**: restoring the book has to restore the comments too, or you end up with "book back, comments still deleted";
- **It pollutes the data state**: in audits and reports, "comments during the unpublish window" become "never existed" — content-type data usually needs to stay traceable;
- **Use `Delete(&models.Comment{})`, not a hand-written `Update("deleted_at", time.Now())`**: the latter bypasses GORM's soft-delete logic (no hooks, no `updated_at` upkeep);
- **Hard deletion is still a separate route**: `/books/:id/permanent` runs `Unscoped().Delete`, and the `ON DELETE CASCADE` foreign key really removes the comment rows (Part 5, §3.4) — **only a real delete should rely on database cascading**.

**Testing:**

```bash
# Main line (read-side check): the child endpoint 404s once the book is soft-deleted
curl -X DELETE http://localhost:8080/books/3
curl "http://localhost:8080/books/3/comments"   # 404 (an empty list with the join version)
psql -c "SELECT id, deleted_at FROM comments WHERE book_id = 3"  # NULL — comment data intact

# Alternative (write-side cascade): comments are soft-deleted together with the book
curl -X POST http://localhost:8080/books/3/comments \
  -H "Content-Type: application/json" -d '{"content":"checking the cascade"}'
curl -X DELETE http://localhost:8080/books/3
psql -c "SELECT id, deleted_at FROM comments WHERE book_id = 3"  # deleted_at now carries a timestamp
```

### 2.5 Route Registration

Put these three lines into `main.go`'s route block (all other routes stay as in the crash course):

```go
r.POST("/books/:id/comments", handlers.CreateComment)
r.GET("/books/:id/comments", handlers.ListComments)
r.DELETE("/books/:id/comments/:cid", handlers.DeleteComment)
```

> If you adopt the `requireBook` middleware from 2.4, these three lines move onto the `r.Group("/books/:id")` group (writing only relative segments like `/comments` and `/comments/:cid`) — the final paths are unchanged, with the parent check added. Part 5's tag routes join the same group.

---

## 3. Reading Relationships: Preload for One-to-Many

**Goal:** make the detail endpoint load a book's comments on demand with `Preload("Comments")` — the heart of reading one-to-many, and the genuinely new knowledge of this part.

### 3.1 Loading Comments On Demand in the Detail Endpoint (Bounded)

`GetBook` in `handlers/book.go` gains a `Preload`: the detail carries a preview of the **latest 5** comments, and the full list stays on 2.2's paginated endpoint.

```go
// Returns one book with a preview of its 5 latest comments
func GetBook(c *gin.Context) {
    id := c.Param("id")

    var book models.Book
    result := db.DB.WithContext(c.Request.Context()).
        Preload("Comments", func(tx *gorm.DB) *gorm.DB {
            return tx.Order("created_at DESC").Limit(5) // preview the 5 latest; never dump them all
        }).
        First(&book, id)
    if errors.Is(result.Error, gorm.ErrRecordNotFound) {
        c.JSON(http.StatusNotFound, gin.H{"error": "book not found"})
        return
    }
    if result.Error != nil {
        _ = c.Error(result.Error)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
        return
    }

    c.JSON(http.StatusOK, book)
}
```

`Preload("Comments")` makes a single `First` also fetch that book's comments: GORM queries `books` first, then fetches `comments` by `book_id` in one bulk query and assembles the result — **no N+1** (two SQL statements assembled once, not row-by-row queries). The second argument to `Preload` is where you put **conditions on the sub-query**: here it orders newest-first and takes 5.

> **Keep the three endpoints' jobs apart:** detail `GET /books/:id` = a 5-comment preview; the full paginated list = 2.2's `GET /books/:id/comments` (it carries `total`, which is what makes paging possible); the per-book comment count on the list page = the next part's JOIN + GROUP BY. **The detail never embeds everything** — comments are unbounded (a popular book can have tens of thousands), so embedding them all makes the response grow linearly with the comment count, and once that contract ships, tightening it is a breaking change.

> **The `Limit`-inside-`Preload` trap:** it applies to **the sub-query as a whole**, not "5 per book". So this shape fits a single-book detail (`First`); if you ever write `Preload("Comments", ...Limit(5))` on a multi-book list, you get the global first 5 rows, not 5 per book.

> **List endpoints don't Preload:** `GetBooks` stays exactly as it is (no comments); only the detail loads them (the reasoning is in the 1.2 note "Why doesn't `Comments` appear in list responses?"), and what it loads is a **bounded preview**, not everything.

**Testing:**

```bash
curl http://localhost:8080/books/1
# the detail response includes "comments":[...], at most 5 (newest first)

curl "http://localhost:8080/books/1/comments?page=2&pageSize=10"
# for paging and the total count, call this one
```

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
