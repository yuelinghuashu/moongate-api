---
title: "Tree-Walking Interpreter Node Architecture (Part 4): Order and Depth — Descent Position and Recursion Bound"
description: "Preorder and postorder do not come from traversal parameters; they come from whether the method body writes itself first or takes the child results first. A left-associative chain never touches the stack; recursion remains only in right subtrees, parentheses, and unary operators, so the limit sits only at the parser."
date: 2026-10-27
series: interpreter-node-architecture
order: 4
tags:
  - Go
  - Interpreter
  - Engineering
---

> Part 4 of the series.

Part 1 set up the three reads; Parts 2 and 3 dealt with coordinates and the export boundary. This part answers two questions only: what "write yourself first, or take the child results first" in a method body decides, and which side carries the unbounded cost of recursion. Both live inside node methods; there is no need to look at the host.

## Where the descent is written decides the order

These operations have almost the same skeleton; the difference is only "write my own part first, or take the child results first":

| Operation   | On entering a node               | Direction of descent                 | On the way back                    |
| ----------- | -------------------------------- | ------------------------------------ | ---------------------------------- |
| `writeTo`   | writes `(` and the operator name | descends along the left spine        | appends the right subtree and `)`  |
| `Eval`      | writes nothing                   | evaluates the leftmost operand first | applies layers from inner to outer |
| `typeCheck` | writes nothing                   | checks the leftmost operand first    | merges types from inner to outer   |
| `Exec`      | writes nothing                   | visits one statement                 | puts the result into `Result`      |

The code for both orders was already shown in Part 1 (the rendering `writeTo` and the left-spine evaluation loop; `leftSpine` is defined there too); it is not repeated here, only the result is compared:

`writeTo` writes the prefix on the way down and appends the right subtree and closing paren on the way back; `Eval` and `typeCheck` do the opposite — take the leftmost operand's result, then compose layer by layer from the inside out. So rendering visits "parent → left → right", while the other two visit "left → right → parent". That is the whole origin of preorder and postorder: what decides the order is not a traversal parameter but what the method body writes first.

The reason these operations are not merged into one is exactly here: rendering needs the parent first, checking and evaluation need the children first, and the two orders conflict inside a single traversal. Forcing them together would mean maintaining both "the prefix already written" and "the child results not yet computed", which is to hard-code the schedule.

After the split into four methods, each method is responsible only for its own order. Order is therefore local: moving the descent in one method affects only that operation.

## Prefix notation pins rendering to preorder

An S-expression is prefix notation: the parent's operator name must appear before the children's text. That output-format requirement directly forces rendering to write its own opening first and then descend; the parentheses are a closing convention — write `(` on entry, `)` on the way back — carrying no information themselves, only marking the tree shape.

Iterating along the left spine just turns "write yourself first" into "write all prefixes on the way down"; the output is identical, character for character, to the recursive version.

Change the output format and the order changes immediately:

- in prefix notation the parent comes first, so rendering can only be **preorder**;
- infix `1+(2*3)` puts the operator between the children, making the order **inorder** (left → parent → right);
- postfix `1 2 3 * +` gives **postorder** (left → right → parent).

So the rendering order is not a designer's preference; it is forced by the output format: order follows the output.

Prefix notation has a side effect: an S-expression does not need to know precedence, because the parentheses already encode the tree shape. Rendering's job is only to write the shape out; it is not responsible for reproducing the source's "multiply before add" convention — that convention was digested during parsing.

## Child results decide the parent, so postorder is not optional

Type checking and evaluation must both be postorder, because a parent's conclusion rests on its children: without the operands' types, `+` cannot be judged legal; without computing the operands, multiplication cannot happen.

The order also leaves observable traces. Two inputs:

```text
1/0 + 2%0  →  求值失败：第 1 行，第 2 列: 除数为零
1/0 + "a"  →  类型检查失败：第 1 行，第 5 列: + 两侧类型不同：int 和 string
```

In the first line, the right `2%0` would also divide by zero, but the left operand is evaluated first and fails first, so the right side never runs. The second is more extreme: type checking runs over the whole program before evaluation, so evaluation never starts at all. Which error appears first, and at which layer, is decided by the same schedule — not by chance.

There is an even stronger observable: `&&` / `||` short-circuiting. When the left side settles the result, the right subtree is never evaluated — `false && (1/0 == 1)` yields `false` directly, and that `1/0` never runs. Order decides not only "who fails first" but "who never happens".

