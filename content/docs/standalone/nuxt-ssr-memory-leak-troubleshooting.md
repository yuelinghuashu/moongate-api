---
title: Nuxt SSR 内存泄漏排查实录：一台 2G 服务器被拖垮的完整复盘
description: 从云厂商磁盘告警与 SSH 卡死出发，逐层深入 Swap、容器、Node 进程，最终定位到 Nuxt.js SSR 模块级全局状态导致的服务端内存泄漏，并给出修复与验证。
date: 2026-09-09
series:
tags:
  - Nuxt
  - SSR
  - Vue
  - Performance
---

## 一、现象：磁盘告警，SSH 卡死

某天，我的服务器收到一条云厂商告警：

| 字段     | 内容                                           |
| -------- | ---------------------------------------------- |
| 事件名称 | Instance:StoragePerformanceReachLimit:Executed |
| 等级     | WARN                                           |
| 原因码   | SysDiskBPS                                     |
| 地域     | cn-beijing                                     |

与此同时，SSH 完全无法登录，连接一直卡住超时。

第一反应是检查磁盘空间是否满了——毕竟告警写着"StoragePerformanceReachLimit"。但磁盘明明没满，为什么会报磁盘告警？SSH 又为什么连不上？

事后才搞清楚一个关键认知差：**磁盘性能告警 ≠ 磁盘空间满**。云厂商的磁盘监控有两类指标，一类是容量（用了多少空间），另一类是性能（读写速度、IOPS）。这次的 `SysDiskBPS` 属于后者，监控的是磁盘**读写吞吐**是否达到上限。

而磁盘吞吐被跑满的根因，藏得很深——它不在服务器配置里，不在容器设置里，而在我的 Nuxt.js 应用代码里。

## 二、系统层初步排查：Swap 与内存

SSH 无法连接，直接强制重启。重启后查看系统资源：

```bash
free -h
```

输出显示 `Swap` 使用量异常偏高，物理内存（`Mem`）所剩无几。

### Swap 是什么？

物理内存不足时，Linux 内核把暂时不用的数据从内存挪到磁盘，这个机制叫 Swap。磁盘远慢于内存，一旦系统频繁使用 Swap，响应急剧变慢；大量磁盘读写又会撑高 BPS，触发云厂商的性能告警。

所以链路是：**内存不足 → 触发 Swap → 磁盘性能被拖垮 → 触发 BPS 告警 → 系统卡死、SSH 无法响应。**

### 为什么连 SSH 都会卡死？

内存极度紧张时，Linux 内核触发**直接内存回收（Direct Reclaim）**，需要申请内存的进程必须同步等待内核回收内存，进入**不可中断睡眠状态（D 状态）**。具体到 SSH：

1. **fork() 阻塞**：SSH 连接建立后，sshd 需要 fork() 子进程执行 bash，而 fork() 需要分配内存，触发 Direct Reclaim 后被卡住。
2. **网络栈响应缓慢**：kswapd0 等内核线程占用大量 CPU，网络中断处理延迟，TCP 连接超时。

此时如果能登录（比如 VNC），可以用 `ps aux | grep " D "` 看到大量 D 状态进程；`top` 中 `%sy`（系统态 CPU）会异常偏高。

### 用 vmstat/iostat 确认 Swap 风暴

健康状态（优化后）：

```bash
# vmstat 1 输出（关键列）
procs -----------memory---------- ---swap-- -----io----
 r  b   swpd   free   buff  cache   si   so    bi    bo
 1  0      0  76676   3088 323184    0    0   191    30
```

- `swpd=0`：未使用 Swap；`si/so=0`：无换入换出。

危险状态（故障时）：

```bash
procs -----------memory---------- ---swap-- -----io----
 r  b   swpd   free   buff  cache   si   so    bi    bo
 5  2 524288    128     64   1024 1500 2000  3000  4000
```

- `swpd` 高位、`si/so` 很大：系统正在疯狂换页，磁盘被拖垮。

