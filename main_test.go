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

func callManagementHandleForTest(t *testing.T, path string) managementResponseForTest {
	t.Helper()
	request, err := json.Marshal(managementRequest{Method: "GET", Path: path})
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

	resp := callManagementHandleForTest(t, "/v0/resource/plugins/credential-usage/")
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

	resp := callManagementHandleForTest(t, "/v0/resource/plugins/credential-usage/2")
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
	if registration.Resources[0].Path != "/" {
		t.Fatalf("first resource path = %q, want /", registration.Resources[0].Path)
	}
	if registration.Resources[1].Path != "/:auth_index" {
		t.Fatalf("second resource path = %q, want /:auth_index", registration.Resources[1].Path)
	}
}
