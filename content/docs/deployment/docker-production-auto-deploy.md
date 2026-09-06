---
title: GitHub Actions + Docker 生产级自动化部署（进阶篇）
description: 通过容器化技术实现环境一致性，自动构建镜像并分发至私有仓库，用 Docker Compose 编排服务，彻底告别环境依赖。
date: 2026-03-16 23:00:00
series: deployment
tags:
  - Caddy
  - Docker
  - CI/CD
---

## 📌 版本声明

所有工具的版本信息与[入门篇](./docker-quickstart-auto-deploy)一致（Docker Engine 29.x、Docker Compose v5、PostgreSQL 17 alpine、Drizzle ORM 0.30+、PM2 5+、Node.js 24.x、Caddy 2.8+）。

---

## 🎯 本章目标

在入门篇的基础上，你将学会：

- ✅ 多容器生产级编排（应用 + 数据库 + 反向代理）
- ✅ 健康检查与容器启动顺序控制
- ✅ 容器网络与服务发现（通过服务名通信）
- ✅ 环境变量安全传递（GitHub Secrets + 服务器 `.env` 权限）
- ✅ 数据持久化与自动备份
- ✅ 数据库迁移自动化（以 Drizzle ORM 为例）
- ✅ 零停机部署策略（解决端口冲突）
- ✅ 镜像加速器配置与国内优化
- ✅ 常见问题深度排查手册

最终你将拥有一套**可上生产、自动修复、安全可控**的 Docker 部署流水线。

---

## 🏗️ 系统架构图

```text
┌─────────────┐     ┌─────────────────┐     ┌─────────────────┐
│  本地开发    │────▶│  GitHub Actions │────▶│   阿里云 ACR    │
└─────────────┘     └─────────────────┘     └─────────────────┘
                                                      │
                                                      ▼
┌───────────────────────────────────────────────────────┐
│                    阿里云 ECS                          │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐ │
│  │  PostgreSQL │◀──▶│  Nuxt 应用  │◀──▶│    Caddy    │ │
│  │   (容器)    │    │   (容器)    │    │   (容器)    │ │
│  └─────────────┘    └─────────────┘    └─────────────┘ │
│         ▲                 ▲                  ▲         │
│         └─────────────────┼──────────────────┘         │
│                   同一网络 `app-network`                │
└───────────────────────────────────────────────────────┘
```

---

## 📦 前置准备

1. **完成入门篇**，已能跑通单容器部署。
2. **一个域名**（例如 `your-domain.com`）并解析到服务器 IP。
3. **阿里云 ACR** 已配置好命名空间和固定密码。
4. **服务器安全组**开放 `80`、`443`、`22` 端口。
5. **项目已集成 Drizzle ORM**，并生成迁移文件（已提交至 Git）。

---

## 🚀 第一部分：生产级 Docker Compose 配置

### 1.1 目录结构

```text
/var/www/my-app/
├── docker-compose.yml
├── .env                  # 环境变量（手动创建，不提交 Git）
├── Caddyfile             # Caddy 配置
└── backups/              # 数据库备份目录（可选）
```

### 1.2 编写生产级 docker-compose.yml

<details>
<summary>点击展开完整代码</summary>

```yaml
name: my-app # 固定项目名，避免网络混乱

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
    networks:
      - app-network
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER} -d ${POSTGRES_DB}"]
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 10s
    # logging 为入门篇没有的生产增强，所有服务统一使用：
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"

  app:
    image: ${ACR_REGISTRY}/my-app:latest
    container_name: my-app
    restart: always
    # 不暴露端口到宿主机，仅内部网络访问（由 Caddy 代理）
    environment:
      NUXT_PUBLIC_SITE_URL: ${NUXT_PUBLIC_SITE_URL}
      NUXT_SESSION_PASSWORD: ${NUXT_SESSION_PASSWORD}
      NUXT_OAUTH_GITHUB_CLIENT_ID: ${NUXT_OAUTH_GITHUB_CLIENT_ID}
      NUXT_OAUTH_GITHUB_CLIENT_SECRET: ${NUXT_OAUTH_GITHUB_CLIENT_SECRET}
      NUXT_DATABASE_URL: postgresql://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}
    depends_on:
      postgres:
        condition: service_healthy
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
      start_period: 15s
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"

  caddy:
    image: caddy:alpine
    container_name: my-app-caddy
    restart: always
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile
      - caddy_data:/data
      - caddy_config:/config
    networks:
      - app-network
    depends_on:
      app:
        condition: service_healthy
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"

volumes:
  postgres_data:
  caddy_data:
  caddy_config:

networks:
  app-network:
    driver: bridge
```

</details>

#### 关键点

- `name` 固定项目名，避免因目录名变化导致网络不一致。
- 所有服务在同一网络，通过服务名通信。
- 应用容器不暴露端口，流量全走 Caddy，提升安全性。
- `depends_on` + `condition: service_healthy` 确保启动顺序。
- 日志切割防止磁盘爆满。

