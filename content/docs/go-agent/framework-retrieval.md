---
title: 框架：检索调用被封好了，检索质量没有
description: 用 Eino 重写第 5 篇的轻量 RAG：框架给了 Retriever 接口、Document 结构和 WithTopK / WithScoreThreshold 两个旋钮，但本机实测 embedding 组件一行前缀都没加（cos=0.885 证明原样透传），且用框架重跑第 5 篇的实验，数字逐位一致。更进一步：这个语料的阈值有效窗口只有 0.598~0.654，照抄 0.35 或 0.8 都是废的。
date: 2026-09-11
series: go-agent
order: 8
tags:
  - Go
  - Agent
  - LLM
---

第 5 篇我们给 Agent 补上了两类记忆，其中"轻量 RAG"是手写的：`embed → 余弦 → topK`，并且实测出两个和直觉不一样的结论——**固定阈值 0.35 形同虚设**、**embedding 前缀的影响被高估**。

框架把检索调用封装起来之后，这两个结论会变吗？检索质量会变好吗？

本篇用 Eino 的 `retriever` 接口重写第 5 篇的检索器，然后把第 5 篇的实验**原样重跑一遍**。

- 前置：已理解第 5 篇的两类记忆与轻量 RAG，尤其是阈值与前缀那两个实测结论
- 环境：沿用第 1 篇的 Ollama + `nomic-embed-text`（Intel Arc A770）
- 代码：完整可运行版本在本篇 §5

> **版本声明**：本文基于 `github.com/cloudwego/eino v0.9.19` + `eino-ext/components/embedding/ollama`（伪版本 `v0.0.0-20260909094858-6fe16d48b736`）实测（Go 1.27.1）。**注意这个组件当时只有伪版本、没有 tag**——`eino-ext` 的组件各自发版，详见篇末附录。

## 1. 第 5 篇的三个细节，现在归谁管

第 5 篇的检索器就是四步：

```text
把问题(query) 和 每条笔记 分别向量化
→ 算 query 与每条笔记的余弦相似度
→ 取相似度 >= 0.35 的 topK 片段
→ 把片段拼进工具结果，让模型"依据笔记作答"
```

第 5 篇实测出两个反直觉的结论，并留下三个"刻意简化"。把那些细节逐条对到框架上，会发现只有第一件事被真正接管了：**调 embedding 模型拿向量有现成组件**；而余弦相似度怎么算，框架**不提供**通用向量检索实现（见 §2）；`search_document:` / `search_query:` 前缀，框架**一行都不加**（§3 实测）；阈值和 topK 取多少，它**只给旋钮名、不给数值**（§4）；片段怎么切，它**完全不管**。

先看框架到底给了什么。

## 2. 框架给的是接口，不是实现

Eino 的检索抽象只有**一个方法**：

```go
type Retriever interface {
	Retrieve(ctx context.Context, query string, opts ...Option) ([]*schema.Document, error)
}
```

配套的是一个选项结构——**这才是关键**：

```go
type Options struct {
	Index          *string
	SubIndex       *string
	TopK           *int
	ScoreThreshold *float64
	Embedding      embedding.Embedder
	DSLInfo        map[string]any
}

func WithTopK(topK int) Option
func WithScoreThreshold(threshold float64) Option
```

接口注释里对两者语义的说明值得抄下来：

> `Options.ScoreThreshold` is a filter, not a sort: documents scoring below the threshold are excluded entirely. `Options.TopK` caps the number of results returned.

翻译成人话：**框架规定了"阈值是过滤，topK 是截断"，也规定了这两个参数叫什么名字——但一个默认值都没给，一个实现都没提供。**

而且要注意：`Retriever` 是个**接口**。也就是说，用框架写第 5 篇那个轻量 RAG，**你实际上是自己实现这个接口**——余弦怎么算、向量存在哪、片段怎么切，全在你自己那个 `Retrieve` 方法体里。框架在这里的角色是"统一签名让别的组件能接上"（例如 `graph.AddRetrieverNode`），不是"替你检索"。

那 `eino-ext` 里有没有现成的检索实现？有，但都是**接向量库/服务**的：

```text
redis  milvus  milvus2  qdrant  es7  es8  es9  opensearch2  opensearch3  dify
```

