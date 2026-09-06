---
title: 从零实现词法分析器（三）：让指针动起来，启动 Lexer 传送带
description: 正式实现词法分析器的核心引擎，通过光标指针的移动与符号表查询，将文本字符串切分成结构化的 Token 流。
date: 2026-07-13 22:00:00
series: lexer-from-scratch
tags:
  - Go
  - Compiler
---

## 一、核心思想：Lexer 就像一条传送带

在动手写代码前，我们先要在脑海里建立一个物理模型。词法分析器（Lexer）的运作方式，极其类似于工厂里的**扫描传送带**：

1. **输入文本**被平铺在传送带上，每个字符（`rune`）占据一个格子。
2. 有一个**光标（指针）**，指向当前正在观察的字符。
3. Lexer 的工作就是：

- 瞧一眼当前光标指着的字符是什么（`peek`）。
- 如果是一个符号（比如左括号 `【`），把它打包成 Token，然后光标往后挪一格（`advance`）。
- 如果是一串连续的普通文字（比如 `贝利亚奥特曼`），就把它们拼在一起打包成一个 `TEXT` Token，直到撞上符号再停下来。

## 二、定义 Lexer 结构体

在 `internal/parser/` 目录下创建 `lexer.go`。我们需要三个基础字段来维护这条“传送带”的状态：

```go
package parser

// Lexer 词法分析器结构体
type Lexer struct {
	input    []rune // 待扫描的完整文本（转为 rune 切片，完美支持中文与 Emoji）
	position int    // 当前扫描到的字符索引位置
	line     int    // 当前行号（从 1 开始，用于后续报错提示）
}

// NewLexer 创建一个词法分析器实例
func NewLexer(input string) *Lexer {
	return &Lexer{
		input:    []rune(input), // 将 string 转为 []rune 存储
		position: 0,
		line:     1,
	}
}
```

### 为什么用 `[]rune(input)` 提前转换？

我们在上一篇强调过，Go 的 `string` 底层是字节流（`byte`）。如果每次读取都去算字节，处理中文会非常痛苦。我们在初始化时直接将它转为 `[]rune` 数组，之后光标的 `position++` 移动的每一个单位，就**稳稳当当地代表一个独立的中文字符、英文字母或 Emoji**。

## 三、传送带的三驾马车：基础辅助方法

为了操纵这个光标，我们需要实现三个最基本的辅助方法：检查结束、瞅一眼、往前移。

```go
// isEOF 检查光标是否已经走到了文件末尾 (End of File)
func (l *Lexer) isEOF() bool {
	return l.position >= len(l.input)
}

// peek 瞅一眼当前位置的字符，但【绝不挪动光标】
// 如果已经到末尾，返回 0
func (l *Lexer) peek() rune {
	if l.isEOF() {
		return 0
	}
	return l.input[l.position]
}

// advance 消费当前字符：读取并返回它，同时【将光标向后移动一位】
// 特别注意：如果消费的是换行符 '\n'，顺手将行号 line++
func (l *Lexer) advance() rune {
	if l.isEOF() {
		return 0
	}
	ch := l.input[l.position]
	if ch == '\n' {
		l.line++
	}
	l.position++
	return ch
}
```

有了这三个基础方法，我们的光标就能安全、自由地在文本数组中穿梭了。

## 四、跳过无意义的空白

> 在 `.meph` 文件中，用户可能会在括号两边打上空格或缩进，比如 `【角色名】  `。
>
> 这些空格没有实际语义，Lexer 应该在识别下一个 Token 之前，默默把它们吞掉。

因为在同一个包（package parser）内部，我们直接越过复杂的函数封装，利用 Go 原生的 map 匹配来跳过空白：

```go
// skipWhitespace 跳过无意义的空白字符（空格、制表符、回车符）
// 注意：千万不能跳过换行符 '\n'，因为换行在我们的语法里代表“区块标题结束”
func (l *Lexer) skipWhitespace() {
	for !l.isEOF() {
		info, ok := symbolMap[l.peek()]
		// 如果在符号表里，且 Category 是 whitespace，就消费掉它
		if !ok || info.Category != "whitespace" {
			break
		}
		l.advance() // 光标无情后移
	}
}
```

## 五、核心逻辑：实现 `NextToken()` 分发器

它的职责是：跳过空白，盯着光标位置。如果是符号表里的符号，一律直接消费并返回；如果表里找不到，那它必然是普通文本！

