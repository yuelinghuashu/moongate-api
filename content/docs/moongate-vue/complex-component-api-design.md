---
title: Vue 3 复杂组件开发实战：Select 与 Pagination 的 API 设计与状态管理
description: 以 Select 和 Pagination 为例，深入探讨 Vue 3 复杂组件的 API 设计、数据格式适配、类型回溯、可搜索/多选、ARIA 键盘导航、组合式函数抽离及 SSR 适配，揭示工业级组件背后的设计权衡与实现细节。
date: 2026-05-19
series: moongate-vue
tags:
  - Vue
  - Design System
  - Engineering
---

> 简单组件是单向的数据消费者，复杂组件是数据适配器与状态协调器。以 Select 和 Pagination 为例，看"工业级细节"落在哪里。

## 一、引言

如果说写 Button 组件是在享受写 CSS 变量的"涂料之美"，那么写 Select 和 Pagination 就是在应对原生 HTML 历史包袱的"泥潭摔跤"。简单组件是单向的数据消费者，而复杂组件则是**数据适配器**（兼容多格式）与**状态协调器**（可搜索、多选、键盘导航、SSR 安全）。

本文以 **Select** 和 **Pagination** 为例，展示 v1.5.0 实际实现的复杂组件开发思路。

## 二、Select 下拉选择框：数据适配器

Select 组件需要接收一组选项，并允许用户选择其中一个。真实世界的 API 可能返回对象数组、字符串数组甚至数字数组，因此组件必须具备强大的数据适配能力。

### 2.1 需求分析

- 支持对象数组 `{ label, value }`（默认）
- 支持自定义字段名（`labelKey` / `valueKey`）
- 支持字符串数组 `['选项A', '选项B']`
- 支持数字数组 `[1, 2, 3]`
- 提供占位符（不可选中的默认选项）
- 支持禁用选项（`disabled: true`）
- **可搜索模式**（filterable）：输入过滤 + 下拉面板
- **多选模式**（multiple + filterable）：标签展示
- **必须解决原生 `<select>` 返回字符串的类型陷阱**
- **完整 ARIA 键盘导航**（listbox + option + aria-activedescendant）

### 2.2 双模式架构：原生模式 + 可搜索模式

v1.5.0 的 Select 支持**双模式**：

- **原生模式**（默认）：渲染原生 `<select>`，零 JS 开销
- **可搜索模式**（`filterable=true`）：渲染自定义输入框 + 下拉面板，支持搜索/多选/键盘导航

```vue
<!-- 原生模式：性能最优 -->
<Select v-model="category" :options="categories" />

<!-- 可搜索模式：过滤 + 下拉 -->
<Select v-model="fruit" :options="fruits" filterable />

<!-- 可搜索 + 多选 -->
<Select v-model="tags" :options="tags" filterable multiple />
```

### 2.3 API 设计与类型防腐

为了避免 `any` 造成类型污染，我们采用联合类型收窄：

```typescript
export type SelectValue = string | number
export type SelectOption = string | number | Record<string, any>

interface Props {
  options?: SelectOption[]
  placeholder?: string
  size?: Size
  disabled?: boolean
  error?: boolean
  labelKey?: string // 默认 'label'
  valueKey?: string // 默认 'value'
  filterable?: boolean // 可搜索模式
  emptyText?: string
  maxHeight?: number // 下拉面板最大高度
  multiple?: boolean // 多选（需 filterable）
}
```

**类型回溯**解决原生 select 总是返回字符串的问题：

```typescript
const handleNativeChange = (event: Event) => {
  const target = event.target as HTMLSelectElement
  const rawValue = target.value

  // 在原始 options 中找回原始类型（数字或对象值）
  const originalItem = props.options?.find(
    (item) => String(getValue(item)) === rawValue,
  )
  const finalValue =
    originalItem !== undefined ? getValue(originalItem) : rawValue

  modelValue.value = finalValue
  emit("change", finalValue)
}
```

这个逻辑保证 `v-model` 绑定的数字值不会意外变成字符串。

### 2.4 属性透传拆分

这是可搜索模式的关键细节——**哪些属性透传到原生 input，哪些保留在外层 wrapper**：

```typescript
const attrs = useAttrs()

/** 透传到原生表单元素的 form/aria 属性 */
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

/** 保留在外层 wrapper 的其余属性（class/style/事件等） */
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

为什么必须拆分？如果 `aria-label` 留在外层 wrapper 而未透传到实际 `<input>`，屏幕阅读器会无法识别输入框的可访问名称——在 `a11y.test.ts` 的 axe-core 检查中会报 `aria-input-field-name` 违规。

### 2.5 ARIA 键盘导航（WAI-ARIA Combobox 模式）

可搜索模式的键盘导航遵循 WAI-ARIA Combobox 模式：

```vue
<!-- 下拉面板 -->
<div
  v-if="isOpen"
  ref="dropdownRef"
  class="mg-select-dropdown"
  role="listbox"
  :aria-label="listboxAriaLabel"
  :aria-activedescendant="focusedIndex >= 0 ? getOptionId(focusedIndex) : undefined"
