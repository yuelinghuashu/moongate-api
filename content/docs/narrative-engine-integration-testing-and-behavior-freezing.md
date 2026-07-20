---
title: 叙事引擎与大模型：集成测试与行为冻结
description: 解析器写完了，但怎么保证以后改代码不会改坏它？Golden File 测试、验证器、滑窗老化测试——用测试将解析行为彻底冻结。
permalink: f1937ff0-8254-47a9-8b24-61418346fbea
date: 2026-07-20 21:00:00
series: narrative-engine
level: P4
tags:
  - Go
  - Engineering
  - CI/CD
---

> **前置阅读**：建议先读第二篇的“四、Parser”和第三篇的“四、小结”，理解解析器输出 `domain.Contract` 的完整流程。本篇假设你已经知道解析器能把 `.meph` 变成结构体。

解析器写完了。它能处理区块、规则、插值。但有一个问题：**我怎么保证以后改代码的时候，不会把它改坏？**

解析器的输入是文本，输出是结构体。要保证它稳定，我需要一种方式——**每次改动后，自动对比解析结果是否和预期一致。**

这就是集成测试的作用：把一组固定的 `.meph` 契约作为“看门狗”，每次代码变更后跑一遍，确保行为没有被意外改变。

## 一、Golden File 测试：把解析结果固化下来

最直接的测试方式：准备一个标准契约文件，解析它，然后把解析结果序列化为 JSON 保存起来。以后每次跑测试，都把当前的解析结果和这个 JSON 文件做对比。

项目里的 `testdata/sample.meph` 就是这份标准契约。测试流程如下：

```go
func TestParseSample(t *testing.T) {
    got, err := ParseFile("testdata/sample.meph")
    if err != nil {
        t.Fatalf("解析失败: %v", err)
    }

    goldenPath := "testdata/sample.golden"
    var want domain.Contract

    if err := loadGolden(goldenPath, &want); err != nil {
        // Golden 文件不存在，自动生成
        saveGolden(goldenPath, got)
        t.Log("Golden 文件已生成，请检查后重新运行测试")
        t.FailNow()
    }

    // 对比 got 和 want
    if diff := cmp.Diff(want, got); diff != "" {
        t.Errorf("解析结果与预期不符:\n%s", diff)
        t.Log("💡 如果更改是预期的，请运行: go test -update")
    }
}
```

首次运行会自动生成 `sample.golden`。之后每次运行都会对比，发现差异就报错。如果改动是预期的（比如新增了一个字段），运行 `go test -update` 即可刷新 Golden 文件。

**这个机制的核心价值是：** 让解析器的行为被“冻结”下来。任何改动都必须经过测试验证，不能偷偷改变解析结果。

## 二、双重保险：解析 + 验证

解析只是“把文本读进来”，不保证“内容是对的”。比如角色名为空、状态值类型错误、规则条件为空——这些在语法上都是合法的（能通过解析），但逻辑上是无效的。

所以引擎在解析之后，还会跑一遍**验证器**：

```go
func Validate(contract *domain.Contract) Result {
    var errors []ValidationError
    errors = append(errors, validateRoleName(contract)...)
    errors = append(errors, validateStateTypes(contract)...)
    errors = append(errors, validateRules(contract)...)
    return Result{Errors: errors}
}
```

验证项包括：

- **角色名不能为空**：没有角色名，引擎不知道“我是谁”
- **状态值类型合法**：`shared.ParseValue` 能正确识别 bool/int/float/string
- **规则完整性**：规则名、条件、动作都不能为空

验证器不负责业务逻辑（比如“堕落指数必须在 0-100 之间”）。那是引擎运行时的职责。

**这是双重保险**：解析器保证“语法正确”，验证器保证“结构完整”。两层都过了，引擎才能开始运行。

## 三、滑窗老化测试：历史记录的正确截断

引擎有一个关键行为：历史记录不能无限增长。它需要自动截断，只保留最近 N 轮对话。

测试用例验证这个行为：

```go
func TestIntegrationHistoryLimit(t *testing.T) {
    contract := loadContract("testdata/test_contract.meph")
    // 设置最大历史保留 2 轮
    eng := engine.New(contract, engine.WithMaxHistory(2))

    // 执行 5 轮对话
    for i := 0; i < 5; i++ {
        eng.Run("你好", nil)
    }

    history := eng.History()
    // 5 轮对话产生 10 条记录，但容量只有 4 条（2 轮 * 2 条/轮）
    if len(history) != 4 {
        t.Errorf("历史记录长度 = %d, want 4", len(history))
    }
}
```

这个测试确保历史截断是“整轮丢弃”而不是“逐条丢弃”。如果逐条丢弃，可能出现“只剩下命运的输入、没有角色的响应”这种半轮数据，会导致 Prompt 混乱。

**整轮丢弃**的策略保证了历史的完整性——要么保留一整轮（fate + assistant），要么全丢。

## 四、错误场景测试：确保报错信息精确

除了“正常路径”，集成测试还覆盖“错误路径”——确保各类格式错误能正确报错，并且报错信息包含行号和区块名：

```go
func TestParseErrors(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        wantErr string // 错误信息应包含的子串
    }{
        {
            name:    "区块外有内容",
            input:   "这是区块外的内容\n【角色名】\n贝利亚",
            wantErr: "内容出现在任何区块之外",
        },
        {
            name:    "列表项缺少 - 前缀",
            input:   "【锚点】\n核心信念：力量",
            wantErr: "列表项必须以 '-' 开头",
        },
        {
            name:    "列表项缺少冒号",
            input:   "【锚点】\n- 核心信念 \"力量\"",
            wantErr: "缺少 ':' 或 '：'",
        },
        // ...
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            _, err := ParseString(tt.input)
            if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
                t.Errorf("期望错误包含 '%s'，实际: %v", tt.wantErr, err)
            }
        })
    }
}
```

这些测试确保报错信息不会退化为“unexpected token at position 42”——那是我们在第一篇就决定要消灭的东西。

## 五、代价

- 维护 Golden 文件需要手动确认（首次生成或更新时要检查内容是否正确）
- 错误场景测试需要覆盖尽可能多的边界情况
- 每次新增区块类型，需要同步更新测试用例

但收益是：**重构时可以放心改代码，只要测试全绿，行为就没变。**

## 六、小结

集成测试是工程的“看门狗”。它把解析器的行为冻结下来，任何改动都必须经过验证。

四件事构成了这套测试体系：

1. **Golden File 测试**：固化标准契约的解析结果
2. **双重保险**：解析 + 验证，保证语法正确且结构完整
3. **滑窗老化测试**：确保历史记录按整轮截断
4. **错误场景测试**：确保报错信息精确到行号

有了这套体系，后续的引擎开发可以放心迭代——不怕改坏东西，测试会告诉你。

下一篇，我们将进入引擎的运行时。要解决的核心问题是：**拿到了 `domain.Contract` 之后，怎么驱动大模型生成符合规则的叙事？**

答案是**三明治 Prompt 结构**——把格式约束放在上下两端，把上下文放在中间，彻底根除括号剧本流。

> 项目地址：[https://github.com/yuelinghuashu/mephisto](https://github.com/yuelinghuashu/mephisto)