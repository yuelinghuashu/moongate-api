---
title: Nuxt 实战：在个人博客中集成 Shiki 自定义 VSCode 主题
description: 从加载自定义主题到深浅色联动，再到彻底解决 SSR 闪动问题。一份完整的 Nuxt + Shiki 自定义主题集成指南。
date: 2026-07-11 22:00:00
permalink: 65b51060-10b8-4e9e-9ba4-c67d2b6afd36
series:
level: P2
tags:
  - Nuxt
  - VSCode
  - Theme
  - Performance
  - Hydration
---

## 为什么要折腾 Shiki？

在个人博客中，代码高亮是阅读体验的核心。市面上主流的 Prism.js 和 Highlight.js 虽然普及度高，但高亮精度有限。相比之下，Shiki 使用与 VSCode 相同的 TextMate 语法引擎，能实现像素级精准的高亮，完全对标 VSCode 的代码着色体验。

我的需求很简单：个人博客使用我自己开发的 VSCode 主题插件 **Moongate Theme** 的深浅两套配色，代码块在 SSR 场景下正常渲染，且深浅主题能一键联动切换。
听起来不难？**实际上坑比想象中多。**

## 坑一：自定义主题怎么加载？

Shiki 官方文档提供了两种常规的加载方式：创建高亮器时直接传入主题对象，或使用 loadTheme 动态加载 JSON 文件。看起来很简单对吧？但当把它放到 Nuxt 项目中时，问题来了。

### ❌ 错误尝试：依赖 nuxt-shiki 模块

我一开始用了 nuxt-shiki 模块，想着它能帮我省事。配置如下：

```typescript
import lightTheme from "./assets/themes/light.json"
import darkTheme from "./assets/themes/dark.json"

export default defineNuxtConfig({
  modules: ["nuxt-shiki"],
  shiki: {
    bundledThemes: [lightTheme.name, darkTheme.name],
    defaultTheme: lightTheme.name,
  },
})
```

运行后直接报错：

```text
Failed to resolve import "shiki/themes/Moongate Theme Light.mjs"
```

> **根本原因**：nuxt-shiki 的 bundledThemes 只接受 Shiki **内置主题的名称**（如 github-dark），它会自动去 shiki/themes/ 目录下查找对应的内置 .mjs 文件。当你传入自定义主题名称时，它找不到对应的文件，自然 404。

### ✅ 正确做法：直接用原生 Shiki API

放弃 nuxt-shiki 模块，直接在 Composable 中使用原生 createHighlighter，将其作为常驻内存的全局单例：

```typescript
// composables/useShikiHighlighter.ts
import { createHighlighter, type Highlighter } from "shiki"
import lightTheme from "~/assets/themes/light.json"
import darkTheme from "~/assets/themes/dark.json"

let highlighterInstance: Highlighter | null = null

export async function getShikiHighlighter() {
  if (!highlighterInstance) {
    highlighterInstance = await createHighlighter({
      themes: [lightTheme, darkTheme],
      langs: [
        "bash",
        "css",
        "docker",
        "go",
        "html",
        "javascript",
        "json",
        "markdown",
        "shell",
        "sql",
        "typescript",
        "vue",
        "xml",
        "yaml",
      ],
    })
  }
  return highlighterInstance
}
```

> **关键点**：主题 JSON 对象直接传入 themes 数组，高亮时通过主题的 name 字段引用即可，不再依赖外部文件的动态寻址。

## 坑二：自定义主题的双主题联动

### 单主题方案的局限性

最初的方案是在客户端组件挂载后通过 DOMParser 解析 HTML，然后根据当前主题传入对应的主题名称重新高亮：

```typescript
// ❌ 客户端高亮 + watch 主题变化
const theme = isDark ? "Moongate Theme Dark" : "Moongate Theme Light"
const result = highlighter.codeToHtml(code, { lang, theme })

watch(() => store.theme, highlight) // 主题变了要重新处理所有代码块
```

这种传统做法会带来三个极具毁灭性的痛点：

