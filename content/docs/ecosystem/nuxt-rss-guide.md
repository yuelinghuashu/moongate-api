---
title: Nuxt 集成 RSS 服务完全指南：从模块到手写的优雅之路
description: 手把手教你绕过第三方模块的坑，亲手构建完全可控的 RSS/Atom/JSON Feed 服务。
date: 2026-02-23
series: ecosystem
tags:
  - Nuxt
  - SEO
---

## 为什么需要 RSS？

在算法推荐泛滥的今天，RSS 依然是最纯粹的内容订阅方式。它让用户真正掌控自己获取信息的渠道，不受平台算法的干扰。为你的 Nuxt 博客添加 RSS 服务，不仅能提升用户体验，更是对开放互联网精神的致敬。

本文将带你从零开始，在 Nuxt 4 中实现一个**完全可控、生产可用**的 RSS 服务。我们将绕过第三方模块的潜在陷阱，亲手构建属于自己的 RSS 生成器。

> 💡 **前置要求**：本文假设你已经熟悉 Nuxt 4 和 @nuxt/content v3 的基本使用，能够独立创建项目并配置 content 模块。如果你还不熟悉这些，建议先查阅官方文档。

---

## 两种方案的对比

### 方案一：使用第三方模块（如 `nuxt-feedme`）

#### 看似简单

```ts
export default defineNuxtConfig({
  modules: ["nuxt-feedme"],
  feedme: {
    feeds: {
      "/feed.xml": { type: "rss2" },
    },
  },
})
```

#### 实际可能遇到的坑

- ❌ 生产环境 API 404（模块试图调用不存在的开发接口）
- ❌ 文档老旧，与实际版本脱节
- ❌ 配置复杂，黑盒调试困难
- ❌ 钩子机制学习成本高
- ❌ 依赖更新可能导致兼容性问题

### 方案二：手写 RSS（本文推荐）

#### 核心优势

- ✅ 完全可控，每一行代码都了然于心
- ✅ 无依赖，零兼容问题
- ✅ 调试简单，哪里错看哪里
- ✅ 性能极致，可按需优化
- ✅ 代码量少，维护成本极低

---

## RSS 的本质：一句话说透

```ts
RSS = 获取数据 + 拼接 XML（或 JSON）
```

仅此而已。理解了这一点，你就掌握了 RSS 的全部奥秘。

---

## 完整实现步骤

### 1. 环境配置

在 `nuxt.config.ts` 中配置好运行时变量：

```ts
export default defineNuxtConfig({
  modules: ["@nuxt/content"],
  runtimeConfig: {
    public: {
      siteUrl: process.env.SITE_URL || "https://yourdomain.com",
      siteName: "你的博客名称",
      siteDescription: "博客描述",
    },
  },
})
```

### 2. 创建 MinimarkTree 转 HTML 工具函数

Nuxt Content v3 返回的 `doc.body.value` 是结构化的 MinimarkTree，需要转换为 HTML。函数会完整保留标签属性，确保图片、链接等元素正常显示。

```ts
// utils/minimarkToHtml.ts

/**
 * 将 Nuxt Content v3 的 MinimarkTree 转换为 HTML 字符串
 * @param node - 文档 body 的 value 节点 (doc.body.value)
 * @returns HTML 字符串
 */
export function minimarkToHtml(node: any): string {
  if (!node) return ""

  // 处理根节点
  if (node.type === "minimark" && Array.isArray(node.value)) {
    return node.value.map(minimarkToHtml).join("")
  }

  // 文本节点
  if (typeof node === "string") return node

  // 数组节点
  if (Array.isArray(node)) {
    return node.map(minimarkToHtml).join("")
  }

  if (node && typeof node === "object") {
    // 元素节点（带标签）
    if (node.tag) {
      // 生成属性字符串
      const attrs = node.props
        ? " " +
          Object.entries(node.props)
            .map(
              ([key, val]) => `${key}="${String(val).replace(/"/g, "&quot;")}"`,
            )
            .join(" ")
        : ""

      const children = (node.children || []).map(minimarkToHtml).join("")

      // 自闭合标签
      if (["img", "br", "hr", "input"].includes(node.tag)) {
        return `<${node.tag}${attrs} />`
      }
      return `<${node.tag}${attrs}>${children}</${node.tag}>`
    }
  }

  return ""
}
```

### 3. 创建 RSS 2.0 生成器

```ts
// server/routes/feed.xml.ts
import { minimarkToHtml } from "~/utils/minimarkToHtml"

