---
title: 'Brand Ecosystem: Design Philosophy & Visual Contract'
description: Elevate your theme into a design system with design philosophy, a visual contract, and a brand ecosystem.
date: 2026-08-06 08:00:00
permalink: 50fad7d3-a637-4908-87f6-e74f2ac069b2
level: P3
series: design-system
tags:
  - Design System
  - Theme
  - Engineering
---

## Introduction: From Engineering to Design System

Through the first four articles, we built Moongate's engineering foundation step by step: from handwritten JSON to modular YAML, from the DTCG three-layer architecture to a testable industrial-grade build system. By now we have an efficient, extensible, self-validating theme production system.

But a truly great theme is not just a collection of color rules — it is a **complete design system**: a clear design philosophy, a reusable visual language, a contract for communicating with users, and a brand ecosystem that can be reused across platforms.

This article completes the final leap: from "engineering" to "design system". You will see how the engineering capabilities built in the first four articles (DTCG tokens, build artifacts, automated validation) support a brand ecosystem that can be reused across platforms.

---

## Part 1: Design Philosophy — Giving Every Color a Meaning

Any design system must stand on a clear design philosophy. Without philosophy, colors are just random combinations; with philosophy, colors become brand recognition.

### 1.1 Cool Base: Eliminating Visual Dirt

**General principle**: add a subtle cool tint (blue or green) to backgrounds, borders, and grays, avoiding the visual "dirt" of pure black or pure white. Pure black makes highlight colors "bleed and glow", while pure white tends to look yellowed and grayish. A touch of coolness keeps the base clean and deep, letting the colorful semantic colors on top appear more purely.

**Moongate example**:

- Dark background: `#0f172a` (deep-space blue-black)
- Light background: `#f9fafb` (cool moon white)

### 1.2 Semantic Layering: A Three-Level Staircase of Information

**General principle**: code is not flat — it naturally has hierarchy. Divide all code elements into three visual levels — foreground (core logic), midground (ordinary code), background (auxiliary information) — distinguished by contrast, saturation, and font styles (bold, italic, etc.), letting the code structure surface by itself.

**Moongate example**:

| Level          | Role           | Visual traits                           | Examples                                    |
| -------------- | -------------- | --------------------------------------- | ------------------------------------------- |
| **Foreground** | Core logic     | High contrast, bold, or high saturation | Keywords, function definitions, class names |
| **Midground**  | Ordinary code  | Medium contrast, visually natural       | Variables, strings, numbers                 |
| **Background** | Auxiliary info | Muted but readable                      | Comments, punctuation, operators            |

This layering runs through all languages and all theme variants — it is the underlying guarantee of Moongate's visual consistency.

### 1.3 Gravity Compensation: Visual Weight Parity Across Day and Night

**General principle**: a light theme is not a simple inversion of a dark theme. Bright colors on dark backgrounds are "emitters"; dark colors on light backgrounds are "absorbents". To give the same semantic role equal visual weight on different backgrounds, you must keep the hue unchanged while scientifically adjusting lightness and saturation. This is called **gravity compensation**.

**Moongate example**:

| Semantic role | Dark version      | Light version     | Adjustment method                                    |
| ------------- | ----------------- | ----------------- | ---------------------------------------------------- |
| Primary       | `#3b82f6` (60% L) | `#0284c7` (48% L) | Hue unchanged, lightness down ~20%                   |
| Success       | `#34d399` (65%)   | `#059669` (40%)   | Lightness down, saturation slightly down             |
| Warning       | `#fbbf24` (75%)   | `#b45309` (35%)   | Bright yellow to amber, avoids disappearing on white |
| Error         | `#f87171` (60%)   | `#b91c1c` (35%)   | Deep red keeps its warning feel                      |

When users switch themes, the visual weight of the same syntax element stays almost unchanged — no relearning needed.

### 1.4 Elevation System: Physical Depth for the UI

**General principle**: express physical depth by defining a staircase of background lightness. In dark mode, the higher the surface, the brighter it is (lightness increases); in light mode, the higher the surface, the brighter it is too (but using brighter whites/light grays). The step size stays consistent, forming a smooth sense of layering.

