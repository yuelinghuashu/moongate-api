---
title: VS Code CompletionProvider 中的 filterText 陷阱
description: VS Code 扩展开发中 CompletionProvider 返回数据但补全列表为空的完整解析，涵盖 filterText 过滤机制、中文输入法下 triggerCharacter 失效问题，以及系统的排查与解决方案。
date: 2026-07-23
series:
tags:
  - VSCode
  - Engineering
---

## 1. 现象

最近在开发一个 VS Code 扩展，为自定义的 `.meph` 文件提供语法支持。其中有一个很自然的需求：用户输入 `【` 时，自动弹出标准区块名的补全列表。

注册方式按官方文档来：

```typescript
vscode.languages.registerCompletionItemProvider(
  { language: "mephisto" },
  new MephistoCompletionProvider(),
  "【", // trigger character
  ".",
  "[",
)
```

预期行为是：输入 `【` → 弹出补全 → 选择"角色名" → 自动变成 `【角色名】`。

实际表现：输入 `【` 后，auto-closing 自动补了 `】`，行变成 `【】`，但**补全列表什么都没有**。

## 2. 第一次排查：triggerCharacter 的问题

起初怀疑是 triggerCharacter 没生效。在 `provideCompletionItems` 开头加了一行测试代码：

```typescript
provideCompletionItems() {
    return [new vscode.CompletionItem('测试-如果能看到我', CompletionItemKind.Text)];
    // ... 原有逻辑
}
```

重新加载扩展后，按 `Ctrl+Space` 手动触发补全，"测试-如果能看到我" **正常显示**。

这说明 completion provider 本身注册成功了，也能正常返回数据。问题出在**触发环节**。

进一步测试发现，在中文输入法下 `'【'` 作为 trigger character 并不可靠。不是 `【` 本身不能作为 trigger——在英文输入法下直接输入 `【` 是可以触发补全的。问题在于中文输入法（如拼音）中，`【` 通常通过候选窗口选择输入，这个输入路径绕过了 VS Code 的 trigger character 检测机制。

改用 `onDidChangeTextDocument` 事件配合 `triggerSuggest` 命令，不依赖 trigger character，而是监听文档内容变化后强制弹出补全：

```typescript
vscode.workspace.onDidChangeTextDocument((e) => {
  if (e.document.languageId === "mephisto") {
    for (const change of e.contentChanges) {
      if (change.text === "【") {
        vscode.commands.executeCommand("editor.action.triggerSuggest")
        break
      }
    }
  }
})
```

注意这里**不需要** `setTimeout(0)`。根据 VS Code 的事件模型，`onDidChangeTextDocument` 在文档内容修改完成后才触发，此时 auto-closing 已经同步插入了 `】`，当前行已经是 `【】` 了。不过在极慢的扩展宿主环境下，`setTimeout(0)` 可以作为保险措施，但通常不需要。

## 3. 第二次排查：补全数据去哪了

用 `onDidChangeTextDocument` 替换 triggerCharacter 后，provider 确实被调用了，也返回了 9 个标准区块名。但**补全列表依然是空的**。

没有报错，没有警告，只有空荡荡的补全弹窗。

这就奇怪了：provider 返回了数据，VS Code 没有报错，为什么用户看不到？

## 4. 真正的原因：filterText

问题出在 VS Code 的补全过滤机制上。

当用户输入 `【` 后，auto-closing 立即插入 `】`，当前行变成了：

```text
【】
```

光标在索引 1 的位置（`【` 和 `】` 之间）。此时 `triggerSuggest` 弹出补全，VS Code **自动提取光标位置的"当前词"**作为过滤前缀。在 `【】` 中间，当前词就是 `【`。

然后 VS Code 拿 `【` 去匹配 provider 返回的每个补全项：

| 补全项 label | 默认用于匹配的文本（filterText = label） | 匹配 `【`？ |
| ------------ | ---------------------------------------- | ----------- |
| `角色名`     | `角色名`                                 | ❌          |
| `锚点`       | `锚点`                                   | ❌          |
| `世界观`     | `世界观`                                 | ❌          |
| ...          | ...                                      | ❌          |

全部不匹配，所以全部被过滤掉。

这就是 `filterText` 的作用：它告诉 VS Code **用什么文本来做匹配**，而不是用 label 作为匹配依据。

修复方式是在 `getBlockCompletions` 中设置 `filterText`：

```typescript
private getBlockCompletions(): CompletionItem[] {
    const items: CompletionItem[] = [];
    for (const name of STANDARD_BLOCKS) {
        const item = new CompletionItem(name, CompletionItemKind.Module);
        item.insertText = name + '】\n';
        item.detail = '标准区块';
        item.filterText = '【' + name;  // ← 关键
        items.push(item);
    }
    return items;
}
```

- `label`（显示文本）仍然是"角色名"
- `insertText`（插入内容）仍然是 `角色名】\n`
- 但 `filterText` 设为 `'【角色名'`，VS Code 拿到当前词 `【` 去匹配 `'【角色名'` → **匹配成功** → 显示在列表中

