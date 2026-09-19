---
title: 框架：流式事件到手了，SSE 还得自己写
description: 用 Eino 重写第 4 篇的流式服务：框架交给你的是 AsyncIterator 事件流（MessageStream 逐 chunk），本机实测热态首字节 28~50ms、完整回答 3.44s，冷态则是 2.81s / 7.50s（差近百倍，取决于模型是否常驻显存）。但 SSE 事件编码、Flush 时机、心跳与超时，框架里一行都没有——eino 核心与 ADK 路径 grep text/event-stream 只命中一行测试注释，而 eino-ext 里仅有的一份 SSE 是它自己调试服务的 internal 实现。另外框架替你填了第 4 篇那个 sc.Buffer 的坑，代价是你失去了旋钮。
date: 2026-09-11
series: go-agent
order: 7
tags:
  - Go
  - Agent
  - LLM
---

第 4 篇我们把 Agent 循环变成了常驻 HTTP 服务：SSE 逐 token 推送、多轮事件流，同一套代码改个环境变量就能切云端。那一篇里最花心思的不是业务逻辑，而是两个流式细节：**`arguments` 分片拼接**和 **`sc.Buffer` 的行上限**。

框架接管流式之后，这两件事归谁管？以及一个更实际的问题：**框架到底交给你什么？**

- 前置：已理解第 4 篇的 SSE 事件流架构与两个流式细节
- 环境：沿用第 1 篇的 Ollama + `llama3.1:8b`（Intel Arc A770）
- 代码：完整可运行版本在本篇 §6

> **版本声明**：本文基于 `github.com/cloudwego/eino v0.9.19` + `eino-ext/components/model/ollama v0.1.9` 实测（Go 1.27.1）。Eino 仍是 0.x，接口随时可能变；**正文只写行为，字段名以官方文档为准**。完整依赖见篇末附录。

## 1. 第 4 篇的两个流式细节，现在归谁管

先把第 4 篇那三行关键代码摆出来：

```go
// ① 第 4 篇：arguments 可能分片，必须按 index 累积拼接
if tc.Function.Arguments != "" {
	p.arguments.WriteString(tc.Function.Arguments) // 分片必须拼接
}

// ② 第 4 篇：把单行上限从 bufio.Scanner 默认的 64KB 提到 1MB
//    不调大会直接报 token too long 并中断流
sc.Buffer(make([]byte, 1024), 1<<20)
```

再加上第 4 篇自己承认"没做"的部分：**取消与心跳**（"本篇没做取消与心跳，生产差异见第 8 节"）。

第 4 篇的判定是：**分片拼接、整轮 `finish_reason`、`Flush()`，三件事缺一不可。** 那么换框架之后，这三件事和它们之外的几件事，分别归谁？

答案是前一半归框架，后一半一点都不归它。**解析上游 SSE 报文、按行读、`arguments` 分片拼接、判定整轮 `finish_reason`——这些框架内部都做了**（分片拼接还给了现成工具，见 §5）。但**写下游 SSE 的那三件事——`event:`/`data:`/空行的编码、`Flush()` 时机、心跳与超时——框架里一行都没有**（下一节会看到一个例外，而它恰好印证了这个分工）。

## 2. 框架交出来的是什么

打开流式只要一个开关：

```go
runner := adk.NewRunner(ctx, adk.RunnerConfig{
	Agent:           agent,
	EnableStreaming: true, // ← 唯一的开关
})
```

然后你会拿到一个**事件迭代器**，而不是字节流：

```go
iter := runner.Run(ctx, []*schema.Message{schema.UserMessage(prompt)})
for {
	ev, ok := iter.Next()
	if !ok {
		break
	}
	// ev.Err          —— 错误也在这里（Run 不返回 error）
	// ev.Output.MessageOutput.IsStreaming  —— 这一条是不是流式
	// ev.Output.MessageOutput.MessageStream —— *schema.StreamReader[*schema.Message]
}
```

