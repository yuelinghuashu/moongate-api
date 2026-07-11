---
title: Vue 3 简单组件开发实战：从 Button 组件看 API 设计
description: 以 Button 这一简单组件为例，深入探讨 Vue 3 组件库的 API 设计哲学，涵盖 Props 定义、变体系统、尺寸取舍、插槽设计、状态管理、无障碍支持及与主流 UI 库的对比，为后续复杂组件设计奠定基础，揭示极简 API 背后的设计权衡。
date: 2026-05-07
permalink: e6526a2f-4d33-4817-9ef0-1cbfb71ef3e7
series: moongate-vue
level: P3
tags:
  - Vue
  - Design System
  - Engineering
---

> 极简不是简陋，克制不是缺失——从 Nuxt UI v4 汲取灵感，如何设计一个好用的组件 API

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

## 一、背景与参考

在之前的文章中，我们讨论了设计令牌优先于原子化 CSS 的理念，以及 CSS 优先 + 组件薄封装的架构。但有一个问题始终没有深入：**具体到单个组件，API 到底该怎么设计？**

设计初期，我深度参考了 Nuxt UI v4 的设计思路。Nuxt UI v4 将对复杂样式和交互的封装收束为 `variant`/`color`/`size` 几个核心维度，这正是我想要的——把复杂逻辑**内聚于组件内部**，对外只暴露最精简的 API。

下文以 Button 组件为例，一步步展示我的设计取舍和思考过程。

本篇聚焦于简单组件的 API 设计，下一篇文章将探讨 Select、Pagination 等复杂组件的实现思路与状态管理。

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

经过分析，Button 组件需要支持：

| 需求     | 实现方式                 |
| -------- | ------------------------ |
| 文字内容 | 默认插槽 或 `label` prop |
| 点击事件 | `click` 事件             |
| 禁用状态 | `disabled` prop          |
| 加载状态 | `loading` prop           |
| 不同样式 | `variant` + `color`      |
| 不同尺寸 | `size` prop              |
| 块级宽度 | `block` prop             |
| 图标     | `#icon` 插槽             |

## 三、Props 设计：类型、默认值、优先级

### 3.1 基础 Props

```typescript
interface Props {
  label?: string; // 按钮文字
  disabled?: boolean; // 是否禁用
  loading?: boolean; // 是否加载中
  block?: boolean; // 是否为块级
}
```

默认值设计：

```typescript
const props = withDefaults(defineProps<Props>(), {
  label: "",
  disabled: false,
  loading: false,
  block: false,
});
```

### 3.2 变体系统：variant + color

常见的按钮类型有：主要按钮、次要按钮、边框按钮、幽灵按钮。受 Nuxt UI v4 的 `variant` + `color` 设计启发，我选择将“视觉模式”与“语义颜色”完全解耦。

```typescript
type Variant = "filled" | "outline";
type Color = "primary" | "success" | "warning" | "error";
```

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

```typescript
type Size = "sm" | "md" | "lg";
```

尺寸选项与 Nuxt UI v4 提供的五种尺寸（xs, sm, md, lg, xl）相比做了精简。我删除了 `xs` 和 `xl`，因为极小尺寸可以用 Badge 或其他非按钮组件替代，而个人博客里几乎碰不到超大尺寸的场景。

默认尺寸的选择：主流 UI 库（Naive UI、PrimeVue 等）的默认按钮高度约 32-34px，对应我们的 `sm`，因此默认尺寸设为 `sm`。

```typescript
const props = withDefaults(defineProps<Props>(), {
  size: "sm", // 默认小号
});
```

### 3.4 图标设计：只使用插槽

为了保持极简，Button 组件**不提供 `icon` prop，只保留 `#icon` 插槽**。用户需要图标时，直接在插槽中放入内容即可，例如：

```vue
<Button>
  <template #icon>🔍</template>
  搜索
</Button>
```

这样写虽然多几个字符，但避免了 prop 与插槽的优先级混乱，也更符合“单一职责”原则。同时，插槽可以传入任何内容（字符串、emoji、图标组件），灵活性完全不受影响。

> 设计考量：Nuxt UI v4 提供了 `icon` prop 和 `leading-icon`/`trailing-icon` 等多个图标相关属性，但在个人博客的场景下，绝大多数图标按钮仅为简单的“图标+文字”，使用插槽足以覆盖所有需求，且降低了学习和维护成本。

## 四、插槽设计：默认插槽 vs label prop

为了支持快速写法和自定义内容，同时提供 `label` prop 和默认插槽：

```vue
<span class="mg-button-label">
  <slot>{{ label }}</slot>
</span>
```

- 如果提供了默认插槽内容，显示插槽内容
- 否则显示 `label` prop

这样两种用法都支持：

