# MoonGate API

个人博客后端服务，为 [MoonGate](https://moongate.top) 前端提供内容管理、GitHub OAuth 认证和用户管理 API。

## 技术栈

- **语言**: Go 1.26
- **Web 框架**: Gin
- **内容处理**: Markdown + Frontmatter → HTML

## API 端点

### 内容 API（公开）

| 方法 | 路径               | 说明                                       |
| ---- | ------------------ | ------------------------------------------ |
| GET  | `/api/docs`        | 文档列表（支持分页、搜索、标签、等级筛选、`?lang=en`） |
| GET  | `/api/docs/:slug`  | 文档详情（支持 `?lang=en`，无译文时回退中文） |
| GET  | `/api/about`       | 关于页面列表                               |
| GET  | `/api/about/:slug` | 关于页面详情                               |

### 其他

| 方法 | 路径      | 说明     |
| ---- | --------- | -------- |
| GET  | `/health` | 健康检查 |

## 内容管理

内容以 Markdown 文件存储在 `content/` 目录：

- `content/docs/*.md` — 技术文章
- `content/about/*.md` — 关于页面

### 中英双语

- 英文译文与中文文章放在同一目录，文件名为 `<slug>.en.md`（如 `content/docs/url-state/component-library-publishing.en.md`），slug 自动去掉 `.en` 后缀与中文配对。
- 英文文件的 frontmatter 以中文为标准：`date/level/series/tags` 与中文一致，`title/description` 为英文。
- API 通过 `?lang=en` 请求英文版本：有译文返回英文（`lang:"en"`），无译文回退中文（`isFallback:true`）；不传 `lang` 或传其他值（如 `ja`）时始终返回中文，行为与旧版一致。
- 响应新增字段：`lang`（实际语言）、`isFallback`（是否回退）、`hasTranslation`（是否存在英文译文）。

### Frontmatter 格式

```yaml
---
title: "文章标题"
description: "简短摘要"
date: 2025-01-15 10:00:00
level: P2 # P1-P5 难度等级
series: "系列名称" # 可选
tags:
  - Go
  - Tutorial
---
正文 Markdown 内容...
```

## 测试

项目包含 46 个自动化测试，覆盖：

- 内容加载与 Markdown 解析
- API 分页/搜索/筛选逻辑
- JWT 认证与中间件
- 领域模型边界情况

```bash
make test   # 运行全部测试
make check  # vet + test（与 CI 一致）
```

## 部署

项目通过 GitHub Actions 自动部署：

1. 推送代码到 `main` 分支
2. CI 运行 `make check`（测试阶段）
3. 测试通过后构建 Docker 镜像并推送阿里云 ACR
4. SSH 到服务器执行 `docker compose up`

详细部署流程见 `.github/workflows/deploy.yml`。

## 项目结构

```text
├── main.go              # 入口：环境变量、DB、路由注册
├── db/                  # PostgreSQL 连接
├── internal/
│   ├── api/             # HTTP handlers + 中间件
│   ├── domain/          # 领域模型（Doc、About、SearchMode）
│   └── loader/          # Markdown 加载与解析
├── models/              # GORM 数据库模型
├── pkg/                 # JWT 工具
└── content/             # Markdown 内容源
```
