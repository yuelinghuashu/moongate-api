---
title: 记忆：多会话与轻量 RAG
description: 给 Agent 补上两类记忆：跨会话键值事实（remember/recall，落盘 JSON）与对本地笔记的轻量 RAG（embed → 余弦 → topK），并用本机实测讲清阈值与 embedding 前缀这两个常被误传的细节（为什么固定阈值会形同虚设）。
date: 2026-09-08
series: go-agent
order: 5
tags:
  - Go
  - Agent
  - LLM
---

第 3 篇的多轮循环让 Agent 会干活了，但它每次启动都是“失忆”的。本篇补上两类记忆：**跨会话事实**（remember/recall 键值记忆，落盘 JSON）与**资料检索**（对本地笔记做向量检索的轻量 RAG）。

- 前置：第 1 篇文末环境自检清单全绿 + 第 3 篇的多轮循环已跑通
- 准备：`ollama pull nomic-embed-text`（约 274MB；没有此模型时示例会自动降级为关键词检索）
- 形态说明：本篇沿用第 3 篇的**命令行一次性运行**（三个会话在同一个进程内顺序演示「会话隔离」；`go run main.go recall` 可另起一个进程单独验证跨进程持久化），不扩展第 4 篇的 SSE 服务；要把记忆接入服务，把这里的工具表与循环搬进服务端即可

**本篇相对第 3 篇改了什么（读代码前先看这张表）：**

| 改动                                                                 | 落在哪里                               | 为什么                                                         |
| -------------------------------------------------------------------- | -------------------------------------- | -------------------------------------------------------------- |
| 工具整体换掉：`remember` / `recall` / `list_memory` / `search_notes` | `tools`                                | 两类记忆各一套工具：键值事实 + 资料检索（§1、§2）              |
| 新增 `memoryStore`，读写本地 `memory.json`                           | `loadMemory` / `set`                   | 跨会话、跨进程的事实记忆（进程退出也不丢）                     |
| 新增 `retriever` 接口与两种实现                                      | `vectorRetriever` / `keywordRetriever` | 有 embedding 模型走向量检索，没有则自动降级关键词              |
| 新增 `tokenize` / `top` / `cosine` / `embedTexts`                    | 检索零件                               | 轻量 RAG 的全部实现，约 60 行（§3）                            |
| `runAgent(userPrompt)` → `runAgent(messages)`，另加三个会话函数      | `main` / `runSessionA-C`               | 一次运行内演示「会话隔离」；`go run main.go recall` 演示跨进程 |
| 固定 `temperature=0`                                                 | `chat()`                               | 演示输出稳定可复现                                             |
| 移除 `trimHistory` 与 `usage` 统计                                   | —                                      | 本篇聚焦记忆与检索；历史增长问题仍见第 3 篇 §4                 |

> 与第 3 篇相同、本篇**没有**改的部分：`Message`/`ToolCall`/`Tool` 等结构体、循环骨架、`role="tool"` 回喂与"`tool_calls` 非空"判据、`toolEntry` 注册表形态。

## 1. 记忆分三种，别混为一谈

| 类型       | 是什么                       | 存哪                   | 本篇做法                  |
| ---------- | ---------------------------- | ---------------------- | ------------------------- |
| 会话内记忆 | 本轮对话上下文               | 内存里的 messages 切片 | 前几篇一直在用            |
| 长期记忆   | 跨会话的事实（用户名、偏好） | 外部存储               | 键值对写入本地 JSON 文件  |
| 资料记忆   | 本地文档/笔记，可检索        | 文档库 + 索引          | 向量检索 topK（轻量 RAG） |

本篇用两套工具补上后两类记忆（前几篇的 Agent 每次启动都是"失忆"的：`messages` 一清空就什么都不记得）：`remember`/`recall`/`list_memory`（长期事实）+ `search_notes`（资料检索）。（本篇说的"会话隔离"就是：每个会话各自持有自己的 `messages` 切片，A 的对话历史不会出现在 B 里；能跨会话共享的只有落盘的 `memory.json`。）

