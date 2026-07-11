---
title: GitHub Actions + Caddy 全自动部署动态网站（动态篇）
description: 深入后端服务的进程管理、环境变量注入、数据库迁移，结合 Caddy 反向代理，打造完整的动态应用部署流水线。
permalink: 051f1e4a-1151-49c2-b61b-e3c43859332b
date: 2026-01-23
series: deployment
level: P4
tags: 
  - Caddy
  - CI/CD
---

## 📚 系列导航

本系列共六篇，覆盖从静态网站到生产级 Docker 部署及服务集成的全流程：

1. [**静态网站自动化部署（静态篇）**](./static-site-auto-deploy)
   —— 纯前端资源的自动化发布，Caddy 自动 HTTPS 和 SPA 路由支持。

2. [**动态网站自动化部署（动态篇）**](dynamic-site-auto-deploy)
   —— 后端服务进程管理、环境变量注入、数据库迁移，结合 Caddy 反向代理。

3. [**Docker 极简入门（入门篇）**](docker-quickstart-auto-deploy)
   —— 从零开始用 Docker + GitHub Actions 实现 CI/CD 流水线。

4. [**Docker 生产级部署（进阶篇）**](docker-production-auto-deploy)
   —— 多容器编排、健康检查、数据库迁移、自动 HTTPS，打造可靠的生产环境。

5. [**自托管 Umami 分析服务与 Nuxt 4 项目集成指南（扩展篇）**](./umami-integration-auto-deploy)
   —— 在现有 Docker 生产环境中集成 Umami 分析服务，实现自动化数据跟踪与安全加固。

6. [**VitePress 文档站接入已有 Docker 基础设施：子域名部署（扩展篇）**](./vitepress-docker-existing-infrastructure-subdomain-deployment)
   —— 将 VitePress 静态文档站作为子域名接入现有 Docker 基础设施，复用 Caddy 反向代理与网络。

---

> **适用技术栈**：本教程以 **Nuxt + Drizzle ORM + PostgreSQL** 为例，完整展示一个现代化动态网站的自动化部署流程。整体架构与配置方法同样适用于其他 Node.js 框架（Express、NestJS）或其他语言的技术栈，只需替换对应的构建、迁移、进程管理命令即可。

本教程将指导你搭建一套 **“代码推送即发布”** 的自动化部署系统，实现从本地 `git push` 到服务器服务热重启的全流程无人值守。

---

## 📌 版本声明

本文档所有工具均采用 **2026 年最新稳定版**，具体版本如下：

| 工具           | 版本        | 说明                                                                                 |
| -------------- | ----------- | ------------------------------------------------------------------------------------ |
| Node.js        | 24.x        | 最新的主要版本，支持所有现代 JavaScript 特性                                         |
| pnpm           | 10.x        | 高性能包管理器，与 Node.js 24 完美兼容                                               |
| Caddy          | 2.8+        | 自动 HTTPS 的反向代理服务器                                                          |
| PostgreSQL     | 17 (alpine) | 轻量级关系型数据库，alpine 版本镜像小巧                                              |
| Drizzle ORM    | 0.30+       | TypeScript 原生 ORM，支持迁移和类型安全查询                                          |
| PM2            | 5+          | 生产级 Node.js 进程管理工具                                                          |
| GitHub Actions | 最新        | CI/CD 平台，所有 Action 均为当前最新版本（如 `checkout@v4`、`ssh-action@v1.0.0` 等） |
| 阿里云 ACR     | –           | 容器镜像服务，需使用固定密码进行认证                                                 |

> **注意**：请根据你的项目实际需求调整具体版本号。若使用其他技术栈（如 Python、Java 等），请替换对应的运行时版本。

---

## 🎯 系统架构与核心理念

动态网站部署需要处理：

1. 运行时环境安装
2. 项目依赖安装
3. 数据库迁移（使用 Drizzle ORM）
4. 应用进程管理（PM2）
5. 反向代理与 HTTPS（Caddy）

