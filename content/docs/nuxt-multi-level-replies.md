---
title: 从零到一：为 Moongate 博客打造一个支持多级引用的评论区
description: 介绍了 Moongate 博客的评论区设计和实现，包括多级引用、扁平时间线、引用块跳转、用户认证、响应式设计等。
date: 2026-03-08
permalink: 2f14097a-5218-4adb-8204-fca2f2eddc85
series: comment
level: P3
tags: 
  - Nuxt
  - Security
---

<details>
<summary>📖 前言</summary>

如果你一路跟随我的系列教程，现在已经拥有了一个坚实的 Nuxt 4 项目基础：

- 通过[《Nuxt 4 集成 Drizzle ORM (PostgreSQL) 完整教程》](./nuxt-drizzle-postgresql.md)，你掌握了数据库的连接、模型定义与查询，为数据持久化铺平了道路。
- 在[《Nuxt 评论区完美支持 Markdown：从解析、高亮到安全渲染全攻略》](./nuxt-comment-markdown-guide.md)中，你学会了如何让用户输入的内容安全地支持 Markdown 和代码高亮。
- 而[《Nuxt 4 集成 GitHub 登录：从原理到实践》](./nuxt-oauth-github.md)则为你的应用添加了可靠的用户认证系统，确保只有真实用户才能参与互动。

现在，是时候将这些模块组合起来，打造一个真正**可用的、支持多级引用的评论区**了。本篇将基于上述基础，从数据库多态关联设计、后端 API 开发，到前端 Pinia 状态管理、组件交互打磨，一步步构建一个简洁但功能完备的评论系统。它不仅能处理常规的评论与回复，还支持**多级引用（引用的引用）**、**扁平时间线展示**、**点击引用块跳转并高亮**等实用功能，最终为你博客的读者提供一个沉浸式的讨论体验。

如果你尚未阅读前置教程，无需担心——我会在关键处说明引用，你也可以直接跟随本篇完成核心部分，待后续再补充细节。现在，让我们开始吧！🚀

</details>

## 📌 代码说明

本文所有代码均基于作者的项目环境编写，旨在清晰展示设计思路与核心实现。由于不同项目的配置（如数据库连接、环境变量、文件路径等）可能存在差异，请根据实际情况灵活调整。**直接复制粘贴可能无法运行，理解原理后再动手，才是最高效的学习方式。**

## 1. 背景与需求

在个人技术博客中，评论区是连接作者与读者的重要桥梁。常见的评论区实现要么过于简单（仅支持一级评论），要么依赖第三方服务（如 Disqus），无法自由定制和掌控数据。Moongate 博客需要一个**轻量、可定制、支持技术讨论深度**的评论区，具体要求包括：

- **多级引用**：读者可以针对某条评论或回复进行精确回应，形成对话链。
- **扁平时间线**：所有评论和回复按时间混合排列，避免视觉上的嵌套混乱。
- **引用块跳转**：点击引用内容可直接跳转到原评论并高亮，方便追溯上下文。
- **用户认证**：仅 GitHub 登录用户可发言，保证社区质量。
- **响应式设计**：在移动端同样有良好体验。

技术栈基于 Nuxt v4、Vue 3、Pinia v3、Drizzle ORM 和 PostgreSQL v18，UI 层采用 Nuxt UI 组件库。

## 2. 技术选型与设计思路

### 2.1 数据库设计：多态关联

传统的评论系统通常设计为“评论表 + 回复表”，回复通过外键指向所属评论。但这种结构无法支持“回复的回复”，即多级引用。为此我们选择了**多态关联**：

- **comments** 表存储独立评论（根节点）。
- **replies** 表存储所有回复，使用 `target_id` + `target_type` 字段指向任意目标（评论或另一条回复）。

这种设计灵活性极高，只需增加枚举类型 `target_type` 即可支持未来的扩展（如指向文章、用户等）。同时，使用 PostgreSQL 枚举确保数据完整性。

```sql
CREATE TYPE target_type AS ENUM ('comment', 'reply');

CREATE TABLE replies (
  id SERIAL PRIMARY KEY,
  target_id INTEGER NOT NULL,
  target_type target_type NOT NULL DEFAULT 'comment',
  user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
  content TEXT NOT NULL,
  created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_replies_target ON replies(target_id, target_type);
```

### 2.2 扁平时间线 vs 嵌套展示

