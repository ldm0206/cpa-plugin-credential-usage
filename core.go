package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// --- Envelope types ---

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *envelopeError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

var errHostCallFailed = &envelopeError{Code: "host_call_failed", Message: "host call returned non-zero exit code"}

func okEnvelopeJSON(result string) ([]byte, error) {
	return json.Marshal(envelope{OK: true, Result: json.RawMessage(result)})
}

func errorEnvelope(code, message string) []byte {
	raw, _ := json.Marshal(envelope{OK: false, Error: &envelopeError{Code: code, Message: message}})
	return raw
}

// --- Data model ---

type quotaState struct {
	Exceeded      bool    `json:"exceeded"`
	Reason        string  `json:"reason,omitempty"`
	NextRecoverAt *string `json:"next_recover_at,omitempty"`
	BackoffLevel  int     `json:"backoff_level,omitempty"`
}

type usageSummary struct {
	TotalRequests   int64 `json:"total_requests"`
	SuccessRequests int64 `json:"success_requests"`
	FailedRequests  int64 `json:"failed_requests"`
	InputTokens     int64 `json:"input_tokens"`
	OutputTokens    int64 `json:"output_tokens"`
	TotalTokens     int64 `json:"total_tokens"`
}

type quotaRemaining struct {
	Available             bool     `json:"available"`
	RequestsRemaining     *int64   `json:"requests_remaining,omitempty"`
	RequestsLimit         *int64   `json:"requests_limit,omitempty"`
	InputTokensRemaining  *int64   `json:"input_tokens_remaining,omitempty"`
	InputTokensLimit      *int64   `json:"input_tokens_limit,omitempty"`
	OutputTokensRemaining *int64   `json:"output_tokens_remaining,omitempty"`
	OutputTokensLimit     *int64   `json:"output_tokens_limit,omitempty"`
	CreditAmount          *float64 `json:"credit_amount,omitempty"`
	MinCreditAmount       *float64 `json:"min_credit_amount,omitempty"`
	PaidTierID            string   `json:"paid_tier_id,omitempty"`
	ResetsAt              *string  `json:"resets_at,omitempty"`
	ResetsInSeconds       *int64   `json:"resets_in_seconds,omitempty"`
	Detail                string   `json:"detail,omitempty"`
	Source                string   `json:"source"`
	UpdatedAt             string   `json:"updated_at"`
}

type credentialEntry struct {
	AuthID         string          `json:"auth_id"`
	AuthIndex      string          `json:"auth_index"`
	Provider       string          `json:"provider"`
	Label          string          `json:"label,omitempty"`
	Email          string          `json:"email,omitempty"`
	Status         string          `json:"status"`
	QuotaState     quotaState      `json:"quota_state"`
	UsageSummary   usageSummary    `json:"usage_summary"`
	QuotaRemaining *quotaRemaining `json:"quota_remaining,omitempty"`
	LastActiveAt   string          `json:"last_active_at,omitempty"`
}

type credentialStore struct {
	mu   sync.RWMutex
	data map[string]*credentialEntry
}

var store = &credentialStore{
	data: make(map[string]*credentialEntry),
}

// getOrCreate returns an existing entry or creates a new one.
// Caller must hold store.mu.
func (s *credentialStore) getOrCreate(authIndex, provider, authID string) *credentialEntry {
	if entry, ok := s.data[authIndex]; ok {
		return entry
	}
	entry := &credentialEntry{
		AuthID:    authID,
		AuthIndex: authIndex,
		Provider:  provider,
		Status:    "active",
	}
	s.data[authIndex] = entry
	return entry
}

func (s *credentialStore) getByIndex(authIndex string) *credentialEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data[authIndex]
}

func (s *credentialStore) all() []*credentialEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*credentialEntry, 0, len(s.data))
	for _, entry := range s.data {
		result = append(result, entry)
	}
	return result
}

