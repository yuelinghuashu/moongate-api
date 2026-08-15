---
title: 自托管 Umami 分析服务与 Nuxt 4 项目集成指南（扩展篇）
description: 在现有 Docker 生产环境中集成自托管的 Umami 分析服务，通过 Caddy 自动 HTTPS 和 GitHub Actions 实现 Nuxt 4 项目的自动化数据跟踪。
date: 2026-03-18
permalink: 89857e17-36c9-4a7b-a879-43a1f64f54a6
series: deployment
level: P4
tags: 
  - Nuxt
  - Docker
  - Caddy
  - Engineering
---

## 📚 系列导航

本系列共六篇：

1. [**静态网站自动化部署（静态篇）**](./static-site-auto-deploy) —— 纯前端静态资源自动化发布
2. [**动态网站自动化部署（动态篇）**](dynamic-site-auto-deploy) —— 后端服务进程管理 + Caddy 反向代理
3. [**Docker 极简入门（入门篇）**](docker-quickstart-auto-deploy) —— 从零搭建 Docker CI/CD 流水线
4. [**Docker 生产级部署（进阶篇）**](docker-production-auto-deploy) —— 多容器编排与生产级可靠性
5. [**自托管 Umami 分析服务（扩展篇）**](./umami-integration-auto-deploy) —— Docker 环境集成分析服务
6. [**VitePress 文档站子域名部署（扩展篇）**](./vitepress-docker-existing-infrastructure-subdomain-deployment) —— 静态文档站接入现有 Docker 基础设施

本篇将在进阶篇的基础上，详细讲解如何将 Umami 分析服务集成到现有 Docker 化部署的 Nuxt 项目中。

---

## 📌 版本声明

Node.js、pnpm、Docker Engine、Docker Compose、Caddy、GitHub Actions 的版本信息与[入门篇](./docker-quickstart-auto-deploy)一致。本文额外涉及：

| 组件           | 版本              | 说明                                                        |
| -------------- | ----------------- | ----------------------------------------------------------- |
| Nuxt           | 4.x               | 前端框架，兼容 Nuxt 3                                       |
| nuxt-umami     | 3.2.1             | Umami 集成模块                                              |
| PostgreSQL     | alpine 最新       | Umami 数据库                                                |
| Umami          | postgresql-latest | 分析服务（生产环境建议固定具体版本，如 `postgresql-3.0.3`） |

---

## 🎯 最终目标

- 在现有 Docker Compose 环境中添加 Umami 服务（应用 + PostgreSQL 数据库）。
- 通过 Caddy 自动 HTTPS 暴露 `umami.你的域名.com`。
- 环境变量安全注入，数据持久化。
- 在 Nuxt 项目中通过 `nuxt-umami` 模块自动加载跟踪脚本，并在每次部署时自动更新配置。
- 强化生产环境安全，提供 IP 白名单、防火墙、备份等建议。

---

## 🏗️ 系统架构图

```text
┌─────────────┐ ┌─────────────────┐ ┌─────────────────┐
│ 本地开发 │────▶│ GitHub Actions │────▶│ 阿里云 ACR │
└─────────────┘ └─────────────────┘ └─────────────────┘
│
▼
┌───────────────────────────────────────────────────────┐
│ 阿里云 ECS │
│ ┌─────────────┐ ┌─────────────┐ ┌─────────────┐ │
│ │ PostgreSQL │◀──▶│ Umami │◀──▶│ Caddy │ │
│ │ (umami-db) │ │ (umami) │ │ (caddy) │ │
│ └─────────────┘ └─────────────┘ └─────────────┘ │
│ ▲ ▲ ▲ │
│ └─────────────────┼──────────────────┘ │
│ 同一网络 `app-network` │
└───────────────────────────────────────────────────────┘
```

**说明**：网络名 `app-network` 与进阶篇保持一致，用户可根据实际情况自定义，但需确保所有服务在同一网络。

---

## 📦 前置准备

1. **完成进阶篇**，已有基于 Docker Compose 的 Nuxt 项目生产环境（含 Caddy、PostgreSQL、应用容器），项目名统一为 `my-app`，网络名为 `app-network`。
2. **一个子域名**（例如 `umami.your-domain.com`）已添加 A 记录指向服务器 IP。
3. **阿里云 ACR** 已配置好命名空间和固定密码。
4. **服务器安全组**开放 `80`、`443` 端口。
5. **GitHub Secrets** 已包含进阶篇所需的所有变量（数据库密码、应用密钥等）。

