---
title: Nuxt 4 国际化(i18n)完整配置：从基础设置到高级优化
description: 本文介绍了 @nuxt/i18n 模块的配置方法，并提供了一些常见问题的解决方案。
date: 2025-12-11
permalink: ba590c8a-5f7f-40ab-8492-e071b8f8731f
series: i18n
level: P1
tags:
  - Nuxt
  - i18n
  - Engineering
  - Performance
---

## 📚 系列导航

本系列共三篇：

1. [**Nuxt 4 国际化完整配置**](./nuxt-i18n-config-guide) —— 基础配置与中文痛点（locales、路由策略、占位符插值）
2. [**Nuxt Content + i18n 终极集成**](./nuxt-content-i18n-ultimate-integration) —— 单语言内容多语言界面（路径转换中间层）
3. [**Nuxt i18n 的 $tm 函数**](./nuxt-i18n-tm-function-guide) —— 环境差异问题与解决方案（useI18nSafe 封装）

---

本文档基于 `@nuxtjs/i18n` 模块，版本为10.2.1，详细讲解在 Nuxt 4 项目中实现国际化的完整流程，并特别指出中文配置中的常见“痛点”及解决方案。

> ⚠️ 重要提示：@nuxtjs/i18n v10 版本相对于 v8 有重大重构，配置方式、API 和路由策略均发生了较大变化。如果你之前使用过旧版本，请务必抛弃固有认知，以本文档和官方文档为准，避免因版本差异导致的配置错误。

---

<details>
<summary>适用版本</summary>

- Nuxt: **v4**
- Nuxt i18n: **V10**

> 如果你用的是其他版本，核心思路可参考，但具体 API 可能需要调整。

</details>

---

## 一、快速开始

### 1.1 安装模块

```bash
pnpm install @nuxt/i18n
```

### 1.2 基础配置 (`nuxt.config.ts`)

```typescript
export default defineNuxtConfig({
  modules: ["@nuxtjs/i18n"],

  i18n: {
    // 语言环境配置（核心）
    locales: [
      {
        code: "zh_cn", // 程序内部标识符（URL路径使用）
        name: "简体中文", // 显示名称
        language: "zh-CN", // 用于HTML lang属性的标准语言标签
        file: "zh_cn.json", // 对应的语言文件
      },
      {
        code: "en",
        name: "English",
        language: "en-US",
        file: "en-US.json",
      },
      {
        code: "ja",
        name: "日本語",
        language: "ja-JP",
        file: "ja-JP.json",
      },
    ],

    // 默认语言设置（必须与某个code完全匹配）
    defaultLocale: "zh_cn",

    // 语言文件目录
    langDir: "locales",

    // 路由策略
    strategy: "prefix_except_default", // 推荐：默认语言无前缀

    // 浏览器语言检测
    detectBrowserLanguage: {
      useCookie: true,
      cookieKey: "i18n_redirected",
      redirectOn: "root",
    },
  },
})
```

### 1.3 语言文件结构

创建语言文件目录和文件：

```text
project-root/
├── i18n/
│   └── locales/
│       ├── zh_cn.json    # 简体中文
│       ├── en.json       # 英文
│       └── ja.json       # 日文
├── nuxt.config.ts
└── app.vue
```

### 1.4 创建语言文件

以 `locales/zh_cn.json` 为例（en、ja 文件结构相同，仅内容翻译不同）：

```json
{
  "welcome": "欢迎使用我们的应用",
  "about": "关于我们",
  "user": {
    "profile": "用户资料",
    "settings": "设置"
  }
}
```

### 1.5 动态区域与方向设置

根据当前语言动态设置 UI 区域（locale）和 HTML 的 `lang`、`dir` 属性：

```vue
<script setup lang="ts">
import * as locales from "@nuxt/ui/locale"

const { locale } = useI18n()

const lang = computed(() => locales[locale.value].code)
const dir = computed(() => locales[locale.value].dir)

useHead({
  htmlAttrs: {
    lang,
    dir,
  },
})
</script>

<template>
  <UApp :locale="locales[locale]">
    <NuxtPage />
  </UApp>
</template>
```

## 二、中文配置的特别痛点与解决方案

### 2.1 痛点一：语言标识符不一致与默认语言配置错误

**问题**：中文有多种标识符格式（`zh`、`zh-CN`、`zh_CN`、`zh_cn`），容易混淆，导致 `defaultLocale` 配置不匹配、页面报错。

**解决方案**：

- **`code` 字段**：用于URL路径和程序内部标识，推荐使用 **`zh_cn`**（全小写下划线）
- **`language` 字段**：用于HTML `lang` 属性和SEO，使用标准 **`zh-CN`**（连字符格式）
- **`defaultLocale`**：必须与 `code` 值**完全一致**