func (s *credentialStore) allByProvider(provider string) []*credentialEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*credentialEntry, 0)
	for _, entry := range s.data {
		if entry.Provider == provider {
			result = append(result, entry)
		}
	}
	return result
}

// --- Pointer helpers for omitempty numeric fields ---

func int64Ptr(v int64) *int64       { return &v }
func float64Ptr(v float64) *float64 { return &v }

// --- Plugin configuration ---

type pluginConfig struct {
	CPABaseURL    string
	ManagementKey string
	PollInterval  time.Duration
}

var cfg pluginConfig

func parseConfig(request []byte) {
	cfg = pluginConfig{
		PollInterval: 5 * time.Minute,
	}
	if len(request) == 0 {
		return
	}
	// The lifecycle request wraps config in a "config_yaml" field
	var lifecycle struct {
		ConfigYAML json.RawMessage `json:"config_yaml"`
	}
	if err := json.Unmarshal(request, &lifecycle); err != nil {
		return
	}
	configBytes := []byte(lifecycle.ConfigYAML)
	if len(configBytes) == 0 {
		return
	}
	var raw map[string]any
	if err := json.Unmarshal(configBytes, &raw); err != nil {
		return
	}
	if v, ok := raw["cpa-base-url"].(string); ok {
		cfg.CPABaseURL = v
	}
	if v, ok := raw["management-key"].(string); ok {
		cfg.ManagementKey = v
	}
	if v, ok := raw["poll-interval"].(string); ok {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.PollInterval = d
		}
	}
}

// --- Method dispatch ---

func handleMethod(method string, request []byte) ([]byte, error) {
	switch method {
	case "plugin.register":
		parseConfig(request)
		startAuthPoller()
		maybeStartActivePoller()
		return handleRegister()
	case "plugin.reconfigure":
		parseConfig(request)
		maybeStartActivePoller()
		return handleRegister()
	case "usage.handle":
		return handleUsage(request)
	case "management.register":
		return handleManagementRegister()
	case "management.handle":
		return handleManagementHandle(request)
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}

var pluginVersion = "0.1.0"

// --- Plugin registration ---

func handleRegister() ([]byte, error) {
	registration := map[string]any{
		"schema_version": 1,
		"metadata": map[string]any{
			"Name":             "credential-usage",
			"Version":          pluginVersion,
			"Author":           "router-for-me",
			"GitHubRepository": "https://github.com/router-for-me/cpa-plugin-credential-usage",
			"ConfigFields": []map[string]any{
				{
					"Name":        "cpa-base-url",
					"Type":        "string",
					"Description": "Base URL of the CPA instance (e.g. http://localhost:3456)",
				},
				{
					"Name":        "management-key",
					"Type":        "string",
					"Description": "Management API key for authenticating requests",
				},
				{
					"Name":        "poll-interval",
					"Type":        "string",
					"Description": "Interval between credential usage polls (e.g. 5m, 30s). Default: 5m",
				},
			},
		},
		"capabilities": map[string]bool{
			"usage_plugin":   true,
			"management_api": true,
		},
	}
	raw, err := json.Marshal(registration)
	if err != nil {
		return nil, err
	}
	return okEnvelopeJSON(string(raw))
}

// --- Task 4: UsagePlugin Handler ---

type usageRecord struct {
	Provider        string              `json:"provider"`
	ExecutorType    string              `json:"executor_type"`
	Model           string              `json:"model"`
	AuthID          string              `json:"auth_id"`
	AuthIndex       string              `json:"auth_index"`
	AuthType        string              `json:"auth_type"`
	Failed          bool                `json:"failed"`
	Failure         usageFailure        `json:"failure"`
	Detail          usageDetail         `json:"detail"`
	ResponseHeaders map[string][]string `json:"response_headers,omitempty"`
}

type usageFailure struct {
	StatusCode int    `json:"status_code"`
	Body       string `json:"body"`
}

type usageDetail struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
}

