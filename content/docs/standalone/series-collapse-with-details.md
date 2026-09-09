---
title: 用原生 <details> 实现系列折叠页：从“点两次”到“稳定可控”
description: 本文记录了在博客系列页面中使用原生 `<details>` 实现折叠列表时，添加“全部折叠/展开”按钮遇到的“点两次”问题，通过放弃 `toggle` 事件监听、直接同步状态变量和使用 `setTimeout` 等待 DOM 更新，最终实现稳定可控的全局控制功能。包含完整代码和原理分析。
date: 2026-03-24
series:
tags:
  - HTML
  - CSS
  - JavaScript
  - Nuxt
---

> 个人博客的系列页面，我选择了原生 `<details>` 元素来实现折叠列表。本以为是最简单直接的方式，却在添加“全部折叠/展开”按钮时遇到了“点两次”的诡异问题。这篇文章记录了排查和解决的过程，也让我对原生 DOM 与状态同步有了更深的理解。

---

## 1. 背景与需求

我的博客最近新增了“系列”页面，用于按主题聚合文章（如“URL状态同步”、“设计系统”等）。每个系列包含多篇文章，默认折叠，用户点击系列标题可展开查看该系列下的文章列表。同时，页面右上角需要一个按钮，能够**一键折叠/展开所有系列**。

理想很朴素：用原生 `<details>` 和 `<summary>` 实现折叠面板，再写几行 JavaScript 控制全局按钮。但实际实现中，我遇到了一个典型问题：**点击全局按钮时，需要点击两次才能完成切换**。下面记录完整的实现过程与解决方案。

---

## 2. 初始实现：简单的 `<details>` 循环

首先，我从数据库中获取所有文章，按系列分组，渲染成一个 `<details>` 列表。每个系列标题显示系列名和文章数量，内部展示文章标题、等级和日期。

```vue
<template>
  <div class="max-w-3xl mx-auto">
    <div v-for="series in seriesList" :key="series.slug" class="mb-4">
      <details>
        <summary class="flex items-center gap-2 cursor-pointer">
          <span>{{ series.name }}</span>
          <span class="text-sm text-gray-500">({{ series.docs.length }})</span>
        </summary>
        <div class="pl-4 mt-2 space-y-2">
          <div v-for="article in series.docs" :key="article.id">
            <NuxtLink :to="article.path" class="text-blue-600 hover:underline">
              {{ article.title }}
            </NuxtLink>
            <div class="text-xs text-gray-500">
              {{ article.level }} · {{ formatDate(article.date) }}
            </div>
          </div>
        </div>
      </details>
    </div>
  </div>
</template>
```

这一步一切正常，每个系列都可以独立展开/折叠。

---

## 3. 添加“全部折叠/展开”按钮

为了实现全局控制，我需要一个变量来记录当前是否有系列处于展开状态，并据此决定按钮的行为（全部折叠或全部展开）。

```ts
const isAnyExpanded = ref(false)

// 更新全局状态：遍历所有 <details>，检查是否有 open 属性为 true
const updateAnyExpanded = () => {
  const details = document.querySelectorAll("details")
  isAnyExpanded.value = Array.from(details).some((detail) => detail.open)
}

// 全部折叠/展开
const toggleAll = () => {
  const details = document.querySelectorAll("details")
  const shouldExpand = !isAnyExpanded.value
  details.forEach((detail) => {
    detail.open = shouldExpand
  })
  updateAnyExpanded() // 更新状态
}
```

### 问题出现了

点击“全部折叠/展开”按钮时，出现了“要点**两次**才生效”的诡异问题：第一次点击似乎没有反应，第二次才正确切换所有系列的状态。

先看初版代码的缺口：`isAnyExpanded` 只在 `toggleAll`（以及它调用的 `updateAnyExpanded`）里被更新，而用户**手动点击 `<summary>` 时并不会**更新它。一旦手动展开/折叠过某个系列，按钮依赖的“当前是否有系列展开”就已经和真实 DOM **脱节**：下一次点击“全部折叠/展开”会基于这个**过期状态**算出错误的目标动作，看起来“没反应”；要再点一次（此时状态已被上一次操作修正）才生效。

