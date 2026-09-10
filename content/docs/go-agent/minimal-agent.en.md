---
title: "Minimal Code: The Complete Single-Tool, Single-Round Loop (Part 2)"
description: "Running the minimal tool-calling loop in about 200 lines of Go (standard library only) — the model names a tool, Go executes it, the result goes back, the model answers — and spelling out the three iron rules of Ollama's “approximately compatible” /v1."
date: 2026-09-08
series: go-agent
order: 2
tags:
  - Go
  - Agent
  - LLM
---

This part is a **deliberately minimal example**: one tool, a single round, about 200 lines of Go (comments included), standard library only (`net/http`, `encoding/json`), running the complete "model names a tool → Go executes it → the result goes back → the model answers" loop.

- Who it's for: you know some Go and are writing your first agent tool call (prerequisite: Ollama and `llama3.1:8b` installed as in Part 1)
- Code: the full source is embedded in Sections 2 and 3 below; save it as `main.go` and run `go run main.go` in that directory
- Requirements: Go 1.27+

## 1. Interface baseline: /v1 is approximately compatible (know this before you write code)

> In a hurry to get something running? Jump straight to Section 2, get one conversation working with curl / Go, and come back to this section's compatibility details afterwards.

The code in this part calls `POST http://localhost:11434/v1/chat/completions` — it is merely the closest thing to OpenAI SDK syntax; officially it is positioned as an **OpenAI compatibility layer, not a field-by-field equivalent**. The differences in the table below were measured on Ollama 0.33.3 and checked against the official compatibility docs (the differences drift between versions — even the streaming message format was only aligned later); the code parts and Part 4 (turning this into a service, migrating to the cloud) both rely on it:

