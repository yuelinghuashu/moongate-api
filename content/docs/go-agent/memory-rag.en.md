---
title: "Memory: Multi-Session Facts and Lightweight RAG (Part 5)"
description: "Giving the agent two kinds of memory: cross-session key-value facts (remember/recall, persisted to a JSON file) and lightweight RAG over local notes (embed → cosine → topK), including the two easy-to-miss pitfalls: the embedding prefix and the similarity threshold."
date: 2026-09-08
series: go-agent
order: 5
tags:
  - Go
  - Agent
  - LLM
---

The multi-round loop of Part 3 made the agent able to get work done, but it starts every run with amnesia. This part adds two kinds of memory: **cross-session facts** (remember/recall key-value memory, persisted to JSON) and **document retrieval** (lightweight RAG doing vector search over local notes).

- Prerequisite: the multi-round loop from Part 3 already works
- Preparation: `ollama pull nomic-embed-text` (about 274MB; without this model the demo degrades to keyword retrieval automatically)
- Requirements: same as the previous part (Go 1.27+; the repository's go.mod declares go 1.27.1)
- Shape note: this part keeps the **one-shot command-line run** of Part 3 (three sessions demonstrating "session isolation" in sequence inside one process) rather than extending Part 4's SSE service; to wire memory into the service, move this tool table and loop into the server

> Environment note: this article is based on measurements of **Ollama 0.33.3 + llama3.1:8b (A770/Vulkan, 2026-09)**; Ollama iterates fast, so treat `ollama serve --help` as the source of truth for environment variables, and the [official OpenAI compatibility docs](https://docs.ollama.com/api/openai-compatibility) as the source of truth for the `/v1` compatibility fields.

## 1. There are three kinds of memory; don't conflate them

| Kind              | What it is                                            | Where it lives                 | What this part does                          |
| ----------------- | ----------------------------------------------------- | ------------------------------ | -------------------------------------------- |
| In-session memory | The context of the current conversation               | The `messages` slice in memory | Used throughout the earlier parts            |
| Long-term memory  | Facts that outlive a session (user name, preferences) | External storage               | Key-value pairs written to a local JSON file |
| Document memory   | Local documents/notes that can be searched            | A document store + an index    | topK vector retrieval (lightweight RAG)      |

The agents in the earlier parts start with amnesia every time: clear `messages` and they remember nothing. This part adds the latter two kinds with two sets of tools: `remember`/`recall`/`list_memory` (long-term facts) plus `search_notes` (document retrieval).

## 2. Long-term memory: key-value facts plus enumeration

Three details of the tool design decide whether memory is actually usable:

1. **Persistence**: `remember` writes key-value pairs into `memory.json`, so a process restart loses nothing;
2. **Namespace-style keys**: `用户:名字` ("user:name"), `用户:语言` ("user:language"), so that different subjects don't collide on the same key;
3. **A `list_memory` (enumeration) tool is mandatory**: key-value memory presupposes "knowing the key names". In a new session the model has no idea which keys were written before, and guessing keys through `recall` alone fails (in our measurements it guessed `用户:名字`, and even passed `*`); once an enumeration tool is available, listing every pair is enough for it to answer.

## 3. Lightweight RAG: embed → cosine → topK

The idea behind `search_notes` is the smallest possible RAG:

```
vectorize the question (query) and each note separately
→ compute the cosine similarity between the query and each note
→ keep the topK fragments with similarity >= 0.35
→ paste the fragments into the tool result so the model "answers from the notes"
```

Two easy-to-miss pitfalls:

- **Embedding models have an input-prefix convention**: `nomic-embed-text` expects documents prefixed with `search_document:` and queries with `search_query:`; without them, Chinese retrieval quality drops noticeably (in our measurements the same question pulled in unrelated notes);
- **The similarity threshold**: without one, even completely unrelated fragments enter the topK with low scores and the model can answer off-topic. This part uses `>= 0.35`.

## 4. Full code (demo/memory-rag/main.go)

<details>
<summary>Full source of demo/memory-rag/main.go (click to expand)</summary>

```go
// Part 5 demo: memory and lightweight RAG
//
// This part upgrades the agent loop from the earlier parts to a "with memory" shape:
//  1. cross-session long-term memory: the remember/recall tools write facts to a local
//     JSON file, so they are still there in another session (even after a process restart);
//  2. lightweight RAG: the search_notes tool retrieves from the built-in "operations
//     notes" — with nomic-embed-text it uses vector cosine similarity, and without it
//     it degrades to keyword scoring automatically;
//  3. session isolation: every session keeps its own messages history, never polluting
//     the others.
//
// Run: go run ./demo/memory-rag
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
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
type retriever interface {
	search(query string, topK int) []searchHit
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

type tool struct {
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
	// fall back to keywords
	resp, err := http.Get("http://localhost:11434/api/tags")
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

var tools = map[string]*tool{
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
			var sb strings.Builder
			for k, v := range mem.data {
				fmt.Fprintf(&sb, "%s = %s\n", k, v)
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
				fmt.Fprintf(&sb, "[片段%d 相似度%.2f] %s\n", i+1, h.score, h.text)
			}
			return strings.TrimSpace(sb.String()), nil
		},
	},
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
// Sessions and the agent loop
// ===========================================

func main() {
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
	const maxRounds = 6
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
		// Same as Parts 2 and 3: go by tool_calls being non-empty + finish_reason=="tool_calls"
		if len(msg.ToolCalls) == 0 || finish != "tool_calls" {
			fmt.Println("🤖", msg.Content)
			return
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

```bash
go run ./demo/memory-rag
```

Real output (Ollama 0.33.3 + llama3.1:8b + nomic-embed-text, with `temperature=0` for reproducibility):

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
🔧 第 1 轮模型请求 1 个工具
   - list_memory({})
     => 用户:名字 = 小明
用户:语言 = Go
🤖 根据列出的内容，我记得关于你的信息如下：

* 名字：小明
* 语言：Go

所以，我知道你是叫小明，且你使用Go语言。

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
2. **B (reading across sessions)**: a brand-new session with no shared history answers correctly after enumerating with `list_memory` — that is the difference between "memory" and "context";
3. **C (RAG)**: retrieval hits the note about "setting `OLLAMA_VULKAN=true`" (similarity 0.78), and the answer sticks strictly to the retrieved fragments, including the verification command.

> To reproduce clean results, delete any old `memory.json` first (`rm -f memory.json`), otherwise it runs with the memory left over from last time.

## 6. Pitfalls and comparisons (verified by measurement)

| Symptom                                                               | Cause                                                                      | Handling                                                                                                                               |
| --------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| Asking a small model to "extract facts by itself" writes wrong values | 8B models extract Chinese unstably (measured: it wrote `小明` as `小明积`) | Use an explicit key/value prompt for demos; in production let a stronger model extract, or validate before writing                     |
| In a new session `recall` cannot guess the key                        | Key-value memory requires "knowing the key names"                          | Provide the `list_memory` enumeration tool (stable in our measurements)                                                                |
| Chinese retrieval pulls in unrelated fragments                        | No embedding prefixes / no threshold                                       | `search_document:`/`search_query:` prefixes + a `>=0.35` threshold                                                                     |
| Retrieval is right but the model adds its own material                | The model is not guaranteed to "copy the fragments"                        | State explicitly at the start of the tool result that answers must rely strictly on that content; production can add citation checking |
| Output differs every time and is hard to reproduce                    | Default sampling is random                                                 | Use `temperature=0` for demos/tests, and raise it again for chat                                                                       |
| The retrieval and chat models keep swapping each other out            | Ollama keeps only a limited number of models resident per GPU by default   | If VRAM allows, set `OLLAMA_MAX_LOADED_MODELS=2` (see Part 1, Section 5.3)                                                             |

## 7. Deliberately simplified vs. production practice

| Deliberately simplified                 | What production does                                                                      |
| --------------------------------------- | ----------------------------------------------------------------------------------------- |
| Memory stored in a local JSON file      | A database plus per-user isolation and expiry policies                                    |
| Six fixed notes vectorized wholesale    | Chunking and overlapping real documents plus a vector store (pgvector/sqlite-vec, …)      |
| Pure vector retrieval                   | Hybrid retrieval (keywords + vectors) with reranking                                      |
| A hard-coded 0.35 threshold             | Tune and evaluate per embedding model and corpus                                          |
| Fact memory is a "dead key-value store" | Summary-style long-term memory (compress old conversations into a summary and store that) |

## FAQ

| Question                                                 | Fix                                                                                                                   |
| -------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| Can it run without an embedding model?                   | Yes: the code degrades to keyword retrieval automatically (it probes `/api/tags` at startup), just with worse results |
| `memory.json` keeps growing                              | Design the keys properly and clean up periodically; use a database in real scenarios                                  |
| Computing cosine over everything is slow with many notes | Index with a vector store; coarse-filter by tag or directory before ranking precisely                                 |
| Why is `list_memory` needed?                             | The keys of key-value memory are invisible to the model, and enumeration is the precondition for "remembering"        |

## Conclusion

1. **Memory = external state + tools**: an agent cannot remember on its own, but it can "call a tool to read and write external storage";
2. **Key-value memory needs enumeration, document memory needs retrieval**: the two tools rest on two different ways of organizing information;
3. **RAG succeeds or fails on retrieval details**: prefixes, thresholds and fragment splitting — make any one of them rough and the answer drifts;
4. **The series finale**: from environment and operations → minimal code → the loop → service-ization → memory, a local agent that never touches Python now has the whole skeleton of "it runs, it calls tools, it serves, it remembers, and it looks things up".
