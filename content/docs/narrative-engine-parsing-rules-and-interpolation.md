---
title: 叙事引擎与大模型：规则表达式与插值语法
description: 解析规则表达式和插值语法——让条件-动作与 {变量} 从文本变为可执行结构
permalink: c7f60196-3aa2-4191-9796-544aa5ba7a3e
date: 2026-07-20 19:00:00
series: narrative-engine
level: P3
tags:
  - Go
  - DSL
  - Engineering
---

## 引言：骨架搭好了，血肉是什么？

第二篇实现了 Lexer 和 Parser 的整体框架——文本被切分为区块，区块被路由给对应的解析函数，错误信息可以精确到行号和区块名。

但还有两个最复杂的语法单元，我们没有深入拆解：

- **规则区块**：`[攻击] if 包含 "攻击" -> 注入 "..."`
- **插值语法**：`{角色名}` 在文本中的识别和替换

这两个是叙事引擎的核心能力。规则决定了角色“怎么反应”，插值决定了文本“怎么动态”。如果解析器只处理了区块结构，但没有解析规则和插值，那引擎拿到的就只是一堆静态文本——世界观是死的，规则是字符串，状态变量不会展开。

所以这一篇要解决两个问题：

> **规则区块里的条件-动作表达式怎么解析？插值语法怎么在文本中被识别和替换？**


## 一、规则解析：把条件-动作拆成可执行的结构

规则区块的每一行是一个独立的规则，语法如下：

```meph
[攻击] if 包含 "攻击" -> 注入 "贝利亚发动了猛烈的攻击"
[光之国] if 包含 "光之国" && 状态.情绪 == "暴怒" -> LLM: 以嘲讽语气回应
[高堕落] if 状态.堕落指数 > 80 -> 状态.情绪 = "癫狂"
```

结构很清晰：`[规则名] if 条件 -> 动作`。但解析起来有几个需要处理的地方。

### 1.1 规则名提取

规则名用 `[]` 包裹，位于行首。提取逻辑很简单：找到第一个 `[` 和第一个 `]`，取中间的内容。

```go
func parseRuleLine(line string, lineNumber int, blockName string) (*domain.Rule, error) {
    trimmed := strings.TrimSpace(line)

    // 规则必须以 [ 开头
    if !strings.HasPrefix(trimmed, "[") {
        return nil, fmt.Errorf("第 %d 行（区块「%s」）：规则必须以 '[' 开头", 
            lineNumber, blockName)
    }

    // 找闭合的 ]
    idx := strings.Index(trimmed, "]")
    if idx == -1 {
        return nil, fmt.Errorf("第 %d 行（区块「%s」）：规则缺少闭合的 ']'", 
            lineNumber, blockName)
    }

    name := strings.TrimSpace(trimmed[1:idx])
    if name == "" {
        return nil, fmt.Errorf("第 %d 行（区块「%s」）：规则名不能为空", 
            lineNumber, blockName)
    }

    // 剩余部分：if 条件 -> 动作
    rest := strings.TrimSpace(trimmed[idx+1:])
    // ...
}
```

### 1.2 条件表达式的结构

条件部分支持以下语法：

| 语法 | 说明 | 示例 |
|------|------|------|
| `包含 "关键词"` | 用户输入包含指定关键词 | `包含 "攻击"` |
| `不包含 "关键词"` | 用户输入不包含指定关键词 | `不包含 "光之国"` |
| `状态.键 > 值` | 状态值大于指定值 | `状态.堕落指数 > 80` |
| `状态.键 == 值` | 状态值等于指定值 | `状态.情绪 == "暴怒"` |
| `条件1 && 条件2` | 与运算（同时满足） | `包含 "攻击" && 状态.堕落指数 > 80` |
| `条件1 \|\| 条件2` | 或运算（任意满足） | `包含 "攻击" \|\| 包含 "战斗"` |

解析策略采用**递归下降 + 优先级拆分**。先按 `||` 拆分，再按 `&&` 拆分，最后处理原子条件。

```go
func (e *DefaultConditionEvaluator) Evaluate(cond string, ctx ConditionContext) (bool, error) {
    cond = strings.TrimSpace(cond)
    if cond == "" {
        return false, fmt.Errorf("条件为空")
    }

    // 先拆分 ||，再拆分 &&
    if strings.Contains(cond, "||") {
        return e.evaluateLogical(cond, "||", false, ctx)
    }
    if strings.Contains(cond, "&&") {
        return e.evaluateLogical(cond, "&&", true, ctx)
    }

    // 原子条件
    return e.evaluateAtomic(cond, ctx)
}
```

