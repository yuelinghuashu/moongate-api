---
title: Vue 3 复杂组件开发实战：Select 与 Pagination 的 API 设计与状态管理
description: 以 Select 和 Pagination 为例，深入探讨 Vue 3 复杂组件的 API 设计、数据格式适配、类型回溯、路由同步、键盘交互、组合式函数抽离及 SSR 适配，揭示工业级组件背后的设计权衡与实现细节。
date: 2026-05-19
permalink: 62e1afa3-c02e-4bf8-bc52-36f3b13032e9
series: moongate-vue
level: P3
tags:
  - Vue
  - Design System
  - Engineering
---

> 从数据格式适配到路由同步，深入复杂组件的设计要点与逻辑复用

## 📚 系列导航

本系列共五篇，覆盖从设计令牌到 npm 发布的 Vue 3 组件库开发全流程：

1. [**设计令牌 vs 原子化 CSS：失败整合与融合之道（理念篇）**](./design-tokens-vs-atomic-css)
   —— 用 UnoCSS 映射设计令牌的失败经历，量化对比后得出设计令牌优先的结论。

2. [**CSS 优先 + 组件薄封装：一个 10KB 组件库的极简实践（架构篇）**](./css-first-component-library)
   —— 四层 CSS 架构、极简 Vue 组件、体积分析与维护性对比，500 行代码构建 10KB 组件库。

3. [**Vue 3 简单组件开发实战：从 Button 组件看 API 设计（简单组件篇）**](./vue-component-api-design)
   —— Props 定义、变体系统、尺寸取舍、插槽设计、状态管理、无障碍支持及与主流 UI 库对比。

4. [**Vue 3 复杂组件开发实战：Select 与 Pagination 的 API 设计（复杂组件篇）**](./complex-component-api-design)
   —— 数据格式适配、类型回溯、路由同步、键盘交互、组合式函数抽离及 SSR 适配，揭示工业级细节。

5. [**从代码到 npm：Vue 3 组件库发布实战与避坑指南（发布篇）**](./component-library-publishing)
   —— nrm 源管理、2FA 配置、WebAuthn 网络代理避坑、本地链接测试、自动化脚本及工业级发布检查清单。

## 一、引言

如果说写 Button 组件是在享受写 CSS 变量的“涂料之美”，那么写 Select 和 Pagination 就是在应对原生 HTML 历史包袱的“泥潭摔跤”。简单组件是单向的数据消费者，而复杂组件则是**数据适配器**（兼容多格式）与**状态同步器**（协同路由与键盘）。

本文以 **Select** 和 **Pagination** 为例，展示复杂组件的开发思路，涵盖：

- 灵活的数据格式支持（对象数组、字符串数组、数字数组）
- 自定义字段映射（`labelKey` / `valueKey`）
- 路由状态同步（页码、筛选条件与 URL 绑定）
- 辅助功能（键盘翻页、移动端手势）
- 逻辑抽离与复用（组合式函数）
- 工业级细节：类型回溯、防御性编程、事件防污染、极端边界处理

## 二、Select 下拉选择框：数据适配器

Select 组件需要接收一组选项，并允许用户选择其中一个。真实世界的 API 可能返回对象数组、字符串数组甚至数字数组，因此组件必须具备强大的数据适配能力。

### 2.1 需求分析

- 支持对象数组 `{ label, value }`（默认）
- 支持自定义字段名（`labelKey` / `valueKey`）
- 支持字符串数组 `['选项A', '选项B']`
- 支持数字数组 `[1, 2, 3]`
- 提供占位符（不可选中的默认选项）
- 支持禁用选项（`disabled: true`）
- 支持错误状态、尺寸、禁用等常规属性
- **必须解决原生 `<select>` 返回字符串的类型陷阱**
- **支持原生 `<form>` 提交（通过 `name` 属性）**

### 2.2 API 设计与类型防腐

为了避免 `any` 造成类型污染，我们采用联合类型收窄。

```typescript
type SelectOption = string | number | Record<string, any>;

interface Props {
  modelValue?: string | number;
  options?: SelectOption[]; // 联合类型，拒绝裸 any
  labelKey?: string; // 默认 'label'
  valueKey?: string; // 默认 'value'
  placeholder?: string;
  name?: string; // 支持原生 form 提交
  size?: "sm" | "md" | "lg";
  disabled?: boolean;
  error?: boolean;
}
```

