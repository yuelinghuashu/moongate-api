---
title: 构建体系：可测试、可验证的工程实践
description: 从单体脚本到模块化工程体系——ESM 模块拆分、WCAG 对比度自动校验、scope 自动验证、自动化测试与多格式产物生成，让构建脚本自身成为一套可信赖的工程基础设施。
date: 2026-08-06 06:00:00
permalink: ef9e2761-be41-4778-8552-22cb86ed3407
level: P4
series: design-system
tags:
  - VSCode
  - Theme
  - Design System
  - Engineering
  - CI/CD
---

## 📚 系列导航

本系列共六篇：

1. [**VS Code 主题：从手写 JSON 到可发布**](./create-vscode-theme-basics) —— 手写最小主题 JSON 与发布流程
2. [**主题工程化：从单体 JSON 到模块化 YAML**](./create-vscode-theme-engineering) —— 单体 JSON 重构为模块化 YAML
3. [**设计系统：DTCG 三层架构与昼夜双变体**](./create-vscode-theme-design-system) —— DTCG 令牌管理与昼夜双变体
4. [**构建体系：可测试、可验证的工程实践**](./create-vscode-theme-build-system) —— 模块化构建与自动化验证
5. [**品牌生态：设计哲学与视觉契约**](./create-vscode-theme-brand-ecosystem) —— 设计哲学与品牌生态
6. [**Nuxt + Shiki 主题集成（实战篇）**](./nuxt-shiki-vscode-theme-ssr-dual-themes) —— SSR 双主题高亮与闪动解决

---

在[设计系统](./create-vscode-theme-design-system)中，我们已经拥有了一套基于 DTCG 三层架构的主题生产系统，构建脚本能自动生成深色/浅色双主题，并能导出 CSS 变量。

但随着语言数量增长到 15 种、构建脚本功能越来越复杂，新的问题浮现了：

1. **构建脚本本身缺乏架构**——所有逻辑堆在 `build.js` 一个文件里，难以测试和维护。
2. **质量无法自动保证**——颜色是否符合 WCAG 对比度标准？引用的变量是否都存在？有没有产生「组件层直接引用原始值」的架构污染？
3. **语言 scope 无法验证**——写了 20 条语言规则，怎么确认每个 scope 真的存在于 VS Code 的 TextMate 语法中？「规则写了对不上」如何自动化检测？
4. **产物只有主题 JSON 和 CSS**——能否进一步导出 SCSS、TypeScript 等更多格式，让设计资产在更多平台复用？

本篇将回答这些问题，把构建脚本从「能用」升级为「工业级」——**一套可测试、可验证、可维护的工程基础设施**。

> 🗺️ **本篇路线图**：五个步骤，层层递进——
>
> 1. **架构**：把单体脚本拆成可维护的模块（`一`）
> 2. **验证**：让构建过程自我证明——质量校验与 scope 验证（`二`、`四`）
> 3. **优化**：让生成的产物更精简（`三`）
> 4. **测试**：让构建系统自身可回归验证（`五`）
> 5. **发布**：把验证串进生态整合与交付链路（`六`、`七`、`八`）
>
> 篇幅较长，建议先通读一遍建立全局认知，再结合自己的项目逐节实践——每节结尾都有与下一节的衔接说明。

> 💡 **提示**：本篇内容涉及较深的工程实践，建议先完成前三篇再阅读。如果你希望先了解「这套体系支撑起什么样的品牌生态」，也可以先读第五篇再回来。

---

## 📁 一、模块化架构：从单体脚本到 `scripts/lib/`

当构建脚本超过 200 行时，它本身也需要架构。Moongate 将构建系统拆分为 `scripts/lib/` 下的多个单职责模块：

