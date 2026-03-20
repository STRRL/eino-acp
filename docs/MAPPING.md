# ACP → Eino Mapping Reference

This document describes how ACP (Agent Client Protocol) concepts map to Eino's model interfaces, where conversion/aggregation is needed, and known gaps.

## Protocol Flow Mapping

```
ACP (streaming-only)                    Eino (Generate or Stream)
────────────────────                    ────────────────────────
Initialize + NewSession                 (setup, no eino equivalent)
    ↓
Prompt(ContentBlock[])        →         ChatModel.Generate() or Stream()
    ↓
SessionUpdate notifications   →         Generate: accumulate internally
  AgentMessageChunk                     Stream:   push chunk to StreamReader
  ToolCall / ToolCallUpdate             Both:     fire tool callbacks in real-time
  AgentThoughtChunk                     (dropped)
  Plan                                  (dropped)
    ↓
PromptResponse(StopReason)    →         Generate: return aggregated *schema.Message
                                        Stream:   close StreamReader
                                        (StopReason itself is currently discarded)
```

## Data Type Mapping

### Messages

| ACP | Direction | Eino | Conversion |
|-----|-----------|------|------------|
| `PromptRequest.Prompt []ContentBlock` | Input | `[]*schema.Message` | **Lossy aggregation**: all messages concatenated into a single `TextBlock` with role prefixes (`[System]`, `[Assistant]`, `[Tool Result]`). Multi-turn conversation structure is flattened. |
| `SessionUpdate.AgentMessageChunk.Content.Text.Text` | Output | `schema.Message{Role: Assistant, Content: text}` | **Direct**: text chunks accumulated, concatenated in `finalMessage()` |
| `SessionUpdate.AgentThoughtChunk` | Output | *(not mapped)* | **Dropped**: reasoning/thinking content is silently discarded |
| `SessionUpdate.UserMessageChunk` | Output | *(not mapped)* | **Dropped**: echoed user messages are discarded |
| `SessionUpdate.Plan` | Output | *(not mapped)* | **Dropped**: agent execution plans are discarded |

### Tool Calls

ACP tool calls have a lifecycle (`ToolCall` → `ToolCallUpdate`*), while Eino represents them as a flat list in the assistant message.

| ACP | Eino | Conversion |
|-----|------|------------|
| `SessionUpdateToolCall.ToolCallId` | `schema.ToolCall.ID` | **Direct** |
| `SessionUpdateToolCall.Kind` (ToolKind) | `schema.ToolCall.Function.Name` | **Direct**: kind string becomes function name. Falls back to `"other"` if empty. |
| `SessionUpdateToolCall.Title` | `schema.ToolCall.Extra["acp_title"]` | **Metadata only**: human-readable title goes to Extra, not used as function name |
| `SessionUpdateToolCall.RawInput` | `schema.ToolCall.Function.Arguments` | **JSON marshal**: arbitrary value → JSON string |
| `SessionUpdateToolCall.RawOutput` | `schema.ToolCall.Extra["acp_raw_output"]` | **Metadata only**: not part of standard ToolCall schema |
| `SessionUpdateToolCall.Status` | `schema.ToolCall.Extra["acp_status"]` | **Metadata only** |
| `SessionUpdateToolCall.Locations` | `schema.ToolCall.Extra["acp_locations"]` | **Metadata only**: file paths affected |
| `SessionUpdateToolCall.Content` | `schema.ToolCall.Extra["acp_content"]` | **Metadata only**: diffs, terminal refs |
| `SessionToolCallUpdate.*` | merges into same `schema.ToolCall` | **Incremental merge**: updates replace non-nil fields |

### Tool Call Status → Eino Callbacks

| ACP Status | Eino Callback | Notes |
|------------|---------------|-------|
| `ToolCall` event received | `callbacks.OnStart(ctx, &tool.CallbackInput{})` | Fires immediately on tool start. Name from Kind or Title. |
| `ToolCallUpdate` with `status=completed` | `callbacks.OnEnd(ctx, &tool.CallbackOutput{})` | Response from `RawOutput`, JSON-marshaled |
| `ToolCallUpdate` with `status=failed` | `callbacks.OnError(ctx, err)` | Error message is generic `"tool {id} failed"` |
| `ToolCallUpdate` with `status=in_progress` | *(no callback)* | Progress updates silently absorbed into tracker |
| `ToolCallUpdate` with `status=pending` | *(no callback)* | Status regression silently absorbed |

### Tool Kinds → Function Names

