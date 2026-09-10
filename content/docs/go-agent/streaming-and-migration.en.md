---
title: "Service & Migration: Turning the Agent into a Streaming API You Can Move to the Cloud (Part 4)"
description: "Turning the agent loop into a long-running HTTP service: per-token pushes over SSE and a multi-round event stream, with the same code switching from local Ollama to an OpenAI-compatible cloud endpoint by changing an environment variable."
date: 2026-09-08
series: go-agent
order: 4
tags:
  - Go
  - Agent
  - LLM
---

This part does two things: it turns the loop from Part 3 **into a long-running HTTP service** (browsers/clients receive tokens in real time over SSE), and it makes **the same code switch to an OpenAI-compatible cloud endpoint by changing one `BASE_URL`** — putting Part 2's "/v1 is approximately compatible" iron rules into code.

- Prerequisite: the multi-round loop from Part 3 already works
- Requirements: Go 1.27+ (the repository's go.mod declares go 1.27.1; the `"GET /health"` method-routing syntax used by this part's server has been available since Go 1.22, so that is satisfied)

> Environment note: this article is based on measurements of **Ollama 0.33.3 + llama3.1:8b (A770/Vulkan, 2026-09)**; Ollama iterates fast, so treat `ollama serve --help` as the source of truth for environment variables, and the [official OpenAI compatibility docs](https://docs.ollama.com/api/openai-compatibility) as the source of truth for the `/v1` compatibility fields.

## 1. Real messages first: how the two streaming formats differ (captured on this machine)

For the same "call a tool" request, Ollama's `/v1` (OpenAI format) and the native `/api/chat` (ndjson) streaming messages are **not the same**:

`/v1/chat/completions` + `"stream":true` (each line is `data: {...}`, ending with `data: [DONE]`):

```json
data: {"id":"chatcmpl-876",...,"choices":[{"index":0,"delta":{"role":"assistant","content":"","tool_calls":[{"id":"call_0sfy5hbq","index":0,"type":"function","function":{"name":"get_current_time","arguments":"{}"}}]},"finish_reason":null}]}

data: {"id":"chatcmpl-876",...,"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]
```

`/api/chat` + `"stream":true` (one JSON per line, no `data:` prefix and no [DONE]):

```json
{"model":"llama3.1:8b","created_at":"2026-09-07T14:09:28.37Z","message":{"role":"assistant","content":"","tool_calls":[{"id":"call_6dem57qn","function":{"index":0,"name":"get_current_time","arguments":{}}}]},"done":false}
{"model":"llama3.1:8b","created_at":"2026-09-07T14:09:28.40Z","message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","eval_count":14,...}
```

Four key differences:

| Difference                 | `/v1` (OpenAI format)                        | `/api/chat` (native)                                   |
| -------------------------- | -------------------------------------------- | ------------------------------------------------------ |
| Message wrapping           | `data: {json}`, ending with `data: [DONE]`   | Bare ndjson, no [DONE]; the end is `done:true`         |
| End marker for a tool call | `finish_reason:"tool_calls"`                 | `done_reason:"stop"` (**even when a tool was called**) |
| Shape of `arguments`       | A JSON **string** (`"{}"`)                   | A JSON **object** (`{}`)                               |
| Compatibility              | Matches OpenAI, so you can move to the cloud | Ollama only                                            |

**Conclusion**: if you want "one codebase that can move to the cloud", standardize on the `/v1` format internally; the native `/api/chat` only suits setups that will never leave Ollama. The service in this part therefore implements just one OpenAI-compatible client.

## 2. Architecture: one conversation = one SSE event stream

```
client ──POST /chat──>  Go service ──/v1/chat/completions stream──> Ollama / cloud
       <── SSE event stream ──   (multi-round loop: model output and tool calls pushed interleaved)
```

Event types (the `event:` field):

| Event            | Meaning                                                                     |
| ---------------- | --------------------------------------------------------------------------- |
| `delta`          | One token of model output (the final answer appears character by character) |
| `round`          | A new round begins; the model requested N tools                             |
| `tool`           | A tool execution result (including the error text fed back)                 |
| `answer`         | The final answer has been assembled                                         |
| `error` / `done` | An exception / the end of the session                                       |

The browser side only needs to `fetch` and read the SSE line by line; testing with curl looks like this (see the real output in Section 5).

## 3. The two streaming details that break most easily (the code already handles both)

**① `delta.tool_calls`' `arguments` may arrive in fragments.** When streaming from the OpenAI cloud, the argument JSON of a single tool call can be **split across several chunks**, and the client has to reassemble them by `index`; measured locally, Ollama sends it in one piece (see the capture in Section 1), but the code is written to expect fragments, so it works against both:

```go
if tc.Function.Arguments != "" {
    p.arguments.WriteString(tc.Function.Arguments) // fragments must be concatenated
}
```

**② `content` may be the empty string, and `finish_reason` only shows up on the last line.** To decide "does this round need to execute tools?", always wait until **a whole round has been accumulated** and go by `finish_reason == "tool_calls"` / `tool_calls` being non-empty — never conclude from the first chunk you receive.

## 4. Full code (demo/stream-server/main.go)

<details>
<summary>Full source of demo/stream-server/main.go (click to expand)</summary>

```go
// Part 4 demo: turning the agent loop into a long-running HTTP service (SSE streaming + cloud-switchable)
//
// Design points:
//  1. only one OpenAI-compatible client (/v1/chat/completions) is implemented, and
//     BASE_URL points at local Ollama or any OpenAI-compatible cloud service — the
//     same code can switch between them;
//  2. streaming (stream:true) drives the multi-round tool loop: each round's model
//     output is forwarded to the browser/client over SSE in real time, and tool
//     calls and their results are emitted as events too;
//  3. delta parsing is defensive: content may be "" or null, and a tool call's
//     arguments may be split into several fragments (OpenAI splits them, Ollama
//     sends them whole), so they are accumulated by index — both kinds of server
//     are handled correctly.
//
// Run:
//
//	OLLAMA_BASE="http://localhost:11434/v1" OLLAMA_MODEL=llama3.1:8b go run ./demo/stream-server
//	Listens on :8899 by default.
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
// Message model (matching /v1/chat/completions)
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
// Structures for parsing streaming chunks
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

// pendingCall accumulates one tool call that has not finished arriving (aligned by index).
type pendingCall struct {
	index     int
	id        string
	name      string
	arguments strings.Builder
}

// ===========================================
// Tool registry (same as Part 3, comments omitted)
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
// OpenAI-compatible client (BASE_URL switches local/cloud)
// ===========================================

type client struct {
	baseURL string // e.g. http://localhost:11434/v1 or https://api.openai.com/v1
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
		key = "ollama" // Ollama ignores the key; OpenAI needs a real one
	}
	model := os.Getenv("OLLAMA_MODEL")
	if model == "" {
		model = "llama3.1:8b"
	}
	return &client{baseURL: base, apiKey: key, model: model,
		http: &http.Client{Timeout: 5 * time.Minute}}
}

// streamChat sends one streaming request and accumulates the model output into
// a single assistant message.
// callback pushes every token downstream (SSE) in real time.
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
		acc       strings.Builder // accumulates this round's content (the final answer)
		calls     []*pendingCall  // accumulates this round's tool_calls (by index)
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
			continue // ignore lines that cannot be parsed (compatibility guard)
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
					p.name = tc.Function.Name // with fragmented names this should append; the simple version takes it as-is
				}
				p.arguments.WriteString(tc.Function.Arguments) // arguments may be split across fragments and must be concatenated
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
// HTTP service
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

// handleChat: the multi-round agent loop plus real-time SSE pushes.
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
	// Non-streaming: run the same logic, collect the events in memory, return JSON at the end
	events := &eventSink{}
	s.runAgent(events, req.Messages)
	answer, _ := events.lastAnswer()
	json.NewEncoder(w).Encode(map[string]any{"answer": answer, "events": events.list})
}

type event struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

// eventSink collects events (used by the non-streaming path).
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

// sseWriter writes events straight to the browser (used by the streaming path).
type sseWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

// emit writes one SSE event. data may contain newlines (the model's paragraph
// and blank-line tokens), so it must be split into several data: lines: in the
// SSE spec consecutive data lines are rejoined with \n, which keeps the frame
// legal while preserving newlines exactly (writing \n directly into a single
// data line would let a blank line terminate the event early and the client
// would drop the bare lines that follow).
func (s *sseWriter) emit(t, d string) {
	fmt.Fprintf(s.w, "event: %s\n", t)
	for _, line := range strings.Split(d, "\n") {
		fmt.Fprintf(s.w, "data: %s\n", line)
	}
	fmt.Fprint(s.w, "\n")
	s.f.Flush()
}

// emitter is the shared interface of both output targets.
type emitter interface{ emit(typ, data string) }

// runAgent is the core multi-round loop: model output is pushed token by token,
// and tool results are pushed once the tool has run.
func (s *server) runAgent(em emitter, history []Message) {
	const maxRounds = 8
	for round := 1; round <= maxRounds; round++ {
		msg, finish, err := s.client.streamChat(history, func(token string) {
			em.emit("delta", token) // push every token in real time
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

## 5. Run results (measured on this machine)

```bash
# Start (BASE_URL points at local Ollama; moving to the cloud only changes environment variables, see Section 6)
PORT=8899 go run ./demo/stream-server
```

`GET /health`:

```
{"model":"llama3.1:8b","status":"ok"}
```

One complete SSE conversation (the raw event stream as the browser sees it):

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
event: delta
data: 。
event: delta
data: 7
event: delta
data:  乘
event: delta
data: 以
event: delta
data:  8
event: delta
data:  等
event: delta
data: 于
event: delta
data:  56
event: delta
data: 。

event: answer
data: 现在是 22:10。7 乘以 8 等于 56。

event: done
data: [DONE]
```

The point: the tool-calling round (`round`/`tool`) arrives **before** the final answer, and the final answer's tokens stream in character by character (`delta`), so a browser can render as it receives. The non-streaming path (`"stream":false`) returns the same result as JSON, which is convenient for debugging and tests.

## 6. Moving to the cloud: change environment variables, not code

The service only speaks the OpenAI-compatible protocol internally, so "local Ollama ↔ cloud" is just three environment variables:

```bash
# Local
OLLAMA_BASE="http://localhost:11434/v1" OLLAMA_API_KEY=ollama OLLAMA_MODEL=llama3.1:8b

# Cloud (measured: Xiaomi MiMo API, an OpenAI-compatible protocol)
OLLAMA_BASE="https://api.xiaomimimo.com/v1" OLLAMA_API_KEY=sk-xxx OLLAMA_MODEL=mimo-v2.5-pro
```

Part 2's compatibility iron rules are already in place in this code:

- With no tools, **omit the `tools` field** (`toolDefs()` always has tools; make it conditional in production) — avoiding the empty `tools: []` difference;
- Tool calls are detected from `finish_reason == "tool_calls"`, not from content alone;
- `delta.content` emptiness checks accept both `""` and a missing field.

### Cloud measurement (MiMo-v2.5-pro, 2026-09-08)

The same code and the same curl request, with only the environment variables pointing at the MiMo cloud:

```
event: round
data: 第 1 轮：模型请求 2 个工具

event: tool
data: get_current_time({}) -> 2026-09-08 16:48:21

event: tool
data: multiply({"a": 7, "b": 8}) -> 56

event: delta
data: 现在是 **202
event: delta
data: 6年9月8日
event: delta
data: 16:48:21
event: delta
data: **。

event: answer
data: 现在是 **2026年9月8日 16:48:21**。另外，**7 × 8 = 56**。还有其他需要帮忙的吗？😊

event: done
data: [DONE]
```

Compared with local Ollama: the event stream structure is exactly the same (`round → tool → delta → answer → done`) with zero code changes. The only subtle difference worth noting: MiMo sends `arguments` as a JSON object with spaces (`"a": 7`) while Ollama sends a compact string (`"{}"`), and `json.Unmarshal` handles both correctly — the defensive design in Section 3 covers exactly this kind of difference.

## 7. Pitfalls and comparisons

| Symptom                                                                 | Cause                                                               | Handling                                                                                                                                           |
| ----------------------------------------------------------------------- | ------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| SSE arrives in fits and starts / the client misses parts                | Not understanding `event:`/`data:`/blank-line separation            | Read line by line and end the current event at a blank line (see the raw stream in Section 5)                                                      |
| Half of a tool call's arguments go missing                              | Treating a fragmented `arguments` as complete JSON                  | Accumulate by `index`, concatenate, then parse the whole thing                                                                                     |
| The model already called a tool but it is treated as an ordinary answer | Returning after only the first chunk                                | Accumulate the whole round before checking `finish_reason`                                                                                         |
| Works locally, behaves differently in the cloud                         | /v1 is approximately compatible; the cloud is stricter about fields | Already measured against the MiMo cloud (§6) with zero code changes; still worth running through the checklist before switching to another service |
| Long tasks time out                                                     | The upstream request has no timeout budget                          | Raise `http.Client.Timeout` as needed + server-side `context` cancellation                                                                         |

## 8. Deliberately simplified vs. production practice

| Deliberately simplified                            | What production does                                                                                                        |
| -------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| No auth and no rate limiting on the service        | API keys / middleware / per-user quotas                                                                                     |
| Conversation history lives only inside one request | Session isolation is discussed in Part 5; production-grade session storage and multi-tenancy are outside this series' scope |
| One SSE connection runs the whole exchange         | A task queue plus progress events and reconnection                                                                          |
| Four tools, hard-coded                             | A plugin registry / configuration loading                                                                                   |
| Cloud migration already measured                   | Still worth running through the compatibility checklist before switching to another OpenAI-compatible service               |

## FAQ

| Question                                    | Fix                                                                                                               |
| ------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| curl disconnects before `event: done`       | Check the upstream timeout and `http.Client.Timeout`; SSE needs proxy buffering off                               |
| The browser receives no stream              | The server must `Flush()`; confirm the `text/event-stream` response header                                        |
| Want to compare the native /api/chat stream | Use the capture command from Section 1 to compare for yourself; the service goes through /v1 uniformly by default |
| 401 after switching to the cloud            | Check `OLLAMA_API_KEY`; local Ollama ignores the key                                                              |

## Conclusion

1. **Turning it into a service only adds a shell**: the core is still the loop from Part 3, plus an HTTP layer forwarding SSE events;
2. **Streaming details decide success or failure**: concatenating fragmented arguments, checking `finish_reason` on the whole round, and `Flush()` — all three are indispensable;
3. **Portability comes from converging on a protocol**: depending only on an OpenAI-compatible subset means switching local/cloud is a matter of environment variables;
4. The service still has no memory — **session isolation, cross-session long-term memory and lightweight RAG** are the subject of Part 5 (in-process demonstration form); production-grade session storage and multi-tenancy are outside this series' scope.

Next up: **"Memory: Multi-Session Facts and Lightweight RAG"**.