### 2.3 内部实现：防御性编程 + 类型回溯

```vue
<template>
  <select
    class="mg-select"
    :class="[`mg-select-${size}`, { 'mg-select-error': error }]"
    :value="modelValue"
    :disabled="disabled"
    :name="name"
    v-bind="$attrs"
    @change="handleChange"
  >
    <option v-if="placeholder" value="" disabled hidden>
      {{ placeholder }}
    </option>
    <option
      v-for="item in options || []"
      :key="getValue(item)"
      :value="getValue(item)"
      :disabled="item.disabled"
    >
      {{ getLabel(item) }}
    </option>
  </select>
</template>

<script setup lang="ts">
// ... 类型定义

const getLabel = (item: any): string => {
  if (item === null || item === undefined) return "";
  if (typeof item !== "object") return String(item);
  const label = item[props.labelKey];
  return label !== undefined ? String(label) : String(item);
};

const getValue = (item: any): any => {
  if (item === null || item === undefined) return undefined;
  if (typeof item !== "object") return item;
  const val = item[props.valueKey];
  return val !== undefined ? val : item;
};

// 🔥 核心：解决原生 select 总是返回字符串的致命问题
const handleChange = (event: Event) => {
  const target = event.target as HTMLSelectElement;
  const rawValue = target.value;

  // 尝试在原始 options 中找回原始类型（数字或对象值）
  const originalItem = (props.options || []).find(
    (item) => String(getValue(item)) === rawValue,
  );
  const finalValue =
    originalItem !== undefined ? getValue(originalItem) : rawValue;

  emit("update:modelValue", finalValue);
  emit("change", finalValue);
};
</script>
```

**防御性编程说明**：`options || []` 确保即使外部传入 `undefined` 或延迟加载，组件也不会白屏。类型回溯逻辑保证了 `v-model` 绑定的数字值不会意外变成字符串。显式支持 `name` 属性，使 `<select>` 能够参与原生表单提交。

### 2.4 使用示例

```vue
<!-- 对象数组（默认字段） -->
<Select v-model="category" :options="categories" name="category" />

<!-- 自定义字段名 -->
<Select v-model="category" :options="cats" label-key="name" value-key="id" />

<!-- 字符串数组（类型自动保持） -->
<Select v-model="color" :options="['红', '绿', '蓝']" />

<!-- 数字数组 -->
<Select v-model="num" :options="[10, 20, 30]" />

<!-- 禁用选项 -->
<Select v-model="status" :options="statusOptions" />
```

## 三、Pagination 分页组件：状态同步器

分页组件需要与当前页码、每页条数配合，并提供上一页/下一页按钮，支持直接输入页码跳转。在 Nuxt 项目中往往需要将页码同步到 URL，并支持键盘左右键翻页（不干扰输入框）。

### 3.1 需求分析

- 显示 `当前页 / 总页数`
- 上一页/下一页按钮（边界禁用）
- 点击当前页码变成输入框，支持直接输入跳转
- 与路由 query 同步（页码变化时更新 URL）
- 支持键盘左右键翻页（**必须不干扰输入框**）
- 极端边界防御（输入框清空、超出范围等）
- 尺寸适配

### 3.2 API 设计

```typescript
interface Props {
  currentPage: number; // v-model:current-page
  totalPages: number;
  size?: "sm" | "md" | "lg";
}
```

### 3.3 核心实现（含极端边界防御）