1.  **闪动**：客户端 Hydration 后才能高亮，用户打开网页会看到“原始内容/黑色外壳 → 高亮内容”的明显跳变。
2.  **延迟**：每次一键切换主题，客户端都需要重新执行 JS 高亮所有代码块，有明显的视觉等待时间。
3.  **CPU 开销**：在手机端低配设备上，每次切换主题都调用 WASM 引擎处理大量文本，会导致页面瞬间掉帧。

## 坑三：服务端高亮 + Dual Themes = 完美方案

真正的突破是**把高亮工作从客户端彻底移到服务端**，同时利用 Shiki 的 **Dual Themes（双主题）** 特性。

### 核心工具函数：服务端高亮处理器

创建 utils/shikiProcessor.ts。这里使用正则异步替换未高亮的 HTML 块，并加入一个健壮的兜底机制：即使代码块没有写 language-xxx，也能默认以 text 纯文本进行高亮渲染。

```typescript
// utils/shikiProcessor.ts
import { getShikiHighlighter } from "~/composables/useShikiHighlighter"

export async function highlightHtmlContent(
  htmlContent: string,
): Promise<string> {
  if (!htmlContent) return ""

  const highlighter = await getShikiHighlighter()

  // 增强正则：允许匹配没有定义 language 类的标准 <code> 块
  const preCodeRegex = /<pre>\s*<code([^>]*)>([\s\S]*?)<\/code>\s*<\/pre>/g
  const matches = [...htmlContent.matchAll(preCodeRegex)]
  let resultHtml = htmlContent

  for (const match of matches) {
    const [fullMatch, attributes, rawCode] = match

    // 提取语言类型，若无则默认为 'text'
    const langMatch = attributes.match(/class="[^"]*language-(\w+)"/)
    const lang = langMatch ? langMatch[1] : "text"

    // 解码 HTML 实体，防止 Shiki 二次转义
    const code = rawCode
      .replace(/&lt;/g, "<")
      .replace(/&gt;/g, ">")
      .replace(/&amp;/g, "&")
      .replace(/&quot;/g, '"')
      .replace(/&#39;/g, "'")

    // 🎯 关键：使用 Dual Themes 一次生成包含两套颜色 Token 的 HTML
    const highlighted = highlighter.codeToHtml(code, {
      lang,
      themes: {
        light: "Moongate Theme Light",
        dark: "Moongate Theme Dark",
      },
      defaultColor: false, // 核心配置：不生成默认内联 color，完全靠 CSS 变量驱动
    })

    resultHtml = resultHtml.replace(fullMatch, highlighted)
  }

  return resultHtml
}
```

### 在数据获取层拦截并转换

在组件内部，利用 useLazyAsyncData 的 transform 选项。**这步是最高级的优化**：数据在服务端被 Node.js 抓取到后瞬间完成高亮替换。数据吐到前端时就已经套好了 Shiki 的外衣。

```typescript
const { data: page, pending } = useLazyAsyncData<DocDetailResponse>(
  `doc-${slug.value}`,
  async () => {
    const {
      public: { apiUrl },
    } = useRuntimeConfig()
    return await $fetch(`${apiUrl}/api/docs/${slug.value}`)
  },
  {
    watch: [slug],
    // 🔥 关键：数据在服务端获取后立即转换为高亮 HTML，客户端 0 开销
    transform: async (data) => {
      if (data && data.content) {
        data.highlightedContent = await highlightHtmlContent(data.content)
      }
      return data
    },
  },
)

// 优先使用服务端已高亮的完全体 HTML
const contentRef = computed(
  () => page.value?.highlightedContent || page.value?.content || "",
)
```

### CSS 变量控制双主题切换

Dual Themes 生成的 HTML 中会精妙地包含 --shiki-light 和 --shiki-dark 两套 CSS 变量。配合 @nuxtjs/color-mode 切换时自动在 <html> 标记的 .dark 类，只需在全局样式表中写下几行映射，就能实现**纯 CSS 级别的高性能切换**：

