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

func TestMergeAuthFileEntryCopiesRuntimeUsageCounts(t *testing.T) {
	resetTestStore()

	mergeAuthFileEntry("7", hostAuthFileEntry{
		ID:        "auth-7",
		AuthIndex: "7",
		Provider:  "antigravity",
		Status:    "active",
		Success:   3,
		Failed:    2,
	})

	entry := store.getByIndex("7")
	if entry == nil {
		t.Fatal("entry is nil, want credential entry")
	}
	if entry.UsageSummary.TotalRequests != 5 {
		t.Fatalf("total_requests = %d, want 5", entry.UsageSummary.TotalRequests)
	}
	if entry.UsageSummary.SuccessRequests != 3 {
		t.Fatalf("success_requests = %d, want 3", entry.UsageSummary.SuccessRequests)
	}
	if entry.UsageSummary.FailedRequests != 2 {
		t.Fatalf("failed_requests = %d, want 2", entry.UsageSummary.FailedRequests)
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
