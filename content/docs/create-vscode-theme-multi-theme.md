---
title: 扩展篇：从工程化到设计系统：构建深色/浅色双主题及多主题变体
description: 在工程化基础上，通过变量分离和重力补偿原则，一键生成深色、浅色及高对比度等多主题变体。让所有主题共享同一套规则，维护成本趋近于零。
date: 2026-03-11 12:00:00
permalink: 97e09fc8-13a0-4703-958d-44700fe20a62
series: design-system
level: P3
tags: 
  - Design System
  - Theme
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

## 🌗 引言：为什么需要多主题？

一个优秀的主题不应只有一副面孔。用户可能在不同时间段、不同环境光下使用编辑器，也可能偏爱不同的视觉风格。提供深色/浅色双主题甚至更多变体（如高对比度主题），不仅能覆盖更广泛的用户需求，更是主题迈向设计系统的关键一步。

在进阶篇中，我们已经将主题源码工程化——通过 YAML 管理颜色变量，通过构建脚本生成最终 JSON。现在，我们将在此基础上，轻松构建出风格一致、视觉对等的多主题家族。

---

## 🧱 一、核心思路：复用规则，分离颜色

多主题的核心思想是：**所有主题共享同一套语法规则和 UI 颜色定义，唯一不同的是颜色变量的具体值。**

因此，我们需要将颜色变量从规则文件中彻底分离，每个主题拥有独立的颜色变量文件，而所有规则文件（如 `languages/*.yaml`、`workbench.yaml`）只引用变量名。

**关于语义规则**：VS Code 主题支持两种高亮方式：`tokenColors`（基于 TextMate 语法的作用域）和 `semanticTokenColors`（基于语言服务提供的语义信息）。语义规则能更精准地反映代码的语义角色（如变量、参数、属性），并支持修饰符（如斜体、下划线）。在多主题体系中，`semantic.yaml` 同样只引用颜色变量，因此深浅主题的语义高亮会自动保持一致。

### 1.1 目录结构进化

```text
your-theme/
├── src/
│   ├── core/
│   │   ├── colors-dark.yaml       # 深色主题变量
│   │   ├── colors-light.yaml       # 浅色主题变量
│   │   ├── colors-hc-black.yaml    # 高对比度深色主题
│   │   └── colors-hc-light.yaml    # 高对比度浅色主题（可选）
│   ├── languages/                  # 各语言规则（完全复用）
│   ├── workbench.yaml              # UI 颜色（引用变量）
│   └── semantic.yaml               # 语义规则（引用变量）
├── scripts/
│   └── build.js                    # 多主题构建脚本
└── themes/
    ├── your-theme-dark.json
    ├── your-theme-light.json
    ├── your-theme-hc-black.json
    └── your-theme-hc-light.json
```

### 1.2 变量文件示例

**`colors-dark.yaml`**

```yaml
# 深色主题
primary: "#3b82f6"
success: "#34d399"
warning: "#fbbf24"
error: "#f87171"
bg: "#0f172a"
text: "#e2e8f0"
# ... 其他变量
```

**`colors-light.yaml`**

```yaml
# 浅色主题（变量名与深色版完全一致，仅色值不同）
primary: "#0284c7"
success: "#059669"
warning: "#b45309"
error: "#b91c1c"
bg: "#f9fafb"
text: "#1e293b"
# ... 其他变量
```

**关键原则**：所有颜色文件的变量名必须**完全一致**，这是实现规则复用的基础。

### 1.3 变量命名最佳实践

为了让主题系统易于维护和扩展，变量命名应遵循 **语义化 + 可组合** 原则：

- **语义化**：变量名应描述颜色的**用途**而非具体颜色，例如使用 `primary` 而不是 `blue`，使用 `success` 而不是 `green`。这样即使未来调整主题色，变量名无需改变。
- **可组合**：对于背景、文本等具有层级关系的变量，可以使用 `bgBase`、`bgElevated`、`textPrimary`、`textSecondary` 等命名，便于快速理解变量间的层次关系。

