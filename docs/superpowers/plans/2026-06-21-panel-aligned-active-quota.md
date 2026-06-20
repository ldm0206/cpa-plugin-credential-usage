# Panel-Aligned Active Quota Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace active quota polling's provider request definitions with CPA management-panel-aligned `/api-call` payloads, especially replacing the Codex prompt probe with `GET https://chatgpt.com/backend-api/wham/usage`.

**Architecture:** Keep plugin resource endpoints unchanged and keep all provider-bound requests routed through CPA `POST /v0/management/api-call`. Add centralized panel-compatible request builders, then update Codex, Claude, and Antigravity response normalization to store panel quota fields in `quota_details`.

**Tech Stack:** Go 1.26, CPA plugin ABI, standard-library JSON/base64/http/time helpers, existing `go test ./...` test suite.

## Global Constraints

- Implement only in this plugin repository: `C:\PythonProject\proxy\cpa-plugin-credential-usage`.
- Do not modify CPA main program source code.
- Do not create or use worktrees for implementation updates.
- Do not add a new CPA backend quota endpoint.
- Do not call provider APIs directly from the plugin with `net/http`; provider-bound requests must go through CPA `/v0/management/api-call` via `host.http.do`.
- Do not keep the Codex active quota probe as fallback.
- Do not log tokens, management keys, or expanded `Authorization` headers.
- Keep public plugin resources unchanged: `/v0/resource/plugins/credential-usage/list` and `/v0/resource/plugins/credential-usage/detail?auth_index=<auth_index>`.

---

## File Structure

- Modify `core.go`
  - Extend `quotaDetails` with panel fields.
  - Extend copy helpers for new pointer/slice fields.
  - Store safe Codex metadata-derived fields on `credentialEntry` with `json:"-"`.
  - Replace provider active query functions to call panel-aligned builders and normalizers.
  - Keep passive header/body parsing intact.
- Create `panel_quota_defs.go`
  - Centralize management-panel-compatible URLs, headers, `/api-call` payload builders, and small auth metadata resolvers.
  - No network calls in this file.
- Create `codex_active.go`
  - Define Codex `wham/usage` payload models.
  - Normalize Codex usage payload into `quotaDetails`.
- Create `claude_active.go`
  - Define expanded Claude usage/profile payload models.
  - Normalize all panel-listed windows, extra usage, and profile-derived plan type.
- Create `antigravity_active.go`
  - Define Antigravity quota summary and subscription payload models.
  - Normalize grouped quota buckets and tier/credit plan data.
- Modify `main_test.go`
  - Add focused tests for builders and normalizers.
  - Update tests that currently expect the old active Codex probe or old Claude/Antigravity active shapes.
- Modify `README.md`
  - Replace active mode probe documentation with panel-aligned request documentation.

Each task below ends in a commit. If a task exposes compile errors in code that a later task depends on, finish the current task's implementation until `go test ./...` passes before committing.

---

### Task 1: Add panel fields and centralized request builders

**Files:**
- Modify: `core.go`
- Create: `panel_quota_defs.go`
- Test: `main_test.go`

**Interfaces:**
- Consumes: existing `quotaDetails`, `credentialEntry`, `hostAuthFileEntry`, `apiCallResponse`, and `queryManagementAPICallFull(authIndex string, apiCallPayload []byte) (*apiCallResponse, bool)`.
- Produces:
  - `buildCodexUsageAPICallPayload(authIndex string, entry *credentialEntry) []byte`
  - `buildClaudeUsageAPICallPayload(authIndex string) []byte`
  - `buildClaudeProfileAPICallPayload(authIndex string) []byte`
  - `buildAntigravityQuotaSummaryAPICallPayload(authIndex, projectID, url string) []byte`
  - `buildAntigravitySubscriptionAPICallPayload(authIndex string) []byte`
  - constants `codexUsageURL`, `claudeUsageURL`, `claudeProfileURL`, `antigravityQuotaSummaryURLs`, `antigravityCodeAssistURL`
  - `quotaDetails.ExtraUsage`, `quotaDetails.QuotaGroups`, `quotaDetails.SubscriptionActiveUntil`, `quotaDetails.RateLimitResetCreditsAvailableCount`
  - safe private metadata fields on `credentialEntry`: `CodexAccountID`, `CodexPlanTypeFallback`, `CodexSubscriptionActiveUntil`, `AntigravityProjectID`

- [ ] **Step 1: Write failing tests for request builders and deep-copy fields**

Add these tests near the end of `main_test.go`, before `findQuotaWindow`:

```go
func TestPanelCodexUsageBuilderMatchesManagementPanel(t *testing.T) {
	entry := &credentialEntry{CodexAccountID: "account-123"}
	payload := buildCodexUsageAPICallPayload("codex-auth", entry)

	var req map[string]any
	if err := json.Unmarshal(payload, &req); err != nil {
		t.Fatalf("unmarshal payload: %v body=%s", err, string(payload))
	}
	if req["auth_index"] != "codex-auth" {
		t.Fatalf("auth_index = %v, want codex-auth", req["auth_index"])
	}
	if req["method"] != "GET" {
		t.Fatalf("method = %v, want GET", req["method"])
	}
	if req["url"] != "https://chatgpt.com/backend-api/wham/usage" {
		t.Fatalf("url = %v, want wham usage", req["url"])
	}
	if _, ok := req["data"]; ok {
		t.Fatalf("payload should not include data body: %s", string(payload))
	}
	body := string(payload)
	if strings.Contains(body, "/backend-api/codex/responses") || strings.Contains(body, `"hi"`) {
		t.Fatalf("payload still contains probe artifacts: %s", body)
	}
	header, ok := req["header"].(map[string]any)
	if !ok {
		t.Fatalf("header = %#v, want object", req["header"])
	}
	if header["Authorization"] != "Bearer $TOKEN$" {
		t.Fatalf("Authorization = %v, want Bearer $TOKEN$", header["Authorization"])
	}
	if header["Chatgpt-Account-Id"] != "account-123" {
		t.Fatalf("Chatgpt-Account-Id = %v, want account-123", header["Chatgpt-Account-Id"])
	}
}

func TestPanelClaudeBuildersMatchManagementPanel(t *testing.T) {
	usage := buildClaudeUsageAPICallPayload("claude-auth")
	profile := buildClaudeProfileAPICallPayload("claude-auth")

	for name, payload := range map[string][]byte{"usage": usage, "profile": profile} {
		var req map[string]any
		if err := json.Unmarshal(payload, &req); err != nil {
			t.Fatalf("%s unmarshal payload: %v body=%s", name, err, string(payload))
		}
		if req["auth_index"] != "claude-auth" || req["method"] != "GET" {
			t.Fatalf("%s request = %+v, want auth_index claude-auth and GET", name, req)
		}
		header := req["header"].(map[string]any)
		if header["Authorization"] != "Bearer $TOKEN$" || header["anthropic-beta"] != "oauth-2025-04-20" {
			t.Fatalf("%s headers = %+v, want panel Claude headers", name, header)
		}
	}
	var usageReq map[string]any
	var profileReq map[string]any
	_ = json.Unmarshal(usage, &usageReq)
	_ = json.Unmarshal(profile, &profileReq)
	if usageReq["url"] != "https://api.anthropic.com/api/oauth/usage" {
		t.Fatalf("usage url = %v", usageReq["url"])
	}
	if profileReq["url"] != "https://api.anthropic.com/api/oauth/profile" {
		t.Fatalf("profile url = %v", profileReq["url"])
	}
}

func TestPanelAntigravityBuildersMatchManagementPanel(t *testing.T) {
	if len(antigravityQuotaSummaryURLs) != 3 {
		t.Fatalf("antigravity urls = %v, want 3", antigravityQuotaSummaryURLs)
	}
	if antigravityQuotaSummaryURLs[0] != "https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary" {
		t.Fatalf("first antigravity quota URL = %q", antigravityQuotaSummaryURLs[0])
	}

	payload := buildAntigravityQuotaSummaryAPICallPayload("ag-auth", "project-123", antigravityQuotaSummaryURLs[0])
	var req map[string]any
	if err := json.Unmarshal(payload, &req); err != nil {
		t.Fatalf("unmarshal quota payload: %v body=%s", err, string(payload))
	}
	if req["auth_index"] != "ag-auth" || req["method"] != "POST" || req["url"] != antigravityQuotaSummaryURLs[0] {
		t.Fatalf("quota request = %+v", req)
	}
	if req["data"] != `{"project":"project-123"}` {
		t.Fatalf("quota data = %v, want project JSON", req["data"])
	}

	subscription := buildAntigravitySubscriptionAPICallPayload("ag-auth")
	var subReq map[string]any
	if err := json.Unmarshal(subscription, &subReq); err != nil {
		t.Fatalf("unmarshal subscription payload: %v body=%s", err, string(subscription))
	}
	if subReq["url"] != "https://daily-cloudcode-pa.googleapis.com/v1internal:loadCodeAssist" {
		t.Fatalf("subscription url = %v", subReq["url"])
	}
	if subReq["data"] != `{"metadata":{"ideType":"ANTIGRAVITY"}}` {
		t.Fatalf("subscription data = %v", subReq["data"])
	}
}

func TestCopyQuotaDetailsCopiesPanelFields(t *testing.T) {
	resetCount := int64(2)
	details := quotaDetails{
		ExtraUsage: &claudeExtraUsage{IsEnabled: true, MonthlyLimit: 100, UsedCredits: 40, Utilization: float64Ptr(0.4)},
		QuotaGroups: []quotaGroup{{
			ID: "group-1", Label: "Group", Description: "Desc",
			Buckets: []quotaBucket{{ID: "bucket-1", Label: "Bucket", RemainingFraction: float64Ptr(0.75), ResetTime: "2026-06-21T00:00:00Z"}},
		}},
		SubscriptionActiveUntil:              "2026-07-01T00:00:00Z",
		RateLimitResetCreditsAvailableCount: &resetCount,
	}

	copied := copyQuotaDetails(details)
	if copied.ExtraUsage == nil || copied.ExtraUsage.Utilization == nil || *copied.ExtraUsage.Utilization != 0.4 {
		t.Fatalf("extra_usage copy = %+v", copied.ExtraUsage)
	}
	*copied.ExtraUsage.Utilization = 0.9
	copied.QuotaGroups[0].Buckets[0].Label = "mutated"
	*copied.QuotaGroups[0].Buckets[0].RemainingFraction = 0.1
	*copied.RateLimitResetCreditsAvailableCount = 9

	if *details.ExtraUsage.Utilization != 0.4 {
		t.Fatalf("original extra usage mutated: %+v", details.ExtraUsage)
	}
	if details.QuotaGroups[0].Buckets[0].Label != "Bucket" || *details.QuotaGroups[0].Buckets[0].RemainingFraction != 0.75 {
		t.Fatalf("original quota group mutated: %+v", details.QuotaGroups)
	}
	if *details.RateLimitResetCreditsAvailableCount != 2 {
		t.Fatalf("original reset credits mutated: %v", details.RateLimitResetCreditsAvailableCount)
	}
}
```