> **注意：** 这里先处理 `||`、后处理 `&&` 的顺序是经过设计的。一个包含 `&&` 的子条件在 `||` 拆分后会被递归求值，因此在没有括号的情况下，这段代码正确地实现了“先与后或”的优先级。

原子条件通过前缀识别分发：

```go
func (e *DefaultConditionEvaluator) evaluateAtomic(cond string, ctx ConditionContext) (bool, error) {
    cond = strings.TrimSpace(cond)

    if strings.HasPrefix(cond, "包含 ") {
        return e.evaluateContains(cond, "包含 ", false, ctx)
    }
    if strings.HasPrefix(cond, "不包含 ") {
        return e.evaluateContains(cond, "不包含 ", true, ctx)
    }
    if strings.HasPrefix(cond, "状态.") {
        return e.evaluateStateCondition(cond, ctx)
    }

    return false, fmt.Errorf("不支持的条件格式: %s", cond)
}
```

状态比较的解析稍复杂一些，需要处理操作符优先级（`>=` 在 `>` 之前匹配）：

```go
func (e *DefaultConditionEvaluator) evaluateStateCondition(cond string, ctx ConditionContext) (bool, error) {
    rest := strings.TrimPrefix(cond, "状态.")
    rest = strings.TrimSpace(rest)

    // 优先匹配多字符操作符
    ops := []string{">=", "<=", "!=", "==", ">", "<"}
    var op string
    var idx int
    for _, o := range ops {
        if i := strings.Index(rest, o); i != -1 {
            op = o
            idx = i
            break
        }
    }
    if op == "" {
        return false, fmt.Errorf("状态条件缺少操作符: %s", cond)
    }

    key := strings.TrimSpace(rest[:idx])
    valStr := strings.TrimSpace(rest[idx+len(op):])

    // 获取当前状态值并与 valStr 比较
    // ...
}
```

### 1.3 动作部分与互斥组

动作在 `->` 之后。除了普通的动作文本，还支持两种特殊标记：

1. **动作类型前缀**：`注入`、`LLM:`、`状态.` 等，用于在运行时分发
2. **互斥组标记**：`[group:combat]`，写在动作最前面

```go
// 提取互斥组
group := ""
if strings.HasPrefix(action, "[group:") {
    endIdx := strings.Index(action, "]")
    if endIdx != -1 {
        group = action[7:endIdx]
        action = strings.TrimSpace(action[endIdx+1:])
    }
}
```

互斥组的作用是：同一组内多个规则，只有第一个匹配的会被触发。这个逻辑在运行时生效，解析层只需要把组名提取出来，存储在 `domain.Rule.Group` 字段中。

### 1.4 引号处理

条件和动作中的字符串用 `"` 包裹。解析时需要用 `unquote` 函数剥离外层引号：

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

为什么支持中文引号？因为创作者可能使用中文输入法，`“` 和 `”` 比 `"` 更容易自然输入。


## 二、插值解析：找到 `{变量名}` 并替换

插值的核心需求是：**在任意文本中识别 `{变量名}` 并替换为对应的值**。

但“对应的值”从哪来，决定了插值的实现层次。在 Mephisto 中，变量来源有两个：

- **角色名**：`{角色名}` 来自契约的 `【角色名】` 区块
- **状态变量**：`{情绪}`、`{生命值}`、`{位置}` 等来自 `【状态】` 区块

### 2.1 解析器不负责替换

这是一个关键的设计决策：**解析器只负责识别插值语法，不负责执行替换**。

为什么？因为状态值是动态变化的。`{堕落指数}` 在契约初始值中可能是 `50`，但运行时可能变成了 `85`。如果解析器在加载时就完成了替换，那这个值就被“冻住”了，无法反映运行时的变化。

所以解析器的职责是：**把 `{变量名}` 当作普通文本原样存储**。运行时由 `ReplacePlaceholders` 函数完成替换。

```go
// internal/shared/template.go
func ReplacePlaceholders(template string, vars map[string]string) string {
    for k, v := range vars {
        template = strings.ReplaceAll(template, "{"+k+"}", v)
    }
    return template
}
```

这个实现简单到只有七行代码。但它够用了——因为我们的需求就是“把 A 替换为 B”，不需要条件、循环、函数调用。

### 2.2 替换时机：两个阶段

变量替换发生在两个不同的阶段：

**阶段一：静态文本显示时**

`【世界观】`、`【角色背景】`、`【开局场景】` 等区块在首次显示时进行一次替换。这些文本在加载后不会变化，所以只需要替换一次。

例如 `【世界观】` 中的 `{角色名}的记忆开始苏醒。`，在会话启动时被替换为 `贝利亚奥特曼的记忆开始苏醒`。

