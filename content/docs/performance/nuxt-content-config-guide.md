---
title: 解决Nuxt Content渲染问题：从基础配置到渲染显示完整指南
description: 记录了我从零开始配置 @nuxt/content 模块，渲染 Markdown 内容的完整过程。
date: 2025-12-28
series: performance
level: P2
tags: 
  - Nuxt
  - Engineering
  - Performance
---

## 📚 系列导航

本系列共三篇，覆盖 Nuxt 性能优化相关实践：

1. [**Nuxt Content 渲染问题解决指南**](./nuxt-content-config-guide) —— @nuxt/content 配置与渲染
2. [**Nuxt 图片引用：\<NuxtImg\> 实践**](./nuxt-image-best-practice) —— 图片优化与路径问题
3. [**Nuxt 4 状态持久化**](./nuxt-state-persistence-guide) —— SSR 水合失败根治

---

## 概述

这篇文档记录了我从零开始配置 Nuxt Content 模块，渲染 Markdown 内容的完整过程。如果你也厌倦了在配置上耗费数小时却一行业务代码都没写的挫败感，这篇实战指南或许能帮你少走弯路。

> **适用版本**：Nuxt Content v3  
> 如果你使用其他版本，核心思路仍可参考，但具体行为可能略有差异。

## 1. 环境与项目初始化

### 1.1 创建 Nuxt 项目

```bash
# 使用官方脚手架创建项目
pnpm create nuxt <My-Project>
cd <My-Project>

# 安装基础依赖（如果你使用 pnpm）
pnpm install
```

### 1.2 安装多个必备模块

```bash
# 安装 Nuxt Content 模块
pnpm add @nuxt/content

# 安装 better-sqlite3 模块
pnpm add better-sqlite3
```

### 1.3 处理可能存在的better-sqlite3模块的兼容问题

在安装 `@nuxt/content` 时，其依赖的 `better-sqlite3` 是一个原生模块，在使用 pnpm 管理依赖时可能会遇到二进制文件路径解析问题。以下是两种解决方案，你可以根据项目环境和需求选择其中一种。

---

#### 方案一：通过 pnpm 配置与重建（推荐大多数项目）

此方案通过显式允许构建 `better-sqlite3` 并重建其二进制文件，确保模块在 pnpm 的严格模式下正常工作。

1. **创建或修改 `pnpm-workspace.yaml`**  
   在项目根目录创建该文件，内容如下：

   ```yaml
   onlyBuiltDependencies:
     - better-sqlite3
   ```

   或者直接在安装时使用 `--allow-build` 参数，pnpm 会自动完成配置：

   ```bash
   pnpm add better-sqlite3 --allow-build=better-sqlite3
   ```

2. **重建 better-sqlite3**  
   执行以下命令强制重新编译原生模块：

   ```bash
   pnpm rebuild better-sqlite3
   ```

3. **启动开发服务器验证**

   ```bash
   pnpm run dev
   ```

> 此方案保留了 pnpm 的所有优势，同时解决了原生模块的兼容性问题，适用于大多数 Nuxt 项目。

---

#### 方案二：启用 Nuxt Content 的原生 SQLite 支持（需要 Node.js v22.5.0+）

从 `@nuxt/content` 的某个版本开始（具体请查阅对应版本的文档），你可以通过配置 `experimental.nativeSqlite` 选项直接启用原生 SQLite 绑定，无需手动处理 `better-sqlite3` 的 pnpm 兼容性问题。

1. **确保 Node.js 版本 ≥ v22.5.0**  
   检查当前 Node 版本：

   ```bash
   node -v
   ```

   如果版本过低，请升级。

2. **在 `nuxt.config.ts` 中添加配置**

   ```typescript
   export default defineNuxtConfig({
     modules: ["@nuxt/content"],
     content: {
       experimental: {
         nativeSqlite: true, // 启用原生 SQLite 支持
       },
     },
   });
   ```

3. **启动项目**

   ```bash
   pnpm run dev
   ```

启用此选项后，Nuxt Content 会使用原生的 `better-sqlite3` 实现，通常在服务器端性能和稳定性上更优，尤其适合生产环境。但请注意，此功能为实验性，可能需要配合特定版本的 Nuxt Content 使用。

