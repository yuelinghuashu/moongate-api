---
title: 框架：循环骨架可以交出去，终止条件不能
description: 用 Eino ADK 重写第 3 篇的 Agent 循环：循环骨架、工具调度、消息顺序都交给框架，但本机实测发现两处语义和手写版不同——迭代上限是硬错误（默认 20，且 0 不等于无限），工具报错不会被自动回喂而是终止整个 run。附 A770 实测数据与「交出去／自己管」边界表。
date: 2026-09-11
series: go-agent
order: 6
tags:
  - Go
  - Agent
  - LLM
---

第 3 篇我们用约 200 行标准库代码手写了 Agent 循环：模型点名 → Go 执行 → 结果回传 → 模型作答，直到它不再要工具。骨架跑通了，`maxRounds` 保险丝也装上了。

现在到了该问一句的地方：**这套循环骨架，有多少可以交出去？**

本篇用 Eino（字节跳动的 Go LLM 框架）的 ADK 层重写同一个 Agent，逐项对照第 3 篇的每个决策。结论不是"框架更好"或"框架不如手写"，而是**哪些交出去是安全的、哪些交出去会出事**——因为本机实测发现了两个手写版不会遇到的语义变化。

- 前置：已理解第 2 篇机制（`role=tool` + `tool_call_id` 对齐、`tool_calls` 非空判定）与第 3 篇的循环、错误回喂、历史裁剪
- 环境：沿用第 1 篇的 Ollama + `llama3.1:8b`（Intel Arc A770）
- 代码：完整可运行版本在本篇 §6，保存为 `main.go` 后执行 `go run main.go`

> **版本声明**：本文基于 `github.com/cloudwego/eino v0.9.19` + `eino-ext/components/model/ollama v0.1.9` 实测（Go 1.27.1）。Eino 仍是 0.x，`eino-ext` 是独立模块、按自己的节奏发版，接口随时可能变。**正文只写行为，字段名以官方文档为准**；完整依赖与实测环境见篇末附录。
>
> 另外提前说一句：本篇写作时 `go get github.com/cloudwego/eino@latest` 解析到的是 **v0.9.19**，而 v0.10.0 那一整条线当时只有 `alpha` 版本（最高 `v0.10.0-alpha.32`）。**钉版本比读文档重要**，理由见 §8。

## 1. 先把第 3 篇的循环骨架摆出来

第 3 篇的核心循环（`agent-loop.md` §4）是这样的：

```go
const maxRounds = 8 // 保险丝：防止模型陷入"调用-报错-再调用"的死循环

for round := 1; round <= maxRounds; round++ {
	resp, err := chat(messages)
	if err != nil {
		fmt.Println("❌ 请求失败:", err)
		return
	}
	msg := resp.Choices[0].Message
	finish := resp.Choices[0].FinishReason
	messages = append(messages, msg)

	// 铁律：以 tool_calls 非空 / finish_reason=="tool_calls" 为准
	if len(msg.ToolCalls) == 0 || finish != "tool_calls" {
		fmt.Printf("🤖 第 %d 轮 最终回答: %s\n", round, msg.Content)
		return
	}

	for _, tc := range msg.ToolCalls {
		result, err := runTool(tc)
		if err != nil {
			result = fmt.Sprintf("工具执行出错：%v。请修正参数后重试，或放弃这一步。", err)
		}
		messages = append(messages, Message{
			Role:       "tool",
			ToolCallID: tc.ID,
			Content:    result,
		})
	}
	messages = trimHistory(messages, resp.Usage.PromptTokens)
}
fmt.Println("⚠️ 达到最大轮数，可能陷入循环。")
```

这段代码替我们做了四件事，每一件都是"必须做对、否则整个循环是错的"：

- **先 append `assistant` 消息，再 append `role="tool"` 结果**——顺序反了，工具结果找不到对应的调用，上游直接校验失败；
- **以 `tool_calls` 非空 / `finish_reason == "tool_calls"` 判定是否继续**——只看 `content` 会误判（第 2 篇的兼容铁律）；
- **工具报错包成 `role="tool"` 内容回喂**——让模型自己纠错，而不是整轮失败；
- **每轮 `trimHistory`**——历史随轮次线性增长，上下文是循环的第一成本。

也留下了三处代价，第 3 篇当时都在表格里承认了：

- `maxRounds` 是**硬上限**，跑满只是打印一句警告（"更细的重试策略（次数/退避/放弃条件）"属生产做法）
- 错误回喂只有**一句话**，没有结构化错误码
- `trimHistory` 在本篇 demo 里**根本不会触发**——第 3 篇自己也注明了："本示例是单个问题一路调工具跑到底，不会触发它"