**Moongate example** (latest values in v2.6.0):

| Elevation level   | Purpose                | Dark mode | Light mode | Brightness change                       |
| ----------------- | ---------------------- | --------- | ---------- | --------------------------------------- |
| `surfaceGround`   | Base background        | `#0f172a` | `#f9fafb`  | Base level                              |
| `surfaceRaised`   | Sidebar, activity bar  | `#1a2538` | `#ffffff`  | dark +5%, light pure white              |
| `surfaceFloating` | Panels, floating cards | `#25364a` | `#f1f5f9`  | dark another +5%, light cool light gray |
| `surfaceTooltip`  | Tooltips, popups       | `#2e3b4d` | `#e2e8f0`  | Highest level                           |

This makes the sidebar gently raised, floating panels lightly elevated, and the code area deep and calm — the editor moves from flat to three-dimensional, and the physical metaphor makes the UI hierarchy obvious at a glance.

---

## Part 2: Visual Contract — Connecting Users and Hardware

**General principle**: no matter how well a theme is designed, if the user's monitor is not calibrated, the result suffers. Provide a monitor calibration guide (visual contract) that helps users adjust Gamma, brightness, contrast, and color temperature, and reminds them to turn off "enhancement" features such as dynamic contrast and vivid mode. The end of calibration is not theoretical perfection, but the balance point where the user feels most comfortable.

**Moongate example**: `extras/VISUAL_CONTRACT.md` provides the detailed Visual Contract document (v2.0), with these core steps:

| Step                     | Action                                                                                                  | Goal                             |
| ------------------------ | ------------------------------------------------------------------------------------------------------- | -------------------------------- |
| **1. Set Gamma**         | Choose `Gamma 2.2`                                                                                      | Smooth grayscale transitions     |
| **2. Adjust brightness** | Dark mode: make the 2% gray block just visible; light mode: make blocks 250–255 clearly distinguishable | Preserve shadow/highlight detail |
| **3. Adjust contrast**   | The 100% white block should be clear but not harsh                                                      | Avoid overexposure               |
| **4. Color temperature** | Recommend `6500K` or `warm` mode                                                                        | Neutralize blue light            |

v2.0 especially emphasizes **mode-specific calibration**:

- **Dark mode** (night environment): in a dark room, adjust brightness until the [Black level test page](http://www.lagom.nl/lcd-test/black.php) shows the 2% gray block as just discernible.
- **Light mode** (day environment): under typical lighting, use the [White saturation test page](http://www.lagom.nl/lcd-test/white_saturation.php) so that blocks 250–255 are clearly distinguishable and the 255 pure white is not harsh.

It also lists common monitor pitfalls and their fixes:

| Pitfall                 | Symptom                                     | Fix                    |
| ----------------------- | ------------------------------------------- | ---------------------- |
| Black stabilizer        | Dark background looks gray, dark text fades | Lock at 50 or turn off |
| Vivid mode / wide gamut | Colors drift, cool tones warm up            | Prefer sRGB mode       |
| Oversharpening          | "Ghosting" around character edges           | Lower to 50–60         |

> 💡 **Why is the visual contract a "contract" rather than a "tutorial"?** Because it is not about teaching users how to calibrate a monitor — it is a **mutual agreement**: the theme developer promises "the colors are carefully designed", and the user promises "the design is faithfully presented through reasonable calibration". This contract makes the theme's behavior across different hardware controllable and predictable.

### 🚀 Quick version: 3 steps of "good enough"

Full calibration is valuable for users who chase precision, but if you would rather not fiddle with hardware parameters, these 3 steps give a good-enough experience:

1. **Pick the right mode**: switch the monitor to **sRGB / Standard**, and turn off "enhancement" features such as Dynamic Contrast, Vivid Mode, and Black Stabilizer — this alone solves 80% of color drift.
2. **Tune by eye**: if dark mode looks grayish or light-mode text is harsh, nudge brightness until it feels comfortable.
3. **Set the color temperature floor**: choose **6500K or Warm** to neutralize blue light.

There is no need for Gamma curves and test blocks at hardware-grade precision — "comfortable to the eye" is the first standard. When you want finer results, follow the full steps above.

---

## Part 3: Brand Ecosystem — From Theme to Community

**General principle**: a complete brand system includes a documentation system (README, CHANGELOG, design docs), community interaction (open source, feedback channels), and ecosystem integration (deep integration with surrounding tools for a consistent experience).

**Moongate example**:

### 3.1 Documentation system

- **README**: bilingual, with preview images, design philosophy, a highlight list, and recommended configuration.
- **CHANGELOG**: records changes by version, collapsing old versions and highlighting the current ones.
- **Visual contract**: a standalone document shipped with the theme.
- **Design-system docs**: the auto-generated `DESIGN_SYSTEM.md`, containing the full palette, the elevation system, WCAG contrast data, and the "variable selection protocol" — **every color must follow the "primitives → semantics → components" chain; any cross-layer direct reference is architecture pollution**.

### 3.2 Community interaction

- **Open source on GitHub**: public source code, accepting PRs.
- **Feedback channel**: encourage users to share calibration experiences to keep improving the visual contract.

### 3.3 Ecosystem integration

- **Better Comments preset**: the official colors are **built into** the theme, zero-config and ready to use (the dual-source elimination mechanism is covered in the build system article).
- **Terminal ANSI sync**: the 16-color ANSI palette maps to the theme's semantic colors, eliminating the visual split between the editor and the terminal.
- **Cross-platform token assets**: automatically exported as CSS / SCSS / TypeScript tokens (generation is covered in the build system article), available for blogs, docs sites, and UI component libraries to reference directly — one color system running through every product.

---

## Part 4: Summary and Outlook

At this point, the five-part series is complete. Looking back at the journey:

- **VS Code Theme**: you started from handwritten JSON, created and published your first VS Code theme, and mastered the core mechanics and publishing flow.
- **Theme Engineering**: through YAML modularization + a build script, you made the theme maintainable and automatically buildable.
- **Design System**: you used the DTCG three-layer architecture to manage colors, building dark/light variants with gravity compensation.
- **Build System**: you built a testable, verifiable build-script architecture that automates quality assurance.
- **Brand Ecosystem**: you elevated the theme into a design system, building a complete brand ecosystem with design philosophy, a visual contract, and cross-platform assets.

**v2.6.0 is the practical result of this system** — every color passes industrial-grade validation, layout tokens are exported as CSS variables, design docs are auto-generated, and cross-platform tokens are produced in both SCSS and TypeScript formats.

---

## Version and Maintenance Strategy

This series describes the actual state of **Moongate v2.6.0**. To set fair expectations about freshness, here is the series' maintenance strategy:

### Version locking

- All color values, scopes, and script examples in this series are based on **Moongate v2.6.0**. If you install a different version of the theme, some details (such as elevation colors and language rules) may differ from this article.
- The "corresponds to Moongate v2.6.0" note in each article is the version-locking marker.

### Update cadence

- **Major version updates** (such as v3.0 introducing new architectural changes): the series articles will be revised in sync, so readers always learn the current best practices.
- **Minor version updates** (such as new language support or subtle palette tweaks): the article bodies are not rewritten per release; these are recorded in the project's `CHANGELOG.md`. Readers can track these incremental changes via the changelog.

### Compatibility considerations

- **VS Code version**: the `engines` field in `package.json` (currently `^1.130.0`) determines the range of UI keys available in `workbench.yaml`. If VS Code later adds many new UI keys, the theme engineering may need to extend accordingly — this is exactly what an engineering system is for.
- **DTCG standard**: the DTCG spec is still evolving. Moongate's strategy is to keep the "primitives → semantics → components" three-layer architecture stable while embracing spec changes within each layer.

---

> **📎 Implementation Reference**
>
> This series is built upon the Moongate Theme project. You can explore the complete source code and install it directly:
>
> - **Code**: [github.com/yuelinghuashu/moongate-theme](https://github.com/yuelinghuashu/moongate-theme)
> - **Install**: [VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=yuelinghuashu.moongate-theme)