整个流程基于 **声明式自动化**，通过 GitHub Actions 串联所有步骤。

```bash
开发者本地 (Local)
↓ [git push]
GitHub 仓库 (Repository)
↓ [触发]
GitHub Actions (CI/CD 管道)
├─ 检出代码
├─ 安装依赖
├─ 运行测试（可选）
├─ 构建项目（Nuxt 生成 .output）
├─ 同步代码至服务器
└─ 远程执行命令（依赖安装、迁移、重启）
阿里云服务器 (ECS)
├─ PM2 守护应用进程
├─ Caddy 反向代理 + 自动 HTTPS
└─ PostgreSQL 数据库
用户访问 → HTTPS → Caddy → 应用（.output/server/index.mjs）→ 数据库
```

---

## 📦 前置准备

1. **一个 GitHub 仓库**，包含你的动态网站源码，并已集成 Drizzle ORM。
   - 确保项目包含 `drizzle.config.ts`、数据库 schema 文件（如 `server/db/schema.ts`）。
   - **注意 Drizzle 迁移目录可能不同**：默认生成在 `drizzle` 目录，但你可能配置为 `.drizzle` 或其他名称。请根据你的 `drizzle.config.ts` 中的 `out` 字段确认实际目录，并在后续步骤中保持一致。
   - **迁移文件必须提前生成并提交到 Git**：在本地运行 `pnpm drizzle-kit generate` 生成初始迁移文件，然后 `git add` 并提交。
2. **一台云服务器**（阿里云 ECS 等），建议 Ubuntu 24.04（确保支持最新软件包）。
   - 安全组必须开放：**SSH(22)**、**HTTP(80)**、**HTTPS(443)** 端口。
3. **一个域名**（强烈推荐，用于自动 HTTPS），并已解析到服务器 IP。
4. **PostgreSQL 数据库**（可安装在服务器上，或使用云数据库如阿里云 RDS）。无论数据库在哪，你需要一个可访问的连接字符串（`DATABASE_URL`）。

---

## 🚀 第一部分：服务器初始化

### 1.1 登录并安装基础软件

通过 SSH 登录你的云服务器。**注意**：下文假设用户名为 `ubuntu`，请根据你的实际用户名替换。

```bash
# 更新系统包
sudo apt update && sudo apt upgrade -y

# 安装 Caddy（使用官方仓库，确保最新版本）
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo apt update
sudo apt install -y caddy

# 验证 Caddy 版本（需 >= 2.6.0）
caddy version

# 安装 Node.js 24（当前最新版本）
curl -fsSL https://deb.nodesource.com/setup_24.x | sudo -E bash -
sudo apt install -y nodejs
node --version  # 应输出 v24.x.x

# 启用 corepack 以使用 pnpm
sudo corepack enable
# 安装 pnpm 10 最新版
corepack prepare pnpm@latest --activate
pnpm --version  # 应输出 10.x.x

# 安装 PM2 全局进程管理（使用 sudo 确保权限）
sudo npm install -g pm2
# 或者用 pnpm（但注意全局路径）：
# sudo pnpm add -g pm2

# 安装 PostgreSQL 客户端（可选，用于调试）
sudo apt install -y postgresql-client

# 创建应用目录，并设置正确所有者（使用你的实际用户名）
sudo mkdir -p /var/www/my-dynamic-app
sudo chown -R ubuntu:ubuntu /var/www/my-dynamic-app
```

### 1.2 配置 Caddy 作为反向代理

编辑 Caddy 配置文件：

```bash
sudo nano /etc/caddy/Caddyfile
```

写入以下内容（**务必替换 `example.com` 为你的域名**，Nuxt 默认运行在 3000 端口）：

```bash
example.com, www.example.com {
    # 反向代理到本地 Nuxt 应用进程
    reverse_proxy 127.0.0.1:3000
    # 启用压缩
    encode gzip zstd
}
```

