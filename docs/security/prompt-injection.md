# Prompt Injection Security

## Overview

Indirect prompt injection is one of the most critical risks for AI agent systems like sclaw. An attacker can embed malicious instructions inside user messages, tool outputs, or external data sources (web pages, documents, API responses) to manipulate the agent into taking unintended actions.

## Threat Model

### Attack Vectors

1. **User message injection**: Direct malicious instructions in chat messages
2. **Tool output injection**: Crafted responses from external APIs/tools
3. **Context poisoning**: Malicious content injected via conversation history
4. **Data exfiltration**: Instructions to leak credentials via tool calls

### Mitigations Implemented

| Risk | Mitigation | Package |
|------|-----------|---------|
| Credential leakage via prompts | `CredentialStore` with context injection — secrets never in LLM context | `security/credentials.go` |
| Secret leakage in logs | `RedactingHandler` wraps all slog output | `security/sloghandler.go` |
| Secret leakage via subprocess env | `SanitizedEnv()` strips sensitive vars | `security/env.go` |
| API key patterns in outputs | `Redactor` with compiled regex patterns | `security/redactor.go` |
| Tool abuse / rapid exploitation | `RateLimiter` with sliding windows | `security/ratelimit.go` |
| JSON bombs / large payloads | `ValidateMessageSize` + `ValidateJSONDepth` | `security/validation.go` |
| Unauthorized tool execution | Policy-based approval (allow/ask/deny) | `tool/registry.go` |
| Cross-session data access | Session isolation + ParentID validation | `subagent/manager.go` |
| Dangerous filesystem access | `ValidatePath` blocks /proc access | `security/env.go` |
| Unrestricted network access | `URLFilter` with default-deny | `security/urlfilter.go` |
| Unaudited security events | `AuditLogger` covers all event types | `security/audit.go` |
| Webhook body bombs | `http.MaxBytesReader` on dispatcher | `gateway/webhook.go` |

## Design Principles

### 1. Never Pass Secrets to LLM Context

Secrets are managed exclusively through `CredentialStore` and accessed via `context.Context`. They are never embedded in system prompts, user messages, or tool outputs that flow through the LLM.

### 2. Defense in Depth

Multiple layers protect against credential leakage:
- **Layer 1**: `CredentialStore` — secrets never reach LLM
- **Layer 2**: `Redactor` — pattern-based detection in strings
- **Layer 3**: `RedactingHandler` — catches any remaining leaks in logs
- **Layer 4**: `SanitizedEnv` — subprocess environment sanitization
- **Layer 5**: `AuditLogger` — full audit trail with redaction

### 3. Fail-Closed

- Empty URL allow-lists block all domains (default-deny)
- Sandbox executor returns error if Docker unavailable (fail-closed)
- Rate limiter rejects requests when limits are exceeded
- Message validation rejects oversized or deeply nested payloads

### 4. Minimal Privilege

- Tools declare required scopes at registration
- Policy system controls execution based on context (DM vs group)
- Subprocess environments stripped of sensitive variables
- Sandbox containers run read-only with network disabled

## Remaining Risks

1. **Prompt injection via tool outputs**: While we redact known patterns, novel credential formats may bypass regex detection. Mitigation: keep patterns updated, audit logs for anomalies.

2. **Timing side-channels**: Rate limiting uses wall-clock time which could leak information about internal processing. Mitigation: use constant-time comparison for auth (already implemented).

3. **Resource exhaustion**: While rate limiting and message validation help, sophisticated attacks could still consume significant compute within limits. Mitigation: monitor metrics, adjust limits.

4. **Model-level jailbreaks**: These are outside sclaw's control and depend on the underlying LLM provider. Mitigation: defense in depth via tool approval system.