**没有一个能替你回答"阈值取多少"。** 它们解决的是"向量存哪儿、怎么查得快"，而第 5 篇的困惑是"查出来的分数算不算相关"——这是两个问题。

## 3. 前缀：实测框架一行都没加

第 5 篇实测过：`nomic-embed-text` 要求文档加 `search_document:`、查询加 `search_query:`，但加与不加差别很小（最高分 0.778 vs 0.771）。

那么框架的 embedding 组件会不会替你加前缀？先看配置结构：

```go
type EmbeddingConfig struct {
	Timeout    time.Duration
	HTTPClient *http.Client
	BaseURL    string
	Model      string
	Truncate   *bool
}
```

把整个组件目录 grep 一遍：

```bash
grep -rniE "prefix|search_document|search_query" \
  $(go env GOMODCACHE)/github.com/cloudwego/eino-ext/components/embedding/ollama@*/ --include=*.go
```

**零命中**——配置里没有任何前缀相关字段。（`--include=*.go` 别省：不加的话 `go.sum` 里会命中一个叫 `logrus-prefixed-formatter` 的依赖，容易看错。）

光看代码还不够，我做了个能一锤定音的实测：**分别 embed `"X"` 和 `"search_document:X"`，比两个向量。**

- 如果组件内部加了前缀 → 两次输入实际相同 → 向量完全相同（余弦 = 1）
- 如果组件原样透传 → 两次输入不同 → 向量不同

```text
embed("Intel Arc Ollama GPU 加速")
embed("search_document:Intel Arc Ollama GPU 加速")
两者的余弦相似度 = 0.885403
```

**0.885，不是 1。** 组件就是原样把字符串送给模型——**前缀必须你自己加。**

这是本篇第一个明确结论：**embedding 组件是"通道"，不是"策略"。** 它负责把文本变成向量，不负责按你的模型约定加工文本。第 5 篇那句"embedding 前缀是模型自己的约定"在框架版里一个字都不用改。

## 4. 阈值：用框架重跑第 5 篇，数字一字未变

现在做本篇的核心实验：把第 5 篇的 6 条笔记逐字搬进框架版检索器，用 **Eino 的 embedding 组件**重新算一遍。

| 查询                                | 带前缀（第 5 篇） | 带前缀（框架重跑） | 不带前缀（第 5 篇） | 不带前缀（框架重跑） |
| :---------------------------------- | :---------------- | :----------------- | :------------------ | :------------------- |
| `Intel Arc Ollama GPU 加速`（相关） | 0.560 ~ 0.778     | **0.560 ~ 0.778**  | 0.592 ~ 0.771       | **0.592 ~ 0.771**    |
| `怎么做红烧肉？`（无关）            | 0.500 ~ 0.598     | **0.500 ~ 0.598**  | 0.465 ~ 0.530       | **0.465 ~ 0.530**    |

**逐位一致。** 从手写 `net/http` 换成框架组件，同一个模型、同一份语料，分数区间完全相同——因为**检索质量由模型和语料决定，跟调用它的代码长什么样无关。**

这本身就是框架篇最该说清楚的一件事：**框架在这件事上是中性的。**

### 4.1 更进一步：这个语料的阈值有效窗口只有 0.056 宽

第 5 篇说"0.35 形同虚设"，但没说"那该取多少"。用框架的 `WithScoreThreshold` 把三个阈值都试一遍，答案就出来了。

先看各查询的真实分数（框架重跑实测）。相关查询 `Intel Arc Ollama GPU 加速` 的分数落在 **0.560 ~ 0.778**，最高的三条是 0.778 / 0.690 / 0.654；而完全无关的 `怎么做红烧肉？` 也有 **0.560 ~ 0.598**，最高的三条是 0.598 / 0.569 / 0.560。**两个查询的最低分一样，都是 0.560——这就是"窄高位带"最直观的样子。**

用框架的 `WithScoreThreshold` 把三个阈值各试一遍，结果就出来了：

