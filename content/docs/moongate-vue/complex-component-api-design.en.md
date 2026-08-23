---
title: 'Vue 3 Complex Component Development: API Design for Select and Pagination'
description: Using Select and Pagination as examples, explore Vue 3 complex component API design, data format adaptation, type backtracking, searchable/multi-select, ARIA keyboard navigation, composable extraction, and SSR adaptation, revealing the design trade-offs and implementation details behind industrial-grade components.
date: 2026-05-19
permalink: 62e1afa3-c02e-4bf8-bc52-36f3b13032e9
level: P3
series: moongate-vue
tags:
  - Vue
  - Design System
  - Engineering
---

> Simple components are one-way data consumers; complex components are data adapters and state coordinators. Using Select and Pagination as examples, let's see where the "industrial-grade details" lie.

## 1. Introduction

If writing a Button component is about enjoying the "paint-like beauty" of CSS variables, then writing Select and Pagination is about wrestling in the "mud pit" of native HTML's historical baggage. Simple components are one-way data consumers, while complex components are **data adapters** (compatible with multiple formats) and **state coordinators** (searchable, multiple selection, keyboard navigation, SSR-safe).

This article uses **Select** and **Pagination** as examples to demonstrate the complex component development approach behind the v1.5.0 implementation.

## 2. Select Dropdown: The Data Adapter

The Select component needs to accept a list of options and allow the user to choose one. Real-world APIs may return object arrays, string arrays, or even number arrays, so the component must have robust data adaptation capabilities.

### 2.1 Requirements Analysis

- Support object arrays `{ label, value }` (default)
- Support custom field names (`labelKey` / `valueKey`)
- Support string arrays `['Option A', 'Option B']`
- Support number arrays `[1, 2, 3]`
- Provide a placeholder (non-selectable default option)
- Support disabled options (`disabled: true`)
- **Searchable mode** (filterable): input filtering + dropdown panel
- **Multiple selection mode** (multiple + filterable): tag display
- **Must solve the type trap of native `<select>` returning strings**
- **Full ARIA keyboard navigation** (listbox + option + aria-activedescendant)

### 2.2 Dual-Mode Architecture: Native Mode + Searchable Mode

The v1.5.0 Select supports **dual modes**:

- **Native mode** (default): renders a native `<select>`, zero JS overhead
- **Searchable mode** (`filterable=true`): renders a custom input + dropdown panel, supporting search/multiple selection/keyboard navigation

```vue
<!-- Native mode: best performance -->
<Select v-model="category" :options="categories" />

<!-- Searchable mode: filtering + dropdown -->
<Select v-model="fruit" :options="fruits" filterable />

<!-- Searchable + multiple selection -->
<Select v-model="tags" :options="tags" filterable multiple />
```

### 2.3 API Design and Type Anti-Corruption

To avoid type pollution caused by `any`, we use union type narrowing:

```typescript
export type SelectValue = string | number
export type SelectOption = string | number | Record<string, any>

interface Props {
  options?: SelectOption[]
  placeholder?: string
  size?: Size
  disabled?: boolean
  error?: boolean
  labelKey?: string // default 'label'
  valueKey?: string // default 'value'
  filterable?: boolean // searchable mode
  emptyText?: string
  maxHeight?: number // dropdown panel max height
  multiple?: boolean // multiple selection (requires filterable)
}
```

**Type backtracking** solves the problem of native select always returning strings:

```typescript
const handleNativeChange = (event: Event) => {
  const target = event.target as HTMLSelectElement
  const rawValue = target.value

  // Find the original type (number or object value) in the original options
  const originalItem = props.options?.find(
    (item) => String(getValue(item)) === rawValue,
  )
  const finalValue =
    originalItem !== undefined ? getValue(originalItem) : rawValue

  modelValue.value = finalValue
  emit("change", finalValue)
}
```

This logic ensures that a number value bound via `v-model` won't accidentally become a string.

