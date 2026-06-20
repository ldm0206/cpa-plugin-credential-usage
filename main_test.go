package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
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

func TestParseClaudeHeadersGenericRateLimitFallbacks(t *testing.T) {
	entry := &credentialEntry{}
	parseClaudeHeaders(entry, map[string][]string{
		"x-ratelimit-limit-requests":     {"100"},
		"x-ratelimit-remaining-requests": {"25"},
		"x-ratelimit-reset-requests":     {"2026-06-20T10:05:00Z"},
		"x-ratelimit-limit-tokens":       {"100000"},
		"x-ratelimit-remaining-tokens":   {"50000"},
		"x-ratelimit-reset-tokens":       {"2026-06-20T10:10:00Z"},
	})

	limits := entry.QuotaDetails.RateLimits
	if limits == nil || limits.Requests == nil || limits.Tokens == nil {
		t.Fatalf("rate limits = %+v, want requests and generic token buckets", limits)
	}
	if entry.QuotaDetails.Source != "response_headers" {
		t.Fatalf("source = %q, want response_headers", entry.QuotaDetails.Source)
	}
	if *limits.Requests.Limit != 100 || *limits.Requests.Remaining != 25 || limits.Requests.ResetAt != "2026-06-20T10:05:00Z" {
		t.Fatalf("requests bucket = %+v, want 100/25/reset", limits.Requests)
	}
	if *limits.Tokens.Limit != 100000 || *limits.Tokens.Remaining != 50000 || limits.Tokens.ResetAt != "2026-06-20T10:10:00Z" {
		t.Fatalf("tokens bucket = %+v, want 100000/50000/reset", limits.Tokens)
	}
}

func TestParseClaudeHeadersRetryAfterFallback(t *testing.T) {
	entry := &credentialEntry{}
	parseClaudeHeaders(entry, map[string][]string{"Retry-After": {"120"}})

	if entry.QuotaDetails.Source != "response_headers" {
		t.Fatalf("source = %q, want response_headers", entry.QuotaDetails.Source)
	}
	if entry.QuotaDetails.Available == nil || *entry.QuotaDetails.Available {
		t.Fatalf("available = %v, want false", entry.QuotaDetails.Available)
	}
	if entry.QuotaDetails.ResetsInSeconds == nil || *entry.QuotaDetails.ResetsInSeconds != 120 {
		t.Fatalf("resets_in_seconds = %v, want 120", entry.QuotaDetails.ResetsInSeconds)
	}
	if entry.QuotaDetails.ResetsAt == "" {
		t.Fatalf("resets_at should be populated")
	}
	if entry.QuotaDetails.Detail != "Retry-After: 120s" {
		t.Fatalf("detail = %q, want Retry-After: 120s", entry.QuotaDetails.Detail)
	}
}

func TestParseClaudeHeadersRetryAfterHTTPDate(t *testing.T) {
	entry := &credentialEntry{}
	resetTime := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	retryAfter := resetTime.Format(http.TimeFormat)
	parseClaudeHeaders(entry, map[string][]string{"Retry-After": {retryAfter}})

	if entry.QuotaDetails.Source != "response_headers" {
		t.Fatalf("source = %q, want response_headers", entry.QuotaDetails.Source)
	}
	if entry.QuotaDetails.Available == nil || *entry.QuotaDetails.Available {
		t.Fatalf("available = %v, want false", entry.QuotaDetails.Available)
	}
	if entry.QuotaDetails.ResetsAt != resetTime.Format(time.RFC3339) {
		t.Fatalf("resets_at = %q, want %q", entry.QuotaDetails.ResetsAt, resetTime.Format(time.RFC3339))
	}
	if entry.QuotaDetails.ResetsInSeconds == nil || *entry.QuotaDetails.ResetsInSeconds <= 0 {
		t.Fatalf("resets_in_seconds = %v, want positive", entry.QuotaDetails.ResetsInSeconds)
	}
}

