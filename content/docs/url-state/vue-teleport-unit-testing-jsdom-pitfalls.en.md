---
title: "Vue 3 Teleport Component Unit Testing Guide: 5 jsdom Traps and 2 Bugs Caught Along the Way"
description: "While writing 204 unit tests for our component library Moongate Vue, Teleport components kept tripping us up in the jsdom environment — 5 traps, plus 2 hidden bugs uncovered along the way. A full post-mortem with reproducible minimal examples."
date: 2026-08-05
series: ""
level: P4
tags:
  - Vue
  - Engineering
  - TypeScript
---

> Writing tests for Teleport components: 5 jsdom traps + 2 hidden bugs caught along the way — every trap comes with a reproducible minimal example.

## Background

Moongate Vue is a Vue 3 component library with 25 components. Several of them — `Modal`, `Drawer`, `Message`, `Toast` — use `<Teleport to="body">` to render content into `document.body`.

To keep the test environment as close to a real browser as possible, we chose Vitest + jsdom as our testing infrastructure. We figured it would just be a few assertions — but we kept tripping over Teleport components: **24 tests failed, and most of them pointed at the same eerie error**:

```text
TypeError: Cannot read properties of null (reading 'insertBefore')
```

After digging in, we found it wasn't a coincidence — it was the inevitable result of **Teleport mechanics + the jsdom environment + Vue's async updates** interacting in a specific timing. Below, each trap is broken down as trap → symptom → root cause → solution.

---

## Part 1: The 5 Teleport Testing Traps

### Trap 1: Teleport Content Sits at the Top Level of body — the Wrapper Can't Find It

**Symptom**:

`wrapper.find('.mg-modal-overlay')` returns empty, even when the component is mounted with `attachTo: document.body`.

**Root cause**: Teleport's target element is `body`, and its rendered content becomes a **direct child of `body`** — it's **not inside the `wrapper.element` subtree**. Even though you can reach the component instance via `wrapper.vm`, the `v-if` content rendered by Teleport lives outside the DOM tree managed by the wrapper.

```ts
// ❌ Wrong: the wrapper can't find Teleport content
const wrapper = mount(Modal, { props: { modelValue: true } })
expect(wrapper.find(".mg-modal-overlay").exists()).toBe(true)

// ✅ Correct: query from the top level of body
const wrapper = mount(Modal, {
  props: { modelValue: true },
  attachTo: document.body, // ① make sure it mounts into body
})
expect(document.body.querySelector(".mg-modal-overlay")).not.toBeNull()
```

**Solution**:

1. `attachTo: document.body` to ensure the component renders in body
2. Always assert Teleport content with `document.body.querySelector`, never `wrapper.find`

---

### Trap 2: Relying on Auto-Unmount Triggers `insertBefore on null`

**Symptom**: All assertions pass, but Vitest throws an async error when it finishes:

```bash
TypeError: Cannot read properties of null (reading 'insertBefore')
  at insert (runtime-dom.cjs.js:31)
  at processCommentNode ...
```

**Root cause**: Vue's reactive updates are **async, batched patches**. When the test ends and the wrapper gets auto-unmounted (or `document.body.innerHTML = ''` clears the body), DOM patch tasks from the previous render may still be sitting in Vue's internal scheduler queue. That patch tries to operate on stale nodes that are already detached from the DOM → crash.

Teleport components trigger this especially easily, because their insertion point (body) is the target **most likely to be cleared** in a test environment.

**Solution**: Explicitly unmount the wrapper at the end of the test, so Teleport has a full chance to detach itself:

```ts
import { mount } from "@vue/test-utils"

// Track all wrappers and unmount them explicitly in one place
const wrappers: ReturnType<typeof mount>[] = []
const mountDrawer = (options = {}) => {
  const wrapper = mount(Drawer, { attachTo: document.body, ...options })
  wrappers.push(wrapper)
  return wrapper
}

afterEach(async () => {
  while (wrappers.length > 0) {
    const wrapper = wrappers.pop()!
    await wrapper.unmount() // explicit unmount instead of relying on auto cleanup
  }
})
```

---

### Trap 3: DOM Pollution Between Tests

**Symptom**: The first test renders a Modal, and the second test's query for `.mg-modal-overlay` unexpectedly finds an element **left over from the previous round**; or conversely, the second test can't find what it expects.

**Root cause**: Teleport adds content to `document.body`, but the test framework's auto cleanup doesn't necessarily destroy it. Especially when a component holds **module-level caches** (like our `createOverlay` shared container Map), lingering references let the DOM pile up.

```ts
// ❌ Can't clean up Teleport leftovers this way
afterEach(() => {
  document.body.innerHTML = "" // nuke the body directly
})
```

Clearing body directly is **problematic**, because Vue still references these nodes internally — the next tick's patch operates on removed nodes and explodes (that's exactly where Trap 2's error comes from).

**Solution**: Flush first, then clear:

```ts
afterEach(async () => {
  await flushPromises() // ① wait for Vue's async work (Teleport removal, nextTick patches) to finish
  document.body.innerHTML = "" // ② then clear
})
```

