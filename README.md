# Credential Usage Plugin

A CPA plugin that exposes credential quota and usage data through the Management API.

## Capabilities

- **UsagePlugin**: Collects usage data from every API request (token counts, response headers)
- **ManagementAPI**: Serves credential usage data via HTTP endpoints

## Management API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/v0/management/credential-usage` | List all credentials with quota/usage data |
| GET | `/v0/management/credential-usage/:auth_index` | Single credential detail |

Query parameters:
- `provider` (optional): Filter by provider name

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
