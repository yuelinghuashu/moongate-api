---
title: 树遍历解释器的 Node 架构（一）：三种读法——都实现为节点方法
description: 求值、检查、渲染三种读法为什么都长在节点方法上；Node / Expr / Stmt 为什么拆成三个接口；每层拒绝了哪些替代、代价落在哪。位置与接口开不开放留给后两篇。
date: 2026-10-27
series: interpreter-node-architecture
order: 1
tags:
  - Go
  - Interpreter
  - Engineering
---

> 系列第 1 篇。

解释器拿到 `1+2*3`，要做三件**读法**上的事：算出它的值、判断它合不合法、把它打印成能读的结构。还有一件不属于读法的事：出错时指到源码里的位置，它横切上述三种读法。三种读法和位置都落在同一棵树上：

```text
        +
       / \
      1   *
         / \
        2   3
```

这还只是一棵普通的树：运算符是分叉，数字是叶子；没有 Node 类型，没有字段，也没有位置。

先摆出两个判断维度。一个 AST 接口真正要回答的只有两个问题：**分派放在哪**（谁来回答"这个节点求值是什么"），**穷尽性由谁保证**（新增一种形态或读法时，谁负责让遗漏暴露）。

两个问题都没有两全的答案，只能选择由谁承担代价。第 3 篇那张对照表，就是把五种主流设计放进这两个坐标里比较。

本实现的前提也先摆在这里：**语法封闭、读法固定（求值、类型检查、渲染，外加语句执行）、只有本包的两个宿主在消费**。第 3 篇的对照表用来检验这个前提站不站得住。

本系列假定你写过、或至少读过树遍历解释器。它谈的是接口设计上的权衡，不是从零教学；词法与语法分析的细节不在讨论范围内。

五篇的分工：

```text
第 1 篇：立接口（三种读法 + Expr / Stmt 拆分）—— 核心
    ├─ 第 2 篇：位置进接口（坐标与错误）
    ├─ 第 3 篇：导出边界（谁能扩展）
    ├─ 第 4 篇：顺序与深度（方法体怎么写、递归压在哪）
    └─ 第 5 篇：接口外的那一圈（宿主、环境镜像、不可变、未来三条缝）
```

## 需求一：子节点要能递归 → 决策：分派放在节点方法上

先只考虑数字：叶子要能求值。给它一个最小表示，只存一个值，`Eval` 原样返回。这时没有递归，也不需要接口。

加上加法之后，问题变了：加法节点要挂两个子表达式，子表达式可能是数字，也可能是另一个加法。父节点求值必须先拿子节点的值，于是需要一个统一入口，不管子节点是哪种形态都能调用"求值"。

子节点的具体类型在编译期未知，父节点只认识"能求值"，接口就是为这件事出现的。

另一个选择是把所有节点塞进 `any`，用类型开关分派。每加一种节点，就要在每一处开关补一个分支；漏掉一个，编译器不会说话，程序运行到那里才报错。把方法放进接口，等于让每种形态自己回答求值问题；有人漏实现这个方法，编译就过不去。

```go
type ValueKind int

const (
	KindInvalid ValueKind = iota // 零值 = 没有值，不是 0
	KindInt
	KindString
	KindBool
)

type Value struct {          // 运行期的值：int、string 或 bool
	kind ValueKind
	i    int64
	s    string
	b    bool
}

type Env struct {
	vars map[string]Value // 变量绑定
}

type Node interface { // 所有节点共有的部分：位置 + 渲染
	Pos() Position
	writeTo(b *strings.Builder)
}

type Expr interface { // 有值的表达式：能求值、能类型检查
	Node
	Eval(env *Env) (Value, error)
	typeCheck(env *TypeEnv) (Type, error)
}

type NumberLiteral struct {
	Value int64
	At    Position // 定义见第 2 篇
}

type Binary struct {
	Op          Op
	Left, Right Expr
	At          Position
}
```

`Value` 是带判别字段的 struct，不是 `interface{}`，也不是泛型。值有三种（int、string、bool），读的时候先看 `kind` 再取值，全是普通方法调用，不必做类型断言；零值 `Value{}` 的 `kind` 是 `KindInvalid`，所以"返回零值 + error"不会被误读成一个合法的 0。代价是字段小写、构造只能走 `IntValue` / `StringValue` / `BoolValue`：读法看得到值，改不了表示。