现在的问题是：换成框架，这四件事和三处代价，分别由谁承担？

## 2. 同一件事，框架的写法

先把最小对照放在一起。同一件事——"跑一个能多轮调工具的 Agent"：

```go
// 手写版（第 3 篇）：循环、工具调度、消息顺序，全在你自己手里
const maxRounds = 8
for round := 1; round <= maxRounds; round++ {
	resp, err := chat(messages)
	// ... 判定、执行工具、追加消息、裁剪历史
}

// Eino 版（本篇）：循环是框架的，你只给配置
agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
	Name:          "local-agent",
	Instruction:   "你可以调用工具……",
	Model:         chatModel,
	ToolsConfig:   adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools}},
	MaxIterations: 8,
})

runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent})
iter := runner.Run(ctx, []*schema.Message{schema.UserMessage(prompt)})
for {
	ev, ok := iter.Next()
	if !ok {
		break
	}
	// 事件流里拿工具调用、工具结果、最终回答
}
```

第 3 篇那四件事里的**前三件直接消失了**：循环、消息追加顺序、`tool_calls` 判定，全部由框架承担。这是实打实的收益——第 3 篇里最容易写错的两条（append 顺序、`finish_reason` 判定）不再需要你操心。

但请注意两处细节：

**第一，`Run` 不返回 error。** 它只返回一个迭代器，错误以 `event.Err` 的形式出现在流里。所以错误处理从"检查函数返回值"变成了"检查每一个事件"——**漏检就是静默失败**。

**第二，`MaxIterations: 8` 这个 8，语义已经和第 3 篇的 `maxRounds = 8` 不一样了。** 这是本篇最重要的一件事，下一节展开。

## 3. 迭代上限：从"轮数预算"变成"硬错误"

先看框架这边的事实（`eino v0.9.19` 源码）：

- `adk.ChatModelAgentConfig.MaxIterations` 的官方注释是 _"Optional. **Defaults to 20**"_
- 源码里默认值是硬编码的：`react.go` 中 `maxIter := 20`，且**只有 `config.maxIterations > 0` 时才覆盖**
- 超限时 `react.go` 直接 `return nil, ErrExceedMaxIterations`，而 `ErrExceedMaxIterations = errors.New("exceeds max iterations")`

对手写版：

```go
// 手写版跑满：打印一句警告，正常返回，进程退出码 0
fmt.Println("⚠️ 达到最大轮数，可能陷入循环。")

// Eino 超限：返回错误，整个 run 终止
ErrExceedMaxIterations = errors.New("exceeds max iterations")
```

**同一根"保险丝"，一个是"轮数预算用完了"，一个是"出错了"。** 这个差别在生产上是实打实的：

- 手写版超限 → 你已经拿到了一部分有用结果，可以选择降级返回、或把"我尽力了"告诉用户
- Eino 超限 → 你拿到的是一个 error，除非自己兜住，否则这一轮对话就是失败

我用确定性桩模型（避免小模型随机性干扰结论）实测了四种情形：

| 用例 | `MaxIterations`   | 模型实际调用轮数 | 终止方式                      | `AfterAgent` |
| :--- | :---------------- | :--------------- | :---------------------------- | :----------- |
| A    | 未设置（默认）    | **20**           | 错误 `exceeds max iterations` | ❌ 未调用    |
| B    | 3（显式）         | **3**            | 错误 `exceeds max iterations` | ❌ 未调用    |
| C    | **0**             | **20**           | 错误 `exceeds max iterations` | ❌ 未调用    |
| D    | 5，但模型正常收尾 | 1                | **无错误，正常结束**          | ✅ 调用      |

三个可以直接拿去用的结论：

1. **默认值是 20，比手写版的 8 更宽松。** 第 3 篇选 8 是为了"防止模型陷入调用-报错-再调用的死循环"；换成框架默认的 20，同一个死循环会多烧 12 轮的 token。**这个数是业务判断，框架给不了。**

2. **`MaxIterations: 0` 不等于"无限"，而是回落到默认的 20。** 这一点容易踩：Eino 的 `LoopAgent`（另一类 Agent）里 `MaxIterations == 0` 的语义是**无限循环**（`i < a.maxIterations || a.maxIterations == 0`），而 `ChatModelAgent` 是"0 就用默认值 20"。**同一个字段名、两种相反语义**，想表达"我这里不设上限"在 `ChatModelAgent` 上是做不到的。

