---
title: 服务与迁移：把 Agent 变成流式 API，可切云端
description: 把 Agent 循环变成常驻 HTTP 服务：SSE 逐 token 推送与多轮事件流，同一套代码只改环境变量即可从本地 Ollama 切到 OpenAI 兼容云端。
date: 2026-09-08
series: go-agent
order: 4
tags:
  - Go
  - Agent
  - LLM
---

本篇做两件事：把第 3 篇的循环**变成常驻 HTTP 服务**（浏览器/客户端通过 SSE 实时接收 token），并让**同一套代码改个 `BASE_URL` 就能切到 OpenAI 云端**——把第 2 篇的“/v1 近似兼容”铁律落成代码。

- 前置：第 3 篇的多轮循环已跑通
- 运行要求：Go 1.27+（本篇服务端用到的 `"GET /health"` 方法路由语法自 Go 1.22 起可用，已满足）

## 1. 先看真实报文：两种流式格式的差别（本机抓包）

同样是"调用工具"请求，Ollama 的 `/v1`（OpenAI 格式）与原生 `/api/chat`（ndjson）流式报文**不一样**：

`/v1/chat/completions` + `"stream":true`（每行 `data: {...}`，末尾 `data: [DONE]`）：

```json
data: {"id":"chatcmpl-876",...,"choices":[{"index":0,"delta":{"role":"assistant","content":"","tool_calls":[{"id":"call_0sfy5hbq","index":0,"type":"function","function":{"name":"get_current_time","arguments":"{}"}}]},"finish_reason":null}]}

data: {"id":"chatcmpl-876",...,"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]
```

`/api/chat` + `"stream":true`（每行一个 JSON，无 `data:` 前缀、无 [DONE]）：

```json
{"model":"llama3.1:8b","created_at":"2026-09-07T14:09:28.37Z","message":{"role":"assistant","content":"","tool_calls":[{"id":"call_6dem57qn","function":{"index":0,"name":"get_current_time","arguments":{}}}]},"done":false}
{"model":"llama3.1:8b","created_at":"2026-09-07T14:09:28.40Z","message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","eval_count":14,...}
```

四个关键差异：

| 差异点           | `/v1`（OpenAI 格式）                | `/api/chat`（原生）                        |
| ---------------- | ----------------------------------- | ------------------------------------------ |
| 报文包装         | `data: {json}`，结尾 `data: [DONE]` | 裸 ndjson，无 [DONE]，靠 `done:true` 判尾  |
| 工具调用结束标记 | `finish_reason:"tool_calls"`        | `done_reason:"stop"`（**即使调用了工具**） |
| `arguments` 形态 | JSON **字符串**（`"{}"`）           | JSON **对象**（`{}`）                      |
| 兼容性           | 与 OpenAI 一致，可切云端            | Ollama 独有                                |

**结论**：想"同一套代码可切云端"，内部就统一走 `/v1` 格式；原生 `/api/chat` 只适合死磕 Ollama 的场景。本篇服务因此只实现一个 OpenAI 兼容客户端。

## 2. 架构：一次对话 = 一条 SSE 事件流

```
客户端 ──POST /chat──>  Go 服务 ──/v1/chat/completions stream──> Ollama / 云端
        <── SSE 事件流 ──   (多轮循环：模型输出+工具调用穿插推送)
```

事件类型（`event:` 字段）：

| 事件             | 含义                                     |
| ---------------- | ---------------------------------------- |
| `delta`          | 模型输出的一个 token（最终回答逐字出现） |
| `round`          | 新的一轮开始、模型请求了 N 个工具        |
| `tool`           | 工具执行结果（含报错回喂内容）           |
| `answer`         | 最终回答组装完毕                         |
| `error` / `done` | 异常 / 会话结束                          |

浏览器端只需 `fetch` 后按行读 SSE；curl 测试则如下（见第 5 节真实输出）。

> **SSE 最小知识（读懂本节代码只需要这三条）**：① 一条事件 = `event: 类型` + 若干 `data:` 行 + 一个**空行**收尾，客户端按行读、遇空行结束当前事件（SSE 即 Server-Sent Events，浏览器原生的服务端推送）；② 服务器每发一个事件都要 `Flush()`，否则第一批 token 会被缓冲区攒着，浏览器要等很久才看到；③ 连接长开，断开由客户端或超时决定——本篇没做取消与心跳，生产差异见第 8 节。

## 3. 两个最容易出错的流式细节（代码已做兼容处理）

