---
title: "The Loop: Letting the Agent Decide How Many Tools to Call (Part 3)"
description: "Upgrading a single round into a loop: multi-tool registration, several parallel tool_calls in one round, feeding tool errors back, history trimming — so that the agent itself decides how many tools to call."
date: 2026-09-08
series: go-agent
order: 3
tags:
  - Go
  - Agent
  - LLM
---

Part 2 stopped at "one tool, one round": a question can call a tool at most once before wrapping up. This part turns "one round" into a "loop" — the model can keep requesting tools round after round (multi-tool registration, parallel calls, error feedback, history trimming) until it produces its final answer.

- Prerequisite: you understand the mechanism from Part 2 (`role=tool` + `tool_call_id` alignment, the `tool_calls` non-empty test, the `/v1` compatibility iron rules)
- Requirements: same as the previous part (Go 1.27+; the repository's go.mod declares go 1.27.1)

> Environment note: this article is based on measurements of **Ollama 0.33.3 + llama3.1:8b (A770/Vulkan, 2026-09)**; Ollama iterates fast, so treat `ollama serve --help` as the source of truth for environment variables, and the [official OpenAI compatibility docs](https://docs.ollama.com/api/openai-compatibility) as the source of truth for the `/v1` compatibility fields.

## 1. From one round to many: what the agentic loop looks like

The code in Part 2 is "ask once → call a tool at most once → wrap up", which is essentially **a single round**. A real agent takes the shape of **a loop**:

```
for {
    send to the model (history + tool list)
    if the response has no tool_calls: print the answer, done
    otherwise: execute every tool_call, send the results back, continue to the next round
}
```

Compared with Part 2, this part adds four things:

| New capability                       | Why it is needed                                                                                         |
| ------------------------------------ | -------------------------------------------------------------------------------------------------------- |
| Registering several tools            | An agent usually has more than one tool, and has to declare parameters with JSON Schema                  |
| Several `tool_calls` in one response | The model may request several tools in parallel within one round (3 in one round in our measurements)    |
| Error feedback                       | A failed tool execution must be visible to the model, so it can fix the arguments or give up             |
| History trimming                     | Every round resends the whole history, and the context grows linearly with rounds, so a cap is mandatory |

## 2. The tool registry: description, parameter Schema and implementation in one place

Part 2's `toolMap` stored only "name → function", with the parameter Schemas scattered through `main`. This part folds all three into one table:

```go
var tools = map[string]*tool{
    "divide": {
        description: "计算两个整数 a 除以 b 的商（整除）",
        parameters: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "a": map[string]any{"type": "integer"},
                "b": map[string]any{"type": "integer"},
            },
            "required": []string{"a", "b"},
        },
        run: func(args json.RawMessage) (string, error) {
            // parse the arguments, run the logic, return a string result
        },
    },
}
```

- `description` is how the model "understands" a tool: spell out what the parameters mean and the model fills them in correctly;
- `parameters` follows JSON Schema (`type`/`properties`/`required`), and both Ollama and OpenAI deliver it in that format;
- **The `arguments` the model sends is a JSON string**, so you must `json.Unmarshal` it yourself before executing (as Part 2 explained); this part's `add`/`multiply`/`divide` all demonstrate reporting an error when parsing fails.

## 3. Error feedback: when a tool fails, return the error as an ordinary tool result

A failed tool execution should not break the whole loop; instead wrap the error **into the content of a `role="tool"` message** and send it back:

```go
result, err := runTool(tc)
if err != nil {
    result = fmt.Sprintf("工具执行出错：%v。请修正参数后重试，或放弃这一步。", err)
}
```

The model reads that "result" and decides for itself: fix the arguments and retry, switch tools, or explain to the user that it is giving up. In our measurements the model deliberately called `divide(10, 0)` first to trigger an error, and after receiving `除数不能为 0` ("the divisor cannot be 0") it **chose to skip that step** and finished the rest of the task — giving up is a legitimate strategy too, see the discussion in Section 7.

## 4. History growth: why a cap is mandatory, and how to keep it simple

The price of looping is that every round resends **every message from the first user turn up to the present** to the model (both OpenAI and Ollama are stateless endpoints; the client accumulates the history). After several rounds:

- prompt tokens grow linearly → every round is slower and more expensive (locally: slower);
- exceeding the model's context window either errors outright or truncates silently.

This part implements a very simple fuse: when a round's `prompt_tokens` crosses a threshold and the history is long enough, drop the oldest exchange (`trimHistory`). In production the more common approach is "once it gets too long, compress the old conversation into a summary and continue" — that is summary-style long-term memory, which this series does not cover (Part 5 covers the two kinds it does: "key-value fact memory + document retrieval (RAG)").

> Note: `trimHistory` cuts between two user messages — it removes the oldest exchange as a whole (that user message plus its assistant message and every `role=tool` result), avoiding the "orphan tool message with no matching assistant message" problem; this demo is a single question that keeps calling tools to the end, so it never triggers, and it only takes effect once you wire it into real multi-turn chat (where each turn appends a new user message).

## 5. Full code (demo/agent-loop/main.go)

<details>
<summary>Full source of demo/agent-loop/main.go (click to expand)</summary>

```go
// Part 3 demo: letting the agent decide how many tools to call (multi-round loop)
//
// Differences from the minimal example in Part 2:
//  1. several tools (time / addition / multiplication / division), with parameters declared as JSON Schema;
//  2. a while loop: keep going as long as the model requests tools, until it prints an ordinary answer;
//  3. one response may carry several tool_calls (parallel calls); execute them all and send them back together;
//  4. when a tool fails, feed the error message back as role="tool" content so the model can fix it or give up;
//  5. simple history trimming (to stop the context from growing without bound).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ===========================================
// Structures matching Ollama /v1/chat/completions
// ===========================================

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // the model sends a JSON string; parse it again before executing
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

type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []Tool    `json:"tools,omitempty"` // omitted when empty, following the compatibility iron rule from Part 2
	Stream   bool      `json:"stream"`
}

type Choice struct {
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type ChatResponse struct {
	Choices []Choice `json:"choices"`
	Usage   struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// ===========================================
// Tool registry: name -> description + parameter Schema + implementation
// ===========================================

// tool describes one tool the model can call.
// Run receives args as the arguments JSON string the model produced, which it has to parse itself.
type tool struct {
	description string
	parameters  map[string]any
	run         func(args json.RawMessage) (string, error)
}

// binaryOp factors out the pattern shared by the three arithmetic tools: parse the
// integer arguments {a,b} → run the operation → return the result.
// Each tool only has to pass in a one-line operation (e.g. func(a,b int)(int,error){ return a+b, nil }).
func binaryOp(name, desc string, fn func(int, int) (int, error)) *tool {
	return &tool{
		description: desc,
		parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"a": map[string]any{"type": "integer"},
				"b": map[string]any{"type": "integer"},
			},
			"required": []string{"a", "b"},
		},
		run: func(args json.RawMessage) (string, error) {
			var p struct{ A, B int }
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("参数解析失败（应为 {\"a\":整数,\"b\":整数}）：%v", err)
			}
			v, err := fn(p.A, p.B)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%d", v), nil
		},
	}
}

// tools is the global registry; adding a tool only means adding an entry here.
var tools = map[string]*tool{
	"get_current_time": {
		description: "获取当前的日期和时间",
		parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		run: func(args json.RawMessage) (string, error) {
			return time.Now().Format("2006-01-02 15:04:05"), nil
		},
	},
	"add": binaryOp("add", "计算两个整数 a 与 b 的和",
		func(a, b int) (int, error) { return a + b, nil }),
	"multiply": binaryOp("multiply", "计算两个整数 a 与 b 的乘积",
		func(a, b int) (int, error) { return a * b, nil }),
	"divide": binaryOp("divide", "计算两个整数 a 除以 b 的商（整除）",
		func(a, b int) (int, error) {
			if b == 0 {
				return 0, fmt.Errorf("除数不能为 0") // deliberately produce one "tool execution failure" to demo error feedback
			}
			return a / b, nil
		}),
}

// toolDefs converts the registry into the tools array used in requests.
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
// The agent loop
// ===========================================

const maxRounds = 8 // fuse: stops the model from getting stuck in a "call - error - call again" infinite loop

func main() {
	if len(os.Args) > 1 {
		runAgent(strings.Join(os.Args[1:], " "))
		return
	}
	// Default question: multiple rounds + deliberately triggering one divide-by-zero + fetching the time along the way
	runAgent("请先计算 10 除以 0，再计算 10 除以 2，然后把两个结果与当前时间一起告诉我。")
}

func runAgent(userPrompt string) {
	messages := []Message{{Role: "user", Content: userPrompt}}
	fmt.Println("🧑 用户:", userPrompt)
	fmt.Println(strings.Repeat("-", 56))

	for round := 1; round <= maxRounds; round++ {
		resp, err := chat(messages)
		if err != nil {
			fmt.Println("❌ 请求失败:", err)
			return
		}
		if len(resp.Choices) == 0 {
			fmt.Println("❌ 无响应")
			return
		}

		msg := resp.Choices[0].Message
		finish := resp.Choices[0].FinishReason
		messages = append(messages, msg)

		// Iron rule: decide from tool_calls being non-empty / finish_reason=="tool_calls"
		if len(msg.ToolCalls) == 0 || finish != "tool_calls" {
			fmt.Printf("🤖 第 %d 轮 最终回答: %s\n", round, msg.Content)
			fmt.Printf("📊 tokens: prompt=%d completion=%d total=%d\n",
				resp.Usage.PromptTokens, resp.Usage.CompletionTokens,
				resp.Usage.PromptTokens+resp.Usage.CompletionTokens)
			return
		}

		fmt.Printf("🔧 第 %d 轮 模型请求 %d 个工具:\n", round, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			result, err := runTool(tc)
			if err != nil {
				result = fmt.Sprintf("工具执行出错：%v。请修正参数后重试，或放弃这一步。", err)
			}
			fmt.Printf("   - %s(%s) -> %s\n", tc.Function.Name, tc.Function.Arguments, result)
			// The result must go back with role="tool" and the matching ID (the mechanism from Part 2)
			messages = append(messages, Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    result,
			})
		}
		messages = trimHistory(messages, resp.Usage.PromptTokens)
	}
	fmt.Println("⚠️ 达到最大轮数，可能陷入循环。")
}

func runTool(tc ToolCall) (string, error) {
	t, ok := tools[tc.Function.Name]
	if !ok {
		return "", fmt.Errorf("未知工具 %q（模型幻觉了工具名）", tc.Function.Name)
	}
	return t.run(json.RawMessage(tc.Function.Arguments))
}

// trimHistory: a minimal history trimmer — once a round's prompt has already grown
// long, drop the oldest complete exchange (that user message and the assistant and
// role=tool messages it produced), keeping the most recent context. Production
// usually triggers on a token threshold together with summarization; this only
// demonstrates the idea.
func trimHistory(messages []Message, lastPromptTokens int) []Message {
	if lastPromptTokens < 4000 || len(messages) <= 4 {
		return messages
	}
	// Find the boundary of the oldest exchange: start at the first user message and
	// end before the next one (in between are the assistant and role=tool messages
	// that user message produced, and the whole group has to go, otherwise an orphan
	// tool message with no matching assistant message is left behind and upstream
	// validation fails).
	start := 0
	for start < len(messages) && messages[start].Role != "user" {
		start++
	}
	end := len(messages)
	for i := start + 1; i < len(messages); i++ {
		if messages[i].Role == "user" {
			end = i
			break
		}
	}
	if start >= len(messages) || end == len(messages) {
		// No second user message found: only one exchange is in progress right now
		// (like this demo's single question with several tool rounds), so trim
		// nothing rather than delete context that is in use.
		return messages
	}
	fmt.Printf("   ✂️ 历史已超过 %d tokens，丢弃最早一轮对话（%d 条消息）\n", lastPromptTokens, end-start)
	return append([]Message{}, messages[end:]...)
}

// ===========================================
// HTTP (talking to Ollama)
// ===========================================

var httpClient = &http.Client{Timeout: 5 * time.Minute} // request timeout: a stuck upstream cannot hang us forever

func chat(messages []Message) (ChatResponse, error) {
	reqBody := ChatRequest{
		Model:    "llama3.1:8b",
		Messages: messages,
		Tools:    toolDefs(),
		Stream:   false,
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return ChatResponse{}, err
	}
	resp, err := httpClient.Post("http://localhost:11434/v1/chat/completions", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return ChatResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return ChatResponse{}, fmt.Errorf("upstream %d: %s", resp.StatusCode, string(b))
	}
	body, _ := io.ReadAll(resp.Body)
	var result ChatResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return ChatResponse{}, fmt.Errorf("解析失败，原始响应: %s", string(body))
	}
	return result, nil
}
```

</details>

## 6. Run results (measured on this machine)

```bash
go run ./demo/agent-loop
```

Real output (Ollama 0.33.3 + llama3.1:8b, measured 2026-09-08):

```
🧑 用户: 请先计算 10 除以 0，再计算 10 除以 2，然后把两个结果与当前时间一起告诉我。
--------------------------------------------------------
🔧 第 1 轮 模型请求 3 个工具:
   - divide({"a":10,"b":0}) -> 工具执行出错：除数不能为 0。请修正参数后重试，或放弃这一步。
   - divide({"a":10,"b":2}) -> 5
   - get_current_time({}) -> 2026-09-08 16:16:50
🤖 第 2 轮 最终回答: 当前时间与计算结果为：
5
2026-09-08 16:16:50
📊 tokens: prompt=197 completion=22 total=219
```

Three things worth noting:

1. **Three tools were called in parallel in one round**: `divide(10,0)`, `divide(10,2)` and `get_current_time` all came back in the same assistant message, and the loop executed every one of them and sent each back as its own `role=tool` message;
2. **The error was fed back correctly**: the `divide(10,0)` failure entered the conversation history, and the model clearly noticed and handled it in the next round;
3. **The token counts make the cost of looping visible**: `prompt=197` in the final round is the total length of the resent history — the more rounds and the longer the history, the bigger that number grows, which confirms why Section 4's trimming is necessary.

## 7. Pitfalls and comparisons (verified by measurement)

| Symptom                                             | Cause                                         | Handling                                                                                                                             |
| --------------------------------------------------- | --------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| The model returns several `tool_calls` in one round | Parallel tool calls are normal behavior       | Execute them all and send each back; don't handle only the first (Part 2's "take `[0]` only" loses results here)                     |
| The model gets an argument type wrong or omits one  | Small local models follow Schemas only so far | Write clearer descriptions; feed parse errors back and the model usually corrects itself                                             |
| The model makes up a tool name                      | Hallucination                                 | `runTool` returns "unknown tool" for an unknown name; never panic                                                                    |
| After the error is fed back, the model gives up     | Giving up is a legitimate strategy            | When the business requires success, stress it in the prompt or force retries in code — don't assume the model will insist on its own |
| The longer the context, the slower it gets          | Every round resends the full history          | Threshold trimming (this part); summarization is production practice and out of scope here                                           |
| Risk of an infinite loop                            | The model keeps requesting the same tool      | The `maxRounds` fuse plus error wording that steers it toward converging                                                             |

## 8. Deliberately simplified vs. production practice

| Deliberately simplified                         | What production does                                                                                            |
| ----------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| History trimming drops only the oldest exchange | A token-aware sliding window plus summarization                                                                 |
| Tool execution is synchronous and serial        | An async execution pool with timeouts and concurrency control                                                   |
| A hard `maxRounds` cap                          | Finer retry policies (counts / backoff / give-up conditions)                                                    |
| Errors are fed back as a single sentence        | Structured error codes plus context the model can read                                                          |
| Stateless, single session                       | Session isolation is covered in Part 5; production-grade multi-session management is outside this series' scope |

## FAQ

| Question                                         | Fix                                                                                                                |
| ------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------ |
| The loop is stuck in "call - error - call again" | Check whether the error wording gives the model a way to correct itself; confirm `maxRounds` is in effect          |
| Results don't line up with the tool calls        | Check whether `tool_call_id` is aligned one by one (the mechanism from Part 2)                                     |
| Only the first of several tools was executed     | Loop over every entry in `ToolCalls`; don't take only `[0]`                                                        |
| Behavior got worse after switching models        | Small local models vary in compliance; try `llama3.1:8b` first, and see Appendix A of Part 1 for the Qwen pitfalls |

## Conclusion

1. **An agent is a loop**: `while the model still wants tools` — turning "one call" into "autonomous multi-round" took only a dozen extra lines;
2. **Error feedback gives an agent resilience**: the error enters the history as a tool result, and whether the model self-corrects or gives up is its decision;
3. **Context is the first cost of looping**: put the threshold fuse in place first, then build memory (Part 5);
4. This part is still a one-shot command-line run — **turning it into a long-running HTTP service with streaming output** is the subject of Part 4.

Next up: **"Service & Migration: Turning the Agent into a Streaming API You Can Move to the Cloud"**.