| Difference                      | Ollama (measured on 0.33.3)                                                                                                                                                                                                                                                                                                          | Official OpenAI                                                                 |
| ------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------- |
| `tool_choice` parameter         | Partially supported and unstable (measured on 0.33.3: `required` does force a call, `none` has no effect; the official compatibility docs do not list it as a supported field and upstream is still working on it: [#17921](https://github.com/ollama/ollama/issues/17921), [#11171](https://github.com/ollama/ollama/issues/11171)) | Supports `auto`/`required`/a named function; commonly used to force a call      |
| Empty `tools: []`               | Returns 200, same as sending no tools at all                                                                                                                                                                                                                                                                                         | Usually errors, or requires omitting the field (several SDKs write workarounds) |
| `message.content` on tool calls | The empty string `""`                                                                                                                                                                                                                                                                                                                | `null`                                                                          |
| `finish_reason`                 | `tool_calls` on tool calls (this version already matches OpenAI)                                                                                                                                                                                                                                                                     | `tool_calls`                                                                    |
| `n` / `user` / `logit_bias`     | Not supported                                                                                                                                                                                                                                                                                                                        | Supported                                                                       |
| Streaming messages              | Inconsistent with OpenAI at first (only aligned on `choices[].delta` after Ollama [PR #17485](https://github.com/ollama/ollama/pull/17485)); older versions still differ                                                                                                                                                             | `choices[].delta`                                                               |

Three iron rules when writing code (the code in this part already follows them):

- Decide "is a tool call wanted?" from **`tool_calls` being non-empty / `finish_reason == "tool_calls"`**, never from `content` alone;
- When checking for emptiness, accept both `content == ""` and `content == null`;
- With no tools, **omit the `tools` field**; never send `tools: []`;
- When you need to "force a particular tool", don't count on `tool_choice` (measured on 0.33.3: `required` forces a call, `none` has no effect, field support is unstable) — constrain it through the prompt instead, or use the native `/api/chat` (Section 1 of Part 4 compares the native `/api/chat` message shapes, but the service sticks to `/v1` throughout so it can move to the cloud).

Reference: [Ollama OpenAI compatibility official docs](https://docs.ollama.com/api/openai-compatibility)

---

## 2. Starting from plain text: the core logic in about 20 lines

First run one conversation by hand with curl — it is language-independent and the easiest way to confirm the service and the model are ready:

```bash
curl -s http://localhost:11434/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"llama3.1:8b","messages":[{"role":"user","content":"你好，用一句话介绍你自己。"}]}'
```

The JSON that comes back looks like this (formatted):

```json
{
  "id": "chatcmpl-9",
  "model": "llama3.1:8b",
  "choices": [
    {
      // ← Go: ChatResponse.Choices[]
      "message": {
        // ← Go: ChatResponse.Choices[].Message
        "role": "assistant",
        "content": "我是语言模型，能理解和生成汉语。" // ← the answer you asked for
      },
      "finish_reason": "stop" // ← the model is done (becomes "tool_calls" for tool calls, see §3)
    }
  ],
  "usage": {
    // ← token counts, the cost behind "resend the full history" in Part 3
    "prompt_tokens": 19,
    "completion_tokens": 13
  }
}
```

All you need is `choices[0].message.content` — that is the answer. The Go code later on defines its structs to match exactly this JSON shape.

Before "adding tools" to the code, think one question through: **when do you not need an agent at all?** For simple Q&A or chit-chat, `/api/chat` (without `tools`) is enough; for plain text completion, `/api/generate` is lighter (no chat template or tool-parsing overhead); deterministic tasks (table lookups, formatting) may not need a model at all. Only when a task needs **real-world side effects or data** (reading the clock, querying a database, running a command) and the execution path cannot be hard-coded in advance is the "model names a tool → code executes → result goes back" loop worth it — Part 3 explains that every round resends the full history, so the earlier you make this call, the more tokens you save.

And **plain-text chat is the simplest form of that lightweight path**: the same `messages` array posted to `/v1/chat/completions`, except that the request carries no `tools` field and the response carries only `content`, with no `tool_calls`.

Full code (about 60 lines, standard library only):

<details>
<summary>Full source of main.go (click to expand)</summary>

```go
// Part 2 (the plain-text starting version) demo: the core logic runs one
// conversation in about 20 lines
//
// Relation to the tool version: plain-text chat is a "subset" of tool
// calling — the same /v1/chat/completions endpoint and the same messages
// array, except that:
//  1. the request carries no tools field (following the "omit tools when
//     there are none" iron rule);
//  2. the response has only content, no tool_calls;
//  3. multi-turn chat = keep appending {role,content} to messages and
//     resending the whole array.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
}

type Choice struct {
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type ChatResponse struct {
	Choices []Choice `json:"choices"`
}

func main() {
	messages := []Message{
		{Role: "user", Content: "你好，请用一句话介绍你自己。"},
	}

	reqBody := ChatRequest{
		Model:       "llama3.1:8b",
		Messages:    messages,
		Temperature: 0, // greedy decoding for the demo: stable, reproducible output
	}
	jsonData, _ := json.Marshal(reqBody)

	resp, err := http.Post("http://localhost:11434/v1/chat/completions",
		"application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result ChatResponse
	if err := json.Unmarshal(body, &result); err != nil {
		fmt.Printf("解析失败，原始响应: %s\n", string(body))
		panic(err)
	}
	if len(result.Choices) == 0 {
		fmt.Printf("无响应，原始响应: %s\n", string(body))
		return
	}

	c := result.Choices[0]
	fmt.Println("🧑 用户:", messages[0].Content)
	fmt.Println("🤖", c.Message.Content)
	fmt.Println("   finish_reason:", c.FinishReason)
}
```

</details>

Three points:

- The request body holds only `model` + `messages`, no `tools` — this is the iron rule from Section 1, "omit the `tools` field when there are no tools", put into practice;
- The response is read from `choices[0].message.content` and `finish_reason` (`stop` here);
- **Multi-turn chat = append to `messages` and resend the whole array**, which is the shared foundation of every later example.

Running it:

```bash
go run main.go
```

Real output (Ollama 0.33.3 + llama3.1:8b, measured 2026-09):

```
🧑 用户: 你好，请用一句话介绍你自己。
🤖 你好！我是 LLaMA，一个由 Meta 开发的基于人工智能的语言模型，能够理解和生成人类语言。
   finish_reason: stop
```

> From this section to the next, only "three things" differ: add the `tools` field to the request, parse `tool_calls` out of the response, and send the execution result back with `role="tool"`. The tool version's structs and `sendRequest` are nearly identical to this section — so a tutorial could equally well start with tools, but seeing plain text first makes it easier to build intuition.

---

## 3. The Go implementation (adding tools to plain text)

This is the **deliberately minimal example**: one tool, a single round, about 200 lines (comments included), Go standard library only (`net/http`, `encoding/json`).

The design trade-offs are below; understand the principle first, then engineer it:

| Deliberately simplified                                            | What production does                                                          |
| ------------------------------------------------------------------ | ----------------------------------------------------------------------------- |
| Only one tool, only a single round                                 | Multi-tool registration + a while loop until the model stops requesting tools |
| HTTP errors go straight to `panic`                                 | Return an `error` and degrade or retry gracefully                             |
| `json.Marshal` and `io.ReadAll` errors are ignored                 | Handle each one and attach context                                            |
| The model name `llama3.1:8b` is hard-coded                         | Move it into configuration (flag / environment variable)                      |
| A fixed 30s timeout, no concurrency control                        | `http.Client` timeouts + a connection pool                                    |
| Reads only `message.content`/`tool_calls`, ignores `finish_reason` | Follows the compatibility list in Section 1 (`tool_choice`, empty `tools`, …) |

### Full code (main.go)

Look at the raw request and response JSON first — **the structs in the code are defined straight from these two JSON documents**:

Request (with the `tools` array):

```json
{
  "model": "llama3.1:8b",
  "messages": [{ "role": "user", "content": "现在几点了？" }],
  "tools": [
    {
      // ← Go: ChatRequest.Tools []Tool
      "type": "function",
      "function": {
        "name": "get_current_time",
        "description": "获取当前时间",
        "parameters": { "type": "object", "properties": {} }
      }
    }
  ]
}
```

Response (measured on Ollama 0.33.3):

```json
{
  "choices": [
    {
      "message": {
        // ← Go: ChatResponse.Choices[].Message
        "role": "assistant",
        "content": "", // ⚠️ the empty string, not null
        "tool_calls": [
          {
            // ← Go: Message.ToolCalls []ToolCall
            "id": "call_dlj5358x", // ⚠️ correlation ID: you must send this back with the result
            "function": {
              "name": "get_current_time",
              "arguments": "{}" // ⚠️ this is a string, not an object! parse it again with json.RawMessage
            }
          }
        ]
      },
      "finish_reason": "tool_calls" // ⚠️ not "stop" (the loop criterion in Part 3)
    }
  ],
  "usage": { "prompt_tokens": 146, "completion_tokens": 14 }
}
```

With those two JSON documents in view, the Go code that follows explains itself: why `ToolCall.Function.Arguments` needs a second parse through `json.RawMessage`, and what `ToolCallID` is for.

<details>
<summary>Full source of main.go (click to expand)</summary>

```go
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ===========================================
// Data structures (matching the Ollama API's JSON format)
// ===========================================

// Message is one message in the conversation
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall is a tool the AI asked to call
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// Tool is one of the tools we offer to the AI
type Tool struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

// Function is the definition of a tool function
type Function struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// ChatRequest is the request sent to Ollama
type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []Tool    `json:"tools"`
	Stream   bool      `json:"stream"`
}

// Choice is one generation choice (including its finish reason)
type Choice struct {
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// ChatResponse is the response Ollama returns
type ChatResponse struct {
	Choices []Choice `json:"choices"`
}

// ===========================================
// Tool functions (the logic that actually runs)
// ===========================================

// getCurrentTime is the tool function we implement
// It runs when the AI decides to call "get_current_time"
// args are the arguments the AI passes in (there are none in this example, so they are ignored)
func getCurrentTime(args json.RawMessage) string {
	return time.Now().Format("2006-01-02 15:04:05")
}

// toolMap is the tool registry: it finds the function by name
// When the AI returns tool_calls, this table resolves the function to run
var toolMap = map[string]func(json.RawMessage) string{
	"get_current_time": getCurrentTime,
}

// ===========================================
// Main program (demonstrates the complete tool-calling flow)
// ===========================================

func main() {
	// Step 1: prepare the user message
	// This is the question the user asks the AI
	messages := []Message{
		{Role: "user", Content: "现在几点了？告诉我当前的具体时间。"},
	}

	// Step 2: define the available tools
	// Tell the AI which tools exist, and what each one does and takes
	tools := []Tool{
		{
			Type: "function",
			Function: Function{
				Name:        "get_current_time",
				Description: "获取当前的日期和时间",
				Parameters: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
		},
	}

	// Step 3: send the request to Ollama
	// Send the user message and the tool list to the model together
	resp := sendRequest(messages, tools)
	if len(resp.Choices) == 0 {
		fmt.Println("无响应")
		return
	}

	// Take the AI's reply
	assistantMsg := resp.Choices[0].Message
	messages = append(messages, assistantMsg)

	// Step 4: check whether the AI wants to call a tool
	if len(assistantMsg.ToolCalls) > 0 {
		// The AI decided to call a tool
		toolCall := assistantMsg.ToolCalls[0]
		fmt.Println("🔧 模型决定调用工具:", toolCall.Function.Name)

		// Step 5: run the tool
		// Resolve the function through the tool registry and run it (a miss is
		// usually the model hallucinating a tool name — don't panic)
		fn, ok := toolMap[toolCall.Function.Name]
		if !ok {
			fmt.Println("⚠️ 未知工具，跳过:", toolCall.Function.Name)
			return
		}
		result := fn(json.RawMessage(toolCall.Function.Arguments))

		// Step 6: add the tool result to the conversation as a new message
		// Note: Role must be "tool", and ToolCallID must match the ID the AI asked with
		messages = append(messages, Message{
			Role:       "tool",
			ToolCallID: toolCall.ID,
			Content:    result,
		})

		fmt.Println("✅ 工具结果:", result)

		// Step 7: send the conversation, now containing the tool result, to the model again
		// The model produces its final answer from the tool result
		finalResp := sendRequest(messages, tools)
		if len(finalResp.Choices) > 0 {
			fmt.Println("💬 最终回答:", finalResp.Choices[0].Message.Content)
		}
	} else {
		// The AI did not call a tool, so print its answer directly
		fmt.Println("💬 回答:", assistantMsg.Content)
	}
}

// ===========================================
// HTTP request function (talking to Ollama)
// ===========================================

// sendRequest posts a request to the Ollama API
// Using the OpenAI-compatible format: /v1/chat/completions
func sendRequest(messages []Message, tools []Tool) ChatResponse {
	// Build the request body
	reqBody := ChatRequest{
		Model:    "llama3.1:8b", // the model to use
		Messages: messages,      // conversation history
		Tools:    tools,         // available tools
		Stream:   false,         // no streaming output
	}

	// Serialize to JSON
	jsonData, _ := json.Marshal(reqBody)

	// POST to Ollama (a Client with a timeout instead of bare http.Post, so that
	// a stuck upstream cannot hang us forever)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(
		"http://localhost:11434/v1/chat/completions",
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	// Read the response
	body, _ := io.ReadAll(resp.Body)

	// Parse the JSON response
	// Note: when Ollama reports an error the body is not the standard shape, so
	// parsing it directly yields an empty response; silently printing "无响应"
	// would make newcomers think the model is not installed, so print the raw
	// body on failure
	var result ChatResponse
	if err := json.Unmarshal(body, &result); err != nil {
		fmt.Printf("解析失败，原始响应: %s\n", string(body))
		panic(err)
	}
	return result
}
```

</details>

### Code walkthrough

The code has three parts, and understanding their responsibilities is enough:

#### 1. Data structures (the first half of the file)

`Message`, `ToolCall`, `Tool` and friends map one-to-one onto the Ollama API's JSON fields (through the `json:"..."` tags). The most confusing one is `Message`:

- a `role="assistant"` message carries `tool_calls` (which tool the AI wants to call)
- a `role="tool"` message carries `tool_call_id` (the tool's result, sent back to the AI)

#### 2. Tool definition and registry

```go
// Tool implementation: this runs when the model calls "get_current_time"
func getCurrentTime(args json.RawMessage) string {
	return time.Now().Format("2006-01-02 15:04:05")
}

// Registry: the model only sends a tool "name"; the program uses this table
// to find the matching Go function
var toolMap = map[string]func(json.RawMessage) string{
	"get_current_time": getCurrentTime,
}
```

#### 3. The main flow `main()` (matching the flow diagram below)

The comments inside `main()` walk through "step one" to "step seven", matching the flow below one-to-one:

```
1 User asks a question         → Go program prepares messages (role="user")
2 Go program defines the tools → POST /v1/chat/completions (messages + tools) to Ollama
3 Ollama responds              → assistant message + tool_calls (test: tool_calls is non-empty)
4 Go program runs it locally   → look the tool name up in toolMap and run the Go function
5 Go program sends it back     → wrapped as role="tool", ToolCallID matching the model's call ID
6 Ollama responds              → the final answer in content (generated from the real tool result)
7 Go program prints the answer → the user sees the result
```

> Key point: the `ToolCallID` in step six must match the `ID` in the model's request, so that the model can tie the result to that specific call. The heart of the whole mechanism is this — **the model does not execute tools, it only names them; execution always happens in your local code**.

### The HTTP request function sendRequest

The only place in the program that talks to Ollama:

- In the request body, `Model` sets the model name, `Messages` carries the full conversation history, and `Tools` carries the tool list
- Requests go to the OpenAI-compatible endpoint `POST http://localhost:11434/v1/chat/completions`
- The response is parsed and returned as a `ChatResponse`, and `main()` reads the AI's reply from `Choices[0].Message`

### First steps toward production: four small changes (the transition from 200 lines to engineering)

The table above only gives direction; here are the smallest versions of each change. The fuller evolution unfolds part by part in Parts 3 and 4.

**① A fixed 30s timeout → a configurable timeout budget**

The minimal version already sends requests with a 30s-timeout `http.Client` (see `sendRequest` in the full code above); this step turns the timeout into a budget you can tune per scenario:

```go
// Before (the minimal version: a fixed 30 seconds)
client := &http.Client{Timeout: 30 * time.Second}

// After (long contexts / slow cloud inference need headroom)
client := &http.Client{Timeout: 5 * time.Minute}
```

> What matters is not the number but giving the upstream request an **explicit timeout budget**: without one, a stuck Ollama hangs the request forever.

**② `panic` → returning an `error`**

```go
// Before
func sendRequest(...) ChatResponse { ...; panic(err) }

// After
func sendRequest(...) (ChatResponse, error) { ...; return ChatResponse{}, fmt.Errorf("请求失败: %w", err) }
```

The main flow then goes from "crashing" to "logging and degrading gracefully".

**③ A hard-coded model name → an environment variable**

```go
model := os.Getenv("OLLAMA_MODEL")
if model == "" {
    model = "llama3.1:8b"
}
```

**④ One round → a loop (the subject of Part 3)**

Wrap "receive `tool_calls` → execute → send back" in a `for` until the response no longer contains `tool_calls` — that is the subject of Part 3, which also fills in multi-tool registration, argument parsing and error feedback.

> Summary: those four points, plus the loop, multiple tools and error handling of Part 3, and the service-ization, timeouts and concurrency of Part 4, are the concrete path to the right-hand column of "deliberately simplified vs. production practice" — each part moves one small step forward, and there is no need to write "production-grade" code in one go.

---

## 4. Run results

```bash
go run main.go
```

Successful output:

```
🔧 模型决定调用工具: get_current_time
✅ 工具结果: 2026-09-07 19:58:44
💬 最终回答: 当前时间是 2026年09月07日 19:58:44
```

The agent really did execute my Go function and take the real time, instead of making an answer up.

### Comparison: the same program with qwen2.5-coder

The same program with `qwen2.5-coder:7b` (the official template already carries the tool format, and the model still does not comply) writes the tool call as ordinary text:

```
💬 回答: {"name": "get_current_time", "arguments": {}}
```

It "knows" it should call a tool but never writes it into the `tool_calls` field — a stable reproduction with qwen2.5-coder + Ollama 0.33.3 (for the cause and the self-check, see Appendix A of Part 1). When you see output like this, check the model and the template first, rather than doubting your own code.

---

## FAQ: quick answers (code)

| Problem                                                      | Cause                                                                                                                                                   | Fix                                                                                      |
| ------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| The code returns 404 / 400                                   | The Ollama version is too old and `/v1/chat/completions` is not enabled                                                                                 | Upgrade to >= 0.3.0 (see the install in Part 1)                                          |
| The model's answer contains `{"name": ...}`                  | The template lacks the tool format, or model compliance is insufficient (qwen2.5-coder reproduces this on 0.33.3 even with a correct official template) | Switch to `llama3.1:8b`; for the Qwen transcript and self-check see Appendix A of Part 1 |
| Behavior differs after switching to OpenAI / a cloud service | `/v1` is only approximately compatible, so field details differ                                                                                         | Self-test against the list in Section 1 (empty `tools`, empty content, `tool_choice`, …) |

---

## Conclusion

1. **Agents in Go are viable**: without touching Python, about 200 lines of standard-library code complete the full tool-calling loop;
2. **This is the minimal example**: it covers only the core principle (one tool, one round); multiple tools and multi-round loops are the subject of Part 3;
3. **/v1 is approximately compatible**: this part's decision rules (`tool_calls` non-empty + omitting empty `tools`) keep behavior identical on Ollama and OpenAI;
4. **Qwen's pitfalls depend on the model line**: qwen2.5-coder is a model-compliance problem (it still fails with a correct official template), not "Qwen is hopeless" — for the transcript see Appendix A of Part 1.

Next up: **"The Loop: Letting the Agent Decide How Many Tools to Call"** — multi-tool registration, argument parsing, feeding tool errors back, and history trimming.
