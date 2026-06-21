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
	OrganizationType   string `json:"organization_type"`
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
	organizationType := firstNonEmptyStringValue(resp.Organization.OrganizationType, resp.Organization.Type)
	if strings.EqualFold(strings.TrimSpace(organizationType), "claude_team") && strings.EqualFold(strings.TrimSpace(resp.Organization.SubscriptionStatus), "active") {
		return "plan_team"
	}
	if resp.Account.HasClaudeMax != nil && !*resp.Account.HasClaudeMax && resp.Account.HasClaudePro != nil && !*resp.Account.HasClaudePro {
		return "plan_free"
	}
	return ""
}
