---
title: "GORM Data Engineering: Batch Import, Request DTOs & Validation-Error Translation"
description: "Part 4: batch-import from a data file (CreateInBatches), separates request DTOs from models with parameterized validation rules, translates validator errors into clear client messages, and covers update DTOs (PUT/PATCH plus the zero-value trap). Ends with a route quick reference and what comes next."
date: 2026-09-04
series: gin-gorm
order: 4
tags:
  - Go
  - PostgreSQL
  - ORM
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

New in `handlers/book.go` (add `encoding/json`, `os`, `fmt`, and `github.com/go-playground/validator/v10` to that file's imports):

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

    // 2. Parse into the DTO first (not the model): keys in the file can only hit fields the DTO declares
    var inputs []createBookInput
    if err := json.Unmarshal(data, &inputs); err != nil {
        _ = c.Error(err)
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid data file format"})
        return
    }
    if len(inputs) == 0 {
        c.JSON(http.StatusBadRequest, gin.H{"error": "data file is empty"})
        return
    }

    // 3. File data obeys the same rules: json.Unmarshal does NOT run binding tags, so run the validator by hand
    v := validator.New()
    books := make([]models.Book, 0, len(inputs))
    for i, in := range inputs {
        if err := v.Struct(in); err != nil {
            _ = c.Error(err)
            c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("row %d is invalid", i+1)})
            return
        }
        books = append(books, models.Book{Title: in.Title, Author: in.Author, Price: in.Price})
    }

    // 4. Insert in batches of 100
    if err := db.DB.WithContext(c.Request.Context()).CreateInBatches(books, 100).Error; err != nil {
        _ = c.Error(err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "batch insert failed"})
        return
    }

    c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("imported %d books", len(books))})
}
```

`CreateInBatches(slice, 100)` inserts in batches of 100 rows — however much data there is, it never becomes one giant INSERT statement.

> **`json.Unmarshal` does not run `binding` tags.** `binding` belongs to gin's layer and only fires inside `ShouldBindJSON`; for file data to obey the same rules you run `validator.Struct(...)` by hand, as step 3 does. That is also why the error names the row number — seed files routinely hold hundreds of rows, and an error without an index is no error at all.
>
> **Why not just call `c.ShouldBindJSON`?** It only reads from `c.Request.Body`, and this data lives in a file on disk (`POST /books/bulk` has an empty body). Stuffing the file into `c.Request.Body` and calling it does work, but it breaks any later middleware that reads the body, and it reports "the file content is invalid" as "the request body is malformed".
>
> **Careful: `validator.New()` is not the same instance gin uses.** Both are `go-playground/validator/v10`, so the rules match; but if the project registered custom validations or tag names (`binding.Validator.Engine().(*validator.Validate).RegisterValidation(...)`), `validator.New()` throws all of that away — and the "request-body validation" and "file validation" paths silently diverge. Share gin's instance when you need them to agree: `v := binding.Validator.Engine().(*validator.Validate)`.

> A side insight: the data file's field names are exactly the **DTO's** `json` tags (`title` / `author` / `price`), so "loading the file" is essentially **deserializing into the DTO and mapping to the model** — the same rule as Section 2's "what you accept and what you store are two different things". Amounts are written directly in cents in the file (`4990` = 49.90 yuan), consistent with the crash course's money convention.
>
> **Note:** `os.ReadFile("seed/books.json")` is relative to the current working directory — the server **must be started from the project root** (`go run main.go`), otherwise the file won't be found. In production, paths like this should become configurable.

**Route registration:** add one line to `main.go`'s route block:

```go
r.POST("/books/bulk", handlers.SeedBooksFromFile)
```

> After the layering refactor this handler is called `Seed` (file reading moved into the Service) — see [Engineering (Part 1)](./gorm-gin-engineering-layering).

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

## 2. Request DTOs, Last Mile: `required` vs. Zero Values, and Error Translation

Part 5 of the crash course already settled "validation goes on the DTO, not on the model", and the relations part replayed it once with the comment `createCommentInput` — **so the DTO itself is not re-taught here**. This section only adds what the earlier parts left out: why `Price` has to become `*int` (how `required` treats zero values), and how to turn a bare 400 into field-level errors (2.1). The update half is in section 2.2 of this part.

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

- `binding:"required,gte=0,lte=1000000"`: price is required and `0 <= price <= 1000000` (i.e. 0–10000 yuan, in cents) — out of range is a straight 400. **Why is `Price` a `*int`?** `required` treats the int zero value `0` as "field missing" too, so `{"price":0}` would be blocked with a 400; switching to `*int`, "not sent" is `nil` (400) while "sent 0" is `&0` (passes) — the first place where the crash course's mental-model-2 pointer semantics ("`nil` means not sent") actually earns its keep;
- **Behavior-change note:** the crash course's Part 5 version used `Price int` (a missing price was created as 0, 201); this part tightens it to `*int` + `required`, so old requests that omit `price` now get 400. A deliberate tightening, not a typo.

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

### 2.2 Update DTOs: PUT / PATCH and the Zero-Value Trap

A create DTO only decides "which fields do we accept". An update DTO must answer one more question: **is this endpoint a full replacement (PUT) or a partial update (PATCH)?** The answer changes how the zero-value trap gets solved.

The trap itself: when `Updates` receives a struct, GORM ignores zero-value fields (`0`, `""`, `false`), so `{"price":0}` is silently skipped. **The root cause is the binding layer, not GORM** — when non-pointer fields receive JSON, "not sent" and "sent as zero" collapse into the same `0`, and the information is gone the moment you bind. Pick one of the three routes by semantics; every SQL below was actually produced by GORM v1.31.2 under `DryRun` (re-check with the SQL logger after you change anything):

| Writing (payload `{"price":0}`)                              | SQL actually generated                                          | Fits                   |
| ------------------------------------------------------------ | --------------------------------------------------------------- | ---------------------- |
| `Updates(models.Book{Price: 0})`                             | `SET author=...` (zero value dropped)                           | ✗ the trap             |
| `Select("title","author","price").Updates(models.Book{...})` | `SET title="",author=...,price=0,updated_at=...`                | PUT                    |
| `Updates(updateBookInput{Price: &0})`                        | `SET price=0` (nil fields skipped, but `updated_at` not bumped) | PATCH                  |
| `Updates(updates(&in))` (reflection helper → map)            | `SET price=0,updated_at=...`                                    | PATCH + auto timestamp |

#### Route 1: a real PUT — the `Select` whitelist (simplest)

PUT means "replace the whole resource": the client sends everything, the server writes every column back, so zero values are legitimate — **no pointers needed**, one `Select` line does it:

```go
// non-pointer: PUT requires a full payload, so `required` is right here
type updateBookInput struct {
    Title  string `json:"title" binding:"required"`
    Author string `json:"author" binding:"required"`
    Price  int    `json:"price" binding:"gte=0,lte=1000000"`
}

