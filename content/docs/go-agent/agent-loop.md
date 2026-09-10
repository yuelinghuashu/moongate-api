---
title: 循环：让 Agent 自己决定调几次工具
description: 把单轮调用升级为循环：多工具注册、一轮多个并行 tool_calls、工具报错回喂、历史裁剪——让 Agent 自己决定调几次工具。
date: 2026-09-08
series: go-agent
order: 3
tags:
  - Go
  - Agent
  - LLM
---

第 2 篇停在“单工具、单轮”：一次问答最多调一次工具就收尾。本篇把“一轮”改成“循环”——模型可以连续请求多轮工具（多工具注册、并行调用、错误回喂、历史裁剪），直到它给出最终回答。

- 前置：已理解第 2 篇机制（`role=tool` + `tool_call_id` 对齐、`tool_calls` 非空判定、`/v1` 兼容铁律）

## 1. 从一轮到多轮：Agentic 循环长什么样

第 2 篇的代码是"问一次 → 最多调一次工具 → 收尾"，本质是**单轮**。真实 Agent 的形态是**循环**：

```
for {
    发给模型（历史 + 工具清单）
    若响应里没有 tool_calls：输出回答，结束
    否则：执行所有 tool_calls，结果回传，继续下一轮
}
```

和第 2 篇相比，本篇新增四件事：

| 新增能力                  | 为什么需要                                               |
| ------------------------- | -------------------------------------------------------- |
| 多个工具注册              | 一个 Agent 通常有多个工具，且要按 JSON Schema 声明参数   |
| 一次响应多个 `tool_calls` | 模型可能一轮里并行请求多个工具（本次实测一轮 3 个）      |
| 错误回喂                  | 工具执行失败要让模型知道，由它修正参数或放弃             |
| 历史裁剪                  | 每轮都要把整段历史重发，上下文随轮次线性增长，必须设上限 |

## 2. 工具注册表：描述、参数 Schema 与实现放在一起

第 2 篇的 `toolMap` 只存了"名字 → 函数"，参数 Schema 散在 main 里。本篇把三者收进一张表：

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
            // 解析参数、执行业务、返回字符串结果
        },
    },
}
```

- `description` 是模型"读懂"工具的关键：写清参数含义，模型才填得对；
- `parameters` 走 JSON Schema（`type`/`properties`/`required`），Ollama 与 OpenAI 都按这个格式下发；
- **模型传来的 `arguments` 是 JSON 字符串**，执行前必须自己 `json.Unmarshal`（第 2 篇讲过），本篇的 `add`/`multiply`/`divide` 都演示了解析失败时报错。

## 3. 错误回喂：工具报错时，把错误当普通工具结果返回

工具执行失败不要直接中断整个循环，而是把错误**包装成 `role="tool"` 消息内容**回传：

```go
result, err := runTool(tc)
if err != nil {
    result = fmt.Sprintf("工具执行出错：%v。请修正参数后重试，或放弃这一步。", err)
}
```

模型会读到这条"结果"并自行决策：修正参数重试、换工具、或向用户说明放弃。本文实测里，模型先故意调 `divide(10, 0)` 触发错误，收到"除数不能为 0"后**选择跳过这一步**并继续完成其余任务——放弃也是一种合法策略，见第 7 节讨论。

另：助手返回的 `assistant` 消息（含 `tool_calls`）必须**先 append 进 `messages`**，再 append `role="tool"` 的结果；顺序反了，工具结果就找不到对应的调用，上游会直接校验失败——这也是第 4 节 `trimHistory` 必须整组删除的原因。

## 4. 历史增长：为什么必须有上限，怎么简单处理

循环的代价是：每轮都要把**从第一条 user 到现在的全部消息**重发给模型（OpenAI/Ollama 都是无状态接口，历史靠客户端累积）。多轮之后：

- prompt tokens 线性增长 → 每轮更慢、更贵（本地是更慢）；
- 超过模型上下文窗口会直接报错或静默截断。

本篇实现了一个极简保险丝：当某轮 `prompt_tokens` 超过阈值且历史足够长时，丢掉最早的一轮对话（`trimHistory`）。生产上更常见的做法是"超长则把旧对话压成摘要再继续"——那属于摘要式长期记忆，本系列不展开（第 5 篇讲的是"键值事实记忆 + 资料检索（RAG）"两类）。

> 注意：`trimHistory` 的裁剪点落在两轮 user 消息之间——整组删除最老那轮（user + 其 assistant + 全部 `role=tool` 结果），避免删出「无 assistant 对应的孤儿 tool 消息」；本示例是单个问题一路调工具跑到底，不会触发它，把它接入真实多轮对话（每轮追加新的 user 消息）后才会生效。

## 5. 完整代码

<details>
<summary>main.go 全文（点击展开）</summary>

```go
// 第 3 篇演示：让 Agent 自己决定调几次工具（多轮循环版）
//
// 与第 2 篇最小案例的差异：
//  1. 多个工具（时间 / 加法 / 乘法 / 除法），按 JSON Schema 声明参数；
//  2. while 循环：只要模型还在请求工具就继续，直到它输出普通回答；
//  3. 一次响应可能带多个 tool_calls（并行调用），全部执行后一起回传；
//  4. 工具执行失败时把错误信息作为 role="tool" 内容回喂，让模型自行修正或放弃；
//  5. 简单的历史裁剪（防止上下文无限增长）。
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
// 与 Ollama /v1/chat/completions 对应的结构
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
		Arguments string `json:"arguments"` // 模型给的是 JSON 字符串，执行前要再解析
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
	Tools    []Tool    `json:"tools,omitempty"` // 空则不传，遵守第 2 篇的兼容铁律
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
// 工具注册表：名字 -> 描述 + 参数 Schema + 实现
// ===========================================

