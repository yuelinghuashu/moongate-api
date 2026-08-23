---
title: 'Design Tokens vs Atomic CSS: A Failed Integration and the Path to Harmony'
description: A personal developer's attempt to map existing design tokens to UnoCSS failed. Quantified comparison, pragmatic boundaries, and the conclusion that design tokens come first, atomic CSS optional.
date: 2026-04-18
permalink: 65f7b804-7301-401d-8461-b04913b29333
level: P3
series: moongate-vue
tags:
  - CSS
  - Vue
  - Design System
  - Engineering
---

> Starting from a failed attempt to map design tokens to UnoCSS, this article quantifies the maintenance cost difference between the two approaches and proposes pragmatic division of responsibilities.

## 1. Starting Point: A Complete Set of Design Tokens

The styling system for my personal project `moongate-vue` consists of three file groups:

- **`src/styles/tokens/colors.css`**: Light/dark dual themes, semantic color variables (`--ui-primary`, `--ui-bg-muted`...), and layout tokens
- **`src/styles/tokens/layout.css`**: Spacing, border-radius, animation duration, font, breakpoint, z-index, and other layout tokens
- **`src/styles/index.css`**: Global style entry point, importing tokens + component styles + minimal utility classes

This token system is framework-agnostic. Any component can access design constraints via `var(--ui-spacing-md)`, `var(--ui-primary)`. It's stable, intuitive, and maintainable—it's the **foundation** of my entire component library.

Regarding variable scale, I should be honest: `colors.css` defines **68 color variables** for each of the light/dark modes (far exceeding the initial 40+). Some are actual UI tokens consumed by the component library; others are **editor theme extension variables** inherited from the `moongate-theme` project—for example, `--ui-ansi-red` (ANSI terminal colors), `--ui-bracket1` (bracket pair colors), `--ui-git-added` (Git status colors), `--ui-debug-start` (debug breakpoint colors), etc.

These variables are needed by my personal editor theme project (moongate-theme). While the component library itself doesn't consume them, they do exist in the token files—this is my choice to "generate the complete token set via script" rather than "generate only the subset needed by components." **They're not redundant code; they're shared design assets from my other personal projects.**

## 2. The Temptation: UnoCSS's Lightweight and Atomic Approach

I'd heard about Tailwind's "bulky" reputation, but UnoCSS claims on-demand generation, zero runtime, and ultra-lightweight. As a solo developer, I craved the experience of "not writing CSS class names"—just stacking `flex p-4 text-center` directly in templates, no file switching, no naming decisions.

So one evening, I decided: map design tokens to UnoCSS, keeping the design system while enjoying atomic CSS syntax.

## 3. The Collision: One Evening's Struggle (With Real Configuration)

I wrote `uno.config.ts`, trying to map each `--ui-*` variable to an atomic class. Here's the failed configuration from that attempt:

```ts
// uno.config.ts (failed version)
import { defineConfig } from "unocss"

export default defineConfig({
  theme: {
    colors: {
      // ❌ Every --ui-* variable needs manual mapping, 68 variables = 68 lines of mapping
      // ❌ Change a variable in one place, must also change here, single source of truth becomes two
      primary: "var(--ui-primary)",
      success: "var(--ui-success)",
      warning: "var(--ui-warning)",
      error: "var(--ui-error)",
      // ❌ Naming gets out of control: bg-muted / border-subtle classes sound stuttery
      "bg-muted": "var(--ui-bg-muted)",
      "border-subtle": "var(--ui-border-subtle)",
      // Need to map 68 variables, omitted here...
    },
    spacing: {
      xs: "var(--ui-spacing-xs)",
      sm: "var(--ui-spacing-sm)",
      md: "var(--ui-spacing-md)",
      lg: "var(--ui-spacing-lg)",
      xl: "var(--ui-spacing-xl)",
      "2xl": "var(--ui-spacing-2xl)",
      "3xl": "var(--ui-spacing-3xl)",
    },
    borderRadius: {
      none: "var(--ui-radius-none)",
      sm: "var(--ui-radius-sm)",
    },
  },
})
```

Then in `Button.vue`, I replaced all scoped styles with atomic classes:

```vue
<!-- Refactored Button.vue (failed attempt) -->
<!-- ❌ Semantic loss: can't tell at a glance this is button styling, just a bunch of layout fragments -->
<button :class="cn('bg-primary text-white', 'bg-bg-muted', 'border-border-subtle')">
```

Problems quickly surfaced:

- **Redundant and non-semantic class names**: `bg-bg-muted`, `border-border-subtle`—they read like stuttering.
- **High mapping maintenance cost**: Change a CSS variable in one place, also change the UnoCSS config—single source of truth becomes two.
- **Conditional logic explosion**: To handle different color variants, you'd write `color === 'neutral' ? 'text-dim' : 'text-'+color`.
- **Difficult debugging**: Seeing `bg-bg-muted` in the browser requires reverse-looking which CSS variable it maps to.
- **No IDE autocompletion**: UnoCSS's type generation couldn't automatically cover my custom variable names, resulting in zero prompts during development.

That same evening, I deleted all the mapping code and returned to the original approach.

## 4. The Epiphany: Design Tokens Are Already the Lightest Atomic Framework

Looking at `bg-bg-muted`, I suddenly asked myself: **Why not just write `background-color: var(--ui-bg-muted)` directly?**

My design token system already provides all the design constraints. The "design constraint" capability UnoCSS claims—my CSS variables already have all of it. Its only remaining value is "quick writing"—essentially syntactic sugar.

Going deeper: **Mapping `--ui-primary` to `bg-primary` is essentially "wrapping CSS in CSS."** UnoCSS generates code like `.bg-primary { background-color: var(--ui-primary); }` at compile time. Since it ultimately becomes this single CSS line, wouldn't it be more direct to write it in scoped styles?

## 5. Quantitative Comparison: Pure Design Tokens vs. UnoCSS Mapping

To objectively assess just how "unworthwhile" it is, I compiled a comparison table (based on actual project testing, token count based on actual 68):

{% collapsible 📊 Full Quantitative Comparison %}

| Metric                         | Pure Design Tokens Approach (Final Choice)   | UnoCSS Mapping Approach (Abandoned)                       |
| ------------------------------ | -------------------------------------------- | --------------------------------------------------------- |
| CSS Variable Count             | 68 (68 each for light/dark)                  | 68 (unchanged)                                            |
| Additional Config File Lines   | 0                                            | ~200 lines (`uno.config.ts`, including 68 color mappings) |
| Template Class Name Length     | Short (`mg-button`)                          | Long (`bg-primary text-white rounded`)                    |
| Places to Change for One Color | 1 (CSS variable definition)                  | 2 (CSS variable + UnoCSS mapping)                         |
| TypeScript Support             | No prompts for native CSS variables          | Available via type generation, but requires extra config  |
| First-Paint CSS Size (gzip)    | ~4 KB (what the component library consumes)  | ~2 KB (on-demand generation is smaller)                   |
| Debugging Experience           | Directly see `background: var(--ui-primary)` | Need to look up which variable `bg-primary` maps to       |
| Learning Curve (Newcomers)     | Low (only need to understand CSS variables)  | Medium (need to understand mapping logic + UnoCSS rules)  |

{% endcollapsible %}

**Conclusion**: Sacrificing ~2 KB of size for a massive reduction in maintenance cost. For personal projects, **maintenance cost, semantic clarity, and debugging experience** matter more than extreme size optimization.

> 💡 About the "68 variables": The component library actually consumes roughly half as UI tokens; the other half are editor theme extension variables (ANSI colors, bracket pair colors, etc., shared from a personal theme project). The UnoCSS mapping approach requires maintaining mappings for all 68 variables, which amplifies the mapping cost—while using CSS variables directly naturally supports "only referencing needed variables."

## 6. The Path to Harmony: Pragmatic Boundaries

My experience doesn't prove atomic CSS is bad—it proves: **When your project already has a mature set of design tokens, forcibly mapping tokens to atomic classes is redundant.**

But this doesn't mean abandoning atomic tools entirely. I later found a reasonable division of responsibilities. The key is **distinguishing "value-independent classes" from "value-dependent classes"**:

| Style Type                                     | Recommended Approach                                                                  | Reason                                                                                                                             |
| ---------------------------------------------- | ------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| **Layout** (flex, grid, position)              | UnoCSS atomic classes (`flex`, `grid`, `items-center`, `justify-between`, `relative`) | No values involved, no tokens needed, works out of the box                                                                         |
| **Spacing** (padding, margin, gap)             | **Prefer scoped styles + `var(--ui-spacing-*)`**                                      | Maintains theme consistency. If you must use atomic classes, modify UnoCSS config to map values to your tokens (see example below) |
| **Colors, border-radius, shadows, animations** | Always scoped styles + CSS variables                                                  | Strong semantics, no mapping cost, intuitive debugging                                                                             |
| **Responsive variants**                        | Can use UnoCSS's `md:` prefix, but only for layout/spacing classes, not for colors    | Clean and doesn't pollute design tokens                                                                                            |

