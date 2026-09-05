---
title: Three Attempts to Refactor a Large Flutter State Class — and the Final Solution
description: When an 800-line State class mixes contract trees, stage operations, and navigation — how do you safely split it? A complete refactoring journey from Mixin and part to Widget Composition.
date: 2026-08-11
level: P3
series: flutter-practice
tags:
  - Flutter
  - Dart
  - State Management
  - Engineering
  - Engineering
---

> A step-by-step record of refactoring an 800-line `State` class.  
> What do you do when your State grows beyond maintainability?

---

## 1. When Do You Know It's Time to Split?

When a **State class** grows to **several hundred lines**, it's usually a sign that it's taken on too many responsibilities.

In a real-world home screen, the 800+ line `_HomeScreenState` mixed together:

| Responsibility        | Example Methods                                      |
| --------------------- | ---------------------------------------------------- |
| Contract tree actions | Master/subcontract menus, rename, delete confirm     |
| Stage actions         | Stage card menu, multi-select, character row actions |
| Navigation            | Navigate to narrative / stage pages                  |
| File operations       | Import / create new / restore built-in               |
| Pure rendering        | Contract tree list, stage dashboard, recent edits    |

> **Rule of thumb:** If the same class contains more than **3 unrelated concerns**, it's time to split.  
> Line count is a symptom, not the root cause — **responsibility mixing** is.

---

## 2. Attempt 1: Extract Mixins → Blocked by Library-Private Visibility

The most intuitive move: extract methods into Mixins, then `with` them in.

```dart
// home_screen.dart
class _HomeScreenState extends ConsumerState<HomeScreen>
    with HomeContractActions, HomeStageActions { ... }

// home_contract_actions.dart (separate file)
mixin HomeContractActions on ConsumerState<HomeScreen> {
  Future<void> _handleMasterMenu(...) { ... }  // ❌ Not accessible from host!
}
```

### Error

`The method '_handleMasterMenu' isn't defined for the type '_HomeScreenState'`

### Why?

Dart's `_` prefix is **library-level private**, not class-level private.

- In many languages (Java, C++, TS), `private` means "visible within the class" → mixin members are naturally visible.
- In Dart, `_` means "**visible only within the same library file**" — across files, even if mixed into the same class, the host can't see the mixin's private members.

There's a "workaround" that looks plausible at first: declare abstract getters in the mixin and implement them in the host.

```dart
mixin HomeStageActions on ConsumerState<HomeScreen> {
  HomeSelectionController get selection; // abstract getter
  void refreshLists();
  Future<void> _handleStageMenu(...) { ... } // ❌ Still private, host can't see it
}
```

The data bridge works (`selection` / `refreshLists`), but **method visibility** remains broken — `_handleStageMenu` is still private to a different library.

### Clarification: The Problem Isn't Mixins — It's Cross-File

If you define the mixin **in the same file**, it works perfectly:

```dart
// home_screen.dart (same file)
mixin HomeStageActions on ConsumerState<HomeScreen> {
  Future<void> _handleStageMenu(...) { ... } // ✅ Visible, same file
}

class _HomeScreenState extends ConsumerState<HomeScreen>
    with HomeStageActions { ... }
```

So the intuition "use Mixins to split State" is **not wrong**. The real hard constraint is: **Dart's library-private visibility + files as library boundaries**. Once you move methods to another file, `_` becomes a hard wall.

> 📌 **Key takeaway:** Dart's private isn't "class-private" — it's "**library-private**".  
> Across files, `_` members are isolated by default; mixins are only safe if they stay in the same file.

---

## 3. Attempt 2: Use `part` → Blocked by Missing `this`

If `_` is library-private, then using `part` to split one library across multiple files should let private members be shared — right?

```dart
// home_screen.dart
part 'home_screen_contract.dart';
part 'home_screen_stage.dart';

// home_screen_contract.dart
part of 'home_screen.dart';

Future<void> _handleMasterMenu(...) { ... } // ✅ Private now visible
```

`part of` does make private members visible (I can see `_selection` / `_refreshLists`), **but a new problem appears:**

> **Top-level functions have no `this` context.**

Code inside `part of` is **library-level top-level functions**, not methods of the host class. So:

