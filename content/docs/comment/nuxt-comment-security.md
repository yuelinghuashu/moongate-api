---
title: 为评论区添加内容过滤与安全防护
description: 为 Nuxt 评论区增加敏感词过滤、文档归属验证、防重复提交与限流，构建多层安全防护体系。包含前端实时验证、后端严格校验、递归 CTE 归属验证及生产环境建议。
date: 2026-03-23
series: comment
tags:
  - Nuxt
  - Vue
  - Security
---

## 1. 背景与需求

在开放的评论区中，可能面临以下风险：

- **敏感词**：用户可能发布违规内容，影响社区氛围。
- **恶意灌水**：大量重复评论或垃圾广告。
- **跨文档污染**：通过伪造 `permalink` 将评论插入到不存在的文档或他人文章。
- **XSS 攻击**：通过 Markdown 注入恶意脚本（已在前文中通过安全渲染解决）。

为了维护评论区秩序，我们需要增加以下防护：

- **前端实时敏感词提示**：提升用户体验，减少无效提交。
- **后端严格过滤**：作为最后防线，确保入库内容安全。
- **文档归属验证**：确保评论只属于当前文档。
- **防重复提交与限流**：防止恶意刷屏。

## 2. 整体架构

过滤功能涉及前后端协作：

```text
用户输入 → 前端实时检测（可选） → 提交 → 后端校验（敏感词、长度、文档归属） → 入库 → 返回结果
```

本文将分模块实现。

## 3. 敏感词过滤

### 3.1 词库设计

敏感词库应仅包含**底线词汇**，避免过度拦截技术术语。同时引入**技术白名单**，允许某些专业词汇（如“暴力破解”）正常使用。

```ts
// utils/commentValidator.ts

// 敏感词列表（仅底线词汇）
const blockedKeywords = [
  "广告",
  "垃圾",
  "诈骗",
  "赌博",
  "色情",
  "暴力",
  "fuck",
  "shit",
  "damn",
]

// 技术白名单（豁免词汇，需与敏感词冲突时使用）
const technicalWhitelist = [
  "暴力破解",
  "暴力枚举",
  "攻击向量",
  "死锁",
  "死循环",
  "垃圾回收",
  "垃圾收集",
]
```

### 3.2 验证函数实现

为避免将白名单词汇误判为敏感词，采用**先移除白名单内容，再检测敏感词**的策略，而非简单的子串匹配。

```ts
// utils/commentValidator.ts

// 构建敏感词正则（大小写不敏感，自动转义）
const sensitiveRegex = new RegExp(
  blockedKeywords
    .map((word) => word.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"))
    .join("|"),
  "i",
)

export interface ValidationResult {
  valid: boolean
  message?: string
  foundWords?: string[]
}

/**
 * 验证评论内容
 * @param content 用户输入的文本
 * @param maxLength 最大长度，默认 5000（考虑代码块）
 */
export function validateComment(
  content: string,
  maxLength: number = 5000,
): ValidationResult {
  // 1. 空内容检查
  if (!content?.trim()) {
    return { valid: false, message: "评论内容不能为空" }
  }

  // 2. 长度限制
  if (content.length > maxLength) {
    return { valid: false, message: `评论内容不能超过 ${maxLength} 个字符` }
  }

  // 3. 移除白名单词汇，避免误判
  let text = content
  for (const word of technicalWhitelist) {
    text = text.replace(new RegExp(word, "gi"), "")
  }

  // 4. 敏感词检测
  const matches = text.match(sensitiveRegex)
  if (matches) {
    const found = [...new Set(matches)]
    return {
      valid: false,
      message: `包含敏感词: ${found.join(", ")}`,
      foundWords: found,
    }
  }

  return { valid: true }
}
```

**说明**：

- 通过先剔除白名单词汇，确保“垃圾回收”不会被误判为“垃圾”。
- 长度默认 5000 字符，足以容纳中等长度的代码块和技术讨论（一段 30 行的代码约 2400 字符）。
- 正则表达式一次匹配所有敏感词，性能优于循环 `includes`。

### 3.3 前端实时验证与提交按钮联动

#### 3.3.1 修改输入预览组件