---

## 🚀 第一部分：Docker Compose 中添加 Umami 服务

编辑服务器上的 `/var/www/my-app/docker-compose.yml`，在 `services` 段末尾添加 Umami 及其数据库。**注意**：如果原文件中已定义 `networks` 和 `volumes`，请合并而非重复添加。

<details>
<summary>点击展开完整代码</summary>

```yaml
name: my-app

services:
  # 原有服务（postgres, app, caddy）保持不变，此处省略...
  # 注意：请将以下服务名“app”替换为您实际的主应用服务名（应与进阶篇一致）

  umami-db:
    image: postgres:alpine
    container_name: my-app-umami-db
    restart: always
    environment:
      POSTGRES_DB: ${UMAMI_DB_NAME}
      POSTGRES_USER: ${UMAMI_DB_USER}
      POSTGRES_PASSWORD: ${UMAMI_DB_PASSWORD}
    volumes:
      - umami_db_data:/var/lib/postgresql/data
    networks:
      - app-network
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${UMAMI_DB_USER} -d ${UMAMI_DB_NAME}"]
      interval: 10s
      timeout: 5s
      retries: 5
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"

  umami:
    image: ghcr.io/umami-software/umami:postgresql-latest # 生产环境建议固定版本，如 postgresql-3.0.3
    container_name: my-app-umami
    restart: always
    depends_on:
      umami-db:
        condition: service_healthy
    environment:
      DATABASE_URL: postgresql://${UMAMI_DB_USER}:${UMAMI_DB_PASSWORD}@umami-db:5432/${UMAMI_DB_NAME}
      DATABASE_TYPE: postgresql
      APP_SECRET: ${UMAMI_APP_SECRET}
    networks:
      - app-network
    healthcheck:
      test:
        [
          "CMD",
          "node",
          "-e",
          "require('http').get('http://localhost:3000', (r) => {process.exit(r.statusCode === 200 ? 0 : 1)})",
        ]
      interval: 30s
      timeout: 5s
      retries: 3
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"

networks:
  app-network:
    driver: bridge
    # 如果原文件已定义，此处无需重复

volumes:
  postgres_data:
  caddy_data:
  caddy_config:
  umami_db_data:
  # 如果原文件已定义相应卷，此处只需追加 umami_db_data
```

</details>

**关键点**：

- 所有服务使用同一网络 `app-network`，通过服务名通信（`umami-db` 和 `umami`）。
- 数据库和应用均配置健康检查，确保启动顺序。
- 日志切割防止磁盘爆满。
- `APP_SECRET` 用于加密会话，必须为足够长的随机字符串（建议使用十六进制生成，见下文）。
- 生产环境应避免使用 `latest` 标签，建议固定具体版本（如 `postgresql-3.0.3`），以确保稳定性。

---

## 🌐 第二部分：Caddy 子域名配置

编辑 `/var/www/my-app/Caddyfile`，添加 `umami.your-domain.com` 配置块。请将 `your-domain.com` 替换为实际域名，并将 `reverse_proxy` 目标指向 Umami 容器服务名 `umami:3000`（而非主应用）。

```caddy
umami.your-domain.com {
    reverse_proxy umami:3000   # 关键：指向 umami 容器
    encode gzip zstd
    header {
        X-Content-Type-Options "nosniff"
        X-Frame-Options "DENY"
        X-XSS-Protection "1; mode=block"
        Referrer-Policy "strict-origin-when-cross-origin"
        Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"
    }
}
```

**说明**：访问控制（如 IP 白名单）将在安全优化部分单独添加，此处仅配置基本代理和安全头。

保存后重启 Caddy 容器使配置生效：

```bash
cd /var/www/my-app
docker compose restart caddy
```

---

## 🔐 第三部分：环境变量与密钥管理

### 3.1 生成 Umami 所需密码和密钥

在服务器上执行以下命令生成强密码（**推荐使用十六进制，避免特殊字符**）：

```bash
# 生成数据库密码（32位十六进制）
openssl rand -hex 16
# 生成 APP_SECRET（64位十六进制）
openssl rand -hex 32
```

> **注意**：使用 `-hex` 生成的字符串只包含 `0-9a-f`，可安全用于环境变量，无需担心 shell 转义问题。

### 3.2 更新服务器 `.env` 文件

编辑 `/var/www/my-app/.env`，添加以下变量（使用生成的值替换占位符）：

