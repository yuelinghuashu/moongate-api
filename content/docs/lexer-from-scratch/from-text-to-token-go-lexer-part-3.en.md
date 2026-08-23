---
title: 'Building a Lexer from Scratch (Part 3): Moving the Pointer and Starting the Lexer Conveyor Belt'
description: 'Implement the core lexer engine: move the cursor pointer, query the symbol table, and slice a text string into a structured stream of tokens.'
date: 2026-07-13 22:00:00
permalink: 7d80bcad-0ef8-4a03-a22c-11a19494ce5a
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

> In the first two parts we prepared the Token component and the unified symbol map. In this part, we officially implement the core engine of the whole lexical analysis — the **Lexer** — so the program can truly "read" text and slice it into a continuous stream of tokens.

## 1. The Core Idea: A Lexer Is Like a Conveyor Belt

Before writing any code, build a mental model. A lexer works very much like a **scanning conveyor belt** on a factory floor:

1. The **input text** is laid flat on the belt, with every character (a `rune`) occupying one slot.
2. There is a **cursor (pointer)** pointing at the character currently being examined.
3. The Lexer's job is:

- Take a peek at the character under the cursor (`peek`).
- If it's a symbol (like the left bracket `【`), wrap it into a token, then move the cursor one slot forward (`advance`).
- If it's a run of continuous plain text (like `贝利亚奥特曼`, "Belial Ultraman"), keep gluing characters together into one `TEXT` token until it hits a symbol.

## 2. Defining the Lexer Struct

Create `lexer.go` in the `internal/parser/` directory. We need three basic fields to maintain the state of this "conveyor belt":

```go
package parser

// Lexer is the lexical analyzer.
type Lexer struct {
	input    []rune // the full text to scan (converted to a rune slice for full Chinese and Emoji support)
	position int    // index of the character currently being scanned
	line     int    // current line number (starting at 1, used for error messages later)
}

// NewLexer creates a lexer instance.
func NewLexer(input string) *Lexer {
	return &Lexer{
		input:    []rune(input), // convert the string to []rune
		position: 0,
		line:     1,
	}
}
```

### Why convert with `[]rune(input)` up front?

We stressed in the last part that Go's `string` is a byte stream under the hood. If we had to compute byte boundaries on every read, handling Chinese would be a nightmare. By converting to a `[]rune` array at initialization, every `position++` step of the cursor now **reliably represents one standalone character — a Chinese character, an English letter, or an Emoji.**

## 3. The Three Workhorses: Basic Helper Methods

To manipulate the cursor, we implement three basic helpers: check the end, take a peek, move forward.

