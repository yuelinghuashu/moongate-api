---
title: 手写一个更适合 Nuxt 的 useRouteQuery：简化 URL 状态同步
description: 封装一套开箱即用的 useRouteQueryString / Number / Array，将 70 行重复的 URL 状态同步代码压缩到 7 行，并彻底解决官方版本的 SSR 隐患。包含完整源码、防抖处理与反向同步示例。
date: 2026-03-22
series: url-state
order: 3
tags:
  - Nuxt
  - Vue
  - State Management
  - Hydration
---

> 在生产项目中，我经历过手写 70 行重复的 `watch` 与 `pushQuery`，也踩过官方 `@vueuse/router` 的 SSR 坑。最终我封装了一套开箱即用的 `useRouteQueryString`、`useRouteQueryNumber`、`useRouteQueryArray`，将代码量从 70 行压缩到 7 行，且完全可控、SSR 安全。本文将分享这套封装的设计思路与完整代码。

## 一、背景：手写方案的痛点

在 Nuxt 中实现 URL 与状态双向同步，最常见的做法是手写整套闭环：为每个状态定义 ref、写一段 `watch(route.query)` 把 URL 变化同步回 ref、再写一个 `pushQuery()` 把 ref 变化写回 URL（完整代码与逐行讲解见系列第 1 篇[《Nuxt 中 URL 与状态双向绑定指南》](./nuxt-url-state-guide) §2.2，此处不再重复）。它的形态大致是：

```ts
// 7 个状态：searchInput / searchOption / page / size / viewMode / level / tags
// 1 段 watch(route.query) → 同步到内部 ref（含 parseTagsFromQuery）
// 1 个 pushQuery() 函数体 + 若干 watch(refs) → 写回 URL
```

重复 7 个状态，代码量庞大，且每个新页面都要重写一遍。这种代码不仅笨重，还容易漏掉某个 `watch`，导致 URL 与状态不同步。

## 二、官方 `useRouteQuery` 的隐患

`@vueuse/router` 提供了 `useRouteQuery`，看似简洁，但我在生产环境踩过坑：它内部使用全局 `WeakMap` + `nextTick` 批量更新，SSR 下可能跨请求污染，最终导致 `Invalid value used as weak map key` 的 500 错误。完整排查经过见系列第 1 篇[《Nuxt 中 URL 与状态双向绑定指南》](./nuxt-url-state-guide) §三，此处不再重复。结论是：我放弃了第三方库，决定自己封装一个稳定、可控的版本。

## 三、封装设计：按类型拆分，各司其职

我将 URL 查询参数按常见类型拆分为三个专用函数，每个函数只做一件事，语义清晰。

### 3.1 基础函数 `useRouteQueryRaw`

不对外暴露，仅用于内部读写原始值，**使用 `replace` 避免产生多余历史记录**：

```ts
function useRouteQueryRaw(name: string) {
  const route = useRoute()
  const router = useRouter()
  const value = ref(route.query[name])

  // 监听路由变化，同步到内部 ref
  watch(
    () => route.query[name],
    (newVal) => {
      value.value = newVal
    },
  )

  // 监听内部 ref 变化，同步到 URL
  watch(value, (newVal) => {
    const query = { ...route.query }
    if (newVal !== undefined && newVal !== null && newVal !== "") {
      query[name] = newVal
    } else {
      delete query[name]
    }
    router.replace({ query })
  })

  return value
}
```

#### 为什么用 `replace` 而不是 `push`？

如果使用 `push`，每次筛选条件变化都会在浏览器历史中产生一条新记录，用户点击后退按钮时需要多次后退才能离开当前页面。`replace` 只替换当前历史记录，用户体验更符合直觉。

### 3.2 字符串类型 `useRouteQueryString`

