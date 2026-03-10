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
	"github.com/cloudwego/eino/schema"
	acp "github.com/coder/acp-go-sdk"
)

var _ model.ChatModel = (*ChatModel)(nil)

// Config for the ACP-based chat model.
type Config struct {
	// Command is the full command to launch the ACP agent.
	// The first element is the binary, the rest are arguments.
	// Examples:
	//   Claude Code: []string{"npx", "-y", "@zed-industries/claude-agent-acp@latest"}
	//   Codex CLI:   []string{"codex", "--acp"}
	//   Gemini CLI:  []string{"gemini", "--experimental-acp"}
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
}

// ChatModel implements eino's model.ChatModel by communicating with
// any ACP-compatible coding agent (Claude Code, Codex CLI, Gemini CLI, etc.)
// over the Agent Client Protocol.
type ChatModel struct {
	command     []string
	cwd         string
	env         []string
	autoApprove bool
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
		command:     config.Command,
		cwd:         cwd,
		env:         config.Env,
		autoApprove: config.AutoApprove,
	}, nil
}

func (cm *ChatModel) GetType() string {
	return "ChatModel/ACP"
}

func (cm *ChatModel) IsCallbacksEnabled() bool {
	return true
}

// BindTools is a no-op; ACP agents manage their own tools.
func (cm *ChatModel) BindTools(_ []*schema.ToolInfo) error {
	return nil
}

func (cm *ChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (outMsg *schema.Message, err error) {
	ctx = callbacks.EnsureRunInfo(ctx, cm.GetType(), components.ComponentOfChatModel)
	ctx = callbacks.OnStart(ctx, cm.getCallbackInput(input))
	defer func() {
		if err != nil {
			callbacks.OnError(ctx, err)
		}
	}()

	var mu sync.Mutex
	var textParts []string

	onUpdate := func(u acp.SessionUpdate) {
		if u.AgentMessageChunk != nil && u.AgentMessageChunk.Content.Text != nil {
			mu.Lock()
			textParts = append(textParts, u.AgentMessageChunk.Content.Text.Text)
			mu.Unlock()
		}
	}

	if err = cm.runPrompt(ctx, input, onUpdate); err != nil {
		return nil, err
	}

	outMsg = &schema.Message{
		Role:    schema.Assistant,
		Content: strings.Join(textParts, ""),
	}

	callbacks.OnEnd(ctx, cm.getCallbackOutput(outMsg))
	return outMsg, nil
}

func (cm *ChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (outStream *schema.StreamReader[*schema.Message], err error) {
	ctx = callbacks.EnsureRunInfo(ctx, cm.GetType(), components.ComponentOfChatModel)
	ctx = callbacks.OnStart(ctx, cm.getCallbackInput(input))
	defer func() {
		if err != nil {
			callbacks.OnError(ctx, err)
		}
	}()

	sr, sw := schema.Pipe[*model.CallbackOutput](1)

	onUpdate := func(u acp.SessionUpdate) {
		if u.AgentMessageChunk != nil && u.AgentMessageChunk.Content.Text != nil {
			msg := &schema.Message{
				Role:    schema.Assistant,
				Content: u.AgentMessageChunk.Content.Text.Text,
			}
			_ = sw.Send(&model.CallbackOutput{Message: msg}, nil)
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
	cmd := exec.CommandContext(ctx, cm.command[0], cm.command[1:]...)
	cmd.Env = cm.buildEnv()
	cmd.Stderr = os.Stderr

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
		onUpdate:    onUpdate,
		autoApprove: cm.autoApprove,
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

	sess, err := conn.NewSession(ctx, acp.NewSessionRequest{
		Cwd:        cm.cwd,
		McpServers: []acp.McpServer{},
	})
	if err != nil {
		return fmt.Errorf("ACP new session: %w", err)
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

func (cm *ChatModel) getCallbackOutput(msg *schema.Message) *model.CallbackOutput {
	return &model.CallbackOutput{
		Message: msg,
		Config:  &model.Config{},
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