>
  <!-- 选项 -->
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

支持的操作：

- `ArrowDown` / `ArrowUp`：移动高亮（`focusedIndex`），并 `scrollIntoView` 保持可视
- `Enter`：选中当前高亮选项
- `Esc`：关闭下拉
- 每个选项有唯一 `id`（基于 `useId()`），供 `aria-activedescendant` 引用

### 2.6 多选模式

多选（`multiple + filterable`）将 `modelValue` 变为数组，选中逻辑的核心分支：

```typescript
// 多选：切换选中（已选则移除，未选则追加）
if (props.multiple) {
  const current = multipleValues.value
  const isAlreadySelected = current.some((v) => String(v) === String(value))
  const next = isAlreadySelected
    ? current.filter((v) => String(v) !== String(value))
    : [...current, value]

  modelValue.value = next
  // 多选保持下拉打开，方便连续多选
  nextTick(() => inputRef.value?.focus())
  return
}

// 单选：选择后关闭
modelValue.value = value
closeDropdown()
```

多选时：

- 已选项渲染为标签（tag），每个标签有 `aria-label="移除 {label}"` 的删除按钮
- 选择后**下拉保持打开**（方便连续多选）
- 输入框只显示搜索文本，已选标签在外部

## 三、Pagination 分页组件：状态同步器

### 3.1 API 设计（v1.5.0 实际）

```typescript
interface Props {
  totalPages: number // 总页数（必传）
  modelValue: number // 当前页码（v-model）
  size?: "sm" | "md" | "lg"
  showQuickJump?: boolean // 首尾页快速跳转按钮（默认 true）
  prevText?: string // 上一页文案（走全局 i18n）
  nextText?: string
  firstText?: string
  lastText?: string
}
```

Pagination 使用 `defineModel<number>` 绑定当前页：

```typescript
const currentPage = defineModel<number>({ required: true })
```

### 3.2 快速跳转 + 页码编辑