### 2.4 Attribute Passthrough Split

This is a key detail of the searchable mode — **which attributes pass through to the native input and which stay on the outer wrapper**:

```typescript
const attrs = useAttrs()

/** Form/aria attributes that pass through to the native form element */
const formAttrs = computed(() => {
  const result: Record<string, unknown> = {}
  for (const [key, value] of Object.entries(attrs)) {
    if (
      key.startsWith("aria-") ||
      ["name", "id", "role", "tabindex"].includes(key)
    ) {
      result[key] = value
    }
  }
  return result
})

/** Remaining attributes retained on the outer wrapper (class/style/events, etc.) */
const wrapperAttrs = computed(() => {
  const result: Record<string, unknown> = {}
  for (const [key, value] of Object.entries(attrs)) {
    if (!(key in formAttrs.value)) {
      result[key] = value
    }
  }
  return result
})
```

Why is this split necessary? If `aria-label` stays on the outer wrapper without being passed through to the actual `<input>`, screen readers won't be able to identify the input's accessible name — this triggers an `aria-input-field-name` violation in axe-core checks in `a11y.test.ts`.

### 2.5 ARIA Keyboard Navigation (WAI-ARIA Combobox Pattern)

The keyboard navigation in searchable mode follows the WAI-ARIA Combobox pattern:

```vue
<!-- Dropdown panel -->
<div
  v-if="isOpen"
  ref="dropdownRef"
  class="mg-select-dropdown"
  role="listbox"
  :aria-label="listboxAriaLabel"
  :aria-activedescendant="focusedIndex >= 0 ? getOptionId(focusedIndex) : undefined"
>
  <!-- Options -->
  <div
    v-for="(item, index) in filteredOptions"
    :id="getOptionId(index)"
    role="option"
    :aria-selected="isSelected(item)"
    :class="{ 'mg-select-option-focused': focusedIndex === index }"
    @click="selectOption(item)"
    @mouseenter="focusedIndex = index"
  >
```

Supported operations:

- `ArrowDown` / `ArrowUp`: move highlight (`focusedIndex`) and `scrollIntoView` to keep it visible
- `Enter`: select the currently highlighted option
- `Esc`: close the dropdown
- Each option has a unique `id` (based on `useId()`) for `aria-activedescendant` reference

### 2.6 Multiple Selection Mode

Multiple selection (`multiple + filterable`) turns `modelValue` into an array. The core branching logic for selection:

```typescript
// Multiple selection: toggle selection (remove if selected, append if not)
if (props.multiple) {
  const current = multipleValues.value
  const isAlreadySelected = current.some((v) => String(v) === String(value))
  const next = isAlreadySelected
    ? current.filter((v) => String(v) !== String(value))
    : [...current, value]

  modelValue.value = next
  // Multiple selection keeps the dropdown open for continuous selection
  nextTick(() => inputRef.value?.focus())
  return
}

// Single selection: close after selection
modelValue.value = value
closeDropdown()
```

During multiple selection:

- Selected items render as tags; each tag has a delete button with `aria-label="Remove {label}"`
- The dropdown **stays open** after selection (convenient for continuous multiple selection)
- The input only displays search text; selected tags appear outside

## 3. Pagination: The State Coordinator

### 3.1 API Design (v1.5.0 Actual)

```typescript
interface Props {
  totalPages: number // total pages (required)
  modelValue: number // current page (v-model)
  size?: "sm" | "md" | "lg"
  showQuickJump?: boolean // first/last page quick jump buttons (default true)
  prevText?: string // previous page text (uses global i18n)
  nextText?: string
  firstText?: string
  lastText?: string
}
```

Pagination uses `defineModel<number>` to bind the current page:

```typescript
const currentPage = defineModel<number>({ required: true })
```

### 3.2 Quick Jump + Page Number Editing

