---
title: Nuxt 评论区完美支持 Markdown：从解析、高亮到安全渲染
description: 手把手教你为 Nuxt 博客评论区添加安全、美观、功能完整的 Markdown 渲染支持，代码块配色与文档（Nuxt Content）自动统一，深浅色模式无缝切换。
date: 2026-02-21
series: comment
tags:
  - Nuxt
  - Security
---

## 📦 适用版本

本文基于以下版本编写，请确保你的项目版本与之匹配：

| 依赖                 | 版本     | 备注                |
| -------------------- | -------- | ------------------- |
| Nuxt                 | **v4**   | 核心框架            |
| Nuxt Content         | **v3**   | 文档内容管理        |
| Nuxt UI              | **v4**   | 提供 `useColorMode` |
| marked               | **v15+** | Markdown 解析器     |
| shiki                | **v3+**  | 代码高亮引擎        |
| isomorphic-dompurify | **v2+**  | XSS 防护            |

> 💡 如果你使用其他版本，核心思路仍可参考，但具体 API 可能需要调整。

---

<details>
<summary>评论区原理</summary>

在开始之前，先聊聊评论区的本质。

很多人（包括我一开始）觉得评论区很复杂——要处理嵌套、要实时更新、要防攻击……但实际上，**一个最小可用的评论区，核心就是最简单的增删改查**：

- **增**：用户提交评论，存到数据库（`INSERT`）
- **删**：用户删除自己的评论（`DELETE`）
- **改**：编辑评论（`UPDATE`，可选）
- **查**：加载文档下的所有评论（`SELECT`）

没有算法、没有实时推送、没有复杂的机制——**就是最基础的后端操作 + 前端展示**。

> 评论区没那么可怕，它只是一个长得像对话框的 CRUD 而已。

本文就是在“增删改查”的基础上，给你的评论加上 **Markdown 渲染** 能力。如果你连基础的评论功能都还没做，可以先花 30 分钟搭一个简单的版本，再回来看本文。

</details>

---

<details>
<summary>前置要求</summary>

本文默认读者已经具备以下能力：

- ✅ **能独立完成评论的基础 CRUD**（数据表设计、API 编写、前端展示）
- ✅ **熟悉 Vue / Nuxt 组件开发**（知道 `props`、`ref`、`watch` 怎么用）
- ✅ **了解 Markdown 基本语法**（知道 `**粗体**`、`` `代码` `` 是什么意思）
- ✅ **能自行查阅文档**（marked、Shiki、DOMPurify 的官网用法）

如果你还不具备这些，建议先补充基础：

- <a href="https://cn.vuejs.org/guide/introduction.html" target="_blank" rel="noopener noreferrer">Vue 3 中文官方文档</a>
- <a href="https://nuxt.zhcndoc.com/docs/4.x/getting-started/installation" target="_blank" rel="noopener noreferrer">Nuxt 4 中文官方文档</a>
- <a href="https://markdown.com.cn/intro.html" target="_blank" rel="noopener noreferrer">Markdown 中文官方文档</a>

**本文不会解释 SQL 怎么写、不会教 Vue 基础、不会重复官网 API——只讲“如何把评论区升级为 Markdown 渲染”。**

</details>

---

<details>
<summary>为什么写这篇文档？</summary>

我在实现评论区 Markdown 功能时，搜遍全网发现：

- 官网文档：只给 API，不给实战
- 个人博客：要么复制粘贴，要么浅尝辄止
- 中文社区：全是其他平台的教程（WordPress、Typecho）

**没有一篇是专门针对 Nuxt 4 的、完整的、经过实战检验的评论区 Markdown 集成教程。**

所以我把自己折腾了七八个小时的过程写下来——包括踩过的坑、填过的土、以及那些“网上搜不到”的解决方案。希望能让后来的人少走些弯路。

</details>

---

<details>
<summary>本文能给你什么</summary>

- ✅ **完整的 Markdown 渲染方案**（marked + Shiki + DOMPurify）
- ✅ **代码块高亮与文档配色统一**（深浅色自动切换）
- ✅ **XSS 防护的正确姿势**（不只是过滤标签）
- ✅ **两种性能方案对比**（预加载 vs 懒加载）
- ✅ **7 个实战踩坑记录**（`$` 陷阱、`watch` 监听、主题不匹配等）

</details>

---

## 一、痛点与目标

许多 Nuxt 博主在搭建评论区时，会遇到以下问题：