### Special Note on Spacing

UnoCSS's default `p-4` maps to `1rem`, while your design tokens might have `--ui-spacing-md: 12px`. Writing `p-4` directly bypasses the design system. If you really want to use atomic spacing classes, you must modify the configuration:

```ts
// uno.config.ts (only if you want atomic spacing)
theme: {
  spacing: {
    sm: 'var(--ui-spacing-sm)',
    md: 'var(--ui-spacing-md)',
    lg: 'var(--ui-spacing-lg)',
  }
}
```

Then you can write `p-sm`, `m-md`. But note: this brings you back to the mapping maintenance problem—every token value change requires syncing the UnoCSS config. **I ultimately chose not to use atomic spacing classes at all, using only value-free layout classes.**

## 7. Should You Use Atomic CSS Without Design Tokens?

I'm not an opponent of atomic CSS. If the following conditions are met, I'd choose UnoCSS/Tailwind without hesitation:

- ✅ New project, rapid prototyping
- ✅ Design system not yet mature, still in rapid iteration
- ✅ Entire team familiar with atomic syntax
- ✅ No need to reuse styles across frameworks

**Not suitable scenarios**:

- ❌ Already have mature design tokens needing long-term maintenance
- ❌ Need style files reusable across Vue/React/Svelte
- ❌ Not sensitive to CSS size (modern atomic frameworks are actually quite small—this isn't the main trade-off)

## 8. Advice for Developers in Similar Situations

If you're like me, already having a complete design token system (CSS variables, Design Tokens) but wanting to try atomic CSS, my advice is:

1. **Don't map core tokens like colors, theme spacing, border-radius, etc.** These should be used directly via `var(--ui-*)` in scoped styles.
2. **Only use the generic layout classes provided by atomic tools** (`flex`, `grid`, `items-center`, `relative`...). These are unrelated to your design system and require no mapping.
3. **Beware of "magic numbers"**: Avoid hardcoding values like `p-3.5` or `gap-11` in templates—they break the design token contract. If you frequently find yourself deviating from the token system, it may indicate your token definitions need extending rather than bypassing.
4. **For responsive layouts**, you can continue using atomic tools' responsive variants (`md:flex`), but try to use them only for layout, not for colors/spacing.
5. **If atomic tools make you feel like "using them just for the sake of it," feel free to skip them entirely.** Native CSS + design tokens are already clear and maintainable enough.

## 9. Future Evolution: DTCG and Beyond

Notably, the W3C's Design Tokens Community Group (DTCG) released its first stable specification by the end of 2025, promoting design tokens as a cross-platform universal language. This means "token-centric" architecture is becoming an industry consensus.

Atomic tools can consume design tokens but shouldn't hold them hostage. Hardcoding tokens into tool-specific atomic classes locks your design system to that tool's syntax. Directly using CSS variables is framework-agnostic and future-proof.

In the future, when the DTCG toolchain matures, it may automatically generate atomic classes from design tokens. I'll re-evaluate UnoCSS then. But currently (2026), manual mapping remains a maintenance burden.

## 10. Conclusion: Design Tokens First, Atomic CSS Optional

Ultimately, my `Button.vue` returned to its most basic form:

```css
/* src/styles/components/button.css */
.mg-button-filled-primary {
  background-color: var(--ui-primary);
  color: white;
}
.mg-button-filled-primary:hover:not(:disabled) {
  background-color: color-mix(in srgb, var(--ui-primary), black 10%);
}
```

Simple, direct, semantic. This is how design tokens should be used.

UnoCSS and Tailwind are great tools, but they're not substitutes for a design system. Design tokens are the foundation; atomic CSS is just a layer of paint on top. When you already have a solid foundation, whether to apply that paint depends on whether you're willing to accept the maintenance cost that comes with the syntactic sugar.

At least for me, **it's not worth it**.

---

## 🌙 About Moongate Vue

This article is from the Moongate Vue Component Library Design Series (4 articles), all content is based on real project practice:

- **Project Repository**: [github.com/yuelinghuashu/moongate-vue](https://github.com/yuelinghuashu/moongate-vue) — Minimalist Vue 3 component library, zero dependencies, CSS-first, 25KB gzip
- **Real-world Example**: [moongate.top](https://moongate.top) — Personal blog, migrated from Nuxt UI v4 to Moongate Vue
- **Online Documentation**: [vue.moongate.top](https://vue.moongate.top) — Component API and theme customization guide
