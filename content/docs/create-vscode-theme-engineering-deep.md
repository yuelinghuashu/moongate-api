---
title: 工程深化篇：工业级构建脚本与 DTCG 完整实现
description: 深入工业级构建脚本，掌握颜色标准化、WCAG 对比度校验、循环引用检测、自动生成 CSS 变量和设计系统文档，将 DTCG 设计令牌从概念落地为可运行的工程代码。
date: 2026-03-28
permalink: ef9e2761-be41-4778-8552-22cb86ed3407
level: P3
series: design-system
tags: 
  - VSCode
  - Theme
  - Design System
  - Engineering
---

## 📚 系列导航

本系列共五篇，覆盖从零基础创建到工业级设计系统的 VS Code 主题开发全流程：

1. [**从零到第一个 VS Code 主题：完全入门指南（入门篇）**](./create-vscode-theme-basics)
   —— 创建、配置并发布你的第一个 VS Code 主题，掌握核心概念与基础工作流。

2. [**告别手改 JSON：用 YAML 工程化你的主题（工程篇）**](./create-vscode-theme-engineering)
   —— 将零散的 JSON 主题重构为模块化 YAML 项目，用构建脚本实现自动化生成。

3. [**亮色、暗色与更多：多主题变体与重力补偿（多主题篇）**](./create-vscode-theme-multi-theme)
   —— 同时构建暗色、亮色和高对比度主题变体，用重力补偿原理实现视觉重量平衡。

4. [**从主题到设计系统：视觉契约与品牌生态（设计系统篇）**](./create-vscode-theme-design-system)
   —— 为你的主题赋予设计哲学、视觉契约和品牌生态，打造完整的设计系统。

5. [**工业级构建脚本与 DTCG 实现（工程进阶篇）**](./create-vscode-theme-engineering-deep)
   —— DTCG 三层架构、颜色归一化、WCAG 对比度检测、循环引用检测，自动生成 CSS 变量与设计文档。

---

## 🚀 引言

在系统篇中，我们介绍了 DTCG 设计令牌的三层架构和工业级质检概念。本篇将把这些概念**落地为可运行的工程代码**，完整呈现 Moongate v2.2.0 的工业级构建脚本。

如果你已经完成了进阶篇的基础构建脚本，并希望进一步升级到工业级水准——支持颜色标准化、WCAG 对比度校验、循环引用检测、自动生成 CSS 变量和设计系统文档，那么本篇正是为你准备的。

> 💡 **提示**：本篇内容涉及较深的工程实践，建议先完成前三篇（基础篇、进阶篇、扩展篇）再阅读。

---

## 📁 目录结构

```text
your-theme/
├── src/
│   ├── core/
│   │   ├── primitives/
│   │   │   └── colors.yaml          # 原始色值
│   │   ├── semantics/
│   │   │   ├── dark.yaml            # 深色语义层
│   │   │   └── light.yaml           # 浅色语义层
│   │   └── layout.yaml              # 布局令牌（间距、排版、断点等）
│   ├── languages/                   # 各语言语法规则
│   ├── workbench.yaml               # UI 颜色
│   └── semantic.yaml                # 语义高亮规则
├── scripts/
│   └── build.js                     # 工业级构建脚本
├── themes/
│   ├── moongate-dark.json           # 生成的主题 JSON
│   ├── moongate-light.json
│   ├── moongate-colors.css          # 颜色令牌（自动生成）
│   └── moongate-layout.css          # 布局令牌（自动生成）
├── docs/
│   └── DESIGN_SYSTEM.md             # 自动生成的设计系统文档
└── package.json
```

---

## 🎨 第一步：原始值文件

**`src/core/primitives/colors.yaml`**

<details>
<summary>查看完整原始值文件</summary>

```yaml
# ==================== 原始色值 ====================
# 按色相-明度命名，作为所有主题的基础构建块

# 蓝色系
blue-500: "#3b82f6"
blue-600: "#2563eb"
blue-700: "#0284c7"
blue-800: "#0369a1"
blue-900: "#1e3a8a"
blue-glow: "#7dd3fc"
blue-glow-dark: "#87cefa"

# 绿色系
green-400: "#34d399"
green-600: "#059669"
green-700: "#10b981"

# 黄色系
yellow-400: "#fbbf24"
yellow-500: "#f59e0b"
yellow-600: "#d97706"
yellow-700: "#b45309"

# 红色系
red-400: "#f87171"
red-500: "#ef4444"
red-600: "#dc2626"
red-700: "#b91c1c"

# 青色系
cyan-400: "#22d3ee"
cyan-500: "#0891b2"
cyan-700: "#0e7490"

# 紫色系
purple-400: "#c084fc"
purple-500: "#9333ea"
purple-700: "#7e22ce"

# 灰阶
gray-900: "#0f172a"
gray-850: "#131c31"
gray-800: "#1e293b"
gray-750: "#252e40"
gray-700: "#2d3748"
gray-600: "#475569"
gray-550: "#7a8c9e"
gray-500: "#94a3b8"
gray-525: "#a5b4cb"
gray-400: "#94a3b8"
gray-300: "#cbd5e1"
gray-200: "#e2e8f0"
gray-100: "#f1f5f9"
gray-50: "#f9fafb"

# 纯色
white: "#ffffff"
black: "#000000"
```

