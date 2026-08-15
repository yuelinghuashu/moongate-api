---
title: VitePress 文档站接入已有 Docker 基础设施：子域名部署（扩展篇）
description: 本文记录如何将 VitePress 文档站部署为子域名，并接入已有 docker-compose 管理的动态站点（如 Nuxt 博客），共用同一 Caddy 反向代理。
date: 2026-06-07
permalink: 177f4b4d-186b-4162-8969-930042f7b804
series: deployment
level: P4
tags:
  - Caddy
  - Docker
  - CI/CD
---

> 本文记录如何将 VitePress 文档站部署为子域名，并接入已有 docker-compose 管理的动态站点（如 Nuxt 博客），共用同一 Caddy 反向代理。

## 📚 系列导航

本系列共六篇：

1. [**静态网站自动化部署（静态篇）**](./static-site-auto-deploy) —— 纯前端静态资源自动化发布
2. [**动态网站自动化部署（动态篇）**](dynamic-site-auto-deploy) —— 后端服务进程管理 + Caddy 反向代理
3. [**Docker 极简入门（入门篇）**](docker-quickstart-auto-deploy) —— 从零搭建 Docker CI/CD 流水线
4. [**Docker 生产级部署（进阶篇）**](docker-production-auto-deploy) —— 多容器编排与生产级可靠性
5. [**自托管 Umami 分析服务（扩展篇）**](./umami-integration-auto-deploy) —— Docker 环境集成分析服务
6. [**VitePress 文档站子域名部署（扩展篇）**](./vitepress-docker-existing-infrastructure-subdomain-deployment) —— 静态文档站接入现有 Docker 基础设施

## 📌 前置说明

**本文的部署环境**：

- **主站点**：Nuxt 构建的个人博客（`moongate.top`），通过 docker-compose 管理（PostgreSQL + Nuxt + Caddy）
- **文档站**：VitePress 构建的组件库文档，部署到子域名 `vue.moongate.top`
- **目标**：将文档站容器接入现有的 Caddy 反向代理，复用同一套 Docker 网络和域名基础设施

**如果你是从零开始部署**（没有现有 Caddy、没有 docker-compose）：

- 本文部分内容（如网络连接）可简化
- 直接 `docker run -p 80:80` 即可运行

本文的核心价值在于 **VitePress 静态站如何与现有 Docker 基础设施整合**，而非从零搭建。

> **关于静态文件目录**：本文沿用系列教程的自定义目录风格（`/docs`）。如果你习惯使用 Caddy 默认目录（`/srv` 或 `/usr/share/caddy`），可相应调整，不影响最终效果。

## 🎯 适用场景

- ✅ 已有 docker-compose 管理的动态站点（如 Nuxt 博客），需要将静态文档站作为子域名接入
- ✅ 希望多个站点共用同一 Caddy 反向代理和 HTTPS 证书
- ✅ 使用 Docker 部署静态网站，但不熟悉网络配置
- ✅ 遇到 pnpm 供应链安全检查报错，需要解决方案
- ✅ 需要将 VitePress 文档站接入自动化部署

## 📌 版本声明

Node.js、pnpm、Docker、Caddy、GitHub Actions 的版本信息与[入门篇](./docker-quickstart-auto-deploy)一致。本文额外涉及：

| 工具      | 版本  | 说明         |
| --------- | ----- | ------------ |
| VitePress | 1.6.x | 文档生成器   |

## 🏗️ 系统架构（接入现有基础设施）

> **背景**：本文档站部署在已有 Nuxt 博客的基础设施之上。主站点 `moongate.top` 由 Nuxt + PostgreSQL + Caddy 组成，通过 docker-compose 管理。文档站作为子域名 `vue.moongate.top` 接入同一 Caddy。
> **架构说明**：实际运行中存在两个 Caddy：
>
> - **网关 Caddy**（docker-compose 管理）：接收外部 HTTPS 请求，反向代理到内部服务
> - **文档站 Caddy**（`moongate-vue` 容器内）：仅负责服务静态文件，监听 80 端口
>
> 由于两者在同一 Docker 网络中，网关 Caddy 通过 `reverse_proxy moongate-vue:80` 直接通信，无需暴露端口到宿主机。容器间通信为内网明文 HTTP（80 端口），外层 HTTPS 证书由网关 Caddy 自动托管解析。

