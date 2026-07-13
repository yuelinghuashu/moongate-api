---
title: 从零实现词法分析器（二）：用一张表统一管理所有符号
description: 避免 Lexer 中出现大量 switch-case，引入符号映射表（symbolMap）统一管理所有符号，实现中英双语支持和数据驱动设计。
permalink: ef2d72a9-756a-4c35-8ad5-53f698ad8986
date: 2026-07-13 21:00:00
series: lexer-from-scratch
level: 
tags:
  - Go
  - Compiler
---

> 上一篇我们定义了 Token，但 Token 只是“零件”。在实现 Lexer 之前，还有一个问题要先解决——如何让 Lexer 能认识所有符号，而不需要每次新增符号都修改它的代码。

## 一、回顾：上一篇我们停在了哪里

上一篇我们定义了 Token：

```go
package parser

type TokenType string

const (
	LEFT_BRACKET  TokenType = "LEFT_BRACKET"  // 【
	RIGHT_BRACKET TokenType = "RIGHT_BRACKET" // 】
	TEXT          TokenType = "TEXT"          // 普通文本
)

type Token struct {
	Type    TokenType
	Literal string
}
```

现在我们需要一个程序——**Lexer（词法分析器）**——来读取文本，不断产出这些 Token。

但在写 Lexer 之前，还有一个问题要先解决：**Lexer 需要识别很多种符号，如果每个符号都在 Lexer 里单独判断，代码会变得又长又乱。**

## 二、问题：Lexer 需要认识多种符号

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
| **判断逻辑分散**     | 符号的“定义”和“判断”混在一起，不容易维护                 |

**我们需要一种更好的方式：把“符号是什么”和“怎么判断符号”分开。**

## 三、方案：用一张表统一管理所有符号

核心思想很简单：**把所有的符号集中在一张表里管理，Lexer 通过查表来判断。**

这张表里，每个符号记录两件事：

1. **对应的 Token 类型**：如 `LEFT_BRACKET`、`RIGHT_BRACKET`、`COLON`
2. **分类**：如 `bracket`（括号）、`colon`（冒号）

Lexer 只需要查这张表，就能知道当前字符是什么类型的 Token。

## 四、关键决策：用 `rune` 而不是 `byte`

在定义映射表之前，先确定一个关键问题：用什么类型来存储字符？

在 Go 中，遍历字符串有两种方式：

| 方式   | 类型   | 特点                                    |
| ------ | ------ | --------------------------------------- |
| `byte` | 1 字节 | 只能处理 ASCII 字符（英文、数字、标点） |
| `rune` | 4 字节 | 能处理任意 Unicode 字符（中文、Emoji）  |

`【` 在 UTF-8 中占 **3 个字节**。如果按 `byte` 遍历，`【` 会被拆成 3 个独立的字节，无法正确识别。

所以必须用 `rune`——它能保证每个字符（无论中英文）都被当作一个整体处理。

**用 `rune` 的代价：** Go 的 `string` 底层是 `[]byte`，把它转成 `[]rune` 需要额外分配内存。但对于我们处理的 `.meph` 文件（通常只有几 KB 到几百 KB），这个代价可以忽略不计。**正确性远比微小的性能损耗重要。**

## 五、定义映射表

### 5.1 结构体定义

创建 `parser/symbols.go`：

```go
package parser

// SymbolInfo 符号信息
type SymbolInfo struct {
	TokenType TokenType // 对应的 Token 类型
	Category  string    // 分类：bracket, colon, hyphen, ...
}
```

### 5.2 映射表

```go
// symbolMap 所有符号的映射表
// 新增符号只需在这里加一行
var symbolMap = map[rune]SymbolInfo{
	// 括号
	'【': {TokenType: LEFT_BRACKET, Category: "bracket"},
	'】': {TokenType: RIGHT_BRACKET, Category: "bracket"},
	'[':  {TokenType: LEFT_BRACKET, Category: "bracket"},
	']':  {TokenType: RIGHT_BRACKET, Category: "bracket"},

	// 冒号
	'：': {TokenType: COLON, Category: "colon"},
	':':  {TokenType: COLON, Category: "colon"},

	// 连字符
	'-':  {TokenType: HYPHEN, Category: "hyphen"},
	'—':  {TokenType: HYPHEN, Category: "hyphen"},
	'•':  {TokenType: HYPHEN, Category: "hyphen"},
}
```

**关键设计：** 你看，`【` 和 `[` 都被定义为 `LEFT_BRACKET`。这就是中英双语支持的核心——**同一个 Token 类型可以对应多个不同字符。** 用户写 `【角色名】` 或 `[角色名]`，Lexer 都能正确识别。

## 六、基于映射表的符号查询

