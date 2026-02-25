# sclaw — Project Documentation

## Overview

`sclaw` is a Go CLI tool.
Module path: `github.com/flemzord/sclaw`
Repository: `https://github.com/flemzord/sclaw`

## Architecture

```
cmd/sclaw/main.go   → Entry point
internal/           → Private packages (not importable)
  internal/config/  → Configuration loading and validation
  internal/core/    → Module system and registry
  internal/provider/→ Provider interface, failover chain, health tracking
pkg/                → Public reusable packages
  pkg/message/      → Platform-agnostic message model
docs/               → Additional documentation
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
lint    # Run golangci-lint
test    # Run tests with race detector + coverage
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

### Concurrency

- **Encapsulate channels behind methods**: Never export raw channels. Expose a method with state validation (e.g., `Respond()` that checks if an approval is pending) to prevent misuse of concurrent primitives.
- **Defer health verdicts**: When tracking provider health, record success only after the full operation completes (including stream consumption), not just at connection time. Mid-stream errors must degrade health.

### Code Quality

- **DRY — Extract duplicated logic**: When the same block of code (5+ lines) appears in multiple branches, extract it into a private method.
- **Avoid mutating value copies**: When you need to read a field with a default, use a pure reader method with a value receiver (e.g., `checkIntervalOrDefault()`) instead of calling a mutating method on a copy.
- **Use modern Go idioms**: Prefer `slices.Sort` over `sort.Strings`, `slices.Contains` over custom `inList()` helpers, and `os.LookupEnv` over `os.Getenv` when you need to distinguish empty from unset.
- **Document design decisions**: When a design choice might surprise a reader (e.g., scopes validated at registration but not enforced at execution), add a comment explaining the intent and where the responsibility lies.

### Testing

- **Never export test helpers in production packages**: Use unexported functions with `_test` build tags, or place helpers in a dedicated `<package>test/` sub-package (e.g., `providertest/`).
- **Clean up provisioned resources**: If a module acquires resources during provisioning (DB connections, files, goroutines), ensure there is a cleanup path. Test teardown must not leak.

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