3. **超限时 `AfterAgent` 钩子不执行。** 这条官方在接口注释里写明了：_"AfterAgent is NOT called when the agent terminates with an error (e.g., ErrExceedMaxIterations, context cancellation, model errors)"_，框架自己的测试里也有个用例就叫 `NotCalledOnMaxIterations`。**后果是：任何你写在 `AfterAgent` 里的收尾逻辑——落盘、清理、统计、通知用户"这次没跑完"——在超限这条路径上会被静默跳过。** 这正是手写版 `fmt.Println("⚠️ 达到最大轮数")` 那行代码在框架里最容易丢失的地方。

所以：**迭代上限交不出去。** 框架提供的是机制（到点就停），不是策略（停在第几轮、停下之后怎么办）。

## 4. 工具报错：框架不会自动回喂

这是本机实测里最反直觉的一条，也是我认为框架篇最值得写的一条。

先回顾第 3 篇的设计意图：工具报错时，把错误包成普通工具结果回喂，让模型自己决定修正参数还是放弃。

```go
// 手写版（第 3 篇）
result, err := runTool(tc)
if err != nil {
	result = fmt.Sprintf("工具执行出错：%v。请修正参数后重试，或放弃这一步。", err)
}
```

我原本以为框架会"自动做这件事"——毕竟它接管了工具调度。**实测结果：不会。**

用确定性桩模型实测两种工具体（`MaxIterations = 3`，工具永远失败）：**工具返回 `error` 时，整个 run 以 `NodeRunError` 终止**，模型再也没机会看到这个错误，`AfterAgent` 也不会被调用；而工具返回失败提示文本 + `nil` error 时，提示正常进入工具消息、**回喂给模型**，循环继续，`AfterAgent` 按正常流程执行。

也就是说：**框架不会把"工具返回的 error"变成回喂内容——它把 error 当成 run 级的失败，直接终止。** 想得到第 3 篇那种"报错回喂、模型自纠"的效果，你**必须自己在工具里把失败转成一段正常字符串返回**（`nil` error）。

这不是理论推演。用真实模型（A770 + `llama3.1:8b`，`temperature=0, seed=42`，各跑 3 次）对照两种**参数解码**策略，差别更刺眼：用 `utils.InferTool` 把参数类型化到 Go `struct`（严格解码），**3 次全部在第 1 轮终止，通过率 0/3**，`AfterAgent` 一次都没执行；改成自己实现 `InvokableTool`、拿原始 JSON 字符串宽松解析后回喂，**3 次全部正常结束，通过率 3/3**，每次 2 轮完成、约 4 秒。

策略 1 三次全部死在同一个地方：

```
failed to unmarshal arguments in json, toolName=add,
err=json: cannot unmarshal string into Go struct field .a of type int
```

**本地小模型把 `{"a":10}` 写成了 `{"a":"10"}`。** 而 `utils.InferTool` 的类型化解码对类型漂移零容忍——一次类型不符就终止整个 run。策略 2 保留原始 JSON 后，我打出了模型实际发来的参数，问题一目了然：

```
[原始参数] {"a":10,"b":0}                          ← 正常
[原始参数] {"a":"2023-12-01T12:00:00","b":0}       ← 模型把时间戳塞进了整数字段
[原始参数] {"a":10,"b":2}                          ← 正常
```

模型**把时间戳序列化进了一个整数参数**。宽松解析把这个错误变成一句可读提示回喂，模型下一轮就修正了，2 轮内完成。这就是第 3 篇"错误回喂让 Agent 有韧性"的实际价值——**而框架把这个韧性默认关掉了。**

> Eino 的接口注释把话说得很直白：_"The framework handles JSON decoding automatically when using the `utils.InferTool` or `utils.NewTool` constructors."_ 换句话说，**严格解码正是 `InferTool` 卖给你的便利**。想要宽松，就得像第 3 篇一样自己拿原始 JSON——框架下依然如此。

**所以：工具错误策略交不出去，而且框架的默认选择对本地小模型不友好。** 这一条在本机实测里直接决定了 0/3 和 3/3 的差距。

## 5. 逐项对照：框架替你做了什么

把第 3 篇的决策逐条对上来。**第三列是判断，也是本篇真正想给你的东西**：

