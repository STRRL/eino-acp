package einoacp

import (
	"encoding/json"
	"testing"

	"github.com/cloudwego/eino/schema"
	acp "github.com/coder/acp-go-sdk"
)

func TestToolCallTrackerBuildFinalFromJSONUpdates(t *testing.T) {
	tracker := newToolCallTracker()

	for _, raw := range []string{
		`{
			"sessionUpdate": "tool_call",
			"toolCallId": "call_read",
			"title": "Read README.md",
			"kind": "read",
			"status": "pending",
			"locations": [{"path": "/repo/README.md"}],
			"rawInput": {"path": "README.md"}
		}`,
		`{
			"sessionUpdate": "tool_call_update",
			"toolCallId": "call_read",
			"status": "completed",
			"rawOutput": {"bytes": 123}
		}`,
		`{
			"sessionUpdate": "tool_call",
			"toolCallId": "call_exec",
			"title": "Run tests",
			"kind": "execute",
			"status": "in_progress",
			"rawInput": {"command": "go test ./..."}
		}`,
	} {
		update := mustSessionUpdateFromJSON(t, raw)
		if _, ok := tracker.applyUpdate(update); !ok {
			t.Fatalf("expected tool update for %s", raw)
		}
	}

	got := tracker.buildFinal()
	if len(got) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(got))
	}

	assertToolCall(t, got[0], "call_read", "read", `{"path":"README.md"}`)
	if got[0].Extra["acp_title"] != "Read README.md" {
		t.Fatalf("expected read title in extra, got %#v", got[0].Extra["acp_title"])
	}
	if got[0].Extra["acp_status"] != "completed" {
		t.Fatalf("expected completed status, got %#v", got[0].Extra["acp_status"])
	}
	rawOutput, ok := got[0].Extra["acp_raw_output"].(map[string]any)
	if !ok {
		t.Fatalf("expected map raw output, got %#v", got[0].Extra["acp_raw_output"])
	}
	if rawOutput["bytes"] != float64(123) {
		t.Fatalf("expected raw output bytes 123, got %#v", rawOutput["bytes"])
	}

	assertToolCall(t, got[1], "call_exec", "execute", `{"command":"go test ./..."}`)
	if got[1].Extra["acp_status"] != "in_progress" {
		t.Fatalf("expected execute status in_progress, got %#v", got[1].Extra["acp_status"])
	}
}

func TestResponseCollectorEmitsTextAndToolStreamChunks(t *testing.T) {
	collector := newResponseCollector()

	textChunk, toolChunk := collector.handleUpdate(acp.UpdateAgentMessageText("Hello "))
	if textChunk == nil || textChunk.Content != "Hello " {
		t.Fatalf("expected first text chunk, got %#v", textChunk)
	}
	if toolChunk != nil {
		t.Fatalf("expected no tool chunk for text update, got %#v", toolChunk)
	}

	textChunk, toolChunk = collector.handleUpdate(mustSessionUpdateFromJSON(t, `{
		"sessionUpdate": "tool_call",
		"toolCallId": "call_fetch",
		"title": "Fetch docs",
		"kind": "fetch",
		"status": "pending",
		"rawInput": {"url": "https://agentclientprotocol.com"}
	}`))
	if textChunk != nil {
		t.Fatalf("expected no text chunk for tool start, got %#v", textChunk)
	}
	if toolChunk == nil {
		t.Fatal("expected tool chunk for tool start")
	}
	if toolChunk.Content != "" {
		t.Fatalf("expected empty content for tool chunk, got %q", toolChunk.Content)
	}
	if len(toolChunk.ToolCalls) != 1 {
		t.Fatalf("expected 1 streamed tool call, got %d", len(toolChunk.ToolCalls))
	}
	assertToolCall(t, toolChunk.ToolCalls[0], "call_fetch", "fetch", `{"url":"https://agentclientprotocol.com"}`)
	if toolChunk.ToolCalls[0].Extra["acp_stream_event"] != "tool_call" {
		t.Fatalf("expected tool start stream marker, got %#v", toolChunk.ToolCalls[0].Extra["acp_stream_event"])
	}

	textChunk, toolChunk = collector.handleUpdate(mustSessionUpdateFromJSON(t, `{
		"sessionUpdate": "tool_call_update",
		"toolCallId": "call_fetch",
		"status": "completed",
		"rawOutput": {"status": 200}
	}`))
	if textChunk != nil {
		t.Fatalf("expected no text chunk for tool update, got %#v", textChunk)
	}
	if toolChunk == nil {
		t.Fatal("expected tool chunk for tool update")
	}
	if toolChunk.ToolCalls[0].Extra["acp_stream_event"] != "tool_call_update" {
		t.Fatalf("expected tool update stream marker, got %#v", toolChunk.ToolCalls[0].Extra["acp_stream_event"])
	}
	if toolChunk.ToolCalls[0].Extra["acp_status"] != "completed" {
		t.Fatalf("expected completed status after update, got %#v", toolChunk.ToolCalls[0].Extra["acp_status"])
	}

	textChunk, toolChunk = collector.handleUpdate(acp.UpdateAgentMessageText("world"))
	if textChunk == nil || textChunk.Content != "world" {
		t.Fatalf("expected final text chunk, got %#v", textChunk)
	}
	if toolChunk != nil {
		t.Fatalf("expected no tool chunk for final text update, got %#v", toolChunk)
	}

	final := collector.finalMessage()
	if final.Role != schema.Assistant {
		t.Fatalf("expected assistant role, got %q", final.Role)
	}
	if final.Content != "Hello world" {
		t.Fatalf("expected merged content, got %q", final.Content)
	}
	if len(final.ToolCalls) != 1 {
		t.Fatalf("expected 1 final tool call, got %d", len(final.ToolCalls))
	}
	assertToolCall(t, final.ToolCalls[0], "call_fetch", "fetch", `{"url":"https://agentclientprotocol.com"}`)
	if _, ok := final.ToolCalls[0].Extra["acp_stream_event"]; ok {
		t.Fatalf("did not expect stream-only metadata in final tool call: %#v", final.ToolCalls[0].Extra)
	}
	if final.ToolCalls[0].Extra["acp_status"] != "completed" {
		t.Fatalf("expected final tool status completed, got %#v", final.ToolCalls[0].Extra["acp_status"])
	}
}

func mustSessionUpdateFromJSON(t *testing.T, raw string) acp.SessionUpdate {
	t.Helper()

	var update acp.SessionUpdate
	if err := json.Unmarshal([]byte(raw), &update); err != nil {
		t.Fatalf("unmarshal session update: %v", err)
	}

	return update
}

func assertToolCall(t *testing.T, got schema.ToolCall, wantID, wantName, wantArgs string) {
	t.Helper()

	if got.ID != wantID {
		t.Fatalf("expected id %q, got %q", wantID, got.ID)
	}
	if got.Type != einoToolCallType {
		t.Fatalf("expected type %q, got %q", einoToolCallType, got.Type)
	}
	if got.Function.Name != wantName {
		t.Fatalf("expected function name %q, got %q", wantName, got.Function.Name)
	}
	if got.Function.Arguments != wantArgs {
		t.Fatalf("expected arguments %s, got %s", wantArgs, got.Function.Arguments)
	}
}