## 2. 长期记忆：键值事实 + 枚举能力

工具设计上有三个细节，决定了记忆好不好用：

1. **持久化**：`remember` 把键值写进 `memory.json`，进程重启也不丢；
2. **key 用命名空间风格**：`用户:名字`、`用户:语言`，避免不同主题撞键；
3. **必须有 `list_memory`（枚举）**：键值记忆的前提是"知道键名"。新会话里模型不知道别人写过什么键，只靠 `recall` 猜键会失败（实测它会去猜 `用户:名字`、甚至传 `*`）；提供枚举工具后，它列出全部键值即可作答。

## 3. 轻量 RAG：embed → 余弦 → topK

先说一句白话：**向量化（embedding）就是把一段文本交给模型换成一串数字（向量）**——语义越近的文本，向量方向越接近，于是"像不像"这件事可以直接用余弦相似度算出来。

`search_notes` 的实现思路就是最小 **RAG**（Retrieval-Augmented Generation，检索增强生成：先把资料检索出来，再让模型"照着资料"作答，而不是靠它自己的记忆）：

```
把问题(query) 和 每条笔记 分别向量化
→ 算 query 与每条笔记的余弦相似度
→ 取相似度 >= 0.35 的 topK 片段
→ 把片段拼进工具结果，让模型"依据笔记作答"
```

一次 `search_notes` 调用里发生了什么：

```
模型给出 query
      │
      ▼
包装成向量输入：查询加 "search_query: "、每条笔记加 "search_document: "
      │   （前缀是 nomic-embed-text 自己的约定，遵守但不指望它单独救场）
      ▼
POST /api/embed（一次请求带上全部文本）──▶ [向量…]
      ▼
逐条算余弦相似度 ──▶ 阈值 0.35 粗筛 ──▶ 排序取 topK=3
      ▼
拼成工具结果：开头写死"只依据这些片段回答，不要编造" + [片段N 相似度X.XX] 片段正文
      ▼
作为 role="tool" 回喂 ──▶ 模型基于片段作答（§5 会话 C 的实测输出）
```

> 降级路径：没有 `nomic-embed-text` 时同一张接口换成关键词打分（命中词数），流程与上面完全一致，只是"相似度"那一栏变成"命中词数"——两种分数不可比，所以不能共用一个标签。

后面两件事**和常见直觉不一样**，先给本机实测（Ollama 0.33.3 + `nomic-embed-text`，语料就是上面的 6 条笔记，数值为余弦相似度）：

| 查询                                | 带前缀（`search_document:` / `search_query:`） | 不带前缀      |
| ----------------------------------- | ---------------------------------------------- | ------------- |
| `Intel Arc Ollama GPU 加速`（相关） | 0.560 ~ 0.778                                  | 0.592 ~ 0.771 |
| `怎么做红烧肉？`（完全无关）        | 0.500 ~ 0.598（6/6 全部过 0.35）               | 0.465 ~ 0.530 |

- **相似度阈值必须按自己的语料标定，照抄一个 0.35 往往会形同虚设**：短文本、同主题的语料里，余弦分数会被压在很窄的高位带（本例 0.5~0.8），**连完全无关的问题也能拿到 0.5 以上**，于是 6 条笔记全部过阈——真正把片段收敛到 3 条的是 `topK`。阈值本身不是错的设计（语料差异大、文档长时分数才会散开），但它得配着自己的语料调；本篇保留 `>= 0.35` 只为演示写法，**别照抄这个数字**。
- **embedding 前缀是模型自己的约定，建议遵守，但别指望它单独解决问题**：`nomic-embed-text` 要求文档加 `search_document:`、查询加 `search_query:`；本机实测加与不加差别很小（最高分 0.778 vs 0.771，排序几乎不变），语料差异更大时才会显出来。相比前缀，**阈值标定、片段切分与 topK 的影响通常更大**。

## 4. 完整代码

<details>
<summary>main.go 全文（点击展开）</summary>