| 第 3 篇的决策                            | Eino 的默认行为                                      | 你需要覆盖吗？                                 |
| :--------------------------------------- | :--------------------------------------------------- | :--------------------------------------------- |
| 循环骨架（while 模型还要工具）           | 框架接管，你只给配置                                 | ❌ 交出去，安全                                |
| append 顺序（先 assistant 再 tool）      | 框架保证                                             | ❌ 交出去，安全                                |
| `tool_calls` 非空 / `finish_reason` 判定 | 框架判断                                             | ❌ 交出去，安全                                |
| `/v1` 兼容铁律（第 2 篇）                | ollama 组件走**原生 `/api/chat`**，绕开 `/v1` 兼容层 | ⚠️ 你写的 workaround 可能失效，见 §8           |
| `maxRounds = 8`                          | `MaxIterations` **默认 20**，`0` 回落到 20           | ✅ **必须自己定**，且要处理超限后的降级        |
| 跑满时打印警告、正常返回                 | 超限返回 `ErrExceedMaxIterations` 错误               | ✅ **必须自己兜**，否则整轮对话失败            |
| 错误包成 `role="tool"` 内容回喂          | **不会自动回喂**；工具返回 error 则终止 run          | ✅ **必须自己写**（错误转文本 + `nil` error）  |
| 参数解析失败时回喂提示                   | `InferTool` 严格解码，类型不符直接终止               | ✅ **必须自己写**（改用 `InvokableTool`）      |
| `trimHistory` 整组删除                   | 框架**不管**上下文预算                               | ✅ **必须自己控**（第 3 篇原本也只演示了思路） |
| 收尾逻辑（退出前做什么）                 | `AfterAgent` 仅在**正常终止**时执行                  | ⚠️ 超限/报错路径不会执行，别把关键逻辑只放这里 |

第三列是这张表真正的产出：它回答的不是"框架有没有这个能力"，而是"**这个能力你敢不敢交出去**"。

## 6. 完整代码

把上面的结论落到代码里。这个版本与第 3 篇的 demo 做同一件事（四个工具、刻意触发一次除零错误），差别在于**工具层用原始 JSON + 宽松解析 + 回喂**——也就是把框架默认关掉的韧性自己装回来。

<details>
<summary>main.go 全文（点击展开）</summary>

