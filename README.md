# MoonGate API

个人博客后端服务，为 [MoonGate](https://moongate.top) 前端提供内容管理、GitHub OAuth 认证和用户管理 API。

## 技术栈

- **语言**: Go 1.26
- **Web 框架**: Gin
- **ORM**: GORM
- **数据库**: PostgreSQL
- **认证**: GitHub OAuth + JWT (HS256)
- **内容处理**: Markdown + Frontmatter → HTML

## 快速开始

### 1. 环境变量

复制 `.env.example` 为 `.env` 并填写：

```bash
cp .env.example .env
```

### 2. 本地运行

```bash
# 需要本机 PostgreSQL 和 .env 配置
make run
```

### 3. 常用命令

```bash
make dev          # 本地开发运行
make build        # 编译二进制
make test         # 运行全部测试（详细输出）
make check        # 代码检查 + 全部测试（与 CI 一致）
make docker-build # 构建 Docker 镜像
```

## API 端点

### 内容 API（公开）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/docs` | 文档列表（支持分页、搜索、标签、等级筛选） |
| GET | `/api/docs/:slug` | 文档详情 |
| GET | `/api/about` | 关于页面列表 |
| GET | `/api/about/:slug` | 关于页面详情 |

### 认证 API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/auth/github` | 发起 GitHub OAuth 登录 |
| GET | `/auth/github/callback` | GitHub OAuth 回调 |

### 用户 API（需要认证）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/users/me` | 获取当前登录用户信息（Bearer token） |

### 其他

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/health` | 健康检查 |

## 内容管理

内容以 Markdown 文件存储在 `content/` 目录：

- `content/docs/*.md` — 技术文章
- `content/about/*.md` — 关于页面

### Frontmatter 格式

```yaml
---
title: "文章标题"
description: "简短摘要"
date: 2025-01-15 10:00:00
permalink: UUID
level: P2            # P1-P5 难度等级
series: "系列名称"    # 可选
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

```
├── main.go              # 入口：环境变量、DB、路由注册
├── db/                  # PostgreSQL 连接
├── internal/
│   ├── api/             # HTTP handlers + 中间件
│   ├── domain/          # 领域模型（Doc、About、SearchMode）
│   └── loader/          # Markdown 加载与解析
├── models/              # GORM 数据库模型
├── pkg/                 # JWT 工具
└── content/             # Markdown 内容源
