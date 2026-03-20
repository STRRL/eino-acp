# Research Topic: Passive/Token-Provider Mode for ACP Agents

## Problem Statement

When using ACP agents (Claude Code, Codex CLI, etc.) through orchestration frameworks like eino ADK, there are scenarios where the **orchestrator** should control the tool execution loop, not the agent. Currently ACP only supports an autonomous mode where the agent decides and executes tools internally.

We need a way to use ACP agents as **token providers** (LLM-only mode) — the agent generates text and proposes tool calls, but does NOT execute them. Instead, tool calls are returned to the client for execution, and results are sent back.

## Why This Matters

- **Orchestrator-managed tools**: eino ADK, LangChain, etc. have their own tool registries, middleware, and execution pipelines (auth, rate limiting, logging, sandboxing). ACP's autonomous execution bypasses all of that.
- **Hybrid tool sets**: Some tools come from the agent (file editing, code search), others from the orchestrator (databases, APIs, custom business logic). Need to mix both.
- **Observability**: When the orchestrator executes tools, it has full visibility into each tool call's timing, input, output, and errors. ACP's internal execution only surfaces results after the fact.
- **Safety/approval**: Orchestrator may need to approve, modify, or reject tool calls before execution, beyond ACP's simple `RequestPermission` mechanism.
- **Testing/replay**: With tool calls returned to the caller, they can be mocked, recorded, and replayed for testing.

## Current State

### ACP Protocol

ACP has no mechanism for this. `session/prompt` triggers autonomous agent execution. The agent controls its own tool loop and only notifies the client via `SessionUpdate` after tools have already been executed.

### Claude Code

Research needed:
- Does Claude Code's CLI have a `--no-tools` or `--tool-mode=passive` flag?
- Can Claude Code be started in a mode where it only generates responses without executing tools?
- Is there a way to provide tool definitions externally and get tool calls back?
- What does `claude code --print` mode do — is it LLM-only?
- How does the VS Code extension handle tool approval — is there an API-level hook?

### Codex CLI (OpenAI)

Research needed:
- Does Codex CLI support an "advisory" mode where it proposes but doesn't execute?
- What approval modes does it have? (`auto`, `suggest`, `ask` — does `suggest` return tool calls?)
- Can it be used as a pure completion engine?

### Anthropic API (direct)

- The Anthropic Messages API natively supports this pattern: send messages + tool definitions → get tool_use blocks back → execute locally → send tool_result → continue
- This is the "standard" LLM API pattern, but loses ACP's agent capabilities (session management, file context, MCP servers)

### Other Agent Protocols

Research needed:
- Does MCP (Model Context Protocol) have a concept of delegated tool execution?
- How does OpenAI's Assistants API handle this? (It has a `requires_action` status where the client must submit tool outputs)
- Google's A2A (Agent-to-Agent) protocol — any relevant patterns?

## Possible Approaches

### Approach 1: ACP Protocol Extension

Add a field to `PromptRequest` or `NewSessionRequest`:

```jsonc
{
  "sessionId": "...",
  "prompt": [...],
  "toolExecution": "autonomous" | "delegated"
  // "autonomous" = current behavior (default)
  // "delegated" = return tool calls to client, wait for results
}
```

When `delegated`:
- Agent sends `ToolCall` update as usual
- But does NOT execute the tool
- Client executes the tool and sends result back via a new method (e.g., `session/tool_result`)
- Agent continues with the result

**Pros**: Clean protocol-level solution. Works with any ACP agent.
**Cons**: Requires ACP spec change + agent implementation changes.

### Approach 2: Agent-Level Configuration

Configure the specific agent (Claude Code, Codex) to not execute tools:
- Claude Code: launch with flags that disable tool execution
- Codex: use an approval mode that returns tool calls

**Pros**: No protocol change needed.
**Cons**: Agent-specific, not portable across ACP agents.

### Approach 3: RequestPermission Hijacking

Use ACP's existing `RequestPermission` callback to intercept every tool call:
1. Agent wants to execute a tool → sends `RequestPermission`
2. Client denies permission but records the tool call intent
3. Client executes the tool externally
4. ???  — no way to send the result back to the agent

**Pros**: Works with current protocol.
**Cons**: Fundamentally broken — there's no mechanism to return tool results to the agent after denying permission. The agent would just give up or try something else.

### Approach 4: Dual-Mode Adapter in eino-acp

eino-acp could support two modes:
1. **ACP mode** (current): agent manages tools autonomously
2. **API mode**: bypass ACP, use the underlying LLM's API directly (e.g., Anthropic API for Claude)

When the caller needs orchestrator-managed tools, switch to API mode.

**Pros**: Works today with no protocol changes.
**Cons**: Loses ACP session features (MCP servers, file context, agent-specific capabilities). Essentially two different implementations behind one interface.

## Questions to Answer

1. **Claude Code**: Can it operate in a mode where tool calls are proposed but not executed? What CLI flags or environment variables control this?
2. **Codex CLI**: Same question — does `suggest` mode or any other flag prevent tool execution?
3. **ACP community**: Is there existing discussion or proposals around delegated tool execution? Check GitHub issues/discussions on the ACP spec repo.
4. **Practical hybrid**: Could we do a hybrid where some tool kinds (read, search) are executed by the agent, but others (execute, edit) are delegated to the client?
5. **MCP angle**: Since ACP agents can connect to MCP servers, could we inject an MCP server that acts as a proxy — receiving tool calls from the agent and forwarding them to the orchestrator?

## Related Links

- ACP spec: https://agentclientprotocol.com/
- ACP Go SDK: https://github.com/coder/acp-go-sdk
- eino: https://github.com/cloudwego/eino
- eino-acp: https://github.com/strrl/eino-acp
- OpenAI Assistants API (requires_action pattern): https://platform.openai.com/docs/assistants/how-it-works/runs-and-run-steps