func TestCredentialStoreReturnsDeepCopy(t *testing.T) {
	resetTestStore()
	store.mu.Lock()
	entry := store.getOrCreate("copy", "claude", "auth-copy")
	entry.QuotaDetails.Available = boolPtr(true)
	entry.QuotaDetails.Windows = []quotaWindow{{Name: "5h", Utilization: float64Ptr(0.1)}}
	entry.QuotaDetails.RateLimits = &rateLimitDetails{Requests: &rateLimitBucket{Limit: int64Ptr(10)}}
	entry.QuotaDetails.Credits = &creditDetails{Amount: float64Ptr(1), Items: []creditItem{{Amount: float64Ptr(2)}}}
	entry.QuotaDetails.ModelQuotas = map[string]modelQuota{"model-a": {RemainingFraction: float64Ptr(0.5), SupportedMimeTypes: map[string]bool{"text/plain": true}}}
	store.mu.Unlock()

	snapshot := store.getByIndex("copy")
	if snapshot == nil {
		t.Fatalf("snapshot = nil")
	}
	snapshot.QuotaDetails.Windows[0].Name = "mutated"
	*snapshot.QuotaDetails.RateLimits.Requests.Limit = 99
	*snapshot.QuotaDetails.Credits.Items[0].Amount = 99
	snapshot.QuotaDetails.ModelQuotas["model-a"].SupportedMimeTypes["text/plain"] = false

	fresh := store.getByIndex("copy")
	if fresh.QuotaDetails.Windows[0].Name != "5h" {
		t.Fatalf("stored window mutated through snapshot: %+v", fresh.QuotaDetails.Windows[0])
	}
	if *fresh.QuotaDetails.RateLimits.Requests.Limit != 10 {
		t.Fatalf("stored rate limit mutated through snapshot: %+v", fresh.QuotaDetails.RateLimits.Requests)
	}
	if *fresh.QuotaDetails.Credits.Items[0].Amount != 2 {
		t.Fatalf("stored credit item mutated through snapshot: %+v", fresh.QuotaDetails.Credits.Items[0])
	}
	if !fresh.QuotaDetails.ModelQuotas["model-a"].SupportedMimeTypes["text/plain"] {
		t.Fatalf("stored model quota map mutated through snapshot: %+v", fresh.QuotaDetails.ModelQuotas["model-a"])
	}
}

func boolPtr(v bool) *bool { return &v }


func TestParseCodexHeadersStoresPrimarySecondaryWindows(t *testing.T) {
	entry := &credentialEntry{}
	parseCodexHeaders(entry, map[string][]string{
		"x-codex-primary-used-percent":                 {"81.5"},
		"x-codex-primary-reset-after-seconds":          {"86400"},
		"x-codex-primary-window-minutes":               {"10080"},
		"x-codex-secondary-used-percent":               {"33"},
		"x-codex-secondary-reset-after-seconds":        {"1200"},
		"x-codex-secondary-window-minutes":             {"300"},
		"x-codex-primary-over-secondary-limit-percent": {"245"},
	})

	if entry.QuotaDetails.Source != "codex_headers" {
		t.Fatalf("source = %q, want codex_headers", entry.QuotaDetails.Source)
	}
	if len(entry.QuotaDetails.Windows) != 2 {
		t.Fatalf("windows = %+v, want primary and secondary", entry.QuotaDetails.Windows)
	}
	primary := findQuotaWindow(entry.QuotaDetails.Windows, "primary")
	if primary == nil {
		t.Fatalf("missing primary window: %+v", entry.QuotaDetails.Windows)
	}
	if primary.Label != "primary window (7d)" || primary.UsedPercent == nil || *primary.UsedPercent != 81.5 || primary.WindowMinutes == nil || *primary.WindowMinutes != 10080 || primary.ResetAfterSeconds == nil || *primary.ResetAfterSeconds != 86400 {
		t.Fatalf("primary window = %+v, want 7d metadata", primary)
	}
	secondary := findQuotaWindow(entry.QuotaDetails.Windows, "secondary")
	if secondary == nil {
		t.Fatalf("missing secondary window: %+v", entry.QuotaDetails.Windows)
	}
	if secondary.Label != "secondary window (5h)" || secondary.UsedPercent == nil || *secondary.UsedPercent != 33 || secondary.WindowMinutes == nil || *secondary.WindowMinutes != 300 || secondary.ResetAfterSeconds == nil || *secondary.ResetAfterSeconds != 1200 {
		t.Fatalf("secondary window = %+v, want 5h metadata", secondary)
	}
	if entry.QuotaDetails.PrimaryOverSecondaryLimitPercent == nil || *entry.QuotaDetails.PrimaryOverSecondaryLimitPercent != 245 {
		t.Fatalf("primary_over_secondary_limit_percent = %v, want 245", entry.QuotaDetails.PrimaryOverSecondaryLimitPercent)
	}
}

