---
title: "Content Only Shows Up After a Refresh: A CORS Postmortem in Three Layers of Disguise"
description: "Client-side navigation to an article rendered an empty skeleton, while pressing F5 showed everything. The backend CORS allowlist pinned `http://localhost:3000` while the browser was on `http://127.0.0.1:3000`. A postmortem of why a refresh helps, why the app layer stayed completely silent, and how to prevent it on both the Gin and the useAsyncData side."
date: 2026-10-19
series:
tags:
  - Nuxt
  - Go
  - Security
---

> Symptom: after a client-side navigation to an article, the content area stayed on a skeleton placeholder; pressing F5 rendered everything at once. Every navigation needed another refresh.
> Root cause: the backend CORS allowlist said `http://localhost:3000`, but the browser address bar said `http://127.0.0.1:3000` — two different origins as far as the browser is concerned, so every client-side request was rejected with 403. A refresh goes through SSR data fetching on the server, which never touches CORS, so it always worked.

## 1. The Symptom: Only Navigation Breaks, Refreshing Always Works

The behavior was perfectly stable, and reproducible in both directions:

| Action | Result |
| --- | --- |
| Click "Docs" on the home page, then any article | URL becomes `/docs/<slug>`, page chrome renders, content area stays on the skeleton |
| Press F5 on the article page | Body, title, and code highlighting all render |
| Hit browser back, then another article | Back to a stuck skeleton |

"Works after a refresh" usually means **the same data from the same endpoint is reachable from the server but not from the browser** — that one observation cuts the search space by more than half.

I say "usually" because SSR can lie too: the server has its own cache, its own baseURL, its own network route. Same symptom, but the root cause would be server-side.

> Reproduction environment: Nuxt 4.5.2 / Vue 3.5.41 / Go 1.26.4 / `gin-contrib/cors` v1.7.7 / Chrome 155.

## 2. Ruling Out the Frontend Suspects One by One

My first reaction was the classic Nuxt pitfalls, but none of them explains "works after a refresh":

- **Hydration mismatch**: no hydration warnings at all, and a refresh is fine.
- **`__NUXT__` payload / cache reuse**: the detail page key is `doc:<url>` (includes the slug), the list page key is `docs-list:<url>` (includes pagination and filters). Any URL change forces a new request — so a hardcoded key or reused payload is out.
- **Routing**: the URL changed, the component swapped, `Skeleton` rendered — routing is fine, it's the data fetch returning nothing.
- **Shiki highlighting**: highlighting happens inside the `transform` of `useAsyncData`, i.e. after data exists.
- **Browser cache**: I did suspect caching, but in the Network panel I only skimmed the request list and never looked at the status column — which turned out to be the answer.

One thing I should have nailed down right away: **is the failure surface "one route" or "every client-side fetch"?** Measurements proved it was the latter (Section 9 has the numbers). Concluding "only the detail page is broken" is itself a sign you haven't reached the root cause yet.

## 3. Three Cheap Moves That Turned It Around

Three very cheap actions actually turned the investigation around. Do them in this order:

**① Look at the status code of that data request in the Network panel.** Mine was `403`. Seeing 403 should move your suspicion from "frontend rendering / hydration" to "the request path and backend middleware".

But 403 can also mean auth, rate limiting, or a WAF — don't jump to conclusions. Use the next step, comparing with and without the `Origin` header, to confirm whether the CORS middleware itself rejected the request.

**② `curl` the same URL with and without an `Origin` header. Add `-i` so the response headers come along.**

```bash
# Equivalent of the server-side fetch (SSR): no Origin
curl -i -s \
  "http://localhost:8080/api/docs?page=1&limit=1" | head -5
# → HTTP/1.1 200 OK

# Equivalent of the browser fetch (CSR): with Origin
curl -i -s \
  -H "Origin: http://127.0.0.1:3000" \
  "http://localhost:8080/api/docs?page=1&limit=1" | head -5
# → HTTP/1.1 403 Forbidden
```

The status code alone separates 403 from 200. With `-i` you also see whether `Access-Control-Allow-Origin` came back — some frameworks don't reject the request, they just omit that header, and then the status code stays 200. Only the response headers reveal it.

> Note these are two different things: `localhost:8080` in the URL is the **request target** (the API service);
> `127.0.0.1:3000` in the `Origin` header is the **simulated page origin** (where the frontend page lives).
> `Origin` always describes which page the request came from, never where it is going — and that is the easiest thing to misread here.

Same endpoint, **one extra request header and it flips from 200 to 403**. That's basically the answer.