</details>

---

## 🌙 第二步：语义层文件

### 深色语义层 `src/core/semantics/dark.yaml`

<details>
<summary>查看完整深色语义层</summary>

```yaml
# ==================== 深色语义层 ====================

primary: "{blue-500}"
success: "{green-400}"
warning: "{yellow-400}"
error: "{red-400}"
highlight: "{blue-glow}"
cyan: "{cyan-400}"
purple: "{purple-400}"

function: "{blue-glow-dark}"
operator: "{gray-600}"
comment: "{gray-525}"
variable: "{gray-200}"
variableDim: "{gray-300}"
textMuted: "{gray-400}"
punctuation: "{gray-400}"

bg: "{gray-900}"
bgElevated: "{gray-850}"
bgMuted: "{gray-800}"
bgHover: "{blue-500}20"
bgActive: "{blue-500}40"
hoverBg: "{gray-750}"
selectedBg: "{blue-600}"

surfaceGround: "{gray-900}"
surfaceRaised: "{gray-850}"
surfaceFloating: "{gray-800}"
surfaceTooltip: "{gray-750}"
borderFloating: "{blue-500}40"

text: "{gray-200}"
textDim: "{gray-300}"
textInactive: "{gray-400}"

border: "{gray-700}"
borderHover: "{blue-500}"
borderDim: "{gray-600}"

buttonHoverBg: "{blue-600}"
white: "{white}"

ansiBlack: "{gray-800}"
ansiRed: "{red-400}"
ansiGreen: "{green-400}"
ansiYellow: "{yellow-400}"
ansiBlue: "{blue-500}"
ansiMagenta: "{purple-400}"
ansiCyan: "{cyan-400}"
ansiWhite: "{gray-200}"
ansiBrightBlack: "{gray-700}"
ansiBrightRed: "{red-400}"
ansiBrightGreen: "{green-400}"
ansiBrightYellow: "{yellow-400}"
ansiBrightBlue: "{blue-500}"
ansiBrightMagenta: "{purple-400}"
ansiBrightCyan: "{cyan-400}"
ansiBrightWhite: "{white}"

bracket1: "{blue-glow}"
bracket2: "{green-400}"
bracket3: "{yellow-400}"
bracket4: "{purple-400}"
bracket5: "{blue-500}"
bracket6: "{gray-400}"

scrollbar: "{blue-500}"
gitAdded: "{green-400}"
gitModified: "{yellow-400}"
gitDeleted: "{red-400}"
gitUntracked: "{gray-400}"
gitIgnored: "{gray-700}"
debugStart: "{green-400}"
debugPause: "{yellow-400}"
debugStop: "{red-400}"
```

</details>

### 浅色语义层 `src/core/semantics/light.yaml`

<details>
<summary>查看完整浅色语义层</summary>

```yaml
# ==================== 浅色语义层 ====================

primary: "{blue-700}"
success: "{green-600}"
warning: "{yellow-700}"
error: "{red-700}"
highlight: "{blue-800}"
cyan: "{cyan-700}"
purple: "{purple-700}"

function: "{blue-800}"
operator: "{gray-600}"
comment: "{gray-600}"
variable: "{gray-900}"
variableDim: "{gray-600}"
textMuted: "{gray-600}"
punctuation: "{gray-400}"

bg: "{gray-50}"
bgElevated: "{white}"
bgMuted: "{gray-100}"
bgHover: "{blue-700}15"
bgActive: "{blue-700}25"
hoverBg: "{gray-100}"
selectedBg: "{gray-300}"

surfaceGround: "{gray-50}"
surfaceRaised: "{white}"
surfaceFloating: "{gray-100}"
surfaceTooltip: "{gray-200}"
borderFloating: "{blue-700}80"

text: "{gray-900}"
textDim: "{gray-600}"
textInactive: "{gray-400}"

border: "{gray-300}"
borderHover: "{blue-700}"
borderDim: "{gray-400}"

buttonHoverBg: "{blue-700}"
white: "{white}"

ansiBlack: "{gray-900}"
ansiRed: "{red-700}"
ansiGreen: "{green-600}"
ansiYellow: "{yellow-700}"
ansiBlue: "{blue-700}"
ansiMagenta: "{purple-700}"
ansiCyan: "{cyan-700}"
ansiWhite: "{gray-200}"
ansiBrightBlack: "{gray-500}"
ansiBrightRed: "{red-400}"
ansiBrightGreen: "{green-400}"
ansiBrightYellow: "{yellow-400}"
ansiBrightBlue: "{blue-600}"
ansiBrightMagenta: "{purple-400}"
ansiBrightCyan: "{cyan-400}"
ansiBrightWhite: "{white}"

bracket1: "{blue-700}"
bracket2: "{green-600}"
bracket3: "{yellow-700}"
bracket4: "{purple-700}"
bracket5: "{cyan-700}"
bracket6: "{gray-500}"

scrollbar: "{blue-700}"
gitAdded: "{green-600}"
gitModified: "{yellow-700}"
gitDeleted: "{red-700}"
gitUntracked: "{gray-500}"
gitIgnored: "{gray-400}"
debugStart: "{green-600}"
debugPause: "{yellow-700}"
debugStop: "{red-700}"
```