有了映射表，查询符号信息变得极其简单——直接查表即可：

```go
// GetSymbolInfo 查询符号信息
// 返回值：(SymbolInfo, bool)，bool 表示是否找到
func GetSymbolInfo(ch rune) (SymbolInfo, bool) {
    info, ok := symbolMap[ch]
    return info, ok
}
```

这个函数虽然简单，但它是整个符号系统的**唯一查询入口**。外部包（如 `main.go`）通过它来查询符号信息，而 `symbolMap` 本身是私有的，保证了数据不会被意外修改。

在 Lexer 内部，我们直接查表：

```go
// lexer.go 中
info, ok := symbolMap[ch]
if !ok {
    // 不在表中 → 普通文本
    return l.readText()
}
// 在表中 → 根据 TokenType 返回对应 Token
l.advance()
return Token{Type: info.TokenType, Literal: string(ch), Line: l.line}
```

**注意：** `GetSymbolInfo` 是给外部包用的（比如测试代码），Lexer 内部直接访问 `symbolMap`，不经过这个函数包装。

**这种设计的优势：** 当 Lexer 需要判断一个字符是什么时，只需要一次 map 查询，不需要一堆 `if-else` 或 `switch-case`。所有符号的判断逻辑都集中在 `symbolMap` 这一张表里。

## 七、从设计角度看这个方案

这个方案背后的设计原则是 **“数据驱动”**：

| 做法                  | 说明                                       |
| --------------------- | ------------------------------------------ |
| **数据（symbolMap）** | 集中定义所有符号及其 TokenType 和 Category |
| **逻辑（查表）**      | Lexer 只查表，不包含任何具体字符的判断     |

你可能会问：**那判断函数去哪了？**

答案是：**不需要了。**

以前我们需要 `IsLeftBracket(ch)` 来判断一个字符是不是左括号，现在只需要：

```go
info, ok := symbolMap[ch]
if ok && info.Category == "bracket" && info.TokenType == LEFT_BRACKET {
    // 是左括号
}
```

而在 `NextToken()` 中，逻辑被精简到极致：

```go
func (l *Lexer) NextToken() Token {
    l.skipWhitespace()
    ch := l.peek()

    if ch == 0 {
        return Token{Type: TOKEN_EOF, Literal: "", Line: l.line}
    }

    info, ok := symbolMap[ch]
    if !ok {
        return l.readText()
    }

    l.advance()
    return Token{Type: info.TokenType, Literal: string(ch), Line: l.line}
}
```

整个函数只有一次 map 查询：

1. 跳过空白字符（空格、制表符、回车）
2. 查看当前字符，查 `symbolMap`
3. 不在表中 → 普通文本（`readText()`）
4. 在表中 → 直接返回 `info.TokenType`

不需要额外的判断函数，不需要 `if-else` 链，新增符号只需修改 `symbolMap`。

这样做的好处：

| 好处                   | 说明                                             |
| ---------------------- | ------------------------------------------------ |
| **新增符号不改 Lexer** | 只需在 `symbolMap` 加一行，Lexer 自动识别        |
| **中英双语自然支持**   | 同一 TokenType 可以对应多个字符                  |
| **代码极简**           | Lexer 中没有 `if-else` 链，只有一次 map 查询     |
| **判断函数全部删除**   | `IsLeftBracket`、`IsRightBracket` 等函数全部消失 |

**核心区别**：不再在之后的 `NextToken()` 中单独处理换行符，全部交给 `symbolMap` 查表。让数据决定一切。

## 八、验证

在 `main.go` 中测试：

```go
package main

import (
	"fmt"
	"mephisto/parser"
)

func main() {
	info, ok := parser.GetSymbolInfo('【')
	fmt.Println(info.TokenType) // LEFT_BRACKET

	info, ok = parser.GetSymbolInfo('[')
	fmt.Println(info.TokenType) // LEFT_BRACKET

	info, ok = parser.GetSymbolInfo('x')
	fmt.Println(ok) // false
}
```

输出：

```text
LEFT_BRACKET
LEFT_BRACKET
false
```

## 九、小结

这一篇完成了一件事：**为 Lexer 准备好一张统一的符号查找表。**

| 做了什么                      | 为什么                               |
| ----------------------------- | ------------------------------------ |
| 创建了 `symbolMap` 符号映射表 | 集中管理所有符号，Lexer 通过查表判断 |
| 删除了所有 `IsXxx()` 判断函数 | 查表即可，不需要额外的包装函数       |
| 解决了中英双语支持            | 同一类型可以对应多个字符             |
| 确认了用 `rune` 处理中文      | 避免 UTF-8 多字节字符被拆散          |

**下一篇：实现 Lexer，真正把文本变成 Token 流。**