要理解的关键一点：**框架交给你的是"结构化消息 chunk 的迭代器"，不是"可以往 TCP 上写的字节"。** 具体来说，`MessageStream` 的读取接口是：

```go
chunk, err := ev.Output.MessageOutput.MessageStream.Recv() // (chunk, error)
```

`chunk` 是一个 `*schema.Message`——里面有 `Content`、`ToolCalls`、`Role`。**框架已经帮你把上游报文解析成了结构化消息**（第 4 篇 §1 花了一整节对比的两种流式格式差别，在这里消失了）。但"把 `chunk.Content` 变成一个 SSE 事件"这件事，**没有任何函数帮你做**。

这不是我的推断。把整个 Eino 仓库 grep 一遍：

```text
$ grep -rniE "text/event-stream|http.Flusher|\bsse\b" \
    $(go env GOMODCACHE)/github.com/cloudwego/eino@v0.9.19 --include=*.go
.../adk/cancel_stream_race_test.go:32: // deltaThenStuckChatModel mimics an HTTP SSE provider that emits one delta,
```

命令只命中 **1 行**，而且是测试文件里的一句注释（拿 SSE 打比方说明一个测试桩的用途），不是实现。**eino 核心与 ADK 这条路径上没有任何 SSE 实现**：没有 `text/event-stream` 响应头，没有 `http.Flusher` 调用。第 4 篇 §2 那套"一条对话 = 一条 SSE 事件流"的架构，**在框架版里一行都不能省**。

（这条命令你可以自己跑一遍核对——结论经得起照抄。）

### 2.1 生态里有一份 SSE，但它在 `internal/` 下

上面那条命令只 grep 了 **eino 核心仓库**。把范围放宽到整个 Eino 生态（`eino-ext`——本篇用的 ollama、openai、embedding 组件都在这个仓库里），结果不一样：

```go
// eino-ext/devops/internal/apihandler/server.go:201,220
res.Header().Set("Content-Type", "text/event-stream")
res.Header().Set("Cache-Control", "no-cache")
res.Header().Set("Connection", "keep-alive")
...
if f, ok := res.(http.Flusher); ok {
    f.Flush()
}
```

这是 `github.com/cloudwego/eino-ext/devops` 模块（v0.1.9）——给 IDE 可视化调试插件用的本地调试服务，它要把图执行过程实时推给前端，于是自己写了一套 SSE：**三行响应头与 `http.Flusher` 断言和本篇 §6 的写法一字不差**，另外还多了两件我列在 §10「刻意简化 vs 生产做法」里的事——用 `sseMutex` / `sseRoutineNum` 做连接数限制，以及每个事件之后 `50ms` 的节流。

想看这段代码不用把它加进依赖，下载到模块缓存就行：

```bash
go mod download github.com/cloudwego/eino-ext/devops@v0.1.9
grep -rn "text/event-stream" $(go env GOMODCACHE)/github.com/cloudwego/eino-ext/devops@v0.1.9/internal/apihandler/
```

关键在于它放在 `internal/` 下——**是私有实现，你连复用都不行**。所以 SSE 层不是"框架还没做"，而是被判定为**每个使用者的传输层细节**：连框架作者自己写调试服务时也是照这个分工动手的。

## 3. 实测：流式的收益不是一个常数

流式到底值不值？我先按最自然的方式测了一遍：同一台机器、同一个模型、纯文本生成（不注册工具，避免小模型乱调工具干扰测量）。跑完得到一个很漂亮的数字——首字节 31ms，完整回答 3.46s，差了约一百倍。

**但这个数字只在一种状态下成立。** 复测时我发现同一份代码、同一个 prompt，首字节有时是 30ms，有时是 2.8 **秒**——差了近百倍。查下去才发现原因在第 1 篇：`OLLAMA_KEEP_ALIVE` 默认 5 分钟，模型空闲超时会被卸载，下一次请求要先把 5.3GB 权重重新搬进显存。用 `ollama ps` 确认了模型常驻状态，再用 `keep_alive=0` 强制卸载，两种状态各测若干次：

