---
title: 测试全绿，组件却坏了——jsdom 看不到的视觉与无障碍盲区
description: 单测 497 个全绿，Tooltip 悬停却看不见、Select 的屏幕阅读器读不到当前选项。复盘两次排查：jsdom 不渲染视觉、也测不到无障碍规范，这些盲区要靠 e2e 的计算样式验证、像素快照与规范核对来补。附可复现的最小示例。
date: 2026-09-07
series:
tags:
  - Vue
  - Engineering
---

> 单测全绿 ≠ 组件正确。jsdom 看不到两件事：**视觉正确性**（`opacity`）和**无障碍规范**（`aria-*`）。本篇是《[Vue 3 Teleport 组件单元测试指南](../url-state/vue-teleport-unit-testing-jsdom-pitfalls)》的续篇。

## 背景

在《Vue 3 Teleport 组件单元测试指南》里，我们讲透了**怎么在 jsdom 里写对测试**——挂载、卸载、清理、时序。那篇文章的隐含前提是：「只要测试写对了、全绿，组件就正确」。

这次我们被现实打脸了两次。

组件库单测一路涨到 **497 个、全绿**，覆盖率 95%/86%。可偏偏在真实浏览器里：

- **Tooltip 悬停，提示框根本不显示**；
- **Select 的下拉，屏幕阅读器读不到当前高亮的选项**。

更讽刺的是，这两个问题**单测都「通过」**——它们不是测试写错了，而是**jsdom 根本没有能力看到这两类错误**：它不渲染像素（视觉），也不实现 ARIA 语义（规范）。这两类盲区，必须靠真浏览器/规范核对来补。

---

## 盲区一：视觉正确性——`opacity: 0` 的隐形组件

### 表现

文档站（VitePress）里，鼠标悬停到 Tooltip 触发区，什么都不显示。

### 排查

模板里浮层确实会挂载：

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

`v-if="visible"` 成立时元素在 DOM 里，`role="tooltip"` 也在。可就是看不见。

看 CSS 才明白：

```css
.mg-tooltip {
  /* ... */
  opacity: 0; /* 默认透明 */
  transition: opacity 150ms ease;
}

.mg-tooltip-visible {
  opacity: 1; /* 需要这个类才可见 */
}
```

**浮层默认 `opacity: 0`，要等 `.mg-tooltip-visible` 才可见。但模板里从头到尾没绑定这个类** —— 于是元素永远挂载、永远透明。

### 为什么单测抓不到？

这是关键。看当时单测的断言：

```ts
// Tooltip.test.ts（修复前）
it("鼠标移入后显示 tooltip", async () => {
  await trigger("mouseenter")
  const tooltip = document.body.querySelector(".mg-tooltip")
  expect(tooltip).not.toBeNull() // ✅ 通过
})
```

断言的是「**`.mg-tooltip` 元素存在于 DOM**」。元素确实存在——只是透明的。**jsdom 不渲染 CSS**（没有排版引擎、不计算 `getComputedStyle` 的视觉结果），所以它既不会告诉你 `opacity` 是多少，也不会因为元素透明而报错。

> **jsdom 的视觉盲区**：jsdom 是「DOM 模拟器」不是「浏览器」。它管 DOM 结构、事件、属性，但**不管像素**。`opacity`、`visibility`、`z-index`、`position` 这类「视觉正确性」，jsdom 一概测不到。

### 修复

绑定 visible 类，让 `opacity` 在显示时变为 1：

```vue
<div
  v-if="visible"
  class="mg-tooltip"
  :class="[
    `mg-tooltip-${placement}`,
    { 'mg-tooltip-visible': visible }, // 补上：显示时加可见类
  ]"
  role="tooltip"
>
```

### 补充断言：逻辑状态与视觉验证的边界

单测无法渲染视觉，它能断言的是 `v-if` 的**结果**——浮层是否挂载（这是可观察的行为）：

```ts
it("悬停后浮层挂载", async () => {
  await trigger("mouseenter")
  // 断言可观察行为：浮层确实出现在 DOM 中
  expect(document.body.querySelector(".mg-tooltip")).not.toBeNull()
})
```

