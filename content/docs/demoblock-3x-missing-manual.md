---
title: VitePress + vitepress-theme-demoblock 3.x 配置指南
description: 详解 demoblock 3.x 的配置原理、踩坑记录和最佳实践，补充官方文档缺失的「连接逻辑」。
date: 2026-06-03
permalink: 1129affd-8dc5-4dd2-8c76-59a477dc5c06
level: P1
series: 
tags:
  - Vue
  - Engineering
---

> 本文基于 `vitepress-theme-demoblock@3.x` 版本，详细解释每一步的作用和背后的原理。

## 背景

为 Vue 3 组件库搭建文档站，需要支持：

- 组件实时预览
- 代码展示 + 折叠
- 一键复制代码

官方推荐 `vitepress-theme-demoblock`，但配置过程中发现：**官方文档虽然写全了步骤，但没有解释「为什么」，导致容易踩坑。**

本文补充官方缺失的「连接逻辑」。

## 官方文档写了什么

根据[官方文档](https://github.com/xinlei3166/vitepress-theme-demoblock)，配置步骤如下：

### 1. 安装

```bash
pnpm add -D vitepress-theme-demoblock
```

### 2. 配置 config.ts

```ts
import {
  demoblockPlugin,
  demoblockVitePlugin,
} from "vitepress-theme-demoblock";

export default {
  markdown: {
    config: (md) => {
      md.use(demoblockPlugin);
    },
  },
  vite: {
    plugins: [demoblockVitePlugin()],
  },
};
```

### 3. 配置主题

```ts
import DefaultTheme from "vitepress/theme";
import "vitepress-theme-demoblock/dist/theme/styles/index.css";
import { useComponents } from "./useComponents"; // ← 从本地导入

export default {
  ...DefaultTheme,
  enhanceApp(ctx) {
    DefaultTheme.enhanceApp(ctx);
    useComponents(ctx.app);
  },
};
```

### 4. 添加脚本

```json
{
  "scripts": {
    "docs:dev": "yarn run register:components && vitepress dev docs",
    "register:components": "vitepress-rc"
  }
}
```

## 官方没说的是什么

### 问题一：`useComponents` 从哪来？

官方写的是 `import { useComponents } from './useComponents'`，但：

- 项目中一开始并没有这个文件
- 官方没说明这个文件是**生成的**

**答案**：`vitepress-rc` 命令会生成这个文件。

### 问题二：`vitepress-rc` 是做什么的？

官方只是列出了一个命令，没说明作用。

**答案**：`vitepress-rc` 会：

1. 扫描你的组件目录（默认读取 `docs/.vitepress/components` 或项目根目录的 `components.json`）
2. 自动注册 demoblock 需要的 Vue 组件
3. 生成 `.vitepress/theme/useComponents.ts` 文件

> **注意**：如果你的组件库在 `src/components` 目录下，需要配置 `vitepress-rc` 的扫描路径，否则会生成空文件。

### 问题三：为什么要先运行 `register:components`？

官方脚本写了 `&& vitepress dev docs`，但没解释为什么要先运行。

**答案**：因为 VitePress 启动时需要读取 `./useComponents.ts`，而这个文件必须先由 `vitepress-rc` 生成。

### 问题四：不运行会怎样？

直接运行 `vitepress dev docs` 会报错：

```bash
Uncaught SyntaxError: Cannot find module './useComponents'
```

## 完整配置流程（含解释）

### 第一步：安装依赖

```bash
pnpm add -D vitepress vitepress-theme-demoblock
```

### 第二步：配置 config.ts

```ts
// docs/.vitepress/config.ts
import { defineConfig } from "vitepress";
import {
  demoblockPlugin,
  demoblockVitePlugin,
} from "vitepress-theme-demoblock";

export default defineConfig({
  title: "My UI",
  description: "Vue 3 组件库",

  markdown: {
    config: (md) => {
      md.use(demoblockPlugin); // Markdown-it 插件：解析 :::demo 语法
    },
  },

  vite: {
    plugins: [demoblockVitePlugin()], // Vite 插件：编译 demo 中的 Vue 组件
  },
});
```

> **💡 原理解析**：两个插件的分工
>
> - `demoblockPlugin`（Markdown-it 插件）：负责「偷梁换柱」。把 Markdown 里的 `:::demo` 语法转换成自定义的 `<demo-block>` 标签，将代码作为字符串和实时运行的组件传入。
> - `demoblockVitePlugin`（Vite 插件）：负责「降维打击」。在编译阶段把抽离出来的虚拟 Vue 代码真正编译成浏览器可执行的 JS/Vue 组件。

### 第三步：配置主题

```ts
// docs/.vitepress/theme/index.ts
import DefaultTheme from "vitepress/theme";
import "vitepress-theme-demoblock/dist/theme/styles/index.css";
// 注意：从本地导入，不是从包导入
import { useComponents } from "./useComponents";

export default {
  ...DefaultTheme,
  enhanceApp(ctx) {
    DefaultTheme.enhanceApp(ctx);
    useComponents(ctx.app); // 注册 demoblock 组件
  },
};
```

### 第四步：添加脚本

```json
{
  "scripts": {
    "docs:dev": "pnpm run register:components && vitepress dev docs",
    "docs:build": "pnpm run register:components && vitepress build docs",
    "register:components": "vitepress-rc"
  }
}
```

**脚本说明**：

| 脚本                  | 作用                           |
| --------------------- | ------------------------------ |
| `vitepress-rc`        | 生成 `./useComponents.ts` 文件 |
| `register:components` | 封装注册命令                   |
| `docs:dev`            | 先注册，再启动开发服务器       |

### 第五步：首次运行

```bash
pnpm docs:dev
```

执行过程：

1. 运行 `vitepress-rc` → 生成 `useComponents.ts`
2. 启动 VitePress → 读取生成的 `useComponents.ts`
3. 文档站正常运行

### 第六步：编写文档

**v3 正确写法**（不支持 `:::demo` 后面加描述文字）：

```md
## 基础用法

这是按钮的基础用法描述。（直接写在 `:::demo` 上方）

:::demo
\`\`\`vue
<template>
<Button type="primary">主要按钮</Button>
</template>

<script setup>
import { Button } from 'my-ui'
</script>

\`\`\`
:::
```

## 官方文档 vs 本文补充

| 维度                   | 官方文档       | 本文补充                   |
| ---------------------- | -------------- | -------------------------- |
| `useComponents` 从哪来 | 只写了导入语句 | 说明是 `vitepress-rc` 生成 |
| `vitepress-rc` 的作用  | 只写了命令     | 解释作用和生成的文件       |
| 命令执行顺序           | 写了 `&&`      | 解释为什么要先执行         |
| 不执行的后果           | 没写           | 说明会报错                 |
| 生成的文件             | 没提           | 讨论 Git 提交策略          |
| 两个插件的分工         | 没提           | 解释原理                   |

## 常见问题

### Q1：`vitepress-rc` 生成的文件要提交到 Git 吗？

**建议：提交 `useComponents.ts`，忽略 `cache/`**

```gitignore
# .gitignore
docs/.vitepress/cache/
# 注意：不要忽略 useComponents.ts
```

**理由**：

- `useComponents.ts` 内容稳定（只在组件列表变化时改变），体积很小（几 KB）
- 提交后团队成员 `git clone` 即可直接运行 `pnpm docs:dev`，无需额外生成
- CI/CD 构建时也不需要重新运行 `vitepress-rc`，减少构建步骤

**如果不提交**：可以使用 `prepare` 钩子在 `pnpm install` 时自动生成：

```json
{
  "scripts": {
    "prepare": "pnpm run register:components"
  }
}
```

但需要注意每次 `install` 都会重新生成，`cache/deps` 也会更新，可能导致 Git 状态变化。

### Q2：更新依赖后需要重新运行吗？

建议重新运行一次：

```bash
pnpm run register:components
```

### Q3：`:::demo` 后面的描述文字不生效？

3.x 版本**不支持**描述文字：

| 版本 | 正确写法                    |
| ---- | --------------------------- |
| v2   | `:::demo 这是描述`          |
| v3   | `:::demo`（不支持描述文字） |

**平替方案**：在 `:::demo` 上方直接写标准 Markdown 文本即可。

```md
### 基础用法

这是按钮的基础用法描述。

:::demo
\`\`\`vue
<template>
<Button type="primary">主要按钮</Button>
</template>
\`\`\`
:::
```

### Q4：`vitepress-rc` 扫描不到我的组件怎么办？

`vitepress-rc` 默认读取 `docs/.vitepress/components` 目录或项目根目录的 `components.json` 配置。

如果你的组件库在 `src/components` 目录下，有两种解决方案：

**方案 A**：创建软链接

```bash
mkdir -p docs/.vitepress/components
ln -s ../../../src/components docs/.vitepress/components/ui
```

**方案 B**：创建 `components.json`

```json
{
  "componentsDir": "./src/components"
}
```

## 总结

官方文档**信息是全的**，但问题是**缺少「连接逻辑」**：

- 写了 `import from './useComponents'`，没写这个文件从哪来
- 写了 `vitepress-rc` 命令，没写它的作用
- 写了 `&&` 串联命令，没写为什么要先执行

**核心要点**：

1. `vitepress-rc` 会生成 `useComponents.ts` 文件
2. 必须先运行 `vitepress-rc`，再启动 VitePress
3. 3.x 版本不支持 `:::demo` 后面的描述文字，需要用普通 Markdown 代替
4. 从本地导入 `useComponents`，不是从包导入
5. `demoblockPlugin`（Markdown-it）负责解析语法，`demoblockVitePlugin`（Vite）负责编译组件

希望这篇文章能帮你理解 demoblock 的配置原理，少踩坑。