| 状态                          | chunk 数 | TTFC（首个 chunk） | 完整回答总耗时   |
| :---------------------------- | :------- | :----------------- | :--------------- |
| **热态**（模型常驻显存，n=5） | 138      | **28 ~ 50ms**      | **3.44 ~ 3.46s** |
| **冷态**（模型被卸载，n=3）   | 190      | **2.81s**          | **7.50 ~ 7.54s** |

从客户端侧（`curl` 打 §6 那个 SSE 服务端）观察与之一致：热态首字节 63ms / 总耗时 3.42s，冷态首字节 2.805s / 总耗时 7.21s。**两态差约 100 倍与 2.7 倍。**

冷态下用户仍要盯着空白等将近 3 秒才看到第一个字。**流式的收益不是常数，它取决于 keep-alive 是否命中**——这一点值得单独强调，因为它把第 1 篇的一个运维参数变成了用户体验参数：**`OLLAMA_KEEP_ALIVE` 调大不只是"减少重复加载"的优化，它直接决定你的用户是等 30ms 还是等 2.8 秒才看到第一个字。** 第 1 篇把它列在内存/卸载那一节，到这里才看出它真正的杠杆位置。

顺带一个复现性提醒：同一个 prompt、同一个 `seed=42`、`temperature=0`，模型重载前后生成的结果并不相同（热态稳定在 138 个 chunk，冷态稳定在 190 个）——所以"固定 seed 就能严格复现"这个假设跨模型重载并不成立。下文所有数字都标注了所处状态。

还有一处测量细节值得写清楚：连续 `curl` 观察事件到达节奏，间隔的**中位数是 23.2ms**（137 个事件、136 个间隔，平均 24.2ms），而**最大间隔是 50ms**——但超过 40ms 的只有 3 个。也就是说打字机效果基本是匀速的，偶有卡顿。**这个节奏不是模型的节奏，是 `Flush()` 的节奏**：模型吐 chunk 的节奏与网络、缓冲共同决定了用户看到的节奏。第 4 篇说"每发一个事件都要 `Flush()`"，在框架版里依然是你的责任。

## 4. 框架替你填了 sc.Buffer 的坑，但你失去了旋钮

这是本篇一个意外发现，也是我觉得最能说明"交出去的代价"的例子。

先说一个反直觉的事实：**框架内部也用了 `bufio.Scanner` 读上游**——和第 4 篇手写版一模一样的机制。ollama 组件依赖的客户端里：

```go
scanner := bufio.NewScanner(response.Body)
// increase the buffer size to avoid running out of space
scanBuf := make([]byte, 0, maxBufferSize)
scanner.Buffer(scanBuf, maxBufferSize)
```

也就是说，**第 4 篇踩过的那个坑（`bufio.Scanner` 默认 64KB 行上限），框架作者也踩过，并且在库里填好了。** 你不会遇到 `token too long`——这是好事。

但代价在这里：

```go
const maxBufferSize = 512 * format.KiloByte // = 512 * 1000 = 512000 字节
```

**这是一个硬编码常量，没有配置项。** 第 4 篇手写版把行上限设成 `1<<20`（1,048,576 字节），而且那个数字**由你决定**；Eino 的 ollama 组件给的是 512,000 字节，**由框架决定，你调不了**。

框架填的坑比第 4 篇手写版**小了一半**。正常情况下这无所谓；但如果单行真的超过 512KB（比如一个超大的 `tool_calls` 参数块），**框架版没有旋钮可调**——你只能换组件、换协议，或者回到底层自己读。

**这就是"交出去"的真实代价：坑被填了，但填成多大、以什么代价填，由框架决定。** 你从"必须自己做对"变成了"必须接受别人的默认值"。

## 5. arguments 分片：现在有现成工具了

第 4 篇那个 `p.arguments.WriteString(...)` 的手写累积，框架提供了现成替代：