---

##### 选择

方案一适用于任何 Node 版本，对 pnpm 项目通用；方案二更简洁，但需要较高版本的 Node 环境和对实验性特性的接受度。请根据实际情况选择。

## 2. 基础配置

### 2.1 配置 `nuxt.config.ts`

```typescript
export default defineNuxtConfig({
  modules: ["@nuxt/content"],
});
```

### 2.2 创建内容目录和第一篇文档

在项目根目录创建 `content` 文件夹，然后创建第一篇文档，文件暂命名为home.md

```markdown
# 欢迎来到我的博客

这是我的第一篇使用 **Nuxt Content** 构建的文档。

## 特性亮点

- ✅ 支持 Markdown 语法
- ✅ 内置代码高亮
- ✅ 前端框架无缝集成

## 代码示例

```

```javascript
// 这是一个 JavaScript 示例
export default function greet(name) {
  console.log(`Hello, ${name}!`);
  return `Welcome to ${name}'s blog`;
}
```

## 3. 创建文档渲染页面

```vue
<!-- 在app/pages的目录下创建文件名为[...slug].vue的文件，将以下代码粘贴到此文件中 -->
<script setup lang="ts">
const route = useRoute();

const { data: page } = await useAsyncData("page-" + route.path, () => {
  return queryCollection("content").path(route.path).first();
});

console.log(page.value);

if (!page.value) {
  throw createError({
    statusCode: 404,
    statusMessage: "Page not found",
    fatal: true,
  });
}
</script>

<template>
  <ContentRenderer v-if="page" :value="page" />
</template>
```

## 总结

### 1. 配置心得

1. **版本一致性**：确保所有依赖版本兼容，特别是 Nuxt、Content 模块
2. **渐进式配置**：不要一次性配置所有功能，先确保基础渲染正常，再逐步添加高级功能。
3. **善用官方文档**：遇到问题时，首先查看各模块的官方文档，注意版本差异。

### 2. 避坑指南

- **问题**：`@nuxt/content` 与 `better-sqlite3` 原生模块在 pnpm 环境下冲突，导致安装或启动失败。
  - **原因**：pnpm 的严格链接模式可能导致原生模块的二进制文件无法被正确加载，引发 `better-sqlite3` 相关错误。
  - **解决**：有两种方式可处理该问题——
    - **方案一**：通过 pnpm 配置显式允许构建并重建模块（详见上文 **1.3 方案一**）。
    - **方案二**：升级 Node.js 至 **v22.5.0 或更高**，并在 `nuxt.config.ts` 中添加 `experimental: { nativeSqlite: true }`。此配置让 Nuxt Content 直接使用原生的 SQLite 绑定，从根本上避免路径解析问题，同时提升生产环境性能。**推荐使用此方案**，它同时解决了下文的生产环境数据丢失问题。
- **问题**：生产环境使用 `useAsyncData` 渲染内容时，数据偶发无法获取，刷新后内容丢失。
  - **原因**：默认情况下，Nuxt Content 在服务端可能回退到 JavaScript 实现的 SQLite，其性能不足以应对生产环境的并发请求，导致数据库访问失败。
  - **解决**：确保 Node.js 版本 **≥ v22.5.0**，并在 `nuxt.config.ts` 的 `content` 配置中启用 `experimental: { nativeSqlite: true }`。该选项强制使用原生的 `better-sqlite3`，大幅提升数据库操作的稳定性和性能，彻底解决数据丢失问题。

> **小结**：两个常见问题的根本原因都与 SQLite 的实现方式有关。通过升级 Node 版本并开启 `experimental.nativeSqlite`，可以同时解决原生模块兼容性与生产环境数据丢失的问题，是当前最简洁有效的做法。

### 3. 性能建议

1. **代码分割**：利用 Nuxt 的自动代码分割功能
2. **图片优化**：使用 `@nuxt/image` 模块优化内容中的图片

---

> **写在最后**：虽然配置过程可能充满挑战，但一旦完成，你将获得一个强大、现代且完全可控的内容管理系统。每一次配置问题的解决，都是对现代前端工具链理解的深化。现在，去创建你的内容吧！