```vue
<!-- 使用 label prop -->
<Button label="提交" />

<!-- 使用默认插槽 -->
<Button>提交</Button>
```

## 五、状态处理

### 5.1 禁用状态

`disabled` 和 `loading` 都会禁用按钮：

```typescript
const isDisabled = computed(() => props.disabled || props.loading);
```

```vue
<button :disabled="isDisabled">
```

### 5.2 加载状态

加载时显示旋转动画，隐藏图标和文字：

```vue
<template v-if="loading">
  <span class="mg-button-loading-icon" />
</template>
<template v-else>
  <span v-if="hasIconSlot" class="mg-button-icon">
    <slot name="icon" />
  </span>
  <span v-if="hasLabel" class="mg-button-label">
    <slot>{{ label }}</slot>
  </span>
</template>
```

纯 CSS 实现加载动画（与你提供的 CSS 一致）：

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

```css
.mg-button-sm {
  padding: var(--ui-spacing-sm) var(--ui-spacing-md);
  font-size: var(--ui-typography-size-code);
}
.mg-button-md {
  padding: var(--ui-spacing-md) var(--ui-spacing-lg);
  font-size: var(--ui-typography-size-body);
}
.mg-button-lg {
  padding: var(--ui-spacing-lg) var(--ui-spacing-xl);
  font-size: 1.125rem;
}
```

### 6.3 块级按钮

```css
.mg-button-block {
  width: 100%;
}
```

### 6.4 图标与文字容器

图标容器使用 `inline-flex` 并设置 `line-height: 0` 来消除行高影响，内部的 SVG 或 iconify 图标强制块级并设置宽高为 `1em`。

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
.mg-button-label {
  display: inline-flex;
  align-items: center;
}
```

### 6.5 解决纯图标按钮居中问题

当按钮只有图标没有文字时，空的 `.mg-button-label` 会占据空间导致图标不居中。通过 `:empty` 伪类隐藏空标签：

```css
.mg-button-label:empty {
  display: none;
}
```

同时在组件中判断是否有真实内容再渲染标签：

```typescript
const hasLabel = computed(() => !!props.label || !!slots.default);
```

```vue
<span v-if="hasLabel" class="mg-button-label">
  <slot>{{ label }}</slot>
</span>
```

### 6.6 变体样式

`filled` 和 `outline` 变体分别对应填充背景和透明背景加边框，颜色通过 `color` prop 动态组合，例如 `.mg-button-filled-primary` 使用 `--ui-primary` 作为背景色。

完整样式可见你提供的 CSS 文件，这里不再赘述。

## 七、无障碍设计

添加 ARIA 属性提升可访问性：

```vue
<button
  :aria-busy="loading"
  :aria-disabled="disabled || loading"
>
```

屏幕阅读器能正确读出按钮的加载和禁用状态。

## 八、属性透传

使用 `v-bind="$attrs"` 透传原生属性：

```vue
<button v-bind="$attrs">
```

用户可以直接传入 `id`、`name`、`data-*`、`aria-*` 等属性：

```vue
<Button id="submit-btn" name="submit" data-testid="submit">
  提交
</Button>
```

## 九、最终代码

```vue
<template>
  <button
    v-bind="$attrs"
    class="mg-button"
    :class="[
      `mg-button-${variant}-${color}`,
      `mg-button-${size}`,
      { 'mg-button-block': block, 'mg-button-loading': loading },
    ]"
    :disabled="disabled || loading"
    :aria-busy="loading"
    :aria-disabled="disabled || loading"
    @click="handleClick"
  >
    <template v-if="loading">
      <span class="mg-button-loading-icon" />
    </template>

    <template v-else>
      <span v-if="hasIconSlot" class="mg-button-icon">
        <slot name="icon" />
      </span>
      <span v-if="hasLabel" class="mg-button-label">
        <slot>{{ label }}</slot>
      </span>
    </template>
  </button>
</template>

<script setup lang="ts">
import { useSlots, computed } from "vue";

const slots = useSlots();
const hasIconSlot = computed(() => !!slots.icon);
const hasLabel = computed(() => !!props.label || !!slots.default);

type Variant = "filled" | "outline";
type Color = "primary" | "success" | "warning" | "error";
type Size = "sm" | "md" | "lg";

interface Props {
  label?: string;
  variant?: Variant;
  color?: Color;
  size?: Size;
  disabled?: boolean;
  loading?: boolean;
  block?: boolean;
}

const props = withDefaults(defineProps<Props>(), {
  label: "",
  variant: "filled",
  color: "primary",
  size: "sm",
  disabled: false,
  loading: false,
  block: false,
});

const emit = defineEmits<{ click: [event: MouseEvent] }>();

