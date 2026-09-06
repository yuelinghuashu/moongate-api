---
title: "Design System: DTCG Three-Layer Architecture & Dark/Light Variants"
description: Use the DTCG design token standard to manage colors and build dark/light variants with visual weight parity.
date: 2026-08-06 04:00:00
series: design-system
tags:
  - Design System
  - Theme
  - Engineering
---

At the end of the previous article, we pointed out three pain points of the engineering approach:

- `colors.yaml` is a **flat pool of variables**. The hierarchy between colors relies purely on naming conventions — there is no structural constraint.
- Dark and light themes need **two separate color variable files**, and maintaining the same hue while varying lightness is done entirely manually.
- The build script only does variable replacement; it **cannot automatically validate** whether colors meet contrast standards.

This article solves these problems. It introduces the three-layer architecture from the **DTCG (Design Tokens Community Group) design token standard**, upgrading colors from "scattered variables" into "structured engineering assets", and uses it to build dark/light variants — a key step toward a design system.

---

## 1. Core Idea: From "Multiple Themes" to "Design System"

A great theme should not have just one face. Offering both dark and light variants not only covers a wider range of user needs, but is also a key step toward a design system.

But the **real value of multiple themes is not having a few extra color sets** — it is this: **all themes share the same rules, and the only thing that differs is the concrete values of the color variables.**

- Language rules (`languages/*.yaml`) are fully reused across all variants.
- UI colors (`workbench.yaml`) and semantic rules (`semantic.yaml`) only reference variable names, never hardcoded color values.
- Each variant provides only one "semantics layer" that defines the concrete color of each semantic role in that variant.

To achieve this, you need a management system that turns "colors" into structured engineering assets — that is the DTCG three-layer architecture.

---

## 2. The DTCG Three-Layer Architecture

DTCG (Design Tokens Community Group) is an industry standard organization under the W3C, aiming to establish a unified format for Design Tokens. Its core idea can be summarized in one simple question: **for any color value, who defines it, who references it, and under what name does it exist?**

Moongate uses the three-layer architecture recommended by DTCG:

```yaml
┌─────────────────────────────────────────────────┐
│  Primitives Layer                                │
│  src/core/primitives/colors.yaml                 │
│  Named by hue-lightness: blue-500, gray-900      │
└───────────────────────┬─────────────────────────┘
                        │ Semantics references primitives with {token}
                        ▼
┌─────────────────────────────────────────────────┐
│  Semantics Layer                                 │
│  src/core/semantics/dark.yaml + light.yaml       │
│  Defines roles: primary, bg, surfaceGround       │
│  primary: "{blue-500}"                           │
└───────────────────────┬─────────────────────────┘
                        │ Components reference semantics with ${variable}
                        ▼
┌─────────────────────────────────────────────────┐
│  Components Layer                                │
│  src/workbench.yaml + semantic.yaml              │
│  Maps UI elements                                │
│  editor.background: "${surfaceGround}"           │
└─────────────────────────────────────────────────┘
```

### 2.1 Layer 1: Primitives

The primitives layer is the **physical truth of every color** — it carries no semantics, and is named only by hue and lightness.

```yaml
# src/core/primitives/colors.yaml
# ==================== Blue family ====================
blue-500: "#3b82f6" # dark primary
blue-600: "#2563eb" # dark button hover
blue-700: "#0284c7" # light primary
blue-800: "#0369a1" # light highlight/function

# Glow blues (special)
blue-glow: "#7dd3fc" # dark highlight
blue-glow-dark: "#87cefa" # dark function

# ==================== Grayscale (cool base) ====================
gray-900: "#0f172a" # dark editor background
gray-850: "#131c31" # dark card/overlay
gray-800: "#1e293b" # dark sidebar/code block
gray-750: "#252e40" # dark hover background
gray-700: "#2d3748" # dark border
# ... a complete hue-lightness scale
```

#### Naming convention

`hue-lightness`, for example `blue-500`, `green-400`, `gray-900`. This naming is not just for looks — it makes "how the same hue changes across lightness levels" traceable.

#### The value of primitives

when you need to "make the primary color of all themes bluer", you only adjust two primitives, `blue-500` and `blue-700` — every semantic color referencing them updates automatically.

### 2.2 Layer 2: Semantics

The semantics layer defines **roles** — `primary`, `bg`, `surfaceGround` — and assigns a primitive to each role. **The semantics layer is not bound to any UI element**; it only answers one question: "what color should the primary be?"

