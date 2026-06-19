package main

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

type testEnvelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

func decodeTestEnvelope(t *testing.T, raw []byte) testEnvelope {
	t.Helper()
	var env testEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v body=%s", err, string(raw))
	}
	if !env.OK {
		t.Fatalf("envelope ok=false error=%+v body=%s", env.Error, string(raw))
	}
	return env
}

func decodeManagementBody(t *testing.T, body string) []byte {
	t.Helper()
	decoded, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return decoded
}

func resetTestStore() {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.data = make(map[string]*credentialEntry)
}

func seedTestCredential(authIndex string) {
	store.mu.Lock()
	defer store.mu.Unlock()
	entry := store.getOrCreate(authIndex, "claude", "auth-"+authIndex)
	entry.Label = "Claude " + authIndex
	entry.Status = "available"
}

func callManagementHandleForTest(t *testing.T, path string, query map[string][]string) managementResponseForTest {
	t.Helper()
	request, err := json.Marshal(managementRequest{Method: "GET", Path: path, Query: query})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	raw, err := handleMethod("management.handle", request)
	if err != nil {
		t.Fatalf("handleMethod returned error: %v", err)
	}
	env := decodeTestEnvelope(t, raw)
	var resp managementResponseForTest
	if err := json.Unmarshal(env.Result, &resp); err != nil {
		t.Fatalf("unmarshal management response: %v result=%s", err, string(env.Result))
	}
	return resp
}

type managementResponseForTest struct {
	StatusCode int                 `json:"StatusCode"`
	Headers    map[string][]string `json:"Headers"`
	Body       string              `json:"Body"`
}

func TestResourceListPathReturnsCredentials(t *testing.T) {
	resetTestStore()
	seedTestCredential("1")

	resp := callManagementHandleForTest(t, "/v0/resource/plugins/credential-usage/list", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeManagementBody(t, resp.Body)
	var entries []credentialEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		t.Fatalf("unmarshal list body: %v body=%s", err, string(body))
	}
	if len(entries) != 1 || entries[0].AuthIndex != "1" {
		t.Fatalf("entries = %+v, want one auth_index=1", entries)
	}
}

func TestResourceDetailPathReturnsCredential(t *testing.T) {
	resetTestStore()
	seedTestCredential("2")

	resp := callManagementHandleForTest(t, "/v0/resource/plugins/credential-usage/detail", map[string][]string{"auth_index": {"2"}})
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeManagementBody(t, resp.Body)
	var entry credentialEntry
	if err := json.Unmarshal(body, &entry); err != nil {
		t.Fatalf("unmarshal detail body: %v body=%s", err, string(body))
	}
	if entry.AuthIndex != "2" {
		t.Fatalf("auth_index = %q, want 2", entry.AuthIndex)
	}
}

