---
title: VS Code 主题：从手写 JSON 到可发布
description: 不依赖脚手架，从零手写最小主题 JSON，理解 colors 与 tokenColors 的核心机制，掌握调试、打包与发布的完整流程，构建属于你的第一个 VS Code 主题。
date: 2026-08-06 00:00:00
series: design-system
tags:
  - VSCode
  - Theme
  - Engineering
---

如果你有编程基础（熟悉 JavaScript、JSON、命令行），想把一套配色方案变成 VS Code 主题，但完全不知道从何下手——这篇文章正是为你准备的。

Moongate 主题最初就是从个人博客的配色衍生而来。在这篇文章中，我们不使用任何脚手架，而是**从一个最小的 JSON 文件开始**，逐步理解主题的运作机制。等机制清楚了，你自然会发现哪些环节需要工程化——那就是整个系列接下来要做的事。

---

## 一、VS Code 主题的本质

一个主题说到底就是一份 JSON 文件，它告诉 VS Code 两件事：

- **界面长什么样**：标题栏、状态栏、侧边栏、编辑器背景……这些 UI 区域的颜色。
- **代码怎么着色**：不同的语法元素（关键字、字符串、注释、函数……）分别用什么颜色和样式。

理解文件结构本身并不难，真正难的是**知道该配置哪些键名、每个键名是什么意思**。所以这篇文章的核心目标是：带你亲手创建一个最小可用的主题，然后告诉你如何用 VS Code 自带的工具，找到任何你想调整的颜色所对应的正确键名。

---

## 二、自定义主题的最小路径

### 1. 创建项目结构

首先准备一个空目录，并创建 `themes/` 文件夹：

```bash
mkdir my-theme
cd my-theme
mkdir themes
```

虽然 VS Code 的扩展项目通常还需要 `package.json`、`README.md` 等文件，但为了让读者先专注理解结构，这里从主题 JSON 本身开始。

### 2. 编写最小主题 JSON

在 `themes/` 下创建一个 JSON 文件，例如 `my-theme.json`：

```json
{
  "name": "My Theme",
  "type": "dark",
  "colors": {
    "editor.background": "#0f172a",
    "editor.foreground": "#e2e8f0"
  },
  "tokenColors": [
    {
      "name": "Comment",
      "scope": ["comment", "punctuation.definition.comment"],
      "settings": {
        "fontStyle": "italic",
        "foreground": "#a5b4cb"
      }
    },
    {
      "name": "Keyword",
      "scope": ["keyword", "storage.type", "storage.modifier"],
      "settings": {
        "foreground": "#3b82f6",
        "fontStyle": "bold"
      }
    },
    {
      "name": "String",
      "scope": ["string", "string.quoted.single", "string.quoted.double"],
      "settings": {
        "foreground": "#34d399"
      }
    }
  ]
}
```

这个文件虽然很小，但它已经包含了主题的全部核心结构。

---

## 三、核心概念：`colors` 与 `tokenColors`

### `colors`：编辑器界面

`colors` 对象定义编辑器 **UI 的配色**——背景、标题栏、状态栏、侧边栏、选区高亮等。它是一个扁平的对象，键名是 VS Code 预定义的接口，例如：

| 键名                        | 作用             |
| --------------------------- | ---------------- |
| `editor.background`         | 编辑器背景       |
| `editor.foreground`         | 编辑器默认前景色 |
| `titleBar.activeBackground` | 标题栏背景       |
| `statusBar.background`      | 状态栏背景       |
| `sideBar.background`        | 侧边栏背景       |

> ⚠️ **`type` 字段**：顶级 `type` 字段决定主题是深色还是浅色，取值为 `"dark"` 或 `"light"`。它会影响 VS Code 默认控件的渲染方式（如滚动条、输入框的自适应）。

### `tokenColors`：代码语法高亮

`tokenColors` 是一个**规则数组**，每条规则由 `scope` 和 `settings` 组成：

- `scope`：匹配的 TextMate 作用域（scope），可以是字符串或字符串数组。
- `settings`：该作用域应用的颜色与样式（`foreground`、`fontStyle`、`background`）。

```json
{
  "name": "Comment",
  "scope": ["comment", "punctuation.definition.comment"],
  "settings": {
    "fontStyle": "italic",
    "foreground": "#a5b4cb"
  }
}
```

#### ⚠️ 重要

`tokenColors` 数组的**顺序很重要**——后面的规则会覆盖前面相同 `scope` 的规则。这也是为什么工程化之后我们会用一个独立的 `base.yaml` 管理通用规则，再在语言文件中叠加专属规则。

---

## 四、如何找到正确的键名？

