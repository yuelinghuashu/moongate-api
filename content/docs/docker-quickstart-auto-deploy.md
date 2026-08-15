---
title: GitHub Actions + Docker 极简自动化部署教程（入门篇）
description: 从零开始，用最简洁的方式将你的应用打包成 Docker 镜像，并通过 GitHub Actions 实现自动构建、推送和服务器部署。适合 Docker 新手快速上手 CI/CD 流水线。
date: 2026-03-16 22:00:00
permalink: fb702a74-215e-4f19-bcde-53486f3b10fe
series: deployment
level: P4
tags: 
  - Caddy
  - Docker
  - CI/CD
---

## 📚 系列导航

本系列共六篇：

1. [**静态网站自动化部署（静态篇）**](./static-site-auto-deploy) —— 纯前端静态资源自动化发布
2. [**动态网站自动化部署（动态篇）**](dynamic-site-auto-deploy) —— 后端服务进程管理 + Caddy 反向代理
3. [**Docker 极简入门（入门篇）**](docker-quickstart-auto-deploy) —— 从零搭建 Docker CI/CD 流水线
4. [**Docker 生产级部署（进阶篇）**](docker-production-auto-deploy) —— 多容器编排与生产级可靠性
5. [**自托管 Umami 分析服务（扩展篇）**](./umami-integration-auto-deploy) —— Docker 环境集成分析服务
6. [**VitePress 文档站子域名部署（扩展篇）**](./vitepress-docker-existing-infrastructure-subdomain-deployment) —— 静态文档站接入现有 Docker 基础设施

---

本教程将带你用最简洁的方式，将你的应用打包成 Docker 镜像，并通过 GitHub Actions 自动部署到服务器。你将学会：

- 编写一个极简的 Dockerfile
- 使用 docker-compose 管理应用 + 数据库
- 配置 GitHub Actions 实现 CI/CD
- 处理环境变量、端口冲突等常见问题

> **适用人群**：对 Docker 有基础了解，想快速搭建自动化部署的新手。

---

## 🚀 最终效果

本地 `git push` → 自动构建镜像 → 推送到阿里云 ACR → 服务器自动拉取 → 服务重启 → 网站更新。

---

## 📌 版本声明

Node.js、pnpm、Caddy、GitHub Actions、阿里云 ACR 的版本信息与[静态篇](./static-site-auto-deploy)一致。本文额外涉及：

| 工具           | 版本  | 说明                                                             |
| -------------- | ----- | ---------------------------------------------------------------- |
| Docker Engine  | 29.x  | 容器运行时，支持 BuildKit 和多阶段构建                           |
| Docker Compose | v5    | 全新 Compose 规范，支持 `name` 项目和 `depends_on` 条件          |
| PostgreSQL     | 17 (alpine) | 轻量级关系型数据库，alpine 版本镜像小巧                    |
| Drizzle ORM    | 0.30+ | TypeScript 原生 ORM，支持迁移和类型安全查询                      |
| PM2            | 5+    | 生产级 Node.js 进程管理工具                                      |

> **注意**：请根据你的项目实际需求调整具体版本号。若使用其他技术栈（如 Python、Java 等），请替换对应的运行时版本。

---

## 📦 第一步：准备项目

### 1.1 项目结构

```text
my-app/
├── .github/workflows/deploy.yml   # GitHub Actions 配置
├── Dockerfile                     # 镜像构建文件
├── docker-compose.yml             # 容器编排文件
├── .env.example                   # 环境变量示例（用于参考，不提交）
└── ... 你的应用代码
```

### 1.2 编写 Dockerfile（以 Node.js 为例）

```dockerfile
# 构建阶段
FROM node:24-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build   # 根据项目调整构建命令，如 npm run generate

# 运行阶段
FROM node:24-alpine
WORKDIR /app
# 根据你的构建输出目录调整以下路径（常见：dist/、.output/、build/）
COPY --from=builder /app/.output ./.output   # 若使用 Nuxt
# 若使用其他框架，请替换为对应的输出目录，例如：
# COPY --from=builder /app/dist ./dist
EXPOSE 3000
# 启动命令也需对应调整，例如：
CMD ["node", ".output/server/index.mjs"]
# 若使用 Express，可能是 CMD ["node", "server.js"]
```