```go
schema.ConcatMessages(msgs)         // 多个 Message 拼成一个
schema.ConcatMessageStream(s)       // 把一个 StreamReader[*Message] 拼成一个 Message
schema.ConcatToolResults(chunks)    // 工具结果分片拼接
```

**这部分确实可以交出去。**

不过有个实测细节值得记下来：我让模型触发一次工具调用，数了流式下 `arguments` 的分片段数——

```text
tool_calls arguments 收到的分片段数: 1
  片 1: "{\"a\":10,\"b\":2}"
```

**只有 1 片。** 这和第 4 篇的结论完全一致：Ollama 本地是**整段下发**，而 OpenAI 云端会把参数 JSON **拆成多段**（第 4 篇 §1 抓包对比过）。

所以结论是分层的：

- **本地 Ollama 走原生 `/api/chat`**：实测整段下发，分片逻辑实际不会触发
- **切云端之后**：分片一定会出现，这时用 `ConcatMessageStream` 比自己手写累积更省事
- **但"是否分片"这件事仍然是后端行为，不是框架承诺**——第 4 篇为此把代码写成"可能拆片"，这个谨慎在框架版里依然成立

## 6. 完整代码

一个最小 SSE 服务：Eino 负责"拿到 chunk"，下面这段负责"发出去"。

<details>
<summary>main.go 全文（点击展开）</summary>

```go
// 框架篇 2 的完整可运行代码：Eino 流式 + 自己写的 SSE 服务端。
//
// 分工：
//   - Eino：EnableStreaming 拿到 AsyncIterator，逐 chunk 读 MessageStream
//   - 本文件：SSE 事件编码、Flush、错误事件 —— 框架里这些一行都没有
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	ollamamodel "github.com/cloudwego/eino-ext/components/model/ollama"
)

const prompt = "用大约 150 字介绍一下 Go 语言的 goroutine 调度模型。"

func newAgent(ctx context.Context) *adk.ChatModelAgent {
	chatModel, err := ollamamodel.NewChatModel(ctx, &ollamamodel.ChatModelConfig{
		BaseURL: "http://localhost:11434",
		Model:   "llama3.1:8b",
		Timeout: 180 * time.Second,
	})
	if err != nil {
		panic(err)
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "stream-agent",
		Description: "流式 Agent",
		Instruction: "你是一个简洁的助手。",
		Model:       chatModel,
		// 本篇只测文本流式，不注册工具
		ToolsConfig:   adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{}},
		MaxIterations: 4,
	})
	if err != nil {
		panic(err)
	}
	return agent
}

func main() {
	addr := "127.0.0.1:18080"
	fmt.Printf("SSE 服务已启动: http://%s/stream\n", addr)

	http.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		// ===========================================
		// 这三行全是"你自己的活"——eino 核心与 ADK 路径里没有任何 SSE 代码
		// ===========================================
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		// 用请求的 ctx：客户端断开时，流式生成会被取消
		ctx := r.Context()
		runner := adk.NewRunner(ctx, adk.RunnerConfig{
			Agent:           newAgent(ctx),
			EnableStreaming: true, // ← 唯一的开关
		})

		iter := runner.Run(ctx, []*schema.Message{schema.UserMessage(prompt)})

		n := 0
		for {
			ev, ok := iter.Next()
			if !ok {
				break
			}
			// 错误也走事件流（Run 本身不返回 error）
			if ev.Err != nil {
				fmt.Fprintf(w, "event: error\ndata: %v\n\n", ev.Err)
				flusher.Flush()
				break
			}
			if ev.Output == nil || ev.Output.MessageOutput == nil || !ev.Output.MessageOutput.IsStreaming {
				continue
			}

			// 逐 chunk 读框架交出来的流
			for {
				chunk, err := ev.Output.MessageOutput.MessageStream.Recv()
				if err != nil {
					break // io.EOF 表示这段流结束
				}
				if chunk.Content == "" {
					continue
				}
				n++
				// SSE 事件格式 + 显式 Flush —— 框架不管这两件事
				fmt.Fprintf(w, "event: token\ndata: %s\n\n",
					strings.ReplaceAll(chunk.Content, "\n", " "))
				flusher.Flush()
			}
		}
		fmt.Fprintf(w, "event: done\ndata: %d chunks\n\n", n)
		flusher.Flush()
	})

	if err := http.ListenAndServe(addr, nil); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
```