- 评论只能输入纯文本，无法贴代码、加粗、列表等。
- 即使勉强支持 Markdown，代码块样式与文档正文（通常由 Nuxt Content 渲染）不一致，显得格格不入。
- 担心 XSS 攻击，不敢直接渲染用户输入的 HTML。

### 本文目标

手把手教你为 Nuxt 博客评论区添加**安全、美观、功能完整**的 Markdown 渲染支持，并且代码块配色与文档（Nuxt Content v3）**自动保持统一**，深浅色模式无缝切换。

---

## 二、技术选型与原理

| 需求            | 选型                     | 理由                                            |
| --------------- | ------------------------ | ----------------------------------------------- |
| Markdown → HTML | `marked`                 | 轻量、快速、可扩展，支持 GFM，社区活跃          |
| 代码语法高亮    | `shiki`                  | Nuxt Content 同款，主题丰富，输出稳定 HTML      |
| XSS 防护        | `isomorphic-dompurify`   | SSR 兼容，过滤恶意标签和属性，保障安全          |
| 深浅色模式      | `useColorMode` (Nuxt UI) | 自动监听系统/用户主题切换，动态更新渲染         |
| 代码块样式统一  | CSS 变量 + 覆盖          | 复用 Nuxt UI 主题变量，使评论块与文档块视觉一致 |

### 为什么不用 Prism 或 highlight.js？

Shiki 与 Nuxt Content 内部使用的高亮器一致，可以直接复用其主题和语言包，确保代码块颜色与文档**完全一致**，无需额外维护两套配色。

