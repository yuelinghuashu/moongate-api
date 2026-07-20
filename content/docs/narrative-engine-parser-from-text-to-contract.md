---
title: 叙事引擎与大模型：手写区块扫描器
description: 从格式设计到解析器实现，手写 区块扫描器 让错误报出“第 12 行”而非“position 246”
permalink: a6a93745-4f18-4778-98bb-e1405b4e5770
date: 2026-07-20 17:00:00
series: narrative-engine
level: P3
tags:
  - Go
  - DSL
  - Engineering
---

## 引言：从"格式设计"到"格式加载"

第一篇讨论了为什么 JSON 和 YAML 都不适合做叙事引擎的配置文件，并引入了 `.meph` 格式的设计原则。但设计是一回事，实现是另一回事——接下来要回答的问题是：

**"【角色名】"和"【规则】"这种人类友好的区块标记，怎么被程序精确地识别、切分、并转化为可操作的数据结构？**

如果用 `json.Unmarshal`，我只需要一行代码。但我现在面对的不是 JSON，而是一种自定义文本格式。我需要自己处理每一行、维护状态机、记录行号、并给出精确的错误信息。

这个选择是否值得，要看最终效果。

## 一、为什么不用现成的解析工具？

坦白说，我现在知道有现成的工具——但在写解析器的时候，我根本不知道。

那时候我只是想解析一种带 `【】` 的文本格式，思路很直接：逐行扫描，记录当前在哪个区块里，遇到标题就切换。这个逻辑用一个循环加几个变量就能搞定，于是我写了大概一百行代码，跑通了，就没再想过别的方案。

转折点是在写这篇文章的时候。我请 AI 帮我梳理文章结构，它问了一句："你考虑过用 go-yacc 或 ANTLR 吗？"

我愣了一下。——什么？解析器还能用工具生成？

那天我才知道，原来解析器生成器这类工具已经存在了几十年。回想一下，如果当初我知道它们的存在，我可能会犹豫"是不是该用更专业的方案"，甚至可能因为学习 yacc 的 BNF 文法而把进度拖慢一两周。而事实是，我凭直觉做了最朴素的选择，它跑得挺好，而且跑到了现在。

现在回头看，我不会换成 yacc。原因有三个：

**1. 错误信息的精确度**

手写解析器可以将每一行内容绑定绝对行号。报错时我能给出：

```
第 12 行（区块「锚点」）：键值对缺少 ':' 或 '：'
```

这在 yacc 生成的解析器中很难做到同样自然——它的报错通常指向"position N"，而 position N 对非技术用户没有任何意义。

**2. 零外部依赖**

整个项目 `go build` 一步编译，不需要额外的代码生成步骤，也不需要引入 yacc 工具链。对用户来说，clone 项目、go build、运行，就这三步。

**3. 可控性**

解析过程中遇到的任何边角情况，我都可以直接修改扫描逻辑来修复。不需要去理解 yacc 的 LALR 冲突表，也不需要调试文法规则的优先级——只需要在循环里加一个条件分支，然后重新编译。

这个选择的价值在于：**我是在用写程序的方式解决问题，而不是在学习一个工具后用工具的方式解决问题。** 前者的成本是一次性的（写几百行代码），后者的成本是持续性的（学习文法 + 调试生成器 + 维护构建流程）。

某种意义上，"不知道"反而让我避开了过早的工具选型陷阱。我在做完之后，才知道还有另一种做法——而那时，手写方案已经被验证为足够好了。

## 二、两阶段设计：Lexer 和 Parser 的分工

既然决定手写，接下来的问题是：解析器应该怎么组织？

我的方案是拆成两个阶段：

| 阶段                   | 职责               | 输入              | 输出                                       |
| ---------------------- | ------------------ | ----------------- | ------------------------------------------ |
| **Lexer（词法分析）**  | 切分区块，记录行号 | 原始文本 `string` | `[]Block`（标题 + 带行号的内容行）         |
| **Parser（语法解析）** | 结构化解析         | `[]Block`         | `*domain.Contract`（角色名、状态、规则等） |

这个拆分的关键收益是：**行号在 Lexer 阶段就被记录并绑定到每一行内容上**。Parser 拿到数据后，不需要自己计算"这一行在文件里是第几行"，直接用就行。

核心数据结构只有两个：

```go
// Line 表示带行号的内容行
type Line struct {
    Text   string
    Number int  // 绝对行号，从 1 开始
}

// Block 表示一个切分后的区块
type Block struct {
    Title   string  // 区块标题，如"角色名"、"锚点"
    Content []Line  // 内容行（不含标题行），每行自带行号
    Line    int     // 标题行的行号
}
```

有了这个结构，Parser 的报错可以写成：