> ⚠️ **Note**: if your test uses `vi.useFakeTimers()`, `flushPromises()` only drains the microtask queue — **it can't drain macrotasks** (like DOM operations triggered by `setTimeout`). In that case, restore real timers before clearing body:
>
> ```ts
> afterEach(async () => {
>   await flushPromises()
>   // If fake timers are in use, restore real timers directly (this clears the fake timer queue too),
>   // which is safer than running all macrotasks to completion.
>   if (vi.isFakeTimers()) {
>     vi.useRealTimers()
>   }
>   document.body.innerHTML = ""
> })
> ```
>
> ⚠️ **Avoid the `vi.runAllTimersAsync()` trap**: it runs all macrotasks in a loop, including **permanent timers** like `setInterval` and `requestAnimationFrame`. If the component under test has an infinite polling loop or an animation loop, this call makes the test **hang / time out**. **Restoring real timers is the safer cleanup.**

---

### Trap 4: Wrong Cleanup Order — Clearing Body First Leaves Vue Without a Parent Node

**Symptom**: Oddly, putting `document.body.innerHTML = ''` at the START of `afterEach` causes an error, while putting it at the END works fine.

**Root cause**: Vue's scheduler doesn't know the test environment exists. If you clear body before Vue has finished the last tick's DOM patch, the parent node is already null by the time it patches.

**Solution**: The correct cleanup order must be: `unmount all wrappers / destroyAllOverlays → flushPromises (useRealTimers if fake timers are in use) → clear body → restoreAllMocks`.

**This is the easiest one to overlook.** Many people (myself included) instinctively think "clearing body — when could that ever be a problem?", and then Vue teaches you otherwise, by crashing.

---

### Trap 5: Asserting Events Right After `v-model` Closes, Before the DOM Patch Happens

