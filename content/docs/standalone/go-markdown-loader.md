---
title: 用 Go 重构 Markdown 加载：从 Nuxt Content 到独立数据 API
description: 记录如何把博客 Markdown 内容加载从 Nuxt Content 中拆出，用 Go 实现独立数据 API：动机、双语内存存储与语言回退、内容上线路径、实测数据、适用边界（何时该上数据库）与已知约束（完整代码见仓库）。
date: 2026-09-10
tags:
  - Go
  - Engineering
---

## 一、背景

博客原本用 Nuxt Content 管理 39 篇技术文章，整体体验不错，但有两个痛点：

1. **内容模型与渲染行为分散**：每新增一种内容类型，都要在 `content.config.ts` 加 collection 与 zod schema。想调整代码高亮、目录深度或主题，又得改 `nuxt.config.ts`。配置本身就在累积复杂度。
2. **内容与构建耦合**：内容更新需要重新构建前端，GitHub Actions 每次跑 3-4 分钟——改错别字也要等这么久，累积起来就不少了。

两个痛点叠加，让我决定把"内容加载"从 Nuxt 中拆出来：需求足够小、边界清晰，适合先用 Go 实现一个独立的 Markdown 数据源，验证自建方案的可行性。

## 二、整体设计

这次拆解有明确的目标：**内容与代码分离**（改内容不再触发前端重建）、**技术透明**（每行代码在自己掌控中）、**轻量依赖**（按需引入，不背全家桶）。整体设计如下：

```text
┌──────────────────────────────────────────────┐
│ content/  Markdown 内容                       │
│  docs/   技术文章 + *.en.md 英文译文          │
│  about/  关于页面                             │
└──────────────────────┬───────────────────────┘
                       ▼
┌──────────────────────────────────────────────┐
│ Go 程序启动时一次性加载                        │
│  递归遍历 .md → 解析 frontmatter + Markdown→  │
│  HTML → 按 slug 存入内存 map（中文 + 译文）    │
└──────────────────────┬───────────────────────┘
                       ▼
┌──────────────────────────────────────────────┐
│ Gin API                                      │
│  GET /api/docs[:slug]   列表/单篇/系列分组/   │
│                         语言回退              │
│  GET /api/about[:slug]  关于页                │
└──────────────────────────────────────────────┘
```

技术选型：

| 组件          | 选择                       | 说明                   |
| ------------- | -------------------------- | ---------------------- |
| Web 框架      | Gin                        | 轻量、性能好、路由直观 |
| YAML 解析     | `github.com/goccy/go-yaml` | 解析快，日期格式友好   |
| Markdown 渲染 | `gomarkdown`               | 纯 Go，无 CGO 依赖     |

## 三、项目结构（概览）

代码按职责分三个包（完整目录见仓库）：

- `internal/domain`：Doc / About 领域模型，以及"能设置 slug 与正文"的 `ContentSetter` 接口；
- `internal/loader`：递归扫描 content 目录、frontmatter + Markdown 解析、双语配对、内存存储；
- `internal/api`：Gin Handler，负责列表、单篇与语言回退。

入口直接放在根目录 `main.go`——项目不大，不需要 `cmd/` 分层。

## 四、数据模型

### 4.1 Doc 结构体

```go
// internal/domain/doc.go（节选）
type Doc struct {
	Title       string    `yaml:"title" json:"title"`
	Description string    `yaml:"description" json:"description"`
	Date        time.Time `yaml:"date" json:"date"`
	Slug        string    `yaml:"slug,omitempty" json:"slug"`   // 由文件名生成
	Series      *string   `yaml:"series" json:"series"`         // nil = 不属于任何系列
	Order       *int      `yaml:"order,omitempty" json:"order"` // nil = 未声明阅读顺序
	Tags        []string  `yaml:"tags" json:"tags"`
	Content     string    `json:"content"`        // 正文 HTML（Markdown 转换而来）
	Lang        string    `json:"lang"`           // 实际返回语言：zh | en
	IsFallback  bool      `json:"isFallback"`     // 是否发生了语言回退
	HasTranslation bool   `json:"hasTranslation"` // 该 slug 是否存在英文译文
}
```