> **⚠️ 重要**：Nuxt 应用在运行时必须监听 `127.0.0.1`（即 localhost），以确保只能通过 Caddy 访问。在 Nuxt 中默认监听所有接口，你需要在启动命令中指定 `HOST=127.0.0.1` 或通过环境变量控制。

验证并重启 Caddy：

```bash
sudo caddy validate --config /etc/caddy/Caddyfile
sudo systemctl restart caddy
sudo systemctl status caddy
```

> 如果使用域名，Caddy 会在首次 HTTPS 请求时自动申请 Let's Encrypt 证书。

### 1.3 准备数据库连接

无论数据库在服务器本地还是云端，你都需要一个有效的连接字符串，格式如：

```bash
postgresql://用户名:密码@主机:端口/数据库名
```

- **如果数据库在服务器本地**（通过 apt 安装）：

  ```bash
  sudo apt install -y postgresql
  sudo systemctl start postgresql
  sudo systemctl enable postgresql
  sudo -u postgres psql
  # 在 psql 中执行：
  CREATE DATABASE mydb;
  CREATE USER myuser WITH ENCRYPTED PASSWORD 'mypassword';
  GRANT ALL PRIVILEGES ON DATABASE mydb TO myuser;
  \q
  ```

  此时连接字符串为 `postgresql://myuser:mypassword@localhost:5432/mydb`。

- **如果使用云数据库**（如阿里云 RDS），直接在控制台获取连接串。

请确保你的数据库可以从服务器访问（如果是云数据库，需在安全组中放行服务器 IP）。

---

## 🔐 第二部分：配置 SSH 密钥对与 GitHub Secrets

### 2.1 在本地生成 SSH 密钥对

在你的**本地电脑**执行：

```bash
ssh-keygen -t ed25519 -f ~/.ssh/id_github_actions_dynamic -N ""
```

### 2.2 将公钥部署到服务器

复制公钥内容：

```bash
cat ~/.ssh/id_github_actions_dynamic.pub
```

登录服务器，将公钥添加到 `~/.ssh/authorized_keys`：

```bash
echo '你的公钥内容' >> ~/.ssh/authorized_keys
chmod 600 ~/.ssh/authorized_keys
chmod 700 ~/.ssh
```

### 2.3 将私钥配置为 GitHub Secrets

复制私钥内容（**务必包含 `-----BEGIN OPENSSH PRIVATE KEY-----` 和 `-----END OPENSSH PRIVATE KEY-----` 行，保持完整格式**）：

```bash
cat ~/.ssh/id_github_actions_dynamic
```

进入 GitHub 仓库 → **Settings** → **Secrets and variables** → **Actions**，点击 **New repository secret**，添加以下 Secrets：

| Secret 名称       | 说明                                                         |
| ----------------- | ------------------------------------------------------------ |
| `SERVER_HOST`     | 服务器公网 IP                                                |
| `SERVER_USER`     | SSH 用户名（如 `ubuntu`）                                    |
| `SSH_PRIVATE_KEY` | 上面复制的私钥全文（保持换行）                               |
| `DATABASE_URL`    | 数据库连接字符串                                             |
| 其他环境变量      | 如 `NUXT_SESSION_PASSWORD`、`NUXT_OAUTH_GITHUB_CLIENT_ID` 等 |

> **💡 提示**：如果私钥内容在 GitHub Secrets 中粘贴后丢失换行，会导致 SSH 连接失败。请确保原样粘贴。

---

## ⚙️ 第三部分：创建 GitHub Actions 工作流

在项目根目录创建 `.github/workflows/deploy.yml`。以下是一个完整的、适配 **Node.js 24 + pnpm 10 + Nuxt + Drizzle ORM** 的示例。