```css
/* 浅色模式默认映射 */
.shiki {
  background-color: var(--shiki-light-bg) !important;
  color: var(--shiki-light) !important;
}
.shiki span {
  color: var(--shiki-light) !important;
}

/* 深色模式映射 - 纯 CSS 触发，不经过任何 JS 运行时 */
.dark .shiki {
  background-color: var(--shiki-dark-bg) !important;
  color: var(--shiki-dark) !important;
}
.dark .shiki span {
  color: var(--shiki-dark) !important;
}
```

## ⚡ 为什么闪动消失了？

通过前后方案的对比，我们可以清晰地看到为什么这个方案能达到“降维打击”的效果：
| 阶段 | 之前（有闪动、有延迟） | 现在（无闪动、零开销） |
|---|---|---|
| **服务端 (SSR)** | 返回原始 HTML（未高亮的纯文本或暗色外壳） | 返回高亮后的 HTML（**已包含全量双主题样式**） |
| **客户端挂载** | 显示原始内容 → 加载 JS / WASM → 替换 DOM → 高亮变色 | **直接显示高亮后的 HTML，没有任何视觉时差** |
| **主题一键切换** | watch 状态变化 → 耗费 CPU 重新高亮渲染 | **纯 CSS 切换变量，瞬间响应，0ms 延迟** |

> **核心突破**：高亮工作在服务端一次性就位，客户端收到即用。由于两套变量早已直出，切换主题变成了浏览器的原生样式渲染，不再需要 Pinia 去跨组件追踪和重绘。

## 🎨 细节打磨：代码块边框与呼吸感

最后，给代码块加点微弱的边框和极浅的阴影，能在长文阅读中有效地为代码建立视觉锚点，提升整体的呼吸感与精致度：

```css
.shiki-content pre.shiki {
  padding: 1.25rem;
  border-radius: 0.5rem;
  overflow-x: auto;
  border: 1px solid #e5e7eb;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.04);
  transition:
    border-color 0.3s,
    box-shadow 0.3s;
}

.dark .shiki-content pre.shiki {
  border-color: #2d3748;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
}
```

## 📐 架构设计流向

```text
 ┌─────────────────┐
 │  服务端 (SSR)    │
 └────────┬────────┘
          │
          ▼
 ┌─────────────────┐
 │ 获取原始内容     │
 └────────┬────────┘
          │
          ▼
 ┌──────────────────────────────────────────────┐
 │ transform: highlightHtmlContent()            │
 │  ├── 正则匹配 <pre><code>                     │
 │  ├── Shiki 原生 API 生成 Dual Themes HTML    │
 │  └── 替换原始代码块                           │
 └────────────────┬─────────────────────────────┘
          │
          ▼
 ┌──────────────────────────────────────────────┐
 │ 返回高亮后的 HTML (含 --shiki-light / dark)   │
 └────────────────┬─────────────────────────────┘
          │
          ▼
 ┌─────────────────┐
 │ 客户端直接渲染   │
 └────────┬────────┘
          │
          ├─► 浅色模式 ──► 自动映射 --shiki-light (纯 CSS)
          └─► 深色模式 ──► 自动映射 --shiki-dark  (纯 CSS)

```

## 📝 总结

| 遇到的坑                        | 解决方案                                                   |
| ------------------------------- | ---------------------------------------------------------- |
| nuxt-shiki 不支持自定义主题     | 弃用扩展模块，改用原生 createHighlighter 自定义导入        |
| 客户端高亮导致 SSR 闪动缺陷     | 利用 useLazyAsyncData 的 transform 在服务端完成高亮        |
| 深浅主题一键切换存在明显的延迟  | 采用 Shiki Dual Themes 生成双主题 CSS 变量                 |
| 主题联动需要复杂的 watch 重渲染 | 纯 CSS 变量控制，**客户端 Shiki JS / WASM 运行时开销归零** |

在前端实战中，面对长文章下的代码高亮需求，**“在服务端多做一点，客户端就能少做很多”**。通过在服务端利用 Shiki 提取双主题直出，不仅完美消灭了视觉闪烁，还让我们的博客客户端免受庞大高亮引擎带来的首屏负荷。

希望这篇实战记录能帮你在使用 Shiki + Nuxt 的路上少走弯路！
