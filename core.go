package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
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
	TotalRequests   int64
	SuccessRequests int64
	FailedRequests  int64
	InputTokens     int64
	OutputTokens    int64
	TotalTokens     int64
}

type quotaDetails struct {
	Source                           string                `json:"source,omitempty"`
	UpdatedAt                        string                `json:"updated_at,omitempty"`
	Available                        *bool                 `json:"available,omitempty"`
	Windows                          []quotaWindow         `json:"windows,omitempty"`
	OverallResetAt                   string                `json:"overall_reset_at,omitempty"`
	RateLimits                       *rateLimitDetails     `json:"rate_limits,omitempty"`
	Credits                          *creditDetails        `json:"credits,omitempty"`
	ModelQuotas                      map[string]modelQuota `json:"model_quotas,omitempty"`
	ResetsAt                         string                `json:"resets_at,omitempty"`
	ResetsInSeconds                  *int64                `json:"resets_in_seconds,omitempty"`
	PlanType                         string                `json:"plan_type,omitempty"`
	ErrorType                        string                `json:"error_type,omitempty"`
	ErrorStatus                      string                `json:"error_status,omitempty"`
	ErrorReason                      string                `json:"error_reason,omitempty"`
	Model                            string                `json:"model,omitempty"`
	RetryDelay                       string                `json:"retry_delay,omitempty"`
	PrimaryOverSecondaryLimitPercent *float64              `json:"primary_over_secondary_limit_percent,omitempty"`
	Detail                           string                `json:"detail,omitempty"`
}

type quotaWindow struct {
	Name               string   `json:"name"`
	Label              string   `json:"label,omitempty"`
	Status             string   `json:"status,omitempty"`
	Utilization        *float64 `json:"utilization,omitempty"`
	UsedPercent        *float64 `json:"used_percent,omitempty"`
	SurpassedThreshold *bool    `json:"surpassed_threshold,omitempty"`
	ResetAt            string   `json:"reset_at,omitempty"`
	ResetAfterSeconds  *int64   `json:"reset_after_seconds,omitempty"`
	WindowMinutes      *int64   `json:"window_minutes,omitempty"`
}

type modelQuota struct {
	RemainingFraction  *float64        `json:"remaining_fraction,omitempty"`
	ResetTime          string          `json:"reset_time,omitempty"`
	DisplayName        string          `json:"display_name,omitempty"`
	SupportsImages     *bool           `json:"supports_images,omitempty"`
	SupportsThinking   *bool           `json:"supports_thinking,omitempty"`
	ThinkingBudget     *int64          `json:"thinking_budget,omitempty"`
	Recommended        *bool           `json:"recommended,omitempty"`
	MaxTokens          *int64          `json:"max_tokens,omitempty"`
	MaxOutputTokens    *int64          `json:"max_output_tokens,omitempty"`
	SupportedMimeTypes map[string]bool `json:"supported_mime_types,omitempty"`
}

type rateLimitDetails struct {
	Requests     *rateLimitBucket `json:"requests,omitempty"`
	InputTokens  *rateLimitBucket `json:"input_tokens,omitempty"`
	OutputTokens *rateLimitBucket `json:"output_tokens,omitempty"`
	Tokens       *rateLimitBucket `json:"tokens,omitempty"`
}

type rateLimitBucket struct {
	Limit     *int64 `json:"limit,omitempty"`
	Remaining *int64 `json:"remaining,omitempty"`
	ResetAt   string `json:"reset_at,omitempty"`
}

type creditDetails struct {
	Amount                  *float64      `json:"amount,omitempty"`
	MinimumForUsage         *float64      `json:"minimum_for_usage,omitempty"`
	PaidTierID              string        `json:"paid_tier_id,omitempty"`
	PaidTierName            string        `json:"paid_tier_name,omitempty"`
	CurrentTierID           string        `json:"current_tier_id,omitempty"`
	CurrentTierName         string        `json:"current_tier_name,omitempty"`
	CloudAICompanionProject string        `json:"cloudaicompanion_project,omitempty"`
	Items                   []creditItem  `json:"items,omitempty"`
	IneligibleTiers         []tierProblem `json:"ineligible_tiers,omitempty"`
	AllowedTiers            []allowedTier `json:"allowed_tiers,omitempty"`
}

type creditItem struct {
	CreditType      string   `json:"credit_type,omitempty"`
	Amount          *float64 `json:"amount,omitempty"`
	MinimumForUsage *float64 `json:"minimum_for_usage,omitempty"`
}

type flexibleFloat float64

func (f *flexibleFloat) UnmarshalJSON(raw []byte) error {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		*f = 0
		return nil
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		value = strings.TrimSpace(asString)
	}
	if value == "" {
		*f = 0
		return nil
	}
	n, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return err
	}
	*f = flexibleFloat(n)
	return nil
}

type tierProblem struct {
	TierID        string `json:"tier_id,omitempty"`
	TierName      string `json:"tier_name,omitempty"`
	ReasonCode    string `json:"reason_code,omitempty"`
	ReasonMessage string `json:"reason_message,omitempty"`
}

type allowedTier struct {
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	IsDefault *bool  `json:"is_default,omitempty"`
}

