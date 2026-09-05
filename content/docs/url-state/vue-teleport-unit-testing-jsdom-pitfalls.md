---
title: Vue 3 Teleport 组件单元测试指南：5 个 jsdom 陷阱与顺手抓到的 2 个 Bug
description: 为我们的组件库 Moongate Vue 编写 204 个单元测试时，Teleport 组件在 jsdom 环境中踩了 5 个坑，还顺手揪出了 2 个隐藏 bug。本文复盘完整过程，附可复现的最小示例。
date: 2026-08-05
series:
level: P4
tags:
  - Vue
  - Engineering
  - TypeScript
---

> 给 Teleport 组件写测试，5 个 jsdom 陷阱 + 顺手抓到的 2 个隐藏 Bug——每个坑都附可复现的最小示例。

## 背景

Moongate Vue 是一个包含 25 个组件的 Vue 3 组件库，其中 `Modal`（模态框）、`Drawer`（抽屉）、`Message`（消息）、`Toast`（通知）等都使用了 `<Teleport to="body">` 将内容渲染到 `document.body`。

为了让测试环境尽量接近真实浏览器，我们选了 Vitest + jsdom 作为测试基础设施。原以为只是写几个断言的事，结果在 Teleport 组件上反复栽跟头——**24 个测试失败，其中一大半都指向同一个诡异报错**：

```text
TypeError: Cannot read properties of null (reading 'insertBefore')
```

排查到最后发现，这不是巧合，而是 **Teleport 机制 + jsdom 环境 + Vue 异步更新** 三者在特定时序下的必然结果。下面按"坑 → 表现 → 根因 → 解法"拆解。

---

## 第一部分：Teleport 测试的 5 个陷阱

### 陷阱 1：Teleport 内容在 body 顶层，wrapper 查不到

**表现**：

`wrapper.find('.mg-modal-overlay')` 返回空，即使组件已经通过 `attachTo: document.body` 挂载。

**根因**：Teleport 的目标元素是 `body`，其渲染内容作为 `body` 的**直接子节点**，**不在 `wrapper.element` 子树内**。即使能通过 `wrapper.vm` 访问到组件实例，Teleport 渲染的 `v-if` 内容也在 wrapper 管理的 DOM 树之外。

```ts
// ❌ 错误：wrapper 找不到 Teleport 内容
const wrapper = mount(Modal, { props: { modelValue: true } })
expect(wrapper.find(".mg-modal-overlay").exists()).toBe(true)

// ✅ 正确：从 body 顶层查询
const wrapper = mount(Modal, {
  props: { modelValue: true },
  attachTo: document.body, // ① 确保挂载到 body
})
expect(document.body.querySelector(".mg-modal-overlay")).not.toBeNull()
```

**解法**：

1. `attachTo: document.body` 确保组件渲染在 body 中
2. 断言 Teleport 内容永远用 `document.body.querySelector`，而不是 `wrapper.find`

---

### 陷阱 2：依赖自动卸载，触发 `insertBefore on null`

**表现**：测试断言全通过，但 Vitest 结束后抛出一个异步错误：

```bash
TypeError: Cannot read properties of null (reading 'insertBefore')
  at insert (runtime-dom.cjs.js:31)
  at processCommentNode ...
```

**根因**：Vue 的响应式更新是**异步批量 patch** 的。当测试结束时 wrapper 被自动卸载（或 `document.body.innerHTML = ''` 被清空），但 Vue 内部 scheduler 队列里还挂着上一轮渲染的 DOM patch 任务。这个 patch 试图操作已经脱离 DOM 的旧节点 → 崩溃。

Teleport 组件尤其容易触发，因为它的插入点（body）是测试环境里**最容易被清空**的目标。

**解法**：测试结束时显式卸载 wrapper，让 Teleport 有完整机会摘除自己：

```ts
import { mount } from "@vue/test-utils"

// 跟踪所有 wrapper，统一显式卸载
const wrappers: ReturnType<typeof mount>[] = []
const mountDrawer = (options = {}) => {
  const wrapper = mount(Drawer, { attachTo: document.body, ...options })
  wrappers.push(wrapper)
  return wrapper
}

afterEach(async () => {
  while (wrappers.length > 0) {
    const wrapper = wrappers.pop()!
    await wrapper.unmount() // 显式卸载，而非依赖自动清理
  }
})
```

---

### 陷阱 3：测试间 DOM 污染

**表现**：第一个测试用例渲染了一个 Modal，第二个用例查询 `.mg-modal-overlay` 莫名找到了**上一轮残留**的元素；或者反过来，第二个用例找不到预期元素。

**根因**：Teleport 把内容加到 `document.body`，但测试框架的自动清理不一定销毁它们。尤其当组件内部持有**模块级缓存**（比如我们的 `createOverlay` 共享容器 Map）时，残留引用会让 DOM 越积越多。

```ts
// ❌ 无法清理 Teleport 残留
afterEach(() => {
  document.body.innerHTML = "" // 直接把 body 清空
})
```

