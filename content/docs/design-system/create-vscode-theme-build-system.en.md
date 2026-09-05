---
title: "Build System: Testable, Verifiable Engineering Practices"
description: Turn your build script into an industrial-grade, testable, verifiable engineering system.
date: 2026-08-06 06:00:00
level: P4
series: design-system
tags:
  - VSCode
  - Theme
  - Design System
  - Engineering
  - CI/CD
---

In the design system article, we built a theme production system based on the DTCG three-layer architecture: the build script can automatically generate dark/light themes and export CSS variables.

But as the language count grew to 15 and the build script got more complex, new problems surfaced:

1. **The build script itself lacks architecture** — all the logic is crammed into one `build.js` file, which is hard to test and maintain.
2. **Quality cannot be guaranteed automatically** — do colors meet WCAG contrast standards? Do all referenced variables exist? Has any "components directly referencing primitives" architecture pollution crept in?
3. **Language scopes cannot be verified** — you wrote 20 language rules; how do you confirm every scope actually exists in VS Code's TextMate grammar? How do you automatically detect a "rule written but never applied"?
4. **The output is only theme JSON and CSS** — can we export SCSS, TypeScript, and other formats so design assets can be reused on more platforms?

This article answers these questions, upgrading the build script from "usable" to "industrial-grade" — **a testable, verifiable, maintainable engineering infrastructure**.

> 🗺️ **Roadmap for this article**: five progressive steps —
>
> 1. **Architecture**: split the monolithic script into maintainable modules (`1`)
> 2. **Verification**: let the build prove itself — quality checks and scope validation (`2`, `4`)
> 3. **Optimization**: make the generated output more compact (`3`)
> 4. **Testing**: make the build system itself regression-safe (`5`)
> 5. **Release**: wire verification into ecosystem integration and the delivery pipeline (`6`, `7`, `8`)
>
> This is a long article. I recommend skimming it once to build a mental map, then practicing section by section against your own project — each section ends with a transition to the next.

> 💡 **Tip**: this article involves fairly deep engineering practice, so read the first three articles first. If you would rather understand "what kind of brand ecosystem this system supports" first, you may read the fifth article and come back.

---

## 1. Modular Architecture: From a Monolithic Script to `scripts/lib/`

When the build script exceeds 200 lines, it needs an architecture of its own. Moongate splits the build system into single-responsibility modules under `scripts/lib/`:

```text
your-theme/
├── scripts/
│   ├── build.js                     # main flow (orchestrator, no implementation)
│   ├── verify-scopes.js             # scope validation CLI
│   ├── generate-better-comments.js  # Better Comments config generator
│   └── lib/
│       ├── config.js                # path configuration
│       ├── tokens.js                # token resolution (resolveTokens, replaceVariables)
│       ├── utils.js                 # shared utilities (safeLoadYaml, normalizeHex, etc.)
│       ├── validators.js            # quality validation (WCAG contrast, structure, unused tokens)
│       ├── optimizers.js            # output optimization (token merge, semantic color pruning)
│       ├── generators.js            # multi-format output (CSS/SCSS/TS/design docs)
│       └── scope-validator.js       # scope validation logic
├── src/
│   ├── core/
│   │   ├── primitives/colors.yaml
│   │   ├── semantics/dark.yaml + light.yaml
│   │   └── layout.yaml
│   ├── languages/*.yaml
│   ├── workbench.yaml
│   ├── semantic.yaml
│   └── special/better-comments.yaml
├── themes/                          # generated output
├── docs/DESIGN_SYSTEM.md            # auto-generated docs
├── test/*.test.js                   # automated tests
└── package.json
```

Responsibilities of each module:

| Module               | Responsibility                             | Key exports                                                                                                                              |
| -------------------- | ------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------- |
| `config.js`          | Central path management                    | `PATHS`, `ROOT_DIR`                                                                                                                      |
| `tokens.js`          | Token resolution and variable substitution | `resolveTokens` (`{token}` cross-layer), `replaceVariables` (`${var}` substitution), `detectPrimitiveReference` (architecture pollution) |
| `utils.js`           | Shared utility functions                   | `safeLoadYaml`, `normalizeHex`, `detectDuplicateColors`, `getThemeInfo`                                                                  |
| `validators.js`      | Quality validation                         | `checkContrast` (WCAG), `validateThemeStructure`, `detectUnusedPrimitives`                                                               |
| `optimizers.js`      | Output streamlining                        | `mergeTokenColors`, `optimizeSemanticTokenColors`                                                                                        |
| `generators.js`      | Multi-format output                        | `generateColorCss`, `generateLayoutCss`, `generateScssTokens`, `generateTsTokens`, `generateDesignSystemDoc`                             |
| `scope-validator.js` | Scope validation logic                     | `verifyAllScopes`, `formatVerificationResult`                                                                                            |

### Why ESM instead of CommonJS?

- Modern Node.js (≥ 14) supports ESM natively, with no extra build tooling.
- `import` / `export` are static, so tooling can statically analyze dependency graphs — easier to refactor and debug.
- Declaring `"type": "module"` in `package.json` makes every `.js` file an ES module by default.

`build.js` is the main flow. It only does **orchestration** — loading modules, calling functions, catching errors — and contains no implementation.

> 📌 **Division of labor with the previous article**: you already saw the full implementations of `resolveTokens` and `replaceVariables` in the design system article, so this article will **not repeat them**. Focus on the four **new** modules: `validators` (quality validation), `optimizers` (output streamlining), `generators` (multi-format output), and `scope-validator` (scope validation) — every step in the data-flow map below lands on one of these four modules.

Before reading the code, build a "data-flow map" describing what each key step takes in and produces:

| Step                                                   | Input                                    | Output                                          |
| ------------------------------------------------------ | ---------------------------------------- | ----------------------------------------------- |
| `loadPrimitives()`                                     | `primitives/colors.yaml`                 | Normalized primitive color dictionary           |
| `resolveTokens()`                                      | Primitives + `semantics/*.yaml`          | Final semantic color dictionary per variant     |
| `loadTokenColors()`                                    | Rules under `languages/` + `special/`    | Merged raw tokenColors rules                    |
| `replaceVariables()`                                   | Rule files + semantic color dictionary   | Rules with variables substituted by real colors |
| `mergeTokenColors()` / `optimizeSemanticTokenColors()` | Substituted rules                        | Streamlined output                              |
| `generate*()`                                          | Final color dictionaries + layout tokens | CSS / SCSS / TS tokens and design docs          |

Keep this map in mind while reading the code — it makes the position of every module function in the pipeline clear.

```javascript
// scripts/build.js (simplified main flow)
import { PATHS } from "./lib/config.js"
import { ensureFileExists, safeLoadYaml } from "./lib/utils.js"
import { resolveTokens, replaceVariables } from "./lib/tokens.js"
import {
  detectUnusedPrimitives,
  validateThemeStructure,
  checkContrast,
} from "./lib/validators.js"
import {
  mergeTokenColors,
  optimizeSemanticTokenColors,
} from "./lib/optimizers.js"
import {
  generateColorCss,
  generateLayoutCss,
  generateDesignSystemDoc,
  generateScssTokens,
  generateTsTokens,
} from "./lib/generators.js"

function main() {
  console.log("🚀 Building theme (DTCG standard + industrial-grade QA)...\n")
  try {
    // 1. Check required files
    // 2. Load and normalize primitive colors
    // 3. Generate layout CSS
    // 4. Load common rules (workbench + semantic)
    // 5. Load language and special rules
    // 6. Scan semantics files and detect unused primitives
    // 7. Build a theme for each semantics file
    // 8. Generate CSS variables, cross-platform tokens, and the design-system doc
    console.log("\n🎉 All themes built!")
  } catch (err) {
    console.error(err.message)
    process.exit(1)
  }
}

main()
```

### Core principle

one function does one thing. Each step in `main()` corresponds to a clearly named function (`loadPrimitives()`, `loadCommonRules()`, `loadTokenColors()`, ...), so a caller can see at a glance what every step of the build does.

With the modular skeleton in place, the real engineering problem comes next: **the build process must be able to prove itself**.

