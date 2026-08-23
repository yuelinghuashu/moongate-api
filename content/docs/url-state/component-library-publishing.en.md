---
title: 'From Code to npm: Publishing a Vue 3 Component Library and Avoiding Pitfalls'
description: 'Complete workflow of publishing the moongate-vue component library to npm: build verification pipeline, distribution contract, nrm registry management, 2FA and WebAuthn network pitfalls, local link testing, bundle budget, automation scripts, and a production release checklist.'
date: 2026-05-20
permalink: 6b5acf3d-2c8c-421b-ab33-404ad767f18b
level: P4
series: 
tags:
  - CI/CD
  - Security
  - Engineering
---

> The component library is done, but publishing to npm is the real test: 2FA, network proxies, registry switching, build artifact verification… This guide covers all the pitfalls encountered and the final standardized release workflow in one go.

## 1. Introduction

After completing the component library development, the final step is also the crucial one—**publishing to npm**. This process seems straightforward, but it hides plenty of modern engineering baggage: package name conflicts, strict 2FA validation, security keys (WebAuthn) getting stuck under certain network conditions, frequent registry mirror switching… and—**the correctness of build artifacts**.

This article documents my publishing journey from **v0.0.1** all the way to **v1.5.0**.

## 2. Pre-Publishing Preparation

### 2.1 Build and Verification Pipeline (v1.5.0 Actual)

The `build` script in v1.5.0 is an automated verification pipeline:

```bash
# package.json scripts
"build": "pnpm run clean && vite build && pnpm run build:types && pnpm run clean:dts && pnpm run copy:reset && pnpm run verify:build && pnpm run check:size",
"prepublishOnly": "pnpm build && pnpm test"
```

Responsibilities of each step:

| Script           | Purpose                                                                                                    |
| ---------------- | ---------------------------------------------------------------------------------------------------------- |
| `vite build`     | Bundles JS + CSS artifacts (27 component multi-entry)                                                      |
| `build:types`    | Generates `.d.ts` type declarations via vue-tsc                                                            |
| `clean:dts`      | Cleans up redundant type files from artifacts                                                              |
| `copy:reset`     | Copies `reset.css` to dist                                                                                 |
| `verify:build`   | Verifies .js / .d.ts / style.css / reset.css exist for **each component entry** + export names are correct |
| `check:size`     | Bundle budget check (Min+Gzip ≤ 25KB)                                                                      |
| `prepublishOnly` | Forces build + test before publishing                                                                      |

Core logic of `verify-build.js` — preventing missed component entry publications:

```javascript
// Each component: dist/<kebab>.js exists + dist/exports/<Name>.d.ts exists + export name is correct
for (const [componentName, kebabName] of Object.entries(componentEntries)) {
  const jsPath = join(distDir, `${kebabName}.js`)
  const dtsPath = join(distDir, "exports", `${componentName}.d.ts`)
  if (!existsSync(jsPath)) {
    errors.push(`Missing component artifact: ${kebabName}.js`)
  }
  // Any error → process.exit(1) prevents publishing
}
```

### 2.2 Distribution Contract (package.json)

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
    // ... 27 component entries
  },
  "files": ["dist"],
  "sideEffects": ["*.css"],
  "peerDependencies": {
    "vue": "^3.5.0"
  }
}
```

**Key Differences (vs initial version)**:

- **Pure ES Module**: Only `.js`, no `.cjs` — this is a deliberate choice (all modern bundlers support ESM, and having an `import` condition in `exports` suffices)
- **27 on-demand export entries**: Users can `import Button from 'moongate-vue/button'` to load only the needed component
- **peerDependencies raised to `^3.5.0`**: Because `useId()` (Vue 3.5+ API) is used
- `sideEffects: ["*.css"]`: Prevents bundlers from tree-shaking away CSS

> **💡 Bundle Size Defense Tip**: The `files` field is a "whitelist". With `["dist"]` configured, npm only uploads the `dist` directory. `package.json`, `README.md`, and `LICENSE` are automatically included by npm.

### 2.3 Managing npm Registry with nrm

{% collapsible 🛠️ nrm Registry Switching Details %}

Publishing to npm requires the official registry. If you previously switched to a domestic mirror for faster downloads, it's recommended to use `nrm`.

```bash
npm install -g nrm
nrm ls
nrm use npm        # Switch to official registry for publishing
nrm current
```

> **Tip**: The domestic Taobao npm mirror has migrated to `https://registry.npmmirror.com`.

{% endcollapsible %}

### 2.4 Local Link Testing

Before publishing to npm, it's best to test in a real project first:

```bash
# 1. In the component library directory: build + global link
pnpm build
pnpm link --global

# 2. In the test project directory: link local component library
pnpm link /home/dark/projects/moongate-vue
```

Testing checklist:

- [ ] Components render correctly
- [ ] Style file import works (`import 'moongate-vue/style.css'`)
- [ ] On-demand entry works (`import Button from 'moongate-vue/button'`)
- [ ] TypeScript type hints work correctly
- [ ] HMR hot reload works

**Note**: After testing, to remove the link you need: `pnpm remove moongate-vue` + manually clean up `link:` entries.

---

## 3. Overcoming the Two-Factor Authentication (2FA) Quagmire

npm requires two-factor authentication to be enabled for publishing. npm has fully embraced the **security key (WebAuthn)** model.

{% collapsible 🛠️ WebAuthn Network Troubleshooting Details %}

> **⚠️ Industrial-Grade Pitfall Warning**: npm's WebAuthn verification attempts to interact with Google's verification service. Under domestic network conditions, using Chrome/Edge to trigger the security key window is **extremely prone to timeouts, unresponsiveness, or errors due to network issues**.

| Browser     | Requires Global Proxy | Success Rate | Recommendation      |
| ----------- | --------------------- | ------------ | ------------------- |
| **Chrome**  | ✅ Must enable        | Very high    | **First choice**    |
| **Edge**    | ❌ Not required       | High         | Second choice       |
| **Firefox** | ❌ Not required       | Very low     | **Not recommended** |

{% endcollapsible %}

### 3.2 CI/CD Alternative: Granular Access Token

```bash
//registry.npmjs.org/:_authToken=your_granular_token_value
```

Select **Read and Write** permissions, and check **"Bypass two-factor authentication for automation"**.

---

## 4. Standardized Release Workflow

### 4.1 Manual Publishing Steps

1. `nrm use npm`
2. `npm login` (complete 2FA in browser)
3. `npm version patch|minor|major` (note `--no-git-tag-version` is optional)
4. `npm publish --access public`

> Scoped packages must include `--access public`.

### 4.2 Step-by-Step Automation Scripts

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

## 5. Final Pre-Publishing Checklist

- [ ] **Build succeeds**: `pnpm run build` completed without errors (including verify-build + check:size)
- [ ] **Artifacts complete**: `dist/` contains index.js, 27 component .js files, style.css, reset.css, index.d.ts
- [ ] **Bundle size meets target**: Min+Gzip ≤ 25KB (confirmed by tree-shake-check.js output)
- [ ] **Tests pass**: `pnpm test` (450 tests all green)
- [ ] **Version is clean**: Current version number has never existed on npm
- [ ] **Local sandbox verified**: Styles + on-demand entry + types all working
- [ ] **peerDependencies correct**: vue ^3.5.0

---

## 6. FAQ

### Q1: Publishing returns 403/404?

403 = package name is taken or not logged in. 404 = forgot `nrm use npm` and published to a read-only mirror.

### Q2: 2FA won't show the security key window?

Confirm that the proxy is set to global/TUN mode. If it still fails, use a Granular Access Token.

### Q3: Users get a white screen after installing styles?

Confirm that `sideEffects: ["*.css"]` is configured, and users explicitly `import 'moongate-vue/style.css'`.

### Q4: Users can `import 'moongate-vue'` but `import 'moongate-vue/button'` fails?

Check whether the `/button` sub-path is declared in `exports`. v1.5.0 has all 27 components fully configured.

---

## 7. Conclusion

The publishing phase is often treated as "the last chore," but it's actually just as important as component design—**the correctness of build artifacts, restraint in bundle size, and API stability** all depend on this stage holding the line.

Publishing in v1.5.0 is no longer a simple `npm publish`, but an automated pipeline guarded by **verify-build.js + tree-shake-check.js + 450 tests** working together.

May your component library also cross the quagmire and reach further horizons. 🚀

---

## 🌙 About Moongate Vue

This article is based on the real publishing practice of [Moongate Vue](https://github.com/yuelinghuashu/moongate-vue). Related resources:

- **Project Repository**: [github.com/yuelinghuashu/moongate-vue](https://github.com/yuelinghuashu/moongate-vue) — Minimalist Vue 3 component library, zero dependencies, CSS-first, 25KB gzip
- **Live Example**: [moongate.top](https://moongate.top) — Personal blog, migrated from Nuxt UI v4 to Moongate Vue
- **Online Documentation**: [vue.moongate.top](https://vue.moongate.top) — Component API and theme customization guide
