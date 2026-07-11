---
title: 当设计令牌遇上原子化CSS：一次整合失败的反思与融合之道
description: 当个人开发者尝试用 UnoCSS 映射已有设计令牌失败后，反思工具迷信，提出设计令牌优先于原子化 CSS 的架构观点，并探索两者融合的务实边界。
date: 2026-04-18
permalink: 65f7b804-7301-401d-8461-b04913b29333
series: moongate-vue
level: P3
tags:
  - CSS
  - Vue
  - Design System
  - Engineering
---

> 从 UnoCSS 映射设计令牌的失败经历出发，量化对比两种方案的维护成本，给出务实的分工边界

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

## 一、起点：我有一套完整的设计令牌

我的个人项目 `moongate-vue` 一开始只有三份 CSS 文件：

- **`colors.css`**：浅色/深色双主题，40+ 语义化颜色变量（`--ui-primary`、`--ui-bg-muted`……）
- **`layout.css`**：间距、圆角、阴影、动效、字体、断点等布局令牌
- **`main.css`**：全局组件样式、月晕协议、潮汐触发等核心契约

这套令牌系统不依赖任何框架，任何组件都可以通过 `var(--ui-spacing-md)`、`var(--ui-primary)` 获取设计约束。它稳定、直观、可维护，是我整个组件库的**地基**。

## 二、诱惑：UnoCSS 的轻量与原子化

我听过 Tailwind 的“笨重”名声，但 UnoCSS 号称按需生成、零运行时、超轻量。作为个人开发者，我渴望“不用写 CSS 类名”的体验——直接在模板里堆 `flex p-4 text-center`，不用切文件，不用想命名。

于是某个夜晚，我决定：把设计令牌“映射”到 UnoCSS 上，既保留设计系统，又享受原子化书写。

## 三、碰撞：一个晚上的挣扎（附真实配置）

我写了 `uno.config.ts`，试图把每个 `--ui-*` 变量映射成原子类。以下是我当时失败的部分配置：

```ts
// uno.config.ts（失败版本）
import { defineConfig } from 'unocss'

export default defineConfig({
  theme: {
    colors: {
      primary: 'var(--ui-primary)',
      success: 'var(--ui-success)',
      warning: 'var(--ui-warning)',
      error: 'var(--ui-error)',
      'bg-muted': 'var(--ui-bg-muted)',
      'border-subtle': 'var(--ui-border-subtle)',
      // 需要映射 40+ 个变量，此处省略...
    },
    spacing: {
      sm: 'var(--ui-spacing-sm)',
      md: 'var(--ui-spacing-md)',
      lg: 'var(--ui-spacing-lg)',
      xl: 'var(--ui-spacing-xl)',
    },
    borderRadius: {
      none: 'var(--ui-radius-none)',
      sm: 'var(--ui-radius-sm)',
      md: 'var(--ui-radius-md)',
      lg: 'var(--ui-radius-lg)',
    }
  },
  shortcuts: {
    'btn': 'px-4 py-2 rounded',
    'btn-primary': 'btn bg-primary text-white'
  }
})
```

然后在 `Button.vue` 里把原来的 scoped 样式全部替换成原子类：

```vue
<!-- 改造后的 Button.vue（失败尝试） -->
<button :class="cn('bg-primary text-white', 'bg-bg-muted', 'border-border-subtle')">
```

问题很快暴露：

- **类名冗余且不语义化**：`bg-bg-muted`、`border-border-subtle`，读起来像口吃。
- **映射维护成本高**：CSS 变量改一处，UnoCSS 配置也要改，单一真实源变成两个。
- **条件逻辑爆炸**：为了处理 `neutral` 颜色，得写 `color === 'neutral' ? 'text-dim' : 'text-'+color`。
- **调试困难**：浏览器里看到 `bg-bg-muted`，得反向查找它对应哪个 CSS 变量。
- **IDE 无补全**：UnoCSS 的类型生成无法自动覆盖我的自定义变量名，导致开发时毫无提示。

