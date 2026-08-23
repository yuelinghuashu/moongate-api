---
title: 'LLM Narrative Engines, Part 6: Runtime Loop and Branching'
description: You have a contract — now how do you bring it to life? Five-layer sandwich prompt, streaming full-width indentation, Mother-Child branching, memory extraction — building a complete runtime loop.
date: 2026-07-20 23:00:00
permalink: e5f9b2c3-4d6e-5f7a-8b9c-2c3d4e5f6a7b
level: P3
series: narrative-engine
tags:
  - Go
  - LLM
  - Engineering
---

> **Before reading this**: This is the final post in the series. I'd recommend reading the previous five — Part 1 covers the `.meph` format, Part 2 walks through a hands-on tutorial, Parts 3 and 4 explain how the parser outputs a `domain.Contract`, and Part 5 covers testing. This post assumes you already know the engine has a `contract` struct. The core problem we're solving: **how do we drive it?**

---

## I. A Narrative Engine's Fifth Problem: How Do You Bring Rules to Life?

The parser turns contracts into structs, but structs don't tell stories. A narrative engine also needs a **runtime** — a complete loop of input → matching → execution → output.

Three engineering problems need solving:

1. **How do rules match in real time?** When user input arrives, the engine needs to iterate over all rules, evaluate conditions, and decide which actions to trigger — all in milliseconds, without affecting user experience.

2. **How does the LLM follow rules?** Rules can't directly constrain the LLM — they must take effect indirectly through prompts. Translating rules into a format the LLM understands is the core challenge here.

3. **How do state and memory persist?** Every turn changes the character's state and memory pool. But when the next turn starts, the engine must restore the complete state from the previous turn. Without persistence, there's no long-form narrative.

This post solves all three. Parts 4 and 5 already covered rule-matching details (two-phase matching, dice evaluation) — I won't rehash them here. Instead, I'll focus on how the runtime wires the entire pipeline together.

---

Once the engine has a `contract`, it enters a loop: receive user input → match rules → execute actions → call LLM → return response. That loop is the runtime.

But before implementing the loop, several engineering problems must be solved first — they determine whether the engine is "functional" or "usable."

---

## II. Novel-Grade Terminal Output: Full-Width Indentation and Streaming Interception

LLM streaming output comes back in chunks. The engine intercepts each chunk via an `onChunk` callback and writes it directly to the terminal.

One design detail: **full-width indentation (`　　`)**.

```go
onChunk := func(chunk string) {
    for _, ch := range chunk {
        if ch == '\n' {
            fmt.Println()
            needIndent = true
            inParagraph = false
        } else {
            if !inParagraph && needIndent {
                fmt.Print("　　")  // two full-width spaces at the start of each paragraph
                needIndent = false
            }
            fmt.Print(string(ch))
            inParagraph = true
        }
    }
}
```

The effect: each paragraph in the terminal output starts with two full-width spaces, making it look like a printed book. This small detail dramatically improves the creator's experience — it turns "terminal output" into "novel pages."

But behind streaming output is a more critical engineering decision: **the action executor handles all streaming callbacks uniformly**.

- State-modification actions: return results immediately, then simulate character-by-character output via callback
- LLM actions: true streaming output, chunk by chunk
- Static text: return immediately, then simulate streaming output

Every action ultimately outputs through `onChunk`, regardless of its source. This ensures a consistent terminal experience.

---

## III. Five-Layer Sandwich Prompt: Welding Constraints at Both Ends

The biggest problem with LLM narrative is format drift — it often outputs stage-direction script format:

```plaintext
( smirks ) [Belial]: You're all too weak.
```

This breaks immersion. The solution: **place format constraints at both the top and bottom of the prompt**, forming a sandwich structure. In v1.1.0, the actual prompt rendering (`RenderPrompt` in `internal/core/llm/prompt.go`) has five layers:

```plaintext
【格式硬性要求】
(NarrativeConstraints — no parentheses, no script markers, no soliloquies)

【世界观】
(context)

【角色】
You are {角色名}, a being with {锚点风格}. Your background: {角色背景}

【当前状态】
(rendered from the runtime `map[string]any`)

【命运的推动】
(conversation history — alternating fate and assistant records)

【你记得的过往】
(dynamically accumulated memories from the runtime)

【此刻】
(user_input)

【要求】
(NarrativeConstraints, emphasized again)
```

