package einoacp

import "os/exec"

// dlxPrefix returns the preferred package runner command.
// It prefers "pnpm dlx" over "npx -y" for faster cold-start performance.
func dlxPrefix() []string {
	if _, err := exec.LookPath("pnpm"); err == nil {
		return []string{"pnpm", "dlx"}
	}
	return []string{"npx", "-y"}
}

// ClaudeCommand returns the command to launch Claude Code via ACP.
func ClaudeCommand() []string {
	return append(dlxPrefix(), "@zed-industries/claude-agent-acp@latest")
}

// CodexCommand returns the command to launch OpenAI Codex CLI via ACP.
// Uses the dedicated ACP adapter package from the ACP registry.
func CodexCommand() []string {
	return append(dlxPrefix(), "@zed-industries/codex-acp@latest")
}

// GeminiCommand returns the command to launch Google Gemini CLI via ACP.
//
// NOTE: Gemini CLI in ACP mode currently does not work with OAuth authentication
// when launched as a subprocess. See upstream issue:
// https://github.com/google-gemini/gemini-cli/issues/12042
//
// Deprecated: Use ClaudeCommand or CodexCommand instead until this is resolved.
func GeminiCommand() []string {
	if _, err := exec.LookPath("gemini"); err == nil {
		return []string{"gemini", "--experimental-acp"}
	}
	return append(dlxPrefix(), "@google/gemini-cli@latest", "--experimental-acp")
}
