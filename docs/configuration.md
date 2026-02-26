# Configuration Reference

## Overview

sclaw is entirely config-driven. A single YAML file declares which modules to load, how agents behave, and what security policies to enforce. The file is passed to the CLI via the `--config` (`-c`) flag or discovered automatically from standard locations.

```bash
# Explicit path
sclaw start --config /etc/sclaw/sclaw.yaml

# Automatic discovery (search order):
#   1. $XDG_CONFIG_HOME/sclaw/sclaw.yaml
#   2. ~/.config/sclaw/sclaw.yaml
#   3. ./sclaw.yaml
sclaw start
```

You can validate a configuration file without starting the application:

```bash
sclaw config check /path/to/sclaw.yaml
```

## Environment Variables

All string values in the YAML file support environment variable expansion before parsing. Two forms are recognized:

| Syntax | Behavior |
|--------|----------|
| `${VAR}` | Replaced by the value of `VAR`. If the variable is unset, loading fails with an error. |
| `${VAR:-default}` | Replaced by the value of `VAR`. If unset, `default` is used instead. |

Variable names must start with a letter or underscore, followed by letters, digits, or underscores (`[A-Za-z_][A-Za-z0-9_]*`).

```yaml
modules:
  provider.openai_compatible:
    api_key: "${OPENAI_API_KEY}"
    base_url: "${OPENAI_BASE_URL:-https://api.openai.com/v1}"
```

## Top-Level Structure

The configuration file has four top-level keys:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `version` | string | yes | Config format version. Currently only `"1"` is supported. |
| `modules` | map[string]object | yes | Module configurations keyed by module ID. At least one module must be present. |
| `agents` | map[string]object | no | Per-agent configurations keyed by agent name. Omit for single-agent mode. |
| `plugins` | list | no | Third-party Go module plugins to compile into the binary. |
| `security` | object | no | Security settings (rate limits, sandbox, URL filter, plugin certification). |

```yaml
version: "1"

modules:
  # ...

agents:
  # ...

security:
  # ...
```

## Modules

Modules are identified by their registration ID, which follows the pattern `<category>.<name>` (e.g. `channel.telegram`, `provider.openai_compatible`, `memory.sqlite`). Each key under `modules` must match a compiled module ID; unknown IDs cause a validation error. Modules not listed in the config are simply not loaded.

### channel.telegram

Connects sclaw to Telegram as a messaging channel.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `token` | string | | Bot token in `<bot_id>:<hash>` format. **Required.** |
| `mode` | string | `"polling"` | Delivery mode: `"polling"` or `"webhook"`. |
| `polling_timeout` | int | `30` | Long-polling timeout in seconds (0-50). |
| `webhook_url` | string | | Public URL for webhook mode. |
| `webhook_secret` | string | | Secret token for webhook verification. |
| `allowed_updates` | list[string] | `["message", "edited_message", "channel_post"]` | Telegram update types to receive. |
| `allow_users` | list[string] | | Telegram user IDs allowed to interact. Empty means all. |
| `allow_groups` | list[string] | | Telegram group IDs allowed. Empty means all. |
| `max_message_length` | int | `4096` | Maximum outbound message length (1-4096). |
| `stream_flush_interval` | duration | `1s` | Interval between streaming message edits (100ms-30s). |
| `api_url` | string | `"https://api.telegram.org"` | Telegram Bot API base URL. |

```yaml
modules:
  channel.telegram:
    token: "${TELEGRAM_BOT_TOKEN}"
    mode: "polling"
    allow_users: ["123456789"]
    max_message_length: 4096
    stream_flush_interval: 1s
```

### provider.openai_compatible

Connects to any OpenAI-compatible API (OpenAI, Azure, local models, etc.).

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `base_url` | string | | API base URL (must be http or https). **Required.** |
| `api_key` | string | | API key. One of `api_key` or `api_key_env` is **required**. |
| `api_key_env` | string | | Environment variable name containing the API key. |
| `model` | string | | Model identifier (e.g. `"gpt-4o"`). **Required.** |
| `context_window` | int | `4096` | Maximum context window size in tokens. |
| `max_tokens` | int | `0` | Maximum tokens to generate (0 = provider default). |
| `headers` | map[string]string | | Extra HTTP headers to send with every request. |
| `timeout` | duration | `30s` | HTTP request timeout. |

```yaml
modules:
  provider.openai_compatible:
    base_url: "https://api.openai.com/v1"
    api_key: "${OPENAI_API_KEY}"
    model: "gpt-4o"
    context_window: 128000
    timeout: 30s
```

### memory.sqlite

Provides SQLite-backed persistent conversation history.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `path` | string | `{DataDir}/memory.db` | Database file path. |
| `wal` | bool | `true` | Enable WAL journal mode for concurrent reads. |
| `busy_timeout` | int | `5000` | Milliseconds to wait on a busy lock. Must be non-negative. |

```yaml
modules:
  memory.sqlite: {}
```

With explicit settings:

```yaml
modules:
  memory.sqlite:
    path: "/var/lib/sclaw/memory.db"
    wal: true
    busy_timeout: 10000
```

## Agents

The `agents` section enables multi-agent mode. Each key is an agent name, and its value configures routing, provider binding, and loop parameters for that agent. When `agents` is omitted, sclaw runs in single-agent mode with a default agent.

### Agent Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `data_dir` | string | `{DataDir}/agents/{name}` | Persistent data directory for this agent. |
| `workspace` | string | | Working directory for tool execution. |
| `provider` | string | | Module ID of the provider to use (must exist in `modules`). |
| `tools` | list[string] | | Tool names available to this agent. |
| `memory` | object | | Memory settings for this agent. |
| `routing` | object | | Routing rules for message dispatch. |
| `loop` | object | | ReAct loop parameter overrides. |