```ts
export function useRouteQueryString(
  name: string,
  options?: { defaultValue?: string },
) {
  const raw = useRouteQueryRaw(name)
  const defaultValue = options?.defaultValue ?? ""

  return computed({
    get: () => (raw.value?.toString() ?? defaultValue) as string,
    set: (v: string) => {
      raw.value = v === defaultValue ? undefined : v
    },
  }) as Ref<string>
}
```

### 3.3 数字类型 `useRouteQueryNumber`

```ts
export function useRouteQueryNumber(
  name: string,
  options?: { defaultValue?: number },
) {
  const raw = useRouteQueryRaw(name)
  const defaultValue = options?.defaultValue ?? 0

  return computed({
    get: () => {
      const val = raw.value
      if (val === undefined) return defaultValue
      const num = Number(val)
      return isNaN(num) ? defaultValue : num
    },
    set: (v: number) => {
      raw.value = v === defaultValue ? undefined : v.toString()
    },
  }) as Ref<number>
}
```

### 3.4 数组类型：从逗号分隔到多参数格式

在早期版本中，`useRouteQueryArray` 使用逗号分隔格式：

```ts
// 旧版：逗号分隔
set: (v: string[]) => {
  raw.value = v.length ? v.join(",") : undefined
}
// URL：?tag=go,vue
```

当项目引入 Go Gin 后端后，问题出现了：

```go
// Go 后端期望：?tag=go&tag=vue
tags := c.QueryArray("tag")  // 期望 ["go", "vue"]

// 但前端发送的是：?tag=go,vue
tags := c.QueryArray("tag")  // 得到 ["go,vue"] ❌
```

Gin 的 `QueryArray` 原生支持多参数格式（`?tag=go&tag=vue`），但不认识逗号分隔。如果继续用逗号分隔，就需要在 Go 后端手动 `strings.Split` 解析。

与其在每个后端接口都写一遍解析逻辑，不如统一改成多参数格式：

```ts
// 新版：多参数格式
watch(
  value,
  (newVal) => {
    const query = { ...route.query }
    if (newVal.length === 0) {
      delete query[name]
    } else {
      query[name] = newVal // Vue Router 自动展开成 ?tag=go&tag=vue
    }
    router.replace({ query })
  },
  { deep: true },
)
```

#### 一次修改，前后端格式对齐

```text
前端写入：tags.value = ['go', 'vue']
URL 变成：?tag=go&tag=vue
Go 读取：c.QueryArray("tag") → ["go", "vue"] ✅
```

#### 完整实现

```ts
/**
 * 字符串数组类型查询参数
 * 使用多参数格式：?tag=go&tag=vue
 * 与 Gin 的 c.QueryArray("tag") 天然兼容，无需后端额外解析
 */
export function useRouteQueryArray(name: string) {
  const route = useRoute()
  const router = useRouter()

  const getValue = (): string[] => {
    const val = route.query[name]
    if (!val) return []
    return Array.isArray(val) ? val : [val]
  }

  const value = ref(getValue())

  watch(
    () => route.query[name],
    () => {
      const newVal = getValue()
      if (JSON.stringify(value.value) !== JSON.stringify(newVal)) {
        value.value = newVal
      }
    },
  )

  watch(
    value,
    (newVal) => {
      const query = { ...route.query }
      if (newVal.length === 0) {
        delete query[name]
      } else {
        query[name] = newVal
      }
      router.replace({ query })
    },
    { deep: true },
  )

  return value
}
```

| 格式             | URL 示例          | Gin 解析                       | 标准程度     |
| ---------------- | ----------------- | ------------------------------ | ------------ |
| 逗号分隔（旧版） | `?tag=go,vue`     | 需手动 `strings.Split`         | ❌ 非标准    |
| 多参数（新版）   | `?tag=go&tag=vue` | `c.QueryArray("tag")` 原生支持 | ✅ HTTP 标准 |

## 四、使用示例：从 70 行到 7 行

### 4.1 定义状态