传统嵌套回复（如楼中楼）在视觉上会随着深度增加而不断缩进，导致界面复杂，且阅读长对话链时容易迷失。扁平时间线将所有条目（评论和回复）按创建时间统一排序，每条回复通过引用块表明针对的对象。这样既保持了上下文的连贯性，又让界面干净清爽。

### 2.3 引用块设计

回复内容上方显示引用块，格式为 `@用户名: 摘要`，左侧用细边框线视觉弱化，但保留可点击性。用户点击引用块可跳转到被引用的原内容并高亮，利用现代 CSS 特性 `color-mix` 实现柔和的高亮效果。

## 3. 后端实现

### 3.1 数据库 Schema 与关系

使用 Drizzle ORM 定义表和关系。由于多态关联的特殊性，我们放弃在 ORM 中定义复杂的关系，而是在业务代码中手动组装数据，保证灵活性和可读性。

<details>
<summary>查看完整 schema</summary>

**`server/db/schema/comments.ts`**

```ts
import {
  pgTable,
  serial,
  varchar,
  timestamp,
  integer,
  text,
} from "drizzle-orm/pg-core";
import { users } from "./users";

export const comments = pgTable("comments", {
  id: serial("id").primaryKey(),
  user_id: integer("user_id").references(() => users.id, {
    onDelete: "set null",
  }),
  content: text("content").notNull(),
  permalink: varchar("permalink", { length: 255 }).notNull(),
  created_at: timestamp("created_at", { withTimezone: true }).defaultNow(),
});

export type CommentSelect = typeof comments.$inferSelect;
export type CommentInsert = typeof comments.$inferInsert;
```

**`server/db/schema/replies.ts`**（含枚举）

```ts
import {
  pgEnum,
  pgTable,
  serial,
  integer,
  text,
  timestamp,
} from "drizzle-orm/pg-core";
import { users } from "./users";

export const targetTypeEnum = pgEnum("target_type", ["comment", "reply"]);

export const replies = pgTable("replies", {
  id: serial("id").primaryKey(),
  target_id: integer("target_id").notNull(),
  target_type: targetTypeEnum("target_type").notNull().default("comment"),
  user_id: integer("user_id").references(() => users.id, {
    onDelete: "set null",
  }),
  content: text("content").notNull(),
  created_at: timestamp("created_at", { withTimezone: true }).defaultNow(),
});

export type ReplySelect = typeof replies.$inferSelect;
export type ReplyInsert = typeof replies.$inferInsert;
```

**`server/db/schema/users.ts`**

```ts
import {
  pgTable,
  serial,
  varchar,
  boolean,
  timestamp,
} from "drizzle-orm/pg-core";

export const users = pgTable("users", {
  id: serial("id").primaryKey(),
  github_id: varchar("github_id", { length: 39 }).notNull().unique(),
  username: varchar("username", { length: 100 }).notNull(),
  is_admin: boolean("is_admin").default(false),
  created_at: timestamp("created_at", { withTimezone: true }).defaultNow(),
});

export type UserSelect = typeof users.$inferSelect;
export type UserInsert = typeof users.$inferInsert;
```

</details>

<details>
<summary>查看完整关系表</summary>

```ts
import { relations } from "drizzle-orm";
import { users, comments, replies } from "./index";

// comments 表的关系
export const commentsRelations = relations(comments, ({ one, many }) => ({
  user: one(users, {
    fields: [comments.user_id],
    references: [users.id],
  }),
  // 指向此评论的回复（通过 target_id 和 target_type 筛选）
  // 注意：这只是一个定义，实际查询时需在 where 中添加 target_type = 'comment'
  repliesFrom: many(replies, {
    relationName: "commentTarget",
  }),
}));

// users 表的关系（不变）
export const usersRelations = relations(users, ({ many }) => ({
  comments: many(comments),
  replies: many(replies),
}));

// replies 表的关系
export const repliesRelations = relations(replies, ({ one }) => ({
  user: one(users, {
    fields: [replies.user_id],
    references: [users.id],
  }),
  // 当 target_type = 'comment' 时，指向被引用的评论
  targetComment: one(comments, {
    fields: [replies.target_id],
    references: [comments.id],
    relationName: "commentTarget", // 与 commentsRelations 中的 repliesFrom 对应
  }),
  // 当 target_type = 'reply' 时，指向被引用的回复
  targetReply: one(replies, {
    fields: [replies.target_id],
    references: [replies.id],
    relationName: "replyTarget",
  }),
}));
```