```dart
part of 'home_screen.dart';

Future<void> editContract(ContractInfo info) {
  return editContractFile(
    context,          // ❌ Undefined name 'context'
    onRefreshLists: _refreshLists, // ❌ Undefined name '_refreshLists'
  );
}
```

`context`, `ref`, `mounted`, `_selection` are all **instance members** of `_HomeScreenState`. Top-level functions can't access them.

### So When Is `part` Actually the Right Tool?

The project already had a successful precedent — **freezed-generated code**:

```dart
// narrative_state.dart
import 'package:freezed_annotation/freezed_annotation.dart';
part 'narrative_state.freezed.dart'; // ✅ Pure generated code, no this dependency
```

`narrative_state.freezed.dart` is generated by the freezed tool, depending only on the `@freezed` annotated `_NarrativeState` class. It **doesn't need to access any host instance members**. That kind of "pure declaration + generated implementation" split is what `part` is actually for.

> 📌 **Key takeaway:** `part` shares **library-private visibility**, not **host instance context**.  
> If your goal is to split methods that need to access `this`, `part` is not the answer.

---

## 4. Final Solution: Widget Composition — Back to the Framework Philosophy (and a Reasonable Boundary)

After two detours, the answer was the classic **Flutter Composition** model all along:

> **Extract rendering into independent Widgets, and inject interactions via callbacks.**

### Core Principle

| What                                                                     | Where It Belongs                       | Why                                                                 |
| ------------------------------------------------------------------------ | -------------------------------------- | ------------------------------------------------------------------- |
| **Interaction orchestration** depending on `ref` / `context` / `mounted` | Stay in State                          | These are State's lifecycle capabilities                            |
| **Read-only view building**                                              | Extract to standalone `ConsumerWidget` | Pure functional rendering — independently maintainable and testable |

### Implementation

Extract the "contract tree + stage dashboard + recent edits" rendering block into `ContractTreeSection`:

```dart
class ContractTreeSection extends ConsumerWidget {
  final List<ContractGroup> groups;
  final HomeSelectionController selection;

  // ---- All interactions injected via callbacks ----
  final void Function(BuildContext, ContractGroup) onMasterTap;
  final void Function(ContractInfo child, String action) onChildMenu;
  final void Function(String stagePath) onStageTap;
  // ...

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    // Pure rendering only: ListView + ContractCard + StageSection ...
  }
}
```

The host `_HomeScreenState` is left with just **assembly + delegation**:

```dart
return ContractTreeSection(
  groups: groups,
  selection: _selection,
  onMasterTap: _onMasterTap,
  onChildMenu: (child, action) => _handleChildMenu(context, child, action),
  onStageTap: _openStageNarrative,
  // ...
);
```

#### Results (split ≠ deleting code; it's about rehoming responsibilities)

| File                         | Before | After | Responsibility                                                  |
| ---------------------------- | ------ | ----- | --------------------------------------------------------------- |
| `home_screen.dart`           | 816    | 640   | Interaction orchestration (assembly + navigation + delegation)  |
| `contract_tree_section.dart` | —      | 263   | Contract tree + stage dashboard + recent edits (pure rendering) |
| `stage_card.dart`            | 806    | 529   | Pure presentation `StageCard`                                   |
| `stage_section.dart`         | —      | 192   | Stage list area                                                 |
| `stage_card_with_meta.dart`  | —      | 92    | Stage data loading glue layer                                   |

**Total code volume stayed the same**, but each file's responsibility went from "mixed" to "single." Pure presentation components (`StageCard` / `ContractTreeSection`) can now be tested independently without a State.

> `stage_card_with_meta.dart` is the thin glue between "data loading" and "pure rendering": it reads stage data (character list, save detection, recent activity) from Riverpod and passes it unchanged to the pure `StageCard` — keeping `StageCard` itself Riverpod-free and easier to test.

### Why This Is "The Flutter Way"

Flutter's composition model is inherently about "a Widget renders, callbacks go upward":

