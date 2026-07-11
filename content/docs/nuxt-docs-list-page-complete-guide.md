---
title: 从零到一：构建一个功能完备的文档列表页
description: 手把手教你用 Nuxt 4 构建一个支持 URL 状态同步、多维度筛选、移动端无限滚动、键盘翻页的文档列表页。包含手写状态管理、SSR 水合问题排查、组件拆分陷阱、标签多选（桌面端 Ctrl/移动端开关）等完整实现，附可复用代码。
date: 2026-03-21
permalink: 760e47b3-05bc-4ad6-9d43-ad95426b8127
level: P3
series: url-state
tags: 
  - Nuxt
  - Vue
  - State Management
  - Hydration
---

> 本文完整记录了我在 Nuxt 4 中构建一个功能完备的文档列表页的全过程，涵盖 URL 状态同步、SSR 水合问题、移动端适配、标签多选、键盘翻页等 20+ 细节。包含可直接复用的代码片段和踩坑总结，适合正在构建类似页面的开发者。

---

## 📚 系列导航

本系列共三篇，覆盖 Nuxt 中 URL 与状态双向同步的全流程：

1. [Nuxt 中 URL 与状态双向绑定的终极指南（原理篇）](./nuxt-url-state-guide)
   —— 讲解 URL 与状态双向同步的原理与手写方案。

2. [手写一个更适合 Nuxt 的 useRouteQuery（封装篇）](./nuxt-use-route-query-composables)
   —— 将重复逻辑封装成开箱即用的 composable。

3. [从零到一：构建一个功能完备的文档列表页（实战篇）](./nuxt-docs-list-page-complete-guide)
   —— 综合运用前两篇的知识，实现完整的文档列表页。

4. [Nuxt + Go 全栈实践：从 URL 状态到后端 API 的完整闭环](./nuxt-go-fullstack-closed-loop)
   —— 将前端 URL 状态与 Go 后端 API 打通，形成完整的数据流闭环。

---

## 一、需求与挑战

### ✅ 最终效果

- **桌面端**：分页 + 键盘左右键翻页，支持 **Ctrl/⌘ + 点击** 多选标签。

<img src="../../images/desktop-demo.gif" style="width: 100%; max-width: 800px;" alt="桌面端综合演示" />

- **移动端**：无限滚动 + 下滑刷新，支持 **“多选模式”开关** 进行标签多选（无需键盘）。

<img src="../../images/mobile-demo.gif" style="width: 100%; max-width: 400px;" alt="移动端综合演示" />

- **所有筛选状态**（搜索词、搜索范围、页码、每页条数、视图模式、等级、标签）均与 URL 同步，页面可分享、可刷新、可后退/前进。
- **SSR 安全**：无水合错误，服务端和客户端渲染结果完全一致。

### ⚠️ 核心挑战

- **URL 与状态双向同步**：用户操作更新 URL，URL 变化（如后退/前进）更新内部状态。
- **SSR 水合问题**：服务端与客户端渲染的 DOM 结构必须完全一致。
- **组件拆分时的 SSR 陷阱**：`isMobile`、`isDesktop` 等环境敏感值不能重复调用，否则会导致水合失败。
- **移动端手势与桌面端键盘**：两套交互逻辑需要兼容并优雅切换。
- **标签多选**：桌面端用 Ctrl，移动端用显式的“多选模式”开关（避免长按误触）。

## 二、技术选型与项目结构

- **框架**：Nuxt 4（SSR）
- **数据源**：Nuxt Content（`queryCollection`）
- **UI 组件**：Nuxt UI 的 `UBlogPost`、`UInput`、`USelect`、`UPagination` 等
- **工具库**：`@vueuse/core` 提供 `useLocalStorage`、`useScroll`、`useEventListener` 等

项目结构（关键部分）：

