---
title: Flutter 流式 UI：AI 回复的打字机体验是怎么实现的
description: 为什么「跳过动画」不等于「中止生成」？从 SSE 到屏幕，拆解流式 UI 的节流、取消语义与可测试性设计。
permalink: 5c50dd33-81e8-4281-a26d-32fe5765fc94
date: 2026-08-13 22:00:00
series: flutter-practice
level: P3
tags:
  - Flutter
  - Dart
  - LLM
  - Performance
  - State Management
---

## 📚 系列导航

本系列共三篇：

1. [**Flutter 桌面端：输入框设计的细节与边界**](./flutter-desktop-input-design) —— Focus 链、键码区分与输入历史持久化
2. [**Flutter 流式 UI：AI 回复的打字机体验是怎么实现的**](./flutter-streaming-typewriter) —— SSE 流式渲染、跳过语义与可测试性设计
3. [**拆超大 Flutter State 类的三种尝试与最终方案**](./refactoring-flutter-state-class) —— Mixin/part/Widget 组合的取舍与决策树

---

> 打字机效果看起来简单：文字一个字一个字蹦出来。但「跳过动画」「不截断」「性能不退化」这些细节，藏着一整套工程决策。
>
> 文中实现基于 Flutter/Dart，但「跳过 ≠ 中止」「缓冲合并」等核心语义决策**跨框架通用**——Web 的 EventSource、原生/RN 的 SSE 客户端都会遇到同样的选择。

## 引子：一个「跳过打字机」按钮的翻车过程

我在一个 AI 叙事应用（用户通过输入命运指令影响 AI 驱动的交互式故事走向）里做了一个「⏩ 跳过打字机」按钮——用户点击后，应该直接看到完整的 AI 回复，而不是等文字慢慢蹦完。

这个按钮在开发日志里经历了三个阶段：

- **第一版**：点了没反应——回调被调用了，用户却什么都没感受到
- **第二版**：点了内容被截断——动画确实没了，但回复也残缺了
- **最终版**：点了一下子显示已有全文，LLM 仍在后台悄悄生成完整内容，生成完毕后一次性补齐

这三个版本背后，是「流式 UI」这个领域里最容易踩的三个坑。这篇文章把它拆开讲清楚。

## 一、从 SSE 到屏幕：流式渲染的管道

### LLM 为什么是「蹦」着出字的

LLM 的回复通过 HTTP 的 SSE（Server-Sent Events）分 chunk 返回。一个典型 chunk 长这样：

```text
data: {"choices":[{"delta":{"content":"梅菲斯特出现在"}}]}

data: {"choices":[{"delta":{"content":"书斋门口。"}}]}

data: [DONE]
```

每个 chunk 到达的间隔取决于模型生成速度——快的时候几十毫秒，慢的时候可能上秒级。这段「逐字出现」的效果，就是用户感知到的打字机动画。

### 为什么不能每 chunk 都更新 UI

如果每收到一个 chunk 就触发一次状态更新，一个几百字的回复可能触发几十上百次 UI 重建，长文本下明显卡顿。所以要做**节流合并**：

```dart
final _buffer = StringBuffer();
Timer? _timer;

void onChunk(String chunk) {
  _buffer.write(chunk);
  // 50ms 窗口内累积，窗口结束统一提交一次
  _timer ??= Timer(const Duration(milliseconds: 50), flush);
}

void flush() {
  _timer?.cancel();
  _timer = null;
  state = state.copyWith(content: _buffer.toString());
}
```

50ms 的节流窗口把「几十次通知」降为「几百毫秒一次通知」。这个数字不是随便定的：人眼能感知的流畅动画通常在 60fps 以上（约 16.6ms/帧），但**打字机「蹦字」的间隔通常在 100-300ms**——50ms 的合并窗口远小于打字机的感知粒度，对使用者视觉零影响，却能有效合并高频的 chunk 帧渲染，显著降低 UI 开销。

### StringBuffer：避免 O(n²) 拼接

每次提交时，如果把完整内容存为 `pending`，再 `state.streamingContent = current + pending`，长回复下就是反复全量拼接——**O(n²) 复杂度**。

正确做法是维护一个 `StringBuffer` 累积器，提交时用 `toString()` 一次性构建：

```dart
final _streamingBuffer = StringBuffer();

String applyAndGet(String pending) {
  _streamingBuffer.write(pending);
  return _streamingBuffer.toString(); // O(总长)，而不是 O(n²)
}
```

## 二、「跳过动画」的正确语义：静默累积 ≠ 中止生成

这是整个流式体验里最微妙、最容易搞反的一个点。

### 第一版误区：只跳过 UI 节流

我最初的实现是这样：

```dart
void revealStreaming() {
  _revealInstant = true;    // 后续 chunk 跳过 50ms 节流
  _flushStreamBuffer();     // 立即提交已缓冲内容
}
```

`_revealInstant = true` 后，后续 chunk 不再经过节流，直接提交到 UI。**但这完全没有解决使用者的问题**——因为打字机效果的真实来源是 **LLM 逐 chunk 返回**，而不是 UI 的 50ms 节流。