这几个定义各管一件事：

| 定义            | 是什么                                             | 设计上的用处                                                                                                     |
| --------------- | -------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| `Value`         | 运行期的值：`kind` + `i`/`s`/`b`                   | 所有节点共用一个返回类型；`IntValue` / `StringValue` / `BoolValue` 构造，`IsInt` / `AsInt` / `IsBool` 等方法读取 |
| `Env`           | 变量名到 `Value` 的表                              | 求值期按名字取值，`let` 往里写                                                                                   |
| `Op`            | 运算符枚举，取值 `OpPlus` 等                       | `String()` 返回 `PLUS` 这类名字，渲染和报错共用；与 `opNames` 双向断言绑定                                       |
| `NumberLiteral` | 叶子：存 `Value int64` 和 `At`                     | 字段与类型 `Value` 同名，Go 里合法；读代码时 `n.Value` 和类型名 `Value` 长得一样，是这套命名里唯一别扭的地方     |
| `Binary`        | 组合：`Op` + `Left` / `Right`（都是 `Expr`）+ `At` | 父节点只认识"能求值"的 `Expr`，不关心子节点是哪种形态                                                            |
| `Node`          | 位置 + 渲染两个方法                                | 表达式和语句共有的最小契约；位置与渲染跟"有没有值"无关                                                           |
| `Expr`          | `Node` + `Eval` + `typeCheck`                      | 这篇长出来的那个接口：有值、能求值、能类型检查；`Stmt` 的两种形态（`ExprStmt` / `Let`）都不实现它                |

两处命名细节值得一提。`NumberLiteral.Value` 与类型 `Value` 同名，是这套命名里唯一别扭的地方：改成 `Int` 或 `Val` 都能消歧义，保留它是因为 `n.Value` 一读就是"节点的值"，属于取舍而不是错误。

`Op.String()` 与 `TokenType.String()` 返回的是同一套名字（`PLUS`、`STAR`…），所以 S-表达式里的 `(PLUS 1 2)` 和词法层输出可以直接对照，不必翻译。

表里还有一个成员没出场：`Stmt`（语句，例如 `let`）。它只加 `Exec` 和一种不返回 `Type` 的 `typeCheck`，本篇末尾单独说。

叶子返回自己，`NumberLiteral.Eval` 是一行 `return IntValue(n.Value), nil`。

`Binary` 要复杂一档：它先沿左脊把链摊平，再自内向外逐层应用，所以很长的左结合链不吃 Go 栈。下面是骨架（展开 `OpPlus` 与 `&&`/`||` 的分支，其余运算符分支省略；短路守卫留在入口，因为它决定右子树求不求值）。片段里的 `Position` 与 `errAtPos` 定义在第 2 篇，`TypeEnv` 见下面的需求二，`Result` 见本篇末尾，先看签名就够：

```go
func (b Binary) Eval(env *Env) (Value, error) {
	steps, leftmost := leftSpine(b) // 一路 Left 上的 Binary，摊平成从外到内的 steps
	left, err := leftmost.Eval(env)
	if err != nil { return Value{}, err }
	for i := len(steps) - 1; i >= 0; i-- { // 自内向外应用
		left, err = applyBinary(steps[i], left, env)
		if err != nil { return Value{}, err }
	}
	return left, nil
}

func applyBinary(st binaryStep, left Value, env *Env) (Value, error) {
	if st.op == OpAnd || st.op == OpOr { // 短路：左值能定结果时，右子树不求值
		if !left.IsBool() {
			return Value{}, errAtPos(st.at, KindType, "运算符 %s 需要 bool，得到 %s", st.op, left.TypeName())
		}
		lb := left.AsBool()
		if st.op == OpAnd && !lb { return BoolValue(false), nil }
		if st.op == OpOr && lb { return BoolValue(true), nil }
	}

	right, err := st.right.Eval(env) // 右子树在这里才求值
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
	// 其余运算符分支（比较、取模等）省略
	return Value{}, errAtPos(st.at, KindInternal, "未知运算符 %d", st.op)
}
```

最左操作数先算，然后自内向外逐层合成；任何一层失败就原样返回。`+` 对两个 int 求和、对两个 string 拼接，其余组合报类型不符；`&&` / `||` 在入口先判左值，能定结果时右子树不再求值。