在 `components/docs/CommentInputPreview.vue` 中增加实时验证和字符计数。组件的基础模板（预览/输入双栏布局）与[《多级引用评论区》§5.3](./nuxt-multi-level-replies)完全一致，此处仅展示**新增的验证逻辑与 UI 差异**：

```vue
<!-- 在原有模板的输入栏中新增（位于 UTextarea 之后）： -->

<!-- 新增1：验证错误提示 -->
<div v-if="validationError" class="mt-2 text-xs font-mono text-ui-error">
  // {{ validationError }}
</div>

<!-- 新增2：字符计数 -->
<div class="text-xs text-ui-text-muted text-right mt-1">
  {{ localValue.length }}/{{ maxLength }}
</div>
```

```ts
// script 中新增的部分：
import { validateComment } from "~/utils/commentValidator";

// props 新增 maxLength（默认 5000）
maxLength: { type: Number, default: 5000 }

// 新增状态
const validationError = ref('');

// 新增验证函数
const validate = (value: string) => {
  const result = validateComment(value, props.maxLength);
  validationError.value = result.valid ? '' : result.message;
  return result.valid;
};

// 修改 handleInput：在原有防抖逻辑基础上增加实时验证
const handleInput = (value: string) => {
  localValue.value = value;
  validate(value);          // ★ 新增：实时验证，仅用于显示错误提示
  debouncedEmit(value);     // 原有防抖逻辑不变
};

// 新增：监听外部 modelValue 变化时重新验证
watch(() => props.modelValue, (newVal) => {
  localValue.value = newVal;
  validate(newVal);         // ★ 新增
});

// 新增：挂载时执行初始验证
onMounted(() => validate(localValue.value));
```

#### 3.3.2 在 Store 中管理验证状态

为了让提交按钮能够响应验证结果，需要在评论 store 中增加计算属性：

```ts
// stores/comment.ts
import { validateComment } from "~/utils/commentValidator"

export const useCommentStore = defineStore("comment", () => {
  const comment = ref("")
  // ... 其他状态

  const isCommentValid = computed(() => {
    const { valid } = validateComment(comment.value, 5000)
    return valid
  })

  return {
    comment,
    isCommentValid,
    // ... 其他
  }
})
```

#### 3.3.3 修改评论区容器组件

在 `CommentSection.vue` 中，使用 store 的计算属性禁用提交按钮，并显示后端错误。

```vue
<template>
  <!-- 其他部分... -->
  <div class="flex justify-end mb-8">
    <ClientOnly v-if="loggedIn">
      <UButton
        :disabled="
          !commentStore.comment.trim() ||
          commentStore.submitting ||
          !commentStore.isCommentValid
        "
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

  <!-- 显示后端错误 -->
  <div v-if="commentStore.error" class="text-ui-error text-sm mt-2">
    {{ commentStore.error }}
  </div>
  <!-- 其他部分... -->
</template>
```

### 3.4 后端严格验证

在评论和回复的 API 中，必须再次调用 `validateComment`，确保任何绕过前端的请求都被拦截。所有 API 统一返回对象格式（不使用 `throw createError`），以便前端统一处理。

#### 修改 `server/api/comment/post.ts`