export default defineEventHandler(async (event) => {
  const { siteName, siteDescription, siteUrl } = useRuntimeConfig().public

  const docs = await queryCollection(event, "docs").order("date", "DESC").all()

  let rss = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/">
  <channel>
    <title>${siteName}</title>
    <link>${siteUrl}</link>
    <description>${siteDescription}</description>
    <language>zh-CN</language>
    <lastBuildDate>${new Date().toUTCString()}</lastBuildDate>
`

  for (const doc of docs) {
    let fullContent = ""
    if (doc.body?.value) {
      try {
        fullContent = minimarkToHtml(doc.body.value)
      } catch (e) {
        console.error("转换失败:", e)
        fullContent = doc.description || ""
      }
    }

    const link = `${siteUrl}${doc.path}`
    const date = new Date(doc.date).toUTCString()

    rss += `
    <item>
      <title><![CDATA[${doc.title}]]></title>
      <link>${link}</link>
      <guid isPermaLink="true">${link}</guid>
      <pubDate>${date}</pubDate>
      <description><![CDATA[${doc.description || ""}]]></description>
      <content:encoded><![CDATA[${fullContent}]]></content:encoded>
    </item>
`
  }

  rss += `
  </channel>
</rss>`

  setResponseHeader(event, "content-type", "application/xml; charset=utf-8")
  return rss
})
```

### 4. 创建 Atom 1.0 生成器

Atom 是另一种 XML 格式的订阅标准，结构更规范：

```ts
// server/routes/feed.atom.ts
import { minimarkToHtml } from "~/utils/minimarkToHtml"

export default defineEventHandler(async (event) => {
  const { siteName, siteDescription, siteUrl } = useRuntimeConfig().public

  const docs = await queryCollection(event, "docs").order("date", "DESC").all()

  const updated = docs[0]?.date
    ? new Date(docs[0].date).toISOString()
    : new Date().toISOString()

  let atom = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>${siteName}</title>
  <subtitle>${siteDescription}</subtitle>
  <link href="${siteUrl}/feed.atom" rel="self"/>
  <link href="${siteUrl}" rel="alternate"/>
  <id>${siteUrl}</id>
  <updated>${updated}</updated>
  <author>
    <name>MoonGate</name>
  </author>
`

  for (const doc of docs) {
    let content = ""
    if (doc.body?.value) {
      try {
        content = minimarkToHtml(doc.body.value)
      } catch (e) {
        console.error("转换失败:", e)
        content = doc.description || ""
      }
    }

    const link = `${siteUrl}${doc.path}`
    const published = new Date(doc.date).toISOString()

    atom += `
  <entry>
    <title>${doc.title}</title>
    <link href="${link}"/>
    <id>${link}</id>
    <published>${published}</published>
    <updated>${published}</updated>
    <summary>${doc.description || ""}</summary>
    <content type="html"><![CDATA[${content}]]></content>
  </entry>
`
  }

  atom += `\n</feed>`

  setResponseHeader(
    event,
    "content-type",
    "application/atom+xml; charset=utf-8",
  )
  return atom
})
```

### 5. 创建 JSON Feed 1.1 生成器

JSON Feed 是现代化的订阅格式，结构清晰：

```ts
// server/routes/feed.json.ts
import { minimarkToHtml } from "~/utils/minimarkToHtml"

export default defineEventHandler(async (event) => {
  const { siteUrl } = useRuntimeConfig().public

  const docs = await queryCollection(event, "docs").order("date", "DESC").all()

  const feed = {
    version: "https://jsonfeed.org/version/1.1",
    title: "MoonGate",
    home_page_url: siteUrl,
    feed_url: `${siteUrl}/feed.json`,
    description: "Where Moon Meets Code",
    language: "zh-CN",
    authors: [
      {
        name: "MoonGate",
        url: siteUrl,
      },
    ],
    items: await Promise.all(
      docs.map(async (doc) => {
        let contentHtml = ""
        if (doc.body?.value) {
          try {
            contentHtml = minimarkToHtml(doc.body.value)
          } catch (e) {
            console.error("转换失败:", e)
            contentHtml = doc.description || ""
          }
        }

        return {
          id: `${siteUrl}${doc.path}`,
          url: `${siteUrl}${doc.path}`,
          title: doc.title,
          content_html: contentHtml,
          summary: doc.description || "",
          date_published: new Date(doc.date).toISOString(),
          language: "zh-CN",
          tags: doc.tags || [],
        }
      }),
    ),
  }

  setResponseHeader(
    event,
    "content-type",
    "application/feed+json; charset=utf-8",
  )
  return feed
})
```

### 6. 添加缓存优化（可选）

使用 Nuxt 内置的缓存处理器，减轻服务器压力：

```ts
export default defineCachedEventHandler(
  async (event) => {
    // ... 上面的代码
  },
  {
    maxAge: 60 * 60, // 缓存 1 小时
    name: "feed-cache",
    getKey: () => "static", // 所有用户共享缓存
  },
)
```

### 7. 在网页中引入 RSS 订阅

在 `app.vue` 或布局文件中添加自动发现链接：

```vue
// app.vue
<script setup>
const { siteName } = useRuntimeConfig().public

