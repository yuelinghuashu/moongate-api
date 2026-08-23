---
title: 'LLM Narrative Engines, Part 2: Quick Start — Write Your First Contract'
description: Write your first .meph contract from scratch, compile it, run it, and watch the LLM generate narrative that follows rules you just wrote. No prior knowledge required.
date: 2026-07-20 15:00:00
permalink: 60b6f1fd-145e-4d31-b5b3-9ef1164170ce
level: P1
series: narrative-engine
tags:
  - Go
  - LLM
  - DSL
---

> **Before reading this**: If you haven't read the first post, [From Freeform to Constrained Generation](https://dev.to/yuelinghuashu/narrative-engines-with-llms-from-freeform-to-constrained-generation-1l4c), I'd recommend starting there to understand *why* `.meph` exists. This post requires zero prior knowledge — just a terminal and Go 1.26+.

In the previous post, we talked about why `.meph` is a better fit for narrative contracts than JSON or YAML. But talk is cheap.

Then let's stop talking and start building. We'll write a real contract from scratch, compile it, and run it. **You'll have your first narrative running in no time** — and you'll see, firsthand, how the rules you write drive LLM-generated storytelling.

---

## I. What You'll Need

Three things:

1. **Go 1.26+** — open a terminal and run `go version` to check
2. **A terminal** — any OS works
3. **(Optional) An LLM API key** — DeepSeek, OpenAI, or Ollama all work. Not required — the engine runs fine without an LLM.

If you have an API key, create a `.env` file in the project root:

```bash
MEPHISTO_CLIENT=openai
MEPHISTO_MODEL=deepseek-v4-flash
OPENAI_API_KEY=sk-your-key-here
```

---

## II. Clone and Build

```bash
git clone https://github.com/yuelinghuashu/mephisto.git
cd mephisto
go build -o ./mephisto ./cmd/mephisto
```

If everything goes well, you'll see a binary named `mephisto`.

---

## III. Write Your First Contract

Now for the real work. Create a new file `data/faust.meph` and fill it from scratch.

### 3.1 Character Name

The first line of any contract is the character name. Mark the block with `【角色名】` (Character Name) and write the name directly below:

```text
【角色名】
Faust
```

That's it — no quotes, no colons, no markup.

### 3.2 Anchor

The `【锚点】` (Anchor) block defines the character's core personality. Use the `- key: value` format:

```text
【锚点】
- 核心信念：Knowledge above all else. I would pay any price for truth.
- 欲望：To experience everything a human can experience.
- 绝对禁忌：I will never admit regret.
```

These are injected directly into the LLM's context — they're the bedrock of the character's behavior.

### 3.3 State

`【状态】` (State) holds dynamic variables, also in key-value format:

```text
【状态】
- 灵魂完整度：100
- 情绪：Never satisfied
- 位置：Study
```

State values support three types: numbers, booleans, and strings. The engine infers types automatically at parse time — `"100"` becomes the number `100`, while `"Never satisfied"` stays a string. This means you can write conditions like `状态.灵魂完整度 > 50` directly, without manual type conversion.

At runtime, state is stored as a `map[string]any` for fast read/write. Initial order is inherited from the contract, but runtime access is key-based and order-agnostic.

### 3.4 Worldview

`【世界观】` (Worldview) is multi-line text. Write freely — line breaks are preserved:

```text
【世界观】
The story takes place in 16th-century Germany, an era where theology and science are locked in tension. Universities are consumed by sterile scholastic debates; real knowledge is suppressed. The world is composed of God, angels, demons, and mortals — Heaven and Hell are real dimensions. Mephistopheles is a hellish emissary, skilled at trapping souls through words and contracts. Soul transactions are legally binding in this world — once a soul contract is signed, there's no revocation.
```

### 3.5 Character Background

`【角色背景】` (Character Background) works alongside Worldview to define the character's history:

```text
【角色背景】
Faust is a scholar of vast learning — philosophy, medicine, law, and theology. Yet he despairs at the limits of human knowledge: after a lifetime of study, he still cannot touch the essence of the world. In his despair, he signs a contract with Mephistopheles: his soul in exchange for unlimited earthly experience.
```

### 3.6 Opening Scene

`【开局场景】` (Opening Scene) sets the stage when the conversation begins:

```text
【开局场景】
Late night. Candlelight flickers in the study, books piled high on the desk. Faust stands by the window, gazing at the moonlight. On the corner of the table lies a contract, its ink still wet.
```

