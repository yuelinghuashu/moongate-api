---
title: "Tree-Walking Interpreter Node Architecture (Part 1): Three Reads — All Implemented as Node Methods"
description: "Why evaluation, checking, and rendering all live as node methods; why Node / Expr / Stmt split into three interfaces; what each layer rejected and where its cost lands. Positions and openness are left to the next two parts."
date: 2026-10-27
series: interpreter-node-architecture
order: 1
tags:
  - Go
  - Interpreter
  - Engineering
---

> Part 1 of the series.

Given `1+2*3`, a tree-walking interpreter has to do three **reads**: compute its value, decide whether it is legal, and print it as a readable structure. There is a fourth concern that is not a read: pointing at a source position when something fails, which cuts across the three. The three reads and the position all live on the same tree:

```text
        +
       / \
      1   *
         / \
        2   3
```

This is still just an ordinary tree: operators branch, numbers are leaves; there is no Node type, no fields, and no positions.

Two dimensions frame the discussion. An AST interface really answers only two questions: **where dispatch lives** (who answers "what is this node's value?") and **who guarantees exhaustiveness** (when a new shape or read is added, who is responsible for making omissions visible?).

Neither question has an answer that is good on both counts; you can only choose who pays. The comparison table in Part 3 places five mainstream designs on these two axes.

The premises of this implementation are laid out here as well: **closed syntax, a fixed set of reads (evaluation, type checking, rendering, plus statement execution), and two in-package hosts**. Part 3's table is there to test whether those premises hold.

This series assumes you have written, or at least read, a tree-walking interpreter. It is about trade-offs in interface design, not a from-scratch tutorial; lexing and parsing details are out of scope.

How the five parts split the work:

```text
Part 1: lay down the interfaces (three reads + Expr / Stmt split) — the core
    ├─ Part 2: positions enter the interface (coordinates and errors)
    ├─ Part 3: the export boundary (who can extend)
    ├─ Part 4: order and depth (how to write the method body, where recursion is bounded)
    └─ Part 5: the ring outside the interface (hosts, environment mirroring, immutability, three future seams)
```

## Requirement 1: child nodes must recurse → decision: dispatch on node methods

Start with numbers only: a leaf must evaluate. Give it a minimal representation that stores just a value; `Eval` returns it unchanged. No recursion, no interface.

Add addition and the problem changes: the addition node holds two child expressions, each of which may be a number or another addition. To evaluate the parent, it must first obtain the children's values, so there needs to be a uniform entry point that can invoke "evaluate" regardless of which shape the child has.

The concrete type of a child is unknown at compile time; the parent only knows "can be evaluated". The interface exists for exactly that.

The alternative is to stuff every node into `any` and dispatch with a type switch. Every new node means a new branch at every switch; miss one and the compiler says nothing — the program fails only when execution reaches it. Putting the method in the interface makes each shape answer the evaluation question itself; miss the method and compilation stops.

```go
type ValueKind int

const (
	KindInvalid ValueKind = iota // zero value = no value, not 0
	KindInt
	KindString
	KindBool
)

type Value struct {          // runtime value: int, string, or bool
	kind ValueKind
	i    int64
	s    string
	b    bool
}

type Env struct {
	vars map[string]Value // variable bindings
}

type Node interface { // what all nodes share: position + rendering
	Pos() Position
	writeTo(b *strings.Builder)
}

type Expr interface { // expressions with a value: evaluable, type-checkable
	Node
	Eval(env *Env) (Value, error)
	typeCheck(env *TypeEnv) (Type, error)
}

type NumberLiteral struct {
	Value int64
	At    Position // see Part 2 for the definition
}

type Binary struct {
	Op          Op
	Left, Right Expr
	At          Position
}
```

`Value` is a struct with a discriminant field, not `interface{}` and not generics. It has three kinds (int, string, bool); reading it means checking `kind` first and then taking the value — ordinary method calls, no type assertions. The zero `Value{}` has `kind == KindInvalid`, so "zero value plus error" cannot be misread as a legitimate 0. The price is lowercase fields and construction only through `IntValue` / `StringValue` / `BoolValue`: reads can see values, but cannot change the representation.

These definitions each do one job:

