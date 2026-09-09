---
title: 2核2G服务器运行多个Docker容器的内存与Swap问题排查
description: 本文记录了在2核2G服务器上运行多个Docker容器时，遇到的内存与Swap问题的排查过程。
date: 2026-09-09
series:
tags:
  - Docker
  - Performance
---

## 一、现象：收到告警，SSH无法登录

某天，我的阿里云ECS收到一条告警：

| 字段     | 内容                                           |
| -------- | ---------------------------------------------- |
| 事件名称 | Instance:StoragePerformanceReachLimit:Executed |
| 等级     | WARN                                           |
| 原因码   | SysDiskBPS                                     |
| 地域     | cn-beijing                                     |

同时，SSH完全无法登录，一直卡住超时。

第一反应是检查磁盘空间是不是满了——毕竟告警里写着"StoragePerformanceReachLimit"。但问题是，这台服务器上只跑了个人博客和一些Docker容器，平时空间占用并不高。

这就是最让人困惑的地方：**磁盘空间明明没满，为什么会报磁盘相关的告警？而且SSH为什么也连不上？**

事后才搞清楚一个关键认知差：**磁盘告警 ≠ 磁盘空间满**。云厂商的磁盘监控有两类指标，一类是容量（空间用了多少），另一类是性能（读写速度、IOPS）。这次的 `SysDiskBPS` 属于后者，它监控的是磁盘的**读写速度**是否达到了上限。

但磁盘读写速度为什么会被跑满？这是后续排查才找到答案的。

## 二、紧急恢复：直接重启

SSH完全无法连接，考虑到这是个人博客，没有关键业务依赖，没什么好顾虑的，直接在ECS控制台强制重启实例。

重启后，SSH恢复正常登录，系统恢复可用。

> 重启只是临时恢复手段，如果不找到根本原因，问题大概率会再次出现。所以重启后的第一件事不是"松口气"，而是排查真相。

## 三、初步检查：内存与Swap

重启后第一件事，查看系统整体资源情况：

```bash
free -h
```

输出结果中，`Swap` 那一栏的使用量异常偏高，而物理内存（`Mem`）已经所剩无几。

**什么是Swap？**

当物理内存（RAM）不足时，Linux内核会将一部分磁盘空间用作"虚拟内存"，把暂时不用的数据从内存挪到磁盘上，这个机制叫做Swap。由于磁盘的读写速度远慢于内存，一旦系统开始频繁使用Swap，整体响应会急剧变慢。同时，大量的磁盘读写操作会撑高磁盘的BPS（每秒读写字节数），从而触发云厂商的性能告警。

这就是为什么磁盘空间没满，但收到了磁盘性能告警的原因——**真正的问题不是磁盘空间不够，而是内存不足引发了Swap，Swap又拖垮了磁盘性能**，最终导致系统卡死，连SSH都无法响应。

**为什么连SSH都会卡死超时？**

当内存极度紧张时，Linux内核会触发**直接内存回收（Direct Reclaim）**。此时，任何需要申请内存的进程（包括SSH服务）都必须同步等待内核回收内存，进程会进入**不可中断睡眠状态（D状态）**。这会导致CPU被内核态大量占用，用户态进程无法响应，SSH连接自然超时。

如果你当时能登录系统（比如通过VNC），可以使用以下命令验证是否存在D状态进程：

```bash
ps aux | grep " D "
```

如果看到大量进程处于D状态，基本可以确认系统正在经历内存回收风暴。

**数据视角：如何判断系统是否正在被Swap拖垮？**

我们可以通过 `vmstat` 和 `iostat` 来实时观察。以下是两个典型状态的对比：

**1. 健康状态（优化后/正常运行）**：

```bash
# vmstat 1 输出（关键列）
procs -----------memory---------- ---swap-- -----io----
 r  b   swpd   free   buff  cache   si   so    bi    bo
 1  0      0  76676   3088 323184    0    0   191    30

# iostat -x 1 输出（关键列）
Device            r/s     rkB/s   w/s     wkB/s  aqu-sz  %util
vda              3.01    187.71  3.30     30.30    0.01   0.27
```

- `swpd=0`：没有使用Swap。
- `si/so=0`：没有Swap换入/换出操作。
- `%util < 1%`：磁盘非常空闲。

**2. 危险状态（故障发生时）**：