```text
┌─────────────┐ ┌─────────────────┐ ┌─────────────────┐
│ 本地开发 │────▶│ GitHub Actions │────▶│ 阿里云 ACR │
│ git push │ │ 自动构建镜像 │ │ 镜像仓库 │
└─────────────┘ └─────────────────┘ └─────────────────┘
│
▼
┌─────────────────────────────────────────────────────────────────┐
│ 阿里云 ECS 服务器 │
│ ┌───────────────────────────────────────────────────────────┐ │
│ │ Docker 网络: my-blog_blog-network │ │
│ │ ┌─────────────┐ ┌─────────────┐ ┌─────────────┐ │ │
│ │ │ Caddy │ │ Nuxt 应用 │ │ PostgreSQL │ │ │
│ │ │ (网关代理) │◀──▶│ (博客) │◀──▶│ (数据库) │ │ │
│ │ └──────┬──────┘ └─────────────┘ └─────────────┘ │ │
│ │ │ │ │
│ │ │ ┌─────────────────────────────────────────┐ │ │
│ │ └──│ moongate-vue │ │ │
│ │ │ (VitePress + 内部 Caddy 静态服务) │ │ │
│ │ └─────────────────────────────────────────┘ │ │
│ └───────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

## 🔄 与动态应用的核心差异

| 维度           | Nuxt 动态应用            | VitePress 静态站                |
| -------------- | ------------------------ | ------------------------------- |
| **构建产物**   | `.output` 服务端代码     | 纯静态 HTML/CSS/JS              |
| **运行环境**   | Node.js 运行时           | 静态文件服务器                  |
| **基础镜像**   | `node:alpine`            | `caddy:alpine`                  |
| **端口映射**   | 需要暴露端口             | 内部网络访问，无需端口          |
| **数据库**     | PostgreSQL               | 无                              |
| **环境变量**   | 多个敏感配置             | 无                              |
| **管理方式**   | docker-compose 编排      | 独立 `docker run` + 网络连接    |
| **Caddy 代理** | `reverse_proxy app:3000` | `reverse_proxy moongate-vue:80` |

## 🐳 第一步：Dockerfile 编写

VitePress 是静态站点生成器，构建后输出纯静态文件。因此 Dockerfile 分为两个阶段：

```bash
# ==================== 构建阶段 ====================
FROM node:24-alpine AS builder

WORKDIR /app

# 安装 git（VitePress 1.x 需要 git 来获取最后更新时间）
RUN apk add --no-cache git

# 安装 pnpm
RUN corepack enable && corepack prepare pnpm@latest --activate

# 复制依赖文件
COPY package.json pnpm-lock.yaml ./

# ⚠️ 关键点：使用 --ignore-scripts 跳过 pnpm 的供应链安全检查
# 否则会触发 ERR_PNPM_MINIMUM_RELEASE_AGE_VIOLATION
RUN pnpm install --frozen-lockfile --ignore-scripts

# 复制源代码并构建
COPY . .
RUN pnpm docs:build

# ==================== 运行阶段 ====================
FROM caddy:alpine

# 复制构建产物到自定义目录 /docs
# 注意：需要在 Caddyfile 中通过 root * /docs 指定根目录
COPY --from=builder /app/docs/.vitepress/dist /docs

