package main

import (
	"fmt"
	"strings"
	"time"
)

type codexUsageResponse struct {
	PlanType                   string                      `json:"plan_type"`
	PlanTypeCamel              string                      `json:"planType"`
	RateLimit                  *codexRateLimitInfo         `json:"rate_limit"`
	RateLimitCamel             *codexRateLimitInfo         `json:"rateLimit"`
	CodeReviewRateLimit        *codexRateLimitInfo         `json:"code_review_rate_limit"`
	CodeReviewRateLimitCamel   *codexRateLimitInfo         `json:"codeReviewRateLimit"`
	AdditionalRateLimits       []codexAdditionalRateLimit  `json:"additional_rate_limits"`
	AdditionalRateLimitsCamel  []codexAdditionalRateLimit  `json:"additionalRateLimits"`
	RateLimitResetCredits      *codexRateLimitResetCredits `json:"rate_limit_reset_credits"`
	RateLimitResetCreditsCamel *codexRateLimitResetCredits `json:"rateLimitResetCredits"`
}

type codexRateLimitInfo struct {
	Allowed           *bool             `json:"allowed"`
	LimitReached      *bool             `json:"limit_reached"`
	LimitReachedCamel *bool             `json:"limitReached"`
	PrimaryWindow     *codexUsageWindow `json:"primary_window"`
	PrimaryWindowCamel *codexUsageWindow `json:"primaryWindow"`
	SecondaryWindow   *codexUsageWindow `json:"secondary_window"`
	SecondaryWindowCamel *codexUsageWindow `json:"secondaryWindow"`
}

type codexUsageWindow struct {
	UsedPercent             *flexibleFloat `json:"used_percent"`
	UsedPercentCamel        *flexibleFloat `json:"usedPercent"`
	LimitWindowSeconds      *int64         `json:"limit_window_seconds"`
	LimitWindowSecondsCamel *int64         `json:"limitWindowSeconds"`
	ResetAfterSeconds       *int64         `json:"reset_after_seconds"`
	ResetAfterSecondsCamel  *int64         `json:"resetAfterSeconds"`
	ResetAt                 string         `json:"reset_at"`
	ResetAtCamel            string         `json:"resetAt"`
}

type codexAdditionalRateLimit struct {
	LimitName          string              `json:"limit_name"`
	LimitNameCamel     string              `json:"limitName"`
	MeteredFeature     string              `json:"metered_feature"`
	MeteredFeatureCamel string             `json:"meteredFeature"`
	RateLimit          *codexRateLimitInfo `json:"rate_limit"`
	RateLimitCamel     *codexRateLimitInfo `json:"rateLimit"`
}

type codexRateLimitResetCredits struct {
	AvailableCount     *int64 `json:"available_count"`
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
	for _, additional := range firstCodexAdditionalRateLimits(resp.AdditionalRateLimits, resp.AdditionalRateLimitsCamel) {
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

func firstCodexAdditionalRateLimits(primary, camel []codexAdditionalRateLimit) []codexAdditionalRateLimit {
	if len(primary) > 0 {
		return primary
	}
	return camel
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