func TestParseCodex429NumericResetAndPlanType(t *testing.T) {
	entry := &credentialEntry{}
	parseCodex429(entry, `{"error":{"type":"usage_limit_reached","message":"Usage limit reached","resets_at":1782000000,"resets_in_seconds":3600,"plan_type":"pro"}}`)

	expectedReset := time.Unix(1782000000, 0).UTC().Format(time.RFC3339)
	if entry.QuotaDetails.ErrorType != "usage_limit_reached" {
		t.Fatalf("error_type = %q, want usage_limit_reached", entry.QuotaDetails.ErrorType)
	}
	if entry.QuotaDetails.PlanType != "pro" {
		t.Fatalf("plan_type = %q, want pro", entry.QuotaDetails.PlanType)
	}
	if entry.QuotaDetails.Detail != "Usage limit reached" {
		t.Fatalf("detail = %q, want Usage limit reached", entry.QuotaDetails.Detail)
	}
	if entry.QuotaDetails.ResetsAt != expectedReset {
		t.Fatalf("resets_at = %q, want %q", entry.QuotaDetails.ResetsAt, expectedReset)
	}
	if entry.QuotaDetails.ResetsInSeconds == nil || *entry.QuotaDetails.ResetsInSeconds != 3600 {
		t.Fatalf("resets_in_seconds = %v, want 3600", entry.QuotaDetails.ResetsInSeconds)
	}
	if entry.QuotaState.NextRecoverAt == nil || *entry.QuotaState.NextRecoverAt != expectedReset {
		t.Fatalf("next_recover_at = %v, want %q", entry.QuotaState.NextRecoverAt, expectedReset)
	}
}