### 3.7 Rules — The Heart That Brings the Character to Life

`【规则】` (Rules) is the engine's core. Each rule has three parts: `[name] if condition -> action`.

```text
【规则】
[New Desire] if 包含 "追求" || 包含 "想要" || 包含 "体验" -> 注入 "{角色名} feels a new longing kindle in his chest — nothing can stop him from experiencing it all firsthand"
[Mephistopheles] if 包含 "梅菲斯特" || 包含 "契约" -> 注入 "Mephistopheles's voice whispers at {角色名}'s ear: 'Is this what you truly want? Have you considered the price?'"
[Soul's Price] if 包含 "代价" || 包含 "灵魂" -> 注入 "{角色名} looks down at his hands, as if he can see something slowly slipping away"
[Never Satisfied] if 不包含 "放弃" && 不包含 "满足" -> 注入 "{角色名}'s eyes burn with an unquenchable flame — he still wants more"
```

Rule conditions support logical operators (`&&`, `||`), state comparisons (`状态.key > value`), and dice expressions (`roll(1d100)`). The most common action is `注入` (inject) — it appends a message to memory, which the LLM then weaves naturally into the ongoing narrative.

### 3.8 Validate

Save the file and validate the parse:

```bash
./mephisto parse data/faust.meph
```

You'll see JSON output like this:

```json
{
  "role_name": "Faust",
  "anchor": [...],
  "state": [...],
  "rules": [...]
}
```

If parsing fails, the error tells you exactly what's wrong and where — something like "line 12 (block '锚点'): missing ':' or '：'."

---

## IV. First Conversation (No LLM Mode)

The engine runs fine even without an LLM:

```bash
./mephisto run data/faust.meph
```

You'll see a welcome message, then enter the conversation loop. Type "I want to experience love":

```text
命运 > I want to experience love
Faust gazes silently at fate.
```

No LLM means the engine returns the default response. But rules still fired — the input contained “想要” (want), which matched the condition `包含 "想要"`, triggering the [New Desire] rule and writing the injection into memory.

Type `/state` to see the current state:

```text
Current state:
  灵魂完整度: 100
  情绪: Never satisfied
  位置: Study
```

Type `/history` to see the conversation log — your input “I want to experience love” is already recorded as a fate directive.

**This is how the engine works without an LLM:** rule matching, injection, state management — everything runs normally. The LLM is just the final “narrative rendering layer.”

---

## V. Bring in the LLM

Now add your API key and run again:

```bash
./mephisto run data/faust.meph -debug
```

You'll see the same welcome message, this time with `-debug` enabled. The LLM configuration will be displayed first.

Then type this at the prompt:

```text
命运 > You want to acquire knowledge beyond human comprehension
```

The terminal shows debug output — and then Mephistopheles steps out of the shadows:

```text
🔍 Rule debug mode
----------------------------------------
📌 Checking rule [New Desire] (line 26)
   Condition: 包含 "追求" || 包含 "想要" || 包含 "体验"
   Result: true
   ✅ Triggered → inject "{角色名} feels a new longing kindle in his chest, no...

  The candlelight in the study flickered, casting the parchment rolls and codices that lined the walls into shifting pools of light and shadow. Faust sat slumped at his desk, piled high with manuscripts, his fingertips tracing the worn cover of an old astronomy text. But his gaze drifted past the window, into the pitch-black night. He muttered, his voice hoarse, almost carried away by the draft: “A lifetime of searching, and I've never even pushed open a single door.”

  Then, from the bookshelves behind him, came a sound — soft, like a mouse gnawing at wood, or a stifled chuckle. Faust did not turn. He said coldly: “Is that you again, Wagner? It's late. You needn't bring me any more warm broth.”

  But the footsteps were light — too light for his clumsy apprentice. A low, metallic voice emerged from the darkness: “Wagner is only fit to stoke your fire. I bring a different kind of warmth.” A figure stepped out of the shadows — tall, draped in a sumptuous crimson robe, with a lean face and a smile that seemed to curve just slightly too far. He toyed with a bronze ring between his fingers. He stopped before the desk, leaned forward slightly, and fixed his gaze on Faust's eyes: “You said a moment ago that you've spent your life unable to push open that door. But have you ever considered that doors weren't meant to be pushed?”

  Faust slowly lifted his head and fixed his stare on this uninvited guest. His hand pressed flat on the astronomy text. “Who are you? You enter my study without permission.”

  The man gave a soft laugh and turned the ring in the candlelight. It cast a twisted shadow — as though it were some kind of unutterable sigil. “I am the answer to all your questions — and the price of all your desires.” He extended his hand, palm up, fingers spread. Dim red light pulsed through the lines of his palm. “I can show you the patterns behind the stars. I can let you hear the melody that played at the beginning of creation. I can let you touch the law itself. All I ask in return is one small, insignificant thing.”

  Faust rose to his feet, his sleeve sweeping across the scattered drafts on the desk. Sheets of formulae and notes drifted to the floor. He stared at that outstretched hand for a long time, then finally spoke: “What do you want?”

  The man's smile widened, his voice soft as a feather brushing a blade: “Your soul. But don't worry — you don't use it anyway. It can't solve your equations. It can't carry you to the heavens. Keeping it is just letting it rot with a body that's already decaying.” He withdrew his hand and instead produced a roll of jet-black parchment, which he unfurled across the desk. The surface held no script, only an abyss of darkness that seemed to swallow the light around it. “Sign it, and I will deliver everything at once.”

  Faust's breath quickened. He looked down at that dark expanse, then up at the man's eyes — which reflected the birth and death of countless stars. At last, he reached out. His fingertip touched the parchment, and a chill shot up along his shoulder. He hesitated no longer. He took the quill the man offered, pricked his own thumb, and let the blood fall onto the page.

  Just then, the study door creaked open a crack. Wagner peered in, clutching a dim oil lamp. He saw the stranger in the room, then saw the blood on his master's fingertip. His face went white. Trembling, he cried out: “Master! What are you doing? When did this man enter?”

  Faust didn't stop. The blood-script was already taking form on the parchment. Without turning, he said: “Wagner, close the door. Tonight you will witness a scholar's final wish.” As the words faded, the darkness on the parchment began to churn, spreading through the room like a starless night unfurling — while the crimson-robed man's laughter echoed among the scrolls.
```

That's what those 30 lines of contract you just wrote can generate.

---

**Now try changing just one number.**

Open `data/faust.meph`, change `灵魂完整度` in `【状态】` from `100` to `50`, save, and run again. Type the same line:

```text
命运 > You want to acquire knowledge beyond human comprehension
```

Same rules, same input, one number different — and the narrative shifts entirely:

```text
  The candlelight in the study swayed, casting enormous shadows that crawled across the shelves like living things. Faust slumped in his chair behind a mountain of books. A quill hung between his fingers, the ink long dry on its tip. His empty gaze swept across the Latin, Greek, and Hebrew scrolls — these symbols that had once enraptured him now lay like dried insect husks, drained of meaning.

  “Forty years,” he rasped, barely audible. “Forty years of study, and I have not even touched the edge of the first heaven.”

  From outside came the low cry of a night bird, as though something was answering his despair. At that moment, the study door opened silently, and in came his old servant Wagner, carrying a steaming bowl of herbal broth. He picked his way carefully through the scattered parchment, set the bowl on the corner of the desk, and spoke with evident worry: “Master, you haven't eaten for three days. You've been reading Paracelsus all night — your eyes are bloodshot. If you keep this up…”

  “If I keep this up, what? What's the worst that could happen — this body rotting before my desires do?” Faust's head snapped up, a near-crazed light kindling in his eyes. “Wagner, you've studied for years too. Tell me — have you ever seen the face of truth?”

  Wagner stepped back, daunted by the look in Faust's eyes. He murmured: “Master, this servant believes that truth is God's affair.”

  “God's affair.” Faust repeated the words, then burst into laughter — laughter thick with bitterness. “Exactly. Exactly. We hand everything over to God, fencing ourselves in with theological rails, safe in our pigsties, chewing the dry crusts the priests toss us — and we're grateful for it!” He rose to his feet, knocking over his chair behind him. The candle flame almost died under the wind of his robe. “I'm done with these words, these symbols, this stuff written by men to deceive one another. I need real knowledge — the knowledge that belongs to the ancient ones, the primordial ones.”

  Wagner stared at him in terror, his hands trembling. “Master… what are you saying? Such thoughts are blasphemy…”

  “Blasphemy?” Faust strode to the window and threw it open. The cold night wind rushed in, whipping his gray hair and robe into frenzy. “If seeking truth is blasphemy, then let this blasphemy be complete.” He turned, his eyes burning like live coals. “Wagner, leave. Tonight I need to be alone.”

  Wagner opened his mouth, but finally lowered his head and retreated quickly. The wooden door closed heavily behind him. Faust stood alone at the open window, staring up at the star-filled, silent sky. He whispered: “You've been standing there for billions of years. Do you have nothing to say to this mere mortal?”

  No sooner had the words left his mouth than the candle flame on the desk leaped high, turning into a sheet of blue fire. From its center, a voice spoke — low, elegant, not coming through the air, but echoing directly inside Faust's skull.

  “You're finally ready to listen, Mr. Faust.” The voice carried a smile. “Then allow me to introduce myself.”
```