**Symptom**: After clicking the close button, `wrapper.emitted('update:modelValue')` has a value, but querying `document.body.querySelector('.mg-modal')` right after still finds the element (it's still there).

**Root cause**:

`emit('update:modelValue', false)` is synchronous, but removing Teleport DOM is an **async patch**. The event has been dispatched; the DOM hasn't updated yet.

**Solution**:

1. **Interact with the UI first, then assert events**: `wrapper.emitted(...)` is synchronous and available immediately after the click
2. **DOM assertions must wait for the async patch**: `await flushPromises()` or `await wrapper.vm.$nextTick()`
3. Assert the event first (synchronous dispatch), then assert the DOM (async update) — separate the two on the timeline:

```ts
// ✅ Recommended: trigger the UI first, assert the event synchronously; then wait for the async DOM update
closeBtn.click()
expect(wrapper.emitted("update:modelValue")).toBeTruthy() // the event is dispatched synchronously
await flushPromises() // wait for the DOM removal
expect(document.body.querySelector(".mg-modal")).toBeNull()
```

---

### Summary: The Golden Rules of Teleport Testing

| Rule        | In one sentence                                                                                                                                                                    |
| ----------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Mount**   | Always `attachTo: document.body`                                                                                                                                                   |
| **Assert**  | Query Teleport content with `document.body.querySelector`                                                                                                                          |
| **Unmount** | Explicitly `await wrapper.unmount()`, don't rely on auto cleanup                                                                                                                   |
| **Cleanup** | Unmount → `flushPromises()` (use `useRealTimers()` if fake timers are in use; never `runAllTimersAsync()` or you risk an infinite loop) → clear body. The order cannot be reversed |
| **Timing**  | Events are synchronous, DOM is async — `flushPromises()` before asserting                                                                                                          |

---

## Part 2: 2 Real Bugs Caught by Test-Driven Development

While writing the tests, failing assertions unexpectedly exposed two hidden defects in the component library itself. That's the real value of tests — **they don't just verify code, they step on the traps ahead of your users**.

### Bug 1: The Input Component's `change` Event Completely Vanished

**Symptom**: The component declares a `change` event, but no test can ever receive it:

```ts
// declared in Input.vue
const emit = defineEmits<{
  /** fires when the value changes (native event passthrough) */
  change: [event: Event]
}>()

// but the test never receives it
wrapper.trigger("change")
expect(wrapper.emitted("change")).toHaveLength(1) // ❌ fails
```

**Root cause**: The template only binds `@input`, `@blur`, and `@focus` — **`@change` is missing**. To understand why the event "completely disappears", you first need to understand Vue 3's event passthrough mechanism:

> In Vue 3, events listened on in a component's template that are **not declared in `emits`** are passed through to the root element as native events (they go into `$attrs`); **once declared in `emits`**, Vue considers the event explicitly handled by the component and stops passing it through.

So once `change` is declared via `defineEmits`, Vue no longer passes it through to the root `<input>`. And if the template also lacks an `@change` handler, the event **vanishes into thin air** — it neither fires a component event nor passes through to the native element.

```vue
<!-- Before the fix: @change is missing -->
<input @input="handleInput" @blur="handleBlur" @focus="handleFocus" />

<!-- After the fix -->
<input
  @input="handleInput"
  @blur="handleBlur"
  @focus="handleFocus"
  @change="handleChange"
/>
```

**Lesson**: Events declared in `defineEmits` without a corresponding handler bound in the template get "swallowed" (neither fired nor passed through). **This rule applies to every component library** — especially components like tables and forms that depend on native event passthrough.

---

### Bug 2: Orphaned References in the `createOverlay` Shared Container

**Symptom**: After a test clears body, the next test creates a Message/Toast, and the container query mysteriously fails.

**Root cause**: Our `createOverlay` caches shared containers in a module-level `Map` (used for stacking messages):

```ts
// createOverlay.ts
const sharedContainers = new Map<string, HTMLDivElement>()

function getSharedContainer(containerClass: string) {
  let container = sharedContainers.get(containerClass)
  if (!container) {
    container = document.createElement("div")
    document.body.appendChild(container)
    sharedContainers.set(containerClass, container)
  }
  return container // ❌ if the container was removed externally, this returns an "orphan node"
}
```

After a test runs `document.body.innerHTML = ''`, the container reference in the Map is **already detached from the DOM** (`isConnected === false`), but the cache wasn't cleared. The next test calls `createOverlay`, gets the orphan node, mounts content into it, and can't find it from `document.body` → failure.

**Solution**: Add an `isConnected` check to the destroy logic, and provide a synchronous cleanup API:

```ts
// ① If the container may have been removed externally, detach it from the DOM synchronously
if (container.childElementCount === 0 || !container.isConnected) {
  container.remove()
  sharedContainers.delete(containerClass)
}

// ② New synchronous cleanup API for tests/app teardown
export function destroyAllOverlays(): void {
  activeInstances.forEach((instance) => {
    instance.element.remove()
    instance.app.unmount()
  })
  sharedContainers.clear()
  activeInstances.clear()
}
```

**Lesson**: For utilities that mount dynamically (lifecycle managed manually via `createApp`), **never rely solely on "the element exists" to decide whether to reuse it** — you must check whether the element is still connected to the document tree (`isConnected`).

---

## Appendix: A Reproducible Minimal Example

If you want to reproduce these 5 traps locally, here's a minimal template (the complete runnable code is in the [Moongate Vue repo `src/__tests__`](https://github.com/yuelinghuashu/moongate-vue/tree/main/src/__tests__)):

```vue
<!-- MyTeleport.vue -->
<template>
  <div>
    <Teleport to="body">
      <div v-if="visible" class="my-overlay">overlay content</div>
    </Teleport>
    <button @click="visible = !visible">toggle</button>
  </div>
</template>

<script setup lang="ts">
import { ref } from "vue"
const visible = ref(true)
</script>
```

```ts
// my-teleport.test.ts — reproduces the "afraid to clear body in afterEach" trap
import { describe, it, expect, afterEach } from "vitest"
import { mount, flushPromises } from "@vue/test-utils"
import MyTeleport from "./MyTeleport.vue"

describe("MyTeleport", () => {
  afterEach(async () => {
    // ❌ Wrong: clearing body first → Vue's patch crashes
    // document.body.innerHTML = ''
    // ✅ Correct: flush first, then clear
    await flushPromises()
    document.body.innerHTML = ""
  })

  it("Teleport content lives in body, not in the wrapper", () => {
    const wrapper = mount(MyTeleport, { attachTo: document.body })
    expect(wrapper.find(".my-overlay").exists()).toBe(false) // the wrapper can't find it
    expect(document.body.querySelector(".my-overlay")).not.toBeNull() // body can
  })
})
```

---

## Conclusion

The biggest takeaway from adding tests to the component library wasn't "we wrote 204 passing assertions" — it was validating a point: **the test environment (jsdom) isn't a browser, but it forces you, in a stricter way, to confront every fuzzy corner of your component implementation and lifecycle management.**

The essence of all these Teleport-in-jsdom traps is the same problem: **DOM produced by manual mounting (Teleport / createApp / dynamic components) has a lifecycle that extends beyond what the component vnode tree manages. That "loss of control" gets amplified in jsdom — like the orphan reference in Bug 2: you think you've cleaned everything up, but lingering caches and detached DOM nodes are still silently at work.** Once you understand that, you have the right mindset for every similar scenario (Portal, Dialog, Notification, ContextMenu).

---

## 🌙 About Moongate Vue

This article is based on real testing practice from [Moongate Vue](https://github.com/yuelinghuashu/moongate-vue). Related resources:

- **Repository**: [github.com/yuelinghuashu/moongate-vue](https://github.com/yuelinghuashu/moongate-vue) — a minimal Vue 3 component library: zero dependencies, CSS-first, 25KB gzip
- **Real-world case**: [moongate.top](https://moongate.top) — a personal blog built with Moongate Vue after migrating from Nuxt UI v4
- **Online docs**: [vue.moongate.top](https://vue.moongate.top) — component API and theming guide