| Definition      | What it is                                              | Role in the design                                                                                                                                            |
| --------------- | ------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Value`         | runtime value: `kind` + `i`/`s`/`b`                     | one return type shared by all nodes; built by `IntValue` / `StringValue` / `BoolValue`, read by `IsInt` / `AsInt` / `IsBool`                                  |
| `Env`           | map from variable names to `Value`                      | evaluation looks names up; `let` writes into it                                                                                                               |
| `Op`            | operator enum with values such as `OpPlus`              | `String()` returns names like `PLUS`, shared by rendering and errors; tied to `opNames` by two-way assertions                                                 |
| `NumberLiteral` | leaf: stores `Value int64` and `At`                     | the field shares its name with the type `Value`, legal in Go; at a glance `n.Value` and the type name `Value` look alike — the only awkward naming in the set |
| `Binary`        | composite: `Op` + `Left` / `Right` (both `Expr`) + `At` | the parent only knows an evaluable `Expr`, not which shape the child has                                                                                      |
| `Node`          | two methods: position + rendering                       | the smallest contract shared by expressions and statements; position and rendering are independent of "has a value"                                           |
| `Expr`          | `Node` + `Eval` + `typeCheck`                           | the interface grown in this part: has a value, evaluates, type-checks; neither `Stmt` shape (`ExprStmt` / `Let`) implements it                                |

Two naming details are worth a note. `NumberLiteral.Value` shares its name with the type `Value`; renaming it to `Int` or `Val` would disambiguate, but `n.Value` reads as "the node's value", so it stays — a trade-off, not a mistake.

`Op.String()` and `TokenType.String()` return the same names (`PLUS`, `STAR`, …), so `(PLUS 1 2)` in an S-expression can be compared directly with the lexer's output; no translation step is needed.

One member has not appeared yet: `Stmt` (statements such as `let`). It adds only `Exec` and a `typeCheck` that does not return a `Type`; it gets its own section at the end of this part.

Leaf evaluation returns the leaf itself; `NumberLiteral.Eval` is one line: `return IntValue(n.Value), nil`.

`Binary` is one step more complex: it first flattens the chain along the left spine, then applies the layers from the inside out, so a very long left-associative chain does not touch the Go stack. Here is the skeleton (the `OpPlus` and `&&`/`||` branches are shown, the remaining operator branches are omitted; the short-circuit guard stays at the entry because it decides whether the right subtree is evaluated at all). `Position` and `errAtPos` are defined in Part 2, `TypeEnv` below in Requirement 2, `Result` at the end of this part; the signatures are enough for now:

```go
func (b Binary) Eval(env *Env) (Value, error) {
	steps, leftmost := leftSpine(b) // Binary nodes along Left, flattened outer-to-inner into steps
	left, err := leftmost.Eval(env)
	if err != nil { return Value{}, err }
	for i := len(steps) - 1; i >= 0; i-- { // apply inner-to-outer
		left, err = applyBinary(steps[i], left, env)
		if err != nil { return Value{}, err }
	}
	return left, nil
}

func applyBinary(st binaryStep, left Value, env *Env) (Value, error) {
	if st.op == OpAnd || st.op == OpOr { // short-circuit: if the left value settles the result, do not evaluate the right subtree
		if !left.IsBool() {
			return Value{}, errAtPos(st.at, KindType, "运算符 %s 需要 bool，得到 %s", st.op, left.TypeName())
		}
		lb := left.AsBool()
		if st.op == OpAnd && !lb { return BoolValue(false), nil }
		if st.op == OpOr && lb { return BoolValue(true), nil }
	}

	right, err := st.right.Eval(env) // the right subtree is evaluated only here
	if err != nil { return Value{}, err }
	switch st.op {
	case OpPlus:
		if left.IsInt() && right.IsInt() {
			return IntValue(left.AsInt() + right.AsInt()), nil
		}
		if left.IsString() && right.IsString() {
			return StringValue(left.AsString() + right.AsString()), nil
		}
		return Value{}, errAtPos(st.at, KindType, "运算符 + 需要 int 或 string，得到 %s 和 %s",
			left.TypeName(), right.TypeName())
	case OpAnd, OpOr:
		if !right.IsBool() {
			return Value{}, errAtPos(st.at, KindType, "运算符 %s 需要 bool，得到 %s", st.op, right.TypeName())
		}
		return BoolValue(right.AsBool()), nil
	}
	// other operator branches (comparisons, modulo, ...) omitted
	return Value{}, errAtPos(st.at, KindInternal, "未知运算符 %d", st.op)
}
```

The leftmost operand is evaluated first, then the layers are composed from the inside out; any failure returns as-is. `+` sums two ints or concatenates two strings; other combinations report a type mismatch. `&&` / `||` inspect the left value at the entry and skip the right subtree when the result is already settled.

`leftSpine` itself is only about ten lines: walk down `Left`, stop at the first non-`Binary`, and collect the operators, positions, and right subtrees along the way into `steps`.

```go
type binaryStep struct {
	op    Op
	at    Position
	right Expr
}

