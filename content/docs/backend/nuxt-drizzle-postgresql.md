---
title: "Nuxt 4 集成 Drizzle ORM (PostgreSQL) 完整教程"
description: "本教程专注于 Drizzle ORM 在 PostgreSQL 数据库上的 Nuxt 4 集成。如果您使用的是 MySQL、SQLite 等其他数据库，部分配置（如连接驱动、数据类型）会有所不同，请参考 Drizzle 官方文档相应部分。"
date: 2026-02-16
series: backend
tags:
  - Nuxt
  - PostgreSQL
  - ORM
---

## 适用版本

| 依赖        | 版本               | 备注                                     |
| ----------- | ------------------ | ---------------------------------------- |
| Drizzle ORM | **v1.0.0-alpha.x** | 本文基于 alpha.10，后续版本 API 可能微调 |
| pg          | **v8**             | PostgreSQL 驱动                          |

> ⚠️ **注意**：Drizzle ORM 目前仍处于 alpha 阶段，如果你使用更新版本，建议参考[官方文档](https://orm.drizzle.org.cn/)。

<details>
<summary>📋教程范围与前置要求</summary>

本教程专注 **PostgreSQL + Nuxt 4** 的 Drizzle ORM 集成。如果您使用 MySQL/SQLite，驱动和类型会有差异，请参考官方文档相应部分。

### 前置要求

- 已有一个 Nuxt 4 项目（`npm create nuxt@latest <project-name>`）
- 本地已安装 PostgreSQL（或使用云数据库）
- 了解 TypeScript 基础

</details>

---

## ⚠️ 核心区别：Drizzle 官方文档 vs Nuxt 集成

| 对比维度        | Drizzle 官方文档   | 本教程（Nuxt 4）                       |
| --------------- | ------------------ | -------------------------------------- |
| **项目类型**    | 普通 Node.js 项目  | Nuxt 4（基于 Nitro 服务器）            |
| **目录结构**    | 自由定义           | 严格遵循 `server/` 目录规范            |
| **数据库连接**  | 直接导出 `db` 实例 | 通过 `server/db/index` 导出 `useDB()`  |
| **Schema 组织** | 通常单个文件       | 建议拆分多文件，并**必须包含关系定义** |
| **运行环境**    | 手动执行脚本       | 通过 API 路由触发，由 Nitro 管理       |

### 关键点

**完全照搬官方文档会在 Nuxt 中失败**，因为 Nuxt 的服务端目录结构和自动导入机制与普通 Node 项目不同。

---

## 📦 第一步：安装依赖

```bash
# 生产依赖
pnpm add drizzle-orm pg
# 开发依赖
pnpm add -D drizzle-kit @types/pg dotenv
```

| 包名          | 作用                         |
| ------------- | ---------------------------- |
| `drizzle-orm` | ORM 核心，提供类型安全的查询 |
| `pg`          | PostgreSQL 驱动              |
| `drizzle-kit` | 迁移工具，自动生成 SQL       |
| `@types/pg`   | TypeScript 类型（用于 pg）   |
| `dotenv`      | 开发时从 `.env` 加载环境变量 |

---

## 🔐 第二步：环境变量与配置

### 1. 创建 `.env` 文件（**必须加入 `.gitignore`**）

```env
# .env
NUXT_DATABASE_URL=postgresql://postgres:yourpassword@localhost:5432/yourdb
```

> 确保 URL 格式正确：`postgresql://用户名:密码@主机:端口/数据库名`

### 2. 配置 `nuxt.config.ts`

```ts
export default defineNuxtConfig({
  runtimeConfig: {
    databaseUrl: process.env.NUXT_DATABASE_URL, // 无默认值，强制从环境变量读取
  },
  // ... 其他配置
})
```

> **重要**：`databaseUrl` 必须从环境变量读取，不留默认值，避免生产环境误连本地数据库。

---

## 📁 第三步：组织 Schema 文件（核心）

Nuxt 项目中，建议将所有数据库相关文件放在 `server/db/` 下。**必须同时包含表定义和关系定义**。

### 目录结构

```text
server/
├── db/
│   ├── schema/
│   │   ├── users.ts
│   │   ├── comments.ts
│   │   ├── relations.ts
│   │   └── index.ts          # 统一导出
│   └── index.ts              # 数据库连接
```

### 3.1 定义表（以 users 和 comments 为例）

#### `server/db/schema/users.ts`

```ts
import {
  pgTable,
  serial,
  varchar,
  boolean,
  timestamp,
} from "drizzle-orm/pg-core"

export const users = pgTable("users", {
  id: serial("id").primaryKey(),
  githubId: varchar("github_id", { length: 39 }).notNull().unique(),
  username: varchar("username", { length: 100 }).notNull(),
  isAdmin: boolean("is_admin").default(false),
  createdAt: timestamp("created_at", { withTimezone: true }).defaultNow(),
})

export type User = typeof users.$inferSelect
export type NewUser = typeof users.$inferInsert
```

#### `server/db/schema/comments.ts`

```ts
import {
  pgTable,
  serial,
  integer,
  text,
  varchar,
  timestamp,
} from "drizzle-orm/pg-core"
import { users } from "./users"

export const comments = pgTable("comments", {
  id: serial("id").primaryKey(),
  userId: integer("user_id").references(() => users.id, {
    onDelete: "set null",
  }),
  content: text("content").notNull(),
  permalink: varchar("permalink", { length: 255 }).notNull(),
  parentId: integer("parent_id").references((): any => comments.id, {
    onDelete: "cascade",
  }),
  createdAt: timestamp("created_at", { withTimezone: true }).defaultNow(),
})

export type Comment = typeof comments.$inferSelect
export type NewComment = typeof comments.$inferInsert
```

### 3.2 定义关系（relations）

#### `server/db/schema/relations.ts`

```ts
import { relations } from "drizzle-orm"
import { users } from "./users"
import { comments } from "./comments"

// 评论 -> 用户（多对一）
export const commentsRelations = relations(comments, ({ one }) => ({
  user: one(users, {
    fields: [comments.userId],
    references: [users.id],
  }),
}))

// 用户 -> 评论（一对多）
export const usersRelations = relations(users, ({ many }) => ({
  comments: many(comments),
}))
```

### 3.3 统一导出

#### `server/db/schema/index.ts`

```ts
export * from "./users"
export * from "./comments"
export * from "./relations"
```

---

## 🔌 第四步：创建数据库连接工具

Nuxt 中，数据库连接应放在 `server/db/` 下以便导入。

### `server/db.ts`

```ts
import { drizzle } from "drizzle-orm/node-postgres"
import { Pool } from "pg"
import * as schema from "../db/schema" // 导入完整的 schema

const config = useRuntimeConfig()

const pool = new Pool({
  connectionString: config.databaseUrl,
})

// 导出函数，每次调用获取新连接（防止连接泄漏）
export const useDB = () => drizzle(pool, { schema })
```

> **注意**：必须传入完整的 `schema` 对象（包含表和关系），否则无法使用 `with` 等关系查询。

---

## 🧪 第五步：验证配置

创建测试 API 路由，确保一切正常。

### `server/api/test/db.get.ts`

```ts
import { sql } from "drizzle-orm"

export default defineEventHandler(async (event) => {
  try {
    const db = useDB()
    const result = await db.execute(sql`SELECT 1+1 as result`)
    return { success: true, data: result.rows[0] }
  } catch (error) {
    console.error("DB connection failed:", error)
    return { success: false, error: String(error) }
  }
})
```

访问 `http://localhost:3000/api/test/db`，若返回 `{ result: 2 }` 则连接成功。

---

## 📜 第六步：数据库迁移

### 6.1 配置 `drizzle.config.ts`

在项目根目录创建：

```ts
import "dotenv/config"
import { defineConfig } from "drizzle-kit"

export default defineConfig({
  out: "./server/db/migrations",
  schema: "./server/db/schema/index.ts", // 指向统一导出文件
  dialect: "postgresql",
  dbCredentials: {
    url: process.env.NUXT_DATABASE_URL!,
  },
})
```

### 6.2 生成迁移文件

```bash
npx drizzle-kit generate
```

这会在 `server/db/migrations` 生成 SQL 文件。

### 6.3 执行迁移

```bash
npx drizzle-kit migrate
```

**开发环境**也可以直接用 `push` 快速同步：

```bash
npx drizzle-kit push
```

> **生产环境**：必须使用 `generate` + `migrate`，并将生成的 SQL 文件纳入版本控制，以便回滚和审核。

---

## 🛠️ 第七步：在 API 中使用 Drizzle

### 查询示例（带关系）

#### `server/api/comments.get.ts`

```ts
import { comments } from "~/server/db/schema" // 需要显式导入表定义
import { eq, desc } from "drizzle-orm"

export default defineEventHandler(async (event) => {
  const query = getQuery(event)
  const permalink = query.permalink as string

  const db = useDB()
  const result = await db.query.comments.findMany({
    where: eq(comments.permalink, permalink),
    orderBy: [desc(comments.createdAt)],
    with: {
      user: {
        columns: { username: true },
      },
    },
  })

  return { success: true, data: result }
})
```

### 插入示例

#### `server/api/comments.post.ts`

```ts
import { comments, type NewComment } from "~/server/db/schema"
import { useDB } from "~~/server/db"

export default defineEventHandler(async (event) => {
  const body = await readBody(event)
  const db = useDB()

  const newComment: NewComment = {
    userId: body.userId,
    content: body.content,
    permalink: body.permalink,
  }

  const [inserted] = await db.insert(comments).values(newComment).returning()
  return { success: true, data: inserted }
})
```

---

## 🐛 常见错误与解决方案

### 错误 1：`Cannot read properties of undefined (reading 'referencedTable')`

**原因**：初始化 `drizzle` 时没有传入完整 schema（缺少 relations）。

**解决**：确保 `useDB()` 中 `drizzle(pool, { schema })` 的 `schema` 对象包含了 `relations` 导出。

### 错误 2：`useDB()` 未定义

**原因**：文件未放在 `server/utils/` 下，或 Nuxt 自动导入失效（需重启 dev）。

**解决**：检查文件路径，重启 `pnpm dev`。

### 错误 3：`db.query.comments.findMany` 不存在

**原因**：没有启用 Drizzle 的关系查询 API，需要传入 schema 并确保 `drizzle-orm` 版本支持。

**解决**：确认 `useDB()` 返回的是带有 `query` 属性的实例（即传入了 schema）。

### 错误 4：迁移时找不到表

**原因**：

`drizzle.config.ts` 中的 `schema` 路径错误，或指向的文件没有导出所有表。
**解决**：确保路径正确，且 `schema/index.ts` 导出了所有表。

### 错误 5：生产环境数据库连接失败

**原因**：环境变量未正确设置，或连接字符串格式错误。

**解决**：在服务器上检查 `NUXT_DATABASE_URL` 是否正确，并确保网络可达。

---

## 💡 最佳实践总结

1. **永远不要提交 `.env`**。
2. **schema 必须包含 relations**，否则无法使用 `with` 查询。
3. **数据库连接函数放在 `server/utils/`**，利用 Nuxt 自动导入。
4. **在 API 中显式导入表定义**（如 `import { users } from '~/server/db/schema'`）。
5. **生产环境使用迁移文件**，禁止用 `push`。
6. **测试环境与开发环境分离**，用不同的数据库 URL。

---

## 🎯 最终验证

完成以上步骤后，你应该能够：

- 通过 `pnpm dev` 启动项目，访问测试 API 得到 `{ result: 2 }`
- 使用 `drizzle-kit generate/migrate` 管理数据库变更
- 在 API 中正确查询带关联的数据
- 在生产环境中通过环境变量连接数据库

如果遇到任何问题，请对照每一步仔细检查。记住：**Drizzle 的官方文档是通用指南，Nuxt 集成需要根据其目录结构和自动导入机制进行调整**。这篇教程已为你铺平道路，祝你顺利！