---

## 2. Quality Verification: Let the Build "Prove Itself"

The defining trait of an industrial-grade build script: **it does not only generate files — it also validates that the files it generated are correct**. When validation fails, the build stops, so errors are caught before publishing.

### 2.1 Automatic WCAG contrast validation

Every theme color must be clearly readable against its background. Moongate enforces a minimum contrast ratio against `editor.background` for key text roles:

| Role                    | Minimum ratio | Note                                          |
| ----------------------- | ------------- | --------------------------------------------- |
| `text` (body)           | 4.5:1         | WCAG AA                                       |
| `textDim` (secondary)   | 4.0:1         | Slightly below AA, balancing visual hierarchy |
| `textMuted` (auxiliary) | 3.0:1         | Large-text/auxiliary information acceptable   |

The validation decision can be expressed as a simple tree:

```plaintext
a semantic color vs background
   ├─ ratio < 3.0        → ❌ build aborts (no role passes)
   ├─ 3.0 ≤ ratio < 4.0  → ⚠️ only textMuted acceptable (others fail)
   ├─ 4.0 ≤ ratio < 4.5  → ⚠️ only textDim/comment acceptable (text fails)
   └─ ratio ≥ 4.5        → ✅ all pass (WCAG AA reached)
```

```javascript
// scripts/lib/validators.js (simplified)
import wcag from "wcag-contrast"

export function checkContrast(color1, color2, role, themeType) {
  if (!color1 || !color2) return
  const ratio = wcag.hex(color1, color2)

  let minRatio = 4.5
  if (role === "textDim" || role === "comment") minRatio = 4.0
  if (role === "textMuted") minRatio = 3.0

  if (ratio < minRatio) {
    if (role === "textMuted") {
      console.warn(
        `⚠️ Contrast slightly low: ${themeType} · ${role} = ${ratio.toFixed(2)}:1`,
      )
    } else {
      throw new Error(
        `❌ Insufficient contrast: ${themeType} · ${role} = ${ratio.toFixed(2)}:1` +
          `\n   WCAG requires ≥${minRatio}:1`,
      )
    }
  } else {
    console.log(`✅ ${themeType} · ${role}: ${ratio.toFixed(2)}:1`)
  }
}
```

When the build succeeds, you will see output like:

```plaintext
✅ dark · text: 14.48:1
✅ dark · textDim: 12.02:1
✅ dark · textMuted: 6.96:1
✅ light · text: 17.08:1
✅ light · textDim: 7.25:1
✅ light · textMuted: 7.25:1
```

#### Key design

`textDim` and `textMuted` use a **staircase standard** instead of a uniform 4.5:1 — because "visually receding" is itself a design intent; as long as minimal readability is kept. If every auxiliary text were forced to 4.5:1, comments and auxiliary information could no longer "recede" visually.

### 2.2 Structure validation: the generated file must be self-consistent

`validateThemeStructure` validates the generated theme JSON:

- It must contain the five required keys: `name`, `type`, `colors`, `tokenColors`, `semanticTokenColors`.
- `colors` must not be an empty object, and every value must be a valid 6- or 8-digit hex color.
- `tokenColors` must be an array.
- **No unresolved `${var}` or `{token}` may remain** — if a variable was not substituted because of a typo, this fails the build immediately.

On failure, a `ThemeValidationError` is thrown carrying the offending output file. Error handling is centralized in the main flow, not scattered across functions.

### 2.3 Circular reference detection

The semantics layer may reference primitives, and in theory a primitive could reference another primitive. If `a → b → a` forms a cycle, `resolveTokens` would recurse forever.

`tokens.js` handles this by **setting a depth limit (20 levels); exceeding it throws `[ENGINEERING_FATAL]` and prints the reference chain**:

```javascript
export function resolveTokens(obj, tokenMap, depth = 0, path = []) {
  const MAX_DEPTH = 20
  if (depth > MAX_DEPTH) {
    throw new Error(
      `[ENGINEERING_FATAL] Token circular reference detected: ${path.join(" → ")}`,
    )
  }
  // ...
}
```

