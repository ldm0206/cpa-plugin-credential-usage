package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	codexUsageURL             = "https://chatgpt.com/backend-api/wham/usage"
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
