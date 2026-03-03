# sclaw — Project Documentation

## Overview

`sclaw` is a Go CLI tool.
Module path: `github.com/flemzord/sclaw`
Repository: `https://github.com/flemzord/sclaw`

## Architecture

```
cmd/sclaw/main.go     → Entry point
cmd/xsclaw/           → Build & distribution tooling
internal/             → Private packages (not importable)
  internal/agent/     → ReAct reasoning loop
  internal/bootstrap/ → Plugin hot-reload detection + rebuild
  internal/cert/      → Plugin certificate signing/verification
  internal/channel/   → Platform adapters (Telegram, Discord, etc.)
  internal/config/    → Configuration loading and validation
  internal/context/   → LLM context management (token budget, compaction)
  internal/core/      → Module system and registry
  internal/cron/      → Scheduled tasks
  internal/gateway/   → HTTP server (admin, webhooks, health)
  internal/hook/      → Message pipeline hooks (audit, filtering)
  internal/memory/    → Conversation history management
  internal/multiagent/→ Multi-agent configuration and routing
  internal/provider/  → Provider interface, failover chain, health tracking
  internal/reload/    → Hot configuration reload
  internal/router/    → Central message dispatch + session management
  internal/security/  → Security hardening (credentials, redaction, audit,
                        rate limiting, validation, env sanitization,
                        sandboxing, URL filtering)
  internal/subagent/  → Ephemeral sub-agent sessions
  internal/tool/      → Tool registry + approval system
  internal/tool/configtool/ → Runtime config tools (get, validate, patch, apply)
  internal/workspace/ → Working directory management
pkg/                  → Public reusable packages
  pkg/app/            → Shared entry point (Run, ResolveConfigPath)
  pkg/message/        → Platform-agnostic message model
skills/               → Embedded SKILL.md files (go:embed)
docs/                 → Additional documentation
  docs/security/      → Security documentation
```

## Development Environment

### Prerequisites

- **Nix** + **direnv** (recommended): `direnv allow` in the project root
- Or manually install: Go, golangci-lint, goreleaser

### Setup

```bash
direnv allow    # Loads shell.nix automatically
```

## Test / Lint

Commands are available directly in the Nix shell:

```bash
lint       # Run golangci-lint
run-tests  # Run tests with race detector + coverage
```

## Code Conventions

- **Language**: Code, comments, variable names, and docs in English
- **Formatting**: gofumpt (enforced by golangci-lint)
- **Project layout**: Follow standard Go project layout (cmd/, internal/, pkg/)
- **Commits**: Conventional Commits (`feat:`, `fix:`, `chore:`, `ci:`, `docs:`)

## Go Rules (from PR reviews)

### Function Design

- **Use request structs for 4+ parameters**: When a function takes more than 3 parameters (excluding `ctx`), group them into a dedicated request/options struct. This improves readability and makes future extensions backward-compatible.
- **Return errors, never panic**: Always return `error` for conditions that depend on caller input. Reserve `panic` only for true programming errors (violated invariants that should never happen). User-provided config should never cause a panic.

### Data & Performance

- **Prefer O(1) lookups**: Normalize data at the source (e.g., trim map keys during validation) so that resolution functions can use direct map lookups instead of iterating.
- **Pass large structs by pointer**: Types like `yaml.Node` that contain subtrees should be passed as `*yaml.Node` to avoid unnecessary copying.
- **Nil slices over empty slices**: Prefer `var candidates []string` over `candidates := []string{}`. Nil slices are more idiomatic in Go and work the same with `append`.
- **Bound append-only collections**: Any in-memory append-only collection (session history, logs, caches) must have a cap, truncation, or eviction strategy. Unbounded growth causes OOM and context window overflow.
- **Gate expensive safety nets**: If a safety-net check (like JSON parsing) runs on every message, gate it behind a cheap pre-check (e.g., `if msg.Raw != nil`) to avoid paying the cost on the common path.

### Concurrency

- **Encapsulate channels behind methods**: Never export raw channels. Expose a method with state validation (e.g., `Respond()` that checks if an approval is pending) to prevent misuse of concurrent primitives.
- **Defer health verdicts**: When tracking provider health, record success only after the full operation completes (including stream consumption), not just at connection time. Mid-stream errors must degrade health.
- **Acquire locks before sharing pointers**: Never pass a pointer to shared mutable state (e.g., `*Session`) to a function before acquiring the lock that protects it. Move the call after lock acquisition, or pass only immutable identifiers (like a session ID) instead of the live pointer.
- **Use atomic.Pointer for hot-path reads of immutable data**: When a shared immutable object (e.g., `*Registry`) is read on every message but swapped rarely (config reload), use `atomic.Pointer[T]` instead of `RWMutex`. `atomic.Load` is a single CPU instruction with zero contention, while `RWMutex.RLock` requires a CAS + counter increment.

### Code Quality