```vue
<template>
  <nav class="mg-pagination" :class="`mg-pagination-${size}`">
    <button
      class="mg-pagination-btn"
      :disabled="currentPage === 1"
      @click="goPrev"
    >
      上一页
    </button>

    <span v-if="!editing" class="mg-pagination-current" @click="startEdit">
      {{ currentPage }}
    </span>
    <input
      v-else
      ref="inputRef"
      v-model="inputPage"
      type="number"
      class="mg-pagination-input"
      :min="1"
      :max="totalPages"
      @blur="commit"
      @keyup.enter="commit"
    />

    <span class="mg-pagination-sep">/</span>
    <span class="mg-pagination-total">{{ totalPages }}</span>

    <button
      class="mg-pagination-btn"
      :disabled="currentPage === totalPages"
      @click="goNext"
    >
      下一页
    </button>
  </nav>
</template>

<script setup lang="ts">
import { ref, nextTick } from "vue";

const props = defineProps<{
  currentPage: number;
  totalPages: number;
  size?: string;
}>();
const emit = defineEmits<{ "update:currentPage": [page: number] }>();

const editing = ref(false);
const inputPage = ref(props.currentPage);
const inputRef = ref<HTMLInputElement>();

const goPrev = () => {
  if (props.currentPage > 1) emit("update:currentPage", props.currentPage - 1);
};
const goNext = () => {
  if (props.currentPage < props.totalPages)
    emit("update:currentPage", props.currentPage + 1);
};

const startEdit = () => {
  editing.value = true;
  inputPage.value = props.currentPage;
  nextTick(() => inputRef.value?.focus());
};

const commit = () => {
  editing.value = false;
  const raw = inputPage.value;
  const pageNum = parseInt(String(raw), 10);

  // 非法输入直接放弃，不更新页码
  if (isNaN(pageNum)) return;

  let page = Math.min(Math.max(pageNum, 1), props.totalPages);
  if (page !== props.currentPage) {
    emit("update:currentPage", page);
  }
};
</script>
```

### 3.4 键盘翻页逻辑（组合式函数）

为了保持组件纯净，将键盘事件抽离为 `useKeyboardPagination`，并确保不干扰输入框。

```typescript
// composables/useKeyboardPagination.ts
import { onMounted, onUnmounted } from "vue";

export function useKeyboardPagination(onPrev: () => void, onNext: () => void) {
  const handleKeyDown = (e: KeyboardEvent) => {
    // 🔥 关键：如果焦点在输入框或文本域内，不触发翻页
    const target = e.target as HTMLElement;
    if (target.tagName === "INPUT" || target.tagName === "TEXTAREA") return;

    if (e.key === "ArrowLeft") onPrev();
    if (e.key === "ArrowRight") onNext();
  };

  onMounted(() => window.addEventListener("keydown", handleKeyDown));
  onUnmounted(() => window.removeEventListener("keydown", handleKeyDown));
}
```

### 3.5 路由同步与全能 Composable

为了展示“一键装配”的优雅，我们将页码路由同步、键盘事件、翻页方法整合为一个全能组合式函数。

```typescript
// composables/useMoongatePagination.ts
import { ref, type Ref } from "vue";
import { useRouteQueryNumber } from "./useRouteQuery";
import { useKeyboardPagination } from "./useKeyboardPagination";

export function useMoongatePagination(totalPages: Ref<number>) {
  const page = useRouteQueryNumber("page", { defaultValue: 1 });

  const goPrev = () => {
    if (page.value > 1) page.value--;
  };
  const goNext = () => {
    if (page.value < totalPages.value) page.value++;
  };

  // 内部直接装配键盘事件
  useKeyboardPagination(goPrev, goNext);

  return { page, goPrev, goNext };
}
```

在业务页面中使用：

```vue
<script setup>
const totalPages = ref(10);
const { page, goPrev, goNext } = useMoongatePagination(totalPages);
// 无需再单独处理键盘事件，一切自动完成
</script>

<template>
  <Pagination v-model:current-page="page" :total-pages="totalPages" />
  <!-- 你也可以自定义按钮，与分页组件状态联动 -->
  <button @click="goPrev">上一页</button>
  <button @click="goNext">下一页</button>
</template>
```

## 四、复杂组件的数据与状态流向