Add `strings` to the `main_test.go` import block:

```go
import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./...
```

Expected: FAIL with undefined symbols such as `buildCodexUsageAPICallPayload`, `antigravityQuotaSummaryURLs`, `claudeExtraUsage`, `quotaGroup`, or `quotaBucket`.

- [ ] **Step 3: Extend data models and deep-copy helpers in `core.go`**

Update `quotaDetails` in `core.go` to include the panel fields. Add these fields after `ModelQuotas`:

```go
	ExtraUsage                       *claudeExtraUsage     `json:"extra_usage,omitempty"`
	QuotaGroups                      []quotaGroup          `json:"quota_groups,omitempty"`
	SubscriptionActiveUntil          string                `json:"subscription_active_until,omitempty"`
	RateLimitResetCreditsAvailableCount *int64            `json:"rate_limit_reset_credits_available_count,omitempty"`
```

Then add these types near the existing quota/credit/model types:

```go
type claudeExtraUsage struct {
	IsEnabled    bool     `json:"is_enabled"`
	MonthlyLimit float64  `json:"monthly_limit"`
	UsedCredits  float64  `json:"used_credits"`
	Utilization  *float64 `json:"utilization,omitempty"`
}

type quotaGroup struct {
	ID          string        `json:"id,omitempty"`
	Label       string        `json:"label,omitempty"`
	Description string        `json:"description,omitempty"`
	Buckets     []quotaBucket `json:"buckets,omitempty"`
}

type quotaBucket struct {
	ID                string   `json:"id,omitempty"`
	Label             string   `json:"label,omitempty"`
	Description       string   `json:"description,omitempty"`
	RemainingFraction *float64 `json:"remaining_fraction,omitempty"`
	ResetTime         string   `json:"reset_time,omitempty"`
}
```

Extend `credentialEntry` with private safe fields after `LastActiveAt`:

```go
	CodexAccountID                 string `json:"-"`
	CodexPlanTypeFallback          string `json:"-"`
	CodexSubscriptionActiveUntil   string `json:"-"`
	AntigravityProjectID           string `json:"-"`
```

Extend `hostAuthFileEntry` with safe metadata fields after `NextRetryAfter`:

```go
	ProjectID                       string         `json:"project_id,omitempty"`
	Metadata                        map[string]any `json:"metadata,omitempty"`
	Attributes                      map[string]any `json:"attributes,omitempty"`
	CodexAccountID                  string         `json:"chatgpt_account_id,omitempty"`
	CodexPlanType                   string         `json:"plan_type,omitempty"`
	CodexSubscriptionActiveUntil    string         `json:"subscription_active_until,omitempty"`
```

In `copyQuotaDetails`, after the existing `copyDetails.Credits = copyCreditDetails(details.Credits)` line, add:

```go
	copyDetails.ExtraUsage = copyClaudeExtraUsage(details.ExtraUsage)
	copyDetails.QuotaGroups = copyQuotaGroups(details.QuotaGroups)
```

After the existing `PrimaryOverSecondaryLimitPercent` copy block, add:

```go
	if details.RateLimitResetCreditsAvailableCount != nil {
		copyDetails.RateLimitResetCreditsAvailableCount = int64Ptr(*details.RateLimitResetCreditsAvailableCount)
	}
```

Add helper functions near the other copy helpers:

```go
func copyClaudeExtraUsage(extra *claudeExtraUsage) *claudeExtraUsage {
	if extra == nil {
		return nil
	}
	copyExtra := *extra
	if extra.Utilization != nil {
		copyExtra.Utilization = float64Ptr(*extra.Utilization)
	}
	return &copyExtra
}

func copyQuotaGroups(groups []quotaGroup) []quotaGroup {
	if groups == nil {
		return nil
	}
	out := make([]quotaGroup, len(groups))
	for i, group := range groups {
		out[i] = group
		out[i].Buckets = make([]quotaBucket, len(group.Buckets))
		for j, bucket := range group.Buckets {
			out[i].Buckets[j] = bucket
			if bucket.RemainingFraction != nil {
				out[i].Buckets[j].RemainingFraction = float64Ptr(*bucket.RemainingFraction)
			}
		}
	}
	return out
}
```

In `mergeAuthFileEntry`, before setting quota state, copy safe metadata into the stored entry:

```go
	if entry.ProjectID != "" {
		storeEntry.AntigravityProjectID = entry.ProjectID
	}
	if v := firstNonEmptyStringValue(
		entry.CodexAccountID,
		stringFromMap(entry.Metadata, "chatgpt_account_id"),
		stringFromMap(entry.Metadata, "chatgptAccountId"),
		stringFromMap(entry.Attributes, "chatgpt_account_id"),
		stringFromMap(entry.Attributes, "chatgptAccountId"),
	); v != "" {
		storeEntry.CodexAccountID = v
	}
	if v := firstNonEmptyStringValue(
		entry.CodexPlanType,
		stringFromMap(entry.Metadata, "plan_type"),
		stringFromMap(entry.Metadata, "planType"),
		stringFromMap(entry.Attributes, "plan_type"),
		stringFromMap(entry.Attributes, "planType"),
	); v != "" {
		storeEntry.CodexPlanTypeFallback = v
	}
	if v := firstNonEmptyStringValue(
		entry.CodexSubscriptionActiveUntil,
		stringFromMap(entry.Metadata, "chatgpt_subscription_active_until"),
		stringFromMap(entry.Metadata, "chatgptSubscriptionActiveUntil"),
		stringFromMap(entry.Metadata, "subscription_active_until"),
		stringFromMap(entry.Metadata, "subscriptionActiveUntil"),
		stringFromMap(entry.Attributes, "chatgpt_subscription_active_until"),
		stringFromMap(entry.Attributes, "chatgptSubscriptionActiveUntil"),
		stringFromMap(entry.Attributes, "subscription_active_until"),
		stringFromMap(entry.Attributes, "subscriptionActiveUntil"),
	); v != "" {
		storeEntry.CodexSubscriptionActiveUntil = v
	}
	if v := firstNonEmptyStringValue(
		storeEntry.AntigravityProjectID,
		stringFromMap(entry.Metadata, "project_id"),
		stringFromMap(entry.Metadata, "projectId"),
		stringFromMap(entry.Attributes, "project_id"),
		stringFromMap(entry.Attributes, "projectId"),
	); v != "" {
		storeEntry.AntigravityProjectID = v
	}
```

- [ ] **Step 4: Create `panel_quota_defs.go` with centralized builders**

Create `panel_quota_defs.go` with this content:

```go
package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	codexUsageURL = "https://chatgpt.com/backend-api/wham/usage"
	codexResetCreditsConsumeURL = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits/consume"

	claudeUsageURL   = "https://api.anthropic.com/api/oauth/usage"
	claudeProfileURL = "https://api.anthropic.com/api/oauth/profile"

	antigravityCodeAssistURL = "https://daily-cloudcode-pa.googleapis.com/v1internal:loadCodeAssist"
)

var antigravityQuotaSummaryURLs = []string{
	"https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary",
	"https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:retrieveUserQuotaSummary",
	"https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary",
}

type panelAPICallPayload struct {
	AuthIndex string            `json:"auth_index"`
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Header    map[string]string `json:"header,omitempty"`
	Data      string            `json:"data,omitempty"`
}

func marshalPanelAPICallPayload(payload panelAPICallPayload) []byte {
	raw, _ := json.Marshal(payload)
	return raw
}

func buildCodexUsageAPICallPayload(authIndex string, entry *credentialEntry) []byte {
	header := map[string]string{
		"Authorization": "Bearer $TOKEN$",
		"Content-Type":  "application/json",
		"User-Agent":    "codex_cli_rs/0.76.0 (Debian 13.0.0; x86_64) WindowsTerminal",
	}
	if entry != nil && strings.TrimSpace(entry.CodexAccountID) != "" {
		header["Chatgpt-Account-Id"] = strings.TrimSpace(entry.CodexAccountID)
	}
	return marshalPanelAPICallPayload(panelAPICallPayload{
		AuthIndex: authIndex,
		Method:    "GET",
		URL:       codexUsageURL,
		Header:    header,
	})
}

func buildClaudeUsageAPICallPayload(authIndex string) []byte {
	return buildClaudeAPICallPayload(authIndex, claudeUsageURL)
}

func buildClaudeProfileAPICallPayload(authIndex string) []byte {
	return buildClaudeAPICallPayload(authIndex, claudeProfileURL)
}

func buildClaudeAPICallPayload(authIndex, url string) []byte {
	return marshalPanelAPICallPayload(panelAPICallPayload{
		AuthIndex: authIndex,
		Method:    "GET",
		URL:       url,
		Header: map[string]string{
			"Authorization":  "Bearer $TOKEN$",
			"Content-Type":   "application/json",
			"anthropic-beta": "oauth-2025-04-20",
		},
	})
}

func buildAntigravityQuotaSummaryAPICallPayload(authIndex, projectID, url string) []byte {
	return marshalPanelAPICallPayload(panelAPICallPayload{
		AuthIndex: authIndex,
		Method:    "POST",
		URL:       url,
		Header:    antigravityPanelHeaders(),
		Data:      fmt.Sprintf(`{"project":%q}`, projectID),
	})
}

func buildAntigravitySubscriptionAPICallPayload(authIndex string) []byte {
	return marshalPanelAPICallPayload(panelAPICallPayload{
		AuthIndex: authIndex,
		Method:    "POST",
		URL:       antigravityCodeAssistURL,
		Header:    antigravityPanelHeaders(),
		Data:      `{"metadata":{"ideType":"ANTIGRAVITY"}}`,
	})
}

func antigravityPanelHeaders() map[string]string {
	return map[string]string{
		"Authorization": "Bearer $TOKEN$",
		"Content-Type":  "application/json",
		"User-Agent":    "antigravity/cli/1.0.8 darwin/arm64",
	}
}

func firstNonEmptyStringValue(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func stringFromMap(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	case json.Number:
		return strings.TrimSpace(typed.String())
	case float64:
		return strings.TrimSpace(fmt.Sprintf("%.0f", typed))
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}
```

- [ ] **Step 5: Run tests to verify Task 1 passes**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 6: Commit Task 1**

```bash
git add core.go panel_quota_defs.go main_test.go
git commit -m "feat: add panel quota request definitions"
```

---

### Task 2: Replace active Codex probe with panel `wham/usage`

**Files:**
- Create: `codex_active.go`
- Modify: `core.go`
- Test: `main_test.go`

**Interfaces:**
- Consumes: `buildCodexUsageAPICallPayload(authIndex string, entry *credentialEntry) []byte`, `queryManagementAPICallFull`, and safe Codex fields on `credentialEntry`.
- Produces:
  - `type codexUsageResponse`
  - `applyCodexUsageResponse(authIndex string, resp *codexUsageResponse)`
  - updated `queryCodexQuota(authIndex string)` that calls `GET /backend-api/wham/usage` and never sends a prompt probe.

- [ ] **Step 1: Write failing tests for Codex usage parsing and active query replacement**

Add these tests before `findQuotaWindow` in `main_test.go`:

