# sclaw — Project Documentation

## Overview

`sclaw` is a Go CLI tool. Module path: `github.com/flemzord/sclaw`.

## Architecture

```
cmd/sclaw/main.go   → Entry point
internal/           → Private packages (not importable)
pkg/                → Public reusable packages
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