LLM 仍然是几秒内慢慢吐完整个回复。使用者看到的效果：点击按钮后，文字依然一个接一个出现。「跳过打字机」变成了「跳过节流」，在感知上约等于什么都没做。

### 第二版误区：触发取消信号

为了让「跳过」真正生效，我加了取消信号：

```dart
void revealStreaming() {
  _revealInstant = true;
  _flushStreamBuffer();
  cancelGeneration(); // 让 LLM 停止生成
}
```

这次确实「立即」了——因为点击后 LLM 被中止，已到达的内容连同 `[DONE]` 一起作为最终回复返回，所有「剩余未生成的内容」也就永远丢失了。

用户反馈：「点跳过之后，内容直接截断了，之后的内容不再出现。」

**问题**：我把「跳过动画」理解成了「跳过生成」。但用户想要的只是「不想看动画」，而不是「不让 AI 把话说完了」。

### 正解：静默累积，让 LLM 把话说完

「跳过动画」的正确语义是：**停止 UI 逐字更新**——打字机光标消失、文字不再一点点蹦——但 **LLM 继续在后台完整生成**，生成完毕后一次性把完整回复写进消息列表。

```dart
void _appendStreamChunk(String chunk) {
  if (_revealInstant) {
    // 跳过打字机：静默忽略后续 chunk，不触发 UI 重建
    // 完整内容由生成完毕后的 ReplySucceeded 一次性写入
  } else {
    _streaming.append(chunk, _applyStreamChunk);
  }
}
```

点击 ⏩ 后：

1. **立即 flush** 当前已到达的内容到消息气泡（使用者立刻看到已有全文）
2. `_revealInstant = true`，后续 chunk **静默忽略**——UI 不再逐字更新
3. LLM 继续后台生成完毕，`ReplySucceeded` 携带**完整 `reply`** 一次性写入消息列表

关键区分就一句话：

> **停止 UI 逐字更新 ≠ 中止 LLM 生成**

### 一个需要正视的体验空窗

静默累积带来一个极端的体验场景：如果模型极慢且回复超长，使用者点击 ⏩ 时 LLM 可能才生成了前 10 个字，而剩余 2000 字还在排队——从点击跳到 `ReplySucceeded` 之间可能有长达 10-20 秒的**完全无 UI 反馈**空窗期。此时仅靠「光标消失」是不够的，使用者可能误以为应用卡死了。

产品层面的解法：在跳过模式下显示一个极淡的「后台生成中…」指示条（弱化视觉打扰，但让使用者知道系统仍在工作）。这是静默累积「稳住即时体验」与「保护使用者耐心」的平衡点——技术决策之下，往往还跟着一个体验决策。

### 一个真实存在的兜底缺陷

静默累积有一个必须正视的风险：**如果 LLM 生成失败（网络超时、服务端 5xx），`ReplySucceeded` 永远不会到来**。此时不能永远卡在「加载中」。

我的做法是依赖生成流程的全局兜底：

```dart
} catch (e, st) {
  debugPrint('生成回复异常: $e\n$st');
  _dispatch(const GenerationFailed(narrativeErrorGenFailed)); // 复位生成状态
} finally {
  endGeneration(); // 释放防重入标志，允许下一条消息
}
```

但这暴露了一个真实的体验缺陷：`GenerationFailed` 会清空 `streamingContent`——**使用者点 ⏩ 时已经看到的部分内容会消失**，只剩下一条错误提示。

这个问题其实可以**立即修复**，不必等到下一版。既然 `_revealInstant = true` 后是「静默忽略」后续 chunk，那么失败时的兜底逻辑应当是：**若已进入跳过模式，失败时先 flush 累积缓冲，将已生成的部分作为一条「不完整回复」归档，再标记失败**：

> 说明：`_flushStreamBuffer()` 会把节流缓冲中已累积的内容提交到界面（即 `streamingContent`）；`_streamingContent` 就是当前已显示的流式文本。失败兜底时先把这部分作为「不完整回复」归档，再置错误。

```dart
} catch (e, st) {
  debugPrint('生成回复异常: $e\n$st');
  if (_revealInstant) {
    // 跳过模式下：先把已累积的内容作为「不完整回复」归档，保住使用者看到的部分。
    // _flushStreamBuffer() 提交缓冲 → _streamingContent 为当前已显示的流式文本
    _flushStreamBuffer();
    final partial = _streamingContent;
    _dispatch(ReplySucceeded(
      reply: partial,
      // 状态/记忆等沿用当前值……
    ));
  }
  _dispatch(const GenerationFailed(narrativeErrorGenFailed));
} finally {
  endGeneration();
}
```

这只改失败分支、不动主框架，就能做到「跳过动画后即使生成失败，已看到的内容也不丢失」。

## 三、数据一致性：流式中断不丢内容

Mephisto 里有两个「打断生成」的入口：