type credentialEntry struct {
	AuthID       string       `json:"auth_id"`
	AuthIndex    string       `json:"auth_index"`
	Provider     string       `json:"provider"`
	Label        string       `json:"label,omitempty"`
	Email        string       `json:"email,omitempty"`
	Status       string       `json:"status"`
	QuotaState   quotaState   `json:"quota_state"`
	UsageSummary usageSummary `json:"-"`
	QuotaDetails quotaDetails `json:"quota_details"`
	LastActiveAt string       `json:"last_active_at,omitempty"`
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
	return copyCredentialEntry(s.data[authIndex])
}

func (s *credentialStore) all() []*credentialEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*credentialEntry, 0, len(s.data))
	for _, entry := range s.data {
		result = append(result, copyCredentialEntry(entry))
	}
	return result
}

func (s *credentialStore) allByProvider(provider string) []*credentialEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*credentialEntry, 0)
	for _, entry := range s.data {
		if entry.Provider == provider {
			result = append(result, copyCredentialEntry(entry))
		}
	}
	return result
}

func copyCredentialEntry(entry *credentialEntry) *credentialEntry {
	if entry == nil {
		return nil
	}
	copyEntry := *entry
	copyEntry.QuotaDetails = copyQuotaDetails(entry.QuotaDetails)
	return &copyEntry
}

func copyQuotaDetails(details quotaDetails) quotaDetails {
	copyDetails := details
	if details.Available != nil {
		available := *details.Available
		copyDetails.Available = &available
	}
	copyDetails.Windows = make([]quotaWindow, len(details.Windows))
	for i, window := range details.Windows {
		copyDetails.Windows[i] = copyQuotaWindow(window)
	}
	copyDetails.RateLimits = copyRateLimitDetails(details.RateLimits)
	copyDetails.Credits = copyCreditDetails(details.Credits)
	if details.ResetsInSeconds != nil {
		copyDetails.ResetsInSeconds = int64Ptr(*details.ResetsInSeconds)
	}
	if details.PrimaryOverSecondaryLimitPercent != nil {
		copyDetails.PrimaryOverSecondaryLimitPercent = float64Ptr(*details.PrimaryOverSecondaryLimitPercent)
	}
	if details.ModelQuotas != nil {
		copyDetails.ModelQuotas = make(map[string]modelQuota, len(details.ModelQuotas))
		for model, quota := range details.ModelQuotas {
			copyDetails.ModelQuotas[model] = copyModelQuota(quota)
		}
	}
	return copyDetails
}

func copyRateLimitDetails(limits *rateLimitDetails) *rateLimitDetails {
	if limits == nil {
		return nil
	}
	return &rateLimitDetails{
		Requests:     copyRateLimitBucket(limits.Requests),
		InputTokens:  copyRateLimitBucket(limits.InputTokens),
		OutputTokens: copyRateLimitBucket(limits.OutputTokens),
		Tokens:       copyRateLimitBucket(limits.Tokens),
	}
}

func copyQuotaWindow(window quotaWindow) quotaWindow {
	copyWindow := window
	if window.Utilization != nil {
		copyWindow.Utilization = float64Ptr(*window.Utilization)
	}
	if window.UsedPercent != nil {
		copyWindow.UsedPercent = float64Ptr(*window.UsedPercent)
	}
	if window.SurpassedThreshold != nil {
		surpassedThreshold := *window.SurpassedThreshold
		copyWindow.SurpassedThreshold = &surpassedThreshold
	}
	if window.ResetAfterSeconds != nil {
		copyWindow.ResetAfterSeconds = int64Ptr(*window.ResetAfterSeconds)
	}
	if window.WindowMinutes != nil {
		copyWindow.WindowMinutes = int64Ptr(*window.WindowMinutes)
	}
	return copyWindow
}

func copyRateLimitBucket(bucket *rateLimitBucket) *rateLimitBucket {
	if bucket == nil {
		return nil
	}
	copyBucket := *bucket
	if bucket.Limit != nil {
		copyBucket.Limit = int64Ptr(*bucket.Limit)
	}
	if bucket.Remaining != nil {
		copyBucket.Remaining = int64Ptr(*bucket.Remaining)
	}
	return &copyBucket
}

func copyCreditDetails(credits *creditDetails) *creditDetails {
	if credits == nil {
		return nil
	}
	copyCredits := *credits
	if credits.Amount != nil {
		copyCredits.Amount = float64Ptr(*credits.Amount)
	}
	if credits.MinimumForUsage != nil {
		copyCredits.MinimumForUsage = float64Ptr(*credits.MinimumForUsage)
	}
	copyCredits.Items = make([]creditItem, len(credits.Items))
	for i, item := range credits.Items {
		copyCredits.Items[i] = item
		if item.Amount != nil {
			copyCredits.Items[i].Amount = float64Ptr(*item.Amount)
		}
		if item.MinimumForUsage != nil {
			copyCredits.Items[i].MinimumForUsage = float64Ptr(*item.MinimumForUsage)
		}
	}
	copyCredits.IneligibleTiers = append([]tierProblem(nil), credits.IneligibleTiers...)
	copyCredits.AllowedTiers = make([]allowedTier, len(credits.AllowedTiers))
	for i, tier := range credits.AllowedTiers {
		copyCredits.AllowedTiers[i] = tier
		if tier.IsDefault != nil {
			isDefault := *tier.IsDefault
			copyCredits.AllowedTiers[i].IsDefault = &isDefault
		}
	}
	return &copyCredits
}