var in updateBookInput
if err := c.ShouldBindJSON(&in); err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON (title and author are required)"})
    return
}

// Select lists the columns that take part in the update: listed columns are written even at zero,
// and the same list doubles as the whitelist
result := db.DB.WithContext(c.Request.Context()).Model(&book).
    Select("title", "author", "price").
    Updates(models.Book{Title: in.Title, Author: in.Author, Price: in.Price})
```

- **One `Select` closes two holes at once**: zero values get written, and only whitelisted columns are touched (`deletedAt` / `coverUrl` can't sneak in — mass assignment blocked).
- The cost is PUT's own semantics: omitting `title` means "clear the title", not "leave it alone" — which is exactly why `required` belongs here.
- The strings in `Select` are **column names**: a typo doesn't error, it just silently does nothing. Glance at the SQL log after editing.

#### Route 2: a real PATCH — hand the pointer DTO straight to `Updates`

PATCH has to express "not sent means don't touch", and only pointers can preserve that (`nil` = not sent, `&0` = sent as 0 — the same pointer semantics this section used for the create DTO's `*int`). You do **not** need a hand-written map: hand the struct to `Updates` as is:

```go
type updateBookInput struct {
    Title  *string `json:"title"`
    Author *string `json:"author"`
    Price  *int    `json:"price" binding:"omitempty,gte=0,lte=1000000"` // no `required` here for updates
}

