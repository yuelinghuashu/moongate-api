---
title: "Memory: Multi-Session Facts and Lightweight RAG (Part 5)"
description: "Giving the agent two kinds of memory: cross-session key-value facts (remember/recall, persisted to a JSON file) and lightweight RAG over local notes (embed → cosine → topK), with measurements showing why a fixed similarity threshold and the embedding prefix are the two most over-trusted details."
date: 2026-09-08
series: go-agent
order: 5
tags:
  - Go
  - Agent
  - LLM
---

The multi-round loop of Part 3 made the agent able to get work done, but it starts every run with amnesia. This part adds two kinds of memory: **cross-session facts** (remember/recall key-value memory, persisted to JSON) and **document retrieval** (lightweight RAG doing vector search over local notes).

- Prerequisite: the environment checklist at the end of Part 1 is all green, and the multi-round loop from Part 3 already works
- Preparation: `ollama pull nomic-embed-text` (about 274MB; without this model the example degrades to keyword retrieval automatically)
- Shape note: this part keeps the **one-shot command-line run** of Part 3 (three sessions demonstrating "session isolation" in sequence inside one process; `go run main.go recall` starts a separate process to prove cross-process persistence) rather than extending Part 4's SSE service; to wire memory into the service, move this tool table and loop into the server

**What changed since Part 3 (read this table before the code):**

| Change                                                                                  | Where                                  | Why                                                                                            |
| --------------------------------------------------------------------------------------- | -------------------------------------- | ---------------------------------------------------------------------------------------------- |
| The tools are swapped wholesale: `remember` / `recall` / `list_memory` / `search_notes` | `tools`                                | One set of tools per memory kind: key-value facts plus document retrieval (§1, §2)             |
| New `memoryStore` reading and writing a local `memory.json`                             | `loadMemory` / `set`                   | Cross-session, cross-process facts (surviving process exit)                                    |
| New `retriever` interface with two implementations                                      | `vectorRetriever` / `keywordRetriever` | Vector search when an embedding model exists, automatic keyword fallback when it does not      |
| New `tokenize` / `top` / `cosine` / `embedTexts`                                        | retrieval parts                        | The whole lightweight RAG implementation, about 60 lines (§3)                                  |
| `runAgent(userPrompt)` → `runAgent(messages)`, plus three session functions             | `main` / `runSessionA-C`               | Demonstrate "session isolation" in one run; `go run main.go recall` demonstrates cross-process |
| `temperature=0` pinned                                                                  | `chat()`                               | Keeps the measured output stable and reproducible                                              |
| `trimHistory` and the `usage` counters removed                                          | —                                      | This part focuses on memory and retrieval; history growth is still Part 3 §4                   |

> Unchanged from Part 3 and worth skimming past: the `Message`/`ToolCall`/`Tool` structs, the loop skeleton, the `role="tool"` feedback and "`tool_calls` non-empty" criterion, and the `toolEntry` registry shape.

## 1. There are three kinds of memory; don't conflate them

| Kind              | What it is                                            | Where it lives                 | What this part does                          |
| ----------------- | ----------------------------------------------------- | ------------------------------ | -------------------------------------------- |
| In-session memory | The context of the current conversation               | The `messages` slice in memory | Used throughout the earlier parts            |
| Long-term memory  | Facts that outlive a session (user name, preferences) | External storage               | Key-value pairs written to a local JSON file |
| Document memory   | Local documents/notes that can be searched            | A document store + an index    | topK vector retrieval (lightweight RAG)      |