EXPOSE 80
```

### ⚠️ 关键踩坑点

#### 1. Git 依赖

VitePress 1.x 的默认主题会调用 `git` 命令获取文件的最后更新时间。如果容器中没有 git，构建会报错：

```bash
[vitepress] spawn git ENOENT
```

**解决方案**：在构建阶段安装 git：

```bash
RUN apk add --no-cache git
```

#### 2. pnpm 供应链安全检查

pnpm 10.x 默认启用供应链安全检查，拦截发布时间 < 24 小时的包：

```bash
[ERR_PNPM_MINIMUM_RELEASE_AGE_VIOLATION] @types/node@25.9.2 was published ... within the minimumReleaseAge cutoff
[ERR_PNPM_IGNORED_BUILDS] Ignored build scripts: esbuild@0.21.5, vitepress-theme-demoblock@3.1.3
```

**解决方案**：使用 `--ignore-scripts` 参数跳过检查：

```bash
RUN pnpm install --frozen-lockfile --ignore-scripts
```

#### 3. 文件权限

Caddy 官方镜像默认以 `caddy` 非 root 用户运行（安全考虑）。如果构建产物权限不正确，容器启动后 Caddy 可能无法读取静态文件，导致 `403 Forbidden`。

**解决方案**：在 Caddyfile 中确保 `root` 目录可读，或使用 `--chown` 参数：

```bash
COPY --from=builder --chown=caddy:caddy /app/docs/.vitepress/dist /docs
```

如果你使用自定义目录 `/docs`，也可以在 Caddyfile 中正常配置 `root * /docs`，Caddy 会自动处理权限。

## 🚀 第二步：GitHub Actions 工作流

```yaml
name: Deploy Docs To Aliyun

on:
  push:
    branches: [main]
    paths:
      - "docs/**"
      - "package.json"
      - "pnpm-lock.yaml"
      - "Dockerfile"
      - ".github/workflows/deploy-docs.yml"

jobs:
  ci:
    runs-on: ubuntu-latest

    steps:
      - name: Checkout
        uses: actions/checkout@v6

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      # ⚙️ Login to ACR 步骤与[入门篇第四步](./docker-quickstart-auto-deploy)完全一致，此处省略。
      # 使用相同的 docker/login-action@v3 + ACR_REGISTRY/ACR_USERNAME/ACR_PASSWORD。

      - name: Build and push Docker image
        uses: docker/build-push-action@v5
        with:
          context: .
          file: Dockerfile
          push: true
          tags: |
            ${{ secrets.ACR_REGISTRY }}/moongate/moongate-vue:latest
            ${{ secrets.ACR_REGISTRY }}/moongate/moongate-vue:${{ github.sha }}
          cache-from: type=gha
          cache-to: type=gha,mode=max

      - name: Deploy to Server via SSH
        uses: appleboy/ssh-action@v1.0.0
        env:
          ACR_REGISTRY: ${{ secrets.ACR_REGISTRY }}
          ACR_USERNAME: ${{ secrets.ACR_USERNAME }}
          ACR_PASSWORD: ${{ secrets.ACR_PASSWORD }}
        with:
          host: ${{ secrets.SERVER_HOST }}
          username: ${{ secrets.SERVER_USER }}
          key: ${{ secrets.SSH_PRIVATE_KEY }}
          envs: ACR_REGISTRY, ACR_USERNAME, ACR_PASSWORD
          script: |
            set -e

            # 登录 ACR（与入门篇 ssh-action 相同）
            echo "$ACR_PASSWORD" | docker login "$ACR_REGISTRY" -u "$ACR_USERNAME" --password-stdin

            # ★ 差异化：独立 docker run 部署（不使用 docker-compose）
            # 拉取最新镜像
            docker pull $ACR_REGISTRY/moongate/moongate-vue:latest

            # 强制删除旧容器（避免容器挂起导致的死锁）
            docker rm -f moongate-vue || true

            # 运行新容器（连接到博客的 Docker 网络）
            docker run -d \
              --name moongate-vue \
              --restart unless-stopped \
              --network my-blog_blog-network \
              $ACR_REGISTRY/moongate/moongate-vue:latest

            # 清理旧镜像
            docker image prune -f --filter "until=24h"
