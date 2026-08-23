---
title: Vue 3 简单组件开发实战：从 Button 组件看 API 设计
description: 以 Button 这一简单组件为例，深入探讨 Vue 3 组件库的 API 设计哲学，涵盖 Props 定义、变体系统、尺寸取舍、插槽设计、状态管理、无障碍支持及与主流 UI 库的对比，揭示极简 API 背后的设计权衡。
date: 2026-05-07
permalink: e6526a2f-4d33-4817-9ef0-1cbfb71ef3e7
series: moongate-vue
level: P3
tags:
  - Vue
  - Design System
  - Engineering
---

> 极简不是简陋，克制不是缺失——以 Button 为例，看一个 11 个 props 的组件如何覆盖日常 90% 的按钮场景。

## 📚 系列导航

本系列共四篇：

1. [**设计令牌 vs 原子化 CSS（理念篇）**](./design-tokens-vs-atomic-css) —— 设计令牌优先的架构结论
2. [**CSS 优先 + 组件薄封装（架构篇）**](./css-first-component-library) —— 四层 CSS 架构与体积验证
3. [**Vue 3 简单组件开发实战（简单组件篇）**](./vue-component-api-design) —— Button 组件的 API 设计
4. [**Vue 3 复杂组件开发实战（复杂组件篇）**](./complex-component-api-design) —— Select/Pagination 的工业级细节

## 一、背景与参考

在之前的文章中，我们讨论了设计令牌优先于原子化 CSS 的理念，以及 CSS 优先 + 组件薄封装的架构。但有一个问题始终没有深入：**具体到单个组件，API 到底该怎么设计？**

设计初期，我深度参考了 Nuxt UI v4 的设计思路。Nuxt UI v4 将对复杂样式和交互的封装收束为 `variant`/`color`/`size` 几个核心维度，这正是我想要的——把复杂逻辑**内聚于组件内部**，对外只暴露最精简的 API。

下文以 Button 组件为例，一步步展示我的设计取舍和思考过程。**本文基于 v1.5.0 的实际实现**——经过多个版本迭代，Button 的 API 已经比初版更完善。

## 二、从需求出发：Button 需要什么？

一个按钮组件最基本的功能：

- 显示文字
- 点击触发事件
- 禁用状态
- 不同样式（主要、次要、危险等）

但只有这些够吗？我们看看实际使用场景：

```vue
<!-- 带图标的按钮 -->
<Button>
  <template #icon>🔍</template>
  搜索
</Button>

<!-- 加载状态 -->
<Button loading>提交中</Button>

<!-- 块级按钮（占满宽度） -->
<Button block>全宽按钮</Button>

<!-- 不同尺寸 -->
<Button size="sm">小号</Button>
```

经过分析，Button 组件在 v1.5.0 支持的需求：

| 需求             | 实现方式                                    |
| ---------------- | ------------------------------------------- |
| 文字内容         | 默认插槽 或 `label` prop                    |
| 点击事件         | `click` 事件                                |
| 禁用状态         | `disabled` prop                             |
| 加载状态         | `loading` prop                              |
| 加载时保留文字   | `showLabelWhileLoading` prop                |
| 加载时自定义文字 | `loadingLabel` prop + `#loading-label` 插槽 |
| 不同样式         | `variant` + `color`                         |
| 不同尺寸         | `size` prop                                 |
| 块级宽度         | `block` prop                                |
| 图标             | `icon` prop 或 `#icon` 插槽                 |
| 原生按钮类型     | `type` prop（button/submit/reset）          |

## 三、Props 设计：类型、默认值、优先级

### 3.1 基础 Props

```typescript
interface Props {
  label?: string // 按钮文字
  disabled?: boolean // 是否禁用
  loading?: boolean // 是否加载中
  block?: boolean // 是否为块级
  type?: "button" | "submit" | "reset" // 原生按钮类型
  showLabelWhileLoading?: boolean // 加载时是否保留文字
  loadingLabel?: string // 加载时的自定义文字
}

const props = withDefaults(defineProps<Props>(), {
  label: "",
  disabled: false,
  loading: false,
  block: false,
  type: "button", // 默认 button，防止表单意外提交
  showLabelWhileLoading: false,
})
```

**为什么默认 `type="button"`**？这是从实际项目踩坑中学到的重要决策。如果使用原生 `<button>` 的默认类型 `submit`，当按钮放在 `<form>` 里时，点击会意外提交表单。将其显式默认为 `button` 可避免绝大多数不期望的表单行为。