```go
// 框架篇 1 的完整可运行代码：用 Eino ADK 重写第 3 篇的 Agent 循环。
//
// 与第 3 篇手写版的对照点：
//   - 循环骨架、工具调度、消息追加顺序 → 交给框架
//   - 迭代上限、工具错误策略、上下文预算 → 自己管
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	ollamamodel "github.com/cloudwego/eino-ext/components/model/ollama"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Println("❌ 运行结束于错误:", err)
		os.Exit(1)
	}
}

// ===========================================
// 1. 工具：这一层决定了"报错"是终止还是回喂
// ===========================================

// lenientTool 自己拿原始 JSON 参数，而不是让框架严格解码进 int 字段。
//
// 实测（A770 + llama3.1:8b）：本地小模型会把 {"a":10} 写成 {"a":"10"}，
// 严格类型解码会直接终止整个 run；宽松解析 + 回喂则 3/3 都能自纠完成。
type lenientTool struct {
	info *schema.ToolInfo
	name string
	run  func(a, b int) (int, error)
}

func (t *lenientTool) Info(ctx context.Context) (*schema.ToolInfo, error) { return t.info, nil }

func (t *lenientTool) InvokableRun(ctx context.Context, argsJSON string, _ ...tool.Option) (string, error) {
	// 解析失败不返回 error（返回 error 会终止整个 run），
	// 而是把可读提示当"正常工具结果"回喂——这正是第 3 篇的做法。
	toInt := func(v any) (int, bool) {
		switch x := v.(type) {
		case float64:
			return int(x), true
		case string:
			if n, err := strconv.Atoi(strings.TrimSpace(x)); err == nil {
				return n, true
			}
		}
		return 0, false
	}

	var raw map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return fmt.Sprintf("工具执行出错：参数不是合法 JSON（%v）。请用 {\"a\":整数,\"b\":整数} 重试。", err), nil
	}
	a, okA := toInt(raw["a"])
	b, okB := toInt(raw["b"])
	if !okA || !okB {
		return fmt.Sprintf("工具执行出错：参数类型不对（收到 a=%v, b=%v）。请传整数，例如 {\"a\":10,\"b\":2}。", raw["a"], raw["b"]), nil
	}

	v, err := t.run(a, b)
	if err != nil {
		// 同样：错误转成文本回喂，而不是返回 err
		return fmt.Sprintf("工具执行出错：%v。请修正参数后重试，或放弃这一步。", err), nil
	}
	return fmt.Sprintf("%d", v), nil
}

type binIn struct {
	A int `json:"a" jsonschema:"description=第一个整数"`
	B int `json:"b" jsonschema:"description=第二个整数"`
}

// newTool 借 utils.InferTool 生成参数 schema，再换成自己的宽松执行体。
func newTool(name, desc string, run func(a, b int) (int, error)) tool.BaseTool {
	ref, err := utils.InferTool(name, desc,
		func(ctx context.Context, in *binIn) (string, error) { return "", nil })
	if err != nil {
		panic(err)
	}
	info, err := ref.Info(context.Background())
	if err != nil {
		panic(err)
	}
	return &lenientTool{info: info, name: name, run: run}
}

func buildTools() []tool.BaseTool {
	timeTool, err := utils.InferTool("get_current_time", "获取当前的日期和时间",
		func(ctx context.Context, in *struct{}) (string, error) {
			return time.Now().Format("2006-01-02 15:04:05"), nil
		})
	if err != nil {
		panic(err)
	}

	return []tool.BaseTool{
		timeTool,
		newTool("add", "计算两个整数 a 与 b 的和", func(a, b int) (int, error) { return a + b, nil }),
		newTool("multiply", "计算两个整数 a 与 b 的乘积", func(a, b int) (int, error) { return a * b, nil }),
		newTool("divide", "计算两个整数 a 除以 b 的商（整除）", func(a, b int) (int, error) {
			if b == 0 {
				return 0, fmt.Errorf("除数不能为 0")
			}
			return a / b, nil
		}),
	}
}

// ===========================================
// 2. 中间件：探测框架的收尾钩子何时执行
// ===========================================

type lifecycleProbe struct {
	adk.TypedBaseChatModelAgentMiddleware[*schema.Message]
}

func (p *lifecycleProbe) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext) (context.Context, *adk.ChatModelAgentContext, error) {
	fmt.Println("  🔔 BeforeAgent")
	return ctx, runCtx, nil
}

func (p *lifecycleProbe) AfterAgent(ctx context.Context, state *adk.ChatModelAgentState) (context.Context, error) {
	// 只有"正常终止"才会走到这里；超限、工具报错、模型错误都不会。
	fmt.Printf("  🔔 AfterAgent（共 %d 条消息）\n", len(state.Messages))
	return ctx, nil
}

// ===========================================
// 3. 循环：把骨架交给 ADK
// ===========================================

// 注意：这个数字的语义和第 3 篇的 maxRounds 不一样。
// 手写版跑满只是打印一句警告就正常返回；ADK 的 MaxIterations
// 超限会以 ErrExceedMaxIterations 终止整个 run。
const maxIterations = 8

func run(ctx context.Context) error {
	chatModel, err := ollamamodel.NewChatModel(ctx, &ollamamodel.ChatModelConfig{
		BaseURL: "http://localhost:11434",
		Model:   "llama3.1:8b",
		Timeout: 120 * time.Second,
	})
	if err != nil {
		return err
	}

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          "local-agent",
		Description:   "本地工具调用 Agent",
		Instruction:   "你可以调用工具。如果工具报错，请自行修正参数后重试，或说明放弃原因。",
		Model:         chatModel,
		ToolsConfig:   adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: buildTools()}},
		Handlers:      []adk.TypedChatModelAgentMiddleware[*schema.Message]{&lifecycleProbe{}},
		MaxIterations: maxIterations,
	})
	if err != nil {
		return err
	}

	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent})

	prompt := "请先计算 10 除以 0，再计算 10 除以 2，然后把两个结果与当前时间一起告诉我。"
	fmt.Println("🧑 用户:", prompt)
	fmt.Println(strings.Repeat("-", 56))

	// Run 只返回迭代器，不返回 error —— 错误以 event.Err 的形式出现在流里。
	iter := runner.Run(ctx, []*schema.Message{schema.UserMessage(prompt)})

	var terminalErr error
	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		if ev.Err != nil {
			terminalErr = ev.Err
			continue
		}
		if ev.Output == nil || ev.Output.MessageOutput == nil {
			continue
		}
		m := ev.Output.MessageOutput.Message
		if m == nil {
			continue
		}
		switch {
		case len(m.ToolCalls) > 0:
			for _, tc := range m.ToolCalls {
				fmt.Printf("  🔧 请求工具 %s(%s)\n", tc.Function.Name, tc.Function.Arguments)
			}
		case m.Role == schema.Tool:
			fmt.Printf("  📥 工具结果: %s\n", strings.TrimSpace(m.Content))
		case m.Content != "":
			fmt.Printf("  🤖 最终回答: %s\n", m.Content)
		}
	}

	if terminalErr != nil {
		// 超限和工具报错都会走到这里，且 AfterAgent 不会执行。
		if strings.Contains(terminalErr.Error(), "exceeds max iterations") {
			fmt.Printf("⚠️ 达到迭代上限 %d 轮，run 以错误终止\n", maxIterations)
		}
		return terminalErr
	}
	fmt.Println("✅ 正常结束")
	return nil
}
```