```go
func TestApplyCodexUsageResponseStoresPanelUsagePayload(t *testing.T) {
	resetTestStore()
	store.mu.Lock()
	entry := store.getOrCreate("codex-usage", "codex", "auth-codex-usage")
	entry.CodexPlanTypeFallback = "plus"
	entry.CodexSubscriptionActiveUntil = "2026-07-01T00:00:00Z"
	store.mu.Unlock()

	resp := &codexUsageResponse{
		PlanType: "pro",
		RateLimit: &codexRateLimitInfo{
			Allowed: true,
			PrimaryWindow: &codexUsageWindow{UsedPercent: flexibleFloatPtr(12.5), LimitWindowSeconds: int64Ptr(18000), ResetAfterSeconds: int64Ptr(3600)},
			SecondaryWindow: &codexUsageWindow{UsedPercent: flexibleFloatPtr(50), LimitWindowSeconds: int64Ptr(604800), ResetAt: "2026-06-28T00:00:00Z"},
		},
		CodeReviewRateLimit: &codexRateLimitInfo{
			PrimaryWindow: &codexUsageWindow{UsedPercent: flexibleFloatPtr(20), LimitWindowSeconds: int64Ptr(18000)},
		},
		AdditionalRateLimits: []codexAdditionalRateLimit{{
			LimitName: "team_monthly",
			RateLimit: &codexRateLimitInfo{SecondaryWindow: &codexUsageWindow{UsedPercent: flexibleFloatPtr(75), LimitWindowSeconds: int64Ptr(2592000)}},
		}},
		RateLimitResetCredits: &codexRateLimitResetCredits{AvailableCount: int64Ptr(3)},
	}

	applyCodexUsageResponse("codex-usage", resp)

	entry = store.getByIndex("codex-usage")
	if entry == nil {
		t.Fatalf("missing codex credential")
	}
	if entry.QuotaDetails.Source != "codex_usage_api" {
		t.Fatalf("source = %q, want codex_usage_api", entry.QuotaDetails.Source)
	}
	if entry.QuotaDetails.PlanType != "pro" {
		t.Fatalf("plan_type = %q, want pro", entry.QuotaDetails.PlanType)
	}
	if entry.QuotaDetails.SubscriptionActiveUntil != "2026-07-01T00:00:00Z" {
		t.Fatalf("subscription_active_until = %q", entry.QuotaDetails.SubscriptionActiveUntil)
	}
	if entry.QuotaDetails.RateLimitResetCreditsAvailableCount == nil || *entry.QuotaDetails.RateLimitResetCreditsAvailableCount != 3 {
		t.Fatalf("reset credits = %v, want 3", entry.QuotaDetails.RateLimitResetCreditsAvailableCount)
	}
	for _, name := range []string{"primary", "secondary", "code_review_primary", "team_monthly_secondary"} {
		if findQuotaWindow(entry.QuotaDetails.Windows, name) == nil {
			t.Fatalf("missing codex window %q in %+v", name, entry.QuotaDetails.Windows)
		}
	}
}

func TestApplyCodexUsageResponseFallsBackToAuthPlan(t *testing.T) {
	resetTestStore()
	store.mu.Lock()
	entry := store.getOrCreate("codex-fallback", "codex", "auth-codex-fallback")
	entry.CodexPlanTypeFallback = "team"
	store.mu.Unlock()

	applyCodexUsageResponse("codex-fallback", &codexUsageResponse{})

	entry = store.getByIndex("codex-fallback")
	if entry.QuotaDetails.PlanType != "team" {
		t.Fatalf("plan_type = %q, want fallback team", entry.QuotaDetails.PlanType)
	}
}

func flexibleFloatPtr(v flexibleFloat) *flexibleFloat { return &v }
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./...
```

Expected: FAIL with undefined symbols such as `codexUsageResponse`, `codexRateLimitInfo`, `codexUsageWindow`, or `applyCodexUsageResponse`.

- [ ] **Step 3: Create `codex_active.go` with Codex panel payload types and normalizer**

Create `codex_active.go`:

```go
package main

import (
	"fmt"
	"strings"
	"time"
)

type codexUsageResponse struct {
	PlanType                 string                       `json:"plan_type"`
	PlanTypeCamel            string                       `json:"planType"`
	RateLimit                *codexRateLimitInfo          `json:"rate_limit"`
	RateLimitCamel           *codexRateLimitInfo          `json:"rateLimit"`
	CodeReviewRateLimit      *codexRateLimitInfo          `json:"code_review_rate_limit"`
	CodeReviewRateLimitCamel *codexRateLimitInfo          `json:"codeReviewRateLimit"`
	AdditionalRateLimits     []codexAdditionalRateLimit   `json:"additional_rate_limits"`
	AdditionalRateLimitsCamel []codexAdditionalRateLimit  `json:"additionalRateLimits"`
	RateLimitResetCredits    *codexRateLimitResetCredits  `json:"rate_limit_reset_credits"`
	RateLimitResetCreditsCamel *codexRateLimitResetCredits `json:"rateLimitResetCredits"`
}

type codexRateLimitInfo struct {
	Allowed              *bool             `json:"allowed"`
	LimitReached         *bool             `json:"limit_reached"`
	LimitReachedCamel    *bool             `json:"limitReached"`
	PrimaryWindow        *codexUsageWindow `json:"primary_window"`
	PrimaryWindowCamel   *codexUsageWindow `json:"primaryWindow"`
	SecondaryWindow      *codexUsageWindow `json:"secondary_window"`
	SecondaryWindowCamel *codexUsageWindow `json:"secondaryWindow"`
}

type codexUsageWindow struct {
	UsedPercent              *flexibleFloat `json:"used_percent"`
	UsedPercentCamel         *flexibleFloat `json:"usedPercent"`
	LimitWindowSeconds       *int64         `json:"limit_window_seconds"`
	LimitWindowSecondsCamel  *int64         `json:"limitWindowSeconds"`
	ResetAfterSeconds        *int64         `json:"reset_after_seconds"`
	ResetAfterSecondsCamel   *int64         `json:"resetAfterSeconds"`
	ResetAt                  string         `json:"reset_at"`
	ResetAtCamel             string         `json:"resetAt"`
}

type codexAdditionalRateLimit struct {
	LimitName           string              `json:"limit_name"`
	LimitNameCamel      string              `json:"limitName"`
	MeteredFeature      string              `json:"metered_feature"`
	MeteredFeatureCamel string              `json:"meteredFeature"`
	RateLimit           *codexRateLimitInfo `json:"rate_limit"`
	RateLimitCamel      *codexRateLimitInfo `json:"rateLimit"`
}

type codexRateLimitResetCredits struct {
	AvailableCount      *int64 `json:"available_count"`
	AvailableCountCamel *int64 `json:"availableCount"`
}

func applyCodexUsageResponse(authIndex string, resp *codexUsageResponse) {
	store.mu.Lock()
	defer store.mu.Unlock()

	entry := store.data[authIndex]
	if entry == nil || resp == nil {
		return
	}

	details := entry.QuotaDetails
	details.Source = "codex_usage_api"
	details.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	details.PlanType = firstNonEmptyStringValue(resp.PlanType, resp.PlanTypeCamel, entry.CodexPlanTypeFallback)
	details.SubscriptionActiveUntil = entry.CodexSubscriptionActiveUntil
	if credits := firstInt64Ptr(resp.RateLimitResetCredits, resp.RateLimitResetCreditsCamel); credits != nil {
		details.RateLimitResetCreditsAvailableCount = credits
	}

	windows := make([]quotaWindow, 0)
	windows = appendCodexRateLimitWindows(windows, "", firstCodexRateLimit(resp.RateLimit, resp.RateLimitCamel))
	windows = appendCodexRateLimitWindows(windows, "code_review", firstCodexRateLimit(resp.CodeReviewRateLimit, resp.CodeReviewRateLimitCamel))
	for _, additional := range append(resp.AdditionalRateLimits, resp.AdditionalRateLimitsCamel...) {
		name := sanitizeQuotaName(firstNonEmptyStringValue(additional.LimitName, additional.LimitNameCamel, additional.MeteredFeature, additional.MeteredFeatureCamel))
		if name == "" {
			name = "additional"
		}
		windows = appendCodexRateLimitWindows(windows, name, firstCodexRateLimit(additional.RateLimit, additional.RateLimitCamel))
	}
	for _, window := range windows {
		details.Windows = upsertQuotaWindow(details.Windows, window)
	}
	available := codexUsageAvailable(resp)
	if available != nil {
		details.Available = available
	}
	entry.QuotaDetails = details
}

func firstInt64Ptr(values ...*codexRateLimitResetCredits) *int64 {
	for _, value := range values {
		if value == nil {
			continue
		}
		if value.AvailableCount != nil {
			return int64Ptr(*value.AvailableCount)
		}
		if value.AvailableCountCamel != nil {
			return int64Ptr(*value.AvailableCountCamel)
		}
	}
	return nil
}

func firstCodexRateLimit(values ...*codexRateLimitInfo) *codexRateLimitInfo {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func appendCodexRateLimitWindows(out []quotaWindow, prefix string, limit *codexRateLimitInfo) []quotaWindow {
	if limit == nil {
		return out
	}
	if window := codexPanelWindow(prefix, "primary", firstCodexWindow(limit.PrimaryWindow, limit.PrimaryWindowCamel)); window != nil {
		out = append(out, *window)
	}
	if window := codexPanelWindow(prefix, "secondary", firstCodexWindow(limit.SecondaryWindow, limit.SecondaryWindowCamel)); window != nil {
		out = append(out, *window)
	}
	return out
}

func firstCodexWindow(values ...*codexUsageWindow) *codexUsageWindow {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func codexPanelWindow(prefix, role string, input *codexUsageWindow) *quotaWindow {
	if input == nil {
		return nil
	}
	name := role
	if prefix != "" {
		name = prefix + "_" + role
	}
	used := firstFlexibleFloat(input.UsedPercent, input.UsedPercentCamel)
	seconds := firstInt64(input.LimitWindowSeconds, input.LimitWindowSecondsCamel)
	resetAfter := firstInt64(input.ResetAfterSeconds, input.ResetAfterSecondsCamel)
	resetAt := firstNonEmptyStringValue(input.ResetAt, input.ResetAtCamel)
	if used == nil && seconds == nil && resetAfter == nil && resetAt == "" {
		return nil
	}
	return &quotaWindow{
		Name:              name,
		Label:             codexWindowLabelFromSeconds(name, seconds),
		UsedPercent:       flexibleFloatToFloat64Ptr(used),
		WindowMinutes:     secondsToMinutes(seconds),
		ResetAfterSeconds: resetAfter,
		ResetAt:           resetAt,
	}
}

func firstFlexibleFloat(values ...*flexibleFloat) *flexibleFloat {
	for _, value := range values {
		if value != nil {
			out := *value
			return &out
		}
	}
	return nil
}

func firstInt64(values ...*int64) *int64 {
	for _, value := range values {
		if value != nil {
			return int64Ptr(*value)
		}
	}
	return nil
}

func flexibleFloatToFloat64Ptr(value *flexibleFloat) *float64 {
	if value == nil {
		return nil
	}
	return float64Ptr(float64(*value))
}

func secondsToMinutes(seconds *int64) *int64 {
	if seconds == nil {
		return nil
	}
	return int64Ptr(*seconds / 60)
}

func codexWindowLabelFromSeconds(name string, seconds *int64) string {
	if seconds == nil || *seconds <= 0 {
		return name + " window"
	}
	return fmt.Sprintf("%s window (%dm)", name, *seconds/60)
}

func codexUsageAvailable(resp *codexUsageResponse) *bool {
	for _, limit := range []*codexRateLimitInfo{
		firstCodexRateLimit(resp.RateLimit, resp.RateLimitCamel),
		firstCodexRateLimit(resp.CodeReviewRateLimit, resp.CodeReviewRateLimitCamel),
	} {
		if limit == nil {
			continue
		}
		if limit.Allowed != nil {
			return boolValuePtr(*limit.Allowed)
		}
		if limit.LimitReached != nil {
			return boolValuePtr(!*limit.LimitReached)
		}
		if limit.LimitReachedCamel != nil {
			return boolValuePtr(!*limit.LimitReachedCamel)
		}
	}
	return nil
}

func boolValuePtr(value bool) *bool {
	return &value
}

func sanitizeQuotaName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	return value
}
```