The 20-level limit is far above normal token reference depth (normally no more than 2–3 levels), so when it triggers, **it is almost certainly a circular reference**.

### 2.4 Duplicate color and unused token detection

- **`detectDuplicateColors`**: detects whether two different primitive names point to the same color value. This is not an error (sometimes the same color value is reused for semantic distinction), but it is worth flagging as a possible naming-confusion signal.
- **`detectUnusedPrimitives`**: detects which primitives are not referenced by any semantics layer. Unused primitives may be "dead code" and also hint that the semantics layer may have a gap.

### 2.5 Architecture pollution detection

As the previous article described, colors must travel the "primitives → semantics → components" chain. Any cross-layer direct reference is architecture pollution.

`detectPrimitiveReference` in `tokens.js` detects at build time whether **the components layer directly references primitives** (for example, `editor.background: "${blue-500}"` in `workbench.yaml` instead of `${surfaceGround}`), and warns:

```plaintext
[Architecture reminder] workbench directly references primitive "blue-500"; route through the semantics layer.
```

This feature turns "architecture conventions" from a verbal agreement into an **automatically enforceable engineering constraint**.

A build must be correct, but correctness alone is not enough — **the generated output must also be lean**, otherwise the file bloats as languages grow.

---

## 3. Output Optimization: Streamlining the Generated JSON

Industrial-grade builds are not only "correct", they are also "refined". Moongate dramatically shrinks the final theme JSON through two optimization steps.

### 3.1 `mergeTokenColors`: merging rules with identical styles

Different languages often assign **exactly the same style** to different scopes. For example:

```yaml
# python.yaml
- name: Python F-string Expression
  scope: ["meta.fstring.python"]
  settings: { foreground: "#7dd3fc" }

# jsx.yaml
- name: JSX Expression Braces
  scope: ["meta.jsx.expression"]
  settings: { foreground: "#7dd3fc" }
```

These two rules share the same color, so they can be merged into one rule with two scopes. `mergeTokenColors` does this automatically:

- Serializes `settings` into a **stable key** (sorting by key name, so `{foreground, fontStyle}` and `{fontStyle, foreground}` are not treated as different rules).
- Merges rules with identical styles, aggregating scopes into an array.
- Sorts the result by scope count descending (more specific rules first).

Real-world effect: in Moongate v2.4.0, the `tokenColors` rule count dropped from about **89 to 34 rules (a 62% reduction)**, and the theme JSON shrank by about 16%.

### 3.2 `optimizeSemanticTokenColors`: removing redundant foreground values

VS Code's semantic roles support **parent inheritance**: `function.declaration` automatically inherits `function`'s styles unless explicitly overridden.

So when `function.declaration`'s `foreground` is identical to the parent `function`'s, you can safely drop the `foreground` and keep only the extra `fontStyle`:

```javascript
// before optimization
{
  "function": "#87cefa",
  "function.declaration": {
    "foreground": "#87cefa",  // redundant! same as parent
    "fontStyle": "bold"
  }
}

// after optimization
{
  "function": "#87cefa",
  "function.declaration": {
    "fontStyle": "bold"  // VS Code inherits function's color automatically
  }
}
```

This optimization is only safe when the parent-child relationship really exists and the foreground is truly identical. `optimizeSemanticTokenColors` implements this logic precisely and logs a count when removing.

The output is leaner, but a deeper question remains: **is the rule itself correct?** Does the scope really exist?

---

## 4. Scope Validation: Making "Written but Never Applied" a Thing of the Past

One of the most frustrating issues in theme development: **you carefully write a language rule, but it has no effect in real code**.

The usual cause: the `scope` in your rule does not exist in the TextMate grammar VS Code actually uses. Each language's (Python, Go, Rust, ...) scopes are **defined by its grammar file**, and they vary wildly across languages and versions. Writing scopes manually against grammar docs is error-prone.

Moongate's solution is **`scripts/verify-scopes.js`** — it automatically parses VS Code's built-in TextMate grammar files, compares every scope in your language config, and finds rules that are "written but never applied".

### 4.1 How it works