- **⏹ 停止**：使用者明确想中断，保留已生成内容
- **⏩ 跳过**：使用者不想看动画，但希望 LLM 把话说完

它们底层复用同一个「协作式取消」信号——`LlmClient` 收到取消信号后，在下一条 SSE 数据行处停止读取，并返回**已累积的完整内容**：

```dart
// LlmClient 的 SSE 循环内
await for (final line in response.stream...) {
  if (isCancelled()) break; // 协作式：下一条数据行处停止
  // ...解析 delta、写入 fullContent、回调 onChunk
}
return fullContent.toString(); // 返回已累积内容，而非抛异常
```

- ⏹ `stopGenerating()`：触发取消 `+` flush 已显示内容 → 生成流程以「已累积内容」正常收尾
- ⏩ `revealStreaming()`：只置 `_revealInstant`，**不触发取消** → LLM 继续生成，最终完整提交

两者都靠「取消后返回已累积内容」保证**不丢已有数据**；区别在于「是否让 LLM 继续」。

> **术语澄清**：这里的「取消信号」是**客户端主动停止读取 SSE 流**（下一条数据行处 break），等于客户端断开接收——**服务端/模型可能仍在后台继续生成、继续消耗算力**。它不是一个「向服务端发送中止指令」的操作；LLM API 通常不支持 client-side cancellation。理解这点很重要：你点了「停止」只是自己不再接收，服务端的成本并不会因此立刻终止。

## 四、如何测试流式场景

流式 UI 是最难测的场景之一：时序敏感、涉及真实网络 IO、还夹着节流缓冲。好在 Flutter 测试里可以用 `MockClient.streaming` + `StreamController` 精确控制时间点。

### 可控分块流

核心思路：用一个 `StreamController` 手动推送 SSE chunk，在**任意时间点**注入「使用者点击 ⏩」的调用，验证后续行为。

```dart
final controller = StreamController<List<int>>();
final streamingClient = MockClient.streaming((request, bodyStream) async {
  return http.StreamedResponse(
    controller.stream,
    200,
    headers: {'content-type': 'text/event-stream; charset=utf-8'},
  );
});
```

### 测试「跳过动画不截断」的关键三步

```dart
// 1. 第一段内容到达
controller.add(utf8.encode(sseChunk('梅菲斯特出现在书斋门口。')));
await Future<void>.delayed(const Duration(milliseconds: 50));

// 2. 使用者点击 ⏩ 跳过打字机
notifier.revealStreaming();
expect(state.streamingContent, contains('梅菲斯特出现在书斋门口'));

// 3. 剩余内容继续到达（reveal 后不应中止生成）
controller.add(utf8.encode(sseChunk('他轻声提议进行一场交易。')));
controller.add(utf8.encode('data: [DONE]\n\n'));
await controller.close();
// waitForGeneration：轮询检查状态.isGenerating 直至复位，
// 确保 LLM 生成的异步流程（含自动存档）完全收尾后再断言
await waitForGeneration(container);

// 最终回复 = 完整内容（不截断）
expect(state.messages.last.content,
    '梅菲斯特出现在书斋门口。他轻声提议进行一场交易。');
```

这个测试一举验证了三个关键不变量：

1. 点击 ⏩ 后，**已有内容立即显示**
2. reveal 后，后续 chunk **仍然被接收**（未被中止）
3. 生成结束后，消息列表写入的是**完整拼接内容**（不截断）

如果当时先写了这个测试，第二版「取消信号导致截断」的 bug 会在测试里立刻暴露——而不是等用户反馈。

## 结语

流式 UI 的「顺滑」不是靠运气，而是靠一套清晰的语义决策。总结三条：

1. **节流不等于动画**：动画来自 LLM 的逐 chunk 输出，节流只是减少 UI 重建的优化手段
2. **跳过不是中止**：跳过动画的语义是「停止逐字更新」，不是「停止生成」——让 LLM 把话说完，再一次性补齐
3. **中断要可测**：用 `StreamController` 可控模拟分块流，把「中途打断」这个最脆弱的时序场景变成可重复验证的测试

## 术语表

| 术语                                     | 说明                                                                |
| ---------------------------------------- | ------------------------------------------------------------------- |
| SSE（Server-Sent Events）                | HTTP 长连接的服务器推送协议，LLM 逐 token/chunk 返回的载体          |
| 节流（Throttle）                         | 在时间窗口内合并多次到达为一次提交，减少 UI 重建次数                |
| `StringBuffer`                           | Dart 的可变字符串累积器，`toString()` 一次性构建，避免 O(n²) 拼接   |
| `StreamController`                       | Dart 的可控流控制器，测试中手动推送 chunk 模拟服务器时序            |
| 协作式取消（Collaborative Cancellation） | 客户端停止读取 SSE 流的机制：下一条数据行处 break，而非中断底层连接 |
| `MockClient.streaming`                   | `package:http/testing` 提供的流式响应 mock，可配 `StreamedResponse` |

---

项目：[Mephisto](https://github.com/yuelinghuashu/mephisto-gui)
