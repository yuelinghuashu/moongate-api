---
title: "Tree-Walking Interpreter Node Architecture (Part 2): Positions and Errors — How Coordinates Enter the Interface"
description: "A position is not another operation but a projection on the interface; why it must be a method rather than a field, why a single point rather than a range, and why the layer name is left to the host."
date: 2026-10-27
series: interpreter-node-architecture
order: 2
tags:
  - Go
  - Interpreter
  - Engineering
---

> Part 2 of the series.

In Part 1, evaluation, execution, and type checking all can fail, and when they do the error carries only a message and no position (`"type mismatch"`, `"undefined variable"`, `"division by zero"` — all the same). In nearly two thousand lines of source, such a message says nothing: you do not know which spot it refers to. This part adds coordinates: why a position is not another operation, how errors carry coordinates, and who owns the layer name of a failure.

It does not add a read: a position neither walks the tree nor produces a value; it only lets each error say "line N, column M". The complication is that it still has to hang off the `Node` interface; once it does, every future shape must implement the method.

## Positions: not another operation

The approach is to give every node a position field and a method that reads it:

```go
type Position struct {
	Line, Column int
}

type Binary struct {
	Op          Op
	Left, Right Expr
	At          Position
}

func (n Binary) Pos() Position { return n.At }

type Node interface { // position + rendering, shared by all nodes
	Pos() Position
	writeTo(b *strings.Builder)
}
```

`Expr` and `Stmt` are not listed again: they simply embed `Node` and add their own methods; see Part 1 for the definitions.

Every node carries its position:

```text
Binary{Op: OpPlus}            @1:2
├── NumberLiteral{Value: 1}   @1:1
└── Binary{Op: OpStar}        @1:4
    ├── NumberLiteral{Value: 2} @1:3
    └── NumberLiteral{Value: 3} @1:5
```

`Pos` only reads the `@line:column` attached to the node. Putting it on the interface defeats two alternatives. **Reading the field directly** does not work: a read's parameter is `Node`, so it cannot reach a concrete type's field. **An optional interface** (only some nodes carry `Positioned`) forces every read to do a type assertion first and downgrades "every node has a position" from a contract to an optional capability; the error path cannot tolerate "this node has no position". So position is not another operation; it is a projection on the interface.

`Pos` lives on `Node`, not on `Expr`, for the same reason: statements also need positions — today a statement's own errors all come from the inner expressions, and `Let.At` is the coordinate reserved for future statement-level errors (duplicate definitions, block endings); statements are not expressions. The three interfaces share this one method, so position becomes a property of "all nodes".

> **Decision 5 | Position as an interface method, a single point rather than a range**
>
> - **Rejected**: reading the field directly (reads only get `Node`); an optional `Positioned` interface (the error path cannot tolerate "this node has no position").
> - **Cost**: one more `At` field and one `Pos()` per node, plus one place that fills `At` at construction; the interface carries one more method forever. A single point cannot answer "mark this span".
> - **When it breaks**: when underlining, multi-line diagnostics, or LSP come in, the single point has to become a `Span` with start and end; `Position`, `errAtPos`, and the position table all change (see the end of this section).

This uses a single-point position: it only answers "which node does the first error point at". Marking a whole span, reporting across lines, or handing diagnostics to an LSP all need a range with start and end. The single point is a deliberately cut boundary, not a general solution.

## How errors carry coordinates

Error construction becomes `errAtPos(pos, kind, format, ...)` (signature `func errAtPos(pos Position, kind ErrorKind, format string, args ...any) error`).

The `"line N, column M: message"` format is assembled by `Error()` and shared by parsing, type checking, and evaluation; `kind` decides which class the error belongs to among `ErrSyntax` / `ErrType` / `ErrUndefinedVar` / `ErrZeroDivisor` / `ErrInternal`.

The parser side uses its thin wrapper `errAt(tok, ...)`: it takes a `Token` and fixes `KindSyntax`.

For `ILLEGAL` / `UNTERMINATED_STRING` / `BOOLEAN`, it appends "which character / what was not closed / which keyword" to the message; internally it still calls `errAtPos`.

The depth-limit error in Part 4 is written `errAt(tok, ...)`, using the same wrapper — not a typo.

Which node an error points at is decided by its source:

| Error source                                | Which position                                         |
| ------------------------------------------- | ------------------------------------------------------ |
| type checking: left operand has wrong type  | that layer's left operand position (`leftPos`)         |
| type checking: right operand has wrong type | that layer's right operand position (`st.right.Pos()`) |
| type checking: operator types do not match  | operator position                                      |
| evaluation: operation cannot be performed   | operator position                                      |
| evaluation / pre-check: undefined variable  | identifier position                                    |

The left-operand row needs care on a left-associative chain: `leftPos` starts as the leftmost operand's position, then is updated after each merge to **the previous layer's operator position**. So it does not remember the leftmost leaf, but the position of "the subtree merged so far". The two cases differ visibly:

```text
1 + 2 * "a"
        ^ column 9: the right operand of STAR has the wrong type (the `"a"` here)

("a" + "b") * 2
     ^ column 6: the left operand is a merged subtree; its position is the operator +
```