```javascript
// scripts/verify-scopes.js (CLI entry)
import {
  verifyAllScopes,
  formatVerificationResult,
} from "./lib/scope-validator.js"

const result = verifyAllScopes({ verbose: true })

console.log("🔍 Validating scopes in language config...\n")
process.stdout.write(formatVerificationResult(result))

if (!result.isValid) {
  console.error(
    `\n❌ Found ${result.totalIssues} unmatched scopes — validation failed!`,
  )
  process.exit(1)
}
```

The core flow of `scope-validator.js`:

1. **Locate the grammar source**: find the corresponding TextMate grammar JSON file for each language in the VS Code installation (such as `python.tmLanguage.json`).
2. **Extract all scopes**: traverse structures such as `patterns`, `captures`, `repository` in the grammar file to collect every available scope.
3. **Compare language rules**: compare every scope defined in `src/languages/*.yaml` against the scopes that actually exist in the grammar.
4. **Output a report**: list each unmatched scope, its file, and which language rule defined it.

### 4.2 What did it find?

In Moongate v2.6.0, `verify-scopes.js` helped fix scope issues in **8 language files**. The most representative fixes:

| Language | Before (wrong scope)       | After (correct scope)             |
| -------- | -------------------------- | --------------------------------- |
| Rust     | `support.macro.rust`       | `entity.name.function.macro.rust` |
| Rust     | `lifetime`                 | `entity.name.type.lifetime.rust`  |
| Go       | `entity.name.package.go`   | `keyword.package.go`              |
| Python   | `meta.decorator.python`    | `meta.function.decorator.python`  |
| Markdown | `heading.1.markdown`, etc. | `markup.heading.markdown`, etc.   |

Even more importantly, **the validator prevents regressions** — run it once after every language-rule change, and you will know immediately which scopes are "written but never effective".

### 4.3 Using it in CI

`verify-scopes.js` exits with code 1 on errors, so it plugs seamlessly into a CI pipeline:

```json
{
  "scripts": {
    "test:scopes": "node scripts/verify-scopes.js"
  }
}
```

Run `pnpm test:scopes` in CI — the build fails immediately if someone commits a wrong scope.

The verification tools are multiplying, so **the verification tools themselves need to be verified** — that is the value of automated testing.

---

## 5. Automated Testing: Making the Build System Regression-Safe

Once the build system carries the heavy burden of "generate all output + quality validation + architecture checks", the **build system itself** needs tests to prevent regressions.

Moongate uses Node.js's built-in `node --test` runner, so no extra test framework is needed:

### 5.1 Test coverage

```plaintext
test/
├── tokens.test.js        # token resolution, variable substitution, cycle detection
├── validators.test.js    # WCAG contrast, structure validation, architecture pollution
├── optimizers.test.js    # token merging, semantic color pruning
├── generators.test.js    # CSS/SCSS/TS/docs generators
├── utils.test.js         # normalizeHex, duplicateColors, and other utilities
├── scope-validator.test.js # scope validation logic
├── better-comments.test.js # Better Comments generator
├── theme-output.test.js  # integrity of generated theme JSON
└── helpers.js            # test helpers (capturing console, asserting throws)
```

The 85 tests cover three categories:

1. **Pure function logic**: `normalizeHex` handling of 3-/4-/8-digit color values, `resolveTokens` cycle detection, `mergeTokenColors` stable keys, and so on.
2. **Edge cases**: invalid color values throwing, undefined-variable warnings, the many boundary cases of transparency suffixes.
3. **Output consistency**: generated CSS variables map one-to-one to the semantics layer, and the Better Comments config stays in sync with the dark semantics layer.

### 5.2 Test helpers: capturing console output

Build tools use `console.log` / `console.warn` heavily to print progress and warnings. During tests, `test/helpers.js` provides a unified helper to capture these outputs and assert on them:

```javascript
// test/helpers.js (illustrative)
export function captureConsole(fn) {
  const logs = []
  const originalLog = console.log
  const originalWarn = console.warn
  console.log = (...args) => logs.push({ type: "log", args })
  console.warn = (...args) => logs.push({ type: "warn", args })
  try {
    const result = fn()
    return { logs, result }
  } finally {
    console.log = originalLog
    console.warn = originalWarn
  }
}
```

