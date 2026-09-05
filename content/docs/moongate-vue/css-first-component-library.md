---
title: CSS 优先 + 组件薄封装：一个 25KB 组件库的极简实践
description: 设计令牌驱动的 Vue 3 组件库架构实录。四层 CSS 架构、极简 Vue 组件、Vite 多入口构建、体积预算自动化验证，展示如何保持组件库在 25KB (gzip) 内的工程实践。
date: 2026-04-19
level: P3
series: moongate-vue
tags:
  - CSS
  - Vue
  - Design System
  - Engineering
---

> 四层 CSS 架构把"样式"从"组件"中彻底解耦：设计令牌是 API，组件只做组合。这篇讲架构怎么落地，以及 25KB 体积是怎么守住的。

## 📚 系列导航

本系列共四篇：

1. [**设计令牌 vs 原子化 CSS（理念篇）**](./design-tokens-vs-atomic-css) —— 设计令牌优先的架构结论
2. [**CSS 优先 + 组件薄封装（架构篇）**](./css-first-component-library) —— 四层 CSS 架构与体积验证
3. [**Vue 3 简单组件开发实战（简单组件篇）**](./vue-component-api-design) —— Button 组件的 API 设计
4. [**Vue 3 复杂组件开发实战（复杂组件篇）**](./complex-component-api-design) —— Select/Pagination 的工业级细节

## 回顾：第一篇文章的结论

在上一篇文章[《design-tokens-vs-atomic-css》](./design-tokens-vs-atomic-css)中，我分享了尝试用 UnoCSS 映射已有设计令牌的失败经历。核心结论是：

- **设计令牌是地基，原子化只是涂料**
- 强行映射只会增加维护成本，得不偿失
- 对于已有成熟设计令牌的项目，原子化 CSS 不是必需品

那么，**不用原子化 CSS，组件库应该怎么写？**

这篇文章给出答案——以及 v1.5.0 在初版方案之上的工程进化。

## 最终架构：四层 CSS 架构

整个样式系统分为多个层级，职责清晰、层层依赖：

```text
设计令牌层（自动生成）          ← 组件库的核心 API 层
├─ tokens/colors.css            颜色令牌（浅/深各 68 个变量）
├─ tokens/layout.css            间距 / 字体 / 动效 / z-index 令牌
│
↓ 组件通过 var(--ui-*) 引用
│
组件样式层（手写）
├─ components/                  各组件独立样式文件
│  （Button.css, Card.css, ... 共 20+ 文件）
│
↓ 引用工具类
│
工具层（手写）
├─ utilities/                   极简语义工具类（颜色 / 文本 / 契约变量）
│
↓ 统一入口
│
入口层（手写）
├─ index.css                    导入令牌 + 组件样式 + 工具类
│
reset.css（可选，独立导出，不属于层级链）
```

### 各文件/文件夹职责

| 文件/文件夹         | 职责                                  | 生成方式           |
| ------------------- | ------------------------------------- | ------------------ |
| `tokens/colors.css` | 浅色/深色模式颜色令牌（各 68 个变量） | 主题脚本自动生成   |
| `tokens/layout.css` | 间距、字体、动效、断点、z-index 令牌  | 主题脚本自动生成   |
| `components/`       | 各组件独立样式文件（Button.css 等）   | 手写               |
| `utilities/`        | 极简工具类                            | 手写               |
| `reset.css`         | 可选全局重置（box-sizing），独立导出  | 手写，不属于层级链 |
| `index.css`         | 总入口，导入令牌 + 组件 + 工具        | 手写               |

### 设计令牌即 API

在这种模式下，`colors.css` 不仅仅是样式，它更像是组件库的 **Configuration API**。用户通过修改这些 CSS 变量（如 `--ui-primary`、`--ui-spacing-md`），就能在不触碰任何 JS 逻辑的情况下，完成整套 UI 的换肤。这是设计令牌最核心的价值——**样式配置与代码逻辑彻底分离**。

### 工程红利：多框架复用

这种解耦意味着，如果明天我想把项目从 Vue 迁移到 React 或 Svelte，我只需要重写一遍 ~50 行的逻辑组件，而那套核心样式可以原地复用，无需任何改动。**这是"样式绑定逻辑"的原子化方案永远无法做到的。**

## 极简组件：Button.vue 为例

有了全局 CSS 类，Vue 组件只需要做三件事：

1. 组合正确的类名
2. 处理交互逻辑（click、disabled、loading）
3. 透传插槽

