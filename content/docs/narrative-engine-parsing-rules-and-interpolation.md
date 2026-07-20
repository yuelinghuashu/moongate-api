---
title: 叙事引擎与大模型：规则表达式与插值语法的解析
description: 规则的条件-动作表达式怎么拆解？{变量} 插值语法怎么处理？本文覆盖解析层最后两块拼图——让规则从文本变为可执行结构。
permalink: c7f60196-3aa2-4191-9796-544aa5ba7a3e
date: 2026-07-20 19:00:00
series: narrative-engine
level: P3
tags:
  - Go
  - DSL
  - LLM
  - Engineering
---

> **前置阅读**：建议先读第二篇的“二、两阶段设计”和“三、区块扫描器”，理解扫描器如何输出 `[]Block`。本篇假设你已经知道 Parser 如何根据 `Title` 路由到不同解析函数。

前两篇完成了格式设计和区块切分。现在要处理解析层最复杂的两个语法单元：**规则的条件-动作表达式**，以及 **`{变量}` 插值语法**。

规则区块长这样：

```meph
[攻击] if 包含 "攻击" -> 注入 "贝利亚发动了猛烈的攻击"
[光之国] if 包含 "光之国" && 状态.情绪 == "暴怒" -> LLM: 以嘲讽语气回应
[高堕落] if 状态.堕落指数 > 80 -> 状态.情绪 = "癫狂"
```

插值语法出现在规则动作和文本区块中：

```meph
注入 "{角色名}的故乡是光之国"
```

这一篇要解决两个问题：

1. **规则的条件和动作怎么拆解成可存储的结构？**
2. **插值语法怎么在解析层被识别和处理？**

## 一、规则解析：拆解条件-动作

规则格式固定：`[规则名] if 条件 -> 动作`

**关键设计决策：解析器只做“拆解”，不做“求值”。**

条件字符串（如 `包含 "攻击" && 状态.堕落指数 > 80`）被原样存入 `domain.Rule.Cond`。引擎运行时会调用 `ConditionEvaluator` 去真正求值。解析和评估是两个独立的阶段，中间隔着清晰的边界。

### 1.1 规则名提取

找到第一个 `[` 和第一个 `]`，取中间内容：

```go
func parseRuleLine(line string, lineNumber int) (*domain.Rule, error) {
    trimmed := strings.TrimSpace(line)
    if !strings.HasPrefix(trimmed, "[") {
        return nil, fmt.Errorf("规则必须以 '[' 开头")
    }
    idx := strings.Index(trimmed, "]")
    if idx == -1 {
        return nil, fmt.Errorf("缺少闭合的 ']'")
    }
    name := strings.TrimSpace(trimmed[1:idx])
    if name == "" {
        return nil, fmt.Errorf("规则名不能为空")
    }

    rest := strings.TrimSpace(trimmed[idx+1:])
    // 接下来提取条件和动作...
}
```

### 1.2 条件与动作的拆分

用 `if` 和 `->` 作为分隔符：

```go
// 去掉 "if " 前缀
if !strings.HasPrefix(rest, "if ") {
    return nil, fmt.Errorf("规则条件必须以 'if ' 开头")
}
rest = strings.TrimPrefix(rest, "if")
rest = strings.TrimSpace(rest)

// 取第一个 "->" 分割条件和动作
cond, action, ok := strings.Cut(rest, "->")
if !ok {
    return nil, fmt.Errorf("规则缺少 '->'")
}
cond = strings.TrimSpace(cond)
action = strings.TrimSpace(action)
```

### 1.3 互斥组

动作中可能带有 `[group:xxx]` 标记：

```
[攻击] if 包含 "攻击" -> [group:combat] 注入 "贝利亚发动了猛烈的攻击"
```

解析时提取组名，存入 `domain.Rule.Group`：

```go
group := ""
if strings.HasPrefix(action, "[group:") {
    endIdx := strings.Index(action, "]")
    if endIdx != -1 {
        group = action[7:endIdx]
        action = strings.TrimSpace(action[endIdx+1:])
    }
}
```

互斥组的作用是：同一组内多条规则，只有第一条匹配的会被触发。这个逻辑在引擎运行时生效，解析层只需存好组名。

### 1.4 引号处理

条件和动作中的字符串用 `"` 包裹。解析时用 `unquote` 剥离外层引号：

```go
func unquote(s string) (string, error) {
    s = strings.TrimSpace(s)
    if len(s) >= 2 {
        if (strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"")) ||
           (strings.HasPrefix(s, "“") && strings.HasSuffix(s, "”")) {
            return s[1:len(s)-1], nil
        }
    }
    return s, nil
}
```

