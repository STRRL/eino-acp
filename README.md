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
		// Claude Code
		Command: []string{"npx", "-y", "@zed-industries/claude-agent-acp@latest"},
		// Codex CLI
		// Command: []string{"codex", "--acp"},
		// Gemini CLI
		// Command: []string{"gemini", "--experimental-acp"},
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
| `Command` | Command to launch the ACP agent. First element is the binary, rest are args. **Required.** |
| `Cwd` | Working directory for the agent session. Defaults to current directory. |
| `Env` | Additional environment variables for the agent subprocess. |
| `AutoApprove` | Auto-approve all permission requests from the agent. Default `false`. |

## How it works

1. Launches the agent as a subprocess
2. Communicates via [ACP](https://agentclientprotocol.com/) (JSON-RPC 2.0 over stdio)
3. Maps ACP session updates to Eino's `schema.Message` streaming interface

Built on [coder/acp-go-sdk](https://github.com/coder/acp-go-sdk).

## License

MIT