```bash
# Umami 配置
UMAMI_DB_NAME=umami
UMAMI_DB_USER=umami
UMAMI_DB_PASSWORD=your-generated-db-password
UMAMI_APP_SECRET=your-generated-app-secret

# 用于 Nuxt 的环境变量（稍后会在 CI 中写入）
NUXT_PUBLIC_UMAMI_ID=待填入
NUXT_PUBLIC_UMAMI_HOST=https://umami.your-domain.com
```

保存后设置权限：

```bash
chmod 600 /var/www/my-app/.env
```

### 3.3 在 GitHub Secrets 中添加变量

进入 GitHub 仓库 → **Settings** → **Secrets and variables** → **Actions**，添加以下 Secrets：

| Secret 名称              | 说明                            |
| ------------------------ | ------------------------------- |
| `UMAMI_DB_NAME`          | 固定为 `umami`                  |
| `UMAMI_DB_USER`          | 固定为 `umami`                  |
| `UMAMI_DB_PASSWORD`      | 生成的数据库密码                |
| `UMAMI_APP_SECRET`       | 生成的 APP_SECRET               |
| `NUXT_PUBLIC_UMAMI_ID`   | 稍后从 Umami 后台获取（暂留空） |
| `NUXT_PUBLIC_UMAMI_HOST` | `https://umami.your-domain.com` |

---

## 🚀 第四部分：首次启动 Umami 并获取 Website ID

> **重要提示**：本步骤需**先手动执行一次**，获取 Website ID 后更新 GitHub Secrets，再触发 CI 部署包含该 ID 的应用。不能在一次 CI 中完成所有步骤。

### 4.1 启动 Umami 服务

```bash
cd /var/www/my-app
docker compose up -d umami-db umami
docker compose logs -f umami   # 观察日志，等待启动成功（看到 "Ready in" 字样）
```

### 4.2 访问后台并添加网站

- 浏览器打开 `https://umami.your-domain.com`。
- 默认登录账号：`admin`，密码：`umami`。
  > ⚠️ **立即修改默认密码**：登录后进入 **Settings** → **Profile**，将密码更改为强密码。
- 点击 **Settings** → **Websites** → **Add Website**，填写：
  - Name：`Your Site Name`（如 `My Blog`）
  - Domain：`your-domain.com`（主站域名）
- 保存后，复制生成的 **Website ID**（UUID 格式）。

### 4.3 更新 GitHub Secrets

将复制的 Website ID 填入 GitHub Secrets 中的 `NUXT_PUBLIC_UMAMI_ID`。

---

## 🧩 第五部分：Nuxt 项目集成 `nuxt-umami`

### 5.1 安装模块

在本地项目根目录执行：

```bash
pnpm add nuxt-umami
# 或使用 nuxi 添加
pnpx nuxi@latest module add nuxt-umami
```

### 5.2 配置 `nuxt.config.ts`

```typescript
export default defineNuxtConfig({
  modules: ["nuxt-umami"],
  umami: {
    id: process.env.NUXT_PUBLIC_UMAMI_ID,
    host: process.env.NUXT_PUBLIC_UMAMI_HOST,
    autoTrack: true,
    // 可选：仅在非开发环境启用
    enabled: process.env.NODE_ENV !== "development",
  },
  // 其他配置...
});
```

### 5.3 本地测试（可选）

在项目根目录创建 `.env` 文件（**不提交 Git**）：

```bash
NUXT_PUBLIC_UMAMI_ID=your-website-id
NUXT_PUBLIC_UMAMI_HOST=https://umami.your-domain.com
```

运行 `pnpm dev`，访问 `http://localhost:3000`，打开开发者工具 → Network，应能看到 Umami 脚本请求。

---

## 🔧 第六部分：Dockerfile 必须接收构建参数

**关键**：`NUXT_PUBLIC_*` 变量在构建时被嵌入客户端代码，必须在 Docker 构建阶段通过 `ARG` 和 `ENV` 传递。

编辑项目根目录的 `Dockerfile`，确保包含以下内容：