func copyModelQuota(quota modelQuota) modelQuota {
	copyQuota := quota
	if quota.RemainingFraction != nil {
		copyQuota.RemainingFraction = float64Ptr(*quota.RemainingFraction)
	}
	if quota.SupportsImages != nil {
		supportsImages := *quota.SupportsImages
		copyQuota.SupportsImages = &supportsImages
	}
	if quota.SupportsThinking != nil {
		supportsThinking := *quota.SupportsThinking
		copyQuota.SupportsThinking = &supportsThinking
	}
	if quota.ThinkingBudget != nil {
		copyQuota.ThinkingBudget = int64Ptr(*quota.ThinkingBudget)
	}
	if quota.Recommended != nil {
		recommended := *quota.Recommended
		copyQuota.Recommended = &recommended
	}
	if quota.MaxTokens != nil {
		copyQuota.MaxTokens = int64Ptr(*quota.MaxTokens)
	}
	if quota.MaxOutputTokens != nil {
		copyQuota.MaxOutputTokens = int64Ptr(*quota.MaxOutputTokens)
	}
	if quota.SupportedMimeTypes != nil {
		copyQuota.SupportedMimeTypes = make(map[string]bool, len(quota.SupportedMimeTypes))
		for mimeType, supported := range quota.SupportedMimeTypes {
			copyQuota.SupportedMimeTypes[mimeType] = supported
		}
	}
	return copyQuota
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
	InputTokens         int64  `json:"input_tokens"`
	OutputTokens        int64  `json:"output_tokens"`
	ReasoningTokens     int64  `json:"reasoning_tokens"`
	CachedTokens        int64  `json:"cached_tokens"`
	CacheReadTokens     int64  `json:"cache_read_tokens"`
	CacheCreationTokens int64  `json:"cache_creation_tokens"`
	TotalTokens         int64  `json:"total_tokens"`
	ReasoningEffort     string `json:"reasoning_effort"`
	ServiceTier         string `json:"service_tier"`
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
	case "codex":
		parseCodexHeaders(entry, headers)
	default:
		if v := firstHeader(headers, "Retry-After"); v != "" {
			parseRetryAfterQuota(entry, "response_headers", v)
		}
	}
}

func parseRetryAfterQuota(entry *credentialEntry, source, value string) {
	details := entry.QuotaDetails
	if !parseRetryAfterIntoDetails(&details, value) {
		return
	}
	details.Source = source
	details.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	entry.QuotaDetails = details
}

func parseRetryAfterIntoDetails(details *quotaDetails, value string) bool {
	value = strings.TrimSpace(value)
	secs, err := strconv.ParseInt(value, 10, 64)
	if err == nil {
		available := false
		details.Available = &available
		details.ResetsAt = time.Now().UTC().Add(time.Duration(secs) * time.Second).Format(time.RFC3339)
		details.ResetsInSeconds = int64Ptr(secs)
		details.Detail = fmt.Sprintf("Retry-After: %ds", secs)
		return true
	}
	resetTime, err := http.ParseTime(value)
	if err != nil {
		return false
	}
	available := false
	details.Available = &available
	details.ResetsAt = resetTime.UTC().Format(time.RFC3339)
	if remaining := int64(time.Until(resetTime).Seconds()); remaining > 0 {
		details.ResetsInSeconds = int64Ptr(remaining)
	}
	details.Detail = "Retry-After: " + value
	return true
}