> ⚠️ **不要用 `classList.contains('mg-tooltip-visible')` 来断言可见性**：那是把**实现细节**当测试。CSS 类名属于样式出口，不是行为——将来把 `opacity` 改成 `transform: scale(0)`、或改用 `<Transition>` 换类名，这个断言会无辜挂掉（这也是测试界常说的「测行为而非实现」）。如果要一个可断言的「状态出口」，现代组件库更倾向 `data-state="open"` 这类属性，而不是样式类名。

### 视觉验证的「三层模型」：不是只有 e2e，也不是一个 e2e 全搞定

说「视觉只能靠 e2e」太笼统。视觉正确性实际分三个层级，工具和盲区各不相同：

| 层级             | 验证什么                                                             | 工具                                                | 能防 / 不能防                                                                                         |
| ---------------- | -------------------------------------------------------------------- | --------------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| **① 逻辑层**     | 状态、DOM 结构                                                       | 单测（jsdom）                                       | 防「没挂载」；测不了像素                                                                              |
| **② 计算样式层** | `opacity`/`visibility`/`z-index`/`position` / 是否被 `overflow` 裁切 | e2e 断 `getComputedStyle` / `getBoundingClientRect` | 防「属性值错」「元素超视口」；**防不了「被别的元素盖住」**（`z-index` 比它高时 `opacity:1` 也看不见） |
| **③ 像素快照层** | 最终渲染「看起来对不对」                                             | 视觉回归（`toHaveScreenshot`、Percy、Chromatic）    | 能防一切（含遮挡、错位）；但脆弱（依赖平台字体/环境），成本高                                         |

- 这次 Tooltip 的 bug 属于 **②**：`getComputedStyle(tooltip).opacity === '1'` 就能抓住。
- 但同一次排查里 Select 的「被 overflow 容器裁剪」问题，**② 也抓不全**——`getComputedStyle` 不会告诉你「元素被裁到看不见」，得量 `getBoundingClientRect()` 是否落在容器可视区，或直接交给 **③**。
- **③ 是终极兜底，但它可以「驯服」而不是一律避用**：
  - **优先组件级快照，而不是页面级 E2E 快照**：用 Chromatic/Storyshots 这类工具**只渲染单个组件**做截图对比——输入可控（固定 props/尺寸/主题），噪声远小于整页截图，是国外一线团队（Spotify、微软等）更推崇的做法；
  - **降低脆弱性**：截图对比时配置**忽略字体装载与抗锯齿差异**（diff 只看结构性的布局/颜色偏差），并把 **Diff threshold（像素差异阈值）** 调到组件可接受的容错值——把「平台字体渲染不同」这类噪音排除在阈值之外；
  - 这样「按关键路径取舍」才真正可落地：不是「能不碰就不碰」，而是「关键场景 + 组件级快照 + 合理阈值」。

> 结论修正：不是「e2e 是唯一解」，而是「**② 覆盖常见视觉属性错误，③ 覆盖像素级正确，按需取舍**」。jsdom 连 ② 都做不了，这是它必须靠 e2e 的部分。

---

## 盲区二：无障碍规范——`aria-activedescendant` 绑错元素

### 表现

给 Select 补 e2e 键盘测试时，断言 `aria-activedescendant` 指向当前高亮选项，**一直失败**。

### 排查：DOM 里到底绑在哪？

用 Playwright 在真浏览器里直接读 DOM，发现：

```json
// 输入框（input）：
{ "inputAriaAttrs": [] }

// 下拉容器（listbox）：
{ "listboxActDesc": "v-0-option-0" }
```

`aria-activedescendant` 被绑在了 **listbox 容器**上，而不是**获得焦点的 input** 上。

### 为什么这是错的？——焦点代理（Focus Delegation）

先看规范结论：WAI-ARIA 的 combobox/listbox 组合中，`aria-activedescendant` **必须放在获得焦点的元素**（这里是可输入的 `<input>`）。

