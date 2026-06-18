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
	if registration.Resources[0].Path != "/" {
		t.Fatalf("first resource path = %q, want /", registration.Resources[0].Path)
	}
	if registration.Resources[1].Path != "/:auth_index" {
		t.Fatalf("second resource path = %q, want /:auth_index", registration.Resources[1].Path)
	}
}
