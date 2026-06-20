package main

import (
	"fmt"
	"time"
)

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
		if !available {
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
		return fmt.Sprintf("%s-%d", fallback, index)
	}
	return fmt.Sprintf("%s-%d", id, index)
}