**阶段二：规则动作执行时**

规则动作中的 `{变量}` 在规则触发时替换。此时状态可能已经发生了变化，所以每次触发都需要重新计算。

例如 `注入 "{角色名}的堕落指数已达 {堕落指数}"`，如果堕落指数已经从 50 涨到了 85，替换结果就是 `贝利亚奥特曼的堕落指数已达 85`。

### 2.3 为什么不用 `text/template`

Go 标准库提供了 `text/template`，功能强大到可以执行循环和条件判断。但为什么不直接用？

原因和第一篇中选择 `.meph` 的理由一致：**这是给创作者用的，不是给程序员用的**。

`text/template` 的语法是：

```go
{{.RoleName}}的堕落指数已达 {{.CorruptionLevel}}
```

而 `.meph` 的插值语法是：

```meph
{角色名}的堕落指数已达 {堕落指数}
```

区别在于：
- `text/template` 需要理解“点号表示法”和“模板上下文”
- `.meph` 只需要看到花括号就知道这是一个变量

创作者不需要区分“这是来自角色名还是来自状态”，他们只需要一个变量，能按预期展开就行。更复杂的语法意味着更多的学习成本和错误机会。这不是技术能力的限制，而是对用户群体的尊重。

**插值语法是设计给故事作者看的，不是设计给程序员看的。**


## 三、交汇点：当规则遇上插值

规则动作中经常包含插值。例如：

```meph
[光之国] if 包含 "光之国" -> 注入 "{角色名}的故乡是光之国"
```

这是一个典型的交汇场景，其完整的执行流程如下：

```
[用户输入: "我要去光之国！"]
        │
        ▼
1. 规则匹配 ── 判定 `包含 "光之国"` 为 true，触发成功
        │
        ▼
2. 动作识别 ── 提取 `->` 后面的动作类型为 `注入`
        │
        ▼
3. 运行时替换 ── 调用 ReplacePlaceholders，将 `{角色名}` 替换为当前状态中的 "贝利亚奥特曼"
        │
        ▼
4. 追加记忆 ── 最终文本 "贝利亚奥特曼的故乡是光之国" 被追加到上下文记忆
        │
        ▼
5. LLM 叙事 ── 带着追加的记忆生成响应
```

**为什么坚持把插值替换放在动作执行时，而不是规则解析时？**

因为叙事引擎需要支持**多分支故事线**。如果规则在解析（加载）时就被固化替换成了静态文本，那么所有分支共享同一个值，无法独立演化。而将替换推迟到运行时，每个分支都能读取自己当前的独立状态——状态变了，插值结果就变了。

把替换推迟到转动齿轮的那一刻，多分支叙事才真正拥有了灵魂。


## 四、小结：解析层完整了

到这一篇为止，解析层（Lexer + Parser）的所有语法单元都已覆盖：

| 区块类型 | 解析函数 | 复杂度 | 说明 |
|---------|---------|--------|------|
| 文本区块（世界观/背景/开局） | `parseTextBlock` | 低 | 拼接内容行，保留换行 |
| 键值对列表（锚点/状态/校验） | `parseKeyValuePairs` | 中 | 支持中英文冒号，精确报错 |
| 规则列表（规则） | `parseRules` + `parseRuleLine` | **高** | 条件表达式、互斥组、动作类型识别 |
| 纯文本列表（记忆/历史） | `parsePlainList` / `parseHistory` | 低 | 逐行提取列表项 |
| **插值语法** | `ReplacePlaceholders`（运行时） | 中 | 解析时保留，运行时替换 |

解析器的完整输出是 `domain.Contract` 结构体：

```go
type Contract struct {
    RoleName   string
    Anchor     []KeyValue
    Worldview  string
    Background string
    Opening    string
    State      []KeyValue    // 初始状态
    Rules      []*Rule       // 规则列表（包含条件和动作的原始字符串）
    Memories   []string
    History    []HistoryEntry
}
```

注意 `Rules` 中的 `Cond` 和 `Action` 仍然是字符串——条件表达式没有被预编译为 AST，动作也没有被解析为中间表示。**解析器的职责到此为止：把文本转化为结构化的数据，但不负责执行。**

这是第二篇和第三篇贯穿始终的设计哲学：**解析器只负责“读出来”，不负责“算出来”**。`{变量}` 的替换、条件表达式的评估、动作的执行，都是引擎运行时的职责。解析器和引擎之间有一条清晰的边界——前者是“文本到结构”，后者是“结构到行为”。

---

> 项目地址：[https://github.com/yuelinghuashu/mephisto](https://github.com/yuelinghuashu/mephisto)
