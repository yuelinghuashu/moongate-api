---
title: 设计系统：DTCG 三层架构与昼夜双变体
description: 用量业界标准的 DTCG 设计令牌标准管理颜色，通过语义层与重力补偿构建深色/浅色双变体，让「同一语义角色在不同背景下视觉重量对等」从理念变为可执行的工程架构。
date: 2026-08-06 04:00:00
permalink: 97e09fc8-13a0-4703-958d-44700fe20a62
series: design-system
level: P3
tags:
  - Design System
  - Theme
  - Engineering
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

在[主题工程化](./create-vscode-theme-engineering)的结尾，我们指出了工程化方案的三个痛点：

- `colors.yaml` 是一个**扁平的变量池**，颜色之间的层级关系完全靠命名约定，没有结构性的约束。
- 深浅两套主题需要两套颜色变量文件，而「同一角色在深色和浅色下应该保持色相一致、明度不同」这件事完全靠手动维护。
- 构建脚本只能做变量替换，不能自动校验颜色是否符合对比度标准。

本篇将解决这些问题。我们将引入 **DTCG（Design Tokens Community Group）设计令牌标准**的三层架构，让颜色从「散乱的变量」升级为「有结构的工程资产」，并基于它构建深色/浅色双变体——这是主题迈向设计系统的关键一步。

---

## 🧱 一、核心思路：从「多主题」到「设计系统」

一个优秀的主题不应只有一副面孔。提供深色/浅色双主题，不仅能覆盖更广泛的用户需求，更是主题迈向设计系统的关键一步。

但**多主题的真正价值不是多几套颜色**，而是：**所有主题共享同一套规则，唯一不同的是颜色变量的具体值。**

- 语言规则（`languages/*.yaml`）在所有变体中完全复用。
- UI 颜色（`workbench.yaml`）和语义规则（`semantic.yaml`）只引用变量名，不写具体色值。
- 每个变体只提供一个「语义层」，定义每个语义角色在该变体下的具体颜色。

要做到这一点，需要一套能让「颜色」成为结构化工程资产的管理体系——这就是 DTCG 三层架构。

---

## 🏗️ 二、DTCG 三层架构

DTCG（Design Tokens Community Group）是 W3C 下属的行业标准组织，旨在为设计令牌（Design Tokens）建立统一格式。它的核心思想可以用一个简单的问题概括：**一个颜色值，应该由谁来定义、被谁引用、以什么名称存在？**

Moongate 采用 DTCG 推荐的三层架构：

```
┌─────────────────────────────────────────────────┐
│  原始值层（Primitives）                           │
│  src/core/primitives/colors.yaml                 │
│  按色相-明度命名：blue-500、gray-900             │
└───────────────────────┬─────────────────────────┘
                        │ 语义层用 {token} 引用原始值
                        ▼
┌─────────────────────────────────────────────────┐
│  语义层（Semantics）                              │
│  src/core/semantics/dark.yaml + light.yaml       │
│  定义角色：primary、bg、surfaceGround            │
│  primary: "{blue-500}"                           │
└───────────────────────┬─────────────────────────┘
                        │ 组件层用 ${variable} 引用语义层
                        ▼
┌─────────────────────────────────────────────────┐
│  组件层（Components）                             │
│  src/workbench.yaml + semantic.yaml              │
│  映射 UI 元素                                    │
│  editor.background: "${surfaceGround}"           │
└─────────────────────────────────────────────────┘
```

### 2.1 第一层：原始值（Primitives）

原始值层是**所有颜色的物理事实**——不表达任何语义，只按色相和明度命名。

```yaml
# src/core/primitives/colors.yaml
# ==================== 蓝色系 ====================
blue-500: "#3b82f6" # 深色主蓝
blue-600: "#2563eb" # 深色按钮悬停
blue-700: "#0284c7" # 浅色主蓝
blue-800: "#0369a1" # 浅色高亮/函数

# 发光蓝（特殊）
blue-glow: "#7dd3fc" # 深色高亮
blue-glow-dark: "#87cefa" # 深色函数

# ==================== 灰色阶（冷调基底）====================
gray-900: "#0f172a" # 深色编辑器背景
gray-850: "#131c31" # 深色卡片/浮层
gray-800: "#1e293b" # 深色侧边栏/代码块
gray-750: "#252e40" # 深色悬停背景
gray-700: "#2d3748" # 深色边框
# ... 完整的色相-明度阶梯
```