func handleUsage(request []byte) ([]byte, error) {
	var record usageRecord
	if err := json.Unmarshal(request, &record); err != nil {
		callHostLog("error", fmt.Sprintf("credential-usage: failed to parse usage record: %v", err))
		return okEnvelopeJSON("{}")
	}

	authIndex := record.AuthIndex
	if authIndex == "" {
		authIndex = "unknown"
	}

	store.mu.Lock()
	entry := store.getOrCreate(authIndex, record.Provider, record.AuthID)
	entry.AuthID = record.AuthID
	entry.Provider = record.Provider
	entry.LastActiveAt = time.Now().UTC().Format(time.RFC3339)
	entry.UsageSummary.TotalRequests++
	if record.Failed {
		entry.UsageSummary.FailedRequests++
	} else {
		entry.UsageSummary.SuccessRequests++
	}
	entry.UsageSummary.InputTokens += record.Detail.InputTokens
	entry.UsageSummary.OutputTokens += record.Detail.OutputTokens
	entry.UsageSummary.TotalTokens += record.Detail.TotalTokens

	parseResponseHeadersLocked(entry, record.Provider, record.ResponseHeaders)

	if record.Failed {
		parseFailureBodyLocked(entry, record.Provider, record.Failure)
	}
	store.mu.Unlock()

	return okEnvelopeJSON("{}")
}

func parseResponseHeadersLocked(entry *credentialEntry, provider string, headers map[string][]string) {
	if len(headers) == 0 {
		return
	}
	switch provider {
	case "claude":
		parseClaudeHeaders(entry, headers)
	default:
		if v := firstHeader(headers, "Retry-After"); v != "" {
			qr := &quotaRemaining{
				Available: true,
				Source:    "response_headers",
				UpdatedAt: time.Now().UTC().Format(time.RFC3339),
			}
			if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
				resetTime := time.Now().UTC().Add(time.Duration(secs) * time.Second).Format(time.RFC3339)
				qr.ResetsAt = &resetTime
				qr.ResetsInSeconds = int64Ptr(secs)
				qr.Detail = fmt.Sprintf("Retry-After: %ds", secs)
			}
			entry.QuotaRemaining = qr
		}
	}
}