```go
fmt.Errorf("第 %d 行（区块「%s」）：列表项必须以 '-' 开头", line.Number, blockName)
```

而不是：

```go
fmt.Errorf("invalid syntax")
```

> **关于"Lexer"这个命名的说明：**
>
> 严格来说，这里的 Lexer 并不是传统编译原理中将字符切碎成 Token 的细粒度词法分析器，而是一个"区块扫描器（Block Scanner）"。它以"行"为最小单位切分结构，把更微观的字符串解析（如冒号分割、规则表达式解析）留给 Parser。这种设计让行号在最上层就被牢牢锚定，也为后续的精确报错打下了基础。

## 三、Lexer 的实现：一个简单的状态机

Lexer 的核心是一个逐行扫描的状态机。它只有两个状态：

- `inBlock == false`：当前不在任何区块内，遇到 `【标题】` 则进入区块
- `inBlock == true`：当前在区块内，遇到下一个 `【标题】` 则结束当前区块并开始新区块

完整实现如下：

```go
func Lex(text string) ([]Block, error) {
    lines := strings.Split(text, "\n")
    var blocks []Block
    var currentTitle string
    var currentContent []Line
    var currentLine int
    inBlock := false

    for i, rawLine := range lines {
        lineNumber := i + 1

        // 不在区块内时，空行跳过
        if !inBlock && strings.TrimSpace(rawLine) == "" {
            continue
        }

        // 检查是否为区块标题
        if title, ok := isBlockTitle(rawLine); ok {
            if inBlock {
                // 保存当前区块
                blocks = append(blocks, Block{
                    Title:   currentTitle,
                    Content: currentContent,
                    Line:    currentLine,
                })
            }
            // 开始新区块
            currentTitle = title
            currentContent = []Line{}
            currentLine = lineNumber
            inBlock = true
            continue
        }

        // 非标题行：必须在区块内
        if !inBlock {
            return nil, fmt.Errorf("第 %d 行：内容出现在任何区块之外", lineNumber)
        }

        // 累加内容行（带上行号）
        currentContent = append(currentContent, Line{
            Text:   rawLine,
            Number: lineNumber,
        })
    }

    // 保存最后一个区块
    if inBlock {
        blocks = append(blocks, Block{
            Title:   currentTitle,
            Content: currentContent,
            Line:    currentLine,
        })
    }

    if len(blocks) == 0 {
        return nil, fmt.Errorf("没有有效区块")
    }

    return blocks, nil
}
```

`isBlockTitle` 的实现同样简洁：

```go
func isBlockTitle(line string) (string, bool) {
    trimmed := strings.TrimSpace(line)
    if strings.HasPrefix(trimmed, "【") && strings.HasSuffix(trimmed, "】") {
        title := strings.TrimSpace(trimmed[3 : len(trimmed)-3]) // 去掉【】
        return title, knownBlocks[title]
    }
    return "", false
}
```

这里有两个关键设计：

**关键设计一：白名单前置**

`isBlockTitle` 不仅检查 `【` 和 `】`，还会检查标题是否在 `knownBlocks` 白名单中：

```go
var knownBlocks = map[string]bool{
    "角色名":  true,
    "锚点":   true,
    "世界观":  true,
    "角色背景": true,
    "开局场景": true,
    "状态":   true,
    "规则":   true,
    "校验":   true,
    "记忆":   true,
    "历史":   true,
}
```

这意味着 `【未知区块】` 不会被当作区块开始，而是被当作普通内容（或报错）。这避免了拼写错误导致的隐式 bug——如果用户写了 `【脚色名】` 而不是 `【角色名】`，解析器会报错"第 X 行：内容出现在任何区块之外"，用户立刻就能发现问题。

**关键设计二：行号绑定**

每一行内容在存储时都带上绝对行号：

```go
type Line struct {
    Text   string
    Number int  // 绝对行号，从 1 开始
}
```

这个 `Number` 从 Lexer 的循环索引 `i+1` 直接赋值，不经过任何偏移量计算。因此它永远是准确的，即使文件开头有空行或注释。

## 四、Parser 的实现：路由到不同的解析函数

Lexer 输出 `[]Block` 后，Parser 的工作是：**根据 Block.Title，将内容分发给对应的解析函数**。

主入口如下：

```go
func parseBlocks(blocks []Block) (*domain.Contract, error) {
    contract := &domain.Contract{}

    for _, block := range blocks {
        var err error

        switch block.Title {
        case "角色名":
            contract.RoleName, err = parseRoleName(block.Content, block.Line)
        case "锚点":
            contract.Anchor, err = parseKeyValuePairs(block.Content, block.Title)
        case "世界观":
            contract.Worldview = parseTextBlock(block.Content)
        case "状态":
            contract.State, err = parseKeyValuePairs(block.Content, block.Title)
        case "规则":
            contract.Rules, err = parseRules(block.Content, block.Title)
        // ... 其他区块
        }

        if err != nil {
            return nil, err  // 错误向上传递，携带行号和区块名
        }
    }

    return contract, nil
}
```

