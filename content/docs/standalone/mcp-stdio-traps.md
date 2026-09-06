---
title: MCP stdio 协议的 3 个隐秘陷阱：当单元测试全绿，但 MCP Server 无法工作
description: 深入剖析 MCP stdio 协议的三个隐秘陷阱，记录一次真实的调试经历——从 401 个测试全绿到线上完全无响应，最终排查出 process.exit、stdout 污染和异步竞态三个致命 Bug，并给出端到端测试的解决方案。
date: 2026-08-16
series:
tags:
  - TypeScript
  - LLM
  - Engineering
---

> 本文记录了一次真实的 MCP Server 调试经历：`story-cli` 的自动化测试全部通过，但 MCP Server 在真实环境中完全无法响应任何请求。最终排查出 3 个 Bug，每一个都涉及 Node.js 进程模型与 stdio 协议的底层细节。

---

## TL;DR

如果你正在开发 MCP Server（或者任何基于 stdio 协议的长期运行进程），请记住三条铁律：

1. **永远不要在 `run()` 函数中调用 `process.exit()`** —— MCP Server、`--watch` 模式等任何长期运行的命令都不是一次性 CLI 工具。`process.exit()` 会在你开始监听之前就把进程杀掉。如果不得不豁免，请提炼「长期运行」的抽象（如 `isLongRunning`），而不是枚举具体命令。
2. **永远不要在 stdout 上打印任何调试日志** —— stdout 是 MCP 协议通道，任何非 JSON-RPC 的输出都会污染消息流，导致客户端无法解析任何响应。诊断信息请走 stderr。
3. **永远在 `close` 事件中等待所有异步操作完成** —— `close` 只代表输入流关闭，不代表你的回调已执行完毕。你需要在退出前等待所有 in-flight 的 Promise 结束。

---

## 背景：story-cli 的 MCP Server

先介绍一下这个项目。`story-cli` 是一个**零部署、Git 原生的 Markdown 内容管理 CLI**。它用简单的目录约定（`NN-名称/` 包含 `config.json` + `text.md`）管理故事/论文/笔记/教程，自动生成 README，导出 EPUB，中英双语。

在我们的 ROADMAP 中，**MCP Server 是 P0 级战略任务**——AI 时代的入口。设计原则是：**"AI 只负责思考，CLI 负责治理"**。

我们通过 JSON-RPC 2.0 over stdio 协议暴露了 6 个工具：

| MCP 工具        | 功能                              |
| --------------- | --------------------------------- |
| `scan_stories`  | 列出所有故事及元数据              |
| `read_chapter`  | 读取指定故事的章节内容            |
| `write_chapter` | 将正文写入指定故事（原子写入）    |
| `validate`      | 校验所有故事的 config.json 合法性 |
| `build`         | 触发 README 重建                  |
| `import_json`   | 从结构化 JSON 批量导入故事        |

代码结构非常干净：

```text
src/mcp/
├── protocol.ts   # JSON-RPC 2.0 协议解析/序列化（纯函数，有完整测试）
├── tools.ts      # MCP 工具注册（复用 core/loader.ts 共享逻辑）
└── server.ts     # stdio 服务器启动与请求分发
```

一切看起来都很完美——**直到我们真正去调用它**。

---

## 现象：自动化测试全绿，但真实请求无响应

我们当时有 **404 个自动化测试，401 通过**。其中 `tests/mcp.test.ts` 覆盖了协议解析、序列化、工具注册、所有工具的 handler——**全部通过**。

于是我在真实的故事仓库中启动 MCP Server，通过管道发送 JSON-RPC 请求：

```bash
echo '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | node bin/index.ts mcp-server
```

💀 **输出为空。** 没有任何响应。

我以为是我的管道写法有问题。换了好几种方式：