**命名规范**：`色相-明度`，例如 `blue-500`、`green-400`、`gray-900`。这样命名不是为了好看，而是为了让「同一个色相在不同明度下如何变化」这件事变得可追溯。

**原始值的价值**：当你需要「给所有主题换一个更蓝的主色」时，只需要调整 `blue-500` 和 `blue-700` 两个原始值，所有引用它的语义色自动同步。

### 2.2 第二层：语义层（Semantics）

语义层定义**角色**——`primary`（主色）、`bg`（背景）、`surfaceGround`（地面层）——并为每个角色指派一个原始值。**语义层不与任何 UI 元素绑定**，它只回答一个问题：「主色应该是什么颜色？」

每个变体都有一个独立的语义层文件：

```yaml
# src/core/semantics/dark.yaml（深色语义层）
# ==================== 月语义主色 ====================
primary: "{blue-500}"
success: "{green-400}"
warning: "{yellow-400}"
error: "{red-400}"
highlight: "{blue-glow}"

# 功能色（语法）
function: "{blue-glow-dark}"
operator: "{gray-600}"
comment: "{gray-525}"
variable: "{gray-200}"

# 海拔系统
surfaceGround: "{gray-900}"
surfaceRaised: "{gray-850}"
surfaceFloating: "{gray-800}"
surfaceTooltip: "{gray-750}"
```

```yaml
# src/core/semantics/light.yaml（浅色语义层）
# 变量名与深色版完全一致，仅色值不同
# ==================== 月语义主色 ====================
primary: "{blue-700}"
success: "{green-600}"
warning: "{yellow-700}"
error: "{red-700}"
highlight: "{blue-800}"

# 功能色（语法）
function: "{blue-800}"
operator: "{gray-600}"
comment: "{gray-600}"
variable: "{gray-900}"

# 海拔系统
surfaceGround: "{gray-50}"
surfaceRaised: "{white}"
surfaceFloating: "{gray-100}"
surfaceTooltip: "{gray-200}"
```

**关键原则**：所有变体的语义层变量名**必须完全一致**。这是规则复用的基础——`workbench.yaml` 和 `languages/*.yaml` 不需要知道当前是深色还是浅色，它们只引用 `${primary}`、`${surfaceRaised}`，具体值交给语义层决定。

### 2.3 第三层：组件层（Components）

组件层**直接映射 UI 元素**和语法角色。它只引用语义层变量，不写任何具体色值：

```yaml
# src/workbench.yaml（组件层：UI 颜色）
editor.background: "${surfaceGround}"
editor.foreground: "${text}"
titleBar.activeBackground: "${surfaceRaised}"
titleBar.activeForeground: "${text}"
statusBar.background: "${surfaceGround}"
statusBar.foreground: "${textDim}"
sideBar.background: "${surfaceRaised}"
# ... 所有 UI 键
```

```yaml
# src/semantic.yaml（组件层：语义高亮）
variable: "${variable}"
variable.readonly:
  foreground: "${variableDim}"
  fontStyle: "italic"
function: "${function}"
function.declaration:
  foreground: "${function}"
  fontStyle: "bold"
class: "${warning}"
# ... 语义规则
```

### 2.4 两种引用语法：`{token}` 与 `${variable}`

在 Moongate 的架构中，两种「引用」有着完全不同的语义：

| 语法          | 含义                               | 使用位置         | 示例                                    |
| ------------- | ---------------------------------- | ---------------- | --------------------------------------- |
| `{token}`     | **层间引用**：引用另一个令牌       | 语义层引用原始值 | `primary: "{blue-500}"`                 |
| `${variable}` | **变量替换**：构建时替换为最终色值 | 组件层引用语义层 | `editor.background: "${surfaceGround}"` |

