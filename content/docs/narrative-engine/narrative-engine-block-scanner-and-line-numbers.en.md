---
title: 'LLM Narrative Engines, Part 3: Block Scanner and Line Numbers'
description: From format design to parser implementation — a handwritten block scanner that reports "line 12" instead of "position 246." Precise line number binding is the core value of a custom parser.
date: 2026-07-20 17:00:00
permalink: a6a93745-4f18-4778-98bb-e1405b4e5770
level: P3
series: narrative-engine
tags:
  - Go
  - DSL
  - Engineering
---

> **Before reading this**: If you jumped here from a search engine, I'd recommend skimming Part 1's "See the Target" and ".meph Design Principles" sections to get familiar with what `.meph` looks like and why it exists. This post assumes you already know what `【角色名】` and `【规则】` are.

A JSON parser reports `position 246`. What a creator needs is **"line 12: missing colon."** This post is about making that precise error reporting a reality.

---

## I. A Narrative Engine's Second Problem: How Do Rules Become Structure?

Once the format is defined, the next problem is **parsing** — how do we turn text into data the program can work with?

Two metrics matter here: **parsing correctness** and **error readability**. General-purpose formats (JSON/YAML) excel at the first but are a disaster on the second — `position 246` means nothing to a creator. And for a custom format, the parser must be written from scratch.

A well-designed parser should do three things:

1. **Precise recognition** — every block and every line must be correctly classified
2. **Line number binding** — every error must trace back to a specific line, not a byte offset
3. **Whitelist validation** — typos like `【脚色名】` (a misspelling of "Character Name") should not be silently treated as valid content

This post implements all three.

---

## II. See the Problem First: What Happens Without Line Numbers

Before diving into the parser, let's look at a real scenario.

A creator writes a contract with this line:

```meph
【锚点】
- 核心信念 "力量就是一切"
```

Notice: `- 核心信念 "力量就是一切"` is missing a colon. The correct form is `- 核心信念: "力量就是一切"`.

With a general-purpose parser, the error would be something like:

```plaintext
unexpected token at position 42
```

The creator has to copy-paste and count characters to figure out where position 42 is. This process is excruciating.

My goal was to make the parser report:

```plaintext
第 2 行（区块「锚点」）：缺少 ':' 或 '：'
```

No counting positions, no understanding what a "token" is — just "line 2, block '锚点': missing ':'."

**This difference is the entire reason for writing a custom parser.**

---

## III. Two-Phase Design

I split the parsing into two phases:

| Phase                     | Responsibility                         | Input     | Output             |
| ------------------------- | -------------------------------------- | --------- | ------------------ |
| **Block Scanner** (Lexer) | Split into blocks, record line numbers | Raw text  | `[]Block`          |
| **Structured Parser**     | Parse into structured data             | `[]Block` | `*domain.Contract` |

**Key design decision**: line numbers are bound during the scan phase. The parser receives them pre-attached and never needs to calculate offsets. This means error reporting always has precise, absolute line numbers.

Two data structures:

```go
type Line struct {
    Text   string
    Number int  // absolute line number, starting from 1
}

type Block struct {
    Title   string  // e.g., "角色名"
    Content []Line  // content lines with their line numbers attached
    Line    int     // the line number of the title line
}
```

With this structure, error reporting becomes straightforward:

```go
fmt.Errorf("第 %d 行（区块「%s」）：列表项必须以 '-' 开头",
    line.Number, blockName)
```

---

## IV. Block Scanner: A Simple State Machine

The core is a line-by-line state machine. It has only two states:

- `inBlock == false`: currently outside any block
- `inBlock == true`: inside a block, collecting content

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

        // Outside a block: skip empty lines
        if !inBlock && strings.TrimSpace(rawLine) == "" {
            continue
        }

        // Check if this line is a block title
        if title, ok := isBlockTitle(rawLine); ok {
            if inBlock {
                // Save the current block
                blocks = append(blocks, Block{
                    Title:   currentTitle,
                    Content: currentContent,
                    Line:    currentLine,
                })
            }
            // Start a new block
            currentTitle = title
            currentContent = []Line{}
            currentLine = lineNumber
            inBlock = true
            continue
        }

        // Non-title line: must be inside a block
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

**Two key design decisions:**

**1. Whitelist first**

`isBlockTitle` only recognizes a predefined set of titles:

```go
var knownBlocks = map[string]bool{
    "角色名": true,
    "锚点":  true,
    "规则":  true,
    "状态":  true,
    // ...
}
```

If a creator writes `【脚色名】` (a typo), the scanner won't treat it as a block start — it will return an error pointing to that line. The exact error message depends on context: if the line appears outside any block, it's "content appears outside any block"; if it appears inside a block, the block's parser will catch it. Either way, the line number is precise.

**2. Line number binding**

Every line carries its `lineNumber` at storage time, never offset. This is the foundation of precise error reporting.

---

## V. Parser: Routing to Different Parse Functions

After the scanner outputs `[]Block`, the parser routes each block to its corresponding parse function based on `Title`:

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
        // ... other block types
        }
        if err != nil {
            return nil, err
        }
    }
    return contract, nil
}
```

Each block type has its own parsing logic. Here's the key-value parser — note that every error includes the line number and block name:

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
        // ... extract key-value pair
    }
    return result, nil
}
```

---

## VI. Error Message Comparison

Same error, two experiences:

| What the user wrote wrong           | Generic parser error                  | `.meph` parser error                             |
| ----------------------------------- | ------------------------------------- | ------------------------------------------------ |
| `- 核心信念 "力量"` (missing colon) | `Unexpected token at position 42`     | `第 2 行（区块「锚点」）：缺少 ':' 或 '：'`      |
| `情绪: 暴怒` (missing `-`)          | `invalid character looking for value` | `第 2 行（区块「状态」）：列表项必须以 '-' 开头` |
| `【脚色名】` (typo)                 | N/A                                   | Points to the exact line with an error           |

---

## VII. The Cost

- Approximately 400 lines of Go code
- Need to write independent parsing logic for each block type
- Adding a new block type means updating the whitelist

But there are no external dependencies. `go build` completes in one step. The parsing logic is fully controllable and can be adjusted at any time.

---

## VIII. Summary

The block scanner and parser complete the "text to structure" transformation. `【角色名】` is now `contract.RoleName`. `【规则】` is now `contract.Rules`.

But the conditions in `contract.Rules` — like `包含 "攻击"` — and the actions — like `注入 "{角色名}的故乡是光之国"` — are still strings. The next post tackles: **how do we parse rule condition-action expressions? How does the `{变量}` interpolation syntax work?**

Those are the last two pieces of the parsing layer.

---

> GitHub: [https://github.com/yuelinghuashu/mephisto](https://github.com/yuelinghuashu/mephisto)
