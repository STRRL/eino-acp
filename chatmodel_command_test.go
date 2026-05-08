package einoacp

import (
	"context"
	"testing"
)

func TestCommandContextUsesSessionCwd(t *testing.T) {
	cm := &ChatModel{
		command: []string{"echo", "ok"},
		cwd:     "/tmp/haye-workspace",
		env:     []string{"EXTRA=1"},
	}

	cmd := cm.commandContext(context.Background())

	if cmd.Dir != "/tmp/haye-workspace" {
		t.Fatalf("cmd.Dir = %q", cmd.Dir)
	}
}
