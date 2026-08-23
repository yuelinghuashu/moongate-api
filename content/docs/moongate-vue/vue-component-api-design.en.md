---
title: 'Vue 3 Simple Component Development: API Design from the Button Component'
description: 'Using the Button component as an example, explore Vue 3 component library API design philosophy: Props definition, variant system, size trade-offs, slot design, state handling, accessibility, and the design trade-offs behind a minimal API.'
date: 2026-05-07
permalink: e6526a2f-4d33-4817-9ef0-1cbfb71ef3e7
level: P3
series: moongate-vue
tags:
  - Vue
  - Design System
  - Engineering
---

> Minimal does not mean crude; restraint does not mean absence — using Button as an example, see how a component with just 11 props covers 90% of everyday button scenarios.

## 1. Background & References

In previous articles, we discussed the philosophy of design tokens over atomic CSS, and the CSS-first + thin component wrapper architecture. But one question was never explored in depth: **for a specific single component, how should the API actually be designed?**

In the early design phase, I closely studied the design philosophy of Nuxt UI v4. Nuxt UI v4 encapsulates complex styles and interactions into a few core dimensions: `variant`/`color`/`size` — this is exactly what I wanted — to **cohesively internalize complex logic within the component** and expose only the most streamlined API externally.

The following sections use the Button component as an example to walk through my design trade-offs and thought process step by step. **This article is based on the actual implementation in v1.5.0** — after multiple version iterations, the Button API has become more refined than the initial version.

## 2. Starting from Requirements: What Does a Button Need?

The most basic functionality of a button component:

- Display text
- Click to trigger events
- Disabled state
- Different styles (primary, secondary, danger, etc.)

But is that enough? Let's look at real-world usage scenarios:

```vue
<!-- Button with icon -->
<Button>
  <template #icon>🔍</template>
  Search
</Button>

<!-- Loading state -->
<Button loading>Submitting</Button>

<!-- Block button (full width) -->
<Button block>Full Width Button</Button>

<!-- Different sizes -->
<Button size="sm">Small</Button>
```

After analysis, the requirements supported by the Button component in v1.5.0:

| Requirement | Implementation |
| --- | --- |
| Text content | Default slot or `label` prop |
| Click event | `click` event |
| Disabled state | `disabled` prop |
| Loading state | `loading` prop |
| Preserve text while loading | `showLabelWhileLoading` prop |
| Custom text while loading | `loadingLabel` prop + `#loading-label` slot |
| Different styles | `variant` + `color` |
| Different sizes | `size` prop |
| Block-level width | `block` prop |
| Icon | `icon` prop or `#icon` slot |
| Native button type | `type` prop (button/submit/reset) |

## 3. Props Design: Types, Defaults, Priority

### 3.1 Basic Props

```typescript
interface Props {
  label?: string // Button text
  disabled?: boolean // Whether disabled
  loading?: boolean // Whether loading
  block?: boolean // Whether block-level
  type?: "button" | "submit" | "reset" // Native button type
  showLabelWhileLoading?: boolean // Whether to preserve text while loading
  loadingLabel?: string // Custom text while loading
}

const props = withDefaults(defineProps<Props>(), {
  label: "",
  disabled: false,
  loading: false,
  block: false,
  type: "button", // Default to button to prevent accidental form submission
  showLabelWhileLoading: false,
})
```

**Why default to `type="button"`?** This is an important lesson learned from actual project pitfalls. If you use the native `<button>` default type `submit`, clicking the button inside a `<form>` will accidentally submit the form. Explicitly defaulting to `button` avoids the vast majority of unintended form behaviors.

### 3.2 Variant System: variant + color

Common button types include: primary button, secondary button, border button, and ghost button. Inspired by Nuxt UI v4's `variant` + `color` design, I chose to completely decouple "visual mode" from "semantic colors" — `variant` only controls two visual modes: `filled` / `outline`, and `color` only controls four semantic colors: `primary` / `success` / `warning` / `error`.

Why keep only `filled` and `outline`, and remove `ghost`?