```bash
# vmstat 1 输出（关键列）
procs -----------memory---------- ---swap-- -----io----
 r  b   swpd   free   buff  cache   si   so    bi    bo
 5  2 524288    128     64   1024 1500 2000  3000  4000

# iostat -x 1 输出（关键列）
Device            r/s     rkB/s   w/s     wkB/s  aqu-sz  %util
vda            150.0   12000.0 200.0   16000.0   15.00  99.80
```

- `swpd` 持续高位：大量内存被换出到磁盘。
- `si/so` 数值很大：系统正在疯狂进行Swap换入换出。
- `%util` 接近100%：磁盘IO被完全占满，响应变慢。
- `aqu-sz`（平均队列长度）很高：大量IO请求在排队。

当你看到第二种数据时，基本可以确认：**系统正在被Swap拖垮，磁盘性能已经崩溃。**

## 四、定位到Docker容器

这台服务器上跑了多个Docker容器，需要看看到底是哪些容器在消耗资源：

```bash
docker stats --no-stream
```

输出结果：

| 容器                    | 内存占用 | 说明                    |
| ----------------------- | -------- | ----------------------- |
| my-blog-db (PostgreSQL) | ~52 MiB  | 数据库容器              |
| my-blog-app (Node.js)   | ~263 MiB | 博客前端应用            |
| moongate-vue            | ~55 MiB  | 静态文档站（Caddy托管） |
| my-blog-caddy           | ~60 MiB  | 反向代理                |
| my-blog-go-backend      | ~26 MiB  | Go后端API               |

这里需要说明一点：`docker stats` 显示的是容器的 **RSS（常驻内存）**，也就是容器进程实际占用的物理内存。**注意：`docker stats` 中的 RSS 包含了共享内存（Shared Memory）的统计，对于多进程容器（如同时运行多个 Node.js 进程），这个数值可能偏高。** 如果需要更精确的物理内存占用，可以查看 Cgroup 的 `memory.stat` 文件中的 `active_anon` 字段。

但这并不是系统的全部内存开销。除了这些容器，操作系统内核、Page Cache、以及 Node.js 的虚拟内存映射（VIRT）都会占用大量内存空间。所以虽然容器 RSS 合计看起来只有 450 MiB 左右，加上系统自身开销后，2GB 的物理内存其实已经相当紧张，Swap 随时可能被激活。

更要命的是 `my-blog-app`（Node.js应用），它的内存会随访问量波动，当访问量稍微增加时很容易进一步膨胀，从而触发Swap。

> **进阶建议**：对于 Node.js 应用，除了 Docker 层面的限制，建议在应用启动时显式设置堆内存上限（如 `NODE_OPTIONS=--max-old-space-size=300`），防止 V8 引擎无限申请内存。这能让应用在内存接近上限时主动触发垃圾回收（GC），而不是被动等待 Docker 的 OOM Killer。

## 五、优化决策

### 1. 删除不必要的PostgreSQL容器

该博客已从动态数据库模式迁移至 Markdown 文件存储，不再需要独立的 PostgreSQL 容器，直接删除：

```bash
docker rm -f my-blog-db
```

同时清理对应的数据卷，释放磁盘空间：

```bash
docker volume ls
docker volume rm my-blog_postgres_data
```

### 2. 为容器设置内存上限（关键预防措施）

为了防止单个容器内存失控拖垮整机，使用 `docker update` 为每个容器设置硬上限。

> `docker update` 可在容器运行时动态生效，重启后配置依然保留（前提是不重新创建容器）。

```bash
docker update --memory=350m --memory-swap=350m my-blog-app
docker update --memory=150m --memory-swap=150m my-blog-go-backend
docker update --memory=100m --memory-swap=100m my-blog-caddy
docker update --memory=100m --memory-swap=100m moongate-vue
```

这里有两个参数需要解释：

- `--memory`：容器能使用的最大内存量。
- `--memory-swap`：容器能使用的“内存 + Swap”总量上限。

**为什么选择彻底禁用容器Swap？**

对于磁盘性能已经很差的小内存机器，允许容器使用Swap会进一步加剧磁盘IO竞争，导致GC（垃圾回收）停顿更严重。因此，将 `--memory-swap` 设置为与 `--memory` 相等，表示**彻底禁止该容器使用Swap**。所有内存压力只能靠物理内存消化，超过上限则容器被OOM Kill。

如果希望进一步强化禁止Swap的效果，可以显式指定 `--memory-swappiness=0`（在 `docker run` 或 `docker update` 中）。这比调整宿主机的 `vm.swappiness` 更具针对性，且仅对该容器生效。