**① `delta.tool_calls` 的 `arguments` 可能是分片的。** OpenAI 云端流式时会把一个工具调用的参数 JSON **拆成多段**下发，客户端必须按 `index` 把多段拼起来；Ollama 本地实测是整段下发（见第 1 节抓包），但代码按"可能拆片"来写，两边都能跑：

```go
if tc.Function.Arguments != "" {
    p.arguments.WriteString(tc.Function.Arguments) // 分片必须拼接
}
```

**② `content` 可能是空串、`finish_reason` 在最后一行才出现。** 判断"这轮要不要执行工具"，永远以**累积完一整轮后**的 `finish_reason == "tool_calls"` / `tool_calls` 非空为准，不要在收到第一个 chunk 时就下结论。

另：代码里的 `sc.Buffer(make([]byte, 1024), 1<<20)` 把单行上限从 `bufio.Scanner` 默认的 64KB 提到 1MB——SSE 的一行（整段 `data:`）可能很长，不调大会直接报 `token too long` 并中断流。

## 4. 完整代码

<details>
<summary>main.go 全文（点击展开）</summary>

```go
// 第 4 篇演示：把 Agent 循环变成常驻 HTTP 服务（SSE 流式 + 可切云端）
//
// 设计要点：
//  1. 只实现一个 OpenAI 兼容客户端（/v1/chat/completions），通过
//     BASE_URL 指向本地 Ollama 或任意 OpenAI 兼容云服务——同一套代码可切换；
//  2. 用流式（stream:true）做多轮工具循环：每一轮的模型输出实时
//     以 SSE 转发给浏览器/客户端，工具调用与结果也以事件形式发出；
//  3. delta 解析做到"防呆"：content 可能为 "" 或 null、tool_calls 的
//     arguments 可能被拆成多个分片（OpenAI 会拆，Ollama 一次性给全），
//     按 index 累积拼接，两种服务端都能正确处理。
//
// 运行：
//
//	OLLAMA_BASE="http://localhost:11434/v1" OLLAMA_MODEL=llama3.1:8b go run main.go
//	默认监听 :8899。
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ===========================================
// 消息模型（与 /v1/chat/completions 对应）
// ===========================================

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type Tool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

// ===========================================
// 流式 chunk 的解析结构
// ===========================================

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

// pendingCall 累积一个"还没发完"的工具调用（按 index 对齐）。
type pendingCall struct {
	index     int
	id        string
	name      string
	arguments strings.Builder
}

// ===========================================
// 工具注册表（与第 3 篇相同，省注释）
// ===========================================

type tool struct {
	description string
	parameters  map[string]any
	run         func(args json.RawMessage) (string, error)
}

var tools = map[string]*tool{
	"get_current_time": {
		description: "获取当前的日期和时间",
		parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		run: func(args json.RawMessage) (string, error) {
			return time.Now().Format("2006-01-02 15:04:05"), nil
		},
	},
	"add": {
		description: "计算两个整数 a 与 b 的和",
		parameters: map[string]any{"type": "object", "properties": map[string]any{
			"a": map[string]any{"type": "integer"},
			"b": map[string]any{"type": "integer"},
		}, "required": []string{"a", "b"}},
		run: intTool(func(a, b int) (string, error) { return fmt.Sprintf("%d", a+b), nil }),
	},
	"multiply": {
		description: "计算两个整数 a 与 b 的乘积",
		parameters: map[string]any{"type": "object", "properties": map[string]any{
			"a": map[string]any{"type": "integer"},
			"b": map[string]any{"type": "integer"},
		}, "required": []string{"a", "b"}},
		run: intTool(func(a, b int) (string, error) { return fmt.Sprintf("%d", a*b), nil }),
	},
	"divide": {
		description: "计算两个整数 a 除以 b 的商（整除）",
		parameters: map[string]any{"type": "object", "properties": map[string]any{
			"a": map[string]any{"type": "integer"},
			"b": map[string]any{"type": "integer"},
		}, "required": []string{"a", "b"}},
		run: intTool(func(a, b int) (string, error) {
			if b == 0 {
				return "", errors.New("除数不能为 0")
			}
			return fmt.Sprintf("%d", a/b), nil
		}),
	},
}

func intTool(fn func(a, b int) (string, error)) func(json.RawMessage) (string, error) {
	return func(args json.RawMessage) (string, error) {
		var p struct {
			A int `json:"a"`
			B int `json:"b"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return "", fmt.Errorf("参数解析失败（应为 {\"a\":整数,\"b\":整数}）：%v", err)
		}
		return fn(p.A, p.B)
	}
}