</details>

---

## 📐 第三步：布局令牌文件

**`src/core/layout.yaml`**

<details>
<summary>查看完整布局令牌文件</summary>

```yaml
# ==================== 布局令牌 ====================
# 包含间距、圆角、阴影、排版、响应式断点、Z-Index

spacing:
  base: "4px"
  xs: "4px"
  sm: "8px"
  md: "16px"
  lg: "24px"
  xl: "32px"
  "2xl": "48px"

radius:
  none: "0px"
  sm: "0px"
  md: "0px"
  lg: "0px"

shadow:
  none: "none"
  border: "0 0 0 1px"

typography:
  family-mono: "'JetBrains Mono', 'Fira Code', monospace"
  family-sans: "Inter, system-ui, -apple-system, sans-serif"
  size-code: "13px"
  size-body: "14px"
  size-small: "12px"
  line-height: "1.5"

breakpoints:
  mobile: "640px"
  tablet: "768px"
  desktop: "1024px"
  wide: "1280px"

z-index:
  base: 1
  sticky: 100
  overlay: 500
  modal: 1000
  tooltip: 1500
```

</details>

---

## 🛠️ 第四步：工业级构建脚本

**`scripts/build.js`**

<details>
<summary>查看完整工业级构建脚本</summary>

```javascript
const fs = require("fs");
const yaml = require("js-yaml");
const path = require("path");
const wcag = require("wcag-contrast");

const ROOT_DIR = path.resolve(__dirname, "..");

// ==================== 路径配置 ====================
const PATHS = {
  primitives: path.join(ROOT_DIR, "src", "core", "primitives", "colors.yaml"),
  semanticsDir: path.join(ROOT_DIR, "src", "core", "semantics"),
  layout: path.join(ROOT_DIR, "src", "core", "layout.yaml"),
  workbench: path.join(ROOT_DIR, "src", "workbench.yaml"),
  semantic: path.join(ROOT_DIR, "src", "semantic.yaml"),
  langDir: path.join(ROOT_DIR, "src", "languages"),
  specialDir: path.join(ROOT_DIR, "src", "special"),
  outputDir: path.join(ROOT_DIR, "themes"),
  docsDir: path.join(ROOT_DIR, "docs"),
};

// ==================== 辅助函数 ====================

function ensureFileExists(filePath, description) {
  if (!fs.existsSync(filePath)) {
    throw new Error(`❌ 未找到 ${description} 文件: ${filePath}`);
  }
}

function safeLoadYaml(filePath, description) {
  try {
    return yaml.load(fs.readFileSync(filePath, "utf8"));
  } catch (err) {
    console.error(`❌ 解析 ${description} 失败 (${filePath}):`, err.message);
    return null;
  }
}

/**
 * 十六进制颜色标准化
 */
function normalizeHex(color, tokenName) {
  if (typeof color !== "string" || !color.startsWith("#")) {
    if (color && !color.startsWith("#")) {
      console.warn(`⚠️ 跳过非十六进制色值: ${tokenName} = ${color}`);
    }
    return color;
  }

  let hex = color.replace("#", "");

  if (hex.length === 3) {
    hex = hex
      .split("")
      .map((c) => c + c)
      .join("");
  } else if (hex.length === 4) {
    hex = hex
      .split("")
      .map((c) => c + c)
      .join("");
  }

  if (!/^[0-9a-fA-F]{6}$|^[0-9a-fA-F]{8}$/.test(hex)) {
    console.error(
      `❌ 致命错误: 令牌 "${tokenName}" 的色值 "${color}" 不符合工业规范。`,
    );
    console.error(`   要求: 6 位 (#RRGGBB) 或 8 位 (#RRGGBBAA) 十六进制`);
    process.exit(1);
  }

  return `#${hex.toLowerCase()}`;
}

/**
 * 递归解析令牌引用 {token-name}
 */
function resolveTokens(obj, tokenMap, depth = 0, path = []) {
  const MAX_DEPTH = 20;
  if (depth > MAX_DEPTH) {
    throw new Error(
      `[ENGINEERING_FATAL] 令牌循环引用检测: ${path.join(" → ")}`,
    );
  }

  if (typeof obj === "string") {
    const resolveOne = (str) => {
      return str.replace(/\{([a-zA-Z0-9_-]+)\}/g, (match, key) => {
        const value = tokenMap[key];
        if (value === undefined) {
          console.warn(`⚠️ 警告: 令牌 "${key}" 未定义，保留原样`);
          return match;
        }
        return resolveOne(value);
      });
    };
    return resolveOne(obj);
  }
  if (Array.isArray(obj)) {
    return obj.map((item) => resolveTokens(item, tokenMap, depth + 1, path));
  }
  if (obj && typeof obj === "object") {
    const result = {};
    for (const [k, v] of Object.entries(obj)) {
      result[k] = resolveTokens(v, tokenMap, depth + 1, [...path, k]);
    }
    return result;
  }
  return obj;
}