| Variant | Usage Frequency | Retained? |
| --- | --- | --- |
| `filled` | 🔥🔥🔥🔥🔥 Very High | ✅ Retained |
| `outline` | 🔥🔥🔥🔥 High | ✅ Retained |
| `ghost` | 🔥 Low | ❌ Removed (can be replaced by `outline`) |

Similarly, only 4 colors are retained:

| Color | Usage Frequency | Retained? |
| --- | --- | --- |
| `primary` | 🔥🔥🔥🔥🔥 Very High | ✅ Retained |
| `success` | 🔥🔥🔥 Medium | ✅ Retained |
| `warning` | 🔥 Low | ✅ Retained |
| `error` | 🔥🔥 Medium | ✅ Retained |
| `neutral` | 🔥 Low | ❌ Removed (can be replaced by `outline` + default color) |

### 3.3 Size Design

Only three size tiers are retained: `sm` / `md` / `lg`. I removed `xs` and `xl`: extremely small sizes can be replaced by Badge or other non-button components, and I rarely encounter super-large size scenarios in a personal blog.

The default size is set to `sm` — common buttons typically have a default height of about 32-34px, which corresponds exactly to our `sm`.

### 3.4 Icon Design: Prop and Slot Coexistence

In the initial design, I only provided an `#icon` slot, not an `icon` prop — the rationale was "maintaining single responsibility." However, in practice, two problems emerged:

1. **Simple icons (like `✓`, emoji) are too verbose with a slot**: `<template #icon>✓</template>` is 20 more characters than `icon="✓"`
2. **Icon library components (like IconHome from lucide-vue-next) aren't intuitive enough with a slot**: you need to wrap them with `<component :is="IconHome" />`

After multiple version iterations, both `icon?: string | Component` (string or Vue component) and the `#icon` slot are now supported simultaneously, with a clear priority established:

```vue
<!-- Using slot (higher priority, more flexible) -->
<Button>
  <template #icon>🔍</template>
  Search
</Button>

<!-- Using string prop -->
<Button icon="✓" label="Confirm" />

<!-- Using Vue component prop -->
<Button :icon="IconHome" label="Home" />
```

Implementation logic in the template:

```vue
<span v-if="hasIconSlot || icon" class="mg-button-icon">
  <!-- Slot takes precedence over prop -->
  <slot name="icon">
    <!-- Render component when prop is a Vue component -->
    <component :is="icon" v-if="typeof icon !== 'string'" />
    <!-- Render text directly when prop is a string -->
    <span v-else-if="icon">{{ icon }}</span>
  </slot>
</span>
```

**Design principle: slot takes precedence over prop.** `hasIconSlot` checks whether an `#icon` slot is provided; if so, the `icon` prop is completely ignored. This ensures flexibility — when users need custom icon layouts, the slot can always override the default behavior of the prop.

> Design consideration: I consolidated into a single `icon` prop (left-side icon — this covers 90% of personal blog scenarios) and retained the `#icon` slot for full control.

## 4. Slot Design: Default Slot vs label Prop

To support both quick usage and custom content, both a `label` prop and a default slot are provided. In the template, `<slot>{{ label }}</slot>` is used — slot content is used when available, otherwise it falls back to `label`.

The `hasLabel` logic has an easily overlooked detail:

```typescript
const hasLabel = computed(() => props.label !== "" || !!slots.default)
```

Note that this uses `props.label !== ""` rather than `!!props.label`. Why?

- `withDefaults` will resolve `undefined` to the default value `""`
- When `label=""` is explicitly passed from outside, **we don't render the empty label container** — this is the classic "icon-only button" scenario, where an empty container prevents the icon from vertically centering
- However, if there's a default slot (regardless of whether the slot content is empty), the container is still rendered

```vue
<!-- Icon-only button: label is empty, don't render empty label container -->
<Button icon="🔍" />

<!-- With slot content: render even if label is empty -->
<Button><template #icon>🔍</template>Search</Button>
```

This solves the **icon-only button centering problem** — an empty `.mg-button-label` would take up space and cause the icon to be off-center.