| ACP ToolKind | Eino Function Name | Semantic |
|--------------|-------------------|----------|
| `read` | `"read"` | File/resource reading |
| `edit` | `"edit"` | File modification |
| `delete` | `"delete"` | File deletion |
| `move` | `"move"` | File movement/rename |
| `search` | `"search"` | Search operation |
| `execute` | `"execute"` | Command/script execution |
| `think` | `"think"` | Internal reasoning/planning |
| `fetch` | `"fetch"` | Network fetch |
| `switch_mode` | `"switch_mode"` | Session mode change |
| `other` / empty | `"other"` | Fallback |

### ACP Client Callbacks (Agent → Client)

| ACP Method | Implementation | Notes |
|------------|---------------|-------|
| `fs/read_text_file` | `os.ReadFile` with line slicing | Absolute path required. No symlink protection. |
| `fs/write_text_file` | `os.WriteFile` with `MkdirAll` | Creates parent dirs automatically. |
| `session/request_permission` | Auto-approve first option or deny | No user interaction. |
| `terminal/create` | **Stub**: returns `"t-stub"` | Terminal not implemented. |
| `terminal/output` | **Stub**: returns empty | Terminal not implemented. |
| `terminal/wait_for_exit` | **Stub**: returns immediately | Terminal not implemented. |
| `terminal/kill` | **Stub**: no-op | Terminal not implemented. |
| `terminal/release` | **Stub**: no-op | Terminal not implemented. |

### Prompt Stop Reasons

| ACP StopReason | Eino Equivalent | Current Mapping |
|----------------|-----------------|-----------------|
| `end_turn` | `ResponseMeta.FinishReason = "stop"` | **Not mapped**: StopReason is discarded |
| `max_tokens` | `ResponseMeta.FinishReason = "length"` | **Not mapped** |
| `max_turn_requests` | `ResponseMeta.FinishReason = "length"` | **Not mapped** |
| `refusal` | `ResponseMeta.FinishReason = "content_filter"` | **Not mapped** |
| `cancelled` | error return | **Not mapped** |

### Token Usage

| ACP | Eino | Current Mapping |
|-----|------|-----------------|
| *(not provided by ACP)* | `model.TokenUsage` in `CallbackOutput` | **Not available**: ACP protocol does not expose token usage. `CallbackOutput.TokenUsage` is always nil. |

## Aggregation Points

These are places where multiple ACP events are aggregated into a single Eino structure:

### 1. Text Message Aggregation
```
AgentMessageChunk[0].Text + AgentMessageChunk[1].Text + ... → Message.Content
```
- Multiple text chunks concatenated in order
- No delimiter between chunks
- Chunks arrive via `onUpdate` callback, accumulated in `responseCollector.textParts`

### 2. Tool Call Aggregation
```
ToolCall{id=A} + ToolCallUpdate{id=A, status=completed} → ToolCall{id=A, ...final state}
```
- `trackedToolCall` struct accumulates fields from both events
- Order preserved via `toolCallTracker.order` array
- Final list built by `buildFinal()` which iterates in encounter order

### 3. Input Message Flattening
```
Message[system] + Message[user] + Message[assistant] + Message[tool] → single TextBlock
```
- **Lossy**: role prefixes added as text markers `[System]`, `[Tool Result]`
- **Lossy**: multi-turn structure destroyed (all concatenated with `\n\n`)
- **Lossy**: tool call IDs and tool result associations lost

## Streaming (Stream vs Generate)

ACP itself has no Generate/Stream distinction. There is only `session/prompt`, and the agent pushes `SessionUpdate` notifications incrementally — ACP is **streaming-only by design**.

The Generate vs Stream split is an eino-acp adapter concern: both call the same `runPrompt()` underneath, but differ in how the `SessionUpdate` stream is surfaced to eino callers.

### Generate (synchronous)

```
ACP onUpdate loop                       Eino
──────────────────                      ────
AgentMessageChunk(text)  →  accumulate in responseCollector.textParts
ToolCall                 →  track in toolCallTracker + fire tool OnStart callback
ToolCallUpdate           →  merge into tracker + fire tool OnEnd callback
  ... (loop until PromptResponse) ...
                         →  responseCollector.finalMessage() → callbacks.OnEnd → return Message
```

All events collected internally; caller gets one final `*schema.Message`.

### Stream (async chunks)

