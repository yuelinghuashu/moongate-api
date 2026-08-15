---
title: 从代码到 npm：Vue 3 组件库发布实战与避坑指南
description: 记录 moongate-vue 组件库从构建到 npm v1.5.0 发布的完整流程，涵盖 nrm 源管理、2FA 配置、WebAuthn 网络代理、本地链接测试、构建验证、体积预算、自动化脚本及工业级发布检查清单。
date: 2026-05-20
permalink: 6b5acf3d-2c8c-421b-ab33-404ad767f18b
series: moongate-vue
level: P4
tags:
  - CI/CD
  - Security
  - Engineering
---

> 记录 `moongate-vue` 组件库从构建到发布的完整流程，以及 2FA 验证、网络代理解密、npm 源自动化管理等实战经验。

## 📚 系列导航

本系列共五篇：

1. [**设计令牌 vs 原子化 CSS（理念篇）**](./design-tokens-vs-atomic-css) —— 设计令牌优先的架构结论
2. [**CSS 优先 + 组件薄封装（架构篇）**](./css-first-component-library) —— 四层 CSS 架构与体积验证
3. [**Vue 3 简单组件开发实战（简单组件篇）**](./vue-component-api-design) —— Button 组件的 API 设计
4. [**Vue 3 复杂组件开发实战（复杂组件篇）**](./complex-component-api-design) —— Select/Pagination 的工业级细节
5. [**从代码到 npm（发布篇）**](./component-library-publishing) —— 发布实战与避坑指南

## 一、引言

组件库开发完成后，最后一步也是至关重要的一步：**发布到 npm**。这个过程看似简单，实则暗藏不少现代工程包袱：包名冲突、2FA 强校验、安全密钥（WebAuthn）在特定网络下的卡死、源镜像频繁切换……以及——**构建产物的正确性**。

本文作为系列收官之作，记录了我从 **v0.0.1** 一路到 **v1.5.0** 的发布实战。

## 二、发布前的准备

### 2.1 构建与验证链路（v1.5.0 实际）

v1.5.0 的 `build` 脚本是一条自动化验证流水线：

```bash
# package.json scripts
"build": "pnpm run clean && vite build && pnpm run build:types && pnpm run clean:dts && pnpm run copy:reset && pnpm run verify:build && pnpm run check:size",
"prepublishOnly": "pnpm build && pnpm test"
```

每一步的职责：

| 脚本             | 作用                                                                         |
| ---------------- | ---------------------------------------------------------------------------- |
| `vite build`     | 打包 JS + CSS 产物（27 组件多入口）                                          |
| `build:types`    | vue-tsc 生成 `.d.ts` 类型声明                                                |
| `clean:dts`      | 清理产物中的冗余类型文件                                                     |
| `copy:reset`     | 复制 `reset.css` 到 dist                                                     |
| `verify:build`   | 验证**每个组件入口**的 .js / .d.ts / style.css / reset.css 存在 + 导出名正确 |
| `check:size`     | 体积预算检查（Min+Gzip ≤ 25KB）                                              |
| `prepublishOnly` | 发布前强制 build + test                                                      |

`verify-build.js` 的核心逻辑——防止漏发布某个组件入口：

```javascript
// 每个组件：dist/<kebab>.js 存在 + dist/exports/<Name>.d.ts 存在 + 导出名正确
for (const [componentName, kebabName] of Object.entries(componentEntries)) {
  const jsPath = join(distDir, `${kebabName}.js`)
  const dtsPath = join(distDir, "exports", `${componentName}.d.ts`)
  if (!existsSync(jsPath)) {
    errors.push(`缺少组件产物: ${kebabName}.js`)
  }
  // 任一错误 → process.exit(1) 阻止发布
}
```

### 2.2 分发契约（package.json）

```json
{
  "name": "moongate-vue",
  "version": "1.5.0",
  "type": "module",
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
      "import": "./dist/button.js",
      "default": "./dist/button.js"
    }
    // ... 27 个组件入口
  },
  "files": ["dist"],
  "sideEffects": ["*.css"],
  "peerDependencies": {
    "vue": "^3.5.0"
  }
}
```

**关键差异（vs 初版）**：

- **纯 ES Module**：只有 `.js`，没有 `.cjs`——这是刻意选择（现代打包工具均支持 ESM，且 `exports` 中有 `import` 条件即可）
- **27 个按需导出入口**：用户可 `import Button from 'moongate-vue/button'` 只加载所需组件
- **peerDependencies 升到 `^3.5.0`**：因为用了 `useId()`（Vue 3.5+ API）
- `sideEffects: ["*.css"]`：防止打包器 tree-shake 误删 CSS

> **💡 包体积防御提示**：`files` 字段是"白名单"。配置 `["dist"]` 后，npm 只上传 `dist` 目录。`package.json`、`README.md`、`LICENSE` 会被 npm 强制包含。

### 2.3 使用 nrm 管理 npm 源