Constraints appear twice, at both ends, with the context sandwiched in between. Two reasons for this:

1. **Primacy effect**: the LLM sees the constraints first — they get the highest priority
2. **Recency effect**: the constraints the LLM sees last influence the final output

Double emphasis ensures the format constraints aren't diluted by the context in the middle.

### 3.1 Deterministic Rendering: Sorting Guarantees Cache Hits

There's a subtle engineering problem here: Go's `map` iteration order is random.

At runtime, `【状态】` from the contract is converted to a `map[string]any` for fast read/write. When rendering `【当前状态】` in the prompt, if the iteration order changes each time, the generated prompt text changes — even if the state content is identical. This causes the **LLM's KV Cache to completely miss**, forcing recomputation every time.

The solution: sort the keys of `state` (`sort.Strings`) to ensure stable rendering order. When the state doesn't change, the generated prompt text is byte-for-byte identical, allowing the LLM service's KV Cache to hit, reducing latency and token consumption.

### 3.2 Anti-Soliloquy Constraint

`NarrativeConstraints` has one deliberately emphasized rule:

> Each response must include dialogue and action from at least one other character (non-player). If no other characters are present in the scene, introduce or create at least one interactive object. Prohibit purely player-only soliloquies.

This solves the "empty world" problem in narrative. If the LLM only responds to player input without introducing other characters, the story becomes a monologue and loses dramatic tension. This constraint forces the LLM to introduce at least one interactive object in every response, making the world feel alive.

---

## IV. Mother-Child Save Mechanism

After every turn, the engine automatically saves the current state to a child file.

**Naming convention** (v1.1.0 onward, aligned with the Flutter version, **dot-separated**):

- Master `story.meph` → default child `story.child.meph`
- Branch `--branch dark` → `story.dark.meph`

```go
func BuildChildPath(filename string, branch string) string {
    dir := filepath.Dir(filename)
    base := filepath.Base(filename)
    ext := filepath.Ext(base)
    name := strings.TrimSuffix(base, ext)

    // Already a child file: overwrite directly (prevent nested generation)
    if isChildFileName(name) {
        return filename
    }

    if branch != "" {
        return filepath.Join(dir, name+"."+branch+ext)
    }
    return filepath.Join(dir, name+childSuffix+ext) // childSuffix = ".child"
}
```

`isChildFileName` precisely identifies existing child files: `xxx.child` (default child) or `xxx.branchName` (branch names starting with letters). This means `my_story_1.meph` (with a numeric suffix) won't be misidentified as a child.

A child file is a complete `.meph` contract, containing:

- All static blocks from the master (character name, worldview, character background, opening scene, anchors, rules)
- Updated `【状态】`
- Accumulated `【记忆】`
- Recent `【历史】`

This means one static contract can evolve into any number of dynamic children:

```plaintext
story.meph (master, read-only)
    ├── story.child.meph (main timeline)
    ├── story.dark.meph (dark branch)
    ├── story.light.meph (light branch)
    └── story.experimental.meph (experimental branch)
```

Each branch evolves independently, without interfering with the others. The project includes `data/dantes.meph` alongside an already-run `data/dantes.child.meph` save.

**Save timing**: instead of writing to disk every turn (which would be inefficient), it's split into two layers:

1. **After each turn**: the Session layer (`cmd/mephisto/session.go`) calls `engine.Save()` to persist progress in real time
2. **On exit**: a `defer` saves one more time, ensuring the final state is written before exit

**Rule freshness during save**: `Save()` has a clever design — before saving, it reads the child file from disk (if it exists) and uses the `【规则】` block from disk as the latest rules. This means the user's real-time edits to the rules section in their editor won't be overwritten by the auto-save. This also supports **rule hot-reload** introduced in v1.0.3: `session.go` watches the child file for changes via `fsnotify`, with a 500ms debounce, then calls `ReloadContract` to re-parse — replacing only the rules while preserving state and history. This makes "edit rules → save → instantly effective" a reality.

**Loading**:

- Default: load the child if it exists
- `--reset`: ignore the child, start fresh from the master
- `--branch dark`: load the corresponding branch file

