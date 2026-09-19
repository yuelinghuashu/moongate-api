---
title: 环境与运维：A770 上装好并长期跑稳 Ollama
description: 在 Intel Arc A770 上把 Ollama 装好并长期跑稳：镜像安装、Vulkan 点亮 GPU、模型选型、实测性能，以及上线前必读的长期运维手册（监控、环境变量、排查顺序）。
date: 2026-09-08
series: go-agent
order: 1
tags:
  - Agent
  - LLM
---

本系列用 Go 在本地搭建 Agent，全程不碰 Python/Node（用 Python 等其他语言的读者，仍可读各篇的机制与避坑内容——工具调用机制、聊天模板、API 兼容与运维概念都与语言无关）。第 1 篇只做环境与运维、不写代码：装好 Ollama、点亮 Vulkan、拉好模型，并讲清楚长期运行会踩哪些坑、怎么监控；代码从第 2 篇开始。

- 前置：基础 Linux 命令行，看得懂 systemd 服务概念（`systemctl`/`journalctl`）
- 想直接上手代码：读完 §1–§4 就够了（本文第 4 节末尾也有 📌 提示）——环境备好即可跳到第 2 篇，第 5 节等要长期运行/上线时再回来精读
- 提示：第 5 节「长期运维手册」为**运维向内容（SRE/DevOps 视角）**——纯 Go 开发读者可先跳过（不影响第 2~5 篇），但它是本系列最独特的稳定性知识，上线长期运行前务必回来精读

