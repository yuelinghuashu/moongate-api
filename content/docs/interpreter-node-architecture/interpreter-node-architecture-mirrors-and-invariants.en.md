---
title: "Tree-Walking Interpreter Node Architecture (Part 5): The Boundary of the Node Contract — Contexts, Failures, and Environment Mirroring"
description: "The Node contract guarantees only positions and rendering; dispatch lives on Expr / Stmt methods. This part stands at the contract boundary: first the host and environment mirroring (why the two-environment mirror is the cost of the pre-check), then the contract itself (immutability and invariants), and finally the two things outside the interface."
date: 2026-10-27
series: interpreter-node-architecture
order: 5
tags:
  - Go
  - Interpreter
  - Engineering
---

> Part 5 of the series.

The first four parts stayed inside the Node interface: how the three reads dispatch, how positions enter the contract, how the shape set expands only inside the package, and how order and depth are decided by method bodies. This part steps back and stands at the contract boundary: first the host and environment mirroring, then the contract itself, and finally the two things outside the interface.

First, draw the boundary: **`Node` guarantees exactly two things — position (`Pos`) and rendering (`writeTo`).** Expressions with a value add `Eval` and `typeCheck`; statements add `Exec` and a `typeCheck` that only reports errors; the shape set is defined in-package (all three methods are unexported).

It does not guarantee what context each operation needs, nor who owns the environment after a failure. Those belong to the host, and the host's choices feed back into constraints on the interface.

One sentence to remember this part's stance: the two-environment mirror is not a Node design; it is the cost of the decision "pre-check before evaluation". Tree immutability is a convention written into the contract's semantics but not enforced by interface methods — it is held up by value receivers and tests.

## Contexts and failures: the host's ring

The four operations each need a context: rendering writes a `strings.Builder`, type checking writes a `TypeEnv`, evaluation and execution write an `Env`.

Their failure surfaces differ too — `Pos` and `writeTo` cannot fail; `Eval`, `Exec`, and both `typeCheck` methods can, so only those methods carry `error`. Failure is a property of the read, not of the node.

Who prepares the context and who owns it after a failure depends on the host.

**How a failure ends.** Start with file mode. The source is two lines: `let x = 1`, then `x/0`. When execution reaches evaluation (only this excerpt):

```text
== 3. 求值 ==
  语句[0] 返回值 = 1
求值失败：第 2 行，第 2 列: 除数为零
```