func parseClaudeHeaders(entry *credentialEntry, headers map[string][]string) {
	qr := &quotaRemaining{
		Available: true,
		Source:    "response_headers",
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	if v := firstHeaderInt(headers, "anthropic-ratelimit-requests-remaining"); v != nil {
		qr.RequestsRemaining = v
		if *v == 0 {
			qr.Available = false
		}
	}
	if v := firstHeaderInt(headers, "anthropic-ratelimit-requests-limit"); v != nil {
		qr.RequestsLimit = v
	}
	if v := firstHeaderInt(headers, "anthropic-ratelimit-input-tokens-remaining"); v != nil {
		qr.InputTokensRemaining = v
	}
	if v := firstHeaderInt(headers, "anthropic-ratelimit-input-tokens-limit"); v != nil {
		qr.InputTokensLimit = v
	}
	if v := firstHeaderInt(headers, "anthropic-ratelimit-output-tokens-remaining"); v != nil {
		qr.OutputTokensRemaining = v
	}
	if v := firstHeaderInt(headers, "anthropic-ratelimit-output-tokens-limit"); v != nil {
		qr.OutputTokensLimit = v
	}
	if v := firstHeader(headers, "anthropic-ratelimit-requests-reset"); v != "" {
		qr.ResetsAt = &v
	}

	// Build detail string
	var parts []string
	if qr.RequestsRemaining != nil && qr.RequestsLimit != nil {
		parts = append(parts, fmt.Sprintf("RPM: %d/%d", *qr.RequestsRemaining, *qr.RequestsLimit))
	}
	if qr.InputTokensRemaining != nil && qr.InputTokensLimit != nil {
		parts = append(parts, fmt.Sprintf("Input tokens: %d/%d", *qr.InputTokensRemaining, *qr.InputTokensLimit))
	}
	if qr.OutputTokensRemaining != nil && qr.OutputTokensLimit != nil {
		parts = append(parts, fmt.Sprintf("Output tokens: %d/%d", *qr.OutputTokensRemaining, *qr.OutputTokensLimit))
	}
	qr.Detail = strings.Join(parts, ", ")

	entry.QuotaRemaining = qr
}

func parseFailureBodyLocked(entry *credentialEntry, provider string, failure usageFailure) {
	switch provider {
	case "codex":
		parseCodex429(entry, failure.Body)
	}
}

func parseCodex429(entry *credentialEntry, body string) {
	if body == "" {
		return
	}
	var parsed struct {
		Error struct {
			Type            string `json:"type"`
			ResetsAt        string `json:"resets_at"`
			ResetsInSeconds int64  `json:"resets_in_seconds"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return
	}
	if parsed.Error.Type != "usage_limit_reached" {
		return
	}
	qr := entry.QuotaRemaining
	if qr == nil {
		qr = &quotaRemaining{
			Source:    "failure_body",
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		}
	}
	qr.Available = false
	qr.Source = "failure_body"
	if parsed.Error.ResetsAt != "" {
		qr.ResetsAt = &parsed.Error.ResetsAt
	}
	if parsed.Error.ResetsInSeconds > 0 {
		qr.ResetsInSeconds = int64Ptr(parsed.Error.ResetsInSeconds)
	}
	entry.QuotaRemaining = qr
	// Also update quota state
	entry.QuotaState.Exceeded = true
	entry.QuotaState.Reason = "usage_limit_reached"
	if parsed.Error.ResetsAt != "" {
		entry.QuotaState.NextRecoverAt = &parsed.Error.ResetsAt
	}
}

func firstHeader(headers map[string][]string, key string) string {
	if headers == nil {
		return ""
	}
	// Case-insensitive lookup
	for k, vals := range headers {
		if strings.EqualFold(k, key) && len(vals) > 0 {
			return vals[0]
		}
	}
	return ""
}

func firstHeaderInt(headers map[string][]string, key string) *int64 {
	v := firstHeader(headers, key)
	if v == "" {
		return nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return nil
	}
	return int64Ptr(n)
}

// --- Task 5: ManagementAPI Handlers ---

type managementRequest struct {
	Method  string              `json:"method"`
	Path    string              `json:"path"`
	Headers map[string][]string `json:"headers,omitempty"`
	Query   map[string][]string `json:"query,omitempty"`
	Body    json.RawMessage     `json:"body,omitempty"`
}

func handleManagementRegister() ([]byte, error) {
	return okEnvelopeJSON(`{"resources":[{"Path":"/list","Menu":"Credential Usage","Description":"List all credentials with quota and usage data"},{"Path":"/detail","Menu":"","Description":"Get single credential quota and usage detail"}]}`)
}

const credentialUsageResourceBasePath = "/v0/resource/plugins/credential-usage"

func normalizeResourcePath(path string) string {
	path = strings.TrimSpace(path)
	if path == credentialUsageResourceBasePath {
		return "/list"
	}
	if strings.HasPrefix(path, credentialUsageResourceBasePath+"/") {
		suffix := strings.TrimPrefix(path, credentialUsageResourceBasePath+"/")
		suffix = strings.TrimRight(suffix, "/")
		if suffix == "" {
			return "/list"
		}
		return "/" + suffix
	}
	return strings.TrimRight(path, "/")
}

func handleManagementHandle(request []byte) ([]byte, error) {
	var req managementRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return managementJSONResponse(400, map[string]string{"error": "invalid request"})
	}

	path := normalizeResourcePath(req.Path)

	if path == "/list" {
		provider := ""
		if req.Query != nil {
			if vals, ok := req.Query["provider"]; ok && len(vals) > 0 {
				provider = vals[0]
			}
		}
		var entries []*credentialEntry
		if provider != "" {
			entries = store.allByProvider(provider)
		} else {
			entries = store.all()
		}
		return managementJSONResponse(200, entries)
	}

	if path == "/detail" {
		authIndex := ""
		if req.Query != nil {
			if vals, ok := req.Query["auth_index"]; ok && len(vals) > 0 {
				authIndex = vals[0]
			}
		}
		if authIndex == "" {
			return managementJSONResponse(400, map[string]string{"error": "auth_index query parameter is required"})
		}
		entry := store.getByIndex(authIndex)
		if entry == nil {
			return managementJSONResponse(404, map[string]string{"error": "credential not found"})
		}
		return managementJSONResponse(200, entry)
	}

	return managementJSONResponse(404, map[string]string{"error": "not found"})
}

func managementJSONResponse(statusCode int, body any) ([]byte, error) {
	bodyJSON, _ := json.Marshal(body)
	encoded := base64.StdEncoding.EncodeToString(bodyJSON)
	result := fmt.Sprintf(`{"StatusCode":%d,"Headers":{"content-type":["application/json"]},"Body":"%s"}`, statusCode, encoded)
	return okEnvelopeJSON(result)
}

// --- Task 6: Host Auth Callback Polling ---

type hostAuthFileEntry struct {
	ID             string `json:"id,omitempty"`
	AuthIndex      string `json:"auth_index,omitempty"`
	Name           string `json:"name"`
	Provider       string `json:"provider,omitempty"`
	Label          string `json:"label,omitempty"`
	Status         string `json:"status,omitempty"`
	StatusMessage  string `json:"status_message,omitempty"`
	Disabled       bool   `json:"disabled,omitempty"`
	Unavailable    bool   `json:"unavailable,omitempty"`
	Email          string `json:"email,omitempty"`
	Success        int64  `json:"success,omitempty"`
	Failed         int64  `json:"failed,omitempty"`
	NextRetryAfter string `json:"next_retry_after,omitempty"`
}

var (
	authPollerStarted   bool
	authPollerStartedMu sync.Once
)

func startAuthPoller() {
	authPollerStartedMu.Do(func() {
		authPollerStarted = true
		go authPollLoop()
	})
}

func authPollLoop() {
	for {
		pollAuthList()
		time.Sleep(30 * time.Second)
	}
}

func pollAuthList() {
	resp, err := callHostWithResponse("host.auth.list", []byte("{}"))
	if err != nil {
		callHostLog("error", fmt.Sprintf("credential-usage: host.auth.list failed: %v", err))
		return
	}

	var env envelope
	if err := json.Unmarshal(resp, &env); err != nil || !env.OK {
		callHostLog("error", fmt.Sprintf("credential-usage: host.auth.list envelope error: %v", err))
		return
	}

	// Result is a JSON object with "files" array
	var listResult struct {
		Files []hostAuthFileEntry `json:"files"`
	}
	if err := json.Unmarshal(env.Result, &listResult); err != nil {
		callHostLog("error", fmt.Sprintf("credential-usage: host.auth.list parse error: %v", err))
		return
	}

	for _, fileEntry := range listResult.Files {
		if fileEntry.Disabled {
			continue
		}
		authIndex := fileEntry.AuthIndex
		if authIndex == "" {
			continue
		}
		pollAuthRuntime(authIndex, fileEntry)
	}
}

func pollAuthRuntime(authIndex string, listEntry hostAuthFileEntry) {
	payload, _ := json.Marshal(map[string]string{"auth_index": authIndex})
	resp, err := callHostWithResponse("host.auth.get_runtime", payload)

	var runtimeEntry hostAuthFileEntry
	if err == nil {
		var env envelope
		if err := json.Unmarshal(resp, &env); err == nil && env.OK {
			// host.auth.get_runtime returns {"auth": HostAuthFileEntry}
			var runtimeResult struct {
				Auth hostAuthFileEntry `json:"auth"`
			}
			if err := json.Unmarshal(env.Result, &runtimeResult); err == nil {
				runtimeEntry = runtimeResult.Auth
			}
		}
	}

	// Fall back to list entry data on failure
	if runtimeEntry.AuthIndex == "" {
		runtimeEntry = listEntry
	}

	mergeAuthFileEntry(authIndex, runtimeEntry)
}

func mergeAuthFileEntry(authIndex string, entry hostAuthFileEntry) {
	store.mu.Lock()
	defer store.mu.Unlock()
	storeEntry := store.getOrCreate(authIndex, entry.Provider, entry.ID)
	if entry.ID != "" {
		storeEntry.AuthID = entry.ID
	}
	if entry.Provider != "" {
		storeEntry.Provider = entry.Provider
	}
	if entry.Label != "" {
		storeEntry.Label = entry.Label
	}
	if entry.Email != "" {
		storeEntry.Email = entry.Email
	}
	if entry.Status != "" {
		storeEntry.Status = entry.Status
	}

	// Set quota state from unavailable/status_message/next_retry_after
	if entry.Unavailable {
		storeEntry.QuotaState.Exceeded = true
		if entry.StatusMessage != "" {
			storeEntry.QuotaState.Reason = entry.StatusMessage
		}
		if entry.NextRetryAfter != "" {
			storeEntry.QuotaState.NextRecoverAt = &entry.NextRetryAfter
		}
	} else if entry.Status == "active" || entry.Status == "" {
		// If the auth reports active and not unavailable, clear exceeded state
		storeEntry.QuotaState.Exceeded = false
		storeEntry.QuotaState.Reason = ""
		storeEntry.QuotaState.NextRecoverAt = nil
	}
}

// --- Task 7: Active Query Mode ---

var (
	activePollerStarted   bool
	activePollerStartedMu sync.Once
)

func maybeStartActivePoller() {
	if cfg.CPABaseURL == "" || cfg.ManagementKey == "" {
		return
	}
	activePollerStartedMu.Do(func() {
		activePollerStarted = true
		go activePollLoop()
	})
}

func activePollLoop() {
	for {
		if cfg.CPABaseURL == "" || cfg.ManagementKey == "" {
			time.Sleep(30 * time.Second)
			continue
		}
		queryAntigravityCredits()
		time.Sleep(cfg.PollInterval)
	}
}

func queryAntigravityCredits() {
	entries := store.all()
	for _, entry := range entries {
		if entry.Provider == "antigravity" || entry.Provider == "gemini-cli" {
			queryLoadCodeAssist(entry.AuthIndex)
		}
	}
}

type apiCallResponse struct {
	StatusCode int    `json:"status_code"`
	Body       string `json:"body"`
}

type loadCodeAssistResponse struct {
	PaidTier struct {
		AvailableCredits []struct {
			CreditAmount        float64 `json:"creditAmount"`
			MinimumCreditAmount float64 `json:"minimumCreditAmountForUsage"`
		} `json:"availableCredits"`
	} `json:"paidTier"`
}

func queryLoadCodeAssist(authIndex string) {
	if cfg.CPABaseURL == "" || cfg.ManagementKey == "" {
		return
	}

	// Build the api-call payload
	apiCallPayload, _ := json.Marshal(map[string]any{
		"auth_index": authIndex,
		"method":     "POST",
		"url":        "https://cloudcode-pa.googleapis.com/v1beta:loadCodeAssist",
		"header": map[string]string{
			"Authorization": "Bearer $TOKEN$",
			"Content-Type":  "application/json",
		},
		"data": `{"metadata":{"ideType":"ANTIGRAVITY"}}`,
	})

	// Build the host.http.do request
	// Host expects: method, url, headers (plural, map[string][]string), body ([]byte = base64 in JSON)
	targetURL := cfg.CPABaseURL + "/v0/management/api-call"
	hostPayload, _ := json.Marshal(map[string]any{
		"method": "POST",
		"url":    targetURL,
		"headers": map[string][]string{
			"Authorization": {"Bearer " + cfg.ManagementKey},
			"Content-Type":  {"application/json"},
		},
		"body": []byte(apiCallPayload),
	})
	resp, err := callHostWithResponse("host.http.do", hostPayload)
	if err != nil {
		callHostLog("error", fmt.Sprintf("credential-usage: host.http.do failed for %s: %v", authIndex, err))
		return
	}

	// Unwrap envelope
	var env envelope
	if err := json.Unmarshal(resp, &env); err != nil || !env.OK {
		callHostLog("error", fmt.Sprintf("credential-usage: host.http.do envelope error for %s: %v", authIndex, err))
		return
	}

	// Unwrap HTTP response
	var httpResp struct {
		StatusCode int                 `json:"StatusCode"`
		Headers    map[string][]string `json:"Headers"`
		Body       string              `json:"Body"`
	}
	if err := json.Unmarshal(env.Result, &httpResp); err != nil {
		callHostLog("error", fmt.Sprintf("credential-usage: host.http.do parse error for %s: %v", authIndex, err))
		return
	}

	if httpResp.StatusCode != 200 {
		return
	}

	// Decode base64 body if present
	var bodyStr string
	if httpResp.Body != "" {
		decoded, err := base64.StdEncoding.DecodeString(httpResp.Body)
		if err != nil {
			// Body might not be base64
			bodyStr = httpResp.Body
		} else {
			bodyStr = string(decoded)
		}
	}

	// Parse apiCallResponse
	var apiResp apiCallResponse
	if err := json.Unmarshal([]byte(bodyStr), &apiResp); err != nil {
		// Try parsing bodyStr directly as loadCodeAssistResponse
		var assistResp loadCodeAssistResponse
		if err := json.Unmarshal([]byte(bodyStr), &assistResp); err != nil {
			return
		}
		updateAntigravityQuota(authIndex, &assistResp)
		return
	}

	if apiResp.StatusCode != 200 {
		return
	}

	// Parse the inner body as loadCodeAssistResponse
	var assistResp loadCodeAssistResponse
	if err := json.Unmarshal([]byte(apiResp.Body), &assistResp); err != nil {
		return
	}

	updateAntigravityQuota(authIndex, &assistResp)
}

func updateAntigravityQuota(authIndex string, resp *loadCodeAssistResponse) {
	if len(resp.PaidTier.AvailableCredits) == 0 {
		return
	}

	credit := resp.PaidTier.AvailableCredits[0]

	store.mu.Lock()
	defer store.mu.Unlock()

	entry := store.data[authIndex]
	if entry == nil {
		return
	}

	qr := entry.QuotaRemaining
	if qr == nil {
		qr = &quotaRemaining{
			Source:    "upstream_api",
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		}
	}
	qr.CreditAmount = float64Ptr(credit.CreditAmount)
	qr.MinCreditAmount = float64Ptr(credit.MinimumCreditAmount)
	qr.Source = "upstream_api"
	qr.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	qr.Available = credit.CreditAmount > credit.MinimumCreditAmount
	qr.Detail = fmt.Sprintf("Credits: %.2f / min: %.2f", credit.CreditAmount, credit.MinimumCreditAmount)
	entry.QuotaRemaining = qr

	if credit.CreditAmount <= credit.MinimumCreditAmount {
		entry.QuotaState.Exceeded = true
		entry.QuotaState.Reason = "insufficient_credits"
	} else {
		entry.QuotaState.Exceeded = false
		entry.QuotaState.Reason = ""
		entry.QuotaState.NextRecoverAt = nil
	}
}