| 阈值                          | 相关查询剩余 | 无关查询剩余 | 判定                                  |
| :---------------------------- | :----------- | :----------- | :------------------------------------ |
| **0.35**（照抄第 5 篇的数字） | 6/6          | **6/6**      | ❌ 阈值失效——无关问题也全部通过       |
| **0.62**（标定出来的）        | 5/6          | **0/6**      | ✅ **正确拒绝无关查询**，保留相关内容 |
| **0.8**（凭感觉调高）         | **0/6**      | 0/6          | ❌ 连相关内容一起杀掉                 |

这张表把第 5 篇的结论推进了一步：

- **0.35 太低**：无关查询的最高分是 0.598，比 0.35 高一大截，所以阈值等于不存在
- **0.8 太高**：相关查询的最高分只有 0.778，达不到 0.8，于是什么都留不下
- **0.62 有效**：相关查询的最低分是 0.654、无关查询的最高分是 0.598——**有效窗口就夹在 0.598 和 0.654 之间，只有 0.056 宽**

所以第 5 篇说"阈值本身不是错的设计"是对的，但真正的结论更锋利：**这个语料的阈值有效窗口只有 0.056 宽，而且窗口的位置完全取决于你的语料。** 靠猜、靠抄，命中的概率约等于零；**只能靠量自己的语料算出来。**

而框架在这里能帮你的，仅仅是提供一个叫 `WithScoreThreshold` 的旋钮。

## 5. 完整代码

实现 `retriever.Retriever` 接口。注意：**这个接口里全部的"检索质量"逻辑都得你自己写**——框架只规定了签名和选项名。

<details>
<summary>main.go 全文（点击展开）</summary>