(The header says "evaluation", the first statement's value is 1, and then evaluation fails at line 2, column 2: division by zero.)

The first statement has already run; `x` is already in the environment before the second fails. File mode does one thing: prints the error and exits with a non-zero code. Partial evaluation did happen, but there is no observer; when the process exits, `x` disappears.

Every line in the REPL is observable, so a failure must make the whole line never happen.

Both hosts run the same layering (parse, type check, evaluate); they differ in exactly two places: who owns the environment after a failure, and where errors are printed. For error content and format see Part 2; this part only cares where errors go.

| Host      | Consequence of failure                            | Environment lifetime                         |
| --------- | ------------------------------------------------- | -------------------------------------------- |
| file mode | process exits; evaluated statements are discarded | dies with the process                        |
| REPL      | session continues; the whole line rolls back      | temporary copy, swapped back only on success |

**The cost of the pre-check.** Part 1's Decision 2 was "the pre-check is a second full pass over the tree". Its cost is here: types exist before values, so the environment has to be kept in two copies, synchronized by hand.

`Let` is the statement `let name = expression`; its main fields are `Name` and `Value` (plus the position `At`). Its shape in the two environments:

```go
func (l Let) typeCheck(env *TypeEnv) error {
	t, err := l.Value.typeCheck(env)
	if err != nil { return err }
	env.Bind(l.Name, t)
	return nil
}

func (l Let) Exec(env *Env) (Result, error) {
	v, err := l.Value.Eval(env)
	if err != nil { return Result{}, err }
	env.Set(l.Name, v)
	return Result{Value: v, HasValue: true}, nil
}
```

Type checking runs over the whole input before evaluation (the whole program in file mode, one line in the REPL), so it can only obtain types; only evaluation obtains values.

Two environments are therefore necessary, but they must stay in sync: the same `let` must have the same key in both.

Synchronization relies on "writing the two methods as a pair"; no mechanism guarantees it. The statement interface does not return a type, and the type stays only in `TypeEnv`, so there is not even a "carry it out along the way" route.

The REPL's approach is to make one input a transaction: copy both `Env` and `TypeEnv` into temporary copies, and swap both back into the main environment only when both passes succeed (`env.vars = tmpEnv.vars`, `typeEnv.vars = tmpTypeEnv.vars`); on failure, roll back the whole line. The flow:

```text
line ──▶ parse ──▶ clone(Env, TypeEnv)
                       │
                       ├─ typeCheck(tmpTypeEnv) ──fail──▶ discard; main environment unchanged
                       │
                       └─ Exec(tmpEnv) ──fail──▶ discard; main environment unchanged
                                │
                                └─ success ──▶ commit: env.vars = tmpEnv.vars
                                                   typeEnv.vars = tmpTypeEnv.vars
```

Take `let y = 1/0`:

```text
toy> let y = 1/0
求值失败：第 1 行，第 10 列: 除数为零
toy> y
类型检查失败：第 1 行，第 1 列: 未定义变量 "y"
toy> :env
无绑定
```

(In English: evaluation fails at line 1, column 10: division by zero; the next line reports an undefined variable `y`; `:env` lists no bindings.)

Type checking did `Bind` `y`, but the temporary environment is discarded after the evaluation failure, so `y` has neither a value nor a type.

If the two environments diverge because of a bug, `:env` marks the missing side explicitly as `<unknown>` or `<unbound>` instead of letting a map zero value pass as an `int`. Concretely: `<unknown>` means a value exists and the type side is missing; `<unbound>` means a type exists and the value side is missing.

Each alternative has a problem:

- **Merge into one environment**: write a placeholder during type checking; the placeholder is eventually overwritten by the evaluation result, but the "no value yet" state disappears, and there is no longer any way to tell "declared but not computed" from "computed".
- **Let `Value` carry its type**: create a value with a type but no content during checking; that hides the difference between the two representations inside one type.
- **Postpone checking to evaluation time**: the pre-check disappears, and the phenomenon from Part 4 — `1/0 + "a"` never even starts evaluating — is gone too.

The conclusion is not pretty: the mirror is the direct consequence of "types exist before values", and it is real architectural debt. Only two things can be done: make synchronization a transaction (the REPL's temporary environments), and make divergence visible (`:env` tagging).

## Immutability: half from the type system, half from convention

This section is the only part of the article that truly belongs to the Node contract. Everything above was about the host; there the interface constrains only "dispatch" and "the shape set". "Can a node be rewritten?" is where the constraint falls back on the nodes themselves.

Other trees made a different choice: Babel allows reads to mutate nodes in place, which suits transformation pipelines; Roslyn (.NET's compiler platform) and rowan (the lossless syntax tree library used by rust-analyzer) make the tree an immutable value and trade re-copying for caching and incremental parsing. This implementation chooses immutability, for the same reason it chose a single-point position: if it is enough, do not add mechanism.

A node stores only data fields and a position: literals, operators, child nodes, names, plus `At`; no parent pointers, no cached types, no computed values. This is not to save a few fields; it is the premise for several reads sharing one tree: read results are not written back into nodes, and the tree stays unchanged until the next read.

Half of that immutability comes directly from the type system, not from people following rules: methods use value receivers, and nodes exist as values inside `Node` and the interfaces they belong to, so what a read gets is always a copy. `n.Left = ...` changes a copy and never reaches the tree; reads do not even have an entry point for "just tweak it a little".

That is also why receivers are values, not pointers: a pointer receiver would hand the rewrite entry point back out.

The only remaining path that can still change the tree is the host holding `[]Stmt`: any pass can replace a slice element, for example "constant-fold while checking". The compiler does not stop that; only test cases can.

One test case takes a snapshot of the tree's shape before the reads, then runs `SExpr` + type checking + execution three times, comparing the signature each time and also comparing the tree snapshot; replacing an element makes it fail.

Only one test case guards this invariant: in the appendix list, the "who would notice" column is mostly empty; during tests there is still this shape-comparison case, and at runtime it is silent. Writing "a node is never modified after construction" as a precondition of the package-level entries has not been done either. Once it breaks, type checking and execution no longer face the same tree.

## The Node contract's invariants: two hard constraints, one convention

Pull out the things the interface must always satisfy; there are three, in two classes:

| Contract invariant                          | What enforces it                                                                                  | Who would notice                        |
| ------------------------------------------- | ------------------------------------------------------------------------------------------------- | --------------------------------------- |
| every shape implements its group's methods  | the interface plus the compiler: `Expr` needs `Eval`+`typeCheck`, `Stmt` needs `Exec`+`typeCheck` | compile failure                         |
| every node carries a position               | `Pos()` is in `Node`, inherited by all three interfaces                                           | compile failure                         |
| a node is never modified after construction | value semantics block in-place rewrites; slice replacement relies on tests                        | nobody (a snapshot test during testing) |

The first two are hard constraints: the compiler enforces them. The second is actually a special case of the first (`Pos` is also in `Node`); it is listed separately because Part 2 argued for it on its own.

The third is only a convention: value receivers block in-place rewrites, but "replace a slice element" is not stopped by the compiler and can only be caught by tests — that is the immutability from the previous section.

The lexical table, the precedence table, the two environments, evaluation depth, and so on are all guarded by something and missing something, but they are not this interface's business. The full list is in the appendix for anyone who needs it.

## The part Node does not own: the operator seam is in the tables

Part 3 counted the cost of two extensions: adding a read and adding a shape. Adding an operator is a third direction, and it does not touch this interface at all — the six places to change include none on `Node` / `Expr` / `Stmt`, plus one test sample (Part 3 listed them briefly; here is why each is where it is):

| Change point                                         | What enforces it                                           |
| ---------------------------------------------------- | ---------------------------------------------------------- |
| lexer-level token type and name table                | compile time (two-way negative-length assertions)          |
| lexer-level symbol table (only for new symbols)      | tests (the all-symbols sweep case)                         |
| precedence table indexed by token (`binOps`)         | tests (metadata test cases)                                |
| operator constants and name table (`Op` + `opNames`) | compile time + tests                                       |
| type-check branch (`applyType`)                      | none (the compiler does not check `switch` exhaustiveness) |
| evaluation branch (`applyBinary`)                    | none                                                       |

Tests also need one more sample — if it is missing, running the cases reports "missing sample", although the implementation itself already works.

The parser does not change a line, because the precedence table is data. So this is not "the interface has to grow something"; it is "someone outside the interface carries it on".

Compared with the seven places for adding a node in Part 3, the two sides differ: the node seam is on the interface, and a missing method fails to compile, while a missing parser branch is found only by tests; the operator seam is in the tables, the parser is freed by data, but type checking and evaluation each still need a hand-written branch.

On paper, these two branches can also go into a table:

```go
type opSpec struct {
	prec  int
	check func(l, r Type) (Type, error)
	apply func(l, r Value) (Value, error)
}
```

Adding an operator then drops from six places to five (token type and name, lexer symbol, `binOps`, operator constants, spec table entry), and type checking and evaluation no longer each keep a branch.

The cost is that rules are encoded as function values, and `switch` readability is lost. Also, the "check first, evaluate after" order must be guaranteed uniformly by table-driven code instead of by the two methods' own shapes. It is still a trade-off; the cost just moves.

This implementation still writes branches, because each side's branches are only a few lines and read more directly than an extra spec layer.

> **Decision 7 | Add operators with a table + two branches, not an `opSpec` table first**
>
> - **Rejected**: moving `check` / `apply` into `opSpec` (rules become function values, `switch` readability is lost, and "check first, evaluate after" must be enforced by table-driven code).
> - **Cost**: one branch each for type checking and evaluation, kept in sync by hand; a missing branch is not reported by the compiler, only by samples and test cases; token/Op name tables, `binOps`, and the lexer symbol table must change together.
> - **When it breaks**: when the number or complexity of operators grows (matrices, string comparison, implicit conversion), or when uniform metadata (associativity, type signatures, documentation) is needed, the `opSpec` table is cheaper.

## The three seams below: blocks, functions, and error recovery

The above is what already exists but is not Node's business; the next layer that will really change the shape set is these three.

Only the three directly related to the shape set are worth mentioning here. The rest (environment chains, the protocol details of error recovery) belong to the host, and can get their own part when they are actually built.

- **Statement lists become blocks**: `[]Stmt` already contains `ExprStmt` and `Let`; adding `if` / `while` means adding another `Stmt`, with `Result{}` meaning "no value", and a block is a slice of `Stmt` — however far the shape set grows, `Exec`'s signature stays put.
- **Functions**: a function needs a parameter list and a body; a call is a new `Expr` (with a value), a definition may be a new `Stmt`; the environment goes from one map to a chain, and the type environment follows.
- **Error recovery**: the protocol must be able to "skip a statement and continue"; that is a layer above `Stmt` and does not exist yet. `Error` already carries a kind; future dispatch can go by `Kind` instead of matching text.

The series ends here. The tree's shape, coordinates, export boundary, scheduling, and contract boundary are all in place; further down, each seam can become its own article.

## Appendix: invariants and known gaps

The following are unrelated to the Node contract but belong to this implementation; they are listed here for reference. The last column in both tables is "who would notice when it breaks".

### Invariants that hold

| Invariant                                                 | What enforces it                                                                                                            | Who would notice               |
| --------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- | ------------------------------ |
| name table length matches the enum count                  | three copies of the two-way negative-length assertions (token / op / type)                                                  | compile failure                |
| name table order matches the enum order                   | `tokenNames` keyed literals (compile time); `opNames` / `typeNames` via test cases                                          | compile failure / test failure |
| each operator is attached to exactly one precedence level | two metadata test cases: unique attachment and coverage, plus per-operator samples end to end                               | test failure                   |
| parse nesting stays under the limit                       | `enter` / `leave` pairing (Part 4)                                                                                          | positional error (normal path) |
| evaluation depth is bounded                               | left-spine iteration: the left chain does not use the stack, right recursion ≤ `maxDepth`; a 1,000,000-item regression case | test failure                   |

### Known gaps (including partial guarantees)

| Invariant                               | Current state                                                                  | Who would notice                                               |
| --------------------------------------- | ------------------------------------------------------------------------------ | -------------------------------------------------------------- |
| the two environments have the same keys | the REPL transaction keeps normal paths paired; divergence is tagged by `:env` | divergence visible; no automatic detection (partial guarantee) |
| integer arithmetic does not overflow    | none (Go semantics wrap silently)                                              | nobody                                                         |

The negative-length assertion is worth a look:

```go
var _ [int(tokenTypeCount) - len(tokenNames)]struct{}
var _ [len(tokenNames) - int(tokenTypeCount)]struct{}
```

If the name table has one entry too few or too many, one of the two array lengths becomes negative and compilation fails. Used together, both a missing name and an extra name are caught.

It guards only the count: a wrong order still has equal length, so order needs another mechanism. This division — compile time blocks the count, tests block the content — gives faster feedback than tests alone.

Here `tokenNames` is the name table corresponding one-to-one with the enum, defined as a `[...]string` array so that `len` is a compile-time constant; switch it to a slice and the two assertions fail to compile.

The same two-way assertions exist in `operator.go` for `opNames` / `opCount` and in `typecheck.go` for `typeNames` / `TypeCount`. The difference is order: `tokenNames` is pinned at compile time by keyed literals, while `opNames` / `typeNames` are unkeyed literals whose order is covered by test cases (the names in S-expressions and the `:env` output); the count in all three tables is pinned by the two-way assertions, so one entry too few or too many fails to compile.

## Appendix: seven decision cards

Part 1 has four (dispatch, pre-check, rendering, interface split), Part 2 has one (positions), Part 3 has one (export boundary), Part 5 has one (operators). Side by side, they are the full set of choices behind the interface:

| Decision                                            | Rejected alternative                                      | Cost                                                                                        | When it breaks                                  |
| --------------------------------------------------- | --------------------------------------------------------- | ------------------------------------------------------------------------------------------- | ----------------------------------------------- |
| (Part 1) dispatch on node methods                   | `any` + type switch; visitor                              | adding a read means adding a method to every shape in the package                           | reads keep growing                              |
| (Part 1) the pre-check is a second full pass        | checking while evaluating                                 | rules written twice; the two environments mirrored by hand                                  | type rules get complex enough to need inference |
| (Part 1) rendering shares a Builder                 | each node returns a string, parent concatenates           | rendering is impure; order is pinned by the output format                                   | transformations inside rendering                |
| (Part 1) split `Expr` / `Stmt` by value presence    | one interface + fake values; `TypeUnit` pretending a type | one more interface; two versions of `typeCheck`; `SExpr` serves both                        | adding functions blurs the boundary             |
| (Part 2) position as an interface method            | reading fields directly; optional `Positioned`            | one more field and method per node; a single point cannot answer a range                    | underlining, multi-line, LSP                    |
| (Part 3) unexported methods limit outside extension | export everything; move `Eval` into entries too           | outside code cannot add reads or shapes; the embedding exception                            | integrating formatting, lint, backends          |
| (Part 5) operators use a table + two branches       | move everything into `opSpec`                             | the two branches are synchronized by hand; a missing branch is not reported by the compiler | rule complexity grows                           |

Put the seven rows side by side: the costs fall at two ends — some land on every node (dispatch, position, export boundary), some stay on the read side (the pre-check writes the rules twice, rendering loses purity); the interface split touches both — one more interface, two versions of `typeCheck`. None of them is good on both counts.
