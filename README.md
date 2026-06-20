# Credential Usage Plugin

A CPA plugin that exposes credential quota details through CPA plugin resource routes.

## Capabilities

- **UsagePlugin**: Collects provider-native quota details from API response headers and failure bodies
- **ManagementAPI**: Serves credential quota data via HTTP endpoints
- **Active polling**: Optionally queries provider usage APIs through CPA's Management API `api-call` endpoint

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

## Response Shape

`quota_state` mirrors CPA runtime cooldown state. It answers whether CPA has temporarily marked a credential unavailable and why. Package/window quota data lives in `quota_details`.

`usage_summary` and top-level `quota_remaining` are not returned. The plugin surfaces provider-observed quota data and does not fabricate absolute limits when providers only expose utilization, percent, or reset times.

### Claude example

```json
{
  "auth_id": "auth-1",
  "auth_index": "1",
  "provider": "claude",
  "label": "Claude Pro",
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
        "status": "allowed_warning",
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
      },
      "tokens": {
        "limit": 200000,
        "remaining": 150000,
        "reset_at": "2026-06-20T10:10:00Z"
      }
    }
  },
  "last_active_at": "2026-06-20T10:00:00Z"
}
```

### Codex example

```json
{
  "provider": "codex",
  "quota_details": {
    "source": "codex_headers",
    "available": true,
    "windows": [
      {
        "name": "primary",
        "label": "primary window (7d)",
        "used_percent": 81.5,
        "reset_after_seconds": 86400,
        "window_minutes": 10080
      },
      {
        "name": "secondary",
        "label": "secondary window (5h)",
        "used_percent": 33,
        "reset_after_seconds": 1200,
        "window_minutes": 300
      }
    ],
    "primary_over_secondary_limit_percent": 245
  }
}
```

Codex 429 failure bodies can additionally populate `error_type`, `plan_type`, `detail`, `resets_at`, and `resets_in_seconds`.

### Antigravity / Gemini CLI example

```json
{
  "provider": "antigravity",
  "quota_details": {
    "source": "upstream_api",
    "available": false,
    "credits": {
      "amount": 12,
      "minimum_for_usage": 20,
      "paid_tier_id": "g1-pro-tier",
      "paid_tier_name": "Google AI Pro",
      "current_tier_id": "free-tier",
      "current_tier_name": "Free",
      "cloudaicompanion_project": "project-123",
      "items": [
        {
          "credit_type": "OTHER",
          "amount": 999,
          "minimum_for_usage": 1
        },
        {
          "credit_type": "GOOGLE_ONE_AI",
          "amount": 12,
          "minimum_for_usage": 20
        }
      ],
      "ineligible_tiers": [
        {
          "tier_id": "ultra-tier",
          "tier_name": "Ultra",
          "reason_code": "INELIGIBLE_ACCOUNT",
          "reason_message": "Not eligible"
        }
      ],
      "allowed_tiers": [
        {
          "id": "free-tier",
          "name": "Free",
          "is_default": true
        }
      ]
    },
    "model_quotas": {
      "gemini-2.0-flash": {
        "remaining_fraction": 0.85,
        "reset_time": "2025-01-01T00:00:00Z",
        "display_name": "Gemini 2.0 Flash",
        "supports_images": true,
        "supports_thinking": true,
        "thinking_budget": 24576,
        "recommended": true,
        "max_tokens": 1000000,
        "max_output_tokens": 65536,
        "supported_mime_types": {
          "text/plain": true
        }
      }
    }
  }
}
```

Antigravity/Gemini CLI Google RPC quota failures can additionally populate `error_status`, `error_reason`, `model`, `retry_delay`, `detail`, `resets_at`, and `resets_in_seconds`.

## Provider Fields Collected

### Passive Mode (default)

Always active. Collects data from:

- Claude/Anthropic response headers:
  - `anthropic-ratelimit-unified-5h-*`
  - `anthropic-ratelimit-unified-7d-*`
  - `anthropic-ratelimit-unified-reset`
  - `anthropic-ratelimit-requests-*`
  - `anthropic-ratelimit-input-tokens-*`
  - `anthropic-ratelimit-output-tokens-*`
  - generic fallback `x-ratelimit-*-requests`, `x-ratelimit-*-tokens`, and `Retry-After` (delta seconds or HTTP-date)
- Codex response headers:
  - `x-codex-primary-used-percent`
  - `x-codex-primary-reset-after-seconds`
  - `x-codex-primary-window-minutes`
  - `x-codex-secondary-used-percent`
  - `x-codex-secondary-reset-after-seconds`
  - `x-codex-secondary-window-minutes`
  - `x-codex-primary-over-secondary-limit-percent`
- Codex failure bodies:
  - `error.type`
  - `error.message`
  - `error.resets_at`
  - `error.resets_in_seconds`
  - `error.plan_type`
- Antigravity/Gemini CLI Google RPC failure bodies:
  - `error.status`
  - `error.message`
  - `error.details[].reason`
  - `error.details[].metadata.model`
  - `error.details[].retryDelay`
- `host.auth.get_runtime` credential status fields for `quota_state`

### Active Mode (when `cpa-base-url` + `management-key` are configured)

Periodically queries upstream APIs through CPA's `api-call` endpoint:

- Claude OAuth usage API (`https://api.anthropic.com/api/oauth/usage`):
  - `five_hour.utilization`, `five_hour.resets_at`
  - `seven_day.utilization`, `seven_day.resets_at`
  - `seven_day_sonnet.utilization`, `seven_day_sonnet.resets_at`
- Codex usage panel (`https://chatgpt.com/backend-api/wham/usage`):
  - reads `plan_type`, `rate_limit` windows (primary/secondary), `code_review_rate_limit`, and `additional_rate_limits`
  - reads `rate_limit_reset_credits.available_count`
  - falls back to auth metadata `plan_type` when the panel response omits it
- Antigravity/Gemini CLI `loadCodeAssist`:
  - `cloudaicompanionProject`
  - `currentTier.id/name/description`
  - `paidTier.id/name/description`
  - all `paidTier.availableCredits[]` entries including `creditType`, `creditAmount`, and `minimumCreditAmountForUsage`
  - `ineligibleTiers[]` reason fields
  - `allowedTiers[]`
- Antigravity/Gemini CLI `fetchAvailableModels`:
  - `models.<model>.quotaInfo.remainingFraction`
  - `models.<model>.quotaInfo.resetTime`
  - model metadata such as display name, image/thinking support, thinking budget, max token fields, recommendation flag, and supported MIME types

For Antigravity credits, the legacy `credits.amount` and `credits.minimum_for_usage` fields are selected from `creditType == "GOOGLE_ONE_AI"` when present, otherwise from the first available credit.

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

## Build

```bash
go build -buildmode=c-shared -o credential-usage.so   # Linux/macOS
go build -buildmode=c-shared -o credential-usage.dll   # Windows
```

Place the output in CPA's plugins directory.
