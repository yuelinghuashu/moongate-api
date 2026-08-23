---
title: 'Flutter Streaming UI: How the Typewriter Experience of AI Replies Is Built'
description: The typewriter effect looks simple — characters appear one by one. But behind "skip without truncation" and "no data loss" lies a full set of engineering decisions. From SSE buffering to skip semantics to testing strategies.
date: 2026-08-13 22:00:00
permalink: 5c50dd33-81e8-4281-a26d-32fe5765fc94
level: P3
series: flutter-practice
tags:
  - Flutter
  - Dart
  - LLM
  - Performance
  - State Management
---

> The typewriter effect looks simple: characters appear one by one. But behind "skip animation", "no truncation", and "no performance regression" lies a whole set of engineering decisions.
>
> The implementation in this article is Flutter/Dart based, but the core semantic decisions — "skip ≠ abort" and "buffer and batch" — are **framework-agnostic**: Web's EventSource, and native/RN SSE clients, face the same choices.

## Prologue: a "skip typewriter" button that kept breaking

In an AI narrative app (where the user influences an AI-driven interactive story by entering fate instructions), I built a "⏩ skip typewriter" button — users click it to see the full AI reply immediately instead of waiting for the text to appear character by character.

The button went through three stages in the dev log:

- **V1**: clicking does nothing — the callback fires, but the user experiences no change
- **V2**: clicking truncates the content — the animation is gone, but the reply is incomplete too
- **Final**: clicking reveals the partial text immediately, while the LLM keeps generating the full reply in the background, which appears all at once when done

Behind these three versions lie the three most common pitfalls in "streaming UI". This article breaks them down.

## 1. From SSE to screen: the streaming rendering pipeline

### Why the LLM "pops" text out

The LLM's reply comes back in chunks via HTTP SSE (Server-Sent Events). A typical chunk looks like this:

```json
data: {"choices":[{"delta":{"content":"Mephistopheles appears"}}]}

data: {"choices":[{"delta":{"content":"at the study door."}}]}

data: [DONE]
```

The interval between chunks is determined by the model's generation speed — tens of milliseconds when fast, possibly a full second when slow. That "character-by-character appearance" is what the user perceives as the typewriter animation.

### Why you can't update the UI on every chunk

If you trigger a state update on every chunk, a reply of a few hundred characters can cause dozens or hundreds of UI rebuilds, which visibly lags on longer texts. So you need **batching** — reading the chunks into a buffer and committing them together:

```dart
final _buffer = StringBuffer();
Timer? _timer;

void onChunk(String chunk) {
  _buffer.write(chunk);
  // Batch chunks within a 50ms window; flush once when the window closes
  _timer ??= Timer(const Duration(milliseconds: 50), flush);
}

void flush() {
  _timer?.cancel();
  _timer = null;
  state = state.copyWith(content: _buffer.toString());
}
```

A 50ms batching window reduces "dozens of notifications" to "one notification per few hundred milliseconds". This number is not arbitrary: the human eye perceives smooth animation at 60fps and above (~16.6ms/frame), but the **typewriter's "popping" interval is usually 100-300ms** — a 50ms merging window is far smaller than the typewriter's perceptual granularity, has zero visible impact, yet effectively batches high-frequency chunk frames and significantly reduces UI overhead.

### StringBuffer: avoiding O(n²) concatenation

If you store the complete content every time and then do `state.streamingContent = current + pending`, longer replies cause repeated full concatenation — **O(n²) complexity**.

The right approach is to maintain a `StringBuffer` accumulator and build the result once via `toString()`:

```dart
final _streamingBuffer = StringBuffer();

String applyAndGet(String pending) {
  _streamingBuffer.write(pending);
  return _streamingBuffer.toString(); // O(total length), not O(n²)
}
```

## 2. The correct semantics of "skip animation": silent accumulation ≠ aborting generation

This is the most subtle point in the whole streaming experience, and the easiest to get backwards.

### V1 mistake: only skipping the UI batching window

My initial implementation was:

```dart
void revealStreaming() {
  _revealInstant = true;    // subsequent chunks skip the 50ms batching window
  _flushStreamBuffer();     // immediately commit buffered content
}
```

After `_revealInstant = true`, subsequent chunks no longer go through the batching window and are committed straight to the UI. **But this doesn't solve the user's problem at all** — because the real source of the typewriter effect is the **LLM returning chunks one by one**, not the UI's 50ms batching window.