```vue
<!-- 显示模式：可点击的数字 → 进入编辑 -->
<span v-else class="mg-pagination-current" @click="startEdit">
  {{ currentPage }}
</span>

<!-- 编辑模式：输入框 -->
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

`commitJump` 的极端边界防御：

```typescript
const commitJump = () => {
  isEditing.value = false
  const newPage = parseInt(String(inputPage.value), 10)

  // 非法输入：放弃并恢复
  if (isNaN(newPage)) {
    inputPage.value = currentPage.value
    return
  }

  goToPage(newPage) // clamp 到 [1, totalPages]
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

### 3.3 全局文案（i18n）

v1.5.0 加入了全局文案系统。Pagination 的所有 aria-label 和按钮文字都走配置链：

```typescript
const texts = useTexts() // 响应式全局文案

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

优先级：**组件 prop > setConfig texts > 语言内置文案**。文案支持 `{current}`/`{total}` 模板占位符。

## 四、组合式函数抽离

复杂组件往往需要抽离共享逻辑。v1.5.0 的核心 composables：

### 4.1 useFormField（Input/Textarea 共享）

处理 `v-model` 更新 + 原生事件透传：

```typescript
export function useFormField(modelValue, emit) {
  const handleInput = (event: Event) => {
    modelValue.value = (event.target as HTMLInputElement).value
    emit("input", event)
  }
  // change/focus/blur 原生事件透传
  return { handleInput, handleChange, handleBlur, handleFocus }
}
```

### 4.2 useFloating（Popover/Tooltip 共享）

自研悬浮层定位引擎：视口翻转 + 边界修正 + ResizeObserver：

```typescript
export function useFloating(options: UseFloatingOptions) {
  // 延迟显示/隐藏
  // 位置计算（按方向定位 + 视口翻转）
  // 滚动/窗口尺寸变化时重新定位
  // ResizeObserver 仅监听悬浮层自身尺寸
  // SSR 安全（isBrowser 守卫）
  return { triggerRef, floatingRef, visible, currentPlacement, floatStyle, show, hide, ... }
}
```

### 4.3 useOverlayBehavior（Modal/Drawer 共享，位于 useScrollLock.ts）

滚动锁定 + ESC 关闭 + 焦点陷阱：

```typescript
// composables/useScrollLock.ts
export function useOverlayBehavior(isOpen, overlayRef, onClose, options) {
  // body 滚动锁定（模块级 lockCount 计数器，多实例安全）
  // ESC 键关闭
  // Tab 焦点陷阱
}
```

该函数与滚动锁定逻辑（`lockBodyScroll`/`unlockBodyScroll`）一同定义在 **`useScrollLock.ts`** 中——滚动锁、ESC 关闭、焦点陷阱三者在语义上同属"浮层行为"这一关注点，因此放在同一个文件内。

**模块级锁计数**解决多 Modal/Drawer 同时打开的滚动锁冲突——只有最后一个关闭时才恢复 body 滚动。

## 五、SSR 适配：useId 与 isBrowser

v1.5.0 的 SSR 适配比初版更完善，核心是两条：

### 5.1 useId() 保证 hydration 安全

所有需要 `id` 的组件（Modal/Drawer/Select/Tabs）使用 Vue 3 的 `useId()`：

```typescript
const selectBaseId = useId()
const getOptionId = (index: number): string => `${selectBaseId}-option-${index}`
```

`useId()` 在服务端与客户端生成一致的 ID，避免 hydration mismatch。

### 5.2 isBrowser 守卫

所有 DOM 操作添加浏览器环境守卫：

```typescript
const isBrowser =
  typeof window !== "undefined" && typeof document !== "undefined"

// 在 watch/onMounted/顶层代码中：
if (!isBrowser) return
```

配合 `useScrollLock.ts` 的模块级计数器，`lockBodyScroll()` 在非浏览器环境下直接跳过 DOM 操作。

### 5.3 createOverlay：命令式组件 SSR 安全

```typescript
// composables/createOverlay.ts
export function createOverlay(component, props, containerClass) {
  if (!isBrowser) return null // SSR 返回 null
  // ...
}
```

## 六、数据与状态流向

```text
外部数据 ──► 数据适配器 ──► 内部状态
(options)    (getLabel/     (selected/
              getValue)      searchText)
   ▲                            │
   │                            ▼
全局环境 ◄── 状态协调器 ◄── 用户交互
(i18n/SSR)   (watch/event)   (click/keyboard)
```

- **左列**：外部输入（数据 + 用户操作）
- **中间**：适配与协调（把外部世界翻译成内部状态）
- **右列**：内部状态（组件自管理）
- **底部**：全局环境（i18n / SSR）反向约束组件行为

## 七、测试策略

v1.5.0 的 Select 有 **40+ 测试**，Pagination 有 **14 测试**，覆盖：

| 关注点        | 测试内容                                                    |
| ------------- | ----------------------------------------------------------- |
| **类型回溯**  | 原生模式数字数组 `[10,20,30]` 选中后 modelValue 仍为 number |
| **ARIA 导航** | `aria-activedescendant` 指向高亮选项、每个 option 有唯一 id |
| **键盘操作**  | ArrowDown/Up 高亮、Enter 选中、Esc 关闭、边界不越界         |
| **多选**      | 标签渲染、切换选中、tag 删除、Enter 连续多选                |
| **边界值**    | 搜索空结果、外部 modelValue 变化、blur 时下拉保持打开       |
| **无障碍**    | axe-core 对 Select（原生+可搜索）无违规                     |

以及 **SSR 检查**：`renderToString` 确认组件在服务端不崩溃且浮层默认隐藏。

## 八、总结

| 关注点         | 简单组件（Button） | 复杂组件（Select / Pagination）                           |
| -------------- | ------------------ | --------------------------------------------------------- |
| **Props 数量** | 较少（11）         | 较多（10-15）                                             |
| **数据格式**   | 固定（字符串）     | 灵活（支持多种数组，可配置字段，类型防腐）                |
| **状态管理**   | 无内部状态         | 可搜索文本、多选数组、编辑状态、下拉显隐                  |
| **无障碍**     | 原生语义           | WAI-ARIA Combobox 模式（listbox/option/activedescendant） |
| **逻辑复用**   | 不需要             | 组合式函数（useFormField/useFloating/useOverlayBehavior） |
| **SSR 适配**   | 自动               | useId hydration 安全 + isBrowser 守卫                     |
| **i18n**       | 少数文案           | 全局配置链（prop > setConfig > 内置）                     |
| **测试策略**   | 快照、事件触发     | 状态组合、边界值、键盘模拟、类型回溯、axe-core            |

一个优秀的复杂组件，对内要像吸尘器一样容纳各种奇葩的后端数据格式（通过 Key 映射和类型回溯），对外要像绅士一样克制地与全局环境（i18n、SSR、键盘）发生耦合。**高内聚、低耦合**，在这两类组件身上体现得淋漓尽致。

---

## 🌙 关于 Moongate Vue

本文来自 Moongate Vue 组件库设计实战系列（共 4 篇），所有内容均基于真实项目实践：

- **项目仓库**：[github.com/yuelinghuashu/moongate-vue](https://github.com/yuelinghuashu/moongate-vue) — 极简 Vue 3 组件库，零依赖、CSS 优先、25KB gzip
- **真实案例**：[moongate.top](https://moongate.top) — 个人博客，从 Nuxt UI v4 迁移至 Moongate Vue 构建
- **在线文档**：[vue.moongate.top](https://vue.moongate.top) — 组件 API 与主题定制指南