> **提示**：请根据你的项目框架调整构建输出目录和启动命令。

---

## 🛠️ 第二步：编写 docker-compose.yml

创建一个包含应用和数据库的极简编排文件。**注意**：首次部署前，请确保服务器上已创建 `.env` 文件（见第五步）。

```yaml
name: my-app

services:
  postgres:
    image: postgres:alpine
    container_name: my-app-db
    restart: always
    environment:
      POSTGRES_DB: ${POSTGRES_DB}
      POSTGRES_USER: ${POSTGRES_USER}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
    volumes:
      - postgres_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER} -d ${POSTGRES_DB}"]
      interval: 10s
      timeout: 5s
      retries: 5

  app:
    image: ${ACR_REGISTRY}/my-app:latest
    container_name: my-app
    restart: always
    ports:
      - "3000:3000" # 直接暴露端口（生产建议用反向代理）
    environment:
      DATABASE_URL: postgresql://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}
    depends_on:
      postgres:
        condition: service_healthy

volumes:
  postgres_data:
```

---

## 🔐 第三步：配置 GitHub Secrets

`SERVER_HOST`、`SERVER_USER`、`SSH_PRIVATE_KEY` 的配置方法与[静态篇 第二部分](./static-site-auto-deploy)一致。本文额外需要：

| Secret 名称         | 说明                                                                                             |
| ------------------- | ------------------------------------------------------------------------------------------------ |
| `ACR_REGISTRY`      | 阿里云镜像仓库地址（例如 `crpi-xxx.cn-beijing.personal.cr.aliyuncs.com`，**不要带 `https://`**） |
| `ACR_USERNAME`      | 阿里云账号（邮箱）                                                                               |
| `ACR_PASSWORD`      | 阿里云容器镜像服务固定密码                                                                       |
| `POSTGRES_DB`       | 数据库名                                                                                         |
| `POSTGRES_USER`     | 数据库用户                                                                                       |
| `POSTGRES_PASSWORD` | 数据库密码                                                                                       |

> **⚠️ 重要**：`.env` 文件包含敏感信息，**切勿提交到 Git**（已包含在 `.gitignore` 中）。首次部署前需手动在服务器上创建（见第五步）。

---

## ⚙️ 第四步：创建 GitHub Actions 工作流

在 `.github/workflows/deploy.yml` 中写入：

```yaml
name: Deploy

on:
  push:
    branches: [main]

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Login to ACR
        uses: docker/login-action@v3
        with:
          registry: ${{ secrets.ACR_REGISTRY }}
          username: ${{ secrets.ACR_USERNAME }}
          password: ${{ secrets.ACR_PASSWORD }}

      - name: Build and push
        uses: docker/build-push-action@v5
        with:
          push: true
          tags: ${{ secrets.ACR_REGISTRY }}/my-app:latest

      - name: Deploy to server
        uses: appleboy/ssh-action@v1.0.0
        env:
          ACR_REGISTRY: ${{ secrets.ACR_REGISTRY }}
          ACR_USERNAME: ${{ secrets.ACR_USERNAME }}
          ACR_PASSWORD: ${{ secrets.ACR_PASSWORD }}
          POSTGRES_DB: ${{ secrets.POSTGRES_DB }}
          POSTGRES_USER: ${{ secrets.POSTGRES_USER }}
          POSTGRES_PASSWORD: ${{ secrets.POSTGRES_PASSWORD }}
        with:
          host: ${{ secrets.SERVER_HOST }}
          username: ${{ secrets.SERVER_USER }}
          key: ${{ secrets.SSH_PRIVATE_KEY }}
          envs: ACR_REGISTRY, ACR_USERNAME, ACR_PASSWORD, POSTGRES_DB, POSTGRES_USER, POSTGRES_PASSWORD
          script: |
            cd /var/www/my-app
            # 写入环境变量（注意不要用单引号，否则变量不会展开）
            cat > .env << EOF
            POSTGRES_DB=$POSTGRES_DB
            POSTGRES_USER=$POSTGRES_USER
            POSTGRES_PASSWORD=$POSTGRES_PASSWORD
            ACR_REGISTRY=$ACR_REGISTRY
            EOF
            # 登录 ACR
            echo "$ACR_PASSWORD" | docker login "$ACR_REGISTRY" -u "$ACR_USERNAME" --password-stdin
            # 拉取新镜像
            docker compose pull app
            # 重启应用（若端口冲突可添加 --force-recreate）
            docker compose up -d --no-deps --force-recreate app
```