```text
┌─────────────────────────────────────────────────────────────┐
│ 复杂组件数据流 │
│ ┌─────────────┐ ┌─────────────┐ ┌─────────────┐ │
│ │ 外部数据 │ -> │ 数据适配器 │ -> │ 内部状态 │ │
│ │ (options) │ │ (getLabel/ │ │ (selected) │ │
│ └─────────────┘ │ getValue) │ └──────┬──────┘ │
│ └─────────────┘ │ │
│ ↓ │
│ ┌─────────────┐ ┌─────────────┐ ┌─────────────┐ │
│ │ 全局环境 │ <- │ 状态同步器 │ <- │ 用户交互 │ │
│ │ (路由/键盘) │ │ (watch/ │ │ (click/ │ │
│ └─────────────┘ │ event) │ │ keyboard) │ │
│ └─────────────┘ └─────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

## 五、复杂组件的测试策略

| 关注点       | 简单组件（Button）              | 复杂组件（Select / Pagination）                                                              |
| ------------ | ------------------------------- | -------------------------------------------------------------------------------------------- |
| **测试策略** | 快照测试、简单的 Click 事件触发 | **状态快照组合、边界值测试（第0页/超出总页数）、DOM 聚焦与键盘模拟测试、类型回溯健壮性测试** |

## 六、SSR 适配：组件库的"最后一公里"

复杂组件往往依赖浏览器 API（`document`、`window`），在 SSR 环境下会直接报错 `document is not defined`。本节以 Popover、Tooltip、Drawer 为例，展示如何系统性地适配 SSR。

### 6.1 问题根源

Vue 组件在服务端渲染时，`onMounted` 不会执行，但 `watch` 的 `immediate: true` 和 `<script setup>` 顶层代码**会执行**。以下代码在 SSR 时会崩溃：

```typescript
// ❌ SSR 报错：document is not defined
watch(
  () => modelValue.value,
  (val) => {
    if (val) {
      document.body.style.overflow = "hidden"; // ← 服务端没有 document
    }
  },
  { immediate: true },
);
```

### 6.2 统一解决方案

**第一步：定义 `isBrowser` 守卫**

```typescript
const isBrowser =
  typeof window !== "undefined" && typeof document !== "undefined";
```

**第二步：在所有 `document`/`window` 访问处添加守卫**

```typescript
// ✅ 正确做法
watch(
  () => modelValue.value,
  (val) => {
    if (!isBrowser) return; // SSR 安全
    if (val) {
      document.body.style.overflow = "hidden";
      window.addEventListener("keydown", handleKeydown);
    }
  },
  { immediate: true },
);

onMounted(() => {
  if (!isBrowser) return;
  // 只有浏览器环境才执行 DOM 操作
});

onBeforeUnmount(() => {
  if (!isBrowser) return;
  document.body.style.overflow = "";
  window.removeEventListener("keydown", handleKeydown);
});
```

### 6.3 需要适配的组件清单

| 组件                      | 涉及的浏览器 API                                                        | 处理方式                              |
| ------------------------- | ----------------------------------------------------------------------- | ------------------------------------- |
| **Drawer / Modal**        | `document.body.style.overflow`、`addEventListener`                      | watch + onBeforeUnmount 加守卫        |
| **Popover / Tooltip**     | `window.innerWidth/Height`、`getBoundingClientRect`、`MutationObserver` | 全部放在 onMounted 中                 |
| **useToast / useMessage** | `document.createElement`、`appendChild`                                 | 命令式调用加 `if (!isBrowser) return` |

### 6.4 效果验证

适配后，组件库可在 **Nuxt 4**、**VitePress** 等服务端渲染框架中无缝运行，无需用户额外配置。

## 七、总结

| 关注点           | 简单组件（Button） | 复杂组件（Select / Pagination）                       |
| ---------------- | ------------------ | ----------------------------------------------------- |
| **Props 数量**   | 较少（5-8）        | 较多（10+）                                           |
| **数据格式**     | 固定（字符串）     | 灵活（支持多种数组，可配置字段，类型防腐）            |
| **状态管理**     | 无内部状态         | 可能有内部编辑状态、输入框显隐、极端边界防御          |
| **外部依赖**     | 无                 | 路由、键盘事件、手势                                  |
| **逻辑复用**     | 不需要             | 强烈推荐抽离 composable，并可整合全能函数             |
| **防御性编程**   | 低                 | 高（需处理 undefined 数据、类型回溯、清空输入框恢复） |
| **原生表单集成** | 自动透传 `name`    | 显式支持 `name` 属性，可参与 `<form>` 提交            |
| **测试策略**     | 快照、事件触发     | 状态组合、边界值、键盘模拟、类型回溯                  |

一个优秀的复杂组件，对内要像吸尘器一样容纳各种奇葩的后端数据格式（通过 Key 映射和类型回溯），对外要像绅士一样克制地与全局环境（路由、窗口键盘）发生耦合。**高内聚、低耦合**，在这两类组件身上体现得淋漓尽致。