</details>

## 7. 运行结果（本机实测）

启动服务后，用 `curl -N` 观察事件流（带到达时间戳）：

```text
$ curl -sN http://127.0.0.1:18080/stream
18:39:00.563|event: token
18:39:00.564|data: Go
18:39:00.564|
18:39:00.587|event: token
18:39:00.588|data:
18:39:00.589|
18:39:00.611|event: token
18:39:00.612|data: 语言
18:39:00.613|
18:39:00.636|event: token
18:39:00.637|data: 的
18:39:00.638|
18:39:00.661|event: token
18:39:00.662|data:  gor
18:39:00.663|
18:39:00.686|event: token
18:39:00.687|data: outine
18:39:00.688|
```

客户端侧统计：

```text
token 事件数: 137
data: 137 chunks          ← done 事件
[curl] 首字节=0.030950s 总耗时=3.463165s
```

两点观察：

1. **每个 chunk 一个 SSE 事件**，`event: token` + `data:` + 空行，格式部分是第 4 篇的设计，我在框架版里**一字未改**——因为框架没提供任何替代品。
2. **事件间隔中位数 23.2ms、最大 50ms**（136 个间隔里只有 3 个超过 40ms），这就是 `Flush()` 的实际节奏。也就是说，**用户感知的"打字机效果"是你 `Flush()` 节奏的直接产物**，而不是模型吐字的节奏。

## 8. 坑与对照（实测验证）

| 现象                           | 原因                                                             | 处理                                                      |
| :----------------------------- | :--------------------------------------------------------------- | :-------------------------------------------------------- |
| 浏览器一直转圈、不出字         | 只写了 `Content-Type`，没调 `Flush()`                            | 每发一个事件都 `Flush()`                                  |
| 以为框架会帮你发 SSE           | 框架里零 SSE 代码（grep 可验证）                                 | SSE 编码、Flush、心跳全部自己写                           |
| 客户端断开后模型还在生成       | 没把请求 `ctx` 传进 `runner.Run`                                 | 用 `r.Context()`，断开即取消                              |
| 错误没被处理、连接挂死         | `Run` 不返回 error，错误只在 `ev.Err` 里                         | 事件循环里检查 `ev.Err`，并推一个 `event: error` 给客户端 |
| 上游超长行报 `token too long`  | 框架内部行上限是 512,000 字节的硬编码常量                        | 换组件或自己读上游；框架版**没有旋钮**                    |
| `arguments` 分片拼接要自己写吗 | 框架有 `ConcatMessageStream` 等工具                              | 用框架的；但本地 Ollama 实测整段下发，云端才分片          |
| 首字节延迟忽高忽低             | 模型被卸载后要重新加载权重（实测热态 28~50ms vs 冷态 **2.81s**） | 调大 `keep_alive` 保活（第 1 篇的 `OLLAMA_KEEP_ALIVE`）   |

## 9. 交出去 vs 自己管

| 交出去（框架做）                                  | 自己管（框架没有）                                 |
| :------------------------------------------------ | :------------------------------------------------- |
| 读上游流、解析报文成结构化 chunk                  | **下游 SSE 事件编码**（`event:`/`data:`/空行）     |
| 把两种流式格式差异抹平（第 4 篇 §1）              | **`Flush()` 时机**（决定用户看到的打字节奏）       |
| `EnableStreaming` 的事件封装                      | **心跳、超时、断连处理**                           |
| `arguments` 分片拼接工具（`ConcatMessageStream`） | **是否要按分片处理**的判断（后端行为，非框架承诺） |
| 上游行缓冲（512KB 硬编码）                        | **行上限该多大**（第 4 篇手写版自己调成 1MB）      |
| ——                                                | **错误如何呈现给客户端**（框架只给 `ev.Err`）      |