`Series`、`Order` 用指针而不是值类型：`nil` 表示"没有"，JSON 序列化时自动省略，从而区分"空值"和"不存在"。`Slug` 不写在 frontmatter 里，由 loader 从文件名生成。末尾三个 `Lang/IsFallback/HasTranslation` 字段不带 `yaml` tag，由 API 层按请求语言回填，因此不参与 YAML 解析。

列表接口不返回整篇正文，而是返回去掉 `Content` 的摘要 `DocSummary`，需要系列分组时再用 `SeriesGroup` 包装；`About` 字段更精简、结构与 Doc 同思路。这些类型连同它们的 Setter 方法都定义在 `internal/domain/` 下（`doc.go` / `about.go`），正文不再重复贴。

### 4.2 ContentSetter 接口

`Doc` 与 `About` 各自实现 `SetSlug` / `SetContent` 两个方法（接口定义见仓库 `domain/content.go`）。这样解析函数不需要为每种类型写一份：它只关心"类型 T 有没有这两个 setter"。泛型 `[T any]` 负责类型安全，接口负责把"能设置 slug 与正文"这个能力抽象出来——两者配合，`ParseMarkdown` 一套逻辑通吃两种类型。

## 五、核心解析逻辑

### 5.1 文件格式

```yaml
---
title: Go 后端开发实践
description: 从 Markdown 到内存的完整方案
date: 2026-07-10
series: backend
tags:
  - Go
  - Markdown
---
# 正文内容
```

文件名即 slug。`docs/go-backend.md` → `slug=go-backend`；`docs/go-backend.en.md` 则去掉 `.en` 后缀，得到同一个 slug 与中文配对。`content/docs/` 下可以任意嵌套子目录，loader 会递归扫描。

### 5.2 解析函数

```go
// internal/loader/parse.go（节选，完整实现见仓库）
func ParseMarkdown[T any](filePath string) (T, error) {
	var result T

	// 1. 读取文件
	data, err := os.ReadFile(filePath)
	if err != nil {
		return result, fmt.Errorf("读取文件失败: %w", err)
	}

	// 2. 按 "---" 分割 frontmatter 与正文
	parts := strings.SplitN(string(data), "---", 3)
	if len(parts) != 3 {
		return result, fmt.Errorf("无效的 frontmatter 格式: %s", filePath)
	}

	// 3. 解析 YAML 到目标结构体
	err = yaml.Unmarshal([]byte(parts[1]), &result)
	if err != nil {
		return result, fmt.Errorf("解析 frontmatter 失败: %w", err)
	}

	// 4. 文件名生成 slug：*.en.md 去掉 .en 后缀，与中文共享同一 slug
	baseName := filepath.Base(filePath)
	slug := strings.TrimSuffix(baseName, filepath.Ext(baseName))
	slug = strings.TrimSuffix(slug, ".en")

	// 5. 泛型 + 接口：T 只要实现 ContentSetter，就统一回填 Slug 与 HTML 正文
	htmlContent := mdToHTML(parts[2])
	if setter, ok := any(&result).(domain.ContentSetter); ok {
		setter.SetSlug(slug)
		setter.SetContent(htmlContent)
	}

	return result, nil
}
```

这里有两个设计点。其一，正文 Markdown 在**加载期**一次性转成 HTML 并缓存，请求期零解析——所以列表接口默认不返回 `Content`，只有显式 `?content=true` 才带正文。其二，frontmatter 切分用的是最朴素的 `SplitN`，其边界（正文里出现 `---`）见文末"已知约束"。

### 5.3 Markdown → HTML 与 details 预处理

渲染用 gomarkdown（CommonMark 实现、纯 Go 无 CGO）。解析与渲染各两行配置——`parser.CommonExtensions | AutoHeadingIDs` 与 `html.CommonFlags | HrefTargetBlank`，见仓库 `loader/html.go`。这里有一个真实踩过的坑：gomarkdown 把 `<details>…</details>` 整段当作"原始 HTML"透传，不解析其中的 Markdown。details 内的代码围栏因此退化成纯文本。对策是在渲染前用 `expandDetailsCodeFences` 改写为真实的 `<pre><code>`。改写时会做 HTML 转义，与前端 Shiki 的反义清单严格对应；实现见 `loader/details.go` 及其单元测试。

## 六、加载与内存存储

双语内容靠"按 slug 索引的三张 map"组织：中文/默认、英文译文、关于页各一张（`Store` 定义见仓库 `load.go`）。入口 `LoadAll`：

