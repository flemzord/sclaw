# Memory System

## Overview

sclaw provides a two-tier memory system that gives agents both short-term and long-term recall:

1. **History Store** — conversation turns (user/assistant messages) persisted per session, restored when a session is recreated.
2. **Fact Store** — long-term knowledge extracted from conversations using an LLM, searchable via full-text search.

Both tiers are defined as Go interfaces in `internal/memory/`. The default production backend is SQLite (`modules/memory/sqlite/`), while in-memory implementations exist for testing. Memory is optional: when disabled or unconfigured, the system degrades gracefully — no errors, no facts, no persistence.

## History Store

`HistoryStore` manages per-session conversation history. Each message is stored with its role, content, tool calls, and metadata. Sessions are identified by a persistence key derived from the `channel:chatID:threadID` triple, which survives session UUID rotation.

### Interface

| Method | Signature | Description |
|--------|-----------|-------------|
| `Append` | `(sessionID string, msg provider.LLMMessage) error` | Adds a message to the session's history |
| `GetRecent` | `(sessionID string, n int) ([]provider.LLMMessage, error)` | Returns the *n* most recent messages |
| `GetAll` | `(sessionID string) ([]provider.LLMMessage, error)` | Returns all messages for a session |
| `SetSummary` | `(sessionID string, summary string) error` | Stores a compaction summary, replacing any previous one |
| `GetSummary` | `(sessionID string) (string, error)` | Returns the stored summary (empty string if none) |
| `Purge` | `(sessionID string) error` | Removes all history and summary for a session |
| `Len` | `(sessionID string) (int, error)` | Returns the number of stored messages |

Implementations must be safe for concurrent use.

### Compaction

When a session's history exceeds a configurable threshold, the context engine (`internal/context/`) triggers compaction:

1. Older messages are summarized by an LLM into a single summary string.
2. The summary is stored via `SetSummary` and prepended as a system message on the next assembly pass.
3. Only the most recent messages (controlled by `RetainRecent`) are kept alongside the summary.

If no summarizer is available, old messages are simply dropped and only the most recent are retained. An emergency compaction path (`EmergencyCompact`) exists as a last resort when the context window is critically exceeded — it retains only `EmergencyRetain` messages.

### In-Memory Implementation

`InMemoryHistoryStore` (`internal/memory/history_mem.go`) stores messages in a `map[string]*sessionData` protected by a `sync.RWMutex`. Suitable for testing and single-process deployments without persistence needs.

```go
store := memory.NewInMemoryHistoryStore()
```

## Fact Store

`Store` manages long-term facts — pieces of knowledge extracted from conversations (user preferences, personal details, decisions, goals). Facts are stored with metadata and are searchable.

### Interface

| Method | Signature | Description |
|--------|-----------|-------------|
| `Index` | `(ctx context.Context, fact Fact) error` | Stores a new fact (upserts by ID) |
| `Search` | `(ctx context.Context, query string, topK int) ([]Fact, error)` | Retrieves top-K facts matching the query |
| `SearchByMetadata` | `(ctx context.Context, key, value string) ([]Fact, error)` | Retrieves facts where `metadata[key] == value` |
| `Delete` | `(ctx context.Context, id string) error` | Removes a fact by ID |
| `Len` | `() int` | Returns the total number of stored facts |

### Fact Structure

```go
type Fact struct {
    ID        string
    Content   string
    Source    string            // session ID where the fact was extracted
    Tags      []string
    Metadata  map[string]string
    CreatedAt time.Time
}
```

### Extraction

`FactExtractor` analyzes user-assistant exchanges and extracts facts using an LLM. The `LLMExtractor` sends each exchange through a prompt that asks the model to identify factual information worth remembering:

```
Analyze the following exchange and extract important facts about the user.
Return one fact per line. If there are no facts worth remembering, return "NONE".
Only extract factual information (preferences, personal details, decisions, goals).
```

The response is parsed line-by-line, bullet markers are stripped, and each line becomes a `Fact` with a unique ID (`{unixnano}-{index}-{seq}`).

When extraction is disabled, `NopExtractor` is used — it always returns nil.

### Injection

`InjectMemory` retrieves relevant facts from the store and formats them for inclusion in the system prompt. It enforces a token budget: facts are added in rank order until `MaxTokens` is reached.

```go
facts, err := memory.InjectMemory(ctx, memory.InjectionRequest{
    Store:     factStore,
    Query:     "user preferences",
    MaxFacts:  10,
    MaxTokens: 2000,
    Estimator: estimator,
})
```

The output is formatted as a markdown section:

```
## Relevant Memory

- User prefers dark mode
- User's name is Alice
```

Returns nil when the store is nil or no relevant facts are found (graceful degradation).

### In-Memory Implementation

`InMemoryStore` (`internal/memory/store_mem.go`) uses simple substring matching for `Search` and a swap-delete strategy for `Delete`. The `ErrFactNotFound` sentinel error is returned when deleting a non-existent fact.

```go
store := memory.NewInMemoryStore()
```

## SQLite Backend

The production backend (`modules/memory/sqlite/`) provides a persistent implementation of both `HistoryStore` and `Store` backed by a single SQLite database. It uses `modernc.org/sqlite` (pure Go, no CGO).