```bash
# 方式 1：printf
printf '{"jsonrpc":"2.0","id":1,"method":"tools/list"}\n' | node bin/index.ts mcp-server

# 方式 2：文件重定向
printf '{"jsonrpc":"2.0","id":1,"method":"tools/list"}\n' > /tmp/req.json && node bin/index.ts mcp-server < /tmp/req.json

# 方式 3：保持 stdin 打开
{ printf '{"jsonrpc":"2.0","id":1,"method":"tools/list"}\n'; sleep 2; } | node bin/index.ts mcp-server
```

**全部无响应。**

更诡异的是，通过 Node.js 的 `spawnSync` 发送请求时，进程的退出码是 0（看起来"成功了"），但 stdout 和 stderr 都是空白。

那一刻我意识到：**这不是调用方式的问题，是我们的 MCP Server 有 Bug。**

但 404 个测试全绿啊！怎么会有 Bug？

---

## Bug #1：process.exit() 的幽灵

### 根因排查

我先去看 CLI 的入口文件 `bin/index.ts`：

```typescript
#!/usr/bin/env node
import { run } from "../src/cli.ts"

const exitCode = await run(process.argv)
process.exit(exitCode)
```

问题一目了然。

当用户执行 `story mcp-server` 时：

1. `run(process.argv)` 被调用
2. `run()` 内部调用 `runMcpServer(rootDir)` → 调用 `startMcpServer()` → 开始监听 stdin
3. **`run()` 立即返回 0**（因为 `startMcpServer()` 是"注册完监听器就返回"的异步模式，不会 block）
4. **`process.exit(0)` 立即执行** → 进程终止
5. stdin 中的 JSON-RPC 请求还没来得及被 readline 读取

**MCP Server 刚出生就死了。**

**修复**：

```typescript
#!/usr/bin/env node
import { run } from "../src/cli.ts"

const exitCode = await run(process.argv)

// MCP server 需要保持进程存活持续监听 stdin
// 进程退出由 server.ts 内部的 close/SIGINT 事件处理
if (process.argv[2] !== "mcp-server" && process.argv[2] !== "mcp") {
  process.exit(exitCode)
}
```

> ⚠️ **注意**：这个修复方案当时看起来没问题，但后来在同一天的测试中暴露了它的**局限性**——见下方的「Bug #1.5：同源 Bug 复发」。

**深层教训**：这是 CLI 工具转服务化时的**第一坑**：

| 模式                                      | 生命周期                  | 退出时机                            |
| ----------------------------------------- | ------------------------- | ----------------------------------- |
| **CLI 工具**                              | 执行完命令就退出          | `process.exit(exitCode)` 是正确做法 |
| **长期运行进程**（MCP Server / 守护进程） | 持续监听输入直到 EOF/信号 | 退出必须由**输入源**触发的回调控制  |

`process.exit()` 是无条件的、立即的、不可中断的。它不会等待 pending 的 IO、定时器或 Promise。在 MCP Server 的场景下，这个"特性"直接杀死了我们的 server。

---

## Bug #1.5：同源 Bug 复发——process.exit() 的第二次幽灵

### 现象

修复 Bug #1 后，我继续对 MCP Server 做测试。当天，我顺便想验证 `story build --watch` 的性能表现：

```bash
story build --watch
```

输出显示「👀 监听模式已启动，文件变更自动重建...」，但 **进程立刻退出**——`--watch` 模式根本没有开始监听文件变更。

我尝试修改一个故事文件：

```bash
echo "新内容" > "01-测试故事/text.md"
```

什么也没有发生。README 文件没有任何更新。

### 根因：枚举命令的白名单缺陷

我回头看 `bin/index.ts` 的修复代码：

```typescript
if (process.argv[2] !== "mcp-server" && process.argv[2] !== "mcp") {
  process.exit(exitCode)
}
```

这个逻辑的表述是：**「除了 mcp-server 和 mcp 之外，其他命令都执行 `process.exit()`」**。

但 `build --watch` 同样是**长期运行的进程**！它需要持续监听文件变更，直到收到 `SIGINT`。而这里只豁免了 MCP Server 两个命令——`build --watch` 不在白名单里，一样会被 `process.exit()` 立即杀死。