### memory

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `true` | Whether conversation history is persisted for this agent. |

### routing

Routing rules determine which agent handles an incoming message. The router evaluates agents in declaration order and picks the first match. At most one agent may be marked as `default`.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `channels` | list[string] | | Channel names this agent handles (e.g. `["telegram"]`). |
| `users` | list[string] | | User IDs this agent responds to. |
| `groups` | list[string] | | Group IDs this agent responds to. |
| `default` | bool | `false` | Fallback agent when no other routing rule matches. Only one agent may set this to `true`. |

### loop

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `max_iterations` | int | `0` | Maximum ReAct loop iterations (0 = unlimited). |
| `token_budget` | int | `0` | Token budget for the agent loop (0 = unlimited). |
| `timeout` | string | | Maximum duration for a single agent invocation (e.g. `"5m"`). |
| `loop_threshold` | int | `0` | Consecutive identical actions before breaking the loop. |

### Example

```yaml
agents:
  main:
    provider: "provider.openai_compatible"
    memory:
      enabled: true
    routing:
      channels: ["telegram"]
      default: true
    loop:
      max_iterations: 10
      timeout: "5m"

  coder:
    provider: "provider.openai_compatible"
    workspace: "/home/user/projects"
    tools: ["exec", "read_file", "write_file"]
    memory:
      enabled: true
    routing:
      users: ["123456789"]
    loop:
      max_iterations: 25
      timeout: "10m"
```

## Security

The optional `security` section configures defense-in-depth features. When omitted, defaults apply (no rate limits, sandbox disabled, no URL filtering).

### plugins

Controls plugin certification requirements.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `require_certified` | bool | `false` | Reject uncertified plugins at build time. |
| `trusted_keys` | list[string] | | Hex-encoded Ed25519 public keys (32 bytes each) allowed to sign plugins. Required when `require_certified` is `true`. |

### rate_limits

Sliding-window rate limiting applied per session.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `max_sessions` | int | `0` | Maximum concurrent sessions (0 = unlimited). |
| `messages_per_min` | int | `0` | Maximum messages per minute per session (0 = unlimited). |
| `tool_calls_per_min` | int | `0` | Maximum tool calls per minute per session (0 = unlimited). |
| `tokens_per_hour` | int | `0` | Maximum tokens per hour per session (0 = unlimited). |

### sandbox

Docker-based sandbox for tool execution.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable sandboxed tool execution. |
| `scopes_requiring_sandbox` | list[string] | | Tool scopes that must run inside a sandbox (e.g. `["exec", "read_write"]`). |
| `image` | string | | Docker image to use for sandbox containers. |
| `cpu_shares` | int | `0` | CPU shares for the container (0 = Docker default). |
| `memory_mb` | int | `0` | Memory limit in MB (0 = unlimited). |
| `disk_mb` | int | `0` | Disk limit in MB (0 = unlimited). |

### url_filter

Default-deny domain filtering for network tools. When `allow_domains` is empty, all outbound URLs are blocked.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `allow_domains` | list[string] | | Domains permitted for outbound requests. |
| `deny_domains` | list[string] | | Domains explicitly blocked (evaluated after allow list). |

### Example

```yaml
security:
  plugins:
    require_certified: true
    trusted_keys:
      - "a1b2c3d4e5f6..."

  rate_limits:
    max_sessions: 10
    messages_per_min: 30
    tool_calls_per_min: 60
    tokens_per_hour: 100000

  sandbox:
    enabled: true
    scopes_requiring_sandbox: ["exec", "read_write"]
    image: "sclaw-sandbox:latest"
    memory_mb: 512
    disk_mb: 1024

  url_filter:
    allow_domains: ["api.openai.com", "api.anthropic.com"]
    deny_domains: ["evil.example.com"]
```

## Plugins

Third-party Go module plugins compiled into the binary by `xsclaw`. Each entry identifies a Go module path and version. The bootstrapper detects plugin list changes on config reload and triggers a rebuild + re-exec.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `module` | string | yes | Go module path (e.g. `"github.com/example/sclaw-plugin"`). |
| `version` | string | no | Go module version (e.g. `"v1.0.0"`). |

```yaml
plugins:
  - module: "github.com/example/sclaw-weather"
    version: "v1.2.0"
  - module: "github.com/example/sclaw-calendar"
    version: "v0.9.1"
```

## Live Reload

sclaw supports live configuration reload without restart. A reload is triggered by:

- Sending `SIGHUP` to the process
- Modifying the configuration file (detected via file watcher)

On reload, sclaw re-reads and re-validates the config file. Modules that implement the `Reloader` interface are hot-reloaded. If the plugin list changed (only applicable to `xsclaw`-built binaries), a full rebuild and re-exec is triggered instead.

## Complete Example

```yaml
version: "1"

modules:
  memory.sqlite: {}

  channel.telegram:
    token: "${TELEGRAM_BOT_TOKEN}"
    mode: "polling"
    allow_users: ["123456789"]
    max_message_length: 4096
    stream_flush_interval: 1s

  provider.openai_compatible:
    base_url: "${OPENAI_BASE_URL:-https://api.openai.com/v1}"
    api_key: "${OPENAI_API_KEY}"
    model: "gpt-4o"
    context_window: 128000
    timeout: 30s

agents:
  main:
    provider: "provider.openai_compatible"
    memory:
      enabled: true
    routing:
      channels: ["telegram"]
      default: true
    loop:
      max_iterations: 10
      timeout: "5m"

security:
  rate_limits:
    max_sessions: 10
    messages_per_min: 30
    tool_calls_per_min: 60
    tokens_per_hour: 100000

  url_filter:
    allow_domains: ["api.openai.com"]
```