- 语义层用 `{token}` 引用原始值，构建脚本递归解析令牌引用（支持循环检测，见构建体系）。
- 组件层用 `${variable}` 引用语义层变量，构建脚本替换为最终色值（支持透明度后缀，如 `${primary}20`）。

这个分工不是形式主义——它让每个文件都清楚自己「属于哪一层、可以引用谁」。构建体系中我们会看到，构建脚本甚至能检测「组件层直接引用原始值」这种架构污染并发出警告。

---

## ⚖️ 三、昼夜双变体与重力补偿

有了三层架构，深色/浅色双主题的构建就变得非常自然：**所有规则文件完全复用，只有语义层不同**。但语义层的色值不是随手填的——深色和浅色之间需要一套科学的映射规则。

### 3.1 为什么浅色不是深色的「反相」？

很多新手以为浅色主题就是把深色主题的颜色反相。但这是完全错误的：

- 深色背景上的亮色是**「发光体」**——它们通过「亮于背景」来获得存在感。
- 浅色背景上的暗色是**「吸光体」**——它们通过「暗于背景」来获得存在感。

同样是 `primary`，在深色下用亮蓝 `#3b82f6`（明度 60%）很好看，但如果直接搬到白底上，就会因为和白色背景的对比度不足而「消失」。要实现视觉重量对等，必须进行**重力补偿**——保持色相不变，科学调整明度和饱和度。

### 3.2 Moongate 的补偿实例

| 语义角色        | 深色版               | 浅色版               | 调整方法                             |
| --------------- | -------------------- | -------------------- | ------------------------------------ |
| 主色（primary） | `#3b82f6` (60% 明度) | `#0284c7` (48% 明度) | 蓝调不变，明度降低约 20%，适应白底   |
| 成功（success） | `#34d399` (65%)      | `#059669` (40%)      | 绿色更沉稳，保证对比度               |
| 警告（warning） | `#fbbf24` (75%)      | `#b45309` (35%)      | 从亮黄转为橙黄，避免在白底上「消失」 |
| 错误（error）   | `#f87171` (60%)      | `#b91c1c` (35%)      | 深红保持警示感                       |

**补偿规律**：

- **色相（H）不变**——这是语义一致性的核心。`primary` 永远是蓝色，用户在切换主题时不需要重新学习。
- **明度（L）降低**——深色版的亮色在白底上需要更暗才能达到同等对比度。通常降低 20-30%。
- **饱和度（S）适当调整**——深色背景上高饱和度产生舒适的「发光感」，但在浅色背景上可能刺眼，通常降低 10-20%。