### 3.2 变体系统：variant + color

常见的按钮类型有：主要按钮、次要按钮、边框按钮、幽灵按钮。受 Nuxt UI v4 的 `variant` + `color` 设计启发，我选择将"视觉模式"与"语义颜色"完全解耦——`variant` 只控制 `filled` / `outline` 两种视觉模式，`color` 只控制 `primary` / `success` / `warning` / `error` 四种语义颜色。

为什么只保留 `filled` 和 `outline`，删除了 `ghost`？

| 变体      | 使用频率        | 是否保留                       |
| --------- | --------------- | ------------------------------ |
| `filled`  | 🔥🔥🔥🔥🔥 极高 | ✅ 保留                        |
| `outline` | 🔥🔥🔥🔥 高     | ✅ 保留                        |
| `ghost`   | 🔥 低           | ❌ 删除（可用 `outline` 替代） |

同样，颜色只保留 4 种：

| 颜色      | 使用频率        | 是否保留                       |
| --------- | --------------- | ------------------------------ |
| `primary` | 🔥🔥🔥🔥🔥 极高 | ✅ 保留                        |
| `success` | 🔥🔥🔥 中       | ✅ 保留                        |
| `warning` | 🔥 低           | ✅ 保留                        |
| `error`   | 🔥🔥 中         | ✅ 保留                        |
| `neutral` | 🔥 低           | ❌ 删除（可用 `outline` 替代） |

### 3.3 尺寸设计

尺寸只保留 `sm` / `md` / `lg` 三档。我删除了 `xs` 和 `xl`：极小尺寸可以用 Badge 或其他非按钮组件替代，而个人博客里几乎碰不到超大尺寸的场景。

默认尺寸设为 `sm`——常见的按钮默认高度约 32-34px，正好对应我们的 `sm`。

### 3.4 图标设计：prop 与插槽共存

初版设计时，我只提供 `#icon` 插槽，不提供 `icon` prop——理由是"保持单一职责"。但实际使用中发现两个问题：

1. **简单图标（如 `✓`、emoji）用插槽太啰嗦**：`<template #icon>✓</template>` 比 `icon="✓"` 多了 20 个字符
2. **图标库组件（如 lucide-vue-next 的 IconHome）用插槽不够直观**：需要用 `<component :is="IconHome" />` 包一层

经过多版本迭代，最终同时支持 `icon?: string | Component`（字符串或 Vue 组件）与 `#icon` 插槽，并建立明确的优先级：

```vue
<!-- 使用插槽（优先级更高，更灵活） -->
<Button>
  <template #icon>🔍</template>
  搜索
</Button>

<!-- 使用字符串 prop -->
<Button icon="✓" label="确认" />

<!-- 使用 Vue 组件 prop -->
<Button :icon="IconHome" label="首页" />
```

模板中的实现逻辑：

```vue
<span v-if="hasIconSlot || icon" class="mg-button-icon">
  <!-- 插槽优先于 prop -->
  <slot name="icon">
    <!-- prop 是 Vue 组件时渲染组件 -->
    <component :is="icon" v-if="typeof icon !== 'string'" />
    <!-- prop 是字符串时直接渲染文本 -->
    <span v-else-if="icon">{{ icon }}</span>
  </slot>
</span>
```

**设计原则：插槽优先于 prop**。`hasIconSlot` 检测是否传入 `#icon` 插槽，如果有则完全忽略 `icon` prop。这保证了灵活性——当用户需要自定义图标布局时，插槽总是能覆盖 prop 的默认行为。

> 设计考量：我合并为单个 `icon` prop（左侧图标——这是个人博客场景 90% 的需求），保留 `#icon` 插槽用于完全控制。

## 四、插槽设计：默认插槽 vs label prop

为了支持快速写法和自定义内容，同时提供 `label` prop 和默认插槽，模板中通过 `<slot>{{ label }}</slot>` 实现——有插槽内容时用插槽，否则回退到 `label`。

`hasLabel` 的判断逻辑有一个容易忽视的细节：

```typescript
const hasLabel = computed(() => props.label !== "" || !!slots.default)
```

注意这里是 `props.label !== ""` 而不是 `!!props.label`。为什么？

