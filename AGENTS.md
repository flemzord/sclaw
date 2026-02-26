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
  internal/heartbeat/ → Monitoring and notifications
  internal/hook/      → Message pipeline hooks (audit, filtering)
  internal/memory/    → Conversation history management
  internal/multiagent/→ Multi-agent configuration and routing
  internal/node/      → Remote device connections (WebSocket)
  internal/provider/  → Provider interface, failover chain, health tracking
  internal/reload/    → Hot configuration reload
  internal/router/    → Central message dispatch + session management
  internal/security/  → Security hardening (credentials, redaction, audit,
                        rate limiting, validation, env sanitization,
                        sandboxing, URL filtering)
  internal/subagent/  → Ephemeral sub-agent sessions
  internal/tool/      → Tool registry + approval system
  internal/workspace/ → Working directory management
pkg/                  → Public reusable packages
  pkg/app/            → Shared entry point (Run, ResolveConfigPath)
  pkg/message/        → Platform-agnostic message model
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
| Audit logging | JSONL audit trail covering messages, tool calls, auth, config changes | `internal/security/audit.go` |
| Session isolation | Lane locks + cross-session ParentID validation | `internal/router/`, `internal/subagent/` |
| Auth middleware | Bearer + Basic auth with constant-time comparison | `internal/gateway/auth.go` |
| Webhook validation | HMAC-SHA256 signature verification per source | `internal/gateway/webhook.go` |
| Plugin certification | Ed25519 signature verification with trusted key list | `internal/cert/` |

See `docs/security/prompt-injection.md` for the full threat model and mitigation details.

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