**③ Treat "works after a refresh" as a clue, not as noise.** A full page load and a client-side navigation are two completely different data-fetch paths; their only commonality is "the data comes from the same endpoint". For any "works after a refresh" bug, compare those two paths first:

| Difference | Refresh (SSR) | Client navigation (CSR) |
| --- | --- | --- |
| Who fetches | Nitro server (Node) | The browser |
| Sends `Origin` header | No | Yes |
| Subject to CORS | No | **Yes** |
| Cookie / UA / IP | Server's point of view | Browser's point of view |

And it isn't only CORS: cookie scope between `*.localhost` and the production domain, User-Agent differences triggering bot protection, a server that can reach the intranet while the browser cannot, environment variables that only exist at build time — that entire family of bugs presents as "works after a refresh".

## 4. Root Cause: `localhost` and `127.0.0.1` Are Two Origins

`Origin` is defined as **scheme + host + port** ([MDN: Origin](https://developer.mozilla.org/en-US/docs/Glossary/Origin)); if any of the three differs, it's a different origin. So:

```text
http://localhost:3000      ← what the allowlist said
http://127.0.0.1:3000      ← what the browser was actually on
```

Those two lines differ by a hair as strings; to the browser they are **cross-origin**.

The backend looked like this at the time:

```go
// Old implementation: the default allowed origin was a single string literal
func corsOrigins() []string {
	const devDefaults = "http://localhost:3000"

	raw := os.Getenv("CORS_ORIGINS")
	if raw == "" {
		raw = devDefaults
	}
	// ...
}
```

And for an origin outside the allowlist, `gin-contrib/cors` doesn't "return one fewer header" — it **aborts the request** (`config.go`, v1.7.7):

```go
if !cors.isOriginValid(c, origin) {
	c.AbortWithStatus(http.StatusForbidden)
	return
}
```

That is why the status code is 403 — a status code that looks like "authentication failed" while having nothing to do with authentication.

## 5. Why Refreshing Works: Server-Side Fetching Has No "Origin"

CORS is a policy enforced by the **browser**, not server-side authorization. On a full page load:

1. The browser requests the document → Nitro renders the page;
2. Nitro calls `$fetch('http://localhost:8080/api/docs')` from the server;
3. Node's `fetch` does **not** send an `Origin` header (it isn't a browser; it has no concept of "the current page's origin");
4. The CORS middleware sees no `Origin`, treats it as an ordinary request → 200.

On a client-side navigation, step 2 runs in the browser, which automatically attaches `Origin: http://127.0.0.1:3000`; the middleware finds it missing from the allowlist → 403.

In one sentence: **the refresh bypassed CORS, it didn't "fix" anything.**

## 6. Layer Two: The App Layer Dresses Failures Up as "Loading"

If the page had shown an error on 403, I'd have found the cause within minutes. It didn't. The reason is on the frontend:

```ts
// app/composables/useDocDetail.ts: only `data` is destructured, `error` is dropped
const { data: page } = useAsyncData<DocDetailBase>(/* ... */)
```

```vue
<!-- app/pages/docs/[slug].vue: renders a skeleton while `page` is empty -->
<Skeleton v-if="!page" />
```

The list page fails differently: `useDocs()` actually **returns** `error`, but the page never destructures it, so it too gets stuck on `<Skeleton v-if="!docs" />`. Two paths, same outcome — the detail page never caught the error, the list page caught it and ignored it.

The upshot: the request fails → `page` stays `undefined` → the skeleton never moves. What the user (and the developer) sees isn't "an error", it's "loading", so you go chasing hydration, caching, and highlighting — all in the wrong direction.

**But be precise about which layer is silent**: the browser console *does* complain. In my run there were four entries:

```text
Access to fetch at 'http://localhost:8080/api/docs?...' from origin 'http://127.0.0.1:3000'
has been blocked by CORS policy: No 'Access-Control-Allow-Origin' header is present
on the requested resource.
```

It only says "a response header is missing"; it never says "the server returned 403", and it points at no visible page state — easy to swipe past as routine noise.

What stayed truly silent was the **app layer**: no toast, no error copy, no fallback; the page just showed a motionless skeleton. The accurate description is "**the browser shouted, the app layer pretended not to hear**", not "there was no error at all".

The fix is cheap — render the failure state explicitly:

```vue
<Skeleton v-if="!docs && !error" />
<div v-else-if="error" class="text-sm" style="color: var(--ui-danger)">
  {{ t("docs.loadFailed") }}
  <button type="button" @click="refresh()">{{ t("common.retry") }}</button>
</div>
```

> **"Loading forever" is a more expensive state than "an error"**. Wherever you use `useAsyncData` / `useFetch`, destructure `error` too.

## 7. A Hidden Trigger: Port Drift

There is another equally quiet branch. When port 3000 is taken, Nuxt dev picks a different port:

```text
[get-port] Unable to find an available port (tried 3000 on host "localhost").
           Using alternative port 3001.
```

So `http://localhost:3001` is yet another new origin, and the allowlist fails again. **As long as the allowlist is a literal origin, any "it's definitely 3000" assumption can silently invalidate it**:

- Opening the page on a different host (`localhost` ↔ `127.0.0.1` ↔ `[::1]`)
- Port taken, drifted to 3001
- A teammate running it on 5173
- Debugging on a physical device over a LAN IP

## 8. The Fix: Allow Local Development by Rule, Not by Literal

The goal: "however you open it locally, it works; production gets no relaxation". Production is same-origin (Caddy reverse-proxies `/api` to this service), so browser requests aren't cross-origin at all — CORS only needs to serve local development.

```go
// devOriginPattern matches every common way to open the local frontend:
// localhost / 127.0.0.1 / [::1] on any port.
var devOriginPattern = regexp.MustCompile(`^http://(localhost|127\.0\.0\.1|\[::1\]):[0-9]{1,5}$`)

// isDevEnv: only an explicit ENV=development counts as a dev environment (why gin.Mode() won't do — see below).
func isDevEnv() bool { return os.Getenv("ENV") == "development" }

func corsConfig() cors.Config {
	cfg := cors.Config{
		AllowMethods: []string{"GET", "OPTIONS"}, // read-only content API
		AllowHeaders: []string{"Content-Type"},
	}

	// An explicit allowlist wins (mutually exclusive with AllowOriginFunc, see cors.Config.Validate)
	if origins := splitList(os.Getenv("CORS_ORIGINS")); len(origins) > 0 {
		cfg.AllowOrigins = origins
		return cfg
	}

	// Never leave this nil: with all three origin settings empty, newCors panics (see below)
	cfg.AllowOriginFunc = func(origin string) bool {
		if !isDevEnv() {
			return false
		}
		return devOriginPattern.MatchString(origin)
	}
	return cfg
}
```

`splitList` is just "split on commas + trim + drop empties" — irrelevant to the main thread, implementation omitted.

A few notes:

- Using `AllowOriginFunc` instead of stacking eight literal entries in `AllowOrigins` — ports and hostnames multiply, you can't enumerate them.
- Keep the explicit `CORS_ORIGINS` override for cases that genuinely can't be expressed as a rule, such as debugging on a physical device over `192.168.x.x`.
- `[0-9]{1,5}` is a **shape match** only; it doesn't validate the port (it would also match `localhost:99999`). That's acceptable for a dev allowlist — no browser can produce such an origin. `http://` is hardcoded because local development is http; switch it to `https?://` if you debug over https.

### Why deciding solely on "is CORS_ORIGINS empty" is unsafe

The first fix missed a step: it only checked whether `CORS_ORIGINS` was configured, never which environment was running.

The consequence — **whenever production has no `CORS_ORIGINS`, it allows any `http://localhost:<port>`, `127.0.0.1`, `[::1]`**. That directly contradicts "production gets no relaxation", and the moment the API grows auth or cookies, this rule becomes a back door standing open to any page on the local machine.

So the check became "**allow only when the environment explicitly says development**": anything other than `ENV=development` (including `ENV` being unset) is denied. The default is now closed, not open.

Two more traps surfaced while writing that code:

1. **Don't use `gin.Mode()` to detect the environment.** `gin.Default()` runs in debug mode by default, so unless production explicitly sets `GIN_MODE=release`, it will be misread as a development environment — which means shipping the dev rule straight to production.
2. **Don't express "off" by leaving the origin settings unset.** With all three origin fields empty, `gin-contrib/cors` makes `Config.Validate()` fail and `newCors` `panic`s immediately — the service won't even start. A textbook case of "hardening the security until the service is down". The correct form is a function that always returns `false`.

Two small pieces go with it:

- `.env.example` must carry `ENV=development`. Otherwise the `.env` a new teammate copies hits the assertion and every local request 403s — handing them the exact bug this article is about.
- Write `ENV=production` explicitly in the container; don't rely on the implicit convention "unset means production".

Finally, log one warning at startup so a "cross-origin fully closed" configuration is visible: `⚠️ CORS: 未配置 CORS_ORIGINS 且 ENV≠development —— 所有跨源请求都会被拒绝（同源部署无影响）`.

## 9. Verification: Same Frontend, Only the Backend Config Changed

I ran the browser measurement before and after the fix (same frontend, same content, only the backend CORS config differed):

| Scenario | First load of `/docs` | Clicking a card into the detail (client navigation) | Pressing F5 |
| --- | --- | --- | --- |
| Before (only `localhost:3000` allowed) | 1700 chars | **36 chars, no title, no code blocks** | 5328 chars, 8 code blocks |
| After (`ENV=development`, local ports allowed) | 1700 chars | **5328 chars, title + 8 code blocks** | 5328 chars, 8 code blocks |

The list page makes the point even better — same filter, empty when clicked, full when refreshed:

| Scenario | Before | After |
| --- | --- | --- |
| Click `#ORM` in the `/docs` filter panel (client-side filtering) | **0 cards, 5 skeleton placeholders** | 8 cards |
| Refresh the same URL (`?tag=ORM`) | 8 cards | 8 cards |

"36 chars" is the text of the skeleton plus the nav bar — not a single character of content made it in. After the fix, the client-side navigation and the refresh render bodies of **exactly the same length**.

> Measurement note: both "chars" counts come from `document.body.innerText.length`, which counts UTF-16 code units and is **not a byte count** (Chinese text is roughly 3x that in UTF-8 bytes). Always compare with the same yardstick.

CORS behavior matrix (**local development configuration**: `ENV=development`, no `CORS_ORIGINS`):

```text
http://localhost:3000     200  Access-Control-Allow-Origin: http://localhost:3000
http://127.0.0.1:3000     200  Access-Control-Allow-Origin: http://127.0.0.1:3000   ← 403 before the fix
http://localhost:3001     200  Access-Control-Allow-Origin: http://localhost:3001   ← 403 before the fix
http://127.0.0.1:3001     200  Access-Control-Allow-Origin: http://127.0.0.1:3001
http://[::1]:3000         200  Access-Control-Allow-Origin: http://[::1]:3000
http://evil.example.com   403  (still rejected)
null                      403  (still rejected)
no Origin (SSR / curl)    200
OPTIONS preflight         204  + correct headers
```

Run the same code in **production configuration** (`ENV=production`, still no `CORS_ORIGINS`) and the first five lines **all turn 403**, "no Origin" stays 200.

The service starts normally and prints that warning line — which is exactly what the environment gate is supposed to guarantee.

> The `null` row covers "the browser couldn't produce a real origin": sandboxed iframes, `file://` pages, and so on. The browser sends the literal string `null` as the `Origin` header. It should be rejected too — otherwise any local HTML file could read your API.

## 10. A Transferable Checklist

When you hit "client-side navigation is blank / works after a refresh", walk through this in order:

1. **Look at Status and response headers in the Network panel**, not just the red console text — the console can't tell 403 from "200 but missing ACAO".
2. **`curl` with and without `Origin`** — the cheapest way to tell "CORS or something else".
3. **Check the origin literals**: `localhost` / `127.0.0.1` / `[::1]`, `www` vs. bare domain, `http` vs. `https`, ports.
4. **Never hardcode ports**: dev servers drift, proxies rewrite ports, containers map ports.
5. **Watch out for `server.proxy` as a silencer**: proxied requests are same-origin, so CORS never applies — remove the proxy and the problem surfaces.
6. **Make failure visible**: render the `error` from `useAsyncData` instead of disguising it as "loading".
7. **Put the matrix into tests**: allowlists are exactly what a future contributor loosens "while passing by", and a table-driven `httptest` is more reliable than manual verification.

## 11. References

- [MDN: Origin](https://developer.mozilla.org/en-US/docs/Glossary/Origin) — the authoritative definition of `scheme + host + port`
- [MDN: Same-origin policy](https://developer.mozilla.org/en-US/docs/Web/Security/Same-origin_policy)
- [gin-contrib/cors](https://github.com/gin-contrib/cors) — the mutual exclusivity of `AllowOrigins` and `AllowOriginFunc`
- Community reports of the same symptom (useful as evidence of how common it is, though most stop at "just configure CORS"):
  - [Nuxt: data loads on refresh but not on navigation](https://stackoverflow.com/questions/79059932)
  - [Nuxt 3 + Directus: CORS error when navigating](https://stackoverflow.com/questions/79079249/nuxt-3-directus-returns-cors-error-when-navigating-the-site)
  - [asyncData cross-origin errors on every route change, fine after a refresh](https://blog.csdn.net/xiaoqihshw/article/details/144127933) (Chinese)

> Postscript: the "three layers of disguise" are really three faces of one thing — **a failure disguised as something else**: 403 looking like auth, a skeleton looking like loading, a working refresh looking like caching. Make errors look like errors, and this collapses into an ordinary cross-origin investigation.