```vue
<!-- Display mode: clickable number → enter editing -->
<span v-else class="mg-pagination-current" @click="startEdit">
  {{ currentPage }}
</span>

<!-- Edit mode: input field -->
<input
  v-if="isEditing"
  v-model="inputPage"
  type="number"
  :min="1"
  :max="totalPages"
  @blur="commitJump"
  @keyup.enter="commitJump"
/>
```

Extreme edge-case defense in `commitJump`:

```typescript
const commitJump = () => {
  isEditing.value = false
  const newPage = parseInt(String(inputPage.value), 10)

  // Invalid input: abandon and restore
  if (isNaN(newPage)) {
    inputPage.value = currentPage.value
    return
  }

  goToPage(newPage) // clamp to [1, totalPages]
}

const goToPage = (page: number) => {
  let newPage = page
  if (newPage < 1) newPage = 1
  if (newPage > props.totalPages) newPage = props.totalPages
  if (newPage === currentPage.value) return
  currentPage.value = newPage
  emit("change", newPage)
}
```

### 3.3 Global Text (i18n)

v1.5.0 introduced a global text system. All aria-labels and button text in Pagination go through the configuration chain:

```typescript
const texts = useTexts() // reactive global text

const prevTextValue = computed(
  () => props.prevText ?? texts.value.paginationPrev,
)
const pageInfoLabel = computed(() =>
  formatTemplate(texts.value.paginationPageInfo, {
    current: currentPage.value,
    total: props.totalPages,
  }),
)
```

Priority: **Component prop > setConfig texts > Built-in language text**. Text supports `{current}`/`{total}` template placeholders.

## 4. Composable Extraction

Complex components often need to extract shared logic. The core composables in v1.5.0:

### 4.1 useFormField (Shared by Input/Textarea)

Handles `v-model` updates + native event passthrough:

```typescript
export function useFormField(modelValue, emit) {
  const handleInput = (event: Event) => {
    modelValue.value = (event.target as HTMLInputElement).value
    emit("input", event)
  }
  // change/focus/blur native event passthrough
  return { handleInput, handleChange, handleBlur, handleFocus }
}
```

### 4.2 useFloating (Shared by Popover/Tooltip)

Custom floating layer positioning engine: viewport flipping + boundary correction + ResizeObserver:

```typescript
export function useFloating(options: UseFloatingOptions) {
  // Delayed show/hide
  // Position calculation (directional positioning + viewport flipping)
  // Reposition on scroll/window resize
  // ResizeObserver monitors only the floating layer's own size
  // SSR-safe (isBrowser guard)
  return { triggerRef, floatingRef, visible, currentPlacement, floatStyle, show, hide, ... }
}
```

### 4.3 useOverlayBehavior (Shared by Modal/Drawer, located in useScrollLock.ts)

Scroll locking + ESC close + focus trap:

```typescript
// composables/useScrollLock.ts
export function useOverlayBehavior(isOpen, overlayRef, onClose, options) {
  // body scroll lock (module-level lockCount counter, multi-instance safe)
  // ESC key close
  // Tab focus trap
}
```

This function is defined together with scroll locking logic (`lockBodyScroll`/`unlockBodyScroll`) in **`useScrollLock.ts`** — scroll lock, ESC close, and focus trap all semantically belong to the "overlay behavior" concern, hence they are placed in the same file.

**Module-level lock counting** solves scroll lock conflicts when multiple Modal/Drawer instances are open simultaneously — body scrolling is only restored when the last one closes.

## 5. SSR Adaptation: useId and isBrowser

The SSR adaptation in v1.5.0 is more refined than the initial version, with two core mechanisms:

### 5.1 useId() Ensures Hydration Safety

All components that require `id` (Modal/Drawer/Select/Tabs) use Vue 3's `useId()`:

```typescript
const selectBaseId = useId()
const getOptionId = (index: number): string => `${selectBaseId}-option-${index}`
```

`useId()` generates consistent IDs on both server and client, avoiding hydration mismatches.