func parseClaudeHeaders(entry *credentialEntry, headers map[string][]string) {
	details := quotaDetails{UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	available := true
	hasDetails := false
	source := ""

	if window, ok := anthropicQuotaWindow(headers, "5h", "5h", "5 hour limit", true); ok {
		details.Windows = append(details.Windows, window)
		hasDetails = true
		source = "anthropic_headers"
		if strings.EqualFold(window.Status, "rejected") {
			available = false
		}
	}
	if window, ok := anthropicQuotaWindow(headers, "7d", "7d", "weekly limit", true); ok {
		details.Windows = append(details.Windows, window)
		hasDetails = true
		source = "anthropic_headers"
		if strings.EqualFold(window.Status, "rejected") {
			available = false
		}
	}
	if v := firstHeader(headers, "anthropic-ratelimit-unified-reset"); v != "" {
		details.OverallResetAt = v
		hasDetails = true
		source = "anthropic_headers"
	}

	rateLimits := &rateLimitDetails{
		Requests: buildRateLimitBucket(
			firstHeaderInt(headers, "anthropic-ratelimit-requests-limit"),
			firstHeaderInt(headers, "anthropic-ratelimit-requests-remaining"),
			firstHeader(headers, "anthropic-ratelimit-requests-reset"),
		),
		InputTokens: buildRateLimitBucket(
			firstHeaderInt(headers, "anthropic-ratelimit-input-tokens-limit"),
			firstHeaderInt(headers, "anthropic-ratelimit-input-tokens-remaining"),
			"",
		),
		OutputTokens: buildRateLimitBucket(
			firstHeaderInt(headers, "anthropic-ratelimit-output-tokens-limit"),
			firstHeaderInt(headers, "anthropic-ratelimit-output-tokens-remaining"),
			"",
		),
	}
	if !rateLimits.empty() {
		source = "anthropic_headers"
	}
	if rateLimits.Requests == nil {
		rateLimits.Requests = buildRateLimitBucket(
			firstHeaderInt(headers, "x-ratelimit-limit-requests"),
			firstHeaderInt(headers, "x-ratelimit-remaining-requests"),
			firstHeader(headers, "x-ratelimit-reset-requests"),
		)
		if rateLimits.Requests != nil && source == "" {
			source = "response_headers"
		}
	}
	rateLimits.Tokens = buildRateLimitBucket(
		firstHeaderInt(headers, "x-ratelimit-limit-tokens"),
		firstHeaderInt(headers, "x-ratelimit-remaining-tokens"),
		firstHeader(headers, "x-ratelimit-reset-tokens"),
	)
	if rateLimits.Tokens != nil && source == "" {
		source = "response_headers"
	}
	if !rateLimits.empty() {
		details.RateLimits = rateLimits
		hasDetails = true
		if rateLimits.Requests != nil && rateLimits.Requests.Remaining != nil && *rateLimits.Requests.Remaining == 0 {
			available = false
		}
	}
	if v := firstHeader(headers, "Retry-After"); v != "" {
		if parseRetryAfterIntoDetails(&details, v) {
			hasDetails = true
			available = false
			if source == "" {
				source = "response_headers"
			}
		}
	}

	var parts []string
	if rateLimits.Requests != nil && rateLimits.Requests.Remaining != nil && rateLimits.Requests.Limit != nil {
		parts = append(parts, fmt.Sprintf("RPM: %d/%d", *rateLimits.Requests.Remaining, *rateLimits.Requests.Limit))
	}
	if rateLimits.InputTokens != nil && rateLimits.InputTokens.Remaining != nil && rateLimits.InputTokens.Limit != nil {
		parts = append(parts, fmt.Sprintf("Input tokens: %d/%d", *rateLimits.InputTokens.Remaining, *rateLimits.InputTokens.Limit))
	}
	if rateLimits.OutputTokens != nil && rateLimits.OutputTokens.Remaining != nil && rateLimits.OutputTokens.Limit != nil {
		parts = append(parts, fmt.Sprintf("Output tokens: %d/%d", *rateLimits.OutputTokens.Remaining, *rateLimits.OutputTokens.Limit))
	}
	if rateLimits.Tokens != nil && rateLimits.Tokens.Remaining != nil && rateLimits.Tokens.Limit != nil {
		parts = append(parts, fmt.Sprintf("Tokens: %d/%d", *rateLimits.Tokens.Remaining, *rateLimits.Tokens.Limit))
	}
	if len(parts) > 0 && details.Detail == "" {
		details.Detail = strings.Join(parts, ", ")
	}

	if !hasDetails {
		return
	}
	if existing := quotaWindowByName(entry.QuotaDetails.Windows, "7d_sonnet"); existing != nil {
		details.Windows = upsertQuotaWindow(details.Windows, *existing)
	}
	details.Available = &available
	details.Source = source
	entry.QuotaDetails = details
}

func anthropicQuotaWindow(headers map[string][]string, prefix, name, label string, includeStatus bool) (quotaWindow, bool) {
	statusHeader := "anthropic-ratelimit-unified-" + prefix + "-status"
	resetHeader := "anthropic-ratelimit-unified-" + prefix + "-reset"
	utilizationHeader := "anthropic-ratelimit-unified-" + prefix + "-utilization"
	thresholdHeader := "anthropic-ratelimit-unified-" + prefix + "-surpassed-threshold"

	names := []string{resetHeader, utilizationHeader, thresholdHeader}
	if includeStatus {
		names = append(names, statusHeader)
	}
	if !hasAnyHeader(headers, names...) {
		return quotaWindow{}, false
	}

	window := quotaWindow{
		Name:               name,
		Label:              label,
		Utilization:        firstHeaderFloat(headers, utilizationHeader),
		SurpassedThreshold: firstHeaderBool(headers, thresholdHeader),
		ResetAt:            firstHeader(headers, resetHeader),
	}
	if includeStatus {
		window.Status = firstHeader(headers, statusHeader)
	}
	return window, true
}

func buildRateLimitBucket(limit, remaining *int64, resetAt string) *rateLimitBucket {
	if limit == nil && remaining == nil && resetAt == "" {
		return nil
	}
	return &rateLimitBucket{Limit: limit, Remaining: remaining, ResetAt: resetAt}
}

func (limits *rateLimitDetails) empty() bool {
	return limits == nil || (limits.Requests == nil && limits.InputTokens == nil && limits.OutputTokens == nil && limits.Tokens == nil)
}

func parseCodexHeaders(entry *credentialEntry, headers map[string][]string) {
	details := quotaDetails{UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	available := true
	hasDetails := false

	if window, ok := codexQuotaWindow(headers, "primary", "primary"); ok {
		details.Windows = append(details.Windows, window)
		hasDetails = true
		if window.UsedPercent != nil && *window.UsedPercent >= 100 {
			available = false
		}
	}
	if window, ok := codexQuotaWindow(headers, "secondary", "secondary"); ok {
		details.Windows = append(details.Windows, window)
		hasDetails = true
		if window.UsedPercent != nil && *window.UsedPercent >= 100 {
			available = false
		}
	}
	if v := firstHeaderFloat(headers, "x-codex-primary-over-secondary-limit-percent"); v != nil {
		details.PrimaryOverSecondaryLimitPercent = v
		hasDetails = true
	}
	if v := firstHeader(headers, "Retry-After"); v != "" {
		if parseRetryAfterIntoDetails(&details, v) {
			hasDetails = true
			available = false
		}
	}
	if !hasDetails {
		return
	}
	details.Source = "codex_headers"
	details.Available = &available
	entry.QuotaDetails = details
}

func codexQuotaWindow(headers map[string][]string, prefix, name string) (quotaWindow, bool) {
	usedHeader := "x-codex-" + prefix + "-used-percent"
	resetHeader := "x-codex-" + prefix + "-reset-after-seconds"
	windowHeader := "x-codex-" + prefix + "-window-minutes"
	if !hasAnyHeader(headers, usedHeader, resetHeader, windowHeader) {
		return quotaWindow{}, false
	}
	windowMinutes := firstHeaderInt(headers, windowHeader)
	window := quotaWindow{
		Name:              name,
		Label:             codexWindowLabel(name, windowMinutes),
		UsedPercent:       firstHeaderFloat(headers, usedHeader),
		ResetAfterSeconds: firstHeaderInt(headers, resetHeader),
		WindowMinutes:     windowMinutes,
	}
	return window, true
}

func codexWindowLabel(name string, minutes *int64) string {
	if minutes == nil {
		return name + " window"
	}
	switch *minutes {
	case 300:
		return name + " window (5h)"
	case 10080:
		return name + " window (7d)"
	default:
		return fmt.Sprintf("%s window (%dm)", name, *minutes)
	}
}

func parseFailureBodyLocked(entry *credentialEntry, provider string, failure usageFailure) {
	switch provider {
	case "codex":
		parseCodex429(entry, failure.Body)
	case "antigravity", "gemini-cli":
		parseAntigravity429(entry, failure.Body)
	}
}

func parseCodex429(entry *credentialEntry, body string) {
	if body == "" {
		return
	}
	var parsed struct {
		Error struct {
			Type            string          `json:"type"`
			Message         string          `json:"message"`
			ResetsAt        json.RawMessage `json:"resets_at"`
			ResetsInSeconds json.RawMessage `json:"resets_in_seconds"`
			PlanType        string          `json:"plan_type"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return
	}
	if parsed.Error.Type != "usage_limit_reached" && parsed.Error.Type != "rate_limit_exceeded" {
		return
	}
	available := false
	details := entry.QuotaDetails
	details.Available = &available
	details.Source = "failure_body"
	details.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	details.ErrorType = parsed.Error.Type
	details.PlanType = parsed.Error.PlanType
	if parsed.Error.Message != "" {
		details.Detail = parsed.Error.Message
	}
	if resetAt := parseResetAt(parsed.Error.ResetsAt); resetAt != "" {
		details.ResetsAt = resetAt
	}
	if secs := parseRawInt64(parsed.Error.ResetsInSeconds); secs != nil && *secs > 0 {
		details.ResetsInSeconds = secs
		if details.ResetsAt == "" {
			details.ResetsAt = time.Now().UTC().Add(time.Duration(*secs) * time.Second).Format(time.RFC3339)
		}
	}
	entry.QuotaDetails = details
	// Also update quota state
	entry.QuotaState.Exceeded = true
	entry.QuotaState.Reason = parsed.Error.Type
	if details.ResetsAt != "" {
		entry.QuotaState.NextRecoverAt = &details.ResetsAt
	}
}

func parseAntigravity429(entry *credentialEntry, body string) {
	if body == "" {
		return
	}
	var parsed struct {
		Error struct {
			Status  string `json:"status"`
			Message string `json:"message"`
			Details []struct {
				Type       string            `json:"@type"`
				Reason     string            `json:"reason"`
				Metadata   map[string]string `json:"metadata"`
				RetryDelay string            `json:"retryDelay"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return
	}

	errorReason := ""
	model := ""
	retryDelay := ""
	for _, detail := range parsed.Error.Details {
		if errorReason == "" && detail.Reason != "" {
			errorReason = detail.Reason
		}
		if model == "" && detail.Metadata != nil {
			model = detail.Metadata["model"]
		}
		if retryDelay == "" && detail.RetryDelay != "" {
			retryDelay = detail.RetryDelay
		}
	}
	if parsed.Error.Status == "" && errorReason == "" && retryDelay == "" {
		return
	}

	available := false
	details := entry.QuotaDetails
	details.Available = &available
	details.Source = "failure_body"
	details.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	details.ErrorStatus = parsed.Error.Status
	details.ErrorReason = errorReason
	details.Model = model
	details.RetryDelay = retryDelay
	if parsed.Error.Message != "" {
		details.Detail = parsed.Error.Message
	}
	if secs := parseRetryDelaySeconds(retryDelay); secs != nil {
		details.ResetsInSeconds = secs
		details.ResetsAt = time.Now().UTC().Add(time.Duration(*secs) * time.Second).Format(time.RFC3339)
	}
	entry.QuotaDetails = details

	entry.QuotaState.Exceeded = true
	if errorReason != "" {
		entry.QuotaState.Reason = strings.ToLower(errorReason)
	} else if parsed.Error.Status != "" {
		entry.QuotaState.Reason = strings.ToLower(parsed.Error.Status)
	}
	if details.ResetsAt != "" {
		entry.QuotaState.NextRecoverAt = &details.ResetsAt
	}
}

func parseResetAt(raw json.RawMessage) string {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return ""
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		asString = strings.TrimSpace(asString)
		if asString == "" {
			return ""
		}
		if formatted := parseResetAtString(asString); formatted != "" {
			return formatted
		}
		return ""
	}
	if n, err := strconv.ParseFloat(value, 64); err == nil {
		return time.Unix(int64(n), 0).UTC().Format(time.RFC3339)
	}
	return ""
}

func parseResetAtString(value string) string {
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t.UTC().Format(time.RFC3339)
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t.UTC().Format(time.RFC3339)
	}
	if n, err := strconv.ParseFloat(value, 64); err == nil {
		return time.Unix(int64(n), 0).UTC().Format(time.RFC3339)
	}
	return ""
}

func parseRawInt64(raw json.RawMessage) *int64 {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return nil
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		value = strings.TrimSpace(asString)
	}
	if value == "" {
		return nil
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return nil
	}
	return int64Ptr(n)
}