- `withDefaults` 会将 `undefined` 解析为默认值 `""`
- 当外部显式传入 `label=""` 时，**我们不渲染空 label 容器**——这是「纯图标按钮」的经典场景，空容器会导致图标无法垂直居中
- 但如果有默认插槽（无论插槽内容是否为空），仍然渲染容器

```vue
<!-- 纯图标按钮：label 为空，不渲染空 label 容器 -->
<Button icon="🔍" />

<!-- 带插槽内容：即使 label 为空也渲染 -->
<Button><template #icon>🔍</template>搜索</Button>
```

这解决了**纯图标按钮居中问题**——空的 `.mg-button-label` 会占据空间导致图标不居中。

## 五、状态处理

### 5.1 禁用状态与加载状态

`disabled` 和 `loading` 都会禁用按钮，但**只有 `disabled` 时 `click` 事件才完全阻止**（原生 disabled 属性）；`loading` 状态我们还希望保留正确的语义——用户知道按钮在"处理中"。

```typescript
const handleClick = (event: MouseEvent) => {
  if (props.disabled || props.loading) return
  emit("click", event)
}
```

模板中通过 `:disabled="disabled || loading"` 让两种状态都禁用按钮，但 click 事件仍由 `handleClick` 统一拦截——这保证了程序化调用（`.trigger('click')`）时也遵守禁用语义。

### 5.2 加载状态增强

初版的 `loading` 只显示旋转动画，隐藏图标和文字。v1.5.0 增加了两个增强：

- `showLabelWhileLoading`：加载时是否保留文字
- `loadingLabel`：加载时的自定义文字（默认复用 `label`）
- `#loading-label` 插槽：完全自定义加载文字（优先级最高）

```vue
<template v-if="loading">
  <span class="mg-button-loading-icon" />
  <!-- 根据开关决定是否显示 label -->
  <span v-if="showLabelWhileLoading" class="mg-button-label">
    <!-- 插槽 > loadingLabel prop > label -->
    <slot name="loading-label">{{ loadingLabel || label }}</slot>
  </span>
</template>
```

使用场景（三种递进的控制粒度）：

```vue
<!-- ① 默认：只显示加载动画 -->
<Button loading label="保存" />

<!-- ② 保留文字：提示用户"正在保存" -->
<Button loading :show-label-while-loading="true" label="保存" />

<!-- ③ 完全自定义：插槽覆盖一切 -->
<Button loading :show-label-while-loading="true">
  <template #loading-label>⏳ 拼命保存中...</template>
</Button>
```

纯 CSS 实现加载动画：

```css
.mg-button-loading-icon {
  width: 1rem;
  height: 1rem;
  border: 2px solid currentColor;
  border-top-color: transparent;
  border-radius: 50%;
  animation: mg-button-spin 0.6s linear infinite;
}
@keyframes mg-button-spin {
  to {
    transform: rotate(360deg);
  }
}
```

## 六、CSS 样式设计

### 6.1 基础样式

按钮使用 `inline-flex` 布局，内容水平和垂直居中，直角边框（`--ui-radius-none`），内边距和字体大小使用设计令牌。

```css
.mg-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--ui-spacing-sm);
  font-weight: 500;
  transition: all var(--ui-motion-duration-neural) ease;
  cursor: pointer;
  border-radius: var(--ui-radius-none);
  border: none;
  background: transparent;
  white-space: nowrap;
  padding: var(--ui-spacing-sm) var(--ui-spacing-md);
  font-size: var(--ui-typography-size-body);
}
```

### 6.2 尺寸变体

| 尺寸 | 内边距      | 字体大小                           |
| ---- | ----------- | ---------------------------------- |
| `sm` | `sm` / `md` | `--ui-typography-size-code` (13px) |
| `md` | `md` / `lg` | `--ui-typography-size-body` (15px) |
| `lg` | `lg` / `xl` | `1.125rem` (18px)                  |

### 6.3 颜色变体：hover/active 状态

v1.5.0 的颜色变体不仅定义了基础色，还完善了 hover/active 反馈。使用 `color-mix()` 在令牌颜色基础上混合黑色实现：

```css
.mg-button-filled-primary {
  background-color: var(--ui-primary);
  color: white;
}
.mg-button-filled-primary:hover:not(:disabled) {
  background-color: color-mix(in srgb, var(--ui-primary), black 10%);
}
.mg-button-filled-primary:active:not(:disabled) {
  background-color: color-mix(in srgb, var(--ui-primary), black 20%);
}

/* outline 变体：透明背景 + 边框 */
.mg-button-outline-primary {
  background-color: transparent;
  color: var(--ui-primary);
  border: 1px solid var(--ui-primary);
}
.mg-button-outline-primary:hover:not(:disabled) {
  background-color: color-mix(in srgb, var(--ui-primary), transparent 90%);
}
```