func toolDefs() []Tool {
	var out []Tool
	for name, t := range tools {
		var td Tool
		td.Type = "function"
		td.Function.Name = name
		td.Function.Description = t.description
		td.Function.Parameters = t.parameters
		out = append(out, td)
	}
	return out
}

// ===========================================
// OpenAI 兼容客户端（BASE_URL 可切本地/云端）
// ===========================================

type client struct {
	baseURL string // 例如 http://localhost:11434/v1 或 https://api.openai.com/v1
	apiKey  string
	model   string
	http    *http.Client
}

func newClient() *client {
	base := os.Getenv("OLLAMA_BASE")
	if base == "" {
		base = "http://localhost:11434/v1"
	}
	key := os.Getenv("OLLAMA_API_KEY")
	if key == "" {
		key = "ollama" // Ollama 忽略 key，OpenAI 需要填真 key
	}
	model := os.Getenv("OLLAMA_MODEL")
	if model == "" {
		model = "llama3.1:8b"
	}
	return &client{baseURL: base, apiKey: key, model: model,
		http: &http.Client{Timeout: 5 * time.Minute}}
}

// streamChat 发一次流式请求，把模型输出累积成一条 assistant 消息。
// callback 用于把每个 token 实时推给下游（SSE）。
func (c *client) streamChat(messages []Message, onDelta func(string)) (Message, string, error) {
	body, _ := json.Marshal(map[string]any{
		"model":    c.model,
		"messages": messages,
		"tools":    toolDefs(),
		"stream":   true,
	})
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Message{}, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return Message{}, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return Message{}, "", fmt.Errorf("upstream %d: %s", resp.StatusCode, string(b))
	}

	var (
		acc       strings.Builder // 累积本轮 content（最终回答）
		calls     []*pendingCall  // 累积本轮 tool_calls（按 index）
		finish    string
		callByIdx = map[int]*pendingCall{}
	)

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var ch streamChunk
		if err := json.Unmarshal([]byte(data), &ch); err != nil {
			continue // 忽略无法解析的行（兼容性防呆）
		}
		for _, choice := range ch.Choices {
			if choice.FinishReason != "" {
				finish = choice.FinishReason
			}
			d := choice.Delta
			if d.Content != "" {
				acc.WriteString(d.Content)
				onDelta(d.Content)
			}
			for _, tc := range d.ToolCalls {
				p := callByIdx[tc.Index]
				if p == nil {
					p = &pendingCall{index: tc.Index}
					callByIdx[tc.Index] = p
					calls = append(calls, p)
				}
				if tc.ID != "" {
					p.id = tc.ID
				}
				if tc.Function.Name != "" {
					p.name = tc.Function.Name // 分片场景下若拆名则追加，此处取简版
				}
				p.arguments.WriteString(tc.Function.Arguments) // arguments 可能被拆成多片，必须拼接
			}
		}
	}

	msg := Message{Role: "assistant", Content: acc.String()}
	for _, p := range calls {
		var tc ToolCall
		tc.ID = p.id
		tc.Type = "function"
		tc.Function.Name = p.name
		tc.Function.Arguments = p.arguments.String()
		msg.ToolCalls = append(msg.ToolCalls, tc)
	}
	return msg, finish, nil
}

// ===========================================
// HTTP 服务
// ===========================================

type server struct {
	client *client
}

func main() {
	s := &server{client: newClient()}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "model": s.client.model})
	})
	mux.HandleFunc("POST /chat", s.handleChat)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8899"
	}
	fmt.Printf("stream-server 已启动: http://localhost:%s  (base=%s model=%s)\n",
		port, s.client.baseURL, s.client.model)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		fmt.Println("启动失败:", err)
	}
}

