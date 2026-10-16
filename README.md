# MoonGate API

个人博客后端服务，为 [MoonGate](https://moongate.top) 前端提供**只读内容 API**：
Markdown 内容加载、语言回退、搜索/标签过滤、分页与系列分组。

## 技术栈

- **语言**: Go 1.26
- **Web 框架**: Gin
- **内容处理**: Markdown + Frontmatter → HTML → 白名单清洗

## API 端点

### 内容 API（公开，只读）

| 方法 | 路径              | 说明                                                                |
| ---- | ----------------- | ------------------------------------------------------------------- |
| GET  | `/api/docs`       | 文档列表（分页、搜索、标签、`?group=series` 系列分组、`?lang=en`）  |
| GET  | `/api/docs/:slug` | 文档详情（支持 `?lang=en`，无译文时回退中文）                       |

查询参数：`page`（默认 1）、`limit`（默认 10，**上限 100**）、`search`、
`searchMode`（`all`/`title`/`description`，非法值 400）、`tag`（可重复）、
`content=true`（列表项带上正文）、`group=series`（返回系列分组裸数组）、`lang`（只认 `en`）。

> `limit` 超过 100 会被钳制为 100，并在响应的 `limit` 字段回显真实生效值。
> 客户端应按信封里的 `limit` / `totalPages` 翻页，不要自己假设页大小。

### 其他

| 方法 | 路径      | 说明     |
| ---- | --------- | -------- |
| GET  | `/health` | 健康检查 |

### CORS

线上前端与 API 同源（`NUXT_PUBLIC_API_URL=https://moongate.top`，Caddy 反代 `/api`），
浏览器请求不跨源，因此**生产不需要放行任何 Origin**。本地开发跨源（Nuxt 跑在 3000），
用 `CORS_ORIGINS`（逗号分隔）放行，默认 `http://localhost:3000`；只放行 `GET`/`OPTIONS`。

## 内容管理

内容以 Markdown 文件存储在 `content/` 目录：

- `content/docs/*.md` — 技术文章

### 中英双语

- 英文译文与中文文章放在同一目录，文件名为 `<slug>.en.md`（如 `content/docs/url-state/component-library-publishing.en.md`），slug 自动去掉 `.en` 后缀与中文配对。
- 英文文件的 frontmatter 以中文为标准：`date/series/tags` 与中文一致，`title/description` 为英文。
- API 通过 `?lang=en` 请求英文版本：有译文返回英文（`lang:"en"`），无译文回退中文（`isFallback:true`）；不传 `lang` 或传其他值（如 `ja`）时始终返回中文。
- 响应字段：`lang`（实际语言）、`isFallback`（是否回退）、`hasTranslation`（是否存在英文译文）。
- **注意**：内容只有中文与英文两种。`ja` 等界面语言拿到的仍是中文正文，
  且 `isFallback` 为 false（它只表达"请求了 en 但没有译文"），
  因此前端若要提示"当前语言无译文"，应比较自身语言与响应的 `lang`，而不是只看 `isFallback`。

### Frontmatter 格式

```yaml
---
title: "文章标题"
description: "简短摘要"
date: 2025-01-15 10:00:00
series: "系列名称" # 可选
tags:
  - Go
  - Tutorial
---
正文 Markdown 内容...
```

### HTML 清洗（内容可信边界）

正文渲染完成后会在**加载期**经白名单策略清洗一次（`internal/loader/sanitize.go`，
bluemonday），随后才存入内存。因此：

- SSR 与浏览器拿到的是同一份 HTML，前端**不做任何运行期清洗**；
- 放行范围按"现有文章一字不改"确定（`details/summary`、标题 `id`、`code` 的
  `language-*` class、表格、任务列表、脚注、`img` 的行内 style、`href="#"` 等）；
- `internal/loader/sanitize_test.go` 会对 `content/docs` 下**每一篇**文章断言
  "清洗前后 DOM 无差异"——它既是上线等价性证明，也是永久守卫：
  新文章若用到策略之外的原始 HTML，测试会指出具体文件与节点，由作者决定放宽策略还是改文章，
  不做静默削减。

> 正文被视为**仓库自产内容**（作者自己的 Markdown）。清洗是纵深防御，
> 不是"允许编辑不可信 HTML"的许可。

## 测试

项目包含 64 个自动化测试（另有 64 个子用例），覆盖：

- 内容加载与 Markdown 解析（含 `details` 内围栏代码块的改写）
- 语言回退矩阵（中/英/仅英文兜底/未命中/未支持语言）
- 排序确定性、过滤、分页边界（含 `limit` 上限）、系列分组
- 清洗策略对全部真实内容为恒等映射（见上文"HTML 清洗"）
- **HTTP 契约快照**：`internal/api/testdata/contract/*.json` 冻结了 15 组真实响应，
  任何改动只要改变对外 JSON（字段名、字段顺序、空值语义、状态码）就会被测试拦下
- 领域模型边界情况

```bash
make test   # 运行全部测试
make check  # vet + test（与 CI 一致）
```

### 契约快照的体积与去向

快照里 `content` / `description` 被替换为占位符 `<text>`（这两项随文章编辑而变，
不应该让契约测试失败），因此 15 个文件合计仅约 92 KB；快照只需存在，`go build`、
`go vet` 皆不读取，运行期更不依赖它。

它只在 GitHub 仓库与 CI 里存在，**不会进入部署产物**：

- 运行镜像只 COPY 二进制与 `content/`（见 `Dockerfile` 的 runtime 阶段）；
- `.dockerignore` 另外排除了 `**/testdata`，构建上下文也不会带上它。

CI 的 `test` job 是 `deploy` job 的前置条件，所以快照需要提交进仓库——删除它会让
契约护栏在 CI 中失效；如果不再需要护栏，应当连同 `docs_contract_test.go` 一起移除。

## 部署

项目通过 GitHub Actions 自动部署：

1. 推送代码到 `main` 分支
2. CI 运行 `make check`（测试阶段）
3. 测试通过后构建 Docker 镜像并推送阿里云 ACR
4. SSH 到服务器执行 `docker compose up`

详细部署流程见 `.github/workflows/deploy.yml`。

## 架构分层

依赖单向（上层可调下层，下层不知道上层）：

```text
api      解析 HTTP 参数 → 校验 → 调用查询层 → 序列化响应
  ▲
catalog  语言回退 / 排序 / 过滤 / 分页 / 系列分组（只读、并发安全）
  ▲
loader   扫描 content/、解析 frontmatter、Markdown → HTML → 白名单清洗、双语配对
  ▲
domain   解析模型（Frontmatter、Doc）与响应视图（DocDetail、DocSummary）
```

要点：解析产物 `Doc` 不含任何请求期状态，因此可被并发请求共享；语言字段只存在于
响应视图 `DocDetail` / `DocSummary` 上，每次请求新构造，不存在"回填共享结构体"的路径。
清洗发生在 `loader`，所以进入 `domain` 的正文 HTML 已经是最终形态。

## 与前端的分工（跨仓约定）

| 关注点 | 归属 | 说明 |
| --- | --- | --- |
| 语言回退/排序/过滤/分页/系列顺序 | 后端 | 前端不再自行排序或过滤，只按响应渲染 |
| 正文 HTML 与清洗 | 后端 | 前端 `v-html` 直接渲染，无运行期清洗 |
| 代码高亮（Shiki） | 前端 | 后端只产出 `<pre><code class="language-*">`，前端替换为高亮结果 |
| 实体转义写法 | 不构成约定 | 前端按实体语法统一解码（`app/utils/htmlEntities.ts`），后端换写法不影响高亮 |
| 站点 SEO 产物（feed/sitemap/robots） | 前端 Nitro | 通过本 API 取数后自行拼装 |

## 项目结构

```text
├── main.go              # 入口：加载内容、装配 catalog 与路由
├── internal/
│   ├── api/             # HTTP handlers（薄层）+ 契约快照测试
│   ├── catalog/         # 查询层：语言回退、排序、过滤、分页、系列分组
│   ├── domain/          # 解析模型（doc.go）与响应视图（dto.go）、SearchMode
│   └── loader/          # Markdown 加载、解析、HTML 渲染与清洗
└── content/             # Markdown 内容源
```