支持中文引号是因为创作者可能使用中文输入法，`“` 和 `”` 比 `"` 更容易自然输入。

## 二、插值语法：{变量名} 的识别与替换

插值的核心需求：**在任意文本中识别 `{变量名}` 并替换为对应的值。**

变量来源有两个：

1. **角色名**：`{角色名}` 来自 `【角色名】` 区块
2. **状态变量**：`{情绪}`、`{堕落指数}` 来自 `【状态】` 区块

### 2.1 关键决策：解析器不负责替换

这是整个插值系统最重要的设计决策。

如果解析器在加载时就把 `{堕落指数}` 替换成 `50`，这个值就被“冻住”了。但运行中堕落指数可能涨到 `85`——那时插值结果就错了。

所以解析器的职责是：**把 `{变量名}` 当作普通文本原样存储**。运行时由专门函数完成替换：

```go
func ReplacePlaceholders(template string, vars map[string]string) string {
    for k, v := range vars {
        template = strings.ReplaceAll(template, "{"+k+"}", v)
    }
    return template
}
```

只有 7 行，但够用了——需求就是“把 A 替换为 B”，不需要条件、循环、函数调用。

### 2.2 为什么不用 text/template？

Go 标准库的 `text/template` 功能强大，支持条件、循环、函数调用。但我刻意避开了它。

`text/template` 的写法是：

```
{{.RoleName}}的堕落指数已达 {{.CorruptionLevel}}
```

而 `.meph` 的写法是：

```
{角色名}的堕落指数已达 {堕落指数}
```

区别在于：创作者不需要理解“点号表示法”和“模板上下文”，他们只需要看到花括号就知道这是一个变量。**插值语法是设计给故事作者看的，不是给 Go 程序员看的。** 更复杂的语法意味着更多的学习成本和错误机会。

### 2.3 替换时机：两个阶段

1. **静态文本显示时**：`【世界观】`、`【开局场景】` 在会话启动时替换一次，之后不再变化。
2. **规则动作执行时**：`注入 "{角色名}的堕落指数已达 {堕落指数}"` 在每次触发时重新计算，反映最新状态。

## 三、交汇点：当规则遇上插值

以这条规则为例：

```meph
[光之国] if 包含 "光之国" -> 注入 "{角色名}的故乡是光之国"
```

完整执行流程：

```
用户输入 "我要去光之国！"
    │
    ▼
规则匹配：包含 "光之国" → true
    │
    ▼
动作识别：提取动作类型为 "注入"
    │
    ▼
运行时替换：{角色名} → "贝利亚奥特曼"
    │
    ▼
追加记忆："贝利亚奥特曼的故乡是光之国" 写入记忆库
    │
    ▼
LLM 叙事：带着新记忆生成响应
```

**为什么坚持把替换放在运行时？**

因为引擎支持多分支故事线。如果加载时就替换成静态文本，所有分支共享同一个值，无法独立演化。运行时替换意味着每个分支读取自己的状态——状态变了，插值结果就变了。

## 四、小结：解析层完整了

到这一篇为止，解析层覆盖了所有语法单元：

| 区块类型     | 解析函数                        | 复杂度 | 说明                         |
| ------------ | ------------------------------- | ------ | ---------------------------- |
| 文本区块     | `parseTextBlock`                | 低     | 拼接内容，保留换行           |
| 键值对列表   | `parseKeyValuePairs`            | 中     | 支持中英文冒号，精确报错     |
| 规则列表     | `parseRules`                    | **高** | 条件表达式、互斥组、动作类型 |
| 纯文本列表   | `parsePlainList`                | 低     | 逐行提取                     |
| **插值语法** | `ReplacePlaceholders`（运行时） | 中     | 解析时保留，运行时替换       |

**最关键的一条边界：**

> 解析器只负责“读出来”——把文本转化为结构化的数据。  
> 引擎负责“算出来”——条件求值、变量替换、动作执行。

下一篇，我们将跨过这条边界，进入引擎的运行时。而引擎面对的第一个工程问题是：**如何保证代码改动不破坏现有的解析行为？**

答案是**集成测试**——用一组固定的 `.meph` 契约作为“看门狗”，每次改动后对比解析结果是否与预期一致。

> 项目地址：[https://github.com/yuelinghuashu/mephisto](https://github.com/yuelinghuashu/mephisto)