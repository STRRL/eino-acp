package einoacp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	acp "github.com/coder/acp-go-sdk"
)

var _ model.ToolCallingChatModel = (*ChatModel)(nil)

// Config for the ACP-based chat model.
type Config struct {
	// Command is the full command to launch the ACP agent.
	// The first element is the binary, the rest are arguments.
	// Use the provided helpers: ClaudeCommand(), CodexCommand().
	// Required, must have at least one element.
	Command []string

	// Cwd is the working directory for the agent session.
	// Defaults to the current working directory.
	Cwd string

	// Env sets additional environment variables for the agent subprocess.
	// These are merged with the current process environment.
	Env []string

	// AutoApprove automatically approves all permission requests from the agent.
	// When false, permission requests are denied.
	AutoApprove bool

	// OnPermission is called for every ACP permission request. When set, it
	// takes precedence over AutoApprove.
	OnPermission func(context.Context, acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error)

	// OnSessionUpdate is called for every ACP SessionUpdate received during
	// execution. This fires in real-time regardless of whether Generate() or
	// Stream() is used, giving consumers access to tool calls, text chunks,
	// and other ACP events as they happen.
	OnSessionUpdate func(acp.SessionUpdate)

	// SelectModel is the ACP model id to switch to via SetSessionModel
	// after each NewSession (the protocol-native way to pick a model
	// for a session). Empty = use the agent's default. Pass --model
	// on the spawn command line is unreliable: Claude Code ignores it
	// in --acp mode and currentModelId stays "default" regardless;
	// SetSessionModel is the only way to actually switch.
	//
	// Per-CLI valid ids:
	//   Claude Code: "default" / "sonnet" / "haiku"
	//   GitHub Copilot: "auto" / "gpt-5.4" / "gpt-5.3-codex" / ...
	// Discoverable via the OnSessionInfo callback below.
	SelectModel string

	// OnSessionInfo fires once per NewSession with the model + session
	// id information the agent reports back. The SessionModelState
	// contains the *resolved* currentModelId (e.g. after applying
	// SelectModel, or whatever the agent's default was) and the full
	// list of availableModels with human-readable names + descriptions.
	//
	// Use this to (a) surface the actual running model to a UI rather
	// than the alias the user picked, and (b) populate a model picker
	// dynamically without hardcoding a per-CLI list.
	//
	// Called BEFORE the first prompt is sent on the new session, so
	// callers can react to the model state synchronously if needed.
	OnSessionInfo func(sessionId acp.SessionId, models *acp.SessionModelState)

	// McpServers is the list of MCP servers to attach to each ACP session.
	// Claude Code will discover and use tools from these servers.
	McpServers []acp.McpServer
}

// ChatModel implements eino's model.ChatModel by communicating with
// any ACP-compatible coding agent (Claude Code, Codex CLI, etc.)
// over the Agent Client Protocol.
type ChatModel struct {
	command         []string
	cwd             string
	env             []string
	autoApprove     bool
	onPermission    func(context.Context, acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error)
	onSessionUpdate func(acp.SessionUpdate)
	selectModel     string
	onSessionInfo   func(acp.SessionId, *acp.SessionModelState)
	mcpServers      []acp.McpServer
}

// NewChatModel creates a new ACP chat model.
func NewChatModel(_ context.Context, config *Config) (*ChatModel, error) {
	if len(config.Command) == 0 {
		return nil, fmt.Errorf("command is required")
	}

	if _, err := exec.LookPath(config.Command[0]); err != nil {
		return nil, fmt.Errorf("binary %q not found: %w", config.Command[0], err)
	}

	cwd := config.Cwd
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get working directory: %w", err)
		}
	}

	return &ChatModel{
		command:         config.Command,
		cwd:             cwd,
		env:             config.Env,
		autoApprove:     config.AutoApprove,
		onPermission:    config.OnPermission,
		onSessionUpdate: config.OnSessionUpdate,
		selectModel:     config.SelectModel,
		onSessionInfo:   config.OnSessionInfo,
		mcpServers:      config.McpServers,
	}, nil
}

// GetType returns the component type name used by Eino callbacks.
func (cm *ChatModel) GetType() string {
	return "ChatModel/ACP"
}

// IsCallbacksEnabled reports whether callback hooks are enabled for this model.
func (cm *ChatModel) IsCallbacksEnabled() bool {
	return true
}

// BindTools is a no-op; ACP agents manage their own tools.
func (cm *ChatModel) BindTools(_ []*schema.ToolInfo) error {
	return nil
}