但要理解「为什么必须放在 input」，得懂底层机制——**焦点代理（Focus Delegation）**——本质就两点：

1. **DOM 焦点只能停在一个元素上**。下拉选择时，焦点宿主是可输入的 `<input>`（combobox），它才是 `document.activeElement`；listbox 容器只是「受体」——它定义 option 集合，但没有焦点。
2. `aria-activedescendant` 是**虚拟焦点代理**：DOM 焦点不动，屏幕阅读器的**虚拟光标**跟随它指向的 option——所以属性必须挂在焦点宿主上，容器挂了等于没挂。

对照这个机制，问题就清楚了：

- 绑在 **input** 上 → 屏幕阅读器以 input 为宿主，虚拟光标跟随 option，朗读「当前选项是 xxx」；
- 绑在 **listbox** 上 → 屏幕阅读器在焦点处找不到「指示当前活动项」的线索，**根本读不到当前选项**。

这是典型的「属性存在但位置错误」——**axe 在默认规则集下通常不会报错**（`aria-activedescendant` 的宿主校验属于实验性/可按规则集启用的组合型规则，默认不强制），只有人工对照规范，或用「断言焦点元素（而非容器）上有该属性」的针对性测试才能发现。

### 为什么单测抓不到？

单测断言的是「listbox 上有这个属性」：

```ts
// Select.test.ts（修复前）
expect(dropdown.attributes("aria-activedescendant")).toBe(
  option0.attributes("id"),
)
```

**断言本身是「错」的**——它验证了错误的位置。jsdom 不验证规范，你断言什么它就给什么。所以单测「全绿」，恰恰是因为**测试把错误实现当成了预期**。

> **jsdom 的无障碍盲区**：jsdom 不实现 ARIA 语义、不做屏幕阅读器。`aria-*` 属性的「对错」取决于**是否符合规范**，而规范正确性 jsdom 无法判断——它只反射你写的绑定。

### 修复

把 `aria-activedescendant` 从 listbox 移到 input：

```vue
<!-- input 上：规范位置 -->
<input
  :aria-activedescendant="
    focusedIndex >= 0 ? getOptionId(focusedIndex) : undefined
  "
  @keydown.down.prevent="moveFocus(1)"
  @keydown.up.prevent="moveFocus(-1)"
  ...
/>
```

并同步修正单测断言位置：

```ts
// 修复后：断言在 input 上
expect(input.attributes("aria-activedescendant")).toBe(option0.attributes("id"))
```

### 同一个 Select 里的另外两个「规范盲区」

排查 activedescendant 时，e2e 又暴露了两个单测测不到的问题：

**① 缺 Home/End 键盘处理**（WAI-ARIA listbox 键盘约定要求支持）：

```vue
@keydown.home.prevent="moveFocusTo(0)"
@keydown.end.prevent="moveFocusTo(filteredOptions.length - 1)"
```

**② `Tab` 无法关闭下拉**——`mousedownInside` 残留 `true`，导致 `blur` 被误判为「点击了选项」而不关闭：

```ts
const openDropdown = () => {
  // ...
  mousedownInside.value = false // 修复：打开时重置，避免残留 true 吞掉 blur
}
```

这两个都是**事件时序/规范**问题，jsdom 单测同样无能为力——只有真浏览器触发真实键盘/焦点流才暴露。

---

## 为什么必须靠 e2e：测试的「分层」

| 层                 | 验证什么                                              | 工具                  | 盲区             |
| ------------------ | ----------------------------------------------------- | --------------------- | ---------------- |
| **逻辑层**         | 状态、事件、props、DOM 结构                           | Vitest + jsdom        | 视觉、规范       |
| **表现层（属性）** | `opacity`/`z-index`/`position`、键盘流、ARIA 属性位置 | Playwright + 真浏览器 | 像素级遮挡/重叠  |
| **表现层（像素）** | 最终渲染「看起来对不对」                              | 视觉回归（截图对比）  | 平台差异（脆弱） |