`leftSpine` 本身只有十来行：顺着 `Left` 往下走，遇到不是 `Binary` 的就停，把沿途的运算符、位置和右子树收进 `steps`。

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

举一条链看 steps 的方向与回程：

```text
1 + 2 + 3 → Binary(+, left=Binary(+, 1, 2), right=3)

leftSpine  steps = [{+, right=3}, {+, right=2}]   ← 从外到内
           leftmost = 1
求值       1 →（套内层 +2）→ 3 →（套外层 +3）→ 6
```

所以 `Eval` 的循环要从 `len(steps)-1` 往回走：先把最内层应用到最左操作数上，再一层层往外套。

注意递归并没有完全消失：右子树和 `Unary` 仍然递归——`leftSpine` 只在 `Binary` 左链上迭代，碰到 `Unary` 就把它当最左操作数，交给它自己的 `Eval` 往下走。这些递归的深度由解析器的 `enter` / `leave` 兜底（第 4 篇）。

这里返回 error 而不是 panic，是把失败后的处置交给宿主：文件模式选择退出，REPL 选择打完一行继续。panic 会把这份处置权从宿主手里收回去。

错误现在带位置和类别（`errAtPos` 返回 `error`，实际构造的是 `*Error`；细节见第 2 篇）。`Eval` 里再做一遍类型判断，是为了让人绕过预检直接求值时也不会静默算错。

这就是第一种读法：组合节点先拿子结果再合成。后面两种读法沿用同一条骨架。

> **决策一｜分派放在节点方法上**
>
> - **拒绝**：`any` 加类型开关，漏一个分支编译器不说话；也拒绝 visitor，它把"每种形态都要实现"的义务从节点移到每个读法。
> - **代价**：加一种读法，所属接口下的每种形态都要补一个方法（`Expr` 六种、`Stmt` 两种）——成本摊在节点上，不在读法上。
> - **失效**：读法会持续增长（诊断、格式化、多后端）时，这份代价开始复利，visitor 或函数表更划算（对照见第 3 篇）。

## 需求二：先检查再求值 → 决策：预检是第二遍完整读树

> 用词：这一遍就是**类型检查**，后文"预检"与"类型检查"混用，指的都是它；错误消息里写的是"类型检查失败"。

第一种读法的问题是错误要等到执行时才暴露：`"a" - 1` 能被解析、能开始求值，直到真做减法才报错；如果前面已经有 `let` 语句，绑定此时已经写进环境了。想要先确认整份程序合法、再产生任何副作用，就得在求值前把树完整走一遍，问每个节点的类型。

这一遍骨架和求值一样，差别在结果与上下文：返回 `Type`，查 `TypeEnv`。两者各管一件事：

| 名称      | 是什么                                                    | 用途与注意                                                                            |
| --------- | --------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| `Type`    | `int` / `string` / `bool` 三种；`String()` 返回它们的名字 | 类型检查的返回值；错误路径返回 `TypeCount`（`String()` 得 `?`），调用方必须先看 `err` |
| `TypeEnv` | 变量名到 `Type` 的表                                      | 预检期按名字查类型，`let` 往里写类型                                                  |

零值 `TypeInt` 是合法类型，所以错误路径不能靠它表达：这里另加一个 `TypeCount` 作占位。代价是类型表和断言各多一项，好处是"返回零值"再也不会被误当成合法类型。

`Expr` 于是多了第二个方法（`typeCheck` 先写成小写，原因见第 3 篇）：

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

接口多一行，所有表达式就多一份义务：加一种读法时，编译器会逐一检查包里每个形态有没有实现它。错误这时还没有位置；第 2 篇补上位置，并决定该指向哪个节点。

`+` 在 `applyType` 与 `applyBinary` 里各有一份规则：一份判断它得到什么类型，一份真的算出来。两份必须同步，这是整套实现里最容易漏的一处——加运算符时尤其明显，第 5 篇列过清单。

这一遍读树时没有任何值产生，可以在"程序还什么都没做"时跑完；之后再求值，副作用只发生在合法程序上。两种读法共享同一棵树，也意味着树不能在两次读之间被改动。这条约束到第 5 篇会写成一条不变量。

预检由包级入口 `CheckProgram(stmts []Stmt) error` 发起：它建一张空 `TypeEnv`，按顺序对每条语句调用 `typeCheck`，全部通过才交给执行；REPL 在当前行的临时 `TypeEnv`（连同临时 `Env`，第 5 篇）上做同样的事。

