---
title: "GORM Engineering in Practice (Part 2): Reliability & Production Readiness"
description: "Part 7 (series finale): a unified error middleware with ok/fail responses (slog, timeout 504), sort whitelists, file-header sniffing behind an Uploader abstraction, and connection-pool tuning. Ends with a mapping table of every promise the series made and where it landed."
date: 2026-09-06 02:00:00
series: gin-gorm
tags:
  - Go
  - PostgreSQL
  - ORM
  - Engineering
---

> 🎓 **Optional engineering reading:** continues the advanced track of [GORM Engineering in Practice (Part 1)](./gorm-gin-engineering-layering) — production-focused topics for architecture and delivery roles; skipping it doesn't affect the main storyline of the first five parts.

> **Prerequisites:** finish [GORM Engineering in Practice (Part 1)](./gorm-gin-engineering-layering) — a project that is already layered (`internal/` three tiers + constructor injection), whose handlers depend on interfaces, and where `go test ./internal/handler/` passes (covering the migrated interfaces). The code conventions are unchanged (`WithContext` / `errors.Is` for 404 / 400 · 404 · 201).

Layering solved *testability*; this part solves *reliability*. The first five parts left teasers everywhere — unified errors, timeout 504, sort whitelist, file-header sniffing, object storage, connection pool — and this part settles all of them at once.

> **Two-phase teaching approach (let me clear up a doubt first):** the first five parts have handlers hand-writing `c.JSON(error)` with error messages all over the place; this part folds that into "the handler only attaches the error, the middleware translates it uniformly". This is not "we taught it wrong before" — in the tutorial phase the boilerplate must be laid out flat so you can see exactly where each status code comes from; in the engineering phase it gets folded up, so you understand what the folding solves. Both phases are indispensable; this part is phase two.
>
> **Map of this part:** four topics, each unfolded as "motivation → old way → new way → why": (1) unified errors (ok/fail + error middleware + slog + 504); (2) security hardening (sort whitelist, file-header sniffing); (3) storage abstraction (Uploader); (4) connection pool. (3) and (4) are "optional production reading" — skipping them doesn't break the closed loop of (1) and (2).

---

## 1. Unified Error Handling: Middleware + ok/fail + slog

**Goal:** fold the `c.JSON(status, gin.H{"error": ...})` boilerplate scattered across handlers into "the handler only attaches the error, the middleware translates it uniformly", log server-side with `slog`, and map timeout errors to 504.

### 1.1 A Unified Response Shape: ok/fail

So far the series has produced two kinds of responses: bare data on success, `{"error": ...}` on failure. The frontend has to guess every time. Unify them into an explicit shape. First, let's lay the series' **response-shape evolution** out in one table — readers who go part by part will see the "lay flat → fold up" teaching arc; skimmers should treat the newest part as the contract:

| Stage | Part | Success shape | Error shape | Why it changed |
| --- | --- | --- | --- | --- |
| Single-table intro | Crash course | bare object / bare array | `{"error": string}` | Teaching: first see that "what you return is what you get" |
| Pagination | Media & Query (Part 3) | `{items, total, page, pageSize}` | still `{"error": string}` | Lists need pagination metadata — **a breaking change**, whose contract is that part's |
| Multiple validation errors | Data engineering (Part 4) | unchanged | optional new `{"errors": []}` | To tell the client about every field problem at once (a new shape; each endpoint opts in) |
| Final unification | This part (Part 7) | `{"ok": true, "data": …}` | `{"ok": false, "error": …}` | Success/failure made explicit + boilerplate folded up — **the final contract** |

So from this part on, success goes through `ok(...)` and failure through `fail(...)`:

```go
// internal/handler/respond.go
package handler

import (
    "net/http"

    "github.com/gin-gonic/gin"
)

func ok(c *gin.Context, status int, data any) {
    c.JSON(status, gin.H{"ok": true, "data": data})
}

func fail(c *gin.Context, status int, msg string) {
    c.JSON(status, gin.H{"ok": false, "error": msg})
}
```

### 1.2 Handlers Only Attach Errors — No More Writing Responses Themselves

The handlers in the layering part already showed the shape (`_ = c.Error(err)` followed by an immediate `return`). Here it becomes the convention:

```go
// the error path in a handler does one thing only: attach the error to the Gin context
if err := h.svc.DeleteBook(c.Request.Context(), id); err != nil {
    _ = c.Error(err) // the concrete status code and the logging are handled by the error middleware
    return
}
ok(c, http.StatusOK, gin.H{"message": "book deleted"})
```