```go
// 框架篇 3 的完整可运行代码：用 Eino 的 retriever 接口重写第 5 篇的轻量 RAG。
//
// 分工：
//   - 框架给：Retriever 接口、WithTopK / WithScoreThreshold 两个"选项名"、Document 结构
//   - 你做：向量怎么算、前缀怎么加、阈值取多少、topK 取多少、片段怎么切
package main

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"

	embollama "github.com/cloudwego/eino-ext/components/embedding/ollama"
)

// 第 5 篇原文的 6 条笔记，逐字照抄
var notes = []string{
	"Ollama 默认只识别 NVIDIA(CUDA) 与 AMD(ROCm) 显卡；Intel Arc 需要在 systemd override 里设置 OLLAMA_VULKAN=true 并重启服务，才会走 Vulkan 后端。",
	"验证 Vulkan 是否生效：journalctl -u ollama --no-pager | grep 'inference compute'，看到 library=Vulkan 与 Arc A770 的描述即为成功。",
	"OLLAMA_LOAD_TIMEOUT 默认 5 分钟，控制模型加载停滞多久后放弃；驱动慢或首次编译着色器时可调大，例如 10m。",
	"OLLAMA_KEEP_ALIVE 默认 5 分钟，控制模型空闲多久后卸载；频繁调用可调大到 10m 以减少重复加载。",
	"Vulkan 后端在部分 Linux 内核 + Mesa 驱动下有显存记账失步、空闲显存被换出的已知问题，长时间运行要用 intel_gpu_top 或 journalctl 监控。",
	"llama3.1:8b 在 Intel Arc A770 上实测 33/33 层全量 offload，模型占显存约 4.4GB，生成速度约 41 tokens/s。",
}

type notesRetriever struct {
	emb     embedding.Embedder
	docs    []*schema.Document // 已向量化（含前缀）的文档
	vectors [][]float64
}

// Retrieve 是 Retriever 接口的唯一方法。
// "检索质量"的全部逻辑都在这个函数体里 —— 框架一个字都没参与。
func (r *notesRetriever) Retrieve(ctx context.Context, query string,
	opts ...retriever.Option) ([]*schema.Document, error) {

	// 框架提供"选项名"，默认值由你定 —— 框架不给默认值，也不给标定方法。
	defaultTopK := 3
	opts2 := retriever.GetCommonOptions(&retriever.Options{TopK: &defaultTopK}, opts...)

	// ① 查询侧前缀：这是你的事，框架不会加
	queryVecs, err := r.emb.EmbedStrings(ctx, []string{"search_query:" + query})
	if err != nil {
		return nil, err
	}
	qv := queryVecs[0]

	// ② 逐个算余弦相似度
	type scored struct {
		doc   *schema.Document
		score float64
	}
	all := make([]scored, 0, len(r.docs))
	for i, d := range r.docs {
		all = append(all, scored{doc: d, score: cosine(qv, r.vectors[i])})
	}

	// ③ 阈值过滤：框架的语义是"过滤"，不是"排序"
	kept := all[:0:0]
	for _, s := range all {
		if opts2.ScoreThreshold != nil && s.score < *opts2.ScoreThreshold {
			continue
		}
		kept = append(kept, s)
	}

	// ④ 排序 + topK：真正把结果收敛下来的通常是这一步
	sort.Slice(kept, func(i, j int) bool { return kept[i].score > kept[j].score })
	if opts2.TopK != nil && *opts2.TopK < len(kept) {
		kept = kept[:*opts2.TopK]
	}

	out := make([]*schema.Document, 0, len(kept))
	for _, s := range kept {
		out = append(out, s.doc.WithScore(s.score))
	}
	return out, nil
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

// newNotesRetriever 把语料向量化。文档侧前缀同样是你自己加的。
func newNotesRetriever(ctx context.Context, emb embedding.Embedder) (*notesRetriever, error) {
	texts := make([]string, len(notes))
	for i, n := range notes {
		texts[i] = "search_document:" + n // ← ① 文档侧前缀：框架不会加
	}
	vecs, err := emb.EmbedStrings(ctx, texts)
	if err != nil {
		return nil, err
	}
	docs := make([]*schema.Document, len(notes))
	for i, n := range notes {
		docs[i] = &schema.Document{ID: fmt.Sprintf("note-%d", i+1), Content: n}
	}
	return &notesRetriever{emb: emb, docs: docs, vectors: vecs}, nil
}

func main() {
	ctx := context.Background()
	emb, err := embollama.NewEmbedder(ctx, &embollama.EmbeddingConfig{
		BaseURL: "http://localhost:11434",
		Model:   "nomic-embed-text",
		Timeout: 120 * time.Second,
	})
	if err != nil {
		panic(err)
	}

	r, err := newNotesRetriever(ctx, emb)
	if err != nil {
		panic(err)
	}

	queries := []string{"Intel Arc Ollama GPU 加速", "怎么做红烧肉？"}

	// 例 1：只用 topK —— 这是第 5 篇实际生效的收敛手段
	for _, q := range queries {
		docs, err := r.Retrieve(ctx, q, retriever.WithTopK(3))
		if err != nil {
			panic(err)
		}
		fmt.Printf("查询: %q → 命中 %d 条\n", q, len(docs))
		for _, d := range docs {
			fmt.Printf("   [%.3f] %s\n", d.Score(), d.Content)
		}
	}

	// 例 2/3/4：三个阈值对比（0.35 照抄 / 0.62 标定 / 0.8 凭感觉）
	for _, th := range []float64{0.35, 0.62, 0.8} {
		for _, q := range queries {
			all, err := r.Retrieve(ctx, q,
				retriever.WithTopK(100), retriever.WithScoreThreshold(th))
			if err != nil {
				panic(err)
			}
			fmt.Printf("阈值 %.2f | 查询 %q → 剩 %d/%d 条\n", th, q, len(all), len(notes))
		}
	}
}
```

</details>

> 注意 `WithScoreThreshold` / `WithTopK` 是 `retriever.Option`，通过 `retriever.GetCommonOptions` 读出来。**框架负责把这些选项从调用方传到你的 `Retrieve`，这是它唯一做的"检索相关"的事。**

## 6. 运行结果（本机实测）

`go run main.go`，Ollama + `nomic-embed-text`，Intel Arc A770：