**Note**: Running a child file directly will overwrite it — the engine treats any `.meph` file as a master and generates a corresponding child. To avoid losing progress, don't run `run` directly on child files.

**The value of this design**: creators can fork storylines at critical moments, exploring different directions without losing any progress.

---

## V. Memory Extraction: Synchronous Weaving After Streaming

Memory extraction is critical for long-form narrative — it extracts key events from conversation history, compresses them for long-term storage, and injects them into the LLM context every turn.

But extraction calls the LLM, which takes seconds. If executed **before** streaming output, users would wait several seconds every 5 turns before seeing the first character.

The solution is simple: **output first, extract later.**

Here's the flow for each turn:

```plaintext
User input
    │
    ▼
Rule matching + action execution + LLM streaming output (user sees text appear character by character)
    │
    ▼
Streaming completes, user finishes reading the response
    │
    ▼
Engine.Run synchronously executes memory extraction (still inside Run, user has finished reading)
    │
    ▼
Return to Session — Session calls Save to auto-save the child
    │
    ▼
Show input prompt, wait for next turn
```

Note the **difference from the old version**: memory extraction runs synchronously inside `engine.Run()`, while child saving is handled by the Session layer after each Run returns. The two are decoupled — the engine handles narrative and memory, while Session handles persistence.

```go
// internal/core/engine/engine.go

func (e *Engine) Run(input string, onChunk func(string)) (string, error) {
    // ... rule matching, LLM call, streaming output ...

    // 4. Record the assistant's response
    runtime.AddHistory("assistant", response)

    // 5. Memory extraction (every N turns, synchronous)
    e.processMemories()   // ← user has finished reading the response by now

    return response, nil
}
```

`processMemories()` internal flow:

1. **Extraction interval check**: `ShouldExtract` — triggers when turn_count % 5 == 0 (`ExtractInterval = 5`)
2. **Call LLM for extraction**: takes the most recent 10 turns (`ExtractWindow = 10`), generates key-event summaries (each no more than 20 words)
3. **Semantic deduplication**: `shared.DeduplicateMemories` uses keyword Jaccard similarity for deduplication — semantically similar memories with different wording are automatically merged. For example: "Faust meets Mephistopheles in his study" and "Mephistopheles visits Faust's study late at night" — both memories share keywords like Faust, Mephistopheles, and study, so they're judged to describe the same event and merged into one
4. **Append + compress**: when the count exceeds the limit (`MaxLimit = 30`), auto-compress — keep the most recent 5 entries plus 3-5 summary entries

**Why this design?**

1. **User-unaware**: streaming is already complete, the user is reading or thinking about the response. Memory extraction happens silently in the background — the user doesn't "wait"
2. **Simple logic**: synchronous calls are easier to control than async goroutines — no race conditions, no "memory isn't written yet when saving"

If extraction fails, it only logs silently (the extraction function returns an error and skips it). The conversation continues — just without saving memories for that turn.

---

## VI. The Complete Loop

Putting it all together, here's how each turn of the engine runs:

```plaintext
User input
    │
    ▼
Rule matching
    │   ├── Passive rules (state modification + memory injection): batch execution, multiple can trigger
    │   └── Active rules (LLM instruction / static text): mutually exclusive, only the first match triggers
    │
    ▼
Execute action → LLM call (60-second timeout protection, falls back to ⚠️ static response on failure)
    │                        ↑—— timeout only protects the LLM call phase
    ▼
Streaming output (full-width indentation + chunk-by-chunk callbacks)
    │
    ▼
Record assistant history → (sync) memory extraction (every 5 turns, no streaming wait)
    │
    ▼
Session layer calls Save → auto-save to child file (story.child.meph)
    │
    ▼
Rule hot-reload listener (background async fsnotify, doesn't block main loop)
    │
    ▼
Wait for next input
```

A few runtime robustness details worth calling out:

- **LLM timeout fallback**: the entire LLM call is wrapped in a 60-second timeout context (`context.WithTimeout`). On timeout or failure, the engine outputs `(⚠️ LLM call failed: request timeout, fallback to static response)` via `onChunk`, then returns a default static text. **Telling the user "LLM is down" is better than making them guess "the character went silent"** — the former is a diagnosable engineering issue, the latter might be misinterpreted as narrative design.
- **Debugging and quiet mode**: debug output (`--debug`) goes to `os.Stderr`, while normal output (`--quiet`) doesn't interfere. Both can be enabled simultaneously.

