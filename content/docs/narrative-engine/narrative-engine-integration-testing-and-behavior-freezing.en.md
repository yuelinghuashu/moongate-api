---
title: 'LLM Narrative Engines, Part 5: Integration Testing and Behavior Freezing'
description: The parser is written — but how do you ensure future changes don't break it? Golden file tests, validators, and sliding-window aging tests — freezing parsing behavior with tests.
date: 2026-07-20 21:00:00
permalink: f1937ff0-8254-47a9-8b24-61418346fbea
level: P4
series: narrative-engine
tags:
  - Go
  - Engineering
  - CI/CD
---

> **Before reading this**: I'd recommend skimming Part 3's "Parser" section and Part 4's summary to understand how the parser outputs a `domain.Contract`. This post assumes you already know the parser can turn `.meph` into a struct.

---

## I. A Narrative Engine's Fourth Problem: How Do You Keep Behavior Stable?

The parser is written. But it's **code that gets maintained long-term** — requirements change, formats expand, bugs get fixed. Every change risks breaking existing behavior.

The tension here is: **creators depend on stable behavior, while developers depend on freedom to change.** If every code change requires manually testing every known scenario, the developer will fear refactoring. If you don't test, broken behavior reaches the creator — but the creator doesn't care that you refactored the parser.

The solution is to "freeze" parsing behavior: use a fixed set of contracts as watchdogs. After every change, automatically compare parse results against expectations.

This is what integration tests do: take a fixed set of `.meph` contracts as "watchdogs," run them after every change, and verify that behavior hasn't been accidentally altered.

---

## II. Golden File Testing: Freezing Parse Results

The most straightforward approach: prepare a standard contract, parse it, serialize the result to JSON, and save it. On every subsequent test run, compare the current parse result against that JSON file.

The project's `testdata/sample.meph` is that standard contract. Here's the test flow:

```go
func TestParseSample(t *testing.T) {
    got, err := ParseFile("testdata/sample.meph")
    if err != nil {
        t.Fatalf("parse failed: %v", err)
    }

    goldenPath := "testdata/sample.golden"
    var want domain.Contract

    if err := loadGolden(goldenPath, &want); err != nil {
        // Golden file doesn't exist — generate it automatically
        saveGolden(goldenPath, got)
        t.Log("Golden file generated. Please review and re-run the test.")
        t.FailNow()
    }

    // Compare got and want
    if diff := cmp.Diff(want, got); diff != "" {
        t.Errorf("Parse result doesn't match expected:\n%s", diff)
        t.Log("💡 If this change is intentional, run: go test -update")
    }
}
```

On first run, `sample.golden` is auto-generated. Every subsequent run compares against it, reporting differences. If the change is intended (e.g., a new field was added), running `go test -update` refreshes the Golden file.

**The core value of this mechanism:** the parser's behavior is "frozen." Any change must pass test validation — it can't silently alter parse results.

---

## III. Parse-Time Validation

Parsing isn't just "reading text in" — it validates required fields during the parse itself.

If the character name is empty, `parseRoleName` errors out immediately:

```plaintext
Line X: character name cannot be empty
```

If a rule name, condition, or action is empty, `parseRuleLine` errors out with the line number.

**If parsing fails, the struct doesn't exist.** There's no state where "parsing succeeded but content is invalid" — this is another advantage of the hand-written parser over JSON/YAML. JSON parsers don't validate semantic completeness — they only validate structural correctness.

---

## IV. Sliding-Window Aging Tests: Correct History Truncation

The engine has a critical behavior: history can't grow indefinitely. It must auto-truncate, keeping only the most recent N turns.

This test verifies that behavior:

```go
func TestIntegrationHistoryLimit(t *testing.T) {
    contract, err := parser.ParseFile("../parser/testdata/sample.meph")
    if err != nil {
        t.Fatalf("parse failed: %v", err)
    }
    // Set max history to 2 turns
    eng := engine.New(contract, engine.WithMaxHistory(2))

    // Run 5 turns
    for range 5 {
        eng.Run("Hello", nil)
    }

    history := eng.History()
    // 5 turns produce 10 records, but capacity is only 4 (2 turns * 2 records/turn)
    if len(history) != 4 {
        t.Errorf("history length = %d, want 4", len(history))
    }
}
```

This test ensures history truncation is **"whole-turn"** rather than **"per-record."** If truncation were per-record, you could end up with only the "fate" input and no "assistant" response — half a turn of data, leading to incomplete context in the `【命运的推动】` prompt block.

**Whole-turn truncation** preserves history integrity — either a full turn (fate + assistant) is retained, or it's discarded entirely.

---

## V. Error Scenario Testing: Ensuring Precise Error Messages

Beyond the "happy path," integration tests cover the **error path** — ensuring all kinds of malformed input produce correct errors, with line numbers and block names included:

```go
func TestParseErrors(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        wantErr string // substring the error should contain
    }{
        {
            name:    "content outside any block",
            input:   "content outside any block\n【角色名】\nBelial",
            wantErr: "content appears outside any block",
        },
        {
            name:    "list item missing - prefix",
            input:   "【锚点】\n核心信念: power",
            wantErr: "list item must start with '-'",
        },
        {
            name:    "list item missing colon",
            input:   "【锚点】\n- 核心信念 \"power\"",
            wantErr: "missing ':' or '：'",
        },
        // ...
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            _, err := ParseString(tt.input)
            if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
                t.Errorf("expected error containing '%s', got: %v", tt.wantErr, err)
            }
        })
    }
}
```

These tests ensure error messages never degrade to `unexpected token at position 42` — the kind of error we set out to eliminate in Part 1.

---

## VI. The Cost

- Golden files need manual review on first generation or update (checking that the content is correct)
- Error-scenario tests need to cover as many edge cases as possible
- Each new block type requires updating test cases

But the payoff is: **you can refactor fearlessly. As long as all tests pass, behavior hasn't changed.**

---

## VII. Summary

Integration tests are the project's "watchdogs." They freeze the parser's behavior, and every change must pass their validation.

Four things make up this test suite:

1. **Golden file tests**: freeze the parse results of a standard contract
2. **Parse-time validation**: check required fields and completeness during parsing
3. **Sliding-window aging tests**: ensure history is truncated by whole turns
4. **Error-scenario tests**: guarantee error messages are precise down to the line number

With this system in place, the engine can evolve confidently — no fear of breaking things, because the tests will catch it.

Next, we enter the engine's runtime. The core problem: **once you have a `domain.Contract`, how do you drive the LLM to generate narrative that follows the rules?**

The answer is the **sandwich prompt structure** — putting format constraints at both the top and bottom, with context in the middle — to completely eliminate parenthetical stage-direction drivel.

---

> GitHub: [https://github.com/yuelinghuashu/mephisto](https://github.com/yuelinghuashu/mephisto)