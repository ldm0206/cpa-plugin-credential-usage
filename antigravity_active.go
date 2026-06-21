package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

type antigravityQuotaSummaryResponse struct {
	Groups []antigravityQuotaSummaryGroup `json:"groups"`
}

type antigravityQuotaSummaryGroup struct {
	Label            string                          `json:"label"`
	DisplayName      string                          `json:"displayName"`
	DisplayNameSnake string                          `json:"display_name"`
	Description      string                          `json:"description"`
	Buckets          []antigravityQuotaSummaryBucket `json:"buckets"`
}

type antigravityQuotaSummaryBucket struct {
	ID                     string         `json:"bucketId"`
	IDSnake                string         `json:"bucket_id"`
	Label                  string         `json:"label"`
	DisplayName            string         `json:"displayName"`
	DisplayNameSnake       string         `json:"display_name"`
	Description            string         `json:"description"`
	Window                 string         `json:"window"`
	RemainingFraction      *flexibleFloat `json:"remainingFraction"`
	RemainingFractionSnake *flexibleFloat `json:"remaining_fraction"`
	ResetTime              string         `json:"resetTime"`
	ResetTimeSnake         string         `json:"reset_time"`
}

type antigravitySubscriptionResponse = loadCodeAssistResponse

func updateAntigravityQuotaGroups(authIndex string, resp *antigravityQuotaSummaryResponse) {
	if resp == nil {
		return
	}
	groups := make([]quotaGroup, 0, len(resp.Groups))
	for groupIndex, group := range resp.Groups {
		label := firstNonEmptyStringValue(group.DisplayName, group.DisplayNameSnake, group.Label)
		if label == "" {
			label = fmt.Sprintf("Quota Group %d", groupIndex+1)
		}
		outGroup := quotaGroup{
			ID:          stablePanelQuotaID(label, fmt.Sprintf("quota-group-%d", groupIndex+1)),
			Label:       label,
			Description: group.Description,
		}
		for bucketIndex, bucket := range group.Buckets {
			remaining := firstFlexibleFloat(bucket.RemainingFraction, bucket.RemainingFractionSnake)
			if remaining == nil {
				continue
			}
			window := firstNonEmptyStringValue(bucket.Window)
			rawID := firstNonEmptyStringValue(bucket.ID, bucket.IDSnake)
			if rawID == "" {
				if window != "" {
					rawID = outGroup.ID + "-" + window
				} else {
					rawID = fmt.Sprintf("%s-bucket-%d", outGroup.ID, bucketIndex+1)
				}
			}
			label := firstNonEmptyStringValue(bucket.DisplayName, bucket.DisplayNameSnake, bucket.Label, rawID)
			outGroup.Buckets = append(outGroup.Buckets, quotaBucket{
				ID:                rawID,
				Label:             label,
				Description:       bucket.Description,
				Window:            window,
				RemainingFraction: flexibleFloatToFloat64Ptr(remaining),
				ResetTime:         firstNonEmptyStringValue(bucket.ResetTime, bucket.ResetTimeSnake),
			})
		}
		if len(outGroup.Buckets) > 0 {
			sort.SliceStable(outGroup.Buckets, func(i, j int) bool {
				left := antigravityBucketWindowOrder(outGroup.Buckets[i].Window)
				right := antigravityBucketWindowOrder(outGroup.Buckets[j].Window)
				if left != right {
					return left < right
				}
				return strings.Compare(outGroup.Buckets[i].Label, outGroup.Buckets[j].Label) < 0
			})
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

func stablePanelQuotaID(label, fallback string) string {
	id := sanitizePanelQuotaID(label)
	if id == "" {
		return fallback
	}
	return id
}

func sanitizePanelQuotaID(value string) string {
	var builder strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func antigravityBucketWindowOrder(window string) int {
	switch strings.ToLower(strings.TrimSpace(window)) {
	case "5h", "five-hour", "five_hour":
		return 0
	case "weekly", "week":
		return 1
	default:
		return int(^uint(0) >> 1)
	}
}