> **主题与语言参考**：Shiki 支持的主题和语言列表请查阅官方文档：[主题列表](https://shiki.zhcndoc.com/themes) | [语言列表](https://shiki.zhcndoc.com/languages)。你可以根据自己博客的实际配色需求，从中选择与 Nuxt Content 匹配的主题（例如 `material-theme-*` 系列）。

---

## 三、逐步实现（附详细注释）

### 3.1 安装依赖

```bash
pnpm add -D shiki marked isomorphic-dompurify
# Nuxt UI v4 已内置 useColorMode，无需额外安装
```

### 3.2 创建 Shiki 插件（全局单例）

Shiki 初始化较慢，且应只创建一次。我们通过 Nuxt 插件在客户端创建全局高亮器实例，供所有组件共享。

```ts
// plugins/shiki.client.ts
import { createHighlighter } from "shiki"

export default defineNuxtPlugin(async () => {
  // 预加载文档用到的主题和语言（可根据实际需求调整）
  // 主题列表：https://shiki.zhcndoc.com/themes
  // 语言列表：https://shiki.zhcndoc.com/languages
  const highlighter = await createHighlighter({
    themes: ["material-theme-lighter", "material-theme-palenight"],
    langs: [
      "javascript",
      "typescript",
      "html",
      "css",
      "vue",
      "python",
      "bash",
      "json",
      "markdown",
      "xml",
      "yaml",
      "shell",
      "diff",
    ],
  })

  return {
    provide: {
      shiki: highlighter, // 通过 $shiki 注入全局
    },
  }
})
```

### 3.3 封装 Markdown 渲染组件

创建 `components/MarkdownRenderer.vue`，核心逻辑如下：

- 使用 `marked.Renderer` 自定义代码块处理，交给 Shiki 高亮。
- 监听 `colorMode` 动态切换主题。
- 最后通过 DOMPurify 过滤输出。

````vue
<template>
  <!-- eslint-disable-next-line vue/no-v-html -->
  <div v-html="renderedContent" />
</template>

<script lang="ts" setup>
import { marked } from "marked"
import DOMPurify from "isomorphic-dompurify"

const props = defineProps({ content: { type: String, required: true } })

// 从 Nuxt 插件中获取全局 Shiki 高亮器实例（已在客户端插件中预加载主题和语言）
const { $shiki } = useNuxtApp()
const colorMode = useColorMode()

const renderedContent = ref("")

// 根据当前颜色模式动态选择 Shiki 主题，确保与文档代码块配色一致
const currentTheme = computed(() => {
  return colorMode.value === "dark"
    ? "material-theme-palenight" // 深色主题
    : "material-theme-lighter" // 浅色主题
})

// 核心渲染函数：将用户输入的 Markdown 内容转换为安全的、高亮的 HTML
const renderContent = async () => {
  // 如果 Shiki 未就绪或内容为空，则直接显示原始内容（降级处理）
  if (!$shiki || !props.content) {
    renderedContent.value = props.content
    return
  }

  try {
    // ---------- 第一步：手动提取并高亮所有代码块 ----------
    let processed = props.content
    // 正则匹配围栏代码块：```lang\n代码\n```（支持语言可选）
    const codeBlockRegex = /```([a-zA-Z0-9+#-]+)\n([\s\S]*?)```/g
    const matches = [...processed.matchAll(codeBlockRegex)]

    for (const match of matches) {
      const [fullMatch, lang, code] = match
      try {
        // 调用 Shiki 进行语法高亮，返回 HTML 字符串或包含 HTML 的对象
        const highlighted = $shiki.codeToHtml(code.trim(), {
          lang: lang || "text", // 未指定语言时当作纯文本
          theme: currentTheme.value, // 使用当前主题
        })

        // 兼容 Shiki 不同版本的返回值（可能直接返回字符串，也可能返回 { html } 对象）
        const htmlStr =
          typeof highlighted === "string"
            ? highlighted
            : highlighted.html || highlighted.value || String(highlighted)

        // 用高亮后的 HTML 替换原始代码块（使用函数替换避免 $ 符号被转义）
        processed = processed.replace(fullMatch, () => htmlStr)
      } catch (e) {
        console.error("高亮失败:", e)
        // 高亮失败时保留原始代码块（不做高亮）
      }
    }

    // ---------- 第二步：将处理后的内容（代码块已替换）解析为 Markdown ----------
    const html = await marked.parse(processed, {
      breaks: true, // 将换行符转换为 <br>
      gfm: true, // 启用 GitHub 风格 Markdown（表格、删除线等）
    })

    // ---------- 第三步：使用 DOMPurify 过滤不安全内容，防止 XSS 攻击 ----------
    renderedContent.value = DOMPurify.sanitize(html, {
      // 明确允许的 HTML 标签（涵盖所有 Markdown 可能生成的标签）
      ALLOWED_TAGS: [
        "p",
        "br",
        "strong",
        "em",
        "u",
        "s",
        "del",
        "ins",
        "span",
        "div",
        "h1",
        "h2",
        "h3",
        "h4",
        "h5",
        "h6",
        "ul",
        "ol",
        "li",
        "a",
        "blockquote",
        "code",
        "pre",
        "table",
        "thead",
        "tbody",
        "tr",
        "th",
        "td",
        "hr",
        "img",
        "sub",
        "sup",
      ],
      // 允许的属性（class/style 用于代码高亮样式，其他为链接、图片等常用属性）
      ALLOWED_ATTR: [
        "class",
        "style",
        "href",
        "lang",
        "src",
        "alt",
        "title",
        "target",
        "rel",
      ],
      // 限制 URL 只能使用以下安全协议：
      // - http: / https: → 网页链接、图片链接（评论区核心需求）
      // - ftp: → 文件下载链接（极少出现，但保留无害）
      // - mailto: → 邮箱联系方式（偶尔有人留邮箱）
      // - tel: → 电话联系方式（虽少但保留）
      // - blob: → 临时文件/本地文件（为可能的图片上传预留）
      // - data: → base64 图片（用户直接贴 base64 图片时用）
      // 其他协议（如 javascript:、vbscript:、file: 等）一律拦截，防止 XSS 攻击
      ALLOWED_URI_REGEXP: /^(https?|ftp|mailto|tel|blob|data):/i,

      // 是否允许未在 ALLOWED_URI_REGEXP 中列出的协议：
      // - true  → 正则只作为“推荐列表”，未知协议可能被放行（不安全）
      // - false → 正则作为“强制列表”，只有列出的协议才允许（安全）
      // 评论区场景必须设置为 false，确保所有 URL 都经过协议白名单检查
      ALLOW_UNKNOWN_PROTOCOLS: false,
    })
  } catch (error) {
    console.error("渲染失败:", error)
    // 发生任何错误时，回退显示原始内容
    renderedContent.value = props.content
  }
}

// 监听内容或主题变化，立即执行一次渲染，之后每次变化重新渲染
watch([() => props.content, () => colorMode.value], renderContent, {
  immediate: true,
})
</script>

<style scoped>
/* 样式部分见 3.5 节，此处先省略 */
</style>
````

### 3.4 在评论区中使用

在你的评论区组件（如 `CommentSection.vue`）中引入并使用：

```vue
<template>
  <div class="comments">
    <div v-for="comment in commentList" :key="comment.id">
      <!-- 头像、用户名等 -->
      <MarkdownRenderer :content="comment.content" />
    </div>
  </div>
</template>
```

### 3.5 样式统一：让评论代码块与文档融为一体

Nuxt Content 默认代码块样式带有背景、边框和圆角。我们通过 CSS 变量（由 Nuxt UI 提供）覆盖评论区的 `<pre>` 和 `<code>` 样式，实现视觉统一。如果你未使用 Nuxt UI，可替换为具体的颜色值。

在 `MarkdownRenderer.vue` 的 `<style scoped>` 中添加：

```css
<style scoped>
/* 代码块整体容器 */
:deep(pre) {
  padding: 1rem;
  background-color: var(--ui-bg-muted);  /* 不使用 !important，通过优先级覆盖 */
  border: 1px solid var(--ui-border);
  border-radius: var(--ui-radius);
  overflow-x: auto;
  margin: 1rem 0;
}

:deep(code) {
  font-family: 'JetBrains Mono', 'Fira Code', monospace;
  font-size: 0.9em;
}

/* 行内代码样式 */
:deep(code:not(pre code)) {
  background-color: var(--ui-bg-muted);
  padding: 0.2em 0.4em;
  border-radius: var(--ui-radius-sm);
}

/* 移除 Shiki 可能自带的背景，避免双重背景 */
:deep(.shiki) {
  background-color: transparent !important; /* 这里仍需 !important 覆盖 Shiki 内联样式 */
}

/* 让 .line 元素正常显示，避免布局错乱 */
:deep(pre code .line) {
  display: contents !important;
}

/* 表格样式（与文档保持一致） */
:deep(table) {
  border-collapse: collapse;
  width: 100%;
  margin: 1rem 0;
}
:deep(th), :deep(td) {
  border: 1px solid var(--ui-border);
  padding: 0.5rem;
}
:deep(th) {
  background-color: var(--ui-bg-muted);
  font-weight: 600;
}

/* 引用块 */
:deep(blockquote) {
  border-left: 4px solid var(--ui-border);
  margin: 1rem 0;
  padding: 0.5rem 1rem;
  color: var(--ui-text-muted);
  background-color: var(--ui-bg-muted);
}

/* 列表 */
:deep(ul), :deep(ol) {
  padding-left: 2rem;
}

/* 链接 */
:deep(a) {
  color: var(--ui-primary);
  text-decoration: underline;
}
</style>
```

> **提示**：如果你的博客未使用 Nuxt UI，请检查文档代码块的实际样式（背景色、边框色、圆角等），然后将上述 `var(--ui-*)` 替换为对应的具体颜色值（如 `#f6f8fa`）。同时可酌情去掉 `!important`。

---

## 四、踩坑与优化记录

### 4.1 主题名称不匹配

- **问题**：按照 Nuxt Content 文档示例使用 `github-light/dark`，但实际默认主题可能是 `material-theme-lighter/palenight`，导致评论区配色与文档不一致。
- **解决**：打开浏览器检查文档代码块的 `<pre>` 标签，查看类名（如 `material-theme-lighter`），或在 `nuxt.config.ts` 中确认 `content.highlight.theme` 配置。然后在组件中替换为对应的主题 ID。

### 4.2 `String.replace` 的 `$` 陷阱

- **问题**：直接用 `replace(fullMatch, htmlStr)` 时，若 `htmlStr` 包含 `$`（如 Shiki 生成的样式），会被解释为特殊替换模式，导致 HTML 损坏。
- **解决**：使用 `replace(fullMatch, () => htmlStr)` 或 `replaceAll`，避免 `$` 被转义。

### 4.3 数据流断连

- **问题**：提交评论后列表不更新，或修改后的内容没存进数据库。
- **解决**：检查提交接口是否正确使用了处理后的 `contentToSave`，并确保评论列表组件监听了数据变化（如用 `refresh` 重新获取）。

### 4.4 性能优化：预加载 vs 懒加载

本文提供的方案（插件预加载主题和语言）适合大多数博客，因为评论中常见的语言通常有限。但如果你的博客涉及大量罕见语言，或对首屏加载体积极其敏感，可以考虑**懒加载**方案。

#### 方案一：预加载（当前方案）

- 优点：代码块渲染速度最快，无额外异步延迟。
- 缺点：初始加载时会包含所有预置语言，体积稍大。

#### 方案二：懒加载版本（基于手动正则提取 + `codeToHtml`）

以下代码是经过实际验证的稳定方案，它不依赖任何 Nuxt 插件，直接在组件中使用 Shiki 的 `codeToHtml` 函数实现**按需加载**。该方案与方案一的核心区别在于：

- **无需创建全局插件**，代码更轻量。
- Shiki 的主题和语言在第一次使用时才加载，后续自动缓存，优化首屏体积。
- 保留了手动正则提取代码块的逻辑，确保参数类型安全，避免 marked 内部传递不确定对象的问题。

````vue
<template>
  <!-- eslint-disable-next-line vue/no-v-html -->
  <div v-html="renderedContent" />
</template>

<script lang="ts" setup>
import { marked } from "marked"
import { codeToHtml } from "shiki"
import DOMPurify from "isomorphic-dompurify"

const props = defineProps({ content: { type: String, required: true } })
const colorMode = useColorMode()
const renderedContent = ref("")

// 根据当前颜色模式动态选择 Shiki 主题，确保与文档代码块配色一致
const currentTheme = computed(() => {
  return colorMode.value === "dark"
    ? "material-theme-palenight" // 深色主题（请根据你的实际主题替换）
    : "material-theme-lighter" // 浅色主题（请根据你的实际主题替换）
})

// 核心渲染函数：将用户输入的 Markdown 内容转换为安全的、高亮的 HTML
const renderContent = async () => {
  if (!props.content) {
    renderedContent.value = ""
    return
  }

  try {
    // ---------- 第一步：手动提取并高亮所有代码块 ----------
    let processed = props.content
    // 正则匹配围栏代码块：```lang\n代码\n```（支持语言可选）
    const codeBlockRegex = /```([a-zA-Z0-9+#-]+)\n([\s\S]*?)```/g
    const matches = [...processed.matchAll(codeBlockRegex)]

    for (const match of matches) {
      const [fullMatch, lang, code] = match
      try {
        // 调用 Shiki 进行语法高亮（懒加载，按需加载主题和语言）
        const highlighted = await codeToHtml(code.trim(), {
          lang: lang || "text", // 未指定语言时当作纯文本
          theme: currentTheme.value, // 使用当前主题
        })

        // 兼容 Shiki 不同版本的返回值（可能直接返回字符串，也可能返回 { html } 对象）
        const htmlStr =
          typeof highlighted === "string"
            ? highlighted
            : highlighted.html || highlighted.value || String(highlighted)

        // 用高亮后的 HTML 替换原始代码块（使用函数替换避免 $ 符号被转义）
        processed = processed.replace(fullMatch, () => htmlStr)
      } catch (e) {
        console.error("代码块高亮失败:", e)
        // 高亮失败时保留原始代码块（不做高亮）
      }
    }

    // ---------- 第二步：将处理后的内容（代码块已替换）解析为 Markdown ----------
    const html = await marked.parse(processed, {
      breaks: true, // 将换行符转换为 <br>
      gfm: true, // 启用 GitHub 风格 Markdown（表格、删除线等）
    })

    // ---------- 第三步：使用 DOMPurify 过滤不安全内容，防止 XSS 攻击 ----------
    renderedContent.value = DOMPurify.sanitize(html, {
      // 明确允许的 HTML 标签（涵盖所有 Markdown 可能生成的标签）
      ALLOWED_TAGS: [
        "p",
        "br",
        "strong",
        "em",
        "u",
        "s",
        "del",
        "ins",
        "span",
        "div",
        "h1",
        "h2",
        "h3",
        "h4",
        "h5",
        "h6",
        "ul",
        "ol",
        "li",
        "a",
        "blockquote",
        "code",
        "pre",
        "table",
        "thead",
        "tbody",
        "tr",
        "th",
        "td",
        "hr",
        "img",
        "sub",
        "sup",
      ],
      // 允许的属性（class/style 用于代码高亮样式，其他为链接、图片等常用属性）
      ALLOWED_ATTR: [
        "class",
        "style",
        "href",
        "lang",
        "src",
        "alt",
        "title",
        "target",
        "rel",
      ],
      // 限制 URL 只能使用以下安全协议：
      // - http: / https: → 网页链接、图片链接（评论区核心需求）
      // - ftp: → 文件下载链接（极少出现，但保留无害）
      // - mailto: → 邮箱联系方式（偶尔有人留邮箱）
      // - tel: → 电话联系方式（虽少但保留）
      // - blob: → 临时文件/本地文件（为可能的图片上传预留）
      // - data: → base64 图片（用户直接贴 base64 图片时用）
      // 其他协议（如 javascript:、vbscript:、file: 等）一律拦截，防止 XSS 攻击
      ALLOWED_URI_REGEXP: /^(https?|ftp|mailto|tel|blob|data):/i,

      // 是否允许未在 ALLOWED_URI_REGEXP 中列出的协议：
      // - true  → 正则只作为“推荐列表”，未知协议可能被放行（不安全）
      // - false → 正则作为“强制列表”，只有列出的协议才允许（安全）
      // 评论区场景必须设置为 false，确保所有 URL 都经过协议白名单检查
      ALLOW_UNKNOWN_PROTOCOLS: false,
    })
  } catch (error) {
    console.error("Markdown 渲染失败:", error)
    // 发生任何错误时，回退显示原始内容
    renderedContent.value = props.content
  }
}

// 监听内容或主题变化，立即执行一次渲染，之后每次变化重新渲染
watch([() => props.content, () => colorMode.value], renderContent, {
  immediate: true,
})
</script>
````

#### 与方案一（插件预加载）的对比

| 特性               | 方案一（预加载插件）                       | 方案二（懒加载 `codeToHtml`）           |
| ------------------ | ------------------------------------------ | --------------------------------------- |
| **Shiki 实例创建** | 通过插件全局创建一次，预加载所有主题和语言 | 直接在组件中调用 `codeToHtml`，按需加载 |
| **初始加载体积**   | 包含所有预置语言，稍大                     | 仅包含核心，语言在用到时才加载          |
| **首次高亮速度**   | 无额外延迟                                 | 首次出现某语言时需等待加载（之后缓存）  |
| **代码复杂度**     | 需要维护插件文件                           | 组件内完成，无需额外文件                |
| **适用场景**       | 评论语言种类固定，对渲染速度要求极高       | 语言种类多，追求首屏性能优化            |

### 使用说明

1. 删除原有的 `plugins/shiki.client.ts` 文件（如果存在）。
2. 确保安装了 `shiki`、`marked`、`isomorphic-dompurify`。
3. 根据你的博客实际配色，修改 `currentTheme` 中的主题 ID（参考 [Shiki 主题列表](https://shiki.zhcndoc.com/themes)）。
4. 将上述组件保存为 `MarkdownRenderer.vue`，并在评论区引入使用。

该方案已在生产环境中验证，能稳定处理代码块高亮、主题切换、XSS 防护，并实现语言按需加载。

---

## 五、总结与扩展

至此，你拥有了一个功能完备、安全可靠的评论区，支持：

- 完整的 Markdown 语法（标题、列表、表格、引用、图片等）
- 代码块语法高亮（与文档配色一致）
- 深浅色模式自动切换
- XSS 防护

在此基础上，你还可以继续扩展：

- 添加评论回复功能
- 支持表情符号（如 `marked-emoji` 插件）
- 实时预览（Markdown 编辑器）

---

## 附录：常见问题

### Q：我用的不是 Nuxt UI，如何实现主题切换？

A：可以使用 `@vueuse/core` 的 `usePreferredDark` 手动监听系统主题，动态改变 Shiki 的 `theme` 参数。

### Q：如何支持更多编程语言？

A：语言标识符请参考 [Shiki 官方语言列表](https://shiki.zhcndoc.com/languages)。Shiki 会自动加载所需语言，无需额外配置。若使用插件预加载，只需在 `langs` 数组中添加对应 ID。

### Q：渲染速度慢怎么办？

A：如果选择预加载方案，确保 Shiki 实例全局单例（插件方式已满足）。如果选择懒加载方案，首次加载某种语言时会有短暂延迟，但之后会缓存。若评论数量极大，可考虑对代码块渲染做虚拟滚动。

### Q：如何确认文档实际使用的主题？

A：打开浏览器开发者工具，选中文档中的一个代码块，查看 `<pre>` 或 `<code>` 标签的类名，通常包含主题名称（如 `material-theme-palenight`）。也可在 `nuxt.config.ts` 中查看 `content.highlight.theme` 配置。

---

本文以“评论区 Markdown 渲染”为核心，详细介绍了从选型到落地的全过程，并融入了与 Nuxt Content 配色统一的技巧。希望能帮到你，也欢迎在评论区留言交流！
