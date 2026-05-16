package einoacp

import (
	"reflect"
	"testing"
)

func TestCopilotCommandUsesPackageRunnerAndACPMode(t *testing.T) {
	prefix := dlxPrefix()
	want := append(append([]string{}, prefix...), "@github/copilot", "--acp")

	if got := CopilotCommand(); !reflect.DeepEqual(got, want) {
		t.Fatalf("CopilotCommand() = %#v, want %#v", got, want)
	}
}