- **DRY — Extract duplicated logic**: When the same block of code (5+ lines) appears in multiple branches, extract it into a private method.
- **Avoid mutating value copies**: When you need to read a field with a default, use a pure reader method with a value receiver (e.g., `checkIntervalOrDefault()`) instead of calling a mutating method on a copy.
- **Use modern Go idioms**: Prefer `slices.Sort` over `sort.Strings`, `slices.Contains` over custom `inList()` helpers, and `os.LookupEnv` over `os.Getenv` when you need to distinguish empty from unset.
- **Document design decisions**: When a design choice might surprise a reader (e.g., scopes validated at registration but not enforced at execution), add a comment explaining the intent and where the responsibility lies.
- **UTF-8 safety in string splitting**: When splitting strings by byte index, always walk back to a valid UTF-8 rune boundary using `utf8.RuneStart`. Never assume byte index equals character boundary.
- **Never reimplement stdlib**: Prefer `strings.TrimSpace`, `strings.Contains`, `strings.Split` over custom equivalents. Custom helpers miss edge cases (e.g., `\v`, `\f` whitespace characters).
- **Consecutive error circuit breaker**: Background loops that call external services (typing indicators, health checks) should stop after N consecutive errors instead of retrying indefinitely until context cancellation.

### Security

- **Never pass secrets to LLM context**: Use `CredentialStore` via `context.Context`, never embed secrets in system prompts or tool outputs.
- **Redact before logging**: All logging goes through `RedactingHandler`; never use `fmt.Println` or raw `slog` for secret-bearing data.
- **Sanitize subprocess environment**: Always use `security.SanitizedEnv()` instead of `os.Environ()` when spawning subprocesses.
- **Default-deny for external access**: Empty allow-lists block everything; URL filters and sandbox policies require explicit opt-in.
- **Validate at system boundaries**: Check message size and JSON depth on ingress (router, webhook); trust internal data after validation.

### Testing

- **Never export test helpers in production packages**: Use unexported functions with `_test` build tags, or place helpers in a dedicated `<package>test/` sub-package (e.g., `providertest/`).
- **Clean up provisioned resources**: If a module acquires resources during provisioning (DB connections, files, goroutines), ensure there is a cleanup path. Test teardown must not leak.

## Security

sclaw implements defense-in-depth security across the entire message pipeline:

| Layer | Mechanism | Package |
|-------|-----------|---------|
| Credential management | `CredentialStore` with context injection — secrets never in LLM prompts | `internal/security/credentials.go` |
| Log redaction | `RedactingHandler` wraps `slog.Handler`, redacts all string attributes | `internal/security/sloghandler.go` |
| Pattern detection | `Redactor` with compiled regex for OpenAI, Anthropic, GitHub, AWS, Slack keys | `internal/security/redactor.go` |
| Rate limiting | Sliding window counters for sessions, messages, tool calls, tokens | `internal/security/ratelimit.go` |
| Input validation | Message size limits + JSON depth checking at system boundaries | `internal/security/validation.go` |
| Subprocess sanitization | `SanitizedEnv()` strips sensitive env vars before `syscall.Exec` | `internal/security/env.go` |
| Tool sandboxing | Docker-based execution with resource limits for exec/read_write scopes | `internal/security/sandbox.go` |
| URL filtering | Default-deny domain allow/deny lists for network tools | `internal/security/urlfilter.go` |
| Path filtering | Per-agent `allowed_dirs` with RO/RW access for read_file/write_file | `internal/security/pathfilter.go` |
| Audit logging | JSONL audit trail covering messages, tool calls, auth, config changes | `internal/security/audit.go` |
| Session isolation | Lane locks + cross-session ParentID validation | `internal/router/`, `internal/subagent/` |
| Auth middleware | Bearer + Basic auth with constant-time comparison | `internal/gateway/auth.go` |
| Webhook validation | HMAC-SHA256 signature verification per source | `internal/gateway/webhook.go` |
| Plugin certification | Ed25519 signature verification with trusted key list | `internal/cert/` |
| Config tool safety | Hash-based optimistic locking, validation before write, secret redaction, fixed path | `internal/tool/configtool/` |

See `docs/security/prompt-injection.md` for the full threat model and mitigation details.

## Skills

Skills are Markdown files (`SKILL.md`) with YAML frontmatter, embedded into the binary via `go:embed`. They provide contextual instructions injected into the LLM prompt based on activation rules.

### SKILL.md Frontmatter

```yaml
---
name: my-skill
description: Short description of what the skill does.
trigger: auto          # always | auto | manual (default: manual)
keywords:              # required when trigger is "auto"
  - keyword1
  - keyword2
metadata:
  { "openclaw": { ... } }
---
```

### Trigger Modes

| Mode | Behavior |
|------|----------|
| `always` | Skill is injected on every message (requires all `tools_required` available) |
| `auto` | Skill is injected when any keyword matches the user message (case-insensitive substring, OR logic) |
| `manual` | Skill is injected only when explicitly activated by name |

### Builtin Skills