> **说明**：使用 `--force-recreate` 确保旧容器被强制重新创建，避免端口冲突。若你的应用需要数据库迁移，请在重启前添加迁移命令，例如 `docker compose exec app npm run migrate`。

---

## 🖥️ 第五步：服务器初始化（只需一次）

### 5.1 安装 Docker 并配置镜像加速器

```bash
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker $USER
# 重新登录或执行 newgrp docker 使组生效
# 配置阿里云镜像加速器（替换为你的加速地址）
sudo tee /etc/docker/daemon.json <<-'EOF'
{
  "registry-mirrors": ["https://your-mirror-id.mirror.aliyuncs.com"]
}
EOF
sudo systemctl restart docker
```

### 5.2 创建项目目录并准备环境

```bash
mkdir -p /var/www/my-app
cd /var/www/my-app
```

### 5.3 首次部署：创建 `.env` 文件并启动服务

由于首次部署时 GitHub Actions 还未运行，需要手动创建 `.env` 文件并启动一次服务，以便数据库初始化。

```bash
# 根据你的实际值创建 .env 文件
cat > .env << EOF
POSTGRES_DB=myapp
POSTGRES_USER=postgres
POSTGRES_PASSWORD=your-strong-password
ACR_REGISTRY=crpi-xxx.cn-beijing.personal.cr.aliyuncs.com
EOF
```

然后将本地的 `docker-compose.yml` 上传到该目录（例如使用 `scp`）：

```bash
# 在本地执行
scp docker-compose.yml root@your-server:/var/www/my-app/
```

首次启动：

```bash
docker compose up -d
```

> **注意**：首次启动后，PostgreSQL 会创建数据卷，后续部署时数据不会丢失。

### 5.4 测试服务

```bash
curl http://localhost:3000
# 或通过浏览器访问 http://你的服务器IP:3000
```

---

## 🔍 第六步：常见问题极简排查

| 现象                     | 可能原因                         | 解决                                                                      |
| ------------------------ | -------------------------------- | ------------------------------------------------------------------------- |
| Actions 中 SSH 连接失败  | 私钥格式错误 / 端口未开放        | 检查私钥是否包含完整换行；确保安全组开放 22 端口                          |
| 容器启动失败，端口占用   | 宿主机有其他进程占用了 3000 端口 | `sudo lsof -i :3000` 找到进程并停止                                       |
| 应用无法连接数据库       | 环境变量 `DATABASE_URL` 错误     | 检查 `.env` 文件中的连接串，确认主机名为 `postgres`（服务名）             |
| 镜像拉取慢 / 超时        | 未配置镜像加速器                 | 按 5.1 配置阿里云加速器并重启 Docker                                      |
| 容器重启后数据丢失       | 数据库卷未正确挂载               | 确认 `docker-compose.yml` 中有 `volumes` 定义，且数据卷存在               |
| 首次部署时数据库未初始化 | `.env` 文件缺失或变量错误        | 按 5.3 手动创建 `.env` 并再次执行 `docker compose up -d`                  |
| 应用更新后未生效         | 镜像未拉取或容器未重启           | 检查 Actions 日志；手动执行 `docker compose pull && docker compose up -d` |

---

## 📖 附录：常用运维命令

```bash
# 查看所有容器状态
docker compose ps

# 查看应用日志
docker compose logs -f app

# 进入应用容器内部调试
docker exec -it my-app sh

# 停止所有服务
docker compose down

# 重新构建并启动（如需重新构建镜像）
docker compose up -d --build
```

---

## 🎉 完成

现在你已经拥有了一套极简但可工作的自动化部署流水线。每次推送代码到 main 分支，都会自动构建镜像、推送到阿里云、并在服务器上重启应用。

**接下来可以探索**：添加健康检查、使用 Caddy 反向代理（避免直接暴露端口）、多环境配置、数据库迁移自动化等进阶功能（参见本系列进阶篇）。