发布到 npm 必须使用官方源。如果你之前为了加速下载切换到了国内镜像，推荐使用 `nrm`。

```bash
npm install -g nrm
nrm ls
nrm use npm        # 发布时切换到官方源
nrm current
```

> **提示**：国内淘宝 npm 镜像已迁移至 `https://registry.npmmirror.com`。

### 2.4 本地集成测试

在发布到 npm 之前，最好在真实项目中先测试一遍：

```bash
# 1. 在组件库目录：构建 + 全局链接
pnpm build
pnpm link --global

# 2. 在测试项目目录：链接本地组件库
pnpm link /home/dark/projects/moongate-vue
```

测试清单：

- [ ] 组件能正常渲染
- [ ] 样式文件导入生效（`import 'moongate-vue/style.css'`）
- [ ] 按需入口生效（`import Button from 'moongate-vue/button'`）
- [ ] TypeScript 类型提示正常
- [ ] HMR 热更新工作正常

**注意**：测试完成后，删除链接需：`pnpm remove moongate-vue` + 手动清理 `link:` 条目。

---

## 三、攻克双重认证（2FA）泥潭

npm 强制要求发布时开启双重认证。npm 已全面拥抱 **安全密钥 (WebAuthn)** 模式。

### 3.1 浏览器选择与网络环境的"隐藏陷阱"

> **⚠️ 工业级避坑警告**：npm 的 WebAuthn 验证会尝试与 Google 验证服务联动。在国内网络环境下，使用 Chrome/Edge 弹出密钥窗口时**极易由于网络超时而无响应或报错**。

| 浏览器      | 是否需要全局代理 | 成功率 | 建议       |
| ----------- | ---------------- | ------ | ---------- |
| **Chrome**  | ✅ 必须开启      | 极高   | **首选**   |
| **Edge**    | ❌ 不需要        | 高     | 次选       |
| **Firefox** | ❌ 不需要        | 极低   | **不建议** |

### 3.2 CI/CD 备选：Granular Access Token

```bash
//registry.npmjs.org/:_authToken=你的_granular_token_值
```

权限勾选 **Read and Write**，并勾选 **"Bypass two-factor authentication for automation"**。

---

## 四、标准化发布流程

### 4.1 手动发布步骤

1. `nrm use npm`
2. `npm login`（浏览器完成 2FA）
3. `npm version patch|minor|major`（注意 `--no-git-tag-version` 可选）
4. `npm publish --access public`

> 作用域包必须加 `--access public`。

### 4.2 分步式自动化脚本

```json
{
  "scripts": {
    "release:pre": "nrm use npm && npm run build && npm test",
    "release:version": "npm version patch --no-git-tag-version",
    "release:tag": "git add package.json && git commit -m \"chore: release v$(node -p 'require(\"./package.json\").version')\" && git tag v$(node -p 'require(\"./package.json\").version')",
    "release:publish": "npm publish --access public",
    "release": "npm run release:pre && npm run release:version && npm run release:tag && npm run release:publish"
  }
}
```

## 五、发布前最终检查清单

- [ ] **构建无误**：`pnpm run build` 未报错（含 verify-build + check:size）
- [ ] **产物完整**：`dist/` 有 index.js、27 个组件 .js、style.css、reset.css、index.d.ts
- [ ] **体积达标**：Min+Gzip ≤ 25KB（tree-shake-check.js 输出确认）
- [ ] **测试通过**：`pnpm test`（450 个测试全绿）
- [ ] **版本干净**：当前版本号从未在 npm 上存在过
- [ ] **本地沙盒**：样式 + 按需入口 + 类型均正常
- [ ] **peerDependencies 正确**：vue ^3.5.0

---

## 六、FAQ

### Q1: 发布返回 403/404？

403 = 包名被占用 或 未登录。404 = 忘了 `nrm use npm` 发布到只读镜像。

### Q2: 2FA 弹不出安全密钥窗口？

确认代理开启全局/TUN 模式。仍失败则用 Granular Access Token。

### Q3: 用户安装后样式白屏？

确认 `sideEffects: ["*.css"]` 已配置，且用户显式 `import 'moongate-vue/style.css'`。

### Q4: 用户能 import 'moongate-vue' 但 import 'moongate-vue/button' 失败？

检查 `exports` 中是否声明了 `/button` 子路径。v1.5.0 已为 27 个组件全部配置。

---

## 七、结语

从第一篇的**设计令牌**，到薄封装**架构**、简单/复杂组件的 **API 设计**，再到今天的 **npm 工业级分发**——五篇文章，见证了一个组件库从零到 v1.5.0 的完整工程闭环。

v1.5.0 的发布不再是简单的 `npm publish`，而是一条由 **verify-build.js + tree-shake-check.js + 450 测试** 共同守护的自动化流水线。**构建产物的正确性、体积的克制、API 的稳定**，是组件库生命线。

愿你的组件库也能跨越泥潭，抵达更远的远方。🚀