The elegance of this pattern: **you do not need to mock every function** — just assert that `console.warn` was called with arguments containing a "warning" keyword, and you can verify behavior such as "warns when a variable is undefined".

### 5.3 Running the tests

```json
{
  "scripts": {
    "test": "node --test \"test/*.test.js\""
  }
}
```

```bash
pnpm test
```

Sample output:

```console
▶ tokens.test.js
  ✔ resolves token references {token}
  ✔ substitutes variables ${var} with transparency suffix
  ✔ throws on circular reference
  ...
▶ validators.test.js
  ✔ structure validation throws when a required key is missing
  ✔ WCAG contrast throws when ratio is insufficient
  ...
✔ 85 tests passed
```

Once the build system is stable, we can point this capability at **ecosystem integration**: keeping colors from drifting in the plugin ecosystem.

---

## 6. Better Comments: Eliminating Dual Sources of Truth

An important ecosystem integration is the **Better Comments** extension — it gives `TODO`, `FIXME`, `NOTE`, and other comment markers distinct colors. The old approach had a problem: **the colors existed in two places** (the theme's semantics layer + the Better Comments JSON config), and changing one but forgetting the other caused color drift.

Moongate's solution: **the Better Comments config is generated by the build script**, reading values from the dark semantics layer.

### 6.1 Single source of truth

```javascript
// scripts/generate-better-comments.js (core logic)
import { resolveTokens } from "./lib/tokens.js"

// Map of Better Comments semantic variables → tags
const TAG_MAP = [
  { tag: "TODO", semanticKey: "warning", bold: true },
  { tag: "FIXME", semanticKey: "error", bold: true, italic: true },
  { tag: "NOTE", semanticKey: "highlight", italic: true },
  { tag: "HACK", semanticKey: "purple", bold: true },
  { tag: "BUG", semanticKey: "error", bold: true, underline: true },
  { tag: "XXX", semanticKey: "warning", bold: true },
]

// Resolve the final color for each tag from the dark semantics layer
const tags = TAG_MAP.map(({ tag, semanticKey, ...style }) => {
  const color = resolveTokens(darkSemantics[semanticKey], primitives)
  return { tag, color, ...style }
})

// Write extras/better-comments.json
```

When you adjust `warning` in `dark.yaml`, run `pnpm run gen:better-comments`, and `extras/better-comments.json` stays in sync automatically.

### 6.2 Built-in rules: zero-config, out of the box

Besides the standalone preset, Moongate also ships 6 special comment scope rules in `src/special/better-comments.yaml` (TODO, FIXME, NOTE, HACK, BUG, XXX). **After installing the Moongate theme, Better Comments uses the official colors automatically — no manual configuration.**

This "two-channel" design covers both kinds of users:

- **Theme users**: install Moongate and get the full colors (built-in rules).
- **Standalone-config users**: people who do not use the theme but only want the comment colors can rest on `extras/better-comments.json`.

Both channels are generated from the semantics layer, so **dual-source drift is impossible**.

---

## 7. Multi-Format Output: One Set of Tokens, Every Platform

The design system article covered CSS variable export. Moongate goes further and exports the semantics layer into **four formats**, covering the Web, Sass, and TypeScript ecosystems:

| Artifact                     | Format                 | Use case                                  |
| ---------------------------- | ---------------------- | ----------------------------------------- |
| `themes/moongate-colors.css` | CSS variables          | Blog, component library, any web project  |
| `themes/moongate-layout.css` | CSS variables (layout) | Spacing, typography, breakpoints, z-index |
| `themes/_tokens.scss`        | SCSS                   | Sass projects                             |
| `themes/tokens.ts`           | TypeScript             | Frontend framework projects               |

### 7.1 SCSS tokens

```scss
// themes/_tokens.scss (auto-generated)
// partial illustration
$ui-colors-dark: (
  bg: #0f172a,
  primary: #3b82f6,
  surface-raised: #1a2538, // ...
);

$ui-colors-light: (
  bg: #f9fafb,
  primary: #0284c7,
  surface-raised: #ffffff, // ...
);

// dark-mode convenience variables
$ui-bg: #0f172a;
$ui-primary: #3b82f6;
```

### 7.2 TypeScript tokens

```typescript
// themes/tokens.ts (auto-generated)
export interface MoongateTokens {
  dark: Record<string, string>
  light: Record<string, string>
}

export const tokens: MoongateTokens = {
  dark: {
    bg: "#0f172a",
    primary: "#3b82f6",
    // ...
  },
  light: {
    bg: "#f9fafb",
    primary: "#0284c7",
    // ...
  },
}

export default tokens
```

### 7.3 Auto-generated design-system docs

The build script also generates `docs/DESIGN_SYSTEM.md` — containing the variable selection protocol, primitive palette preview, elevation-system tables, and WCAG contrast data. This document is **not handwritten**; it is generated from real data on every build, so the docs always match the code.

---

## 8. CI/Release Pipeline: Errors Caught Before Publishing

The last piece of the industrial-grade puzzle: **embedding validation into the release pipeline**.

```json
{
  "scripts": {
    "build": "node scripts/build.js",
    "test": "node --test \"test/*.test.js\"",
    "test:scopes": "node scripts/verify-scopes.js",
    "gen:better-comments": "node scripts/generate-better-comments.js",
    "package": "pnpm run build && pnpm run gen:better-comments && vsce package",
    "publish": "pnpm run build && pnpm run gen:better-comments && vsce publish",
    "prepublishOnly": "pnpm run build && pnpm run gen:better-comments"
  }
}
```

The release chain is gated layer by layer:

```shell
pnpm run package
  → pnpm run build               # build + WCAG checks + structure validation + architecture detection
  → pnpm run gen:better-comments # regenerate Better Comments config
  → vsce package                 # package the .vsix
```

- Build failure → no `.vsix` is produced.
- Insufficient contrast → the build aborts, with an error naming the theme, role, and shortfall.
- Scope errors → `pnpm run test:scopes` exits 1, caught by CI.

**The "pre-publishing checklist" moves from a manual process to an automated gate** — this is exactly the line between industrial-grade and hand-rolled.

---

## 9. Summary

At this point, your theme build system has completed the jump from "usable" to "industrial-grade":

| Capability             | Theme Engineering     | Build System                                 |
| ---------------------- | --------------------- | -------------------------------------------- |
| Modularity             | Monolithic `build.js` | `scripts/lib/` single-responsibility modules |
| Style                  | CommonJS              | ESM                                          |
| WCAG checks            | ❌                    | ✅ staircase-style automatic contrast checks |
| Structure validation   | ❌                    | ✅ unresolved variable/token detection       |
| Circular references    | ❌                    | ✅ depth limit + reference chain output      |
| Architecture pollution | ❌                    | ✅ primitive-direct-reference warnings       |
| Output optimization    | ❌                    | ✅ token merge + semantic color pruning      |
| Scope validation       | ❌                    | ✅ `verify-scopes.js` automatic comparison   |
| Automated testing      | ❌                    | ✅ 85 tests (`node --test`)                  |
| Multi-format output    | CSS                   | CSS + SCSS + TypeScript + design docs        |
| Better Comments        | ❌                    | ✅ auto-generated + built-in rules           |
| Release gate           | manual check          | build/test/scope validation auto-intercepts  |

This system is not just a production tool for the Moongate theme — it is a reusable **design-system engineering template**. It proves the full path of "scattered colors → engineering assets → reusable on every platform".

But engineering capability, however strong, only answers "how". The next question matters just as much: **"why" — where does design philosophy come from? How do users get a consistent experience across different hardware? How does a theme go beyond code and become part of a brand?**

That is exactly what the final article of this series answers.

---

> **📎 Implementation Reference**
>
> This series is built upon the Moongate Theme project. You can explore the complete source code and install it directly:
>
> - **Code**: [github.com/yuelinghuashu/moongate-theme](https://github.com/yuelinghuashu/moongate-theme)
> - **Install**: [VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=yuelinghuashu.moongate-theme)