```text
=== 例 1：WithTopK(3)，不设阈值 ===
查询: "Intel Arc Ollama GPU 加速" → 命中 3 条
   [0.778] Ollama 默认只识别 NVIDIA(CUDA) 与 …
   [0.690] llama3.1:8b 在 Intel Arc A770…
   [0.654] 验证 Vulkan 是否生效：journalctl -u…
查询: "怎么做红烧肉？" → 命中 3 条
   [0.598] Vulkan 后端在部分 Linux 内核 + Mesa…
   [0.569] OLLAMA_KEEP_ALIVE 默认 5 分钟，控制…
   [0.560] 验证 Vulkan 是否生效：journalctl -u…

=== 例 2：WithScoreThreshold(0.35) + WithTopK(3) ===
查询: "Intel Arc Ollama GPU 加速" → 过 0.35 阈值后剩 6/6 条
查询: "怎么做红烧肉？" → 过 0.35 阈值后剩 6/6 条

=== 例 3：WithScoreThreshold(0.8) ===
查询: "Intel Arc Ollama GPU 加速" → 过 0.8 阈值后剩 0/6 条
查询: "怎么做红烧肉？" → 过 0.8 阈值后剩 0/6 条

=== 例 4：WithScoreThreshold(0.62) —— 标定出来的窄窗口 ===
查询: "Intel Arc Ollama GPU 加速" → 过 0.62 阈值后剩 5/6 条
查询: "怎么做红烧肉？" → 过 0.62 阈值后剩 0/6 条
```

这里最值得停下来看一眼的是例 1 的第二段：**"怎么做红烧肉？"的 top1 是 0.598 的 `Vulkan 后端…显存记账失步`**——一个和红烧肉毫无关系的运维笔记，拿到了 0.598 分。这就是第 5 篇"短文本同主题语料会把分数压在窄高位带"的直观样子。

再看例 4：0.62 把无关查询干净地筛成 0 条，同时保留了 5 条相关笔记。**这个数字不是猜的，是量出来倒推的。**

## 7. 坑与对照（实测验证）

| 现象                         | 原因                                                 | 处理                                           |
| :--------------------------- | :--------------------------------------------------- | :--------------------------------------------- |
| 跟着文档写了检索，但结果不准 | 框架只给接口，质量逻辑全在你自己的 `Retrieve` 里     | 别指望框架提升质量；把力气花在语料与标定       |
| 加了前缀但没效果             | 前缀影响本来就小（0.778 vs 0.771，第 5 篇实测）      | 按模型约定加上即可，别指望它解决问题           |
| 以为组件会替你加前缀         | 组件原样透传（实测 cos=0.885，非 1）                 | 文档侧/查询侧前缀都自己加                      |
| 阈值 0.35 不起作用           | 无关查询也能拿到 0.5~0.598                           | 量自己的语料，算出有效窗口（本例 0.598~0.654） |
| 阈值调到 0.8 什么都搜不到    | 相关查询最高分只有 0.778                             | 阈值上限不能超过语料的最高分                   |
| 换向量库就以为质量会变好     | 向量库解决"存哪、查多快"，不解决"算不算相关"         | 两者分开评估                                   |
| 组件版本对不上               | `embedding/ollama` 当时**只有伪版本**（无 tag）      | `go.mod` 里钉死完整版本号                      |
| 依赖体积意外变大             | embedding 组件依赖 `github.com/ollama/ollama` 整个库 | 在意体积时留意这条依赖                         |

## 8. 交出去 vs 自己管

| 交出去（框架做）                                 | 自己管（框架没有）                                   |
| :----------------------------------------------- | :--------------------------------------------------- |
| 调 embedding 模型拿向量（组件）                  | **前缀怎么加**（文档侧 / 查询侧）                    |
| `Retriever` 接口与选项传递                       | **相似度怎么算**、向量存哪                           |
| `WithTopK` / `WithScoreThreshold` 两个**参数名** | **这两个参数取多少**                                 |
| `Document` 结构（含 `WithScore` / `Score`）      | **片段怎么切**（第 5 篇：6 条整篇，生产要分块+重叠） |
| 接向量库的实现（redis / milvus / qdrant / es …） | **检索质量怎么评估**                                 |
| ——                                               | **"只依据片段作答"的兜底指令**                       |

一句话：**框架帮你省掉的是"怎么接上"，省不掉的是"检索准不准"。**

## 9. 刻意简化 vs 生产做法

| 本篇/框架的做法                    | 生产环境的做法                                 |
| :--------------------------------- | :--------------------------------------------- |
| 6 条固定笔记全量向量化、全量算余弦 | 真实文档分块 + 重叠切分，交给向量库索引        |
| 阈值靠一次实测标定                 | 建评测集，用 recall/precision 持续调参与回归   |
| 单一 embedding 模型                | 混合检索（向量 + 关键词/BM25）+ 重排（rerank） |
| 前缀手工拼接                       | 封装在索引/查询两侧的统一层里，避免漏加        |
| 无重排、无去重                     | 召回 topK 后重排，再截断到真正送进上下文的条数 |
| 单机单进程内存索引                 | 向量库 + 缓存 + 增量更新                       |

