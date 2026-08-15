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

本系列共五篇：

1. [**设计令牌 vs 原子化 CSS（理念篇）**](./design-tokens-vs-atomic-css) —— 设计令牌优先的架构结论
2. [**CSS 优先 + 组件薄封装（架构篇）**](./css-first-component-library) —— 四层 CSS 架构与体积验证
3. [**Vue 3 简单组件开发实战（简单组件篇）**](./vue-component-api-design) —— Button 组件的 API 设计
4. [**Vue 3 复杂组件开发实战（复杂组件篇）**](./complex-component-api-design) —— Select/Pagination 的工业级细节
5. [**从代码到 npm（发布篇）**](./component-library-publishing) —— 发布实战与避坑指南

## 一、起点：我有一套完整的设计令牌

我的个人项目 `moongate-vue` 的样式系统由三组文件构成：

- **`src/styles/tokens/colors.css`**：浅色/深色双主题，语义化颜色变量（`--ui-primary`、`--ui-bg-muted`……），以及布局令牌
- **`src/styles/tokens/layout.css`**：间距、圆角、动效时长、字体、断点、z-index 等布局令牌
- **`src/styles/index.css`**：全局样式入口，导入令牌 + 组件样式 + 极简工具类

这套令牌系统不依赖任何框架，任何组件都可以通过 `var(--ui-spacing-md)`、`var(--ui-primary)` 获取设计约束。它稳定、直观、可维护，是我整个组件库的**地基**。

关于变量规模，需要诚实说明：`colors.css` 浅色/深色模式各定义了 **68 个颜色变量**（远超初版的 40+）。其中一部分是组件库实际消费的 UI 令牌，另一部分是沿袭自 `moongate-theme` 项目的**编辑器主题扩展变量**——例如 `--ui-ansi-red`（ANSI 终端色）、`--ui-bracket1`（括号对色）、`--ui-git-added`（Git 状态色）、`--ui-debug-start`（调试断点色）等。

这些变量是我个人的编辑器主题项目（moongate-theme）所需的，虽然组件库自身不消费它们，但它们确实存在于令牌文件中——这是我通过"生成脚本自动产出完整令牌集"而非"只产出组件所需子集"的选择。**它们不是冗余代码，只是个人其他项目的设计资产共享。**

## 二、诱惑：UnoCSS 的轻量与原子化

我听过 Tailwind 的"笨重"名声，但 UnoCSS 号称按需生成、零运行时、超轻量。作为个人开发者，我渴望"不用写 CSS 类名"的体验——直接在模板里堆 `flex p-4 text-center`，不用切文件，不用想命名。

于是某个夜晚，我决定：把设计令牌"映射"到 UnoCSS 上，既保留设计系统，又享受原子化书写。

## 三、碰撞：一个晚上的挣扎（附真实配置）

我写了 `uno.config.ts`，试图把每个 `--ui-*` 变量映射成原子类。以下是我当时失败的部分配置：