这是所有主题开发者最常遇到的问题：「我想把状态栏文字改成蓝色，该用什么键名？」

VS Code 提供了两个非常强大的内置工具，彻底解决了这个问题。

### 1. UI 颜色：提取当前主题

按下 `Ctrl+Shift+P`，运行命令 **`Developer: Generate Color Theme From Current Settings`**。

这会在输出面板中生成一个 JSON，包含**当前主题所用到的所有 UI 颜色键名**以及它们的值。你只需要：

1. 在输出中找到你关心的区域（比如 `statusBar.foreground`）。
2. 复制这个键名，粘贴到你自己的 `colors` 对象中。
3. 改成你自己的颜色值。

这个命令生成的是「当前生效值」，即使某个键名你从未配置过（VS Code 在使用默认值），它也会出现在输出中——相当于一份完整的键名清单。

### 2. 语法 scope：Inspect 工具

这是定位语法高亮问题的**神器**。打开任意代码文件，把光标放在你想着色的元素上，运行 **`Developer: Inspect Editor Tokens and Scopes`**。

弹出的窗口会显示：

- **该元素当前的 TextMate scope 链**，从最具体到最通用。
- **当前颜色来自哪条规则**（`foreground` 来源），以及是哪个文件定义的。

使用技巧：

- **选择最具体的 scope** 来精确命中目标元素，避免误伤同一族元素。
- 例如注释既有 `comment.line` 也有 `comment.block`，如果你只想给行注释着色，就选 `comment.line`；如果想统一处理所有注释，就选 `comment`。
- 当你发现某个元素颜色「不对」时，Inspect 窗口会直接告诉你当前颜色来自哪个规则——这排查思路清晰得多。

---

## 五、本地调试

在确认主题文件书写正确后，下一步就是把它加载进 VS Code 实时查看。

### 1. 创建 package.json

要运行主题扩展，需要一个最小的 `package.json` 将它声明为扩展：

```json
{
  "name": "my-theme",
  "displayName": "My Theme",
  "version": "0.0.1",
  "publisher": "your-name",
  "engines": {
    "vscode": "^1.109.0"
  },
  "categories": ["Themes"],
  "contributes": {
    "themes": [
      {
        "label": "My Theme",
        "uiTheme": "vs-dark",
        "path": "./themes/my-theme.json"
      }
    ]
  }
}
```

关键字段：

- **`contributes.themes`**：声明主题列表，每项包含 `label`（在颜色主题列表中显示的名称）、`uiTheme`（基础色系：`vs-dark` 深色 / `vs` 浅色）、`path`（主题 JSON 的相对路径）。
- **`publisher`**：你的发布者 ID。可以先填占位符，发布前注册后再回来修改。
- **`engines`**：最低 VS Code 版本要求。

### 2. F5 启动扩展开发主机

1. 在 VS Code 中打开项目文件夹。
2. 按 `F5`，启动「扩展开发主机」窗口。
3. 在开发主机中按 `Ctrl+K Ctrl+T`，选择你的主题。
4. 打开测试代码文件，实时查看效果。

修改主题 JSON 后，在开发主机中按 `Ctrl+R` 重新加载，修改立即生效。**注意**：修改 `package.json` 中的 `contributes.themes` 后，需要重启开发主机会话才能生效。

---

## 六、准备发布

### 1. 完善 package.json

发布之前，需要补全关键字段：

```json
{
  "name": "my-theme",
  "displayName": "My Theme",
  "description": "简短介绍你的主题",
  "version": "1.0.0",
  "publisher": "你的发布者ID",
  "engines": { "vscode": "^1.109.0" },
  "categories": ["Themes"],
  "icon": "images/icon.png",
  "repository": {
    "type": "git",
    "url": "https://github.com/yourname/my-theme"
  }
}
```

### 2. 准备截图

在根目录创建 `images/` 文件夹，放入至少 3-5 张不同语言的代码截图（建议 1280×640），用于 README 和商店展示。

### 3. 编写 README.md

包含主题名称、预览截图、设计理念、安装方法、配色表（可选）等。

### 4. 添加 LICENSE

建议使用 MIT 许可证，创建 `LICENSE` 文件。

### 5. 创建 `.vscodeignore`

排除不需要打包的文件，**但务必保留 `images/` 文件夹**：

```bash
.vscode/**
.gitignore
node_modules
```

---

## 七、发布前检查清单

在运行 `vsce package` 之前，花两分钟核对以下事项，可以避免大部分常见的发布错误：