当晚我就删掉了所有映射代码，回到了原始方案。

## 四、顿悟：设计令牌就是最轻量的原子化框架

看着 `bg-bg-muted`，我突然问自己：**为什么不直接写 `background-color: var(--ui-bg-muted)` 呢？**

我的设计令牌系统本身已经提供了所有设计约束。UnoCSS 宣称的“设计约束”能力，我的 CSS 变量全都有。它剩下的唯一价值就是“快速书写”——即语法糖。

更深一层：**把 `--ui-primary` 映射成 `bg-primary`，本质上是在“用 CSS 封装 CSS”**。UnoCSS 在编译阶段生成 `.bg-primary { background-color: var(--ui-primary); }` 这样的代码。既然最终都是这行 CSS，我直接在 scoped 样式里写它，不是更直接吗？

## 五、量化对比：纯设计令牌方案 vs. UnoCSS 映射方案

为了客观判断“不划算”到底多不划算，我整理了下表（基于我的项目实测）：

| 指标 | 纯设计令牌方案（最终采用） | UnoCSS 映射方案（放弃） |
| ------ | -------------------------- |
| CSS 变量数量 | 40+ | 40+ （不变） |
| 额外配置文件行数 | 0 | ~150 行（`uno.config.ts`） |
| 组件模板中类名长度 | 短（`mg-button`） | 长（`bg-primary text-white rounded`） |
| 修改一个颜色需要改几处 | 1 处（CSS 变量定义） | 2 处（CSS 变量 + UnoCSS 映射） |
| TypeScript 支持 | 原生 CSS 变量无提示 | 可通过类型生成获得，但需额外配置 |
| 首屏 CSS 体积（gzip） | ~4 KB | ~2 KB（按需生成更小） |
| 调试体验 | 直接看到 `background: var(--ui-primary)` | 需要查找 `bg-primary` 映射到哪个变量 |
| 学习成本（新人） | 低（只需理解 CSS 变量） | 中（需理解映射逻辑 + UnoCSS 规则） |

**结论**：牺牲 ~2 KB 体积，换取维护成本的巨大降低。对于个人项目，**维护成本、语义清晰度、调试体验**比极致的体积优化更重要。

## 六、融合之道：务实的边界（重写版）

我的经历并不证明原子化 CSS 不好，而是证明：**当你的项目已经拥有一套成熟的设计令牌时，强行把令牌映射成原子类是多余的**。

但这不等于要完全放弃原子化工具。我后来找到了合理的分工，关键是**区分“与数值无关的类”和“与数值相关的类”**：

| 样式类型 | 推荐方案 | 理由 |
| ------ | ------ | ------ |
| **布局**（flex、grid、position） | UnoCSS 原子类（`flex`、`grid`、`items-center`、`justify-between`、`relative`） | 不涉及数值，无需令牌，开箱即用 |
| **间距**（padding、margin、gap） | **优先 scoped 样式 + `var(--ui-spacing-*)`** | 保持主题一致性。如果非要用原子类，必须修改 UnoCSS 配置将数值映射到你的令牌（见下方案例） |
| **颜色、圆角、阴影、动效** | 一律 scoped 样式 + CSS 变量 | 语义化强，无映射成本，调试直观 |
| **响应式变体** | 可使用 UnoCSS 的 `md:` 前缀，但只用于布局/间距类，不用于颜色 | 简洁且不污染设计令牌 |

### 关于间距的特殊说明

UnoCSS 默认的 `p-4` 对应 `1rem`，而你的设计令牌中可能是 `--ui-spacing-md: 0.75rem`。如果直接写 `p-4`，就会绕过设计系统。如果你真的想用原子类的间距，必须修改配置：

```ts
// uno.config.ts（仅当你想用原子间距时）
theme: {
  spacing: {
    sm: 'var(--ui-spacing-sm)',
    md: 'var(--ui-spacing-md)',
    lg: 'var(--ui-spacing-lg)',
  }
}
```