const handleClick = (event: MouseEvent) => {
  if (props.disabled || props.loading) return;
  emit("click", event);
};
</script>
```

## 十、与其他主流 UI 库的 API 对比

为了更直观地展示 Moongate UI Button 与当今主流、活跃的 UI 库在设计哲学上的异同，下表对比了 Nuxt UI v4、Naive UI 和 PrimeVue 的 API 风格。

| API 特性            | **Moongate UI (本章节)**   | **✨ Nuxt UI v4 (灵感之源)**                               | Naive UI (NButton)                          | PrimeVue (Button)                                                                    |
| :------------------ | :------------------------- | :--------------------------------------------------------- | :------------------------------------------ | :----------------------------------------------------------------------------------- |
| **核心风格**        | `variant` (filled/outline) | `variant` + `color` (solid/outline/soft/subtle/ghost/link) | `type` (primary/success/warning/error/info) | `severity` (primary/secondary/success/info/warning/danger/help/contrast) + `variant` |
| **尺寸**            | `size` (sm/md/lg)          | `size` (xs/sm/md/lg/xl)                                    | `size` (small/medium/large)                 | `size` (small/medium/large)                                                          |
| **禁用**            | `disabled`                 | `disabled`                                                 | `disabled`                                  | `disabled`                                                                           |
| **加载**            | `loading`                  | `loading` / `loadingAuto`                                  | `loading`                                   | `loading`                                                                            |
| **块级**            | `block`                    | `block`                                                    | `block`                                     | `fluid`                                                                              |
| **图标**            | `#icon` 插槽               | `icon` / `leading-icon` / `trailing-icon` + 插槽           | 无                                          | `icon` + `iconPos`                                                                   |
| **额外样式**        | 无                         | `square`                                                   | `dashed`, `circle`, `round`                 | `rounded`, `raised`, `outlined`                                                      |
| **Vue Router 集成** | 不支持 (由用户包装)        | 原生支持 (`to`/`href`)                                     | 不支持                                      | 通过 `as` 属性间接支持                                                               |

> 说明：表格中 **Naive UI** 和 **PrimeVue** 的 API 信息均来自其官方文档 (2026 年版本)。

通过这张表，可以清晰地看到：

- **`variant` 与 `color` 的解耦**：`variant` 专注“视觉模式”，`color` 专注“语义颜色”，逻辑更清晰。
- **尺寸的精简**：Nuxt UI 提供了 5 种尺寸选择，而在个人博客的场景下，3 种尺寸已经足够，删除 `xs` 和 `xl` 有助于简化用户选择。
- **图标支持的灵活性**：虽然 Moongate UI 不提供 `icon` prop，但通过 `#icon` 插槽可以实现同样的效果，且更加灵活。
- **语义化命名**：使用 `filled` 和 `outline` 来命名变体，比单纯的 `primary` 和 `secondary` 更准确地描述了视觉样式。

## 十一、补充细节：加载状态下的宽度变化陷阱

使用 `loading` 属性时，有一个常见但容易被忽视的问题——“按钮加载时宽度会变化”。当加载动画（如旋转图标）出现时，会改变按钮内部的子元素，通常导致按钮容器扩宽，造成布局晃动。这其实就是 **Cumulative Layout Shift (CLS)**。

一个优雅的解决方案是：使用 `min-width` 预留给文字加载前后的空间，同时将加载图标绝对定位，使其不参与布局。

```css
.mg-button {
  /* 1. 使用 min-width 预留足够空间，确保加载和正常状态宽度一致 */
  min-width: 88px;
}

.mg-button-loading .mg-button-label {
  opacity: 0;
}

.mg-button-loading-icon {
  /* 2. 绝对定位加载图标居中，不影响布局 */
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
}
```

这个优化可以有效防止页面布局偏移，提升用户体验。

## 十二、设计决策总结

| 决策                           | 原因                           |
| ------------------------------ | ------------------------------ |
| 删除 `xs` 尺寸                 | 使用频率低，简化 API           |
| 删除 `ghost` 变体              | 可用 `outline` 替代            |
| 删除 `neutral` 颜色            | 可用 `outline` + 默认色替代    |
| 默认尺寸为 `sm`                | 主流 UI 库默认按钮约 32px      |
| 只使用 `#icon` 插槽            | 避免 prop 与插槽混乱，保持简洁 |
| `label` prop + 默认插槽        | 两种写法都支持                 |
| 隐藏空标签                     | 解决纯图标按钮居中问题         |
| `v-bind="$attrs"`              | 透传原生属性，保持灵活性       |
| `min-width` + 绝对定位加载图标 | 防止加载状态布局偏移           |

本篇以 Button 为例梳理了简单组件的设计要点。下一篇将深入复杂组件，涵盖数据适配、内部状态、逻辑复用等更高级的话题。