于是我想当然地加了一层全局 `toggle` 事件监听，想把手动点击也同步进来——结果问题变得更隐蔽，见下一节。

---

## 4. 问题定位：两套状态同步机制互相打架

为什么会出现“点两次”？先厘清两个平台事实：

1. **`toggle` 事件是异步的，而且任何 `open` 变化都会触发它**。无论用户点击 `<summary>`，还是 JavaScript 直接修改 `open` 属性，只要 `open` 状态发生改变，浏览器就会**排队一个任务**去触发 `toggle` 事件。也就是说，程序化修改同样会触发 `toggle`，只是**异步**触发；并且该事件**不冒泡、不可取消**。
2. **`<summary>` 的点击默认动作在事件派发结束后才同步执行**。点击 `<summary>` 时，浏览器先派发 `click` 事件（此刻 `open` 仍是旧值），派发结束后才**同步**执行默认动作去切换 `open`。因此在 `@click` 处理器内部同步读取 `detail.open`，读到的必然是**切换前**的旧值。

回到我的实现。我在 `onMounted` 里加了全局 `toggle` 事件监听，试图把手动点击的状态同步给按钮。但这套“事件同步”与 `toggleAll` 的“直接写状态”两套机制开始互相打架：

- `toggleAll` 批量设置所有 `open` 后，**立即**把 `isAnyExpanded` 写成目标值；
- 同一批修改会让浏览器为**每一个** `<details>`（程序化变化同样会触发）**排队一个异步 `toggle` 回调**，N 个元素就是 N 个回调；
- 这些异步回调随后逐个执行、再次写入状态，与 `toggleAll` 里刚写好的值交错覆盖——最终按钮的意图和真实 DOM 失步，表现就是“要点两次才生效”。

简单来说：**批量操作的“直接同步”与手动点击的“事件同步”混在一起，异步回调把状态覆盖回了错误的值**。要解决它，只需要保留一套同步机制。

---

## 5. 解决方案：放弃事件监听，直接同步状态

既然 `toggle` 事件会干扰批量操作，我决定**完全放弃监听 `toggle` 事件**，改为：

- 批量操作时，直接根据目标状态设置所有 `<details>` 的 `open`，并**立即将状态变量设为目标值**，不再依赖 DOM 查询。
- 手动点击时，通过 `<summary>` 的 `click` 事件，用 `setTimeout` 把读取推迟到 `click` 默认动作**执行完之后**——此时 `open` 才是切换后的新值。

### 修改后的 `toggleAll` 函数

```ts
const toggleAll = () => {
  const details = document.querySelectorAll("details")
  const shouldExpand = !isAnyExpanded.value // 目标状态
  details.forEach((detail) => {
    detail.open = shouldExpand
  })
  // 直接根据本次意图设置状态，不再调用 updateAnyExpanded
  isAnyExpanded.value = shouldExpand
}
```

### 手动点击时的状态同步

为 `<summary>` 添加 `@click="onSummaryClick"`，在回调中使用 `setTimeout` 延迟到下一个事件循环再读取 DOM 状态。

```ts
const onSummaryClick = () => {
  setTimeout(() => {
    updateAnyExpanded()
  }, 0)
}
```

模板中的 `<summary>`：

```vue
<summary class="flex items-center gap-2 cursor-pointer" @click="onSummaryClick">
  <span>{{ series.name }}</span>
  <span class="text-sm text-gray-500">({{ series.docs.length }})</span>
</summary>
```

### 组件挂载时初始化状态

```ts
onMounted(() => {
  updateAnyExpanded()
})
```

#### 为什么用 `setTimeout(..., 0)`？

点击 `<summary>` 后的事件顺序是：浏览器先派发 `click` 事件（此刻 `open` 仍是旧值），派发结束**同步**执行默认动作切换 `open`，之后 `toggle` 事件才被**异步**触发。`setTimeout(0)` 把读取推迟到下一个宏任务——此时默认动作早已执行完毕，读到的必然是切换后的 `open`。