/**
 * 标准化所有颜色值
 */
function normalizeColors(obj, tokenName) {
  if (typeof obj === "string") {
    return normalizeHex(obj, tokenName);
  }
  if (Array.isArray(obj)) {
    return obj.map((item) => normalizeColors(item, tokenName));
  }
  if (obj && typeof obj === "object") {
    const result = {};
    for (const [k, v] of Object.entries(obj)) {
      result[k] = normalizeColors(v, `${tokenName}.${k}`);
    }
    return result;
  }
  return obj;
}

/**
 * 检测是否直接引用了原始值（如 {blue-500}）
 * 该函数用于组件层/语义层的原始值引用提醒，目前主要作为架构辅助。
 */
function detectPrimitiveReference(value, context) {
  if (typeof value === "string" && /\{([a-zA-Z0-9_-]+)\}/.test(value)) {
    const match = value.match(/\{([a-zA-Z0-9_-]+)\}/)[1];
    const primitivePrefixes = [
      "blue-",
      "green-",
      "yellow-",
      "red-",
      "cyan-",
      "purple-",
      "gray-",
      "white",
      "black",
    ];
    if (primitivePrefixes.some((prefix) => match.startsWith(prefix))) {
      console.warn(
        `[架构提醒] ${context} 中直接引用了原始值 "${match}"，建议通过语义层引用。`,
      );
    }
  }
}

/**
 * 替换变量 ${var} 为最终色值
 * 透明度后缀说明：透明度后缀使用两位十六进制数（00–FF），例如 20 对应约 12.5% 透明度，
 * 80 对应 50%，FF 对应完全不透明。这种表示法直接对应 CSS 的 #RRGGBBAA 格式。
 */