> 环境说明：本文基于 **Ollama 0.33.3 + llama3.1:8b（A770/Vulkan，2026-09）** 实测；Ollama 迭代快，环境变量以 `ollama serve --help` 为准，`/v1` 兼容字段以[官方 OpenAI 兼容文档](https://docs.ollama.com/api/openai-compatibility)为准。

## 背景

我是 Nuxt + Go 全栈开发者，不想为学 Agent 额外学一门语言，于是直接用熟悉的 Go 试。

硬件：Intel Arc A770 16GB + 16GB 内存（非主流 AI 配置，下文有对应的坑）。

## 1. 安装 Ollama

本文命令与自启示例基于 Linux(systemd)。**Windows/macOS 用户**：Ollama 官方 Windows 版在系统设置里配置环境变量与开机自启，没有 `systemctl`/`journalctl`；模型与后续代码章节不受平台影响。macOS 无 A770 对应，代码篇可照跑（Metal 后端自动），细节以[官方安装文档](https://docs.ollama.com)为准。

> 本教程要求 **Ollama 版本 >= 0.3.0**，确保原生支持 OpenAI 兼容的 `/v1/chat/completions` 接口（旧版本默认不开启，代码会报 404/400）。

> ⚠️ 注意：这个 `/v1` 接口是 **OpenAI 的「近似兼容」实现，不是逐字段等同**——官方兼容文档未列入 `tool_choice`、`n`、`logit_bias` 等支持字段（`tool_choice` 的部分行为与版本有关，见第 2 篇第 1 节）。具体差异与迁移前自测清单见第 2 篇开头「接口基调」。

安装失败基本都是网络问题（下载中断、文件不完整），解决方式就两种：**启动 VPN** 或 **换镜像**。

用镜像安装：

```bash
export OLLAMA_MIRROR="https://ghproxy.cn/https://github.com/ollama/ollama/releases/latest/download"
curl -fsSL https://ollama.com/install.sh | sed "s|https://ollama.com/download|$OLLAMA_MIRROR|g" | sh
```

若报 `llama-server binary not found`（安装文件不完整），删掉重装：

```bash
sudo rm -rf /usr/local/lib/ollama
curl -fsSL https://ollama.com/install.sh | sh
```

安装完成，验证服务：

```bash
curl http://localhost:11434
```

安装脚本会提示 `No NVIDIA/AMD GPU detected`——Intel 显卡不被默认识别，下一步解决。

---

## 2. 启用 Intel Arc A770 GPU 加速

Ollama 原生优先 NVIDIA（CUDA）与 AMD（ROCm）。Intel Arc 要走 **Vulkan 后端**。

### 配置

```bash
sudo systemctl edit ollama
```

填入：

```ini
[Service]
Environment="OLLAMA_VULKAN=true"
```

保存后重启：

```bash
sudo systemctl daemon-reload
sudo systemctl enable ollama   # 开机自启（多数安装脚本已自动 enable，此命令幂等可放心执行）
sudo systemctl restart ollama
```

> 只此一个变量。网上流传的 `OLLAMA_INTEL_GPU`、`OLLAMA_NUM_GPU_LAYERS` 等并未出现在 Ollama 官方支持列表里（不同版本支持情况可能变化，可以 `ollama serve --help` 输出的环境变量清单为准）。注意：`OLLAMA_VULKAN` 在部分版本（含本环境 0.33.3）不会出现在这份清单里，但该变量确实被识别生效——别因为它没被列出就怀疑配置没生效。设了 Vulkan 后 Ollama 会自动把模型全部层加载到 GPU。

### 验证

```bash
journalctl -u ollama --no-pager | grep "inference compute" | tail -3
```

看到 `Vulkan0 ... Intel Arc A770 Graphics` 即成功：

```
inference compute id=0 library=Vulkan name=Vulkan0
  description="Intel(R) Arc(tm) A770 Graphics (DG2)" type=discrete total="15.9 GiB"
```

> 若在 WSL2 且**未开启 systemd**（开启方法见第 5.6 节），或纯容器等非 systemd 环境，直接前台运行 `ollama serve`，观察终端输出的 `inference compute` 日志即可，效果相同。

> ⚠️ **Vulkan 后端有已知稳定性风险：装好能跑 ≠ 能长期稳定跑。** 上游（llama.cpp / Ollama）在部分 Linux 内核 + Mesa（Intel ANV）驱动组合下存在显存记账失步、空闲显存被换出、偶发 OOM 等记录（如 [ollama #17802](https://github.com/ollama/ollama/issues/17802)、[ollama #18272](https://github.com/ollama/ollama/issues/18272)、[llama.cpp #18946](https://github.com/ggml-org/llama.cpp/issues/18946)、[llama.cpp #25646](https://github.com/ggml-org/llama.cpp/issues/25646)），并随 Ollama/Mesa 版本持续修复。长期部署建议：
>
> - **别只看加载时的显存占用**（第 4 节表格是静态值），长时间运行要观察显存曲线是否单调上涨或异常回落；
> - 监控手段：`journalctl -u ollama` 看 OOM/换出日志；Intel 独显可用 `intel_gpu_top` 或 `xpu-smi` 观察显存；
> - 相关环境变量（写入 `sudo systemctl edit ollama`）：`OLLAMA_LOAD_TIMEOUT`（模型加载停滞多久后放弃，默认 5m）、`OLLAMA_KEEP_ALIVE`（空闲多久卸载，默认 5m）、`OLLAMA_GPU_OVERHEAD`（为驱动/其他进程预留显存）；
> - 出问题时按顺序排查：升级 Ollama（连带更新 llama.cpp 后端）→ 更新 Mesa 驱动 → 换 CUDA/ROCm 或纯 CPU 后端对比，定位是驱动问题还是后端问题。

---

## 3. 选模型

本教程默认使用 **`llama3.1:8b`**：它对工具调用格式的遵循更稳，8B 量化模型也能全量放入 A770 显存。

> 读模型名：`8b` = 80 亿参数（参数越多通常越"聪明"、越吃显存）；`Q4` 之类的量化把权重压到约 4 位，体积与显存约占 1/4、质量略降。`llama3.1:8b` 在本机落盘 4.9GB（`ollama list`）。

```bash
ollama pull llama3.1:8b
```

> 为什么不是 `qwen2.5-coder`？实测中该模型会把工具调用写成纯文本 `content`——这是**模型遵循度**问题而非模板问题，也与"Qwen 不支持工具调用"无关；完整实录、结论与模板自查见文末**附录 A**。若用新版 Ollama，请先 `ollama show <model> --modelfile` 自查模板是否已更新——模板/模型层问题可能随版本迭代修复。

> 关于内存：8B 模型（Q4 量化约 4.5GB）可全量放入 A770 的 16GB 显存（实测见本篇第 4 节）。模型主体在显存时系统内存占用很小（实测约 300MB），16GB 内存完全够用；若同时跑多个大模型或加大上下文才需要担心内存，报 `killed` 时先关闭其他大型应用，或换更小的模型（如 `qwen2.5:3b`）。

---

## 4. 实测性能（Intel Arc A770 + llama3.1:8b）

> 数据来自本机实测（Ollama 0.33.3），通过 `journalctl -u ollama` 的模型加载与计时日志读取；环境快照（2026-09 实测）：Ubuntu 24.04 LTS · 内核 7.0.0-31-generic · Mesa Vulkan 驱动 25.2.8（intel-media-va-driver 24.1.0）· Arc A770（DG2）——性能与稳定性随内核/Mesa 组合变化，本文数字以此为基线。

> ⚠️ 下表是**加载后的静态占用**，不等同于长期稳定性：Vulkan 后端在部分内核/Mesa 驱动下有显存记账失步、空闲显存被换出等已知问题，长时间运行应监控显存曲线而非只看这一时刻，方法见第 2 节。

> 先对齐计量单位：**token 是模型的最小文本单位**，不是字也不是词——本机用 `llama3.1` 分词器实测，中文 ≈ **0.78 token/字**、英文 ≈ **1.1 token/词**。速度表里的 tokens/s、以及后文的上下文窗口都以它计。复现命令：同一问题分别用 13 字与 193 字提问，对比响应 `usage.prompt_tokens`（实测差 140）。

#### 显存与内存占用

| 项目         | 实测值                   | 说明                                                                                  |
| ------------ | ------------------------ | ------------------------------------------------------------------------------------- |
| GPU 层加载   | **33/33 层全量 offload** | 整个模型都在 A770 上，非 CPU 混合（"层"= transformer 块，offload = 把层权重放进显存） |
| 模型占显存   | ~4.4 GB                  | A770 16GB 显存余量充足                                                                |
| KV Cache     | ~512 MB                  | 推理时缓存的注意力 Key/Value，随上下文长度线性增长，也占显存                          |
| 系统内存占用 | ~300 MB                  | 模型主体在显存，16GB 内存轻松扛住                                                     |

#### 推理速度（生成 14~24 个 token 的实测区间）

| 指标               | 实测值              |
| ------------------ | ------------------- |
| Prompt 处理        | ~470 ~ 700 tokens/s |
| Token 生成（eval） | **~41 tokens/s**    |

> 对比参考：纯 CPU 跑 8B 模型通常只有个位数到十几 tokens/s，A770 的 Vulkan 加速收益明显。41 tokens/s 对交互式对话够用，属于"能正常用"的水平，谈不上飞快。

> 补充：上表速度来自第 2 篇那种几十 token 的小请求场景，实际耗时主要是 prompt 处理而非生成，整体响应在 1 秒内完成。

> **长上下文与 KV Cache**：上面的性能数据来自短请求。长上下文场景下，KV Cache 会随 token 数线性增长——32K tokens 下占用可达数 GB（A770 16GB 对 8B 模型日常交互够用，但长上下文场景需关注显存余量）。可通过 `OLLAMA_CONTEXT_LENGTH`（默认 4k/32k/256k 按显存自适应）限制最大上下文长度，避免显存溢出。

> 📌 **只想跑代码？** 读到这里就够了——直接跳到第 2 篇。第 5 节的运维内容是给"准备长期运行"的读者看的，以后再回来不迟。

---

## 5. 长期运维手册（运维向 · 可先跳过，上线前必读）

> 本节是**运维向内容**，面向需要长期部署与维护这台机器的读者（SRE/DevOps 视角）：环境变量、监控、Mesa 驱动排查属于运维知识。**初次跟代码的读者可以先跳过**——不影响第 2~5 篇（第 2 篇只用到"模型已拉好、服务正常"）；但本节是本系列**最独特的稳定性知识**（Vulkan 已知风险、监控与排查不会自己出现在代码里），**准备长期运行/上线前，务必回来精读**。

> 本节承接第 2 节的已知风险，给出一套可照做的长期运维流程。先说结论：长期稳定运行没有捷径，就是监控指标、出问题按顺序排查、及时升级到官方修复版本。

### 5.1 先给症状分级（判断是不是 Vulkan 的锅）

| 症状                                         | 更像谁的问题                        | 先看哪                                                                                           |
| -------------------------------------------- | ----------------------------------- | ------------------------------------------------------------------------------------------------ |
| 生成变慢/卡顿（tokens/s 下滑）               | 显存被换出/接近满载                 | 第 5.2 节监控；上游记录见 [llama.cpp #25646](https://github.com/ggml-org/llama.cpp/issues/25646) |
| 显存单调上涨、偶发 OOM / `Not enough memory` | Vulkan/驱动显存记账问题             | 第 5.2 节 + 升级 Ollama/Mesa                                                                     |
| 服务直接崩溃退出                             | 多为后端崩溃                        | `journalctl -u ollama` 崩溃段                                                                    |
| 模型加载久/超时                              | 加载停滞（驱动慢、首次编译 shader） | 第 5.3 节 `OLLAMA_LOAD_TIMEOUT`                                                                  |

### 5.2 监控三件套（命令与看什么）

- **journalctl（主）**：`journalctl -u ollama -f` 实时看；过滤异常用 `journalctl -u ollama --no-pager | grep -iE "error|out of memory|failed"`，注意模型加载/换出日志与 OOM 段；
- **intel_gpu_top（辅）**：Debian/Ubuntu 安装 `intel-gpu-tools` 后运行，观察 VRAM 占用曲线与渲染引擎占用。建议跑一个长任务 30~60 分钟，确认显存占用会随请求回落而不是单调上涨；
- **备用**：`xpu-smi`（若装 Intel 工具链）或直接看 Ollama 日志中的 GPU 统计。

> Vulkan 下没有 `nvidia-smi` 那样标准的显存工具，本机以 journalctl 为主、intel_gpu_top 为辅即可。

### 5.3 环境变量速查（写入 systemd override）

```bash
sudo systemctl edit ollama
```

```ini
[Service]
Environment="OLLAMA_KEEP_ALIVE=10m"
Environment="OLLAMA_LOAD_TIMEOUT=10m"
Environment="OLLAMA_GPU_OVERHEAD=1073741824"
```

| 变量                       | 默认            | 含义 / 何时调                                                                        |
| -------------------------- | --------------- | ------------------------------------------------------------------------------------ |
| `OLLAMA_KEEP_ALIVE`        | 5m              | 模型空闲多久卸载。频繁调用可调大（如 10m）减少重复加载                               |
| `OLLAMA_LOAD_TIMEOUT`      | 5m              | 模型加载停滞多久后放弃。驱动慢/首次编译着色器久时可调大                              |
| `OLLAMA_GPU_OVERHEAD`      | 0               | 为驱动/桌面进程预留显存（字节）。显存接近满时预留可避免 OOM                          |
| `OLLAMA_MAX_LOADED_MODELS` | 每 GPU 1        | 同显存跑多个模型时控制换入换出                                                       |
| `OLLAMA_HOST`              | 127.0.0.1:11434 | 改监听地址/端口。**改成 `0.0.0.0` 等于把无鉴权的推理接口暴露到内网**，务必配合防火墙 |
| `OLLAMA_NUM_PARALLEL`      | 按显存自动      | 同时处理的请求数；第 4 篇的常驻服务被多人调用时要看它                                |
| `OLLAMA_MAX_QUEUE`         | 512             | 排队上限，超出直接拒绝；配合上一行一起调                                             |

> 完整清单以 `ollama serve --help` 输出的环境变量为准（第 2 节也提过这一点，版本不同支持情况可能变化）；`OLLAMA_VULKAN` 是否出现在清单里同样随版本而定，见第 2 节。

### 5.4 出问题后的排查顺序

1. 先判断是回归还是环境变化：`journalctl -u ollama --since today` 找最近一次「正常 → 异常」的转折点；
2. **升级 Ollama**（连带更新 llama.cpp 后端）→ 复测。很多 Vulkan 问题在发布说明里标注了修复版本；
3. 仍复现 → 更新 **Mesa / 内核**（Intel ANV 驱动在 Mesa 里）→ 复测；
4. 仍复现 → **换后端对照**：临时设 `OLLAMA_VULKAN=false` 纯 CPU 跑一次，判断是驱动路径还是后端逻辑；
5. 升级后仍怀疑上游 bug → 带着最小复现（模型、请求、日志）去 [ollama/ollama](https://github.com/ollama/ollama/issues) 或 [llama.cpp](https://github.com/ggml-org/llama.cpp/issues) 搜同款 issue。

怎么查自己的 Mesa 版本：Ubuntu/Debian 用 `dpkg -l | grep mesa-vulkan-drivers`；想看运行态驱动名可装 `vulkan-tools` 后 `vulkaninfo | grep -i driverName`。升级目标怎么定：上游 issue / 发布说明通常会标注“修复于 Mesa X.Y / Ollama vX”，以该版本为升级目标即可，不必盲目追最新。本文不给出“通用最低 Mesa 版本”——稳定性与内核、发行版打包强相关，请以自身环境实测与上游标注为准。

### 5.5 常规健康检查（发布/开机前跑一遍）

```bash
curl -s http://localhost:11434/api/version          # 服务活着
ollama list                                          # 模型都在
journalctl -u ollama --no-pager | grep "inference compute" | tail -1   # 仍是 Vulkan
ollama ps                                            # 空闲 10 分钟后应为空（KEEP_ALIVE 生效）
```

### 5.6 服务管理：自启、关闭与崩溃自愈

前面的配置都假设服务"一直在跑"，但**重启电脑后它回不回来？** 先查三件事：

```bash
systemctl is-enabled ollama         # enabled = 开机自启已开启
systemctl status ollama             # active (running) + 最近日志
sudo systemctl enable --now ollama  # 没自启就这样开启并立即启动（幂等，多数安装脚本已自动执行）
```

- 不需要开机自启时用 `sudo systemctl disable ollama` 关闭；
- 官方安装脚本装出的 `ollama.service` 自带 `Restart=always`：**进程崩溃会被 systemd 自动拉起**。所以遇到第 5.1 节"服务直接崩溃退出"时，先 `journalctl -u ollama` 找根因，别因为"它自己又活了"就略过；
- **WSL2 首选：开启 systemd**。新版 WSL2 支持 systemd：在 `/etc/wsl.conf` 的 `[boot]` 段写入 `systemd=true`，随后 `wsl --shutdown` 并重新进入发行版；之后与本文完全一致（`systemctl enable --now ollama`、`journalctl -u ollama` 照用）；
- **无法用 systemd 的环境（WSL1、容器等）**：用 `tmux new -d 'ollama serve'`（或 `screen`）让会话后台保活，需要看日志时 `tmux attach`；`nohup ... &` 只适合临时验证，不建议作为常驻方案。

---

## 附录 A：qwen2.5-coder 工具调用实录（为什么默认选 llama3.1）

> 本附录解释第 3 节“默认选 llama3.1”的原因；失败输出样例见第 2 篇「运行结果」的对比小节。

现象：同一份代码换用 `qwen2.5-coder:7b`（Ollama 0.33.3），模型把工具调用以 JSON 文本写进 `content`，而不是标准的 `tool_calls` 字段。这不是 Qwen 不支持 Function Calling——Ollama 能否把 `tools` 正确解析成 `tool_calls`，取决于模型的聊天模板与后端配合。实测发现要分两层看：

1. **模板层（通常已不是问题）**：官方 `qwen2.5-coder:7b` 的 TEMPLATE 已自带完整工具调用格式（`ollama show qwen2.5-coder:7b --modelfile` 可见 `<tool_call>` 指令段）；
2. **模型层（真正的坑）**：无论走 `/v1`、原生 `/api/chat` 还是 `temperature=0`，该模型都稳定把 JSON 写进 `content`——属于**模型遵循度**问题（2026 年社区仍有同类报告），改模板无法解决。

结论按型号区分：想留用 Qwen，先试 **`qwen2.5`（instruct 版）**或升级新版 Ollama 后重测；想最快跑通就用本教程默认的 `llama3.1:8b`。

> 结论范围：以上实录限定于 **Ollama 0.33.3 + 官方 `qwen2.5-coder:7b`**（`temperature=0` 亦复现）；其他 Ollama 版本或采样参数（temperature/seed）下表现可能不同，请以自身环境的实测为准。

### 自查：模型模板是否带工具格式

- `ollama show <model> --modelfile | grep -n "Tools\|tool_call"`：能看到 `.Tools` / `<tool_call>` 说明模板本身支持工具调用；
- 官方库模型的模板随库维护更新：先 `ollama pull` 拉最新，再 `ollama show` 对比；
- 需要自定义模板/参数时用 Modelfile 派生新模型：`FROM <原模型>` + 覆盖 `TEMPLATE`、`PARAMETER`，`ollama create` 后 API 改用新模型名——注意：模板只决定提示词长什么样，模型是否遵守是另一回事；
- 想理解"模板为什么会让模型吐 JSON 文本"、"工具结果为什么被渲染成 `ipython` 块"，见下方**附录 B**。

---

## 附录 B：模型实际看到什么（tools 是被模板渲染进提示词的）

前面几篇一直在往请求里塞 `tools` 数组，但模型**看不到 JSON 字段**——它只看到一串文本。`tools` 之所以管用，是因为 Ollama 用**模型自带的聊天模板**把它们渲染进了提示词。理解这一层，第 2/3 篇那些"铁律"为什么必须遵守、以及第 2 篇 §4「模型把 JSON 写进 `content`」的怪现象，就都讲得通了。

先看模板（本机 Ollama 0.33.3 + `llama3.1:8b` 实测，你也可以用同一条命令自查自己的模型）：

```bash
ollama show llama3.1:8b --modelfile | sed -n '/^TEMPLATE/,/^"""/p'
```

<details>
<summary>llama3.1:8b 的 TEMPLATE 原文（点击展开；下面四条结论全部出自它）</summary>

```text
TEMPLATE """{{- if or .System .Tools }}<|start_header_id|>system<|end_header_id|>
{{- if .System }}

{{ .System }}
{{- end }}
{{- if .Tools }}

Cutting Knowledge Date: December 2023

When you receive a tool call response, use the output to format an answer to the orginal user question.

You are a helpful assistant with tool calling capabilities.
{{- end }}<|eot_id|>
{{- end }}
{{- range $i, $_ := .Messages }}
{{- $last := eq (len (slice $.Messages $i)) 1 }}
{{- if eq .Role "user" }}<|start_header_id|>user<|end_header_id|>
{{- if and $.Tools $last }}

Given the following functions, please respond with a JSON for a function call with its proper arguments that best answers the given prompt.

Respond in the format {"name": function name, "parameters": dictionary of argument name and its value}. Do not use variables.

{{ range $.Tools }}
{{- . }}
{{ end }}
Question: {{ .Content }}<|eot_id|>
{{- else }}

{{ .Content }}<|eot_id|>
{{- end }}{{ if $last }}<|start_header_id|>assistant<|end_header_id|>

{{ end }}
{{- else if eq .Role "assistant" }}<|start_header_id|>assistant<|end_header_id|>
{{- if .ToolCalls }}
{{ range .ToolCalls }}
{"name": "{{ .Function.Name }}", "parameters": {{ .Function.Arguments }}}{{ end }}
{{- else }}

{{ .Content }}
{{- end }}{{ if not $last }}<|eot_id|>{{ end }}
{{- else if eq .Role "tool" }}<|start_header_id|>ipython<|end_header_id|>

{{ .Content }}<|eot_id|>{{ if $last }}<|start_header_id|>assistant<|end_header_id|>

{{ end }}
{{- end }}
{{- end }}"""
```

</details>

同一件事在三个层次的样子：

| 层次                 | 长什么样                                                                                                                                                                                                                                      |
| -------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| ① Go 里写的结构体    | `Tool{Type: "function", Function: FunctionDef{Name: "get_current_time", Description: "获取当前的日期和时间", Parameters: …}}`                                                                                                                 |
| ② 发出去的请求 JSON  | `"tools": [{"type": "function", "function": {"name": "get_current_time", "description": "…", "parameters": {"type": "object", …}}}]`                                                                                                          |
| ③ 模型真正读到的文本 | 系统块尾部多了一句 *"You are a helpful assistant with tool calling capabilities."*；最后一条 user 消息前面多了 *"Given the following functions, please respond with a JSON …"*，接着是每个工具的 JSON 定义，最后才是 `Question: 现在几点了？` |

> ③ 是按上面模板手工渲染的示意（字段顺序与空白随版本略有差异，不影响结论）。服务端真实渲染出的提示词要开 `OLLAMA_DEBUG` 才能从日志里看到，本系列不把它作为前置条件。

从模板原文能读出四条结论：

1. **`tools` 只在"最后一条 user 消息"处展开**：模板里的 `{{ if and $.Tools $last }}` 决定工具清单跟着最后一条 user 消息走，历史里早先的 user 消息旁边并没有工具清单。所以每次请求都得把 `tools` 重新带上——服务端无状态，模板每次都要重渲染。
2. **`role="tool"` 在提示词里被渲染成 `ipython` 块**：工具结果落在 `<|start_header_id|>ipython<|end_header_id|>` 里（llama3.1 训练时用的就是这个名字）。这就是第 2/3 篇反复强调"`role="tool"` 消息必须紧跟对应的 `assistant(tool_calls)`、顺序反了上游直接校验失败"的底层原因：模板按消息顺序逐条渲染，错位渲染出来的就是另一串文本。
3. **提示词本身就在要求模型"输出 JSON"，`tool_calls` 是服务端事后解析出来的**：模板写着 *"Respond in the format {"name": function name, "parameters": dictionary of argument name and its value}"*。模型若不按这个格式吐文本（或吐出的格式解析器不认），API 层就解析不出 `tool_calls`，这段 JSON 便原样留在 `content` 里——第 2 篇 §4 的 `qwen2.5-coder` 输出、附录 A 的"模型遵循度"结论到这里闭环。（qwen2.5-coder 的模板改用 `<tool_call></tool_call>` 标记，同样可以用上面的命令自查。）
4. **命名不一致是常态**：模板里叫 `parameters`，API 响应里叫 `arguments`；`tools[].function` 与 `tool_calls[].function` 也不同义（第 2 篇 §3）。跨模型、跨服务商迁移时，这类细节正是"近似兼容"的成本来源。

**这层知识怎么用**：模型不按预期调工具时，先看它的模板在怎么教它（附录 A 的自查命令），再决定是换模型、还是把格式要求写进工具描述；而不是先怀疑自己的 Go 代码。

---

## 附录 C：术语与概念速查（读代码前扫一眼）

按"这个概念是什么 → 相关实测数字 → 在哪篇用到"排；正文首次出现处也都有短注。

| 术语                           | 一句话                                                                                         | 详见       |
| ------------------------------ | ---------------------------------------------------------------------------------------------- | ---------- |
| token                          | 模型的最小文本单位：中文 ≈ **0.78 token/字**、英文 ≈ **1.1 token/词**（`llama3.1` 分词器实测） | §4         |
| prompt / completion tokens     | 输入与输出各占多少 token，响应 `usage` 里直接给出                                              | 第 2 篇 §2 |
| 上下文窗口                     | 一次请求的 token 上限（输入+输出共享），Ollama 默认 **4096** ≈ 5,000 中文字                    | §4         |
| 工具定义的 token 成本          | 单个工具约 **+134 tokens**（同问题带 tools 149 vs 不带 15），且**每轮重发**                    | 第 3 篇 §4 |
| `finish_reason`                | 这轮为何结束：`stop` 正常 / `tool_calls` 要调工具 / `length` 被 `max_tokens` 截断              | 第 2 篇 §1 |
| `max_tokens` vs `options`      | `/v1` 用顶层 `max_tokens`，**会静默忽略 `options`**（实测）；原生 `/api/chat` 才用 `options`   | 第 2 篇 §1 |
| temperature / `top_p`          | 采样随机性：0 = 贪心解码（尽量可复现），默认更随机                                             | 第 2 篇 §3 |
| 聊天模板（chat template）      | 模型自带的"把 messages + tools 拼成一段文本"的模板；模型只读文本                               | 附录 B     |
| system prompt                  | 模板固定注入的指令段（带工具时 llama3.1 会注入"你有工具调用能力"）                             | 附录 B     |
| JSON Schema                    | 描述工具参数形状的 JSON 约定（`type`/`properties`/`required`），只是给模型读的文本             | 第 3 篇 §2 |
| 量化（Q4 等）                  | 权重压到约 4 位：体积/显存约 1/4、质量略降；`8B` = 80 亿参数，落盘 4.9GB                       | §3         |
| offload / 层                   | "层" = transformer 块；offload = 把层权重放进显存                                              | §4         |
| KV Cache                       | 缓存的注意力 Key/Value，随上下文线性增长（4096 上下文约 **512MB** 显存）                       | §4         |
| 无状态（stateless）            | 服务端不记对话，多轮靠客户端把历史整段重发                                                     | 第 3 篇 §4 |
| 流式 / SSE                     | 边生成边推 token；SSE = Server-Sent Events，浏览器原生                                         | 第 4 篇 §2 |
| ndjson                         | newline-delimited JSON：一行一个 JSON 对象（Ollama 原生接口的流式格式）                        | 第 4 篇 §1 |
| embedding / 余弦相似度 / topK  | 文本→向量；用夹角衡量"像不像"；按分数排序取前 K 条                                             | 第 5 篇 §3 |
| RAG                            | Retrieval-Augmented Generation（检索增强生成）：先检索资料，再让模型照着答                     | 第 5 篇    |
| 分块 / 重排                    | 长文档切小片（相邻留重叠）；先粗排再精排                                                       | 第 5 篇 §7 |
| 幻觉 / 遵循度                  | 编造事实 / 按格式与指令行事的能力（决定工具调用稳不稳）                                        | 附录 A     |
| Agentic 循环                   | 让模型自己决定调几次工具：`while tool_calls 非空`                                              | 第 3 篇 §1 |
| `tool_calls` 非空（判据）      | 判"要不要调工具"只看 `tool_calls` 是否非空，`finish_reason` 仅作校验                           | 第 2 篇 §1 |
| `role="tool"` 必须带 `content` | 工具结果消息省略 `content` 会被上游判 400（实测）                                              | 第 2 篇 §3 |

---

## FAQ：常见问题速查（环境与运维）

| 问题                                       | 原因                                                       | 解决                                                                                                       |
| ------------------------------------------ | ---------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| 启动服务报 `llama-server binary not found` | 安装文件不完整                                             | `sudo rm -rf /usr/local/lib/ollama` 后重跑安装脚本                                                         |
| 端口 `11434` 被占用                        | 另一个 Ollama 实例在运行                                   | `ps aux \| grep ollama` 找到并停掉旧进程                                                                   |
| 重启电脑后 `curl localhost:11434` 连不上   | 服务未设为开机自启                                         | `sudo systemctl enable --now ollama`（WSL2/无 systemd 见第 5.6 节）                                        |
| 日志显示层全部在 CPU（offload 为 0 层）    | `OLLAMA_VULKAN` 未设置或未重启服务                         | 按第 2 节配置并 `systemctl restart ollama`                                                                 |
| 运行时报 `killed` 或内存溢出               | 同时跑多个大模型或上下文设得过大                           | 关闭大型应用，或换更小的模型                                                                               |
| 长时间运行显存上涨、OOM 或生成变慢         | Vulkan 后端在部分内核 + Mesa 驱动下有显存记账失步/换出问题 | 升级 Ollama 与 Mesa；`journalctl -u ollama`、`intel_gpu_top` 监控；可设 `OLLAMA_GPU_OVERHEAD`（第 5.3 节） |
| 模型加载卡住 / 等很久没反应                | 驱动或后端问题导致加载停滞                                 | 设 `OLLAMA_LOAD_TIMEOUT` 并看日志定位（第 5.3 节）                                                         |

---

## 结论

1. **A770 走 Vulkan 可以全量 offload 8B 模型**：33/33 层、~4.4GB 显存、~41 tokens/s，非 NVIDIA 入门成立；
2. 环境篇到此齐活：镜像安装 → Vulkan → 选模型 → 性能实测 → 长期运维手册；
3. **本文是入门教程，不是生产部署模板**：Vulkan 稳定性风险真实存在，升级与监控是常态动作而非可选项（第 2 节风险清单 + 第 5 节手册），其中第 5 节是部署前必读内容。

### 给第 2 篇读者的环境自检清单（全绿再往下读）

- [ ] `curl http://localhost:11434` 返回正常
- [ ] `ollama list` 里有 `llama3.1:8b`
- [ ] `journalctl -u ollama | grep "inference compute"` 显示 Vulkan + Arc A770
- [ ] 知道 `OLLAMA_LOAD_TIMEOUT`/`OLLAMA_KEEP_ALIVE`/`OLLAMA_GPU_OVERHEAD` 存在且在哪里配置

下一篇预告：**《最小代码：单工具一轮调用的完整闭环》**——开始写第一个 Go Agent。