使用 `:not(:disabled)` 确保禁用状态不触发 hover 效果。

### 6.4 图标与文字容器

图标容器使用 `inline-flex` 并设置 `line-height: 0` 来消除行高影响，内部的 SVG 或 iconify 图标强制块级并设置宽高为 `1em`。配合 `:empty` 伪类隐藏空标签，修复只有图标时的居中问题：

```css
.mg-button-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  line-height: 0;
}
.mg-button-icon svg,
.mg-button-icon .iconify {
  display: block;
  width: 1em;
  height: 1em;
}

/* 空标签隐藏 - 修复只有图标时的居中问题 */
.mg-button-label:empty {
  display: none;
}
```

CSS 层的 `:empty` 伪类 + 组件层的 `hasLabel` 判断共同保证了纯图标按钮的正确居中。

## 七、无障碍设计

当前 Button 不添加 `aria-busy` 和 `aria-disabled` 属性。无障碍策略的核心是依赖原生语义：

- **`disabled` 原生属性**已经可以让屏幕阅读器正确读出禁用状态
- **加载状态**通过视觉反馈（旋转动画）表达，比额外的 ARIA 属性更直观
- 移除多余的 ARIA 属性，避免和原生语义重复

这不是"极简 ≠ 简陋"的妥协，而是**避免 ARIA 过度使用**——原生 HTML 语义（`<button disabled>`）本身就是最可靠的无障碍。项目真正的无障碍保障来自测试层：`a11y.test.ts` 使用 axe-core 对 12 个核心组件做自动化规范检查（`expectNoViolations`）。

## 八、属性透传

模板根元素通过 `v-bind="$attrs"` 透传原生属性（见 §九 完整代码），用户可以直接传入 `id`、`name`、`data-*`、`aria-*` 等属性：

```vue
<Button id="submit-btn" name="submit" data-testid="submit">
  提交
</Button>
```

配合 `defineOptions({ inheritAttrs: false })`，外部 class 会通过 `$attrs` 暴露给用户自行处理，同时组件内部的 class 绑定不会冲突。

## 九、最终代码（v1.5.0 实际实现）

```vue
<template>
  <button
    v-bind="$attrs"
    :type="type"
    class="mg-button"
    :class="[
      `mg-button-${variant}-${color}`,
      `mg-button-${size}`,
      { 'mg-button-block': block, 'mg-button-loading': loading },
    ]"
    :disabled="disabled || loading"
    @click="handleClick"
  >
    <!-- 加载状态 -->
    <template v-if="loading">
      <span class="mg-button-loading-icon" />
      <span v-if="showLabelWhileLoading" class="mg-button-label">
        <slot name="loading-label">{{ loadingLabel || label }}</slot>
      </span>
    </template>

    <!-- 正常状态 -->
    <template v-else>
      <span v-if="hasIconSlot || icon" class="mg-button-icon">
        <slot name="icon">
          <component :is="icon" v-if="typeof icon !== 'string'" />
          <span v-else-if="icon">{{ icon }}</span>
        </slot>
      </span>
      <span v-if="hasLabel" class="mg-button-label">
        <slot>{{ label }}</slot>
      </span>
    </template>
  </button>
</template>

<script setup lang="ts">
import { useSlots, computed } from "vue"
import type { Component } from "vue"
import type { Size, AddonColor } from "../types/components"

defineOptions({ name: "Button", inheritAttrs: false })

type Variant = "filled" | "outline"
type ButtonType = "button" | "submit" | "reset"

interface Props {
  label?: string
  variant?: Variant
  color?: AddonColor
  size?: Size
  type?: ButtonType
  disabled?: boolean
  loading?: boolean
  showLabelWhileLoading?: boolean
  loadingLabel?: string
  block?: boolean
  icon?: string | Component
}

const props = withDefaults(defineProps<Props>(), {
  label: "",
  variant: "filled",
  color: "primary",
  size: "sm",
  type: "button",
  disabled: false,
  loading: false,
  showLabelWhileLoading: false,
  block: false,
})

defineSlots<{
  default: () => any
  icon: () => any
  "loading-label": () => any
}>()

const slots = useSlots()
const hasIconSlot = computed(() => !!slots.icon)
const hasLabel = computed(() => props.label !== "" || !!slots.default)

const emit = defineEmits<{ click: [event: MouseEvent] }>()

const handleClick = (event: MouseEvent) => {
  if (props.disabled || props.loading) return
  emit("click", event)
}
</script>
```

