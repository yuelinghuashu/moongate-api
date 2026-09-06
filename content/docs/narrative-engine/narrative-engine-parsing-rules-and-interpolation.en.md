---
title: "LLM Narrative Engines, Part 4: Parsing Rules and Interpolation"
description: How do you parse condition-action expressions? How does the {variable} interpolation syntax work? This post covers the last two pieces of the parsing layer — turning rules from text into executable structures.
date: 2026-07-20 19:00:00
series: narrative-engine
tags:
  - Go
  - DSL
  - LLM
  - Engineering
---

> **Before reading this**: I'd recommend skimming Part 3's "Two-Phase Design" and "Block Scanner" sections to understand how the scanner outputs `[]Block`. This post assumes you already know how the parser routes blocks to different parse functions based on their `Title`.

---

## I. A Narrative Engine's Third Problem: How Do You Parse Condition-Action Rules?

Once the blocks are split, the most complex part is **rule expressions**. A narrative engine needs to answer: how are conditions written, how are actions written, and how are variables referenced?

There's an important design boundary here: **the parser only parses, it doesn't evaluate**. Condition strings like `包含 "攻击" && 状态.堕落指数 > 80` are stored as-is in the struct. The engine evaluates them at runtime. Why? Because the runtime state needed for evaluation doesn't exist at parse time — state values change dynamically during conversation and can't be determined at load time.

This post is about the parsing logic itself.

---

## II. Rule Parsing: Splitting Conditions and Actions

The rule format is fixed: `[name] if condition -> action`

```meph
[攻击] if 包含 "攻击" -> 注入 "贝利亚发动了猛烈的攻击"
[光之国] if 包含 "光之国" && 状态.情绪 == "暴怒" -> 注入 "光之国的记忆让贝利亚更加愤怒"
[高堕落] if 状态.堕落指数 > 80 -> 状态.情绪 = "癫狂"
```

Interpolation syntax appears in rule actions and text blocks:

```meph
注入 "{角色名}的故乡是光之国"
```

This post tackles two problems:

1. **How do we parse rule conditions and actions into storable structures?**
2. **How is interpolation syntax recognized and handled at the parsing layer?**

### 2.1 Rule Name Extraction

Find the first `[` and the first `]`, extract what's between them:

```go
func parseRuleLine(line string, lineNumber int) (*domain.Rule, error) {
    trimmed := strings.TrimSpace(line)
    if !strings.HasPrefix(trimmed, "[") {
        return nil, fmt.Errorf("规则必须以 '[' 开头")
    }
    idx := strings.Index(trimmed, "]")
    if idx == -1 {
        return nil, fmt.Errorf("缺少闭合的 ']'")
    }
    name := strings.TrimSpace(trimmed[1:idx])
    if name == "" {
        return nil, fmt.Errorf("规则名不能为空")
    }

    rest := strings.TrimSpace(trimmed[idx+1:])
    // Extract condition and action next...
}
```

> **Note**: The error messages in the Go code above are shown in Chinese — they're the actual output from the engine's parser.

### 2.2 Splitting Condition and Action

Use `if` and `->` as delimiters:

```go
// Remove the "if " prefix
if !strings.HasPrefix(rest, "if ") {
    return nil, fmt.Errorf("规则条件必须以 'if ' 开头")
}
rest = strings.TrimPrefix(rest, "if")
rest = strings.TrimSpace(rest)

// Use the first "->" to split condition and action
cond, action, ok := strings.Cut(rest, "->")
if !ok {
    return nil, fmt.Errorf("规则缺少 '->'")
}
cond = strings.TrimSpace(cond)
action = strings.TrimSpace(action)
```

### 2.3 Mutex Groups

Actions may contain a `[group:xxx]` marker:

```meph
[攻击] if 包含 "攻击" -> [group:combat] 注入 "贝利亚发动了猛烈的攻击"
```

When parsing, we extract the group name and store it in `domain.Rule.Group`:

```go
group := ""
if strings.HasPrefix(action, "[group:") {
    endIdx := strings.Index(action, "]")
    if endIdx != -1 {
        group = action[7:endIdx]
        action = strings.TrimSpace(action[endIdx+1:])
    }
}
```

Mutex groups ensure that within the same group, only the first matching rule is triggered. This logic takes effect at runtime; the parser just stores the group name.

Mutex groups can be used with any action type, not just `注入`:

```meph
[高堕落] if 状态.堕落指数 > 80 -> [group:escalate] 状态.情绪 = "癫狂"
[失控] if 状态.情绪 == "癫狂" && 状态.堕落指数 > 90 -> [group:escalate] 注入 "{角色名}已完全失控"
```

### 2.4 Quote Handling

Strings in conditions and actions are wrapped in `"`. During parsing, I call `unquote` to strip the outer quotes:

```go
func unquote(s string) (string, error) {
    s = strings.TrimSpace(s)
    if len(s) >= 2 {
        if (strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"")) ||
           (strings.HasPrefix(s, "“") && strings.HasSuffix(s, "”")) {
            return s[1:len(s)-1], nil
        }
    }
    return s, nil
}
```

Chinese quotes are supported because creators may use Chinese input methods — `“` and `”` are more natural to type than `"`.

---

## III. Interpolation Syntax: Recognizing and Replacing `{variable}`

The core requirement for interpolation: **recognize `{角色名}` in any text and replace it with the corresponding character name.** Currently, only one interpolation variable is supported — `{角色名}`, which comes from the `【角色名】` block and is statically defined.

### 3.1 Why Only `{角色名}`?

You might wonder: why can't state variables like corruption index or emotion be interpolated directly with `{堕落指数}`?

Because state variables are **read and written dynamically at runtime** — creators modify them via `状态.键 = 值` and compare them via `状态.键 > 值`. Allowing `{堕落指数}` interpolation would introduce a parallel mechanism: the same variable could be referenced both via `状态.堕落指数` and `{堕落指数}`. Two syntaxes for the same thing, increasing the learning curve.

So state variables are uniformly referenced using the `状态.键` syntax, with no interpolation. The engine's template substitution does one thing only: replace `{角色名}` with the actual character name.

### 3.2 Substitution Timing: Three Scenarios

Interpolation substitution happens in **three different scenarios**:

1. **CLI welcome screen display**: Worldview and opening scenes are substituted once when displayed to the user. The creator sees "贝利亚奥特曼" instead of `{角色名}`.

2. **Rule action execution**: `注入 "{角色名}的故乡是光之国"` is substituted every time the rule triggers. This is the most common usage of interpolation.

3. **Prompt construction**: Worldview and background text are passed to the LLM **as-is**, without engine-side substitution. The LLM naturally understands `{角色名}` from context (e.g., "You are 贝利亚奥特曼").

#### Why not substitute in scenario 3?

Because substitution changes the raw form of the text. If we replace `{角色名}的故乡是光之国` with `贝利亚奥特曼的故乡是光之国`, the LLM receives a "hardened" narrative. Keeping `{角色名}` as-is lets the LLM dynamically decide how to use the name during generation — in some narrative branches, the character name might change (e.g., the character is renamed or forgotten), and keeping the placeholder is actually more flexible.

---

## IV. The Intersection: When Rules Meet Interpolation

Take this rule as an example:

```meph
[光之国] if 包含 "光之国" -> 注入 "{角色名}的故乡是光之国"
```

Full execution flow:

```plaintext
User input: "I'm going to the Land of Light!"
    │
    ▼
Rule matching: contains "光之国" → true
    │
    ▼
Action recognition: action type is "注入"
    │
    ▼
Runtime substitution: {角色名} → "贝利亚奥特曼"
    │
    ▼
Append to memory: "贝利亚奥特曼的故乡是光之国" written to memory
    │
    ▼
LLM narrative: generates response with the new memory
```

### Why is substitution done at runtime?

Because the engine supports multiple branching storylines. If substitution happened at load time, all branches would share the same static value and couldn't evolve independently. Runtime substitution means each branch reads its own state — when the state changes, the interpolation result changes with it.

### Dice Expression Error Tolerance

Dice expressions like `roll(1d100) >= 80` are stored as-is during parsing and evaluated at runtime. If the expression has a format error — e.g., `roll(1d10` missing a closing parenthesis — the engine doesn't report an error. Instead, it treats the condition as not satisfied (returns `false`). This is because dice expressions are part of conditions, and the parsing layer isn't responsible for validating runtime correctness — malformed expressions are simply treated as "no match."

---

## V. Summary: The Parsing Layer Is Complete

At this point, the parsing layer covers all syntax units:

| Block Type        | Parse Function                  | Complexity | Notes                                                    |
| ----------------- | ------------------------------- | ---------- | -------------------------------------------------------- |
| Text blocks       | `parseTextBlock`                | Low        | Concatenates content, preserves newlines                 |
| Key-value lists   | `parseKeyValuePairs`            | Medium     | Supports Chinese/English colons, precise error reporting |
| Rule lists        | `parseRules`                    | **High**   | Condition expressions, mutex groups, dice expressions    |
| Plain text lists  | `parsePlainList`                | Low        | Extracts lines one by one                                |
| **Interpolation** | `ReplacePlaceholders` (runtime) | Medium     | Preserved at parse time, substituted at runtime          |

### The most critical boundary

> The parser only "reads" — it turns text into structured data.
> The engine "computes" — condition evaluation, variable substitution, action execution.

Next, we cross that boundary and enter the engine's runtime. The first engineering challenge the engine faces: **how do we ensure code changes don't break existing parsing behavior?**

The answer is **integration testing** — using a fixed set of `.meph` contracts as "watchdogs," comparing parse results against expectations after every change.

---

> GitHub: [https://github.com/yuelinghuashu/mephisto](https://github.com/yuelinghuashu/mephisto)
