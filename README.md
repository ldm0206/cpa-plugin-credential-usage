# Credential Usage Plugin

A CPA plugin that exposes credential quota and usage data through CPA plugin resource routes.

## Capabilities

- **UsagePlugin**: Collects usage data from every API request (token counts, response headers)
- **ManagementAPI**: Serves credential usage data via HTTP endpoints

## Resource Endpoints

CPA exposes this plugin through its plugin resource route mechanism. These endpoints do not use the Management API path and do not require the Management API key.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/v0/resource/plugins/credential-usage/list` | List all credentials with quota/usage data |
| GET | `/v0/resource/plugins/credential-usage/detail?auth_index=<auth_index>` | Single credential detail |

Query parameters:
- `provider` (optional): Filter by provider name (list endpoint)
- `auth_index` (required): Credential auth index (detail endpoint)

Example:

```bash
curl http://127.0.0.1:8317/v0/resource/plugins/credential-usage/list
curl http://127.0.0.1:8317/v0/resource/plugins/credential-usage/detail?auth_index=2
```

## Configuration

```yaml
plugins:
  configs:
    credential-usage:
      enabled: true
      cpa-base-url: ""        # Optional: CPA URL for active queries
      management-key: ""      # Optional: Management API key for active queries
      poll-interval: "5m"     # Optional: Active query interval (default 5m)
```

### Passive Mode (default)

Always active. Collects data from:
- UsageRecord response headers (Claude rate limits, Retry-After)
- UsageRecord failure bodies (Codex 429 usage_limit_reached)
- host.auth.get_runtime (credential status, quota state)

### Active Mode (when cpa-base-url + management-key configured)

Periodically queries upstream APIs through CPA's api-call endpoint:
- Antigravity/Gemini CLI: loadCodeAssist credit balance

## Build

```bash
go build -buildmode=c-shared -o credential-usage.so   # Linux/macOS
go build -buildmode=c-shared -o credential-usage.dll   # Windows
```

Place the output in CPA's plugins directory.
