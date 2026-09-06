---
title: "GORM Data Engineering: Batch Import, Request DTOs & Validation-Error Translation"
description: "Part 4: batch-import from a data file (CreateInBatches), separates request DTOs from models with parameterized validation rules, and translates validator errors into clear client messages. Ends with the series overview, the full route table, and what comes next."
date: 2026-09-04
series: gin-gorm
tags:
  - Go
  - PostgreSQL
  - ORM
---

> **Prerequisites:** finish the [crash course](./gorm-gin-crud-tutorial), the [relations part](./gorm-gin-relations), and the [media & query part](./gorm-gin-media-query) — the project with `books` + `comments` + cover images + pagination/search. Code conventions are the same as the crash course (`WithContext` / `errors.Is` for 404 / the 400 · 404 · 201 semantics).

This is Part 4 of the series. The first three parts completed the "API features": CRUD, relations, uploads and pagination; this part turns to **engineering method**: where the data comes from (batch import), where the API boundary closes (request DTOs), and how a failed validation becomes something a client can understand (error translation).

---

## 1. Batch Import: From Data File to Database

The real use cases of `CreateInBatches` are **back-office bulk import, data initialization and test-data seeding** — not everyday CRUD. So the teaching takes the real shape too: the data lives in `seed/books.json`, and the handler reads the file, parses it, and inserts in batches.

`seed/books.json` (create it at the project root):

```json
[
  {
    "title": "The Go Programming Language",
    "author": "Donovan & Kernighan",
    "price": 4990
  },
  { "title": "Go in Action", "author": "William Kennedy", "price": 5900 },
  {
    "title": "Concurrency in Go",
    "author": "Katherine Cox-Buday",
    "price": 4600
  },
  { "title": "Cloud Native Go", "author": "Matthew Titmus", "price": 5200 },
  { "title": "100 Go Mistakes", "author": "Teiva Harsanyi", "price": 4800 }
]
```

New in `handlers/book.go` (imports are added **per file**: you need `encoding/json` and `os`; if the function uses `fmt.Sprintf`, `fmt` must be added to that file as well — Go's imports are file-scoped; cover.go having them doesn't mean book.go can use them):

```go
// SeedBooksFromFile batch-imports books from seed/books.json (back-office / data-initialization scenario)
func SeedBooksFromFile(c *gin.Context) {
    // 1. Read the data file
    data, err := os.ReadFile("seed/books.json")
    if err != nil {
        _ = c.Error(err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read data file"})
        return
    }

    // 2. Parse the JSON into a slice (fields aligned by json tags; amounts in cents, see the note below)
    var books []models.Book
    if err := json.Unmarshal(data, &books); err != nil {
        _ = c.Error(err)
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid data file format"})
        return
    }
    if len(books) == 0 {
        c.JSON(http.StatusBadRequest, gin.H{"error": "data file is empty"})
        return
    }

    // 3. Insert in batches of 100
    if err := db.DB.WithContext(c.Request.Context()).CreateInBatches(books, 100).Error; err != nil {
        _ = c.Error(err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "batch insert failed"})
        return
    }

    c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("imported %d books", len(books))})
}
```

`CreateInBatches(slice, 100)` inserts in batches of 100 rows — however much data there is, it never becomes one giant INSERT statement.

> A side insight: the data file's field names are exactly the model's `json` tags (`title` / `author` / `price`), so "loading the file" is essentially **deserializing into the model** — amounts are written directly in cents in the file (`4990` = 49.90 yuan), consistent with the crash course's money convention.
>
> **Note:** `os.ReadFile("seed/books.json")` is relative to the current working directory — the server **must be started from the project root** (`go run main.go`), otherwise the file won't be found. In production, paths like this should become configurable.

**Route registration:** add one line to `main.go`'s route block:

```go
r.POST("/books/bulk", handlers.SeedBooksFromFile)
```

**Testing:**

