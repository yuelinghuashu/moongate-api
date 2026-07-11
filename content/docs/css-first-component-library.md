---
title: CSS 优先 + 组件薄封装：一个 10KB 组件库的极简实践
description: 从 UnoCSS 回归原生 CSS，一套设计令牌驱动的组件库方案。四层 CSS 架构、极简 Vue 组件、体积分析与维护性对比，展示如何用 500 行代码构建一个 10KB 的组件库。
date: 2026-04-19
permalink: 7b75c994-ce43-4f78-89e1-817c295f3f00
level: P3
series: moongate-vue
tags:
  - CSS
  - Vue
  - Design System
  - Engineering
---

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

## 回顾：第一篇文章的结论

在上一篇文章[《design-tokens-vs-atomic-css》](./design-tokens-vs-atomic-css.md)中，我分享了尝试用 UnoCSS 映射已有设计令牌的失败经历。核心结论是：

- **设计令牌是地基，原子化只是涂料**
- 强行映射只会增加维护成本，得不偿失
- 对于已有成熟设计令牌的项目，原子化 CSS 不是必需品

那么，**不用原子化 CSS，组件库应该怎么写？**

这篇文章给出答案。

## 最终架构：四层 CSS 文件

整个样式系统分为四个层级，职责清晰、层层依赖：

```text
┌─────────────────────────────────────────────────────────────┐
│ 设计令牌层（自动生成） │
│ ┌─────────────────────┐ ┌─────────────────────────────┐ │
│ │ colors.css │ │ layout.css │ │
│ │ 颜色令牌（40+变量） │ │ 布局令牌（间距/字体/动效） │ │
│ │ 【组件库的API层】 │ │ │ │
│ └──────────┬──────────┘ └─────────────┬───────────────┘ │
│ └──────────────┬──────────────┘ │
│ ↓ │
│ 组件样式层（手写） │
│ ┌─────────────────────────────────────────────────────┐ │
│ │ components/ │ │
│ │ 各组件独立样式文件（Button.css, Card.css...） │ │
│ │ 引用 var(--ui-\*) 令牌 │ │
│ └──────────────────────────┬──────────────────────────┘ │
│ ↓ │
│ 入口层（手写） │
│ ┌─────────────────────────────────────────────────────┐ │
│ │ main.css │ │
│ │ 导入令牌文件 + 组件样式 + 全局重置 + 极简工具类 │ │
│ └─────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

### 各文件/文件夹职责

| 文件/文件夹   | 职责                                          | 生成方式         |
| ------------- | --------------------------------------------- | ---------------- |
| `colors.css`  | 浅色/深色模式颜色令牌                         | 主题插件自动生成 |
| `layout.css`  | 间距、字体、动效等布局令牌                    | 主题插件自动生成 |
| `components/` | 各组件独立样式文件（Button.css, Card.css...） | 手写             |
| `main.css`    | 入口文件 + 全局重置 + 工具类                  | 手写             |

### 设计令牌即 API

在这种模式下，`colors.css` 不仅仅是样式，它更像是组件库的 **Configuration API**。用户通过修改这些 CSS 变量（如 `--ui-primary`、`--ui-spacing-md`），就能在不触碰任何 JS 逻辑的情况下，完成整套 UI 的换肤。这是设计令牌最核心的价值——**样式配置与代码逻辑彻底分离**。

### 工程红利：多框架复用

这种解耦意味着，如果明天我想把项目从 Vue 迁移到 React 或 Svelte，我只需要重写一遍 ~50 行的逻辑组件，而那套核心样式可以原地复用，无需任何改动。**这是“样式绑定逻辑”的原子化方案永远无法做到的。**

## 极简组件：Button.vue 为例

有了全局 CSS 类，Vue 组件只需要做三件事：

1. 组合正确的类名
2. 处理交互逻辑（click、disabled、loading）
3. 透传插槽

```vue
<script setup lang="ts">
import { useSlots, computed } from "vue";

const slots = useSlots();
const hasIconSlot = computed(() => !!slots.icon);

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

<template>
  <button
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
    <span v-if="loading" class="mg-button-loading-icon" />
    <span v-if="!loading && hasIconSlot" class="mg-button-icon">
      <slot name="icon" />
    </span>
    <span class="mg-button-label">
      <slot>{{ label }}</slot>
    </span>
  </button>
</template>
```

**组件特点**：

- 无 `<style>` 块，样式全部来自全局 CSS
- 只有 ~50 行代码，极简清晰
- 类型安全（TypeScript）
- 支持无障碍属性（`aria-busy`、`aria-disabled`），**极简并不代表简陋**
- 支持 7 种 props + 2 种插槽，覆盖日常使用

## 动态样式：CSS 变量局部覆盖

对于 Button 这种状态极多的组件，除了预设的 `filled-primary` 等组合类，还可以利用 CSS 变量局部覆盖的技巧：

```vue
<!-- 用户自定义颜色，无需修改组件源码 -->
<Button :style="{ '--btn-bg': '#ff6b6b' }" class="custom-button">
  自定义颜色
