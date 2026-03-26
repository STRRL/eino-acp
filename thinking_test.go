package einoacp

import (
	"testing"

	"github.com/cloudwego/eino/schema"
	acp "github.com/coder/acp-go-sdk"
)

// TestResponseCollectorThinkingAccumulated verifies that AgentThoughtChunk
// events are accumulated and surfaced as ReasoningContent on the final message.
func TestResponseCollectorThinkingAccumulated(t *testing.T) {
	collector := newResponseCollector()

	// Three thought chunks interleaved with a text chunk
	for _, u := range []acp.SessionUpdate{
		acp.UpdateAgentThoughtText("Let me think about this..."),
		acp.UpdateAgentThoughtText(" The answer is 42."),
		acp.UpdateAgentMessageText("The answer is 42."),
	} {
		textChunk, toolChunk := collector.handleUpdate(u)
		if toolChunk != nil {
			t.Fatalf("expected no tool chunk for thought/text update, got %#v", toolChunk)
		}
		_ = textChunk
	}

	final := collector.finalMessage()
	if final.ReasoningContent != "Let me think about this... The answer is 42." {
		t.Fatalf("expected accumulated reasoning, got %q", final.ReasoningContent)
	}
	if final.Content != "The answer is 42." {
		t.Fatalf("expected text content, got %q", final.Content)
	}
}

// TestResponseCollectorThinkingStreamChunks verifies that each AgentThoughtChunk
// produces a streaming chunk with ReasoningContent set (and Content empty).
func TestResponseCollectorThinkingStreamChunks(t *testing.T) {
	collector := newResponseCollector()

	textChunk, toolChunk := collector.handleUpdate(acp.UpdateAgentThoughtText("thinking..."))
	if toolChunk != nil {
		t.Fatalf("expected no tool chunk for thought update")
	}
	if textChunk == nil {
		t.Fatal("expected a stream chunk for thought update")
	}
	if textChunk.ReasoningContent != "thinking..." {
		t.Fatalf("expected ReasoningContent in stream chunk, got %q", textChunk.ReasoningContent)
	}
	if textChunk.Content != "" {
		t.Fatalf("expected empty Content in thought stream chunk, got %q", textChunk.Content)
	}
}

// TestResponseCollectorThoughtOnlySession exercises a session where only
// thought chunks arrive (no text output). Final message should have
// ReasoningContent but empty Content.
func TestResponseCollectorThoughtOnlySession(t *testing.T) {
	collector := newResponseCollector()

	for _, u := range []acp.SessionUpdate{
		acp.UpdateAgentThoughtText("step 1"),
		acp.UpdateAgentThoughtText("step 2"),
	} {
		collector.handleUpdate(u)
	}

	final := collector.finalMessage()
	if final.Role != schema.Assistant {
		t.Fatalf("expected assistant role, got %q", final.Role)
	}
	if final.ReasoningContent != "step 1step 2" {
		t.Fatalf("expected joined reasoning, got %q", final.ReasoningContent)
	}
	if final.Content != "" {
		t.Fatalf("expected empty content, got %q", final.Content)
	}
}

// TestResponseCollectorPlanDropped verifies that Plan updates are silently
// absorbed and do not affect the final message (known gap, documented in MAPPING.md).
func TestResponseCollectorPlanDropped(t *testing.T) {
	collector := newResponseCollector()

	collector.handleUpdate(acp.UpdatePlan(
		acp.PlanEntry{Content: "Analyze codebase", Priority: "high", Status: "pending"},
		acp.PlanEntry{Content: "Write tests", Priority: "medium", Status: "pending"},
	))
	collector.handleUpdate(acp.UpdateAgentMessageText("done"))

	final := collector.finalMessage()
	if final.Content != "done" {
		t.Fatalf("expected content 'done', got %q", final.Content)
	}
	// Plan is intentionally dropped; nothing in final message should reference it
	if final.ReasoningContent != "" {
		t.Fatalf("plan should not populate ReasoningContent, got %q", final.ReasoningContent)
	}
}

// TestResponseCollectorThinkingWithToolCalls exercises a realistic mixed sequence:
// thought → tool call → tool update → thought → text.
func TestResponseCollectorThinkingWithToolCalls(t *testing.T) {
	collector := newResponseCollector()

	updates := []acp.SessionUpdate{
		acp.UpdateAgentThoughtText("I should read the file first."),
		acp.StartToolCall("call_1", "Read main.go",
			acp.WithStartKind(acp.ToolKindRead),
			acp.WithStartStatus(acp.ToolCallStatusPending),
			acp.WithStartLocations([]acp.ToolCallLocation{{Path: "/repo/main.go"}}),
			acp.WithStartRawInput(map[string]any{"path": "/repo/main.go"}),
		),
		acp.UpdateToolCall("call_1",
			acp.WithUpdateStatus(acp.ToolCallStatusCompleted),
			acp.WithUpdateRawOutput(map[string]any{"lines": 42}),
		),
		acp.UpdateAgentThoughtText(" File has 42 lines."),
		acp.UpdateAgentMessageText("The file has 42 lines."),
	}

	for _, u := range updates {
		collector.handleUpdate(u)
	}

	final := collector.finalMessage()
	if final.ReasoningContent != "I should read the file first. File has 42 lines." {
		t.Fatalf("expected merged reasoning, got %q", final.ReasoningContent)
	}
	if final.Content != "The file has 42 lines." {
		t.Fatalf("expected text content, got %q", final.Content)
	}
	if len(final.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(final.ToolCalls))
	}
	assertToolCall(t, final.ToolCalls[0], "call_1", "read", `{"path":"/repo/main.go"}`)
}

// TestResponseCollectorThinkingStreamChunkDoesNotLeakIntoFinalToolCall verifies
// that ReasoningContent in stream chunks does not bleed into ToolCall extra fields.
func TestResponseCollectorThinkingStreamChunkDoesNotLeakIntoFinalToolCall(t *testing.T) {
	collector := newResponseCollector()

	collector.handleUpdate(acp.UpdateAgentThoughtText("deciding which tool to use"))
	_, toolChunk := collector.handleUpdate(
		acp.StartToolCall("call_2", "Search",
			acp.WithStartKind(acp.ToolKindSearch),
			acp.WithStartStatus(acp.ToolCallStatusPending),
			acp.WithStartRawInput(map[string]any{"query": "eino acp"}),
		),
	)

	if toolChunk == nil {
		t.Fatal("expected tool chunk for StartToolCall")
	}
	// Tool chunk should have no ReasoningContent
	if toolChunk.ReasoningContent != "" {
		t.Fatalf("tool chunk must not carry ReasoningContent, got %q", toolChunk.ReasoningContent)
	}
}