```bash
curl -X POST http://localhost:8080/books/bulk
# {"message":"imported 5 books"}

# Use the media & query part's pagination/search to confirm the real data landed
curl "http://localhost:8080/books?q=Go&pageSize=5"
# items should include The Go Programming Language, Go in Action, etc.
```

> ⚠️ **Batch import is not idempotent:** a repeated POST inserts the same batch of books again (the code has no dedup/truncate logic). Its role is a "data initialization / test seeding" tool — before running it again, decide whether to empty `books` first (e.g. `TRUNCATE books RESTART IDENTITY`).

---

## 2. Request DTOs: Separating What You Accept from What You Store

When creating comments in the [relations part](./gorm-gin-relations), `input` (only `nickname` / `content`) and `models.Comment` (which also carries `BookID` and `gorm.Model`) were already separated. The crash course's "advanced: adding parameter validation" also gave the reason — **validation tags go on the DTO, not on the model**, because models get reused by update endpoints and `required` would block partial updates. This part makes it official with book creation, demonstrated more thoroughly:

```go
// Request DTO: declares only the fields the API is "willing to accept" + validation rules.
// Price is *int: binding:"required" treats int's 0 as "missing"
// (validator regards zero values as not provided); a pointer can tell "not sent" from "sent 0".
type createBookInput struct {
    Title  string `json:"title" binding:"required"`
    Author string `json:"author" binding:"required"`
    Price  *int   `json:"price" binding:"required,gte=0,lte=1000000"` // unit: cents, required, 0–10000 yuan
}

// CreateBook's reworked binding section:
var input createBookInput
if err := c.ShouldBindJSON(&input); err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": "please send valid JSON"})
    return
}
book := models.Book{Title: input.Title, Author: input.Author, Price: *input.Price}
```

- `binding:"required,gte=0,lte=1000000"`: price is required and `0 <= price <= 1000000` (i.e. 0–10000 yuan, in cents) — out of range is a straight 400. **Why is `Price` a `*int`?** `required` treats the int zero value `0` as "field missing" too, so `{"price":0}` would be blocked with a 400; switching to `*int`, "not sent" is `nil` (400) while "sent 0" is `&0` (passes) — exactly the pointer semantics the crash course explained: **`nil` means "this field never appeared"**;
- The DTO's second benefit: a negative `price` or an `"abc"` never gets into the model — **the API boundary closes at the DTO layer, not at the model layer**;
- Why didn't the crash course do this? Because at the single-table entry level "what you accept = what you store" — learn GORM itself first. Only now, at the multi-table + validation stage, does the explicit separation become necessary (this is the series' recurring "teaching gradient": each hard topic is spread across parts, one rung per part — the earlier text wasn't wrong, the gap was deliberate);
- **Behavior-change note:** the crash course's advanced version, `Price int` + `binding:"gte=0"`, let a missing price pass through (created as 0); this part tightens it to `*int` + `required` — old requests that omit `price` (only `title` / `author`) got 201 in the crash course and now get 400. The same endpoint, different semantics per stage: the tutorial deliberately turns "missing means success" into "explicitly required" — not a typo.

**Testing:**

> ⚠️ The **translated messages** asserted in the curl expectations below only appear from Section 2.1 on — this section's handler still returns the generic wording "please send valid JSON" (see the binding code above). Understand the pointer semantics here first; the translated output materializes in 2.1.

```bash
# Send 0: the pointer field passes; 201, created
curl -X POST http://localhost:8080/books \
  -H "Content-Type: application/json" \
  -d '{"title":"x","author":"y","price":0}'          # 201

# Missing price: required fires (pointer is nil); 400
curl -X POST http://localhost:8080/books \
  -H "Content-Type: application/json" \
  -d '{"title":"x","author":"y"}'                    # 400: {"error":"missing required field Price"}
```

### 2.1 Validation-Error Translation: From a Bare 400 to a Meaningful 400

