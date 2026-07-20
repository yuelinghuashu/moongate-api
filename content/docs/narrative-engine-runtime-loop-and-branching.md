---
title: 叙事引擎与大模型：运行时闭环与多分支存档
description: 契约在手，怎么让它活起来？三明治 Prompt 结构、流式全角缩进、Mother-Child 分支存档、异步记忆提取——构建完整的运行时闭环。
permalink: e5f9b2c3-4d6e-5f7a-8b9c-2c3d4e5f6a7b
date: 2026-07-20 23:00:00
series: narrative-engine
level: P3
tags:
  - Go
  - LLM
  - Engineering
---

> **前置阅读**：这是本系列的最后一篇，建议先读前四篇——第一篇了解 `.meph` 格式，第二、三篇理解解析器如何输出 `domain.Contract`，第四篇理解测试如何保障行为稳定。本篇假设你已经知道引擎拿到了 `contract` 结构体，要解决的核心问题是：**怎么驱动它运转起来？**

前面四篇解决了一个问题：**把 `.meph` 契约变成可执行的数据结构**。

现在契约在手——角色名、状态初始值、规则列表、世界观文本——但它是死的。它不会说话，不会决策，不会叙事。

这篇要解决的是：**怎么让这份静态契约活起来？**

引擎拿到 `contract` 后进入一个循环：接收用户输入 → 匹配规则 → 执行动作 → 调用 LLM → 返回响应。这个循环构成了引擎的运行时。

但在实现这个循环之前，有几个工程问题必须先解决——它们决定了引擎是“能跑”还是“能用”。

## 一、小说级终端流：全角缩进与流式拦截

LLM 的流式输出是逐块返回的。引擎通过 `onChunk` 回调拦截每一块，然后直接写到终端。

这里有一个设计细节：**全角缩进（`　　`）**。

```go
onChunk := func(chunk string) {
    for _, ch := range chunk {
        if ch == '\n' {
            fmt.Println()
            needIndent = true
            inParagraph = false
        } else {
            if !inParagraph && needIndent {
                fmt.Print("　　")  // 每个段落开头空两格
                needIndent = false
            }
            fmt.Print(string(ch))
            inParagraph = true
        }
    }
}
```

效果：终端输出的文本，每个自然段开头都有两个全角空格，看起来像一本实体书。这个细节对创作者的体验提升是巨大的——它把“终端输出”变成了“小说页面”。

但流式输出背后有一个更关键的工程决策：**动作执行器统一处理流式回调**。

- 状态修改动作：立即返回结果，然后通过回调模拟逐字符输出
- LLM 动作：真正的流式输出，逐块回调
- 静态文本：立即返回，然后模拟流式输出

所有动作最终都通过 `onChunk` 输出，不管来源是什么。这保证了终端的输出体验是一致的。

## 二、Prompt 三明治：把约束焊死在上下两端

LLM 叙事最大的问题是格式跑偏——它经常输出括号剧本流：

```
（冷笑一声）【贝利亚】：你们太弱了。
```

这破坏了沉浸感。解决方案是**把格式约束放在 Prompt 的顶部和底部**，形成三明治结构：

```
【格式硬性要求】
（NarrativeConstraints——禁止括号、禁止剧本标记、禁止独角戏）

【世界观】
（context）

【当前状态】
（context）

【你记得的过往】
（context）

【此刻】
（user_input）

【要求】
（NarrativeConstraints，再次强调）
```

约束在两端出现两次，中间夹着上下文。这样做有两个原因：

1. **首因效应**：LLM 最先看到约束，优先级最高
2. **近因效应**：LLM 最后看到的约束会影响最终输出

两次强调，确保格式约束不会被中间的上下文稀释。

### 2.1 确定性渲染：排序保证 Cache 命中

这里有一个微妙的工程问题：Go 的 `map` 遍历顺序是随机的。

`Prompt` 中的 `【当前状态】` 部分渲染自 `map[string]any`。如果每次遍历顺序都不同，生成的 Prompt 文本就会变化——哪怕状态内容完全一样。这会导致 **LLM 的 Prompt Cache 完全失效**，每次都要重新计算。

解决方案：对 `state` 的键做排序，确保渲染顺序稳定：

```go
func renderStateList(state map[string]any) string {
    keys := make([]string, 0, len(state))
    for k := range state {
        keys = append(keys, k)
    }
    sort.Strings(keys)
    // 按排序后的键依次渲染
}
```

### 2.2 反独角戏约束

`NarrativeConstraints` 中有一条被刻意强调：

> 每段回复必须包含至少两名其他角色（非玩家）的对话和动作反应。如果场景中没有其他角色，请引入或创造至少一个互动对象。禁止只有玩家独角戏。

这是为了解决叙事中的“空旷感”。如果 LLM 只回应玩家输入，不引入其他角色互动，故事会变成单人独白，失去戏剧张力。这条约束强制 LLM 在每轮回复中至少引入一个互动对象，让世界活起来。