### 5.2 isBrowser Guard

All DOM operations include a browser environment guard:

```typescript
const isBrowser =
  typeof window !== "undefined" && typeof document !== "undefined"

// In watch/onMounted/top-level code:
if (!isBrowser) return
```

Combined with the module-level counter in `useScrollLock.ts`, `lockBodyScroll()` directly skips DOM operations in non-browser environments.

### 5.3 createOverlay: SSR-Safe Imperative Component

```typescript
// composables/createOverlay.ts
export function createOverlay(component, props, containerClass) {
  if (!isBrowser) return null // SSR returns null
  // ...
}
```

## 6. Data and State Flow

```text
External Data ──► Data Adapter ──► Internal State
(options)        (getLabel/        (selected/
                  getValue)         searchText)
   ▲                                  │
   │                                  ▼
Global Env  ◄── State Coordinator ◄── User Interaction
(i18n/SSR)     (watch/event)        (click/keyboard)
```

- **Left column**: External input (data + user actions)
- **Middle**: Adaptation and coordination (translating the external world into internal state)
- **Right column**: Internal state (component self-managed)
- **Bottom**: Global environment (i18n / SSR) reverse-constraining component behavior

## 7. Testing Strategy

The v1.5.0 Select has **40+ tests** and Pagination has **14 tests**, covering:

| Concern | Test Content |
| --- | --- |
| **Type Backtracking** | Native mode number array `[10,20,30]` still produces a number modelValue after selection |
| **ARIA Navigation** | `aria-activedescendant` points to highlighted option, each option has a unique id |
| **Keyboard Operations** | ArrowDown/Up highlight, Enter select, Esc close, boundary doesn't overflow |
| **Multiple Selection** | Tag rendering, toggle selection, tag deletion, Enter continuous multiple selection |
| **Edge Cases** | Empty search results, external modelValue change, dropdown stays open on blur |
| **Accessibility** | axe-core reports no violations for Select (native + searchable) |

Plus **SSR checks**: `renderToString` confirms components don't crash on the server and floating layers are hidden by default.

## 8. Summary

| Concern | Simple Component (Button) | Complex Component (Select / Pagination) |
| --- | --- | --- |
| **Props Count** | Fewer (11) | More (10-15) |
| **Data Format** | Fixed (string) | Flexible (supports multiple array types, configurable fields, type anti-corruption) |
| **State Management** | No internal state | Searchable text, multiple selection array, edit state, dropdown visibility |
| **Accessibility** | Native semantics | WAI-ARIA Combobox pattern (listbox/option/activedescendant) |
| **Logic Reuse** | Not needed | Composables (useFormField/useFloating/useOverlayBehavior) |
| **SSR Adaptation** | Automatic | useId hydration safety + isBrowser guard |
| **i18n** | Minimal text | Global configuration chain (prop > setConfig > built-in) |
| **Testing Strategy** | Snapshots, event triggers | State combinations, edge cases, keyboard simulation, type backtracking, axe-core |

An excellent complex component, internally, should be like a vacuum cleaner — accommodating all kinds of bizarre backend data formats (through key mapping and type backtracking) — and externally, should be like a gentleman — coupling with the global environment (i18n, SSR, keyboard) in a restrained manner. **High cohesion, low coupling** is fully embodied in these two types of components.

---

## 🌙 About Moongate Vue

This article is from the Moongate Vue Component Library Design Series (4 articles), all content is based on real project practice:

- **Project Repository**: [github.com/yuelinghuashu/moongate-vue](https://github.com/yuelinghuashu/moongate-vue) — Minimalist Vue 3 component library, zero dependencies, CSS-first, 25KB gzip
- **Real-world Example**: [moongate.top](https://moongate.top) — Personal blog, migrated from Nuxt UI v4 to Moongate Vue
- **Online Documentation**: [vue.moongate.top](https://vue.moongate.top) — Component API and theme customization guide