### Features

- **WAL mode** for concurrent reads (enabled by default)
- **FTS5** full-text search on fact content
- **Automatic schema migration** with version tracking
- **Single-connection pool** (SQLite serializes writes)
- **Busy timeout** to handle lock contention

### Database Schema

The database contains the following tables:

| Table | Purpose |
|-------|---------|
| `messages` | Conversation messages, keyed by `(session_id, seq)` |
| `summaries` | Compaction summaries, one per session |
| `facts` | Long-term memory facts with metadata (JSON) |
| `facts_fts` | FTS5 virtual table indexing `facts.content` |
| `schema_version` | Tracks the current schema version for migrations |

Three triggers (`facts_ai`, `facts_ad`, `facts_au`) keep the FTS5 index in sync with the `facts` table on insert, delete, and update.

Fact search uses FTS5 `MATCH` with ranking by BM25 (`ORDER BY rank`). Metadata search uses `json_extract` for structured queries.

### Module Registration

The SQLite module registers itself as `memory.sqlite` via `core.RegisterModule`. During provisioning, it:

1. Creates the data directory if needed
2. Opens the database with `PRAGMA journal_mode=WAL` and `PRAGMA busy_timeout`
3. Runs the schema migration
4. Registers two services: `memory.history` and `memory.store`

### Standalone Usage

For per-agent history (used by `multiagent.Factory`), the `OpenHistoryStore` function provides a lightweight way to open a dedicated database without the full module lifecycle:

```go
store, db, err := sqlite.OpenHistoryStore("/path/to/memory.db")
defer db.Close()
```

## Configuration

### Module Configuration

The SQLite memory module is configured under the `modules` section with the key `memory.sqlite`:

```yaml
modules:
  memory.sqlite:
    path: "/data/memory.db"   # Database file path (default: {data_dir}/memory.db)
    wal: true                  # Enable WAL journal mode (default: true)
    busy_timeout: 5000         # Milliseconds to wait on busy lock (default: 5000)
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `path` | `string` | `{data_dir}/memory.db` | Database file path |
| `wal` | `*bool` | `true` | Enable WAL journal mode for concurrent reads |
| `busy_timeout` | `int` | `5000` | Milliseconds to wait when the database is locked |

### Per-Agent Configuration

Each agent can control whether memory is enabled via the `memory` section in its agent config:

```yaml
agents:
  assistant:
    memory:
      enabled: true   # default: true when omitted
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | `*bool` | `true` | Whether memory (history persistence) is enabled for this agent |

When `enabled` is `false` (or when `data_dir` is empty), `ResolveHistory` returns nil and no SQLite database is opened for that agent.

### Pipeline Settings

The router pipeline controls history size via `MaxHistoryLen` in `PipelineConfig`:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `MaxHistoryLen` | `int` | `100` | Maximum number of LLM messages kept in session history |

When the history exceeds this limit, the oldest entries are trimmed after each message append.

## Pipeline Integration

Memory is wired into the router's 15-step message pipeline at several points:

### Step 7b: History Restore

When a **new session** is created and a `HistoryResolver` is configured:

1. The pipeline calls `HistoryResolver.ResolveHistory(agentID)` to get the agent's `HistoryStore`.
2. If a store is returned, it calls `GetRecent(persistenceKey, MaxHistoryLen)` to restore previous messages.
3. The persistence key is `channel:chatID:threadID` — stable across session UUID rotation.
4. Restored messages are set as the session's initial history.

### Step 8 / 8b: User Message Persistence

After appending the user message to in-memory history:

1. History is trimmed to `MaxHistoryLen`.
2. If a persistent store is available, the user message is written via `Append(persistenceKey, llmMsg)`.
3. Persistence is **write-behind** and **non-fatal**: errors are logged but do not block the pipeline.

### Step 13 / 13b: Assistant Message Persistence

After the agent produces a response and it is sent to the user:

1. The assistant message is appended to in-memory history.
2. History is trimmed again to maintain the `MaxHistoryLen` invariant.
3. The assistant message is persisted to SQLite (same write-behind, non-fatal pattern).

### Lazy Store Opening

`multiagent.Factory` implements `HistoryResolver` with lazy initialization:

1. On first `ResolveHistory(agentID)` call, the factory checks the agent's config.
2. If `memory.enabled` is true and `data_dir` is set, it opens `{data_dir}/memory.db` via `sqlite.OpenHistoryStore`.
3. The store is cached for subsequent calls (double-checked locking with `sync.RWMutex`).
4. All opened databases are closed when `Factory.Close()` is called.

## Data Directory Layout

Per-agent data is stored under the configured `data_dir` (defaults to `{global_data_dir}/agents/{agent_name}/`):

```
{data_dir}/
└── agents/
    ├── assistant/
    │   └── memory.db       # SQLite database (history + facts + FTS5)
    └── support/
        └── memory.db
```

When the global `memory.sqlite` module is used instead of per-agent stores, a single database is created at the configured `path` (defaults to `{data_dir}/memory.db`).

Each `memory.db` file contains the full schema: `messages`, `summaries`, `facts`, `facts_fts`, and `schema_version` tables. WAL mode produces additional `memory.db-wal` and `memory.db-shm` files alongside the main database.