## 三、Mother-Child 存档机制

每一轮对话结束后，引擎会自动保存当前状态到子版文件。

命名规则：

- 母版 `story.meph` → 默认子版 `story_child.meph`
- 分支 `-branch dark` → `story_dark.meph`

```go
func BuildChildPath(filename string, branch string) string {
    dir := filepath.Dir(filename)
    name := strings.TrimSuffix(filepath.Base(filename), ext)
    if branch != "" {
        return filepath.Join(dir, name+"_"+branch+ext)
    }
    return filepath.Join(dir, name+"_child"+ext)
}
```

子版文件是完整的 `.meph` 契约，包含：

- 母版的所有静态区块（角色名、世界观、背景、规则）
- 更新后的 `【状态】`
- 累积的 `【记忆】`
- 最近的 `【历史】`

这意味着一份静态契约可以演化出无数个动态子版：

```
story.meph (母版，只读)
    ├── story_child.meph (主线存档)
    ├── story_dark.meph (黑暗分支)
    ├── story_light.meph (光明分支)
    └── story_experimental.meph (实验分支)
```

每个分支独立演化，互不影响。

**加载时**：

- 默认加载子版（如果存在）
- `-reset` 忽略子版，从母版重新开始
- `-branch dark` 加载对应的分支文件

**这个设计的价值**：创作者可以在关键时刻分叉故事线，探索不同走向，而不丢失任何进度。

## 四、异步记忆提取：不阻塞主对话

记忆提取是长线叙事的关键——它把关键事件从对话历史中提取出来，压缩后长期保存，在每一轮中注入 LLM 上下文。

但提取需要调用 LLM，会耗时数秒。如果同步执行，用户每 5 轮就要等几秒才能继续对话。

解决方案：**独立 Context，非阻塞执行**。

```go
// 每轮对话结束后
if s.engine != nil {
    memCtx, memCancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer memCancel()

    // 在后台 goroutine 中执行，不阻塞主流程
    go func() {
        if err := s.engine.ProcessMemories(memCtx, input, response); err != nil {
            // 提取失败只记录提示，不中断对话
            fmt.Printf("\n⚠️ 记忆提取失败: %v\n", err)
        }
    }()
}
```

30 秒超时保护，确保记忆提取不会无限卡住。提取失败只提示、不中断——对话可以继续，只是本轮的记忆没有被保存。

## 五、完整闭环

把所有部分串起来，引擎的每一轮对话是这样运转的：

```
用户输入
    │
    ▼
规则匹配（条件评估器求值）
    │
    ├── 匹配成功 → 执行动作
    │       ├── 注入 → 追加到记忆，继续调用 LLM
    │       ├── 状态修改 → 更新状态，返回确认
    │       └── LLM → 调用 LLM 生成响应
    │
    └── 无匹配 → 调用 LLM 自由叙事
    │
    ▼
流式输出（全角缩进 + 逐块回调）
    │
    ▼
（后台）记忆提取（每 5 轮触发一次）
    │
    ▼
自动保存到子版文件
    │
    ▼
等待下一轮输入
```

这就是 Mephisto 的运行时。

## 六、代价与局限

这套机制不是没有代价的：

**1. 记忆提取依赖 LLM 质量**

如果主模型状态不佳，提取的摘要可能跑偏。DeepSeek 和 GPT-4 质量稳定，但 7B 本地模型摘要质量明显下降。对于生产环境，建议用高质量模型做提取，即使主对话用了轻量模型。

**2. 分支切换需要手动管理**

子版文件是独立存储的，切换分支需要用户主动指定 `-branch`。不像真正的版本控制有 diff 和 merge，分支之间的内容不会自动同步。

**3. 流式输出占用终端**

全角缩进和流式输出在终端看起来很好，但如果用户想复制粘贴文本，缩进和换行符会一起被复制——有时会产生干扰。

## 七、小结

五篇走完了一条完整的路径：

| 篇目   | 解决的问题           | 核心产出                    |
| ------ | -------------------- | --------------------------- |
| 第一篇 | 用什么格式写规则？   | `.meph` 格式设计            |
| 第二篇 | 怎么精确解析并报错？ | 区块扫描器 + Parser         |
| 第三篇 | 规则和变量怎么解析？ | 规则表达式 + 插值语法       |
| 第四篇 | 怎么保证不改坏？     | Golden File + 验证器        |
| 第五篇 | 怎么让契约活起来？   | Prompt 三明治 + 分支 + 记忆 |

合在一起，就是一个完整的长线叙事引擎：

```
契约（.meph）
    │
    ▼
解析器（第二、三篇）──→ Contract
    │
    ▼
验证器（第四篇）──→ 结构保证
    │
    ▼
引擎（第五篇）──→ 规则匹配 + LLM 叙事
    │
    ▼
子版存档──→ 长期连续性 + 多分支
```

> 项目地址：[https://github.com/yuelinghuashu/mephisto](https://github.com/yuelinghuashu/mephisto)