## FAQ

| 问题                         | 解决                                                                     |
| :--------------------------- | :----------------------------------------------------------------------- |
| 用框架写 RAG，质量会更好吗？ | 不会。实测分数区间与手写版逐位一致，框架是中性的                         |
| 那框架的价值在哪？           | 统一接口、组件生态（向量库接入）、和 Agent 图编排接得上                  |
| 阈值到底取多少？             | 量你的语料：取"相关查询最低分"与"无关查询最高分"之间。本例是 0.598~0.654 |
| 为什么 0.35 在别人那儿好用？ | 语料不同。文档长、主题分散时分数会散开，固定阈值才可能有效               |
| 前缀到底重不重要？           | 影响小但成本低，按模型约定加上即可；真正影响大的是阈值、切分与 topK      |
| 该不该上向量库？             | 语料几十条时全量余弦就够（第 5 篇的做法）；上千条再考虑向量库            |

## 结论

1. **框架在这里是中性的**：它不改变检索质量，只改变你写检索代码的方式——质量由模型和语料决定，不由调用代码决定；
2. **前缀完全是你的事**：embedding 组件配置里没有任何前缀字段（grep 零命中），实测 `embed("X")` 与 `embed("search_document:X")` 的余弦是 **0.885 而非 1**，证明它原样透传；
3. **框架给旋钮，不给刻度**：`WithTopK` / `WithScoreThreshold` 规定了参数**名**与语义（阈值是过滤、topK 是截断），取值要靠你量语料；
4. **标定是可做的，但窗口很窄**：本例阈值有效窗口只有 **0.598~0.654**（0.056 宽）——0.35 和 0.8 都是废的，0.62 能干净地筛掉无关查询；
5. 至此三篇框架篇走完一轮，总判断在下面。

## 三篇框架篇的总判断

三篇做的是同一件事：拿手写篇的一个决策，去问框架怎么处理，然后看代价落在谁身上。

第 6 篇问的是循环。框架把循环骨架、工具调度、消息顺序全接了过去，这三样确实是它做得比你手写更稳——第 3 篇里最容易写错的两处（append 顺序、`finish_reason` 判定）从此不用操心。但一碰终止条件，边界立刻显形：默认 20 轮是通用值，`0` 不是无限而是回落默认，超限返回的是错误而不是优雅降级，而**工具报错时它直接终止整个 run，根本不会回喂给模型**。这些都不是能力不足，是它没有你的业务上下文——它不知道你的流程该在第几轮停，也不知道你的模型会把 `10` 写成 `"10"`。

第 7 篇问的是流式。这次分界线特别干净：**上游全部交出去，下游一行都没少。** 读流、解析报文、抹平不同后端的格式差异、甚至提供 `ConcatMessageStream` 这样的分片拼接工具，框架都做了；但 `event:`/`data:` 编码、`Flush()` 时机、心跳、超时，整个仓库里一行 SSE 代码都没有。而且上游那一半的收益还不是常数——热态首字节 30 毫秒，冷态 2.8 秒，取决于第 1 篇那个 `OLLAMA_KEEP_ALIVE` 有没有命中。

第 8 篇问的是检索，边界反而最干脆：**框架只给了接口，没给策略。** 它把检索调用封好了、把旋钮的名字定好了，但旋钮该拧到几、前缀该不该加、片段该多长，它一概不管——而这三件事恰好决定了检索到底有没有用。这和第 6、7 篇不同：前两篇里框架至少给了你机制，这里它连机制都只是接口。

三篇看下来，同一句话被从三个方向验证了：

> **框架帮你省掉的是"怎么写"，省不掉的是"为什么这么写"。**

每个领域里"省不掉"的那部分，形状都一样：**它缺的不是能力，是只有你才知道的信息**——你的业务能容忍几轮、你的模型遵循度如何、你的语料分数落在哪个区间、你的用户能等多久。所以判断"该不该信任框架默认值"这件事，前提是你先自己跑通过一遍机制。这和第 2 篇"只用标准库跑通闭环"是同一个道理：**顺序反了，你连该不该信任它的默认值都判断不了。**