Unfold the tree of the second input (`@line:column` is each node's `At`):

```text
Binary{Op: OpStar}  @1:13
├── Binary{Op: OpPlus}  @1:6   ← after merging this layer, leftPos takes the position of +
│   ├── StringLiteral{Value: "a"} @1:2
│   └── StringLiteral{Value: "b"} @1:8
└── NumberLiteral{Value: 2} @1:15
```

Only the second case shows `leftPos` at work: the error points at the `+` of that layer, not at the leftmost `"a"`. That is deliberate: whichever merge layer the problem is in, that is where the error points.

If the pre-check is bypassed and evaluation runs directly, `1 + 2 * "a"` reports at `*` (column 7): that is evaluation's fallback branch, which always uses the operator position.

Three complete errors, with different positions and different targets:

```text
"a" * 2   →  第 1 行，第 1 列: 运算符 STAR 需要整数，得到 string
1 + "a"   →  第 1 行，第 3 列: + 两侧类型不同：int 和 string
1/0       →  第 1 行，第 2 列: 除数为零
```

In English, they report that STAR needs an integer (got a string), that `+` has mismatched sides (int and string), and that the divisor is zero. The first points at `"a"` because type checking asks "is this operand's type right?"; the second points at the operator because both operands are legal and the problem is the act of adding; the third also points at the operator because evaluation asks "can this operation be performed?".

The rule is one sentence: **point at whoever owns the problem**. Making that rule work depends on being able to get a position from any node; that is also why position is an interface method rather than an external index table: a read's parameter is only `Node`, so the coordinates it needs must be in the contract.

The error itself is `*Error{Pos, Kind, Msg}`: position and kind travel to the caller together; `errors.As` extracts `Pos` for reordering, `errors.Is` dispatches on kind; neither needs to match text.

## The boundary of positions: when a single point is not enough

A single point is enough for an interpreter, because an interpreter only needs to answer "where does this error point".

Change the requirement and the shape of the position changes: formatters, linters, and IDEs need comments, incremental edits, and therefore range-carrying syntax trees, parent pointers, and trivia (comments, whitespace — marks that do not participate in semantics).

Whether the tree is mutable or immutable follows the use case too, but that is another topic (Part 5); here it is only about position: once a whole span must be marked, the position goes from a point to a range.

Several mainstream position representations side by side make the difference obvious:

| Design              | Position representation                     |
| ------------------- | ------------------------------------------- |
| Lox (jlox)          | a `Token` reference                         |
| Lox (clox)          | none (no tree is kept)                      |
| ESTree / Babel      | `loc` / `start` / `end`                     |
| TypeScript          | `pos` / `end`                               |
| Rust `syn`          | `Span`                                      |
| Go `go/ast`         | `Pos()` / `End()` (`token.Pos` + `FileSet`) |
| this implementation | `At`, a single point                        |

The boundary is not "how precise", but "how much of the source the error must mark". This implementation needs at most "which spot", so one point suffices.

Once a whole span, multiple lines, or a squiggle in an editor is needed, `Position` must become `Span`, and `errAtPos` and the position table above change with it.

Go's `go/ast` takes the range route: `Node` has only `Pos()` / `End()`, positions are `token.Pos` offsets, and line/column are computed on demand from `token.FileSet`. This implementation stores line/column directly in `At`, saving `FileSet` and `End()` computation but unable to mark a span; to serve an LSP, switching to `Span` is only the first step — the byte offsets of `token.Pos` and the UTF-16 columns the LSP wants still need conversion.

This boundary is also reflected back from elsewhere. Part 4 puts a depth gate in the parser but no matching gate on evaluation — not because "positions cannot say it", but because **evaluation no longer needs it**.

After the left-spine iteration (Part 4), evaluation has no unbounded recursion left; the only unbounded recursion is the parser's three paths (parentheses, unary minus, unary `!`), which are stopped during parsing. The position protocol therefore only has to answer "which spot".

## The layer name is added by the host

The layer name (parse failure, type check failure, evaluation failure) is not part of the error itself. File mode adds it when wrapping the error, the REPL adds it when printing; both are host actions at the boundary. The error itself carries only position, kind, and message.

Another option is to put a layer field on the error, or use error codes. This implementation takes a middle path: the error carries a **kind** but no layer.

Five sentinels `ErrSyntax` / `ErrType` / `ErrUndefinedVar` / `ErrZeroDivisor` / `ErrInternal` classify; `errors.Is` matches by kind, `errors.As` extracts `*Error` to read the position.

Layering is still added by the host: `run` wraps errors with "parse failed / type check failed / evaluation failed"; the REPL adds its own prefixes.

The benefit is that programs can dispatch by kind; tests and future error recovery do not need to match text.

The cost is maintaining the kind table, and kinds do not map one-to-one to layers: a `KindType` may come from the pre-check or from evaluation's fallback. So this part separates three things: **format for the reader** (`Error()` renders uniformly), **kind for the program** (sentinels + `errors.Is`), **layer stays with the host**.

The two hosts differ in more than where errors go: who owns the environment after a failure and whether the whole line rolls back are Part 5's topic. Before that one decision remains: how many of the three interfaces' methods to export (Part 3).