## 5. State Handling

### 5.1 Disabled State and Loading State

Both `disabled` and `loading` disable the button, but **only when `disabled` is set is the `click` event fully prevented** (native disabled attribute); for the `loading` state, we also want to maintain correct semantics — the user should know the button is "processing."

```typescript
const handleClick = (event: MouseEvent) => {
  if (props.disabled || props.loading) return
  emit("click", event)
}
```

In the template, `:disabled="disabled || loading"` disables the button for both states, but the click event is still uniformly intercepted by `handleClick` — this ensures that programmatic calls (`.trigger('click')`) also respect the disabled semantics.

### 5.2 Loading State Enhancement

The initial version's `loading` only showed a spinning animation, hiding the icon and text. v1.5.0 added two enhancements:

- `showLabelWhileLoading`: whether to preserve text while loading
- `loadingLabel`: custom text while loading (defaults to reusing `label`)
- `#loading-label` slot: fully customizable loading text (highest priority)

```vue
<template v-if="loading">
  <span class="mg-button-loading-icon" />
  <!-- Show label based on the toggle -->
  <span v-if="showLabelWhileLoading" class="mg-button-label">
    <!-- Slot > loadingLabel prop > label -->
    <slot name="loading-label">{{ loadingLabel || label }}</slot>
  </span>
</template>
```

Usage scenarios (three progressive levels of control):

```vue
<!-- ① Default: only show loading animation -->
<Button loading label="Save" />

<!-- ② Preserve text: indicate to the user "saving in progress" -->
<Button loading :show-label-while-loading="true" label="Save" />

<!-- ③ Fully custom: slot overrides everything -->
<Button loading :show-label-while-loading="true">
  <template #loading-label>⏳ Saving furiously...</template>
</Button>
```

Pure CSS loading animation:

```css
.mg-button-loading-icon {
  width: 1rem;
  height: 1rem;
  border: 2px solid currentColor;
  border-top-color: transparent;
  border-radius: 50%;
  animation: mg-button-spin 0.6s linear infinite;
}
@keyframes mg-button-spin {
  to {
    transform: rotate(360deg);
  }
}
```

## 6. CSS Style Design

### 6.1 Base Styles

The button uses `inline-flex` layout with content centered both horizontally and vertically, sharp corners (`--ui-radius-none`), and padding and font size using design tokens.

```css
.mg-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--ui-spacing-sm);
  font-weight: 500;
  transition: all var(--ui-motion-duration-neural) ease;
  cursor: pointer;
  border-radius: var(--ui-radius-none);
  border: none;
  background: transparent;
  white-space: nowrap;
  padding: var(--ui-spacing-sm) var(--ui-spacing-md);
  font-size: var(--ui-typography-size-body);
}
```

### 6.2 Size Variants

| Size | Padding | Font Size |
| --- | --- | --- |
| `sm` | `sm` / `md` | `--ui-typography-size-code` (13px) |
| `md` | `md` / `lg` | `--ui-typography-size-body` (15px) |
| `lg` | `lg` / `xl` | `1.125rem` (18px) |

### 6.3 Color Variants: Hover/Active States

The color variants in v1.5.0 not only define base colors but also include complete hover/active feedback. This is achieved using `color-mix()` to blend black into the token color:

```css
.mg-button-filled-primary {
  background-color: var(--ui-primary);
  color: white;
}
.mg-button-filled-primary:hover:not(:disabled) {
  background-color: color-mix(in srgb, var(--ui-primary), black 10%);
}
.mg-button-filled-primary:active:not(:disabled) {
  background-color: color-mix(in srgb, var(--ui-primary), black 20%);
}

/* outline variant: transparent background + border */
.mg-button-outline-primary {
  background-color: transparent;
  color: var(--ui-primary);
  border: 1px solid var(--ui-primary);
}
.mg-button-outline-primary:hover:not(:disabled) {
  background-color: color-mix(in srgb, var(--ui-primary), transparent 90%);
}
```