### 1.3 The Error Middleware: Translate + Classify + Log

After `c.Next()` returns, look at all the errors that were attached in one place, translate each kind into a status code, and write `slog` output:

```go
// internal/handler/middleware.go
package handler

import (
    "context"
    "errors"
    "log/slog"
    "net/http"

    "github.com/gin-gonic/gin"
    "gorm.io/gorm"
)

func errorMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        c.Next()

        logList := c.Errors.ByType(gin.ErrorTypePrivate)
        if len(logList) == 0 {
            return // nothing attached (or a success response) — no fallback needed
        }

        // 1. Server-side log: the real cause goes into the log, not into the response
        for _, e := range logList {
            slog.Error("request failed",
                "method", c.Request.Method,
                "path", c.Request.URL.Path,
                "err", e.Err,
            )
        }

        // 2. Status-code classification: 404 (record not found) / 504 (timeout) / anything else 500
        status := http.StatusInternalServerError
        switch {
        case errors.Is(logList.Last().Err, gorm.ErrRecordNotFound):
            status = http.StatusNotFound // 404
        case errors.Is(logList.Last().Err, context.DeadlineExceeded):
            status = http.StatusGatewayTimeout // 504: the server-side timeout (requestTimeout) expired. A client disconnect is context.Canceled — it doesn't match here, so it falls to 500
        }

        c.AbortWithStatusJSON(status, gin.H{"ok": false, "error": http.StatusText(status)})
    }
}
```

Register it:

```go
r.Use(gin.Logger(), gin.Recovery())
r.Use(errorMiddleware())
```

> **Two benefits of the error middleware:** first, **status-code semantics are centralized** — the `errors.Is` discrimination logic (404 vs 500 vs 504) is gathered from every handler into one place; the tutorial parts kept the explicit checks so you could see them, and the engineering part is where they get folded up. Second, **logging is separated from the response** — `slog` records the real cause, while the client only receives `"Internal Server Error"`, so no internal details leak (echoing the "Why are the error messages uniform?" note in [GORM Relations in Practice](./gorm-gin-relations)).

**Testing:**

```bash
curl -i http://localhost:8080/books/999   # 404: {"ok":false,"error":"Not Found"}
curl -i -X DELETE http://localhost:8080/books/1/permanent

# Demonstrating 504 (server-side timeout): temporarily shorten main.go's requestTimeout (e.g. to 200ms),
# then make a slow query against books (run SELECT pg_sleep(2); in psql before sending a request); the log should show
# request failed + err=context deadline exceeded, and the response is 504.
# Note: stopping the database is a "connection refused", which yields 500 rather than 504; this part has no path that produces 502.
```

> **`slog` comes from the standard library** (`log/slog`, Go 1.21+): structured logging ships built-in `key=value` output, so a real project no longer patches things together with `fmt.Println`. This is also the payoff of the promise in the [GORM Relations in Practice](./gorm-gin-relations) note that "production upgrades to `slog` + a unified error middleware".

---

## 2. Request Security Hardening: Sort Whitelist & File-Header Sniffing

**Goal:** pay off two security promises — GetBooks accepts external sort fields but only after a whitelist check; upload validation upgrades from an "extension whitelist" to "file-content sniffing".

### 2.1 The GetBooks Sort Whitelist

Media & Query §2.2's note promised it ("external sort fields must be whitelisted first", see [GORM Media & Query Enhancement](./gorm-gin-media-query) §2.2). Now the payoff: the `sort` parameter is mapped through the whitelist first; anything not found is rejected — never spliced straight into `Order()`:

```go
// internal/service/book.go — the sort-field whitelist: keys are external params, values are column names
var bookSortWhitelist = map[string]string{
    "createdAt": "created_at",
    "updatedAt": "updated_at",
    "price":     "price",
    "title":     "title",
}

// ListBooks paginates + searches + sorts through the whitelist
func (s *BookService) ListBooks(ctx context.Context, q, sort, dir string, page, pageSize int) ([]models.Book, int64, error) {
    query := s.repo.Query(ctx, q) // builds the chain with Where already attached (see the repository.List prototype in the layering part)

    order := "created_at DESC" // default
    if col, ok := bookSortWhitelist[sort]; ok {
        if dir == "asc" {
            order = col + " ASC"
        } else {
            order = col + " DESC"
        }
    }
    return s.repo.ListOrdered(ctx, query, order, page, pageSize)
}
```