### 1.3 Caddyfile 配置（自动 HTTPS）

请将 `your-domain.com` 替换为你的实际域名。

```caddy
www.your-domain.com {
    redir https://your-domain.com{uri} permanent
}

your-domain.com {
    reverse_proxy app:3000
    encode gzip zstd
    header {
        X-Content-Type-Options "nosniff"
        X-Frame-Options "DENY"
        X-XSS-Protection "1; mode=block"
        Referrer-Policy "strict-origin-when-cross-origin"
    }
}
```

---

## 🔐 第二部分：环境变量安全

### 2.1 GitHub Secrets 完整列表

`SERVER_HOST`、`SERVER_USER`、`SSH_PRIVATE_KEY` 的配置方法与[入门篇 第三步](./docker-quickstart-auto-deploy)一致。此外需要：

| Secret 名称                       | 说明                                                                    |
| --------------------------------- | ----------------------------------------------------------------------- |
| `ACR_REGISTRY`                    | 阿里云镜像仓库地址（如 `crpi-xxx.cn-beijing.personal.cr.aliyuncs.com`） |
| `ACR_USERNAME`                    | 阿里云账号邮箱                                                          |
| `ACR_PASSWORD`                    | ACR 固定密码                                                            |
| `POSTGRES_DB`                     | 数据库名                                                                |
| `POSTGRES_USER`                   | 数据库用户                                                              |
| `POSTGRES_PASSWORD`               | 数据库密码                                                              |
| `NUXT_PUBLIC_SITE_URL`            | 网站域名（如 `https://your-domain.com`）                                |
| `NUXT_SESSION_PASSWORD`           | 会话加密密钥（至少 32 位）                                              |
| `NUXT_OAUTH_GITHUB_CLIENT_ID`     | GitHub OAuth Client ID                                                  |
| `NUXT_OAUTH_GITHUB_CLIENT_SECRET` | GitHub OAuth Client Secret                                              |

### 2.2 服务器 `.env` 文件权限

在首次部署前，手动创建 `/var/www/my-app/.env`，并设置权限：

```bash
chmod 600 /var/www/my-app/.env
```

内容示例（请替换为实际值）：

```bash
POSTGRES_DB=myapp
POSTGRES_USER=postgres
POSTGRES_PASSWORD=StrongPassword123!
ACR_REGISTRY=crpi-xxx.cn-beijing.personal.cr.aliyuncs.com
NUXT_PUBLIC_SITE_URL=https://your-domain.com
NUXT_SESSION_PASSWORD=xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
NUXT_OAUTH_GITHUB_CLIENT_ID=xxxxxxxxxx
NUXT_OAUTH_GITHUB_CLIENT_SECRET=xxxxxxxxxxxxxxxxxxxxxxxx
```

---

## ⚙️ 第三部分：GitHub Actions 工作流（进阶版）

以下增量配置是在[入门篇第四步](./docker-quickstart-auto-deploy)的完整 workflow 基础上添加的。`checkout@v4`、`Login to ACR`、`Build and push` 等基础步骤与入门篇一致，此处仅展示**差异化部分**：

```yaml
# 在入门篇 workflow 基础上：
# 1) Build and push 步骤增加多标签推送与缓存加速
- name: Build and push
  uses: docker/build-push-action@v5
  with:
    push: true
    tags: |
      ${{ secrets.ACR_REGISTRY }}/my-app:latest
      ${{ secrets.ACR_REGISTRY }}/my-app:${{ github.sha }}
    cache-from: type=gha
    cache-to: type=gha,mode=max

# 2) Deploy 步骤的 .env 写入逻辑中，将基础变量替换为环境变量引用展开（与入门篇相同），并追加 NUXT_* 变量
#    完整 ssh-action 步骤中的 envs 列表需包含：ACR_REGISTRY, ACR_USERNAME, ACR_PASSWORD,
#    POSTGRES_DB, POSTGRES_USER, POSTGRES_PASSWORD, NUXT_PUBLIC_SITE_URL,
#    NUXT_SESSION_PASSWORD, NUXT_OAUTH_GITHUB_CLIENT_ID, NUXT_OAUTH_GITHUB_CLIENT_SECRET

# 3) 服务器远程脚本中的差异化命令：
set -e
cd /var/www/my-app

# 基础 .env 写入（与入门篇相同：POSTGRES_* + ACR_REGISTRY）
# 追加 NUXT_* 变量到 .env
chmod 600 .env

# 登录 ACR
echo "$ACR_PASSWORD" | docker login "$ACR_REGISTRY" -u "$ACR_USERNAME" --password-stdin
docker compose pull app

# ★ 差异化：执行数据库迁移（容器内安装 drizzle-kit 后运行）
docker compose run --rm app sh -c "npm install -g drizzle-kit && drizzle-kit migrate"

# ★ 差异化：重启应用 + Caddy（--force-recreate 强制替换容器）
docker compose up -d --force-recreate app
docker compose up -d --force-recreate caddy

# ★ 差异化：清理 24 小时前的旧镜像
docker image prune -f --filter "until=24h"
```