Each variant has its own independent semantics file:

```yaml
# src/core/semantics/dark.yaml (dark semantics layer)
# ==================== Moon-semantic primary colors ====================
primary: "{blue-500}"
success: "{green-400}"
warning: "{yellow-400}"
error: "{red-400}"
highlight: "{blue-glow}"

# Functional colors (syntax)
function: "{blue-glow-dark}"
operator: "{gray-600}"
comment: "{gray-525}"
variable: "{gray-200}"

# Elevation system
surfaceGround: "{gray-900}"
surfaceRaised: "{gray-850}"
surfaceFloating: "{gray-800}"
surfaceTooltip: "{gray-750}"
```

```yaml
# src/core/semantics/light.yaml (light semantics layer)
# Variable names exactly match the dark version; only values differ
# ==================== Moon-semantic primary colors ====================
primary: "{blue-700}"
success: "{green-600}"
warning: "{yellow-700}"
error: "{red-700}"
highlight: "{blue-800}"

# Functional colors (syntax)
function: "{blue-800}"
operator: "{gray-600}"
comment: "{gray-600}"
variable: "{gray-900}"

# Elevation system
surfaceGround: "{gray-50}"
surfaceRaised: "{white}"
surfaceFloating: "{gray-100}"
surfaceTooltip: "{gray-200}"
```

#### Key principle

the semantic-layer variable names of **all variants must be identical**. This is the foundation of rule reuse — `workbench.yaml` and `languages/*.yaml` do not need to know whether the current theme is dark or light; they only reference `${primary}`, `${surfaceRaised}`, and let the semantics layer decide the concrete values.

### 2.3 Layer 3: Components

The components layer **directly maps UI elements** and syntax roles. It only references semantic variables, and never writes concrete color values:

```yaml
# src/workbench.yaml (components layer: UI colors)
editor.background: "${surfaceGround}"
editor.foreground: "${text}"
titleBar.activeBackground: "${surfaceRaised}"
titleBar.activeForeground: "${text}"
statusBar.background: "${surfaceGround}"
statusBar.foreground: "${textDim}"
sideBar.background: "${surfaceRaised}"
# ... all UI keys
```

```yaml
# src/semantic.yaml (components layer: semantic highlighting)
variable: "${variable}"
variable.readonly:
  foreground: "${variableDim}"
  fontStyle: "italic"
function: "${function}"
function.declaration:
  foreground: "${function}"
  fontStyle: "bold"
class: "${warning}"
# ... semantic rules
```

### 2.4 Two Reference Syntaxes: `{token}` vs `${variable}`

In Moongate's architecture, the two kinds of "references" have completely different meanings:

| Syntax        | Meaning                                                              | Where it is used                | Example                                 |
| ------------- | -------------------------------------------------------------------- | ------------------------------- | --------------------------------------- |
| `{token}`     | **Cross-layer reference**: reference another token                   | Semantics references primitives | `primary: "{blue-500}"`                 |
| `${variable}` | **Variable substitution**: replaced by the final color at build time | Components reference semantics  | `editor.background: "${surfaceGround}"` |

- The semantics layer references primitives with `{token}`; the build script resolves token references recursively (with cycle detection — you will see this in the build system article).
- The components layer references semantic variables with `${variable}`; the build script substitutes the final color value (supporting transparency suffixes such as `${primary}20`).

This division is not formalism — it makes every file clear about "which layer it belongs to and what it may reference". In the build system article, you will even see the build script detect "components directly referencing primitives" as an architecture violation and warn about it.

---

## 3. Dark/Light Variants and Gravity Compensation

With the three-layer architecture, building dark/light variants becomes natural: **all rule files are fully reused; only the semantics layer differs**. But the values in the semantics layer are not picked at random — dark and light need a scientific mapping rule.

### 3.1 Why a Light Theme Is Not an "Inversion" of the Dark One

Many beginners think a light theme is just the inverted colors of the dark theme. That is completely wrong:

- Bright colors on a dark background are **"emitters"** — they gain presence by being brighter than the background.
- Dark colors on a light background are **"absorbents"** — they gain presence by being darker than the background.

Take `primary`: on dark, the bright blue `#3b82f6` (60% lightness) looks great. But if you copy it directly onto a white background, it "disappears" because its contrast against white is insufficient. To achieve visual weight parity, you must perform **gravity compensation** — keep the hue unchanged while scientifically adjusting lightness and saturation.