```bash
curl "http://localhost:8080/books?sort=price&dir=asc&page=1&pageSize=10"   # ascending by price ✓
curl "http://localhost:8080/books?sort=created_at;DROP%20TABLE%20books--"   # outside the whitelist → falls back to the default sort, never reaches SQL ✓
```

> **The point is "never splicing":** the whitelist keeps a mapping table forever between "the user's literal input" and "SQL fragments" — anything not found is simply ignored (falling back to the default sort) instead of erroring or guessing. The `order` variable can never hold any string other than the column name resolved through the whitelist.

### 2.2 File-Header Sniffing for Uploads

Anyone can fake an extension (drop an exe inside `hack.jpg`). `http.DetectContentType` reads the first 512 bytes and judges the "true type" by content:

```go
// same layer as internal/repository, or standalone: the upload check lives at the handler boundary
func sniffImage(r io.Reader) (string, error) {
    buf := make([]byte, 512)
    n, _ := r.Read(buf)

    ctype, _, _ := mime.ParseMediaType(http.DetectContentType(buf[:n]))
    switch ctype {
    case "image/jpeg", "image/png", "image/webp":
        return ctype, nil
    default:
        return "", errors.New("only jpg/png/webp images allowed")
    }
}
```

Wiring it into `UploadCover` has a **detail you must handle**: `sniffImage` reads the first 512 bytes out of the stream, and if you then hand that same `fh` straight to the save step, the file written to disk will **lose its header and be corrupted** (`multipart.File` supports `Seek` — call `fh.Seek(0, 0)` to rewind before saving). This step combines with the extension whitelist, the 2MB cap, and `Uploader.Save` — §3.2 shows a complete, compilable `UploadCover` (sniff → rewind → save, all in one go); for now, just remember the pitfall: what you read, rewind it back.

```bash
# A non-image with a faked extension: 400 used to come from the suffix; now it comes from the content
curl -X POST http://localhost:8080/books/1/cover -F "cover=@/etc/hosts;filename=pic.jpg"
# {"ok":false,"error":"only jpg/png/webp images allowed"}
```

> **Where the two checks sit:** the extension check is the "UX layer" (fast, can give a friendly hint); content sniffing is the "security layer" — it verifies the **magic number in the file header**, which stops "renamed with a new suffix" forgeries, but it is no guarantee of decodability (a PNG header plus arbitrary trailing data passes too). In production you keep both layers; this part's full function likewise keeps the Media & Query part's 2MB size cap (see §3.2).

---

## 3. The Uploader Abstraction: From Disk to Object Storage

**Goal:** pay off the "object storage" promise — use the `Uploader` interface to lift "where the image is stored" out of the business logic; Disk and S3 are merely two implementations. **Interface contract: give it a key, get back a displayable URL/path** — what the database stores is `Save`'s return value (a `/uploads/...` relative address under Disk, a full https URL under S3); the column keeps the Media & Query part's `cover_path`. Whether to also store the raw key is the trade-off covered in the closing note of this section.

### 3.1 Why an Interface?

In the Media & Query part, `UploadCover` nailed storage to the local disk with `c.SaveUploadedFile(file, filepath.Join("uploads", ...))`. Switching to S3 would mean editing the handler — and a handler shouldn't know storage details. Extract an interface:

```go
// internal/storage/uploader.go — one file with everything up front (interface + Disk + S3 in a single file)
package storage

import (
    "context"
    "fmt"
    "io"
    "os"
    "path/filepath"
)

type Uploader interface {
    // Save stores the content of r under key and returns a displayable URL/path (Disk: /uploads/key; S3: the full https URL)
    Save(ctx context.Context, key string, r io.Reader) (string, error)
}
```

