---
title: 主题工程化：从单体 JSON 到模块化 YAML
description: 将单体 JSON 重构为模块化 YAML 项目，用构建脚本实现变量替换与自动生成。让颜色变量可复用、语言规则可维护，为设计系统升级打下坚实基础。
date: 2026-08-06 02:00:00
permalink: 7199f437-f5ae-40e9-b08b-fba6968205b5
series: design-system
level: P3
tags:
  - VSCode
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

在[VS Code 主题](./create-vscode-theme-basics)中，你已经学会了如何手写一个可发布的 VS Code 主题。但随着主题功能越来越丰富，你可能遇到了以下痛点：

- 一个 JSON 文件动辄上千行，修改一个颜色需要全局搜索，容易误改。
- 想为不同语言定制高亮，却要在同一个 `tokenColors` 数组里堆砌规则，难以维护。
- 想尝试浅色版本，不得不复制整个文件，然后手动修改几百个颜色值。

是时候引入工程化了！本篇将带你**将一个单体的 JSON 主题重构为模块化、可自动构建的工程化项目**：用 YAML 拆分源文件，用构建脚本自动合并与变量替换。

> 💡 **本篇的定位**：本文采用的「单一颜色变量文件 + 构建脚本」方案是工程化的第一步。在[设计系统](./create-vscode-theme-design-system)中，这个方案会进一步升级为 DTCG 三层架构（原始值 → 语义层 → 组件层）。建议按顺序阅读，理解每一步的动机。

---

## 📦 准备工作

首先确保你已经安装了 Node.js 和 npm（或 pnpm）。然后安装构建依赖：

```bash
pnpm add -D js-yaml
# 或
npm install --save-dev js-yaml
```

> ⚠️ **注意**：`js-yaml` 必须安装在 `devDependencies` 中，因为它只是构建工具，不应作为生产依赖随主题发布。

---

## 📁 设计目录结构

我们将源码放在 `src/` 目录下，构建脚本放在 `scripts/`，最终生成的 JSON 放在 `themes/`：

```text
your-theme/
├── src/
│   ├── core/
│   │   └── colors.yaml          # 颜色变量（主色、背景、文本等）
│   ├── languages/                # 各语言的语法规则
│   │   ├── base.yaml             # 跨语言通用规则
│   │   ├── python.yaml
│   │   ├── go.yaml
│   │   └── ... (其他语言)
│   ├── workbench.yaml             # UI 颜色（colors 对象）
│   └── semantic.yaml              # 语义高亮（semanticTokenColors）
├── scripts/
│   └── build.js                   # 构建脚本
├── themes/
│   └── your-theme.json            # 构建生成的最终文件
├── package.json
└── .vscodeignore
```

> 📌 **注**：上图中的 `languages/` 目录中并没有 `javascript.yaml`、`typescript.yaml`——因为 JS/TS 的语法规则完全被 `base.yaml` 的通用规则覆盖，无需单独文件。这也是「通用规则 + 语言独有规则」架构的核心思路：**只在语言文件里放真正独有的规则**。

---

## 🎨 第一步：提取颜色变量

打开你原有的主题 JSON，找出所有 `colors` 对象中的颜色值以及 `tokenColors` 中反复出现的颜色，将它们定义为变量。创建 `src/core/colors.yaml`：

```yaml
# 核心颜色变量
primary: "#3b82f6" # 主蓝
success: "#34d399" # 成功绿
warning: "#fbbf24" # 警告黄
error: "#f87171" # 错误红
highlight: "#7dd3fc" # 发光蓝

bg: "#0f172a" # 背景
bgMuted: "#1e293b" # 次级背景
text: "#e2e8f0" # 前景色
textMuted: "#94a3b8" # 辅助文字
border: "#2d3748" # 边框


# ... 其他变量
```

**⚠️ 重要规则**：

- 变量名使用驼峰或小写连字符，并且**只使用字母、数字和下划线**。
- **变量中不要包含透明度**（如 `#3b82f620` 中的 `20`），透明度应通过后缀 `${primary}20` 在引用时添加，构建脚本会自动拼接。
- 确保 `colors.yaml` 覆盖了所有将在其他文件中引用的变量，否则构建时会警告并保留原样。

---

## ✂️ 第二步：拆分语法规则

将 `tokenColors` 数组按语言拆分为多个 YAML 文件。以 `src/languages/base.yaml` 为例，存放所有语言共用的规则：

