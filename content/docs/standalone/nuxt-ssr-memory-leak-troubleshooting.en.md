---
title: "A Nuxt.js SSR Memory Leak Postmortem: How One Global Variable Crippled a 2GB Server"
description: "From a cloud disk alert and unresponsive SSH, this postmortem digs through Swap, containers, and the Node process to find the real culprit: a module-level global state leak in a Nuxt.js SSR app—plus the fix and verification."
date: 2026-09-09
series:
tags:
  - Nuxt
  - SSR
  - Vue
  - Performance
---

## 1. The Symptom: Disk Alert, SSH Dead

One day, my server received a cloud provider alert:

| Field       | Content                                        |
| ----------- | ---------------------------------------------- |
| Event Name  | Instance:StoragePerformanceReachLimit:Executed |
| Severity    | WARN                                           |
| Reason Code | SysDiskBPS                                     |
| Region      | cn-beijing                                     |

At the same time, SSH was completely unreachable—connections hung and timed out.

My first instinct was to check if the disk was full, since the alert said "StoragePerformanceReachLimit." But the disk wasn't full, so why a disk alert? And why was SSH dead?

It took a while to grasp the key misconception: **A disk performance alert ≠ a disk capacity alert.** Cloud providers monitor two kinds of disk metrics: capacity (how much space is used) and performance (read/write throughput, IOPS). This `SysDiskBPS` alert belongs to the latter—it watches whether disk **read/write throughput** has hit its ceiling.

And the root cause of that saturated disk throughput was buried deep—not in the server configuration, not in the container settings, but in my Nuxt.js application code.

## 2. System-Level Investigation: Swap & Memory

SSH was down, so I force-rebooted the instance. After it came back, I checked system resources:

```bash
free -h
```

`Swap` usage was abnormally high, and physical memory (`Mem`) was nearly exhausted.

### What is Swap?

When physical memory runs low, the Linux kernel moves temporarily unused data from RAM to disk—that mechanism is called Swap. Disk is far slower than RAM, so once the system starts swapping heavily, responsiveness collapses. The resulting disk I/O also inflates BPS, tripping the cloud provider's performance alert.

The chain: **insufficient memory → Swap kicks in → disk performance collapses → BPS alert fires → system freezes and SSH stops responding.**

### Why did even SSH freeze?

Under extreme memory pressure, the Linux kernel triggers **Direct Reclaim**: any process that needs to allocate memory must synchronously wait for the kernel to reclaim it, entering an **uninterruptible sleep state (D-state)**. For SSH specifically:

1. **fork() blocks**: After an SSH connection is established, sshd forks a child process to run bash. fork() needs to allocate memory, hits Direct Reclaim, and stalls.
2. **Network stack slowdown**: kswapd0 and other kernel threads burn CPU, delaying network interrupt handling until TCP connections time out.

If you can still log in (e.g., via VNC), `ps aux | grep " D "` will show many D-state processes, and `top` will show abnormally high `%sy` (system CPU).

### Confirming the Swap storm with vmstat

Healthy state (after the fix):

```bash
# vmstat 1 output (key columns)
procs -----------memory---------- ---swap-- -----io----
 r  b   swpd   free   buff  cache   si   so    bi    bo
 1  0      0  76676   3088 323184    0    0   191    30
```

- `swpd=0`: no Swap in use; `si/so=0`: no swapping in/out.

Dangerous state (during the incident):

```bash
procs -----------memory---------- ---swap-- -----io----
 r  b   swpd   free   buff  cache   si   so    bi    bo
 5  2 524288    128     64   1024 1500 2000  3000  4000
```

- High `swpd`, large `si/so`: the system is thrashing, and the disk is being dragged down.

At this point my diagnosis was still "2GB isn't enough and I'm running too many containers." The truth went much deeper.

## 3. Container Layer: Finding the Suspect

The server ran a Nuxt.js blog application (container `my-blog-app`) alongside a few other containers. `docker stats` revealed that `my-blog-app`'s RSS was abnormally high and still climbing.

I applied the usual mitigations: removed an unneeded PostgreSQL container, set `--memory` limits on every container, and disabled Swap for them. These did stabilize the system for a while—but **memory kept slowly growing**. They were only suppressing symptoms, not fixing the root cause. (This is the most important point of this article: container-level limits are a band-aid; the real disease lives in the application code.)

## 4. Truth #1: The Memory Wasn't in the Node Heap

Container memory climbed to 859 MiB, but checking the Node.js heap from inside the container told a different story:

```bash
/app # node -e "console.log(process.memoryUsage())"
{
  rss: 48873472,        // ~48 MiB process physical memory
  heapTotal: 6062080,   // ~6 MiB
  heapUsed: 4066256,    // ~4 MiB
  ...
}
```

Then `/proc/1/status`:

```
VmPeak: 18706992 kB   // peak virtual memory: 17.8 GB!
VmRSS:  896188 kB     // current physical memory: 875 MiB
VmData: 1490800 kB    // data segment: 1.45 GB
Threads: 11
```

**The key contradiction**: the container reported 859 MiB and the process RSS agreed at 875 MiB, yet the V8 heap was using only a few MiB. The memory wasn't in the heap—something "invisible" in the server runtime was accumulating it, with peak virtual memory even reaching 17.8 GB.

That ruled out "the business code creates big objects" and pointed squarely at **global state in the framework/runtime layer**.