// handleChat：多轮 Agent 循环 + SSE 实时推送。
func (s *server) handleChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []Message `json:"messages"`
		Stream   bool      `json:"stream"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Stream {
		s.runAgentSSE(w, req.Messages)
		return
	}
	// 非流式：跑同一套逻辑，把事件收进内存，最后返回 JSON
	events := &eventSink{}
	s.runAgent(events, req.Messages)
	answer, _ := events.lastAnswer()
	json.NewEncoder(w).Encode(map[string]any{"answer": answer, "events": events.list})
}

type event struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

// eventSink 收集事件（非流式路径用）。
type eventSink struct{ list []event }

func (e *eventSink) emit(t, d string) { e.list = append(e.list, event{t, d}) }
func (e *eventSink) lastAnswer() (string, bool) {
	for i := len(e.list) - 1; i >= 0; i-- {
		if e.list[i].Type == "answer" {
			return e.list[i].Data, true
		}
	}
	return "", false
}

// sseWriter 直接把事件写给浏览器（流式路径用）。
type sseWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

// emit 写一个 SSE 事件。data 可能含换行（模型的段落/空行 token），必须按行
// 拆成多条 data: 行：SSE 规范中连续 data 行会以 \n 重新拼接，这样既保持帧
// 合法，又能逐字保留换行（若把 \n 直接写进单行 data，空行会提前终止事件、
// 后续裸行会被客户端丢弃）。
func (s *sseWriter) emit(t, d string) {
	fmt.Fprintf(s.w, "event: %s\n", t)
	for _, line := range strings.Split(d, "\n") {
		fmt.Fprintf(s.w, "data: %s\n", line)
	}
	fmt.Fprint(s.w, "\n")
	s.f.Flush()
}

// emitter 是两种输出目标的共同接口。
type emitter interface{ emit(typ, data string) }

// runAgent 是核心多轮循环：模型输出逐 token 推送；工具调用执行后推送结果。
func (s *server) runAgent(em emitter, history []Message) {
	const maxRounds = 8
	for round := 1; round <= maxRounds; round++ {
		msg, finish, err := s.client.streamChat(history, func(token string) {
			em.emit("delta", token) // 每个 token 实时推送
		})
		if err != nil {
			em.emit("error", err.Error())
			return
		}
		history = append(history, msg)

		if len(msg.ToolCalls) == 0 || finish != "tool_calls" {
			em.emit("answer", msg.Content)
			return
		}
		em.emit("round", fmt.Sprintf("第 %d 轮：模型请求 %d 个工具", round, len(msg.ToolCalls)))
		for _, tc := range msg.ToolCalls {
			result, err := runTool(tc)
			if err != nil {
				result = fmt.Sprintf("工具执行出错：%v。请修正参数后重试，或放弃这一步。", err)
			}
			em.emit("tool", fmt.Sprintf("%s(%s) -> %s", tc.Function.Name, tc.Function.Arguments, result))
			history = append(history, Message{Role: "tool", ToolCallID: tc.ID, Content: result})
		}
	}
	em.emit("error", "达到最大轮数")
}

func (s *server) runAgentSSE(w http.ResponseWriter, history []Message) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	f, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	sw := &sseWriter{w: w, f: f}
	s.runAgent(sw, history)
	sw.emit("done", "[DONE]")
}

func runTool(tc ToolCall) (string, error) {
	t, ok := tools[tc.Function.Name]
	if !ok {
		return "", fmt.Errorf("未知工具 %q（模型幻觉了工具名）", tc.Function.Name)
	}
	return t.run(json.RawMessage(tc.Function.Arguments))
}
```

</details>

## 5. 运行结果（Ollama 0.33.3 + llama3.1:8b，2026-09 实测）

```bash
# 启动（BASE_URL 指向本地 Ollama；切云端只需改环境变量，见第 6 节）
PORT=8899 go run main.go
```

`GET /health`：

```
{"model":"llama3.1:8b","status":"ok"}
```

一次完整 SSE 对话（浏览器视角收到的原始事件流）：

```bash
curl -sN -X POST http://localhost:8899/chat -H "Content-Type: application/json" \
  -d '{"stream":true,"messages":[{"role":"user","content":"现在几点了？顺便帮我算 7 乘以 8。"}]}'
```

```
event: round
data: 第 1 轮：模型请求 2 个工具

event: tool
data: get_current_time({}) -> 2026-09-07 22:10:16

event: tool
data: multiply({"b":8,"a":7}) -> 56

event: delta
data: 现在
event: delta
data: 是
event: delta
data: 22
event: delta
data: :
event: delta
data: 10
…
（其余 9 个 delta 事件从略：单 token 逐字到达，直到整句拼完）

event: answer
data: 现在是 22:10。7 乘以 8 等于 56。

event: done
data: [DONE]
```

要点：工具调用回合（`round`/`tool`）**先于**最终回答出现，最终回答的 token 逐字流式到达（`delta`），浏览器可以边收边渲染。非流式路径（`"stream":false`）返回同一份结果的 JSON，方便调试与测试。

## 6. 切云端：只改环境变量，不改代码

服务内部只认 OpenAI 兼容协议，所以"本地 Ollama ↔ 云端"只是换三个环境变量：