When `ShouldBindJSON` validation fails, the returned error isn't an ordinary string but `validator.ValidationErrors` (under the hood Gin uses go-playground/validator) — each entry carries `.Field()` (the field name), `.Tag()` (the rule that fired: `required` / `gte` / `lte`), and `.Param()` (the rule's argument, e.g. `1000000`). Break it apart, and you can return genuinely useful messages per field and per rule:

```go
// Approach 1: report only the first validation error (say one thing clearly at a time)
if err := c.ShouldBindJSON(&input); err != nil {
    var ve validator.ValidationErrors
    if errors.As(err, &ve) && len(ve) > 0 { // unwrap the validator error (the classic pattern; works on any version)
        e := ve[0] // only taking the first one, so no loop is needed
        switch e.Tag() {
        case "required":
            c.JSON(http.StatusBadRequest, gin.H{"error": "missing required field " + e.Field()})
        case "gte":
            c.JSON(http.StatusBadRequest, gin.H{"error": e.Field() + " cannot be less than " + e.Param()})
        case "lte":
            c.JSON(http.StatusBadRequest, gin.H{"error": e.Field() + " cannot be greater than " + e.Param()})
        default:
            c.JSON(http.StatusBadRequest, gin.H{"error": "invalid parameter"})
        }
        return
    }
    c.JSON(http.StatusBadRequest, gin.H{"error": "please send valid JSON"}) // not a validation error
    return
}
```

```go
// Approach 2: aggregate every validation error and tell the client about all field problems at once
if err := c.ShouldBindJSON(&input); err != nil {
    var ve validator.ValidationErrors
    if errors.As(err, &ve) && len(ve) > 0 {
        msgs := make([]string, 0, len(ve))
        for _, e := range ve {
            switch e.Tag() {
            case "required":
                msgs = append(msgs, "missing required field "+e.Field())
            case "gte":
                msgs = append(msgs, e.Field()+" cannot be less than "+e.Param())
            case "lte":
                msgs = append(msgs, e.Field()+" cannot be greater than "+e.Param())
            default:
                msgs = append(msgs, "invalid parameter")
            }
        }
        c.JSON(http.StatusBadRequest, gin.H{"errors": msgs}) // return once, outside the loop
        return
    }
    c.JSON(http.StatusBadRequest, gin.H{"error": "please send valid JSON"})
    return
}
```