以 v1.5.0 的实际代码为例，模板核心只有三部分（完整代码见[第 3 篇 §九](./vue-component-api-design)）：

```vue
<!-- Button.vue 核心：类名组合 + 状态 + 透传 -->
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
  <!-- 图标 / 文字 / 加载状态插槽，见第 3 篇完整代码 -->
</button>
```

### 组件特点

- 无 `<style>` 块，样式全部来自全局 CSS
- 完整实现约 110 行，极简清晰（见[第 3 篇 §九](./vue-component-api-design)）
- 类型安全（TypeScript），共享类型从 `src/types/components.ts` 导入
- 支持 11 种 props + 3 种插槽，覆盖日常场景
- `v-bind="$attrs"` 透传原生属性

## 构建架构：Vite 多入口 + 独立导出

初版组件库只有一个主入口。但随着组件增多，需要支持**按需引入**——用户只想用 Button 时不应加载全部组件。

v1.5.0 采用 Vite library mode 的多入口构建：

```ts
// vite.config.ts（简化）
import { componentNames } from "./scripts/component-list.js"

// 每个组件独立入口（src/exports/<Name>.ts → dist/<kebab>.js）
const componentEntries = Object.fromEntries(
  componentNames.map((name) => {
    const kebab = name.replace(/([a-z])([A-Z])/g, "$1-$2").toLowerCase()
    return [`${kebab}`, resolve(__dirname, `src/exports/${name}.ts`)]
  }),
)

export default defineConfig({
  build: {
    lib: {
      entry: {
        index: resolve(__dirname, "src/index.ts"),
        ...componentEntries, // 27 个组件 + 主入口
      },
      formats: ["es"], // 纯 ES Module，无 CJS
    },
    rollupOptions: {
      external: ["vue"], // Vue 作为 peerDependency
      output: {
        assetFileNames: "style.css", // CSS 统一输出
      },
    },
    cssCodeSplit: false,
  },
})
```

对应的 `package.json` 导出映射：

```json
{
  "main": "./dist/index.js",
  "module": "./dist/index.js",
  "types": "./dist/index.d.ts",
  "exports": {
    ".": {
      "types": "./dist/index.d.ts",
      "import": "./dist/index.js",
      "default": "./dist/index.js"
    },
    "./style.css": "./dist/style.css",
    "./reset.css": "./dist/reset.css",
    "./button": {
      "types": "./dist/exports/Button.d.ts",
      "import": "./dist/button.js"
    },
    "./badge": { "...": "..." }
  }
}
```

用户既可以使用全量引入 `import { Button } from 'moongate-vue'`，也可以按需 `import Button from 'moongate-vue/button'`。

## 体积控制：25KB 预算 + 自动化验证

体积是组件库的生命线。为了**不让体积悄悄失控**，我在 `pnpm build` 后自动执行 `scripts/tree-shake-check.js`：

- 使用 Vite JS API 将 `src/index.ts` 打包为单个 ESM bundle（minify）
- 统计 JS + CSS 的 gzip 体积
- 如果超过 **25KB 预算**，build 会在 CI 中断言失败

```bash
# 构建后自动输出（简化示例）
📦 完整库 Min+Gzip：
  ✅ 完整库: 32.50 KB (gzipped 24.80 KB)
      ├─ JS:    22.00 KB (gzipped 9.20 KB)
      └─ CSS:   10.50 KB (gzipped 5.60 KB)

✅ 完整库 Min+Gzip 在 25KB 预算内
```

这个"预算"与文化有关：我用「25KB gzip 完整组件库」作为设计挑战来对抗组件库普遍臃肿的现状。

### 为什么能这么小？

1. **零运行时依赖**：peerDependencies 只有 `vue`，没有 lodash、async-validator 等
2. **CSS 变量代替 JS 主题系统**：主题切换不需要 JS 集成
3. **组件薄封装**：逻辑极简，组合式函数复用
4. **极少的运行时 JS**：组合式函数复用 + 无运行时依赖

## 微工具类：极简语义工具类

`utilities/` 中保留了一套极简的**语义工具类**，直接引用设计令牌：

```css
/* 语义颜色工具类 */
.text-primary {
  color: var(--ui-primary);
}
.text-muted {
  color: var(--ui-text-muted);
}
.bg-primary {
  background-color: var(--ui-primary);
}
.bg-muted {
  background-color: var(--ui-bg-muted);
}

/* 核心契约 */
:root {
  --ui-radius: 0px;
  --ui-glow-alpha: var(--ui-physics-glow-alpha-dawn);
}
```