> **完整可复制版本**：将上述差异化命令合并进入门篇的 workflow，替换对应的 `Deploy to server` 步骤脚本，并在 `envs` 列表中追加所有 NUXT\_\* 变量即可。

### 进阶要点

- 使用 `cache-from` 加速构建。
- 远程脚本中执行数据库迁移（使用 `npm install -g` 确保有 `drizzle-kit`，避免依赖缺失）。
- `--force-recreate` 确保旧容器被完全替换，解决端口残留问题。
- 清理 24 小时前的旧镜像，避免磁盘占满。
- `envs` 列表中包含了所有需要传递的变量，确保远程 shell 能正确读取。

---

## 🧪 第四部分：服务器初始化（生产准备）

### 4.1 安装 Docker 并配置镜像加速器

Docker 安装与基础加速器配置请参见[入门篇 5.1](./docker-quickstart-auto-deploy)。生产环境额外推荐配置日志切割，在 `daemon.json` 中追加：

```json
{
  "log-driver": "json-file",
  "log-opts": {
    "max-size": "10m",
    "max-file": "3"
  }
}
```

> 若加速器无效，可使用 ACR 的海外源镜像同步功能或自行推送镜像至私有仓库。

### 4.2 创建项目目录并上传文件

```bash
mkdir -p /var/www/my-app
cd /var/www/my-app
# 将本地的 docker-compose.yml 和 Caddyfile 上传到该目录
# 例如：scp docker-compose.yml Caddyfile root@your-server:/var/www/my-app/
```

### 4.3 首次启动（手动）

```bash
# 手动创建 .env 文件（参照 2.2 的示例）
vim .env
chmod 600 .env

# 启动所有服务
docker compose up -d
```

### 4.4 验证服务

```bash
docker compose ps
curl -I http://localhost:3000  # 应返回 200（应用内部端口）
curl -I https://your-domain.com # 应返回 200（通过 Caddy）
```

---

## 🔧 第五部分：深度问题排查手册

| 现象                                                              | 可能原因                                       | 解决方案                                                                             |
| ----------------------------------------------------------------- | ---------------------------------------------- | ------------------------------------------------------------------------------------ |
| **Caddy 容器无法启动，端口 80/443 被占用**                        | 宿主机有其他 Web 服务（如系统级 Caddy、Nginx） | `sudo lsof -i :80 -i :443` 找到并停止进程；或修改端口映射                            |
| **应用无法连接数据库，日志显示 `getaddrinfo EAI_AGAIN postgres`** | 容器间网络问题 / 数据库服务名错误              | 确认 `app` 和 `postgres` 在同一网络；检查 `DATABASE_URL` 中的主机名是否为 `postgres` |
| **数据库迁移失败，提示 `drizzle-kit: not found`**                 | 容器内未安装 drizzle-kit                       | 已改为使用 `npm install -g drizzle-kit`，确保网络通畅；也可在 Dockerfile 中预装      |
| **应用容器不断重启**                                              | 健康检查失败 / 依赖服务未就绪                  | 查看日志：`docker logs my-app --tail 50`；检查 `depends_on` 条件                     |
| **部署后网站未更新**                                              | 容器未重启 / 镜像标签未更新                    | 检查 Actions 日志；手动执行 `docker compose pull && docker compose up -d`            |

---

## 📈 第六部分：日常运维

### 6.1 常用命令

`docker compose ps`、`logs -f`、`exec` 等基础命令请参见[入门篇 附录](./docker-quickstart-auto-deploy)。本文特有的运维命令：

```bash
# 备份数据库
docker exec my-app-db pg_dump -U postgres myapp > backups/backup_$(date +%Y%m%d).sql

# 恢复数据库
cat backups/backup.sql | docker exec -i my-app-db psql -U postgres -d myapp

# 查看容器健康状态
docker inspect --format='{{.State.Health.Status}}' my-app
```

### 6.2 自动备份（可选）

添加定时任务（crontab -e）：

```bash
0 2 * * * cd /var/www/my-app && docker exec my-app-db pg_dump -U postgres myapp > backups/backup_$(date +\%Y\%m\%d).sql
```

---

## 🏁 总结

至此，你已经构建了一套完整的、生产可用的 Docker 自动化部署体系：

- ✅ 多服务容器化编排
- ✅ 健康检查与依赖控制
- ✅ 环境变量安全注入
- ✅ 数据库迁移自动化
- ✅ 零停机部署
- ✅ 自动 HTTPS
- ✅ 数据持久化与备份
- ✅ 日志切割与镜像清理

这套方案可支撑中小型项目稳定运行。未来若需扩展微服务、K8s 等，亦可基于此基础演进。