func TestParseCodex429NumericStringReset(t *testing.T) {
	entry := &credentialEntry{}
	parseCodex429(entry, `{"error":{"type":"usage_limit_reached","resets_at":"1782000000"}}`)

	expectedReset := time.Unix(1782000000, 0).UTC().Format(time.RFC3339)
	if entry.QuotaDetails.ResetsAt != expectedReset {
		t.Fatalf("resets_at = %q, want %q", entry.QuotaDetails.ResetsAt, expectedReset)
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

func TestUpdateAntigravitySubscriptionStoresCreditsInQuotaDetails(t *testing.T) {
	resetTestStore()
	store.mu.Lock()
	store.getOrCreate("ag", "antigravity", "auth-ag")
	store.mu.Unlock()

	resp := &antigravitySubscriptionResponse{}
	resp.PaidTier.ID = "g1-pro-tier"
	resp.PaidTier.AvailableCredits = append(resp.PaidTier.AvailableCredits, loadCodeAssistCredit{CreditAmount: 123.45, MinimumCreditAmount: 1})

	updateAntigravitySubscription("ag", resp)

	entry := store.getByIndex("ag")
	if entry == nil {
		t.Fatalf("missing antigravity credential")
	}
	if entry.QuotaDetails.Source != "antigravity_subscription" {
		t.Fatalf("source = %q, want antigravity_subscription", entry.QuotaDetails.Source)
	}
	if entry.QuotaDetails.PlanType != "pro" {
		t.Fatalf("plan_type = %q, want pro", entry.QuotaDetails.PlanType)
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
	if credits.PaidTierID != "g1-pro-tier" {
		t.Fatalf("paid_tier_id = %q, want g1-pro-tier", credits.PaidTierID)
	}
}

func TestUpdateAntigravitySubscriptionSelectsGoogleOneCreditAndStoresAllCredits(t *testing.T) {
	resetTestStore()
	store.mu.Lock()
	store.getOrCreate("ag-google", "antigravity", "auth-ag-google")
	store.mu.Unlock()

	resp := &antigravitySubscriptionResponse{}
	resp.CloudAICompanionProject = "project-123"
	resp.CurrentTier.ID = "free-tier"
	resp.CurrentTier.Name = "Free"
	resp.PaidTier.ID = "g1-pro-tier"
	resp.PaidTier.Name = "Google AI Pro"
	resp.PaidTier.AvailableCredits = append(resp.PaidTier.AvailableCredits,
		loadCodeAssistCredit{CreditType: "OTHER", CreditAmount: 999, MinimumCreditAmount: 1},
		loadCodeAssistCredit{CreditType: "GOOGLE_ONE_AI", CreditAmount: 12, MinimumCreditAmount: 20},
	)

	updateAntigravitySubscription("ag-google", resp)

	entry := store.getByIndex("ag-google")
	if entry == nil {
		t.Fatalf("missing antigravity credential")
	}
	if entry.QuotaDetails.PlanType != "pro" {
		t.Fatalf("plan_type = %q, want pro", entry.QuotaDetails.PlanType)
	}
	credits := entry.QuotaDetails.Credits
	if credits == nil {
		t.Fatalf("credits = nil, want credit details")
	}
	if credits.Amount == nil || *credits.Amount != 12 {
		t.Fatalf("amount = %v, want selected GOOGLE_ONE_AI amount 12", credits.Amount)
	}
	if credits.MinimumForUsage == nil || *credits.MinimumForUsage != 20 {
		t.Fatalf("minimum_for_usage = %v, want selected GOOGLE_ONE_AI minimum 20", credits.MinimumForUsage)
	}
	if entry.QuotaDetails.Available == nil || *entry.QuotaDetails.Available {
		t.Fatalf("available = %v, want false", entry.QuotaDetails.Available)
	}
	if !entry.QuotaState.Exceeded {
		t.Fatalf("quota_state.exceeded = %v, want true for insufficient credits", entry.QuotaState.Exceeded)
	}
	if entry.QuotaState.Reason != "insufficient_credits" {
		t.Fatalf("quota_state.reason = %q, want insufficient_credits", entry.QuotaState.Reason)
	}
	if len(credits.Items) != 2 {
		t.Fatalf("items = %+v, want 2 credit items", credits.Items)
	}
	if credits.Items[0].CreditType != "OTHER" || credits.Items[1].CreditType != "GOOGLE_ONE_AI" {
		t.Fatalf("items = %+v, want credit types preserved", credits.Items)
	}
	if credits.PaidTierName != "Google AI Pro" || credits.CurrentTierName != "Free" || credits.CloudAICompanionProject != "project-123" {
		t.Fatalf("credits metadata = %+v, want tier/project metadata", credits)
	}
}

func TestUpdateAntigravitySubscriptionAvailableCreditsClearsQuotaState(t *testing.T) {
	resetTestStore()
	store.mu.Lock()
	store.getOrCreate("ag-available", "antigravity", "auth-ag-available")
	// Pre-set exceeded quota state to verify it gets cleared.
	store.data["ag-available"].QuotaState.Exceeded = true
	store.data["ag-available"].QuotaState.Reason = "insufficient_credits"
	recoverAt := "2026-06-22T00:00:00Z"
	store.data["ag-available"].QuotaState.NextRecoverAt = &recoverAt
	store.mu.Unlock()

	resp := &antigravitySubscriptionResponse{}
	resp.PaidTier.ID = "g1-pro-tier"
	resp.PaidTier.AvailableCredits = append(resp.PaidTier.AvailableCredits,
		loadCodeAssistCredit{CreditType: "GOOGLE_ONE_AI", CreditAmount: 50, MinimumCreditAmount: 1},
	)

	updateAntigravitySubscription("ag-available", resp)

	entry := store.getByIndex("ag-available")
	if entry == nil {
		t.Fatalf("missing antigravity credential")
	}
	if entry.QuotaDetails.Available == nil || !*entry.QuotaDetails.Available {
		t.Fatalf("available = %v, want true", entry.QuotaDetails.Available)
	}
	if entry.QuotaState.Exceeded {
		t.Fatalf("quota_state.exceeded = %v, want false for available credits", entry.QuotaState.Exceeded)
	}
	if entry.QuotaState.Reason != "" {
		t.Fatalf("quota_state.reason = %q, want empty for available credits", entry.QuotaState.Reason)
	}
	if entry.QuotaState.NextRecoverAt != nil {
		t.Fatalf("quota_state.next_recover_at = %v, want nil for available credits", entry.QuotaState.NextRecoverAt)
	}
}

func TestUpdateAntigravitySubscriptionStoresTierMetadataWithoutCredits(t *testing.T) {
	resetTestStore()
	store.mu.Lock()
	store.getOrCreate("ag-tier", "antigravity", "auth-ag-tier")
	store.mu.Unlock()

	defaultTier := true
	resp := &antigravitySubscriptionResponse{}
	resp.CloudAICompanionProject = "project-456"
	resp.CurrentTier.ID = "free-tier"
	resp.CurrentTier.Name = "Free"
	resp.PaidTier.ID = "g1-ultra-tier"
	resp.PaidTier.Name = "Pro"
	resp.IneligibleTiers = append(resp.IneligibleTiers, struct {
		Tier          tierInfo `json:"tier"`
		ReasonCode    string   `json:"reasonCode"`
		ReasonMessage string   `json:"reasonMessage"`
	}{Tier: tierInfo{ID: "ultra-tier", Name: "Ultra"}, ReasonCode: "INELIGIBLE_ACCOUNT", ReasonMessage: "Not eligible"})
	resp.AllowedTiers = append(resp.AllowedTiers, struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		IsDefault *bool  `json:"isDefault"`
	}{ID: "free-tier", Name: "Free", IsDefault: &defaultTier})

	updateAntigravitySubscription("ag-tier", resp)

	entry := store.getByIndex("ag-tier")
	if entry == nil {
		t.Fatalf("missing antigravity credential")
	}
	if entry.QuotaDetails.PlanType != "ultra" {
		t.Fatalf("plan_type = %q, want ultra", entry.QuotaDetails.PlanType)
	}
	credits := entry.QuotaDetails.Credits
	if credits == nil {
		t.Fatalf("credits = nil, want metadata-only credit details")
	}
	if entry.QuotaDetails.Available != nil {
		t.Fatalf("available = %v, want nil without credit balance", entry.QuotaDetails.Available)
	}
	if credits.PaidTierID != "g1-ultra-tier" || credits.PaidTierName != "Pro" || credits.CurrentTierID != "free-tier" || credits.CloudAICompanionProject != "project-456" {
		t.Fatalf("credits metadata = %+v, want tier/project metadata", credits)
	}
	if len(credits.IneligibleTiers) != 1 || credits.IneligibleTiers[0].ReasonCode != "INELIGIBLE_ACCOUNT" {
		t.Fatalf("ineligible_tiers = %+v, want reason", credits.IneligibleTiers)
	}
	if len(credits.AllowedTiers) != 1 || credits.AllowedTiers[0].IsDefault == nil || !*credits.AllowedTiers[0].IsDefault {
		t.Fatalf("allowed_tiers = %+v, want default allowed tier", credits.AllowedTiers)
	}
}

func TestParseAntigravity429StoresGoogleRPCQuotaDetails(t *testing.T) {
	entry := &credentialEntry{}
	parseAntigravity429(entry, `{"error":{"status":"RESOURCE_EXHAUSTED","message":"Quota exhausted","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"INSUFFICIENT_G1_CREDITS_BALANCE","metadata":{"model":"gemini-2.5-pro"}},{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"3600s"}]}}`)

	if entry.QuotaDetails.Source != "failure_body" {
		t.Fatalf("source = %q, want failure_body", entry.QuotaDetails.Source)
	}
	if entry.QuotaDetails.Available == nil || *entry.QuotaDetails.Available {
		t.Fatalf("available = %v, want false", entry.QuotaDetails.Available)
	}
	if entry.QuotaDetails.ErrorStatus != "RESOURCE_EXHAUSTED" || entry.QuotaDetails.ErrorReason != "INSUFFICIENT_G1_CREDITS_BALANCE" || entry.QuotaDetails.Model != "gemini-2.5-pro" || entry.QuotaDetails.RetryDelay != "3600s" {
		t.Fatalf("quota_details = %+v, want Google RPC quota fields", entry.QuotaDetails)
	}
	if entry.QuotaDetails.ResetsInSeconds == nil || *entry.QuotaDetails.ResetsInSeconds != 3600 {
		t.Fatalf("resets_in_seconds = %v, want 3600", entry.QuotaDetails.ResetsInSeconds)
	}
	if entry.QuotaDetails.ResetsAt == "" {
		t.Fatalf("resets_at should be populated")
	}
	if !entry.QuotaState.Exceeded || entry.QuotaState.Reason != "insufficient_g1_credits_balance" {
		t.Fatalf("quota_state = %+v, want insufficient_g1_credits_balance", entry.QuotaState)
	}
}

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
	if w := findQuotaWindow(entry.QuotaDetails.Windows, "iguana_necktie"); w == nil || w.Utilization == nil || *w.Utilization != 0.3 {
		t.Fatalf("iguana_necktie window = %+v, want utilization 0.3", w)
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

func TestResolveClaudePlanTypeNilBooleansAndNonTeam(t *testing.T) {
	t.Run("nil booleans non-team org returns empty", func(t *testing.T) {
		got := resolveClaudePlanType(&claudeProfileResponse{
			Organization: claudeProfileOrganization{Type: "personal", SubscriptionStatus: "active"},
		})
		if got != "" {
			t.Fatalf("resolveClaudePlanType = %q, want empty string (nil booleans + non-team org is unresolvable)", got)
		}
	})
	t.Run("nil booleans team org inactive subscription returns empty", func(t *testing.T) {
		got := resolveClaudePlanType(&claudeProfileResponse{
			Organization: claudeProfileOrganization{Type: "claude_team", SubscriptionStatus: "inactive"},
		})
		if got != "" {
			t.Fatalf("resolveClaudePlanType = %q, want empty string (nil booleans + inactive team org is unresolvable)", got)
		}
	})
	t.Run("nil input returns empty", func(t *testing.T) {
		got := resolveClaudePlanType(nil)
		if got != "" {
			t.Fatalf("resolveClaudePlanType(nil) = %q, want empty string", got)
		}
	})
}

func TestUpdateClaudeUsageQuotaNilResponseDoesNotPanicOrMutate(t *testing.T) {
	resetTestStore()
	store.mu.Lock()
	store.getOrCreate("claude-nil-test", "claude", "auth-claude-nil-test")
	store.mu.Unlock()

	// Pre-populate quota details.
	store.mu.Lock()
	entry := store.data["claude-nil-test"]
	entry.QuotaDetails.Source = "pre_existing"
	entry.QuotaDetails.PlanType = "plan_pro"
	entry.QuotaDetails.Windows = []quotaWindow{{Name: "5h", Utilization: float64Ptr(0.5)}}
	store.mu.Unlock()

	updateClaudeUsageQuota("claude-nil-test", nil)

	entry = store.getByIndex("claude-nil-test")
	if entry.QuotaDetails.Source != "pre_existing" {
		t.Fatalf("source mutated to %q, want pre_existing", entry.QuotaDetails.Source)
	}
	if entry.QuotaDetails.PlanType != "plan_pro" {
		t.Fatalf("plan_type mutated to %q, want plan_pro", entry.QuotaDetails.PlanType)
	}
	if len(entry.QuotaDetails.Windows) != 1 || entry.QuotaDetails.Windows[0].Name != "5h" {
		t.Fatalf("windows mutated to %+v, want unchanged [5h]", entry.QuotaDetails.Windows)
	}
}

func TestUpdateClaudePlanFromProfileNilResponseDoesNotPanicOrMutate(t *testing.T) {
	resetTestStore()
	store.mu.Lock()
	store.getOrCreate("claude-nil-plan", "claude", "auth-claude-nil-plan")
	store.mu.Unlock()

	store.mu.Lock()
	entry := store.data["claude-nil-plan"]
	entry.QuotaDetails.PlanType = "plan_pro"
	entry.QuotaDetails.Source = "pre_existing"
	store.mu.Unlock()

	updateClaudePlanFromProfile("claude-nil-plan", nil)

	entry = store.getByIndex("claude-nil-plan")
	if entry.QuotaDetails.PlanType != "plan_pro" {
		t.Fatalf("plan_type mutated to %q, want plan_pro", entry.QuotaDetails.PlanType)
	}
	if entry.QuotaDetails.Source != "pre_existing" {
		t.Fatalf("source mutated to %q, want pre_existing", entry.QuotaDetails.Source)
	}
}

func TestUpdateAntigravityModelQuotasStoresFetchAvailableModels(t *testing.T) {
	resetTestStore()
	store.mu.Lock()
	store.getOrCreate("ag-models", "antigravity", "auth-ag-models")
	store.mu.Unlock()

	supportsImages := true
	supportsThinking := true
	thinkingBudget := int64(24576)
	recommended := true
	maxTokens := int64(1000000)
	maxOutputTokens := int64(65536)
	resp := &fetchAvailableModelsResponse{Models: map[string]struct {
		QuotaInfo *struct {
			RemainingFraction flexibleFloat `json:"remainingFraction"`
			ResetTime         string        `json:"resetTime"`
		} `json:"quotaInfo"`
		DisplayName        string          `json:"displayName"`
		SupportsImages     *bool           `json:"supportsImages"`
		SupportsThinking   *bool           `json:"supportsThinking"`
		ThinkingBudget     *int64          `json:"thinkingBudget"`
		Recommended        *bool           `json:"recommended"`
		MaxTokens          *int64          `json:"maxTokens"`
		MaxOutputTokens    *int64          `json:"maxOutputTokens"`
		SupportedMimeTypes map[string]bool `json:"supportedMimeTypes"`
	}{
		"gemini-2.0-flash": {
			QuotaInfo: &struct {
				RemainingFraction flexibleFloat `json:"remainingFraction"`
				ResetTime         string        `json:"resetTime"`
			}{RemainingFraction: 0.85, ResetTime: "2025-01-01T00:00:00Z"},
			DisplayName:        "Gemini 2.0 Flash",
			SupportsImages:     &supportsImages,
			SupportsThinking:   &supportsThinking,
			ThinkingBudget:     &thinkingBudget,
			Recommended:        &recommended,
			MaxTokens:          &maxTokens,
			MaxOutputTokens:    &maxOutputTokens,
			SupportedMimeTypes: map[string]bool{"text/plain": true},
		},
		"gemini-2.5-pro": {
			QuotaInfo: &struct {
				RemainingFraction flexibleFloat `json:"remainingFraction"`
				ResetTime         string        `json:"resetTime"`
			}{RemainingFraction: 0.5},
		},
	}}

	updateAntigravityModelQuotas("ag-models", resp)

	entry := store.getByIndex("ag-models")
	if entry == nil {
		t.Fatalf("missing antigravity credential")
	}
	quotas := entry.QuotaDetails.ModelQuotas
	if len(quotas) != 2 {
		t.Fatalf("model_quotas = %+v, want 2 entries", quotas)
	}
	flash := quotas["gemini-2.0-flash"]
	if flash.RemainingFraction == nil || *flash.RemainingFraction != 0.85 || flash.ResetTime != "2025-01-01T00:00:00Z" {
		t.Fatalf("flash quota = %+v, want remaining fraction/reset", flash)
	}
	if flash.DisplayName != "Gemini 2.0 Flash" || flash.SupportsImages == nil || !*flash.SupportsImages || flash.MaxTokens == nil || *flash.MaxTokens != 1000000 || !flash.SupportedMimeTypes["text/plain"] {
		t.Fatalf("flash metadata = %+v, want model metadata", flash)
	}
	pro := quotas["gemini-2.5-pro"]
	if pro.RemainingFraction == nil || *pro.RemainingFraction != 0.5 {
		t.Fatalf("pro quota = %+v, want remaining fraction", pro)
	}
}

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
			Allowed: boolPtr(true),
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

func flexibleFloatPtr(v flexibleFloat) *flexibleFloat { return &v }

func findQuotaWindow(windows []quotaWindow, name string) *quotaWindow {
	for i := range windows {
		if windows[i].Name == name {
			return &windows[i]
		}
	}
	return nil
}