This part adds the latter two kinds with two sets of tools (the agents in the earlier parts start with amnesia every time — clear `messages` and they remember nothing): `remember`/`recall`/`list_memory` (long-term facts) plus `search_notes` (document retrieval). (The "session isolation" this part refers to means: each session keeps its own `messages` slice, so A's conversation history never appears in B — the only thing shared across sessions is the `memory.json` on disk.)

## 2. Long-term memory: key-value facts plus enumeration

Three details of the tool design decide whether memory is actually usable:

1. **Persistence**: `remember` writes key-value pairs into `memory.json`, so a process restart loses nothing;
2. **Namespace-style keys**: `用户:名字` ("user:name"), `用户:语言` ("user:language"), so that different subjects don't collide on the same key;
3. **A `list_memory` (enumeration) tool is mandatory**: key-value memory presupposes "knowing the key names". In a new session the model has no idea which keys were written before, and guessing keys through `recall` alone fails (in our measurements it guessed `用户:名字`, and even passed `*`); once an enumeration tool is available, listing every pair is enough for it to answer.

## 3. Lightweight RAG: embed → cosine → topK

In plain words first: **embedding turns a piece of text into a list of numbers (a vector)** — the closer two texts are in meaning, the closer their vectors point, so "how alike are these two?" becomes something you can compute directly with cosine similarity.

The idea behind `search_notes` is the smallest possible **RAG** (Retrieval-Augmented Generation: retrieve the material first, then have the model answer *from that material* rather than from its own memory):

```
vectorize the question (query) and each note separately
→ compute the cosine similarity between the query and each note
→ keep the topK fragments with similarity >= 0.35
→ paste the fragments into the tool result so the model "answers from the notes"
```

What happens inside one `search_notes` call:

```
The model supplies a query
      │
      ▼
Wrap it for the embedding model: the query gets "search_query: ", every note gets "search_document: "
      │   (the prefixes are nomic-embed-text's own convention — follow them, but do not expect them alone to save you)
      ▼
POST /api/embed (every text in one request) ──▶ [vectors…]
      ▼
Cosine similarity per note ──▶ coarse filter at 0.35 ──▶ sort and take topK=3
      ▼
Build the tool result: a fixed "answer only from these fragments, invent nothing" header + [片段N 相似度X.XX] text
      ▼
Feed it back as role="tool" ──▶ the model answers from the fragments (measured output in §5, session C)
```

> Fallback path: without `nomic-embed-text` the same interface scores by keyword matches (count of matched words), with an identical flow — only the score label changes from "相似度" to "命中词数". The two scores are not comparable, so they must not share one label.

Two things here **go against the usual intuition**, so here are the measurements from this machine first (Ollama 0.33.3 + `nomic-embed-text`, over exactly the 6 notes above; the numbers are cosine similarities):

| Query                                   | With prefixes (`search_document:` / `search_query:`) | Without prefixes |
| --------------------------------------- | ---------------------------------------------------- | ---------------- |
| `Intel Arc Ollama GPU 加速` (relevant)  | 0.560 ~ 0.778                                        | 0.592 ~ 0.771    |
| `怎么做红烧肉？` (completely unrelated) | 0.500 ~ 0.598 (all 6 clear 0.35)                     | 0.465 ~ 0.530    |

- **The similarity threshold has to be calibrated against your own corpus; copying a 0.35 usually makes it a no-op**: with short, topically similar text the cosine scores are squeezed into a narrow high band (0.5–0.8 here), so **even a completely unrelated question scores above 0.5** and all 6 notes pass — what actually narrows things down to 3 is `topK`. A threshold is not a bad design (scores only spread out once your corpus is more diverse or your documents are long), but it must be tuned for that corpus; this part keeps `>= 0.35` purely to demonstrate the shape of the code, so **don't copy that number**.
- **The embedding prefix is the model's own convention: follow it, but don't expect it to solve retrieval on its own**: `nomic-embed-text` expects documents prefixed with `search_document:` and queries with `search_query:`; measured here, with or without them the difference is small (top score 0.778 vs 0.771, essentially the same ranking), and it only becomes visible on a more diverse corpus. Compared with the prefix, **threshold calibration, chunking and topK usually matter more**.

## 4. Full code

<details>
<summary>Full source of main.go (click to expand)</summary>

```go
// Part 5 demo: memory and lightweight RAG.
// Differences from Part 3 are in the article's "what changed" table; the core is memoryStore
// (JSON on disk), the retriever (vector search with a keyword fallback) and the four new tools.
//
// Run: go run main.go        — runs sessions A / B / C in order
//
//	go run main.go recall   — runs session B alone (proves cross-process recall, see Section 5)
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
// Structures matching /v1/chat/completions
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

// FunctionCall is explained in Part 2 §3 (tool_calls[].function)
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
// Memory store (cross-session, persisted to a file)
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
// Note store (standing in for "local material") and the retriever
// ===========================================

var notes = []string{
	"Ollama 默认只识别 NVIDIA(CUDA) 与 AMD(ROCm) 显卡；Intel Arc 需要在 systemd override 里设置 OLLAMA_VULKAN=true 并重启服务，才会走 Vulkan 后端。",
	"验证 Vulkan 是否生效：journalctl -u ollama --no-pager | grep 'inference compute'，看到 library=Vulkan 与 Arc A770 的描述即为成功。",
	"OLLAMA_LOAD_TIMEOUT 默认 5 分钟，控制模型加载停滞多久后放弃；驱动慢或首次编译着色器时可调大，例如 10m。",
	"OLLAMA_KEEP_ALIVE 默认 5 分钟，控制模型空闲多久后卸载；频繁调用可调大到 10m 以减少重复加载。",
	"Vulkan 后端在部分 Linux 内核 + Mesa 驱动下有显存记账失步、空闲显存被换出的已知问题，长时间运行要用 intel_gpu_top 或 journalctl 监控。",
	"llama3.1:8b 在 Intel Arc A770 上实测 33/33 层全量 offload，模型占显存约 4.4GB，生成速度约 41 tokens/s。",
}

// retriever finds the most relevant note fragments for a query.
// metric() names what score means: cosine similarity for the vector path, matched-word
// count for the keyword fallback. The two are different units, so they must not share
// the "similarity" label when printed.
type retriever interface {
	search(query string, topK int) []searchHit
	metric() string
}

type searchHit struct {
	text  string
	score float64
}

// vectorRetriever uses the vectors from /api/embed for cosine similarity.
// nomic-embed-text only performs well with the standard prefixes: documents get
// "search_document:" and queries get "search_query:" (the embedding model's own
// convention).
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
	if len(vecs) != len(texts) { // avoid an out-of-range panic when the count does not match the input
		fmt.Println("   ⚠️ 向量检索返回数量不符，本次降级为空结果")
		return nil
	}
	q := vecs[0]
	var hits []searchHit
	for i, n := range notes {
		s := cosine(q, vecs[i+1])
		if s >= 0.35 { // too low a threshold drags unrelated fragments in as well
			hits = append(hits, searchHit{n, s})
		}
	}
	return top(hits, topK)
}

// keywordRetriever is the fallback when there is no embedding model: score by the number of matched words.
type keywordRetriever struct{}

func (keywordRetriever) metric() string { return "命中词数" }

func (keywordRetriever) search(query string, topK int) []searchHit {
	words := tokenize(query)
	var hits []searchHit
	for _, n := range notes {
		score := 0.0
		lower := strings.ToLower(n) // case-insensitive for English (e.g. intel ↔ Intel)
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
	// Chinese is split into 2-character sliding windows plus English split on
	// whitespace, which is enough for a demo
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
	if len(a) != len(b) { // different dimensions: return 0 instead of panicking on an out-of-range index
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
// Tool registration
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
	// Probe /api/tags for an embedding model; use vectors if present, otherwise
	// fall back to keywords. This HTTP call sits on the startup path, so it needs a
	// timeout: a stuck Ollama must not stall startup (Part 2's "give every upstream
	// request an explicit timeout budget" applies here too).
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
			// Sort the output: Go iterates maps in random order, so without sorting the
			// key order changes from run to run (same reason as toolDefs: stable output
			// is what makes a run reproducible and comparable)
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

// toolDefs is identical to Part 3's (sorted by name for a stable request body; see Part 3 §2).
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
// Sessions and the agent loop
// ===========================================

func main() {
	// Cross-process persistence demo: run `go run main.go` once to write the memory
	// (then the process exits), then `go run main.go recall` to run session B alone —
	// nothing is shared but memory.json.
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
	const maxRounds = 8 // fuse, same as Parts 3/4: stop the model from looping forever
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
		// Same as Parts 2 and 3: go by tool_calls being non-empty (finish_reason is
		// only a cross-check — some upstreams still report "stop" while calling tools)
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
				result = "(工具无输出)" // as in Part 3: a role="tool" message must carry content (Part 2 §3)
			}
			fmt.Printf("   - %s(%s)\n     => %s\n", tc.Function.Name, tc.Function.Arguments, result)
			messages = append(messages, Message{Role: "tool", ToolCallID: tc.ID, Content: result})
		}
	}
}

// ===========================================
// HTTP calls (chat and embed)
// ===========================================

var httpClient = &http.Client{Timeout: 5 * time.Minute} // request timeout: a stuck upstream cannot hang us forever

func chat(messages []Message) (ChatResponse, error) {
	reqBody := ChatRequest{
		Model:       "llama3.1:8b",
		Messages:    messages,
		Tools:       toolDefs(),
		Stream:      false,
		Temperature: 0, // greedy decoding keeps demo output stable; a chat scenario can go back to 0.7
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

## 5. Run results (measured on this machine)

> ⚠️ **Clear the old memory before running**: `rm -f memory.json` — otherwise it starts with the facts written by the previous run and session A looks like it did nothing.

```bash
rm -f memory.json   # clean reproduction: do not carry history over
go run main.go
```

Real output (Ollama 0.33.3 + llama3.1:8b + nomic-embed-text, measured 2026-09-19; `temperature=0`, and this part's prompt carries no dynamic content such as a timestamp, so it reproduces word for word):
> The console output and prompts in this transcript are Chinese exactly as captured; substituting English prompts changes the wording, not the structure or the conclusions.


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

Each of the three sessions demonstrates one thing:

1. **A (writing)**: two `remember` calls write facts into `memory.json`;
2. **B (reading across sessions)**: a brand-new session with no shared history enumerates the keys with `list_memory`, fetches a value with `recall`, and answers correctly — that is the difference between "memory" and "context". To verify *cross-process* rather than merely cross-session, run two commands (the second is a **separate process**, so its in-process `messages` is guaranteed empty):

   ```bash
   go run main.go          # session A writes memory.json, then the process exits
   go run main.go recall   # a new process runs session B only: it still answers "小明 / Go"
   ```

   The measured output of that second command (same `memory.json`, new process, no conversation history at all):

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

   > This is "memory = external state + tools" in action: the model itself remembers nothing — it simply **calls the tools again**.

3. **C (RAG)**: retrieval hits the note about "setting `OLLAMA_VULKAN=true`" (similarity 0.78), and the answer sticks strictly to the retrieved fragments, including the verification command.



## 6. Pitfalls and comparisons (verified by measurement)

| Symptom                                                               | Cause                                                                                                                                                              | Handling                                                                                                                                                                                    |
| --------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Asking a small model to "extract facts by itself" writes wrong values | 8B models extract Chinese unstably (measured: it wrote `小明` as `小明积`)                                                                                         | Use an explicit key/value prompt for demos; in production let a stronger model extract, or validate before writing                                                                          |
| In a new session `recall` cannot guess the key                        | Key-value memory requires "knowing the key names", and those keys are invisible to the model                                                                       | Provide the `list_memory` enumeration tool — enumeration is the precondition for "remembering" (stable in our measurements)                                                                 |
| Retrieval is not accurate / pulls in unrelated fragments              | For short, topically similar text the cosine scores sit in a narrow 0.5–0.8 band, so a fixed threshold filters almost nothing; the prefix effect is overstated too | Calibrate the threshold against your own corpus; let `topK` do the narrowing, back it with the "answer only from these fragments" instruction, and add the model's prefixes as a convention |
| Retrieval is right but the model adds its own material                | The model is not guaranteed to "copy the fragments"                                                                                                                | State explicitly at the start of the tool result that answers must rely strictly on that content; production can add citation checking                                                      |
| Output differs every time and is hard to reproduce                    | Default sampling is random                                                                                                                                         | Use `temperature=0` for demos/tests, and raise it again for chat                                                                                                                            |
| The retrieval and chat models keep swapping each other out            | Ollama keeps only a limited number of models resident per GPU by default                                                                                           | If VRAM allows, set `OLLAMA_MAX_LOADED_MODELS=2` (see Part 1, Section 5.3)                                                                                                                  |

## 7. Deliberately simplified vs. production practice

| Deliberately simplified                 | What production does                                                                                                                                     |
| --------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Memory stored in a local JSON file      | A database plus per-user isolation and expiry policies                                                                                                   |
| Six fixed notes vectorized wholesale    | Real **chunking** (cut documents into small pieces, overlapping neighbours so no meaning is sliced in half) plus a vector store (pgvector/sqlite-vec, …) |
| Pure vector retrieval                   | Hybrid retrieval (keywords + vectors) with reranking                                                                                                     |
| A hard-coded 0.35 threshold             | Tune and evaluate per embedding model and corpus                                                                                                         |
| Fact memory is a "dead key-value store" | Summary-style long-term memory (compress old conversations into a summary and store that)                                                                |
| Memory is global (single user)          | Isolate by user/session (`用户:<id>:名字`-style) plus a database and expiry policies                                                                     |


### Wiring memory into Part 4's service (three steps, no changes to this part's code)

Part 4's conclusion says "the service still has no memory", and this part goes back to a command-line demo — merging the two takes three steps:

1. **Swap the tool table**: replace Part 4's `tools` (time and arithmetic) with this part's `remember`/`recall`/`list_memory`/`search_notes` (copy the `tools` variable; the loop code does not change).
2. **Give a session somewhere to live**: `handleChat` already has `history []Message`. The stateless option is to let the client send the history every time; to keep sessions server-side, add a `map[string][]Message` keyed by `session_id` — and **guard it with a `sync.Mutex`**, because Go's HTTP handlers run concurrently (something the command-line version never has to think about).
3. **Give memory a dimension**: `mem` (`memory.json`) and `retrieve` are **package-level shared state** in a server, so several users would see each other's facts. Before shipping, key entries by user (`用户:<id>:名字`-style) or give each user their own file or database layer.

> On concurrency: Ollama does not serve unlimited requests in parallel either (`OLLAMA_NUM_PARALLEL`, `OLLAMA_MAX_QUEUE`; see Part 1 §5.3). Once ten requests arrive at your service at once, the extras queue or get rejected — another gap between "demo" and "shipped".

## FAQ

| Question                                                 | Fix                                                                                                                   |
| -------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| Can it run without an embedding model?                   | Yes: the code degrades to keyword retrieval automatically (it probes `/api/tags` at startup), just with worse results |
| I pulled an embedding model but it still uses keywords   | The probe runs **once, at process start**; restart the process after pulling to switch to vector retrieval            |
| `memory.json` keeps growing                              | Design the keys properly and clean up periodically; use a database in real scenarios                                  |
| Computing cosine over everything is slow with many notes | Index with a vector store; coarse-filter by tag or directory before ranking precisely                                 |

## Conclusion

1. **Memory = external state + tools**: an agent cannot remember on its own, but it can "call a tool to read and write external storage";
2. **Key-value memory needs enumeration, document memory needs retrieval**: the two tools rest on two different ways of organizing information;
3. **RAG succeeds or fails on retrieval details**: prefixes, thresholds and fragment splitting — make any one of them rough and the answer drifts;
4. **The series finale**: from environment and operations → minimal code → the loop → service-ization → memory, a local agent that never touches Python now has the whole skeleton of "it runs, it calls tools, it serves, it remembers, and it looks things up".
