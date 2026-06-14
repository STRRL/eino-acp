package einoacp

import (
	"encoding/json"
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

// ptr is a tiny helper for the optional pointer fields in the SDK types.
func ptr[T any](v T) *T { return &v }

// modelSelectOption builds the category="model" select config option an
// ACP agent reports in NewSessionResponse.ConfigOptions. This mirrors the
// wire shape produced by @agentclientprotocol/claude-agent-acp after model
// selection moved from the (now removed) session/set_model RPC to a
// session/set_config_option of category "model".
func modelSelectOption() acp.SessionConfigOption {
	return acp.SessionConfigOption{
		Select: &acp.SessionConfigOptionSelect{
			Id:           "model",
			Name:         "Model",
			Category:     ptr(acp.SessionConfigOptionCategoryModel),
			CurrentValue: "default",
			Options: acp.SessionConfigSelectOptions{
				Ungrouped: &acp.SessionConfigSelectOptionsUngrouped{
					{Value: "default", Name: "Default"},
					{Value: "sonnet", Name: "Sonnet 4.6", Description: ptr("Balanced model")},
					{Value: "haiku", Name: "Haiku 4.5"},
				},
			},
		},
	}
}

func TestExtractModelConfigFindsModelCategorySelect(t *testing.T) {
	opts := []acp.SessionConfigOption{
		// A non-model boolean option that must be ignored.
		{Boolean: &acp.SessionConfigOptionBoolean{Id: "yolo", Name: "YOLO"}},
		modelSelectOption(),
	}

	mc := ExtractModelConfig(opts)
	if mc == nil {
		t.Fatal("ExtractModelConfig returned nil; expected the category=model select option")
	}
	if mc.ConfigID != "model" {
		t.Fatalf("ConfigID = %q, want %q", mc.ConfigID, "model")
	}
	if mc.CurrentModelID != "default" {
		t.Fatalf("CurrentModelID = %q, want %q", mc.CurrentModelID, "default")
	}
	if len(mc.Available) != 3 {
		t.Fatalf("Available = %d models, want 3", len(mc.Available))
	}
	if mc.Available[1].ID != "sonnet" || mc.Available[1].Name != "Sonnet 4.6" || mc.Available[1].Description != "Balanced model" {
		t.Fatalf("Available[1] = %+v, want sonnet/Sonnet 4.6/Balanced model", mc.Available[1])
	}
}

func TestExtractModelConfigNilWhenNoModelOption(t *testing.T) {
	opts := []acp.SessionConfigOption{
		{Boolean: &acp.SessionConfigOptionBoolean{Id: "yolo", Name: "YOLO"}},
	}
	if mc := ExtractModelConfig(opts); mc != nil {
		t.Fatalf("ExtractModelConfig = %+v, want nil when no model option is advertised", mc)
	}
}

func TestModelConfigResolveValue(t *testing.T) {
	mc := ExtractModelConfig([]acp.SessionConfigOption{modelSelectOption()})
	if mc == nil {
		t.Fatal("setup: ExtractModelConfig returned nil")
	}

	// Exact value id match.
	if v, ok := mc.ResolveValue("sonnet"); !ok || v != "sonnet" {
		t.Fatalf("ResolveValue(sonnet) = %q,%v want sonnet,true", v, ok)
	}
	// Case-insensitive human name match.
	if v, ok := mc.ResolveValue("haiku 4.5"); !ok || v != "haiku" {
		t.Fatalf("ResolveValue(\"haiku 4.5\") = %q,%v want haiku,true", v, ok)
	}
	// Unknown model: fail fast, no fallback.
	if v, ok := mc.ResolveValue("gpt-5"); ok {
		t.Fatalf("ResolveValue(gpt-5) = %q,true want \"\",false", v)
	}
}

// TestExtractModelConfigGrouped verifies grouped option lists are flattened.
func TestExtractModelConfigGrouped(t *testing.T) {
	raw := `{
		"type": "select",
		"id": "model",
		"name": "Model",
		"category": "model",
		"currentValue": "sonnet",
		"options": [
			{"group": "anthropic", "name": "Anthropic", "options": [
				{"value": "sonnet", "name": "Sonnet 4.6"},
				{"value": "opus", "name": "Opus 4.8"}
			]}
		]
	}`
	var opt acp.SessionConfigOption
	if err := json.Unmarshal([]byte(raw), &opt); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	mc := ExtractModelConfig([]acp.SessionConfigOption{opt})
	if mc == nil {
		t.Fatal("ExtractModelConfig returned nil for grouped options")
	}
	if len(mc.Available) != 2 {
		t.Fatalf("Available = %d, want 2 flattened grouped options", len(mc.Available))
	}
	if v, ok := mc.ResolveValue("opus"); !ok || v != "opus" {
		t.Fatalf("ResolveValue(opus) = %q,%v want opus,true", v, ok)
	}
}
