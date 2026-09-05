---
title: 从零实现词法分析器（二）：用一张表统一管理所有符号
description: 避免 Lexer 中出现大量 switch-case，引入符号映射表（symbolMap）统一管理所有符号，实现中英双语支持和数据驱动设计。
date: 2026-07-13 21:00:00
series: lexer-from-scratch
level: P1
tags:
  - Go
  - Compiler
---

## 📚 系列导航

本系列共三篇：

1. [**从零实现词法分析器（一）：Token**](./from-text-to-token-go-lexer-part-1) —— Token 的概念与第一个结构体
2. [**从零实现词法分析器（二）：符号表**](./from-text-to-token-go-lexer-part-2) —— 用统一符号表管理所有符号
3. [**从零实现词法分析器（三）：Lexer 引擎**](./from-text-to-token-go-lexer-part-3) —— 指针驱动的词法分析引擎

---

> 上一篇我们定义了 Token，但 Token 只是"零件"。在实现 Lexer 之前，还有一个问题要先解决——如何让 Lexer 能认识所有符号，而不需要每次新增符号都修改它的代码。

## 一、回顾：上一篇我们停在了哪里

上一篇我们定义了 Token：

```go
package parser

type TokenType string

const (
	TOKEN_LEFT_BRACKET  TokenType = "LEFT_BRACKET"  // 【
	TOKEN_RIGHT_BRACKET TokenType = "RIGHT_BRACKET" // 】
	TOKEN_TEXT          TokenType = "TEXT"          // 普通文本
)

type Token struct {
	Type    TokenType
	Literal string
}
```

现在我们需要一个程序——**Lexer（词法分析器）**——来读取文本，不断产出这些 Token。

但在写 Lexer 之前，我们还需要做一件事：**扩充 Token 类型，并建立一套让 Lexer 能识别所有符号的机制。**

## 二、扩充 Token 类型

上一篇我们只定义了三种 Token 类型。但在一个完整的 `.meph` 文件中，除了 `【`、`】` 和普通文本，还有更多符号需要识别：

| 符号       | 用途      | 示例                         |
| ---------- | --------- | ---------------------------- |
| `：` / `:` | 冒号      | `好感度：78`                 |
| `-`        | 列表标记  | `- 核心信念："力量就是一切"` |
| `@`        | 引用符号  | `@[世界观](worlds/eva.meph)` |
| `#`        | 标签/注释 | `# 硬约束`                   |

`parser/token.go` 现在的完整代码如下：

```go
package parser

type TokenType string

const (
	// 语法符号
	TOKEN_LEFT_BRACKET  TokenType = "LEFT_BRACKET"  // 【
	TOKEN_RIGHT_BRACKET TokenType = "RIGHT_BRACKET" // 】
	TOKEN_COLON         TokenType = "COLON"         // ：
	TOKEN_HYPHEN        TokenType = "HYPHEN"        // -
	TOKEN_AT            TokenType = "AT"            // @
	TOKEN_HASH          TokenType = "HASH"          // #

	// 内容类型
	TOKEN_TEXT TokenType = "TEXT" // 普通文本

	// 控制标记
	TOKEN_NEWLINE TokenType = "NEWLINE" // 换行符
	TOKEN_EOF     TokenType = "EOF"     // 文件结束（哨兵）
)

type Token struct {
	Type    TokenType
	Literal string    // 符号的原始文本
	Line    int       // 当前行号（从 1 开始）
}
```

**注意**：

`NEWLINE` 和 `EOF` 虽然在 Lexer 实现中才会用到，但我们提前定义好，这样后续文章就不需要回头修改 `token.go` 了。

## 三、问题：Lexer 需要认识多种符号

Lexer 的工作是扫描文本，识别出每个符号是什么。

一个最直接的写法是：

```go
switch ch {
case '【':
    // 返回 LEFT_BRACKET
case '】':
    // 返回 RIGHT_BRACKET
case '：':
    // 返回 COLON
case '-':
    // 返回 HYPHEN
// ... 越来越多的 case
}
```