```yaml
name: Deploy Dynamic App to Production

on:
  push:
    branches: [main]

jobs:
  deploy:
    runs-on: ubuntu-latest

    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      # 设置 Node.js 24 环境
      - name: Setup Node.js
        uses: actions/setup-node@v4
        with:
          node-version: "24" # 使用 Node.js 24
          cache: "pnpm" # 启用 pnpm 缓存

      # 启用 corepack 并安装 pnpm 10
      - name: Install pnpm
        run: |
          corepack enable
          corepack prepare pnpm@latest --activate
          pnpm --version  # 应输出 10.x

      # 安装依赖（包含所有依赖）
      - name: Install dependencies
        run: pnpm install --frozen-lockfile

      # 可选：运行测试
      - name: Run tests
        run: pnpm test
        continue-on-error: true

      # 构建 Nuxt 应用（生成 .output 目录）
      - name: Build Nuxt app
        run: pnpm run build
        env:
          # 构建时可能需要的环境变量
          NUXT_PUBLIC_SITE_URL: ${{ secrets.NUXT_PUBLIC_SITE_URL }}
          DATABASE_URL: ${{ secrets.DATABASE_URL }}

      # 同步代码到服务器（排除不需要的文件）
      # 注意：burnett01/rsync-deployments 是一个社区维护的 action，你可以审查其源码：
      # https://github.com/burnett01/rsync-deployments
      - name: Deploy to Server via Rsync
        uses: burnett01/rsync-deployments@7.0.1
        with:
          switches: -avz --delete --exclude='.env' --exclude='node_modules' --exclude='.git'
          path: ./ # 同步整个项目（.output 是构建产物，需要同步）
          remote_path: /var/www/my-dynamic-app/
          remote_host: ${{ secrets.SERVER_HOST }}
          remote_user: ${{ secrets.SERVER_USER }}
          remote_key: ${{ secrets.SSH_PRIVATE_KEY }}

      # 在服务器上执行远程命令（核心步骤）
      - name: Remote execution
        uses: appleboy/ssh-action@v1.0.0
        env:
          DATABASE_URL: ${{ secrets.DATABASE_URL }}
          NUXT_SESSION_PASSWORD: ${{ secrets.NUXT_SESSION_PASSWORD }}
          NUXT_OAUTH_GITHUB_CLIENT_ID: ${{ secrets.NUXT_OAUTH_GITHUB_CLIENT_ID }}
          NUXT_OAUTH_GITHUB_CLIENT_SECRET: ${{ secrets.NUXT_OAUTH_GITHUB_CLIENT_SECRET }}
        with:
          host: ${{ secrets.SERVER_HOST }}
          username: ${{ secrets.SERVER_USER }}
          key: ${{ secrets.SSH_PRIVATE_KEY }}
          envs: DATABASE_URL, NUXT_SESSION_PASSWORD, NUXT_OAUTH_GITHUB_CLIENT_ID, NUXT_OAUTH_GITHUB_CLIENT_SECRET
          script: |
            set -e  # 遇到任何错误立即退出

            cd /var/www/my-dynamic-app

            # 创建环境变量文件（一次性写入，注意不要用单引号，否则变量不会展开）
            cat > .env << EOF
            DATABASE_URL=$DATABASE_URL
            NUXT_SESSION_PASSWORD=$NUXT_SESSION_PASSWORD
            NUXT_OAUTH_GITHUB_CLIENT_ID=$NUXT_OAUTH_GITHUB_CLIENT_ID
            NUXT_OAUTH_GITHUB_CLIENT_SECRET=$NUXT_OAUTH_GITHUB_CLIENT_SECRET
            NODE_ENV=production
            EOF

            # 设置 .env 文件权限，防止其他用户读取
            chmod 600 .env

            # 启用 corepack 并安装 pnpm（服务器上可能没有）
            export PNPM_HOME=~/.local/share/pnpm
            export PATH=$PNPM_HOME:$PATH
            if ! command -v pnpm &> /dev/null; then
              corepack enable
              corepack prepare pnpm@latest --activate
            fi

            # 安装生产依赖
            pnpm install --prod --frozen-lockfile

            # 执行数据库迁移
            # 注意：请根据你的 drizzle.config.ts 中的 out 目录调整
            # 默认是 drizzle，也可能是 .drizzle 或其他名称
            # 迁移需要 drizzle-kit，临时安装
            pnpm add -D drizzle-kit
            npx drizzle-kit migrate
            # 迁移完成后可卸载（可选）
            # pnpm remove drizzle-kit

            # 重启应用（使用 PM2）
            # 注意：如果使用 Nuxt，启动命令应为：
            # pm2 start .output/server/index.mjs --name "my-app" -- --host 127.0.0.1
            if pm2 describe my-app > /dev/null 2>&1; then
              pm2 reload my-app
            else
              pm2 start npm --name "my-app" -- start
              pm2 save
              pm2 startup
            fi

            pm2 save
```