```bash
nuxt.config.ts
plugins/
  useResponsive.ts         # 响应式判断（ssrWidth: 768）
  useSwipe.ts              # 滑动检测
layouts/
pages/
  index.vue                # 主页面
components/
  docs/
    SearchHeader.vue       # 搜索栏 + 下拉框
    NavigationLevel.vue    # 等级导航
    TagFilter.vue          # 标签云 + 多选模式开关
    List.vue               # 文章列表
    PaginationBar.vue      # 分页组件
composables/
  useResponsive.ts         # 响应式判断（ssrWidth: 768）
  useSwipe.ts              # 滑动检测
utils/
  tags.ts                  # 标签白名单
```

## 三、URL 状态同步：手写闭环

### 3.1 核心思路

- **单一数据源**：所有状态从 `route.query` 初始化。
- **双向同步**：`watch(route.query)` 将 URL 变化同步到内部 ref；`watch` 内部 ref 变化时调用 `router.push` 更新 URL。
- **防抖**：搜索输入防抖，其他立即更新。

### 3.2 代码示例（主文件 `index.vue` 中的状态定义与同步）

```ts
const route = useRoute();
const router = useRouter();

// 状态定义（全部从 URL 初始化）
const searchInput = ref(route.query.search?.toString() || "");
const searchOption = ref(Number(route.query.option) || 1);
const page = ref(Number(route.query.page) || 1);
const size = ref(Number(route.query.size) || 10);
const viewMode = ref(Number(route.query.viewMode) || 1);
const level = ref(route.query.level?.toString() || "");
const tags = ref<string[]>([]);

// 解析 URL 中的标签（支持逗号分隔）
const parseTagsFromQuery = () => {
  const tagParam = route.query.tag;
  tags.value = tagParam
    ? Array.isArray(tagParam)
      ? tagParam
      : tagParam.split(",")
    : [];
};
parseTagsFromQuery();

// 监听路由变化（后退/前进）
watch(
  () => route.query,
  (newValue) => {
    searchInput.value = newValue.search?.toString() || "";
    searchOption.value = Number(newValue.option) || 1;
    page.value = Number(newValue.page) || 1;
    size.value = Number(newValue.size) || 10;
    viewMode.value = Number(newValue.viewMode) || 1;
    level.value = newValue.level?.toString() || "";
    parseTagsFromQuery();
  },
  { immediate: true },
);

// 推送路由
function pushQuery() {
  const query: Record<string, string> = {};
  if (searchInput.value) query.search = searchInput.value;
  if (searchOption.value !== 1) query.option = String(searchOption.value);
  if (page.value !== 1) query.page = String(page.value);
  if (size.value !== 10) query.size = String(size.value);
  if (viewMode.value !== 1) query.viewMode = String(viewMode.value);
  if (level.value) query.level = level.value;
  if (tags.value.length) query.tag = tags.value.join(",");

  if (JSON.stringify(route.query) !== JSON.stringify(query)) {
    router.push({ query });
  }
}

// 监听状态变化，自动更新 URL（关键：直接监听 ref 确保数组变化也能捕获）
watch([page, size, viewMode, level, tags], () => pushQuery());
watchDebounced(
  searchInput,
  () => {
    page.value = 1;
    pushQuery();
  },
  { debounce: 500 },
);
watch(searchOption, () => pushQuery());
```

## 四、数据获取：useAsyncData + 响应式依赖

### 4.1 核心代码