到这里，我的判断还停留在"2G 内存不够用、容器太多"。真相远不止于此。

## 三、容器层：发现"嫌疑"

服务器上跑着一个 Nuxt.js 博客应用（容器 `my-blog-app`）和其他几个容器。用 `docker stats` 观察，发现 `my-blog-app` 的 RSS 异常且持续增长。

当时做了一些常规缓解：删除不再需要的 PostgreSQL 容器、给各容器设置 `--memory` 上限并禁用 Swap。这些措施确实让系统暂时稳定，但**内存仍在缓慢增长**——它们只是把症状按住，没有解决根因。（这也是本文最想强调的一点：容器层面的限制是"治标"，真正的病在应用代码里。）

## 四、真相一：内存根本不在 Node 堆里

容器内存一路涨到 859 MiB，但进入容器检查 Node.js 的堆内存，结果令人意外：

```bash
/app # node -e "console.log(process.memoryUsage())"
{
  rss: 48873472,        // ~48 MiB，进程物理内存
  heapTotal: 6062080,   // ~6 MiB
  heapUsed: 4066256,    // ~4 MiB
  ...
}
```

再看 `/proc/1/status`：

```bash
VmPeak: 18706992 kB   // 峰值虚拟内存：17.8 GB！
VmRSS:  896188 kB     // 当前物理内存：875 MiB
VmData: 1490800 kB    // 数据段：1.45 GB
Threads: 11
```

**关键矛盾出现了**：容器显示 859 MiB、进程 RSS 也是 875 MiB，但 V8 堆只用了几 MiB。内存不在堆里——它被服务端运行时的某个"看不见的东西"持续累积，峰值虚拟内存甚至冲到 17.8 GB。

这排除了"业务代码产生大对象"的猜测，把矛头指向**框架/运行时层的全局状态**。

## 五、真相二：Nuxt SSR 模块级全局状态泄漏

排查到 `app/composables/useRouteQuery.ts`，发现了一个典型的 SSR 内存泄漏模式——**模块级可变全局状态**：

```typescript
// ❌ 问题代码
const registry = new Set<QueryRegistration>()   // 模块级全局 Set

function useRouteQueryRaw(name: string) {
  const value = ref(route.query[name])
  const registration = { name, getValue: () => value.value }
  registry.add(registration)                     // 每次调用都添加

  const instance = getCurrentInstance()
  if (instance) {
    onUnmounted(() => registry.delete(registration))  // 依赖卸载清理
  }
  ...
}
```

**泄漏机制**：

1. 每次 SSR 请求渲染 `/docs` 页面，`useDocs()` 会调用多个 `useRouteQuery*`，每个都向模块级 `registry` Set 添加一个注册项，注册项闭包持有 ref 引用。
2. 清理依赖 `onUnmounted`——但**服务端渲染没有卸载生命周期**，注册项永远不会被删除。
3. 于是注册项跨请求无限累积，每个都拽着一整条响应式引用链，内存随之无上限增长。VmPeak 冲到 17.8 GB 就是累积的结果。

## 六、修复：让全局注册表随请求释放

把模块级 Set 改成**以 nuxtApp 为 key 的 WeakMap**：

```typescript
// ✅ 修复代码
const registryMap = new WeakMap<object, Set<QueryRegistration>>()

function getRegistry() {
  const nuxtApp = useNuxtApp()
  let registry = registryMap.get(nuxtApp)
  if (!registry) {
    registry = new Set()
    registryMap.set(nuxtApp, registry)
  }
  return registry
}
```

原理：

- **服务端**：每个请求有独立的 nuxtApp → 每个请求有独立注册表，不再跨请求累积。
- **客户端**：全局唯一 nuxtApp → 所有组件共享同一个注册表，`resetFilters` 批量更新 URL 的行为不受影响。

