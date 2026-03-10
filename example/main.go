// Example demonstrates how to use the ACP chat model with Eino.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	einoacp "github.com/strrl/eino-acp"
)

func main() {
	provider := flag.String("provider", "claude", "ACP provider to use: claude, codex")
	flag.Parse()

	var command []string
	switch *provider {
	case "claude":
		command = einoacp.ClaudeCommand()
	case "codex":
		command = einoacp.CodexCommand()
	default:
		fmt.Fprintf(os.Stderr, "unknown provider: %s (supported: claude, codex)\n", *provider)
		os.Exit(1)
	}

	ctx := context.Background()

	cm, err := einoacp.NewChatModel(ctx, &einoacp.Config{
		Command:     command,
		AutoApprove: false,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("> ")
	input, err := reader.ReadString('\n')
	if err != nil {
		fmt.Fprintf(os.Stderr, "read error: %v\n", err)
		os.Exit(1)
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return
	}

	stream, err := cm.Stream(ctx, einoacp.UserMessages(input))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
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
