---
title: Nuxt 图片引用：<NuxtImg> 替代原生 <img> 的一次实践
description: 介绍了为什么在 Nuxt 项目中，永远优先使用 <NuxtImg> 而不是原生 <img>。
date: 2026-02-17
series: performance
level: P1
tags:
  - Nuxt
  - Performance
  - Performance
---

## 📚 系列导航

本系列共三篇，覆盖 Nuxt 性能优化相关实践：

1. [**Nuxt Content 渲染问题解决指南**](./nuxt-content-config-guide) —— @nuxt/content 配置与渲染
2. [**Nuxt 图片引用：\<NuxtImg\> 实践**](./nuxt-image-best-practice) —— 图片优化与路径问题
3. [**Nuxt 4 状态持久化**](./nuxt-state-persistence-guide) —— SSR 水合失败根治

---

## 从一次诡异的图片 404 错误说起

如果你在 Nuxt 项目中使用原生 `<img>` 标签引用图片，比如：

```vue
<img src="/images/ali-pay.jpg" alt="支付宝赞赏码" />
```

有没有遇到过这样的情况：

- 在**页面里**直接写，图片正常显示 ✅
- 把同样的代码**封装到组件里**，图片突然不显示了 ❌
- 打开开发者工具，发现请求的 URL 变成了 `http://localhost:3000/&/images/ali-pay.jpg`
- 控制台报错：`No match found for location with path "/&/images/ali-pay.jpg"`

我就在最近踩了这个坑。更诡异的是：

- 强制 `<details>` 默认展开也报错 ❌
- 整个页面刷新后才正常显示 ✅
- 换成 `<NuxtImg>` 组件后，问题消失 ✅

这篇文档不是要深挖这个错误的技术根源（因为可能涉及你的具体配置），而是要告诉你一个更重要的结论：

**在 Nuxt 项目中，永远优先使用 `<NuxtImg>` 而不是原生 `<img>`。**

---

## 📊 原生 `<img>` 的隐患

| 场景                                             | 原生 `<img>`           | `<NuxtImg>`                       |
| ------------------------------------------------ | ---------------------- | --------------------------------- |
| **项目部署在子目录**（如 `https://a.com/blog/`） | 需要手动拼接 `baseURL` | ✅ 自动处理                       |
| **动态路由页面中使用**（如 `[...slug].vue`）     | 可能路径解析错误       | ✅ 稳定可靠                       |
| **组件内使用**                                   | 可能受组件上下文影响   | ✅ 始终如一                       |
| **图片优化**                                     | 无                     | ✅ 支持格式转换、尺寸调整、懒加载 |
| **开发服务器热重载**                             | 可能缓存问题           | ✅ 优化良好                       |

换句话说，原生 `<img>` 不是“不能用”，而是有太多“可能出问题”的场景，尤其是当你的项目稍微复杂一点时。

---

## 🧪 对比实验

### ❌ 原生 `<img>` 组件版

```vue
<template>
  <details>
    <summary>请我喝杯咖啡 ☕️</summary>
    <img src="/images/ali-pay.jpg" alt="支付宝" class="w-32" />
  </details>
</template>
```

**结果**：请求 `/&/images/ali-pay.jpg` → 404

### ✅ `<NuxtImg>` 组件版

```vue
<template>
  <details>
    <summary>请我喝杯咖啡 ☕️</summary>
    <NuxtImg src="/images/ali-pay.jpg" alt="支付宝" class="w-32" />
  </details>
</template>
```

**结果**：请求 `/images/ali-pay.jpg` → 200 ✅

唯一的区别就是用了 `<NuxtImg>` 替换 `<img>`。

---

## 🔧 安装和使用

### 1. 安装模块

```bash
# 使用 npm
npm install @nuxt/image
# 使用 pnpm
pnpm add @nuxt/image
```

### 2. 添加到 `nuxt.config.ts`

```typescript
export default defineNuxtConfig({
  modules: ["@nuxt/image"],
})
```

### 3. 在组件中使用