**Notice the difference:** at soul integrity 100, Faust is a “contemplative scholar,” calmly skeptical when Mephistopheles appears — “Who are you? Entering my study without permission.” At soul integrity 50, he becomes a “brilliant scholar teetering on the edge of self-destruction,” shouting at Wagner, redefining the pursuit of knowledge as blasphemy. Even Mephistopheles' entrance changes — not emerging from shadow, but erupting from candle flames, his voice resonating directly inside Faust's skull.

A single state value changes, and the entire texture of the narrative transforms. **That's the power of state-driven storytelling.**

---

Look closely at the first output:

- **Faust's identity as a scholar** — drawn directly from `核心信念：Knowledge above all else` in `【锚点】` and the `【世界观】` line about spending a lifetime without touching the essence of reality. The LLM faithfully inherited these traits.
- **Mephistopheles's appearance** — triggered by the [New Desire] rule from the input “want.” The injected memory gave the LLM the context: “Faust feels a new longing kindle in his chest.”
- **“Price” and “soul”** — the LLM naturally introduced the theme of “price,” which triggered the [Soul's Price] rule — even though you didn't explicitly type those words.
- **“Never satisfied” as an underlying tone** — the [Never Satisfied] rule has the condition `不包含 "放弃" && 不包含 "满足"` (doesn't contain “give up” and doesn't contain “satisfied”), which fires on almost every turn. It constantly injects “Faust still wants more,” making this not just a one-turn interaction but a persistent character trait that runs through the entire narrative.

**Every rule is doing work.** Not a single one is wasted.

---

## VI. Try Changing Something

Now that you've seen the engine in action, make some changes and see what happens:

### Change a State Value

```text
- 灵魂完整度：50
```

You already saw the result. Try `10` or `0` and see how Faust changes.

### Add a Dice Rule

```text
[Favor of Fate] if 包含 "追求" && roll(1d100) >= 80 -> 注入 "Fate seems to favor {角色名} — things go more smoothly than expected"
```

`roll(1d100) >= 80` means: roll a 100-sided die, and the rule only triggers if the result is >= 80. A 20% success rate — not every pursuit is lucky. This introduces real randomness into the story.

### Run Two Turns, Then Look at the Child Save

After a couple of turns, open `data/faust_child.meph` in a text editor:

```text
【状态】
- 灵魂完整度：100
- 情绪：Never satisfied

【记忆】
- Faust feels a new longing kindle in his chest…
- Mephistopheles's voice whispers at Faust's ear…

【历史】
- fate: I want to experience love
- assistant: …
```

This is the engine's Mother-Child save mechanism: `faust.meph` is the read-only master contract; `faust_child.meph` is the dynamic snapshot containing runtime state, memories, and history. Automatically saved after every conversation.

---

## VII. Summary

At this point, you've done three things:

1. **Written a complete contract** — 30 lines covering character name, anchor, state, worldview, background, opening scene, and rules
2. **Validated its structure with the parser** — the `parse` command tells you precisely whether your contract is correct
3. **Run the engine and watched the character come alive** — even without an LLM, rules still fire; with an LLM, every rule you wrote shapes the narrative direction

Those 30 lines are the contract between you and the engine. The engine ensures the LLM honors it.

---

Next post goes under the hood. It answers one critical question: **how does the block scanner precisely recognize `【角色名】` and `【规则】`? And why can errors report “line 12, block ‘锚点’: missing ':' or '：'” — instead of `unexpected token at position 246`?**

The answer is a **handwritten block scanner** — a lightweight lexer that scans line by line, binds precise line numbers, and enforces a block whitelist. See you in the next one.

---

> GitHub: [https://github.com/yuelinghuashu/mephisto](https://github.com/yuelinghuashu/mephisto)