这里的要点是：**不要用 `toggle` 事件来同步状态**。它不但异步触发，而且与 `setTimeout` 回调之间没有可靠的先后顺序，依赖它去“修正”状态必然引入竞态——这正是上一节“点两次”问题的根源。只保留“点击后延迟读一次 DOM”这一套机制，状态就永远是准的。

> 如果用户快速连续点击同一个 `<summary>`，多个 `setTimeout` 会排队执行，可能导致 `updateAnyExpanded` 被多次调用。虽然最终状态正确，但会有不必要的性能开销。由于系列页面的交互频率极低，这个影响可以忽略；如果希望更严谨，可以增加一个简单的防抖函数，但当前实现已经足够稳定。

---

## 6. 最终代码

以下是完整的系列页面代码，包含数据获取、分组、折叠控制及样式。该实现假设页面在加载后不会动态增减 `<details>`（符合博客系列页的静态特性），因此状态同步逻辑简单可靠。

```vue
<template>
  <div class="max-w-3xl mx-auto">
    <div class="flex justify-end items-center mb-6">
      <UButton
        :ui="{ leadingIcon: 'toolbar-icon-btn' }"
        class="cursor-pointer"
        variant="ghost"
        :icon="isAnyExpanded ? 'lucide-chevrons-up' : 'lucide-chevrons-down'"
        @click="toggleAll"
      />
    </div>

    <div v-for="series in seriesList" :key="series.slug" class="mb-4">
      <details>
        <summary
          class="flex items-center gap-2 cursor-pointer"
          @click="onSummaryClick"
        >
          <span>{{ series.name }}</span>
          <span class="text-sm text-gray-500">({{ series.docs.length }})</span>
        </summary>
        <div class="pl-4 mt-2 space-y-2">
          <div v-for="article in series.docs" :key="article.id">
            <NuxtLink :to="article.path" class="text-blue-600 hover:underline">
              {{ article.title }}
            </NuxtLink>
            <div class="text-xs text-gray-500">
              {{ article.level }} · {{ formatDate(article.date) }}
            </div>
          </div>
        </div>
      </details>
    </div>
  </div>
</template>

<script setup lang="ts">
import dayjs from "dayjs"

const { tm } = useI18nSafe()

// ==================== 数据获取与分组 ====================
const { data: docsList } = await useAsyncData("series", () => {
  return queryCollection("docs")
    .order("date", "ASC")
    .select("id", "series", "title", "level", "path", "seo", "date")
    .all()
})

// 将文档按 series 分组
const seriesMap = computed(() => {
  const map = new Map<string, typeof docsList.value>()
  if (!docsList.value) return map
  for (const doc of docsList.value) {
    if (!doc.series) continue
    if (!map.has(doc.series)) map.set(doc.series, [])
    map.get(doc.series)!.push(doc)
  }
  return map
})

// 系列列表（从 i18n 获取系列名）
const seriesList = computed(() => {
  const seriesObj = tm("series") as Record<string, string>
  return Object.entries(seriesObj).map(([slug, name]) => ({
    slug,
    name,
    docs: seriesMap.value.get(slug) || [],
  }))
})

// ==================== 折叠/展开所有系列 ====================

// 记录当前是否有任何系列处于展开状态，用于动态切换按钮图标
const isAnyExpanded = ref(false)

/**
 * 更新全局展开状态
 * 直接遍历 DOM 中所有 <details> 元素，检查是否有任一展开
 * 该函数仅在手动点击 summary 时调用，用于同步按钮状态
 */
const updateAnyExpanded = () => {
  const details = document.querySelectorAll("details")
  isAnyExpanded.value = Array.from(details).some((detail) => detail.open)
}

/**
 * 切换所有系列的展开/折叠状态
 * 由右上角按钮触发
 * 1. 根据当前 isAnyExpanded 计算目标状态（全部展开或全部折叠）
 * 2. 批量设置所有 <details> 的 open 属性
 * 3. 直接更新 isAnyExpanded 为目标状态，无需再次查询 DOM
 */
const toggleAll = () => {
  const details = document.querySelectorAll("details")
  const shouldExpand = !isAnyExpanded.value // 目标状态：当前全部折叠则展开，否则折叠
  details.forEach((detail) => {
    detail.open = shouldExpand
  })
  // 直接根据本次操作意图设置状态，避免因 toggle 事件干扰而需要两次点击
  isAnyExpanded.value = shouldExpand
}

/**
 * 用户手动点击 summary（系列标题）时的回调
 * click 事件的默认动作会在事件派发结束后才切换 <details> 的 open，
 * 处理器内同步读取到的仍是旧值；因此推迟到 setTimeout 后再读取，
 * 此时 open 已是切换后的新值，据此同步按钮图标
 */
const onSummaryClick = () => {
  setTimeout(() => {
    updateAnyExpanded()
  }, 0)
}

/**
 * 组件挂载后，初始化全局展开状态（页面加载时所有 <details> 默认为折叠）
 */
onMounted(() => {
  updateAnyExpanded()
})

// ==================== 辅助函数 ====================
const formatDate = (date: string) => dayjs(date).format("YYYY-MM-DD")
</script>

<style scoped>
/* 隐藏 details 默认的三角形图标（与 flex 布局无关，确保所有浏览器都隐藏） */
details > summary {
  list-style: none;
}
details > summary::-webkit-details-marker {
  display: none;
}
</style>
```