每种区块的解析逻辑是独立的，这里以键值对列表为例：

```go
func parseKeyValuePairs(lines []Line, blockName string) ([]KeyValue, error) {
    var result []KeyValue

    for _, line := range lines {
        trimmed := strings.TrimSpace(line.Text)

        // 跳过空行和注释
        if trimmed == "" || strings.HasPrefix(trimmed, "#") {
            continue
        }

        // 必须以 "-" 开头
        if !strings.HasPrefix(trimmed, "-") {
            return nil, fmt.Errorf("第 %d 行（区块「%s」）：列表项必须以 '-' 开头",
                line.Number, blockName)
        }

        rest := strings.TrimPrefix(trimmed, "-")
        rest = strings.TrimSpace(rest)

        // 支持中英文冒号
        key, value, ok := splitKeyValue(rest)
        if !ok {
            return nil, fmt.Errorf("第 %d 行（区块「%s」）：缺少 ':' 或 '：'",
                line.Number, blockName)
        }

        result = append(result, KeyValue{Key: key, Value: value})
    }

    return result, nil
}
```

注意到所有错误信息都包含两个信息：**行号**（`line.Number`）和**区块名**（`blockName`）。这使报错信息精确到足以让创作者一眼定位问题。

## 五、错误信息对比：同一个错误，两种体验

假设创作者在配置时，不小心漏掉了一个冒号，写成了这样：

```meph
【锚点】
- 核心信念 "力量就是一切"
```

注意：`- 核心信念 "力量就是一切"` 缺少了冒号（`：` 或 `:`）。

**如果使用 JSON：** 报错通常是：

```
Unexpected token in JSON at position 42
```

创作者需要把文件复制到在线工具里，去数第 42 个字符在哪里，然后猜测自己是不是少写了什么。

**`.meph` 解析器的报错：**

```
第 2 行（区块「锚点」）：缺少 ':' 或 '：'
```

不用解释什么是 Token，不用数 position，创作者秒懂，抬手就能修好。

再看一个稍复杂的场景。假设用户写了：

```meph
【状态】
情绪: 暴怒
堕落指数: 85
```

注意：状态区块的每一项需要以 `-` 开头。

**如果使用 JSON：** 报错可能是：

```
invalid character '情' looking for beginning of value
```

创作者看到这个信息，大概率只能复制粘贴去搜索引擎碰运气。

**`.meph` 解析器的报错：**

```
第 2 行（区块「状态」）：列表项必须以 '-' 开头
```

创作者立刻知道：第二行需要加一个 `-`。

这种错误信息的精确性，是手写 Lexer 带来的直接收益。

## 六、代价与取舍

这一章的开头问"是否值得"，现在可以给出答案了。

**代价：**

- 需要写约 400 行 Go 代码（Lexer + Parser）
- 需要维护一个白名单（新增区块时要同步更新）
- 需要为每个区块类型写独立的解析逻辑

**收益：**

- 错误信息精确到行号和区块名，无需数 `position`
- 零外部依赖，`go build` 一步完成
- 解析逻辑完全可控，可以随时调整
- 代码本身就是格式规范（不需要额外维护语法文档）

**核心收益**：对于叙事引擎这个场景，"精确的错误报告"直接转化为"创作者的调试效率"。一个写故事的人，如果因为漏了一个冒号就要在 JSON 报错里翻找半天，他会放弃这个工具。而如果错误信息直接告诉他"第二行缺少冒号"，他可以立刻修复，继续创作。

这个取舍，我后来认为是值得的。

## 📎 附：错误信息对照表

| 用户写错的内容                | JSON 报错（示意）                     | `.meph` 报错                                     |
| ----------------------------- | ------------------------------------- | ------------------------------------------------ |
| `- 核心信念 "力量"`（缺冒号） | `Unexpected token at position 42`     | `第 2 行（区块「锚点」）：缺少 ':' 或 '：'`      |
| `情绪: 暴怒`（缺 `-`）        | `invalid character looking for value` | `第 2 行（区块「状态」）：列表项必须以 '-' 开头` |
| 区块外写了内容                | `Unexpected token`                    | `第 5 行：内容出现在任何区块之外`                |
| `【脚色名】`（错别字）        | 不适用                                | `第 1 行：内容出现在任何区块之外`                |

---

> **项目地址**：[https://github.com/yuelinghuashu/mephisto](https://github.com/yuelinghuashu/mephisto)