</details>

### 3.2 获取时间线接口

该接口需要返回当前文章的所有评论和回复，并为每条回复附上被引用内容的摘要（`reply_to`）。我们采用两步查询：先获取所有评论，再获取所有回复，然后在内存中组装并排序。

**`server/api/comment/timeline.get.ts`**

<details>
<summary>完整的获取时间线接口代码</summary>

```ts
import { eq, sql } from "drizzle-orm";
import { useDB } from "~~/server/db";
import { comments, replies, users } from "~~/server/db/schema";

export default defineEventHandler(async (event) => {
  const { permalink } = getQuery(event);
  if (!permalink)
    throw createError({ status: 400, statusText: "缺少 permalink" });

  const db = useDB();

  // 获取所有评论
  const commentsData = await db
    .select({
      id: comments.id,
      content: comments.content,
      user_id: comments.user_id,
      created_at: comments.created_at,
      user: { username: users.username, is_admin: users.is_admin },
    })
    .from(comments)
    .leftJoin(users, eq(comments.user_id, users.id))
    .where(eq(comments.permalink, permalink as string))
    .orderBy(comments.created_at);

  // 构建评论映射，供后续引用摘要使用
  const commentMap = new Map(
    commentsData.map((c) => [
      c.id,
      { content: c.content, username: c.user?.username },
    ]),
  );

  // 获取所有回复（限制属于当前文章）
  const repliesData = await db
    .select({
      id: replies.id,
      content: replies.content,
      user_id: replies.user_id,
      created_at: replies.created_at,
      target_id: replies.target_id,
      target_type: replies.target_type,
      user: { username: users.username, is_admin: users.is_admin },
    })
    .from(replies)
    .leftJoin(users, eq(replies.user_id, users.id))
    .where(
      sql`${replies.target_id} IN (SELECT id FROM comments WHERE permalink = ${permalink})
                OR ${replies.target_id} IN (SELECT id FROM replies r2 WHERE r2.target_id IN (SELECT id FROM comments WHERE permalink = ${permalink}))`,
    )
    .orderBy(replies.created_at);

  // 构建回复映射
  const replyMap = new Map(
    repliesData.map((r) => [
      r.id,
      { content: r.content, username: r.user?.username },
    ]),
  );

  // 格式化评论
  const formattedComments = commentsData.map((c) => ({
    id: c.id,
    type: "comment" as const,
    content: c.content,
    user: c.user,
    created_at: c.created_at,
  }));

  // 格式化回复并添加引用摘要
  const formattedReplies = repliesData.map((r) => {
    const target =
      r.target_type === "comment"
        ? commentMap.get(r.target_id)
        : replyMap.get(r.target_id);

    return {
      id: r.id,
      type: "reply" as const,
      content: r.content,
      user: r.user,
      created_at: r.created_at,
      target_id: r.target_id,
      target_type: r.target_type,
      reply_to: target
        ? {
            id: r.target_id,
            type: r.target_type,
            username: target.username,
            excerpt:
              target.content.substring(0, 100) +
              (target.content.length > 100 ? "…" : ""),
          }
        : null,
    };
  });

  // 合并并按时间排序
  const timeline = [...formattedComments, ...formattedReplies].sort(
    (a, b) =>
      new Date(a.created_at).getTime() - new Date(b.created_at).getTime(),
  );

  return { success: true, data: timeline };
});
```

</details>

### 3.3 提交评论接口

简单地将用户输入插入 `comments` 表，返回新评论数据。

**`server/api/comment/post.ts`**（略，可参考类似逻辑）

### 3.4 提交回复接口

需要验证目标是否存在，并处理多态引用。注意使用 `createError` 抛出规范错误。

**`server/api/reply/post.ts`**

<details>
<summary>查看完整的回复接口代码</summary>