> **关键提示**：设置硬上限后，务必确保容器配置了合理的重启策略。建议使用 `--restart=on-failure:3`（最多重试3次）代替 `unless-stopped`，以限制重启次数。如果内存余量设置过低，容器可能在启动瞬间再次被OOM Kill，形成**CrashLoopBackOff**（崩溃循环）。你可以通过 `docker ps -a` 检查容器状态，或使用 `docker events --filter event=oom` 实时监听OOM事件，确保服务处于“自愈”而非“反复崩溃”的状态。

对于这台小内存服务器来说，宁可让单个容器偶尔重启，也不能让它拖垮整台机器的磁盘性能。

### 3. 确认静态托管模式

`moongate-vue` 容器执行检查：

```bash
docker exec moongate-vue ps aux
```

输出显示运行的是 `caddy file-server --root /docs --listen :80`，即Caddy直接托管静态文件，而非Node.js开发服务器。这是生产环境下的正确部署方式，内存占用稳定，没有额外风险。

## 六、验证效果

再次执行 `docker stats` 和 `free -h`，重点观察Swap使用率是否显著下降，以及物理内存是否有足够的余量。

同时，使用 `vmstat 1` 和 `iostat -x 1` 进行实时监控，确认系统恢复到健康状态：

- `vmstat` 中的 `swpd` 应接近或等于0，`si/so` 应为0。
- `iostat` 中的 `%util` 应低于10%，`aqu-sz` 应接近0。

优化后，系统运行流畅，内存占用稳定，Swap不再被大量使用，告警不再触发，SSH始终保持稳定连接。

## 七、总结

这次问题的本质是：**多个Docker容器（特别是Node.js应用）占用了大量物理内存 → 系统触发Swap机制 → 磁盘读写速度被拖垮 → 触发云厂商BPS告警 → 系统卡死导致SSH无法登录。**

几点经验：

1. **磁盘告警不只看容量**——云厂商的磁盘监控有容量和性能（BPS/IOPS）两类指标，告警时要先看清楚是哪一种。
2. **Swap是救命稻草也是性能杀手**——内存紧张时它会帮系统"续命"，但代价是磁盘性能被拖垮，甚至导致系统完全卡死。
3. **`docker stats` 是排查容器资源占用的利器**——一眼就能发现谁在消耗内存。
4. **小内存服务器要敢于做减法**——不需要的容器该删就删，数据库这类重服务如果确实需要，可以考虑迁移到云数据库。
5. **给容器设置内存上限时，记得一并设置 `--memory-swap`**——否则容器仍然可能使用Swap，问题只是被缓解，并未彻底解决。
6. **可以适当降低 `vm.swappiness` 值**——让系统尽量少用Swap，从系统层面减轻磁盘被拖垮的风险。例如执行 `sysctl -w vm.swappiness=10` 可以降低系统使用Swap的倾向。但需要注意：在 Cgroup v2（新版 Docker 默认）下，容器内的 Swap 行为受 `--memory-swappiness` 控制，宿主机的 `vm.swappiness` 对容器的影响已减弱。配置前请确认自己的 Docker 版本和运行时。
7. **善用 `vmstat` 和 `iostat` 进行实时监控**——当怀疑系统被Swap拖垮时，使用 `vmstat 1` 观察 `si/so`（Swap换入换出）和 `iostat -x 1` 观察 `%util`（磁盘利用率），可以快速确认问题。

一次排查，搞清楚的不只是一个告警的含义，更是对Linux内存管理、Swap机制和Docker资源限制有了更实在的理解。

## 八、事后跟踪：堵不如疏

容器内存限制只是“治标”，要真正解决问题，还需要“治本”。

1. **监控Node.js内存趋势**：使用 `node --trace-gc` 观察垃圾回收频率和耗时，或通过 Prometheus + Grafana 监控堆内存变化曲线。如果内存呈现持续上涨趋势，可能存在闭包引用残留等内存泄漏问题。
2. **分析堆内存快照**：在开发环境中，使用 `heapdump` 或 Chrome DevTools 抓取内存快照，分析是否存在未释放的对象引用。
3. **评估是否需要升级配置**：如果业务增长导致2GB内存确实不够用，且优化代码后内存仍然紧张，那么升级服务器配置（如4GB）才是根本解决方案。**堵不如疏，限制只是手段，理解业务需求才是目的。**

希望这篇文章能帮到遇到同样问题的你。