```ts
import { eq } from "drizzle-orm"
import { useDB } from "~~/server/db"
import { comments, users } from "~~/server/db/schema"
import { validateComment } from "~/../utils/commentValidator"

export default defineEventHandler(async (event) => {
  const body = await readBody(event)
  const session = await getUserSession(event)

  // 1. 验证 session
  if (!session.user?.id) {
    return { success: false, status: 401, message: "请先登录" }
  }

  // 2. 查询用户是否存在
  const db = useDB()
  const user = await db.query.users.findFirst({
    where: eq(users.id, session.user.id),
  })
  if (!user) {
    await clearUserSession(event)
    return { success: false, status: 401, message: "用户不存在" }
  }

  // 3. 验证评论内容
  const content = body.content?.trim()
  const { valid, message } = validateComment(content, 5000)
  if (!valid) {
    return { success: false, status: 400, message: message || "评论内容无效" }
  }

  // 4. 验证 permalink 非空
  if (!body.permalink) {
    return { success: false, status: 400, message: "永久链接不能为空" }
  }

  // 5. 验证文档是否存在（假设使用 Nuxt Content，具体实现需根据项目调整）
  // 注意：这里使用了 `#content/server` 虚拟模块，实际项目中可能需要替换为其他方式。
  // 若无法验证，应返回错误而不是放行。
  try {
    const { queryCollection } = await import("#content/server")
    const doc = await queryCollection("docs")
      .where("permalink", "=", body.permalink)
      .first()
    if (!doc) {
      return { success: false, status: 404, message: "文档不存在" }
    }
  } catch (err) {
    console.error("文档存在性验证失败，请检查 Nuxt Content 服务端配置", err)
    return {
      success: false,
      status: 500,
      message: "服务器配置错误，无法验证文档",
    }
  }

  // 6. 保存评论到数据库
  try {
    const [comment] = await db
      .insert(comments)
      .values({
        user_id: user.id,
        content: body.content.trim(),
        permalink: body.permalink,
      })
      .returning()

    return {
      success: true,
      status: 201,
      message: "评论存储成功",
      data: { ...comment },
    }
  } catch (error) {
    console.error("评论存储失败", error)
    return { success: false, status: 500, message: "评论存储失败" }
  }
})
```

同样修改 `server/api/reply/post.ts`（完整代码见第 4 节）。

## 4. 文档归属验证

### 4.1 评论 API 验证文档存在

已在评论 API 中实现（见 3.4 节）。注意：文档存在性验证依赖于 Nuxt Content 的服务端能力，若不可用，建议在数据库中维护 `documents` 表，并在评论表中关联文档 ID。

### 4.2 回复 API 完整代码（含归属验证）

由于回复表没有 `permalink` 字段，需要通过目标评论的 `permalink` 来验证。下面给出完整的 `reply/post.ts` 实现，包含用户认证、参数校验、敏感词过滤、归属验证和限流（可选）。

```ts
import { eq, sql } from "drizzle-orm"
import { useDB } from "~~/server/db"
import { replies, users, comments } from "~~/server/db/schema"
import { validateComment } from "~/../utils/commentValidator"

// 注意：内存限流仅用于演示，生产环境请替换为 Redis 或数据库
const rateLimit = new Map()