### 3.2 Moongate's Compensation Examples

| Semantic role | Dark version      | Light version     | Adjustment method                                    |
| ------------- | ----------------- | ----------------- | ---------------------------------------------------- |
| primary       | `#3b82f6` (60% L) | `#0284c7` (48% L) | Blue unchanged, lightness down ~20%, fits white      |
| success       | `#34d399` (65%)   | `#059669` (40%)   | Green more grounded, keeps contrast                  |
| warning       | `#fbbf24` (75%)   | `#b45309` (35%)   | Bright yellow to amber, avoids disappearing on white |
| error         | `#f87171` (60%)   | `#b91c1c` (35%)   | Deep red keeps its warning feel                      |

#### Compensation rules

- **Hue (H) stays unchanged** — this is the core of semantic consistency. `primary` stays blue forever, so users do not need to relearn when switching themes.
- **Lightness (L) decreases** — bright colors from the dark theme need to be darker to reach equal contrast on white. Usually 20–30% lower.
- **Saturation (S) is adjusted moderately** — high saturation creates a pleasant "glow" on dark backgrounds, but can be harsh on light ones. Usually 10–20% lower.

> 💡 **How to determine the compensated value scientifically**: adjusting lightness by feel is inefficient and inconsistent. Use the **HSL color model**: convert HEX to HSL, keep H unchanged, and adjust L and S. You can do this programmatically with tools such as [chroma.js](https://gka.github.io/chroma.js/):
>
> ```javascript
> const darkPrimary = chroma("#3b82f6")
> const lightPrimary = darkPrimary.set("hsl.l", 0.48).set("hsl.s", 0.7)
> ```

### 3.3 Elevation System: Physical Depth for the UI

Beyond syntax colors, the UI needs a consistent "sense of space" across both themes. The elevation system expresses physical depth by defining a staircase of background lightness levels:

- Dark mode: the higher the elevation, the brighter the surface (lightness increases).
- Light mode: the higher the elevation, the brighter the surface too (using brighter whites/light grays).
- The step size is consistent between levels, producing a smooth layering feel.

#### Moongate's four elevation levels

| Elevation level   | Purpose                        | Dark mode | Light mode | Notes                                   |
| ----------------- | ------------------------------ | --------- | ---------- | --------------------------------------- |
| `surfaceGround`   | Ground (editor)                | `#0f172a` | `#f9fafb`  | base level                              |
| `surfaceRaised`   | Sidebar, activity bar, tab bar | `#1a2538` | `#ffffff`  | dark +5%, light pure white              |
| `surfaceFloating` | Panels, floating cards, menus  | `#25364a` | `#f1f5f9`  | dark another +5%, light cool light gray |
| `surfaceTooltip`  | Tooltips, popups               | `#2e3b4d` | `#e2e8f0`  | highest level                           |

> 📌 **Note**: whether the light mode uses "higher = brighter" or "higher = darker" is a design choice. Moongate chooses the light mode with floating layers **brighter** (whiter) than the background, creating a "stacked sheets of paper" transparency; the dark mode uses "higher = brighter", letting floating layers gently rise from the background. Both modes follow the physical metaphor of "the higher, the more prominent".

This makes the sidebar gently raised, popups float lightly, and the code area stay deep and calm — the editor moves from flat to three-dimensional.

---

## 4. Automatically Building Both Themes

With the three-layer architecture, the build script only needs to do one thing: **scan the `semantics/` directory and generate one theme JSON per semantics file**.

Using a simplified version of `scripts/build.js`:

```javascript
// scripts/build.js (simplified — full version in the build system article)
import fs from "node:fs"
import path from "node:path"
import yaml from "js-yaml"

const ROOT_DIR = process.cwd()

// Path config
const PATHS = {
  primitives: path.join(ROOT_DIR, "src", "core", "primitives", "colors.yaml"),
  semanticsDir: path.join(ROOT_DIR, "src", "core", "semantics"),
  workbench: path.join(ROOT_DIR, "src", "workbench.yaml"),
  semantic: path.join(ROOT_DIR, "src", "semantic.yaml"),
  langDir: path.join(ROOT_DIR, "src", "languages"),
  outputDir: path.join(ROOT_DIR, "themes"),
}

// Load primitives
const primitives = yaml.load(fs.readFileSync(PATHS.primitives, "utf8"))

// Resolve token references {token}: semantics reference primitives
function resolveTokens(obj, tokenMap, depth = 0) {
  const MAX_DEPTH = 20
  if (depth > MAX_DEPTH) {
    throw new Error(
      `[ENGINEERING_FATAL] Token circular reference detected: ${JSON.stringify(obj)}`,
    )
  }
  if (typeof obj === "string") {
    return obj.replace(/\{([a-zA-Z0-9_-]+)\}/g, (match, key) => {
      const value = tokenMap[key]
      if (value === undefined) {
        console.warn(
          `⚠️ Warning: token "${key}" is not defined, leaving it as-is`,
        )
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

// Replace variables ${var}: components reference semantics (supports transparency suffix)
function replaceVariables(obj, colors) {
  if (typeof obj === "string") {
    return obj.replace(
      /\$\{([a-zA-Z0-9_-]+)\}([0-9a-fA-F]{2})?/g,
      (match, key, alpha) => {
        const value = colors[key]
        if (value === undefined) {
          console.warn(
            `⚠️ Warning: variable "${key}" is not defined, leaving it as-is`,
          )
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

// Load common rules
const workbenchRaw = yaml.load(fs.readFileSync(PATHS.workbench, "utf8"))
const semanticRaw = yaml.load(fs.readFileSync(PATHS.semantic, "utf8"))

// Scan and merge language rules
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

// Read package.json to get the theme base name
const pkg = JSON.parse(
  fs.readFileSync(path.join(ROOT_DIR, "package.json"), "utf8"),
)
const baseName = pkg.name.replace(/[^a-z0-9-]/gi, "-").toLowerCase()

// Build one theme per semantics file
const semanticFiles = fs
  .readdirSync(PATHS.semanticsDir)
  .filter((f) => f.endsWith(".yaml"))

semanticFiles.forEach((semanticFile) => {
  const themeType = path.basename(semanticFile, ".yaml") // 'dark' or 'light'
  const semantics = yaml.load(
    fs.readFileSync(path.join(PATHS.semanticsDir, semanticFile), "utf8"),
  )

  // 1. Resolve token references {token} in semantics -> final color values
  const resolved = resolveTokens(semantics, primitives)

  // 2. Replace variables ${var} in the components layer
  const uiColors = replaceVariables(workbenchRaw, resolved)
  const semanticColors = replaceVariables(semanticRaw, resolved)
  const tokenColors = replaceVariables(tokenColorsRaw, resolved)

  // 3. Assemble the theme object
  const type = themeType.includes("light") ? "light" : "dark"
  const theme = {
    name: `${pkg.displayName || "My Theme"} ${themeType === "dark" ? "Dark" : "Light"}`,
    type: type,
    colors: uiColors,
    tokenColors: tokenColors,
    semanticTokenColors: semanticColors,
  }

  // 4. Write the output file
  const outputFile = path.join(PATHS.outputDir, `${baseName}-${themeType}.json`)
  if (!fs.existsSync(PATHS.outputDir)) {
    fs.mkdirSync(PATHS.outputDir, { recursive: true })
  }
  fs.writeFileSync(outputFile, JSON.stringify(theme, null, 2))
  console.log(`   ✅ Built: ${outputFile}`)
})
```

### Core logic

1. Load primitives.
2. Scan the `semantics/` directory; each semantics file (`dark.yaml` / `light.yaml`) maps to one theme.
3. Resolve `{token}` references in the semantics layer to get the variant's final color dictionary.
4. Replace `${var}` in the components layer with the final color values.
5. Output `${baseName}-dark.json` and `${baseName}-light.json`.

**Cost of adding a theme**: you only add a new YAML semantics file under `semantics/` (such as `sepia.yaml`); the build script automatically scans and generates the corresponding theme. **No rule file needs to change.**

> 🔮 **Preview**: the build logic is getting more complex — parsing tokens, replacing variables, merging rules, scanning semantics layers. You may ask: **how do we prove it is correct?** When colors are wrong or scopes do not match, how do we detect it automatically? That is why the next article introduces **automated testing and quality validation**.

---

## 5. Registering Multiple Themes

In `package.json`, add one entry per theme under `contributes.themes`, paying attention to the `uiTheme` field:

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

- **The `uiTheme` field decides the base color scheme**:
  - Dark theme: `"vs-dark"`
  - Light theme: `"vs"`
- **`label`** is shown in the VS Code "Color Theme" picker.

> 📌 **An easy trap**: if a light theme's `uiTheme` is mistakenly set to `"vs-dark"`, VS Code renders the controls (scrollbars, input boxes, etc.) using the dark base scheme — the light theme ends up with dark controls, which looks awkward. Always set the correct `uiTheme` based on the theme type.

---

## 6. Cross-Platform Assets: Colors Are Not Just a Theme

The DTCG three-layer architecture has one more huge benefit: **the semantic color dictionary can be exported directly as cross-platform assets**.

Along with generating the theme JSON, the build script can also generate a CSS variables file:

```css
/* themes/moongate-colors.css (auto-generated) */
:root,
.light {
  --ui-primary: #0284c7;
  --ui-bg: #f9fafb;
  --ui-surface-raised: #ffffff;
  /* ... all light-mode semantic colors */
}

.dark {
  --ui-primary: #3b82f6;
  --ui-bg: #0f172a;
  --ui-surface-raised: #1a2538;
  /* ... all dark-mode semantic colors */
}
```

This means your blog, component library, and docs site can **directly reference the theme's visual language**:

```html
<link rel="stylesheet" href="/themes/moongate-colors.css" />
```

```css
body {
  background: var(--ui-bg);
  color: var(--ui-text);
}
```

Switching between dark and light is just adding/removing a `.dark` class on the root element:

```javascript
document.documentElement.classList.toggle("dark")
```

Moongate does exactly this — the blog, the design-system docs, and the VS Code theme all share the same colors, so "one color system runs through every product". Besides CSS, SCSS and TypeScript tokens can also be generated automatically (see the build system article).

---

## 7. Common Problems and Pitfalls

| Problem                                                   | Likely cause                                                        | Solution                                                                                                        |
| --------------------------------------------------------- | ------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| **Theme does not appear in the color theme list**         | Not registered correctly in `package.json`, or wrong JSON file path | Check the `contributes.themes` entries; make sure `path` points to the right file                               |
| **Light theme renders as dark**                           | `uiTheme` mistakenly set to `"vs-dark"`                             | Light themes should use `"vs"`                                                                                  |
| **Color variables not replaced, still `${var}`**          | Typo in variable name, or the semantics layer does not define it    | Check names match; make sure every semantic variable exists in both `dark.yaml` and `light.yaml`                |
| **Dark/light themes differ too much visually**            | Unreasonable gravity compensation, uneven lightness adjustment      | Follow the rule: hue unchanged, lightness lowered regularly, saturation adjusted moderately                     |
| **One theme errors after adding a new semantic variable** | `dark.yaml` and `light.yaml` variable names diverge                 | All variants' semantic-layer variable names must be identical                                                   |
| **Build script "file not found"**                         | A required YAML file is missing                                     | Ensure `src/core/primitives/`, `src/core/semantics/`, `src/languages/`, etc. exist and contain the needed files |

---

## 8. Summary

By introducing the DTCG three-layer architecture, you upgrade "multiple themes" into a "design system":

- ✅ **Primitives layer**: colors named by hue-lightness become traceable physical facts.
- ✅ **Semantics layer**: roles decouple from variants; each variant only defines "what color each role should be".
- ✅ **Components layer**: rule files are fully reused and never write concrete color values.
- ✅ **Gravity compensation**: the same semantic role has equal visual weight across backgrounds.
- ✅ **Elevation system**: the UI has physical depth, with consistent layering in both modes.
- ✅ **One-click extension**: adding a theme only requires adding one semantics file.
- ✅ **Cross-platform asset**: the semantics layer exports CSS variables directly, running one color system through every product.

But you may have noticed that the build script in this article is still fairly simple — it only does variable substitution, and it **cannot yet validate** whether colors meet contrast standards, whether undefined variables are referenced, or whether architecture violations have crept in. And as the language count grows (Moongate currently supports 15 languages), the "rule written but never matches" problem will surface too.

Those are exactly what the next article solves — **how to turn the build script itself into a testable, verifiable engineering system**.

---

> **📎 Implementation Reference**
>
> This series is built upon the Moongate Theme project. You can explore the complete source code and install it directly:
>
> - **Code**: [github.com/yuelinghuashu/moongate-theme](https://github.com/yuelinghuashu/moongate-theme)
> - **Install**: [VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=yuelinghuashu.moongate-theme)
