# Agent Personality — SOUL.md

## Overview

Every sclaw agent can have a unique personality defined in a `SOUL.md` file. This Markdown file acts as the agent's system prompt — the instructions that shape how the agent thinks, communicates, and behaves. It is injected at the beginning of every LLM request as the system message.

SOUL.md enables operators to customize agent tone, role, constraints, and behavior without changing code or restarting the process. Changes take effect on the next message the agent processes.

## File Location

Each agent reads its personality from:

```
{data_dir}/SOUL.md
```

Where `{data_dir}` is the agent's configured `data_dir` field. When `data_dir` is not explicitly set, it defaults to:

```
{global_data_dir}/agents/{agent_name}/SOUL.md
```

For example, with the default data directory and an agent named `support`:

```
data/agents/support/SOUL.md
```

The path is resolved in `internal/multiagent/factory.go` via `ResolveSoul()`, which constructs `filepath.Join(agentCfg.DataDir, "SOUL.md")`.

## How It Works

The SOUL.md system uses stat-based cache invalidation for hot-reload without restart.

### Loading Flow

1. **On each inbound message**, the router pipeline calls `SoulResolver.ResolveSoul(agentID)`.
2. The `Factory` lazily creates a `SoulLoader` for the agent on first access and caches it.
3. `SoulLoader.Load()` calls `os.Stat()` on the file (~1 microsecond, negligible compared to an LLM call).
4. If the file's modification time has not changed, the cached content is returned via the `RLock` fast path.
5. If the modification time has changed, the file is re-read and the cache is updated under a write lock.

### Concurrency

`SoulLoader` is safe for concurrent use. It uses a `sync.RWMutex` to separate the fast read path (unchanged file) from the slow write path (file changed or first load). Multiple goroutines can read the cached content simultaneously without contention.

### Pipeline Integration

The system prompt is resolved in Step 9 of the message pipeline (`internal/router/pipeline.go`):

```go
systemPrompt := workspace.DefaultSoulPrompt
if p.cfg.SoulResolver != nil && session.AgentID != "" {
    if s, err := p.cfg.SoulResolver.ResolveSoul(session.AgentID); err == nil {
        systemPrompt = s
    }
}
```

If the `SoulResolver` is nil or the agent has no ID, the pipeline falls back to the default prompt. If loading fails, a warning is logged and the default prompt is used — the agent never crashes due to a missing or unreadable SOUL.md.

## Default Behavior

When SOUL.md is **missing**, **empty**, or **contains only whitespace**, the agent uses the default system prompt:

```
You are a helpful assistant.
```

This default is defined as `workspace.DefaultSoulPrompt` in `internal/workspace/soul.go`.

Specifically:
- **File does not exist** — returns default prompt, no error.
- **File is empty** (0 bytes) — returns default prompt, no error.
- **File contains only whitespace** (spaces, tabs, newlines) — returns default prompt, no error.
- **File exists with content** — content is trimmed with `strings.TrimSpace()` and returned.

## Writing Guide

### Structure

A SOUL.md file is free-form Markdown. There are no required sections or format constraints — the entire file content (after whitespace trimming) is used as the system prompt. That said, a well-structured personality file typically includes:

1. **Role definition** — Who the agent is and its primary purpose.
2. **Tone and style** — How it should communicate (formal, casual, terse, verbose).
3. **Constraints** — What it should never do or always do.
4. **Domain knowledge** — Context about its operating environment.
5. **Boundaries** — Privacy, authorization, and safety rules.

### Tips

- **Be specific about tone.** "Be concise" is better than "be professional." Show, don't tell.
- **State constraints as rules, not suggestions.** "Never share private data" is stronger than "try to keep things private."
- **Keep it focused.** The system prompt competes with conversation context for the model's attention budget. A 200-line manifesto dilutes the important parts.
- **Use Markdown formatting.** Headers, lists, and bold text help the model parse structure.
- **Iterate.** Edit the file and send another message — hot-reload lets you tune the personality in real time.
- **Test with edge cases.** Try adversarial prompts to verify your constraints hold.

## Example

A realistic SOUL.md for a DevOps support agent:

```markdown
# DevOps Support Agent

You are a senior DevOps engineer embedded in the platform team. Your job is to help developers debug infrastructure issues, review deployment configs, and explain system architecture.

## Communication Style

- Be direct and technical. Skip pleasantries.
- Use code blocks for commands, configs, and logs.
- When you don't know something, say so — don't guess at infrastructure.

## Rules

- Never run destructive commands (rm -rf, DROP TABLE, force-push) without explicit confirmation.
- Always check the runbook before suggesting manual interventions.
- Escalate security-related issues to #security-oncall immediately.

## Context

- Infrastructure runs on AWS (EKS, RDS, S3).
- CI/CD uses GitHub Actions with ArgoCD for deployments.
- Monitoring stack: Prometheus + Grafana + PagerDuty.
```

## Multi-Agent Setup

In a multi-agent configuration, each agent has its own `data_dir` and therefore its own SOUL.md. This allows different agents to have completely different personalities.

Example configuration:

```yaml
agents:
  support:
    data_dir: data/agents/support
    provider: anthropic
    routing:
      channels: ["#help-desk"]

  creative:
    data_dir: data/agents/creative
    provider: openai
    routing:
      channels: ["#brainstorm"]
```

This creates two independent personality files:

```
data/agents/support/SOUL.md   → professional, concise support persona
data/agents/creative/SOUL.md  → playful, creative brainstorming persona
```

Each agent's `SoulLoader` is lazily created on first message and cached for the process lifetime. Changes to either file are detected independently — updating one agent's personality does not affect the other.

When `data_dir` is omitted, it defaults to `{global_data_dir}/agents/{agent_name}`, so the SOUL.md path is automatically namespaced per agent.