这样写有三个问题：

| 问题                 | 说明                                                     |
| -------------------- | -------------------------------------------------------- |
| **新增符号要改代码** | 每次增加新的符号类型，都要修改 Lexer 的源码              |
| **中英双语支持困难** | 如果要同时支持 `【` 和 `[` 作为左括号，`case` 会越来越多 |
| **判断逻辑分散**     | 符号的"定义"和"判断"混在一起，不容易维护                 |

**我们需要一种更好的方式：把"符号是什么"和"怎么判断符号"分开。**

## 四、方案：用一张表统一管理所有符号

核心思想很简单：**把所有的符号集中在一张表里管理，Lexer 通过查表来判断。**

这张表里，每个符号记录两件事：

1. **对应的 Token 类型**：如 `LEFT_BRACKET`、`RIGHT_BRACKET`、`COLON`
2. **分类**：如 `bracket`（括号）、`colon`（冒号）

Lexer 只需要查这张表，就能知道当前字符是什么类型的 Token。

## 五、关键决策：用 `rune` 而不是 `byte`

在定义映射表之前，先确定一个关键问题：用什么类型来存储字符？

在 Go 中，遍历字符串有两种方式：

| 方式   | 类型   | 特点                                    |
| ------ | ------ | --------------------------------------- |
| `byte` | 1 字节 | 只能处理 ASCII 字符（英文、数字、标点） |
| `rune` | 4 字节 | 能处理任意 Unicode 字符（中文、Emoji）  |

`【` 在 UTF-8 中占 **3 个字节**。如果按 `byte` 遍历，`【` 会被拆成 3 个独立的字节，无法正确识别。

所以必须用 `rune`——它能保证每个字符（无论中英文）都被当作一个整体处理。

**用 `rune` 的代价：** Go 的 `string` 底层是 `[]byte`，把它转成 `[]rune` 需要额外分配内存。但对于我们处理的 `.meph` 文件（通常只有几 KB 到几百 KB），这个代价可以忽略不计。**正确性远比微小的性能损耗重要。**

## 六、定义映射表

### 6.1 结构体定义

创建 `parser/symbols.go`：

```go
package parser

// SymbolInfo 符号信息
type SymbolInfo struct {
    TokenType TokenType // 对应的 Token 类型
    Category  string    // 分类：bracket, colon, hyphen, at, hash, whitespace, newline
}
```

### 6.2 映射表

```go
// symbolMap 所有符号的映射表
// 新增符号只需在这里加一行
var symbolMap = map[rune]SymbolInfo{
    // 括号
    '【': {TokenType: TOKEN_LEFT_BRACKET, Category: "bracket"},
    '】': {TokenType: TOKEN_RIGHT_BRACKET, Category: "bracket"},
    '[':  {TokenType: TOKEN_LEFT_BRACKET, Category: "bracket"},
    ']':  {TokenType: TOKEN_RIGHT_BRACKET, Category: "bracket"},

    // 冒号
    '：': {TokenType: TOKEN_COLON, Category: "colon"},
    ':':  {TokenType: TOKEN_COLON, Category: "colon"},

    // 连字符
    '-':  {TokenType: TOKEN_HYPHEN, Category: "hyphen"},

    // 引用符号
    '@':  {TokenType: TOKEN_AT, Category: "at"},

    // 标签/注释符号
    '#': {TokenType: TOKEN_HASH, Category: "hash"},

    // 空白字符（空格、制表符、回车）
    // 注意：这些字符会被 skipWhitespace() 提前消费，不会作为 Token 返回
    ' ':  {TokenType: TOKEN_TEXT, Category: "whitespace"},
    '\t': {TokenType: TOKEN_TEXT, Category: "whitespace"},
    '\r': {TokenType: TOKEN_TEXT, Category: "whitespace"},

    // 换行符
    '\n': {TokenType: TOKEN_NEWLINE, Category: "newline"},
}
```

#### 关键设计说明