## 5. Truth #2: A Module-Level Global State Leak in Nuxt SSR

Digging into `app/composables/useRouteQuery.ts`, I found a textbook SSR memory leak pattern—**mutable module-level global state**:

```typescript
// ❌ Problematic code
const registry = new Set<QueryRegistration>()   // module-level global Set

function useRouteQueryRaw(name: string) {
  const value = ref(route.query[name])
  const registration = { name, getValue: () => value.value }
  registry.add(registration)                     // added on every call

  const instance = getCurrentInstance()
  if (instance) {
    onUnmounted(() => registry.delete(registration))  // cleanup relies on unmount
  }
  ...
}
```

**How it leaks**:

1. Every SSR request that renders the `/docs` page calls `useDocs()`, which invokes several `useRouteQuery*` helpers. Each one adds a registration—a closure holding a ref—to the module-level `registry` Set.
2. Cleanup depends on `onUnmounted`, but **server-side rendering has no unmount lifecycle**, so those registrations are never removed.
3. Registrations accumulate across requests without bound, each dragging an entire reactive reference chain along, and memory grows indefinitely. A VmPeak of 17.8 GB is the result.

## 6. The Fix: Scope the Registry to the Request

Replace the module-level Set with a **WeakMap keyed by nuxtApp**:

```typescript
// ✅ Fixed code
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

How it works:

- **Server**: every request has its own nuxtApp, so every request gets its own registry—no more cross-request accumulation.
- **Client**: there's a single global nuxtApp, so all components share one registry and batched URL updates (e.g., `resetFilters`) behave exactly as before.

**Why does the WeakMap "follow the request lifecycle"?** The key insight is that a WeakMap's keys are **weak references**. When a server request ends, the Nuxt framework releases that request's nuxtApp (it's no longer strongly referenced), and at the next GC the WeakMap's entire entry—including its value, the registry Set—is collected together. The registry therefore lives and dies with the request, with no manual cleanup needed.

**What about the client?** The client works the opposite way: nuxtApp is global and long-lived, so its registry never disappears on its own. Balance is maintained by **cleanup on component unmount**—when route changes tear down a component, `onUnmounted` fires and removes the registrations that component added. The client side stays bounded across page navigations thanks to that manual cleanup. Server-side automatic release, client-side manual cleanup: the two mechanisms are neatly symmetrical.

There was also a subtle trap: inside async `watch` callbacks, `getCurrentInstance()` returns null, so looking up the registry dynamically at that point silently failed (the symptom: state changed but the URL didn't update). The fix captures the **registry reference during setup** and passes it into the watch closure:

```typescript
function useRouteQueryRaw(name: string) {
  const registry = getRegistry() // captured during setup
  if (registry) {
    registry.add({ name, getValue: () => value.value })
    // ...onUnmounted cleanup (client only)
  }
  watch(value, () => {
    router.replace({ query: buildQueryFromRegistry(registry) }) // use captured ref
  })
  return value
}
```

## 7. Verification: Memory Stabilized

After redeploying with the fix, the contrast is stark:

| Metric          | Before                               | After                                                      |
| --------------- | ------------------------------------ | ---------------------------------------------------------- |
| Startup memory  | ~60 MiB                              | ~62 MiB                                                    |
| Peak / trend    | 859 MiB with no ceiling (~6 MiB/min) | 152 MiB after 30 minutes                                   |
| Long-term trend | Growing until OOM                    | **Stable at ~150 MiB after 20 minutes, no further growth** |
| VmPeak          | 17.8 GB                              | Back to normal                                             |

Feature regressions were also ruled out:

- **State → URL**: searching, paging, and clicking tags all update the URL correctly.
- **URL → state**: manually editing the URL syncs the filter state.
- **Cross-component sharing**: tag filtering stays consistent between the list page and the filter widget.

## 8. Summary & Lessons

### A methodology for debugging SSR memory leaks

This investigation followed a clean path that any SSR application can reuse:

1. **Container layer** (`docker stats`): find which process has abnormal memory.
2. **Process layer** (`/proc/1/status`): check RSS, VmPeak, VmData—a big gap between RSS and the heap, or an abnormally high VmPeak, are strong signals.
3. **Runtime layer** (`process.memoryUsage()`): confirm the problem isn't in the V8 heap.
4. **Code layer**: search for mutable module-level globals (module-level `let`, `const x = new Set()/Map()/[]`)—under SSR these are shared and accumulate across requests.

### Three takeaways

1. **Mutable module-level global state is the #1 suspect for SSR memory leaks.** State that must be shared across requests belongs in request scope (Nuxt's nuxtApp or useState), not at the top of a module.
2. **Container limits are a mitigation, not a root cause.** A `--memory` cap prevents one process from dragging down the whole host, but the leak is still there—finding and fixing the root cause is the real solution.
3. **A huge gap between VmPeak and RSS is an alarm.** A healthy process's peak virtual memory shouldn't be orders of magnitude above its resident set. When it is, suspect accumulation in the runtime layer first.

All this started with a single disk alert from a cloud provider and ended at one line of global state in my own application code. On the surface it looked like "a 2GB server that wasn't enough"—in reality, the code made 2GB never enough. The core issue wasn't that the memory was too small; it was the **unbounded accumulation**—even upgrading to 16GB would only have stretched the crash from hours to days. Scaling up merely delays the symptom; fixing the root cause is the finish line. I hope this post helps you skip some of these detours.