</details>

## 7. 运行结果（本机实测）

`go run main.go`，Ollama + `llama3.1:8b`，Intel Arc A770：

```text
🧑 用户: 请先计算 10 除以 0，再计算 10 除以 2，然后把两个结果与当前时间一起告诉我。
--------------------------------------------------------
  🔔 BeforeAgent
  🔧 请求工具 get_current_time({})
  🔧 请求工具 divide({"a":"10","b":"0"})
  🔧 请求工具 divide({"a":"10","b":"2"})
  📥 工具结果: 工具执行出错：除数不能为 0。请修正参数后重试，或放弃这一步。
  🔔 AfterAgent（共 7 条消息）
  🤖 最终回答: 你给出的第一个计算是 10 除以 0，这是不可能的，因为任何数字除以 0都无法得到一个正确的结果。

第二个计算是 10 除以 2，结果是 5。

当前时间是 2026-09-11 18:37:17。

注意：除数不能为 0。
✅ 正常结束
```

注意上面第三、四行：**这次运行模型发来的参数是 `{"a":"10","b":"0"}`——字符串，不是整数。** 如果这个 Agent 用的是 `utils.InferTool` 的严格解码，这次运行会在这一行直接终止，什么答案都拿不到。这就是 §4 那张 0/3 表格在真实运行里的样子。

有一点必须说清楚：**上面这段输出只是其中一次运行。** 同一份代码复跑，模型也可能发来 `{"a":10,"b":0}`（整数、完全合法），运行就正常通过了。模型对参数类型的遵循**在多次运行之间并不稳定**——而这恰恰是关键：严格解码把"偶发的类型漂移"升级成"整个 run 失败"，宽松解析把它降级成"一次可自纠的小插曲"。所以别把上面那段输出当成必然现象；要复现 §4 的 0/3，得看多次采样的统计——那张对照表之所以各跑 3 次而不是 1 次，正是因为单次结果说明不了任何问题。

一次性跑通的正常路径是 1~2 轮、约 4 秒量级（`temperature=0` 下实测 3.97~4.24s）；这也说明一件事：**这套本地组合的瓶颈不在框架，在模型。**

## 8. 坑与对照（实测验证）

| 现象                                                   | 原因                                                                                                  | 处理                                                 |
| :----------------------------------------------------- | :---------------------------------------------------------------------------------------------------- | :--------------------------------------------------- |
| 工具一报错，整个 run 就结束了                          | 框架把工具的 `error` 当 run 级失败，不当作回喂内容                                                    | 工具内部把失败转成字符串 + `nil` error 返回          |
| 换个工具就报 `cannot unmarshal string ... of type int` | `InferTool` 严格解码，小模型把数字写成字符串                                                          | 改用 `InvokableTool` 自己拿原始 JSON，宽松解析后回喂 |
| 跑满轮数后收尾逻辑没执行                               | 超限是错误终止，`AfterAgent` 不会调用                                                                 | 超限降级逻辑写在事件循环里，别只放 `AfterAgent`      |
| 轮数不对：默认跑了 20 轮，或设 `0` 以为不限            | 框架默认 20（比手写版 8 宽松）；且 `ChatModelAgent` 的 `0` 会回落到 20（`LoopAgent` 的 `0` 才是不限） | 按业务显式给一个明确数字                             |
| 忘了检查 `event.Err`                                   | `Run` 不返回 error，错误只在事件流里                                                                  | 事件循环里逐个检查 `ev.Err`                          |
| 第 2 篇的 `/v1` 兼容 workaround 好像失效了             | ollama 组件走**原生 `/api/chat`**，绕开 `/v1` 兼容层                                                  | 换组件等于换协议，兼容层差异要重新验证               |
| 依赖版本对不上文档                                     | `@latest` 可能解析到与文档不同的版本线（本篇实测 `latest` = v0.9.19，v0.10.0 当时只有 alpha）         | `go.mod` 里钉死版本，别依赖 `latest`                 |

