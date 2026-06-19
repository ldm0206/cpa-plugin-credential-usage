# Credential Usage Plugin

A CPA plugin that exposes credential quota details through CPA plugin resource routes.

## Capabilities

- **UsagePlugin**: Collects quota details from every API request response header and failure body
- **ManagementAPI**: Serves credential quota data via HTTP endpoints

## Resource Endpoints

CPA exposes this plugin through its plugin resource route mechanism. These endpoints do not use the Management API path and do not require the Management API key.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/v0/resource/plugins/credential-usage/list` | List all credentials with quota details |
| GET | `/v0/resource/plugins/credential-usage/detail?auth_index=<auth_index>` | Single credential quota detail |

Query parameters:
- `provider` (optional): Filter by provider name (list endpoint)
- `auth_index` (required): Credential auth index (detail endpoint)

Example:

```bash
curl http://127.0.0.1:8317/v0/resource/plugins/credential-usage/list
curl http://127.0.0.1:8317/v0/resource/plugins/credential-usage/detail?auth_index=2
```

Example response item:

```json
{
  "auth_id": "auth-1",
  "auth_index": "1",
  "provider": "claude",
  "label": "Claude Pro",
  "email": "user@example.com",
  "status": "available",
  "quota_state": {
    "exceeded": false
  },
  "quota_details": {
    "source": "anthropic_headers",
    "updated_at": "2026-06-20T10:00:00Z",
    "available": true,
    "windows": [
      {
        "name": "5h",
        "label": "5 hour limit",
        "status": "allowed",
        "utilization": 0.42,
        "surpassed_threshold": false,
        "reset_at": "2026-06-20T15:00:00Z"
      },
      {
        "name": "7d",
        "label": "weekly limit",
        "utilization": 0.77,
        "surpassed_threshold": true,
        "reset_at": "2026-06-24T00:00:00Z"
      }
    ],
    "overall_reset_at": "2026-06-20T15:00:00Z",
    "rate_limits": {
      "requests": {
        "limit": 1000,
        "remaining": 750,
        "reset_at": "2026-06-20T10:05:00Z"
      },
      "input_tokens": {
        "limit": 100000,
        "remaining": 90000
      },
      "output_tokens": {
        "limit": 50000,
        "remaining": 45000
      }
    }
  },
  "last_active_at": "2026-06-20T10:00:00Z"
}
```

`quota_state` mirrors CPA runtime cooldown state. It answers whether CPA has temporarily marked a credential unavailable and why. Package/window quota data lives in `quota_details`.

`quota_details.windows` is populated from provider response headers observed in CPA usage events. For Anthropic, the plugin reads `anthropic-ratelimit-unified-5h-*` and `anthropic-ratelimit-unified-7d-*` headers. These headers expose utilization, reset time, status, and threshold state; they do not necessarily expose absolute package limit numbers.

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
- UsageRecord response headers (Claude unified 5-hour/weekly quota windows, Claude rate limits, Retry-After)
- UsageRecord failure bodies (Codex 429 usage_limit_reached)
- host.auth.get_runtime (credential status, quota state)

### Active Mode (when cpa-base-url + management-key configured)

Periodically queries upstream APIs through CPA's api-call endpoint:
- Antigravity/Gemini CLI: loadCodeAssist credit balance, exposed as `quota_details.credits`

## Build

```bash
go build -buildmode=c-shared -o credential-usage.so   # Linux/macOS
go build -buildmode=c-shared -o credential-usage.dll   # Windows
```

Place the output in CPA's plugins directory.