直接清空 body 是**有问题的**，因为 Vue 内部仍引用着这些节点，下一个 tick 的 patch 会对已移除节点操作而爆炸（这正是陷阱 2 的错误来源）。

**解法**：先 flush 再清空：

```ts
afterEach(async () => {
  await flushPromises() // ① 等待 Vue 异步作业（Teleport 移除、nextTick patch）完成
  document.body.innerHTML = "" // ② 再清空
})
```

> ⚠️ **注意**：如果测试中使用了 `vi.useFakeTimers()`，`flushPromises()` 只能清空微任务队列，**清不掉宏任务**（如 `setTimeout` 触发的 DOM 操作）。此时应还原真实定时器，再清空 body：
>
> ```ts
> afterEach(async () => {
>   await flushPromises()
>   // 若用了假定时器，直接还原真实定时器（会自动清空假定时器队列），比跑完所有宏任务更安全
>   if (vi.isFakeTimers()) {
>     vi.useRealTimers()
>   }
>   document.body.innerHTML = ""
> })
> ```
>
> ⚠️ **避免 `vi.runAllTimersAsync()` 的陷阱**：它会把所有宏任务（包括 `setInterval`、`requestAnimationFrame` 这类**永久性定时器**）循环跑完。如果被测组件内部有无限轮询或动画循环，这个调用会导致测试**卡死/超时**。**还原真实定时器是更稳妥的清理方式**。

---

### 陷阱 4：清理顺序错误，先清空 body 导致 Vue 找不到父节点

**表现**：诡异的是，把 `document.body.innerHTML = ''` 放在 `afterEach` 的开头反而报错，放在结尾就正常。

**根因**：Vue 的调度器不知道测试环境的存在。如果在 Vue 还没完成 last tick 的 DOM patch 时清空 body，等它去 patch 时父节点已经是 null。

**解法**：正确的清理顺序必须是：`卸载所有 wrapper / destroyAllOverlays → flushPromises（若用了 fake timers 则 useRealTimers 还原）→ 清空 body → restoreAllMocks`。

**这是最容易忽略的一条**。很多人（包括我）第一反应是"清空 body 嘛，什么时候不行"，结果 Vue 用崩溃告诉你不可以。

---

### 陷阱 5：`v-model` 关闭后立刻断言事件，但 DOM patch 还没发生

**表现**：点击关闭按钮后，`wrapper.emitted('update:modelValue')` 有值，但紧接着查询 `document.body.querySelector('.mg-modal')` 仍能查到（元素还在）。

**根因**：

`emit('update:modelValue', false)` 是同步的，但 Teleport 移除 DOM 是**异步 patch**。事件已派发、DOM 还没更新。

**解法**：

1. **先操作 UI 再断言事件**：`wrapper.emitted(...)` 是同步的，点击后立即可用
2. **DOM 断言必须等待异步 patch**：`await flushPromises()` 或 `await wrapper.vm.$nextTick()`
3. 先断言事件（同步派发），再断言 DOM（异步更新），在时间线上分离二者：

```ts
// ✅ 推荐写法：先触发 UI，事件同步断言；再等 DOM 异步更新
closeBtn.click()
expect(wrapper.emitted("update:modelValue")).toBeTruthy() // 事件是同步派发的
await flushPromises() // 等待 DOM 移除
expect(document.body.querySelector(".mg-modal")).toBeNull()
```

---

### 小结：Teleport 测试的黄金法则

| 法则     | 一句话                                                                                                                                 |
| -------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| **挂载** | 一律 `attachTo: document.body`                                                                                                         |
| **断言** | Teleport 内容用 `document.body.querySelector`                                                                                          |
| **卸载** | 显式 `await wrapper.unmount()`，不依赖自动清理                                                                                         |
| **清理** | 卸载 → `flushPromises()`（用了 fake timers 则 `useRealTimers()` 还原，勿用 `runAllTimersAsync()` 以免死循环）→ 清空 body，顺序不可颠倒 |
| **时序** | 事件同步，DOM 异步，断言前先 `flushPromises()`                                                                                         |

---

## 第二部分：测试驱动顺手抓到的 2 个真实 Bug

写测试的过程中，失败的断言意外暴露了组件库本身的两个隐藏缺陷。这才是测试真正的价值——**它不只是验证代码，而是替用户提前踩坑**。

### Bug 1：Input 组件的 `change` 事件完全丢失

**表现**：组件声明了 `change` 事件，但无论如何测试都收不到：

```ts
// Input.vue 中声明了
const emit = defineEmits<{
  /** 值变化时触发（原生事件透传） */
  change: [event: Event]
}>()

// 但测试始终收不到
wrapper.trigger("change")
expect(wrapper.emitted("change")).toHaveLength(1) // ❌ 失败
```

**根因**：模板里只绑定了 `@input`、`@blur`、`@focus`，**漏掉了 `@change`**。要理解为什么事件会"彻底消失"，需要先弄清 Vue 3 的事件透传机制：

