// Example demonstrates how to use the ACP chat model with Eino.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	einoacp "github.com/strrl/eino-acp"
)

func main() {
	ctx := context.Background()

	cm, err := einoacp.NewChatModel(ctx, &einoacp.Config{
		Command:     einoacp.ClaudeCommand(),
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
