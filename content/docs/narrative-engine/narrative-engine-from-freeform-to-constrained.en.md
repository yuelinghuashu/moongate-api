---
title: "LLM Narrative Engines, Part 1: From Freeform to Constrained"
description: Why JSON and YAML fail for narrative engines, and how a custom DSL (.meph) solves the consistency problem — trading parser complexity for creator experience.
date: 2026-07-20 13:00:00
series: narrative-engine
tags:
  - DSL
  - JSON
  - LLM
  - Engineering
---

On the first day I started building narratives with LLMs, I hit a wall.

I defined a character: Belial, a fallen Ultraman — arrogant, contemptuous of the Land of Light, obsessed with power. The first three turns were perfect. Every response carried that satisfying villainous edge. By the fifth turn, he started giving me life advice, like a mild-mannered philosopher. By the tenth, he called himself "a guardian of the Land of Light." He had completely forgotten who he was.

This isn't a model quality issue. It's the fundamental flaw of free generation — LLMs have no built-in mechanism to "enforce rules." You can write "you are an arrogant villain" in the system prompt, but the model's understanding of "arrogant" is probabilistic. Over long contexts, it attenuates, overwritten by user inputs, model outputs, and even token-order drift.

I needed a way to write down rules that the model must follow. So I started building Mephisto.

But before writing the engine code, I had to solve a more fundamental problem: **what format should these rules be written in?**

The format needs to carry: the character name (single-line), the worldview (multi-line), state variables (key-value pairs), and behavioral rules (condition-action pairs). Each type has a different structure and parsing strategy. The choice of format determines whether the creator spends their time writing stories or debugging syntax.

This post documents my trade-offs and final decision.

---

## I. A Narrative Engine's First Problem: How Do You Express Rules?

A narrative engine's first problem is the **rule expression layer** — how does the creator tell the engine what this character will and won't do? There are three common approaches:

- **General-purpose structured formats** (JSON/YAML): program-friendly, but cruel to creators — escaping hell, unreadable error messages
- **Custom domain-specific formats** (like .meph, Ink): a middle ground, but the parser must be written from scratch
- **Plain text**: most friendly to creators, but almost impossible for programs to parse precisely

Each approach has a cost. I'll compare them one by one, then explain why I chose the custom path.

---

## II. See the Destination: A Real .meph Contract

Before discussing any design decisions, let's look at the finished product. Here's a complete `.meph` contract, taken from the project's `data/sample.meph`:

```plaintext
【角色名】
贝利亚奥特曼

【锚点】
- 核心信念：力量就是一切
- 说话风格：狂傲、嘲讽、不容置疑

【世界观】
光之国是宇宙中最强大的文明，也是贝利亚的故乡。
但他早已被驱逐，如今他带着对光之国的憎恨归来。

【状态】
- 堕落指数：50
- 情绪：暴怒
- 位置：宇宙空间站

【规则】
[攻击] if 包含 "攻击" -> 注入 "贝利亚发动了猛烈的攻击"
[防御] if 包含 "防御" || 包含 "防守" -> 注入 "贝利亚摆出了防御姿态"
[光之国] if 包含 "光之国" -> 注入 "{角色名}的故乡是光之国，也是他最大的仇恨来源"
[高堕落] if 状态.堕落指数 > 80 -> 状态.情绪 = "癫狂"
```

This is the entire contract. The creator sees not `{` and `}`, not escaped quotes, not indentation levels, but:

- A name directly under `【角色名】`
- Key-value pairs under `【锚点】` using `- key: value`
- Rules under `【规则】` using `[name] if condition -> action`

Notice `{角色名}` in the rules — that's **interpolation syntax**. The parser stores it as-is, and the engine replaces it at runtime with the current character name. This ensures branches evolve independently without interfering with each other.

It reads like a document. But the program can parse it precisely, and when something goes wrong, it can tell the creator **"line 12, block '状态': list item must start with '-'"** — not `unexpected token at position 246`.

The question is: how did I get here?

---

## III. JSON: Friendly to Programs, Hostile to People

JSON was the obvious first choice. Clean structure, simple parsing, mature libraries in every language. I actually built a prototype with JSON — I still have that yellowed printout somewhere, covered in handwritten notes. Then I hit the fatal problem.

That rule from above, in JSON:

```json
{
  "rules": [
    {
      "name": "攻击",
      "condition": "包含 \"攻击\"",
      "action": "注入 \"贝利亚发动了猛烈的攻击\""
    }
  ]
}
```

The creator's intent:

```plaintext
包含 "攻击"
```

In JSON, it becomes:

```plaintext
"condition": "包含 \"攻击\""
```

The problem isn't that the syntax is "hard." It's **context switching cost**.

The creator can't directly express their intent — they have to constantly think "am I writing JSON or am I writing rules?" Over a large contract, this cost accumulates. You're not reading content; you're constantly verifying how many backslashes are on this line.

Even worse: error messages. A common mistake — a trailing comma after the last element in a JSON array. The JSON parser reports:

```plaintext
Unexpected token } in JSON at position 246
```

The creator has to paste it into an online tool and count characters to figure out where position 246 is. For non-technical users, this experience is devastating.

**JSON is friendly to programs, but hostile to people.** And the author of this file is a creator, not a programmer.

---

## IV. YAML: The Mirage of Simplicity

YAML seemed to fix JSON's readability problem:

```yaml
role_name: 贝利亚奥特曼
rules:
  - name: 攻击
    condition: 包含 "攻击"
    action: 注入 "贝利亚发动了猛烈的攻击"
```