Using `:not(:disabled)` ensures disabled state doesn't trigger hover effects.

### 6.4 Icon and Text Containers

The icon container uses `inline-flex` with `line-height: 0` to eliminate line-height influence. Internal SVG or iconify icons are forced to block-level with width and height set to `1em`. Combined with the `:empty` pseudo-class to hide empty tags, this fixes centering issues when only an icon is present:

```css
.mg-button-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  line-height: 0;
}
.mg-button-icon svg,
.mg-button-icon .iconify {
  display: block;
  width: 1em;
  height: 1em;
}

/* Hide empty tags - fixes centering issue when only icon is present */
.mg-button-label:empty {
  display: none;
}
```

The CSS layer's `:empty` pseudo-class combined with the component layer's `hasLabel` check together ensure correct centering for icon-only buttons.

## 7. Accessibility Design

The current Button does not add `aria-busy` or `aria-disabled` attributes. The accessibility strategy relies on native semantics:

- The **native `disabled` attribute** already allows screen readers to correctly read out the disabled state
- The **loading state** is expressed through visual feedback (spinning animation), which is more intuitive than additional ARIA attributes
- Removing redundant ARIA attributes avoids duplication with native semantics

This is not a compromise of "minimal ≠ crude" but rather **avoiding ARIA overuse** — native HTML semantics (`<button disabled>`) are themselves the most reliable accessibility. The project's real accessibility assurance comes from the test layer: `a11y.test.ts` uses axe-core to perform automated compliance checks (`expectNoViolations`) on 12 core components.

## 8. Attribute Fallthrough

The template root element passes through native attributes via `v-bind="$attrs"` (see §IX for full code), allowing users to directly pass `id`, `name`, `data-*`, `aria-*` and other attributes:

```vue
<Button id="submit-btn" name="submit" data-testid="submit">
  Submit
</Button>
```

Combined with `defineOptions({ inheritAttrs: false })`, external classes are exposed to users through `$attrs` for their own handling, while the component's internal class bindings won't conflict.

## 9. Final Code (v1.5.0 Actual Implementation)

```vue
<template>
  <button
    v-bind="$attrs"
    :type="type"
    class="mg-button"
    :class="[
      `mg-button-${variant}-${color}`,
      `mg-button-${size}`,
      { 'mg-button-block': block, 'mg-button-loading': loading },
    ]"
    :disabled="disabled || loading"
    @click="handleClick"
  >
    <!-- Loading state -->
    <template v-if="loading">
      <span class="mg-button-loading-icon" />
      <span v-if="showLabelWhileLoading" class="mg-button-label">
        <slot name="loading-label">{{ loadingLabel || label }}</slot>
      </span>
    </template>

    <!-- Normal state -->
    <template v-else>
      <span v-if="hasIconSlot || icon" class="mg-button-icon">
        <slot name="icon">
          <component :is="icon" v-if="typeof icon !== 'string'" />
          <span v-else-if="icon">{{ icon }}</span>
        </slot>
      </span>
      <span v-if="hasLabel" class="mg-button-label">
        <slot>{{ label }}</slot>
      </span>
    </template>
  </button>
</template>

<script setup lang="ts">
import { useSlots, computed } from "vue"
import type { Component } from "vue"
import type { Size, AddonColor } from "../types/components"

defineOptions({ name: "Button", inheritAttrs: false })

type Variant = "filled" | "outline"
type ButtonType = "button" | "submit" | "reset"

interface Props {
  label?: string
  variant?: Variant
  color?: AddonColor
  size?: Size
  type?: ButtonType
  disabled?: boolean
  loading?: boolean
  showLabelWhileLoading?: boolean
  loadingLabel?: string
  block?: boolean
  icon?: string | Component
}

const props = withDefaults(defineProps<Props>(), {
  label: "",
  variant: "filled",
  color: "primary",
  size: "sm",
  type: "button",
  disabled: false,
  loading: false,
  showLabelWhileLoading: false,
  block: false,
})

defineSlots<{
  default: () => any
  icon: () => any
  "loading-label": () => any
}>()

const slots = useSlots()
const hasIconSlot = computed(() => !!slots.icon)
const hasLabel = computed(() => props.label !== "" || !!slots.default)

const emit = defineEmits<{ click: [event: MouseEvent] }>()

const handleClick = (event: MouseEvent) => {
  if (props.disabled || props.loading) return
  emit("click", event)
}
</script>
```