This is Mephisto's runtime.

---

## VII. Costs and Limitations

This system doesn't come without cost:

**1. Memory extraction depends on LLM quality**

If the main model is in poor state, extracted summaries may drift — even altering key facts (e.g., writing "banished" instead of "defeated"). This is a "hallucination" risk.

Two layers of mitigation:

- **Prompt protection**: extraction and compression prompts explicitly forbid modifying core character settings (name, anchor content, state values, etc.). This works reliably on DeepSeek and GPT-4.
- **Model selection advice**: for 7B local models, summary quality drops significantly. If you must use a lightweight model, consider disabling automatic memory extraction (`ExtractInterval = 0`) and managing memories manually.

Future enhancement: **post-extraction validation** — after extraction results return, the engine checks for conflicts with core settings. If a conflict is found, discard that memory entry.

**2. Branch switching requires manual management**

Child files are stored independently. Switching branches requires the user to explicitly specify `--branch`. Unlike real version control with diff and merge, branches don't auto-sync.

**3. Streaming output occupies the terminal**

Full-width indentation and streaming look great in the terminal, but if the user wants to copy-paste text, the indentation and newlines come along — sometimes interfering.

---

## VIII. Summary

Six posts complete a full path:

| Post | Problem Solved                            | Core Output                                       |
| ---- | ----------------------------------------- | ------------------------------------------------- |
| 1    | What format to write rules in?            | `.meph` format design                             |
| 2    | What to run first?                        | Write a Faust contract from scratch and run it    |
| 3    | How to parse precisely with good errors?  | Block scanner + Parser                            |
| 4    | How to parse rules and variables?         | Rule expressions + interpolation syntax           |
| 5    | How to ensure changes don't break things? | Golden file testing                               |
| 6    | How to bring a contract to life?          | Five-layer prompt + branching + memory extraction |

Together, they form a complete long-form narrative engine:

```plaintext
Contract (.meph)
    │
    ▼
Parser (Posts 3 & 4) ──→ Contract
    │
    ▼
Engine (Post 6) ──→ Rule matching + LLM streaming narrative + memory extraction
    │
    ▼
Child save (story.child.meph) ──→ Long-term continuity + multiple branches
```

---

## Quick Reference: Complete Flow

```plaintext
.meph contract file
    │
    ▼ Scanner (line number binding)
    │
    ▼ Block list []Block
    │
    ▼ Parser (routing by block title)
    │
    ▼ domain.Contract
    │   ├─ RoleName
    │   ├─ Anchor
    │   ├─ State
    │   ├─ Worldview
    │   └─ Rules (conditions stored as-is, evaluated at runtime)
    │
    ▼ Engine
    │   ├─ Five-layer sandwich prompt (top constraints → worldview → character → state/history/memory → bottom constraints)
    │   ├─ Rule matching (passive batch + active mutex)
    │   ├─ Action execution (inject / state modify / LLM call / static text)
    │   ├─ Streaming output (full-width indentation)
    │   ├─ Memory extraction (every 5 turns, synchronous, semantic deduplication)
    │   ├─ LLM timeout fallback (60s, ⚠️ message + static response)
    │   └─ Child save (story.child.meph, dot-separated naming)
    │
    ▼ Engine loop
        User input → rule matching → execute action → LLM streaming narrative
        → memory extraction → Session auto-save → hot-reload listener → wait for next input
```

For quick reference on where to find specific content:

| Flow Node      | Corresponding Post | Key Concepts                                    |
| -------------- | ------------------ | ----------------------------------------------- |
| `.meph` format | Post 1             | Block titles, rule syntax                       |
| Hands-on       | Post 2             | Write a contract from scratch, no-LLM mode      |
| Scanner        | Post 3             | Line number binding, whitelist-first            |
| Parser         | Post 4             | Rule decomposition, interpolation syntax        |
| Testing        | Post 5             | Golden file, error-scenario tests               |
| Engine runtime | Post 6             | Five-layer prompt, branching, memory extraction |

---

> GitHub: [https://github.com/yuelinghuashu/mephisto](https://github.com/yuelinghuashu/mephisto)