```ts
const searchInput = useRouteQueryString("search", { defaultValue: "" })
const searchOption = useRouteQueryNumber("option", { defaultValue: 1 })
const page = useRouteQueryNumber("page", { defaultValue: 1 })
const size = useRouteQueryNumber("size", { defaultValue: 10 })
const viewMode = useRouteQueryNumber("viewMode", { defaultValue: 1 })
const level = useRouteQueryString("level", { defaultValue: "" })
const tags = useRouteQueryArray("tag")
```

### 4.2 在模板中使用

```vue
<template>
  <UInput v-model="searchInput" placeholder="搜索" />
  <!-- 其他筛选组件直接使用对应的状态变量 -->
</template>
```

### 4.3 处理搜索防抖

由于直接修改 `searchInput` 会立即更新 URL，如果你希望实现"输入停止后才更新"的效果，可以引入一个防抖中间变量：

```ts
// 实际搜索词（与 URL 同步）
const searchInput = useRouteQueryString("search", { defaultValue: "" })
const page = useRouteQueryNumber("page", { defaultValue: 1 })

// 防抖中间变量
const searchInputDebounced = ref(searchInput.value)

// 防抖写入 URL
watchDebounced(
  searchInputDebounced,
  (val) => {
    searchInput.value = val
    page.value = 1
  },
  { debounce: 500 },
)

// URL 变化时反向同步到防抖变量（后退/前进时保持输入框一致）
watch(searchInput, (val) => {
  searchInputDebounced.value = val
})
```

模板中绑定 `searchInputDebounced` 而不是 `searchInput`，实现输入防抖同时保持 URL 双向同步。

## 五、方案对比

| 维度         | 手写方案（70行/页面） | 官方 `useRouteQuery`          | 本封装                  |
| ------------ | --------------------- | ----------------------------- | ----------------------- |
| **SSR 安全** | ✅                    | ⚠️ 有隐患（`WeakMap` 跨请求） | ✅                      |
| **数组支持** | 需手动解析            | 需 `transform`                | ✅ 内置                 |
| **数组格式** | 任意                  | 任意                          | **多参数格式**（标准）  |
| **使用便捷** | ❌ 繁琐               | 中等                          | 函数名即类型            |
| **代码量**   | ~70行/页面            | ~15行                         | ~7行                    |
| **历史记录** | 可配置                | `push`                        | `replace`（更符合直觉） |

## 六、SSR 安全保证

以下三条针对上文 3.1 的**简化版实现**成立（其中"初始状态从 URL 同步读取"等更通用的水合原则，与系列第 1 篇[《Nuxt 中 URL 与状态双向绑定指南》](./nuxt-url-state-guide) §四一致，此处不展开）：

- **无全局状态**：所有数据存储在组件实例的 `ref` 中，不会跨请求污染。
- **直接监听 `route.query`**：保证服务端和客户端初始值一致。
- **不使用 `nextTick`**：避免在 SSR 中因异步更新导致 DOM 不匹配。

> **实现演进（重要）**：上文 3.1 的简化版每个参数独立 `watch`，确实"无全局状态"。但当页面需要 **`resetFilters` 一次性重置多个参数** 时，多个独立 watch 会在同一 tick 各自基于旧的 `route.query` 写回，导致前面的修改被后面的覆盖（重置 6 个参数最终只生效最后一个）。
>
> 为解决覆盖问题，实际项目的实现演进出**注册表机制**：所有 `useRouteQuery*` 参数先向同一个注册表登记，写回 URL 前从注册表读取所有参数的最新值，一次构建完整 query。此时"无全局状态"的前提已不成立——注册表本身就是共享状态，它的存放位置直接决定 SSR 安全性：
>
> - ❌ **模块级 `Set`**：跨请求累积（`onUnmounted` 在服务端不触发），会造成服务端内存泄漏——正是 [《Nuxt SSR 内存泄漏排查实录》](./nuxt-ssr-memory-leak-troubleshooting) 记录的真实案例。
> - ✅ **以 `nuxtApp` 为 key 的 `WeakMap`**：服务端每个请求有独立注册表（请求结束随 nuxtApp 被 GC），客户端全局唯一注册表（组件卸载时由 `onUnmounted` 清理）。这才是"SSR 安全"的完整形态。