## 9. 交出去 vs 自己管

本篇的核心产出，也是"框架篇"这个系列每篇都会给的一张表：

| 交出去（框架做，且做得比你手写更稳） | 自己管（框架给机制，不给策略）                   |
| :----------------------------------- | :----------------------------------------------- |
| 循环骨架（多轮 while）               | **迭代上限取多少**、超限之后怎么降级             |
| 工具调度与并行调用                   | **工具错误策略**（回喂还是终止）                 |
| 消息追加顺序、`tool_calls` 判定      | **参数解析的严格程度**（对本地小模型尤其关键）   |
| 单轮内的 ReAct 流转                  | **上下文预算与历史裁剪**                         |
| ——                                   | **收尾逻辑的触发路径**（正常 vs 出错要分开处理） |

一句话：**框架帮你省掉的是"循环怎么写"，省不掉的是"什么时候停、错了怎么办"。**

## 10. 刻意简化 vs 生产做法

| 本篇/框架的做法                 | 生产环境的做法                                                 |
| :------------------------------ | :------------------------------------------------------------- |
| 固定 `MaxIterations` 上限       | 轮数 + 总 token 双预算，超限降级返回部分结果                   |
| 工具错误一律回喂一句话          | 结构化错误码，区分"可重试"与"不该重试"                         |
| 宽松解析一切参数                | 宽松 + 严格双轨：先试严格，失败带明确提示回喂                  |
| 单会话、无状态                  | 会话隔离与记忆见第 5 篇                                        |
| 把整个 run 的错误直接抛给调用方 | 区分"模型错误/工具错误/超限"，映射成不同的 HTTP 状态与用户话术 |

## FAQ

| 问题                             | 解决                                                                                                   |
| :------------------------------- | :----------------------------------------------------------------------------------------------------- |
| 该不该用框架？                   | 循环骨架、工具调度值得交出去；但只要你的模型是本地小模型，参数解码和错误策略必须自己拿回来             |
| 为什么框架不自动回喂工具错误？   | 它把工具 `error` 视为 run 级失败。要回喂就自己把错误转成字符串返回                                     |
| 超限了怎么给用户一个体面的回答？ | 在事件循环里检查 `errors.Is(err, adk.ErrExceedMaxIterations)`，用已积累的历史自己再请求一次摘要式收尾  |
| 手写版要不要改成框架版？         | 如果循环已经稳定、工具就几个，收益有限；要接多 Agent、多模型、可观测性时收益才明显                     |
| 框架升级会不会把本篇结论推翻？   | 有可能。所以本篇写的是**行为**（"超限是错误"、"不自动回喂"）而不是字段名；动手前用附录里的版本重跑一次 |

## 结论

1. **循环骨架可以交出去**：多轮循环、工具调度、消息顺序、`tool_calls` 判定都由框架接管，第 3 篇里最容易写错的两处不再需要操心；
2. **终止条件必须自己控**：`MaxIterations` 默认 20（手写版是 8），`0` 不是无限而是回落默认，超限返回 `ErrExceedMaxIterations` 错误而不是优雅降级；
3. **错误策略必须自己写**：框架**不会**把工具错误回喂给模型，而是终止整个 run；实测严格解码 0/3 通过、自己宽松解析 + 回喂 3/3 通过；
4. **收尾钩子有盲区**：`AfterAgent` 只在正常终止时执行，超限和报错路径都会跳过——关键收尾别只挂在那里；
5. 本篇仍是一次性运行的命令行程序，**把它变成流式 HTTP 服务**是下一篇的主题。

下一篇预告：**《框架：流式事件到手了，SSE 还得自己写》**——框架交给你的是事件迭代器，不是 SSE。

## 附录：版本与实测环境

**依赖版本**（`go.mod` 锁定，实测日期 2026-09-11）：

```text
github.com/cloudwego/eino                          v0.9.19
github.com/cloudwego/eino-ext/components/model/ollama  v0.1.9
github.com/cloudwego/eino-ext/components/model/openai  v0.1.13
github.com/eino-contrib/ollama                     v0.1.0
```

**实测环境**：Go 1.27.1 / Ollama + `llama3.1:8b` / Intel Arc A770 / 生成参数 `temperature=0, seed=42`（对照实验）或默认（§7 单次运行）。

**关于版本的三个提醒**：

