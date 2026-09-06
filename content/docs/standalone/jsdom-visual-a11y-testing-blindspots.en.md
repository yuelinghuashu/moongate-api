---
title: "All Tests Green, Component Still Broken — the Visual & A11y Blind Spots jsdom Can't See"
description: "497 unit tests all green, yet the Tooltip never appears on hover and the Select's screen reader can't announce the highlighted option. A two-part post-mortem: jsdom renders no pixels and enforces no ARIA spec, so these blind spots can only be covered by e2e computed-style assertions, pixel snapshots, and spec review. With reproducible minimal examples."
date: 2026-09-07
series: ""
tags:
  - Vue
  - Engineering
---

> Green unit tests ≠ a correct component. jsdom can't see two things: **visual correctness** (`opacity`) and **accessibility spec compliance** (`aria-*`). This is a follow-up to the [Vue 3 Teleport Component Unit Testing Guide](../url-state/vue-teleport-unit-testing-jsdom-pitfalls.en).

## Background

In the [Vue 3 Teleport Component Unit Testing Guide](../url-state/vue-teleport-unit-testing-jsdom-pitfalls.en), we covered how to write jsdom tests _correctly_ — mounting, unmounting, cleanup, timing. That article carried an implicit assumption: "if the tests are written right and everything is green, the component is correct."

Reality slapped us in the face — twice.

Our component library's unit tests climbed to **497, all green**, with 95%/86% coverage. And yet, in a real browser:

- **Hovering the Tooltip trigger showed nothing at all**;
- **The Select dropdown's screen reader couldn't announce the currently highlighted option**.

The ironic part: both issues **"passed" the unit tests**. They weren't cases of badly written tests — they were cases where **jsdom fundamentally lacks the ability to see these two classes of bugs**: it doesn't render pixels (visuals), and it doesn't implement ARIA semantics (spec). Both blind spots can only be covered by real browsers / spec review.

---

## Blind Spot 1: Visual correctness — the invisible component behind `opacity: 0`

### Symptom

In the docs site (VitePress), hovering over the Tooltip trigger shows nothing.

### Investigation

The floating layer does mount in the template:

```vue
<Teleport to="body">
  <div
    v-if="visible"
    class="mg-tooltip"
    :class="`mg-tooltip-${placement}`"
    role="tooltip"
  >
    {{ content }}
  </div>
</Teleport>
```

When `v-if="visible"` holds, the element is in the DOM, and `role="tooltip"` is there too. But it's simply invisible.

The CSS explains it:

```css
.mg-tooltip {
  /* ... */
  opacity: 0; /* transparent by default */
  transition: opacity 150ms ease;
}

.mg-tooltip-visible {
  opacity: 1; /* needs this class to become visible */
}
```

**The floating layer defaults to `opacity: 0` and only becomes visible with `.mg-tooltip-visible` — but the template never binds that class anywhere** — so the element stays mounted, and stays transparent, forever.

### Why didn't the unit tests catch it?

This is the key part. Look at the unit test assertion at the time:

```ts
// Tooltip.test.ts (before the fix)
it("shows the tooltip on mouseenter", async () => {
  await trigger("mouseenter")
  const tooltip = document.body.querySelector(".mg-tooltip")
  expect(tooltip).not.toBeNull() // ✅ passes
})
```

It asserts that "**the `.mg-tooltip` element exists in the DOM**". And the element does exist — it's just transparent. **jsdom doesn't render CSS** (no layout engine, no visual result of `getComputedStyle`), so it can neither tell you what `opacity` is, nor fail because the element is transparent.

> **jsdom's visual blind spot**: jsdom is a "DOM simulator", not a browser. It manages DOM structure, events, and attributes — but it has nothing to do with **pixels**. "Visual correctness" concerns like `opacity`, `visibility`, `z-index`, and `position` are entirely invisible to jsdom.

### The fix

Bind the visible class so `opacity` becomes 1 when shown:

```vue
<div
  v-if="visible"
  class="mg-tooltip"
  :class="[
    `mg-tooltip-${placement}`,
    { 'mg-tooltip-visible': visible }, // added: apply the visible class when shown
  ]"
  role="tooltip"
>
```

