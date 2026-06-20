# Panel-Aligned Active Quota Design

## Goal

Update the credential usage plugin's active quota polling so provider quota requests match the CPA management panel source code, while still running only inside this plugin repository and using CPA's existing Management API `api-call` endpoint.

The main correction is Codex: active Codex quota must use the management panel's `GET https://chatgpt.com/backend-api/wham/usage` request, not the plugin's current `POST /backend-api/codex/responses` minimal prompt probe.

## Non-Goals

- Do not modify CPA main program source code.
- Do not add a new CPA backend quota endpoint.
- Do not call provider APIs directly from the plugin with `net/http`; provider-bound requests still go through CPA.
- Do not execute or import the TypeScript management panel code at runtime.
- Do not keep the Codex active quota probe as a fallback.

## Source Findings

### CPA backend

CPA exposes a generic Management API proxy endpoint:

- `POST /v0/management/api-call`
- Request fields: `auth_index` / `authIndex` / `AuthIndex`, `method`, `url`, `header`, `data`
- Response fields: `status_code`, `header`, `body`
- It resolves `$TOKEN$` using the selected `auth_index`, applies credential/global proxy settings, and returns the upstream response.

Relevant CPA files verified locally:

- `C:\PythonProject\proxy\CLIProxyAPI\internal\api\server.go`
- `C:\PythonProject\proxy\CLIProxyAPI\internal\api\handlers\management\api_tools.go`

No existing CPA backend endpoint was found that directly returns unified Claude/Codex/Antigravity quota details. Therefore this plugin must still provide the `/api-call` request templates, but those templates should be centralized and aligned with the management panel source.

### CPA management panel

Management panel quota code defines provider requests in:

- `src/utils/quota/constants.ts`
- `src/components/quota/quotaConfigs.ts`
- `src/types/quota.ts`
- `src/services/api/antigravitySubscription.ts`

Important panel-derived requests:

- Codex usage: `GET https://chatgpt.com/backend-api/wham/usage`
- Claude usage: `GET https://api.anthropic.com/api/oauth/usage`
- Claude profile: `GET https://api.anthropic.com/api/oauth/profile`
- Antigravity quota summary: `POST .../v1internal:retrieveUserQuotaSummary`
- Antigravity subscription: `POST https://daily-cloudcode-pa.googleapis.com/v1internal:loadCodeAssist`

## Architecture

Active quota polling keeps this call chain:

```text
credential-usage plugin
  -> host.http.do
  -> CPA /v0/management/api-call
  -> CPA resolves auth_index token and proxy
  -> provider quota endpoint
  -> CPA returns status_code/header/body
  -> plugin normalizes into quota_details
```

The plugin owns three responsibilities:

1. Discover credentials and runtime state through CPA host calls.
2. Build management-panel-compatible `/api-call` payloads.
3. Normalize provider responses into the plugin's resource response shape.

Public plugin resources remain unchanged:

- `GET /v0/resource/plugins/credential-usage/list`
- `GET /v0/resource/plugins/credential-usage/detail?auth_index=<auth_index>`

## Component Design

### Credential discovery

Keep the existing host auth polling:

- `host.auth.list`
- `host.auth.get_runtime`

This populates the plugin store with `auth_index`, provider, label, email, status, and CPA runtime quota/cooldown state. It does not perform active provider quota queries.

### CPA API-call client

Keep a single helper that posts to:

```text
{cpa-base-url}/v0/management/api-call
```

through `host.http.do` with:

```text
Authorization: Bearer {management-key}
Content-Type: application/json
```

It should continue to unwrap the host response and decode the CPA `/api-call` response into:

```go
type apiCallResponse struct {
    StatusCode int                 `json:"status_code"`
    Header     map[string][]string `json:"header"`
    Body       string              `json:"body"`
}
```

### Panel quota request definitions

Add a centralized internal section or file for management-panel-compatible request definitions. Provider query functions should call these builders instead of inlining URL/header/body constants throughout active polling code.

The definitions are intentionally maintained in this plugin because the management panel exposes TypeScript functions, not reusable backend HTTP endpoints. Centralizing them limits drift and makes future panel syncs easy to review.

#### Codex definition

Active Codex quota uses the management panel's usage endpoint:

```text
method: GET
url: https://chatgpt.com/backend-api/wham/usage
headers:
  Authorization: Bearer $TOKEN$
  Content-Type: application/json
  User-Agent: codex_cli_rs/0.76.0 (Debian 13.0.0; x86_64) WindowsTerminal
optional header:
  Chatgpt-Account-Id: <resolved account id>
```

The builder must not emit:

- `https://chatgpt.com/backend-api/codex/responses`
- a request body containing a prompt such as `"hi"`

If the plugin can resolve a ChatGPT account id from auth metadata, id token, or attributes using the same candidate fields as the panel, add `Chatgpt-Account-Id`.

#### Claude definition

Active Claude quota uses two management panel requests:

```text
GET https://api.anthropic.com/api/oauth/usage
GET https://api.anthropic.com/api/oauth/profile
```

Headers:

```text
Authorization: Bearer $TOKEN$
Content-Type: application/json
anthropic-beta: oauth-2025-04-20
```

Usage is the required quota source. Profile is supplemental and only provides plan detection.

#### Antigravity definition

Active Antigravity quota summary tries these endpoints in panel order:

```text
POST https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary
POST https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:retrieveUserQuotaSummary
POST https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary
```

Body:

```json
{"project":"<project id>"}
```

Headers:

```text
Authorization: Bearer $TOKEN$
Content-Type: application/json
User-Agent: antigravity/cli/1.0.8 darwin/arm64
```

