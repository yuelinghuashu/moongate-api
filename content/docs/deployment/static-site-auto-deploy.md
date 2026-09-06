---
title: GitHub Actions + Caddy 静态网站自动化部署（静态篇）
description: 专注于纯前端资源的自动化发布，利用 Caddy 自动 HTTPS 和 SPA 路由支持，实现“推送即发布”。
date: 2026-01-22
series: deployment
tags:
  - Caddy
  - CI/CD
---

本教程将完整复现一个现代化静态网站从本地开发到自动化部署的全流程。你将搭建一套 **“Git推送即发布”** 的自动化系统，无需手动操作服务器。教程基于 **Nuxt.js** 静态生成，但核心流程适用于任何静态网站（如VitePress、Next.js SSG、Hugo等）。

> **最终效果**：本地 `git push` → 自动构建、测试 → 安全同步至云服务器 → 网站即刻更新（HTTPS自动启用）。

---

## 📌 版本声明

本文档所有工具均采用 **2026 年最新稳定版**，具体版本如下：

| 工具           | 版本 | 说明                                                                                 |
| -------------- | ---- | ------------------------------------------------------------------------------------ |
| Node.js        | 24.x | 最新的主要版本，支持所有现代 JavaScript 特性                                         |
| pnpm           | 10.x | 高性能包管理器，与 Node.js 24 完美兼容                                               |
| Caddy          | 2.8+ | 自动 HTTPS 的反向代理服务器                                                          |
| GitHub Actions | 最新 | CI/CD 平台，所有 Action 均为当前最新版本（如 `checkout@v4`、`ssh-action@v1.0.0` 等） |
| 阿里云 ACR     | –    | 容器镜像服务，需使用固定密码进行认证                                                 |

> **注意**：请根据你的项目实际需求调整具体版本号。若使用其他技术栈（如 Python、Java 等），请替换对应的运行时版本。

---

## 🎯 系统架构与核心理念

这套方案的核心是 **“声明式自动化”**：你只需在代码仓库中声明“做什么”（配置文件），GitHub Actions 和 Caddy 就会自动执行“怎么做”。

```text
开发者本地 (Local)
↓ [git push]
GitHub 仓库 (Repository)
↓ [触发]
GitHub Actions (CI/CD 管道)
↓ [构建、测试、同步]
阿里云服务器 (Alibaba Cloud ECS)
↓ [Caddy 提供 HTTPS 服务]
用户访问 (HTTPS Website)
```

## 📦 前置准备

1. **一个 GitHub 仓库**
2. **一台阿里云 ECS 实例**（或任何具有公网 IP 的 Linux 服务器）
   - 推荐系统：Ubuntu 22.04 / Alibaba Cloud Linux 3
   - **安全组必须开放**：**SSH(22)**、**HTTP(80)**、**HTTPS(443)** 端口（请登录阿里云控制台检查确认）
3. **一个域名**（可选，但推荐。教程以 `example.com` 为例）

---

## 🚀 第一部分：服务器初始化

### 1.1 登录并安装基础软件

通过 SSH 登录你的云服务器（假设登录用户名为 `your-user`，后续步骤中请将 `$USER` 替换为实际用户名）。

```bash
# 更新系统包
sudo apt update && sudo apt upgrade -y

# 安装 Caddy（现代化的 Web 服务器，自动 HTTPS）
sudo apt install caddy

# 安装 Node.js 环境
curl -fsSL https://deb.nodesource.com/setup_24.x | sudo -E bash -
sudo apt install -y nodejs

# 创建网站根目录，并将所有权交给当前登录用户（确保后续 rsync 有写入权限）
sudo mkdir -p /var/www/my-site
sudo chown -R $USER:$USER /var/www/my-site
```

### 1.2 配置 Caddy

编辑 Caddy 的主配置文件，告诉它如何服务你的网站。

```bash
sudo nano /etc/caddy/Caddyfile
```

粘贴以下配置，**请务必将 `example.com` 替换为你的真实域名**。如果没有域名，可以用 `http://你的服务器IP` 格式，但将无法享受自动 HTTPS。

```bash
example.com, www.example.com {
    # 网站根目录（必须与后续自动化部署的目录一致）
    root * /var/www/my-site
    # 启用静态文件服务器
    file_server
    # 对单页应用(SPA)至关重要：使前端路由正常工作
    try_files {path} /index.html
    # 启用压缩
    encode gzip zstd
}
```

保存退出后（在 nano 中：`Ctrl+X`，然后 `Y`，再 `Enter`），重启 Caddy 使配置生效。