```vue
<!-- 基础用法 -->
<NuxtImg src="/images/ali-pay.jpg" alt="支付宝" />
<!-- 指定宽度，自动优化 -->
<NuxtImg src="/images/ali-pay.jpg" width="200" height="200" />
<!-- 响应式图片 -->
<NuxtImg src="/images/ali-pay.jpg" sizes="sm:100vw md:50vw lg:400px" />
<!-- 懒加载 -->
<NuxtImg src="/images/ali-pay.jpg" loading="lazy" />
```

---

## 🎯 为什么 `<NuxtImg>` 更可靠

### 1. 自动处理 `baseURL`

如果你在 `nuxt.config.ts` 中配置了：

```typescript
export default defineNuxtConfig({
  app: {
    baseURL: "/blog/",
  },
})
```

- 原生 `<img src="/images/ali-pay.jpg">` 会请求 `/blog/images/ali-pay.jpg`？**不会**，它还是请求 `/images/ali-pay.jpg`，404。
- `<NuxtImg src="/images/ali-pay.jpg">` 会自动加上 `/blog/` 前缀，请求正确地址。

### 2. 内置图片优化服务

`@nuxt/image` 会在开发环境启动一个图片优化中间件，在生产环境生成优化后的图片：

- 支持 WebP 等现代格式（自动根据浏览器选择）
- 支持图片裁剪、缩放
- 支持响应式图片（根据屏幕尺寸加载不同大小）
- 支持懒加载

### 3. 构建时处理

生产构建时，`@nuxt/image` 会：

- 将图片复制到输出目录
- 生成哈希化的文件名（`ali-pay.abc123.jpg`）
- 避免路径冲突和缓存问题

### 4. 官方维护，社区验证

作为 Nuxt 官方模块，它经过了大量项目的测试，适配各种部署场景（Vercel、Netlify、自托管等）。

---

## 💡 最佳实践总结

### ✅ 推荐做法

1. **所有内部图片**：使用 `<NuxtImg>` 或 `<NuxtPicture>`
2. **外部图片**（如 CDN）：配置 `provider` 后同样使用 `<NuxtImg>`
3. **结合 `sizes` 属性**：实现响应式图片，提升性能
4. **开启懒加载**：对长页面的图片设置 `loading="lazy"`

### ❌ 避免的做法

1. **直接使用原生 `<img>`**，除非你有特殊理由
2. **手动拼接 `baseURL`**，交给 `<NuxtImg>` 处理
3. **引用 `public` 目录外的图片**，保持项目结构清晰

---

## 🎁 额外收获：图片优化带来的性能提升

除了解决路径问题，`@nuxt/image` 还能显著提升网站性能：

| 优化项       | 效果                                          |
| ------------ | --------------------------------------------- |
| **格式转换** | 自动将 JPEG/PNG 转为 WebP（节省 30-50% 体积） |
| **尺寸调整** | 只加载当前视口需要的图片大小                  |
| **懒加载**   | 减少初始加载时间                              |
| **预连接**   | 可配置 CDN 预连接，加速加载                   |

用 Lighthouse 测试一下，你会发现图片相关分数明显提高。

---

## 📝 结语

那个让我研究了半小时的 `/&/` 报错，最后被一个官方模块轻松解决。这让我意识到：

**在 Nuxt 项目里，能用官方模块解决的问题，就尽量不要自己手写。**

`@nuxt/image` 不只是“图片组件”，它是 Nuxt 官方提供的一整套图片处理方案。安装它，不仅能避免各种诡异的路径问题，还能免费获得图片优化、响应式支持等高级功能。

如果你还没用上，现在就去装一个：

```bash
npx nuxi@latest module add image
```

然后：

```vue
<NuxtImg src="/images/your-image.jpg" />
```

世界清净了。

---

### 遇到过的坑

图片路径 `/&/` 错误、组件内图片不显示、部署子目录后图片 404

### 用过的方案

原生 `<img>`、显式 import、`<NuxtImg>`

### 最终的答案

永远优先用 `<NuxtImg>`
