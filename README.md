# sclaw

A plugin-first, self-hosted personal AI assistant. The core does almost nothing by design — every capability (messaging channels, LLM providers, agent tools, memory, cron, hooks) is a pluggable module.

## Philosophy

Inspired by [Caddy](https://caddyserver.com/)'s modular architecture: modules are compiled into a single binary with no inter-process communication. Adding a Telegram channel, an Ollama provider, or a cron job never requires touching the core.

The core handles module lifecycle, message routing, and config loading. Nothing else.

## Architecture

```
cmd/sclaw/main.go   → Entry point
internal/           → Private packages
pkg/                → Public reusable packages
docs/               → Documentation
```

### Core responsibilities

- **Module registry** — Global registry with hierarchical IDs (`channel.telegram`, `provider.anthropic`, `memory.sqlite`)
- **Router** — Session management, message dispatch, agent resolution, hook pipeline
- **Agent loop** — ReAct pattern (Reason + Act) with guardrails (max iterations, loop detection, token budget)
- **Context engine** — Dynamic system prompt assembly, compaction, context window management
- **Model failover** — Provider chain with auth profiles, cooldown, and health checks
- **Streaming** — Provider-to-channel bridge with typing indicators
- **Tool approvals** — Granular allow/ask/deny policy per tool

### Module categories

| Category | Interface | Examples |
|----------|-----------|---------|
| Channels | `Channel` | Telegram, Discord, WhatsApp, WebChat |
| Providers | `Provider` | Anthropic, OpenAI, Ollama |
| Tools | `Tool` | Web search, shell, file read/write |
| Memory | `MemoryStore` | SQLite, LanceDB, PostgreSQL |
| Hooks | `Hook` | Rate limiting, logging, audit |
| Cron | `CronJob` | Session cleanup, memory extraction |
| Skills | `Skill` | Web research, code review (Markdown-based) |

## Configuration

sclaw is config-driven. Describe your assistant in YAML:

```yaml
providers:
  - module: provider.anthropic
    model: claude-sonnet-4-5-20250929
    role: primary
  - module: provider.ollama
    model: llama3.1
    role: fallback

channels:
  - module: channel.telegram
    token: "${TELEGRAM_BOT_TOKEN}"
    allow_from:
      - "user:123456789"

memory:
  session_store:
    module: memory.sqlite
  long_term:
    module: memory.sqlite

agents:
  - id: main
    workspace: "${HOME}/.assistant/agents/main"
    tools:
      - module: tool.web_search
      - module: tool.shell
    tool_policy:
      default: ask
      allow: [tool.web_search]
      deny: [tool.file_delete]
```

## Custom builds

Add or remove modules with `xsclaw` (similar to `xcaddy`):

```bash
# Default plugins + a custom one
xsclaw build \
  --plugin=github.com/user/my-tool \
  --output ./sclaw

# Minimal build: just Telegram + Anthropic
xsclaw build \
  --only=channel.telegram \
  --only=provider.anthropic \
  --output ./sclaw
```

## Development

### Prerequisites

- **Nix + direnv** (recommended): `direnv allow` in the project root
- Or manually install: Go 1.25+, golangci-lint, goreleaser

### Commands

```bash
direnv allow           # Load Nix dev environment
go build ./cmd/sclaw   # Build binary
lint                   # Run golangci-lint
test                   # Run tests with race detector + coverage
```

### Code conventions

- Code, comments, and variable names in **English**
- Formatting: **gofumpt** (enforced by linter)
- Commits: **Conventional Commits** (`feat:`, `fix:`, `chore:`)

## Status

Early development. The core module system and interfaces are being designed. See [VISION.md](VISION.md) for the full architectural vision.

## License

MIT