```ts
// uno.config.ts（失败版本）
import { defineConfig } from "unocss"

export default defineConfig({
  theme: {
    colors: {
      primary: "var(--ui-primary)",
      success: "var(--ui-success)",
      warning: "var(--ui-warning)",
      error: "var(--ui-error)",
      "bg-muted": "var(--ui-bg-muted)",
      "border-subtle": "var(--ui-border-subtle)",
      // 需要映射 68 个变量，此处省略...
    },
    spacing: {
      xs: "var(--ui-spacing-xs)",
      sm: "var(--ui-spacing-sm)",
      md: "var(--ui-spacing-md)",
      lg: "var(--ui-spacing-lg)",
      xl: "var(--ui-spacing-xl)",
      "2xl": "var(--ui-spacing-2xl)",
      "3xl": "var(--ui-spacing-3xl)",
    },
    borderRadius: {
      none: "var(--ui-radius-none)",
      sm: "var(--ui-radius-sm)",
    },
  },
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
- **条件逻辑爆炸**：为了处理不同颜色变体，得写 `color === 'neutral' ? 'text-dim' : 'text-'+color`。
- **调试困难**：浏览器里看到 `bg-bg-muted`，得反向查找它对应哪个 CSS 变量。
- **IDE 无补全**：UnoCSS 的类型生成无法自动覆盖我的自定义变量名，导致开发时毫无提示。

当晚我就删掉了所有映射代码，回到了原始方案。

## 四、顿悟：设计令牌就是最轻量的原子化框架

看着 `bg-bg-muted`，我突然问自己：**为什么不直接写 `background-color: var(--ui-bg-muted)` 呢？**

我的设计令牌系统本身已经提供了所有设计约束。UnoCSS 宣称的"设计约束"能力，我的 CSS 变量全都有。它剩下的唯一价值就是"快速书写"——即语法糖。

更深一层：**把 `--ui-primary` 映射成 `bg-primary`，本质上是在"用 CSS 封装 CSS"**。UnoCSS 在编译阶段生成 `.bg-primary { background-color: var(--ui-primary); }` 这样的代码。既然最终都是这行 CSS，我直接在 scoped 样式里写它，不是更直接吗？

## 五、量化对比：纯设计令牌方案 vs. UnoCSS 映射方案

为了客观判断"不划算"到底多不划算，我整理了下表（基于我的项目实测，令牌数按实际 68 统计）：

| 指标                   | 纯设计令牌方案（最终采用）               | UnoCSS 映射方案（放弃）                      |
| ---------------------- | ---------------------------------------- | -------------------------------------------- |
| CSS 变量数量           | 68（浅/深各 68）                         | 68（不变）                                   |
| 额外配置文件行数       | 0                                        | ~200 行（`uno.config.ts`，含 68 个颜色映射） |
| 组件模板中类名长度     | 短（`mg-button`）                        | 长（`bg-primary text-white rounded`）        |
| 修改一个颜色需要改几处 | 1 处（CSS 变量定义）                     | 2 处（CSS 变量 + UnoCSS 映射）               |
| TypeScript 支持        | 原生 CSS 变量无提示                      | 可通过类型生成获得，但需额外配置             |
| 首屏 CSS 体积（gzip）  | ~4 KB（组件库实际消费）                  | ~2 KB（按需生成更小）                        |
| 调试体验               | 直接看到 `background: var(--ui-primary)` | 需要查找 `bg-primary` 映射到哪个变量         |
| 学习成本（新人）       | 低（只需理解 CSS 变量）                  | 中（需理解映射逻辑 + UnoCSS 规则）           |

**结论**：牺牲 ~2 KB 体积，换取维护成本的巨大降低。对于个人项目，**维护成本、语义清晰度、调试体验**比极致的体积优化更重要。

> 💡 关于"68 个变量"的说明：组件库实际消费的 UI 令牌大约一半，另一半是编辑器主题扩展变量（ANSI 色、括号对色等，用于个人主题项目共享）。UnoCSS 映射方案需要为全部 68 个变量维护映射，这恰恰放大了映射成本——而直接使用 CSS 变量则天然支持"只引用需要的变量"。

## 六、融合之道：务实的边界

我的经历并不证明原子化 CSS 不好，而是证明：**当你的项目已经拥有一套成熟的设计令牌时，强行把令牌映射成原子类是多余的**。

但这不等于要完全放弃原子化工具。我后来找到了合理的分工，关键是**区分"与数值无关的类"和"与数值相关的类"**：

| 样式类型                         | 推荐方案                                                                       | 理由                                                                                     |
| -------------------------------- | ------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------- |
| **布局**（flex、grid、position） | UnoCSS 原子类（`flex`、`grid`、`items-center`、`justify-between`、`relative`） | 不涉及数值，无需令牌，开箱即用                                                           |
| **间距**（padding、margin、gap） | **优先 scoped 样式 + `var(--ui-spacing-*)`**                                   | 保持主题一致性。如果非要用原子类，必须修改 UnoCSS 配置将数值映射到你的令牌（见下方案例） |
| **颜色、圆角、阴影、动效**       | 一律 scoped 样式 + CSS 变量                                                    | 语义化强，无映射成本，调试直观                                                           |
| **响应式变体**                   | 可使用 UnoCSS 的 `md:` 前缀，但只用于布局/间距类，不用于颜色                   | 简洁且不污染设计令牌                                                                     |

### 关于间距的特殊说明

UnoCSS 默认的 `p-4` 对应 `1rem`，而你的设计令牌中可能是 `--ui-spacing-md: 12px`。如果直接写 `p-4`，就会绕过设计系统。如果你真的想用原子类的间距，必须修改配置：

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
3. **警惕"魔法数字"**：避免在模板里写 `p-3.5` 或 `gap-11` 这种硬编码值，它们会破坏设计令牌的契约。如果你发现自己经常需要偏离令牌系统，说明你的令牌定义可能不够灵活，需要扩展而非绕过。
4. **对于响应式布局**，可以继续使用原子化工具的响应式变体（`md:flex`），但尽量只用于布局，不用于颜色/间距。
5. **如果原子化工具让你觉得"为了用而用"，完全可以不用**。原生 CSS + 设计令牌已经足够清晰、可维护。

## 九、后续演进：DTCG 与未来

值得一提的是，W3C 旗下的设计令牌社区组（DTCG）已在 2025 年底发布首个稳定规范，推动设计令牌成为跨平台通用语言。这意味着"以令牌为中心"的架构，正成为行业共识。

原子化工具可以消费设计令牌，但不应绑架设计令牌。将令牌硬编码成特定工具的原子类，相当于把自己的设计系统锁死在该工具的语法上。而直接使用 CSS 变量，则是框架无关、面向未来的选择。

未来，当 DTCG 工具链成熟后，可能会自动从设计令牌生成原子类，届时我会重新评估 UnoCSS。但目前（2026），手动映射仍是维护负担。

## 十、结论：设计令牌优先，原子化可选

最终，我的 `Button.vue` 回到了最原始的样子：

```css
/* src/styles/components/button.css */
.mg-button-filled-primary {
  background-color: var(--ui-primary);
  color: white;
}
.mg-button-filled-primary:hover:not(:disabled) {
  background-color: color-mix(in srgb, var(--ui-primary), black 10%);
}
```

简单、直接、语义化。这才是设计令牌该有的用法。

UnoCSS 和 Tailwind 是好工具，但它们不是设计系统的替代品。设计令牌才是地基，原子化只是上面的一层涂料。当你已经有一块坚实的地基时，是否涂上这层涂料，取决于你愿不愿意接受那点语法糖带来的维护成本。

至少对我来说，**不划算**。