## 七、完整代码

```ts
// composables/useRouteQuery.ts
import { useRoute, useRouter } from "vue-router"
import type { Ref } from "vue"

/**
 * 基础原始查询参数读写（不暴露给外部，仅内部使用）
 * 负责核心的 URL 同步逻辑，使用 replace 避免产生多余历史记录
 */
function useRouteQueryRaw(name: string) {
  const route = useRoute()
  const router = useRouter()
  const value = ref(route.query[name])

  watch(
    () => route.query[name],
    (newVal) => {
      value.value = newVal
    },
  )

  watch(value, (newVal) => {
    const query = { ...route.query }
    if (newVal !== undefined && newVal !== null && newVal !== "") {
      query[name] = newVal
    } else {
      delete query[name]
    }
    router.replace({ query })
  })

  return value
}

/**
 * 字符串类型查询参数
 */
export function useRouteQueryString(
  name: string,
  options?: { defaultValue?: string },
) {
  const raw = useRouteQueryRaw(name)
  const defaultValue = options?.defaultValue ?? ""
  return computed({
    get: () => (raw.value?.toString() ?? defaultValue) as string,
    set: (v: string) => {
      raw.value = v === defaultValue ? undefined : v
    },
  }) as Ref<string>
}

/**
 * 数字类型查询参数
 */
export function useRouteQueryNumber(
  name: string,
  options?: { defaultValue?: number },
) {
  const raw = useRouteQueryRaw(name)
  const defaultValue = options?.defaultValue ?? 0
  return computed({
    get: () => {
      const val = raw.value
      if (val === undefined) return defaultValue
      const num = Number(val)
      return isNaN(num) ? defaultValue : num
    },
    set: (v: number) => {
      raw.value = v === defaultValue ? undefined : v.toString()
    },
  }) as Ref<number>
}

/**
 * 字符串数组类型查询参数
 * 使用多参数格式：?tag=go&tag=vue
 * 与 Gin 的 c.QueryArray("tag") 天然兼容，无需后端额外解析
 */
export function useRouteQueryArray(name: string) {
  const route = useRoute()
  const router = useRouter()

  const getValue = (): string[] => {
    const val = route.query[name]
    if (!val) return []
    return Array.isArray(val) ? val : [val]
  }

  const value = ref(getValue())

  watch(
    () => route.query[name],
    () => {
      const newVal = getValue()
      if (JSON.stringify(value.value) !== JSON.stringify(newVal)) {
        value.value = newVal
      }
    },
  )

  watch(
    value,
    (newVal) => {
      const query = { ...route.query }
      if (newVal.length === 0) {
        delete query[name]
      } else {
        query[name] = newVal
      }
      router.replace({ query })
    },
    { deep: true },
  )

  return value
}
```

## 八、总结

这套封装解决了四个核心问题：

1. **减少重复代码**：从 70 行重复逻辑缩减到 7 行声明。
2. **保证 SSR 安全**：无全局状态、无 `nextTick` 依赖，彻底避免水合错误。
3. **更好的历史记录体验**：使用 `replace` 而非 `push`，避免后退按钮产生困惑。
4. **标准数组格式**：使用多参数格式（`?tag=go&tag=vue`），与主流后端框架天然兼容。

其中第 4 点是在引入 Go Gin 后端后才意识到的。最初的设计用了逗号分隔，但当后端需要读取 `?tag=go&tag=vue` 时，才发现格式不兼容。这个教训让我意识到：**前端组件的设计不仅要考虑前端使用体验，也要考虑后端接口的兼容性。** 多参数格式是 HTTP 标准，比自定义的逗号分隔格式更通用。

如果你的项目中也有类似的 URL 状态同步需求，不妨试试这套封装。它已经在我的 Nuxt 项目中稳定运行，希望也能帮到你。
