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
func CodexCommand() []string {
	return append(dlxPrefix(), "@openai/codex@latest", "--acp")
}

// GeminiCommand returns the command to launch Google Gemini CLI via ACP.
func GeminiCommand() []string {
	return append(dlxPrefix(), "@google/gemini-cli@latest", "--experimental-acp")
}