export default defineEventHandler(async (event) => {
  const body = await readBody(event)
  const session = await getUserSession(event)

  // 1. 参数校验
  if (
    !body.target_id ||
    !["comment", "reply"].includes(body.target_type) ||
    !body.content?.trim()
  ) {
    return { success: false, status: 400, message: "参数错误" }
  }
  if (!body.permalink) {
    return { success: false, status: 400, message: "缺少 permalink 参数" }
  }
  // 确保 target_id 为数字
  const targetId = Number(body.target_id)
  if (isNaN(targetId)) {
    return { success: false, status: 400, message: "target_id 必须为数字" }
  }

  // 2. 验证 session
  if (!session.user?.id) {
    return { success: false, status: 401, message: "请先登录" }
  }

  const db = useDB()

  // 3. 验证用户存在
  const user = await db.query.users.findFirst({
    where: eq(users.id, session.user.id),
  })
  if (!user) {
    await clearUserSession(event)
    return { success: false, status: 401, message: "用户不存在" }
  }

  // 4. 验证回复内容
  const content = body.content?.trim()
  const { valid, message } = validateComment(content, 5000)
  if (!valid) {
    return {
      success: false,
      status: 400,
      message: message || "回复包含敏感词",
    }
  }

  // 5. 验证目标存在并检查是否属于当前文档
  let targetPermalink = ""
  if (body.target_type === "comment") {
    const comment = await db.query.comments.findFirst({
      where: eq(comments.id, targetId),
    })
    if (!comment) {
      return { success: false, status: 404, message: "评论不存在" }
    }
    targetPermalink = comment.permalink
  } else {
    // 目标是回复，需要向上追溯找到根评论的 permalink
    // 使用递归 CTE 查询（PostgreSQL）
    const result = await db.execute(sql`
      WITH RECURSIVE reply_chain AS (
        SELECT id, target_id, target_type
        FROM replies
        WHERE id = ${targetId}
        UNION ALL
        SELECT r.id, r.target_id, r.target_type
        FROM replies r
        JOIN reply_chain rc ON rc.target_id = r.id AND rc.target_type = 'reply'
      )
      SELECT c.permalink
      FROM reply_chain rc
      JOIN comments c ON c.id = rc.target_id AND rc.target_type = 'comment'
      LIMIT 1
    `)
    if (!result.rows.length) {
      return { success: false, status: 404, message: "目标评论或回复不存在" }
    }
    targetPermalink = result.rows[0].permalink
  }

  if (targetPermalink !== body.permalink) {
    return { success: false, status: 400, message: "目标不属于当前文档" }
  }

  // 6. 可选：防重复提交限流（同一用户对同一文档 1 分钟内只能回复一次）
  // 注意：内存限流仅用于演示，生产环境请使用 Redis 或数据库
  const rateKey = `${user.id}:${body.permalink}`
  const last = rateLimit.get(rateKey)
  if (last && Date.now() - last < 60000) {
    return { success: false, status: 429, message: "操作过于频繁，请稍后再试" }
  }
  rateLimit.set(rateKey, Date.now())
  setTimeout(() => rateLimit.delete(rateKey), 60000) // 自动清理过期条目

  // 7. 保存回复
  try {
    const [newReply] = await db
      .insert(replies)
      .values({
        user_id: user.id,
        target_id: targetId,
        target_type: body.target_type,
        content: content,
      })
      .returning()

    return {
      success: true,
      status: 201,
      message: "回复成功",
      data: newReply,
    }
  } catch (error) {
    console.error(error)
    return { success: false, status: 500, message: "服务器内部错误" }
  }
})
```

**说明**：

- 递归 CTE 查询支持多级引用（回复的回复），并获取根评论的 `permalink`。
- 若不需要多级引用，可以限制 `target_type` 只能为 `'comment'`，简化归属验证。
- 限流使用内存 Map，**生产环境请替换为 Redis 或数据库实现**，以保证多实例同步。
- 增加了 `target_id` 类型校验，确保为有效数字。

## 5. 防重复提交与限流

### 5.1 前端禁用按钮

已在 `CommentSection.vue` 中实现，通过 `submitting` 状态禁用提交按钮。

### 5.2 后端限流

已在回复 API 中添加简单内存限流示例（每分钟 1 次）。**生产环境请务必替换为 Redis 或数据库**，避免内存丢失或跨实例不同步。

## 6. 多语言翻译

新增的文案需要添加到多语言文件中。以下提供中、英、日三语示例，可根据实际路径调整。

### /i18n/locales/zh_cn.json

````json
{
  "comment": {
    "input": {
      "preview": "预览",
      "input": "输入",
      "placeholder": "支持 Markdown，代码块请用 ``` 包裹..."
    }
  }
}
````

### /i18n/locales/en.json

````json
{
  "comment": {
    "input": {
      "preview": "Preview",
      "input": "Input",
      "placeholder": "Markdown supported, use ``` for code blocks..."
    }
  }
}
````

### /i18n/locales/ja.json

````json
{
  "comment": {
    "input": {
      "preview": "プレビュー",
      "input": "入力",
      "placeholder": "Markdown 対応、コードブロックは ``` で囲んでください..."
    }
  }
}
````

## 7. 整合与测试

### 7.1 文件清单

- `utils/commentValidator.ts` – 敏感词验证逻辑（含白名单剔除）
- `components/docs/CommentInputPreview.vue` – 前端实时验证、字符计数
- `stores/comment.ts` – 添加 `isCommentValid` 计算属性和 `error` 状态
- `server/api/comment/post.ts` – 后端验证 + 文档归属
- `server/api/reply/post.ts` – 后端验证 + 归属验证 + 限流（含类型校验）

### 7.2 测试用例

- **空内容** → 提示“评论内容不能为空”
- **超长内容**（>5000 字符）→ 提示“不能超过 5000 个字符”
- **包含敏感词**（如“垃圾”）→ 前端提示，提交按钮禁用；若绕过前端，后端返回错误
- **包含白名单词**（如“暴力破解”）→ 正常提交
- **伪造 `permalink`** → 后端返回“文档不存在”
- **快速多次提交** → 触发限流提示（若实现）
- **回复多级引用** → 递归查询正确找到根评论归属

## 8. 总结

通过添加内容过滤、归属验证和限流机制，评论区的安全性大大提升，能够抵御常见的恶意行为。这些功能与已有的 Markdown 安全渲染、用户认证共同构成了一个健壮的评论系统。

现在，评论区已经可以放心地开放给所有读者了。