```ts
const { data: docsData, pending } = await useAsyncData(
  "docs-list",
  async () => {
    let query = queryCollection("docs").order("date", "DESC");
    const keyword = searchInput.value.trim();

    // 搜索条件
    if (keyword) {
      if (searchOption.value === 1) {
        query = query.orWhere((q) =>
          q
            .where("title", "LIKE", `%${keyword}%`)
            .where("description", "LIKE", `%${keyword}%`),
        );
      } else {
        query = query.where("title", "LIKE", `%${keyword}%`);
      }
    }

    // 等级过滤
    if (level.value) {
      query = query.where("level", "=", level.value);
    }

    // 标签过滤（AND 关系）
    if (tags.value.length) {
      query = query.andWhere((q) => {
        tags.value.forEach((tag) => {
          q = q.where("tags", "LIKE", `%${tag}%`);
        });
        return q;
      });
    }

    // 分页
    const [total, list] = await Promise.all([
      query.count(),
      query
        .skip((page.value - 1) * size.value)
        .limit(size.value)
        .all(),
    ]);

    return { total, list };
  },
  {
    // 注意：viewMode 仅用于 UI 展示，不应触发数据重新请求，因此未放入 watch
    watch: [searchInput, searchOption, page, size, level, tags],
  },
);
```

> **关键点**：`watch` 数组中直接使用 ref 本身（如 `tags`），确保数组内部变化能被正确捕获。同时将 `viewMode` 移出 watch，避免因视图模式切换导致无意义的数据重载。

### 4.2 移动端累积列表（无限滚动）

```ts
const docsList = ref<any[]>([]);
watch(
  () => docsData.value?.list,
  (newList) => {
    if (!newList) return;
    // 仅当移动端且 page > 1 时才合并（水合阶段 page=1，不会进入）
    if (isMobile.value && page.value > 1) {
      const merged = [...docsList.value, ...newList];
      const uniqueMap = new Map(merged.map((item) => [item.id, item]));
      docsList.value = Array.from(uniqueMap.values());
    } else {
      docsList.value = newList;
    }
  },
  { immediate: true },
);
```

> **说明**：当用户执行搜索、切换等级或标签时，`page` 会被重置为 1，此时 `page.value > 1` 条件不满足，`docsList` 会被重新赋值为新数据，累积列表自动清空，符合预期。

## 五、SSR 安全：避免水合失败的黄金法则

水合失败的根本原因是服务端与客户端渲染的 DOM 结构不一致。我们的解决方案：

1. **所有影响初始 DOM 的状态从 URL 初始化**（`level`、`tags`、`page` 等），保证服务端和客户端初始值一致。
2. **`isMobile` / `isDesktop` 只在根组件计算一次，通过 props 传递给子组件**，避免子组件重复调用 `useResponsive` 导致服务端/客户端判断不一致。下文示例中，`TagFilter.vue` 的 `isDesktop` 即由父组件传入。
3. **`isFilterVisible` 使用 `useLocalStorage`**（不影响初始 DOM，服务端默认为 false，客户端从 localStorage 读取后更新，但不会改变水合结构）。
4. **累积列表逻辑仅在水合完成后才启用**（通过 `page.value > 1` 条件限制，水合时 `page` 为 1）。
5. **`useAsyncData` 的 `watch` 直接使用 ref**，确保数据变化能正确触发。

## 六、组件拆分与职责划分

### 6.1 父组件（`index.vue`）负责

- 所有 URL 状态的管理与同步
- 数据获取（`useAsyncData`）
- 全局交互（键盘、手势、滚动）
- 将 `isDesktop`、`tags`、`getTagLink`、`isTagSelected`、`handleTagClick` 等传递给子组件

### 6.2 子组件仅负责展示与事件转发

例如 `TagFilter.vue`，其 `isDesktop` 由父组件通过 props 传入，确保 SSR 安全：