> **Response-shape note:** `{"errors": [...]}` (an array) is the **first** error-response shape in the series — every endpoint so far returned `{"error": string}`. Main-line endpoints can stay with Approach 1 (a single message); the aggregated version is left for endpoints that want to "tell the client about all field problems at once". If you expose multiple field errors, clients need to handle the `errors` array.
>
> The import needs `github.com/go-playground/validator/v10` added (a gin dependency, reused directly; `errors` is already used in `book.go`, no need to import it again); `.Field()` returns the Go field name (`Price`) — map it to lowercase if you want it to line up with frontend conventions.
>
> **Pitfall: don't put the `return` inside the "iterate over all" loop** — the loop would return after its first iteration and never "aggregate" anything (the semantics degenerate to "report only the first", duplicating Approach 1). Approach 1 states its intent with `ve[0]` ("only the first"); Approach 2 moves the return outside the loop so the loop truly runs to the end. Pick one semantics — don't mix "a `return` inside the loop" with the expectation of aggregation.
>
> **(Optional) generics shorthand:** on newer Go, if `errors.AsType[validator.ValidationErrors](err)` is available (this API is a proposal-level capability that evolves with Go versions — whether it works depends on your Go version's docs), it can replace `var ve ...; errors.As(err, &ve)` in one line; when you can't be sure of the environment's version, the classic `errors.As` used in the main text works on any version — this part sticks with the classic form.

**Testing:**

```bash
curl -X POST http://localhost:8080/books \
  -H "Content-Type: application/json" \
  -d '{"title":"x","author":"y","price":-1}'        # 400: {"error":"Price cannot be less than 0"}
curl -X POST http://localhost:8080/books \
  -H "Content-Type: application/json" \
  -d '{"title":"x","author":"y","price":1000001}'    # 400: {"error":"Price cannot be greater than 1000000"}
curl -X POST http://localhost:8080/books \
  -H "Content-Type: application/json" \
  -d '{"title":"x","author":"y"}'                    # 400: {"error":"missing required field Price"}
```

---

## 3. Series Overview

| Part        | File                         | Topic                                | New routes                                                        |
| ----------- | ---------------------------- | ------------------------------------ | ---------------------------------------------------------------- |
| 1           | `gorm-gin-crud-tutorial.md`  | single-table CRUD, soft delete, the zero-value trap | the five `/books` routes + `/books/:id/permanent` (optional)     |
| 2           | `gorm-gin-relations.md`      | relations: comments + Preload        | `/books/:id/comments`, `/books/:id/comments/:cid`                |
| 3           | `gorm-gin-media-query.md`    | uploads / pagination / search / aggregation | `/books/:id/cover`, `/uploads/*` static                    |
| 4 (this part) | `gorm-gin-dto-batch.md`    | batch import / DTO / validation translation | `/books/bulk`                                             |
| 5           | `gorm-gin-tags.md`           | many-to-many: tags + join table      | `/books/:id/tags`, `/tags/:name/books`                           |

Complete route list (in registration order):

| Method | Path                       | Handler                                | Part            |
| ------ | -------------------------- | -------------------------------------- | --------------- |
| POST   | `/books`                   | CreateBook                             | 1               |
| GET    | `/books`                   | GetBooks (pagination + search + comment count) | 1 → 3 upgrade  |
| GET    | `/books/:id`               | GetBook (with Comments)                | 1 → 2 upgrade   |
| PUT    | `/books/:id`               | UpdateBook                             | 1               |
| DELETE | `/books/:id`               | DeleteBook                             | 1               |
| DELETE | `/books/:id/permanent`     | DeleteBookPermanently (optional)       | 1               |
| POST   | `/books/:id/comments`      | CreateComment                          | 2               |
| GET    | `/books/:id/comments`      | ListComments                           | 2               |
| DELETE | `/books/:id/comments/:cid` | DeleteComment                          | 2               |
| POST   | `/books/:id/cover`         | UploadCover                            | 3               |
| GET    | `/uploads/*`               | `r.Static` static serving              | 3               |
| POST   | `/books/bulk`              | SeedBooksFromFile                      | 4 (this part)   |
| POST   | `/books/:id/tags`          | AddBookTag                             | 5               |
| DELETE | `/books/:id/tags/:tid`     | RemoveBookTag                          | 5               |
| GET    | `/tags/:name/books`        | GetBooksByTag                          | 5               |

## 4. Engineering Checklist: Landed in the Engineering Parts

The items below were foreshadowed at various points earlier in the series, and have now landed respectively in [GORM Engineering in Practice (Part 1): Layering, Dependency Injection & Testability](./gorm-gin-engineering-layering) and [GORM Engineering in Practice (Part 2): Reliability & Production Readiness](./gorm-gin-engineering-reliability):

- **Layering & testing:** a `BookRepository` interface + a Service layer + `httptest` table-driven tests (Engineering Part 1, Sections 1 & 2);
- **Productivity wrapper:** the generic `GetPaginated[T]` centralizes pagination and error aggregation (Engineering Part 1, Section 3);
- **Stronger file validation:** header sniffing with `http.DetectContentType` guards against forged file extensions (Engineering Part 2, Section 2);
- **Sort whitelist:** external sort fields are whitelisted to shut down ORDER injection (Engineering Part 2, Section 2);
- **Object storage:** an `Uploader` interface abstraction, switchable between Disk / S3, with the key stored in the database (Engineering Part 2, Section 3).