```text
your-theme/
├── scripts/
│   ├── build.js                     # 主流程（编排者，不包含具体实现）
│   ├── verify-scopes.js             # scope 验证 CLI
│   ├── generate-better-comments.js  # Better Comments 配置生成器
│   └── lib/
│       ├── config.js                # 路径配置
│       ├── tokens.js                # 令牌解析（resolveTokens、replaceVariables）
│       ├── utils.js                 # 通用工具（safeLoadYaml、normalizeHex 等）
│       ├── validators.js            # 质量验证（WCAG 对比度、结构验证、未使用令牌检测）
│       ├── optimizers.js            # 输出优化（token 合并、语义色精简）
│       ├── generators.js            # 多格式产物生成（CSS/SCSS/TS/设计文档）
│       └── scope-validator.js       # scope 验证逻辑
├── src/
│   ├── core/
│   │   ├── primitives/colors.yaml
│   │   ├── semantics/dark.yaml + light.yaml
│   │   └── layout.yaml
│   ├── languages/*.yaml
│   ├── workbench.yaml
│   ├── semantic.yaml
│   └── special/better-comments.yaml
├── themes/                          # 生成产物
├── docs/DESIGN_SYSTEM.md            # 自动生成的文档
├── test/*.test.js                   # 自动化测试
└── package.json
```

各模块的职责：

| 模块                 | 职责                 | 关键导出                                                                                                                   |
| -------------------- | -------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| `config.js`          | 集中管理所有文件路径 | `PATHS`、`ROOT_DIR`                                                                                                        |
| `tokens.js`          | 令牌解析与变量替换   | `resolveTokens`（`{token}` 层间引用）、`replaceVariables`（`${var}` 变量替换）、`detectPrimitiveReference`（架构污染检测） |
| `utils.js`           | 通用工具函数         | `safeLoadYaml`、`normalizeHex`、`detectDuplicateColors`、`getThemeInfo`                                                    |
| `validators.js`      | 质量验证             | `checkContrast`（WCAG）、`validateThemeStructure`、`detectUnusedPrimitives`                                                |
| `optimizers.js`      | 输出精简             | `mergeTokenColors`、`optimizeSemanticTokenColors`                                                                          |
| `generators.js`      | 多格式产物           | `generateColorCss`、`generateLayoutCss`、`generateScssTokens`、`generateTsTokens`、`generateDesignSystemDoc`               |
| `scope-validator.js` | scope 验证逻辑       | `verifyAllScopes`、`formatVerificationResult`                                                                              |

**为什么用 ESM 而不是 CommonJS？**

- 现代 Node.js（≥ 14）原生支持 ESM，无需额外构建工具。
- `import` / `export` 是静态的，工具可以静态分析依赖关系，更容易重构和调试。
- 项目 `package.json` 中声明 `"type": "module"`，所有 `.js` 文件默认视为 ESM。

`build.js` 作为主流程，只负责**编排**——加载模块、调用函数、捕获错误，不包含具体实现。

> 📌 **与上一篇的分工**：`resolveTokens`、`replaceVariables` 的完整实现在[设计系统](./create-vscode-theme-design-system)中已经见过，本篇**不再重复**。你要关注的是四个**新增**模块：`validators`（质量验证）、`optimizers`（输出精简）、`generators`（多格式产物）、`scope-validator`（scope 验证）——数据流地图里的每一步，都会落到这四个模块上。

在阅读下面的代码之前，先建立一张「数据流地图」——每个关键步骤输入什么、输出什么：

| 步骤                                                   | 输入                               | 输出                           |
| ------------------------------------------------------ | ---------------------------------- | ------------------------------ |
| `loadPrimitives()`                                     | `primitives/colors.yaml`           | 标准化后的原始色值字典         |
| `resolveTokens()`                                      | 原始值 + `semantics/*.yaml`        | 每个变体的最终语义色值字典     |
| `loadTokenColors()`                                    | `languages/` + `special/` 下的规则 | 合并后的 tokenColors 原始规则  |
| `replaceVariables()`                                   | 规则文件 + 语义色值字典            | 变量已替换为实际色值的规则     |
| `mergeTokenColors()` / `optimizeSemanticTokenColors()` | 替换后的规则                       | 精简后的产物                   |
| `generate*()`                                          | 最终色值字典 + 布局令牌            | CSS / SCSS / TS 令牌与设计文档 |

