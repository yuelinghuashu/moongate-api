---
title: Nuxt i18n 的 `$tm` 函数：环境差异问题与解决方案
description: 介绍了 Nuxt.js 项目中使用 @nuxtjs/i18n 模块的 $tm 函数（或组合式 API 中的 tm()）时，一个常见的问题是：在开发环境 (nuxt dev) 下运行正常的代码，在生产环境构建 (nuxt build) 后运行会报错或渲染异常。
date: 2026-01-21
permalink: 87d4fd2c-9ef2-48c9-b0ac-a686f3c527f9
series: i18n
level: P1
tags: 
  - Nuxt
  - i18n
  - Engineering
  - Performance
---

> **适用版本**：`@nuxtjs/i18n` v10.x  
> _如果你使用其他版本，核心思路仍可参考，但具体行为可能略有差异。_

## 核心问题

在 Nuxt 4 + `@nuxtjs/i18n` v10 项目中，使用 `$tm` 或 `tm()` 获取结构化翻译数据（如数组、对象）时，可能会遇到：

- **开发环境 (`nuxt dev`)**：一切正常，模板渲染正确。
- **生产环境 (`nuxt build` 后运行)**：报错 `Cannot read properties of undefined (reading 'source')`，或页面渲染异常。

## 问题根源

问题的根本原因在于 **i18n 模块在开发环境和生产环境下对语言文件的编译处理方式不同**：

- **开发环境**：为了支持热更新和源码映射，模块会保留语言文件中的原始结构信息，字符串值被包装为 `{ loc: { source: "实际文本" } }` 的形式。
- **生产环境**：为了减小体积和提升性能，模块会移除这些包装，直接输出纯字符串值。

这种差异导致在模板中访问数据时，如果写死了开发环境下的访问路径（例如 `item.name.loc.source`），生产环境就会因找不到 `loc` 属性而报错。

## 解决方案

### 方案一：快速修补（适合临时修复）

在模板中根据环境动态选择访问路径：

```vue
<template>
  <div v-for="item in tm('navigationBar')" :key="item.id">
    <NuxtLink
      :to="isDev ? item.link?.loc?.source : item.link"
      rel="noopener noreferrer"
    >
      {{ isDev ? item.name?.loc?.source : item.name }}
    </NuxtLink>
  </div>
</template>

<script setup>
const isDev = import.meta.env.DEV;
const { tm } = useI18n();
</script>
```

**优点**：直接了当，改动最小。  
**缺点**：每个使用 `tm` 的地方都要写判断，代码冗余。

---

### 方案二：封装统一适配函数（推荐）

创建一个组合式函数，自动处理环境差异，让模板代码保持简洁。

```ts
// composables/useI18nSafe.ts
import { useI18n } from "vue-i18n";

/**
 * 递归提取开发环境下的实际值（移除 loc.source 包装）
 */
function extractValue(value: any): any {
  if (!value || typeof value !== "object") return value;

  // 处理被包装的字符串（开发环境特有）
  if (value.loc?.source !== undefined) {
    return value.loc.source;
  }

  // 处理数组
  if (Array.isArray(value)) {
    return value.map(extractValue);
  }

  // 处理对象
  const result: Record<string, any> = {};
  for (const key in value) {
    result[key] = extractValue(value[key]);
  }
  return result;
}

export function useI18nSafe() {
  const { tm: originalTm, ...rest } = useI18n();

  const tm = (key: string) => {
    const value = originalTm(key);
    // 仅开发环境需要提取，生产环境直接返回
    if (import.meta.env.DEV) {
      return extractValue(value);
    }
    return value;
  };

  return { tm, ...rest };
}
```

在组件中使用：

```vue
<script setup>
const { tm } = useI18nSafe();
</script>

<template>
  <div v-for="item in tm('navigationBar')" :key="item.id">
    <NuxtLink :to="item.link" rel="noopener noreferrer">{{
      item.name
    }}</NuxtLink>
  </div>
</template>
```

**优点**：模板代码与生产环境完全一致，无环境感知，维护简单。  
**缺点**：需要额外封装，但一次投入长期受益。

---

## 注意事项

1. **仅字符串字段受影响**  
   数字、布尔值、数组等类型不会被包装，因此无需特殊处理。

2. **递归提取**  
   方案二中的 `extractValue` 会递归遍历所有层级，可处理深层嵌套对象。

3. **性能**  
   开发环境下会有微小递归开销，不影响生产环境。

4. **该问题在 v10 中依然存在**  
   不要误以为 v10 已修复。只要 i18n 模块为了开发体验保留 AST 信息，这种差异就可能存在。

## 总结

`$tm` 环境差异是 `@nuxtjs/i18n` 模块为了兼顾开发体验和生产优化而产生的副作用。通过封装一个环境自适应的 `useI18nSafe` 组合式函数，可以优雅地解决此问题，让代码在不同环境下都能稳定运行。

如果你在使用中遇到其他问题，欢迎留言交流。