// WithTools is a no-op; ACP agents manage their own tools.
func (cm *ChatModel) WithTools(_ []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return cm, nil
}

// Generate is not supported for ACP-based chat models.
//
// ACP is a streaming-only protocol: the agent emits ordered session updates
// (tool_call → tool_call_update → agent_message_chunk) that must be consumed
// in real time. Collapsing them into a single *schema.Message via Generate
// destroys event ordering and streaming granularity.
//
// Use Stream instead.
func (cm *ChatModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	return nil, fmt.Errorf("einoacp: Generate is not supported — ACP is a streaming-only protocol; use Stream instead")
}

// Stream runs the ACP agent and yields assistant output chunks as they arrive.
func (cm *ChatModel) Stream(ctx context.Context, input []*schema.Message, _ ...model.Option) (outStream *schema.StreamReader[*schema.Message], err error) {
	ctx = callbacks.EnsureRunInfo(ctx, cm.GetType(), components.ComponentOfChatModel)
	ctx = callbacks.OnStart(ctx, cm.getCallbackInput(input))
	defer func() {
		if err != nil {
			callbacks.OnError(ctx, err)
		}
	}()

	sr, sw := schema.Pipe[*model.CallbackOutput](1)
	var mu sync.Mutex
	collector := newResponseCollector()
	toolCtxMap := make(map[string]context.Context)

	onUpdate := func(u acp.SessionUpdate) {
		mu.Lock()
		defer mu.Unlock()

		// Fire tool OnStart/OnEnd callbacks (same logic as Generate)
		if u.ToolCall != nil {
			toolID := string(u.ToolCall.ToolCallId)
			toolName := string(u.ToolCall.Kind)
			if toolName == "" {
				toolName = u.ToolCall.Title
			}
			toolCtx := callbacks.ReuseHandlers(ctx, &callbacks.RunInfo{
				Name:      toolName,
				Type:      "Tool/ACP",
				Component: components.ComponentOfTool,
			})
			toolCtx = callbacks.OnStart(toolCtx, &tool.CallbackInput{
				ArgumentsInJSON: marshalACPValue(u.ToolCall.RawInput, "{}"),
				Extra: map[string]any{
					"acp_title":        u.ToolCall.Title,
					"acp_kind":         string(u.ToolCall.Kind),
					"acp_tool_call_id": toolID,
				},
			})
			toolCtxMap[toolID] = toolCtx
		}
		if u.ToolCallUpdate != nil {
			toolID := string(u.ToolCallUpdate.ToolCallId)
			if tCtx, ok := toolCtxMap[toolID]; ok {
				status := acp.ToolCallStatusCompleted
				if u.ToolCallUpdate.Status != nil {
					status = *u.ToolCallUpdate.Status
				}
				switch status {
				case acp.ToolCallStatusCompleted:
					callbacks.OnEnd(tCtx, &tool.CallbackOutput{
						Response: marshalACPValue(u.ToolCallUpdate.RawOutput, ""),
						Extra:    map[string]any{"acp_tool_call_id": toolID},
					})
					delete(toolCtxMap, toolID)
				case acp.ToolCallStatusFailed:
					callbacks.OnError(tCtx, fmt.Errorf("tool %s failed", toolID))
					delete(toolCtxMap, toolID)
				}
			}
		}

		textChunk, toolChunk := collector.handleUpdate(u)
		if textChunk != nil {
			_ = sw.Send(&model.CallbackOutput{Message: textChunk}, nil)
		}
		if toolChunk != nil {
			_ = sw.Send(&model.CallbackOutput{Message: toolChunk}, nil)
		}
	}

	go func() {
		defer func() {
			if pe := recover(); pe != nil {
				_ = sw.Send(nil, fmt.Errorf("panic: %v\n%s", pe, debug.Stack()))
			}
			sw.Close()
		}()

		if err := cm.runPrompt(ctx, input, onUpdate); err != nil {
			_ = sw.Send(nil, err)
		}
	}()

	_, sr = callbacks.OnEndWithStreamOutput(ctx, sr)
	outStream = schema.StreamReaderWithConvert(sr, func(t *model.CallbackOutput) (*schema.Message, error) {
		if t.Message == nil {
			return nil, schema.ErrNoValue
		}
		return t.Message, nil
	})

	return outStream, nil
}

