---
title: "MCP stdio Protocol's 3 Hidden Traps: When All Unit Tests Pass but the MCP Server Won't Respond"
description: "A deep dive into three hidden traps of the MCP stdio protocol, recording a real debugging journey — from 401 green tests to a completely unresponsive server — tracing the root cause to three fatal bugs: process.exit(), stdout pollution, and an async race, plus an end-to-end testing solution."
date: 2026-08-16
permalink: a1473eea-0a80-4118-9fff-ccc71223a5bc
series: ''
level: P5
tags:
  - TypeScript
  - LLM
  - Engineering
---

> This article records a real MCP Server debugging session: every automated test of `story-cli` passed, yet in a real environment the MCP Server couldn't respond to any request at all. The root cause turned out to be 3 bugs, each touching low-level details of the Node.js process model and the stdio protocol.

---

## TL;DR

If you're building an MCP Server (or any long-running process that speaks a stdio protocol), remember three iron rules:

1. **Never call `process.exit()` inside a `run()` function** — MCP Servers, `--watch` modes, and any other long-running command are not one-shot CLI tools. `process.exit()` kills the process before it even starts listening. If you must make an exception, extract the "long-running" abstraction (e.g. `isLongRunning`) instead of enumerating specific commands.
2. **Never print debug logs to stdout** — stdout is the MCP protocol channel. Any output that isn't JSON-RPC pollutes the message stream and makes the client unable to parse any response. Diagnostics belong on stderr.
3. **Always wait for all async work in the `close` event** — `close` only means the input stream closed, not that your callbacks have finished. You need to wait for all in-flight Promises before exiting.

---

## Background: story-cli's MCP Server

First, a quick introduction to the project. `story-cli` is a **zero-deployment, Git-native Markdown content management CLI**. It manages stories/papers/notes/tutorials with a simple directory convention (`NN-名称/` — "NN-name/" — containing `config.json` + `text.md`), auto-generates READMEs, exports EPUB, and is bilingual (Chinese/English).

On our roadmap, **the MCP Server was a P0-level strategic task** — the gateway to the AI era. The design principle: **"AI does the thinking, the CLI does the governance."**

We exposed 6 tools over JSON-RPC 2.0 over stdio:

| MCP tool | Purpose |
| --------------- | --------------------------------- |
| `scan_stories` | List all stories and their metadata |
| `read_chapter` | Read a chapter's content from a story |
| `write_chapter` | Write body text to a story (atomic write) |
| `validate` | Validate the config.json of every story |
| `build` | Trigger a README rebuild |
| `import_json` | Bulk-import stories from structured JSON |

The code structure was clean:

```text
src/mcp/
├── protocol.ts   # JSON-RPC 2.0 protocol parsing/serialization (pure functions, fully tested)
├── tools.ts      # MCP tool registration (reuses shared logic from core/loader.ts)
└── server.ts     # stdio server startup and request dispatch
```

Everything looked perfect — **until we actually called it**.

---

## The Symptom: All Automated Tests Green, But Real Requests Get No Response

At the time we had **404 automated tests, 401 passing**. `tests/mcp.test.ts` covered protocol parsing, serialization, tool registration, and every tool handler — **all passing**.

So I started the MCP Server against a real story repository and sent a JSON-RPC request through a pipe:

```bash
echo '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | node bin/index.ts mcp-server
```

💀 **Empty output.** No response at all.

I thought my pipe syntax was wrong. I tried several variations:

```bash
# approach 1: printf
printf '{"jsonrpc":"2.0","id":1,"method":"tools/list"}\n' | node bin/index.ts mcp-server

# approach 2: file redirection
printf '{"jsonrpc":"2.0","id":1,"method":"tools/list"}\n' > /tmp/req.json && node bin/index.ts mcp-server < /tmp/req.json

# approach 3: keep stdin open
{ printf '{"jsonrpc":"2.0","id":1,"method":"tools/list"}\n'; sleep 2; } | node bin/index.ts mcp-server
```

**Still nothing.**

Even weirder: when sending the request through Node.js's `spawnSync`, the process exit code was 0 (it looked "successful"), but both stdout and stderr were empty.

At that moment I realized: **this isn't a calling convention problem — our MCP Server has a bug.**

But 404 tests were green! How could there be a bug?

---

## Bug #1: The Ghost of process.exit()

### Root Cause Investigation

I first looked at the CLI entry file `bin/index.ts`:

```typescript
#!/usr/bin/env node
import { run } from "../src/cli.ts"

const exitCode = await run(process.argv)
process.exit(exitCode)
```

The problem was obvious at a glance.

When a user runs `story mcp-server`:

1. `run(process.argv)` is invoked
2. Inside `run()`, `runMcpServer(rootDir)` is called → `startMcpServer()` starts listening on stdin
3. **`run()` returns 0 immediately** (because `startMcpServer()` is an async pattern that "registers listeners and returns" — it doesn't block)
4. **`process.exit(0)` executes immediately** → the process terminates
5. The JSON-RPC request sitting in stdin never gets read by readline

**The MCP Server died the moment it was born.**

### The Fix

```typescript
#!/usr/bin/env node
import { run } from "../src/cli.ts"

const exitCode = await run(process.argv)

// An MCP server needs to stay alive and keep listening on stdin.
// Process exit is handled by the close/SIGINT events inside server.ts.
if (process.argv[2] !== "mcp-server" && process.argv[2] !== "mcp") {
  process.exit(exitCode)
}
```

> ⚠️ **Note**: this fix looked fine at the time, but later that same day testing exposed its **limitation** — see "Bug #1.5: The Same Bug Returns" below.

### The Deeper Lesson

This is the **first trap** when turning a CLI tool into a service:

| Mode | Lifecycle | When to exit |
| ----------------------------------------- | ------------------------- | ----------------------------------- |
| **CLI tool** | Exits after the command finishes | `process.exit(exitCode)` is the right thing |
| **Long-running process** (MCP Server / daemon) | Keeps listening for input until EOF/signal | Exit must be driven by a callback triggered by the **input source** |

`process.exit()` is unconditional, immediate, and uninterruptible. It doesn't wait for pending I/O, timers, or Promises. In the MCP Server scenario, that "feature" killed our server outright.

---

## Bug #1.5: The Same Bug Returns — process.exit()'s Second Ghost

### The Symptom

After fixing Bug #1, I kept testing the MCP Server. That same day, I wanted to check the performance of `story build --watch`:

```bash
story build --watch
```

The output said 「👀 监听模式已启动，文件变更自动重建...」 ("👀 watch mode started, auto-rebuilding on file changes..."), but **the process exited immediately** — `--watch` mode never actually started watching files.

I tried modifying a story file:

```bash
echo "新内容" > "01-测试故事/text.md"
```

Nothing happened. The README was never updated.

### Root Cause: The Whitelist-of-Commands Flaw

I looked back at the fix in `bin/index.ts`:

```typescript
if (process.argv[2] !== "mcp-server" && process.argv[2] !== "mcp") {
  process.exit(exitCode)
}
```

What this logic says is: **"for every command except `mcp-server` and `mcp`, call `process.exit()`."**

But `build --watch` is also a **long-running process**! It needs to keep watching files until it receives `SIGINT`. Only the two MCP Server commands were exempted — `build --watch` wasn't on the whitelist, so it got killed by `process.exit()` immediately too.

**The first MCP Server bug was fixed, and the same ghost reappeared on `build --watch`.**

### The Fix: Extract the "Long-Running" Abstraction

The right fix isn't to enumerate even more commands — it's to extract the essential property of "**which commands are long-running**":

```typescript
#!/usr/bin/env node
import { run } from "../src/cli.ts"

const exitCode = await run(process.argv)

// Long-running processes need to stay alive; exit is handled by internal close/SIGINT events:
// - MCP server: keeps listening on stdin; exit is controlled by server.ts's close/SIGINT
// - build --watch: keeps watching file changes; exit is controlled by build.ts's SIGINT
const isLongRunning =
  process.argv[2] === "mcp-server" ||
  process.argv[2] === "mcp" ||
  (process.argv[2] === "build" && process.argv[3] === "--watch") ||
  (process.argv[2] === "b" && process.argv[3] === "--watch")

if (!isLongRunning) {
  process.exit(exitCode)
}
```

### The Deeper Lesson: Fix Bugs by Extracting an "Abstraction", Not Enumerating "Instances"

This was the **biggest lesson** of the whole session:

| Fix approach | Code shape | Problem |
| -------------------- | -------------------------------------------- | ---------------------------------------- |
| **Enumerate instances** (at the time) | `if (cmd !== "mcp-server" && cmd !== "mcp")` | Adding one more long-running command means coming back to edit this line |
| **Extract an abstraction** (final) | `const isLongRunning = ...` | Any new command just expresses its property inside this set |

When you see an "exclusion list" in code (`if (cmd !== "A" && cmd !== "B")`), it means you're **enumerating specific commands** instead of expressing the **essential property** of "which commands are long-running". The moment a new long-running command appears (like `--watch`), the same bug returns.

**Checklist**: if your CLI is ever going to add a "keep-listening" feature (watch / serve / daemon), check the `isLongRunning` list in `bin/index.ts` first — it must include the new command.

---

## Bug #2: The Fatal Pollution of console.log

### The Surprise: After Fixing Bug #1, Some Responses Appeared

After fixing Bug #1, I was pleasantly surprised to see `tools/list` respond! But only these responded:

- `tools/list` ✅
- `initialize` ✅
- Error responses for unknown tools ✅

Meanwhile, the **async `tools/call` still got no response** (`scan_stories` / `read_chapter` / `validate`).

I tested `scan_stories` on its own:

```bash
echo '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"scan_stories","arguments":{}}}' | node bin/index.ts mcp-server
```

Still empty.

I tried a different angle — calling `loadStories()` directly in Node:

```bash
node --experimental-strip-types -e "
import { loadStories } from './src/core/loader.ts';
const { stories } = await loadStories('/tmp/test-story-cli');
console.log('STORIES:', stories.length);
"
```

Output:

```text
📊 01-测试故事: 自动计算字数为 约 13 字（未写回，使用 --save-counts 持久化）
📊 02-二创故事: 自动计算字数为 约 13 字（未写回，使用 --save-counts 持久化）
📊 03-English-Story: 自动计算字数为 ~7 words（未写回，使用 --save-counts 持久化）
STORIES: 3
```

**Found it!** Inside `loadStories()`, a `console.log` was printing "auto-computed word count" diagnostic lines.

### Why Can a Single `console.log` Kill MCP?

MCP's stdio transport spec says **stdout is the protocol-dedicated channel**:

```text
├── stdin  ← client sends JSON-RPC requests
├── stdout → server returns JSON-RPC responses (protocol-dedicated, the only legal output)
└── stderr → logs/warnings/errors (for humans, not for the protocol)
```

When an MCP client sends a `scan_stories` request, the server calls `loadStories()` while handling it, and `console.log` dumps a `📊 01-测试故事: ...` line to stdout. Now stdout looks like:

```text
📊 01-测试故事: 自动计算字数为 约 13 字...      ← pollution!
{"jsonrpc":"2.0","id":3,"result":{...}}        ← the real response
```

MCP clients (VSCode / Claude Desktop / Cursor) expect every line of stdout to be a valid JSON-RPC message when parsing. The first line isn't JSON at all —

**the client gives up on parsing, which looks like "no response".**

As a side note, MCP's stdio transport also has a hard requirement about newlines: **every JSON-RPC message must end with `\n`**. If your server outputs JSON without a trailing newline, the client also fails to parse it. That's why the official MCP docs' Debugging page states it plainly:

> _"Local MCP servers should not log messages to stdout (standard out), as this will interfere with protocol operation."_

— The docs warned us all along; we only truly understood it after stepping on it in a real environment.

And this kind of bug is especially sneaky:

- In unit tests, `scan_stories`'s handler is called directly and nobody parses stdout → tests pass
- In a real environment, the MCP client strictly parses stdout → immediate breakage

### The Fix

```typescript
// Before
if (!config.wordCount) {
  console.log(locale.autoWordCount(folder, story.wordCount, saveCounts))
}
```

```typescript
// After
if (!config.wordCount) {
  // Use stderr for diagnostics so we don't pollute the stdout channel of the MCP stdio protocol.
  console.error(locale.autoWordCount(folder, story.wordCount, saveCounts))
}
```

The `console.log(locale.generatedText(...))` inside `loadStoryContentAsync` was changed the same way.

### The Deeper Lesson

**In a stdio protocol, stdout is not for logging.** It's the protocol channel between two processes. Any extra output — even a single seemingly harmless log line — breaks protocol parsing.

This is a **silent runtime failure**: the code doesn't throw, tests don't fail, and only real clients mysteriously stop working.

> In an MCP Server, `stdout = protocol`, `stderr = logs`. Never mix them.

---

## Bug #3: The Async Race on readline close

### Another Surprise

After fixing Bug #2, I thought everything was done. But testing showed `tools/call` still responded **intermittently**: sometimes a response came back, sometimes not.

I stared at the old code in `src/mcp/server.ts`:

```typescript
export function startMcpServer(rootDir: string, tools: RegisteredTool[]): void {
  const rl = createInterface({ input: process.stdin, terminal: false })

  rl.on("line", async (line) => {
    // ... parse and handle the request
    const response = await handleRequest(request, rootDir, tools)
    if (response) process.stdout.write(serializeMessage(response))
  })

  rl.on("close", () => {
    // Wait for stdout to flush before exiting (avoid truncated output)
    process.stdout.write("", () => process.exit(0))
  })
  // ...
}
```

In pipe mode (`echo '...' | node bin/index.ts mcp-server`), stdin closes immediately after all lines are read, which fires the `close` event. **When `close` fires, the async `await handleRequest()` inside `rl.on("line")` hasn't finished yet!**

Here's the timing:

```text
t0:  stdin receives the JSON-RPC request line
t1:  rl fires the "line" event and enters the async callback
t2:  the async callback hits await handleRequest() and suspends (shaded area = waiting for the async result)
t3:  stdin finishes reading all lines → rl fires the "close" event
t4:  the "close" callback runs process.stdout.write("", () => process.exit(0))
t5:  the process exits while await handleRequest() is still suspended → the response is lost forever
```

This is an **async race**: `close` says "the input stream is closed", but it doesn't wait for your Promises to finish.

### The Fix

Track all in-flight requests with a `pending` Set, and wait for all of them on `close` before exiting:

```typescript
export function startMcpServer(rootDir: string, tools: RegisteredTool[]): void {
  const rl = createInterface({ input: process.stdin, terminal: false })
  const pending = new Set<Promise<void>>()

  rl.on("line", (line) => {
    const trimmed = line.trim()
    if (!trimmed) return
    let request: JsonRpcRequest
    try {
      request = parseRequest(trimmed)
    } catch (e) {
      const code =
        (e as Error & { code?: number }).code ?? JsonRpcErrorCode.InternalError
      process.stdout.write(
        serializeMessage(makeErrorResponse(null, code, (e as Error).message)),
      )
      return
    }
    // Track in-flight requests so we know the async handler has finished when stdin closes.
    const task = (async () => {
      const response = await handleRequest(request, rootDir, tools)
      if (response) process.stdout.write(serializeMessage(response))
    })()
    pending.add(task)
    task.finally(() => pending.delete(task))
  })

  rl.on("close", () => {
    // Wait for all in-flight requests to finish, then flush stdout before exiting (avoid truncated output).
    void Promise.allSettled([...pending]).then(() => {
      process.stdout.write("", () => process.exit(0))
    })
  })
  process.on("SIGINT", () => {
    rl.close()
  })
}
```

### The Deeper Lesson

In Node.js's event loop, **readline's `close` event only means "the input stream closed", not "your async callbacks have run"**.

This is a universal problem for every stdio protocol server: when stdin hits EOF, you may still have queued Promises. You need to track and wait for them explicitly:

1. Maintain a set of all in-flight operations
2. On `close` or `SIGINT`, wait with `Promise.allSettled`
3. Only then call `process.exit`

---

## The Takeaway: Test Layering

The biggest insight from this session was **the value of layered testing**:

| Test layer | Our previous coverage | What it would catch |
| ---------------------------------------------------------- | -------------- | ------------------------- |
| **Unit tests** (calling handler functions directly) | ✅ 401 all green | Can't catch Bug #1 / #2 / #3 |
| **Integration tests** (calling `startMcpServer` without a real process) | ❌ none | — |
| **End-to-end tests** (spawnSync a real child process + real stdin/stdout) | ❌ none | All 3 bugs at once |

**Green unit tests don't mean the system works.** You need to start the server in a real process, send requests through real pipes, and parse real stdout — because only end-to-end tests can catch problems at the "process lifecycle" and "protocol integrity" levels.

```typescript
// tests/mcp-server.test.ts (the end-to-end test we added)
function sendRequests(dir: string, requests: string[]) {
  const input = `${requests.join("\n")}\n`
  const result = spawnSync(process.execPath, [binPath, "mcp-server"], {
    cwd: dir,
    input,
    encoding: "utf-8",
    timeout: 5000,
  })
  return {
    stdout: result.stdout || "",
    stderr: result.stderr || "",
    status: result.status ?? -1,
  }
}

test("MCP server responds to async tools/call (scan_stories)", () => {
  const { stdout, stderr } = sendRequests(dir, [
    '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"scan_stories","arguments":{}}}',
  ])
  // stderr must not contain JsonRpcResponse content → guards against console.log/stdout pollution
  assert.ok(!stderr.includes("jsonrpc"))

  // Split by lines and filter empty lines instead of JSON.parse(stdout.trim()).
  // If stdout contains multiple lines, trim() only strips leading/trailing whitespace
  // and the inner newlines make JSON.parse fail.
  const lines = stdout
    .split("\n")
    .map((l) => l.trim())
    .filter(Boolean)
  assert.ok(lines.length >= 1, "there should be at least one JSON-RPC response")
  // Take the last line (or look up the line by id when multiple responses are requested)
  const response = JSON.parse(lines[lines.length - 1] ?? "{}")
  // ...
})
```

This test starts the MCP Server in a **real child process**, sends JSON-RPC requests through **real pipes**, and validates the stdout contents. If anyone ever adds a `console.log` to `loadStories`, this test fails immediately.

> **Follow-up (same day)**: after fixing Bug #1.5, we added an end-to-end regression test for `build --watch` (`tests/watch.test.ts`) — it uses spawnSync to start a real child process and asserts "the process stays alive" + "the README is rebuilt within 5 seconds of editing a story". If we'd had that test back then, Bug #1.5 would have been caught the day it was fixed, instead of surfacing by accident during a later performance check. That's test layering proven once again: **unit tests can't cover process lifecycle — only end-to-end tests can.**

---

## Appendix 1: The Complete Debugging Flow (for Reference)

```bash
# 1. Create a test repository
mkdir -p /tmp/test-story-cli && cd /tmp/test-story-cli
node /path/to/story-cli/bin/index.ts init
node /path/to/story-cli/bin/index.ts new "测试故事"

# 2. Start the MCP Server (find the problem)
echo '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | node /path/to/story-cli/bin/index.ts mcp-server
# → empty output (Bug #1)

# 3. After fixing #1 → tools/list responds, but scan_stories doesn't (Bug #2's stdio pollution)

# 4. Verify loadStories behavior in isolation
node --experimental-strip-types -e "
import { loadStories } from './src/core/loader.ts';
await loadStories('/tmp/test-story-cli');
"
# → see the 📊 logs appearing on stdout

# 5. After fixing #2 → sometimes responds, sometimes not (Bug #3's async race)

# 6. Verify repeatedly via the end-to-end test
node --test tests/mcp-server.test.ts
# → 7 tests pass
```

## Appendix 2: The MCP Inspector Debugging Tool

Everything above is "post-mortem" debugging. If you integrate **[MCP Inspector](https://github.com/modelcontextprotocol/inspector)** (MCP's official debugging tool) during development, many of these problems can be caught before release:

```bash
npx @modelcontextprotocol/inspector node /path/to/story-cli/bin/index.ts mcp-server
```

MCP Inspector launches a visual web UI that lets you:

- **View the full tool list / parameter schemas** (catch registration problems)
- **Call each tool one by one and inspect the raw responses** (catch stdout pollution)
- **Check the protocol-layer communication logs** (catch handshake failures / newline issues)

It's the "X-ray machine" of MCP Server development — I recommend every MCP Server developer run everything through the Inspector before CI/CD.

> There are also third-party helpers in the community (like `mcp-stdio-guard` for catching stdout pollution), but the Inspector as the official tool covers most scenarios.

## Appendix 3: Format Drift in the AI Interaction Layer

An MCP Server has to handle not only protocol traps (#1 / #2 / #3) but also traps in the **AI interaction layer**:

When `create_story` creates a directory, it converts spaces in the title to hyphens (e.g. `"AI 创作的故事"` → `02-AI-创作的故事`), but the LLM may pass back the original space form (`"02-AI 创作的故事"`) — `safeFolder` has to match both variants to hit the right directory.

**Protocol-layer traps and interaction-layer traps — we stepped on all of them the same day.**

---

## Summary: Three Iron Rules + One Meta-Lesson

If you only take away three sentences, plus one lesson about fixing bugs themselves:

1. **`process.exit()` belongs only to one-shot CLI commands.** Long-running processes (MCP Servers / watch mode / daemons) must have their exit controlled by input/signal callbacks. When making exceptions, extract the "long-running" abstraction — don't enumerate specific commands.
2. **stdout is a protocol channel, not a log channel.** In any stdio protocol server, non-protocol output is pollution. Diagnostics belong on stderr.
3. **`close` ≠ all work finished.** Use a `pending` Set + `Promise.allSettled` to explicitly wait for async work.
4. **Fix bugs by extracting an "abstraction", not by enumerating "instances".** When you see an exclusion list like `if (cmd !== "A" && cmd !== "B")`, you're enumerating specific commands — when a new long-running command appears, the same bug recurs somewhere new.

What these four problems have in common: **none of them can be caught by unit tests**; they only surface in real process environments. So — after writing your handlers, don't forget to write a `spawnSync` end-to-end test.

Note that these four iron rules are **language-agnostic** — whether you build a stdio server in Node.js, Python, or Go, the same four traps exist: `process.exit()` / stdout pollution / un-awaited async work / enumerating instead of abstracting. This article uses Node.js only because our project happens to be on the Node stack.

---

This article is based on a real debugging session from the story-cli project. Repository: [story-cli](https://github.com/yuelinghuashu/story-cli)