```vue
<template>
  <div>
    <!-- 桌面端提示 -->
    <span v-if="isDesktop" class="ml-2 text-xs text-gray-500">
      Ctrl+点击多选
    </span>

    <!-- 移动端多选模式开关 -->
    <div v-if="!isDesktop" class="flex justify-end mb-2">
      <button
        @click="multiSelectMode = !multiSelectMode"
        class="text-xs px-2 py-1 rounded bg-gray-700 text-gray-300"
        :class="{ 'bg-blue-600 text-white': multiSelectMode }"
      >
        {{ multiSelectMode ? "退出多选" : "多选模式" }}
      </button>
    </div>

    <div class="w-full flex flex-wrap">
      <NuxtLink
        v-for="tag in ALLOWED_TAGS"
        :key="tag"
        :to="getTagLink(tag)"
        class="block p-2 mx-1 nav-link"
        :class="{ active: isTagSelected(tag) }"
        @click.prevent="onTagClick(tag, $event)"
      >
        #{{ tag }}
      </NuxtLink>
    </div>
  </div>
</template>

<script setup>
import { ALLOWED_TAGS } from "~/utils/tags";

const { isMobile } = useResponsive(); // 仅在子组件内部使用 isMobile 判断移动端分支，不会影响初始 DOM
const props = defineProps({
  isDesktop: { type: Boolean, required: true }, // 从父组件传入
  getTagLink: { type: Function, required: true },
  isTagSelected: { type: Function, required: true },
});

const multiSelectMode = ref(false);
const emit = defineEmits(["tag-click"]);

const onTagClick = (tag, event) => {
  let isMulti = false;
  if (isMobile.value) {
    // 移动端：使用多选模式开关
    isMulti = multiSelectMode.value;
  } else {
    // 桌面端：按 Ctrl/Cmd 多选
    isMulti = event.ctrlKey || event.metaKey;
  }
  emit("tag-click", tag, { ctrlKey: isMulti, metaKey: isMulti });
};
</script>
```

父组件中的 `handleTagClick` 统一处理 URL 更新：

```ts
const handleTagClick = (tag: string, event: MouseEvent) => {
  const isMulti = event.ctrlKey || event.metaKey; // 子组件传递的 event 已包含 ctrlKey
  let newTags: string[];
  if (isMulti) {
    newTags = tags.value.includes(tag)
      ? tags.value.filter((t) => t !== tag)
      : [...tags.value, tag];
  } else {
    newTags = tags.value.includes(tag) ? [] : [tag];
  }
  const query = { ...route.query };
  if (newTags.length) query.tag = newTags.join(",");
  else delete query.tag;
  query.page = "1";
  router.push({ query });
};
```

> **注意**：子组件 `onTagClick` 和父组件 `handleTagClick` 命名不同，职责清晰，避免混淆。

## 七、移动端体验优化

### 7.1 无限滚动与下拉刷新

使用自定义 `useSwipe` 组合式函数监听上滑/下滑，上滑触发 `loadMoreDocs`（`page += 1`），下滑触发 `refreshDocs`（`page = 1`）。滚动检测使用 `useScroll` 判断是否接近底部或顶部。

`useSwipe` 简化实现：

```ts
// composables/useSwipe.ts
import { useEventListener } from "@vueuse/core";

export function useSwipe(
  handlers: {
    onUp?: () => void;
    onDown?: () => void;
  },
  { threshold = 60 } = {},
) {
  const touchStartY = ref(0);

  useEventListener("touchstart", (e: TouchEvent) => {
    touchStartY.value = e.touches[0].clientY;
  });

  useEventListener("touchend", (e: TouchEvent) => {
    const distance = touchStartY.value - e.changedTouches[0].clientY;
    if (distance > threshold) handlers.onUp?.();
    else if (-distance > threshold) handlers.onDown?.();
  });
}
```

### 7.2 多选模式开关

- 移动端没有 Ctrl 键，因此在标签区域右上角增加“多选模式”按钮。
- 点击按钮进入多选状态，再次点击退出。多选模式下点击标签直接切换选中状态（无需 Ctrl）。

### 7.3 响应式布局

- 桌面端：2 列网格，奇数条时最后一条占满。
- 移动端：1 列网格，隐藏分页组件，使用无限滚动 + “加载更多”按钮（实际上使用手势上滑，但也可保留按钮作为备选）。

## 八、标签系统设计

### 8.1 标签白名单

为保持标签系统整洁，定义受控词表（`utils/tags.ts`）：