## 5. CompletionItem 的文本相关属性

`CompletionItem` 有三个与文本相关的属性：

| 属性         | 作用                         | 在这个场景中的值 |
| ------------ | ---------------------------- | ---------------- |
| `label`      | 用户看到的补全项文本         | `角色名`         |
| `filterText` | VS Code 用于做模糊匹配的文本 | `【角色名`       |
| `insertText` | 用户选择后实际插入的文本     | `角色名】\n`     |

默认情况下，`filterText` 等于 `label`。当补全项 label 与触发字符（当前词）不匹配时，需要显式设置 `filterText` 来提供正确的匹配文本。

值得注意的是，VS Code 的过滤是 **fuzzy matching（模糊匹配）**，不是严格的前缀匹配。`filterText` 的值会参与评分算法，当前词不需要严格作为 `filterText` 的前缀也能匹配。但在这个场景中，设 `filterText` 为 `'【角色名'` 已经足够让 `【` 匹配上了。

此外，`sortText` 控制补全列表的排序顺序：

```typescript
item.sortText = "0" + name // 标准区块排在前面
item.sortText = "1" + name // 其他项排在后面
```

`CompletionItemKind` 则影响显示的图标：

```typescript
item.kind = CompletionItemKind.Module // 方块图标
item.kind = CompletionItemKind.Class // 菱形图标
item.kind = CompletionItemKind.Struct // 结构体图标
```

挑一个视觉上顺眼的即可。

## 6. 最终代码

将上述改动整合在一起，最终实现如下：

```typescript
// extension.ts — 监听中文输入法下的 【 输入
vscode.workspace.onDidChangeTextDocument(e => {
    if (e.document.languageId !== 'mephisto') return;
    for (const change of e.contentChanges) {
        if (change.text === '【') {
            vscode.commands.executeCommand('editor.action.triggerSuggest');
            break;
        }
    }
});

// completion.ts — CompletionProvider 中设置 filterText 和 range
private getBlockCompletions(): CompletionItem[] {
    return STANDARD_BLOCKS.map(name => {
        const item = new CompletionItem(name, CompletionItemKind.Module);
        item.insertText = name + '】\n';
        item.filterText = '【' + name;
        item.detail = '标准区块';
        return item;
    });
}
```

## 7. 总结

### 核心要点回顾

| 问题                     | 原因                                                | 解决方案                                          |
| ------------------------ | --------------------------------------------------- | ------------------------------------------------- |
| 输入 `【` 不触发补全     | 中文输入法下 triggerCharacter 不可靠                | 改用 `onDidChangeTextDocument` + `triggerSuggest` |
| 补全弹出了但列表为空     | `filterText` 默认与 label 相同，无法匹配当前词 `【` | 设置 `filterText` 为 `'【' + name`                |
| VS Code 提取的当前词不对 | `wordPattern` 不包含 `【`                           | 设置 `range` 手动指定当前词范围                   |

### 一句话总结

`filterText` 解决的是"**拿什么去匹配**"的问题，`range` 解决的是"**当前要匹配的词是哪个**"的问题。两者结合，能应对大多数补全过滤异常的场景。

### 最终的完整解决方案

```typescript
// 1. 监听输入触发补全（不依赖 triggerCharacter）
vscode.workspace.onDidChangeTextDocument(e => {
    if (e.document.languageId !== 'mephisto') return;
    for (const change of e.contentChanges) {
        if (change.text === '【') {
            vscode.commands.executeCommand('editor.action.triggerSuggest');
            break;
        }
    }
});

// 2. 在 CompletionProvider 中设置 filterText 和 range
provideCompletionItems(document: vscode.TextDocument, position: vscode.Position) {
    const line = document.lineAt(position.line).text;
    const openIdx = line.indexOf('【');
    const closeIdx = line.indexOf('】');

    if (openIdx !== -1 && closeIdx !== -1 && position.character > openIdx && position.character <= closeIdx) {
        const items = STANDARD_BLOCKS.map(name => {
            const item = new vscode.CompletionItem(name, vscode.CompletionItemKind.Module);
            item.insertText = name + '】\n';
            item.filterText = '【' + name;
            item.range = new vscode.Range(
                new vscode.Position(position.line, openIdx),
                new vscode.Position(position.line, openIdx + 1)
            );
            item.detail = '标准区块';
            return item;
        });
        return items;
    }
    // 其他补全逻辑...
}
```

### 排查清单

当你在 VS Code 扩展中发现补全不显示时：

1. 补全列表完全没出现 → 检查 triggerCharacter / triggerSuggest 是否生效
2. 补全列表弹出了但什么都没有 → 检查 provider 是否返回了数据（加一个测试项验证）
3. 确认返回了数据但用户看不到 → **检查 filterText 是否匹配当前词**
4. 以上都无效 → 检查 wordPattern 或设置 range

其中第 3 步是最容易忽略的，因为 VS Code 不会告诉你它过滤掉了什么。
