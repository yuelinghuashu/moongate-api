---
title: 'Building a Lexer from Scratch (Part 2): Managing Every Symbol with a Single Table'
description: 'Avoid a wall of switch-cases in your lexer: introduce a symbol map (symbolMap) to manage every symbol in one table, with built-in bilingual (Chinese/English) support and a data-driven design.'
date: 2026-07-13 21:00:00
permalink: ef2d72a9-756a-4c35-8ad5-53f698ad8986
series: lexer-from-scratch
level: P1
tags:
  - Go
  - Compiler
---

## 📚 Series Navigation

This series has three parts:

1. [**Building a Lexer from Scratch (Part 1): Token**](./from-text-to-token-go-lexer-part-1) — The concept of Token and the first struct
2. [**Building a Lexer from Scratch (Part 2): Symbol Table**](./from-text-to-token-go-lexer-part-2) — Managing every symbol with one unified table
3. [**Building a Lexer from Scratch (Part 3): Lexer Engine**](./from-text-to-token-go-lexer-part-3) — A pointer-driven lexing engine

---

> In the last part we defined the Token, but a Token is only a "part". Before implementing the Lexer, there's one more problem to solve: how can the Lexer recognize every symbol without having its code changed every time a new symbol is added?

## 1. Recap: Where We Left Off

In the last part we defined the Token:

```go
package parser

type TokenType string

const (
	TOKEN_LEFT_BRACKET  TokenType = "LEFT_BRACKET"  // 【
	TOKEN_RIGHT_BRACKET TokenType = "RIGHT_BRACKET" // 】
	TOKEN_TEXT          TokenType = "TEXT"          // plain text
)

type Token struct {
	Type    TokenType
	Literal string
}
```

Now we need a program — the **Lexer** — to read text and keep producing these tokens.

But before writing the Lexer, we still need to do one thing: **expand the token types and build a mechanism that lets the Lexer recognize every symbol.**

## 2. Expanding the Token Types

In the last part we only defined three token types. But in a complete `.meph` file, besides `【`, `】` and plain text, there are more symbols to recognize:

| Symbol | Use | Example |
| ---------- | --------- | ---------------------------- |
| `：` / `:` | colon | `好感度：78` ("affection: 78") |
| `-` | list marker | `- 核心信念："力量就是一切"` ("core belief: might is everything") |
| `@` | reference symbol | `@[世界观](worlds/eva.meph)` ("worldview") |
| `#` | tag / comment | `# 硬约束` ("hard constraint") |

Here is the complete `parser/token.go`:

```go
package parser

type TokenType string

const (
	// Syntax symbols
	TOKEN_LEFT_BRACKET  TokenType = "LEFT_BRACKET"  // 【
	TOKEN_RIGHT_BRACKET TokenType = "RIGHT_BRACKET" // 】
	TOKEN_COLON         TokenType = "COLON"         // ：
	TOKEN_HYPHEN        TokenType = "HYPHEN"        // -
	TOKEN_AT            TokenType = "AT"            // @
	TOKEN_HASH          TokenType = "HASH"          // #

	// Content types
	TOKEN_TEXT TokenType = "TEXT" // plain text

	// Control markers
	TOKEN_NEWLINE TokenType = "NEWLINE" // newline
	TOKEN_EOF     TokenType = "EOF"     // end of file (sentinel)
)

type Token struct {
	Type    TokenType
	Literal string    // the raw text of the symbol
	Line    int       // current line number (starting at 1)
}
```

**Note:** `NEWLINE` and `EOF` are only actually used by the Lexer implementation, but we define them now so later parts won't need to go back and modify `token.go`.

## 3. The Problem: The Lexer Must Recognize Many Symbols

The Lexer's job is to scan text and identify what each symbol is.

The most direct way to write it:

```go
switch ch {
case '【':
    // return LEFT_BRACKET
case '】':
    // return RIGHT_BRACKET
case '：':
    // return COLON
case '-':
    // return HYPHEN
// ... more and more cases
}
```

Writing it this way has three problems:

| Problem | Explanation |
| -------------------- | -------------------------------------------------------- |
| **Adding a symbol means changing code** | Every new symbol type requires modifying the Lexer source |
| **Bilingual support is painful** | To support both `【` and `[` as left brackets, the `case`s keep growing |
| **Logic is scattered** | The "definition" of a symbol and its "detection" are mixed together, which is hard to maintain |

**We need a better way: separate "what a symbol is" from "how to detect a symbol".**

## 4. The Solution: Manage Every Symbol with a Single Table

The core idea is simple: **put all the symbols in one table and let the Lexer decide by looking it up.**

In this table, each symbol records two things:

1. **The corresponding token type**: e.g. `LEFT_BRACKET`, `RIGHT_BRACKET`, `COLON`
2. **Its category**: e.g. `bracket` (brackets), `colon` (colons)

The Lexer just looks up the table to know what kind of token the current character is.

## 5. A Key Decision: Use `rune` Instead of `byte`

Before defining the map, settle a key question: what type should we use to store a character?

In Go, there are two ways to iterate over a string:

| Way | Type | Characteristics |
| ------ | ------ | --------------------------------------- |
| `byte` | 1 byte | Only handles ASCII characters (English, digits, punctuation) |
| `rune` | 4 bytes | Handles any Unicode character (Chinese, Emoji) |

`【` takes **3 bytes** in UTF-8. If we iterate byte by byte, `【` gets split into 3 separate bytes and cannot be recognized correctly.

So we must use `rune` — it guarantees every character (Chinese or English) is treated as a single whole.

**The cost of `rune`:** Go's `string` is backed by `[]byte`, so converting it to `[]rune` allocates extra memory. But for the `.meph` files we process (usually a few KB to a few hundred KB), the cost is negligible. **Correctness matters far more than a tiny performance loss.**

