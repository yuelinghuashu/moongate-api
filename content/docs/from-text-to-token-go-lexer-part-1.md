---
title: 从零实现词法分析器（一）：Token——计算机的最小理解单位
description: 从计算机不认识文本的痛点出发，理解 Token 的概念，并用 Go 定义第一个 Token 结构体，完成词法分析器的第一步。
permalink: d6784b4c-89b0-4a75-b303-10b49c67d576
date: 2026-07-13 20:00:00
series: lexer-from-scratch
level: P1
tags:
  - Go
  - Engineering
---

> 一个文本文件对计算机来说只是一串字符，毫无结构可言。我们要做的第一件事，就是让计算机能“认出”这些字符里藏着什么。

## 一、问题：计算机看不懂文本的结构

假设我有一个文本文件，内容是这样的：

```text
【角色名】
贝利亚奥特曼
```

我（人类）一眼就能看出：

- `【角色名】` 是一个标题
- `贝利亚奥特曼` 是它的内容

但计算机看到的是：

```text
[ 0xE3 0x80 0x90 0xE8 0xA7 0x92 0xE8 0x89 0xB2 0xE5 0x90 0x8D 0xE3 0x80 0x91 0x0A 0xE8 0xB4 0x9D 0xE5 0x88 0xA9 0xE4 0xBA 0x9A 0xE5 0xA5 0xA5 0xE7 0x89 0xB9 0xE6 0x9B 0xBC ]
```

这就是计算机眼中的文本——一串基于 **UTF-8 编码**的原始字节流。它不知道 `0xE3 0x80 0x90` 连起来就是中文的 `【`，也不知道 `【` 和 `】` 是配对的。

它看到的只是一串毫无意义的数字。

如果我想让程序读懂这种格式的文本，第一个要解决的问题就是：**让计算机能“认出”文本的结构。**

## 二、怎么办：拆成小块，贴上标签

计算机不认识“区块标题”这种概念，但它能识别字符。

如果我把这段文本拆成最小的“有意义的片段”，并给每个片段贴上**固定的标签**，计算机就能一步步理解它了。

比如，把：

```text
【角色名】
贝利亚奥特曼
```

拆成：

| 片段           | 标签            | 说明         |
| -------------- | --------------- | ------------ |
| `【`           | `LEFT_BRACKET`  | 左括号       |
| `角色名`       | `TEXT`          | 普通文本     |
| `】`           | `RIGHT_BRACKET` | 右括号       |
| `\n`           | （换行符）      | 文本中的换行 |
| `贝利亚奥特曼` | `TEXT`          | 普通文本     |

这样，程序就能知道：

1. 这里有一个左括号
2. 后面跟了一段文本（“角色名”）
3. 然后是一个右括号
4. 换行后，又有一段文本（“贝利亚奥特曼”）

**下一步，程序就可以根据这个规律，识别出“【】”包裹的是标题，标题下面跟着的是内容。**

## 三、Token：有标签的最小片段

这种**有标签的最小片段**，就叫 **Token**。

每个 Token 需要记录两件事：

1. **类型**：这是什么？（左括号？文本？右括号？）
2. **字面量**：它长什么样？（`【`？`角色名`？）

把文本拆成 Token 的过程，叫**词法分析**。

执行词法分析的程序，叫 **Lexer（词法分析器）**。

## 四、定义 Token：第一行代码

### 4.1 项目结构

在开始写代码前，先建立清晰的模块化目录：

```text
mephisto/
├── go.mod
├── main.go
├── testdata/
│   └── sample.meph
└── internal/
    └── parser/
        ├── token.go
        ├── symbols.go
        └── lexer.go
```

**注意：** 所有解析相关的代码都放在 `parser/` 目录下，这样随着系列文章的推进，我们始终在同一个包里工作，目录结构的变化只是“新增文件”，不是“切换包”。读者从头到尾只需要关注 `internal/parser/` 这一个目录的演进。

### 4.2 初始化 Go 模块

```bash
go mod init mephisto
```

### 4.3 定义 Token 类型与结构体

创建 `parser/token.go`：

```go
package parser

// TokenType 表示 Token 的类型
// 用 string 而不是 int，调试时可以直接看到类型名
type TokenType string

const (
	LEFT_BRACKET  TokenType = "LEFT_BRACKET"  // 【
	RIGHT_BRACKET TokenType = "RIGHT_BRACKET" // 】
	TEXT          TokenType = "TEXT"          // 普通文本
	// 更多 Token 类型将在后续文章中逐步引入
)

// Token 是计算机理解文本的最小有意义片段
type Token struct {
	Type    TokenType // 类型：LEFT_BRACKET？TEXT？
	Literal string    // 字面量："【"？"角色名"？
	// Line 字段将在后续文章中引入（用于错误提示）
}
```

**为什么用 `string` 而不是 `int` 定义类型？**

如果用一个数字表示类型，调试时打印出来的是一串数字，需要翻代码才能知道 `0` 代表什么。而用 `string`，`fmt.Println(tok.Type)` 直接打印 `"LEFT_BRACKET"`，可读性高得多。词法分析器处理的 Token 数量通常只有几百个，`string` 的性能开销可以忽略不计。

**为什么 `Token` 的字段要大写？**

在 Go 里，**大写字母开头的字段是公开的**，小写是私有的。`Token` 会被 `main.go` 使用，所以它的字段必须大写，否则外部包无法访问。

## 五、验证：让代码能编译

### 5.1 创建入口文件

在项目根目录创建 `main.go`。目前它只负责读取文件并打印内容——虽然还没有用到 `parser` 包，但项目的骨架已经搭建完毕：

```go
package main

import (
	"fmt"
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

	fmt.Print(string(content))
}
```

### 5.2 创建测试文件

`testdata/sample.meph`：

```text
【角色名】
贝利亚奥特曼
```

### 5.3 运行

```bash
go run main.go testdata/sample.meph
```

看到文件内容输出，说明项目能跑了：

```text
【角色名】
贝利亚奥特曼
```

## 六、小结

这一篇完成了一件事：**为整个系列打下地基。**

| 做了什么                      | 为什么                           |
| ----------------------------- | -------------------------------- |
| 理解了 Token 的概念           | Token 是计算机理解文本的最小单位 |
| 定义了 `TokenType` 和 `Token` | 为后续实现 Lexer 准备好数据结构  |
| 搭建了项目骨架                | 后续所有代码都在这个基础上扩展   |

**接下来我们要做的就是实现 Lexer——让程序能自动从文本里“吐”出 Token。**

## 完整代码

**`parser/token.go`**

```go
package parser

type TokenType string

const (
	LEFT_BRACKET  TokenType = "LEFT_BRACKET"
	RIGHT_BRACKET TokenType = "RIGHT_BRACKET"
	TEXT          TokenType = "TEXT"
)

type Token struct {
	Type    TokenType
	Literal string
}
```

**`main.go`**

```go
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) <  {
		fmt.Println("用法: mephisto <文件>")
		os.Exit(1)
	}

	content, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Printf("读取文件失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Print(string(content))
}
```