带着这张地图读代码，你会更清楚每个模块函数在整条流水线中的位置。

```javascript
// scripts/build.js（主流程精简示意）
import { PATHS } from "./lib/config.js"
import { ensureFileExists, safeLoadYaml } from "./lib/utils.js"
import { resolveTokens, replaceVariables } from "./lib/tokens.js"
import {
  detectUnusedPrimitives,
  validateThemeStructure,
  checkContrast,
} from "./lib/validators.js"
import {
  mergeTokenColors,
  optimizeSemanticTokenColors,
} from "./lib/optimizers.js"
import {
  generateColorCss,
  generateLayoutCss,
  generateDesignSystemDoc,
  generateScssTokens,
  generateTsTokens,
} from "./lib/generators.js"

function main() {
  console.log("🚀 开始构建主题 (DTCG 标准 + 工业级质检)...\n")
  try {
    // 1. 检查必要文件
    // 2. 加载并标准化原始色值
    // 3. 生成布局 CSS
    // 4. 加载公共规则（workbench + semantic）
    // 5. 加载语言与特殊规则
    // 6. 扫描语义文件并检测未使用原始值
    // 7. 为每个语义层文件构建主题
    // 8. 生成 CSS 变量、跨平台令牌和设计系统文档
    console.log("\n🎉 所有主题构建完毕！")
  } catch (err) {
    console.error(err.message)
    process.exit(1)
  }
}

main()
```

**核心原则**：每个函数只做一件事。`main()` 中每个步骤对应一个命名清晰的函数（`loadPrimitives()`、`loadCommonRules()`、`loadTokenColors()`……），调用者一眼就能看出构建流程的每一步在做什么。

模块化的骨架搭好了，接下来要解决真正的工程问题：**构建过程必须能够自我证明**。

---

## 🛡️ 二、质量验证：让构建过程「自我证明」

工业级构建脚本的核心特征：**它不仅仅生成文件，还会验证生成的文件是否正确**。验证失败时中断构建，让错误在发布之前就被发现。

### 2.1 WCAG 对比度自动校验

主题的每个颜色都必须在背景上清晰可读。Moongate 为关键文本角色设置了对 `editor.background` 的最低对比度要求：

| 角色                    | 最低对比度 | 说明                    |
| ----------------------- | ---------- | ----------------------- |
| `text`（正文）          | 4.5:1      | WCAG AA                 |
| `textDim`（次要文字）   | 4.0:1      | 略低于 AA，权衡视觉层次 |
| `textMuted`（辅助文字） | 3.0:1      | 大字号/辅助信息可用     |

校验决策可用一棵简单的树来表达：

```
某个语义色 vs 背景
   ├─ ratio < 3.0        → ❌ 构建中断（任何角色都不合格）
   ├─ 3.0 ≤ ratio < 4.0  → ⚠️ 仅 textMuted 可接受（其余失败）
   ├─ 4.0 ≤ ratio < 4.5  → ⚠️ 仅 textDim/comment 可接受（text 失败）
   └─ ratio ≥ 4.5        → ✅ 全部通过（达到 WCAG AA）
```