function replaceVariables(obj, colors, context = "") {
  if (typeof obj === "string") {
    if (context) detectPrimitiveReference(obj, context);

    return obj.replace(
      /\$\{([a-zA-Z0-9_-]+)\}([0-9a-fA-F]{2})?/g,
      (match, key, alpha) => {
        const value = colors[key];
        if (value === undefined) {
          console.warn(`⚠️ 警告: 变量 "${key}" 未定义，保留原样`);
          return match;
        }
        if (alpha) {
          if (/^rgba?\(/.test(value)) {
            console.warn(
              `⚠️ 警告: 变量 "${key}" 值 ${value} 已是 rgba 格式，忽略后缀`,
            );
            return value;
          }
          if (/^#[0-9a-fA-F]{8}$/.test(value)) {
            console.warn(
              `⚠️ 警告: 变量 "${key}" 值 ${value} 已包含透明度，忽略后缀 "${alpha}"`,
            );
            return value;
          }
          if (/^#[0-9a-fA-F]{6}$/.test(value)) {
            return value + alpha;
          }
          console.warn(
            `⚠️ 警告: 变量 "${key}" 值 ${value} 格式异常，无法处理透明度`,
          );
          return value;
        }
        return value;
      },
    );
  }
  if (Array.isArray(obj)) {
    return obj.map((item) => replaceVariables(item, colors, context));
  }
  if (obj && typeof obj === "object") {
    const result = {};
    for (const [k, v] of Object.entries(obj)) {
      result[k] = replaceVariables(v, colors, context);
    }
    return result;
  }
  return obj;
}

/**
 * WCAG 对比度校验（阶梯式标准）
 */
function checkContrast(color1, color2, role, themeType) {
  if (!color1 || !color2) return;
  const ratio = wcag.hex(color1, color2);

  let minRatio = 4.5;
  if (role === "textDim" || role === "comment") {
    minRatio = 4.0;
  }
  if (role === "textMuted") {
    minRatio = 3.0;
  }

  if (ratio < minRatio) {
    if (role === "textMuted") {
      console.warn(
        `⚠️ 对比度略低: ${themeType} · ${role} (${color1}) vs 背景 (${color2}) = ${ratio.toFixed(2)}:1`,
      );
      console.warn(`   建议保持 ≥3.0:1，当前满足最低要求。`);
    } else {
      console.error(
        `❌ 对比度不足: ${themeType} · ${role} (${color1}) vs 背景 (${color2}) = ${ratio.toFixed(2)}:1`,
      );
      console.error(`   WCAG 要求 ≥${minRatio}:1，当前值低于标准`);
      process.exit(1);
    }
  } else {
    console.log(`✅ ${themeType} · ${role}: ${ratio.toFixed(2)}:1`);
  }
}

/**
 * 生成颜色 CSS 变量文件（包含深浅模式）
 */
function generateColorCss(lightColors, darkColors) {
  let css = `/* ===== Moongate 颜色令牌 - 自动生成 ===== */\n`;
  css += `/* 来源: VS Code 主题构建脚本 */\n`;
  css += `/* 请勿手动修改，修改请编辑 primitives/ 和 semantics/ 目录 */\n\n`;

  css += `/* 浅色模式 */\n:root,\n.light {\n`;
  Object.entries(lightColors).forEach(([key, val]) => {
    const cssKey = key.replace(/([A-Z])/g, "-$1").toLowerCase();
    css += `  --ui-${cssKey}: ${val};\n`;
  });
  css += `}\n\n`;

  css += `/* 深色模式 */\n.dark {\n`;
  Object.entries(darkColors).forEach(([key, val]) => {
    const cssKey = key.replace(/([A-Z])/g, "-$1").toLowerCase();
    css += `  --ui-${cssKey}: ${val};\n`;
  });
  css += `}\n`;

  const cssPath = path.join(PATHS.outputDir, "moongate-colors.css");
  fs.writeFileSync(cssPath, css);
  console.log(`✅ 颜色令牌已生成: ${cssPath}`);
}

/**
 * 生成布局/排版/响应式 CSS 变量文件（支持嵌套对象）
 */
function generateLayoutCss(layoutTokens) {
  let css = `/* ===== Moongate 布局令牌 - 自动生成 ===== */\n`;
  css += `/* 包含：间距、圆角、阴影、排版、响应式断点、Z-Index */\n`;
  css += `/* 请勿手动修改，修改请编辑 src/core/layout.yaml */\n\n`;

  css += `:root {\n`;

  function flattenObject(obj, prefix = "") {
    Object.entries(obj).forEach(([key, val]) => {
      const fullKey = prefix ? `${prefix}-${key}` : key;
      if (val && typeof val === "object" && !Array.isArray(val)) {
        flattenObject(val, fullKey);
      } else {
        let formattedVal = val;
        if (typeof formattedVal === "string") {
          if (
            (formattedVal.startsWith("'") && formattedVal.endsWith("'")) ||
            (formattedVal.startsWith('"') && formattedVal.endsWith('"'))
          ) {
            formattedVal = formattedVal.slice(1, -1);
          }
        }
        css += `  --ui-${fullKey}: ${formattedVal};\n`;
      }
    });
  }

  flattenObject(layoutTokens);

  css += `}\n`;

  const cssPath = path.join(PATHS.outputDir, "moongate-layout.css");
  fs.writeFileSync(cssPath, css);
  console.log(`✅ 布局令牌已生成: ${cssPath}`);
}

/**
 * 生成设计系统文档（增强版）
 * 包含变量选择协议、原始色值、海拔系统、语义层对比度
 */
function generateDesignSystemDoc(primitives, lightColors, darkColors) {
  const md = [];

  md.push("# Moongate 设计系统\n");
  md.push("## 🧭 Moongate 变量选择协议\n");
  md.push("为了确保设计系统的长期可维护性和语义一致性，请遵循以下决策路径：\n");
  md.push("| 场景 | 查找位置 | 禁止行为 |");
  md.push("|------|----------|----------|");
  md.push(
    "| **我需要定义新的基础色值**（如 `blue-600`） | `primitives/colors.yaml` | ❌ 不要在语义层或组件层直接写色值 |",
  );
  md.push(
    "| **我需要给某个语义角色赋值**（如 `primary` 应该是什么颜色） | `semantics/*.yaml`（引用原始值） | ❌ 不要在组件层直接引用原始值 |",
  );
  md.push(
    "| **我要为 UI 组件设置样式**（如 `sideBar.background`） | 引用语义层变量（如 `${surfaceRaised}`） | ❌ 不要直接使用 `${blue-500}` 或硬编码色值 |",
  );
  md.push(
    "| **语义层缺少我需要的角色** | 在语义层新增一个逻辑角色（如 `actionHover`），再在组件中引用它 | ❌ 禁止在组件层发明新变量 |",
  );
  md.push(
    "\n> **核心原则**：所有颜色必须经过“原始值 → 语义层 → 组件层”的传递链条，任何跨层直接引用都是**架构污染**。\n",
  );

  md.push("## 🎨 原始色值\n");

  const colorGroups = {
    blue: [
      "blue-500",
      "blue-600",
      "blue-700",
      "blue-800",
      "blue-900",
      "blue-glow",
      "blue-glow-dark",
    ],
    green: ["green-400", "green-600", "green-700"],
    yellow: ["yellow-400", "yellow-500", "yellow-600", "yellow-700"],
    red: ["red-400", "red-500", "red-600", "red-700"],
    cyan: ["cyan-400", "cyan-500", "cyan-700"],
    purple: ["purple-400", "purple-500", "purple-700"],
    gray: Object.keys(primitives).filter((k) => k.startsWith("gray-")),
    special: ["white", "black"],
  };

  for (const [group, keys] of Object.entries(colorGroups)) {
    if (keys.length === 0) continue;
    md.push(`### ${group.charAt(0).toUpperCase() + group.slice(1)} 色系\n`);
    md.push("| 令牌 | 色值 | 预览 |");
    md.push("|------|------|------|");
    for (const key of keys) {
      if (!primitives[key]) continue;
      const val = primitives[key];
      const preview = `![](https://placehold.co/20x20/${val.slice(1)}/${val.slice(1)}?text=+)`;
      md.push(`| \`--moongate-${key}\` | \`${val}\` | ${preview} |`);
    }
    md.push("");
  }

  md.push("## 🏔️ 海拔系统（Elevation System）\n");
  md.push(
    "海拔系统通过明度差异表达 UI 元素的物理深度，遵循 Material Design 海拔规范。",
  );
  md.push("| 变量 | 浅色模式 | 深色模式 | 说明 |");
  md.push("|------|----------|----------|------|");
  md.push(
    `| \`surfaceGround\` | \`${lightColors.surfaceGround}\` | \`${darkColors.surfaceGround}\` | 地面层（0dp）—— 编辑器背景 |`,
  );
  md.push(
    `| \`surfaceRaised\` | \`${lightColors.surfaceRaised}\` | \`${darkColors.surfaceRaised}\` | 隆起层（2dp）—— 侧边栏、活动栏 |`,
  );
  md.push(
    `| \`surfaceFloating\` | \`${lightColors.surfaceFloating}\` | \`${darkColors.surfaceFloating}\` | 漂浮层（8dp）—— 弹窗、菜单 |`,
  );
  md.push(
    `| \`surfaceTooltip\` | \`${lightColors.surfaceTooltip}\` | \`${darkColors.surfaceTooltip}\` | 提示层（12dp）—— 工具提示 |`,
  );
  md.push(
    `| \`borderFloating\` | \`${lightColors.borderFloating}\` | \`${darkColors.borderFloating}\` | 浮层边框（半透明主色） |\n`,
  );

  const contrast = (color1, color2) => {
    if (!color1 || !color2) return null;
    try {
      return wcag.hex(color1, color2).toFixed(2);
    } catch {
      return null;
    }
  };

  md.push("## 🌙 浅色模式语义层\n");
  md.push("| 语义变量 | 色值 | 预览 | WCAG 对比度（vs `bg`） |");
  md.push("|----------|------|------|------------------------|");
  const lightBg = lightColors.bg;
  const lightImportantKeys = [
    "text",
    "textDim",
    "textMuted",
    "comment",
    "primary",
    "success",
    "warning",
    "error",
  ];
  for (const [key, val] of Object.entries(lightColors)) {
    const preview = `![](https://placehold.co/20x20/${val.slice(1)}/${val.slice(1)}?text=+)`;
    let contrastRatio = "-";
    if (lightImportantKeys.includes(key) && lightBg) {
      const ratio = contrast(val, lightBg);
      if (ratio) contrastRatio = `${ratio}:1`;
    }
    md.push(`| \`${key}\` | \`${val}\` | ${preview} | ${contrastRatio} |`);
  }

  md.push("\n## 🌑 深色模式语义层\n");
  md.push("| 语义变量 | 色值 | 预览 | WCAG 对比度（vs `bg`） |");
  md.push("|----------|------|------|------------------------|");
  const darkBg = darkColors.bg;
  for (const [key, val] of Object.entries(darkColors)) {
    const preview = `![](https://placehold.co/20x20/${val.slice(1)}/${val.slice(1)}?text=+)`;
    let contrastRatio = "-";
    if (lightImportantKeys.includes(key) && darkBg) {
      const ratio = contrast(val, darkBg);
      if (ratio) contrastRatio = `${ratio}:1`;
    }
    md.push(`| \`${key}\` | \`${val}\` | ${preview} | ${contrastRatio} |`);
  }

  const mdPath = path.join(PATHS.docsDir, "DESIGN_SYSTEM.md");
  if (!fs.existsSync(PATHS.docsDir)) {
    fs.mkdirSync(PATHS.docsDir, { recursive: true });
  }
  fs.writeFileSync(mdPath, md.join("\n"), "utf8");
  console.log(`✅ 设计系统文档已生成: ${mdPath}`);
}

function getThemeInfo() {
  const pkgPath = path.join(ROOT_DIR, "package.json");
  if (!fs.existsSync(pkgPath)) {
    return { name: "your-theme", displayName: "Your Theme" };
  }
  try {
    const pkg = JSON.parse(fs.readFileSync(pkgPath, "utf8"));
    let displayName = pkg.displayName || pkg.name || "Your Theme";
    if (displayName.startsWith("@") && displayName.includes("/")) {
      displayName = displayName.split("/")[1];
    }
    return {
      name: pkg.name || "your-theme",
      displayName,
    };
  } catch {
    return { name: "your-theme", displayName: "Your Theme" };
  }
}

// ==================== 主流程 ====================
function main() {
  console.log("🚀 开始构建主题 (DTCG 标准 + 工业级质检)...\n");

  try {
    ensureFileExists(PATHS.primitives, "原始值");
    ensureFileExists(PATHS.semanticsDir, "语义目录");
    ensureFileExists(PATHS.layout, "布局令牌");
    ensureFileExists(PATHS.workbench, "workbench");
    ensureFileExists(PATHS.semantic, "semantic");
  } catch (err) {
    console.error(err.message);
    process.exit(1);
  }

  console.log("📦 加载原始值...");
  const primitivesRaw = safeLoadYaml(PATHS.primitives, "primitives.yaml");
  if (!primitivesRaw) process.exit(1);

  const primitives = {};
  Object.entries(primitivesRaw).forEach(([key, val]) => {
    primitives[key] = normalizeHex(val, `primitives.${key}`);
  });

  console.log("📦 加载布局令牌...");
  const layoutTokens = safeLoadYaml(PATHS.layout, "layout.yaml");
  if (layoutTokens) {
    generateLayoutCss(layoutTokens);
  } else {
    console.error("❌ layout.yaml 加载失败，构建终止");
    process.exit(1);
  }

  console.log("📦 加载公共规则...");
  const workbenchRaw = safeLoadYaml(PATHS.workbench, "workbench.yaml");
  const semanticRaw = safeLoadYaml(PATHS.semantic, "semantic.yaml");
  if (!workbenchRaw || !semanticRaw) process.exit(1);

  console.log("📚 加载语言规则...");
  let tokenColorsRaw = [];
  if (fs.existsSync(PATHS.langDir)) {
    const langFiles = fs
      .readdirSync(PATHS.langDir)
      .filter((f) => f.endsWith(".yaml"))
      .sort();
    langFiles.forEach((file) => {
      const filePath = path.join(PATHS.langDir, file);
      const langRules = safeLoadYaml(filePath, `语言规则 ${file}`);
      if (langRules?.tokenColors) {
        tokenColorsRaw = tokenColorsRaw.concat(langRules.tokenColors);
        console.log(`   ✅ 已加载: ${file}`);
      }
    });
  }

  console.log("✨ 加载特殊规则...");
  if (fs.existsSync(PATHS.specialDir)) {
    const specialFiles = fs
      .readdirSync(PATHS.specialDir)
      .filter((f) => f.endsWith(".yaml"));
    specialFiles.forEach((file) => {
      const filePath = path.join(PATHS.specialDir, file);
      const specialRules = safeLoadYaml(filePath, `特殊规则 ${file}`);
      if (specialRules?.tokenColors) {
        tokenColorsRaw = tokenColorsRaw.concat(specialRules.tokenColors);
        console.log(`   ✅ 已加载: ${file}`);
      }
    });
  }

  console.log("\n🎨 扫描语义文件...");
  const semanticFiles = fs
    .readdirSync(PATHS.semanticsDir)
    .filter((f) => f.endsWith(".yaml"));
  if (semanticFiles.length === 0) {
    console.error("❌ semantics 目录下没有找到 .yaml 文件");
    process.exit(1);
  }

  const themeInfo = getThemeInfo();
  let baseName = themeInfo.name.replace(/[^a-z0-9-]/gi, "-").toLowerCase();

  let lightSemantics, darkSemantics;

  console.log(`\n🔨 开始构建主题...\n`);
  semanticFiles.forEach((semanticFile) => {
    const themeType = path.basename(semanticFile, ".yaml");
    const outputFile = path.join(
      PATHS.outputDir,
      `${baseName}-${themeType}.json`,
    );

    const semanticsPath = path.join(PATHS.semanticsDir, semanticFile);
    const semantics = safeLoadYaml(semanticsPath, `语义层 ${semanticFile}`);
    if (!semantics) {
      console.error(`   ❌ 跳过 ${semanticFile}`);
      return;
    }

    const resolved = resolveTokens(semantics, primitives);
    const normalized = normalizeColors(resolved, `semantics.${semanticFile}`);

    if (themeType === "light") lightSemantics = normalized;
    if (themeType === "dark") darkSemantics = normalized;

    const uiColors = replaceVariables(workbenchRaw, normalized, `workbench`);
    const semanticColors = replaceVariables(
      semanticRaw,
      normalized,
      `semantic`,
    );
    const tokenColors = replaceVariables(
      tokenColorsRaw,
      normalized,
      `tokenColors`,
    );

    const type = themeType.includes("light") ? "light" : "dark";
    const displaySuffix = themeType === "dark" ? "Dark" : "Light";

    const theme = {
      name: `${themeInfo.displayName} ${displaySuffix}`,
      type: type,
      colors: uiColors,
      tokenColors: tokenColors,
      semanticTokenColors: semanticColors,
    };

    if (!fs.existsSync(PATHS.outputDir)) {
      fs.mkdirSync(PATHS.outputDir, { recursive: true });
    }

    fs.writeFileSync(outputFile, JSON.stringify(theme, null, 2));
    console.log(`   ✅ 构建完成: ${outputFile}`);

    if (normalized.bg && normalized.text) {
      checkContrast(normalized.text, normalized.bg, "text", themeType);
    }
    if (normalized.bg && normalized.textDim) {
      checkContrast(normalized.textDim, normalized.bg, "textDim", themeType);
    }
    if (normalized.bg && normalized.textMuted) {
      checkContrast(
        normalized.textMuted,
        normalized.bg,
        "textMuted",
        themeType,
      );
    }
  });

  if (lightSemantics && darkSemantics) {
    generateColorCss(lightSemantics, darkSemantics);
    generateDesignSystemDoc(primitives, lightSemantics, darkSemantics);
  }

  console.log("\n🎉 所有主题构建完毕！");
}

main();
```

---

## ⚙️ 第五步：配置 package.json

```json
{
  "name": "moongate-theme",
  "version": "2.2.0",
  "scripts": {
    "build": "node scripts/build.js",
    "watch": "nodemon --watch src -e yaml --exec \"pnpm run build\"",
    "prepublishOnly": "pnpm run build"
  },
  "devDependencies": {
    "js-yaml": "^4.1.1",
    "nodemon": "^3.1.14",
    "wcag-contrast": "^3.0.0"
  }
}
```

> ⚠️ **注意**：请确保执行 `pnpm install` 或 `npm install` 安装所有依赖，否则脚本将因缺少 `wcag-contrast` 而报错。

---

## 🚀 第六步：运行构建

```bash
pnpm run build
```

构建成功后，你会看到以下输出：

```
🚀 开始构建主题 (DTCG 标准 + 工业级质检)...

📦 加载原始值...
📦 加载布局令牌...
✅ 布局令牌已生成: themes/moongate-layout.css
📦 加载公共规则...
📚 加载语言规则...
   ✅ 已加载: base.yaml
   ...
✨ 加载特殊规则...
   ✅ 已加载: better-comments.yaml

🎨 扫描语义文件...

🔨 开始构建主题...
   ✅ 构建完成: themes/moongate-dark.json
✅ dark · text: 14.48:1
✅ dark · textDim: 12.02:1
✅ dark · textMuted: 6.96:1
   ✅ 构建完成: themes/moongate-light.json
✅ light · text: 17.08:1
✅ light · textDim: 7.25:1
✅ light · textMuted: 7.25:1
✅ 颜色令牌已生成: themes/moongate-colors.css
✅ 设计系统文档已生成: docs/DESIGN_SYSTEM.md

🎉 所有主题构建完毕！
```

生成的文件：

- `themes/moongate-dark.json`、`moongate-light.json`：VS Code 主题
- `themes/moongate-colors.css`：颜色令牌（深浅模式）
- `themes/moongate-layout.css`：布局令牌（间距、排版、断点等）
- `docs/DESIGN_SYSTEM.md`：完整设计系统文档，包含变量选择协议

---

## 📊 第七步：使用生成的资产

### CSS 变量命名规则

生成的 CSS 变量使用 `--ui-` 前缀，并将语义层变量名（驼峰）转换为 kebab-case。例如：

| 语义变量        | 生成的 CSS 变量       | 说明         |
| --------------- | --------------------- | ------------ |
| `bg`            | `--ui-bg`             | 编辑器背景   |
| `surfaceRaised` | `--ui-surface-raised` | 隆起层背景   |
| `textMuted`     | `--ui-text-muted`     | 辅助文字颜色 |
| `spacing.md`    | `--ui-spacing-md`     | 中等间距     |

### 在博客中使用 CSS 变量

```html
<link rel="stylesheet" href="/themes/moongate-colors.css" />
<link rel="stylesheet" href="/themes/moongate-layout.css" />
```

```css
body {
  background: var(--ui-bg);
  color: var(--ui-text);
  font-family: var(--ui-typography-family-sans);
  font-size: var(--ui-typography-size-body);
}

.card {
  background: var(--ui-surface-raised);
  border: var(--ui-shadow-border) var(--ui-border);
  padding: var(--ui-spacing-md);
  border-radius: var(--ui-radius-none);
}

.button-primary:hover {
  /* 状态复合：底色 + 遮罩 */
  background-image: linear-gradient(
    var(--ui-action-hover),
    var(--ui-action-hover)
  );
  background-color: var(--ui-primary);
}

@media (min-width: var(--ui-breakpoint-tablet)) {
  .container {
    padding: var(--ui-spacing-lg);
  }
}
```

### 切换深浅模式

在根元素上添加/移除 `.dark` 类：

```javascript
document.documentElement.classList.toggle("dark");
```

</details>

---

## 📝 总结

通过本篇，你将工业级构建脚本完整落地：

- ✅ DTCG 三层架构完整实现（原始值、语义层、组件层）
- ✅ 颜色标准化与循环检测
- ✅ WCAG 对比度自动校验
- ✅ 自动生成颜色 CSS 变量文件
- ✅ 自动生成布局 CSS 变量文件（间距、排版、断点等）
- ✅ 自动生成设计系统文档（含变量选择协议）

这套脚本不仅服务于 Moongate 主题，更可以作为你未来所有设计系统项目的工程基石。现在，你已经拥有了一整套从“设计哲学”到“工程代码”再到“跨平台资产”的完整设计系统。

**探索不息，编码不止。**

[⬆ 返回顶部](#)