- **`package.json` 信息**：确保 `publisher`、`name`、`version` 字段正确无误（`publisher` 必须与你在市场注册的 ID 完全一致）。
- **图标文件**：确认 `icon` 路径指向一个 **128×128 像素的 PNG 图片**，且文件确实存在于该位置。
- **预览图**：检查 `README.md` 中是否包含了至少一张主题预览图（建议使用 `images/` 文件夹内的截图），没有预览图的主题很难吸引用户。
- **`.vscodeignore` 配置**：确认已排除不必要的文件，但**务必保留 `images/` 文件夹**，否则截图无法随扩展一起发布。
- **本地打包测试**：运行 `vsce package` 命令，若能成功生成 `.vsix` 文件，说明配置基本正确。如果失败，仔细阅读错误提示——最常见的原因是 `icon` 路径错误或 `publisher` 未设置。

---

## 八、打包与发布

### 1. 安装发布工具

```bash
npm install -g @vscode/vsce
```

### 2. 打包测试

```bash
vsce package
```

如果成功，会生成 `.vsix` 文件。可以拖进 VS Code 手动安装测试。若失败，仔细阅读错误提示，常见原因是 `icon` 路径错误或 `.vscodeignore` 误排除了必要文件。

### 3. 获取 Personal Access Token

- 登录 [Azure DevOps](https://dev.azure.com)（用你注册市场的微软账号）。
- 右上角头像 → Personal access tokens → New Token。
- 名称随意，组织选 **All accessible organizations**，有效期建议 1 年。
- **权限**：只勾选 **Marketplace → Manage**。
- 创建后**立即复制 Token**（只显示一次）。

### 4. 登录并发布

```bash
vsce login 你的发布者ID
# 粘贴刚才的 Token（不会显示，直接回车）

vsce publish
```

几秒后主题就会上传。约 5-10 分钟即可在 VS Code 中搜索到。

#### 常见发布错误

- `Token verification failed`：权限未正确设置或 Token 过期，重新生成。
- `Version already exists`：版本号重复，更新 `package.json` 中的 `version`。
- 网络问题：尝试更换网络或使用代理。

### 🔁 替代方案：手动上传 .vsix 文件

如果你在命令行方式中遇到困难，完全可以通过浏览器手动上传：

1. 首先确保你已经运行 `vsce package` 成功生成了 `.vsix` 文件。
2. 访问 [VS Code 市场管理页](https://marketplace.visualstudio.com/manage)，用你的微软账号登录。
3. 在页面中点击你的发布者名称，进入发布者管理界面。
4. 点击右上角的 **Publish extension** 按钮。
5. 选择你的 `.vsix` 文件上传。
6. 几分钟后主题就会出现在市场中。

#### 优点

完全绕过命令行 token 验证，过程可视化。

---

## 九、发布后

- 查看市场页面：`https://marketplace.visualstudio.com/items?itemName=你的发布者ID.你的主题名`
- 登录 [市场管理后台](https://marketplace.visualstudio.com/manage) 查看报表（页面浏览、安装量、转化率）。
- 在 GitHub 仓库添加 README 徽章（如版本、下载量）。
- 收集反馈，准备后续更新。

---

## 十、设计哲学：从「好看」到「好用」

很多新手以为主题就是配几个好看的颜色。但真正优秀的主题，能让代码的结构自己「浮现」出来。这就是 **「视觉远近法」** 的理念：

- **操作符、标点应该退后**（亮度低一些），不干扰阅读；
- **函数名应该发光**（亮度高一些），成为视觉锚点；
- **只读变量应该用斜体**暗示「不可变」；
- **废弃代码应该加上删除线**，一眼识别。

这套原则在 Moongate 中落地为「语义分层」：前景（核心逻辑）、中景（普通代码）、背景（辅助信息）三个视觉层级。你可以在后续的系列文章中看到它如何演变为完整的设计体系。

---

## 为什么手写 JSON 不可持续？

现在你已经拥有一个可以手动编辑、可以发布的 VS Code 主题了。但当你认真用起来，很快就会遇到这些痛点：

- 一个 JSON 文件动辄上千行，修改一个颜色需要全局搜索，容易误改。
- 想为不同语言定制高亮，却要在同一个 `tokenColors` 数组里堆砌规则，难以维护。
- 想尝试浅色版本，不得不复制整个文件，然后手动修改几百个颜色值。
- 一个不小心，就会用错 scope，某个语言的高亮「规则写了对不上」。

这些问题不是你的失误，而是**手写 JSON 这种工作方式的极限**。这正是我们接下来的系列要解决的问题——从工程化到设计系统，让主题维护变得轻松而优雅。

[**主题工程化：从单体 JSON 到模块化 YAML**](./create-vscode-theme-engineering)