## 十、设计取舍总结

回顾上面的设计过程，几个关键取舍：

- **`variant` 与 `color` 的解耦**：关注"视觉模式"与"语义颜色"分离
- **尺寸的精简**：3 种尺寸足够覆盖个人博客场景
- **加载状态的精细化**：`showLabelWhileLoading`/`loadingLabel` 是对"加载时文案"需求的响应
- **图标支持的灵活性**：prop 快速写法 + 插槽完全控制

## 十一、关于加载状态宽度变化的讨论

组件开发中有一个经典陷阱——**按钮加载时宽度变化导致布局偏移（CLS）**。

常见的解法是 `min-width` 预留 + 加载图标绝对定位。但在 v1.5.0 的实际实现中，我**没有采用这种方式**，原因是：

1. **`min-width: 88px` 是硬编码值**，会破坏"小型组件响应不同内容宽度"的灵活性
2. 绝对定位的加载图标在 `loading="true"` 时脱离文档流，如果按钮内有其他内容（如加载文字），布局仍可能变化
3. 更好的性能优化是**保证加载状态的文案长度接近正常状态**（`loadingLabel` 帮助用户保持文案一致）

如果你的场景确实对 CLS 要求严格（如电商下单按钮），可以在业务层手动添加：

```css
.my-order-button {
  min-width: 120px; /* 根据你实际按钮宽度预留 */
}
```

组件库层面保持灵活，用户按需优化。

## 十二、设计决策总结

| 决策                           | 原因                                       |
| ------------------------------ | ------------------------------------------ |
| 删除 `xs` 尺寸                 | 使用频率低，简化 API                       |
| 删除 `ghost` 变体              | 可用 `outline` 替代                        |
| 删除 `neutral` 颜色            | 可用 `outline` + 默认色替代                |
| 默认尺寸为 `sm`                | 主流 UI 库默认按钮约 32px                  |
| 默认 `type="button"`           | 防止表单意外提交                           |
| `icon` prop + `#icon` 插槽共存 | 快速写法 + 完全控制，插槽优先              |
| `label` prop + 默认插槽        | 两种写法都支持                             |
| `hasLabel = label !== ''`      | 纯图标按钮不渲染空 label 容器              |
| loading 增强                   | `showLabelWhileLoading` + `loadingLabel`   |
| `v-bind="$attrs"`              | 透传原生属性，保持灵活性                   |
| 不添加 aria-busy/aria-disabled | 依赖原生 disabled 语义 + axe-core 测试保障 |
| 不做 CLS 魔法                  | 保持灵活性，用户按需优化                   |

## 十三、测试保障

v1.5.0 的 Button 有 **20 个组件测试**，覆盖：

- 默认 props 渲染
- variant/color/size 变体 class
- loading 状态（自动禁用、隐藏文字、showLabelWhileLoading、loadingLabel、插槽）
- 纯图标按钮（label 为空不渲染容器）
- `icon` prop（字符串渲染文本、Component 渲染组件、插槽优先于 prop）
- disabled/loading 时 click 不触发
- 属性透传（id、data-\*）
- `type` prop 透传

以及 **axe-core 可访问性检查**（Button 无违规）和 **SSR 渲染检查**。

---

本篇以 Button 为例梳理了简单组件的设计要点。下一篇将深入复杂组件，涵盖数据适配、内部状态、逻辑复用等更高级的话题。

---

## 🌙 关于 Moongate Vue

本文来自 Moongate Vue 组件库设计实战系列（共 4 篇），所有内容均基于真实项目实践：

- **项目仓库**：[github.com/yuelinghuashu/moongate-vue](https://github.com/yuelinghuashu/moongate-vue) — 极简 Vue 3 组件库，零依赖、CSS 优先、25KB gzip
- **真实案例**：[moongate.top](https://moongate.top) — 个人博客，从 Nuxt UI v4 迁移至 Moongate Vue 构建
- **在线文档**：[vue.moongate.top](https://vue.moongate.top) — 组件 API 与主题定制指南
