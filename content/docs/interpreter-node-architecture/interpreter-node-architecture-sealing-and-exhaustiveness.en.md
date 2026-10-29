---
title: "Tree-Walking Interpreter Node Architecture (Part 3): Unexported Methods and Two Extensions — Who Guarantees Exhaustiveness"
description: "Three interfaces, six methods, only three exported; unexported methods keep the shape set extensible only inside the package. Adding a read versus adding a shape: who guarantees exhaustiveness in each case? Finally, five mainstream designs put the choice to the test."
date: 2026-10-27
series: interpreter-node-architecture
order: 3
tags:
  - Go
  - Interpreter
  - Engineering
---

> Part 3 of the series.

Part 1 laid down the three reads and, in the "Expressions and statements" section, split the interface into `Node` / `Expr` / `Stmt`; Part 2 added positions. All six methods are now in place, but one decision is still open: is this interface open or closed? This part answers only that question: after leaving those methods unexported, what does each of the two extension directions have to change; and how do five mainstream designs handle exhaustiveness.

## Unexported methods: keep the three reads inside the package

The interface is complete at this point, but one design question is still undecided: is it open? If `writeTo` and the two `typeCheck` methods were all exported, any type outside the package could implement these interfaces and then slip into the tree: it might return a value the type checker does not understand, report a position somewhere else, or render a shape the in-package reads do not recognize. No one inside the package could verify those promises.

Start with a premise: the current implementation is `package main`, with a dozen files in one package, so **there is no "outside the package"** (the comment in `ast.go` says exactly that: "this package is main; outside code cannot import it anyway").

So these unexported methods are not a compiler-enforced boundary today; they are a discipline to keep before extracting a package. The cost exists today; the benefit arrives only when an external package really imports this one. To verify it today there is only one route: move the three interfaces into their own package and add a compile-time test that "an external type cannot implement them".

So three of the six methods stay unexported: `Node.writeTo`, `Expr.typeCheck`, `Stmt.typeCheck`. One unexported method is enough to close the interface to outside implementations; there are three because the three reads (rendering, pre-checking, statement checking) serve only in-package flows and each is called by one package-level entry.

`CheckProgram` builds a `TypeEnv` and runs statements in order; `SExpr` creates a Builder and takes the string.

For this contract, only `Pos`, `Eval`, and `Exec` are exported, plus the two entries for checking and rendering. The six methods laid out:

| Interface | Methods              | Exported?             |
| --------- | -------------------- | --------------------- |
| `Node`    | `Pos` / `writeTo`    | exported / unexported |
| `Expr`    | `Eval` / `typeCheck` | exported / unexported |
| `Stmt`    | `Exec` / `typeCheck` | exported / unexported |

Without the three unexported methods, the interface cannot be implemented: outside code can read and call, but cannot create a new shape that enters the tree.

Whether a type satisfies an interface depends on the method's full identity: package name plus method name, not spelling. A type outside the package can write a `writeTo` or `typeCheck` with the same spelling, but that is a different method in a different package and does not satisfy the same-named method here. So outside code cannot create a node shape from scratch, nor a statement that enters the tree.

The only exception outside the package is embedding: embed an exported node or node interface and wrap it. Embedding adds no new shape (`writeTo` / `typeCheck` still go to the embedded implementation), but it can override the exported `Eval` / `Exec` / `Pos`, so unexported methods block new shapes but not replacement of those three exported methods.

The catch is that the embedded value must be non-nil; calling through an embedded nil interface value panics.

So unexported methods constrain the shape set and the three in-package reads. What can still be replaced is `Eval`, `Exec`, `Pos`, and field reads; to keep evaluation and execution from being replaced too, they would have to move into package-level entries.

Keeping the methods inside the package buys two things. First, the shape set is defined in-package, so the three reads and statement execution face exactly the language's full set of shapes. Second, the interface can evolve: adding a read (or a new execution mode) touches only a few in-package types, and the compiler checks each one.