一句话：**框架帮你省掉的是"读懂上游"，省不掉的是"写给下游"。**

## 10. 刻意简化 vs 生产做法

| 本篇/框架的做法             | 生产环境的做法                                    |
| :-------------------------- | :------------------------------------------------ |
| 直接读 `r.Context()` 做取消 | 取消 + 心跳（`: ping` 注释行）+ 空闲超时          |
| 错误直接塞进 `event: error` | 区分错误类型，映射成不同的 SSE 事件与用户话术     |
| 每个 chunk 一个事件         | 按时间窗/字节数合并，减少事件数量                 |
| 单次请求单条流              | 多轮 Agent 的中间事件（工具调用、工具结果）都要推 |
| 行上限依赖框架常量          | 自己控制上游读取与缓冲，或选可配置的组件          |
| 无并发与限流控制            | 连接数限制、背压、单用户并发限制                  |

## FAQ

| 问题                               | 解决                                                                                                           |
| :--------------------------------- | :------------------------------------------------------------------------------------------------------------- |
| 用了框架还需要自己写 SSE 吗？      | 需要。eino 核心没有 SSE 代码，`Flush()` 只能你自己调；eino-ext 里唯一那份在调试服务的 `internal/` 下，复用不了 |
| 那我用框架图什么？                 | 图"上游那一半"：解析、格式差异抹平、分片拼接工具、循环与工具调度                                               |
| 为什么首字节这么快、总耗时这么长？ | 首字节 ≈ 31ms（模型开始吐字），完整回答 ≈ 3.46s（生成完整段落）                                                |
| 要不要把第 4 篇的代码改成框架版？  | 如果只是 SSE 服务且逻辑简单，收益有限；要接多 Agent、多后端、可观测性时收益明显                                |
| 框架升级会不会推翻本篇结论？       | 会。所以本篇写**行为**（"框架不含 SSE"、"行上限是常量"）而非字段名，动手前按附录版本重跑                       |

## 结论

1. **框架接管的是上游**：读流、解析报文、抹平格式差异、提供分片拼接工具，这部分确实省事；
2. **SSE 依然是你的活**：`event:`/`data:` 编码、`Flush()` 时机、心跳、超时，框架里一行都没有——eino 核心既没有实现，生态里唯一那份也锁在调试服务的 `internal/` 下（§2.1）；
3. **流式的收益与框架无关，但取决于模型是否常驻**：热态首字节 28~50ms vs 完整回答 3.44s（约 100 倍），冷态则是 2.81s vs 7.50s（约 2.7 倍）——**`OLLAMA_KEEP_ALIVE` 是这件事真正的旋钮**；
4. **填坑是有代价的**：框架替你填了第 4 篇那个上游行缓冲的坑，但填成 512,000 字节的硬编码常量——你从"自己做对"变成"接受别人的默认值"；
5. 记忆与检索是下一篇的主题：**框架把检索调用封好了，但检索质量还是你的责任。**

下一篇预告：**《框架：检索调用被封好了，检索质量没有》**。

## 附录：版本与实测环境

**依赖版本**（`go.mod` 锁定，实测日期 2026-09-11）：

```text
github.com/cloudwego/eino                             v0.9.19
github.com/cloudwego/eino-ext/components/model/ollama v0.1.9
github.com/eino-contrib/ollama                        v0.1.0
```

**实测环境**：Go 1.27.1 / Ollama + `llama3.1:8b` / Intel Arc A770 / `temperature=0, seed=42`。

**关键数字的复现方式**：

- TTFC 与 chunk 数：不注册工具，直接 `runner.Run` 后逐 chunk `Recv()`，用 `time.Since` 记首个 chunk 的时刻
- 首字节与总耗时：`curl -sN -w "\n首字节=%{time_starttransfer}s 总耗时=%{time_total}s"`
- 事件节奏：`curl -sN ... | while read line; do echo "$(date +%H:%M:%S.%3N) $line"; done`