```go
// internal/loader/load.go（节选）
func LoadAll(contentDir string) (*Store, error) {
	store := &Store{
		DocsBySlug:  make(map[string]*domain.Doc),
		DocsEn:      make(map[string]*domain.Doc),
		AboutBySlug: make(map[string]*domain.About),
	}

	if err := loadDocs(filepath.Join(contentDir, "docs"), store); err != nil {
		return nil, fmt.Errorf("加载 docs 失败: %w", err)
	}
	if err := loadAbout(filepath.Join(contentDir, "about"), store); err != nil {
		return nil, fmt.Errorf("加载 about 失败: %w", err)
	}

	// 健壮性提示：英文译文没有对应的中文文章
	for slug := range store.DocsEn {
		if _, ok := store.DocsBySlug[slug]; !ok {
			fmt.Printf("⚠️ 英文译文 %s.en.md 没有对应的中文文章\n", slug)
		}
	}
	return store, nil
}
```

docs 的核心加载逻辑（`loadDocs` 节选）：

```go
// loadDocs 递归加载 content/docs/ 下所有技术文章（支持子目录与 .en.md 译文）
func loadDocs(dir string, store *Store) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".md" { // 只处理 .md 文件
			return nil
		}

		doc, err := ParseMarkdown[domain.Doc](path)
		if err != nil {
			fmt.Printf("⚠️ 跳过 %s: %v\n", path, err) // 单文件失败只警告，服务照常启动
			return nil
		}
		if doc.Series != nil && *doc.Series == "" { // YAML 空串 → nil，语义与"未声明"一致
			doc.Series = nil
		}

		// 双语分流：.en.md 进译文 map，其余进默认 map
		if strings.HasSuffix(path, ".en.md") {
			store.DocsEn[doc.Slug] = &doc
		} else {
			store.DocsBySlug[doc.Slug] = &doc
		}
		return nil
	})
}
```

几点设计说明：

- **键用 slug 而不是自增 id**：slug 直接对应文件名与 URL（`/api/docs/:slug`）。中文与英文译文共享同一 slug，靠 `.en.md` 后缀分流到 `DocsBySlug` 与 `DocsEn` 两张 map。
- **`docs/` 递归、`about/` 扁平**：博客按系列分了子目录（如 `docs/narrative-engine/`），`loadDocs` 因此用 `filepath.WalkDir` 递归。`about/` 既无子目录也无译文，`loadAbout` 用 `filepath.Glob` 一层即可（见仓库 `load.go`）。

## 七、提供 HTTP API

API 层在 `internal/api/` 下。核心是 `DocsHandler`，持有两张按 slug 索引的 map，提供四组路由：

- `GET /api/docs`：摘要列表，支持 `page`/`limit`/`search`/`tag`/`group=series`/`lang`，`?content=true` 才返回含正文的完整文档；
- `GET /api/docs/:slug`：单篇，`?lang=en` 优先译文、无译文自动回退；
- `GET /api/about` / `GET /api/about/:slug`：关于页列表与单篇；
- `GET /health`：健康检查。

Handler 最关键的一段是语言回退解析（`resolveDoc`，节选）：

```go
// internal/api/docs.go（节选，完整实现见仓库）
// resolveDoc 按请求语言解析文档：lang=en 时优先英文译文、无则回退中文；
// 其余语言优先中文、缺失时兜底英文（防 404）
func (h *DocsHandler) resolveDoc(slug, lang string) (doc *domain.Doc, resolvedLang string, isFallback bool) {
	if lang == "en" {
		if en, ok := h.StoreEn[slug]; ok {
			return en, "en", false
		}
		if zh, ok := h.StoreBySlug[slug]; ok {
			return zh, "zh", true
		}
		return nil, "", false
	}
	if zh, ok := h.StoreBySlug[slug]; ok {
		return zh, "zh", false
	}
	if en, ok := h.StoreEn[slug]; ok {
		return en, "en", true
	}
	return nil, "", false
}
```

配套的两个小函数在仓库完整实现里：`requestLang` 只把 `lang=en` 识别为英文请求，其余按默认中文处理；`hasTranslation` 判断该 slug 是否存在英文译文。单篇 handler 拿到解析结果后做**浅拷贝**再回填 `Lang/IsFallback/HasTranslation`，避免污染共享存储中的文档。