```javascript
// scripts/lib/validators.js（简化）
import wcag from "wcag-contrast"

export function checkContrast(color1, color2, role, themeType) {
  if (!color1 || !color2) return
  const ratio = wcag.hex(color1, color2)

  let minRatio = 4.5
  if (role === "textDim" || role === "comment") minRatio = 4.0
  if (role === "textMuted") minRatio = 3.0

  if (ratio < minRatio) {
    if (role === "textMuted") {
      console.warn(
        `⚠️ 对比度略低: ${themeType} · ${role} = ${ratio.toFixed(2)}:1`,
      )
    } else {
      throw new Error(
        `❌ 对比度不足: ${themeType} · ${role} = ${ratio.toFixed(2)}:1` +
          `\n   WCAG 要求 ≥${minRatio}:1`,
      )
    }
  } else {
    console.log(`✅ ${themeType} · ${role}: ${ratio.toFixed(2)}:1`)
  }
}
```

构建成功时，你会看到类似输出：

```
✅ dark · text: 14.48:1
✅ dark · textDim: 12.02:1
✅ dark · textMuted: 6.96:1
✅ light · text: 17.08:1
✅ light · textDim: 7.25:1
✅ light · textMuted: 7.25:1
```

**关键设计**：`textDim` 和 `textMuted` 使用**阶梯式标准**而非统一 4.5:1——因为「视觉退后」本身就是设计意图，只要保持最低可读性即可。如果所有辅助文字都强制 4.5:1，注释和辅助信息就无法在视觉上「退后」了。

### 2.2 结构验证：生成的文件必须自洽

`validateThemeStructure` 验证生成的主题 JSON：

- 必须包含 `name`、`type`、`colors`、`tokenColors`、`semanticTokenColors` 五个必需键。
- `colors` 不能为空对象，且所有值必须是合法的 6 位或 8 位十六进制色值。
- `tokenColors` 必须是数组。
- **不能存在未解析的 `${var}` 或 `{token}` 残留**——如果某个变量因为拼写错误没有被替换，这里会直接报错并中断构建。

验证失败时，抛出 `ThemeValidationError` 并携带是哪个输出文件出错的信息，错误处理统一由主流程捕获，而不是散落在各个函数里。

### 2.3 循环引用检测

语义层可能引用原始值，原始值理论上也可能引用另一个原始值。如果 `a → b → a` 形成循环，`resolveTokens` 会无限递归。

`tokens.js` 的做法：**设定深度上限（20 层），超过则抛出 `[ENGINEERING_FATAL]` 错误并输出引用链**：

```javascript
export function resolveTokens(obj, tokenMap, depth = 0, path = []) {
  const MAX_DEPTH = 20
  if (depth > MAX_DEPTH) {
    throw new Error(`[ENGINEERING_FATAL] 令牌循环引用检测: ${path.join(" → ")}`)
  }
  // ...
}
```

20 层的上限远高于正常的令牌引用深度（正常不会超过 2-3 层），因此一旦触发，**几乎可以确定是循环引用**。

### 2.4 重复色值与未使用令牌检测

- **`detectDuplicateColors`**：检测原始值中是否有两个不同名字指向同一个色值。这不是错误（有时同名色值用于语义区分），但值得提醒——可能是命名混乱的信号。
- **`detectUnusedPrimitives`**：检测哪些原始值没有被任何语义层引用。未被引用的原始值可能是「死代码」，也提示语义层可能存在缺口。

### 2.5 架构污染检测

上一篇文章介绍过：颜色必须经历「原始值 → 语义层 → 组件层」的传递链条，任何跨层直接引用都是架构污染。

`tokens.js` 中的 `detectPrimitiveReference` 会在构建时检测**组件层是否直接引用了原始值**（如 `workbench.yaml` 中出现 `editor.background: "${blue-500}"` 而非 `${surfaceGround}`），并给出警告：

```
[架构提醒] workbench 中直接引用了原始值 "blue-500"，建议通过语义层引用。
```

这个功能让「架构规范」从口头约定变成了**可自动检查的工程约束**。

构建必须正确，但正确还不够——**生成的产物还要精简**，否则文件会随语言增多而膨胀。

---

## ⚙️ 三、输出优化：让生成的 JSON 更精简

工业级构建不仅要「生成正确」，还要「生成精致」。Moongate 通过两个优化步骤，让最终主题 JSON 大幅精简。