```ts
import { eq } from "drizzle-orm";
import { useDB } from "~~/server/db";
import { replies, users, comments } from "~~/server/db/schema";

export default defineEventHandler(async (event) => {
  const body = await readBody(event);
  const session = await getUserSession(event);

  // 参数校验
  if (
    !body.target_id ||
    !["comment", "reply"].includes(body.target_type) ||
    !body.content?.trim()
  ) {
    throw createError({ status: 400, statusText: "参数错误" });
  }
  if (!session.user?.id)
    throw createError({ status: 401, statusText: "请先登录" });

  const db = useDB();

  // 验证用户存在
  const user = await db.query.users.findFirst({
    where: eq(users.id, session.user.id),
  });
  if (!user) {
    await clearUserSession(event);
    throw createError({ status: 401, statusText: "用户不存在" });
  }

  // 验证目标存在
  if (body.target_type === "comment") {
    const comment = await db
      .select()
      .from(comments)
      .where(eq(comments.id, body.target_id))
      .limit(1);
    if (!comment.length)
      throw createError({ status: 404, statusText: "评论不存在" });
  } else {
    const reply = await db
      .select()
      .from(replies)
      .where(eq(replies.id, body.target_id))
      .limit(1);
    if (!reply.length)
      throw createError({ status: 404, statusText: "回复不存在" });
  }

  // 插入回复
  try {
    const [newReply] = await db
      .insert(replies)
      .values({
        user_id: user.id,
        target_id: body.target_id,
        target_type: body.target_type,
        content: body.content.trim(),
      })
      .returning();

    event.node.res.statusCode = 201;
    return { success: true, data: newReply };
  } catch (error) {
    console.error(error);
    throw createError({ status: 500, statusText: "服务器内部错误" });
  }
});
```

</details>

## 4. 前端状态管理

使用 Pinia 管理评论相关状态，包括当前输入内容、评论列表、加载状态等。

**`stores/comment.ts`**

<details>
<summary>查看完整的pinia代码</summary>

```ts
import { defineStore } from "pinia";

export const useCommentStore = defineStore("comment", () => {
  const comment = ref(""); // 当前输入的评论内容
  const permalink = ref(""); // 当前文章标识
  const commentList = ref<any[]>([]); // 扁平时间线数据
  const loading = ref(false); // 获取列表加载状态
  const submitting = ref(false); // 提交评论/回复中

  const getCommentList = async (newPermalink?: string) => {
    if (newPermalink) permalink.value = newPermalink;
    if (!permalink.value) return;
    loading.value = true;
    try {
      const { data } = await $fetch("/api/comment/timeline", {
        query: { permalink: permalink.value },
      });
      commentList.value = data || [];
    } catch (error) {
      console.error("获取评论失败", error);
      commentList.value = [];
    } finally {
      loading.value = false;
    }
  };

  const submitComment = async () => {
    if (!comment.value.trim() || submitting.value) return false;
    submitting.value = true;
    try {
      const response = await $fetch("/api/comment/post", {
        method: "POST",
        body: { content: comment.value, permalink: permalink.value },
      });
      if (response.success) {
        comment.value = "";
        await getCommentList();
        return true;
      }
      return false;
    } catch (error) {
      console.error(error);
      return false;
    } finally {
      submitting.value = false;
    }
  };

  const submitReply = async (
    targetId: number,
    targetType: string,
    content: string,
  ) => {
    if (!content.trim() || submitting.value) return false;
    submitting.value = true;
    try {
      const response = await $fetch("/api/reply/post", {
        method: "POST",
        body: { target_id: targetId, target_type: targetType, content },
      });
      if (response.success) {
        await getCommentList();
        return true;
      }
      return false;
    } catch (error) {
      console.error(error);
      return false;
    } finally {
      submitting.value = false;
    }
  };

  return {
    comment,
    permalink,
    commentList,
    loading,
    submitting,
    getCommentList,
    submitComment,
    submitReply,
  };
});
```

</details>

## 5. 前端组件实现

### 5.1 评论区容器组件

**`components/docs/CommentSection.vue`**

<details>
<summary>查看完整组件代码</summary>

```vue
<template>
  <details ref="containerRef" @toggle="onDetailsToggle">
    <summary class="text-center">{{ t("comment.section") }}</summary>

    <ClientOnly>
      <DocsCommentInputPreview
        v-model="commentStore.comment"
        :debounce-time="500"
        :permalink="prop.permalink"
        storage-type="none"
      />
    </ClientOnly>

    <div class="flex justify-end mb-8">
      <ClientOnly v-if="loggedIn">
        <UButton
          :disabled="!commentStore.comment.trim() || commentStore.submitting"
          :loading="commentStore.submitting"
          :label="t('comment.actions.send')"
          size="lg"
          @click="commentStore.submitComment()"
        />
      </ClientOnly>
      <div v-else class="flex items-center gap-2">
        <p>{{ t("comment.status.login_to_comment") }}</p>
        <SharedLogin />
      </div>
    </div>

    <div class="mt-4 min-h-50">
      <DocsCommentList v-if="commentStore.commentList.length" />
      <div v-else class="text-center">{{ t("comment.status.noComments") }}</div>
    </div>
  </details>
</template>

<script setup>
import { useCommentStore } from "~/stores/comment";
const commentStore = useCommentStore();
const { t } = useI18n();
const { containerRef, onDetailsToggle } = useDetailsScroll();
const { loggedIn } = useUserSession();

const prop = defineProps({ permalink: { type: String, required: true } });

watch(
  () => prop.permalink,
  (newPermalink) => {
    commentStore.getCommentList(newPermalink);
  },
  { immediate: true },
);
</script>
```