> Vue 3 中，组件模板上监听的**未在 `emits` 中声明**的事件，会被当作原生事件透传给根元素（进入 `$attrs`）；**一旦在 `emits` 中声明**，Vue 就认为该事件已由组件内部显式处理，不再透传。

因此，`change` 被 `defineEmits` 声明后，Vue 不会再把它透传给根 `<input>`。此时如果模板中又没有绑定 `@change` 处理器，这个事件就**凭空消失**了——既不会触发组件事件，也不会透传到原生元素。

```vue
<!-- 修复前：漏了 @change -->
<input @input="handleInput" @blur="handleBlur" @focus="handleFocus" />

<!-- 修复后 -->
<input
  @input="handleInput"
  @blur="handleBlur"
  @focus="handleFocus"
  @change="handleChange"
/>
```

**教训**：

`defineEmits` 声明的事件如果没在模板绑定对应处理器，会被"吞掉"（既不触发、也不透传）。**这条例外适用于所有组件库**——尤其是表格、表单这类依赖原生事件透传的组件。

---

### Bug 2：`createOverlay` 共享容器的孤儿引用

**表现**：测试清空 body 后，下一个用例创建 Message/Toast，查询容器时莫名失败。

**根因**：我们的 `createOverlay` 用模块级 `Map` 缓存共享容器（用于消息堆叠）：

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
  return container // ❌ 如果容器已被外部 remove，这里返回的是"孤儿节点"
}
```

当测试执行 `document.body.innerHTML = ''` 后，Map 里的容器引用**已脱离 DOM**（`isConnected === false`），但缓存没清空。下一个用例调用 `createOverlay` 时拿到孤儿节点，内容挂进去后从 `document.body` 查不到 → 失败。

**解法**：销毁逻辑增加 `isConnected` 检测，并提供同步清理 API：

```ts
// ① 容器可能已被外部移除时，同步从 DOM 摘除
if (container.childElementCount === 0 || !container.isConnected) {
  container.remove()
  sharedContainers.delete(containerClass)
}

// ② 新增同步清理 API，供测试/应用卸载使用
export function destroyAllOverlays(): void {
  activeInstances.forEach((instance) => {
    instance.element.remove()
    instance.app.unmount()
  })
  sharedContainers.clear()
  activeInstances.clear()
}
```

**教训**：动态挂载（`createApp` 手动管理生命周期）的工具，**绝不能只依赖"元素存在"来判断是否复用**——必须检测元素是否仍连接在文档树中（`isConnected`）。

---

## 附：可复现的最小示例

如果你想在本地复现这 5 个陷阱，下面是最小化的模板（完整可跑代码见 [Moongate Vue 仓库 `src/__tests__`](https://github.com/yuelinghuashu/moongate-vue/tree/main/src/__tests__)）：

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
// my-teleport.test.ts —— 复现"不敢在 afterEach 清空 body"之坑
import { describe, it, expect, afterEach } from "vitest"
import { mount, flushPromises } from "@vue/test-utils"
import MyTeleport from "./MyTeleport.vue"

describe("MyTeleport", () => {
  afterEach(async () => {
    // ❌ 错误：先清空 body → Vue patch 崩溃
    // document.body.innerHTML = ''
    // ✅ 正确：先 flush 再清空
    await flushPromises()
    document.body.innerHTML = ""
  })

  it("Teleport 内容在 body 而非 wrapper", () => {
    const wrapper = mount(MyTeleport, { attachTo: document.body })
    expect(wrapper.find(".my-overlay").exists()).toBe(false) // wrapper 查不到
    expect(document.body.querySelector(".my-overlay")).not.toBeNull() // body 才能查到
  })
})
```

---

## 结语

这次为组件库补测试的经历，最大收获不是"写出了 204 个通过的断言"，而是验证了一个观点：**测试环境（jsdom）不是浏览器，但它会以更严格的方式逼你直面组件实现和生命周期管理的每一处模糊地带。**

Teleport 在 jsdom 中的这些坑，本质都是同一个问题：**手动挂载（Teleport / createApp / 动态组件）产生的 DOM，其生命周期超出了组件 vnode 树的管理范围。这种"失控"在 jsdom 中会被放大——就像 Bug 2 中的孤儿引用，你以为清理干净了，实则残留的缓存和游离的 DOM 节点还在暗中作祟**。理解了这一点，就掌握了应对所有类似场景（Portal、Dialog、Notification、ContextMenu）的思路。

---

## 🌙 关于 Moongate Vue

本文基于 [Moongate Vue](https://github.com/yuelinghuashu/moongate-vue) 的真实测试实践，相关资源：

- **项目仓库**：[github.com/yuelinghuashu/moongate-vue](https://github.com/yuelinghuashu/moongate-vue) — 极简 Vue 3 组件库，零依赖、CSS 优先、25KB gzip
- **真实案例**：[moongate.top](https://moongate.top) — 个人博客，从 Nuxt UI v4 迁移至 Moongate Vue 构建
- **在线文档**：[vue.moongate.top](https://vue.moongate.top) — 组件 API 与主题定制指南