- `Button.onPressed`, `TextField.onChanged`, `ListView.builder` — all examples of callback-based decoupling.
- The project already did this with `ContractCard` / `StageSection` — **this time we just applied the same pattern to a whole page-level rendering block**.
- **It also reduces rebuild scope:** extracting into a standalone `ConsumerWidget` means unrelated field changes in the State won't reach this subtree — Flutter's `Element` reuse + subtree isolation prevents unnecessary repaints of `ContractTreeSection`.

Mixins are for **behavior reuse** (e.g., `GenerationCoordinator`). Using them to reorganize State is simply a misapplication.

> 💡 **TIPS: Should the `String action` be replaced with `enum`?**
> A `String` for `action` might trigger type-safety instincts. But for PopupMenu scenarios, this is a pragmatic trade-off: `PopupMenuItem<String>` works natively with String payloads, `menu_actions.dart` shared constants already provide compile-time guardrails, and menu actions are an evolving set (each new action with an enum would require modifying the enum definition + updating every `switch`).
> **Bottom line:** If the action set is closed and stable (like `MessageRole`), enum exhaustiveness is worth it. Otherwise, String + shared constants fits the PopupMenu ecosystem better.

### Why We Stopped Here (and Didn't Extract a Coordinator)

Extracting interaction orchestration to a Riverpod `Notifier` coordinator is a common advanced suggestion — but we evaluated it and **decided not to**:

- The remaining navigation / dialog / menu dispatch in the State is **inherently UI work** — `Navigator.push`, `ScaffoldMessenger`, and confirmation dialogs all need `BuildContext`. Moving them to a Notifier just shifts the shell without reducing parameters.
- These logic blocks were **already extracted to top-level functions** (`home_operations.dart` / `home_menu_actions.dart`). What's left in the State is clean one-line delegations.
- Adding a coordinator would introduce `ref.read(coordinator.notifier).xxx()` + `context.mounted` boilerplate — extra indirection that increases cognitive load without real gain.

> So 640 lines isn't "unfinished" — it's **a reasonable stopping point after achieving single responsibility**. Maintainability is about "every method clearly shows what it does," not line count.
>
> 📌 **Key takeaway:** When you hit language-mechanism limits, don't hack around them with `dynamic` or workarounds.  
> Revisit the architecture from a framework-idiomatic angle — that's usually the real answer. **Knowing when to stop is also part of architectural judgment.**

---

## 5. Decision Tree: Which Approach to Choose

```text
State class too large?
│
├─ Refactoring for behavior reuse? (e.g., streaming throttling, generation coordination)
│    └─ ✅ Use Mixin (define in the same file to avoid library-private issues)
│
├─ Splitting pure data / generated code? (e.g., freezed .g.dart)
│    └─ ✅ Use part / part of
│
└─ Splitting State methods that need `this`?
     ├─ Pure rendering parts → ✅ Extract as standalone Widget + callback injection
     └─ Interaction orchestration parts → ✅ Keep in State
```

| Approach                          | Best For                           | Key Limitation                                  |
| --------------------------------- | ---------------------------------- | ----------------------------------------------- |
| **Mixin**                         | Behavior reuse (no cross-file)     | `_` is library-private; invisible across files  |
| **part**                          | Generated code / pure              | Shares private visibility but no `this` context |
| **Standalone Widget + Callbacks** | Rendering ↔ Interaction decoupling | Requires explicit parameter passing (callbacks) |

---

## 6. Engineering Safeguards for Landing the Refactor

Refactoring is where things break most easily, so **pure moves + pure parameter extraction = zero behavioral change** is the baseline:

1. **Keep behavior identical:** Only move method bodies and rewrite call sites — no logic changes.
2. **Static analysis as gatekeeper:** `flutter analyze` must return `No issues found!`
3. **Full test suite:** 406 tests all green before calling it done.
4. **Small commits:** Split one concern at a time, validate independently — avoid "big bang" refactoring.

---

## Summary

- Dart's `_` is **library-private**, not class-private → Mixins fail across files.
- `part` shares private visibility but has no `this` → not suitable for splitting State methods.
- **Extract standalone Widget + inject callbacks** is the Flutter-idiomatic way to refactor oversized State classes.
- Decoupling rendering from interaction aligns with the framework philosophy and enables independent maintenance and testing.

---

Project: [Mephisto](https://github.com/yuelinghuashu/mephisto-gui)