The cost is that half of the extensibility is gone. Outside code can read but not extend: fields are readable, `Eval` / `Exec` / `Pos` are callable, the checking and rendering entries are callable, and one can even write a type switch to traverse the tree — but without the lowercase methods, no new read or shape can be added. Exported fields make reading easy; rewrites still land only on copies (the mechanism is in Part 5).

To support lint, formatting plugins, or multiple backends, the reads have to move from the nodes to outside the package and be reorganized with a visitor or a function table. That is a different encoding, and the cost lands elsewhere.

> **Decision 6 | Use unexported methods to let the shape set expand only inside the package**
>
> - **Rejected**: exporting all six methods (the shape set opens to outside code, and no one verifies the in-package reads' assumptions); also rejected: moving `Eval` / `Exec` into package-level entries too (more restrictive, but the host loses per-node evaluation).
> - **Cost**: outside code cannot add reads or shapes; the embedding exception cannot stop overriding `Eval` / `Exec` / `Pos`; the benefit arrives only after extracting a package.
> - **When it breaks**: when integrating with formatting, lint, or multiple backends, the right move is to export those methods and move the reads outside as a visitor. Then the unexported methods turn from protection into an obstacle.

## Two extensions: adding a read versus adding a shape

Node is a closed shape set: one interface plus one struct per shape.

The implementation has eight node kinds — six expressions (`NumberLiteral` / `StringLiteral` / `BoolLiteral` / `Identifier` / `Binary` / `Unary`) and two statements (`ExprStmt` / `Let`); Part 1 listed them.

After the methods move into the package, both extension directions can only be started in-package, but the compiler enforces them to different degrees: adding a read is fully enforced; adding a shape is only half enforced — the type must have the methods to fit into the tree, but the parser's constructor branch is not enforced.

Adding a shape touches seven places: the struct definition, `Pos`, `writeTo`, `Eval` or `Exec`, `typeCheck`, the parser branch that constructs it, and the compile-time pin (`var _ Expr = ...` / `var _ Stmt = ...`).

This counts only places in the implementation; tests are separate (both evaluation and type checking need samples).

Six of the seven are enforced by the compiler: omit any of `Pos` / `writeTo` / `Eval` (or `Exec`) / `typeCheck` and the new type cannot enter the three interfaces; the pins enforce "group membership", so putting a statement in `Binary.Left` fails immediately.

Only the parser's constructor branch is invisible to the compiler: the struct is added, all methods are present, but the parser can never build it, and the program still compiles.

| Change                           | Does the compiler enforce it?                                                       |
| -------------------------------- | ----------------------------------------------------------------------------------- |
| add a read                       | yes: the interface grows, and in-package shapes fail to compile until they catch up |
| add a shape                      | yes: the shape enters the tree only when its interface's methods are complete       |
| put a shape in the wrong group   | yes: the `var _ Expr` / `var _ Stmt` pins fail to compile                           |
| make the parser build it         | no: miss the parser entry and compilation still succeeds                            |
| make the read semantically right | no: the interface guarantees "has a method", not "the method is correct"            |

One more observation: **the cost of adding a read now depends on the interface**. Add a read to `Expr` and six expression shapes change; add an execution mode to `Stmt` and only two statement shapes change (future blocks, `if`, `while` stay in the statement group).

The same "add a read" has a bill that depends on which interface it lands on — that is exactly how the new structure differs from "one `Node` with four methods".

So the design really guarantees "no missed reads", not "no missed shapes": the former is enforced by the compiler, the latter by test cases — the parser can miss its branch and the compiler says nothing.

Adding an operator is a third direction. The precedence table is data, so the parser's dispatch code does not change; but the lexer, the operator constants, and the name table still need entries, type checking and evaluation each need a hand-written branch, and the test samples need one more entry. Adding a shape and adding a read are two different changes; Part 5 expands this.

## Comparison: how five upstream designs guarantee exhaustiveness

Part 1 opened with two questions: where dispatch lives, and who guarantees exhaustiveness. This part answers the second; the first was answered by node methods.

First, make the unit of comparison clear: the table compares **how shapes are represented, where dispatch lives, and who guarantees exhaustiveness** — not "interpreters versus compilers".

The samples deliberately cross categories: two interpreters/VMs (jlox, clox), one AST specification plus transformation toolchain (ESTree / Babel), two compiler front ends (TypeScript, `syn`), plus this implementation. Each answers the same question in its own pipeline, producing this table:

| Design              | Shape representation                                              | Dispatch                             | Exhaustiveness comes from                                     |
| ------------------- | ----------------------------------------------------------------- | ------------------------------------ | ------------------------------------------------------------- |
| Lox (jlox)          | class hierarchy                                                   | visitor double dispatch              | every visitor implements all methods, checked at compile time |
| Lox (clox)          | no AST: bytecode is generated while parsing                       | single pass, VM dispatches on opcode | not applicable: no shape set                                  |
| ESTree / Babel      | plain objects with a `type` string                                | external traverser plus plugins      | no compile-time check; tools verify                           |
| TypeScript          | `kind` discriminated union                                        | central `forEachChild`               | the union narrows; a `never` assertion catches the rest       |
| Rust `syn`          | native enum                                                       | `match`                              | the compiler checks exhaustiveness                            |
| this implementation | three interfaces (`Node`/`Expr`/`Stmt`) plus one struct per shape | node methods                         | interface: a missing method fails to compile                  |

The most interesting contrast is the two extremes. clox keeps no tree at all: it generates bytecode while parsing, has no shape set, and exhaustiveness does not even arise.

But it also has no "tree" to read again — whole-program pre-checking and multiple passes are out of reach. It falsifies the premise rather than offering a design on the same axis.

This implementation goes the other way: every step is spread onto node methods, and the compiler checks each one. The middle rows are a third route — a central function or a single `match` takes over dispatch.

**`syn` versus Go.** Rust's `syn` uses a native enum plus `match`, and one `match` guarantees exhaustiveness centrally. Go has no native sum type, so each shape's reads become methods: adding a shape means implementing the interface's methods, and adding a read means implementing it on every shape. Both extensions are spread across the nodes, and the compiler checks each. Go's own `go/ast` takes external traversal: `Node` has only `Pos` / `End`, traversal goes through `ast.Walk` / `ast.Inspect`, and there is no compile-time exhaustiveness; by putting reads back on the nodes, this implementation makes a different choice in the same ecosystem.

**Selection criteria.** Three:

- few and fixed reads: node methods plus a closed interface have the least boilerplate;
- reads expected to keep growing (diagnostics, formatting, multiple backends): a visitor or function table is cheaper;
- syntax shapes expected to grow often: a central switch or a native enum is easier.

Part 1's premise (fixed reads, closed syntax, only two in-package hosts) still holds here. When the requirement turns into formatting, lint, or an IDE, the right move is a CST or a visitor layer, not stuffing every read back into the nodes.

Lox's visitor version needs a script to generate AST classes and the visitor interface, which itself shows the boilerplate cost is real. This implementation replaces the generator with a method on each node; the cost is that the "add a read" cost is spread over every node.

Unexported methods decide only "who can extend"; they do not guarantee the order of reads, the context, or failure handling. The next part returns to the four method bodies: where preorder and postorder come from, and which side recursion lives on.

References:

- Crafting Interpreters, [Representing Code](https://craftinginterpreters.com/representing-code.html) and [Compiling Expressions](https://craftinginterpreters.com/compiling-expressions.html)
- [The ESTree specification](https://github.com/estree/estree)
- Babel, [Plugin Handbook](https://github.com/jamiebuilds/babel-handbook/blob/master/translations/en/plugin-handbook.md)
- TypeScript, [AST children / forEachChild](https://github.com/basarat/typescript-book/blob/master/docs/compiler/ast-tip-children.md)
- Rust, [syn documentation](https://docs.rs/syn/latest/syn/)
- Roslyn, [Syntax transformation](https://learn.microsoft.com/en-us/dotnet/csharp/roslyn-sdk/syntax-transformation)
- rust-analyzer, [rowan](https://github.com/rust-lang/rowan)
