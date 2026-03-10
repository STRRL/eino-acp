package main

import (
	"context"
	"fmt"
	"os"

	einoacp "github.com/strrl/eino-acp"
)

func main() {
	ctx := context.Background()

	// Use any ACP-compatible agent: "claude", "codex", "gemini"
	binary := "claude"
	if len(os.Args) > 1 {
		binary = os.Args[1]
	}

	cm, err := einoacp.NewChatModel(ctx, &einoacp.Config{
		Binary:      binary,
		AutoApprove: false,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// --- Generate (non-streaming) ---
	fmt.Println("=== Generate ===")
	msg, err := cm.Generate(ctx, einoacp.UserMessages("Say hello in one sentence."))
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(msg.Content)

	// --- Stream ---
	fmt.Println("\n=== Stream ===")
	stream, err := cm.Stream(ctx, einoacp.UserMessages("Tell me a short joke."))
	if err != nil {
		fmt.Fprintf(os.Stderr, "stream error: %v\n", err)
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