**MCP Server 的第一次 bug 修好了，同样的幽灵在 `build --watch` 上再次出现。**

### 修复：提炼「长期运行」这个抽象

正确的修复不是继续枚举更多命令，而是提炼出「**哪些命令是长期运行的**」这个本质属性：

```typescript
#!/usr/bin/env node
import { run } from "../src/cli.ts"

const exitCode = await run(process.argv)

// 长期运行的进程需要保持存活，进程退出由内部 close/SIGINT 事件处理：
// - MCP server：持续监听 stdin，退出由 server.ts 的 close/SIGINT 控制
// - build --watch：持续监听文件变更，退出由 build.ts 的 SIGINT 控制
const isLongRunning =
  process.argv[2] === "mcp-server" ||
  process.argv[2] === "mcp" ||
  (process.argv[2] === "build" && process.argv[3] === "--watch") ||
  (process.argv[2] === "b" && process.argv[3] === "--watch")

if (!isLongRunning) {
  process.exit(exitCode)
}
```

### 深层教训：修复 Bug 要提炼「抽象」，而非枚举「实例」

这是本次调试中**最大的反思**：

| 修复方式             | 代码形态                                     | 问题                                     |
| -------------------- | -------------------------------------------- | ---------------------------------------- |
| **枚举实例**（当时） | `if (cmd !== "mcp-server" && cmd !== "mcp")` | 新加一个长期运行命令就得回来改这行       |
| **提炼抽象**（最终） | `const isLongRunning = ...`                  | 任何新命令只需在这个集合里表达自己的属性 |

当代码中出现「排除列表」（`if (cmd !== "A" && cmd !== "B")`）时，说明你在**枚举具体命令**，而不是表达「哪些命令是长期运行的」这个**本质属性**。一旦有新的长期运行命令出现（比如 `--watch`），同样的 bug 就会复发。

#### 检查清单

如果你的 CLI 未来要加任何「持续监听」功能（watch / serve / daemon），第一时间检查 `bin/index.ts` 的 `isLongRunning` 列表——它必须包含新命令。

---

## Bug #2：console.log 的致命污染

### 惊喜：修好 Bug #1 后出现了部分响应

修复了 Bug #1 后，我惊喜地发现 `tools/list` 开始有响应了！但有响应的是：

- `tools/list` ✅
- `initialize` ✅
- 未知工具的错误响应 ✅

而 **异步的 `tools/call` 仍然无响应**（`scan_stories` / `read_chapter` / `validate`）。

我单独测试 `scan_stories`：

```bash
echo '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"scan_stories","arguments":{}}}' | node bin/index.ts mcp-server
```

还是空。

我换了个思路——直接在 Node 环境中调用 `loadStories()`：

```bash
node --experimental-strip-types -e "
import { loadStories } from './src/core/loader.ts';
const { stories } = await loadStories('/tmp/test-story-cli');
console.log('STORIES:', stories.length);
"
```

输出：

```text
📊 01-测试故事: 自动计算字数为 约 13 字（未写回，使用 --save-counts 持久化）
📊 02-二创故事: 自动计算字数为 约 13 字（未写回，使用 --save-counts 持久化）
📊 03-English-Story: 自动计算字数为 ~7 words（未写回，使用 --save-counts 持久化）
STORIES: 3
```

**找到了！** `loadStories()` 内部用 `console.log` 输出了"自动计算字数"的诊断日志。

### 为什么某个 `console.log` 就能杀死 MCP？

MCP 的 stdio 传输规范是 **stdout 是协议专用通道**：

```text
├── stdin ← 客户端发送 JSON-RPC 请求
├── stdout → 服务器返回 JSON-RPC 响应（协议专用，唯一合法输出）
└── stderr → 日志/警告/错误（人看的，不是协议看的）
```

当 MCP 客户端发送 `scan_stories` 请求，MCP Server 处理时先调用了 `loadStories()`，`console.log` 往 stdout 吐了一行 `📊 01-测试故事: ...` 日志。此时 stdout 变成了：