```bash
# 检查配置语法
sudo caddy validate --config /etc/caddy/Caddyfile
# 重启服务
sudo systemctl restart caddy
# 检查服务状态
sudo systemctl status caddy
```

> **💡 提示**：看到 `active (running)` 状态即表示 Caddy 已就绪。如果使用域名，Caddy 会在首次访问时**自动申请并配置 Let's Encrypt SSL 证书**。

---

## 🔐 第二部分：配置 SSH 密钥对与 GitHub Secrets

自动化部署的核心是让 GitHub Actions 能安全地连接到你的服务器。我们使用 SSH 密钥对进行认证。

### 2.1 在本地生成 SSH 密钥对

在你的**本地电脑**（而非服务器）上执行：

```bash
# 生成一对新的密钥，专用于自动化部署
ssh-keygen -t ed25519 -f ~/.ssh/id_github_actions -N ""
```

这将生成两个文件：

- **私钥** (`~/.ssh/id_github_actions`)：**绝密**，相当于你的“钥匙”。
- **公钥** (`~/.ssh/id_github_actions.pub`)：可以公开，相当于“锁芯”。

### 2.2 将公钥部署到服务器

1. 复制公钥内容：

   ```bash
   cat ~/.ssh/id_github_actions.pub
   ```

2. **登录你的云服务器**，将公钥添加到授权列表：

   ```bash
   # 将上一步复制的公钥内容，粘贴到引号内，然后执行整条命令
   echo '你的公钥内容' >> ~/.ssh/authorized_keys
   # 设置正确的权限（非常重要！）
   chmod 600 ~/.ssh/authorized_keys
   chmod 700 ~/.ssh
   ```

### 2.3 将私钥配置为 GitHub Secrets

1. 查看私钥内容：

   ```bash
   cat ~/.ssh/id_github_actions
   ```

2. 进入你的 GitHub 仓库，点击 **Settings** → **Secrets and variables** → **Actions**。
3. 点击 **New repository secret**，添加以下三个密钥：
   - **`SERVER_HOST`**：你的云服务器**公网 IP 地址**。
   - **`SERVER_USER`**：用于 SSH 登录的用户名（例如 `root`、`ubuntu` 或你在服务器上使用的用户名）。
   - **`SSH_PRIVATE_KEY`**：粘贴你刚刚复制的**完整私钥内容**（包括 `-----BEGIN OPENSSH PRIVATE KEY-----` 和 `-----END OPENSSH PRIVATE KEY-----` 行）。

> **提示**：如果你的网站构建时需要环境变量（如 `NUXT_PUBLIC_API_BASE`），请一并添加到 Secrets 中，后续会在构建步骤使用。

---

## ⚙️ 第三部分：创建 GitHub Actions 工作流

这是自动化的“大脑”。在你的项目根目录创建文件：`.github/workflows/deploy.yml`

```yaml
name: Deploy to Production

on:
  push:
    branches: [main] # 仅在推送到 main 分支时触发

jobs:
  build-and-deploy:
    runs-on: ubuntu-latest

    steps:
      # 1. 拉取代码
      - name: Checkout code
        uses: actions/checkout@v4

      # 2. 设置 Node.js 环境 (以 Nuxt 项目为例)
      - name: Setup Node.js
        uses: actions/setup-node@v4
        with:
          node-version: "24" # 使用你项目所需的 Node 版本
          cache: "pnpm" # 启用依赖缓存，加速构建

      # 3. 安装依赖
      - name: Install dependencies
        run: pnpm install

      # 4. 构建静态网站（如需环境变量，通过 env 传入）
      - name: Build
        run: pnpm run generate # 或 pnpm run build（取决于项目配置）
        env:
          # 从 GitHub Secrets 读取构建所需变量
          NUXT_PUBLIC_API_BASE: ${{ secrets.NUXT_PUBLIC_API_BASE }}
          # 可根据需要添加更多变量

      # 5. 将构建产物同步到云服务器
      - name: Deploy to Server via Rsync
        uses: burnett01/rsync-deployments@7.0.1
        with:
          switches: -avz --delete # 递归、压缩、同步删除（保持两端完全一致）
          path: .output/public/ # Nuxt 静态文件输出目录
          remote_path: /var/www/my-site/ # 服务器目标目录，必须与 Caddyfile 中配置一致
          remote_host: ${{ secrets.SERVER_HOST }}
          remote_user: ${{ secrets.SERVER_USER }}
          remote_key: ${{ secrets.SSH_PRIVATE_KEY }}
```

### 关键配置说明

