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

- Prerequisite: the environment checklist at the end of Part 1 is all green (service running, model pulled), and you understand the mechanism from Part 2 (`role=tool` + `tool_call_id` alignment, the `tool_calls` non-empty test, the `/v1` compatibility iron rules)

**What changed since Part 2 (read this table before the code):**

| Change                                                                   | Where                             | Why                                                                                                    |
| ------------------------------------------------------------------------ | --------------------------------- | ------------------------------------------------------------------------------------------------------ |
| Tools go from `map[string]func` to `toolEntry{description, schema, run}` | `binaryOp` / `tools` / `toolDefs` | Several tools, with the JSON Schema and the implementation side by side (§2)                           |
| `ChatResponse` gains `usage`; `ChatRequest` gains `temperature`          | structs                           | Read per-round prompt tokens (the trigger for trimming in §4); pinned sampling keeps runs reproducible |
| One round → a `for` loop with a `maxRounds` fuse                         | `runAgent`                        | Let the model decide how many tools to call (§1)                                                       |
| Iterate over **all** `ToolCalls` (no longer `[0]`)                       | `runAgent`                        | One round may request several tools in parallel (§7)                                                   |
| Tool errors are wrapped as `role="tool"` content and fed back            | `runAgent`                        | Let the model correct itself or give up instead of breaking the whole loop (§3)                        |
| New `trimHistory`                                                        | §4                                | The whole history is resent every round, so a cap is mandatory                                         |
| `panic` → returning `error`; status-code check and a 5-minute timeout    | `chat()`                          | Two of Part 2's "five small production changes" land here                                              |
| Accept the question from the command line                                | `main()`                          | Makes it easy to re-test with your own prompt                                                          |

> Unchanged from Part 2 and worth skimming past: the `Message`/`ToolCall`/`Tool`/`FunctionDef`/`FunctionCall` structs, the `tool_call_id` alignment mechanism, and the "`tool_calls` non-empty" criterion.

## 1. From one round to many: what the agentic loop looks like

The code in Part 2 is "ask once → call a tool at most once → wrap up", which is essentially **a single round**. A real agent takes the shape of **a loop**:

```
for {
    send to the model (history + tool list)
    if the response has no tool_calls: print the answer, done
    otherwise: execute every tool_call, send the results back, continue to the next round
}
```

How `messages` grows inside the loop (the whole thing is resent every round):

```
Round 1   messages = [user]
          ├─ POST /v1/chat/completions (messages + tools)
          └─ ← assistant: tool_calls ×3        ← criterion: tool_calls non-empty
Round 1 done  execute each call → append role="tool" (ids aligned one by one)
          messages = [user, assistant(3×tool_calls), tool, tool, tool]
Round 2   ├─ POST (still a full resend; prompt tokens grow with it)
          └─ ← assistant: content (no tool_calls) → print the answer, stop
```