func parseRetryDelaySeconds(value string) *int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return nil
	}
	secs := int64(d / time.Second)
	if secs == 0 && d > 0 {
		secs = 1
	}
	return int64Ptr(secs)
}

func firstHeader(headers map[string][]string, key string) string {
	if headers == nil {
		return ""
	}
	// Case-insensitive lookup
	for k, vals := range headers {
		if strings.EqualFold(k, key) && len(vals) > 0 {
			return strings.TrimSpace(vals[0])
		}
	}
	return ""
}

func hasAnyHeader(headers map[string][]string, keys ...string) bool {
	for _, key := range keys {
		if firstHeader(headers, key) != "" {
			return true
		}
	}
	return false
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

func firstHeaderFloat(headers map[string][]string, key string) *float64 {
	v := firstHeader(headers, key)
	if v == "" {
		return nil
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return nil
	}
	return float64Ptr(n)
}

func firstHeaderBool(headers map[string][]string, key string) *bool {
	v := firstHeader(headers, key)
	if v == "" {
		return nil
	}
	n, err := strconv.ParseBool(v)
	if err != nil {
		return nil
	}
	return &n
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
	return okEnvelopeJSON(`{"resources":[{"Path":"/list","Menu":"Credential Usage","Description":"List all credentials with quota details"},{"Path":"/detail","Menu":"","Description":"Get single credential quota detail"}]}`)
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

func queryProviderQuotaDetails() {
	entries := store.all()
	for _, entry := range entries {
		switch entry.Provider {
		case "antigravity", "gemini-cli":
			queryLoadCodeAssist(entry.AuthIndex)
		case "claude":
			queryClaudeUsage(entry.AuthIndex)
		}
	}
}

func queryAntigravityCredits() {
	queryProviderQuotaDetails()
}

type apiCallResponse struct {
	StatusCode int    `json:"status_code"`
	Body       string `json:"body"`
}

type claudeUsageResponse struct {
	FiveHour struct {
		Utilization float64 `json:"utilization"`
		ResetsAt    string  `json:"resets_at"`
	} `json:"five_hour"`
	SevenDay struct {
		Utilization float64 `json:"utilization"`
		ResetsAt    string  `json:"resets_at"`
	} `json:"seven_day"`
	SevenDaySonnet struct {
		Utilization float64 `json:"utilization"`
		ResetsAt    string  `json:"resets_at"`
	} `json:"seven_day_sonnet"`
}

type loadCodeAssistResponse struct {
	CloudAICompanionProject string   `json:"cloudaicompanionProject"`
	CurrentTier             tierInfo `json:"currentTier"`
	PaidTier                struct {
		ID               string                 `json:"id"`
		Name             string                 `json:"name"`
		Description      string                 `json:"description"`
		AvailableCredits []loadCodeAssistCredit `json:"availableCredits"`
	} `json:"paidTier"`
	IneligibleTiers []struct {
		Tier          tierInfo `json:"tier"`
		ReasonCode    string   `json:"reasonCode"`
		ReasonMessage string   `json:"reasonMessage"`
	} `json:"ineligibleTiers"`
	AllowedTiers []struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		IsDefault *bool  `json:"isDefault"`
	} `json:"allowedTiers"`
}

type loadCodeAssistCredit struct {
	CreditType          string        `json:"creditType"`
	CreditAmount        flexibleFloat `json:"creditAmount"`
	MinimumCreditAmount flexibleFloat `json:"minimumCreditAmountForUsage"`
}

type fetchAvailableModelsResponse struct {
	Models map[string]struct {
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
	} `json:"models"`
}

type tierInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func queryClaudeUsage(authIndex string) {
	if cfg.CPABaseURL == "" || cfg.ManagementKey == "" {
		return
	}

	apiCallPayload, _ := json.Marshal(map[string]any{
		"auth_index": authIndex,
		"method":     "GET",
		"url":        "https://api.anthropic.com/api/oauth/usage",
		"header": map[string]string{
			"Accept":         "application/json, text/plain, */*",
			"Authorization":  "Bearer $TOKEN$",
			"Content-Type":   "application/json",
			"anthropic-beta": "oauth-2025-04-20",
			"User-Agent":     "claude-code/2.1.7",
		},
	})
	bodyStr, ok := queryManagementAPICall(authIndex, apiCallPayload)
	if !ok {
		return
	}

	var usageResp claudeUsageResponse
	if err := json.Unmarshal([]byte(bodyStr), &usageResp); err != nil {
		return
	}
	updateClaudeUsageQuota(authIndex, &usageResp)
}

func queryManagementAPICall(authIndex string, apiCallPayload []byte) (string, bool) {
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
		return "", false
	}

	// Unwrap envelope
	var env envelope
	if err := json.Unmarshal(resp, &env); err != nil || !env.OK {
		callHostLog("error", fmt.Sprintf("credential-usage: host.http.do envelope error for %s: %v", authIndex, err))
		return "", false
	}

	// Unwrap HTTP response
	var httpResp struct {
		StatusCode int                 `json:"StatusCode"`
		Headers    map[string][]string `json:"Headers"`
		Body       string              `json:"Body"`
	}
	if err := json.Unmarshal(env.Result, &httpResp); err != nil {
		callHostLog("error", fmt.Sprintf("credential-usage: host.http.do parse error for %s: %v", authIndex, err))
		return "", false
	}

	if httpResp.StatusCode != 200 {
		return "", false
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
		return bodyStr, true
	}
	if apiResp.StatusCode != 200 {
		return "", false
	}
	return apiResp.Body, true
}