- [ ] **Step 4: Replace `queryCodexQuota` in `core.go`**

Replace the whole existing `queryCodexQuota` function with:

```go
func queryCodexQuota(authIndex string) {
	if cfg.CPABaseURL == "" || cfg.ManagementKey == "" {
		return
	}
	entry := store.getByIndex(authIndex)
	apiResp, ok := queryManagementAPICallFull(authIndex, buildCodexUsageAPICallPayload(authIndex, entry))
	if !ok || apiResp == nil {
		return
	}
	var usageResp codexUsageResponse
	if err := json.Unmarshal([]byte(apiResp.Body), &usageResp); err != nil {
		callHostLog("error", fmt.Sprintf("credential-usage: parse Codex usage failed for %s: %v", authIndex, err))
		return
	}
	applyCodexUsageResponse(authIndex, &usageResp)
}
```

Keep `applyCodexAPIResponse` and passive Codex header/429 parsing for real request observations; do not call it from active Codex polling anymore.

- [ ] **Step 5: Run tests to verify Task 2 passes**

Run:

```bash
go test ./...
```

Expected: PASS. The new builder test must prove no `/backend-api/codex/responses` or `"hi"` prompt exists in the active Codex payload.

- [ ] **Step 6: Commit Task 2**

```bash
git add core.go codex_active.go main_test.go
git commit -m "feat: align codex active quota with panel usage API"
```

---

### Task 3: Expand Claude active quota to panel usage and profile

**Files:**
- Create: `claude_active.go`
- Modify: `core.go`
- Test: `main_test.go`

**Interfaces:**
- Consumes: `buildClaudeUsageAPICallPayload`, `buildClaudeProfileAPICallPayload`, `queryManagementAPICallFull`.
- Produces:
  - expanded `claudeUsageResponse` handling all panel windows and `extra_usage`.
  - `type claudeProfileResponse`
  - `updateClaudeUsageQuota(authIndex string, resp *claudeUsageResponse)` with all panel windows.
  - `updateClaudePlanFromProfile(authIndex string, resp *claudeProfileResponse)`.
  - `queryClaudeUsage(authIndex string)` that fetches usage and profile; profile failure does not block usage.

- [ ] **Step 1: Write failing tests for all Claude windows, extra usage, and profile plan**

Replace the existing `TestUpdateClaudeUsageQuotaStoresActiveUsageAPIWindows` with:

```go
func TestUpdateClaudeUsageQuotaStoresPanelWindowsAndExtraUsage(t *testing.T) {
	resetTestStore()
	store.mu.Lock()
	store.getOrCreate("claude-active", "claude", "auth-claude-active")
	store.mu.Unlock()

	resp := &claudeUsageResponse{}
	resp.FiveHour.Utilization = 0.25
	resp.FiveHour.ResetsAt = "2026-06-20T15:00:00Z"
	resp.SevenDay.Utilization = 0.6
	resp.SevenDay.ResetsAt = "2026-06-24T00:00:00Z"
	resp.SevenDayOAuthApps.Utilization = 0.7
	resp.SevenDayOAuthApps.ResetsAt = "2026-06-24T01:00:00Z"
	resp.SevenDayOpus.Utilization = 0.2
	resp.SevenDayOpus.ResetsAt = "2026-06-24T02:00:00Z"
	resp.SevenDaySonnet.Utilization = 0.8
	resp.SevenDaySonnet.ResetsAt = "2026-06-25T00:00:00Z"
	resp.SevenDayCowork.Utilization = 0.1
	resp.SevenDayCowork.ResetsAt = "2026-06-26T00:00:00Z"
	resp.IguanaNecktie.Utilization = 0.3
	resp.IguanaNecktie.ResetsAt = "2026-06-27T00:00:00Z"
	resp.ExtraUsage = &claudeExtraUsage{IsEnabled: true, MonthlyLimit: 200, UsedCredits: 50, Utilization: float64Ptr(0.25)}

	updateClaudeUsageQuota("claude-active", resp)

	entry := store.getByIndex("claude-active")
	if entry == nil {
		t.Fatalf("missing claude credential")
	}
	if entry.QuotaDetails.Source != "anthropic_usage_api" {
		t.Fatalf("source = %q, want anthropic_usage_api", entry.QuotaDetails.Source)
	}
	for _, name := range []string{"5h", "7d", "7d_oauth_apps", "7d_opus", "7d_sonnet", "7d_cowork", "iguana_necktie"} {
		if findQuotaWindow(entry.QuotaDetails.Windows, name) == nil {
			t.Fatalf("missing Claude window %q: %+v", name, entry.QuotaDetails.Windows)
		}
	}
	if entry.QuotaDetails.ExtraUsage == nil || entry.QuotaDetails.ExtraUsage.MonthlyLimit != 200 || entry.QuotaDetails.ExtraUsage.Utilization == nil || *entry.QuotaDetails.ExtraUsage.Utilization != 0.25 {
		t.Fatalf("extra_usage = %+v, want panel extra usage", entry.QuotaDetails.ExtraUsage)
	}
}

func TestUpdateClaudePlanFromProfileStoresPlanType(t *testing.T) {
	cases := []struct {
		name string
		resp claudeProfileResponse
		want string
	}{
		{name: "max", resp: claudeProfileResponse{Account: claudeProfileAccount{HasClaudeMax: boolPtr(true)}}, want: "plan_max"},
		{name: "pro", resp: claudeProfileResponse{Account: claudeProfileAccount{HasClaudePro: boolPtr(true)}}, want: "plan_pro"},
		{name: "team", resp: claudeProfileResponse{Organization: claudeProfileOrganization{Type: "claude_team", SubscriptionStatus: "active"}}, want: "plan_team"},
		{name: "free", resp: claudeProfileResponse{Account: claudeProfileAccount{HasClaudeMax: boolPtr(false), HasClaudePro: boolPtr(false)}}, want: "plan_free"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetTestStore()
			store.mu.Lock()
			store.getOrCreate("claude-profile", "claude", "auth-claude-profile")
			store.mu.Unlock()

			updateClaudePlanFromProfile("claude-profile", &tc.resp)

			entry := store.getByIndex("claude-profile")
			if entry.QuotaDetails.PlanType != tc.want {
				t.Fatalf("plan_type = %q, want %q", entry.QuotaDetails.PlanType, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./...
```

Expected: FAIL because existing `claudeUsageResponse` lacks the extra panel windows/profile types.

- [ ] **Step 3: Create `claude_active.go` with profile types and plan resolver**

Create `claude_active.go`:

```go
package main

import "strings"

type claudeProfileResponse struct {
	Account      claudeProfileAccount      `json:"account"`
	Organization claudeProfileOrganization `json:"organization"`
}

type claudeProfileAccount struct {
	HasClaudeMax *bool `json:"has_claude_max"`
	HasClaudePro *bool `json:"has_claude_pro"`
}

type claudeProfileOrganization struct {
	Type               string `json:"type"`
	SubscriptionStatus string `json:"subscription_status"`
}

func updateClaudePlanFromProfile(authIndex string, resp *claudeProfileResponse) {
	if resp == nil {
		return
	}
	planType := resolveClaudePlanType(resp)
	if planType == "" {
		return
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	entry := store.data[authIndex]
	if entry == nil {
		return
	}
	details := entry.QuotaDetails
	details.PlanType = planType
	entry.QuotaDetails = details
}

func resolveClaudePlanType(resp *claudeProfileResponse) string {
	if resp == nil {
		return ""
	}
	if resp.Account.HasClaudeMax != nil && *resp.Account.HasClaudeMax {
		return "plan_max"
	}
	if resp.Account.HasClaudePro != nil && *resp.Account.HasClaudePro {
		return "plan_pro"
	}
	if strings.EqualFold(strings.TrimSpace(resp.Organization.Type), "claude_team") && strings.EqualFold(strings.TrimSpace(resp.Organization.SubscriptionStatus), "active") {
		return "plan_team"
	}
	if resp.Account.HasClaudeMax != nil && !*resp.Account.HasClaudeMax && resp.Account.HasClaudePro != nil && !*resp.Account.HasClaudePro {
		return "plan_free"
	}
	return ""
}
```

- [ ] **Step 4: Expand `claudeUsageResponse` and `updateClaudeUsageQuota` in `core.go`**

Replace the existing `claudeUsageResponse` type with:

```go
type claudeUsageResponse struct {
	FiveHour          claudeUsageWindow `json:"five_hour"`
	SevenDay          claudeUsageWindow `json:"seven_day"`
	SevenDayOAuthApps claudeUsageWindow `json:"seven_day_oauth_apps"`
	SevenDayOpus      claudeUsageWindow `json:"seven_day_opus"`
	SevenDaySonnet    claudeUsageWindow `json:"seven_day_sonnet"`
	SevenDayCowork    claudeUsageWindow `json:"seven_day_cowork"`
	IguanaNecktie     claudeUsageWindow `json:"iguana_necktie"`
	ExtraUsage        *claudeExtraUsage `json:"extra_usage"`
}

type claudeUsageWindow struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    string  `json:"resets_at"`
}
```

Replace `updateClaudeUsageQuota` with:

```go
func updateClaudeUsageQuota(authIndex string, resp *claudeUsageResponse) {
	store.mu.Lock()
	defer store.mu.Unlock()

	entry := store.data[authIndex]
	if entry == nil || resp == nil {
		return
	}

	details := entry.QuotaDetails
	details.Source = "anthropic_usage_api"
	details.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	windows := []struct {
		name   string
		label  string
		window claudeUsageWindow
	}{
		{name: "5h", label: "5 hour limit", window: resp.FiveHour},
		{name: "7d", label: "weekly limit", window: resp.SevenDay},
		{name: "7d_oauth_apps", label: "weekly OAuth apps limit", window: resp.SevenDayOAuthApps},
		{name: "7d_opus", label: "weekly Opus limit", window: resp.SevenDayOpus},
		{name: "7d_sonnet", label: "weekly Sonnet limit", window: resp.SevenDaySonnet},
		{name: "7d_cowork", label: "weekly cowork limit", window: resp.SevenDayCowork},
		{name: "iguana_necktie", label: "iguana necktie limit", window: resp.IguanaNecktie},
	}
	for _, item := range windows {
		if item.window.ResetsAt == "" && item.window.Utilization == 0 {
			continue
		}
		details.Windows = upsertQuotaWindow(details.Windows, quotaWindow{
			Name:        item.name,
			Label:       item.label,
			Utilization: float64Ptr(item.window.Utilization),
			ResetAt:     item.window.ResetsAt,
		})
	}
	if resp.ExtraUsage != nil {
		details.ExtraUsage = copyClaudeExtraUsage(resp.ExtraUsage)
	}
	entry.QuotaDetails = details
}
```

- [ ] **Step 5: Update `queryClaudeUsage` in `core.go` to fetch usage and profile**

Replace the existing `queryClaudeUsage` with:

```go
func queryClaudeUsage(authIndex string) {
	if cfg.CPABaseURL == "" || cfg.ManagementKey == "" {
		return
	}

	bodyStr, ok := queryManagementAPICall(authIndex, buildClaudeUsageAPICallPayload(authIndex))
	if !ok {
		return
	}
	var usageResp claudeUsageResponse
	if err := json.Unmarshal([]byte(bodyStr), &usageResp); err != nil {
		callHostLog("error", fmt.Sprintf("credential-usage: parse Claude usage failed for %s: %v", authIndex, err))
		return
	}
	updateClaudeUsageQuota(authIndex, &usageResp)

	profileBody, profileOK := queryManagementAPICall(authIndex, buildClaudeProfileAPICallPayload(authIndex))
	if !profileOK {
		return
	}
	var profileResp claudeProfileResponse
	if err := json.Unmarshal([]byte(profileBody), &profileResp); err != nil {
		callHostLog("error", fmt.Sprintf("credential-usage: parse Claude profile failed for %s: %v", authIndex, err))
		return
	}
	updateClaudePlanFromProfile(authIndex, &profileResp)
}
```

- [ ] **Step 6: Run tests to verify Task 3 passes**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 7: Commit Task 3**