**关键说明**：

- **Node.js 24 + pnpm 10**：所有步骤都针对最新版本优化，包括 corepack 的启用和 pnpm 的安装方式。
- **Nuxt 构建产物**：`.output` 目录必须被同步，rsync 命令中**没有排除** `.output`。
- **Drizzle 迁移目录**：请根据你的 `drizzle.config.ts` 中的 `out` 字段确认迁移文件目录（可能是 `drizzle`、`.drizzle` 或自定义名称）。该目录必须被同步到服务器，因此不应在 rsync 中排除。
- **环境变量**：所有运行时需要的变量通过 GitHub Secrets 传递，并在远程脚本中写入 `.env`（注意此处使用 `<< EOF` 而非 `'EOF'`，确保变量正确展开）。
- **PM2 启动命令**：Nuxt 的启动入口是 `.output/server/index.mjs`，并通过 `--host 127.0.0.1` 确保只监听本地。
- **服务器上的 pnpm**：远程脚本中检测并安装 pnpm，确保服务器环境一致。
- **迁移依赖**：临时安装 `drizzle-kit` 执行迁移，之后可卸载，保持生产环境干净。

---

## 🧪 第四部分：触发首次部署与验证

### 4.1 提交并推送代码

```bash
git add .github/workflows/deploy.yml
git commit -m "ci: 添加动态网站自动化部署"
git push origin main
```

### 4.2 监控部署过程

在 GitHub 仓库的 **Actions** 标签页查看运行状态。成功时所有步骤应为绿色。

### 4.3 验证服务

- 访问 `https://你的域名`，确认网站功能正常。
- 检查服务器进程：`pm2 status` 应显示 `nuxt-app` 为 `online`。
- 查看应用日志：`pm2 logs nuxt-app`。
- 查看 Caddy 日志：`sudo journalctl -u caddy -f`。

---

## 🔧 第五部分：高级配置与问题排查

### 5.1 Drizzle 迁移的最佳实践

- **迁移文件必须提交到 Git**，确保 CI 和服务器能获取到相同的迁移历史。注意你的迁移目录可能是 `.drizzle`（以点开头），在 Git 中需要显式添加（`git add .drizzle`）。
- **首次部署前**，在本地运行 `pnpm drizzle-kit generate` 生成初始迁移文件并提交。后续每次修改 schema 后，同样在本地生成新迁移文件并提交。
- **迁移目录的 rsync 排除**：确保你的迁移目录**不被 rsync 排除**。检查 `--exclude` 参数中是否误排了类似 `.drizzle` 的目录。

### 5.2 Nuxt 应用监听地址

确保 Nuxt 应用监听 `127.0.0.1` 而非 `0.0.0.0`。可以通过以下方式之一实现：

- 在启动命令中指定：`pm2 start .output/server/index.mjs --name "nuxt-app" -- --host 127.0.0.1`
- 或在 `nuxt.config.ts` 中配置：
  ```ts
  export default defineNuxtConfig({
    nitro: {
      devServer: {
        host: "127.0.0.1",
      },
    },
  });
  ```

### 5.3 环境变量安全

- 不要在代码中硬编码敏感信息，全部通过 GitHub Secrets 注入。
- 服务器上的 `.env` 文件权限设为 `600`（已在脚本中执行 `chmod 600 .env`）。