```text
📊 01-测试故事: 自动计算字数为 约 13 字...      ← 污染！
{"jsonrpc":"2.0","id":3,"result":{...}}        ← 真正的响应
```

MCP 客户端（VSCode / Claude Desktop / Cursor）在解析 stdout 时，期望每一行都是合法的 JSON-RPC 消息。结果第一行根本不是 JSON——

**客户端直接放弃解析，表现为"无响应"。**

顺带一提，MCP 的 stdio 传输还有一个换行符的硬性要求：**每条 JSON-RPC 消息必须以 `\n`（换行符）结尾**。如果你的服务器输出了一条不带换行的 JSON，客户端也会解析失败。这就是为什么 MCP 官方文档的 Debugging 页面明确指出：

> _"Local MCP servers should not log messages to stdout (standard out), as this will interfere with protocol operation."_

——官方早就警告过，但我们直到真实环境踩坑才真正理解这句话。

而且这种 Bug 特别隐蔽：

- 单测环境中，`scan_stories` 的 handler 被直接调用，stdout 内容没人解析 → 测试通过
- 真实环境中，MCP 客户端严格解析 stdout → 立刻崩溃

**修复**：

```typescript
// 修复前
if (!config.wordCount) {
  console.log(locale.autoWordCount(folder, story.wordCount, saveCounts))
}
```

```typescript
// 修复后
if (!config.wordCount) {
  // 使用 stderr 输出诊断信息，避免污染 MCP stdio 协议的 stdout 通道
  console.error(locale.autoWordCount(folder, story.wordCount, saveCounts))
}
```

同时 `loadStoryContentAsync` 中的 `console.log(locale.generatedText(...))` 也一并改掉。

**深层教训**：**stdio 协议中的 stdout 不是给你打日志的。** 它是两个进程之间的协议通道。任何额外的输出——哪怕是看起来无害的一行日志——都会导致协议解析失败。

这是一个**运行时静默失败**的问题：代码不会抛异常，测试不会失败，只有真实客户端会"莫名其妙"不工作。

> 在 MCP Server 中，`stdout = 协议`，`stderr = 日志`。永远不要混用。

---

## Bug #3：readline close 的异步竞态

### 又一个意外

修复了 Bug #2 后，我以为一切搞定了。但测试发现 `tools/call` 仍然有**概率性**无响应：有时能收到响应，有时不行。

我盯着 `src/mcp/server.ts` 的旧代码思考：

```typescript
export function startMcpServer(rootDir: string, tools: RegisteredTool[]): void {
  const rl = createInterface({ input: process.stdin, terminal: false })

  rl.on("line", async (line) => {
    // ... 解析并处理请求
    const response = await handleRequest(request, rootDir, tools)
    if (response) process.stdout.write(serializeMessage(response))
  })

  rl.on("close", () => {
    // 等待 stdout 刷新后再退出（避免输出被截断）
    process.stdout.write("", () => process.exit(0))
  })
  // ...
}
```

在管道模式下（`echo '...' | node bin/index.ts mcp-server`），stdin 在读入所有行后立即关闭，触发 `close` 事件。**`close` 触发时，`rl.on("line")` 中的异步 `await handleRequest()` 还没执行完！**

时序是这样的：

```text
时间 t0:  stdin 收到 JSON-RPC 请求行
时间 t1:  rl 触发 "line" 事件，进入 async 回调
时间 t2:  async 回调遇到 await handleRequest()，挂起（黄色区域 = 等待异步结果）
时间 t3:  stdin 读完所有行 → 触发 rl "close" 事件
时间 t4:  "close" 回调执行 process.stdout.write("", () => process.exit(0))
时间 t5:  进程退出，await handleRequest() 还没恢复 → 响应永远丢失
```

这就是**异步竞态**：`close` 通知"输入流已关闭"，但它不等你的 Promise 完成。