```

### 与动态应用 workflow 的差异

| 差异点     | Nuxt 动态应用               | VitePress 静态站                |
| ---------- | --------------------------- | ------------------------------- |
| 触发路径   | 全量触发                    | 仅 `docs/**` 变更触发           |
| 构建参数   | 需要 `NUXT_PUBLIC_SITE_URL` | 无需                            |
| 部署脚本   | docker-compose              | 独立 `docker run` + `--network` |
| 环境变量   | 多个 Secrets 传递           | 无需                            |
| 数据库迁移 | 有                          | 无                              |
| 容器更新   | `docker compose up -d`      | `docker rm -f` + `docker run`   |

## 🌐 第三步：服务器端配置

### 3.1 首次部署：运行容器并连接网络

```bash
# 拉取镜像
docker pull crpi-xxx/moongate/moongate-vue:latest

# 运行容器（加入博客的网络）
docker run -d \
  --name moongate-vue \
  --restart unless-stopped \
  --network my-blog_blog-network \
  crpi-xxx/moongate/moongate-vue:latest
```

> **注意**：不需要 `-p` 端口映射。Caddy 通过 Docker 内部网络直接访问容器，不经过宿主机端口。

### 3.2 Caddyfile 配置

在 `/var/www/my-site/Caddyfile` 中添加子域名配置，并指定静态文件根目录：

```bash
# 组件库文档站
vue.moongate.top {
    reverse_proxy moongate-vue:80
    encode gzip zstd
}

# 可选：添加 www 重定向
www.vue.moongate.top {
    redir https://vue.moongate.top{uri} permanent
}
```

> **说明**：
>
> - 网关 Caddy 只负责反向代理，不负责静态文件服务
> - 静态文件服务由文档站容器内的 Caddy 负责
> - 容器间通信使用内网 HTTP，HTTPS 证书由网关 Caddy 统一托管

### 3.3 重启网关 Caddy

```bash
cd /var/www/my-site
docker compose restart caddy
```

### 3.4 验证网络连接

```bash
# 查看容器网络
docker inspect moongate-vue | grep -A5 "Networks"

# 从网关 Caddy 容器测试连接
docker exec my-blog-caddy wget -qO- http://moongate-vue:80 | head -5
```

## 🌍 第四步：DNS 配置

在阿里云 DNS 控制台添加 A 记录：

| 记录类型 | 主机记录 | 记录值             |
| -------- | -------- | ------------------ |
| A        | `vue`    | `你的服务器公网IP` |

等待 DNS 生效后，访问 `https://vue.moongate.top` 即可看到文档站。

## 🔧 第五步：完整配置清单

### GitHub Secrets

`SERVER_HOST`、`SERVER_USER`、`SSH_PRIVATE_KEY` 的配置方法与[静态篇 第二部分](./static-site-auto-deploy)一致。本文额外需要：

| Secret         | 说明                |
| -------------- | ------------------- |
| `ACR_REGISTRY` | 阿里云 ACR 仓库地址 |
| `ACR_USERNAME` | 阿里云用户名        |
| `ACR_PASSWORD` | ACR 固定密码        |

### 服务器端命令速查

```bash
# 首次部署：运行容器并连接网络
docker run -d \
  --name moongate-vue \
  --restart unless-stopped \
  --network my-blog_blog-network \
  crpi-xxx/moongate/moongate-vue:latest

# 常用运维命令
docker logs moongate-vue          # 查看日志
docker restart moongate-vue       # 重启容器
docker stop moongate-vue          # 停止容器
docker start moongate-vue         # 启动容器
docker rm -f moongate-vue          # 强制删除容器

# 网络管理
docker network connect my-blog_blog-network moongate-vue  # 连接网络
docker network disconnect my-blog_blog-network moongate-vue  # 断开网络
```

## 🐛 踩坑记录

| 问题                                     | 原因                            | 解决方案                                  |
| ---------------------------------------- | ------------------------------- | ----------------------------------------- |
| `spawn git ENOENT`                       | 容器中没有 git                  | `RUN apk add --no-cache git`              |
| `ERR_PNPM_MINIMUM_RELEASE_AGE_VIOLATION` | pnpm 供应链安全检查             | `--ignore-scripts`                        |
| `ERR_PNPM_IGNORED_BUILDS`                | 构建脚本被忽略                  | 同上                                      |
| Caddy 返回 403                           | 静态文件权限不足                | 使用 `--chown=caddy:caddy` 或检查目录权限 |
| Caddy 返回 502                           | 文档站容器未连接到 Caddy 的网络 | `docker network connect`                  |
| 域名无法访问                             | DNS 未配置                      | 添加 A 记录                               |
| Caddy 无法解析 `moongate-vue` 主机名     | 容器不在同一网络                | 使用 `--network` 参数运行                 |
| CI 部署时容器名冲突                      | 旧容器未完全清理                | 使用 `docker rm -f` 强制删除              |

## 📈 部署流程图

```text
┌─────────────────────────────────────────────────────────────────────┐
│                           开发者本地                                 │
│  $ git add docs/                                                     │
│  $ git commit -m "update docs"                                       │
│  $ git push origin main                                              │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│                        GitHub Actions                                │
│  1. 检出代码                                                         │
│  2. 安装 pnpm、依赖                                                   │
│  3. 执行 pnpm docs:build                                             │
│  4. 构建 Docker 镜像                                                 │
│  5. 推送到阿里云 ACR                                                  │
│  6. SSH 到服务器                                                      │
│  7. docker pull && docker rm -f && docker run --network ...         │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│                          阿里云 ECS                                   │
│  1. 拉取最新镜像                                                      │
│  2. 强制删除旧容器                                                    │
│  3. 启动新容器（加入博客网络）                                         │
│  4. 网关 Caddy 代理 vue.moongate.top → moongate-vue:80               │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│                           用户访问                                    │
│                    https://vue.moongate.top                          │
└─────────────────────────────────────────────────────────────────────┘
```

## 📊 与动态应用部署的优劣对比

| 维度       | VitePress 静态站    | Nuxt 动态应用       |
| ---------- | ------------------- | ------------------- |
| 部署复杂度 | ⭐ 极低             | ⭐⭐⭐⭐ 较高       |
| 构建时间   | ~30 秒              | ~2 分钟             |
| 镜像体积   | ~50 MB              | ~200 MB             |
| 运行时内存 | ~20 MB              | ~150 MB             |
| 启动时间   | <1 秒               | ~5 秒               |
| 端口配置   | 无需暴露端口        | 需要端口映射        |
| 网络配置   | 需连接到 Caddy 网络 | compose 自动管理    |
| 容器管理   | 独立 `docker run`   | docker-compose 编排 |
| 可维护性   | 极高                | 中等                |

## 🎉 总结

VitePress 文档站接入已有 Docker 基础设施的关键步骤：

1. **Dockerfile**：使用多阶段构建 + Caddy 静态服务器，注意：
   - git 依赖（VitePress 需要）
   - pnpm 供应链安全检查（`--ignore-scripts`）
   - 文件权限（可选 `--chown=caddy:caddy`）
2. **镜像推送**：推送到阿里云 ACR，利用 GitHub Actions 缓存加速
3. **容器部署**：使用 `docker rm -f` 强制替换，`--network` 接入现有网络
4. **Caddy 代理**：在网关 Caddyfile 中添加子域名配置，指定 `root * /docs`
5. **DNS 解析**：为子域名添加 A 记录

**架构亮点**：

- 双 Caddy 架构：网关 Caddy 处理 HTTPS 和路由，容器内 Caddy 仅服务静态文件
- 内网通信：容器间通过 Docker 内部网络通信，无需暴露端口到宿主机
- 统一证书管理：HTTPS 证书由网关 Caddy 统一托管，子域名自动继承
- 系列风格统一：沿用自定义目录 `/docs`，与之前教程保持一致

与动态应用不同，静态站部署更简单、资源占用更少。本文完整记录了接入已有 Docker 基础设施的全过程，这套流程同样适用于任何静态站点生成器（如 Hexo、Hugo、Astro 等），只需调整构建命令和静态文件输出目录即可。