The loop exits for exactly two reasons: the model stops asking for tools (normal), or the round count hits `maxRounds` (the fuse).

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
var tools = map[string]*toolEntry{
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

> All three of these are ultimately rendered into prompt text by the **model's chat template** (the `description` and the parameter Schema included) — for the raw template and the text the model actually reads, see **Appendix B of Part 1**; it is also the clearest explanation of why a `description` has to be written carefully.
>
> Two concepts worth pinning down: **JSON Schema** is the JSON convention that describes "what the parameters look like" (`type` declares the type, `properties` lists the fields, `required` marks the mandatory ones). You write it by hand and it is **read by the model only** — nothing validates against it at runtime, so parameter validation is yours to do in Go (this part's `binaryOp` returns an error when parsing fails). And `any` in `map[string]any` is an alias for `interface{}` (the Go 1.18+ spelling; the two are identical).

## 3. Error feedback: when a tool fails, return the error as an ordinary tool result

A failed tool execution should not break the whole loop; instead wrap the error **into the content of a `role="tool"` message** and send it back:

```go
result, err := runTool(tc)
if err != nil {
    result = fmt.Sprintf("工具执行出错：%v。请修正参数后重试，或放弃这一步。", err)
}
```

The model reads that "result" and decides for itself: fix the arguments and retry, switch tools, or explain to the user that it is giving up. In our measurements, after `divide(10, 0)` raised an error the model **honestly reported that "dividing 10 by 0 is invalid"** in the next round and carried on with the remaining results — it neither invented a number nor aborted the loop, see the discussion in Section 7.

One more thing: the `assistant` message the model returns (the one carrying `tool_calls`) must be **appended to `messages` first**, and only then the `role="tool"` results; reverse the order and a tool result has no matching call, which upstream rejects outright — the same reason Section 4's `trimHistory` has to drop an exchange as a whole.

## 4. History growth: why a cap is mandatory, and how to keep it simple

The price of looping is that every round resends **every message from the first user turn up to the present** to the model (both OpenAI and Ollama are stateless endpoints; the client accumulates the history). After several rounds:

- prompt tokens grow linearly → every round is slower and more expensive (locally: slower);
- exceeding the model's context window either errors outright or truncates silently.

This part implements a very simple fuse: when a round's `prompt_tokens` crosses a threshold and the history is long enough, drop the oldest exchange (`trimHistory`). In production the more common approach is "once it gets too long, compress the old conversation into a summary and continue" — that is summary-style long-term memory, which this series does not cover (Part 5 covers the two kinds it does: "key-value fact memory + document retrieval (RAG)").

**First, see where the budget goes** (unit conversion in Part 1 §4: Chinese ≈ 0.78 token/char):

- **The tool definitions themselves cost tokens**: for the same question, `prompt_tokens` is **149** with one tool definition and **15** without — about **+134 tokens per tool**, and they are **resent every round**. More tools means a higher starting cost for the loop.
- **What a tool returns costs tokens too**: a tool that hands back several thousand characters of file content or query results can eat most of the window in one call. So the tool implementation has to **truncate or summarize** (say, the first N lines plus a total count) instead of pasting the raw result back into the conversation.
- The default window of 4096 tokens is about **5,000 Chinese characters** of history — factor in the two items above and it is clear why Section 4's trimming is not optional.

**Where that 4000 comes from**: it is not a guess — it hugs Ollama's default context window, which on this machine is **4096** (the CONTEXT column of `ollama ps`, measured; `OLLAMA_CONTEXT_LENGTH`, mentioned in Part 1 §4, defaults to 4k/32k/256k depending on VRAM). So:

- why 4000 rather than 4096: the 96 left over is deliberate lead time for this round's output plus template/system-prompt overhead — trimming is braking early, not running right up against the ceiling;
- a threshold only means something relative to the window in use: raise the window to 32k and this 4000 has to grow with it, otherwise you throw away context you could still afford;
- the more robust production choice is a percentage of the window (say 70%) rather than a hard-coded number;
- do not lean on trimming alone either — Ollama itself truncates the oldest tokens when the history overflows; the point of trimming is that **you** decide what to drop (and can summarize it first).

> Note: `trimHistory` cuts between two user messages — it removes the oldest exchange as a whole (that user message plus its assistant message and every `role=tool` result), avoiding the "orphan tool message with no matching assistant message" problem; this example is a single question that keeps calling tools to the end, so it never triggers, and it only takes effect once you wire it into real multi-turn chat (where each turn appends a new user message).

## 5. Full code

<details>
<summary>Full source of main.go (click to expand)</summary>

```go
// Part 3 demo: letting the agent decide how many tools to call (multi-round loop).
// Differences from Part 2 are listed in the "what changed" table at the top of the article;
// the new material here is the toolEntry/binaryOp registry, the runAgent loop, error feedback
// and trimHistory.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
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
	ID       string       `json:"id"`
	Function FunctionCall `json:"function"`
}

type Tool struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

type FunctionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// FunctionCall is the shape of the function inside one tool call (tool_calls[].function)
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // the model sends a JSON string; parse it again before executing
}

type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []Tool    `json:"tools,omitempty"` // omitted when empty, following the compatibility iron rule from Part 2
	Stream      bool      `json:"stream"`
	Temperature float64   `json:"temperature"`
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

// toolEntry describes one tool the model can call.
// Run receives args as the arguments JSON string the model produced, which it has to parse itself.
type toolEntry struct {
	description string
	parameters  map[string]any
	run         func(args json.RawMessage) (string, error)
}

// binaryOp factors out the pattern shared by the three arithmetic tools: parse the
// integer arguments {a,b} → run the operation → return the result.
// Each tool only has to pass in a one-line operation (e.g. func(a,b int)(int,error){ return a+b, nil }).
func binaryOp(desc string, fn func(int, int) (int, error)) *toolEntry {
	return &toolEntry{
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
var tools = map[string]*toolEntry{
	"get_current_time": {
		description: "获取当前的日期和时间",
		parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		run: func(args json.RawMessage) (string, error) {
			return time.Now().Format("2006-01-02 15:04:05"), nil
		},
	},
	"add": binaryOp("计算两个整数 a 与 b 的和",
		func(a, b int) (int, error) { return a + b, nil }),
	"multiply": binaryOp("计算两个整数 a 与 b 的乘积",
		func(a, b int) (int, error) { return a * b, nil }),
	"divide": binaryOp("计算两个整数 a 除以 b 的商（整除）",
		func(a, b int) (int, error) {
			if b == 0 {
				return 0, fmt.Errorf("除数不能为 0") // deliberately produce one "tool execution failure" to demo error feedback
			}
			return a / b, nil
		}),
}

// toolDefs turns the registry into the request's tools array.
// Sorted by name: Go iterates maps in random order, and a stable request body is
// easier to compare when inspecting captures and friendlier to upstream prompt caching.
func toolDefs() []Tool {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]Tool, 0, len(names))
	for _, name := range names {
		t := tools[name]
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

		// Iron rule: go by tool_calls being non-empty. finish_reason is only a
		// cross-check — some upstreams still report "stop" (or omit it) while
		// requesting tools, and trusting it alone silently drops tool calls and
		// prints an empty answer.
		if len(msg.ToolCalls) == 0 {
			fmt.Printf("🤖 第 %d 轮 最终回答: %s\n", round, msg.Content)
			fmt.Printf("📊 tokens: prompt=%d completion=%d total=%d\n",
				resp.Usage.PromptTokens, resp.Usage.CompletionTokens,
				resp.Usage.PromptTokens+resp.Usage.CompletionTokens)
			return
		}
		if finish != "tool_calls" {
			fmt.Printf("   ⚠️ finish_reason=%q 但本轮有 tool_calls，按 tool_calls 继续执行\n", finish)
		}

		fmt.Printf("🔧 第 %d 轮 模型请求 %d 个工具:\n", round, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			result, err := runTool(tc)
			if err != nil {
				result = fmt.Sprintf("工具执行出错：%v。请修正参数后重试，或放弃这一步。", err)
			}
			if result == "" {
				result = "(工具无输出)" // a role="tool" message must carry content, else upstream 400 (Part 2 §3)
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

// trimThreshold is the trimming threshold in prompt tokens. The value 4000 is not
// arbitrary: Ollama's default context window is 4k (the CONTEXT column of `ollama ps`
// reads 4096; raise it with OLLAMA_CONTEXT_LENGTH). The 96 left over is not a
// throwaway margin — it covers this round's output plus template/system-prompt
// overhead. A threshold only means something when it hugs the window actually in
// use, so scale it up when you enlarge the window.
const trimThreshold = 4000

// trimHistory: a minimal history trimmer — once a round's prompt has already grown
// long, drop the oldest complete exchange (that user message and the assistant and
// role=tool messages it produced), keeping the most recent context. Production
// usually triggers on a token threshold together with summarization; this only
// demonstrates the idea.
func trimHistory(messages []Message, lastPromptTokens int) []Message {
	if lastPromptTokens < trimThreshold || len(messages) <= 4 {
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
		Model:       "llama3.1:8b",
		Messages:    messages,
		Tools:       toolDefs(),
		Stream:      false,
		Temperature: 0, // greedy decoding: keeps the recorded output reproducible; use 0.7 for chat
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
go run main.go
```

Real output (Ollama 0.33.3 + llama3.1:8b, measured 2026-09-19; the request pins `temperature=0`):

> The console output and prompts in this transcript are Chinese exactly as captured; substituting English prompts changes the wording, not the structure or the conclusions.

```
🧑 用户: 请先计算 10 除以 0，再计算 10 除以 2，然后把两个结果与当前时间一起告诉我。
--------------------------------------------------------
🔧 第 1 轮 模型请求 3 个工具:
   - get_current_time({}) -> 2026-09-19 14:57:57
   - divide({"a":10,"b":0}) -> 工具执行出错：除数不能为 0。请修正参数后重试，或放弃这一步。
   - divide({"a":10,"b":2}) -> 5
🤖 第 2 轮 最终回答: 由于 10 除以 0 是无效的计算，结果为 "除数不能为 0"。因此，两个结果是：

*   10 除以 0：除数不能为 0
*   10 除以 2：5
*   当前时间：2026-09-19 14:57:57
📊 tokens: prompt=197 completion=79 total=276
```

Four things worth noting:

1. **Three tools were called in parallel in one round**: `divide(10,0)`, `divide(10,2)` and `get_current_time` all came back in the same assistant message, and the loop executed every one of them and sent each back as its own `role=tool` message (they run in the order the model listed them, which is not guaranteed to be the same every time);
2. **The error was fed back correctly**: the `divide(10,0)` failure entered the conversation history, and in the next round the model explicitly cited it and explained that "dividing 10 by 0 is invalid" instead of inventing a number;
3. **The token counts make the cost of looping visible**: `prompt=197` in the final round is the total length of the resent history — the more rounds and the longer the history, the bigger that number grows, which confirms why Section 4's trimming is necessary;
4. **`temperature=0` does not mean "always byte-identical"**: measured here, the same request run three times produced identical output (greedy decoding _is_ deterministic for one fixed prompt); but the prompt in this loop **contains dynamic content** — the timestamp the tool returns differs every round — so two runs will not word the answer the same way. What stays stable is the **structure and the conclusion**; for a byte-for-byte reproduction you have to freeze the tool's return value too.

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

| Question                                  | Fix                                                                                                                |
| ----------------------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| Results don't line up with the tool calls | Check whether `tool_call_id` is aligned one by one (the mechanism from Part 2)                                     |
| Behavior got worse after switching models | Small local models vary in compliance; try `llama3.1:8b` first, and see Appendix A of Part 1 for the Qwen pitfalls |

## Conclusion

1. **An agent is a loop**: `while the model still wants tools` — turning "one call" into "autonomous multi-round" took only a dozen extra lines;
2. **Error feedback gives an agent resilience**: the error enters the history as a tool result, and whether the model self-corrects or gives up is its decision;
3. **Context is the first cost of looping**: put the threshold fuse in place first, then build memory (Part 5);
4. This part is still a one-shot command-line run — **turning it into a long-running HTTP service with streaming output** is the subject of Part 4.

Next up: **"Service & Migration: Turning the Agent into a Streaming API You Can Move to the Cloud"**.
