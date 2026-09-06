---
title: "Theme Engineering: From a Monolithic JSON to Modular YAML"
description: Refactor your monolithic theme JSON into modular YAML with a build script, and automate theme generation.
date: 2026-08-06 02:00:00
series: design-system
tags:
  - VSCode
  - Theme
  - Engineering
---

In the first article of this series, you learned how to handwrite a publishable VS Code theme. But as your theme grows, you will probably run into these pain points:

- A single JSON file easily grows to thousands of lines. To change one color you have to search the whole file, and it is easy to edit the wrong one.
- To customize highlighting for each language, you end up piling more and more rules into one `tokenColors` array, which becomes hard to maintain.
- To try a light version, you have to copy the entire file and manually change hundreds of color values.

It is time to introduce engineering! This article will walk you through **refactoring a monolithic JSON theme into a modular, automatically buildable project**: split the source into YAML files, and use a build script to merge them and replace variables automatically.

> 💡 **Where this article fits**: the "single color variable file + build script" approach is the first step of engineering. In the design system article, this approach is upgraded into the DTCG three-layer architecture (primitives → semantics → components). Reading in order will help you understand the motivation behind each step.

---

## Prerequisites

First make sure you have Node.js and npm (or pnpm) installed. Then install the build dependency:

```bash
pnpm add -D js-yaml
# or
npm install --save-dev js-yaml
```

> ⚠️ **Note**: `js-yaml` must go into `devDependencies`, because it is only a build tool and should not be shipped with the theme as a production dependency.

---

## Designing the directory structure

We will put the source under `src/`, the build script under `scripts/`, and the generated JSON under `themes/`:

```text
your-theme/
├── src/
│   ├── core/
│   │   └── colors.yaml          # color variables (primary, background, text, etc.)
│   ├── languages/                # per-language syntax rules
│   │   ├── base.yaml             # shared rules across all languages
│   │   ├── python.yaml
│   │   ├── go.yaml
│   │   └── ... (other languages)
│   ├── workbench.yaml             # UI colors (the colors object)
│   └── semantic.yaml              # semantic highlighting (semanticTokenColors)
├── scripts/
│   └── build.js                   # the build script
├── themes/
│   └── your-theme.json            # the final generated file
├── package.json
└── .vscodeignore
```

> 📌 **Note**: notice that the `languages/` directory has no `javascript.yaml` or `typescript.yaml` — because JS/TS syntax rules are completely covered by the shared rules in `base.yaml`, so no separate files are needed. This is the core idea of the "shared rules + language-specific rules" architecture: **only put the truly unique rules in each language file**.

---

## Step 1: Extract color variables

Open your original theme JSON, find every color value in the `colors` object and the repeated colors in `tokenColors`, and define them as variables. Create `src/core/colors.yaml`:

```yaml
# Core color variables
primary: "#3b82f6" # primary blue
success: "#34d399" # success green
warning: "#fbbf24" # warning yellow
error: "#f87171" # error red
highlight: "#7dd3fc" # glow blue

bg: "#0f172a" # background
bgMuted: "#1e293b" # secondary background
text: "#e2e8f0" # foreground
textMuted: "#94a3b8" # muted text
border: "#2d3748" # border


# ... other variables
```

### ⚠️ Important rules

- Variable names use camelCase or lowercase-with-hyphens, and **only letters, digits, and underscores**.
- **Do not put transparency in the variable itself** (such as the `20` in `#3b82f620`). Transparency should be added through a suffix like `${primary}20` at the point of use; the build script will concatenate it automatically.
- Make sure `colors.yaml` covers every variable referenced by other files; otherwise the build will warn and leave the placeholder untouched.

---

## Step 2: Split the syntax rules

Split the `tokenColors` array into multiple YAML files by language. Using `src/languages/base.yaml` as an example, it holds the rules shared by all languages:

```yaml
# Shared rules (base.yaml)
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
  # ... other shared rules
```

### Core principle

whatever `base.yaml` already covers (keywords, strings, comments, operators, variables, and so on), language files must **not redefine**. Language files should only contain rules that are truly unique to that language.

Using `src/languages/go.yaml` as an example, it only contains Go-specific syntax elements:

```yaml
# Go-specific rules (go.yaml)
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
  # ... other Go-specific rules
```

This "shared + unique" architecture keeps every file's responsibility crystal clear:

- Want to change shared colors? Edit `base.yaml` only — it applies everywhere.
- Want to add Go-specific rules? Touch only `go.yaml`, without affecting other languages.
- Want to see the full palette of a language? Read `base.yaml` plus that language's file.

---

## Step 3: Split UI colors and semantic rules

Move the `colors` object into `src/workbench.yaml`, and replace every color value with a variable reference:

```yaml
# Editor UI colors (workbench.yaml)
editor.background: "${bg}"
editor.foreground: "${text}"
titleBar.activeBackground: "${bg}"
titleBar.activeForeground: "${text}"
statusBar.background: "${bg}"
statusBar.foreground: "${textMuted}"
# ... all UI keys
```

Move `semanticTokenColors` into `src/semantic.yaml`, using variables as well:

```yaml
# Semantic highlight rules (semantic.yaml)
variable: "${variable}"
function: "${function}"
class: "${warning}"
"*.decorator":
  foreground: "${purple}"
  fontStyle: "italic"
# ... other semantic rules
```

### 🔍 Note

in YAML, keys containing special characters (such as `*.decorator`) must be wrapped in double quotes, otherwise YAML parsing will fail.

---

## Step 4: Write the build script

Create `scripts/build.js` using **ESM (ES Modules)**. Its task is:

1. Load `colors.yaml` to get the variable dictionary.
2. Load `workbench.yaml`, `semantic.yaml`, and all language rules under `languages/`.
3. Recursively replace every `${variableName}` with the actual color value (with transparency-suffix support).
4. Merge `tokenColors` (`base.yaml` first, language rules after — later rules take higher priority).
5. Output the final JSON to `themes/`.

```javascript
// scripts/build.js
import fs from "node:fs"
import path from "node:path"
import yaml from "js-yaml"
import { fileURLToPath } from "node:url"

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const ROOT_DIR = path.resolve(__dirname, "..")

// Path config (adjust to your project structure)
const PATHS = {
  colors: path.join(ROOT_DIR, "src", "core", "colors.yaml"),
  workbench: path.join(ROOT_DIR, "src", "workbench.yaml"),
  semantic: path.join(ROOT_DIR, "src", "semantic.yaml"),
  langDir: path.join(ROOT_DIR, "src", "languages"),
  outputDir: path.join(ROOT_DIR, "themes"),
}

// Load the color variables
const colors = yaml.load(fs.readFileSync(PATHS.colors, "utf8"))

// Recursively replace `${var}`, supporting two-digit hex transparency suffixes (e.g. ${primary}20)
function replaceVariables(obj) {
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

// Read and parse a YAML file
function loadYaml(filePath, description) {
  try {
    return yaml.load(fs.readFileSync(filePath, "utf8"))
  } catch (err) {
    console.error(`❌ Failed to parse ${description}:`, err.message)
    return null
  }
}

// Load UI colors and semantic rules
const workbench = replaceVariables(loadYaml(PATHS.workbench, "workbench.yaml"))
const semantic = replaceVariables(loadYaml(PATHS.semantic, "semantic.yaml"))

// Automatically scan the languages/ directory and merge files in filename order
// base.yaml is loaded first (shared rules),
// language-specific rules are merged after (overriding rules with the same scope)
let tokenColors = []

if (fs.existsSync(PATHS.langDir)) {
  const langFiles = fs
    .readdirSync(PATHS.langDir)
    .filter((f) => f.endsWith(".yaml"))
    .sort() // base.yaml naturally sorts first alphabetically

  for (const file of langFiles) {
    const rules = loadYaml(
      path.join(PATHS.langDir, file),
      `language rule ${file}`,
    )
    if (rules?.tokenColors) {
      tokenColors = tokenColors.concat(rules.tokenColors)
      console.log(`   ✅ Loaded: ${file}`)
    }
  }
}

// Replace variables in tokenColors
const processedTokenColors = replaceVariables(tokenColors)

// Ensure the output directory exists
if (!fs.existsSync(PATHS.outputDir)) {
  fs.mkdirSync(PATHS.outputDir, { recursive: true })
}

// Build the final theme object
const theme = {
  name: "Your Theme Name",
  type: "dark",
  colors: workbench,
  tokenColors: processedTokenColors,
  semanticTokenColors: semantic,
}

// Write the file
const outputFile = path.join(PATHS.outputDir, "your-theme.json")
fs.writeFileSync(outputFile, JSON.stringify(theme, null, 2))
console.log("✅ Theme built successfully!")
```

### ⚠️ Notes

- **Automatic scanning instead of manual ordering**: unlike maintaining an `order` array by hand, this directly scans the `languages/` directory and sorts by filename. `base.yaml` naturally sorts first, and the other language files follow in alphabetical order — later rules override earlier rules with the same scope, so language-specific rules naturally take higher priority. To add a language, you only add a YAML file; no need to change the build script.
- If a color variable is undefined, the script warns and leaves the `${var}` placeholder in the output — which makes the theme invalid. Make sure every variable is defined.
- **Transparency suffix format**: the suffix is a two-digit hex number (00–FF), where `20` is roughly 12.5% opacity, `80` is 50%, and `FF` is fully opaque. This notation directly matches the CSS `#RRGGBBAA` format, so the build script can simply concatenate it.
- Variable names must use only letters, digits, and underscores (like `primary`, `primaryColor`) — avoid hyphens or other symbols, because the regex `\$\{([a-zA-Z0-9_-]+)\}` only matches these characters.

---

## Step 5: Integrate into package.json

Add the build command to the `scripts` section of `package.json`, and set up `prepublishOnly` to build automatically:

```json
{
  "scripts": {
    "build": "node scripts/build.js",
    "prepublishOnly": "npm run build"
  }
}
```

🔍 Check: make sure `js-yaml` is in `devDependencies`, not `dependencies`. As a build tool, it should not be shipped with the theme.

---