```bash
git add core.go claude_active.go main_test.go
git commit -m "feat: align claude active quota with panel usage"
```

---

### Task 4: Align Antigravity active quota with panel summary and subscription

**Files:**
- Create: `antigravity_active.go`
- Modify: `core.go`
- Test: `main_test.go`

**Interfaces:**
- Consumes: `buildAntigravityQuotaSummaryAPICallPayload`, `buildAntigravitySubscriptionAPICallPayload`, `antigravityQuotaSummaryURLs`, `queryManagementAPICallFull`, `queryManagementAPICall`.
- Produces:
  - `type antigravityQuotaSummaryResponse`
  - `type antigravitySubscriptionResponse`
  - `updateAntigravityQuotaGroups(authIndex string, resp *antigravityQuotaSummaryResponse)`
  - `updateAntigravitySubscription(authIndex string, resp *antigravitySubscriptionResponse)`
  - `queryLoadCodeAssist(authIndex string)` reworked to panel quota summary + subscription behavior.

- [ ] **Step 1: Write failing tests for Antigravity grouped quota and subscription plan mapping**

Add these tests before `findQuotaWindow`:

```go
func TestUpdateAntigravityQuotaGroupsStoresPanelGroups(t *testing.T) {
	resetTestStore()
	store.mu.Lock()
	store.getOrCreate("ag-groups", "antigravity", "auth-ag-groups")
	store.mu.Unlock()

	resp := &antigravityQuotaSummaryResponse{Groups: []antigravityQuotaSummaryGroup{{
		Label:       "Claude and GPT Models",
		Description: "Premium model quota",
		Buckets: []antigravityQuotaSummaryBucket{{
			Label:             "5 hour limit",
			Description:       "Short window",
			RemainingFraction: flexibleFloatPtr(0.65),
			ResetTime:         "2026-06-21T10:00:00Z",
		}},
	}}}

	updateAntigravityQuotaGroups("ag-groups", resp)

	entry := store.getByIndex("ag-groups")
	if entry == nil {
		t.Fatalf("missing antigravity credential")
	}
	if entry.QuotaDetails.Source != "antigravity_quota_summary" {
		t.Fatalf("source = %q, want antigravity_quota_summary", entry.QuotaDetails.Source)
	}
	if len(entry.QuotaDetails.QuotaGroups) != 1 || len(entry.QuotaDetails.QuotaGroups[0].Buckets) != 1 {
		t.Fatalf("quota_groups = %+v, want one group with one bucket", entry.QuotaDetails.QuotaGroups)
	}
	bucket := entry.QuotaDetails.QuotaGroups[0].Buckets[0]
	if bucket.RemainingFraction == nil || *bucket.RemainingFraction != 0.65 || bucket.ResetTime != "2026-06-21T10:00:00Z" {
		t.Fatalf("bucket = %+v, want remaining fraction/reset", bucket)
	}
}

func TestUpdateAntigravitySubscriptionStoresPanelPlanAndCredits(t *testing.T) {
	resetTestStore()
	store.mu.Lock()
	store.getOrCreate("ag-sub", "antigravity", "auth-ag-sub")
	store.mu.Unlock()

	resp := &antigravitySubscriptionResponse{}
	resp.CurrentTier.ID = "free-tier"
	resp.CurrentTier.Name = "Free"
	resp.PaidTier.ID = "g1-ultra-tier"
	resp.PaidTier.Name = "Ultra"
	resp.PaidTier.AvailableCredits = []loadCodeAssistCredit{{CreditType: "GOOGLE_ONE_AI", CreditAmount: 20, MinimumCreditAmount: 1}}

	updateAntigravitySubscription("ag-sub", resp)

	entry := store.getByIndex("ag-sub")
	if entry == nil {
		t.Fatalf("missing antigravity credential")
	}
	if entry.QuotaDetails.PlanType != "ultra" {
		t.Fatalf("plan_type = %q, want ultra", entry.QuotaDetails.PlanType)
	}
	if entry.QuotaDetails.Credits == nil || entry.QuotaDetails.Credits.PaidTierID != "g1-ultra-tier" || entry.QuotaDetails.Credits.Amount == nil || *entry.QuotaDetails.Credits.Amount != 20 {
		t.Fatalf("credits = %+v, want paid tier and credits", entry.QuotaDetails.Credits)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./...
```

Expected: FAIL with undefined symbols such as `antigravityQuotaSummaryResponse`, `updateAntigravityQuotaGroups`, or `updateAntigravitySubscription`.

- [ ] **Step 3: Create `antigravity_active.go` with panel quota summary and subscription normalizers**

Create `antigravity_active.go`:

```go
package main

import "time"

type antigravityQuotaSummaryResponse struct {
	Groups []antigravityQuotaSummaryGroup `json:"groups"`
}

type antigravityQuotaSummaryGroup struct {
	Label       string                         `json:"label"`
	Description string                         `json:"description"`
	Buckets     []antigravityQuotaSummaryBucket `json:"buckets"`
}

type antigravityQuotaSummaryBucket struct {
	Label             string         `json:"label"`
	Description       string         `json:"description"`
	RemainingFraction *flexibleFloat `json:"remainingFraction"`
	RemainingFractionSnake *flexibleFloat `json:"remaining_fraction"`
	ResetTime         string         `json:"resetTime"`
	ResetTimeSnake    string         `json:"reset_time"`
}

type antigravitySubscriptionResponse = loadCodeAssistResponse

func updateAntigravityQuotaGroups(authIndex string, resp *antigravityQuotaSummaryResponse) {
	if resp == nil {
		return
	}
	groups := make([]quotaGroup, 0, len(resp.Groups))
	for groupIndex, group := range resp.Groups {
		outGroup := quotaGroup{
			ID:          stableQuotaID(group.Label, "quota-group", groupIndex),
			Label:       group.Label,
			Description: group.Description,
		}
		for bucketIndex, bucket := range group.Buckets {
			remaining := firstFlexibleFloat(bucket.RemainingFraction, bucket.RemainingFractionSnake)
			resetTime := firstNonEmptyStringValue(bucket.ResetTime, bucket.ResetTimeSnake)
			if remaining == nil && resetTime == "" && bucket.Label == "" {
				continue
			}
			outGroup.Buckets = append(outGroup.Buckets, quotaBucket{
				ID:                stableQuotaID(bucket.Label, outGroup.ID+"-bucket", bucketIndex),
				Label:             bucket.Label,
				Description:       bucket.Description,
				RemainingFraction: flexibleFloatToFloat64Ptr(remaining),
				ResetTime:         resetTime,
			})
		}
		if len(outGroup.Buckets) > 0 {
			groups = append(groups, outGroup)
		}
	}
	if len(groups) == 0 {
		return
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	entry := store.data[authIndex]
	if entry == nil {
		return
	}
	details := entry.QuotaDetails
	details.Source = "antigravity_quota_summary"
	details.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	details.QuotaGroups = groups
	entry.QuotaDetails = details
}

func updateAntigravitySubscription(authIndex string, resp *antigravitySubscriptionResponse) {
	if resp == nil {
		return
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	entry := store.data[authIndex]
	if entry == nil {
		return
	}
	selected := selectAntigravityCredit(resp.PaidTier.AvailableCredits)
	details := entry.QuotaDetails
	if details.Source == "" {
		details.Source = "antigravity_subscription"
	}
	details.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	details.PlanType = antigravityPlanType(resp)
	details.Credits = buildCreditDetails((*loadCodeAssistResponse)(resp), selected)
	if selected != nil {
		available := selected.CreditAmount > selected.MinimumCreditAmount
		details.Available = &available
	}
	entry.QuotaDetails = details
}

func antigravityPlanType(resp *antigravitySubscriptionResponse) string {
	tierID := firstNonEmptyStringValue(resp.PaidTier.ID, resp.CurrentTier.ID)
	switch tierID {
	case "free-tier":
		return "free"
	case "g1-pro-tier":
		return "pro"
	case "g1-ultra-tier":
		return "ultra"
	case "g1-ultra-lite-tier":
		return "ultra-lite"
	default:
		return "unknown"
	}
}

func stableQuotaID(label, fallback string, index int) string {
	id := sanitizeQuotaName(label)
	if id == "" {
		id = fallback
	}
	return id
}
```