```go
// NextToken 获取下一个 Token
func (l *Lexer) NextToken() Token {
	// 1. 每次进来，先清洗掉前面的无意义空格
	l.skipWhitespace()

	// 2. 查看当前光标指着谁
	ch := l.peek()

	// 3. 边界处理：如果到文件末尾了，吐出 EOF 哨兵 Token
	if ch == 0 {
		return Token{Type: TOKEN_EOF, Literal: "", Line: l.line}
	}

	// 4. 🚀 终极查表驱动：因为在同包内，直接匹配 symbolMap！
	if info, ok := symbolMap[ch]; ok {
		l.advance() // 是符号？光标前进，消费它！
		return Token{Type: info.TokenType, Literal: string(ch), Line: l.line}
	}

	// 5. 符号表里查不到？那它一定是普通文本文字（如 "贝利亚奥特曼"）
	return l.readText()
}
```

你看！得益于同包内直接查表的设计，整个分发核心干净得让人感动。**这里没有任何复杂的条件分支，更去掉了不必要的套娃函数调用**。无论未来你的语法扩充到有多少种符号，这个 `NextToken()` 的大框架都**稳如磐石，永远不需要修改一行。**

## 六、文本的贪婪读取：`readText()`

`NextToken()` 的核心逻辑我们已经看完了。现在进入它调用的最后一个分支——`readText()`，看看普通文本是如何被一口气吞下的。

当字符不在符号表里时（比如碰到了中文汉字 `贝`），Lexer 应该开启“贪婪模式”：**只要后面接下来的字符不是符号，就一路把它们全部吞下，拼成一个长字符串。**

有了上一篇建立的统一符号表，判定“什么时候该停下来”也变得不可思议的优雅：**只要 `peek()` 到的字符能在 `symbolMap` 里匹配成功，就说明撞到了符号（比如换行符或括号），它天然就是分隔符，文本读取立刻停止！**

```go
// readText 读取一段连续的普通文本
// 停止条件：遇到符号表中的任意符号（如换行符、括号、冒号）或文件结束
func (l *Lexer) readText() Token {
	// 记录起始位置
	start := l.position

	for !l.isEOF() {
		// 🌟 降维打击：直接查表！只要当前字符在符号表里，说明撞墙了，立刻停下
		_, ok := symbolMap[l.peek()]
		if ok {
			break
		}
		l.advance() // 否则，继续快乐地吞噬文字
	}

	// 利用切片，把这段光标走过的 rune 范围直接转成字符串字面量
	literal := string(l.input[start:l.position])
	return Token{Type: TOKEN_TEXT, Literal: literal, Line: l.line}
}
```

## 七、大功告成：在 `main.go` 中验证成果

让我们把现在的 `main.go` 升级一下，用我们亲手写的 `Lexer` 去解析测试文件，看看它能不能顺利吐出我们要的零件流：

```go
package main

import (
	"fmt"
	"mephisto/internal/parser"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("用法: mephisto <文件>")
		os.Exit(1)
	}

	content, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Printf("读取文件失败: %v\n", err)
		os.Exit(1)
	}

	// 1. 初始化我们的传送带 Lexer
	l := parser.NewLexer(string(content))

	// 2. 循环驱动传送带，直到撞上 TOKEN_EOF
	fmt.Printf("%-5s | %-20s | %s\n", "行号", "类型", "字面量")
	fmt.Println("------+----------------------+-----------")
	for {
		tok := l.NextToken()
		// 为了防止换行符 \n 导致终端实际换行破坏表格，打印时做个转换处理
		literal := tok.Literal
		if tok.Type == parser.TOKEN_NEWLINE {
			literal = "\\n"
		}

		fmt.Printf("%-5d | %-20s | %s\n", tok.Line, tok.Type, literal)

		if tok.Type == parser.TOKEN_EOF {
			break
		}
	}
}
```

再次运行我们的测试文件（`testdata/sample.meph`）：

```bash
go run main.go testdata/sample.meph
```

终端将会输出一行极其漂亮、对齐完美、极具工业美感的词法流结果：

```text
行号    | 类型                 | 字面量
------+----------------------+-----------
1     | LEFT_BRACKET         | 【
1     | TEXT                 | 角色名
1     | RIGHT_BRACKET        | 】
1     | NEWLINE              | \n
2     | TEXT                 | 贝利亚奥特曼
3     | EOF                  |
```

看！计算机通过我们写的 Lexer，成功把一串冰冷的、毫无结构的原始字节，变成了一个个生动的、自带行号和类型的结构化 Token 块！

## 八、小结

到这一篇为止，我们的 **Lexer 词法分析器已经完全体诞生了**！

| 做了什么                               | 为什么                                                                  |
| -------------------------------------- | ----------------------------------------------------------------------- |
| 设计了光标状态机（`position`）         | 建立了多字节文本遍历的底层传送带模型                                    |
| 实现了纯查表驱动的 `NextToken()`       | 贯彻了数据驱动思想，去掉了多余函数，让符号分发效率达到极致              |
| 实现了极其精简的 `readText()` 截断机制 | 只要 peek 字符在 `symbolMap` 中即自动作为边界，消灭了所有零散的判断逻辑 |

至此，词法分析的战役完美结束。
