---
title: 构建大模型叙事引擎：区块扫描与行号绑定
description: 从格式设计到解析器实现，手写区块扫描器让错误报出"第 12 行"而非"position 246"——精确的行号绑定是手写解析器的核心价值。
permalink: a6a93745-4f18-4778-98bb-e1405b4e5770
date: 2026-07-20 17:00:00
series: narrative-engine
level: P3
tags:
  - Go
  - DSL
  - Engineering
---

> **前置阅读**：如果你是从搜索引擎直接跳到这一篇，建议先读第一篇的“一、先看目标”和“六、.meph 的设计原则”，了解 `.meph` 的长相和设计初衷。本篇假设你已经知道 `【角色名】` 和 `【规则】` 是什么。

`.json` 解析器报 `position 246`，创作者需要的是**"第 12 行缺冒号"**。这一篇实现的就是这种精确报错。

## 一、叙事引擎的第二个问题：规则怎么变成结构？

格式定义好之后，下一个问题是**解析**——怎么把文本变成程序能操作的数据。

这里有两个关键指标：**解析正确性**和**报错可读性**。通用格式（JSON/YAML）在第一个指标上很好，但在第二个指标上几乎是灾难——`position 246` 对创作者毫无意义。而对于自定义格式，解析器必须从头实现。

一篇好的解析器设计，应该做到三件事：

1. **精确识别**：每个区块、每行内容都能被正确归类
2. **行号绑定**：每一条错误都能追溯到具体行，而不是字符串偏移量
3. **白名单校验**：错误的写法（如 `【脚色名】`）不会被当作有效内容

这一篇实现的就是这三件事。

---

## 二、先看问题：如果没有行号绑定

在开始写解析器之前，先看一个真实的场景。

创作者写了一份契约，其中一行是：

```meph
【锚点】
- 核心信念 "力量就是一切"
```

注意，`- 核心信念 "力量就是一切"` 缺少了冒号（正确写法是 `- 核心信念: "力量就是一切"`）。

如果我用通用解析器，报错大概是：

```
unexpected token at position 42
```

创作者需要复制粘贴去数 position 42 是哪个字符。这个过程极其折磨。

而我的目标是让解析器报出这样的错误：

```
第 2 行（区块「锚点」）：缺少 ':' 或 '：'
```

不需要数 position，不需要理解“token”是什么，直接告诉创作者：“第二行缺了一个冒号。”

**这个差异，就是手写解析器的全部理由。**

## 三、两阶段设计

我把解析拆成两个阶段：

| 阶段                       | 职责               | 输入      | 输出               |
| -------------------------- | ------------------ | --------- | ------------------ |
| **区块扫描器**             | 切分区块，记录行号 | 原始文本  | `[]Block`          |
| **结构化解析器**（Parser） | 结构化解析         | `[]Block` | `*domain.Contract` |

**关键设计**：行号在扫描阶段就绑定到每一行，Parser 直接使用，无需计算偏移量。这样报错时永远是精确的绝对行号。

数据结构就两个：

```go
type Line struct {
    Text   string
    Number int  // 绝对行号，从 1 开始
}

type Block struct {
    Title   string  // 如 "角色名"
    Content []Line  // 内容行，自带行号
    Line    int     // 标题行号
}
```

有了这个结构，报错可以这样写：

```go
fmt.Errorf("第 %d 行（区块「%s」）：列表项必须以 '-' 开头",
    line.Number, blockName)
```

## 四、区块扫描器：一个简单的状态机

核心是一个逐行扫描的状态机。它只有两个状态：

- `inBlock == false`：当前不在任何区块内
- `inBlock == true`：当前在区块内，正在收集内容

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

        currentContent = append(currentContent, Line{
            Text:   rawLine,
            Number: lineNumber,
        })
    }

    if inBlock {
        blocks = append(blocks, Block{...})
    }

    if len(blocks) == 0 {
        return nil, fmt.Errorf("没有有效区块")
    }
    return blocks, nil
}
```

**两个关键设计：**

**1. 白名单前置**

`isBlockTitle` 只认预定义的标题列表：

```go
var knownBlocks = map[string]bool{
    "角色名": true,
    "锚点":  true,
    "规则":  true,
    "状态":  true,
    // ...
}
```

如果创作者写了 `【脚色名】`（错别字），扫描器不会把它当作区块开始——而是会得到一个指向该行的错误。具体报错信息取决于上下文：如果该行出现在任何区块之外，报错“内容出现在任何区块之外”；如果出现在某个区块内部，则会在该区块的解析中报错。无论哪种情况，行号都是精确的。

**2. 行号绑定**

每一行在存入时直接携带 `lineNumber`，永不偏移。这是实现精确报错的根基。

## 五、Parser：路由到不同解析函数

扫描器输出 `[]Block` 后，Parser 根据 `Title` 路由到对应的解析函数：

```go
func parseBlocks(blocks []Block) (*domain.Contract, error) {
    contract := &domain.Contract{}
    for _, block := range blocks {
        switch block.Title {
        case "角色名":
            contract.RoleName, err = parseRoleName(block.Content, block.Line)
        case "锚点":
            contract.Anchor, err = parseKeyValuePairs(block.Content, block.Title)
        case "规则":
            contract.Rules, err = parseRules(block.Content, block.Title)
        // ... 其他区块
        }
        if err != nil {
            return nil, err
        }
    }
    return contract, nil
}
```

每种区块的解析逻辑是独立的。以键值对列表为例——所有错误都带行号和区块名：

```go
func parseKeyValuePairs(lines []Line, blockName string) ([]KeyValue, error) {
    for _, line := range lines {
        trimmed := strings.TrimSpace(line.Text)
        if trimmed == "" || strings.HasPrefix(trimmed, "#") {
            continue
        }
        if !strings.HasPrefix(trimmed, "-") {
            return nil, fmt.Errorf("第 %d 行（区块「%s」）：列表项必须以 '-' 开头",
                line.Number, blockName)
        }
        // ... 提取键值对
    }
    return result, nil
}
```

## 六、错误信息对比

同一个错误，两种体验：

| 用户写错的内容                | 通用解析器报错                        | `.meph` 报错                                     |
| ----------------------------- | ------------------------------------- | ------------------------------------------------ |
| `- 核心信念 "力量"`（缺冒号） | `Unexpected token at position 42`     | `第 2 行（区块「锚点」）：缺少 ':' 或 '：'`      |
| `情绪: 暴怒`（缺 `-`）        | `invalid character looking for value` | `第 2 行（区块「状态」）：列表项必须以 '-' 开头` |
| `【脚色名】`（错别字）        | 不适用                                | 指向该行的精确错误                               |

## 七、代价

- 写了约 400 行 Go 代码
- 需要为每个区块类型写独立的解析逻辑
- 新增区块要同步更新白名单

但没有外部依赖，`go build` 一步完成。解析逻辑完全可控，可以随时调整。

## 八、小结

区块扫描器和 Parser 完成了“文本到结构”的转化。现在 `【角色名】` 变成了 `contract.RoleName`，`【规则】` 变成了 `contract.Rules`。

但 `contract.Rules` 里的条件（如 `包含 "攻击"`）和动作（如 `注入 "{角色名}的故乡是光之国"`）仍然是字符串。下一篇我们要处理的是：**规则的条件-动作表达式怎么拆解？插值语法 `{变量}` 怎么处理？**

这是解析层最后两块拼图。

> 项目地址：[https://github.com/yuelinghuashu/mephisto](https://github.com/yuelinghuashu/mephisto)