func leftSpine(root Binary) (steps []binaryStep, leftmost Expr) {
	cur := Expr(root)
	for {
		bin, ok := cur.(Binary)
		if !ok {
			return steps, cur
		}
		steps = append(steps, binaryStep{op: bin.Op, at: bin.At, right: bin.Right})
		cur = bin.Left
	}
}
```

Take a chain and watch the direction of `steps` and the return trip:

```text
1 + 2 + 3 → Binary(+, left=Binary(+, 1, 2), right=3)

leftSpine  steps = [{+, right=3}, {+, right=2}]   ← outer to inner
           leftmost = 1
eval       1 → (apply inner +2) → 3 → (apply outer +3) → 6
```

So `Eval`'s loop must walk from `len(steps)-1` back: apply the innermost layer to the leftmost operand, then wrap one layer at a time.

Recursion has not disappeared entirely: the right subtree and `Unary` still recurse — `leftSpine` iterates only over the `Binary` left chain; when it meets a `Unary` it treats it as the leftmost operand and lets `Unary.Eval` descend. The depth of those recursions is bounded by the parser's `enter` / `leave` (Part 4).

Returning an error instead of panicking hands the post-failure decision to the host: file mode exits, the REPL prints the line and continues. A panic would take that decision away from the host.

Errors now carry a position and a kind (`errAtPos` returns `error`; the constructed value is `*Error`; details in Part 2). `Eval` checks types again so that a caller bypassing the pre-check does not silently compute the wrong thing.

That is the first read: composite nodes obtain child results first, then compose. The other two reads reuse the same skeleton.

> **Decision 1 | Dispatch on node methods**
>
> - **Rejected**: `any` plus a type switch — miss a branch and the compiler stays silent; also rejected: a visitor, which moves the "every shape must implement it" obligation from the nodes to every read.
> - **Cost**: adding a read means adding a method to every shape under that interface (`Expr` six, `Stmt` two) — the cost is spread over the nodes, not over the reads.
> - **When it breaks**: when reads keep growing (diagnostics, formatting, multiple backends), that cost compounds; a visitor or function table becomes cheaper (comparison in Part 3).

## Requirement 2: check before evaluating → decision: the pre-check is a second full pass over the tree

> Terminology: this pass is **type checking**; below, "pre-check" and "type checking" are used interchangeably. The error message says "type check failed".

The first read has a problem: errors surface only at execution time. `"a" - 1` parses and starts evaluating, and only fails when the subtraction actually happens; if a `let` statement came earlier, its binding is already in the environment. To confirm the whole program is valid before any side effect, walk the entire tree before evaluation and ask each node for its type.

The skeleton is the same as evaluation; the result and the context differ: return `Type`, look things up in `TypeEnv`. Each has one job:

| Name      | What it is                                             | Use and caveat                                                                                                       |
| --------- | ------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------- |
| `Type`    | `int` / `string` / `bool`; `String()` returns the name | type-check return value; error paths return `TypeCount` (`String()` gives `?`), so the caller must check `err` first |
| `TypeEnv` | map from variable names to `Type`                      | the pre-check looks up types by name; `let` writes types                                                             |

The zero value `TypeInt` is a legitimate type, so the error path cannot be expressed by returning zero: a sentinel `TypeCount` is added. The cost is one more entry in the type table and in the assertions; the benefit is that "returned zero value" is never mistaken for a legitimate type.

`Expr` therefore grows a second method (`typeCheck` starts lowercase; why in Part 3):

```go
type Expr interface {
	Node
	Eval(env *Env) (Value, error)
	typeCheck(env *TypeEnv) (Type, error)
}