> 💡 **如何科学地确定补偿值**：手动「凭感觉」调整明度效率低且不一致。推荐使用 **HSL 颜色模型**：把 HEX 转成 HSL，保持 H 不变，调整 L 和 S。可以用 [chroma.js](https://gka.github.io/chroma.js/) 等工具程序化调整，例如：
>
> ```javascript
> const darkPrimary = chroma("#3b82f6")
> const lightPrimary = darkPrimary.set("hsl.l", 0.48).set("hsl.s", 0.7)
> ```

### 3.3 海拔系统：为 UI 注入物理深度

除了语法颜色，UI 界面也需要在深浅两套主题中保持一致的「空间感」。海拔系统通过定义多级背景明度阶梯，表达 UI 元素的物理深度：

- 深色模式：海拔越高表面越亮（明度递增）。
- 浅色模式：海拔越高表面越亮（但用更高亮度的白色/浅色）。
- 每层阶梯的步长保持一致，形成平滑的层次感。

**Moongate 的四层海拔**（v2.6.0 最新值）：

| 海拔层级          | 用途                     | 深色模式  | 浅色模式  | 说明               |
| ----------------- | ------------------------ | --------- | --------- | ------------------ |
| `surfaceGround`   | 底层背景（编辑器）       | `#0f172a` | `#f9fafb` | 基准层             |
| `surfaceRaised`   | 侧边栏、活动栏、选项卡栏 | `#1a2538` | `#ffffff` | 深色 +5%，浅色纯白 |
| `surfaceFloating` | 面板、悬浮卡片、菜单     | `#25364a` | `#f1f5f9` | 再 +5%，浅色浅灰蓝 |
| `surfaceTooltip`  | 提示框、弹窗             | `#2e3b4d` | `#e2e8f0` | 最高层             |

> 📌 **注意**：浅色模式采用「越高越亮」还是「越高越暗」是一个设计选择。Moongate 选择浅色模式下浮层比背景**更亮**（更白），形成「纸张层叠」的通透感；深色模式则采用「越高越亮」，让浮层从背景中微微隆起。两种模式都遵循「海拔越高越醒目」的物理隐喻。

这种设计让侧边栏微微隆起，弹窗轻盈浮现，代码区沉静深邃——编辑器从平面走向立体。

---

## 🔨 四、构建脚本自动生成双主题

有了三层架构，构建脚本只需要做一件事：**扫描 `semantics/` 目录，为每个语义层文件生成一个主题 JSON**。

以 `scripts/build.js` 的简化版为例：

```javascript
// scripts/build.js（简化版，完整版见构建体系）
import fs from "node:fs"
import path from "node:path"
import yaml from "js-yaml"

const ROOT_DIR = process.cwd()

// 路径配置
const PATHS = {
  primitives: path.join(ROOT_DIR, "src", "core", "primitives", "colors.yaml"),
  semanticsDir: path.join(ROOT_DIR, "src", "core", "semantics"),
  workbench: path.join(ROOT_DIR, "src", "workbench.yaml"),
  semantic: path.join(ROOT_DIR, "src", "semantic.yaml"),
  langDir: path.join(ROOT_DIR, "src", "languages"),
  outputDir: path.join(ROOT_DIR, "themes"),
}

// 加载原始值
const primitives = yaml.load(fs.readFileSync(PATHS.primitives, "utf8"))

// 解析令牌引用 {token}：语义层引用原始值
function resolveTokens(obj, tokenMap, depth = 0) {
  const MAX_DEPTH = 20
  if (depth > MAX_DEPTH) {
    throw new Error(
      `[ENGINEERING_FATAL] 令牌循环引用检测: ${JSON.stringify(obj)}`,
    )
  }
  if (typeof obj === "string") {
    return obj.replace(/\{([a-zA-Z0-9_-]+)\}/g, (match, key) => {
      const value = tokenMap[key]
      if (value === undefined) {
        console.warn(`⚠️ 警告: 令牌 "${key}" 未定义，保留原样`)
        return match
      }
      return resolveTokens(value, tokenMap, depth + 1)
    })
  }
  if (Array.isArray(obj))
    return obj.map((item) => resolveTokens(item, tokenMap, depth + 1))
  if (obj && typeof obj === "object") {
    const result = {}
    for (const [k, v] of Object.entries(obj)) {
      result[k] = resolveTokens(v, tokenMap, depth + 1)
    }
    return result
  }
  return obj
}

// 替换变量 ${var}：组件层引用语义层（支持透明后缀）
function replaceVariables(obj, colors) {
  if (typeof obj === "string") {
    return obj.replace(
      /\$\{([a-zA-Z0-9_-]+)\}([0-9a-fA-F]{2})?/g,
      (match, key, alpha) => {
        const value = colors[key]
        if (value === undefined) {
          console.warn(`⚠️ 警告: 变量 "${key}" 未定义，保留原样`)
          return match
        }
        return value + (alpha || "")
      },
    )
  }
  if (Array.isArray(obj))
    return obj.map((item) => replaceVariables(item, colors))
  if (obj && typeof obj === "object") {
    const result = {}
    for (const [k, v] of Object.entries(obj)) {
      result[k] = replaceVariables(v, colors)
    }
    return result
  }
  return obj
}

// 加载公共规则
const workbenchRaw = yaml.load(fs.readFileSync(PATHS.workbench, "utf8"))
const semanticRaw = yaml.load(fs.readFileSync(PATHS.semantic, "utf8"))

// 扫描并合并语言规则
let tokenColorsRaw = []
fs.readdirSync(PATHS.langDir)
  .filter((f) => f.endsWith(".yaml"))
  .sort()
  .forEach((file) => {
    const rules = yaml.load(
      fs.readFileSync(path.join(PATHS.langDir, file), "utf8"),
    )
    if (rules?.tokenColors) {
      tokenColorsRaw = tokenColorsRaw.concat(rules.tokenColors)
    }
  })

// 读取 package.json 获取主题基础名
const pkg = JSON.parse(
  fs.readFileSync(path.join(ROOT_DIR, "package.json"), "utf8"),
)
const baseName = pkg.name.replace(/[^a-z0-9-]/gi, "-").toLowerCase()

// 为每个语义层文件构建一个主题
const semanticFiles = fs
  .readdirSync(PATHS.semanticsDir)
  .filter((f) => f.endsWith(".yaml"))

semanticFiles.forEach((semanticFile) => {
  const themeType = path.basename(semanticFile, ".yaml") // 'dark' 或 'light'
  const semantics = yaml.load(
    fs.readFileSync(path.join(PATHS.semanticsDir, semanticFile), "utf8"),
  )

  // 1. 解析语义层的令牌引用 {token} -> 最终色值
  const resolved = resolveTokens(semantics, primitives)

  // 2. 组件层替换变量 ${var}
  const uiColors = replaceVariables(workbenchRaw, resolved)
  const semanticColors = replaceVariables(semanticRaw, resolved)
  const tokenColors = replaceVariables(tokenColorsRaw, resolved)

  // 3. 组装主题对象
  const type = themeType.includes("light") ? "light" : "dark"
  const theme = {
    name: `${pkg.displayName || "My Theme"} ${themeType === "dark" ? "Dark" : "Light"}`,
    type: type,
    colors: uiColors,
    tokenColors: tokenColors,
    semanticTokenColors: semanticColors,
  }

  // 4. 写入输出文件
  const outputFile = path.join(PATHS.outputDir, `${baseName}-${themeType}.json`)
  if (!fs.existsSync(PATHS.outputDir)) {
    fs.mkdirSync(PATHS.outputDir, { recursive: true })
  }
  fs.writeFileSync(outputFile, JSON.stringify(theme, null, 2))
  console.log(`   ✅ 构建完成: ${outputFile}`)
})
```

**核心逻辑**：

1. 加载原始值。
2. 扫描 `semantics/` 目录，每个语义层文件（`dark.yaml` / `light.yaml`）对应一个主题。
3. 解析语义层的 `{token}` 引用，得到该变体的最终色值字典。
4. 组件层用 `${var}` 替换为最终色值。
5. 输出 `${baseName}-dark.json` 和 `${baseName}-light.json`。

**新增主题的成本**：只需在 `semantics/` 目录下添加一个新的 YAML 语义层文件（如 `sepia.yaml`），构建脚本自动扫描生成对应主题。**无需修改任何规则文件**。

> 🔮 **预告**：构建逻辑越来越复杂——解析令牌、替换变量、合并规则、扫描语义层。你可能会问：**怎么证明它写得对？** 当颜色出错、scope 匹配不上时，如何自动发现？这就是下一篇引入**自动化测试与质量验证**的原因。

---

## 📦 五、注册多个主题

在 `package.json` 的 `contributes.themes` 中为每个主题添加一个条目，注意 `uiTheme` 字段的正确设置：

```json
"contributes": {
  "themes": [
    {
      "label": "Moongate Dark",
      "uiTheme": "vs-dark",
      "path": "./themes/moongate-theme-dark.json"
    },
    {
      "label": "Moongate Light",
      "uiTheme": "vs",
      "path": "./themes/moongate-theme-light.json"
    }
  ]
}
```

- **`uiTheme` 字段决定主题的基础色系**：
  - 深色主题：`"vs-dark"`
  - 浅色主题：`"vs"`
- **`label`** 将显示在 VS Code 命令面板的「颜色主题」列表中。

> 📌 **一个容易踩的坑**：如果浅色主题的 `uiTheme` 误设为 `"vs-dark"`，VS Code 会按照深色基础色系渲染控件（滚动条、输入框等），导致浅色主题出现深色控件——看起来很别扭。务必根据主题类型设置正确的 `uiTheme`。

---

## 🌐 六、跨平台资产：颜色不只是主题

DTCG 三层架构还有一个额外的巨大收益：**语义层的颜色字典可以直接导出为跨平台资产**。

构建脚本可以在生成主题 JSON 的同时，自动生成一个 CSS 变量文件：

```css
/* themes/moongate-colors.css（自动生成） */
:root,
.light {
  --ui-primary: #0284c7;
  --ui-bg: #f9fafb;
  --ui-surface-raised: #ffffff;
  /* ... 浅色模式所有语义色 */
}

.dark {
  --ui-primary: #3b82f6;
  --ui-bg: #0f172a;
  --ui-surface-raised: #1a2538;
  /* ... 深色模式所有语义色 */
}
```

这意味着你的博客、组件库、文档站可以**直接引用主题的视觉语言**：

```html
<link rel="stylesheet" href="/themes/moongate-colors.css" />
```

```css
body {
  background: var(--ui-bg);
  color: var(--ui-text);
}
```

切换深浅模式只需要在根元素上添加/移除 `.dark` class：

```javascript
document.documentElement.classList.toggle("dark")
```

Moongate 正是这样做的——博客、设计系统文档与 VS Code 主题共享同一套颜色，实现「一个颜色体系，贯穿所有产品」。除了 CSS，还可以自动生成 SCSS 和 TypeScript 令牌（见构建体系）。

---

## ⚠️ 七、常见问题与陷阱

| 问题                                  | 可能原因                                          | 解决方法                                                                                        |
| ------------------------------------- | ------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| **主题未出现在颜色主题列表中**        | `package.json` 中未正确注册，或 JSON 文件路径错误 | 检查 `contributes.themes` 条目，确保 `path` 指向正确的文件                                      |
| **浅色主题显示为深色**                | `uiTheme` 字段误设为 `"vs-dark"`                  | 浅色主题应使用 `"vs"`                                                                           |
| **颜色变量未替换，仍显示为 `${var}`** | 变量名拼写错误，或语义层未定义该变量              | 检查变量名是否一致，确保所有语义变量在 `dark.yaml` 和 `light.yaml` 中都有定义                   |
| **深色/浅色主题视觉差异过大**         | 重力补偿不合理，明度调整幅度不均                  | 遵循「色相不变、明度有规律降低、饱和度适度调整」的补偿规则                                      |
| **新增语义变量后，某个主题报错**      | `dark.yaml` 和 `light.yaml` 变量名不一致          | 所有变体的语义层变量名必须完全一致                                                              |
| **构建脚本报错「找不到文件」**        | 缺少必要的 YAML 文件                              | 确保 `src/core/primitives/`、`src/core/semantics/`、`src/languages/` 等目录存在，且包含所需文件 |

---

## 🚀 八、总结

通过引入 DTCG 三层架构，你将「多主题」升级为「设计系统」：

- ✅ **原始值层**：颜色按色相-明度命名，成为可追溯的物理事实。
- ✅ **语义层**：角色与变体解耦，每个变体只定义「角色该是什么颜色」。
- ✅ **组件层**：规则文件完全复用，不写任何具体色值。
- ✅ **重力补偿**：同一语义角色在不同背景下视觉重量对等。
- ✅ **海拔系统**：UI 拥有物理深度，深浅模式层次一致。
- ✅ **一键扩展**：新增主题只需添加一个语义层文件。
- ✅ **跨平台资产**：语义层直接导出 CSS 变量，一套颜色贯穿所有产品。

但你可能已经注意到：本篇的构建脚本仍然比较简单——它只能做变量替换，**还不能验证颜色是否符合对比度标准、是否引用了未定义的变量、是否产生了架构污染**。而且随着语言数量增加（当前 Moongate 支持 15 种语言），「语言规则写了对不上」的问题也会浮现。

下一篇将解决这些问题——**如何让构建脚本自身成为一套可测试、可验证的工程体系**。

[**构建体系：可测试、可验证的工程实践**](./create-vscode-theme-build-system)