func TestPluginRegisterUsesInjectedVersion(t *testing.T) {
	oldVersion := pluginVersion
	pluginVersion = "9.8.7-test"
	defer func() { pluginVersion = oldVersion }()

	raw, err := handleMethod("plugin.register", nil)
	if err != nil {
		t.Fatalf("handleMethod returned error: %v", err)
	}
	env := decodeTestEnvelope(t, raw)

	var registration struct {
		Metadata struct {
			Version string `json:"Version"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(env.Result, &registration); err != nil {
		t.Fatalf("unmarshal registration result: %v result=%s", err, string(env.Result))
	}
	if registration.Metadata.Version != "9.8.7-test" {
		t.Fatalf("metadata version = %q, want 9.8.7-test", registration.Metadata.Version)
	}
}

func TestManagementRegisterReturnsResourceRoutes(t *testing.T) {
	raw, err := handleMethod("management.register", nil)
	if err != nil {
		t.Fatalf("handleMethod returned error: %v", err)
	}
	env := decodeTestEnvelope(t, raw)

	var registration struct {
		Routes    []any `json:"routes"`
		Resources []struct {
			Path        string `json:"Path"`
			Menu        string `json:"Menu"`
			Description string `json:"Description"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(env.Result, &registration); err != nil {
		t.Fatalf("unmarshal registration result: %v result=%s", err, string(env.Result))
	}
	if len(registration.Routes) != 0 {
		t.Fatalf("routes length = %d, want 0", len(registration.Routes))
	}
	if len(registration.Resources) != 2 {
		t.Fatalf("resources length = %d, want 2; result=%s", len(registration.Resources), string(env.Result))
	}
	if registration.Resources[0].Path != "/list" {
		t.Fatalf("first resource path = %q, want /list", registration.Resources[0].Path)
	}
	if registration.Resources[1].Path != "/detail" {
		t.Fatalf("second resource path = %q, want /detail", registration.Resources[1].Path)
	}
}

func TestResourceMissingCredentialReturns404(t *testing.T) {
	resetTestStore()

	resp := callManagementHandleForTest(t, "/v0/resource/plugins/credential-usage/detail", map[string][]string{"auth_index": {"missing"}})
	if resp.StatusCode != 404 {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	body := decodeManagementBody(t, resp.Body)
	var payload map[string]string
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal error body: %v body=%s", err, string(body))
	}
	if payload["error"] != "credential not found" {
		t.Fatalf("error = %q, want credential not found", payload["error"])
	}
}

func TestUnknownResourcePathReturns404(t *testing.T) {
	resetTestStore()

	resp := callManagementHandleForTest(t, "/v0/resource/plugins/credential-usage/unknown", nil)
	if resp.StatusCode != 404 {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	body := decodeManagementBody(t, resp.Body)
	var payload map[string]string
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal error body: %v body=%s", err, string(body))
	}
	if payload["error"] != "not found" {
		t.Fatalf("error = %q, want not found", payload["error"])
	}
}

func handleUsageForTest(t *testing.T, record usageRecord) {
	t.Helper()
	rawRecord, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal usage record: %v", err)
	}
	raw, err := handleMethod("usage.handle", rawRecord)
	if err != nil {
		t.Fatalf("handleMethod returned error: %v", err)
	}
	decodeTestEnvelope(t, raw)
}

func TestCredentialResponseUsesQuotaDetailsNotUsageSummary(t *testing.T) {
	resetTestStore()
	seedTestCredential("3")

	resp := callManagementHandleForTest(t, "/v0/resource/plugins/credential-usage/detail", map[string][]string{"auth_index": {"3"}})
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeManagementBody(t, resp.Body)
	var entry map[string]any
	if err := json.Unmarshal(body, &entry); err != nil {
		t.Fatalf("unmarshal detail body: %v body=%s", err, string(body))
	}
	if _, ok := entry["usage_summary"]; ok {
		t.Fatalf("response should not contain usage_summary: %s", string(body))
	}
	if _, ok := entry["quota_remaining"]; ok {
		t.Fatalf("response should not contain quota_remaining: %s", string(body))
	}
	if _, ok := entry["quota_details"]; !ok {
		t.Fatalf("response missing quota_details: %s", string(body))
	}
	if _, ok := entry["quota_state"]; !ok {
		t.Fatalf("response missing quota_state: %s", string(body))
	}
}

func TestUsageHandleUpdatesAnthropicQuotaDetailsInCredentialResponse(t *testing.T) {
	resetTestStore()
	handleUsageForTest(t, usageRecord{
		Provider:  "claude",
		AuthID:    "auth-4",
		AuthIndex: "4",
		ResponseHeaders: map[string][]string{
			"anthropic-ratelimit-unified-5h-status":              {"allowed"},
			"anthropic-ratelimit-unified-5h-reset":               {"2026-06-20T15:00:00Z"},
			"anthropic-ratelimit-unified-5h-utilization":         {"0.42"},
			"anthropic-ratelimit-unified-5h-surpassed-threshold": {"false"},
			"anthropic-ratelimit-unified-7d-status":              {"allowed_warning"},
			"anthropic-ratelimit-unified-7d-reset":               {"2026-06-24T00:00:00Z"},
			"anthropic-ratelimit-unified-7d-utilization":         {"0.77"},
			"anthropic-ratelimit-unified-7d-surpassed-threshold": {"true"},
			"anthropic-ratelimit-unified-reset":                  {"2026-06-20T15:00:00Z"},
		},
	})

	resp := callManagementHandleForTest(t, "/v0/resource/plugins/credential-usage/detail", map[string][]string{"auth_index": {"4"}})
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeManagementBody(t, resp.Body)
	var entry credentialEntry
	if err := json.Unmarshal(body, &entry); err != nil {
		t.Fatalf("unmarshal detail body: %v body=%s", err, string(body))
	}
	if entry.QuotaDetails.Source != "anthropic_headers" {
		t.Fatalf("source = %q, want anthropic_headers", entry.QuotaDetails.Source)
	}
	if entry.QuotaDetails.OverallResetAt != "2026-06-20T15:00:00Z" {
		t.Fatalf("overall_reset_at = %q, want 2026-06-20T15:00:00Z", entry.QuotaDetails.OverallResetAt)
	}
	if len(entry.QuotaDetails.Windows) != 2 {
		t.Fatalf("windows = %+v, want 2 windows", entry.QuotaDetails.Windows)
	}
	fiveHour := findQuotaWindow(entry.QuotaDetails.Windows, "5h")
	if fiveHour == nil {
		t.Fatalf("missing 5h window: %+v", entry.QuotaDetails.Windows)
	}
	if fiveHour.Label != "5 hour limit" || fiveHour.Status != "allowed" || fiveHour.ResetAt != "2026-06-20T15:00:00Z" {
		t.Fatalf("5h window = %+v, want label/status/reset", fiveHour)
	}
	if fiveHour.Utilization == nil || *fiveHour.Utilization != 0.42 {
		t.Fatalf("5h utilization = %v, want 0.42", fiveHour.Utilization)
	}
	if fiveHour.SurpassedThreshold == nil || *fiveHour.SurpassedThreshold {
		t.Fatalf("5h surpassed_threshold = %v, want false", fiveHour.SurpassedThreshold)
	}
	weekly := findQuotaWindow(entry.QuotaDetails.Windows, "7d")
	if weekly == nil {
		t.Fatalf("missing 7d window: %+v", entry.QuotaDetails.Windows)
	}
	if weekly.Label != "weekly limit" || weekly.Status != "allowed_warning" || weekly.ResetAt != "2026-06-24T00:00:00Z" {
		t.Fatalf("7d window = %+v, want weekly label/status/reset", weekly)
	}
	if weekly.Utilization == nil || *weekly.Utilization != 0.77 {
		t.Fatalf("7d utilization = %v, want 0.77", weekly.Utilization)
	}
	if weekly.SurpassedThreshold == nil || !*weekly.SurpassedThreshold {
		t.Fatalf("7d surpassed_threshold = %v, want true", weekly.SurpassedThreshold)
	}
}

func TestParseClaudeHeadersRateLimitsIntoQuotaDetails(t *testing.T) {
	entry := &credentialEntry{}
	parseClaudeHeaders(entry, map[string][]string{
		"anthropic-ratelimit-requests-limit":          {"1000"},
		"anthropic-ratelimit-requests-remaining":      {"750"},
		"anthropic-ratelimit-requests-reset":          {"2026-06-20T10:05:00Z"},
		"anthropic-ratelimit-input-tokens-limit":      {"100000"},
		"anthropic-ratelimit-input-tokens-remaining":  {"90000"},
		"anthropic-ratelimit-output-tokens-limit":     {"50000"},
		"anthropic-ratelimit-output-tokens-remaining": {"45000"},
	})

	limits := entry.QuotaDetails.RateLimits
	if limits == nil || limits.Requests == nil || limits.InputTokens == nil || limits.OutputTokens == nil {
		t.Fatalf("rate limits = %+v, want requests/input/output buckets", limits)
	}
	if *limits.Requests.Limit != 1000 || *limits.Requests.Remaining != 750 || limits.Requests.ResetAt != "2026-06-20T10:05:00Z" {
		t.Fatalf("requests bucket = %+v, want 1000/750/reset", limits.Requests)
	}
	if *limits.InputTokens.Limit != 100000 || *limits.InputTokens.Remaining != 90000 {
		t.Fatalf("input_tokens bucket = %+v, want 100000/90000", limits.InputTokens)
	}
	if *limits.OutputTokens.Limit != 50000 || *limits.OutputTokens.Remaining != 45000 {
		t.Fatalf("output_tokens bucket = %+v, want 50000/45000", limits.OutputTokens)
	}
}

func TestParseClaudeHeadersPartialUnifiedHeaders(t *testing.T) {
	entry := &credentialEntry{}
	parseClaudeHeaders(entry, map[string][]string{
		"Anthropic-RateLimit-Unified-5H-Utilization": {"0.5"},
	})

	if entry.QuotaDetails.Source != "anthropic_headers" {
		t.Fatalf("source = %q, want anthropic_headers", entry.QuotaDetails.Source)
	}
	if len(entry.QuotaDetails.Windows) != 1 {
		t.Fatalf("windows = %+v, want one partial window", entry.QuotaDetails.Windows)
	}
	window := entry.QuotaDetails.Windows[0]
	if window.Name != "5h" || window.Label != "5 hour limit" {
		t.Fatalf("window = %+v, want 5h window", window)
	}
	if window.Utilization == nil || *window.Utilization != 0.5 {
		t.Fatalf("utilization = %v, want 0.5", window.Utilization)
	}
}

func TestParseClaudeHeadersMalformedUnifiedHeaders(t *testing.T) {
	entry := &credentialEntry{}
	parseClaudeHeaders(entry, map[string][]string{
		"anthropic-ratelimit-unified-5h-utilization":         {"not-a-number"},
		"anthropic-ratelimit-unified-5h-surpassed-threshold": {"not-a-bool"},
		"anthropic-ratelimit-unified-5h-reset":               {"2026-06-20T15:00:00Z"},
	})

	if len(entry.QuotaDetails.Windows) != 1 {
		t.Fatalf("windows = %+v, want one malformed-tolerant window", entry.QuotaDetails.Windows)
	}
	window := entry.QuotaDetails.Windows[0]
	if window.ResetAt != "2026-06-20T15:00:00Z" {
		t.Fatalf("reset_at = %q, want 2026-06-20T15:00:00Z", window.ResetAt)
	}
	if window.Utilization != nil {
		t.Fatalf("utilization = %v, want nil for malformed value", window.Utilization)
	}
	if window.SurpassedThreshold != nil {
		t.Fatalf("surpassed_threshold = %v, want nil for malformed value", window.SurpassedThreshold)
	}
}

func TestParseCodex429StoresQuotaDetails(t *testing.T) {
	entry := &credentialEntry{}
	parseCodex429(entry, `{"error":{"type":"usage_limit_reached","resets_at":"2026-06-21T00:00:00Z","resets_in_seconds":3600}}`)

	if entry.QuotaDetails.Source != "failure_body" {
		t.Fatalf("source = %q, want failure_body", entry.QuotaDetails.Source)
	}
	if entry.QuotaDetails.Available == nil || *entry.QuotaDetails.Available {
		t.Fatalf("available = %v, want false", entry.QuotaDetails.Available)
	}
	if entry.QuotaDetails.ResetsAt != "2026-06-21T00:00:00Z" {
		t.Fatalf("resets_at = %q, want 2026-06-21T00:00:00Z", entry.QuotaDetails.ResetsAt)
	}
	if entry.QuotaDetails.ResetsInSeconds == nil || *entry.QuotaDetails.ResetsInSeconds != 3600 {
		t.Fatalf("resets_in_seconds = %v, want 3600", entry.QuotaDetails.ResetsInSeconds)
	}
	if !entry.QuotaState.Exceeded || entry.QuotaState.Reason != "usage_limit_reached" {
		t.Fatalf("quota_state = %+v, want usage_limit_reached exceeded", entry.QuotaState)
	}
}

func TestUpdateAntigravityQuotaStoresCreditsInQuotaDetails(t *testing.T) {
	resetTestStore()
	store.mu.Lock()
	store.getOrCreate("ag", "antigravity", "auth-ag")
	store.mu.Unlock()

	resp := &loadCodeAssistResponse{}
	resp.PaidTier.ID = "paid-tier-id"
	resp.PaidTier.AvailableCredits = append(resp.PaidTier.AvailableCredits, struct {
		CreditAmount        float64 `json:"creditAmount"`
		MinimumCreditAmount float64 `json:"minimumCreditAmountForUsage"`
	}{CreditAmount: 123.45, MinimumCreditAmount: 1})

	updateAntigravityQuota("ag", resp)

	entry := store.getByIndex("ag")
	if entry == nil {
		t.Fatalf("missing antigravity credential")
	}
	credits := entry.QuotaDetails.Credits
	if credits == nil {
		t.Fatalf("credits = nil, want credit details")
	}
	if credits.Amount == nil || *credits.Amount != 123.45 {
		t.Fatalf("amount = %v, want 123.45", credits.Amount)
	}
	if credits.MinimumForUsage == nil || *credits.MinimumForUsage != 1 {
		t.Fatalf("minimum_for_usage = %v, want 1", credits.MinimumForUsage)
	}
	if credits.PaidTierID != "paid-tier-id" {
		t.Fatalf("paid_tier_id = %q, want paid-tier-id", credits.PaidTierID)
	}
}

func findQuotaWindow(windows []quotaWindow, name string) *quotaWindow {
	for i := range windows {
		if windows[i].Name == name {
			return &windows[i]
		}
	}
	return nil
}