```typescript
// 正确的完整示例
locales: [
  { code: 'zh_cn', language: 'zh-CN', file: 'zh_cn.json' }
],
defaultLocale: 'zh_cn', // 必须与上面的code完全相同
strategy: 'prefix_except_default'
```

```typescript
defaultLocale: "zh_cn" // 正确
defaultLocale: "zh-CN" // 错误！会导致配置不匹配
```

### 2.2 痛点二：语言文件加载失败

**问题**：控制台报错 `Cannot find module './locales/zh.json'`。

**解决方案**：

1. **检查文件路径**：确认 `langDir` 配置正确
2. **验证JSON格式**：语言文件必须是**严格有效的JSON**

```json
// 正确
{ "welcome": "欢迎" }

// 错误（有注释）
{
  // 欢迎语
  "welcome": "欢迎"
}

// 错误（有尾随逗号）
{ "welcome": "欢迎", }
```

## 三、高级配置

### 3.1 子域名国际化（像Vue官网一样）

```typescript
i18n: {
  locales: [
    {
      code: 'zh_cn',
      domain: 'cn.your-app.com', // 生产环境子域名
      language: 'zh-CN',
      file: 'zh_cn.json'
    },
    {
      code: 'en',
      domain: 'your-app.com', // 主域名作为默认语言
      language: 'en-US',
      file: 'en.json'
    }
  ],
  differentDomains: true, // 启用子域名模式
  defaultLocale: 'en', // 默认语言对应主域名
  detectBrowserLanguage: false // 子域名模式下通常禁用
}
```

### 3.2 翻译占位符（参数插值）

在实际项目中，经常需要动态替换翻译文本中的变量，例如"共 {count} 条记录"。`@nuxtjs/i18n` 支持在翻译字符串中使用 `{key}` 占位符，并在组件中传入对应的值。

#### 语言文件示例

在 `i18n/locales/zh_cn.json` 和 `i18n/locales/en.json` 中定义带占位符的键：

```json
// zh_cn.json
{
  "findCount": "已查询到 {count} 条文档",
  "greeting": "你好，{name}！欢迎回来。",
  "balance": "当前余额：{amount, number} 元"
}
```

```json
// en.json
{
  "findCount": "Found {count} documents",
  "greeting": "Hello {name}! Welcome back.",
  "balance": "Balance: {amount, number} USD"
}
```

#### 组件中使用

在 Vue 组件中通过 `$t` 或 `t` 函数传递参数对象：

```vue
<template>
  <div>
    <p>{{ t("findCount", { count: totalDocs }) }}</p>
    <p>{{ t("greeting", { name: userName }) }}</p>
    <p>{{ t("balance", { amount: userBalance }) }}</p>
  </div>
</template>

<script setup>
const { t } = useI18n()
const totalDocs = ref(25)
const userName = ref("张三")
const userBalance = ref(12345.67)
</script>
```

### 注意事项

- 占位符键名必须**严格匹配**（区分大小写），如 `{count}` 不能写成 `{Count}`。
- 如果某个占位符未传入值，它会原样输出（如 `{count}`）。
- 对于需要复数处理的场景（如"1 条评论 / 2 条评论"），请使用 `vue-i18n` 的复数机制，而非手动拼接。

## 总结

`@nuxtjs/i18n` v10 为 Nuxt 应用提供了强大且灵活的国际化方案，但配置细节繁多，尤其在中文场景下容易踩坑。回顾全文，有几个关键点值得再次强调：

1. **版本差异巨大**  
   v10 与 v8 不兼容，务必以本文和官方文档为准，摒弃旧版认知。

2. **语言标识符规范化**
   - `code`：全小写下划线（如 `zh_cn`）
   - `language`：标准连字符格式（如 `zh-CN`）
   - `defaultLocale`：必须与 `code` **严格一致**

3. **语言文件维护**  
   存放目录与 JSON 格式需准确无误，避免注释或尾随逗号。

4. **占位符参数插值**  
   使用 `{key}` 配合 `t('key', { key: value })` 实现动态文本，并支持数字/日期格式化。

5. **路由策略与子域名**  
   小型项目推荐 `prefix_except_default`，大型多站点可启用 `differentDomains`。

6. **性能与 SEO**  
   通过 `useHead` 动态设置 `lang` 和 `dir` 提升可访问性。

> 国际化的核心目标是让不同语言的用户获得一致的体验，希望本文能帮助你少走弯路，快速构建高质量的 Nuxt 多语言应用。