```dockerfile
# 构建阶段
FROM node:24-alpine AS builder

# 接收所有 NUXT_PUBLIC_* 变量
ARG NUXT_PUBLIC_SITE_URL
ENV NUXT_PUBLIC_SITE_URL=$NUXT_PUBLIC_SITE_URL

ARG NUXT_PUBLIC_UMAMI_ID
ENV NUXT_PUBLIC_UMAMI_ID=$NUXT_PUBLIC_UMAMI_ID

ARG NUXT_PUBLIC_UMAMI_HOST
ENV NUXT_PUBLIC_UMAMI_HOST=$NUXT_PUBLIC_UMAMI_HOST

# 其余构建步骤保持不变...
```

**提醒**：若后续增加其他 `NUXT_PUBLIC_*` 变量，必须同步添加 `ARG` 和 `ENV`。

---

## ⚙️ 第七部分：GitHub Actions 工作流完善

### 7.1 在 `build-push` 步骤中传递构建参数

编辑 `.github/workflows/deploy.yml`，在 `docker/build-push-action` 步骤的 `build-args` 中添加 Umami 变量：

```yaml
- name: Build and push Docker image
  uses: docker/build-push-action@v5
  with:
    context: .
    push: true
    build-args: |
      NUXT_PUBLIC_SITE_URL=${{ secrets.NUXT_PUBLIC_SITE_URL }}
      NUXT_PUBLIC_UMAMI_ID=${{ secrets.NUXT_PUBLIC_UMAMI_ID }}
      NUXT_PUBLIC_UMAMI_HOST=${{ secrets.NUXT_PUBLIC_UMAMI_HOST }}
    tags: |
      ${{ secrets.ACR_REGISTRY }}/my-app:latest
      ${{ secrets.ACR_REGISTRY }}/my-app:${{ github.sha }}
```

### 7.2 在 SSH 部署脚本中写入 `.env` 文件

在[进阶篇](./docker-production-auto-deploy)的 `appleboy/ssh-action` 步骤基础上，**新增以下 Umami 变量**（`env` 和 `envs` 两部分都需添加）。`SCRIPT` 中基础变量写入逻辑与进阶篇相同，只需在 `cat > .env` 中**追加 Umami 变量段落**，并在拉取/重启命令中**追加 umami 服务**：

```yaml
# 新增到 env（与进阶篇原有变量并列）
UMAMI_DB_NAME: ${{ secrets.UMAMI_DB_NAME }}
UMAMI_DB_USER: ${{ secrets.UMAMI_DB_USER }}
UMAMI_DB_PASSWORD: ${{ secrets.UMAMI_DB_PASSWORD }}
UMAMI_APP_SECRET: ${{ secrets.UMAMI_APP_SECRET }}
NUXT_PUBLIC_UMAMI_ID: ${{ secrets.NUXT_PUBLIC_UMAMI_ID }}
NUXT_PUBLIC_UMAMI_HOST: ${{ secrets.NUXT_PUBLIC_UMAMI_HOST }}

# envs 列表在原有基础上追加：
# UMAMI_DB_NAME, UMAMI_DB_USER, UMAMI_DB_PASSWORD, UMAMI_APP_SECRET,
# NUXT_PUBLIC_UMAMI_ID, NUXT_PUBLIC_UMAMI_HOST

# script 中的差异化部分：
cat > .env << EOF
# (原有变量，与进阶篇相同：POSTGRES_*、ACR_REGISTRY、NUXT_*)

# Umami 变量（追加）
UMAMI_DB_NAME=$UMAMI_DB_NAME
UMAMI_DB_USER=$UMAMI_DB_USER
UMAMI_DB_PASSWORD=$UMAMI_DB_PASSWORD
UMAMI_APP_SECRET=$UMAMI_APP_SECRET
NUXT_PUBLIC_UMAMI_ID=$NUXT_PUBLIC_UMAMI_ID
NUXT_PUBLIC_UMAMI_HOST=$NUXT_PUBLIC_UMAMI_HOST
EOF

chmod 600 .env

# ★ 差异化：额外拉取 umami 镜像
docker compose pull umami

# ★ 差异化：可选手动更新 Umami 容器（会导致短暂停机）
# docker compose up -d --force-recreate umami
```

**说明**：

- 脚本中 `app` 请替换为您实际的主应用服务名。
- `envs` 列表使用逗号分隔在一行内，避免换行导致解析错误。
- 每次更新 `NUXT_PUBLIC_UMAMI_ID` 或 `UMAMI_*` 变量时，需确保它们已包含在 `envs` 和 `env` 部分中。

---

## ✅ 第八部分：验证集成

### 8.1 检查网络请求