The LLM still spends seconds emitting the whole reply. What the user sees: after clicking, text still appears one character at a time. "Skip typewriter" became "skip batching", which is equivalent to doing nothing from the user's perspective.

### V2 mistake: triggering the cancel signal

To make "skip" actually effective, I added a cancel signal:

```dart
void revealStreaming() {
  _revealInstant = true;
  _flushStreamBuffer();
  cancelGeneration(); // make the LLM stop generating
}
```

This time it was indeed "immediate" — because the LLM was aborted, the accumulated content was returned as the final reply together with `[DONE]`, and all "content not yet generated" was lost forever.

The user's report: "after clicking skip, the content is truncated, and the rest never shows up."

**The problem**: I interpreted "skip the animation" as "skip the generation". But the user only wanted "not to watch the animation", not "to stop the AI from finishing its response".

### The right answer: silent accumulation, let the LLM finish

The correct semantics of "skip animation" is: **stop the UI from updating character by character** — the cursor disappears and text no longer pops in one at a time — but **the LLM continues generating fully in the background**, and when done, the complete reply is written into the message list all at once.

```dart
void _appendStreamChunk(String chunk) {
  if (_revealInstant) {
    // Skip typewriter: silently ignore subsequent chunks, no UI rebuilds
    // The complete content is written by ReplySucceeded once finished
  } else {
    _streaming.append(chunk, _applyStreamChunk);
  }
}
```

After clicking ⏩:

1. **Immediately flush** the content received so far into the message bubble (the user instantly sees the existing partial text)
2. `_revealInstant = true`, subsequent chunks are **silently ignored** — the UI no longer updates character by character
3. The LLM finishes generating in the background; `ReplySucceeded` carries the **complete `reply`** and writes it into the message list all at once

The key distinction, in one sentence:

> **Stopping character-by-character UI updates ≠ aborting LLM generation**

### A UX gap worth acknowledging

Silent accumulation brings an extreme scenario: if the model is very slow and the reply is very long, by the time the user clicks ⏩ the LLM may have only generated the first ten characters, with the remaining 2000 queued up — there can be a **10-20 second window with no UI feedback at all** between clicking skip and `ReplySucceeded`. At that point, "cursor gone" is not enough; the user may think the app has frozen.

A product-level solution: in skip mode, show a very subtle "generating in background…" indicator (low visual distraction, but lets the user know the system is still working). This is the balance between "stabilizing immediate experience" and "protecting user patience" — behind every technical decision, there is often an experience decision as well.

### A real fallback flaw

Silent accumulation has a risk that must be acknowledged: **if the LLM generation fails (network timeout, server 5xx), `ReplySucceeded` never arrives**. At that point you can't be stuck on "loading" forever.

My approach relies on the global fallback of the generation flow:

```dart
} catch (e, st) {
  debugPrint('generation failed: $e\n$st');
  _dispatch(const GenerationFailed(narrativeErrorGenFailed)); // reset generation state
} finally {
  endGeneration(); // release re-entry guard, allow next message
}
```

But this exposes a real UX flaw: `GenerationFailed` clears `streamingContent` — **the partial content the user saw after clicking ⏩ disappears**, leaving only an error message.

This can actually be **fixed immediately**, not deferred to the next version. Since `_revealInstant = true` means "silently ignore" subsequent chunks, the fallback on failure should be: **if already in skip mode, flush the accumulated buffer first and archive the generated part as an "incomplete reply" before marking the failure**:

> Note: `_flushStreamBuffer()` commits the buffered content to the UI (i.e., `streamingContent`); `_streamingContent` is the streaming text currently displayed. In the failure fallback, archive this part as an "incomplete reply" first, then set the error.

```dart
} catch (e, st) {
  debugPrint('generation failed: $e\n$st');
  if (_revealInstant) {
    // Skip mode: archive the accumulated content as an "incomplete reply"
    // to preserve what the user has seen.
    // _flushStreamBuffer() commits the buffer → _streamingContent is the displayed text
    _flushStreamBuffer();
    final partial = _streamingContent;
    _dispatch(ReplySucceeded(
      reply: partial,
      // keep current state/memories...
    ));
  }
  _dispatch(const GenerationFailed(narrativeErrorGenFailed));
} finally {
  endGeneration();
}
```

This only touches the failure branch, not the main framework, and achieves "even if generation fails after skip, the content the user already saw is not lost".

## 3. Data consistency: interrupted streaming never loses content

Mephisto has two "interrupt generation" entry points:

- **⏹ Stop**: the user explicitly wants to interrupt, keeping the generated content
- **⏩ Skip**: the user doesn't want to watch the animation, but wants the LLM to finish

They share the same "cooperative cancellation" signal underneath — when `LlmClient` receives the cancel signal, it stops reading at the next SSE data line and returns **the complete accumulated content**:

```dart
// Inside LlmClient's SSE loop
await for (final line in response.stream...) {
  if (isCancelled()) break; // cooperative: stop at the next data line
  // ...parse delta, write fullContent, call onChunk
}
return fullContent.toString(); // return accumulated content, not throw
```

- ⏹ `stopGenerating()`: trigger cancel `+` flush displayed content → generation flow wraps up normally with "accumulated content"
- ⏩ `revealStreaming()`: only set `_revealInstant`, **don't trigger cancel** → LLM keeps generating, final full commit

Both guarantee **no loss of existing data** through "return accumulated content after cancel"; the difference is "whether the LLM continues".

> **Terminology clarification**: the "cancel signal" here is the **client actively stopping reading the SSE stream** (breaking at the next data line), i.e., the client disconnects its reception — **the server/model may still be generating in the background and still consuming compute**. It is not an "instruct the server to abort" operation; LLM APIs usually don't support client-side cancellation. This matters: when you hit "stop", you simply stop receiving; the server-side cost doesn't end immediately.

## 4. How to test streaming scenarios

Streaming UI is among the trickiest things to test: timing-sensitive, involves real network IO, and is mixed with buffered batching. Fortunately, in Flutter tests you can use `MockClient.streaming` + `StreamController` to control the timing precisely.

### Controllable chunked stream

The core idea: use a `StreamController` to push SSE chunks manually, injecting the "user clicks ⏩" call at any point in time and verifying subsequent behavior.

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

### The three key steps for testing "skip animation without truncation"

```dart
// 1. First chunk arrives
controller.add(utf8.encode(sseChunk('Mephistopheles appears at the study door.')));
await Future<void>.delayed(const Duration(milliseconds: 50));

// 2. User clicks ⏩ skip typewriter
notifier.revealStreaming();
expect(state.streamingContent, contains('Mephistopheles appears at the study door'));

// 3. More content keeps arriving (reveal must not abort generation)
controller.add(utf8.encode(sseChunk('He quietly proposes a bargain.')));
controller.add(utf8.encode('data: [DONE]\n\n'));
await controller.close();
// waitForGeneration: polls state.isGenerating until it resets,
// ensuring the async generation flow (including auto-save) fully wraps up before asserting
await waitForGeneration(container);

// Final reply = complete content (not truncated)
expect(state.messages.last.content,
    'Mephistopheles appears at the study door. He quietly proposes a bargain.');
```

This test verifies three key invariants at once:

1. After clicking ⏩, **the existing content is displayed immediately**
2. After reveal, subsequent chunks **are still received** (not aborted)
3. When generation ends, the message list contains the **fully concatenated content** (not truncated)

If this test had been written first, the "cancel signal causes truncation" bug in V2 would have surfaced in the test immediately — instead of waiting for a user report.

## Conclusion

The "smoothness" of streaming UI is not luck; it comes from a clear set of semantic decisions. Three takeaways:

1. **Batching is not animation**: the animation comes from the LLM's chunk-by-chunk output; batching is only an optimization to reduce UI rebuilds
2. **Skip is not abort**: the semantics of skipping the animation is "stop character-by-character updates", not "stop generation" — let the LLM finish, then fill in everything at once
3. **Interruptions must be testable**: use `StreamController` to simulate a chunked stream controllably, turning the most fragile timing scenario — "interrupting mid-stream" — into a repeatable test

## Glossary

| Term | Description |
| --- | --- |
| SSE (Server-Sent Events) | HTTP long-connection server push protocol, the carrier for LLM token/chunk streaming |
| Batching (Debounce) | Merging multiple arrivals within a time window into one commit, reducing UI rebuild count |
| `StringBuffer` | Dart's mutable string accumulator; `toString()` builds once, avoiding O(n²) concatenation |
| `StreamController` | Dart's controllable stream controller; in tests, push chunks manually to simulate server timing |
| Cooperative cancellation | The mechanism where the client stops reading the SSE stream: break at the next data line rather than aborting the underlying connection |
| `MockClient.streaming` | The streaming response mock from `package:http/testing`, used with `StreamedResponse` |

---

Project: [Mephisto](https://github.com/yuelinghuashu/mephisto-gui) (MIT License)