> **注**：本实现假设页面中的 `<details>` 元素数量在加载后不会发生变化（符合博客系列页的静态特性）。如果后续通过异步操作动态增删系列，则需要对 `isAnyExpanded` 的同步逻辑做额外处理（例如在增删时手动调用 `updateAnyExpanded`）。不过对个人博客而言，当前实现已足够稳定。

---

## 7. 思考与总结

### 为什么不用 UI 库的手风琴组件？

我的博客追求极简风格，不希望引入太多依赖。原生 `<details>` 足以满足基础折叠功能，且代码量少，完全可控。虽然官方组件（如 Nuxt UI 的 `UAccordion`）功能更强大（动画、多选、无障碍），但对个人博客而言，**够用就好**。

### 这次踩坑的收获

- **批量操作原生 DOM 时，要警惕其触发的事件对状态的影响**。批量修改时，直接同步状态变量比依赖 DOM 事件更可靠。
- **用 `setTimeout(..., 0)` 等待 DOM 更新后再读取状态是常见模式**，尤其适用于需要等待异步事件或框架响应式更新完成的情况。
- **放弃复杂的全局事件监听，采用简单的点击回调配合状态变量，代码更可控**。

### 什么时候适合用原生 `<details>`？

- 项目规模小，不需要复杂动画
- 你希望完全控制样式和行为
- 读者群体以技术人群为主，对原生 Web 标准接受度高

### 什么时候该用组件库的手风琴？

- 需要平滑动画、键盘导航、多选模式
- 团队协作，希望快速交付
- 无障碍要求高

---

## 8. 结语

这次为系列页面添加“全部折叠/展开”功能，让我对原生 DOM 操作和 Vue 状态同步有了更深的认识。虽然只是一个小小的功能，但背后的原理和调试过程值得记录。希望这篇文章能帮到同样在使用原生 `<details>` 时遇到类似问题的你。

---

> **后记（方案演进）**：本文记录的 `<details>` + DOM 查询方案是系列页的早期实现。后来该页面已重构为**纯状态驱动**方案：`<button>` + `v-show` + 一个 `expandedSeries` 数组（展开状态存于响应式状态，并持久化到 `localStorage`），不再需要 `document.querySelectorAll("details")`，也不需要 `setTimeout` 延迟同步。展开状态只由 Vue 维护，手动点击与“全部展开/折叠”走同一套更新逻辑，SSR 水合与按钮图标天然一致——本文踩到的“DOM 事件同步脆弱”问题也随之消失。重构后的实现见博客仓库的 `app/pages/series.vue`。