列表 `GET /api/docs` 的行为：

- 默认返回摘要（`DocSummary`，不含 `Content`），支持 `page`/`limit` 分页、`search`/`tag` 过滤与 `group=series` 系列分组。只有显式传 `?content=true` 才返回含正文的完整 `Doc`。
- 排序按日期倒序；日期并列时用确定性次级规则（同系列按 `order` 降序、其余按 slug 升序），防止 map 随机迭代顺序导致每次请求顺序不同、引发 SSR 与客户端水合不一致。
- `group=series` 时先按系列分组，再按阅读顺序排序（显式 `order` 优先、未声明按日期升序兜底，见 `orderSeriesDocs`）。

## 八、内容如何上线

拆出 Go 服务后，发布链路分成两条：

- **前端**：只消费 API 返回的 JSON，内容改动不再触发 Nuxt 构建——原流程里"3-4 分钟"的那一段消失了；
- **内容与后端**：content/ 与代码同仓，改动提交到 main 后触发 GitHub Actions 跑测试、构建镜像并部署——Dockerfile 会把 content/ 一并打进镜像。服务启动时一次性加载全部内容。

所以当前的真实边界是：更新文章不再需要"重建前端"，但仍要经过一次后端镜像的 CI 构建与部署（内容同时获得 git 版本管理）。换来的收益是前端构建产物与内容彻底解耦，前端与内容的发布节奏可以各自独立。若想进一步做到"只同步 Markdown 文件"，把 content/ 从镜像中移出、改为服务器卷挂载后重启进程即可——服务没有热重载，内容改动需重启生效，这是当前最值得做的下一步演进。

生产化还有三件小事，本文没有展开（当前量级也不需要）：

- **容器崩溃自愈**：`restart: always`（Docker）或 systemd 托管，进程挂了自动拉起；
- **可观测性**：开一个 `net/http/pprof` 端点，就能用 §9 那套内存口径在线上定位增长——小流量下 gin 默认 logger 已够用，暂不需要结构化日志；
- **零停机发布**：先起新实例、验过 `/health` 再切流——当前 CI 是 `docker compose up --force-recreate` 直接重建，存在一个重启窗口。

这个窗口的实际影响很小：服务是纯读 API、无进行中写入。滚动更新 / 多副本（K8s 或 `--scale` + 负载均衡）是另一个复杂度台阶，收益撑不起成本——单机部署是刻意的取舍。

## 九、运行数据与压力实测

### 9.1 复现测量（当前仓库）

在仓库当前实现与真实 `content/` 上复测：

| 指标           | 实测值                                                                                      |
| -------------- | ------------------------------------------------------------------------------------------- |
| 文章规模       | 107 个 `.md`（docs 104 = 中文 69 + 英文 35；about 3），≈ 1.64 MiB                           |
| 加载耗时       | 8 次连测：min / avg / max ≈ 41.5 / 44.7 / 46.4 ms                                           |
| 内存（加载后） | RSS 增量 ≈ 8.8 MB（基线 10.5 → 19.3 MB）；VmHWM ≈ 19.3 MB；Heap sys / alloc ≈ 15.5 / 2.8 MB |

**API 响应**（进程内 gin + `httptest`，各 200 次，含 JSON 序列化、不含网络）：

| 接口                                     | avg       | p50       |
| ---------------------------------------- | --------- | --------- |
| 列表摘要 `GET /api/docs?lang=zh`         | ≈ 28.1 µs | ≈ 25.3 µs |
| 单篇含正文 `GET /api/docs/:slug?lang=zh` | ≈ 27.0 µs | ≈ 25.1 µs |

### 9.2 压力测试：这套做法能装下多少文章

用合成数据把规模推到 2 万篇，量化"全量载入 + 启动解析"方案的上限（合成文档 ≈16 KB/篇、frontmatter 结构同真实文章；环境同 9.1，每档独立进程）：