func updateClaudeUsageQuota(authIndex string, resp *claudeUsageResponse) {
	store.mu.Lock()
	defer store.mu.Unlock()

	entry := store.data[authIndex]
	if entry == nil {
		return
	}

	details := entry.QuotaDetails
	details.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if details.Source == "" {
		details.Source = "anthropic_usage_api"
	}
	details.Windows = upsertQuotaWindow(details.Windows, quotaWindow{
		Name:        "5h",
		Label:       "5 hour limit",
		Utilization: float64Ptr(resp.FiveHour.Utilization),
		ResetAt:     resp.FiveHour.ResetsAt,
	})
	if resp.SevenDay.ResetsAt != "" || resp.SevenDay.Utilization != 0 {
		details.Windows = upsertQuotaWindow(details.Windows, quotaWindow{
			Name:        "7d",
			Label:       "weekly limit",
			Utilization: float64Ptr(resp.SevenDay.Utilization),
			ResetAt:     resp.SevenDay.ResetsAt,
		})
	}
	if resp.SevenDaySonnet.ResetsAt != "" || resp.SevenDaySonnet.Utilization != 0 {
		details.Windows = upsertQuotaWindow(details.Windows, quotaWindow{
			Name:        "7d_sonnet",
			Label:       "weekly Sonnet limit",
			Utilization: float64Ptr(resp.SevenDaySonnet.Utilization),
			ResetAt:     resp.SevenDaySonnet.ResetsAt,
		})
	}
	entry.QuotaDetails = details
}