`typeCheck` 本身不导出，暴露给外部的只是这个入口：它负责环境准备和执行顺序，`typeCheck` 只负责单个节点。

> **决策二｜预检做成独立的一遍，而不是边求值边检查**
>
> - **拒绝**：在求值时顺路查类型。那样 `"a" - 1` 要等真的做减法才失败，前面语句的副作用已经发生；REPL 里连"整行从未发生"都做不到。
> - **代价**：同一套类型规则写两遍，`typeCheck` 与 `Eval` 必须手工同步（见上）；环境也从一张 map 变成两张，要手工镜像，第 5 篇专门算这笔债。
> - **失效**：类型规则复杂到需要推导（泛型、重载）时，两份手写实现会失控，得把规则收进表里。

## 需求三：看清树的结构 → 决策：共享 Builder，把"写"当副作用

前两种读法都是给机器看的。写解释器时还需要给自己看：解析器把这行源码拼成了什么，光看源码看不出来。最省事的方式是打印成 S-表达式，`1+2*3` 打印成 `(PLUS 1 (STAR 2 3))`。

渲染是第三种读法，骨架不变，差别在产出：不返回值，把文本写进共享的 `strings.Builder`。组合节点下行时先写下 `(` 和运算符名，到最左操作数后再回程补右子树和右括号；叶子的 `writeTo` 只写字面量。

```go
func (n Binary) writeTo(b *strings.Builder) {
	steps, leftmost := leftSpine(n)
	for _, st := range steps { // 下行：把沿路的前缀一次写下
		b.WriteByte('(')
		b.WriteString(st.op.String())
		b.WriteByte(' ')
	}
	leftmost.writeTo(b)
	for i := len(steps) - 1; i >= 0; i-- { // 回程：补右子树和右括号
		b.WriteByte(' ')
		steps[i].right.writeTo(b)
		b.WriteByte(')')
	}
}
```

为什么共用一个 Builder，而不是每个节点返回自己的字符串、再由父节点拼接？后者会把复制成本按深度叠加：深度 d 的树，总代价从输出长度抬成输出长度乘深度。

共用 Builder 则把这份代价挪到签名上：`writeTo` 只能返回 `void`，把"写"当副作用，渲染因此不再是纯函数。想换回纯函数当然可以，付出复制成本即可。

此时 `Node` 有两个方法（`Pos`、`writeTo`），`Expr` 在它之上再加 `Eval`、`typeCheck`；其中 `typeCheck` 与 `writeTo` 都是小写（`Eval` 导出，原因见第 3 篇）。渲染的包级入口是 `SExpr`：借一个 `strings.Builder`，把节点写进去，取字符串。

`writeTo` 不导出，暴露的只是这个入口。

注意它和求值、检查的调度不同：渲染在进入节点时先写自己的部分再深入；求值与类型检查反过来，先把最左操作数算出来，再自内向外逐层应用。调度差异是第 4 篇的主题。

> **决策三｜共享一个 Builder，把"写"当副作用**
>
> - **拒绝**：每个节点返回自己的字符串、父节点再拼接。签名更纯，但复制成本随深度上涨。
> - **代价**：渲染不纯；顺序被输出格式钉死——S-表达式是前缀记法，所以先写自己再下钻，换成中缀就得倒过来。
> - **失效**：要在渲染里做变换或缓存子树文本时，"只写不返回"不够用，得换成带返回值的形状或回调。

## 表达式与语句：为什么是两个接口

到这一步，三种读法都挂在同一条继承链上：`Node` 管位置和渲染，`Expr` 在其上多出求值与类型检查。

但树里还有一类节点：`let`，以及将来的 `if`、`while`、块。它们没有值——不能 `Eval` 出 `Value`，也没有 `Type` 可检查。硬塞进同一条链，要么让 `Eval` 对语句返回一个假值，要么让语句假装有类型。

于是接口拆成三层：

- `Node`：位置 + 渲染，是所有节点共有的部分；
- `Expr`：`Node` + `Eval` + `typeCheck`，有值的表达式；
- `Stmt`：`Node` + `Exec` + `typeCheck`（只返回 `error`），只执行的语句。