```yaml
# 推荐的命名方式
bgBase: "#0f172a" # 最底层背景
bgElevated: "#131c31" # 浮层背景
bgMuted: "#1e293b" # 次级背景
textPrimary: "#e2e8f0" # 主要文字
textMuted: "#94a3b8" # 辅助文字
```

这样，在添加新主题时，只需为新颜色文件中的这些变量赋予合适的值，无需改动任何规则文件。

> **⚠️ 透明度处理的最佳实践**  
> 在 Moongate 的工程体系中，**颜色变量本身不应包含透明度**。透明度应统一通过后缀形式添加，例如在引用变量时使用 `${primary}20` 表示 12.5% 透明度的主色。构建脚本会自动将六位色值（如 `#3b82f6`）与透明度后缀拼接为八位色值（如 `#3b82f620`）。如果变量值本身已经是八位色值（即已包含透明度），脚本会发出警告并保留原值，忽略后缀。这可以避免意外生成非法色值，同时保持透明度处理的清晰可预测。

---

## ⚖️ 二、重力补偿：让深浅主题视觉重量对等

浅色主题不是深色主题的简单反相。深色背景上，亮色是“发光体”；浅色背景上，暗色是“吸光体”。要实现视觉重量对等，必须进行 **重力补偿**——同一语义角色，在不同背景下使用同一色相、不同明度。

### 补偿规则示例

| 语义角色        | 深色版               | 浅色版               | 说明                               |
| --------------- | -------------------- | -------------------- | ---------------------------------- |
| 主色（primary） | `#3b82f6` (60% 明度) | `#0284c7` (48% 明度) | 蓝调不变，明度降低，适应白底       |
| 成功（success） | `#34d399` (65%)      | `#059669` (40%)      | 绿色更沉稳，保证对比度             |
| 警告（warning） | `#fbbf24` (75%)      | `#b45309` (35%)      | 从亮黄转为橙黄，避免在白底上“消失” |
| 错误（error）   | `#f87171` (60%)      | `#b91c1c` (35%)      | 深红保持警示感                     |

### 如何工程化地确定补偿值？

手动“凭感觉”调整明度往往效率低且不一致。推荐使用 **HSL 颜色模型** 进行科学补偿：