```bash
# 本地
OLLAMA_BASE="http://localhost:11434/v1" OLLAMA_API_KEY=ollama OLLAMA_MODEL=llama3.1:8b

# 云端（已实测：小米 MiMo API，OpenAI 兼容协议）
OLLAMA_BASE="https://api.xiaomimimo.com/v1" OLLAMA_API_KEY=sk-xxx OLLAMA_MODEL=mimo-v2.5-pro
```

第 2 篇的兼容铁律在这份代码里已经落实：

- 无工具时**省略 `tools` 字段**（`toolDefs()` 恒有工具，生产可改条件化）——避免踩空 `tools: []` 的差异；
- 判工具调用看 `finish_reason == "tool_calls"`，不只看 content；
- `delta.content` 判空同时兼容 `""` 与缺失。

### 云端实测（MiMo-v2.5-pro，2026-09-08）

同一份代码、同一个 curl 请求，只改环境变量指向 MiMo 云端：

```
event: round
data: 第 1 轮：模型请求 2 个工具

event: tool
data: get_current_time({}) -> 2026-09-08 16:48:21

event: tool
data: multiply({"a": 7, "b": 8}) -> 56

…（delta 事件从略，逐 token 到达；这里注意 `arguments` 带空格）

event: answer
data: 现在是 **2026年9月8日 16:48:21**。另外，**7 × 8 = 56**。还有其他需要帮忙的吗？😊

event: done
data: [DONE]
```

与本地 Ollama 对比：事件流结构完全一致（`round → tool → delta → answer → done`），代码零改动。唯一可注意的细微差异：MiMo 的 `arguments` 以带空格的 JSON 对象下发（`"a": 7`），Ollama 以紧凑字符串下发（`"{}"`），两种都被 `json.Unmarshal` 正确处理——代码第 3 节的防呆设计恰好覆盖了这类差异。

## 7. 坑与对照

| 现象                         | 原因                                 | 处理                                                                         |
| ---------------------------- | ------------------------------------ | ---------------------------------------------------------------------------- |
| SSE 断断续续/客户端收不全    | 没理解 `event:`/`data:`/空行分隔     | 按行读、遇到空行结束当前事件（见第 5 节原始流）                              |
| 工具调用参数丢失一半         | 把分片 arguments 当成了完整 JSON     | 按 `index` 累积拼接后再整体解析                                              |
| 模型已调工具却当普通回答处理 | 只看了第一行 chunk 就返回            | 收完整轮再判 `finish_reason`                                                 |
| 本地正常、云端行为不同       | /v1 是近似兼容，云端字段更严         | 已实测 MiMo 云端（§6），代码零改动；换其他服务前仍建议先跑一遍               |
| 长任务超时 / curl 提前断开   | 上游请求没有超时预算；或代理缓冲没关 | `http.Client.Timeout` 按需求调大 + 服务端 `context` 取消；SSE 需关闭代理缓冲 |

## 8. 刻意简化 vs 生产做法

| 刻意简化的地方           | 生产环境的做法                                             |
| ------------------------ | ---------------------------------------------------------- |
| 服务无鉴权、无限流       | API key / 中间件 / 每用户配额                              |
| 对话历史只存在单次请求内 | 会话隔离思路见第 5 篇；生产级会话存储/多租户不在本系列范围 |
| SSE 一个连接跑完整轮     | 任务队列 + 进度事件重连                                    |
| 工具固定 4 个写死        | 注册式插件 / 配置加载                                      |
| 已实测云端迁移           | 换其他 OpenAI 兼容服务前仍建议跑一遍兼容清单               |

## FAQ

| 问题                    | 解决                                                 |
| ----------------------- | ---------------------------------------------------- |
| 浏览器收不到流          | 服务端必须 `Flush()`；确认响应头 `text/event-stream` |
| 想比较 /api/chat 原生流 | 用第 1 节抓包命令自行对比；服务默认统一走 /v1        |
| 切云端报 401            | 检查 `OLLAMA_API_KEY`；Ollama 本地会忽略 key         |

## 结论

1. **服务化只多了"外壳"**：核心仍是第 3 篇的循环，加一层 HTTP + SSE 事件转发即可；
2. **流式细节决定成败**：分片 arguments 拼接、整轮判 `finish_reason`、`Flush()`，三件事缺一不可；
3. **可迁移性来自协议收敛**：只依赖 OpenAI 兼容子集，本地/云端切换 = 改环境变量；
4. 服务目前仍无记忆——**会话隔离、跨会话长期记忆与轻量 RAG** 是第 5 篇的主题（进程内演示形态）；生产级会话存储/多租户不在本系列范围。

下一篇预告：**《记忆：多会话与轻量 RAG》**。
