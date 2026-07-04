package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const validIntentJSON = `{
	"kind": "FunctionIntent",
	"name": "validateDonationAmount",
	"language": "typescript",
	"inputs": [{"name": "amount", "type": "Money"}],
	"returns": {"type": "ValidationResult<Money>"},
	"sideEffects": []
}`

func postJSON(t *testing.T, h http.HandlerFunc, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func TestIIRGenerate_ReturnsSource(t *testing.T) {
	rec := postJSON(t, IIRGenerate(), `{"intent": `+validIntentJSON+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Source string `json:"source"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(resp.Source, "function validateDonationAmount(") {
		t.Errorf("unexpected source:\n%s", resp.Source)
	}
}

func TestIIRVerify_RoundTripsGeneratedSource(t *testing.T) {
	// Generate, then verify the generated source against the same intent.
	genRec := postJSON(t, IIRGenerate(), `{"intent": `+validIntentJSON+`}`)
	var gen struct {
		Source string `json:"source"`
	}
	json.Unmarshal(genRec.Body.Bytes(), &gen) //nolint:errcheck

	body, _ := json.Marshal(map[string]any{
		"intent": json.RawMessage(validIntentJSON),
		"source": gen.Source,
	})
	rec := postJSON(t, IIRVerify(), string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var report struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report.Status != "passed" {
		t.Errorf("round-trip status = %q, want passed\nbody: %s", report.Status, rec.Body.String())
	}
}

func TestIIRGenTests_ReturnsArtifact(t *testing.T) {
	rec := postJSON(t, IIRGenTests(), `{"intent": `+validIntentJSON+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var artifact struct {
		Source   string          `json:"source"`
		Coverage json.RawMessage `json:"coverage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &artifact); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(artifact.Source, "describe(") {
		t.Errorf("expected a test suite, got:\n%s", artifact.Source)
	}
}

func TestIIRHandlers_BadRequests(t *testing.T) {
	cases := []struct {
		name string
		h    http.HandlerFunc
		body string
	}{
		{"malformed json", IIRGenerate(), `{not json`},
		{"invalid intent", IIRGenerate(), `{"intent": {"kind": "FunctionIntent"}}`}, // missing name/language
		{"empty body", IIRVerify(), ``},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := postJSON(t, c.h, c.body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
			}
		})
	}
}