### 特点

- 只有最常用的 ~20 个类，按需添加
- 数值绑定设计令牌（`var(--ui-*)`），保持主题一致
- 通过 `--ui-radius`/`--ui-glow-alpha` 契约变量为全局提供样式锚点
- 附带 `.mg-lunar-halo`（月晕阴影效果）等设计系统特有的工具类
- 布局需求在组件内部通过 scoped 样式解决，工具层不承担布局职责

## 非侵入式样式

组件库在 v1.5.0 明确了**样式非侵入原则**：

- `style.css` 只包含组件样式，**不会重置你的全局样式**
- 可选引入 `moongate-vue/reset.css` 统一 `box-sizing: border-box`

```js
// 只引入组件样式
import "moongate-vue/style.css"

// 或额外引入全局重置（可选）
import "moongate-vue/reset.css"
```

## 体积与维护性分析

### 体积数据（v1.5.0 实测口径）

| 类型                   | 原始大小     | Gzip 压缩后  |
| ---------------------- | ------------ | ------------ |
| CSS（令牌 + 组件样式） | ~10.5 KB     | ~5.6 KB      |
| JS（完整组件库）       | ~22 KB       | ~9.2 KB      |
| **总计**               | **~32.5 KB** | **~24.8 KB** |

（实际构建产物以 `pnpm build` 后的 `size-report.json` 为准）

### 维护性对比

<details>
<summary>📊 完整对比（点击展开）</summary>

| 维度           | 原子化方案（UnoCSS 映射）                | 本方案（CSS 变量 + 薄封装）               |
| -------------- | ---------------------------------------- | ----------------------------------------- |
| **CSS 体积**   | 按需生成，极小                           | ~5.6 KB (gzip)                            |
| **维护成本**   | 需同步映射配置                           | 直接改 CSS                                |
| **心智负担**   | 记忆数百个类名及其映射逻辑               | 只需 ~20 个组件类名                       |
| **可读性**     | 模板臃肿，难以一眼看出组件层级           | 模板极简，类名语义化清晰                  |
| **首屏渲染**   | 需等待 JS 注入样式                       | 纯 CSS，浏览器原生渲染                    |
| **运行环境**   | 需要 Node + PostCSS/Vite 插件 + 配置文件 | 只需浏览器支持 CSS Variables（98%+ 环境） |
| **多框架复用** | 不可能                                   | 样式文件可跨框架                          |
| **按需引入**   | -                                        | 27 个独立导出入口（v1.5.0）               |
| **体积预算**   | -                                        | 25KB gzip 强制验证（CI 中断）             |

</details>

#### 核心差异一句话

原子化方案赢在体积，本方案赢在维护成本、可读性和多框架复用——对组件库来说，后者更重要。

## 总结

### 适用场景

- ✅ 已有成熟设计令牌的项目
- ✅ 追求极致体积（gzip < 25KB）的组件库
- ✅ 需要按需引入的构建场景（Vite multi-entry）
- ✅ 不希望引入复杂工具链的场景

### 不适用场景

- ❌ 从零开始、没有设计令牌的项目
- ❌ 需要动态主题切换的大型设计系统（需 JS 主题引擎）
- ❌ 需要大量业务组件（DatePicker、Tree 等）

### 核心收获

初版的 10KB 承诺在 v1.5.0 经过多轮功能迭代（全局 i18n 文案、可搜索 Select、多选、无障碍增强、450 测试），依然保持 **25KB gzip 预算内**——这不是偶然，而是通过**架构纪律**（零依赖 + 薄封装）和**自动化**（tree-shake-check.js 体积门禁）共同守住的。

**这 25KB 不仅是体积的缩减，更是思维的减负。**

---

## 🌙 关于 Moongate Vue

本文来自 Moongate Vue 组件库设计实战系列（共 4 篇），所有内容均基于真实项目实践：

- **项目仓库**：[github.com/yuelinghuashu/moongate-vue](https://github.com/yuelinghuashu/moongate-vue) — 极简 Vue 3 组件库，零依赖、CSS 优先、25KB gzip
- **真实案例**：[moongate.top](https://moongate.top) — 个人博客，从 Nuxt UI v4 迁移至 Moongate Vue 构建
- **在线文档**：[vue.moongate.top](https://vue.moongate.top) — 组件 API 与主题定制指南