### Sidebar: the boundary between logic assertions and visual verification

Unit tests can't render visuals. What they _can_ assert is the **result** of `v-if` — whether the floating layer mounted (an observable behavior):

```ts
it("mounts the floating layer on hover", async () => {
  await trigger("mouseenter")
  // Assert the observable behavior: the layer does appear in the DOM
  expect(document.body.querySelector(".mg-tooltip")).not.toBeNull()
})
```

> ⚠️ **Don't assert visibility via `classList.contains('mg-tooltip-visible')`**: that treats an **implementation detail** as the test target. CSS class names are a styling seam, not behavior — if you later switch `opacity` to `transform: scale(0)`, or move to a `<Transition>` that renames classes, this assertion will fail for innocent reasons (this is the classic "test behavior, not implementation" pitfall). If you want an assertable "state seam", modern component libraries increasingly prefer attributes like `data-state="open"` over styling class names.

### A three-tier model for visual verification: it's not "only e2e", nor "one e2e solves everything"

Saying "visuals can only be verified by e2e" is too coarse. Visual correctness actually splits into three tiers, each with different tools and blind spots:

| Tier                 | What it verifies                                                          | Tools                                                      | Covers / Doesn't cover                                                                                                                                                      |
| -------------------- | ------------------------------------------------------------------------- | ---------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **① Logic**          | State, DOM structure                                                      | Unit tests (jsdom)                                         | Catches "not mounted"; can't test pixels                                                                                                                                    |
| **② Computed style** | `opacity` / `visibility` / `z-index` / `position` / clipped by `overflow` | e2e asserting `getComputedStyle` / `getBoundingClientRect` | Catches "wrong property value", "element off-viewport"; **can't catch "covered by another element"** (when a higher `z-index` hides it, `opacity: 1` still means invisible) |
| **③ Pixel snapshot** | Whether the final render "looks right"                                    | Visual regression (`toHaveScreenshot`, Percy, Chromatic)   | Catches everything (including occlusion and misalignment); but brittle (platform fonts/environment dependent) and costly                                                    |

- The Tooltip bug this time belongs to **②**: `getComputedStyle(tooltip).opacity === '1'` would have caught it.
- But in the same investigation, the Select bug — "clipped by an `overflow` container" — **② couldn't fully catch either**: `getComputedStyle` won't tell you "the element is clipped out of sight". You need to measure whether `getBoundingClientRect()` lands inside the container's visible area, or just hand it to **③**.
- **③ is the ultimate backstop, but it can be "tamed" rather than uniformly avoided**:
  - **Prefer component-level snapshots over page-level E2E snapshots**: use tools like Chromatic/Storyshots to render a **single component** for screenshot diffing — inputs are controlled (fixed props/size/theme), noise is far lower than whole-page screenshots. This is the approach leading international teams (Spotify, Microsoft, etc.) favor;
  - **Reduce brittleness**: when diffing, configure the tool to **ignore font loading and anti-aliasing differences** (diff only on structural layout/color deviations), and raise the **diff threshold** to a tolerance your component accepts — keeping noise like "platform font rendering differences" outside the threshold;
  - That's how "choose by critical path" becomes actually executable: not "avoid it whenever possible", but "critical scenarios + component-level snapshots + a sensible threshold".

> Corrected takeaway: it's not that "e2e is the only solution", but that "**② covers common visual-property errors, ③ covers pixel-level correctness — pick per need**". jsdom can't even do ②, which is exactly the part that must lean on e2e.

---

## Blind Spot 2: A11y spec — `aria-activedescendant` bound to the wrong element

### Symptom

While adding e2e keyboard tests for Select, the assertion that `aria-activedescendant` points at the currently highlighted option **kept failing**.

### Investigation: where is it actually bound in the DOM?

Reading the DOM directly in a real browser via Playwright revealed:

```json
// the input element:
{ "inputAriaAttrs": [] }

// the dropdown container (listbox):
{ "listboxActDesc": "v-0-option-0" }
```

`aria-activedescendant` was bound on the **listbox container**, not on the **focused input**.

### Why is this wrong? — Focus Delegation