- 访问 `https://your-domain.com`，打开开发者工具 → **Network** 标签，刷新页面。
- 过滤 `umami` 或 `api/send`，应能看到：
  - 一个指向 `https://umami.your-domain.com/script.js` 的 GET 请求（加载跟踪脚本）。
  - 一个指向 `https://umami.your-domain.com/api/send` 的 POST 请求（发送页面视图数据）。

### 8.2 查看 Umami 后台实时数据

登录 `https://umami.your-domain.com`，进入 **Realtime** 页面，应显示当前访问记录。

### 8.3 验证 `window.umami` 对象

在浏览器控制台输入 `window.umami`，应返回包含 `track`、`identify` 等方法的对象。

---

## 🔒 第九部分：安全优化建议

### 9.1 限制 Umami 子域名访问范围

#### 9.1.1 IP 白名单（推荐）

如果仅允许特定 IP（如家庭宽带）访问 Umami 后台，可在 Caddy 中添加 IP 白名单。

编辑 Caddyfile，修改 `umami.your-domain.com` 块：

```caddy
umami.your-domain.com {
    @allowed remote_ip 192.0.2.1 2001:db8::1 127.0.0.1
    handle @allowed {
        reverse_proxy umami:3000   # 指向 umami 容器
    }
    handle {
        respond "Access Denied" 403
    }

    encode gzip zstd
    header { ... }
}
```

将 `192.0.2.1` 和 `2001:db8::1` 替换为实际家庭或办公室的公网 IP（IPv4 和 IPv6）。注意家庭宽带 IP 可能变化，建议配合 DDNS 或定期更新。

#### 9.1.2 基础认证（可选）

若需在移动网络下访问，可启用 Caddy 的 `basicauth`，但需注意与 Umami 自身登录页面的潜在冲突。正确配置方法：

1. 生成密码哈希（使用 `caddy hash-password` 命令）：

   ```bash
   docker exec my-app-caddy caddy hash-password --plaintext 'your-password'
   ```

   **注意**：将命令输出的字符串用**单引号**包裹后填入 Caddyfile，例如 `admin '$2a$...'`，以防止 `$` 被解析为环境变量。

2. 在 Caddyfile 中添加：

```caddy
umami.your-domain.com {
    basicauth * {
        admin '$2a$14$...'   # 使用单引号包裹哈希值
    }
    reverse_proxy umami:3000 {
        header_up -Authorization  # 移除 Authorization 头，避免干扰 Umami 会话
    }
    # ... 其他配置
}
```

**警告**：这会导致浏览器先弹出 Basic Auth 对话框，通过后再显示 Umami 登录页，可能造成体验不佳。建议仅作为临时方案或与 IP 白名单结合使用。

### 9.2 数据库安全

- 使用强密码（已使用 `openssl rand -hex` 生成）。
- Umami 数据库仅对内部网络暴露，无需映射端口。
- 定期备份数据库（确保 `backups` 目录存在）：

  ```bash
  mkdir -p /var/www/my-app/backups
  docker exec my-app-umami-db pg_dump -U umami umami > /var/www/my-app/backups/umami_$(date +%Y%m%d).sql
  ```

### 9.3 Caddy 安全头增强

在 Caddyfile 中添加更严格的 CSP 头（需根据实际资源调整，生产环境建议移除 `'unsafe-inline'` 并采用 nonce 或 hash 策略）：

```caddy
header {
    Content-Security-Policy "default-src 'self'; script-src 'self' https://umami.your-domain.com; style-src 'self'; img-src 'self' data:; connect-src 'self' https://umami.your-domain.com;"
    # 其他头...
}
```

### 9.4 日志配置与轮转

Caddy 日志可输出到文件并配置轮转。需提前创建日志目录并设置权限（Caddy 容器内用户 UID 为 1000）：

```bash
sudo mkdir -p /var/log/caddy
sudo chown 1000:1000 /var/log/caddy
```

然后在 Caddyfile 中添加：

```caddy
log {
    output file /var/log/caddy/umami-access.log {
        roll_size 10MB
        roll_keep 5
    }
}
```

若不想处理文件权限，可直接输出到标准输出（由 Docker 收集）。

### 9.5 防火墙配置

- 在阿里云安全组中，仅开放 80、443 端口给公网，22 端口限制为特定管理 IP。
- 在服务器内部使用 `ufw` 进一步限制：

  ```bash
  sudo ufw default deny incoming
  sudo ufw default allow outgoing
  sudo ufw allow 22/tcp  # 建议限制来源 IP
  sudo ufw allow 80/tcp
  sudo ufw allow 443/tcp
  sudo ufw enable
  ```