### 3.1 `mergeTokenColors`：合并相同样式的规则

不同语言中经常有为不同 scope 分配**完全相同样式**的规则。例如：

```yaml
# python.yaml
- name: Python F-string Expression
  scope: ["meta.fstring.python"]
  settings: { foreground: "#7dd3fc" }

# jsx.yaml
- name: JSX Expression Braces
  scope: ["meta.jsx.expression"]
  settings: { foreground: "#7dd3fc" }
```

这两个规则的颜色相同，完全可以合并为一条规则、两个 scope。`mergeTokenColors` 自动完成这件事：

- 将 `settings` 序列化为**稳定键**（按键名排序，避免 `{foreground, fontStyle}` 和 `{fontStyle, foreground}` 被当作不同规则）。
- 相同样式的 rule 合并，scope 聚合为数组。
- 结果按 scope 数量降序排序（更具体的规则在前）。

实际效果：Moongate v2.4.0 中，`tokenColors` 规则数从约 **89 条精简至约 34 条（减少 62%）**，主题 JSON 体积减少约 16%。

### 3.2 `optimizeSemanticTokenColors`：删除冗余 foreground

VS Code 的语义角色支持**父级继承**：`function.declaration` 会自动继承 `function` 的样式，除非被显式覆盖。

因此，当 `function.declaration` 的 `foreground` 与父级 `function` 完全相同时，可以安全删除 `foreground`，只保留额外的 `fontStyle`：

```javascript
// 优化前
{
  "function": "#87cefa",
  "function.declaration": {
    "foreground": "#87cefa",  // 冗余！与父级相同
    "fontStyle": "bold"
  }
}

// 优化后
{
  "function": "#87cefa",
  "function.declaration": {
    "fontStyle": "bold"  // VS Code 自动继承 function 的颜色
  }
}
```

这个优化需要小心：只有当父子关系确实存在、且 foreground 确实相同时才安全。`optimizeSemanticTokenColors` 精确实现这个逻辑，并在删除时计数输出。

输出精简了，还藏着一个更深的问题：**规则本身对不对**？scope 是不是真的存在？

---

## 🔍 四、Scope 验证：让「规则写了对不上」成为历史

主题开发中最令人沮丧的问题之一：**精心编写的语言规则，在代码里却不生效**。

原因通常是：规则里的 `scope` 并不存在于 VS Code 实际使用的 TextMate 语法中。每个语言（Python、Go、Rust……）的 scope 是**由语法文件定义的**，不同语言、不同版本之间可能有巨大差异。手动对照语法文档编写 scope 非常容易出错。

Moongate 的解决方案是 **`scripts/verify-scopes.js`**——自动解析 VS Code 内置的 TextMate 语法文件，比对语言配置中的每个 scope，找出「写了却永远不生效」的死规则。

### 4.1 工作原理

```javascript
// scripts/verify-scopes.js（CLI 入口）
import {
  verifyAllScopes,
  formatVerificationResult,
} from "./lib/scope-validator.js"

const result = verifyAllScopes({ verbose: true })

console.log("🔍 验证语言配置中的 scope...\n")
process.stdout.write(formatVerificationResult(result))

if (!result.isValid) {
  console.error(`\n❌ 发现 ${result.totalIssues} 个 scope 不匹配，验证失败！`)
  process.exit(1)
}
```

`scope-validator.js` 的核心流程：

1. **定位语法源**：在 VS Code 安装目录中找到对应语言的 TextMate 语法 JSON 文件（如 `python.tmLanguage.json`）。
2. **提取全部作用域**：遍历语法文件中的 `patterns`、`captures`、`repository` 等结构，收集该语言所有可用的 scope 列表。
3. **比对语言规则**：将 `src/languages/*.yaml` 中定义的每个 scope 与语法中实际存在的 scope 比对。
4. **输出报告**：列出每个不匹配的 scope、所在文件、是哪个语言规则定义的。