First, the spec conclusion: in a WAI-ARIA combobox/listbox combo, `aria-activedescendant` **must sit on the element that has focus** (here, the typeable `<input>`).

But to understand "why it must be on the input", you need the underlying mechanism — **focus delegation** — which boils down to two points:

1. **DOM focus can only rest on one element.** While choosing from the dropdown, the focus host is the typeable `<input>` (the combobox) — it _is_ `document.activeElement`; the listbox container is just a "receiver": it defines the option collection but never holds focus.
2. `aria-activedescendant` is a **virtual focus proxy**: DOM focus doesn't move, but the screen reader's **virtual cursor** follows the option it points to — so the attribute must live on the focus host. Putting it on the container is equivalent to not putting it anywhere.

Against this mechanism, the problem becomes obvious:

- Bound on the **input** → the screen reader treats the input as the host, the virtual cursor follows the option, and it announces "the current option is xxx";
- Bound on the **listbox** → at the focus point, the screen reader finds no cue indicating "the currently active item" — **it cannot read the current option at all**.

This is a classic case of "attribute exists but sits in the wrong place" — **axe usually won't report it under its default rule set** (host-element validation for `aria-activedescendant` belongs to experimental / rule-set-enabled composite rules, not enforced by default). Only manual spec review, or a targeted test asserting the attribute is on the **focused element** (not the container), can surface it.

### Why didn't the unit tests catch it?

The unit test asserted that "the listbox has this attribute":

```ts
// Select.test.ts (before the fix)
expect(dropdown.attributes("aria-activedescendant")).toBe(
  option0.attributes("id"),
)
```

**The assertion itself was "wrong"** — it validated the wrong location. jsdom doesn't validate specs; whatever you assert, it hands back. So the unit tests were "green" precisely because **the tests had enshrined the incorrect implementation as the expectation**.

> **jsdom's accessibility blind spot**: jsdom doesn't implement ARIA semantics and has no screen reader. Whether an `aria-*` attribute is "right" depends on **spec compliance**, and spec correctness is something jsdom can't judge — it merely reflects the bindings you wrote.

### The fix

Move `aria-activedescendant` from the listbox to the input:

```vue
<!-- on the input: the spec-mandated location -->
<input
  :aria-activedescendant="
    focusedIndex >= 0 ? getOptionId(focusedIndex) : undefined
  "
  @keydown.down.prevent="moveFocus(1)"
  @keydown.up.prevent="moveFocus(-1)"
  ...
/>
```

And fix the unit test assertion location to match:

```ts
// After the fix: assert on the input
expect(input.attributes("aria-activedescendant")).toBe(option0.attributes("id"))
```

### Two more "spec blind spots" in the same Select

While tracking down `aria-activedescendant`, the e2e tests surfaced two more problems the unit tests couldn't catch:

**① Missing Home/End keyboard handling** (required by the WAI-ARIA listbox keyboard convention):

```vue
@keydown.home.prevent="moveFocusTo(0)"
@keydown.end.prevent="moveFocusTo(filteredOptions.length - 1)"
```

**② `Tab` couldn't close the dropdown** — a stale `mousedownInside=true` made `blur` be misread as "clicked an option", so the dropdown stayed open:

```ts
const openDropdown = () => {
  // ...
  mousedownInside.value = false // fix: reset on open, so a stale true can't swallow blur
}
```

Both are **event-timing / spec** problems that jsdom unit tests are equally powerless against — only a real browser firing real keyboard/focus flows exposes them.

---

## Why e2e is unavoidable: the "layering" of tests

| Layer                     | What it verifies                                                            | Tool                                | Blind spot                     |
| ------------------------- | --------------------------------------------------------------------------- | ----------------------------------- | ------------------------------ |
| **Logic**                 | State, events, props, DOM structure                                         | Vitest + jsdom                      | Visuals, spec                  |
| **Presentation (attrs)**  | `opacity` / `z-index` / `position`, keyboard flow, ARIA attribute placement | Playwright + real browser           | Pixel-level occlusion/overlap  |
| **Presentation (pixels)** | Whether the final render "looks right"                                      | Visual regression (screenshot diff) | Platform differences (brittle) |

