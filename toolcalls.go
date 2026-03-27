package einoacp

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudwego/eino/schema"
	acp "github.com/coder/acp-go-sdk"
)

const (
	einoToolCallType  = "function"
	defaultToolName   = "other"
	streamEventStart  = "tool_call"
	streamEventUpdate = "tool_call_update"
)

type responseCollector struct {
	textParts     []string
	thoughtParts  []string
	toolCalls     *toolCallTracker
}

func newResponseCollector() *responseCollector {
	return &responseCollector{
		toolCalls: newToolCallTracker(),
	}
}

func (c *responseCollector) handleUpdate(u acp.SessionUpdate) (textChunk *schema.Message, toolChunk *schema.Message) {
	if text := assistantTextFromUpdate(u); text != "" {
		c.textParts = append(c.textParts, text)
		textChunk = &schema.Message{
			Role:    schema.Assistant,
			Content: text,
		}
	}

	if thought := assistantThoughtFromUpdate(u); thought != "" {
		c.thoughtParts = append(c.thoughtParts, thought)
		// Emit thought as a stream chunk with ReasoningContent set, Content empty.
		textChunk = &schema.Message{
			Role:             schema.Assistant,
			ReasoningContent: thought,
		}
	}

	if tc, ok := c.toolCalls.applyUpdate(u); ok {
		toolChunk = &schema.Message{
			Role:      schema.Assistant,
			ToolCalls: []schema.ToolCall{tc},
		}
	}

	return textChunk, toolChunk
}

func (c *responseCollector) finalMessage() *schema.Message {
	return &schema.Message{
		Role:             schema.Assistant,
		Content:          strings.Join(c.textParts, ""),
		ReasoningContent: strings.Join(c.thoughtParts, ""),
		ToolCalls:        c.toolCalls.buildFinal(),
	}
}

func assistantTextFromUpdate(u acp.SessionUpdate) string {
	if u.AgentMessageChunk == nil || u.AgentMessageChunk.Content.Text == nil {
		return ""
	}

	return u.AgentMessageChunk.Content.Text.Text
}

func assistantThoughtFromUpdate(u acp.SessionUpdate) string {
	if u.AgentThoughtChunk == nil || u.AgentThoughtChunk.Content.Text == nil {
		return ""
	}

	return u.AgentThoughtChunk.Content.Text.Text
}

type toolCallTracker struct {
	order []string
	calls map[string]*trackedToolCall
}

type trackedToolCall struct {
	id        string
	title     string
	kind      acp.ToolKind
	status    acp.ToolCallStatus
	content   []acp.ToolCallContent
	locations []acp.ToolCallLocation
	rawInput  any
	rawOutput any
	meta      any
}

func newToolCallTracker() *toolCallTracker {
	return &toolCallTracker{
		calls: make(map[string]*trackedToolCall),
	}
}

func (t *toolCallTracker) applyUpdate(u acp.SessionUpdate) (schema.ToolCall, bool) {
	switch {
	case u.ToolCall != nil:
		call := t.ensure(string(u.ToolCall.ToolCallId))
		call.mergeStart(*u.ToolCall)
		return call.toSchemaToolCall(streamEventStart), true
	case u.ToolCallUpdate != nil:
		call := t.ensure(string(u.ToolCallUpdate.ToolCallId))
		call.mergeUpdate(*u.ToolCallUpdate)
		return call.toSchemaToolCall(streamEventUpdate), true
	default:
		return schema.ToolCall{}, false
	}
}

func (t *toolCallTracker) buildFinal() []schema.ToolCall {
	if len(t.order) == 0 {
		return nil
	}

	out := make([]schema.ToolCall, 0, len(t.order))
	for _, id := range t.order {
		call, ok := t.calls[id]
		if !ok {
			continue
		}
		out = append(out, call.toSchemaToolCall(""))
	}

	return out
}

func (t *toolCallTracker) ensure(id string) *trackedToolCall {
	if call, ok := t.calls[id]; ok {
		return call
	}

	call := &trackedToolCall{id: id}
	t.calls[id] = call
	t.order = append(t.order, id)
	return call
}

func (c *trackedToolCall) mergeStart(update acp.SessionUpdateToolCall) {
	if update.Meta != nil {
		c.meta = update.Meta
	}
	if update.Content != nil {
		c.content = slices.Clone(update.Content)
	}
	if update.Locations != nil {
		c.locations = slices.Clone(update.Locations)
	}
	if update.RawInput != nil {
		c.rawInput = update.RawInput
	}
	if update.RawOutput != nil {
		c.rawOutput = update.RawOutput
	}

	if update.Title != "" {
		c.title = update.Title
	}
	if update.Kind != "" {
		c.kind = update.Kind
	}
	if update.Status != "" {
		c.status = update.Status
	}
}

func (c *trackedToolCall) mergeUpdate(update acp.SessionToolCallUpdate) {
	if update.Meta != nil {
		c.meta = update.Meta
	}
	if update.Content != nil {
		c.content = slices.Clone(update.Content)
	}
	if update.Locations != nil {
		c.locations = slices.Clone(update.Locations)
	}

	if update.RawInput != nil {
		c.rawInput = update.RawInput
	}
	if update.RawOutput != nil {
		c.rawOutput = update.RawOutput
	}
	if update.Title != nil {
		c.title = *update.Title
	}
	if update.Kind != nil {
		c.kind = *update.Kind
	}
	if update.Status != nil {
		c.status = *update.Status
	}
}

func (c *trackedToolCall) toSchemaToolCall(streamEvent string) schema.ToolCall {
	return schema.ToolCall{
		ID:   c.id,
		Type: einoToolCallType,
		Function: schema.FunctionCall{
			Name:      c.functionName(),
			Arguments: marshalACPValue(c.rawInput, "{}"),
		},
		Extra: c.extra(streamEvent),
	}
}

func (c *trackedToolCall) functionName() string {
	if c.kind != "" {
		return string(c.kind)
	}

	return defaultToolName
}

func (c *trackedToolCall) extra(streamEvent string) map[string]any {
	extra := map[string]any{}
	if c.title != "" {
		extra["acp_title"] = c.title
	}
	if c.kind != "" {
		extra["acp_kind"] = string(c.kind)
	}
	if c.status != "" {
		extra["acp_status"] = string(c.status)
	}
	if c.rawOutput != nil {
		extra["acp_raw_output"] = c.rawOutput
	}
	if len(c.locations) > 0 {
		extra["acp_locations"] = c.locations
	}
	if len(c.content) > 0 {
		extra["acp_content"] = c.content
	}
	if c.meta != nil {
		extra["acp_meta"] = c.meta
	}
	if streamEvent != "" {
		extra["acp_stream_event"] = streamEvent
	}
	if len(extra) == 0 {
		return nil
	}

	return extra
}

func marshalACPValue(v any, fallback string) string {
	if v == nil {
		return fallback
	}

	b, err := json.Marshal(v)
	if err == nil {
		return string(b)
	}

	b, err = json.Marshal(fmt.Sprint(v))
	if err == nil {
		return string(b)
	}

	return fallback
}