- [ ] **Step 4: Replace `queryLoadCodeAssist` in `core.go` with panel summary + subscription**

Replace existing `queryLoadCodeAssist` with:

```go
func queryLoadCodeAssist(authIndex string) {
	if cfg.CPABaseURL == "" || cfg.ManagementKey == "" {
		return
	}
	entry := store.getByIndex(authIndex)
	projectID := ""
	if entry != nil {
		projectID = entry.AntigravityProjectID
	}
	if projectID != "" {
		for _, url := range antigravityQuotaSummaryURLs {
			bodyStr, ok := queryManagementAPICall(authIndex, buildAntigravityQuotaSummaryAPICallPayload(authIndex, projectID, url))
			if !ok {
				continue
			}
			var quotaResp antigravityQuotaSummaryResponse
			if err := json.Unmarshal([]byte(bodyStr), &quotaResp); err != nil {
				callHostLog("error", fmt.Sprintf("credential-usage: parse Antigravity quota summary failed for %s: %v", authIndex, err))
				continue
			}
			if len(quotaResp.Groups) == 0 {
				continue
			}
			updateAntigravityQuotaGroups(authIndex, &quotaResp)
			break
		}
	}

	bodyStr, ok := queryManagementAPICall(authIndex, buildAntigravitySubscriptionAPICallPayload(authIndex))
	if !ok {
		return
	}
	var subscriptionResp antigravitySubscriptionResponse
	if err := json.Unmarshal([]byte(bodyStr), &subscriptionResp); err != nil {
		applyAntigravityFailureBody(authIndex, bodyStr)
		return
	}
	updateAntigravitySubscription(authIndex, &subscriptionResp)
}
```

Do not call `queryAntigravityAvailableModels` from active polling. Leave the old function and `updateAntigravityModelQuotas` in place only for backward-compatible tests/helpers unless you remove all references and tests in the same task.

- [ ] **Step 5: Update `queryProviderQuotaDetails` only if needed**

Keep this switch shape in `core.go`:

```go
func queryProviderQuotaDetails() {
	entries := store.all()
	for _, entry := range entries {
		switch entry.Provider {
		case "antigravity", "gemini-cli":
			queryLoadCodeAssist(entry.AuthIndex)
		case "claude":
			queryClaudeUsage(entry.AuthIndex)
		case "codex":
			queryCodexQuota(entry.AuthIndex)
		}
	}
}
```

This preserves existing provider dispatch while changing Antigravity internals to panel behavior.

- [ ] **Step 6: Run tests to verify Task 4 passes**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 7: Commit Task 4**

```bash
git add core.go antigravity_active.go main_test.go
git commit -m "feat: align antigravity active quota with panel APIs"
```

---

### Task 5: Update README and run full regression

**Files:**
- Modify: `README.md`
- Test: `go test ./...`

**Interfaces:**
- Consumes: completed panel-aligned active behavior from Tasks 1-4.
- Produces: README active mode documentation that matches implementation and removes Codex probe wording.

- [ ] **Step 1: Update the Active Mode README section**

Replace `README.md` lines under `### Active Mode (when cpa-base-url + management-key are configured)` with this text:

```markdown
### Active Mode (when `cpa-base-url` + `management-key` are configured)

Periodically queries provider quota APIs through CPA's Management API `api-call` endpoint. The plugin does not send provider HTTP requests directly; CPA resolves `$TOKEN$` from the selected `auth_index`, applies credential/global proxy settings, and returns the upstream response.

The active request definitions are centralized in the plugin and aligned with the CPA management panel quota source.

- Claude OAuth APIs:
  - `GET https://api.anthropic.com/api/oauth/usage`
    - `five_hour.utilization`, `five_hour.resets_at`
    - `seven_day.utilization`, `seven_day.resets_at`
    - `seven_day_oauth_apps.utilization`, `seven_day_oauth_apps.resets_at`
    - `seven_day_opus.utilization`, `seven_day_opus.resets_at`
    - `seven_day_sonnet.utilization`, `seven_day_sonnet.resets_at`
    - `seven_day_cowork.utilization`, `seven_day_cowork.resets_at`
    - `iguana_necktie.utilization`, `iguana_necktie.resets_at`
    - `extra_usage`
  - `GET https://api.anthropic.com/api/oauth/profile`
    - plan detection for Max, Pro, Team, Free, or unknown
- Codex usage API:
  - `GET https://chatgpt.com/backend-api/wham/usage`
  - reads `plan_type`, `rate_limit`, `code_review_rate_limit`, `additional_rate_limits`, and `rate_limit_reset_credits`
  - uses `Chatgpt-Account-Id` when the account id is available from safe auth metadata
  - does not send a prompt probe
- Antigravity/Gemini CLI quota summary:
  - tries `POST https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary`
  - then `POST https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:retrieveUserQuotaSummary`
  - then `POST https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary`
  - reads `groups[].buckets[].remainingFraction` and `groups[].buckets[].resetTime`
- Antigravity/Gemini CLI subscription:
  - `POST https://daily-cloudcode-pa.googleapis.com/v1internal:loadCodeAssist`
  - reads `currentTier`, `paidTier`, `paidTier.availableCredits[]`, and maps `free-tier`, `g1-pro-tier`, `g1-ultra-tier`, and `g1-ultra-lite-tier` to plan labels

Active query failures do not clear previously observed `quota_details`.
```

Keep the following paragraph after the new section:

```markdown
For Antigravity credits, the legacy `credits.amount` and `credits.minimum_for_usage` fields are selected from `creditType == "GOOGLE_ONE_AI"` when present, otherwise from the first available credit.
```

- [ ] **Step 2: Verify no probe wording remains in README**

Run:

```bash
git grep -n "probe\|/backend-api/codex/responses\|\"hi\"" README.md core.go panel_quota_defs.go codex_active.go
```

Expected: no output. If output references passive tests or explicit negative tests in `main_test.go`, that is acceptable; the command above intentionally excludes tests.

- [ ] **Step 3: Run full test suite**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 4: Review git diff**

Run:

```bash
git diff -- README.md core.go panel_quota_defs.go codex_active.go claude_active.go antigravity_active.go main_test.go
```

Expected: diff shows panel-aligned active request definitions, no Codex prompt probe in production code, and README updated to panel behavior.

- [ ] **Step 5: Commit Task 5**

```bash
git add README.md
git commit -m "docs: document panel-aligned active quota"
```

---

## Final Verification

After all tasks are complete, run:

```bash
go test ./...
git status --short
git log --oneline -n 6
```

Expected:

- `go test ./...` passes.
- `git status --short` is empty.
- Recent commits include:
  - `feat: add panel quota request definitions`
  - `feat: align codex active quota with panel usage API`
  - `feat: align claude active quota with panel usage`
  - `feat: align antigravity active quota with panel APIs`
  - `docs: document panel-aligned active quota`

## Self-Review Notes

- Spec coverage: Tasks cover centralized panel builders, Codex wham usage, Claude usage/profile, Antigravity quota summary/subscription, error-preserving active updates, README updates, and unchanged resource endpoints.
- Placeholder scan: The plan contains concrete file paths, function names, commands, expected outcomes, and code snippets for each implementation step.
- Type consistency: Builder names produced in Task 1 are consumed by Tasks 2-4; new `quotaDetails` fields are copied in Task 1 and populated in Tasks 2-4.