然后你就可以写 `p-sm`、`m-md`。但注意：这又回到了映射维护的问题——每次修改令牌值，都要同步更新 UnoCSS 配置。**我最终选择完全不使用原子间距类，只使用无数值的布局类**。

### 响应式布局的权衡

原子化 CSS 的 `md:p-4` 写法确实比在 scoped 样式里写媒体查询更简洁。对于复杂的响应式布局，我仍然会使用 UnoCSS 的响应式变体，但仅限于与设计令牌无关的通用属性（如 `flex`、`grid`、`padding` 的数值直接用 UnoCSS 的默认比例，不强行映射我的令牌）。这算是一种务实的妥协。

## 七、如果没有设计令牌，应该用原子化吗？

我并不是原子化 CSS 的反对者。如果以下条件满足，我会毫不犹豫选择 UnoCSS/Tailwind：

- ✅ 新项目、快速原型
- ✅ 设计系统尚未成熟，还在快速迭代
- ✅ 团队全员熟悉原子化语法
- ✅ 不需要跨框架复用样式

**不适用场景**：

- ❌ 已有成熟设计令牌且需要长期维护
- ❌ 需要样式文件跨 Vue/React/Svelte 复用
- ❌ 对 CSS 体积不敏感（现代原子化框架其实很小，这不是主要矛盾）

## 八、给同样处境的开发者建议

如果你和我一样，已经有一套完整的设计令牌系统（CSS 变量、Design Tokens），但想尝试原子化 CSS，我的建议是：

1. **不要映射颜色、主题间距、圆角等核心令牌**。这些应该直接通过 `var(--ui-*)` 在 scoped 样式中使用。
2. **只使用原子化工具提供的通用布局类**（`flex`, `grid`, `items-center`, `relative`……）。这些与你的设计系统无关，且无需任何映射。
3. **警惕“魔法数字”**：避免在模板里写 `p-3.5` 或 `gap-11` 这种硬编码值，它们会破坏设计令牌的契约。如果你发现自己经常需要偏离令牌系统，说明你的令牌定义可能不够灵活，需要扩展而非绕过。
4. **对于响应式布局**，可以继续使用原子化工具的响应式变体（`md:flex`），但尽量只用于布局，不用于颜色/间距。
5. **如果原子化工具让你觉得“为了用而用”，完全可以不用**。原生 CSS + 设计令牌已经足够清晰、可维护。

## 九、后续演进：DTCG 与未来

值得一提的是，W3C 旗下的设计令牌社区组（DTCG）已在 2025 年底发布首个稳定规范，推动设计令牌成为跨平台通用语言。这意味着“以令牌为中心”的架构，正成为行业共识。

原子化工具可以消费设计令牌，但不应绑架设计令牌。将令牌硬编码成特定工具的原子类，相当于把自己的设计系统锁死在该工具的语法上。而直接使用 CSS 变量，则是框架无关、面向未来的选择。

未来，当 DTCG 工具链成熟后，可能会自动从设计令牌生成原子类，届时我会重新评估 UnoCSS。但目前（2026），手动映射仍是维护负担。

## 十、结论：设计令牌优先，原子化可选

最终，我的 `Button.vue` 回到了最原始的样子：

```vue
<style scoped>
.mg-button {
  background-color: var(--ui-primary);
  color: white;
  padding: var(--ui-spacing-md) var(--ui-spacing-lg);
}
</style>
```

简单、直接、语义化。这才是设计令牌该有的用法。

UnoCSS 和 Tailwind 是好工具，但它们不是设计系统的替代品。设计令牌才是地基，原子化只是上面的一层涂料。当你已经有一块坚实的地基时，是否涂上这层涂料，取决于你愿不愿意接受那点语法糖带来的维护成本。

至少对我来说，**不划算**。