## 6. Defining the Map

### 6.1 The Struct

Create `parser/symbols.go`:

```go
package parser

// SymbolInfo holds the information of a symbol.
type SymbolInfo struct {
    TokenType TokenType // the corresponding token type
    Category  string    // category: bracket, colon, hyphen, at, hash, whitespace, newline
}
```

### 6.2 The Map

```go
// symbolMap maps every symbol to its info.
// Adding a new symbol is just adding one line here.
var symbolMap = map[rune]SymbolInfo{
    // Brackets
    '【': {TokenType: TOKEN_LEFT_BRACKET, Category: "bracket"},
    '】': {TokenType: TOKEN_RIGHT_BRACKET, Category: "bracket"},
    '[':  {TokenType: TOKEN_LEFT_BRACKET, Category: "bracket"},
    ']':  {TokenType: TOKEN_RIGHT_BRACKET, Category: "bracket"},

    // Colons
    '：': {TokenType: TOKEN_COLON, Category: "colon"},
    ':':  {TokenType: TOKEN_COLON, Category: "colon"},

    // Hyphen
    '-':  {TokenType: TOKEN_HYPHEN, Category: "hyphen"},

    // Reference symbol
    '@':  {TokenType: TOKEN_AT, Category: "at"},

    // Tag / comment symbol
    '#': {TokenType: TOKEN_HASH, Category: "hash"},

    // Whitespace (space, tab, carriage return)
    // Note: these are consumed early by skipWhitespace() and never returned as tokens.
    ' ':  {TokenType: TOKEN_TEXT, Category: "whitespace"},
    '\t': {TokenType: TOKEN_TEXT, Category: "whitespace"},
    '\r': {TokenType: TOKEN_TEXT, Category: "whitespace"},

    // Newline
    '\n': {TokenType: TOKEN_NEWLINE, Category: "newline"},
}
```

**Key design notes:**

| Design point | Explanation |
| ------------------------------- | ------------------------------------------------------------------------- |
| **One token type maps to many characters** | `【` and `[` are both `TOKEN_LEFT_BRACKET` — bilingual support comes for free |
| **Category is used for filtering** | The Lexer can skip spaces via `Category == "whitespace"` |
| **`\n` is `TOKEN_NEWLINE`** | Newlines carry syntax meaning and are returned as tokens (unlike spaces, which are skipped) |
| **Space is `TOKEN_TEXT`** | Spaces are never returned (consumed early by `skipWhitespace`); `TOKEN_TEXT` is just a placeholder |

## 7. Looking Up Symbols via the Map

With the map in place, querying symbol info becomes trivial — just look it up:

```go
// GetSymbolInfo looks up the info of a symbol.
// Returns: (SymbolInfo, bool), where bool reports whether it was found.
// This is the query entry point for external packages (e.g. test code).
func GetSymbolInfo(ch rune) (SymbolInfo, bool) {
    info, ok := symbolMap[ch]
    return info, ok
}
```

**This function is the only visible surface of `symbolMap`**: it tells the caller "is this character a symbol we know? If so, what type is it?"

In the next part, we'll see how the Lexer uses `symbolMap` internally to drive the entire lexical analysis with a single lookup. For now, let's verify that the table works.

## 8. Verification: Is the Lookup Logic Correct?

Test `GetSymbolInfo` in `main.go`:

```go
package main

import (
    "fmt"
    "mephisto/internal/parser"
)

func main() {
    // Look up the Chinese left bracket
    info, ok := parser.GetSymbolInfo('【')
    fmt.Printf("'【' → %s, found: %v\n", info.TokenType, ok) // LEFT_BRACKET, true

    // Look up the English left bracket
    info, ok = parser.GetSymbolInfo('[')
    fmt.Printf("'[' → %s, found: %v\n", info.TokenType, ok) // LEFT_BRACKET, true

    // Look up an ordinary letter (not in the table)
    info, ok = parser.GetSymbolInfo('x')
    fmt.Printf("'x' → found: %v\n", ok) // false

    // Look up the newline character
    info, ok = parser.GetSymbolInfo('\n')
    fmt.Printf("'\\n' → %s, found: %v\n", info.TokenType, ok) // NEWLINE, true
}
```

Output:

```text
'【' → TOKEN_LEFT_BRACKET, found: true
'[' → TOKEN_LEFT_BRACKET, found: true
'x' → found: false
'\n' → TOKEN_NEWLINE, found: true
```

**What to verify:**

- Both `【` and `[` return `TOKEN_LEFT_BRACKET` — that is the bilingual support
- `x` is not in the table — it will be treated as plain text
- `\n` is in the table and recognized as `TOKEN_NEWLINE` — it carries syntax meaning and is returned

**Note:** spaces are not in the verification list, because they are consumed early by `skipWhitespace()`. We only verify the symbols that the Lexer actually returns.

## 9. The Advantages of This Design

| Advantage | Explanation |
| ------------------------- | ------------------------------------------- |
| **New symbols don't touch the Lexer** | Just add one line to `symbolMap` and the Lexer recognizes it automatically |
| **Bilingual support for free** | One token type maps to multiple characters |
| **Detection logic is centralized** | All symbol detection lives in one table |
| **Category adds another dimension** | Process symbols by group (e.g. skip all whitespace at once) |

**The core idea: let the data decide everything.**

## 10. Summary

This part accomplished two things:

| What we did | Why |
| ----------------------------- | -------------------------------------------------- |
| Expanded the token types | The Lexer needs to recognize more symbols (colon, hyphen, reference symbol, etc.) |
| Created the `symbolMap` | Centralizes all symbols so the Lexer decides by looking up the table |

**Next part: implementing the Lexer and actually turning text into a stream of tokens.**