---

## 🧪 第十部分：深度问题排查手册

| 现象                                                                   | 可能原因                                                   | 解决方案                                                                                                                                                         |
| ---------------------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Umami 容器不断重启，日志显示 `password authentication failed`**      | 数据库密码与 `.env` 不一致，或密码含特殊字符               | 检查 `.env` 中 `UMAMI_DB_PASSWORD` 是否匹配；使用字母数字密码；删除卷重建：`docker compose down -v umami-db umami && docker compose up -d umami-db umami`        |
| **访问 `umami.your-domain.com` 返回 502**                              | Umami 容器未就绪，或 Caddy 代理配置错误                    | 检查 `docker compose ps umami` 状态；查看 Caddy 日志；测试内部连通性：`docker exec my-app-caddy wget -O- http://umami:3000`                                      |
| **浏览器中 Umami 脚本未加载，Network 中无请求**                        | 构建时未传递 `NUXT_PUBLIC_UMAMI_*` 变量                    | 检查 Dockerfile 是否包含对应的 `ARG`/`ENV`；检查 GitHub Actions 日志中 `build-args` 是否传递                                                                     |
| **后台无数据，但脚本已加载**                                           | Website ID 错误，或广告拦截器阻止                          | 核对 `NUXT_PUBLIC_UMAMI_ID`；在无痕模式下测试                                                                                                                    |
| **本地开发环境报 `id is missing`**                                     | 本地未设置环境变量                                         | 可忽略，或使用 `enabled: process.env.NODE_ENV !== 'development'` 禁用                                                                                            |
| **IP 白名单不生效，所有 IP 均可访问**                                  | Caddy 未重新加载配置 / `remote_ip` 匹配器语法错误          | 重启 Caddy 容器；检查 `@allowed` 定义中 IP 格式是否正确                                                                                                          |
| **Basic Auth 配置后页面转圈**                                          | `Authorization` 头干扰 Umami 会话 / 浏览器缓存             | 添加 `header_up -Authorization` 到 `reverse_proxy`；清除浏览器缓存                                                                                               |
| **Caddy 无法写入日志文件**                                             | 宿主机目录权限不足                                         | 确保目录存在且 UID 1000 有写权限：`sudo chown 1000:1000 /var/log/caddy`                                                                                          |

---

## 🛠️ 第十一部分：日常运维

### 11.1 常用命令

`docker compose ps`、`logs -f`、`exec` 等基础命令请参见[入门篇 附录](./docker-quickstart-auto-deploy)。本文特有的运维命令：

```bash
# 备份 Umami 数据库（确保 backups 目录存在）
mkdir -p backups
docker exec my-app-umami-db pg_dump -U umami umami > backups/umami_$(date +%Y%m%d).sql

# 恢复数据库
cat backups/umami_20260318.sql | docker exec -i my-app-umami-db psql -U umami -d umami

# 手动更新 Umami 镜像（拉取最新版本并重启）
docker compose pull umami && docker compose up -d umami
```

### 11.2 自动备份（可选）

添加定时任务（crontab -e）：

```bash
0 3 * * * cd /var/www/my-app && mkdir -p backups && docker exec my-app-umami-db pg_dump -U umami umami > backups/umami_$(date +\%Y\%m\%d).sql
```

---

## 🏁 总结

通过本指南，读者成功实现了：

- 在现有 Docker 生产环境中自托管 Umami 分析服务。
- 通过 Caddy 自动 HTTPS 暴露子域名。
- 环境变量安全注入与 CI/CD 自动化部署。
- Nuxt 项目正确集成 `nuxt-umami` 模块，并在构建时传递必要变量。
- 生产级安全加固措施，保障服务仅被授权访问。

现在，网站访问数据将被清晰记录，且整个过程完全自动化，无需人工干预。后续如需升级 Umami 或修改配置，只需修改 `docker-compose.yml` 中的镜像标签或环境变量，重新部署即可。

**核心维护要点**：

- 定期更新 Umami 镜像以获取安全补丁（建议测试后再更新）。
- 监控日志和磁盘使用情况。
- 若家庭 IP 变化，及时更新 IP 白名单。
- 妥善保管 `.env` 文件和 GitHub Secrets。
- 确保 `envs` 列表始终包含所有新增的环境变量，避免部署时丢失。