Subscription/plan uses:

```text
POST https://daily-cloudcode-pa.googleapis.com/v1internal:loadCodeAssist
```

Body:

```json
{"metadata":{"ideType":"ANTIGRAVITY"}}
```

Plan mapping follows the panel:

- `free-tier` -> `free`
- `g1-pro-tier` -> `pro`
- `g1-ultra-tier` -> `ultra`
- `g1-ultra-lite-tier` -> `ultra-lite`

## Response Normalization

### Codex

Parse the management panel `CodexUsagePayload` shape:

- `plan_type` / `planType`
- `rate_limit` / `rateLimit`
- `code_review_rate_limit` / `codeReviewRateLimit`
- `additional_rate_limits` / `additionalRateLimits`
- `rate_limit_reset_credits` / `rateLimitResetCredits`

Map primary and secondary windows into `quota_details.windows`. Preserve names that distinguish default, code-review, and additional limits so callers can display all returned windows.

Add these fields to `quotaDetails` for the panel Codex payload:

- `subscription_active_until`
- `rate_limit_reset_credits_available_count`

`plan_type` must prefer the usage payload value, then fall back to auth metadata.

### Claude

Parse all usage windows listed by the panel:

- `five_hour`
- `seven_day`
- `seven_day_oauth_apps`
- `seven_day_opus`
- `seven_day_sonnet`
- `seven_day_cowork`
- `iguana_necktie`

Keep existing `quota_details.windows` representation with stable names. Add a dedicated `extra_usage` field to `quotaDetails` that preserves Claude's `extra_usage.is_enabled`, `monthly_limit`, `used_credits`, and `utilization` values when present.

Use the profile response to infer `plan_type`:

- Max
- Pro
- Team
- Free
- unknown

Profile failure must not block usage window updates.

### Antigravity

Parse quota summary `groups[].buckets[]` from `retrieveUserQuotaSummary`:

- group label and description
- bucket label and description
- `remainingFraction`
- `resetTime`

The existing `model_quotas` map does not naturally represent grouped quota buckets. Add a dedicated `quota_groups` field to `quotaDetails` with group label/description and bucket label/description/remaining_fraction/reset_time fields, preserving the panel payload structure without forcing it into model quota semantics.

Parse subscription details from `loadCodeAssist`:

- current tier
- paid tier
- selected plan
- available credits

Existing `credits` fields can continue to hold credit/tier information, with plan mapping added.

## Error Handling

- Missing `cpa-base-url` or `management-key`: do not start active queries; passive usage collection and auth runtime polling continue.
- CPA `/api-call` request failure: log without token or management key; do not clear old quota details.
- CPA `/api-call` non-200: log provider/auth index and status; do not clear old quota details.
- Provider non-2xx:
  - Codex active usage: treat as active query failure; do not probe fallback.
  - Antigravity quota summary: continue to the next panel URL. Prefer 403/404 as final status if all URLs fail.
  - Claude usage: fail the active Claude update.
  - Claude profile: ignore profile failure if usage succeeds.
- Parse failure: log and preserve the previous successful quota details.

## Configuration

Keep existing configuration:

```yaml
plugins:
  configs:
    credential-usage:
      enabled: true
      cpa-base-url: "http://127.0.0.1:8317"
      management-key: "<CPA management key>"
      poll-interval: "5m"
```

No provider URL/header config is added in this design. The user selected centralized plugin builders with panel-compatible definitions. Future URL/header changes should update the central definitions and tests.

## Security and Privacy

- Never log tokens, management keys, or expanded `Authorization` headers.
- Use `$TOKEN$` placeholders in `/api-call` payloads so CPA resolves credentials.
- Store only quota/status summaries in plugin memory.
- Document that enabling active mode grants the plugin access to CPA Management API calls through the configured management key.

## Documentation Updates

Update `README.md` active mode docs:

- State that active queries use CPA `/v0/management/api-call` and panel-aligned request definitions.
- Remove Codex probe wording and the warning that the active Codex query sends a tiny prompt.
- Document Codex as `GET https://chatgpt.com/backend-api/wham/usage`.
- Document Claude usage + profile.
- Document Antigravity `retrieveUserQuotaSummary` + `loadCodeAssist`.
- Keep the statement that plugin resource endpoints remain `/v0/resource/plugins/credential-usage/list` and `/detail`.

## Test Plan

Run:

```bash
go test ./...
```

Add or update tests for:

1. Codex builder emits `GET https://chatgpt.com/backend-api/wham/usage`.
2. Codex builder does not emit `/backend-api/codex/responses` or a `"hi"` prompt body.
3. Codex parser handles `plan_type`, default windows, code-review windows, additional rate limits, reset credits, account id, and subscription expiry fallback.
4. Claude active query calls usage and profile endpoints.
5. Claude parser handles every panel-listed usage window and does not require profile success.
6. Antigravity builder tries all `retrieveUserQuotaSummary` URLs in panel order.
7. Antigravity subscription builder calls daily `loadCodeAssist` with the panel body.
8. Antigravity parser stores grouped quota buckets and tier/credit plan data.
9. Active query failures preserve previous `quota_details`.
10. Existing resource route tests remain green.

## Acceptance Criteria

- Active Codex quota no longer uses a probe request.
- Active provider request constants are centralized and explicitly marked as management-panel-compatible definitions.
- Plugin active queries still go through CPA `/v0/management/api-call`.
- Existing resource endpoints remain backward-compatible.
- README accurately reflects panel-aligned active quota behavior.
- `go test ./...` passes.