### 4.2 它发现了什么？

在 Moongate v2.6.0 中，`verify-scopes.js` 帮助修复了 **8 个语言文件**的 scope 问题。其中最有代表性的几个：

| 语言     | 修复前（错误的 scope）   | 修复后（正确的 scope）            |
| -------- | ------------------------ | --------------------------------- |
| Rust     | `support.macro.rust`     | `entity.name.function.macro.rust` |
| Rust     | `lifetime`               | `entity.name.type.lifetime.rust`  |
| Go       | `entity.name.package.go` | `keyword.package.go`              |
| Python   | `meta.decorator.python`  | `meta.function.decorator.python`  |
| Markdown | `heading.1.markdown` 等  | `markup.heading.markdown` 等      |

更关键的是，**验证工具可以防止回归**——每次修改语言规则后运行一次，立刻知道哪些 scope 是「写了但永远不会生效」的。

### 4.3 在 CI 中使用

`verify-scopes.js` 在发现错误时以退出码 1 结束，因此可以无缝集成到 CI 流程中：

```json
{
  "scripts": {
    "test:scopes": "node scripts/verify-scopes.js"
  }
}
```

CI 中运行 `pnpm test:scopes`，一旦有人提交了错误的 scope，构建立即失败。

验证工具越来越多，**验证工具自身也需要被验证**——这就是自动化测试的价值。

---

## 🧪 五、自动化测试：让构建系统可回归验证

当构建系统承担了「生成全部产物 + 质量验证 + 架构检查」的重任后，构建系统**自身**也需要测试来防止回归。

Moongate 使用 Node.js 内置的 `node --test` 测试运行器，不需要额外安装测试框架：

### 5.1 测试覆盖范围

```
test/
├── tokens.test.js        # 令牌解析、变量替换、循环检测
├── validators.test.js    # WCAG 对比度、结构验证、架构污染检测
├── optimizers.test.js    # token 合并、语义色精简
├── generators.test.js    # CSS/SCSS/TS/文档生成器
├── utils.test.js         # normalizeHex、duplicateColors 等工具
├── scope-validator.test.js # scope 验证逻辑
├── better-comments.test.js # Better Comments 生成器
├── theme-output.test.js  # 构建输出的主题 JSON 完整性
└── helpers.js            # 测试辅助（捕获 console、断言抛错）
```

85 个测试覆盖三大类：

1. **纯函数逻辑**：`normalizeHex` 对 3 位/4 位/8 位色值的处理、`resolveTokens` 的循环检测、`mergeTokenColors` 的稳定键等。
2. **边界条件**：非法色值抛错、未定义变量警告、透明度后缀的多种边界情况。
3. **产物一致性**：生成的 CSS 变量与语义层一一对应、Better Comments 配置与深色语义层色值同步。

### 5.2 测试辅助：捕获 console 输出

构建工具大量使用 `console.log` / `console.warn` 输出进度和警告。测试时，`test/helpers.js` 提供统一的辅助函数来捕获这些输出并断言：

```javascript
// test/helpers.js（示意）
export function captureConsole(fn) {
  const logs = []
  const originalLog = console.log
  const originalWarn = console.warn
  console.log = (...args) => logs.push({ type: "log", args })
  console.warn = (...args) => logs.push({ type: "warn", args })
  try {
    const result = fn()
    return { logs, result }
  } finally {
    console.log = originalLog
    console.warn = originalWarn
  }
}
```

这个模式的优雅之处：**不需要 mock 每个函数**——只要断言 `console.warn` 被调用过，且传入了包含「警告」关键词的参数，就能验证「未定义变量时会发出警告」这类行为。

### 5.3 运行测试

```json
{
  "scripts": {
    "test": "node --test \"test/*.test.js\""
  }
}
```

```bash
pnpm test
```

输出示例：