| 设计点                          | 说明                                                                      |
| ------------------------------- | ------------------------------------------------------------------------- |
| **同一 TokenType 对应多个字符** | `【` 和 `[` 都是 `TOKEN_LEFT_BRACKET`，中英双语自然支持                   |
| **Category 用于筛选**           | Lexer 可以通过 `Category == "whitespace"` 跳过空格                        |
| **`\n` 是 `TOKEN_NEWLINE`**     | 换行有语法意义，会被作为 Token 返回（不同于被跳过的空格）                 |
| **空格是 `TOKEN_TEXT`**         | 空格永远不会被返回（被 `skipWhitespace` 提前消费），`TOKEN_TEXT` 只是占位 |

## 七、基于映射表的符号查询

有了映射表，查询符号信息变得极其简单——直接查表即可：

```go
// GetSymbolInfo 查询符号信息
// 返回值：(SymbolInfo, bool)，bool 表示是否找到
// 这是给外部包（如测试代码）用的查询入口
func GetSymbolInfo(ch rune) (SymbolInfo, bool) {
    info, ok := symbolMap[ch]
    return info, ok
}
```

### 这个函数是 `symbolMap` 的唯一直观体现

它告诉调用方"这个字符是不是我们认识的符号？如果是，它是什么类型？"

在下一篇文章中，我们会看到 Lexer 内部如何使用 `symbolMap` 来实现一次查表驱动整个词法分析。现在我们先验证这张表是否工作正常。

## 八、验证：查表逻辑的正确性

在 `main.go` 中测试 `GetSymbolInfo`：

```go
package main

import (
    "fmt"
    "mephisto/internal/parser"
)

func main() {
    // 查询中文左括号
    info, ok := parser.GetSymbolInfo('【')
    fmt.Printf("'【' → %s, 找到: %v\n", info.TokenType, ok) // LEFT_BRACKET, true

    // 查询英文左括号
    info, ok = parser.GetSymbolInfo('[')
    fmt.Printf("'[' → %s, 找到: %v\n", info.TokenType, ok) // LEFT_BRACKET, true

    // 查询普通字母（不在表中）
    info, ok = parser.GetSymbolInfo('x')
    fmt.Printf("'x' → 找到: %v\n", ok) // false

    // 查询换行符
    info, ok = parser.GetSymbolInfo('\n')
    fmt.Printf("'\\n' → %s, 找到: %v\n", info.TokenType, ok) // NEWLINE, true
}
```

输出：

```text
'【' → TOKEN_LEFT_BRACKET, 找到: true
'[' → TOKEN_LEFT_BRACKET, 找到: true
'x' → 找到: false
'\n' → TOKEN_NEWLINE, 找到: true
```

### 验证要点

- `【` 和 `[` 都返回 `TOKEN_LEFT_BRACKET`——这就是中英双语支持
- `x` 不在表中——它会被当作普通文本处理
- `\n` 在表中，被识别为 `TOKEN_NEWLINE`——它有语法意义，会被返回

**注意**：空格不在验证列表中，因为空格被 `skipWhitespace()` 提前消费了，我们验证的重点是"会被 Lexer 返回的符号"。

## 九、这种设计的优势

| 优势                      | 说明                                        |
| ------------------------- | ------------------------------------------- |
| **新增符号不改 Lexer**    | 只需在 `symbolMap` 加一行，Lexer 自动识别   |
| **中英双语自然支持**      | 同一 TokenType 对应多个字符                 |
| **判断逻辑集中**          | 所有符号的判断都在一张表里                  |
| **Category 提供额外维度** | 可以按分类批量处理（如跳过所有 whitespace） |

**核心思想：让数据决定一切。**

## 十、小结

这一篇完成了两件事：

| 做了什么                      | 为什么                                             |
| ----------------------------- | -------------------------------------------------- |
| 扩充了 Token 类型             | Lexer 需要识别更多符号（冒号、连字符、引用符号等） |
| 创建了 `symbolMap` 符号映射表 | 集中管理所有符号，Lexer 通过查表判断               |

**下一篇：实现 Lexer，真正把文本变成 Token 流。**