But short-circuiting happens only in **evaluation**: type checking still visits the right operand (it needs the right side's type), so `false && 1/0` reports at pre-check time that "operator AND needs bool, got int". That is another piece of evidence that checking and evaluation are two passes.

This gives a conclusion: on the type-checking and evaluation chain, type checking is the first full pass over the tree and evaluation is the second. Both can fail, only at different times; that decides the layering of error handling: the layer name belongs to the host, the position to the node (Part 2).

Order also decides "which error is seen". If `Eval` were changed to evaluate the right side before the left, `1/0 + 2%0` would report the right-hand division by zero; when both sides fail, only the one evaluated first surfaces. That observability is the direct cost of writing the schedule into method order, and the reason it deserves its own section.

## Who bounds depth: a gate on the parser, left-spine iteration on evaluation

Beyond order, where recursion lives is the other face of the same question. The parser handles same-precedence operators with a loop: the precedence table `binOps` is data, and a same-precedence left-associative chain stacks to the left inside the loop. Unbounded recursion exists in only three places — parentheses, unary minus, and unary `!`; right recursion for binary operators also recurses, but the number of precedence levels caps it, and it passes through `enter` / `leave` as well.

```go
// same-precedence operators stay in the loop and stack to the left; they cannot enter right recursion
info := binOps[p.cur.Type]
if !info.isBinary() {
	return left, nil // not a binary operator; this layer ends here
}
if info.prec < minPrec {
	return left, nil // looser than this layer's threshold; leave it to the outer layer
}
p.advance()
if err := p.enter(opTok); err != nil { return nil, err } // right recursion also passes the gate
right, err := p.parseExpr(info.prec + 1)                 // threshold +1; same precedence cannot grab into right recursion
p.leave()
```

So a flat long chain such as `1+1+1` does not deepen the stack while parsing: same-precedence operators cannot enter right recursion, so they return to this layer's loop.

Evaluation used to be a different story: `Eval` evaluated left then right, so a left-deep chain was n real levels of recursion. Now `Eval`, `typeCheck`, and `writeTo` all iterate along the left spine; the chain length affects only a heap slice, not the Go stack.

The phrase "only right subtrees remain" needs completing: `Unary` is not on the left spine and still recurses (Part 1 noted this).

Both kinds of recursion depth come from the parse-time structure: right recursion ≤ 5 (`binOps` has 5 levels: `||`, `&&`, comparisons, add/sub, mul/div/mod), parentheses and unary ≤ `maxDepth`. So the parser's gate covers the evaluation side too.

Where should the gate go, then? First distinguish two kinds of "deep": parentheses make the AST deeper and also make parse recursion deeper; a flat long chain makes only the AST deeper, not parse recursion. That distinction decides where the gate goes:

```text
1+1+1+…+1 (flat long chain)        ((((1)))) (parentheses)
AST depth: n                       AST depth: n
parse recursion depth: constant    parse recursion depth: n
```

The long chain's AST is just as deep, but its parse recursion depth is constant — the gate presses on the latter, not the former.

The criterion is one sentence: put the limit on recursion, not on input that merely looks long. So the gate is installed only on the three unbounded paths (parentheses, unary minus, unary `!`):

```go
const maxDepth = 1000

func (p *Parser) enter(tok Token) error {
	if p.depth >= maxDepth {
		return errAt(tok, "表达式嵌套过深（上限 %d 层）", maxDepth)
	}
	p.depth++
	return nil
}

func (p *Parser) leave() { p.depth-- }
```

Check the limit before incrementing, so a failure leaves the count unchanged. `leave` pairs in two styles: the parenthesis and unary paths use `defer p.leave()`.

The right recursion in `parseExpr` sits inside a loop, and `defer` would accumulate across iterations, so it uses explicit pairing — `p.leave()` immediately follows `p.parseExpr(...)`, which the normal error paths also reach; only a panic skips it.

The boundary is pinned at "exactly 1000 levels pass, 1001 reports with a position", and each of the three unbounded paths is tested.

There is also a 1MB scanner limit that only protects the REPL line buffer: the default 64KB would report long lines as "token too long", so the code uses `scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)`. It is unrelated to depth; the two limits guard different things:

| Limit               | What it guards                      | What sets it               |
| ------------------- | ----------------------------------- | -------------------------- |
| `maxDepth`: 1000    | parse recursion depth               | put the limit on recursion |
| scanner buffer: 1MB | memory used by the REPL line buffer | input size                 |

The evaluation side has no matching gate because it no longer needs one: after the left-spine iteration, unbounded recursion remains only on the parser's three paths; right-subtree depth is bounded by the number of precedence levels (5 levels, see above) and still passes through the same `enter` / `leave` — when a right-associative operator is added later (passing `prec` rather than `prec + 1`), chained right recursion will not bypass it.

The old implementation was different: `Eval` was purely recursive; a 100,000-item chain barely ran by letting the Go stack grow, and a 1,000,000-item chain hit a fatal stack overflow at about 810,000 levels (not recoverable).

Now one extreme-scale regression case runs a 1,000,000-item chain directly — parsing, type checking, `SExpr`, and execution all pass; another pins the 100,000-item level.

Adding an evaluation depth counter is not hard; the hard part is saying clearly where the "depth exceeded" error points: which node in the tree? Part 2's single-point position can be reused, but the anchor for "which layer counts as exceeded" still needs to be defined. The left-spine iteration removes unbounded recursion on the evaluation side, so that cost does not have to be paid today.

Each alternative has a cost. An explicit stack means pushing `Env` and `Value` by hand, turning "evaluation" into a small machine; bytecode adds a compilation layer and moves further from the tree; adding a depth counter on the evaluation side is cheapest, but the error position needs a separate design. This implementation chooses none of the three: a gate on the parser, iteration on the evaluation side to eliminate the recursion.

The four operations each carry a context, the two hosts handle failure differently, and one invariant has no mechanism guarding it. Part 5 ties these three together.