// tool 描述一个可被模型调用的工具。
// Run 收到的 args 是模型输出的 arguments JSON 字符串，需要自己解析。
type tool struct {
	description string
	parameters  map[string]any
	run         func(args json.RawMessage) (string, error)
}

// binaryOp 提取了三个算术工具的公共模式：解析 {a,b} 整数参数 → 执行运算 → 返回结果。
// 每个工具只需传入一行运算函数（如 func(a,b int)(int,error){ return a+b, nil }）。
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

// tools 是全局注册表，新增工具只需在这里加一项。
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
				return 0, fmt.Errorf("除数不能为 0") // 刻意制造一次"工具执行失败"，演示错误回喂
			}
			return a / b, nil
		}),
}

// toolDefs 把注册表转成请求里的 tools 数组。
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
// Agent 循环
// ===========================================

const maxRounds = 8 // 保险丝：防止模型陷入"调用-报错-再调用"的死循环

func main() {
	if len(os.Args) > 1 {
		runAgent(strings.Join(os.Args[1:], " "))
		return
	}
	// 默认问题：多轮 + 刻意触发一次除零错误 + 顺带取时间
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

		// 铁律：以 tool_calls 非空 / finish_reason=="tool_calls" 为准
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
			// 结果必须以 role="tool" + 对应 ID 回传（第 2 篇的机制）
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

// trimHistory：极简历史裁剪——当某轮 prompt 已经很长时，丢掉最早的
// 一轮完整对话（该轮 user 及其后的 assistant 与 role=tool 消息），
// 保留最近的上下文。生产中一般按 token 阈值触发并配合摘要压缩，
// 这里只演示思路。
func trimHistory(messages []Message, lastPromptTokens int) []Message {
	if lastPromptTokens < 4000 || len(messages) <= 4 {
		return messages
	}
	// 定位最早一轮的边界：从第一条 user 开始，到下一个 user 之前结束
	// （期间是这条 user 引发的 assistant 与 role=tool 消息，必须整组删除，
	// 否则会留下无 assistant 对应的孤儿 tool 消息，上游会校验失败）。
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
		// 找不到第二条 user：当前只有一轮对话在进行中（如本 demo 的
		// 单个问题多轮调工具），此时不裁剪，避免删掉正在使用的上下文。
		return messages
	}
	fmt.Printf("   ✂️ 历史已超过 %d tokens，丢弃最早一轮对话（%d 条消息）\n", lastPromptTokens, end-start)
	return append([]Message{}, messages[end:]...)
}