</Button>
```

```css
/* components/Button.css 中增加一行支持 */
.mg-button {
  background-color: var(--btn-bg, var(--ui-primary));
}
```

这样可以让组件更灵活，甚至处理用户传入的任意颜色，而不局限于预设的 4 种。

## 使用示例

```vue
<template>
  <!-- 基础用法 -->
  <Button label="默认按钮" />

  <!-- 带图标 -->
  <Button icon="🔍" label="搜索" />

  <!-- 加载状态（自动禁用点击） -->
  <Button loading label="提交中" />

  <!-- 块级按钮 -->
  <Button block label="全宽按钮" />

  <!-- 完整组合 -->
  <Button variant="outline" color="error" size="lg" block :loading="isDeleting">
    删除项目
  </Button>
</template>
```

## 体积与维护性分析

### 体积数据

| 类型                   | 原始大小   | Gzip 压缩后 |
| ---------------------- | ---------- | ----------- |
| CSS（令牌 + 组件样式） | ~15 KB     | ~4 KB       |
| JS（Button 组件）      | ~2 KB      | ~0.8 KB     |
| 其他组件（按需）       | ~10 KB     | ~3 KB       |
| **总计**               | **~27 KB** | **~8 KB**   |

### 维护性对比

| 维度           | 原子化方案（UnoCSS 映射）                | 本方案                                         |
| -------------- | ---------------------------------------- | ---------------------------------------------- |
| **CSS 体积**   | 按需生成，极小                           | ~4 KB (gzip)                                   |
| **维护成本**   | 需同步映射配置                           | 直接改 CSS                                     |
| **心智负担**   | 记忆数百个类名及其映射逻辑               | 只需 ~20 个组件类名                            |
| **可读性**     | 模板臃肿，难以一眼看出组件层级           | 模板极简，类名语义化清晰                       |
| **首屏渲染**   | 需等待 JS 注入样式                       | 纯 CSS，浏览器原生渲染                         |
| **运行环境**   | 需要 Node + PostCSS/Vite 插件 + 配置文件 | 只需浏览器支持 CSS Variables（全球 98%+ 环境） |
| **多框架复用** | 不可能                                   | 样式文件可跨框架                               |

## 微工具类：极简原子化

虽然放弃了完整的原子化框架，但常用的布局工具类仍然很有价值。我在 `main.css` 中手写了一套极简工具类：

```css
/* 布局 */
.flex {
  display: flex;
}
.inline-flex {
  display: inline-flex;
}
.items-center {
  align-items: center;
}
.justify-between {
  justify-content: space-between;
}

/* 间距（基于设计令牌） */
.gap-2 {
  gap: var(--ui-spacing-sm);
}
.p-4 {
  padding: var(--ui-spacing-lg);
}
.mx-auto {
  width: fit-content;
  margin-left: auto;
  margin-right: auto;
}

/* 尺寸 */
.w-full {
  width: 100%;
}

/* 响应式补丁（5 行解决 80% 移动端适配） */
@media (max-width: 768px) {
  .sm-hidden {
    display: none !important;
  }
  .sm-flex-col {
    flex-direction: column !important;
  }
}
```

**特点**：

- 只有最常用的 ~30 个类，按需添加
- 数值绑定设计令牌（`var(--ui-spacing-*)`），保持主题一致
- 响应式补丁仅 5 行，零依赖

### 命名自由度

由于这些工具类是手写的，你可以根据自己的项目偏好自由命名。如果你讨厌 Tailwind 风格，甚至可以叫它 `.mobile-stack` 而不是 `.sm-flex-col`。**这种命名自由度，也是原生 CSS 方案相较于原子化框架的魅力之一。**

## 总结

### 适用场景

- ✅ 已有成熟设计令牌的项目
- ✅ 个人博客、小型网站
- ✅ 追求长期可维护性的组件库
- ✅ 不希望引入复杂工具链的场景

### 不适用场景

- ❌ 从零开始、没有设计令牌的项目
- ❌ 需要动态主题切换的大型设计系统
- ❌ 需要极致定制化（如每个组件都要求不同样式）

### 核心收获

很多时候，我们引入复杂的工具链是为了缓解“写 CSS”的焦虑。但当你真正拥有一套由设计令牌驱动的底层系统时，你会发现 CSS 不再是杂乱无章的补丁，而是一场精确、优雅的逻辑拆解。

这套方案的总代码量不到 500 行（CSS + 组件），打包后不到 10KB，却完整支撑了一个组件库的核心功能。

**这 10KB 不仅是体积的缩减，更是思维的减负。**