```yaml
# 通用规则（base.yaml）
tokenColors:
  - name: Comment
    scope: ["comment", "punctuation.definition.comment"]
    settings:
      fontStyle: "italic"
      foreground: "${comment}"

  - name: Keyword
    scope: ["keyword", "storage.type", "storage.modifier"]
    settings:
      foreground: "${primary}"
      fontStyle: "bold"

  - name: String
    scope: ["string", "string.quoted.single", "string.quoted.double"]
    settings:
      foreground: "${success}"
  # ... 其他通用规则
```

**核心原则**：`base.yaml` 已有的通用规则（关键字、字符串、注释、操作符、变量等），语言文件**不再重复定义**。语言文件只放真正属于该语言的独有规则。

以 `src/languages/go.yaml` 为例，只包含 Go 的特有语法元素：

```yaml
# Go 专用规则（go.yaml）
tokenColors:
  - name: Go Package Clause
    scope: ["source.go keyword.package.go"]
    settings:
      foreground: "${primary}"
      fontStyle: "bold"

  - name: Go Struct
    scope: ["keyword.struct.go"]
    settings:
      foreground: "${warning}"
      fontStyle: "bold"

  - name: Go Error Variable
    scope: ["variable.other.object.err.go"]
    settings:
      foreground: "${error}"
      fontStyle: "italic"
  # ... 其他 Go 独有规则
```

这种「通用 + 独有」的架构让每个文件的职责都非常清晰：

- 修改通用配色 → 只改 `base.yaml` 一处，全局生效。
- 为 Go 添加独有规则 → 只动 `go.yaml`，不影响其他语言。
- 想了解某语言的完整配色 → 看 `base.yaml` + 对应语言文件即可。

---

## 🧩 第三步：拆分 UI 颜色和语义规则

将 `colors` 对象移到 `src/workbench.yaml`，并将所有颜色值替换为变量引用：

```yaml
# 编辑器 UI 颜色（workbench.yaml）
editor.background: "${bg}"
editor.foreground: "${text}"
titleBar.activeBackground: "${bg}"
titleBar.activeForeground: "${text}"
statusBar.background: "${bg}"
statusBar.foreground: "${textMuted}"
# ... 所有 UI 键
```

将 `semanticTokenColors` 移到 `src/semantic.yaml`，同样使用变量：

```yaml
# 语义高亮规则（semantic.yaml）
variable: "${variable}"
function: "${function}"
class: "${warning}"
"*.decorator":
  foreground: "${purple}"
  fontStyle: "italic"
# ... 其他语义规则
```

**🔍 注意**：在 YAML 中，键名如果包含特殊字符（如 `*.decorator`）必须用双引号括起来，否则 YAML 解析会失败。

---

## 🔨 第四步：编写构建脚本

创建 `scripts/build.js`，使用 **ESM（ES Module）** 语法。它的任务是：

1. 加载 `colors.yaml` 获得变量字典。
2. 加载 `workbench.yaml`、`semantic.yaml` 以及 `languages/` 下的所有语言规则。
3. 递归替换所有 `${变量名}` 为实际颜色值（支持透明度后缀）。
4. 合并 `tokenColors`（`base.yaml` 先合并、语言规则后合并，后面的规则拥有更高优先级）。
5. 输出最终的 JSON 到 `themes/`。