```go
// 第 5 篇演示：记忆与轻量 RAG。
// 与第 3 篇的差异见正文改动清单表；核心是 memoryStore（落盘 JSON）、
// retriever（向量检索 + 关键词降级）与四个新工具。
//
// 运行：go run main.go         —— 顺序跑会话 A / B / C
//
//	go run main.go recall    —— 只跑会话 B（用于演示"跨进程"仍记得，见第 5 节）
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// ===========================================
// 与 /v1/chat/completions 对应的结构
// ===========================================

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
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

// FunctionCall 的含义见第 2 篇 §3（tool_calls[].function）
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []Tool    `json:"tools,omitempty"`
	Stream      bool      `json:"stream"`
	Temperature float64   `json:"temperature"`
}

type Choice struct {
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type ChatResponse struct {
	Choices []Choice `json:"choices"`
}

// ===========================================
// 记忆存储（跨会话，持久化到文件）
// ===========================================

type memoryStore struct {
	path string
	data map[string]string
}

func loadMemory(path string) *memoryStore {
	m := &memoryStore{path: path, data: map[string]string{}}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &m.data)
	}
	return m
}

func (m *memoryStore) set(key, value string) error {
	m.data[key] = value
	b, _ := json.MarshalIndent(m.data, "", "  ")
	return os.WriteFile(m.path, b, 0o644)
}

// ===========================================
// 笔记库（模拟"本地资料"）与检索器
// ===========================================

var notes = []string{
	"Ollama 默认只识别 NVIDIA(CUDA) 与 AMD(ROCm) 显卡；Intel Arc 需要在 systemd override 里设置 OLLAMA_VULKAN=true 并重启服务，才会走 Vulkan 后端。",
	"验证 Vulkan 是否生效：journalctl -u ollama --no-pager | grep 'inference compute'，看到 library=Vulkan 与 Arc A770 的描述即为成功。",
	"OLLAMA_LOAD_TIMEOUT 默认 5 分钟，控制模型加载停滞多久后放弃；驱动慢或首次编译着色器时可调大，例如 10m。",
	"OLLAMA_KEEP_ALIVE 默认 5 分钟，控制模型空闲多久后卸载；频繁调用可调大到 10m 以减少重复加载。",
	"Vulkan 后端在部分 Linux 内核 + Mesa 驱动下有显存记账失步、空闲显存被换出的已知问题，长时间运行要用 intel_gpu_top 或 journalctl 监控。",
	"llama3.1:8b 在 Intel Arc A770 上实测 33/33 层全量 offload，模型占显存约 4.4GB，生成速度约 41 tokens/s。",
}

// retriever 负责给 query 找最相关的笔记片段。
// metric() 返回 score 的含义：向量检索是余弦相似度，关键词降级是命中词数——
// 两者量纲不同，打印时不能共用"相似度"这个标签。
type retriever interface {
	search(query string, topK int) []searchHit
	metric() string
}

type searchHit struct {
	text  string
	score float64
}

// vectorRetriever 用 /api/embed 的向量做余弦相似度。
// nomic-embed-text 需要标准前后缀才能发挥效果：文档加 "search_document:"、
// 查询加 "search_query:"（这是 embedding 模型自己的约定）。
type vectorRetriever struct{}

func (vectorRetriever) metric() string { return "相似度" }
func (vectorRetriever) search(query string, topK int) []searchHit {
	texts := make([]string, 0, len(notes)+1)
	texts = append(texts, "search_query: "+query)
	for _, n := range notes {
		texts = append(texts, "search_document: "+n)
	}
	vecs, err := embedTexts(texts)
	if err != nil {
		fmt.Println("   ⚠️ 向量检索失败，本次降级为空结果:", err)
		return nil
	}
	if len(vecs) != len(texts) { // 返回数量与输入不一致时避免越界
		fmt.Println("   ⚠️ 向量检索返回数量不符，本次降级为空结果")
		return nil
	}
	q := vecs[0]
	var hits []searchHit
	for i, n := range notes {
		s := cosine(q, vecs[i+1])
		if s >= 0.35 { // 阈值太低会把无关片段也捞进来
			hits = append(hits, searchHit{n, s})
		}
	}
	return top(hits, topK)
}

// keywordRetriever 无 embedding 模型时的降级：按命中的词数打分。
type keywordRetriever struct{}

func (keywordRetriever) metric() string { return "命中词数" }

func (keywordRetriever) search(query string, topK int) []searchHit {
	words := tokenize(query)
	var hits []searchHit
	for _, n := range notes {
		score := 0.0
		lower := strings.ToLower(n) // 英文大小写不敏感（如 intel ↔ Intel）
		for _, w := range words {
			if strings.Contains(lower, w) {
				score++
			}
		}
		if score > 0 {
			hits = append(hits, searchHit{n, score})
		}
	}
	return top(hits, topK)
}

func tokenize(s string) []string {
	// 中文按 2 字滑窗切分 + 英文按空白切分，足够演示用
	var out []string
	runes := []rune(s)
	for i := 0; i+1 < len(runes); i++ {
		if runes[i] < 128 && runes[i+1] < 128 {
			continue
		}
		out = append(out, string(runes[i:i+2]))
	}
	for _, w := range strings.FieldsFunc(s, func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') }) {
		if len(w) > 1 {
			out = append(out, strings.ToLower(w))
		}
	}
	return out
}

func top(hits []searchHit, k int) []searchHit {
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0 && hits[j].score > hits[j-1].score; j-- {
			hits[j], hits[j-1] = hits[j-1], hits[j]
		}
	}
	if len(hits) > k {
		hits = hits[:k]
	}
	return hits
}

func cosine(a, b []float64) float64 {
	if len(a) != len(b) { // 维度不一致时直接判 0，避免越界 panic
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// ===========================================
// 工具注册
// ===========================================

type toolEntry struct {
	description string
	parameters  map[string]any
	run         func(args json.RawMessage) (string, error)
}

var (
	mem                = loadMemory("memory.json")
	retrieve retriever = newRetriever()
)

func newRetriever() retriever {
	// 探测 /api/tags 里有没有 embedding 模型；有则用向量，没有降级关键词。
	// 这是启动路径上的一次 HTTP 调用，必须带超时：Ollama 卡住时不能拖住启动
	// （第 2 篇「给上游一个明确的超时预算」在这里同样适用）。
	probe := &http.Client{Timeout: 3 * time.Second}
	resp, err := probe.Get("http://localhost:11434/api/tags")
	if err == nil {
		defer resp.Body.Close()
		var tags struct {
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		}
		if json.NewDecoder(resp.Body).Decode(&tags) == nil {
			for _, m := range tags.Models {
				if strings.Contains(m.Name, "nomic-embed-text") {
					fmt.Println("📚 检测到 nomic-embed-text，启用向量检索")
					return vectorRetriever{}
				}
			}
		}
	}
	fmt.Println("📚 未检测到 embedding 模型，降级为关键词检索（可 ollama pull nomic-embed-text 升级）")
	return keywordRetriever{}
}

var tools = map[string]*toolEntry{
	"remember": {
		description: "把一条事实写入长期记忆（跨会话保留）。key 用命名空间风格，如 用户:名字、项目:目标",
		parameters: map[string]any{"type": "object", "properties": map[string]any{
			"key":   map[string]any{"type": "string", "description": "记忆的键，如 用户:名字"},
			"value": map[string]any{"type": "string", "description": "记忆的内容"},
		}, "required": []string{"key", "value"}},
		run: func(args json.RawMessage) (string, error) {
			var p struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("参数解析失败：%v", err)
			}
			if err := mem.set(p.Key, p.Value); err != nil {
				return "", err
			}
			return fmt.Sprintf("已记住 %s = %s", p.Key, p.Value), nil
		},
	},
	"recall": {
		description: "从长期记忆里读取一条事实。key 与 remember 写入时一致；不确定键名时先用 list_memory 列出",
		parameters: map[string]any{"type": "object", "properties": map[string]any{
			"key": map[string]any{"type": "string", "description": "记忆的键"},
		}, "required": []string{"key"}},
		run: func(args json.RawMessage) (string, error) {
			var p struct {
				Key string `json:"key"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("参数解析失败：%v", err)
			}
			if v, ok := mem.data[p.Key]; ok {
				return v, nil
			}
			return fmt.Sprintf("长期记忆中没有 %s 的记录", p.Key), nil
		},
	},
	"list_memory": {
		description: "列出长期记忆里所有的键与值，用于不确定键名时查看",
		parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		run: func(args json.RawMessage) (string, error) {
			if len(mem.data) == 0 {
				return "长期记忆为空", nil
			}
			// 排序输出：Go 的 map 遍历顺序随机，不排序则每次运行的键序都可能不同
			// （与 toolDefs 同一个理由：输出稳定才便于复现与比对）
			keys := make([]string, 0, len(mem.data))
			for k := range mem.data {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			var sb strings.Builder
			for _, k := range keys {
				fmt.Fprintf(&sb, "%s = %s\n", k, mem.data[k])
			}
			return strings.TrimSpace(sb.String()), nil
		},
	},
	"search_notes": {
		description: "检索本地运维笔记库，返回最相关的片段，用于回答需要查资料的问题",
		parameters: map[string]any{"type": "object", "properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "要检索的问题或关键词"},
		}, "required": []string{"query"}},
		run: func(args json.RawMessage) (string, error) {
			var p struct {
				Query string `json:"query"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("参数解析失败：%v", err)
			}
			hits := retrieve.search(p.Query, 3)
			if len(hits) == 0 {
				return "笔记库中没有找到相关内容。", nil
			}
			var sb strings.Builder
			sb.WriteString("（以下是检索到的笔记片段，请严格只依据这些内容回答，不要编造笔记里没有的细节）\n")
			for i, h := range hits {
				fmt.Fprintf(&sb, "[片段%d %s%.2f] %s\n", i+1, retrieve.metric(), h.score, h.text)
			}
			return strings.TrimSpace(sb.String()), nil
		},
	},
}

// toolDefs 与第 3 篇相同（按名字排序，保证请求体稳定，理由见第 3 篇 §2）。
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
// 会话与 Agent 循环
// ===========================================

func main() {
	// 跨进程持久化演示：先 go run main.go 写入记忆（进程退出），
	// 再 go run main.go recall 只跑会话 B——历史完全不共享，靠 memory.json 答出来。
	if len(os.Args) > 1 && os.Args[1] == "recall" {
		fmt.Println("===== 会话 B（新进程）：换一个全新会话（历史不共享），问它记得我吗 =====")
		runSessionB()
		return
	}

	fmt.Println("===== 会话 A：让 Agent 记住一些事实 =====")
	runSessionA()
	fmt.Println()
	fmt.Println("===== 会话 B：换一个全新会话（历史不共享），问它记得我吗 =====")
	runSessionB()
	fmt.Println()
	fmt.Println("===== 会话 C：查笔记回答问题（轻量 RAG）=====")
	runSessionC()
}

func runSessionA() {
	runAgent([]Message{{Role: "user", Content: "请调用两次 remember 工具：第一次 key=用户:名字、value=小明；第二次 key=用户:语言、value=Go。只做这两次调用，不要做其他事。"}})
}

func runSessionB() {
	runAgent([]Message{{Role: "user", Content: "这是一个全新的会话。请调用 list_memory 一次把长期记忆全部列出来，然后直接根据列出的内容回答我：你记得关于我的什么？列完就不用再调用其他工具了。"}})
}

func runSessionC() {
	runAgent([]Message{{Role: "user", Content: "问题：Intel Arc 显卡怎么在 Ollama 里启用 GPU 加速？请先 search_notes 检索运维笔记，再严格基于检索到的内容回答。"}})
}

func runAgent(messages []Message) {
	const maxRounds = 8 // 保险丝：与第 3/4 篇一致，防止模型陷入死循环
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
		// 与第 2/3 篇一致：以 tool_calls 非空为准（finish_reason 只作校验，
		// 部分上游调用工具时仍返回 "stop"，不能拿它当唯一判据）
		if len(msg.ToolCalls) == 0 {
			fmt.Println("🤖", msg.Content)
			return
		}
		if finish != "tool_calls" {
			fmt.Printf("   ⚠️ finish_reason=%q 但本轮有 tool_calls，按 tool_calls 继续执行\n", finish)
		}
		fmt.Printf("🔧 第 %d 轮模型请求 %d 个工具\n", round, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			t, ok := tools[tc.Function.Name]
			result := ""
			if !ok {
				result = fmt.Sprintf("未知工具 %q（模型幻觉了工具名）", tc.Function.Name)
			} else {
				result, err = t.run(json.RawMessage(tc.Function.Arguments))
				if err != nil {
					result = fmt.Sprintf("工具执行出错：%v。请修正参数后重试，或放弃这一步。", err)
				}
			}
			if result == "" {
				result = "(工具无输出)" // 同第 3 篇：role="tool" 消息必须带 content（见第 2 篇 §3）
			}
			fmt.Printf("   - %s(%s)\n     => %s\n", tc.Function.Name, tc.Function.Arguments, result)
			messages = append(messages, Message{Role: "tool", ToolCallID: tc.ID, Content: result})
		}
	}
}

// ===========================================
// HTTP 调用（chat 与 embed）
// ===========================================

var httpClient = &http.Client{Timeout: 5 * time.Minute} // 请求超时：上游卡住时不至于无限挂起

func chat(messages []Message) (ChatResponse, error) {
	reqBody := ChatRequest{
		Model:       "llama3.1:8b",
		Messages:    messages,
		Tools:       toolDefs(),
		Stream:      false,
		Temperature: 0, // 演示场景用贪心解码，输出更稳定；聊天场景可调回 0.7
	}
	jsonData, _ := json.Marshal(reqBody)
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
		return ChatResponse{}, fmt.Errorf("解析失败: %s", string(body))
	}
	return result, nil
}

func embedTexts(texts []string) ([][]float64, error) {
	payload, _ := json.Marshal(map[string]any{"model": "nomic-embed-text", "input": texts})
	resp, err := httpClient.Post("http://localhost:11434/api/embed", "application/json", bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embed %d: %s", resp.StatusCode, string(b))
	}
	var out struct {
		Embeddings [][]float64 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Embeddings, nil
}
```

</details>

## 5. 运行结果（本机实测）

> ⚠️ **运行前先清掉旧记忆**：`rm -f memory.json`——否则会带着上次运行写入的事实跑，会话 A 的"记住"看起来就像没生效。

```bash
rm -f memory.json   # 干净复现：不要带上历史记忆
go run main.go
```

真实输出（Ollama 0.33.3 + llama3.1:8b + nomic-embed-text，2026-09-19 实测；`temperature=0`，且本篇的 prompt 不含时间戳这类动态内容，所以可以逐字复现）：

```
📚 检测到 nomic-embed-text，启用向量检索
===== 会话 A：让 Agent 记住一些事实 =====
🔧 第 1 轮模型请求 2 个工具
   - remember({"key":"用户:名字","value":"小明"})
     => 已记住 用户:名字 = 小明
   - remember({"key":"用户:语言","value":"Go"})
     => 已记住 用户:语言 = Go
🤖 这两次调用成功记住了用户的名字和语言。

===== 会话 B：换一个全新会话（历史不共享），问它记得我吗 =====
🔧 第 1 轮模型请求 2 个工具
   - list_memory({})
     => 用户:名字 = 小明
用户:语言 = Go
   - recall({"key":"用户:名字"})
     => 小明
🤖 你记得关于我的名字是“小明”，语言是Go。

===== 会话 C：查笔记回答问题（轻量 RAG）=====
🔧 第 1 轮模型请求 1 个工具
   - search_notes({"query":"Intel Arc Ollama GPU 加速"})
     => （以下是检索到的笔记片段，请严格只依据这些内容回答，不要编造笔记里没有的细节）
[片段1 相似度0.78] Ollama 默认只识别 NVIDIA(CUDA) 与 AMD(ROCm) 显卡；Intel Arc 需要在 systemd override 里设置 OLLAMA_VULKAN=true 并重启服务，才会走 Vulkan 后端。
[片段2 相似度0.69] llama3.1:8b 在 Intel Arc A770 上实测 33/33 层全量 offload，模型占显存约 4.4GB，生成速度约 41 tokens/s。
[片段3 相似度0.65] 验证 Vulkan 是否生效：journalctl -u ollama --no-pager | grep 'inference compute'，看到 library=Vulkan 与 Arc A770 的描述即为成功。
🤖 基于检索到的内容，答案如下：

要在 Ollama 里启用 GPU 加速的 Intel Arc 显卡，请按照以下步骤操作：

1. 在 systemd override 里设置 OLLAMA_VULKAN=true。
2. 重启 Ollama 服务。

这样，Ollama 就会使用 Vulkan 后端，来利用 Intel Arc 显卡的 GPU 加速功能。

另外，为了验证 Vulkan 是否生效，可以使用以下命令：

journalctl -u ollama --no-pager | grep 'inference compute'

如果看到 library=Vulkan 与 Arc A770 的描述，则说明 Vulkan 已经生效，GPU 加速功能已经启用。
```

三个会话各演示一件事：

1. **A（写入）**：两个 `remember` 把事实写入 `memory.json`；
2. **B（跨会话读取）**：全新会话、历史完全不共享，靠 `list_memory` 枚举键名、再用 `recall` 取具体值，然后正确回答——这就是"记忆"与"上下文"的区别。想验证"跨进程"而不只是"跨会话"，跑两条命令即可（第二条是**另一个进程**，进程内的 messages 一定是空的）：

   ```bash
   go run main.go          # 会话 A 写入 memory.json 后进程退出
   go run main.go recall   # 新进程只跑会话 B：仍然答得出"小明 / Go"
   ```

   第二条命令的实测输出（同一份 `memory.json`，新进程、无任何对话历史）：

   ```
   📚 检测到 nomic-embed-text，启用向量检索
   ===== 会话 B（新进程）：换一个全新会话（历史不共享），问它记得我吗 =====
   🔧 第 1 轮模型请求 2 个工具
      - list_memory({})
        => 用户:名字 = 小明
   用户:语言 = Go
      - recall({"key":"用户:名字"})
        => 小明
   🤖 你记得关于我的名字是“小明”，语言是Go。
   ```

   > 这正是"记忆 = 外部状态 + 工具"的实证：模型自己不记得任何东西，它只是**又一次调用了工具**。

3. **C（RAG）**：检索命中"设置 `OLLAMA_VULKAN=true`"这条笔记（相似度 0.78），回答严格围绕检索片段，包括验证命令。



## 6. 坑与对照（实测验证）

| 现象                           | 原因                                                                              | 处理                                                                                     |
| ------------------------------ | --------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| 让小模型"自己抽取事实"会写错值 | 8B 模型对中文抽取不稳（实测把"小明"写成"小明积"）                                 | 演示场景用显式 key/value 提示；生产让强模型抽取或写入前校验                              |
| 新会话里 `recall` 猜不到键     | 键值记忆需要"知道键名"，而键对模型是不可见的                                      | 提供 `list_memory` 枚举工具——枚举是"想起来"的前提（本篇实测此法稳定）                    |
| 检索结果不够准、捞到无关片段   | 短文本同主题时余弦分数被压在 0.5~0.8 窄带，固定阈值几乎不过滤；前缀的影响也被高估 | 按语料标定阈值；先用 `topK` 收敛，再靠"只依据片段作答"的指令兜底；前缀按模型约定加上即可 |
| 检索对了但模型自己加料         | 模型不保证"照抄片段"                                                              | 工具结果开头显式要求"严格只依据这些内容"；生产可加引用校验                               |
| 输出每次都不一样、难复现       | 默认采样有随机性；或 prompt 里含动态内容（时间戳、检索结果变动）                  | 演示/测试用 `temperature=0`；prompt 里若有动态内容，还得先把它固定住（见第 3 篇 §6）     |
| 检索与对话模型互相换入换出     | Ollama 默认每 GPU 驻留模型数有限                                                  | 显存够可设 `OLLAMA_MAX_LOADED_MODELS=2`（见第 1 篇第 5.3 节）                            |

## 7. 刻意简化 vs 生产做法

| 刻意简化的地方         | 生产环境的做法                                                                                                       |
| ---------------------- | -------------------------------------------------------------------------------------------------------------------- |
| 记忆存本地 JSON 文件   | 数据库 + 用户维度隔离 + 过期策略                                                                                     |
| 6 条固定笔记全量向量化 | 真实文档**分块**（chunking：切成小片段，相邻片段留一点**重叠**以免从中间截断语义）+ 向量库（pgvector/sqlite-vec 等） |
| 纯向量检索             | 混合检索（关键词 + 向量）与重排                                                                                      |
| 阈值写死 0.35          | 按 embedding 模型与语料调参/评测                                                                                     |
| 事实记忆是"死键值"     | 摘要式长期记忆（把旧对话压成摘要再存）                                                                               |
| 记忆是全局的（单用户） | 按用户/会话维度隔离（`用户:<id>:名字`）+ 数据库 + 过期策略                                                           |


### 怎么把记忆接进第 4 篇的服务（三步，不必改本篇代码）

第 4 篇的结论说"服务目前仍无记忆"，本篇又回到命令行演示——两者合体只需要三步：

1. **换工具表**：把第 4 篇的 `tools`（时间/加减乘除）整体替换成本篇的 `remember`/`recall`/`list_memory`/`search_notes`（复制 `tools` 变量即可，循环代码不用动）。
2. **给会话一个容器**：`handleChat` 里已经有 `history []Message`。无状态方案是让客户端每次带上历史；要在服务端记会话，就加 `map[string][]Message` 按 `session_id` 存——**记得加 `sync.Mutex`**，Go 的 HTTP handler 是并发的，这一点在命令行版里不用考虑。
3. **给记忆加维度**：`mem`（`memory.json`）与 `retrieve` 在服务端是**包级共享变量**，多用户会互相看到记忆。上线前至少要把键改成 `用户:<id>:名字` 这类带维度的形式（或每个用户一份文件/一层数据库）。

> 顺带一句并发：Ollama 默认并非无限并行（`OLLAMA_NUM_PARALLEL`、`OLLAMA_MAX_QUEUE`，见第 1 篇 §5.3）。服务化之后同时来了 10 个请求，超出的会排队或直接被拒——这也是"演示"与"上线"之间的一道坎。

## FAQ

| 问题                                      | 解决                                                                 |
| ----------------------------------------- | -------------------------------------------------------------------- |
| 没有 embedding 模型能跑吗                 | 能：代码自动降级为关键词检索（启动时探测 `/api/tags`），只是效果差些 |
| `ollama pull` 了 embedding 模型仍走关键词 | 探测只在**进程启动时做一次**；拉到模型后要重启进程才会切到向量检索   |
| `memory.json` 越写越大                    | 键值设计 + 定期清理；真实场景换数据库                                |
| 笔记很多时全量算余弦太慢                  | 向量库索引；先按标签/目录粗筛再精排                                  |

## 结论

1. **记忆 = 外部状态 + 工具**：Agent 自己记不住，但可以"调用工具读写外部存储"；
2. **键值记忆补枚举，资料记忆补检索**：两个工具背后是两种不同的信息组织方式；
3. **RAG 的成败在检索细节**：前缀、阈值、片段切分，任何一步糙了答案就飘；
4. **系列收官**：从环境运维 → 最小代码 → 循环 → 服务化 → 记忆，一个不碰 Python 的本地 Agent 已经具备"能跑、能调工具、能服务、能记住、能查资料"的全部骨架。