var in updateBookInput
if err := c.ShouldBindJSON(&in); err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
    return
}
db.DB.WithContext(c.Request.Context()).Model(&book).Updates(in)
```

Measured semantics: `{"price":0}` → `SET price=0`; `{}` (all nil) → **no SQL at all** (`RowsAffected == 0`, no error; returning 200 or 400 is your call).

Two costs you must know about:

1. **`updated_at` stops being bumped**: the struct you pass is not the model type, so GORM's automatic timestamp logic does not engage (measured on v1.31.2; trust the SQL log across versions). Use Route 3 to keep it.
2. **Field names must map onto columns**: `Title` → `title` hits the default naming strategy; something like `CoverURL` needs `gorm:"column:cover_url"` (the `json` tag plays no part in GORM's mapping).

> **⚠️ Never mix a pointer DTO with `Select`**: `Select("title","author","price").Updates(in)` writes NULL into **fields that were not sent** (`SET title=NULL,author=NULL,price=0`) — that wipes data. `Select` goes with the non-pointer DTO only (Route 1).

#### Route 3: PATCH plus automatic `updated_at` — a reflection helper written once

Fold the repetitive "pointer DTO → map" step into one function (imports: `reflect` / `strings`), and every update endpoint's call site collapses to a single line:

```go
// updates: collect the fields actually sent in a pointer DTO into a map keyed by json name
// (keys must match column names to be directly updatable)
func updates(v any) map[string]any {
    out := map[string]any{}
    rv, rt := reflect.ValueOf(v).Elem(), reflect.TypeOf(v).Elem()
    for i := 0; i < rt.NumField(); i++ {
        if f := rv.Field(i); f.Kind() == reflect.Ptr && !f.IsNil() {
            out[strings.Split(rt.Field(i).Tag.Get("json"), ",")[0]] = f.Elem().Interface()
        }
    }
    return out
}

db.DB.WithContext(c.Request.Context()).Model(&book).Updates(updates(&in)) // SET price=0, updated_at=...
```

Because it takes the map branch, `updated_at` is bumped again — and no type guarantees are lost (`f.Elem().Interface()` is still an `int` / `string`). [GORM Engineering (Part 1)](./gorm-gin-engineering-layering) collects this kind of cross-cutting helper into shared infrastructure.

> **Two meanings of `nil`, keep them apart:** in the create DTO `Price *int` exists so that `binding:"required"` can tell "price was never sent" (`nil` → 400); in the update DTO pointers exist so that "not sent" (skip) is distinguishable from "sent as 0" (write 0). Same pointer semantics, two different purposes — and never put `required` on an update DTO, or partial updates turn into full-payload validation.

**Testing:**

```bash
# Route 1: full PUT replacement — the 0 really does get written
curl -X PUT http://localhost:8080/books/1 -H "Content-Type: application/json" \
  -d '{"title":"","author":"New Author","price":0}'

# Route 2/3: PATCH, send only what changes; a follow-up GET should show price=0
# (and with Route 3, a bumped updated_at)
curl -X PATCH http://localhost:8080/books/1 -H "Content-Type: application/json" \
  -d '{"price":0}'
```

---

## 3. Route Quick Reference

Every route registered across the series, in registration order (the last column is the part that introduced it):

| Method | Path                       | Handler                                        | Part          |
| ------ | -------------------------- | ---------------------------------------------- | ------------- |
| POST   | `/books`                   | CreateBook                                     | 1             |
| GET    | `/books`                   | GetBooks (pagination + search + comment count) | 1 → 3 upgrade |
| GET    | `/books/:id`               | GetBook (with a 5-comment preview)             | 1 → 2 upgrade |
| PUT    | `/books/:id`               | UpdateBook                                     | 1             |
| DELETE | `/books/:id`               | DeleteBook                                     | 1             |
| DELETE | `/books/:id/permanent`     | DeleteBookPermanently (optional)               | 1             |
| POST   | `/books/:id/comments`      | CreateComment                                  | 2             |
| GET    | `/books/:id/comments`      | ListComments                                   | 2             |
| DELETE | `/books/:id/comments/:cid` | DeleteComment                                  | 2             |
| POST   | `/books/:id/cover`         | UploadCover                                    | 3             |
| GET    | `/uploads/*`               | `r.Static` static serving                      | 3             |
| POST   | `/books/bulk`              | SeedBooksFromFile                              | 4 (this part) |
| POST   | `/books/:id/tags`          | AddBookTag                                     | 5             |
| DELETE | `/books/:id/tags/:tid`     | RemoveBookTag                                  | 5             |
| GET    | `/tags/:name/books`        | GetBooksByTag                                  | 5             |

## 4. Engineering Checklist: Landed in the Engineering Parts

Everything foreshadowed earlier in the series landed in [Engineering (Part 1)](./gorm-gin-engineering-layering) (layering, injection, tests, generic pagination) and [Engineering (Part 2)](./gorm-gin-engineering-reliability) (error middleware, sort whitelist, file-header sniffing, object storage, connection pool). The item-by-item reconciliation table is in [Engineering (Part 2) §4.2](./gorm-gin-engineering-reliability).