**修复**：用 `pending` Set 跟踪所有 in-flight 请求，在 `close` 时等待它们全部完成再退出：

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
    // 跟踪 in-flight 请求，确保 stdin 关闭时异步 handler 已完成
    const task = (async () => {
      const response = await handleRequest(request, rootDir, tools)
      if (response) process.stdout.write(serializeMessage(response))
    })()
    pending.add(task)
    task.finally(() => pending.delete(task))
  })

  rl.on("close", () => {
    // 等待所有 in-flight 请求完成后刷新 stdout 再退出（避免输出被截断）
    void Promise.allSettled([...pending]).then(() => {
      process.stdout.write("", () => process.exit(0))
    })
  })
  process.on("SIGINT", () => {
    rl.close()
  })
}
```

**深层教训**：在 Node.js 的事件循环中，**`readline` 的 `close` 事件只代表"输入流关闭"，不代表"你的异步回调已执行"**。

这是所有 stdio 协议服务器的通用问题：stdin EOF 到达时，你可能仍然有 queued 的 Promise。你需要显式地跟踪和等待它们：

1. 用一个集合维护所有 in-flight 操作
2. 在 `close` 或 `SIGINT` 时用 `Promise.allSettled` 等待
3. 然后再执行 `process.exit`

---

## 启发：测试的分层

这次调试给我最大的启发是**测试的分层价值**：

| 测试层级                                                   | 我们之前的覆盖 | 发现的问题                |
| ---------------------------------------------------------- | -------------- | ------------------------- |
| **单元测试**（直接调用 handler 函数）                      | ✅ 401 个全绿  | 无法发现 Bug #1 / #2 / #3 |
| **集成测试**（调用 `startMcpServer` 但不走真实进程）       | ❌ 没有        | —                         |
| **端到端测试**（spawnSync 真实子进程 + 真实 stdin/stdout） | ❌ 没有        | 一次性暴露全部 3 个 Bug   |

**单元测试全绿不代表系统可用。** 你需要在真正的进程中启动 server，通过真正的管道发送请求，解析真正的 stdout——因为只有端到端测试能捕捉"进程生命周期"和"协议完整性"这两个层面的问题。

```typescript
// tests/mcp-server.test.ts（我们新增的端到端测试）
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

test("MCP server 能响应异步 tools/call（scan_stories）", () => {
  const { stdout, stderr } = sendRequests(dir, [
    '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"scan_stories","arguments":{}}}',
  ])
  // stderr 不应包含 JsonRpcResponse 内容 → 防止 console.log/stdout 污染
  assert.ok(!stderr.includes("jsonrpc"))

  // 按行分割 + 过滤空行，而不是直接 JSON.parse(stdout.trim())。
  // 如果 stdout 混入了多行输出，trim() 只去首尾空白，中间换行会导致 JSON.parse 失败。
  const lines = stdout
    .split("\n")
    .map((l) => l.trim())
    .filter(Boolean)
  assert.ok(lines.length >= 1, "应至少有一条 JSON-RPC 响应")
  // 取最后一条（如果请求了多个响应，也可以按 id 查找对应行）
  const response = JSON.parse(lines[lines.length - 1] ?? "{}")
  // ...
})
```

这个测试会在**真实子进程**中启动 MCP Server，通过**真正的管道**发送 JSON-RPC 请求，并验证 stdout 的内容。如果未来有人往 `loadStories` 加一个 `console.log`，这个测试会立即失败。

> **后续验证（同日）**：Bug #1.5 修复后，我们补上了 `build --watch` 的端到端回归测试（`tests/watch.test.ts`）——用 spawnSync 启动真实子进程，断言「进程应保持存活」+「修改故事后 README 在 5 秒内被重建」。如果当时就有这个测试，Bug #1.5 在修复后当天就能被发现，而不是等到后续性能测试时偶然暴露。这正是"测试分层"的又一次验证：**单元测试无法覆盖进程生命周期，只有端到端测试可以。**

---

## 附 1：完整的排查流程（供参考）

```bash
# 1. 创建测试仓库
mkdir -p /tmp/test-story-cli && cd /tmp/test-story-cli
node /path/to/story-cli/bin/index.ts init
node /path/to/story-cli/bin/index.ts new "测试故事"