func upsertQuotaWindow(windows []quotaWindow, window quotaWindow) []quotaWindow {
	for i := range windows {
		if windows[i].Name == window.Name {
			windows[i] = mergeQuotaWindow(windows[i], window)
			return windows
		}
	}
	return append(windows, window)
}

func quotaWindowByName(windows []quotaWindow, name string) *quotaWindow {
	for i := range windows {
		if windows[i].Name == name {
			return &windows[i]
		}
	}
	return nil
}

func mergeQuotaWindow(existing, update quotaWindow) quotaWindow {
	merged := existing
	if update.Label != "" {
		merged.Label = update.Label
	}
	if update.Status != "" {
		merged.Status = update.Status
	}
	if update.Utilization != nil {
		merged.Utilization = update.Utilization
	}
	if update.UsedPercent != nil {
		merged.UsedPercent = update.UsedPercent
	}
	if update.SurpassedThreshold != nil {
		merged.SurpassedThreshold = update.SurpassedThreshold
	}
	if update.ResetAt != "" {
		merged.ResetAt = update.ResetAt
	}
	if update.ResetAfterSeconds != nil {
		merged.ResetAfterSeconds = update.ResetAfterSeconds
	}
	if update.WindowMinutes != nil {
		merged.WindowMinutes = update.WindowMinutes
	}
	return merged
}

func queryLoadCodeAssist(authIndex string) {
	if cfg.CPABaseURL == "" || cfg.ManagementKey == "" {
		return
	}

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
	bodyStr, ok := queryManagementAPICall(authIndex, apiCallPayload)
	if !ok {
		return
	}

	var assistResp loadCodeAssistResponse
	if err := json.Unmarshal([]byte(bodyStr), &assistResp); err != nil {
		applyAntigravityFailureBody(authIndex, bodyStr)
		return
	}
	updateAntigravityQuota(authIndex, &assistResp)
	if assistResp.CloudAICompanionProject != "" {
		queryAntigravityAvailableModels(authIndex, assistResp.CloudAICompanionProject)
	}
}