- `path`: 你的静态网站构建输出目录。对于其他框架：
  - VitePress: `docs/.vitepress/dist/`
  - Next.js (SSG): `out/`
  - Vue CLI: `dist/`
  - Hugo: `public/`
- `remote_path`: 必须与服务器上 Caddy 配置的 `root` 目录完全一致。
- `switches: --delete`: 确保服务器上的文件是构建结果的精确镜像，自动删除多余文件。
- 如果构建过程需要环境变量，务必在 `Build` 步骤的 `env` 中传入，并在 GitHub Secrets 中预先定义。

---

## 🧪 第四部分：触发首次部署与验证

### 4.1 提交并推送代码

将工作流配置文件添加到 Git 并推送到仓库，触发首次自动化部署。

```bash
git add .github/workflows/deploy.yml
git commit -m "feat: 添加自动化部署工作流"
git push origin main
```

### 4.2 监控部署过程

1. 进入你的 GitHub 仓库，点击 **Actions** 标签页。
2. 你会看到名为 “Deploy to Production” 的工作流正在运行。
3. 点击进入，可以实时查看每个步骤的日志。
4. 当所有步骤显示绿色对勾（✅），表示部署成功。

### 4.3 验证网站

- 打开浏览器，访问你的域名（如 `https://example.com`）。
- 如果使用 IP，访问 `http://你的服务器IP`。
- 你应该能看到部署的网站，并且地址栏显示**安全的 HTTPS 锁标**（如果使用了域名）。

---

## 🔧 第五部分：高级配置与问题排查

### 5.1 处理静态资源（图标、图片）

确保项目的**静态资源**（如 `favicon.ico`、图片）放在正确的目录：

- **Nuxt 3/4**: `/public/` 目录
- **Nuxt 2**: `/static/` 目录
- 构建后，这些文件会自动复制到 `.output/public/` 下。

#### 推荐做法

在 `public/` 下创建子目录（如 `public/icons/`, `public/images/`）进行分类管理。

### 5.2 关键问题排查清单

| 现象                                  | 可能原因                                                                             | 解决方案                                                                                                                                                                   |
| ------------------------------------- | ------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Actions 日志卡在 SSH 连接**         | 1. SSH 密钥格式错误 <br>2. 安全组未开放 22 端口 <br>3. 服务器 `sshd_config` 配置限制 | 1. 检查私钥格式，确保在 GitHub Secrets 中完整、多行 <br>2. 检查阿里云安全组入方向规则 <br>3. 检查服务器 `/etc/ssh/sshd_config` 中的 `PermitRootLogin` 和 `AllowUsers` 设置 |
| **网站可以 HTTP 访问，但 HTTPS 报错** | 1. 域名 DNS 解析未生效或错误 <br>2. 安全组未开放 443 端口                            | 1. 运行 `nslookup yourdomain.com` 检查 DNS 解析 <br>2. 检查安全组 443 端口规则                                                                                             |
| **网站显示 “404 Not Found”**          | 1. Caddy `root` 目录配置错误 <br>2. 文件未成功同步到服务器                           | 1. 核对 `/etc/caddy/Caddyfile` 中的 `root` 路径与 `deploy.yml` 中的 `remote_path` <br>2. 登录服务器检查 `/var/www/my-site/` 目录下是否有文件                               |
| **构建失败 (Lint/Type Error)**        | 代码检查或类型错误                                                                   | 1. 本地运行 `pnpm run lint` 和 `pnpm run typecheck` 修复错误 <br>2. 或暂时在 `deploy.yml` 中注释掉相关检查步骤                                                             |

### 5.3 查看服务器日志

当遇到问题时，服务器日志是寻找线索的黄金位置。

```bash
# 查看 Caddy 实时日志
sudo journalctl -u caddy -f

# 查看 SSH 认证日志（排查连接问题）
sudo tail -f /var/log/auth.log
```

---

## 📈 总结：你现在拥有了什么？

通过本教程，你已成功搭建了一套**完全自动化、可追溯、可回滚**的现代静态网站部署流水线：

1. **自动化**：只需 `git push`，后续所有流程自动完成。
2. **零配置 HTTPS**：Caddy 自动管理 SSL 证书的申请和续期。
3. **环境一致**：每次构建都在全新的 GitHub 运行器中进行，杜绝“在我机器上好好的”问题。
4. **安全可靠**：基于 SSH 密钥认证，密钥安全存储在 GitHub Secrets。
5. **可回滚**：如需回滚，只需在 Git 中检出旧版本并推送，Actions 会自动将服务器文件同步至旧状态。

**从此，你可以专注于本地开发，将构建、测试、发布的重复劳动全部交给自动化流程。**
