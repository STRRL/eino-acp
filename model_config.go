package einoacp

import (
	"strings"

	acp "github.com/coder/acp-go-sdk"
)

// ModelOption is one selectable model advertised by an ACP agent's
// "model" session config option.
type ModelOption struct {
	ID          string
	Name        string
	Description string
	// Meta carries the agent's per-option `_meta` extension payload (e.g.
	// Copilot's usage metadata), passed through verbatim for callers that
	// synthesize richer descriptions. nil when the agent reports none.
	Meta map[string]any
}

// ModelConfig is the agent's model-selection config option for a session,
// extracted from NewSessionResponse.ConfigOptions.
//
// Model selection is no longer a dedicated RPC. The (unstable, now removed)
// session/set_model method was folded into the generic session config
// option mechanism: agents advertise a `select` option with category
// "model" at session creation, and the client switches models by calling
// session/set_config_option against that option's id. See
// https://agentclientprotocol.com/protocol/session-config-options
type ModelConfig struct {
	// ConfigID is the SessionConfigId to target in a set_config_option call.
	ConfigID string
	// CurrentModelID is the value id the agent currently has selected.
	CurrentModelID string
	// Available is the flattened (grouped + ungrouped) list of selectable
	// models in advertised order.
	Available []ModelOption
}

// ExtractModelConfig returns the category=="model" select option from a
// NewSession response's config options, or nil when the agent advertises
// no model selector. Boolean options and non-model categories are ignored.
func ExtractModelConfig(opts []acp.SessionConfigOption) *ModelConfig {
	for _, opt := range opts {
		sel := opt.Select
		if sel == nil {
			continue
		}
		if sel.Category == nil || *sel.Category != acp.SessionConfigOptionCategoryModel {
			continue
		}
		mc := &ModelConfig{
			ConfigID:       string(sel.Id),
			CurrentModelID: string(sel.CurrentValue),
			Available:      flattenSelectOptions(sel.Options),
		}
		return mc
	}
	return nil
}

// flattenSelectOptions collapses the grouped/ungrouped union into a flat
// ordered list of ModelOption.
func flattenSelectOptions(o acp.SessionConfigSelectOptions) []ModelOption {
	var out []ModelOption
	appendOne := func(v acp.SessionConfigSelectOption) {
		desc := ""
		if v.Description != nil {
			desc = *v.Description
		}
		out = append(out, ModelOption{
			ID:          string(v.Value),
			Name:        v.Name,
			Description: desc,
			Meta:        v.Meta,
		})
	}
	if o.Ungrouped != nil {
		for _, v := range *o.Ungrouped {
			appendOne(v)
		}
	}
	if o.Grouped != nil {
		for _, g := range *o.Grouped {
			for _, v := range g.Options {
				appendOne(v)
			}
		}
	}
	return out
}

// ResolveValue maps a user-requested model id/alias to a concrete value id
// from the available options. It matches by exact value id first, then
// case-insensitively by human-readable name. Returns "", false when the
// requested model is not advertised — callers fail fast rather than
// silently falling back to a default.
func (mc *ModelConfig) ResolveValue(requested string) (string, bool) {
	req := strings.TrimSpace(requested)
	if req == "" {
		return "", false
	}
	for _, m := range mc.Available {
		if m.ID == req {
			return m.ID, true
		}
	}
	for _, m := range mc.Available {
		if strings.EqualFold(m.Name, req) {
			return m.ID, true
		}
	}
	return "", false
}