```
▶ tokens.test.js
  ✔ 解析令牌引用 {token}
  ✔ 替换变量 ${var} 支持透明度后缀
  ✔ 检测到循环引用时抛出错误
  ...
▶ validators.test.js
  ✔ 结构验证：缺少必需键时报错
  ✔ WCAG 对比度不足时抛出异常
  ...
✔ 85 tests passed
```

构建体系稳定之后，可以把这套能力伸向**生态整合**：让配色在插件生态里也不漂移。

---

## 🎨 六、Better Comments 双源消除

主题的一个重要生态整合是 **Better Comments** 插件——它让注释中的 `TODO`、`FIXME`、`NOTE` 等标记拥有专属颜色。旧方案的问题在于：**颜色存在两处**（主题的语义层 + Better Comments 的 JSON 配置），修改一处后另一处容易忘改，导致配色漂移。

Moongate 的解法：**Better Comments 配置由构建脚本自动生成**，从深色语义层读取色值。

### 6.1 单源真相

```javascript
// scripts/generate-better-comments.js（核心逻辑）
import { resolveTokens } from "./lib/tokens.js"

// Better Comments 需要的语义变量 → tag 映射
const TAG_MAP = [
  { tag: "TODO", semanticKey: "warning", bold: true },
  { tag: "FIXME", semanticKey: "error", bold: true, italic: true },
  { tag: "NOTE", semanticKey: "highlight", italic: true },
  { tag: "HACK", semanticKey: "purple", bold: true },
  { tag: "BUG", semanticKey: "error", bold: true, underline: true },
  { tag: "XXX", semanticKey: "warning", bold: true },
]

// 从深色语义层解析每个 tag 对应的最终色值
const tags = TAG_MAP.map(({ tag, semanticKey, ...style }) => {
  const color = resolveTokens(darkSemantics[semanticKey], primitives)
  return { tag, color, ...style }
})

// 写入 extras/better-comments.json
```

当你在 `dark.yaml` 中调整 `warning` 的颜色时，运行 `pnpm run gen:better-comments`，`extras/better-comments.json` 自动同步。

### 6.2 内置规则：零配置开箱即用

除了生成独立预设，Moongate 还在 `src/special/better-comments.yaml` 中内置了 6 个特殊注释 scope 的规则（TODO、FIXME、NOTE、HACK、BUG、XXX）。**安装 Moongate 主题后，Better Comments 插件自动使用官方配色，无需任何手动配置。**

这条「双通道」设计覆盖了两种用户：

- **主题用户**：安装 Moongate 即获得完整配色（内置规则）。
- **独立配置用户**：不使用主题、只看注释配色的人，可以单独引入 `extras/better-comments.json`。

两条通道都从语义层生成，**不会产生双源漂移**。

---

## 📦 七、多格式产物生成：一套令牌，全平台复用

设计系统介绍了 CSS 变量导出，Moongate 进一步将语义层导出为**四种格式**，覆盖 Web、Sass 和 TypeScript 生态：

| 产物                         | 格式             | 适用场景                    |
| ---------------------------- | ---------------- | --------------------------- |
| `themes/moongate-colors.css` | CSS 变量         | 博客、组件库、任何 Web 项目 |
| `themes/moongate-layout.css` | CSS 变量（布局） | 间距、排版、断点、z-index   |
| `themes/_tokens.scss`        | SCSS             | Sass 项目                   |
| `themes/tokens.ts`           | TypeScript       | 前端框架项目                |

### 7.1 SCSS 令牌

```scss
// themes/_tokens.scss（自动生成）
// 部分示意
$ui-colors-dark: (
  bg: #0f172a,
  primary: #3b82f6,
  surface-raised: #1a2538, // ...
);

$ui-colors-light: (
  bg: #f9fafb,
  primary: #0284c7,
  surface-raised: #ffffff, // ...
);

// 深色模式便捷变量
$ui-bg: #0f172a;
$ui-primary: #3b82f6;
```