- 将 HEX 色值转换为 HSL（色相、饱和度、明度）。
- 保持色相（H）不变，这是语义一致性的核心。
- 适当降低饱和度（S）和明度（L）以适应浅色背景。具体降幅可参考经验值（如明度降低 20-30%），或使用工具如 [chroma.js](https://gka.github.io/chroma.js/) 进行程序化调整。

例如，使用 chroma.js 生成浅色版主色：

```javascript
const darkPrimary = chroma("#3b82f6");
const lightPrimary = darkPrimary.set("hsl.l", 0.48); // 将明度设为 48%
```

**饱和度调整**：除了明度，饱和度也建议适当调整。深色背景下高饱和度颜色能产生舒适的“发光感”，但在浅色背景下同样的饱和度可能显得刺眼。通常可以将饱和度降低 10-20%，以获得更柔和的视觉效果。使用 chroma.js 可以同时调整明度和饱和度：

```javascript
const darkPrimary = chroma("#3b82f6");
const lightPrimary = darkPrimary.set("hsl.l", 0.48).set("hsl.s", 0.7); // 明度降至48%，饱和度降至70%
```

通过这套映射，用户在切换主题时，同一语法元素的视觉重量几乎不变，无需重新学习。

---

## 🔨 三、构建脚本升级：自动生成多主题

在进阶篇的构建脚本基础上，我们只需做少量增强，即可实现多主题自动生成。以下是核心逻辑的代码片段（完整脚本见附录）：

### 3.1 扫描颜色文件

> **📁 自定义路径**：如果您的项目结构不同（例如将语言规则放在 `src/tokens/languages` 下），请修改 `PATHS` 对象中的对应路径。本脚本的路径基于推荐结构，但您可以灵活调整。

```javascript
const colorFiles = fs
  .readdirSync(coreDir)
  .filter((f) => f.startsWith("colors-") && f.endsWith(".yaml"));

colorFiles.forEach((file) => {
  const match = file.match(/^colors-(.+)\.yaml$/);
  if (!match) return;
  const themeType = match[1]; // 'dark', 'light', 'hc-black', 'hc-light' 等
  // ... 加载颜色变量并生成主题
});
```

### 3.2 主题类型映射（显示名称与 uiTheme）

为了让生成的主题名称更友好，并正确设置 `uiTheme`，可以定义一个映射表：

```javascript
const themeDisplayMap = {
  dark: { suffix: "Dark", uiTheme: "vs-dark" },
  light: { suffix: "Light", uiTheme: "vs" },
  "hc-black": { suffix: "High Contrast (Black)", uiTheme: "hc-black" },
  "hc-light": { suffix: "High Contrast (Light)", uiTheme: "hc-light" },
  // 可根据需要添加更多
};

// 在构建每个主题时：
const display = themeDisplayMap[themeType] || {
  suffix: themeType,
  uiTheme: "vs-dark",
};
const themeName = `${themeInfo.displayName} ${display.suffix}`;
const uiTheme = display.uiTheme;
// 注意：主题对象的 type 字段通常为 'dark' 或 'light'，可从 themeType 推断
const type = themeType.includes("light") ? "light" : "dark";
```

### 3.3 增强的变量替换函数（支持透明度安全网）

```javascript
function replaceVariables(obj, colors) {
  if (typeof obj === "string") {
    return obj.replace(
      /\$\{([a-zA-Z0-9_-]+)\}([0-9a-fA-F]{2})?/g,
      (match, key, alpha) => {
        const value = colors[key];
        if (!value) {
          console.warn(`⚠️ 变量 ${key} 未定义`);
          return match;
        }
        // 智能处理透明度
        if (alpha && /^#[0-9a-fA-F]{6}$/.test(value)) {
          return value + alpha; // 6位色值 + 透明度后缀
        }
        if (alpha && /^#[0-9a-fA-F]{8}$/.test(value)) {
          console.warn(`⚠️ 变量 ${key} 已包含透明度，忽略后缀`);
          return value;
        }
        return value;
      },
    );
  }
  // ... 处理数组和对象
}
```

### 3.4 从 package.json 读取主题信息

```javascript
const pkg = require("../package.json");
const baseName = pkg.name.replace(/[^a-z0-9-]/gi, "-").toLowerCase();
```

### 3.5 调试多主题的小技巧

在开发过程中，你可能希望只构建某个特定主题以加快构建速度。可以在脚本中增加命令行参数支持（可选），但在默认情况下，你可以通过以下方式快速预览所有主题：

1. 运行 `npm run build` 生成所有主题 JSON。
2. 按 `F5` 启动扩展开发宿主。
3. 在开发宿主中打开命令面板（`Ctrl+Shift+P`），选择“首选项: 颜色主题”，即可看到所有已注册的主题（深色、浅色、高对比度等）。快速切换即可验证各主题效果。
4. 如果修改了颜色变量文件，只需重新运行 `npm run build`，然后在开发宿主中执行“开发人员: 重新加载窗口”即可看到更新。

---

## 📦 四、注册多个主题

在 `package.json` 的 `contributes.themes` 中为每个主题添加一个条目，注意 `uiTheme` 字段的正确设置：

```json
"contributes": {
  "themes": [
    {
      "label": "Moongate Dark",
      "uiTheme": "vs-dark",
      "path": "./themes/moongate-dark.json"
    },
    {
      "label": "Moongate Light",
      "uiTheme": "vs",
      "path": "./themes/moongate-light.json"
    },
    {
      "label": "Moongate High Contrast (Black)",
      "uiTheme": "hc-black",
      "path": "./themes/moongate-hc-black.json"
    },
    {
      "label": "Moongate High Contrast (Light)",
      "uiTheme": "hc-light",
      "path": "./themes/moongate-hc-light.json"
    }
  ]
}
```

- **`uiTheme`** 字段决定主题类型：
  - 深色标准主题：`"vs-dark"`
  - 浅色标准主题：`"vs"`
  - 高对比度深色主题：`"hc-black"`
  - 高对比度浅色主题：`"hc-light"`
- **`label`** 将显示在 VS Code 命令面板的“颜色主题”列表中，建议与生成的主题名称保持一致或更友好。

---

## 🌈 五、扩展更多主题变体

基于同一套规则，我们可以轻松添加更多主题：

1. **创建新的颜色文件**：如 `colors-hc-black.yaml`（高对比度深色）、`colors-hc-light.yaml`（高对比度浅色）、`colors-sepia.yaml`（复古风格）等。
2. **在颜色文件中定义与基础主题完全一致的变量名**，但赋予不同的色值。
3. **构建脚本自动扫描**，生成对应的 JSON 文件。
4. **在 `package.json` 中注册**新主题，并根据主题类型设置正确的 `uiTheme`。

无需修改任何语言规则或 UI 颜色定义，维护成本几乎为零。

### 高对比度深色主题示例

```yaml
# colors-hc-black.yaml
primary: "#ffff00" # 亮黄
success: "#00ff00" # 亮绿
warning: "#ff8800"
error: "#ff0000"
bg: "#000000"
text: "#ffffff"
# ... 其他变量使用极高对比度色值
```

---

## 🧠 六、维护建议

1. **变量命名规范**：使用语义化名称，如 `primary`、`success`、`bg`、`text`，避免使用具体颜色名（如 `blue`）。这样即使主题色变化，规则文件也不需要改动。
2. **保持规则文件与主题无关**：所有规则文件（`languages/`、`workbench.yaml`、`semantic.yaml`）中**不得出现具体色值**，只能引用变量。
3. **利用构建脚本的“安全网”**：确保变量替换函数能处理透明度后缀、已含透明度的变量等边界情况。
4. **版本控制**：将颜色变量文件纳入 Git，但忽略生成的 JSON 文件（除非需要发布预览）。在 `.gitignore` 中添加 `themes/*.json`，但保留 `!themes/.gitkeep` 以保持目录存在。
5. **发布前检查**：运行构建脚本后，手动检查各主题 JSON 文件是否生成正确，颜色值是否替换无误。

---

## ⚠️ 七、常见问题与陷阱

| 问题                                  | 可能原因                                          | 解决方法                                                         |
| ------------------------------------- | ------------------------------------------------- | ---------------------------------------------------------------- |
| **主题未出现在颜色主题列表中**        | `package.json` 中未正确注册，或 JSON 文件路径错误 | 检查 `contributes.themes` 条目，确保 `path` 指向正确的文件。     |
| **浅色主题显示为深色**                | `uiTheme` 字段误设为 `"vs-dark"`                  | 浅色主题应使用 `"vs"`。                                          |
| **高对比度主题显示不正确**            | `uiTheme` 未使用 `"hc-black"` 或 `"hc-light"`     | 根据主题类型设置正确的 `uiTheme`。                               |
| **颜色变量未替换，仍显示为 `${var}`** | 变量名拼写错误，或变量未在颜色文件中定义          | 检查变量名是否一致，确保颜色文件包含所有被引用的变量。           |
| **透明度效果异常（颜色太深/太浅）**   | 变量本身已含透明度，同时又添加了透明度后缀        | 确保颜色变量中不预置透明度，透明度统一通过后缀 `${var}20` 添加。 |
| **构建脚本报错“找不到文件”**          | 缺少必要的 YAML 文件                              | 确保 `src/core/`、`src/languages/` 等目录存在，且包含所需文件。  |
| **新添加的颜色文件未被扫描**          | 文件名不符合 `colors-*.yaml` 模式                 | 检查文件名是否以 `colors-` 开头并以 `.yaml` 结尾。               |

---

## 🚀 八、从多主题到设计系统

多主题的建立，标志着你的主题已经从“一套配色”升级为“可扩展的视觉系统”。接下来，你可以：

- **定义设计哲学**：如冷调基底、语义分层、重力补偿，让颜色选择有据可依。
- **提供视觉契约**：编写显示器校准指南，帮助用户在不同硬件上获得一致体验。
- **建立协议索引**：将博客文章按难度分级，与主题形成品牌闭环。

这些内容将在**系统篇**中详细展开。

---

## 📌 九、总结

通过分离颜色变量、增强构建脚本，我们实现了从单主题到多主题的平滑扩展。核心收获：

- **复用规则**：所有主题共享同一套语言和 UI 定义，维护成本极低。
- **一键生成**：构建脚本自动扫描颜色文件，生成多个主题 JSON。
- **无限扩展**：新增主题只需添加颜色文件，无需改动其他代码。
- **重力补偿**：深浅主题视觉重量对等，切换时无需重新适应。
- **正确注册**：通过映射表和 `uiTheme` 设置，确保各主题类型正确识别。

现在，你的主题已经能够一键生成深色、浅色乃至高对比度等多个变体，并通过重力补偿保证了它们之间的视觉对等。用户可以根据自己的环境随心切换，而你的维护成本依然为零。

但一个真正优秀的主题，不应只是颜色规则的集合。它应该有自己的设计哲学、与用户的沟通契约，甚至成为你技术品牌的核心资产。在系统篇中，我们将完成最后的跃迁——**从工程化到设计系统，打造完整的主题品牌体系，包括视觉契约、协议索引等原创概念**。

[**系统篇：从设计系统到视觉契约，打造完整的主题品牌体系**](./create-vscode-theme-design-system.md)

---

**探索不息，编码不止。**

[⬆ 返回顶部](#)

---

## 📎 附录：完整构建脚本参考

> **📁 自定义路径**：如果您的项目结构不同（例如将语言规则放在 `src/tokens/languages` 下），请修改 `PATHS` 对象中的对应路径。本脚本的路径基于推荐结构，但您可以灵活调整。

<details>
<summary>点击完整构建脚本</summary>

```javascript
// scripts/build.js
// 完整脚本代码，包含上述所有增强功能

const fs = require("fs");
const yaml = require("js-yaml");
const path = require("path");

const ROOT_DIR = path.resolve(__dirname, "..");

// ==================== 路径配置 ====================
const PATHS = {
  workbench: path.join(ROOT_DIR, "src", "workbench.yaml"),
  semantic: path.join(ROOT_DIR, "src", "semantic.yaml"),
  langDir: path.join(ROOT_DIR, "src", "languages"),
  specialDir: path.join(ROOT_DIR, "src", "special"),
  coreDir: path.join(ROOT_DIR, "src", "core"),
  outputDir: path.join(ROOT_DIR, "themes"),
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

function replaceVariables(obj, colors) {
  if (typeof obj === "string") {
    return obj.replace(
      /\$\{([a-zA-Z0-9_-]+)\}([0-9a-fA-F]{2})?/g,
      (match, key, alpha) => {
        const value = colors[key];
        if (value === undefined) {
          console.warn(`⚠️ 警告: 变量 "${key}" 未定义，保留原样`);
          return match;
        }

        if (alpha) {
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
    return obj.map((item) => replaceVariables(item, colors));
  }
  if (obj && typeof obj === "object") {
    const result = {};
    for (const [k, v] of Object.entries(obj)) {
      result[k] = replaceVariables(v, colors);
    }
    return result;
  }
  return obj;
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

// 主题显示名称和 uiTheme 映射
const themeDisplayMap = {
  dark: { suffix: "Dark", uiTheme: "vs-dark" },
  light: { suffix: "Light", uiTheme: "vs" },
  "hc-black": { suffix: "High Contrast (Black)", uiTheme: "hc-black" },
  "hc-light": { suffix: "High Contrast (Light)", uiTheme: "hc-light" },
  // 可根据需要扩展
};

// ==================== 主流程 ====================
function main() {
  console.log("🚀 开始构建主题...\n");

  // 1. 检查必需文件
  try {
    ensureFileExists(PATHS.workbench, "workbench");
    ensureFileExists(PATHS.semantic, "semantic");
    ensureFileExists(PATHS.coreDir, "core 目录");
  } catch (err) {
    console.error(err.message);
    process.exit(1);
  }

  // 2. 加载公共规则
  console.log("📦 加载公共规则...");
  const workbenchRaw = safeLoadYaml(PATHS.workbench, "workbench.yaml");
  const semanticRaw = safeLoadYaml(PATHS.semantic, "semantic.yaml");
  if (!workbenchRaw || !semanticRaw) {
    process.exit(1);
  }

  // 3. 收集语言规则
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
  } else {
    console.warn("⚠️ languages 目录不存在，跳过语言规则加载");
  }

  // 4. 加载特殊规则
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
  } else {
    console.log("ℹ️ special 目录不存在，跳过特殊规则");
  }

  // 5. 获取颜色变量文件
  console.log("\n🎨 扫描颜色变量文件...");
  const colorFiles = fs
    .readdirSync(PATHS.coreDir)
    .filter((f) => f.startsWith("colors-") && f.endsWith(".yaml"));

  if (colorFiles.length === 0) {
    console.error("❌ core 目录下没有找到 colors-*.yaml 文件");
    process.exit(1);
  }

  // 6. 读取主题信息
  const themeInfo = getThemeInfo();
  let baseName = themeInfo.name.replace(/[^a-z0-9-]/gi, "-").toLowerCase();

  // 7. 为每个颜色文件构建主题
  console.log(`\n🔨 开始构建主题 (基础名称: ${baseName})...\n`);
  colorFiles.forEach((colorFile) => {
    const match = colorFile.match(/^colors-(.+)\.yaml$/);
    if (!match) return;
    const themeType = match[1]; // 如 'dark', 'light', 'hc-black'
    const outputFile = path.join(
      PATHS.outputDir,
      `${baseName}-${themeType}.json`,
    );

    // 加载颜色变量
    const colorsPath = path.join(PATHS.coreDir, colorFile);
    const colors = safeLoadYaml(colorsPath, `颜色变量 ${colorFile}`);
    if (!colors) {
      console.error(`   ❌ 跳过 ${colorFile}`);
      return;
    }

    // 替换变量
    const uiColors = replaceVariables(workbenchRaw, colors);
    const semanticColors = replaceVariables(semanticRaw, colors);
    const tokenColors = replaceVariables(tokenColorsRaw, colors);

    // 确定显示名称和 uiTheme
    const display = themeDisplayMap[themeType] || {
      suffix: themeType,
      uiTheme: "vs-dark",
    };
    const themeName = `${themeInfo.displayName} ${display.suffix}`;

    // 主题类型映射表（用于 theme.type 字段）
    const themeTypeMap = {
      dark: "dark",
      light: "light",
      "hc-black": "dark",
      "hc-light": "light",
      // 可根据需要扩展，例如 'sepia' 可映射为 'light'
    };
    const type = themeTypeMap[themeType] || "dark"; // 默认深色

    const theme = {
      name: themeName,
      type: type,
      colors: uiColors,
      tokenColors: tokenColors,
      semanticTokenColors: semanticColors,
    };

    // 确保输出目录存在
    if (!fs.existsSync(PATHS.outputDir)) {
      fs.mkdirSync(PATHS.outputDir, { recursive: true });
    }

    fs.writeFileSync(outputFile, JSON.stringify(theme, null, 2));
    console.log(`   ✅ 构建完成: ${outputFile}`);
  });

  console.log("\n🎉 所有主题构建完毕！");
}

main();
```

</details>
