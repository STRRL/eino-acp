# eino-acp

Eino ChatModel provider that connects to any [ACP (Agent Client Protocol)](https://github.com/agentclientprotocol/agent-client-protocol) compatible coding agent.

Use Claude Code, Codex CLI, Gemini CLI, or any other ACP agent as an LLM backend for [CloudWeGo Eino](https://github.com/cloudwego/eino).

## Install

```bash
go get github.com/strrl/eino-acp
```

## Usage

```go
package main

import (
	"context"
	"fmt"

	einoacp "github.com/strrl/eino-acp"
)

func main() {
	ctx := context.Background()

	cm, err := einoacp.NewChatModel(ctx, &einoacp.Config{
		Command: einoacp.ClaudeCommand(),
		// or: einoacp.CodexCommand()
		// or: einoacp.GeminiCommand()
	})
	if err != nil {
		panic(err)
	}

	// Generate (non-streaming)
	msg, err := cm.Generate(ctx, einoacp.UserMessages("Hello!"))
	if err != nil {
		panic(err)
	}
	fmt.Println(msg.Content)

	// Stream
	stream, err := cm.Stream(ctx, einoacp.UserMessages("Tell me a joke."))
	if err != nil {
		panic(err)
	}
	defer stream.Close()

	for {
		chunk, err := stream.Recv()
		if err != nil {
			break
		}
		fmt.Print(chunk.Content)
	}
	fmt.Println()
}
```

## Config

| Field | Description |
|-------|-------------|
| `Command` | Command to launch the ACP agent. Use `ClaudeCommand()`, `CodexCommand()`, or `GeminiCommand()` helpers, which auto-detect `pnpm dlx` (preferred) or `npx`. **Required.** |
| `Cwd` | Working directory for the agent session. Defaults to current directory. |
| `Env` | Additional environment variables for the agent subprocess. |
| `AutoApprove` | Auto-approve all permission requests from the agent. Default `false`. |

## How it works

1. Launches the agent as a subprocess
2. Communicates via [ACP](https://agentclientprotocol.com/) (JSON-RPC 2.0 over stdio)
3. Maps ACP session updates to Eino's `schema.Message` streaming interface

## Tool Call Mapping

`eino-acp` translates ACP `tool_call` and `tool_call_update` session updates into Eino assistant `ToolCalls`.

| ACP field | Eino field | Notes |
|-------|-------------|-------|
| `toolCallId` | `ToolCall.ID` | Preserved as-is. |
| `kind` | `ToolCall.Function.Name` | Uses ACP tool kind such as `read`, `edit`, or `execute`. Falls back to `other` when ACP omits the kind. |
| `rawInput` | `ToolCall.Function.Arguments` | JSON-marshaled into a stable string. Defaults to `{}` when missing. |
| `title`, `status`, `rawOutput`, `locations`, `content`, `_meta` | `ToolCall.Extra` | Preserved under `acp_*` keys so observability consumers can inspect ACP-specific metadata. |

For `Generate`, the final assistant message includes the accumulated `ToolCalls`.

For `Stream`, each ACP tool event is emitted as an assistant chunk with empty `Content` and a single `ToolCall`, so consumers such as tape, Langfuse-style handlers, or ADK can observe tool activity as it happens.

`agent_thought_chunk`, `plan`, and other non-message ACP session updates are still ignored.

Built on [coder/acp-go-sdk](https://github.com/coder/acp-go-sdk).

## License

MIT