</details>

### 5.2 评论列表组件

**`components/docs/CommentList.vue`**

<details>
<summary>查看完整组件代码</summary>

```vue
<template>
  <div class="max-h-150 overflow-y-auto mt-4 space-y-4">
    <div
      v-for="item in commentStore.commentList"
      :key="`${item.type}-${item.id}`"
      :id="`${item.type}-${item.id}`"
      class="group relative py-6 border-b border-ui-border/30 hover:bg-ui-bg-elevated/50 transition-colors"
    >
      <!-- 引用块（仅回复） -->
      <div
        v-if="item.type === 'reply' && item.reply_to"
        class="mb-2 pl-3 text-sm text-ui-text-muted/80 border-l-2 border-ui-border/40 hover:border-ui-primary/40 transition-colors cursor-pointer"
        @click="scrollToElement(item.reply_to.id, item.reply_to.type)"
      >
        <span class="font-medium">@{{ item.reply_to.username }}</span>
        <span class="italic ml-1">{{ item.reply_to.excerpt }}</span>
      </div>

      <!-- 作者信息 -->
      <div class="flex items-center gap-2 mb-1 text-xs">
        <span class="font-mono font-bold text-ui-text">{{
          item.user?.username
        }}</span>
        <span
          v-if="item.user?.is_admin"
          class="text-[9px] px-1 bg-ui-primary/10 text-ui-primary border border-ui-primary/20"
        >
          {{ t("comment.badge.admin") }}
        </span>
        <span class="text-ui-text-muted/60 text-[10px]">
          {{ dayjs(item.created_at).format("MM-DD HH:mm") }}
        </span>
      </div>

      <!-- 评论内容 -->
      <div
        class="text-ui-text/90 text-sm leading-relaxed break-words max-w-3xl"
      >
        <docsMarkdownRenderer :content="item.content" />
      </div>

      <!-- 回复按钮 -->
      <div class="flex justify-end mt-2">
        <button
          class="text-xs text-ui-text-muted/70 hover:text-ui-primary transition-colors cursor-pointer"
          @click="toggleReply(item.id, item.type)"
        >
          {{ t("comment.actions.reply") }}
        </button>
      </div>

      <!-- 回复输入框 -->
      <div
        v-if="replyingTo?.id === item.id && replyingTo?.type === item.type"
        class="mt-3 pt-3 border-t border-ui-border/20"
      >
        <DocsCommentInputPreview
          v-model="reply"
          :permalink="commentStore.permalink"
        />
        <div class="flex justify-end gap-2 mt-2">
          <UButton size="sm" variant="ghost" @click="cancelReply">
            {{ t("common.cancel") }}
          </UButton>
          <UButton
            size="sm"
            :disabled="!reply.trim() || commentStore.submitting"
            :loading="commentStore.submitting"
            @click="handleReply"
          >
            {{ t("comment.actions.send") }}
          </UButton>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import dayjs from 'dayjs';
import { useCommentStore } from '~/stores/comment';

const commentStore = useCommentStore();
const { user, loggedIn } = useUserSession();
const { t } = useI18n();

const replyingTo = ref<{ id: number; type: string } | null>(null);
const reply = ref('');

const toggleReply = (id: number, type: string) => {
  if (replyingTo.value?.id === id && replyingTo.value?.type === type) {
    replyingTo.value = null;
  } else {
    replyingTo.value = { id, type };
  }
  reply.value = '';
};

const cancelReply = () => {
  replyingTo.value = null;
  reply.value = '';
};

const handleReply = async () => {
  if (!replyingTo.value) return;
  const success = await commentStore.submitReply(
    replyingTo.value.id,
    replyingTo.value.type,
    reply.value
  );
  if (success) {
    cancelReply();
  }
};

const scrollToElement = (id: number, type: string) => {
  const el = document.getElementById(`${type}-${id}`);
  if (el) {
    el.scrollIntoView({ behavior: 'smooth', block: 'center' });
    el.classList.add('highlight-flash');
    setTimeout(() => el.classList.remove('highlight-flash'), 1000);
  }
};
</script>

<style scoped>
.highlight-flash {
  background-color: color-mix(in srgb, var(--ui-primary), transparent 90%);
  transition: background-color 0.3s ease;
}
</style>
```