```ts
export const ALLOWED_TAGS = [
  'Nuxt', 'Vue', 'Docker', 'Caddy', 'GitHub Actions', ...
];
```

### 8.2 标签筛选逻辑

- 多标签 **AND** 关系（文章必须包含所有选中标签）。
- 查询时使用 `andWhere` + 循环添加条件。
- 前端通过 `tags.value.includes(tag)` 判断高亮。

## 九、空状态与用户体验细节

- 当筛选结果为空时，显示友好提示和“清除所有筛选”按钮。
- 加载状态：通过 `pending` 显示骨架或禁用按钮。
- 键盘事件：左右键翻页，ESC 失焦。为避免在输入框中误触翻页，全局键盘事件中需判断当前聚焦元素：

```ts
const isInputFocused = computed(() => {
  const active = document.activeElement;
  return active?.tagName === "INPUT" || active?.tagName === "TEXTAREA";
});

useEventListener("keydown", (e) => {
  if (e.key === "Escape" && isInputFocused.value) {
    (document.activeElement as HTMLElement)?.blur();
    return;
  }
  if (isInputFocused.value) return; // 输入框聚焦时不响应翻页

  if (e.key === "ArrowLeft" && hasPrevPage.value) {
    e.preventDefault();
    page.value -= 1;
  } else if (e.key === "ArrowRight" && hasNextPage.value) {
    e.preventDefault();
    page.value += 1;
  }
});
```

- 移动端手势：上滑加载更多，下滑刷新。

```ts
// 空状态提示
const emptyStateMessage = computed(() => {
  const hasSearch = searchInput.value;
  const hasLevel = level.value;
  const hasTags = tags.value.length;
  if (hasSearch || hasLevel || hasTags) {
    return "没有找到符合条件的文档，试试调整筛选条件吧";
  }
  return "还没有文档，请稍后再来";
});

// 清除所有筛选
const clearAllFilters = () => {
  searchInput.value = "";
  level.value = "";
  tags.value = [];
  page.value = 1;
  size.value = 10;
  viewMode.value = 1;
};
```

## 十、踩坑与总结

### 10.1 踩过的坑

1. **水合失败**：子组件重复调用 `useResponsive` 导致 `isMobile` 不一致。解决方案：将 `isDesktop` 作为 props 从父组件传递（见第六节）。
2. **`useAsyncData` 的 `watch` 不响应数组变化**：直接监听 `tags` 而不是 `() => tags.value` 即可解决。
3. **`viewMode` 触发数据请求**：将 `viewMode` 从 `watch` 中移除，避免无意义的网络请求。
4. **移动端长按多选**：长按容易误触且需要处理滚动干扰，最终改为显式“多选模式”开关，体验更好。
5. **分页组件在移动端显示过多**：移动端隐藏分页，改用无限滚动 + 加载更多按钮。
6. **空状态重复提示**：底部“已查询到 0 条文档”与空状态重复，移除。

### 10.2 最终成果

- **功能完整性**：分页、搜索、等级、标签、视图模式、键盘左右键、移动端手势、无限滚动、下拉刷新、空状态。
- **SSR 安全**：无任何水合错误，服务端与客户端渲染一致。
- **代码质量**：状态集中管理，组件职责清晰，支持未来扩展。

## 十一、结语

构建这个文档列表页花了不少时间，但也让我对 Nuxt SSR 的运作机制有了更深的理解。如果你也在类似项目中遇到水合问题或状态同步的困扰，希望这份记录能给你一些启发。所有代码均已在实际项目中稳定运行，欢迎参考。

**相关链接**：

- [Nuxt 中 URL 与状态双向绑定指南](./nuxt-url-state-guide)
- [Nuxt 4 中安全实现状态持久化：根治水合失败指南](./nuxt-state-persistence-guide)

---

_文中代码为实际项目简化版，完整实现请参考项目源码。_