1. `go get github.com/cloudwego/eino@latest` 在本篇实测时解析到 **v0.9.19**；v0.10.0 那一整条线当时只发布了 `alpha`（最高 `v0.10.0-alpha.32`）。**pkg.go.dev 的版本列表里出现过 `v0.10.0`，但 `go get` 拉不到**——以 `go get` 的实际结果为准。
2. `eino-ext` 的组件是**独立模块**，各自发版。例如 `components/model/ollama` 有 tag（v0.1.9），而 `components/embedding/ollama` 当时**只有伪版本**（`v0.0.0-20260909094858-6fe16d48b736`）。所以"Eino 的版本号"从来不是一个数，而是**一组数**。
3. 框架迭代上限等行为的**源码位置**（v0.9.19）：默认值在 `adk/react.go`（`maxIter := 20`），错误定义在 `adk/react.go`（`ErrExceedMaxIterations`），`AfterAgent` 的执行条件在 `adk/handler.go` 的接口注释里。**换版本后请以源码为准重跑一次**。

## 本文结论是否仍成立（自己就能验）

Eino 还在 0.x，本篇写的是**行为**而不是字段名，所以换个版本很可能有人想重跑一遍。好消息是：**验这些结论不需要任何额外代码——你手上就有。**

第 3 篇那份循环、第 2 篇那个 `chat()` 都是你自己敲的。把 `chat()` 换成一个固定返回 `tool_calls` 的桩函数（十几行：不管输入是什么，都回一个请求调用某工具的消息），就能验完下面四条。每条给三样：**怎么验 / 看到什么算成立 / 变了该怎么读**。

**① 默认迭代上限还是 20 吗？**（改几行）

用上面那个桩模型，`MaxIterations` 不赋值，数它调了几次模型。
**成立**：20 次后以错误终止。**变了**：数字本身变了只影响 §3 那句"比手写版 8 更宽松"的对比，改数字即可；但如果它跑个不停，说明默认值的语义变了，§3 第 2 条要重写。

**② `MaxIterations: 0` 还是回落到默认吗？**（改几行）

同上，但显式设成 `0`。
**成立**：行为和"不设"完全一样（都是 20）。
**变了**：若 `0` 变成了"无限"（也就是向 `LoopAgent` 的语义靠齐），§3 第 2 条结论和 §8 坑表里对应那行都要改写。

**③ 超限还是返回错误吗？**（改几行）

取事件流最后那个事件的 `ev.Err`，用 `errors.Is(err, adk.ErrExceedMaxIterations)` 判断。
**成立**：为 `true`。
**变了**：**若改成优雅降级**（像手写版那样打印一句就正常收尾），§3 第 1 条、以及"终止条件必须自己控"这条主线都要重写——那是本篇最核心的结论。

**④ `AfterAgent` 还是不在错误路径执行吗？**（改几行，但有个坑）

注册一个中间件（嵌入 `*adk.BaseChatModelAgentMiddleware`，在 `AfterAgent` 里打印一行），然后在三种情况下各跑一次：正常收尾、超限、工具报错。**成立**：只有正常收尾那次会打印。
**变了**：若三个错误路径也开始执行，§3 第 3 条与 §9 边界表里"收尾逻辑的触发路径"那一行都要删掉。

> **这里有个必须知道的坑**：这个钩子**没法从事件流里观测**。它被调用时才有输出；不被调用时不产生任何事件——在事件流里，"钩子没执行"和"钩子执行了但没说话"长得一模一样。**"没发生"这件事本身是不可观测的**，所以必须自己架一个"本该说话"的探针，它的沉默才成为证据。

**⑤ 工具报错还会终止整个 run 吗？**（改几行）

写一个一定返回 `error` 的工具，看 run 是被终止，还是把错误回喂给模型继续跑。
**成立**：终止（`NodeRunError`），模型再没机会看到那个错误。
**变了**：**这是本篇最容易被改掉的一条。** 如果哪天框架默认把工具错误回喂给模型，§4 整节、§9 边界表里两行、以及 FAQ 里"为什么框架不自动回喂"都要重写。

**§7 的轮数与耗时**属环境相关（模型是否常驻显存影响很大，见第 7 篇 §3），复跑对不上是正常的，不代表框架变了。

### 判读原则

**只有"默认值"和"轮数"这类数字变了，文章主体仍然成立，改数字就行；一旦"超限是错误"或"不自动回喂"这两条变了，说明框架改了立场——那要重写，不是改数字。**

顺带说一句：这四条检查里，有两条（③④）恰好验证的是**框架的立场**而不是参数。立场变了，说明框架对你的责任范围变了，这比任何数字变化都更值得重新读一遍。