</details>

### 5.3 输入预览组件

**`components/docs/CommentInputPreview.vue`** 实现了带防抖的 Markdown 输入和预览。

<details>
<summary>查看完整组件代码</summary>

```vue
<template>
  <div class="grid grid-cols-1 md:grid-cols-2 gap-6 mt-4 mb-6">
    <!-- 左侧预览 -->
    <div class="bg-ui-bg-elevated">
      <div class="text-xs font-mono text-ui-text-muted mb-2 tracking-wider">
        // {{ t("comment.input.preview") }}
      </div>
      <DocsMarkdownRenderer
        class="text-ui-text/90 text-base leading-relaxed"
        :content="localValue"
      />
    </div>

    <!-- 右侧输入 -->
    <div class="bg-ui-bg">
      <div class="text-xs font-mono text-ui-text-muted mb-2 tracking-wider">
        // {{ t("comment.input.input") }}
      </div>
      <UTextarea
        :model-value="localValue"
        autoresize
        :rows="5"
        variant="none"
        :placeholder="t('comment.input.placeholder')"
        class="w-full bg-transparent border-0 focus:ring-0 p-0 text-ui-text placeholder:text-ui-text-muted/50 font-mono text-sm"
        @update:model-value="(value) => handleInput(value)"
      />
    </div>
  </div>
</template>

<script lang="ts" setup>
import { useDebounceFn } from "@vueuse/core";
const { t } = useI18n();

const props = defineProps({
  modelValue: { type: String, default: "" }, // v-model 绑定的值
  debounceTime: { type: Number, default: 300 }, // 防抖延迟（毫秒），默认 300ms
  permalink: { type: String, required: true }, // 用于构建存储 key
  storageType: {
    type: String,
    default: "none",
    validator: (val: string) => ["session", "local", "none"].includes(val),
  },
});

const emit = defineEmits(["update:modelValue"]);

// 创建一个 ref 来存储本地输入值
const localValue = ref(props.modelValue);

// 监听父组件 prop 变化，同步到本地
watch(
  () => props.modelValue,
  (newVal) => {
    localValue.value = newVal;
  },
);

// 用防抖函数包装 emit
const debouncedEmit = useDebounceFn((value: string) => {
  emit("update:modelValue", value);
}, props.debounceTime);

// 当输入框的文本改变时
const handleInput = (value: string) => {
  localValue.value = value; // 立即更新预览
  debouncedEmit(value); // 防抖更新父组件
};
</script>
```

</details>

## 6. 交互细节打磨

### 6.1 防抖输入

在 `CommentInputPreview` 中使用 `useDebounceFn` 实现用户停止输入 300ms 后才更新父组件，避免频繁请求。

### 6.2 点击引用跳转并高亮

如上代码所示，点击引用块时调用 `scrollToElement`，利用 `scrollIntoView` 平滑滚动到目标元素，并添加一个临时 CSS 类实现高亮。高亮采用 `color-mix` 生成半透明背景色，简洁现代。

### 6.3 回复框的开关管理

每个条目独立控制回复框的展开/关闭，使用 `replyingTo` 记录目标，确保同时只能打开一个回复框，防止界面混乱。

### 6.4 提交后自动刷新

提交评论或回复成功后，调用 `getCommentList` 刷新整个列表，确保数据一致性。

## 7. 总结与展望

至此，Moongate 博客拥有了一套功能完备、体验优雅的评论区系统。它不仅支持多级引用、扁平时间线、引用跳转高亮，还具备良好的响应式设计和用户体验。

未来计划：

- 开源此评论系统，让更多开发者受益。
- 增加删除、编辑评论功能。
- 添加 @ 用户通知机制。

通过本项目的实践，我们深刻体会到合理的数据设计和灵活的架构能为后续扩展打下坚实基础。希望这篇文章能为你自建评论区提供有价值的参考。如果你有任何问题或建议，欢迎在评论区留言。