## Better dev experience: live preview with auto-build

Running `npm run build` manually after every change is tedious. We can add a **watch mode** so the build script runs automatically whenever source files change — an "edit and preview" workflow.

### 1. Install nodemon

`nodemon` is a common tool that watches files and restarts a command automatically. Install it as a dev dependency:

```bash
pnpm add -D nodemon
# or
npm install --save-dev nodemon
```

### 2. Add the watch script

Add these two commands to the `scripts` section in `package.json`:

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

- `--watch src`: watches all file changes under the `src` directory.
- `-e yaml`: only watches files with a `.yaml` extension.
- `--exec "npm run build"`: runs the build command when a file changes.

The `dev` script is just an alias of `watch` for convenience.

### 3. Use watch mode

During development, open a terminal and run:

```bash
npm run watch
# or
npm run dev
```

The terminal stays running. Whenever you edit and save any YAML file under `src/`, the build script runs automatically and regenerates the JSON files under `themes/`.

Combined with VS Code's debug feature, you simply press `F5` to launch the Extension Development Host and keep watch running. After editing the source, run `Developer: Reload Window` in the development host and see the effect immediately — no manual rebuild needed.

### 4. Notes

- `nodemon` is only a development helper; it does not need to ship with the theme, so install it in `devDependencies`.
- If your project structure is more complex, you can customize `--watch` to watch more directories.
- If you prefer to avoid an extra dependency, you can write a simple watcher with Node's built-in `fs.watch` — but `nodemon` is more mature and easier to use.

---

## Step 6: Update .vscodeignore

Make sure your package only ships the final output, not source or dependencies. A typical `.vscodeignore` looks like:

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

💡 Note: `!themes/*.json` keeps the JSON files under `themes/` — these are the build outputs.

---

## Step 7: Test the build

Run the following command and check that the generated JSON matches the original:

```bash
npm run build
```

Then use a diff tool to compare the newly generated `themes/your-theme.json` with the original JSON, and make sure there are no unexpected differences. If there are, check:

- Whether all variables are defined.
- Whether transparency suffixes are correct.
- Whether the language rule merge order matches expectations.

---

## Step 8: Enjoy the benefits of engineering

Your theme source is now modular, and maintaining it becomes trivial:

- Want to change the primary color? Edit one line in `colors.yaml`.
- Want to add Python rules? Add entries to `python.yaml`.
- Want a light version? Create `colors-light.yaml` and adjust the build script to output two themes.

---

## Troubleshooting

| Problem                                                          | Likely cause                                                                    | Solution                                                                                                                     |
| ---------------------------------------------------------------- | ------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `${var}` still appears after the build                           | The variable is not defined in `colors.yaml`                                    | Check the spelling and make sure the variable is defined                                                                     |
| Transparency is wrong                                            | The variable itself already contains alpha, or the suffix format is wrong       | Variables should not contain transparency; use the `${var}20` form                                                           |
| A language's highlighting is missing                             | The language rule file is missing, or the rule is not in the shared `base.yaml` | Add the language file; remember language files only hold unique rules, shared rules belong in `base.yaml`                    |
| Rules get overridden unexpectedly                                | The merge order is not what you expected                                        | Filename ordering controls it: `base.yaml` first, then language files alphabetically — later rules override same-scope rules |
| `vsce package` reports "missing dependencies"                    | `js-yaml` was placed in `dependencies`                                          | Move it to `devDependencies`                                                                                                 |
| Three-digit transparency suffixes (e.g. `200`) are not supported | The script only supports two-digit hex suffixes (e.g. `20`)                     | Always use two-digit hex transparency suffixes, e.g. `${var}20`                                                              |
| Variable names with hyphens are not replaced                     | The regex `\$\{(\w+)\}` only matches letters, digits, and underscores           | Use only letters, digits, and underscores in variable names (e.g. `primary`, `primaryColor`)                                 |

---

## Summary

Through engineering refactoring, you have evolved from a hard-to-maintain JSON monolith into a clear, extensible modular project. You now have:

- ✅ Modular YAML source files (colors, language rules, UI, and semantic highlighting separated)
- ✅ A build script that merges and replaces variables automatically
- ✅ Watch mode for live-preview development
- ✅ Correct auto-build configuration for publishing

But you may have noticed that the approach in this article still has some issues to solve:

- `colors.yaml` is a **flat pool of variables**. The hierarchy between colors (which are primitives, which are semantic roles) relies purely on naming conventions — there is no structural constraint.
- Dark and light themes need **two separate color variable files**, and maintaining the same hue while varying lightness is done entirely manually.
- The build script only does variable replacement; it **cannot automatically validate** whether colors meet contrast standards or whether variables that do not exist are referenced.

These are exactly what the next article solves — **design systems: the DTCG three-layer architecture and dark/light variants**.

---

> **📎 Implementation Reference**
>
> This series is built upon the Moongate Theme project. You can explore the complete source code and install it directly:
>
> - **Code**: [github.com/yuelinghuashu/moongate-theme](https://github.com/yuelinghuashu/moongate-theme)
> - **Install**: [VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=yuelinghuashu.moongate-theme)