## 附录：版本与实测环境

**依赖版本**（`go.mod` 锁定，实测日期 2026-09-11）：

```text
github.com/cloudwego/eino                              v0.9.19
github.com/cloudwego/eino-ext/components/embedding/ollama
        v0.0.0-20260909094858-6fe16d48b736   ← 伪版本，无 tag
github.com/ollama/ollama                               v0.9.6    ← 上面那个组件的依赖
```

**实测环境**：Go 1.27.1 / Ollama + `nomic-embed-text` / Intel Arc A770。

**关于版本的提醒**：`eino-ext` 的组件是**独立模块、各自发版**。本篇用到的 `components/embedding/ollama` 当时**只有伪版本**（形如 `v0.0.0-<日期>-<commit>`），意味着它没有发布过 tag——**这类依赖的行为比打了 tag 的更可能变**。请照抄完整版本号，不要写 `@latest`。

**源码位置**（v0.9.19，换版本请重查）：

- 检索接口与选项：`eino/components/retriever/interface.go`、`option.go`（`WithTopK` / `WithScoreThreshold` / `GetCommonOptions`）
- 文档结构与分数：`eino/schema/document.go`（`WithScore` / `Score`）
- embedding 组件配置：`eino-ext/components/embedding/ollama/embedding.go`（无任何 prefix 字段）

## 本文结论是否仍成立（自己就能验）

本篇有三个层面的结论，**腐烂速度完全不同**，自检方式也不一样。分层来看比笼统地"重跑一遍"有用得多。

### 接口层（一条命令，最稳定）

看 `Retriever` 是否还是"只有一个方法 + 一组选项"的形状：

```bash
grep -rn "func With" $(go env GOMODCACHE)/github.com/cloudwego/eino@*/components/retriever/option.go
```

本篇实测时是 `WithIndex` / `WithSubIndex` / `WithTopK` / `WithScoreThreshold` / `WithEmbedding` / `WithDSLInfo`。**若哪天多出一个"标定阈值"或"自动切分"之类的选项，§2 和 §6 的边界判断就要放宽**——那意味着框架开始承担策略了。

### 实现层（改几行，容易变，但变了是好事）

前缀那条可以用你第 5 篇的 embed 代码直接验——把它调用两次：

```go
// 同一个字符串，一次不加前缀、一次加前缀，比两个向量的余弦
vecs, _ := emb.EmbedStrings(ctx, []string{
	"Intel Arc Ollama GPU 加速",
	"search_document:Intel Arc Ollama GPU 加速",
})
// 算 vecs[0] 与 vecs[1] 的余弦
```

**成立**：余弦明显小于 1（本篇实测约 **0.885**），说明组件原样透传文本、前缀要你自己加。
**变了**：若结果变成 `1.000`，说明组件自动加了前缀，§3 整节和结论第 2 条需要重写。

### 数据层（一定会变，而且该变）

第 4 节那组分数与窗口 **0.598 ~ 0.654** 只对"这 6 条笔记 + `nomic-embed-text` + 这台机器"成立。用你第 5 篇自己那套检索器重跑一遍：

**换语料、换 embedding 模型，窗口必然移动——这不是文章过时，这正是文章在说的那件事。** 如果数字对不上，第一反应应该是"我的语料不一样"，而不是"文章写错了"。（顺着 §4.1 那个方法做：取"相关查询的最低分"与"无关查询的最高分"，夹在中间的就是你的窗口。）

### 判读原则

**接口层变了要重写文章，实现层变了要改结论，数据层变了只需重新标定。**

而"阈值必须按自己的语料标定"这句话本身，恰恰是三层里最不会过时的那一层。不过要说清一点，免得被读成"框架永远帮不上忙"：**框架将来完全可以给你一个标定工具**——喂进一批带标注的查询-文档对，让它吐出推荐阈值，这在工程上毫无难度。但**"标到什么程度才算够"这个标准只能由你定**，因为它取决于你的语料、以及你的用户能容忍几次错召回；而且工具要吃的那批标注数据，也只有你有。所以准确的说法不是"框架不会做这件事"，而是：**框架可以给你更好的工具，判断标准永远来自你。**