```
ACP onUpdate loop                       Eino StreamReader[*schema.Message]
──────────────────                      ──────────────────────────────────
AgentMessageChunk(text)  →  sw.Send(Message{Content: chunk})     ← text chunk emitted immediately
ToolCall(id=A, start)    →  sw.Send(Message{ToolCalls: [{A}]})   ← tool start emitted as message
                            + fire tool OnStart callback
ToolCallUpdate(id=A)     →  sw.Send(Message{ToolCalls: [{A}]})   ← tool update emitted as message
                            + fire tool OnEnd/OnError callback
  ... (loop) ...
runPrompt returns        →  sw.Close()                            ← stream ends
```

Key differences from Generate:
- Each text chunk is sent immediately via `sw.Send()` as a separate `schema.Message`
- Each tool event (start/update) is also sent as a separate message with one `ToolCall`
- The stream uses `schema.Pipe[*model.CallbackOutput]` with buffer size 1
- `callbacks.OnEndWithStreamOutput` wraps the stream so eino's callback system can observe chunks
- The final conversion strips `model.CallbackOutput` wrapper → `*schema.Message` via `StreamReaderWithConvert`
- **No final aggregated message**: unlike Generate, Stream does not call `finalMessage()`. The consumer must concatenate chunks themselves using `schema.ConcatMessages()` if needed.

### Callback timing comparison

| Event | Generate | Stream |
|-------|----------|--------|
| `callbacks.OnStart` (ChatModel) | Before `runPrompt` | Before `runPrompt` |
| `callbacks.OnStart` (Tool) | Real-time per tool | Real-time per tool |
| `callbacks.OnEnd` (Tool) | Real-time per tool | Real-time per tool |
| `callbacks.OnEnd` (ChatModel) | After `finalMessage()` | **Not called directly** — uses `OnEndWithStreamOutput` instead |
| `callbacks.OnEndWithStreamOutput` | Not used | Wraps the `StreamReader` so handlers observe each chunk |

### Stream chunk types

The stream emits these message shapes:

1. **Text chunk**: `Message{Role: Assistant, Content: "partial text..."}`
2. **Tool call start**: `Message{Role: Assistant, ToolCalls: [{ID: "A", Function: {Name: "read", Arguments: "{...}"}, Extra: {acp_stream_event: "tool_call"}}]}`
3. **Tool call update**: `Message{Role: Assistant, ToolCalls: [{ID: "A", ..., Extra: {acp_stream_event: "tool_call_update"}}]}`

The `acp_stream_event` extra field distinguishes start from update in the stream. This field is intentionally absent from the final aggregated message in Generate mode.

## Known Gaps

### Critical
- **Terminal I/O not implemented**: Agent cannot execute commands. All terminal methods return stubs. This means tools of kind `execute` that rely on ACP terminal (rather than the agent's internal execution) will silently fail.

### Data Loss
- **Input message structure**: Multi-turn conversations flattened to single text block. Tool results lose their `ToolCallID` associations.
- **Agent thought/reasoning**: `AgentThoughtChunk` discarded; could map to `Message.ReasoningContent`.
- **Agent plan**: `SessionUpdatePlan` discarded; no eino equivalent but could be useful metadata.
- **Stop reason**: `PromptResponse.StopReason` not mapped to `ResponseMeta.FinishReason`.
- **Token usage**: ACP doesn't expose this; eino callbacks always report nil usage.
- **Multimodal content**: ACP supports image/audio/resource ContentBlocks; current implementation only handles text.

### Semantic Mismatch
- **Function name from Kind**: ACP `ToolKind` (e.g., `read`, `execute`) becomes eino `FunctionCall.Name`. This loses the actual tool identity — two different read tools both get `Name: "read"`. The descriptive `Title` field is only in `Extra`, not in the function name.
- **Tool output location**: ACP `RawOutput` goes to `Extra["acp_raw_output"]` on the final message, but the `tool.CallbackOutput.Response` in OnEnd callback gets it too. These can diverge if there are intermediate updates.

## Potential Improvements

1. **Structured input messages**: Use eino's multi-message support instead of concatenating into one text block. ACP's `PromptRequest.Prompt` accepts `[]ContentBlock` so this is technically possible.
2. **Map AgentThoughtChunk**: Route to `Message.ReasoningContent` for thinking/reasoning capture.
3. **Map StopReason → FinishReason**: Preserve completion semantics.
4. **Implement terminal**: Full terminal support for command execution via ACP.
5. **Multimodal support**: Map ACP image/audio ContentBlocks to eino's `UserInputMultiContent`.
6. **Richer tool naming**: Consider `Kind + "/" + Title` or just `Title` as function name for better tool identification.
