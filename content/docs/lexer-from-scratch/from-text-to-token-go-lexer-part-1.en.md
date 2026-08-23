---
title: 'Building a Lexer from Scratch (Part 1): Token — the Smallest Unit a Computer Can Understand'
description: 'Starting from the pain point that computers cannot understand text, learn what a Token is, define your first Token struct in Go, and take the first step toward a lexer.'
date: 2026-07-13 20:00:00
permalink: d6784b4c-89b0-4a75-b303-10b49c67d576
series: lexer-from-scratch
level: P1
tags:
  - Go
  - Engineering
---

## 📚 Series Navigation

This series has three parts:

1. [**Building a Lexer from Scratch (Part 1): Token**](./from-text-to-token-go-lexer-part-1) — The concept of Token and the first struct
2. [**Building a Lexer from Scratch (Part 2): Symbol Table**](./from-text-to-token-go-lexer-part-2) — Managing every symbol with one unified table
3. [**Building a Lexer from Scratch (Part 3): Lexer Engine**](./from-text-to-token-go-lexer-part-3) — A pointer-driven lexing engine

---

> To a computer, a text file is just a string of characters with no structure at all. Our first job is to make the computer "see" what is hidden inside those characters.

## 1. The Problem: Computers Don't Understand Text Structure

Suppose I have a text file that looks like this:

```text
【角色名】
贝利亚奥特曼
```

As a human, I can tell at a glance:

- `【角色名】` is a heading (in Chinese, it reads "character name")
- `贝利亚奥特曼` ("Belial Ultraman") is its content

But this is what the computer sees:

```text
[ 0xE3 0x80 0x90 0xE8 0xA7 0x92 0xE8 0x89 0xB2 0xE5 0x90 0x8D 0xE3 0x80 0x91 0x0A 0xE8 0xB4 0x9D 0xE5 0x88 0xA9 0xE4 0xBA 0x9A 0xE5 0xA5 0xA5 0xE7 0x89 0xB9 0xE6 0x9B 0xBC ]
```

This is what text looks like to a computer — a raw stream of bytes based on **UTF-8 encoding**. It has no idea that `0xE3 0x80 0x90` spells the Chinese character `【`, nor that `【` and `】` form a matching pair.

All it sees is a string of meaningless numbers.

So if I want a program to understand text in this format, the first problem to solve is: **make the computer "recognize" the structure hidden in the text.**

## 2. What To Do: Split Into Small Pieces and Label Them

The computer doesn't understand a concept like "section heading", but it can recognize characters.

If I split this text into the smallest "meaningful pieces" and attach a **fixed label** to each one, the computer can understand it step by step.

For example, split:

```text
【角色名】
贝利亚奥特曼
```

into:

| Piece | Label | Meaning |
| -------------- | --------------- | ------------ |
| `【` | `LEFT_BRACKET` | left bracket |
| `角色名` | `TEXT` | plain text ("character name") |
| `】` | `RIGHT_BRACKET` | right bracket |
| `\n` | (newline) | a line break in the text |
| `贝利亚奥特曼` | `TEXT` | plain text ("Belial Ultraman") |

Now the program can tell:

1. There is a left bracket here
2. Followed by a piece of text ("character name")
3. Then a right bracket
4. After a newline, another piece of text ("Belial Ultraman")

**Next, the program can use this pattern to recognize that the `【...】` pair wraps a heading, and the lines below it are the content.**

## 3. Token: The Smallest Labeled Piece

This kind of **smallest piece with a label** is called a **Token**.

Each Token needs to record two things:

1. **Type**: What is it? (A left bracket? Text? A right bracket?)
2. **Literal**: What does it look like? (`【`? `角色名`?)

The process of splitting text into tokens is called **lexical analysis**.

The program that performs lexical analysis is called a **Lexer**.

## 4. Defining the Token: The First Line of Code

### 4.1 Project Structure

Before writing any code, set up a clean, modular directory:

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

**Note:** all parsing-related code lives under the `parser/` directory. As the series progresses, we always work inside the same package — the directory only *gains files*, it never *switches packages*. From start to finish, you only need to follow the evolution of a single directory: `internal/parser/`.

### 4.2 Initialize the Go Module

```bash
go mod init mephisto
```

### 4.3 Define the Token Type and Struct

Create `parser/token.go`:

```go
package parser

// TokenType represents the type of a Token.
// We use string instead of int so the type name is visible when debugging.
type TokenType string

const (
	TOKEN_LEFT_BRACKET  TokenType = "LEFT_BRACKET"  // 【
	TOKEN_RIGHT_BRACKET TokenType = "RIGHT_BRACKET" // 】
	TOKEN_TEXT          TokenType = "TEXT"          // plain text
	// More token types will be introduced in later parts of this series.
)

// Token is the smallest meaningful piece of text a computer can understand.
type Token struct {
	Type    TokenType // type: LEFT_BRACKET? TEXT?
	Literal string    // literal: "【"? "角色名"?
	Line    int       // The Line field will be introduced in a later part (for error messages).
}
```

**Why use `string` instead of `int` for the type?**

If the type were a number, debugging would print a number and you'd have to dig through the code to figure out what `0` means. With `string`, `fmt.Println(tok.Type)` prints `"LEFT_BRACKET"` directly — much more readable. A lexer typically deals with only a few hundred tokens, so the performance cost of `string` is negligible.

**Why are the `Token` fields capitalized?**

In Go, **fields starting with an uppercase letter are exported; lowercase fields are private**. `Token` is used by `main.go`, so its fields must be capitalized, otherwise the external package cannot access them.

## 5. Verification: Make the Code Compile

### 5.1 Create the Entry File

Create `main.go` in the project root. For now it only reads a file and prints its contents — the `parser` package isn't used yet, but the project skeleton is in place:

```go
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: mephisto <file>")
		os.Exit(1)
	}

	content, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Printf("failed to read file: %v\n", err)
		os.Exit(1)
	}

	fmt.Print(string(content))
}
```

### 5.2 Create the Test File

`testdata/sample.meph`:

```text
【角色名】
贝利亚奥特曼
```

### 5.3 Run

```bash
go run main.go testdata/sample.meph
```

If you see the file contents printed, the project runs:

```text
【角色名】
贝利亚奥特曼
```

## 6. Summary

This part accomplished one thing: **laying the foundation for the whole series.**

| What we did | Why |
| ----------------------------- | -------------------------------- |
| Understood the concept of Token | A Token is the smallest unit of text a computer can understand |
| Defined `TokenType` and `Token` | The data structures the Lexer will need |
| Set up the project skeleton | Everything that follows builds on this base |

**Next up: implementing the Lexer — making the program automatically "spit out" tokens from text.**

## Complete Code

**`parser/token.go`**

```go
package parser

type TokenType string

const (
	TOKEN_LEFT_BRACKET  TokenType = "LEFT_BRACKET"
	TOKEN_RIGHT_BRACKET TokenType = "RIGHT_BRACKET"
	TOKEN_TEXT          TokenType = "TEXT"
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
	if len(os.Args) < 2 {
		fmt.Println("Usage: mephisto <file>")
		os.Exit(1)
	}

	content, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Printf("failed to read file: %v\n", err)
		os.Exit(1)
	}

	fmt.Print(string(content))
}
```