// runPrompt launches the agent subprocess, runs the ACP protocol flow,
// and calls onUpdate for each session update received.
func (cm *ChatModel) runPrompt(ctx context.Context, input []*schema.Message, onUpdate func(acp.SessionUpdate)) error {
	// Wrap onUpdate to also fire the user-provided OnSessionUpdate callback
	if cm.onSessionUpdate != nil {
		inner := onUpdate
		onUpdate = func(u acp.SessionUpdate) {
			cm.onSessionUpdate(u)
			inner(u)
		}
	}

	cmd := cm.commandContext(ctx)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start agent: %w", err)
	}

	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	client := &acpClient{
		onUpdate:     onUpdate,
		autoApprove:  cm.autoApprove,
		onPermission: cm.onPermission,
	}

	conn := acp.NewClientSideConnection(client, stdin, stdout)

	_, err = conn.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs: acp.FileSystemCapability{
				ReadTextFile:  true,
				WriteTextFile: true,
			},
			Terminal: true,
		},
	})
	if err != nil {
		return fmt.Errorf("ACP initialize: %w", err)
	}

	mcpServers := cm.mcpServers
	if mcpServers == nil {
		mcpServers = []acp.McpServer{}
	}
	sess, err := conn.NewSession(ctx, acp.NewSessionRequest{
		Cwd:        cm.cwd,
		McpServers: mcpServers,
	})
	if err != nil {
		return fmt.Errorf("ACP new session: %w", err)
	}

	// Switch to the caller-requested model BEFORE firing
	// OnSessionInfo. SetSessionModel is the protocol-native way to
	// pick a model for a session; `--model <id>` on the spawn command
	// line is silently ignored by Claude Code in --acp mode.
	//
	// We mutate sess.Models.CurrentModelId after a successful switch
	// so the OnSessionInfo callback sees the post-switch state. The
	// ACP protocol does not echo a fresh SessionModelState back from
	// SetSessionModel (the response is empty); we trust the protocol —
	// if no error, the requested model is now in effect.
	if cm.selectModel != "" {
		if _, err := conn.SetSessionModel(ctx, acp.SetSessionModelRequest{
			SessionId: sess.SessionId,
			ModelId:   acp.ModelId(cm.selectModel),
		}); err != nil {
			return fmt.Errorf("ACP set session model %q: %w", cm.selectModel, err)
		}
		if sess.Models != nil {
			sess.Models.CurrentModelId = acp.ModelId(cm.selectModel)
		}
	}

	// Now surface the (possibly post-switch) model state to the
	// caller — currentModelId + availableModels. Callers use this
	// to populate UI / discover models dynamically and to verify
	// that what they asked for is what got applied. See
	// Config.OnSessionInfo doc for the rationale.
	if cm.onSessionInfo != nil {
		cm.onSessionInfo(sess.SessionId, sess.Models)
	}

	prompt := messagesToContentBlocks(input)

	_, err = conn.Prompt(ctx, acp.PromptRequest{
		SessionId: sess.SessionId,
		Prompt:    prompt,
	})
	if err != nil {
		return fmt.Errorf("ACP prompt: %w", err)
	}

	return nil
}

func (cm *ChatModel) commandContext(ctx context.Context) *exec.Cmd {
	cmd := exec.CommandContext(ctx, cm.command[0], cm.command[1:]...)
	cmd.Dir = cm.cwd
	cmd.Env = cm.buildEnv()
	cmd.Stderr = os.Stderr
	return cmd
}

func (cm *ChatModel) buildEnv() []string {
	env := os.Environ()
	filtered := make([]string, 0, len(env)+len(cm.env))
	for _, e := range env {
		if !strings.HasPrefix(e, "CLAUDECODE=") {
			filtered = append(filtered, e)
		}
	}
	filtered = append(filtered, cm.env...)
	return filtered
}

func (cm *ChatModel) getCallbackInput(input []*schema.Message) *model.CallbackInput {
	return &model.CallbackInput{
		Messages: input,
		Config:   &model.Config{},
	}
}

func messagesToContentBlocks(messages []*schema.Message) []acp.ContentBlock {
	var parts []string
	for _, msg := range messages {
		prefix := ""
		switch msg.Role {
		case schema.System:
			prefix = "[System] "
		case schema.User:
			// no prefix
		case schema.Assistant:
			prefix = "[Assistant] "
		case schema.Tool:
			prefix = "[Tool Result] "
		}
		parts = append(parts, prefix+msg.Content)
	}
	return []acp.ContentBlock{acp.TextBlock(strings.Join(parts, "\n\n"))}
}