**为什么 WeakMap 能做到"随请求释放"？** 关键在于 WeakMap 的 key 是**弱引用**。服务端每个请求结束后，Nuxt 框架会释放该请求的 nuxtApp（不再被强引用），下一次 GC 时 WeakMap 中对应的整条条目——包括它的 value（那个注册表 Set）——就会一并被回收。所以注册表天然跟着请求的生死走，不需要手动清理。

**那客户端呢？** 客户端的机制正好相反：nuxtApp 全局唯一且常驻，注册表不会自动消失，靠的是**组件卸载时清理**——路由切换导致组件卸载，`onUnmounted` 触发，把该组件添加的注册项从注册表删除。这样客户端一侧靠"手动清理"维持平衡，也不会随页面切换而膨胀。服务端自动释放、客户端手动清理，两套机制恰好对称。

还有一个隐蔽的坑：`watch` 回调在异步执行时 `getCurrentInstance()` 返回 null，原代码在 watch 里动态查注册表会失败（表现为状态改了 URL 不更新）。修复方式是在 setup 期间**捕获注册表引用**，供 watch 闭包使用：

```typescript
function useRouteQueryRaw(name: string) {
  const registry = getRegistry() // setup 期捕获
  if (registry) {
    registry.add({ name, getValue: () => value.value })
    // ...onUnmounted 清理（客户端）
  }
  watch(value, () => {
    router.replace({ query: buildQueryFromRegistry(registry) }) // 用捕获的引用
  })
  return value
}
```

## 七、验证：内存稳定了

修复后重新部署，对比非常直观：

| 指标      | 修复前                          | 修复后                                 |
| --------- | ------------------------------- | -------------------------------------- |
| 启动内存  | ~60 MiB                         | ~62 MiB                                |
| 峰值/趋势 | 859 MiB 且无上限（~6 MiB/分钟） | 30 分钟后 152 MiB                      |
| 长期趋势  | 持续增长直至 OOM                | **20 分钟后稳定在 ~150 MiB，不再增长** |
| VmPeak    | 17.8 GB                         | 回归正常                               |

同时验证了功能没有回归：

- **状态 → URL**：搜索、翻页、点标签后 URL 正常更新。
- **URL → 状态**：手动改 URL，筛选状态同步。
- **跨组件共享**：标签筛选（列表页 + 筛选器）状态一致。

## 八、总结与经验

### SSR 内存泄漏排查方法论

这次排查走了一条清晰的链路，任何 SSR 应用都可以复用：

1. **容器层**（`docker stats`）：发现哪个进程内存异常。
2. **进程层**（`/proc/1/status`）：看 RSS、VmPeak、VmData——RSS 与堆差距大、VmPeak 异常高，都是强信号。
3. **运行时层**（`process.memoryUsage()`）：确认问题不在 V8 堆里。
4. **代码层**：搜索模块级可变全局状态（模块级 `let`、`const xxx = new Set()/Map()/[]`），SSR 下它们会在请求间共享、累积。

### 三条教训

1. **模块级可变全局状态 = SSR 内存泄漏头号嫌疑**。需要跨请求共享的状态，应放在请求作用域（Nuxt 的 nuxtApp / useState）里，而不是模块顶层。
2. **容器限制只是缓解，不是根因**。给容器设 `--memory` 上限能防止拖垮整机，但内存泄漏还在——找到并修复根因才算真正解决。
3. **VmPeak 与 RSS 的巨大差距是泄漏的警报**。正常进程的峰值虚拟内存不会比常驻内存高出几个数量级，出现这种情况先怀疑运行时层的累积。

一次排查，从云厂商的一条磁盘告警，挖到了自家应用代码里的一行全局状态。表面是"2G 服务器不够用"，实际是代码让 2G 服务器怎么都不够用。问题的本质不是内存太小，而是**无上限累积**——即便当时升到 16G，也只是把崩溃从几小时延长到几天。扩容只能延缓症状，修复根因才是终点。希望这篇文章能帮你少走这些弯路。