# 2. 启动 MCP Server（发现问题）
echo '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | node /path/to/story-cli/bin/index.ts mcp-server
# → 空输出（Bug #1）

# 3. 修复 #1 后 → tools/list 有响应，但 scan_stories 无响应（Bug #2 的 stdio 污染）

# 4. 单独验证 loadStories 的行为
node --experimental-strip-types -e "
import { loadStories } from './src/core/loader.ts';
await loadStories('/tmp/test-story-cli');
"
# → 看到 📊 日志出现在 stdout

# 5. 修复 #2 后 → 有时有响应有时没（Bug #3 的异步竞态）

# 6. 通过端到端测试反复验证
node --test tests/mcp-server.test.ts
# → 7 tests pass
```

## 附 2：补充调试工具 MCP Inspector

以上是"事后排查"的思路。如果你在开发阶段就接入 **[MCP Inspector](https://github.com/modelcontextprotocol/inspector)**（MCP 官方调试工具），很多问题可以在发布前被提前发现：

```bash
npx @modelcontextprotocol/inspector node /path/to/story-cli/bin/index.ts mcp-server
```

MCP Inspector 会启动一个可视化 Web 界面，让你：

- **查看所有工具列表 / 参数 schema**（发现注册问题）
- **逐个调用工具并观察原始响应**（发现 stdout 污染）
- **检查协议层通信日志**（发现握手失败 / 换行符问题）

它是 MCP Server 开发的"X 光机"——推荐所有 MCP Server 开发者在 CI/CD 前先过一遍 Inspector。

> 社区还有一些第三方辅助工具（如 `mcp-stdio-guard` 用于捕获 stdout 污染），但 Inspector 作为官方工具足以覆盖大部分场景。

## 附 3：AI 交互层的格式漂移

MCP Server 不仅要处理协议陷阱（#1 / #2 / #3），还要处理 **AI 交互层**的陷阱：

`create_story` 创建目录时会把标题中的空格转为连字符（如 `"AI 创作的故事"` → `02-AI-创作的故事`），但 LLM 可能回传原始空格形式（`"02-AI 创作的故事"`）——`safeFolder` 需要同时匹配两种变体才能命中。

**协议层的坑、交互层的坑，我们当天全踩了一遍。**

---

## 总结：三条铁律 + 一条元教训

如果你只带走三句话，再加上一条「关于修复 Bug 本身的教训」：

1. **`process.exit()` 只属于一次性 CLI 命令**。长期运行的进程（MCP Server / watch 模式 / 守护进程）必须由输入流/信号回调控制退出。豁免时要提炼「长期运行」这个抽象，不要枚举具体命令。
2. **stdout 是协议通道，不是日志通道**。任何 stdio 协议服务器中，非协议输出都是污染。诊断信息请走 stderr。
3. **`close` ≠ 所有操作完成**。用 `pending` Set + `Promise.allSettled` 显式等待异步操作。
4. **修复 Bug 要提炼「抽象」，而非枚举「实例」**。当代码中出现 `if (cmd !== "A" && cmd !== "B")` 这种排除列表时，说明你在枚举具体命令——新的长期运行命令出现时，同样的 Bug 会在新的地方复发。

这四个问题的共同点是：它们**无法通过单元测试发现**，只能在真实进程环境中暴露。所以——写完 handler 后，别忘了写一个 `spawnSync` 端到端测试。

请注意，这四条铁律是**语言无关**的——无论你是用 Node.js、Python 还是 Go 开发 stdio 服务器，`process.exit()` / stdout 污染 / 异步未等待 / 枚举而非抽象 这四类坑都存在。本文以 Node.js 为例，只是因为我们的项目恰好是 Node 栈。

---

本文基于 story-cli 项目的真实调试经历撰写。项目地址：[story-cli](https://github.com/yuelinghuashu/story-cli)
