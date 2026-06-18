# Credential Usage Resource Route Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose credential usage data through CPA's existing unauthenticated plugin resource route mechanism.

**Architecture:** Keep the existing usage collection and in-memory credential store. Change only the HTTP exposure layer so `management.register` advertises CPA `resources`, and normalize CPA resource paths before routing list/detail requests.

**Tech Stack:** Go 1.26, cgo shared-library plugin ABI, standard library `testing`, JSON/base64 envelopes, CPA plugin `ManagementAPI` resource route protocol.

## Global Constraints

- Base plugin behavior on the checked CPA source code/protocol; verify plugin capabilities and route exposure against CPA before implementing.
- Implement plugin functionality only within this plugin repository; do not modify CPA main program source code for plugin features.
- CPA resource plugin paths are exposed under `/v0/resource/plugins/<pluginID>/...`.
- Do not document `/v0/management/credential-usage` as the public endpoint.
- Do not document `/v0/credential-usage` because CPA cannot expose that path from plugin code alone.

---

## File Structure

- Modify `main.go`: change resource registration, normalize incoming resource paths, and keep existing list/detail JSON response behavior.
- Create `main_test.go`: package-level unit tests for registration and resource handler behavior.
- Modify `README.md`: replace Management API endpoint docs with CPA resource endpoint docs.

---

### Task 1: Register CPA Resource Routes

**Files:**
- Modify: `main.go`
- Create: `main_test.go`

**Interfaces:**
- Consumes: existing `handleMethod(method string, request []byte) ([]byte, error)`.
- Produces: `management.register` envelope whose `result.resources` contains resource route declarations.

- [ ] **Step 1: Write the failing tests**

Create `main_test.go` with:

```go
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```bash
go test ./... -run TestManagementRegisterReturnsResourceRoutes -count=1
```

Expected: FAIL because current `management.register` returns `routes` instead of `resources`.

- [ ] **Step 3: Implement minimal registration change**

In `main.go`, replace `handleManagementRegister` with:

```go
func handleManagementRegister() ([]byte, error) {
	return okEnvelopeJSON(`{"resources":[{"Path":"/list","Menu":"Credential Usage","Description":"List all credentials with quota and usage data"},{"Path":"/detail","Menu":"","Description":"Get single credential quota and usage detail"}]}`)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run:

```bash
go test ./... -run TestManagementRegisterReturnsResourceRoutes -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add main.go main_test.go
git commit -m "fix: register credential usage resource routes"
```

---

### Task 2: Normalize CPA Resource Paths

**Files:**
- Modify: `main.go`
- Modify: `main_test.go`

**Interfaces:**
- Consumes: existing `managementRequest`, `managementJSONResponse`, `store`, and `credentialEntry`.
- Produces: `normalizeResourcePath(path string) string`, used by `handleManagementHandle` before routing.

- [ ] **Step 1: Write failing tests for list/detail resource paths**

Append to `main_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./... -run 'TestResource(List|Detail)PathReturnsCredential' -count=1
```

Expected: FAIL because `handleManagementHandle` only recognizes `/credential-usage` paths.

- [ ] **Step 3: Implement path normalization**

In `main.go`, add this helper near the management handlers:

```go
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
```

Then in `handleManagementHandle`, replace:

```go
path := req.Path
```

with:

```go
path := normalizeResourcePath(req.Path)
```

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
go test ./... -run 'TestResource(List|Detail)PathReturnsCredential' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add main.go main_test.go
git commit -m "fix: handle credential usage resource paths"
```

---

### Task 3: Preserve 404 Behavior for Unknown and Missing Resources

**Files:**
- Modify: `main_test.go`

**Interfaces:**
- Consumes: `callManagementHandleForTest(t, path string) managementResponseForTest` from Task 2.
- Produces: regression coverage for existing 404 behavior through resource paths.

- [ ] **Step 1: Write failing or regression tests**

Append to `main_test.go`:

```go
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
```

- [ ] **Step 2: Run tests**

Run:

```bash
go test ./... -run 'Test(ResourceMissingCredential|UnknownResourcePath)' -count=1
```

Expected: PASS if Task 2 normalization is correct. If `TestUnknownResourcePathReturns404` fails, ensure `normalizeResourcePath` only matches the exact base path or the base path followed by `/`.

- [ ] **Step 3: Run the full test suite**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add main_test.go
git commit -m "test: cover credential usage resource 404s"
```

---

### Task 4: Update README for Resource Route Usage

**Files:**
- Modify: `README.md`

**Interfaces:**
- Consumes: endpoint behavior from Tasks 1-3.
- Produces: user-facing documentation for Docker/CPA deployment and curl usage without Management API key.

- [ ] **Step 1: Update README endpoint section**

Replace the current "Management API Endpoints" section with:

```markdown
## Resource Endpoints

CPA exposes this plugin through its plugin resource route mechanism. These endpoints do not use the Management API path and do not require the Management API key.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/v0/resource/plugins/credential-usage/list` | List all credentials with quota/usage data |
| GET | `/v0/resource/plugins/credential-usage/detail?auth_index=<auth_index>` | Single credential detail |

Query parameters:
- `provider` (optional): Filter by provider name

Example:

```bash
curl http://127.0.0.1:8317/v0/resource/plugins/credential-usage/list
```
```

- [ ] **Step 2: Update the first description line**

Change:

```markdown
A CPA plugin that exposes credential quota and usage data through the Management API.
```

to:

```markdown
A CPA plugin that exposes credential quota and usage data through CPA plugin resource routes.
```

- [ ] **Step 3: Update configuration wording**

Keep the existing plugin config block. Do not tell users to call `/v0/management/credential-usage` or pass a Management API key for the resource endpoint.

- [ ] **Step 4: Verify docs do not mention the wrong endpoint**

Run:

```bash
grep -R "v0/management/credential-usage\|v0/credential-usage" README.md docs/superpowers/specs docs/superpowers/plans
```

Expected: no matches, except historical design text in the spec/plan that explicitly says not to document or rely on those paths. If matches appear in README, remove them.

- [ ] **Step 5: Run full verification**

Run:

```bash
go test ./...
go vet ./...
```

Expected: both commands PASS.

- [ ] **Step 6: Commit**

```bash
git add README.md
git commit -m "docs: document credential usage resource endpoint"
```

---

## Self-Review

Spec coverage:

- Uses CPA resource route mechanism: Task 1.
- Does not modify CPA main program source: all tasks touch only plugin repository files.
- Normalizes `/v0/resource/plugins/credential-usage/...`: Task 2.
- JSON error handling and 404 behavior: Task 3.
- README updates: Task 4.
- Verification with `go test ./...`: Tasks 3 and 4.

Placeholder scan:

- No TBD/TODO/FIXME placeholders.
- Each task has concrete file paths, code, commands, expected outputs, and commit commands.

Type consistency:

- Tests use existing package `main` symbols directly.
- `managementResponseForTest` mirrors current JSON field names emitted by `managementJSONResponse`.
- `normalizeResourcePath(path string) string` is defined before use in Task 2.