## 10. Design Trade-offs Summary

Looking back at the design process above, several key trade-offs:

- **Decoupling `variant` and `color`**: focusing on separating "visual mode" from "semantic colors"
- **Size simplification**: 3 sizes are sufficient to cover personal blog scenarios
- **Loading state refinement**: `showLabelWhileLoading`/`loadingLabel` is a response to the "loading text" requirement
- **Icon support flexibility**: prop for quick usage + slot for full control

## 11. Discussion on Loading State Width Changes

There's a classic pitfall in component development — **width changes during button loading cause layout shift (CLS)**.

The common solution is `min-width` reservation + absolutely positioned loading icon. But in v1.5.0's actual implementation, I **did not adopt this approach**, for the following reasons:

1. **`min-width: 88px` is a hardcoded value** that breaks the flexibility of "small components responding to different content widths"
2. An absolutely positioned loading icon breaks out of the document flow when `loading="true"`; if the button has other content (like loading text), the layout may still change
3. A better performance optimization is to **ensure the loading state text length is close to the normal state** (`loadingLabel` helps users keep text consistent)

If your scenario truly has strict CLS requirements (like an e-commerce checkout button), you can manually add this at the business layer:

```css
.my-order-button {
  min-width: 120px; /* Reserve based on your actual button width */
}
```

Keep the component library flexible; users optimize as needed.

## 12. Design Decision Summary

| Decision | Reason |
| --- | --- |
| Remove `xs` size | Low usage frequency, simplifies API |
| Remove `ghost` variant | Can be replaced by `outline` |
| Remove `neutral` color | Can be replaced by `outline` + default color |
| Default size is `sm` | Mainstream UI libraries default button is ~32px |
| Default `type="button"` | Prevents accidental form submission |
| `icon` prop + `#icon` slot coexistence | Quick usage + full control, slot takes precedence |
| `label` prop + default slot | Both writing styles are supported |
| `hasLabel = label !== ''` | Icon-only button doesn't render empty label container |
| Loading enhancement | `showLabelWhileLoading` + `loadingLabel` |
| `v-bind="$attrs"` | Passes through native attributes, maintains flexibility |
| No aria-busy/aria-disabled added | Relies on native disabled semantics + axe-core test assurance |
| No CLS magic | Keeps flexibility, users optimize as needed |

## 13. Test Assurance

The Button in v1.5.0 has **20 component tests** covering:

- Default props rendering
- variant/color/size variant classes
- Loading state (auto-disable, hide text, showLabelWhileLoading, loadingLabel, slot)
- Icon-only button (empty label doesn't render container)
- `icon` prop (string renders text, Component renders component, slot takes precedence over prop)
- Click doesn't trigger when disabled/loading
- Attribute fallthrough (id, data-\*)
- `type` prop fallthrough

Along with **axe-core accessibility checks** (Button has zero violations) and **SSR rendering checks**.

---

This article used Button as an example to outline the key design points of simple components. The next article will dive into complex components, covering data adaptation, internal state, logic reuse, and other more advanced topics.

---

## 🌙 About Moongate Vue

This article is from the Moongate Vue Component Library Design Series (4 articles), all content is based on real project practice:

- **Project Repository**: [github.com/yuelinghuashu/moongate-vue](https://github.com/yuelinghuashu/moongate-vue) — A minimal Vue 3 component library, zero dependencies, CSS-first, 25KB gzip
- **Real Example**: [moongate.top](https://moongate.top) — A personal blog built by migrating from Nuxt UI v4 to Moongate Vue
- **Online Documentation**: [vue.moongate.top](https://vue.moongate.top) — Component API and theme customization guide