Indentation replaced brackets and commas. It looked more like "writing a document."

But YAML's problems are more insidious than JSON's. They're not syntax errors — they're **logic errors**. The file parses, but the behavior is wrong.

Imagine defining a multi-line text block in YAML (a worldview, for example), using the `|` marker:

```yaml
worldview: |
  光之国是宇宙中最强大的文明。
  贝利亚被驱逐后，一直在寻找复仇的机会。
```

Then you define a list below it. One space off in indentation, and the parser may treat subsequent lines of the multi-line block as children of the list. Result: **the worldview is truncated, the list structure is broken, no error is reported, and the engine behaves unexpectedly.**

Even more subtle: Chinese full-width spaces (`　`) and English half-width spaces (` `) are visually indistinguishable. A creator accidentally types a full-width space instead of a half-width one — YAML parsers don't treat it as indentation. They either throw a syntax error or, worse, misidentify the entire block as a key. Without syntax highlighting, this is almost impossible to debug by eye.

The creator isn't reading content; they're constantly verifying "how many spaces are on this line." For a hundred-line contract, this cognitive burden never stops.

**YAML's simplicity is a mirage.** It's friendly to people, but to programs, its rules are more complex and unpredictable than JSON's.

---

## V. Plain Text: Free but Ambiguous

Since structured formats have problems, why not skip structure entirely and just write plain text?

Like this:

```plaintext
角色名是贝利亚奥特曼。
世界观是光之国。
规则是如果用户提到攻击，就执行攻击。
```

For creators, this is the most natural way — zero learning curve.

But there's a problem: the program can't understand it. It doesn't know whether the "是" in "角色名是贝利亚奥特曼" is a declaration or a description. It doesn't know the relationship between "世界观是光之国" and the next sentence "规则是...". Natural language is clear to humans, but to programs, it's ambiguous, imprecise, and unparseable.

If I used plain text, I'd have to design an implicit convention — recognizing blocks by keyword matching, distinguishing entries by line breaks. That's harder to guarantee correctness than an explicit syntax.

**Plain text is most friendly to creators, but almost unfriendly to programs.**

---

## VI. Three Options Compared

| Format         | Creator-Friendly | Program-Friendly | Core Problem                                           |
| -------------- | :--------------: | :--------------: | ------------------------------------------------------ |
| **JSON**       |      ❌ No       |      ✅ Yes      | Escaping hell, unreadable error messages               |
| **YAML**       |    ⚠️ Partial    |      ✅ Yes      | Indentation sensitivity, multi-line vs. list confusion |
| **Plain Text** |      ✅ Yes      |      ❌ No       | Ambiguous structure, unparseable                       |

I needed a format that: **reads like a document, but has structure.** It had to satisfy two conditions simultaneously:

1. Creators can write naturally, without learning JSON escaping or YAML indentation rules
2. Programs can parse precisely, and on error, tell the creator the exact location and reason

This meant I couldn't use any existing general-purpose format — I needed to design one specifically for the "narrative contract" scenario.

Even if only one out of ten users is a minimalist who craves a simpler config — that's still 10%, if you round it. So I decided to go all in on the custom format.

---

## VII. .meph Design Principles

Back to that `.meph` contract from the beginning. Its design is based on three principles:

### 1. Use human language for boundaries, eliminate bracket-phobia

Creators see `【角色名】`, not `{` and `}`. Chinese guillemets are more natural than curly braces for Chinese-speaking creators. `【角色名】` itself says what this section contains — no comments needed.

More importantly, block titles are restricted to a whitelist (`角色名`, `锚点`, `规则`, `状态`, etc.). If a creator writes `【脚色名】` (a typo), the parser won't treat it as a block start — the creator will get an error pointing to that exact line.

(The specific message depends on whether the typo appears inside a block or outside one, but the line number is always precise.)

### 2. Distinguish "semantic blocks" rather than "data structures"

In JSON, the creator decides whether to use an object or an array — that's an implementation detail. In `.meph`, the creator only needs to know "this is a list" or "this is a paragraph." The parser identifies the structure based on the block title — the creator doesn't declare it.

### 3. Syntax mirrors natural logic

Rules use the intuitive `[name] if condition -> action` form. Condition logic uses readable expressions like `包含 "keyword"` and `状态.key > value`, not symbolic operators.

---

## VIII. The Cost

This design doesn't come without cost:

- **The parser must be handwritten**: I can't use `json.Unmarshal` or `yaml.Unmarshal` — I have to write my own lexer and parser
- **The whitelist must be maintained**: adding a new block type means updating the `knownBlocks` list
- **Documentation is required**: creators need to learn the format — though the learning curve is lower than JSON's, it's still a curve
- **Tooling is absent**: no off-the-shelf syntax highlighting, formatting, or validation tools

But the deciding factor is simple: **who writes this file?**

If it's a programmer writing for a program, JSON is fine. If it's a creator writing for a program, you need a "human-centric" format. And the target users of a narrative engine are creators — people who write stories.

**I believe this trade-off is worth it.**

---

## IX. Summary

This post answered the question "what format to use?" The answer is `.meph` — a custom text format designed specifically for narrative contracts, optimized for creator experience.

But "design" only solves half the problem. The next post puts theory aside and walks through writing a real contract from scratch — so you can see what it actually feels like to use this format.

---

> GitHub: [https://github.com/yuelinghuashu/mephisto](https://github.com/yuelinghuashu/mephisto)