func queryAntigravityAvailableModels(authIndex, project string) {
	apiCallPayload, _ := json.Marshal(map[string]any{
		"auth_index": authIndex,
		"method":     "POST",
		"url":        "https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels",
		"header": map[string]string{
			"Authorization": "Bearer $TOKEN$",
			"Content-Type":  "application/json",
			"User-Agent":    "antigravity/1.0",
		},
		"data": fmt.Sprintf(`{"project":%q}`, project),
	})
	bodyStr, ok := queryManagementAPICall(authIndex, apiCallPayload)
	if !ok {
		return
	}
	var modelsResp fetchAvailableModelsResponse
	if err := json.Unmarshal([]byte(bodyStr), &modelsResp); err != nil {
		applyAntigravityFailureBody(authIndex, bodyStr)
		return
	}
	updateAntigravityModelQuotas(authIndex, &modelsResp)
}

func applyAntigravityFailureBody(authIndex, body string) {
	if body == "" {
		return
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	entry := store.data[authIndex]
	if entry == nil {
		return
	}
	parseAntigravity429(entry, body)
}

func updateAntigravityQuota(authIndex string, resp *loadCodeAssistResponse) {
	store.mu.Lock()
	defer store.mu.Unlock()

	entry := store.data[authIndex]
	if entry == nil {
		return
	}

	selected := selectAntigravityCredit(resp.PaidTier.AvailableCredits)
	details := entry.QuotaDetails
	details.Source = "upstream_api"
	details.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	details.Credits = buildCreditDetails(resp, selected)

	if selected != nil {
		available := selected.CreditAmount > selected.MinimumCreditAmount
		details.Available = &available
		details.Detail = fmt.Sprintf("Credits: %.2f / min: %.2f", selected.CreditAmount, selected.MinimumCreditAmount)
		if selected.CreditAmount <= selected.MinimumCreditAmount {
			entry.QuotaState.Exceeded = true
			entry.QuotaState.Reason = "insufficient_credits"
		} else {
			entry.QuotaState.Exceeded = false
			entry.QuotaState.Reason = ""
			entry.QuotaState.NextRecoverAt = nil
		}
	}
	entry.QuotaDetails = details
}

func updateAntigravityModelQuotas(authIndex string, resp *fetchAvailableModelsResponse) {
	store.mu.Lock()
	defer store.mu.Unlock()

	entry := store.data[authIndex]
	if entry == nil || resp == nil || len(resp.Models) == 0 {
		return
	}

	details := entry.QuotaDetails
	if details.Source == "" {
		details.Source = "upstream_api"
	}
	details.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if details.ModelQuotas == nil {
		details.ModelQuotas = make(map[string]modelQuota, len(resp.Models))
	}
	for modelName, model := range resp.Models {
		quota := modelQuota{
			DisplayName:        model.DisplayName,
			SupportsImages:     model.SupportsImages,
			SupportsThinking:   model.SupportsThinking,
			ThinkingBudget:     model.ThinkingBudget,
			Recommended:        model.Recommended,
			MaxTokens:          model.MaxTokens,
			MaxOutputTokens:    model.MaxOutputTokens,
			SupportedMimeTypes: cloneBoolMap(model.SupportedMimeTypes),
		}
		if model.QuotaInfo != nil {
			quota.RemainingFraction = float64Ptr(float64(model.QuotaInfo.RemainingFraction))
			quota.ResetTime = model.QuotaInfo.ResetTime
		}
		details.ModelQuotas[modelName] = quota
	}
	entry.QuotaDetails = details
}

func cloneBoolMap(input map[string]bool) map[string]bool {
	if input == nil {
		return nil
	}
	out := make(map[string]bool, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func selectAntigravityCredit(credits []loadCodeAssistCredit) *loadCodeAssistCredit {
	if len(credits) == 0 {
		return nil
	}
	for i := range credits {
		if strings.EqualFold(credits[i].CreditType, "GOOGLE_ONE_AI") {
			return &credits[i]
		}
	}
	return &credits[0]
}

func buildCreditDetails(resp *loadCodeAssistResponse, selected *loadCodeAssistCredit) *creditDetails {
	credits := &creditDetails{
		PaidTierID:              resp.PaidTier.ID,
		PaidTierName:            resp.PaidTier.Name,
		CurrentTierID:           resp.CurrentTier.ID,
		CurrentTierName:         resp.CurrentTier.Name,
		CloudAICompanionProject: resp.CloudAICompanionProject,
	}
	if selected != nil {
		credits.Amount = float64Ptr(float64(selected.CreditAmount))
		credits.MinimumForUsage = float64Ptr(float64(selected.MinimumCreditAmount))
	}
	for _, credit := range resp.PaidTier.AvailableCredits {
		credits.Items = append(credits.Items, creditItem{
			CreditType:      credit.CreditType,
			Amount:          float64Ptr(float64(credit.CreditAmount)),
			MinimumForUsage: float64Ptr(float64(credit.MinimumCreditAmount)),
		})
	}
	for _, tier := range resp.IneligibleTiers {
		credits.IneligibleTiers = append(credits.IneligibleTiers, tierProblem{
			TierID:        tier.Tier.ID,
			TierName:      tier.Tier.Name,
			ReasonCode:    tier.ReasonCode,
			ReasonMessage: tier.ReasonMessage,
		})
	}
	for _, tier := range resp.AllowedTiers {
		credits.AllowedTiers = append(credits.AllowedTiers, allowedTier{
			ID:        tier.ID,
			Name:      tier.Name,
			IsDefault: tier.IsDefault,
		})
	}
	return credits
}