```go
// isEOF checks whether the cursor has reached the end of the file (End of File).
func (l *Lexer) isEOF() bool {
	return l.position >= len(l.input)
}

// peek looks at the character at the current position, but 【never moves the cursor】.
// Returns 0 if we've reached the end.
func (l *Lexer) peek() rune {
	if l.isEOF() {
		return 0
	}
	return l.input[l.position]
}

// advance consumes the current character: reads and returns it, while 【moving the cursor one step forward】.
// Special case: if the consumed character is a newline '\n', the line number is bumped.
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

With these three basic methods, the cursor can travel through the text array safely and freely.

## 4. Skipping Meaningless Whitespace

> In a `.meph` file, users might put spaces or indentation around brackets, like `【角色名】  `.
>
> These spaces carry no meaning — the Lexer should silently swallow them before recognizing the next token.

Since we're inside the same package (`package parser`), we skip the heavy function wrappers and use Go's native map lookup to skip whitespace:

```go
// skipWhitespace skips meaningless whitespace (space, tab, carriage return).
// Note: never skip newlines '\n' — in our syntax a newline marks the end of a section heading.
func (l *Lexer) skipWhitespace() {
	for !l.isEOF() {
		info, ok := symbolMap[l.peek()]
		// If it's in the symbol map and its category is whitespace, consume it.
		if !ok || info.Category != "whitespace" {
			break
		}
		l.advance() // the cursor marches relentlessly forward
	}
}
```

## 5. The Core Logic: Implementing the `NextToken()` Dispatcher

Its job: skip whitespace, watch the cursor position. If the character is in the symbol map, consume it and return right away; if it's not in the map, it must be plain text!

```go
// NextToken returns the next token.
func (l *Lexer) NextToken() Token {
	// 1. On every call, first clean up the meaningless whitespace ahead.
	l.skipWhitespace()

	// 2. See which character the cursor is pointing at.
	ch := l.peek()

	// 3. Edge case: if we've reached the end of the file, emit the EOF sentinel token.
	if ch == 0 {
		return Token{Type: TOKEN_EOF, Literal: "", Line: l.line}
	}

	// 4. 🚀 The ultimate table-driven dispatch: since we're in the same package, match symbolMap directly!
	if info, ok := symbolMap[ch]; ok {
		l.advance() // a symbol? advance the cursor and consume it!
		return Token{Type: info.TokenType, Literal: string(ch), Line: l.line}
	}

	// 5. Not in the symbol map? Then it must be plain text (like "贝利亚奥特曼").
	return l.readText()
}
```

See? Thanks to the same-package table lookup, the entire dispatch core is clean enough to make you emotional. **There are no complex conditional branches here, and no unnecessary nested function calls.** No matter how many symbols your grammar grows to, the big skeleton of `NextToken()` stands rock-solid — it will never need a single line changed.

## 6. Greedy Text Reading: `readText()`

We've seen the core logic of `NextToken()`. Now let's look at the last branch it calls — `readText()` — and see how plain text gets swallowed in one gulp.

When a character isn't in the symbol map (for example a Chinese character like `贝`), the Lexer enters "greedy mode": **as long as the following characters aren't symbols, gobble them all up into one long string.**

Thanks to the unified symbol map from the last part, deciding "when to stop" becomes almost impossibly elegant: **as soon as the character returned by `peek()` matches an entry in `symbolMap`, you've hit a symbol (a newline or a bracket, say), which is a natural delimiter — text reading stops immediately!**

```go
// readText reads a run of continuous plain text.
// Stop condition: hitting any symbol in the map (newline, bracket, colon, etc.) or the end of the file.
func (l *Lexer) readText() Token {
	// Record the start position.
	start := l.position

	for !l.isEOF() {
		// 🌟 Dimensional reduction: just look up the table! If the current character is in the symbol map,
		// we've hit a wall — stop immediately.
		_, ok := symbolMap[l.peek()]
		if ok {
			break
		}
		l.advance() // otherwise, keep happily devouring text
	}

	// Use a slice to turn the rune range the cursor walked over into a string literal.
	literal := string(l.input[start:l.position])
	return Token{Type: TOKEN_TEXT, Literal: literal, Line: l.line}
}
```

## 7. Done: Verify in `main.go`

Let's upgrade `main.go` to parse the test file with our hand-written Lexer and see if it smoothly produces the stream of parts we want:

```go
package main

import (
	"fmt"
	"mephisto/internal/parser"
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

	// 1. Initialize our conveyor-belt Lexer
	l := parser.NewLexer(string(content))

	// 2. Drive the belt in a loop until we hit TOKEN_EOF
	fmt.Printf("%-5s | %-20s | %s\n", "Line", "Type", "Literal")
	fmt.Println("------+----------------------+-----------")
	for {
		tok := l.NextToken()
		// Convert newline literals to "\\n" so the terminal doesn't break the table alignment.
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

Run our test file again (`testdata/sample.meph`):

```bash
go run main.go testdata/sample.meph
```

The terminal prints a beautifully aligned, industrial-grade token stream:

```text
Line   | Type                 | Literal
------+----------------------+-----------
1     | LEFT_BRACKET         | 【
1     | TEXT                 | 角色名
1     | RIGHT_BRACKET        | 】
1     | NEWLINE              | \n
2     | TEXT                 | 贝利亚奥特曼
3     | EOF                  |
```

Look! Through our Lexer, the computer successfully turned a cold, structureless string of raw bytes into vivid, structured token blocks, each carrying its own line number and type!

## 8. Summary

With this part, our **Lexer is fully born**!

| What we did | Why |
| -------------------------------------- | ----------------------------------------------------------------------- |
| Designed the cursor state machine (`position`) | Established the low-level conveyor-belt model for iterating multi-byte text |
| Implemented the purely table-driven `NextToken()` | Carried the data-driven idea to its end, removed redundant functions, and made symbol dispatch maximally efficient |
| Implemented the ultra-minimal `readText()` truncation | As soon as the peeked character matches `symbolMap`, it's automatically a boundary — eliminating all scattered detection logic |

And with that, the battle of lexical analysis is complete.