The Disk implementation (keeps today's directory semantics, wrapped as an interface implementation):

```go
type DiskUploader struct {
    Dir string // ./uploads
    BaseURL string // /uploads
}

func (d *DiskUploader) Save(ctx context.Context, key string, r io.Reader) (string, error) {
    dst := filepath.Join(d.Dir, key)
    if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
        return "", err
    }
    f, err := os.Create(dst)
    if err != nil {
        return "", err
    }
    defer f.Close()
    if _, err := io.Copy(f, r); err != nil {
        return "", err
    }
    return d.BaseURL + "/" + key, nil
}
```

The S3 skeleton (no real AWS wiring — just the shape and the call site):

```go
type S3Uploader struct {
    Bucket  string
    Region  string
}

func (s *S3Uploader) Save(ctx context.Context, key string, r io.Reader) (string, error) {
    // when building the real implementation: aws-sdk-go-v2's s3.PutObject goes here
    return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", s.Bucket, s.Region, key), nil
}
```

### 3.2 The Handler Only Knows the Interface

After the refactor, `UploadCover` depends only on `storage.Uploader` — business logic and storage are decoupled. First close the three "wiring" gaps (Part 6's `BookHandler` has only one field, `svc`): add `up` to the struct and give the constructor a second parameter; add `SetCover` to the Service (check the book exists first, then update `cover_path`); add the matching single-field update to the repository.

```go
// internal/handler/book.go — the BookHandler struct and constructor (compare Part 6's single-argument version)
type BookHandler struct {
    svc *service.BookService
    up  storage.Uploader // Part 6 had only svc; this part adds the storage abstraction (see 3.1)
}

func NewBookHandler(svc *service.BookService, up storage.Uploader) *BookHandler {
    return &BookHandler{svc: svc, up: up}
}
```

```go
// internal/service/book.go — BookService adds SetCover: first confirm the book exists (404), then update the cover field
func (s *BookService) SetCover(ctx context.Context, id uint, url string) error {
    if _, err := s.repo.FirstBook(ctx, id); err != nil {
        return err
    }
    return s.repo.SetCover(ctx, id, url)
}

// internal/repository/book.go — repo.SetCover single-field update (illustrative):
//   r.db.WithContext(ctx).Model(&models.Book{Model: gorm.Model{ID: id}}).Update("cover_path", url).Error
```

The complete new `UploadCover` (the §2.2 sniffer + the Media & Query part's size cap + the rewound stream header + the 3.1 storage abstraction, all combined in one function):

```go
func (h *BookHandler) UploadCover(c *gin.Context) {
    // 0. Parse the id (invalid → 400)
    id, err := strconv.ParseUint(c.Param("id"), 10, 64)
    if err != nil {
        fail(c, http.StatusBadRequest, "invalid id")
        return
    }
    // 1. The book must exist (the 404 is translated by the error middleware)
    book, err := h.svc.GetBook(c.Request.Context(), uint(id))
    if err != nil {
        _ = c.Error(err)
        return
    }
    // 2. Grab the file
    file, err := c.FormFile("cover")
    if err != nil {
        fail(c, http.StatusBadRequest, "missing file field cover")
        return
    }
    fh, err := file.Open()
    if err != nil {
        fail(c, http.StatusBadRequest, "failed to read file")
        return
    }
    defer fh.Close()

    // 3. Content sniffing: the extension can't be trusted; judge the "true type" by the file header (see §2.2)
    ctype, err := sniffImage(fh)
    if err != nil {
        fail(c, http.StatusBadRequest, err.Error())
        return
    }
    // 4. Keep the size cap (the Media & Query part's 2MB check)
    if file.Size > 2<<20 {
        fail(c, http.StatusBadRequest, "file too large (max 2MB)")
        return
    }
    // 5. Crucial: sniffing already consumed the first 512 bytes — rewind the stream to the file header before saving, or the file on disk is corrupted
    if _, err := fh.Seek(0, 0); err != nil {
        _ = c.Error(err)
        return
    }
    // 6. Hand it to the Uploader to store: the extension comes from the content, never trusting the user's filename suffix
    ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}[ctype]
    key := fmt.Sprintf("%d_%d%s", book.ID, time.Now().UnixNano(), ext)
    url, err := h.up.Save(c.Request.Context(), key, fh)
    if err != nil {
        _ = c.Error(err)
        return
    }
    // 7. What goes into the DB is Save's displayable URL/path (not the raw key); then respond
    if err := h.svc.SetCover(c.Request.Context(), book.ID, url); err != nil {
        _ = c.Error(err)
        return
    }
    ok(c, http.StatusOK, gin.H{"coverUrl": url})
}
```

Switching storage implementations in `main.go` is one line:

```go
// up := &storage.S3Uploader{Bucket: "my-books", Region: "ap-southeast-1"}  // switch to S3 by changing only this line
up := &storage.DiskUploader{Dir: uploadDir, BaseURL: "/uploads"}
bookH := handler.NewBookHandler(bookSvc, up)
```

> **Naming decision recap (store the key or the URL?):** the Media & Query part's comparison table planted two candidate fields — `cover_path` (stores the relative address/URL) and `cover_key` (stores the object store's raw key). This part's implementation choice: **`Save` returns a displayable address directly, and that address goes into `cover_path`** (Disk and S3 differ only in URL shape; the column means "displayable address" in both cases), which removes the extra hop of "store the key first, then rebuild the URL when reading". Only when you need to do secondary operations on the raw key server-side (migration, CDN signing, bulk deletion) does it pay to open a separate `cover_key` column for the raw key and assemble the URL when responding — that is the advanced move of fully separating "storage semantics" from the "display contract", and this part only touches on it.

---

## 4. Connection-Pool Tuning & Wrap-Up

**Goal:** pay off the connection-pool bullet promised in ch10 — add the pool parameters to `db.InitDB()`; along the way, revisit the 504 timeout (already landed in the middleware).

### 4.1 The Connection Pool in Three Lines

The `*gorm.DB` returned by `gorm.Open` wraps a `database/sql` connection pool; get the underlying handle with `db.DB()` and configure:

```go
// db/db.go — appended at the end of InitDB (db is the package name; the package-level variable is called DB, of type *gorm.DB)
sqlDB, err := DB.DB() // get the underlying *sql.DB
if err != nil {
    log.Fatal("failed to get the underlying connection: ", err)
}
sqlDB.SetMaxOpenConns(50)           // concurrency cap: keep the database from being overwhelmed
sqlDB.SetConnMaxLifetime(time.Hour) // lifetime cap: a connection must not live forever (the database side usually times out too)
sqlDB.SetMaxIdleConns(10)           // idle cap: at most 10 idle connections stay in the pool
```

> **All three are "caps" — don't memorize them as "floors":** `SetMaxOpenConns` is the concurrency cap (beyond it, requests queue for a connection); `SetConnMaxLifetime` is the lifetime cap (expired connections are replaced, sidestepping connection-recycling problems at the database/middleware layer); `SetMaxIdleConns` caps the **number of idle connections** — database/sql closes idle connections beyond that number, and it never dials proactively to "keep them alive". If you want the "pre-warmed" effect, the application itself must fire a few queries at startup — that is another topic.

### 4.2 This Part's Wrap-Up: Every Promise Kept

The seven-part series closes its loop here — let's reconcile, one by one, every teaser the parts scattered along the way:

| Promise | Where it was made | Where it landed |
| --- | --- | --- |
| `BookRepository` interface + Service + injection + `internal/` | [GORM Crash Course](./gorm-gin-crud-tutorial) "Learning-grade structure" note / ch10 "Engineering pay-off" / [GORM Data Engineering](./gorm-gin-dto-batch) §4 | Engineering (Part 1), §1 |
| `httptest` table-driven tests (+ sqlmock) | [GORM Data Engineering](./gorm-gin-dto-batch) §4 / crash course note | Engineering (Part 1), §2 |
| Generic `GetPaginated[T]` | crash course ch10 / [GORM Data Engineering](./gorm-gin-dto-batch) §4 | Engineering (Part 1), §3 |
| Aggregation + pagination fold-up (`GetPaginatedScan`, optional) | [GORM Media & Query Enhancement](./gorm-gin-media-query) §2.3 | Engineering (Part 1), §3 (optional note) |
| Transactions `db.Transaction()` | crash course ch10 | Engineering (Part 1), §1 (atomic Service operations) |
| Timeout mapped to 504 | crash course ch10 | This part, §1 (the error middleware) |
| Unified error handling / ok-fail / slog | [GORM Relations in Practice](./gorm-gin-relations) "Why are the error messages uniform?" note / crash course ch10 | This part, §1 |
| Sort whitelist | [GORM Media & Query Enhancement](./gorm-gin-media-query) §2.2 note & key point | This part, §2 |
| File-header sniffing | [GORM Media & Query Enhancement](./gorm-gin-media-query) §1.3 key point | This part, §2 |
| Object-storage abstraction | [GORM Media & Query Enhancement](./gorm-gin-media-query) §1.3 (cover_path/cover_key comparison table) | This part, §3 |
| Connection pool | crash course ch10 | This part, §4 |

**Your project now:** a three-tier architecture (the book-dimension chain migrated end to end; the rest filled in from the same isomorphic template) + table-driven tests at the handler layer + a unified error middleware (404/500/504 semantics centralized + `slog` logging) + a sort whitelist + file-header sniffing (with stream rewind and the 2MB cap) + a swappable storage abstraction + connection-pool configuration — the skeleton is complete and safe to put into production.

Looking back, every `WithContext` and every `errors.Is` planted in the earlier parts ultimately flows into this part's one error middleware and one promise mapping table — their meaning only fully reveals itself at the moment of the fold-up.
