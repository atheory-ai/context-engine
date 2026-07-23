package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/atheory-ai/context-engine/internal/config"
	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/runner"
)

// defaultRules is the rule-pack provider used by most handler tests.
func defaultRules() iir.RulePack { return iir.DefaultRulePack() }

// iirExtractor builds a plugin-backed extractor for the verify handler tests,
// skipping when the default plugins aren't built.
func iirExtractor(t *testing.T) iir.Extractor {
	t.Helper()
	dir, err := os.MkdirTemp("", "ce-iir-*")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.DataDir = dir
	ch := core.NewAppChannels()
	ext, _, err := runner.NewIIRExtractor(context.Background(), cfg, &ch)
	if err != nil {
		t.Skipf("iir extractor unavailable: %v", err)
	}
	res, err := ext.Extract(context.Background(), iir.ExtractionInput{
		Language: "typescript", Source: []byte("export function probe(): void {}"), Target: "probe",
	})
	if err != nil || res.Function == nil {
		t.Skip("default plugins not built — run `make bundle-default-plugins`")
	}
	return ext
}

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
	ext := iirExtractor(t)
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
	rec := postJSON(t, IIRVerify(ext, defaultRules), string(body))
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

func TestIIRVerify_ChangedFailureModeReportsFailed(t *testing.T) {
	ext := iirExtractor(t)
	intent := `{
  "kind": "FunctionIntent",
  "name": "f",
  "language": "typescript",
  "returns": {"type": "void"},
  "sideEffects": [],
  "failureModes": ["invalid_entity_key"]
}`
	body, _ := json.Marshal(map[string]any{
		"intent": json.RawMessage(intent),
		"source": `export function f(): void { throw new Error("entity_not_found"); }`,
	})
	rec := postJSON(t, IIRVerify(ext, defaultRules), string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var report struct {
		Status     string `json:"status"`
		Mismatches []struct {
			Kind     string `json:"kind"`
			Severity string `json:"severity"`
		} `json:"mismatches"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report.Status != "failed" {
		t.Fatalf("status = %q, want failed; body: %s", report.Status, rec.Body.String())
	}
	for _, mismatch := range report.Mismatches {
		if mismatch.Kind == "changed_failure_mode" && mismatch.Severity == "error" {
			return
		}
	}
	t.Errorf("expected error-severity changed_failure_mode mismatch, got: %s", rec.Body.String())
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

func TestIIRHandlers_OversizedBodyRejected(t *testing.T) {
	// A body over the cap is rejected with 413 before parsing.
	huge := `{"intent":{"kind":"FunctionIntent","name":"f","language":"typescript","junk":"` +
		strings.Repeat("a", maxIIRBodyBytes+1) + `"}}`
	rec := postJSON(t, IIRGenerate(), huge)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
}

func TestIIRVerify_UsesProvidedRulePack(t *testing.T) {
	ext := iirExtractor(t)
	// A provider returning a pack with a uniquely-named rule proves the handler
	// evaluates the supplied pack (plugin-merged) rather than only the defaults.
	withPluginRule := func() iir.RulePack {
		return iir.RulePack{Rules: []iir.Rule{{
			ID: "team-plugin-rule", Target: iir.KindFunctionIntent, Severity: iir.SeverityWarning,
		}}}
	}
	body, _ := json.Marshal(map[string]any{
		"intent": json.RawMessage(validIntentJSON),
		"source": `export function validateDonationAmount(amount: Money, campaign: Campaign): ValidationResult<Money> { return ok(amount); }`,
	})
	rec := postJSON(t, IIRVerify(ext, withPluginRule), string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"team-plugin-rule"`) {
		t.Errorf("report should reflect the provided rule pack, got: %s", rec.Body.String())
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
		{"empty body", IIRVerify(nil, defaultRules), ``},
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