func (b Binary) typeCheck(env *TypeEnv) (Type, error) {
	steps, leftmost := leftSpine(b)
	lt, err := leftmost.typeCheck(env)
	if err != nil { return TypeCount, err }
	leftPos := leftmost.Pos()
	for i := len(steps) - 1; i >= 0; i-- {
		st := steps[i]
		rt, err := st.right.typeCheck(env)
		if err != nil { return TypeCount, err }
		lt, err = applyType(st, lt, leftPos, rt)
		if err != nil { return TypeCount, err }
		leftPos = st.at
	}
	return lt, nil
}
```

One more line in the interface is one more obligation for every expression: when a read is added, the compiler checks every shape in the package. Errors still have no position; Part 2 adds positions and decides which node they point at.

`+` has a rule in both `applyType` and `applyBinary`: one decides what type it produces, the other actually computes it. The two must stay in sync — the easiest place in the whole implementation to slip, especially when adding an operator; Part 5 has the checklist.

This pass produces no values, so it can run while "the program has done nothing"; evaluation afterwards only produces side effects on valid programs. The two reads share one tree, which means the tree must not be modified between passes. Part 5 turns that into an invariant.

The pre-check is started by the package-level entry `CheckProgram(stmts []Stmt) error`: it creates an empty `TypeEnv`, calls `typeCheck` on each statement in order, and only hands control to execution when everything passes; the REPL does the same on a temporary `TypeEnv` for the current line (together with a temporary `Env`, Part 5).

`typeCheck` itself is unexported; only this entry is exposed: it owns environment setup and execution order, while `typeCheck` handles a single node.

> **Decision 2 | Make the pre-check a separate pass, not checking while evaluating**
>
> - **Rejected**: checking types along the way during evaluation. Then `"a" - 1` fails only when the subtraction happens, after earlier statements' side effects; in the REPL, even "the line never happened" is impossible.
> - **Cost**: the same type rules written twice; `typeCheck` and `Eval` must be kept in sync by hand (see above); the environment goes from one map to two, mirrored by hand — Part 5 counts that debt.
> - **When it breaks**: when type rules become complex enough to need inference (generics, overloading), two hand-written implementations get out of control; the rules need to move into a table.

## Requirement 3: see the tree's structure → decision: share a Builder, treat writing as a side effect

Both previous reads are for the machine. While writing an interpreter you also need to look at it yourself: what the parser actually built from the line is not visible from the source. The cheapest way is to print an S-expression: `1+2*3` becomes `(PLUS 1 (STAR 2 3))`.

Rendering is the third read; the skeleton is unchanged, the output differs: nothing is returned; text goes into a shared `strings.Builder`. A composite node writes `(` and the operator on the way down, then on the way back appends the right subtree and `)`; a leaf's `writeTo` writes only the literal.

```go
func (n Binary) writeTo(b *strings.Builder) {
	steps, leftmost := leftSpine(n)
	for _, st := range steps { // descending: write the prefixes along the way
		b.WriteByte('(')
		b.WriteString(st.op.String())
		b.WriteByte(' ')
	}
	leftmost.writeTo(b)
	for i := len(steps) - 1; i >= 0; i-- { // returning: append the right subtree and the closing paren
		b.WriteByte(' ')
		steps[i].right.writeTo(b)
		b.WriteByte(')')
	}
}
```

Why a shared Builder instead of each node returning its own string for the parent to concatenate? The latter stacks copying cost by depth: for a tree of depth d, the total cost rises from output length to output length times depth.

A shared Builder moves that cost into the signature: `writeTo` returns `void` and treats writing as a side effect, so rendering is no longer pure. Switching back to a pure function is possible; you pay the copying cost.

At this point `Node` has two methods (`Pos`, `writeTo`), and `Expr` adds `Eval`, `typeCheck`; `typeCheck` and `writeTo` are lowercase (`Eval` is exported; why in Part 3). The package-level rendering entry is `SExpr`: it borrows a `strings.Builder`, writes the node, and takes the string.

`writeTo` is unexported; only this entry is exposed.

Note that its schedule differs from evaluation and checking: rendering writes its own part on entering a node, then descends; evaluation and type checking do the opposite — evaluate the leftmost operand first, then apply layers from the inside out. The scheduling difference is Part 4's topic.

> **Decision 3 | Share one Builder and treat writing as a side effect**
>
> - **Rejected**: each node returns its own string, the parent concatenates. Purer signature, but copying cost grows with depth.
> - **Cost**: rendering is impure; the order is pinned by the output format — S-expressions are prefix notation, so write yourself first and descend; with infix it would be reversed.
> - **When it breaks**: when rendering needs transformations or cached subtree text, "write only, return nothing" is not enough; switch to a returning shape or a callback.

## Expressions and statements: why two interfaces

At this point all three reads hang off one inheritance chain: `Node` owns position and rendering; `Expr` adds evaluation and type checking.

But there is another kind of node: `let`, and future `if`, `while`, blocks. They have no value — they cannot `Eval` to a `Value`, and have no `Type` to check. Forcing them into the same chain means either making `Eval` return a fake value for statements, or making statements pretend to have a type.

So the interface splits into three layers:

- `Node`: position + rendering, shared by every node;
- `Expr`: `Node` + `Eval` + `typeCheck`, expressions with a value;
- `Stmt`: `Node` + `Exec` + `typeCheck` (returns only `error`), statements that only execute.

One term is fixed here: **execution is not a fourth read**. A read is one of the three ways to read an expression (evaluation, type checking, rendering); execution is the operation of a statement — a statement has no value and no type to check; it only puts its result into a `Result`. Later, "four operations" means the three reads plus execution.

`Exec` returns not a `Value` but a `Result`:

```go
type Result struct {
	Value    Value
	HasValue bool
}
```

`let x = 1` and `1+2` both carry a value (both the REPL and file mode display it), so `HasValue` is true; a future `while` has no value and returns `Result{}`, and `Exec`'s signature does not change. The tests contain a stub statement that implements only `Stmt` and produces no value, pinning the contract that a statement may have no value.

The host changed one line too: file mode calls `Exec` per statement against `env`, the REPL against a temporary `tmpEnv` (Part 5); the value to display comes from `Result.Value`, not from `Eval`'s return value.

Why `ExprStmt`? The parser now produces `[]Stmt`; a top-level bare expression (`1+2`) must be wrapped in a statement to enter the list. Its position and S-expression reuse the inner expression, so `SExpr`'s output is byte-for-byte what it was before the split.

The split adds a compile-time guardrail: `var _ Expr = NumberLiteral{}` pins each shape; a node put in the wrong group (a statement inside `Binary.Left`, say) fails to compile.

The cost is one more interface and one more layer of mental overhead: `SExpr` now serves both expressions and statements (`let x = 1` renders as `(let x 1)`), and `typeCheck` needs two versions — the expression one returns `Type`, the statement one returns only `error`. This matches the bet of this part: each shape answers "which group do I belong to?" itself.

> **Decision 4 | Split `Expr` and `Stmt` by "has a value"**
>
> - **Rejected**: one interface for everything, with `Eval` returning a fake value for statements; or adding `TypeUnit` to `Type` so statements can pretend to have a type.
> - **Cost**: one more interface, one more mental layer; two versions of `typeCheck`; `SExpr` serves both.
> - **When it breaks**: adding `if` / `while` / blocks benefits (`Result{}` means no value, `Exec` unchanged); adding functions blurs the boundary again — a function definition is a `Stmt`, a call is an `Expr`.

## Summary: three reads, eight node kinds

With the three reads in place, the implementation has eight node kinds: six expressions (`Expr`) and two statements (`Stmt`). The methods each interface layer carries, and where the eight kinds belong:

```text
Node { Pos, writeTo }
├── Expr { Eval, typeCheck }        ← has a value
│   ├── NumberLiteral{Value int64, At}
│   ├── StringLiteral{Value string, At}
│   ├── BoolLiteral{Value bool, At}
│   ├── Identifier{Name string, At}
│   ├── Binary{Op Op, Left, Right Expr, At}
│   └── Unary{Op Op, X Expr, At}
└── Stmt { Exec, typeCheck }        ← no value
    ├── ExprStmt{X Expr}
    └── Let{Name string, Value Expr, At}
```

This part only expanded numbers and binary operations; the other shapes are written the same way. What each read delivers, which can fail, plus statement execution:

| Need       | Method      | What each node delivers          | How it fails                                                              |
| ---------- | ----------- | -------------------------------- | ------------------------------------------------------------------------- |
| evaluation | `Eval`      | `Value`                          | undefined, division by zero, type mismatch (when bypassing the pre-check) |
| pre-check  | `typeCheck` | `Type` (statements only `error`) | undefined, type mismatch                                                  |
| inspection | `writeTo`   | appends to a shared Builder      | never fails                                                               |
| execution  | `Exec`      | `Result` (value + has-value)     | undefined, division by zero, type mismatch                                |

The three reads share one skeleton; they differ only in what they return, what context they carry, and **where the descent is written**; the last decides traversal order and is Part 4's topic. Here the interface still lacks positions (Part 2), and its openness is decided in Part 3.

After this part you should be able to answer three questions:

- Why are the three reads node methods rather than an external visitor?
- Why is the pre-check a separate pass instead of checking while evaluating?
- Why split the interface into `Expr` and `Stmt` by "has a value"?