useHead({
  link: [
    {
      rel: "alternate",
      type: "application/rss+xml",
      title: siteName,
      href: "/feed.xml",
    },
    {
      rel: "alternate",
      type: "application/atom+xml",
      title: siteName,
      href: "/feed.atom",
    },
    {
      rel: "alternate",
      type: "application/json",
      title: siteName,
      href: "/feed.json",
    },
  ],
})
</script>
```

---

## 验证与测试

### 本地测试

```bash
# 启动服务
pnpm dev

# 测试三种格式
curl http://localhost:3000/feed.xml
curl http://localhost:3000/feed.atom
curl http://localhost:3000/feed.json

# 检查响应头
curl -I http://localhost:3000/feed.xml
```

---

## 常见问题排查

| 问题 | 原因 | 解决 |
| ---- | ---- | ---- |
| RSS 显示 `[object Object]` | 没有将 `doc.body` 正确转换为 HTML | 使用本文提供的 `minimarkToHtml` 函数 |
| 链接是相对路径，没有域名 | 拼接 URL 时遗漏了 `siteUrl` | 确保使用 `${siteUrl}${doc.path}` |
| 日期格式错误 | 直接使用了 ISO 字符串 | RSS 2.0 用 `new Date(date).toUTCString()`，Atom 和 JSON 用 `.toISOString()` |
| 生产环境 404 | `server/routes/` 下的文件未正确部署 | 检查构建输出是否包含 `.output/server/` 目录 |
| JSON Feed 在浏览器中显示不全 | 浏览器插件或开发者工具为了性能做了预览截断 | 直接用 RSS 阅读器测试，或使用 `curl` 查看完整内容 |

---

## 为什么手写比用模块更好？

| 维度           | 第三方模块           | 手写方案                   |
| -------------- | -------------------- | -------------------------- |
| **代码量**     | 配置复杂，还要写钩子 | 每个文件约 40 行，简单明了 |
| **依赖**       | 多个间接依赖         | 零依赖                     |
| **学习成本**   | 高（要懂黑盒逻辑）   | 低（懂 RSS 格式即可）      |
| **调试难度**   | 高（报错看不懂）     | 极低（哪里错看哪里）       |
| **生产稳定性** | 容易踩坑             | 稳定可靠                   |
| **维护成本**   | 依赖作者更新         | 自己掌控                   |

### 记住

RSS 的本质就是“查数据 + 拼 XML/JSON”，没有任何复杂逻辑需要模块来封装。

---

## 进阶优化

### 1. 分页限制

```ts
const docs = await queryCollection(event, "docs")
  .order("date", "DESC")
  .limit(20) // 只取最近 20 篇
  .all()
```

### 2. 自定义命名空间（RSS 2.0）

```ts
xmlns: media = "http://search.yahoo.com/mrss/" // 支持媒体内容
xmlns: dc = "http://purl.org/dc/elements/1.1/" // 支持 Dublin Core
```

### 3. 添加更多元数据

```ts
// 在 JSON Feed 中添加
"authors": [{ "name": "作者名", "url": "个人主页" }],
"language": "zh-CN",
"tags": ["技术", "前端"]
```

---

## 结语

技术世界总是充满各种“开箱即用”的解决方案，但有时候，**亲手构建一个简单功能所获得的理解和掌控感，远胜于使用复杂的第三方模块**。

RSS 作为一个诞生了 20 多年的简单协议，其魅力就在于透明和可控。通过本文的手写方案，你不仅为博客添加了实用的功能，更深入理解了 Web 的本质。

现在，去享受自己动手的成果吧！🎉