### 7.2 TypeScript 令牌

```typescript
// themes/tokens.ts（自动生成）
export interface MoongateTokens {
  dark: Record<string, string>
  light: Record<string, string>
}

export const tokens: MoongateTokens = {
  dark: {
    bg: "#0f172a",
    primary: "#3b82f6",
    // ...
  },
  light: {
    bg: "#f9fafb",
    primary: "#0284c7",
    // ...
  },
}

export default tokens
```

### 7.3 设计系统文档自动生成

构建脚本还会自动生成 `docs/DESIGN_SYSTEM.md`——包含变量选择协议、原始色板预览、海拔系统表格、WCAG 对比度数据。这份文档**不是手写的**，而是每次构建时从真实数据生成，保证文档永远与代码一致。

---

## 🔧 八、CI/发布链路：错误在发布前被发现

工业级工程的最后一块拼图：**把验证融入发布流程**。

```json
{
  "scripts": {
    "build": "node scripts/build.js",
    "test": "node --test \"test/*.test.js\"",
    "test:scopes": "node scripts/verify-scopes.js",
    "gen:better-comments": "node scripts/generate-better-comments.js",
    "package": "pnpm run build && pnpm run gen:better-comments && vsce package",
    "publish": "pnpm run build && pnpm run gen:better-comments && vsce publish",
    "prepublishOnly": "pnpm run build && pnpm run gen:better-comments"
  }
}
```

发布链路逐层把关：

```
pnpm run package
  → pnpm run build              # 构建 + WCAG 校验 + 结构验证 + 架构检测
  → pnpm run gen:better-comments # 重新生成 Better Comments 配置
  → vsce package                # 打包 .vsix
```

- 构建失败 → 不产生 `.vsix`。
- 对比度不足 → 构建中断，错误信息标明是哪个主题、哪个角色、差多少。
- scope 错误 → `pnpm run test:scopes` 退出码 1，CI 拦截。

**「发布前检查清单」从手动流程升级为自动化门禁**——这正是工业级与手动的分水岭。

---

## 📊 九、总结

至此，你的主题构建系统已经完成了从「能用」到「工业级」的跃迁：

| 能力            | 主题工程化      | 构建体系                           |
| --------------- | --------------- | ---------------------------------- |
| 模块化          | 单体 `build.js` | `scripts/lib/` 单职责模块          |
| 代码风格        | CommonJS        | ESM                                |
| WCAG 校验       | ❌              | ✅ 阶梯式对比度自动检查            |
| 结构验证        | ❌              | ✅ 未解析变量/令牌检测             |
| 循环引用        | ❌              | ✅ 深度上限 + 引用链输出           |
| 架构污染        | ❌              | ✅ 原始值直接引用警告              |
| 输出优化        | ❌              | ✅ token 合并 + 语义色精简         |
| Scope 验证      | ❌              | ✅ `verify-scopes.js` 自动比对     |
| 自动化测试      | ❌              | ✅ 85 个测试（`node --test`）      |
| 多格式产物      | CSS             | CSS + SCSS + TypeScript + 设计文档 |
| Better Comments | ❌              | ✅ 自动生成 + 内置规则             |
| 发布门禁        | 手动检查        | 构建/测试/scope 验证自动拦截       |

这套体系不仅是 Moongate 主题的生产工具，更是一个可以复用的**设计系统工程模板**。它验证了「零散的颜色 → 工程资产 → 全平台可复用」的完整路径。

但工程能力再强，也只是「怎么做」的问题。下一个问题同样重要：**「为什么这样做」——设计哲学从何而来？如何让用户在不同硬件上获得一致的体验？如何让主题超越代码，成为品牌的组成部分？**

这正是系列的最后一篇——**品牌生态**要回答的。

[**品牌生态：设计哲学与视觉契约**](./create-vscode-theme-brand-ecosystem)