单测（逻辑层）快而密，但它**默认你「会写对视觉类绑定的位置」**；一旦你写错（漏绑定、绑错元素），它不会提醒你。

e2e（表现层）慢而少，但它验证的是**真实用户看到、读到**的东西。其中「属性验证」覆盖常见视觉问题（`opacity`/`z-index`），「像素快照」覆盖终极正确性（含遮挡）——后者昂贵，按关键路径取舍。

**正确的分工**：

1. 单测覆盖逻辑与数据流（快、多、兜底回归）；
2. e2e 覆盖「看得见 + 读得出」——悬停出提示、焦点在选项、屏幕阅读器语义正确；
3. 关键路径再加像素快照，防「属性全对但画面错」。

---

## 小结：给测试补上「看不见」的维度

| 盲区            | 例子                                               | 为什么 jsdom 测不到           | 真正的解法                                          |
| --------------- | -------------------------------------------------- | ----------------------------- | --------------------------------------------------- |
| **视觉属性**    | Tooltip `opacity: 0`、Select 被 `overflow` 裁切    | jsdom 不渲染 CSS/像素         | e2e 断 `getComputedStyle` / `getBoundingClientRect` |
| **像素级正确**  | 浮层被 `z-index` 盖住、布局错位                    | jsdom 无渲染                  | 视觉回归（截图对比，按关键路径取舍）                |
| **无障碍规范**  | `aria-activedescendant` 绑错元素（焦点代理位置错） | jsdom 不实现 ARIA 语义        | e2e 断言**焦点元素**上的属性 + 规范核对             |
| **键盘/焦点流** | Select 缺 Home/End、Tab 关不掉                     | jsdom 不模拟真实键盘/焦点时序 | e2e 真键盘事件                                      |

单测帮你确认「**代码按我的意图跑**」，e2e 帮你确认「**用户按我的意图看到**」。二者缺一不可——尤其是组件库这种「一个组件被千百人复用」的场景，视觉与无障碍的盲区会被无限放大。

---

## 附：可复现的最小示例

### Tooltip 视觉盲区（jsdom 测不到 opacity）

```vue
<!-- MyTooltip.vue -->
<template>
  <div
    class="tooltip-trigger"
    @mouseenter="show = true"
    @mouseleave="show = false"
  >
    悬停我
    <Teleport to="body">
      <div v-if="show" class="my-tooltip">提示内容</div>
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
} /* ❌ 漏了 .visible 类，元素透明 */
</style>
```

```ts
// my-tooltip.test.ts —— jsdom 里全绿，浏览器里看不见
import { mount } from "@vue/test-utils"
import MyTooltip from "./MyTooltip.vue"

it("悬停显示 tooltip", async () => {
  const wrapper = mount(MyTooltip, { attachTo: document.body })
  await wrapper.find(".tooltip-trigger").trigger("mouseenter")
  // ✅ 元素存在 —— jsdom 断言通过
  expect(document.body.querySelector(".my-tooltip")).not.toBeNull()
  // ❌ 但 opacity 是 0，用户根本看不见
})
```

### aria-activedescendant 绑错元素（规范盲区）

```vue
<!-- 错误：绑在 listbox 容器 -->
<div role="listbox" :aria-activedescendant="activeId">
  <div role="option" :id="option0Id">A</div>

<!-- 正确：绑在获得焦点的 input -->
<input
  :aria-activedescendant="activeId"
  role="combobox"
  aria-expanded="true"
  aria-controls="listbox-id"
/>
```

---

## 结语

这次排查最大的收获不是「修好了两个 bug」，而是重新认识了测试的边界：**jsdom 是极好的逻辑验证器，却是糟糕的「视觉/规范」验证器**。

一个组件库若只有单测，就像给一辆车做了全套电路检测，却没上路试驾——**灯亮没亮、方向盘能不能转向，单测测不出来**。把 e2e 补上，才算是真正「能上路」的组件库。

而这两个 bug 也提醒我们：**测试全绿从来不是终点，只是起点**。真正要问的是——「全绿」的测试，到底验证了什么，又漏掉了什么。