| 合成规模 N | 加载耗时（3 次均值） | RSS 增量 | 每篇 RSS 均摊 | 列表 API p50 |
| ---------- | -------------------- | -------- | ------------- | ------------ |
| 500        | ≈ 136 ms             | ≈ 21 MB  | ≈ 43 KB       | ≈ 77 µs      |
| 1,000      | ≈ 232 ms             | ≈ 37 MB  | ≈ 38 KB       | ≈ 162 µs     |
| 2,000      | ≈ 449 ms             | ≈ 69 MB  | ≈ 35 KB       | ≈ 357 µs     |
| 5,000      | ≈ 1.08 s             | ≈ 170 MB | ≈ 35 KB       | ≈ 1.02 ms    |
| 10,000     | ≈ 2.07 s             | ≈ 328 MB | ≈ 34 KB       | ≈ 2.29 ms    |
| 20,000     | ≈ 4.13 s             | ≈ 662 MB | ≈ 34 KB       | ≈ 5.10 ms    |

读法：

- **加载耗时基本线性**：约 0.21 ms/篇——数千篇冷启动秒级以内，2 万篇约 4 s。
- **内存稳态均摊 ≈ 34 KB/篇**：500 篇约 43 KB（含一次性开销），真实 104 篇小样本 ≈ 87 KB/篇（固定开销占比大）。按（预算 − 基线 ≈ 20 MB）÷ 0.034 MB/篇：512 MB ≈ 1.4 万篇、1 GB ≈ 3 万篇、2 GB ≈ 6 万篇。
- **列表接口会全量排序**：默认摘要列表 p50 从 500 篇的 ≈ 77 µs 升到 2 万篇的 ≈ 5.1 ms（单篇查询仍是 O(1)）。

结论：这套"启动全量解析 + 内存 map"的做法在个人博客/文档站量级（≤ 数千篇）下内存几十到几百 MB、冷启动秒级以内，余量非常充足。按 512 MB 预算约可承载 1.4 万篇（实测 1 万篇 ≈ 0.35 GB）；2 万篇实测 ≈ 0.68 GB，需要 1 GB 档。此量级下列表全量排序开始出现毫秒级成本，若目标到数万篇，再考虑懒加载、摘要与正文分离或索引化等演进。

> 测量方法如下。9.1 针对真实 content：规模用 `find content -name "*.md" | wc -l` 与 `du -sb content` 统计；加载耗时取进程内多次 `time.Since`；内存读 `/proc/self/status`（VmRSS/VmHWM）与 `runtime.ReadMemStats`；API 延迟为进程内 gin + `httptest`，均不含网络。9.2 使用合成数据（正文 ≈ 16 KB/篇、8 字段 frontmatter），档位 500 → 20,000；每档新起进程、冷加载 3 次取均值，列表 p50 为预热 20 次后 100 次的进程内请求。环境均为 AMD Ryzen 5 5600X / 16 GB / Linux，Go 1.27.1。部署时间 ~10 秒为发布/CI 环境值，本地未复测；旧表"< 5MB"为 39 篇时代的进程占用口径。

## 十、适用边界：什么时候用它，什么时候上数据库

前两节实测回答了"这套方案能装多少"，这里回答"该不该用它"。判断基准不是文章数量，而是数据性质——**你的内容是内容，还是业务数据**：前者只读、低频、作者可控，适合本文方案甚至静态生成；后者需要写入、实时与灵活查询，才真正需要数据库。

| 判断维度   | 继续用本文的内存加载方案                                       | 换数据库 / 全文索引                               |
| ---------- | -------------------------------------------------------------- | ------------------------------------------------- |
| 内容性质   | 只读为主、作者自产，正文即最终数据                             | 用户产生或运行期写入：评论、草稿、多作者、UGC     |
| 更新方式   | 低频，随发布流程（git + CI）重启生效，接受重启窗口             | 高频 / 实时，不能等发布与重启                     |
| 查询形态   | 固定几种：按 slug 单篇、列表分页/标签/标题摘要关键词、语言回退 | 正文全文检索 + 相关性排序、临时组合查询、统计报表 |
| 规模       | 数千～1-2 万篇：512 MB ≈ 1.4 万、1 GB ≈ 3 万（§9.2）           | 数万篇以上或单篇体积巨大，内存 / 冷启动预算吃紧   |
| 写入与权限 | 无并发写入，不需要事务与审计                                   | 需要事务、并发控制、权限分级与审计                |
| 运维成本   | 零迁移零备份，行为全部可控                                     | 迁移、备份、连接管理的持续成本（常被低估）        |