**两个源码位置**（v0.9.19，换版本请重查）：

- ollama 组件的上游读取与行上限：`eino-contrib/ollama/api/client.go`（`maxBufferSize = 512 * format.KiloByte`）
- 流式事件结构：`eino/adk/interface.go`（`TypedMessageVariant` / `TypedAgentEvent`）

## 本文结论是否仍成立（自己就能验）

本篇最容易腐烂的是两个"否定式"结论——"框架没有 SSE"和"行上限由框架定死"。这里正好是**必须分清两个范围**的地方：**核心没有**不等于**生态里没有**。下面每条都标了要付多大代价：**一条命令** / **改几行**。

### 一条命令就能验的

**① eino 核心里还是没有 SSE 吗？**

```bash
grep -rniE "text/event-stream|http.Flusher|\bsse\b" \
  $(go env GOMODCACHE)/github.com/cloudwego/eino@<你的版本> --include=*.go
```

本篇实测只命中 1 行测试文件注释。**如果哪天它开始命中实现代码**，说明 eino 核心补上了 SSE——§2 的论证要重写（但结论未必翻，还得看它是否导出成可复用组件）。

**② 生态里那份"藏在 internal 下"的 SSE 还在吗？**

```bash
go mod download github.com/cloudwego/eino-ext/devops@v0.1.9   # 取到缓存即可，不必加进依赖
grep -rn "text/event-stream" \
  $(go env GOMODCACHE)/github.com/cloudwego/eino-ext/devops@*/internal/apihandler/
```

本篇实测 `devops` v0.1.9 里**有**命中——框架自己调试服务手写的 SSE，放在 `internal/` 下不对外复用（见 §2.1）。**真正的信号是它哪天从 `internal/` 里搬出来变成公开组件**，那时 §2.1、§7 与结论第 2 条才要按"框架开始托管 SSE"重写。**只是 `internal/` 里的实现本身变了，不影响本文任何结论。**

**③ 上游行上限还是硬编码吗？**

```bash
grep -rn "maxBufferSize" $(go env GOMODCACHE)/github.com/eino-contrib/ollama@*/api/client.go
```

本篇实测是 `512 * format.KiloByte`（= 512,000 字节）的常量。**如果它变成了可配置字段**，§4 的"你失去了旋钮"就要改成"旋钮换了地方"——那其实是框架把坑填好了。

### 改几行就能验的

**④ 冷/热两态的收益差还在吗？**

把你第 4 篇那个流式服务换个上游：别再自己 `bufio.Scanner` 读 SSE，改成用 Eino 的 `runner.Run` 拿 `MessageStream`，其余 SSE 写回原样不动。记录首字节和总耗时两个数。

**热态**（模型已加载）应当首字节几十毫秒、总耗时三四秒；**冷态**要先让模型卸载：

```bash
curl -s -X POST http://localhost:11434/api/generate -d '{"model":"llama3.1:8b","keep_alive":0}'
sleep 3   # 确认 ollama ps 里没有 llama3.1:8b
```

冷态首字节应当落在**秒级**。**具体数字随机器变，但"两态差近百倍"这个结构性结论不该变**——如果哪天冷态也只要几十毫秒，那是 Ollama 改了模型加载策略，不是框架变了。

**⑤ `arguments` 还是整段下发吗？**

让模型触发一次工具调用，读 `MessageStream` 时统计 `tc.Function.Arguments` 收到了几片。
**成立**：本地 Ollama 路径只收到 1 片（整段下发）。**变了**：若开始拆片，说明后端行为改了，§5 那段分层结论要更新——但代码本来按"可能拆片"写，不用改。

### 判读原则

**"内部实现变了"和"职责划分变了"是两件完全不同的事。** ①②③ 里真正需要重写文章的只有一种情况：SSE 实现从 `internal/` 里搬到公开 API。**其余任何变化都只影响数字或措辞，不影响结论。**