顺带把一个用词固定下来：**执行不算第四种读法**。读法是对表达式的三种读（求值、类型检查、渲染）；执行是语句的操作——语句没有值，也没有类型可判，它只把结果装进 `Result`。后文说"四种操作"，指的就是三种读法加执行。

`Exec` 返回的不是 `Value`，而是 `Result`：

```go
type Result struct {
	Value    Value
	HasValue bool
}
```

`let x = 1` 和 `1+2` 都带值（REPL 和文件模式都要展示），所以 `HasValue` 为真；将来 `while` 没有值，返回 `Result{}` 即可，`Exec` 的签名不用再动。测试里有一个只实现 `Stmt`、执行时不产生值的桩语句，专门钉住"语句可以没有值"这条契约。

宿主也跟着换了一行：文件模式对 `env`、REPL 对临时 `tmpEnv` 逐条 `Exec`（第 5 篇），要展示的值从 `Result.Value` 取，而不是从 `Eval` 的返回值取。

为什么还要 `ExprStmt`：解析器现在产出 `[]Stmt`，顶层裸表达式（`1+2`）必须包成一条语句才能进列表。它的位置与 S-表达式都沿用内部表达式，所以 `SExpr` 的输出与拆分前逐字相同。

拆分还带来一道编译期护栏：`var _ Expr = NumberLiteral{}` 这样逐条钉桩，节点归属写错（比如把语句塞进 `Binary.Left`）就编译不过。

代价是多一个接口、多一层心智负担：`SExpr` 现在同时服务表达式与语句（`let x = 1` 渲染成 `(let x 1)`），`typeCheck` 也要写两个版本——表达式版返回 `Type`，语句版只返回 `error`。这和本篇的赌注一致：每种形态自己回答"我属于哪一组"。

> **决策四｜按"有没有值"拆成 `Expr` 与 `Stmt`**
>
> - **拒绝**：一个接口全包，让 `Eval` 对语句返回一个假值；或者给 `Type` 加一个 `TypeUnit`，让语句假装有类型。
> - **代价**：多一个接口、多一层心智负担；`typeCheck` 要写两个版本；`SExpr` 同时服务表达式与语句。
> - **失效**：加 `if` / `while` / 块时反而受益（`Result{}` 表示无值，`Exec` 签名不动）；但加函数时边界会重新模糊——函数定义是 `Stmt`，函数调用是 `Expr`。

## 小结：三种读法、八种节点

三种读法补齐，实现里一共八种节点：六种表达式（`Expr`）加两种语句（`Stmt`）。三层接口各自带的方法，以及八种节点的归属：

```text
Node { Pos, writeTo }
├── Expr { Eval, typeCheck }        ← 有值
│   ├── NumberLiteral{Value int64, At}
│   ├── StringLiteral{Value string, At}
│   ├── BoolLiteral{Value bool, At}
│   ├── Identifier{Name string, At}
│   ├── Binary{Op Op, Left, Right Expr, At}
│   └── Unary{Op Op, X Expr, At}
└── Stmt { Exec, typeCheck }        ← 无值
    ├── ExprStmt{X Expr}
    └── Let{Name string, Value Expr, At}
```

本篇只展开了数字和二元运算；其余形态的写法一样。三种读法各自要交出什么、哪种会失败，再加语句的执行：

| 需求 | 方法        | 每个节点交什么               | 失败方式                             |
| ---- | ----------- | ---------------------------- | ------------------------------------ |
| 求值 | `Eval`      | `Value`                      | 未定义、除零、类型不符（绕过预检时） |
| 预检 | `typeCheck` | `Type`（语句只返回 `error`） | 未定义、类型不符                     |
| 查看 | `writeTo`   | 追加到共享 Builder           | 不会失败                             |
| 执行 | `Exec`      | `Result`（值 + 是否有值）    | 未定义、除零、类型不符               |

三种读法共用一条骨架，差别只在返回什么、带什么上下文，以及**下钻写在哪一段**；最后这一项决定访问顺序，是第 4 篇的主题。到这里接口还差位置（第 2 篇），而它的开放性要到第 3 篇才能定。

读完这一篇，应该能回答三个问题：

- 三种读法为什么都放在节点方法上，而不是外部 visitor？
- 预检为什么是独立的一遍，而不是边求值边检查？
- 为什么要按"有没有值"把接口拆成 `Expr` 与 `Stmt`？