怎么读这张表：**右侧前两行是硬信号**——出现运行期写入或实时更新需求，无论规模大小都该考虑数据库。**查询与规模是软信号**，只有吃紧时才轮到数据库 / 全文索引更省事。后两行则提醒你为右侧方案付出的运维税。个人博客通常六行全落在左侧：内容只读、改动随发布、查询固定、量级数千篇——此时内存 map 反而是比数据库更优的答案，正好兑现 §2 的三个目标。

还有一个容易撞到的天花板：本文方案的 `search` 只是对标题/摘要做内存 `contains`，**没有正文全文检索与相关性排序**。一旦需要正文级搜索、拼写容错或标签权重，就该引入全文索引（Meilisearch、Postgres FTS 或外部搜索服务），而不是继续在 loader 里手写遍历。

最后，二者不是二选一。Markdown 可以始终留在 git 里当唯一事实来源（编辑、评审、双语配对都不变），只把元数据或检索下沉到数据库 / 索引，正文 HTML 仍由 loader 生成并缓存。本文的"全量载入内存"只是谱系的一端，懒加载 + 缓存、摘要与正文分离、外置索引都是按需演进的中间站。其中最容易先做的是**摘要与正文分离**：启动只载 frontmatter 元数据，正文 HTML 按 slug 首次命中再解析并缓存。它会引入两个新成本——singleflight 防并发重复解析，以及解析失败从启动期警告移到请求期；取舍随之变化。

不过对博客场景，它并非明显的优化。每篇正文最终都会被读到，懒加载的稳态内存会收敛到与全量载入相当的水平。它真正省的只有冷启动时间，代价是把解析失败从启动期警告移到请求期，还引入并发去重。真正划算的场景是正文访问远少于元数据的长尾档案库——那不是博客的形态。

## 十一、已知约束与演进

下面这些条目都是**已意识到的边界，刻意不修**：当前均未触发，逐个修会稀释本文"记录当前实现"的主线——因此只在此登记边界与对应的最小修法，哪个先触发再改。

| 边界 | 现状与影响 | 修法 / 约定 |
| --- | --- | --- |
| Frontmatter 切分非行锚定 | 正文靠前出现 `---`（水平线、setext 标题下划线）可能错位；当前靠"frontmatter 紧贴文件开头"约定规避 | 行扫描：首行（容忍 BOM）恰为 `---`，以第一个"整行恰好是 `---`"的行为关闭定界符；或引入 frontmatter 解析库 |
| Slug 只取 basename | 子目录同名文件会静默互相覆盖；`x.en.md` 与任意目录的 `x.md` 配对；loader 未做冲突检测 | 命名约定：basename 全局唯一（当前已满足）；先触发再加冲突检测 / 警告 |
| 渲染未做清理 | 原始 HTML 透传，`HrefTargetBlank` 无 `rel="noopener noreferrer"` | 自管内容可接受；开放投稿前补 sanitize 与 noopener |
| API 信封不统一 | 列表返回 `{data,total,...}`，系列分组与 about 列表返回裸数组 | 前端分别处理；收口信封是破坏性变更，需前后端同步改——自用 API、无第三方消费者，统一收口的收益低于成本，暂缓 |
| 图片与静态资源（刻意回避） | 正文尽量不放图，避免资源托管 / CDN 等外部依赖；loader 只产出 HTML | 必须配图时用绝对 URL 指向已有图床，不做资源搬迁 |

> 提示：goccy 的 YAML 解析容忍开头的 `---` 文档头（见其解码测试），但它只省掉第一个分隔符。第二个 `---` 之后是 Markdown 正文，不能交给 YAML 解析器——所以切分必须自己做。

## 十二、小结

至此，一个由 Go 实现的轻量 Markdown 数据源就完成了：启动时把 content/ 解析进内存，通过 Gin 暴露列表与单篇接口；前端只消费 JSON，与内容解耦。相比 Nuxt Content 全家桶，这套方案的依赖与行为都更可控；代价（内容随镜像发布、无热重载、若干解析边界）已在上文如实列出——知道自己停在哪，是自建方案的一部分。

---

> 注：正文只展示关键代码节选，完整实现见[仓库](https://github.com/yuelinghuashu/moongate-api) `internal/` 下的 `domain`、`loader`、`api` 三包与根目录 `main.go`。