// ===========================================
// HTTP（与 Ollama 通信）
// ===========================================

var httpClient = &http.Client{Timeout: 5 * time.Minute} // 请求超时：上游卡住时不至于无限挂起

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

## 6. 运行结果（本机实测）

```bash
go run main.go
```

真实输出（Ollama 0.33.3 + llama3.1:8b，2026-09-08 实测）：

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

值得注意的三点：

1. **一轮并行调了 3 个工具**：`divide(10,0)` 与 `divide(10,2)` 和 `get_current_time` 在同一个 assistant 消息里返回，循环把它们全部执行并各自回传 `role=tool`；
2. **错误被正确回喂**：`divide(10,0)` 的报错进入了对话历史，模型在下一轮明确感知并处理；
3. **tokens 统计可见循环成本**：最后一轮 `prompt=197` 是把全部历史重发的总长——轮次越多、历史越长，这个数越大，印证第 4 节的裁剪必要性。

## 7. 坑与对照（实测验证）

| 现象                          | 原因                           | 处理                                                                        |
| ----------------------------- | ------------------------------ | --------------------------------------------------------------------------- |
| 模型一轮返回多个 `tool_calls` | 并行工具调用是正常行为         | 全部执行、逐条回传，不要只处理第一个（第 2 篇的"只取 `[0]`"在这里会丢结果） |
| 模型把参数填错类型/漏字段     | 本地小模型的 Schema 遵循度有限 | 描述写清楚；解析失败时把错误回喂，模型通常能自纠                            |
| 模型乱编工具名                | 幻觉                           | `runTool` 对未知名字返回"未知工具"，不要 panic                              |
| 报错回喂后模型选择放弃        | 放弃也是合法策略               | 业务上"必须成功"时，靠提示词强调或代码层强制重试，不要假设模型会自动坚持    |
| 上下文越长越慢                | 每轮全量重发历史               | 阈值裁剪（本篇）；摘要压缩属生产做法，不在本系列范围                        |
| 死循环风险                    | 模型反复请求同一工具           | `maxRounds` 保险丝 + 报错文案引导其收敛                                     |

## 8. 刻意简化 vs 生产做法

| 刻意简化的地方       | 生产环境的做法                                    |
| -------------------- | ------------------------------------------------- |
| 历史裁剪只删最早一轮 | token 感知的滑动窗口 + 摘要压缩                   |
| 工具执行同步、串行   | 异步执行池、超时与并发控制                        |
| `maxRounds` 硬上限   | 更细的重试策略（次数/退避/放弃条件）              |
| 错误只回喂一句话     | 结构化错误码 + 让模型可读的上下文                 |
| 无状态、单会话       | 会话隔离见第 5 篇；生产级多会话管理不在本系列范围 |

## FAQ

| 问题                   | 解决                                                                   |
| ---------------------- | ---------------------------------------------------------------------- |
| 结果对不上工具调用     | 检查 `tool_call_id` 是否逐条对齐（第 2 篇机制）                        |
| 多个工具只执行了第一个 | 循环里遍历全部 `ToolCalls`，不要只取 `[0]`                             |
| 换模型后行为变差       | 本地小模型遵循度参差，先试 `llama3.1:8b`；Qwen 系列的坑见第 1 篇附录 A |

## 结论

1. **Agent = 循环**：`while 模型还想要工具`，把"一次调用"变成"自主多轮"只多了十几行；
2. **错误回喂让 Agent 有韧性**：报错作为工具结果进入历史，模型自纠或放弃都由它决定；
3. **上下文是循环的第一成本**：先有阈值保险丝，再做记忆（第 5 篇）；
4. 本篇仍是命令行一次性运行，**把它变成常驻 HTTP 服务、支持流式输出**是第 4 篇的主题。

下一篇预告：**《服务与迁移：把 Agent 变成流式 API，可切云端》**。