```javascript
// scripts/build.js
import fs from "node:fs"
import path from "node:path"
import yaml from "js-yaml"
import { fileURLToPath } from "node:url"

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const ROOT_DIR = path.resolve(__dirname, "..")

// 路径配置（根据你的项目结构调整）
const PATHS = {
  colors: path.join(ROOT_DIR, "src", "core", "colors.yaml"),
  workbench: path.join(ROOT_DIR, "src", "workbench.yaml"),
  semantic: path.join(ROOT_DIR, "src", "semantic.yaml"),
  langDir: path.join(ROOT_DIR, "src", "languages"),
  outputDir: path.join(ROOT_DIR, "themes"),
}

// 加载颜色变量
const colors = yaml.load(fs.readFileSync(PATHS.colors, "utf8"))

// 递归替换 `${var}`，支持两位十六进制透明度后缀（如 ${primary}20）
function replaceVariables(obj) {
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
  if (Array.isArray(obj)) {
    return obj.map(replaceVariables)
  }
  if (obj && typeof obj === "object") {
    const result = {}
    for (const [k, v] of Object.entries(obj)) {
      result[k] = replaceVariables(v)
    }
    return result
  }
  return obj
}

// 读取并解析 YAML
function loadYaml(filePath, description) {
  try {
    return yaml.load(fs.readFileSync(filePath, "utf8"))
  } catch (err) {
    console.error(`❌ 解析 ${description} 失败:`, err.message)
    return null
  }
}

// 加载 UI 颜色与语义规则
const workbench = replaceVariables(loadYaml(PATHS.workbench, "workbench.yaml"))
const semantic = replaceVariables(loadYaml(PATHS.semantic, "semantic.yaml"))

// 自动扫描 languages/ 目录下所有 YAML 文件，按文件名排序合并
// base.yaml 会被优先加载（作为通用规则），
// 语言专属规则随后合并（覆盖通用规则中相同 scope 的规则）
let tokenColors = []

if (fs.existsSync(PATHS.langDir)) {
  const langFiles = fs
    .readdirSync(PATHS.langDir)
    .filter((f) => f.endsWith(".yaml"))
    .sort() // base.yaml 按字母序排在最前

  for (const file of langFiles) {
    const rules = loadYaml(path.join(PATHS.langDir, file), `语言规则 ${file}`)
    if (rules?.tokenColors) {
      tokenColors = tokenColors.concat(rules.tokenColors)
      console.log(`   ✅ 已加载: ${file}`)
    }
  }
}

// 替换 tokenColors 中的变量
const processedTokenColors = replaceVariables(tokenColors)

// 确保输出目录存在
if (!fs.existsSync(PATHS.outputDir)) {
  fs.mkdirSync(PATHS.outputDir, { recursive: true })
}

// 构建最终主题对象
const theme = {
  name: "Your Theme Name",
  type: "dark",
  colors: workbench,
  tokenColors: processedTokenColors,
  semanticTokenColors: semantic,
}

// 写入文件
const outputFile = path.join(PATHS.outputDir, "your-theme.json")
fs.writeFileSync(outputFile, JSON.stringify(theme, null, 2))
console.log("✅ 主题构建完成！")
```

**⚠️ 注意事项**：

- **自动扫描而非手动排列**：与过去手动维护 `order` 数组不同，这里直接扫描 `languages/` 目录并按文件名排序。`base.yaml` 按字母序天然排在最前，其余语言文件按字母序排在其后——后合并的规则覆盖前面相同 scope 的规则，语言专属规则天然拥有更高优先级。新增语言时只需添加 YAML 文件，无需修改构建脚本。
- 如果颜色变量未定义，脚本会给出警告，生成的 JSON 中会保留 `${var}` 占位符，导致主题无效。务必确保所有变量均已定义。
- **透明度后缀格式**：透明度后缀使用两位十六进制数（00–FF），其中 `20` 对应约 12.5% 透明度，`80` 对应 50%，`FF` 对应完全不透明。这种表示法直接对应 CSS 的 `#RRGGBBAA` 格式，便于构建脚本直接拼接。
- 变量名仅使用字母、数字和下划线（如 `primary`、`primaryColor`），避免使用连字符或其他符号——因为正则表达式 `\$\{([a-zA-Z0-9_-]+)\}` 只匹配这些字符。

---

## ⚙️ 第五步：集成到 package.json

在 `package.json` 的 `scripts` 中添加构建命令，并设置 `prepublishOnly` 自动构建：

```json
{
  "scripts": {
    "build": "node scripts/build.js",
    "prepublishOnly": "npm run build"
  }
}
```

🔍 检查：确保 `js-yaml` 在 `devDependencies` 中，而不是 `dependencies`。因为它是构建工具，不应随主题发布。

---

## ✨ 开发体验优化：实时预览与自动构建

手动运行 `npm run build` 每次修改后都很繁琐。我们可以添加一个 **watch 模式**，让构建脚本在源码文件变化时自动执行，实现「修改即预览」的高效工作流。

### 1. 安装 nodemon

`nodemon` 是一个常用的工具，可以监视文件变化并自动重启命令。将它安装为开发依赖：

```bash
pnpm add -D nodemon
# 或
npm install --save-dev nodemon
```

### 2. 添加 watch 脚本

在 `package.json` 的 `scripts` 中添加以下两个命令：

```json
{
  "scripts": {
    "build": "node scripts/build.js",
    "watch": "nodemon --watch src -e yaml --exec \"npm run build\"",
    "dev": "npm run watch",
    "prepublishOnly": "npm run build"
  }
}
```

- `--watch src`：监视 `src` 目录下的所有文件变化。
- `-e yaml`：只监视扩展名为 `.yaml` 的文件。
- `--exec "npm run build"`：文件变化时执行构建命令。

`dev` 脚本是 `watch` 的别名，方便记忆。

### 3. 使用 watch 模式

在开发过程中，打开终端运行：

```bash
npm run watch
# 或
npm run dev
```

终端会保持运行状态，每当你在 `src/` 下修改并保存任何 YAML 文件时，构建脚本会自动执行，重新生成 `themes/` 下的 JSON 文件。