Unit tests (logic layer) are fast and dense, but they **assume you "get the placement of visual bindings right"**; if you get it wrong (missing binding, wrong element), they won't remind you.

E2E (presentation layer) is slow and sparse, but it verifies what **real users see and hear**. Among them, "attribute verification" covers common visual issues (`opacity`/`z-index`), and "pixel snapshots" cover ultimate correctness (including occlusion) — the latter is expensive, so choose by critical path.

**The right division of labor**:

1. Unit tests cover logic and data flow (fast, numerous, regression net);
2. E2E covers "visible + readable" — hover shows a tooltip, focus lands on an option, screen-reader semantics are correct;
3. Add pixel snapshots on critical paths, to guard against "all properties right but the picture wrong".

---

## Summary: give your tests the "invisible" dimensions

| Blind spot                  | Example                                                                      | Why jsdom can't see it                            | The real solution                                                 |
| --------------------------- | ---------------------------------------------------------------------------- | ------------------------------------------------- | ----------------------------------------------------------------- |
| **Visual properties**       | Tooltip `opacity: 0`, Select clipped by `overflow`                           | jsdom doesn't render CSS/pixels                   | e2e asserting `getComputedStyle` / `getBoundingClientRect`        |
| **Pixel-level correctness** | Floating layer hidden behind `z-index`, layout misalignment                  | jsdom has no rendering                            | Visual regression (screenshot diff, chosen by critical path)      |
| **A11y spec**               | `aria-activedescendant` on the wrong element (focus-delegation misplacement) | jsdom doesn't implement ARIA semantics            | e2e asserting attributes on the **focused element** + spec review |
| **Keyboard/focus flow**     | Select missing Home/End; Tab can't close it                                  | jsdom doesn't simulate real keyboard/focus timing | e2e real-keyboard events                                          |

Unit tests confirm "**the code runs the way I intended**"; e2e confirms "**the user sees what I intended**". You need both — especially for a component library where "one component is reused by thousands of people": visual and a11y blind spots get magnified without limit.

---

## Appendix: reproducible minimal examples

### Tooltip visual blind spot (jsdom can't see `opacity`)

```vue
<!-- MyTooltip.vue -->
<template>
  <div
    class="tooltip-trigger"
    @mouseenter="show = true"
    @mouseleave="show = false"
  >
    Hover me
    <Teleport to="body">
      <div v-if="show" class="my-tooltip">Tooltip content</div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { ref } from "vue"
const show = ref(false)
</script>

<style>
.my-tooltip {
  opacity: 0;
} /* ❌ the .visible class is missing, so the element stays transparent */
</style>
```

```ts
// my-tooltip.test.ts —— all green in jsdom, invisible in the browser
import { mount } from "@vue/test-utils"
import MyTooltip from "./MyTooltip.vue"

it("shows the tooltip on hover", async () => {
  const wrapper = mount(MyTooltip, { attachTo: document.body })
  await wrapper.find(".tooltip-trigger").trigger("mouseenter")
  // ✅ the element exists — jsdom assertion passes
  expect(document.body.querySelector(".my-tooltip")).not.toBeNull()
  // ❌ but opacity is 0 — the user can't see it at all
})
```

### `aria-activedescendant` bound to the wrong element (spec blind spot)

```vue
<!-- Wrong: bound on the listbox container -->
<div role="listbox" :aria-activedescendant="activeId">
  <div role="option" :id="option0Id">A</div>

<!-- Correct: bound on the focused input -->
<input
  :aria-activedescendant="activeId"
  role="combobox"
  aria-expanded="true"
  aria-controls="listbox-id"
/>
```

---

## Closing thoughts

The biggest takeaway from this investigation wasn't "we fixed two bugs" — it was rediscovering the boundary of our tests: **jsdom is an excellent logic validator, but a poor visual/spec validator**.

A component library with only unit tests is like a car that passed a full electrical inspection but was never taken for a test drive — **whether the headlights turn on, or the steering wheel turns, unit tests can't tell you**. Adding e2e is what makes a library genuinely "road-worthy".

And these two bugs also remind us: **green tests were never the finish line — only the starting point**. The real question to ask is: what exactly do the "green" tests verify, and what do they miss?