| Skill | Trigger | Keywords |
|-------|---------|----------|
| 1password | auto | `1password`, `op vault`, `secret`, `credential`, `password` |
| apple-notes | auto | `apple notes`, `note`, `memo` |
| apple-reminders | auto | `reminder`, `remindctl`, `todo`, `task` |
| bear-notes | auto | `bear`, `grizzly` |
| blogwatcher | auto | `blog`, `rss`, `feed`, `atom` |
| canvas | auto | `canvas`, `html`, `display`, `render` |
| coding-agent | auto | `codex`, `claude code`, `coding agent`, `delegate`, `spawn agent` |
| github | auto | `github`, `pull request`, `issue`, `pr`, `ci`, `gh` |
| gog | auto | `gmail`, `calendar`, `drive`, `google`, `sheets`, `docs`, `gog` |
| obsidian | auto | `obsidian`, `vault`, `obsidian-cli` |
| tmux | auto | `tmux`, `terminal`, `session`, `pane` |
| trello | auto | `trello`, `board`, `card`, `kanban` |
| weather | auto | `weather`, `forecast`, `temperature`, `météo` |

### Key Packages

- `internal/workspace/skill.go` — `ParseSkill`, `SkillMeta` struct, frontmatter parsing
- `internal/workspace/activator.go` — `SkillActivator.Activate` (3-pass: always → auto → manual)
- `skills/embed.go` — `go:embed` directive for builtin skills

## Hot Reload

sclaw supports live configuration reload via `SIGHUP` or file polling. The reload flow is:

1. `reload.Handler` loads and validates the new config from disk
2. `AppContext` carries both `ModuleConfigs` and `AgentConfigs` (raw YAML nodes)
3. `core.App.ReloadModules()` calls `Reload()` on every module that implements `core.Reloader`

### Agent Reload (`routerModule.Reload`)

- Parses new agent YAML nodes → `multiagent.ParseAgents`
- Resolves defaults and creates directories
- Builds a new immutable `*Registry`
- Atomically swaps the Registry in `Factory` via `atomic.Pointer[Registry]`
- Selectively invalidates caches (souls, history stores) for changed agents
- In-flight messages continue with the old Registry snapshot; next `ForSession()` uses the new one

### Cron Reload (`schedulerModule.Reload`)

- Parses new agent configs → builds new Registry
- Stops the old `cron.Scheduler` (10s timeout, waits for in-flight jobs)
- Creates a new Scheduler with per-agent jobs (session cleanup, memory extraction, memory compaction)
- Starts the new Scheduler

### What Does NOT Reload

| Component | Reason |
|-----------|--------|
| Channels (Telegram, Discord, etc.) | Stateful connections, require restart |
| Providers | Single default provider, no per-agent resolution yet |
| Global tools | Built once at startup; per-agent allowlists take effect immediately |
| Memory module | DB connections are long-lived; new agents get lazy-opened stores |

## Config Tools (Runtime)

The agent can read and modify `sclaw.yaml` at runtime via four tools registered in `internal/tool/configtool/`:

| Tool | Scope | Default Policy | Description |
|------|-------|----------------|-------------|
| `config.get` | `read_only` | `allow` | Read the current config (redacted) + SHA-256 `base_hash` |
| `config.validate` | `read_only` | `allow` | Dry-run validation of YAML content (no disk write) |
| `config.patch` | `read_write` | `ask` | Merge a partial YAML patch into the current config |
| `config.apply` | `read_write` | `ask` | Replace the entire config with new YAML content |

### Concurrency Control

Write tools (`patch`, `apply`) require a `base_hash` obtained from `config.get`. If the file changed since that hash was computed, the operation is rejected (optimistic locking).

### Write Flow

1. Verify `base_hash` matches current file hash
2. Merge (patch) or replace (apply) the YAML content
3. Validate the result (`config.LoadFromBytes` + `config.Validate`)
4. Atomic write (temp file + `os.Rename`)
5. Trigger in-process hot-reload via `reload.Handler`
6. Warn if plugin list changed (requires rebuild)

### Key Design Decisions

- Works on `*yaml.Node` (not `map[string]any`) to preserve `${VAR}` references, comments, and key ordering
- Merge follows RFC 7386 (JSON Merge Patch) adapted for YAML: mappings merge recursively, sequences replace entirely, `null` deletes keys
- Config path is fixed by closure — the agent cannot target arbitrary files
- Max config/patch size: 1 MiB
- Secrets are redacted in `config.get` output via `security.Redactor.RedactMap()`

## CI/CD

- **GitHub Actions** (`.github/workflows/ci.yml`): Lint + Test on push/PR to main
- **GoReleaser** (`.goreleaser.yaml`): Multi-platform release builds (linux/darwin, amd64/arm64)

## Configuration Files

| File               | Purpose                          |
| ------------------ | -------------------------------- |
| `.golangci.yml`    | golangci-lint v2 configuration   |
| `.goreleaser.yaml` | GoReleaser v2 release config     |
| `shell.nix`        | Nix development environment      |
| `.envrc`           | direnv integration               |