### 5.4 PM2 开机自启的完整操作

1. 首次部署成功后，登录服务器。
2. 执行 `pm2 startup`，复制输出的带有 `sudo` 的命令（如 `sudo env PATH=$PATH:/usr/bin pm2 startup systemd -u ubuntu --hp /home/ubuntu`）。
3. 粘贴并执行该命令，输入服务器密码（如果需要）。
4. 之后执行 `pm2 save` 确保当前进程列表被保存。

### 5.5 关键问题排查清单

| 现象                             | 可能原因                                                             | 解决方案                                                                                                       |
| -------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------- |
| **Actions 日志卡在 SSH 连接**    | SSH 密钥格式错误、安全组未开放 22 端口、服务器 `sshd_config` 限制    | 检查 Secrets 中的私钥是否包含完整换行；检查安全组入方向规则；查看服务器 `/var/log/auth.log` 寻找原因           |
| **Caddy 返回 502 Bad Gateway**   | 后端应用未运行、Caddy 配置端口错误、应用绑定地址不是 127.0.0.1       | 检查 `pm2 status`；确认 Caddyfile 中的端口；确保应用监听 `127.0.0.1`                                           |
| **应用启动失败**                 | 依赖未安装、环境变量缺失、端口被占用、入口文件路径错误               | 登录服务器手动运行 `pnpm install`；检查 `.env` 文件；`netstat -tlnp                                            | grep 3000`查看端口占用；确认`.output/server/index.mjs` 是否存在 |
| **数据库迁移失败**               | 数据库连接串错误、迁移文件缺失、数据库服务未启动、drizzle-kit 未安装 | 检查 `DATABASE_URL` 是否正确；确认迁移目录（如 `.drizzle`）存在；检查数据库服务状态；确保 `drizzle-kit` 已安装 |
| **迁移目录找不到**               | rsync 排除了点开头的目录                                             | 检查 rsync 命令的 `--exclude` 参数，确保没有排除 `.drizzle` 或你的自定义迁移目录                               |
| **pnpm 命令未找到**              | 服务器未安装 pnpm，或 PATH 未设置                                    | 检查远程脚本中是否正确安装了 pnpm，并设置了 `PATH`                                                             |
| **网站 HTTPS 证书未自动生成**    | 域名 DNS 未生效、Caddy 版本过旧、80/443 端口未开放                   | 检查 DNS 解析；升级 Caddy 到最新版；检查安全组端口                                                             |
| **PM2 进程在服务器重启后未恢复** | 未执行 `pm2 startup` 后的 sudo 命令                                  | 登录服务器，重新执行 `pm2 startup` 并根据提示运行 sudo 命令                                                    |

### 5.6 查看日志

```bash
# 查看应用日志
pm2 logs nuxt-app

# 查看 Caddy 日志
sudo journalctl -u caddy -f

# 查看系统认证日志（SSH 问题）
sudo tail -f /var/log/auth.log
```

---

## 📈 总结：你现在拥有了什么

1. **全自动部署**：一次 `git push`，从代码到服务更新全部自动化。
2. **最新工具链**：Node.js 24 + pnpm 10，享受最新性能和特性。
3. **Nuxt 专属优化**：正确处理 `.output` 构建产物和启动方式。
4. **数据库迁移集成**：Drizzle ORM 的迁移在部署时自动执行，支持自定义迁移目录（如 `.drizzle`）。
5. **进程守护**：PM2 保证应用持续运行，崩溃自动重启。
6. **自动 HTTPS**：Caddy 自动申请和续期 SSL 证书。
7. **安全可控**：所有敏感信息通过 GitHub Secrets 管理，服务器上的 `.env` 文件权限严格。
8. **跨技术栈适配**：本教程的结构可轻松迁移到其他语言和框架。

**下一步**：如果你的项目需要更复杂的多容器编排（如应用、数据库、Redis 等），可以考虑迁移到 Docker 部署（参见本系列《进阶 Docker 篇》）。