配合 VS Code 的调试功能，你只需按 `F5` 启动扩展开发宿主，然后保持 watch 运行。修改源码后，在开发宿主中执行 `Developer: Reload Window` 即可立即看到效果，无需手动重新构建。

### 4. 注意事项

- `nodemon` 只是开发时的辅助工具，不需要随主题发布，因此务必安装在 `devDependencies` 中。
- 如果你的项目结构复杂，可以自定义 `--watch` 参数监视更多目录。
- 如果不想安装额外依赖，也可以使用 Node.js 自带的 `fs.watch` 编写简单的监视脚本，但 `nodemon` 更成熟易用。

---

## 📦 第六步：更新 .vscodeignore

确保发布时只包含最终产物，不包含源码和依赖。一个典型的 `.vscodeignore` 内容如下：

```bash
.vscode/**
.gitignore
vsc-extension-quickstart.md
node_modules
pnpm-lock.yaml
src/**
scripts/**
!themes/*.json
```

💡 说明：`!themes/*.json` 表示保留 `themes` 目录下的所有 JSON 文件，这些是构建产物。

---

## ✅ 第七步：测试构建

运行以下命令，检查生成的 JSON 是否与原文件一致：

```bash
npm run build
```

然后用 diff 工具对比新生成的 `themes/your-theme.json` 与原始 JSON，确保没有意外差异。如果有差异，请检查：

- 变量定义是否完整。
- 透明度后缀是否正确。
- 语言规则合并顺序是否符合预期。

---

## 🚀 第八步：享受工程化带来的便利

现在你的主题源码已经模块化，维护变得轻而易举：

- 想修改主色？只需改 `colors.yaml` 一处。
- 想为 Python 添加新规则？直接在 `python.yaml` 中增加条目。
- 想创建浅色版本？新建 `colors-light.yaml`，并调整构建脚本输出两个主题。

---

## 🔍 常见问题排查

| 问题                                        | 可能原因                                                            | 解决方法                                                                                            |
| ------------------------------------------- | ------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- |
| 构建后颜色值仍为 `${var}`                   | 变量未在 `colors.yaml` 中定义                                       | 检查变量名拼写，确保变量已定义                                                                      |
| 透明度不正确                                | 变量本身已包含 alpha，或透明度后缀格式错误                          | 变量中不应包含透明度，使用 `${var}20` 形式                                                          |
| 某语言高亮缺失                              | 语言规则文件不存在，或规则不在 `base.yaml` 通用范围内               | 添加对应语言文件；注意语言文件只放独有规则，通用规则放 `base.yaml`                                  |
| 规则被意外覆盖                              | 合并顺序不符合预期                                                  | 文件名排序控制顺序：`base.yaml` 排最前，语言文件按字母序在后，后合并的规则覆盖前面的相同 scope 规则 |
| `vsce package` 报错「missing dependencies」 | `js-yaml` 被放在了 `dependencies` 中                                | 将其移到 `devDependencies`                                                                          |
| 透明度后缀格式不支持三位（如 `200`）        | 脚本仅支持两位十六进制透明度后缀（如 `20`）                         | 确保透明度后缀始终为两位十六进制，并在引用时使用 `${var}20` 形式                                    |
| 变量名包含连字符或非单词字符导致无法替换    | 变量命名不规范，正则表达式 `\$\{(\w+)\}` 只能匹配字母、数字和下划线 | 变量名仅使用字母、数字和下划线（如 `primary`、`primaryColor`），避免使用连字符或其他符号            |

---

## 📝 总结

通过工程化重构，你从一个难以维护的 JSON 单体进化到了一个清晰、可扩展的模块化项目。你已经拥有了：

- ✅ 模块化的 YAML 源文件（颜色、语言规则、UI、语义高亮分离）
- ✅ 自动合并与变量替换的构建脚本
- ✅ watch 模式实时预览开发
- ✅ 发布时自动构建的正确配置

但你可能已经注意到，上一篇文章的方案还有一些问题需要解决：

- `colors.yaml` 是一个**扁平的变量池**，颜色之间的层级关系（哪些是原始色、哪些是语义角色）完全靠命名约定，没有结构性的约束。
- 深浅两套主题需要**两套颜色变量文件**，而「同一角色在深色和浅色下应该保持色相一致、明度不同」这件事完全靠手动维护。
- 构建脚本只能做变量替换，还**不能自动校验**颜色是否符合对比度标准、是否引用了不存在的变量。

这些问题，正是我们下一篇要解决的——**设计系统：DTCG 三层架构与昼夜双变体**。

[**设计系统：DTCG 三层架构与昼夜双变体**](./create-vscode-theme-design-system)
