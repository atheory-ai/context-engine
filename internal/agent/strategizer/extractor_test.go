package strategizer

import (
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
)

// ── Test Case 1 — Standard investigative query ─────────────────────────────

func TestExtractStandardInvestigativeQuery(t *testing.T) {
	response := `Let me analyze this query. It asks about the relationship between volunteer
assignment operations and billing event creation — this is a cross-cutting
concern involving at least two subsystems.

<mode>thinking</mode>

The key entry points are the assignment operation and the billing event type.
I need to understand both the call chain from assignment to billing, and whether
there's a direct relationship or whether it's mediated by another mechanism.

<anchors>
  <anchor type="concept" id="volunteer-op" confidence="high"/>
  <anchor type="concept" id="billing-event" confidence="high"/>
  <anchor type="namespace" id="internal/volunteer" confidence="medium"/>
  <anchor type="namespace" id="internal/billing" confidence="medium"/>
</anchors>

<predicates>
  <predicate name="callgraph" value="true"/>
  <predicate name="concepts" value="true"/>
  <predicate name="crossproject" value="false"/>
</predicates>

<open_queries>
  <open_query>What function or method in the volunteer subsystem triggers billing event creation?</open_query>
  <open_query>Is billing triggered directly from assignment, or through an event/message bus?</open_query>
  <open_query>What fields on a volunteer-op determine the billing event type or amount?</open_query>
</open_queries>

<max_loops>4</max_loops>
<k_limit>30</k_limit>
<role_hint></role_hint>
<model_tier></model_tier>`

	ir, err := Extract(response)
	if err != nil {
		t.Fatalf("Extract returned unexpected error: %v", err)
	}

	if ir.Mode != core.IRModeThinking {
		t.Errorf("Mode = %q, want %q", ir.Mode, core.IRModeThinking)
	}

	wantAnchors := []core.AnchorRef{
		{Type: "concept", ID: "volunteer-op", Confidence: "high"},
		{Type: "concept", ID: "billing-event", Confidence: "high"},
		{Type: "namespace", ID: "internal/volunteer", Confidence: "medium"},
		{Type: "namespace", ID: "internal/billing", Confidence: "medium"},
	}
	if !anchorsEqual(ir.Anchors, wantAnchors) {
		t.Errorf("Anchors = %+v, want %+v", ir.Anchors, wantAnchors)
	}

	if err := ir.Validate(); err != nil {
		t.Errorf("Validate() returned unexpected error: %v", err)
	}

	// "crossproject": "false" must be dropped by Validate() — only "true" values are kept
	if _, ok := ir.Predicates["crossproject"]; ok {
		t.Error("Predicates should not contain crossproject (value was false)")
	}
	if ir.Predicates["callgraph"] != "true" {
		t.Errorf("Predicates[callgraph] = %q, want %q", ir.Predicates["callgraph"], "true")
	}
	if ir.Predicates["concepts"] != "true" {
		t.Errorf("Predicates[concepts] = %q, want %q", ir.Predicates["concepts"], "true")
	}

	wantQueries := []string{
		"What function or method in the volunteer subsystem triggers billing event creation?",
		"Is billing triggered directly from assignment, or through an event/message bus?",
		"What fields on a volunteer-op determine the billing event type or amount?",
	}
	if !stringSliceEqual(ir.OpenQueries, wantQueries) {
		t.Errorf("OpenQueries = %v, want %v", ir.OpenQueries, wantQueries)
	}

	if ir.MaxLoops != 4 {
		t.Errorf("MaxLoops = %d, want 4", ir.MaxLoops)
	}
	if ir.KLimit != 30 {
		t.Errorf("KLimit = %d, want 30", ir.KLimit)
	}
}

// ── Test Case 2 — Simple direct query ──────────────────────────────────────

func TestExtractSimpleDirectQuery(t *testing.T) {
	response := `This is a direct question about a specific symbol's return type.
No deep investigation needed.

<mode>direct</mode>

<anchors>
  <anchor type="symbol" id="internal/billing:ProcessPayment" confidence="high"/>
</anchors>

<predicates>
  <predicate name="references" value="true"/>
</predicates>

<open_queries>
  <open_query>What are the return types of ProcessPayment?</open_query>
</open_queries>

<max_loops>2</max_loops>
<k_limit>20</k_limit>
<role_hint></role_hint>
<model_tier></model_tier>`

	ir, err := Extract(response)
	if err != nil {
		t.Fatalf("Extract returned unexpected error: %v", err)
	}

	if ir.Mode != core.IRModeDirect {
		t.Errorf("Mode = %q, want %q", ir.Mode, core.IRModeDirect)
	}

	wantAnchors := []core.AnchorRef{
		{Type: "symbol", ID: "internal/billing:ProcessPayment", Confidence: "high"},
	}
	if !anchorsEqual(ir.Anchors, wantAnchors) {
		t.Errorf("Anchors = %+v, want %+v", ir.Anchors, wantAnchors)
	}

	if ir.Predicates["references"] != "true" {
		t.Errorf("Predicates[references] = %q, want %q", ir.Predicates["references"], "true")
	}

	wantQueries := []string{"What are the return types of ProcessPayment?"}
	if !stringSliceEqual(ir.OpenQueries, wantQueries) {
		t.Errorf("OpenQueries = %v, want %v", ir.OpenQueries, wantQueries)
	}

	if ir.MaxLoops != 2 {
		t.Errorf("MaxLoops = %d, want 2", ir.MaxLoops)
	}
	if ir.KLimit != 20 {
		t.Errorf("KLimit = %d, want 20", ir.KLimit)
	}

	if err := ir.Validate(); err != nil {
		t.Errorf("Validate() returned unexpected error: %v", err)
	}
}

// ── Test Case 3 — Validation failure (no anchors) ──────────────────────────

func TestExtractValidationFailureNoAnchors(t *testing.T) {
	response := `<mode>thinking</mode>
<anchors>
</anchors>
<open_queries>
  <open_query>How does billing work?</open_query>
</open_queries>`

	ir, err := Extract(response)
	if err != nil {
		t.Fatalf("Extract returned unexpected error: %v", err)
	}

	if ir.Mode != core.IRModeThinking {
		t.Errorf("Mode = %q, want %q", ir.Mode, core.IRModeThinking)
	}

	if len(ir.Anchors) != 0 {
		t.Errorf("Anchors = %+v, want empty", ir.Anchors)
	}

	wantQueries := []string{"How does billing work?"}
	if !stringSliceEqual(ir.OpenQueries, wantQueries) {
		t.Errorf("OpenQueries = %v, want %v", ir.OpenQueries, wantQueries)
	}

	// Validation must fail with ErrInvalidIR
	if err := ir.Validate(); err == nil {
		t.Error("Validate() returned nil, want error (no anchors)")
	}
}

// ── Test Case 4 — Malformed XML, extractor recovers ────────────────────────

func TestExtractMalformedXML(t *testing.T) {
	response := `Thinking about this...

<mode>thinking

<anchors>
  <anchor id="internal/graph:SubstrateReader" type="symbol" confidence=high/>
  <anchor type="namespace" id="internal/graph" confidence="medium">
</anchors>

<open_queries>
  <open_query>What does SubstrateReader expose?</open_query>
</open_queries>`

	ir, err := Extract(response)
	if err != nil {
		t.Fatalf("Extract returned unexpected error: %v", err)
	}

	// mode tag is malformed (no closing tag) → defaults to thinking
	if ir.Mode != core.IRModeThinking {
		t.Errorf("Mode = %q, want %q (default for missing close tag)", ir.Mode, core.IRModeThinking)
	}

	// First anchor: confidence=high has no quotes → attrRegex misses it → defaults to "medium"
	// Second anchor: bare > open tag → anchorRegex matches via (?:/>|>) fallback
	wantAnchors := []core.AnchorRef{
		{Type: "symbol", ID: "internal/graph:SubstrateReader", Confidence: "medium"},
		{Type: "namespace", ID: "internal/graph", Confidence: "medium"},
	}
	if !anchorsEqual(ir.Anchors, wantAnchors) {
		t.Errorf("Anchors = %+v, want %+v", ir.Anchors, wantAnchors)
	}

	wantQueries := []string{"What does SubstrateReader expose?"}
	if !stringSliceEqual(ir.OpenQueries, wantQueries) {
		t.Errorf("OpenQueries = %v, want %v", ir.OpenQueries, wantQueries)
	}

	// Validation must pass (anchors and open_query present)
	if err := ir.Validate(); err != nil {
		t.Errorf("Validate() returned unexpected error: %v", err)
	}
}

// ── Test Case 5 — Plugin predicate ─────────────────────────────────────────

func TestExtractPluginPredicate(t *testing.T) {
	response := `<mode>thinking</mode>
<anchors>
  <anchor type="namespace" id="internal/billing" confidence="high"/>
</anchors>
<predicates>
  <predicate name="callgraph" value="true"/>
  <predicate name="go-test-runner" value="true"/>
</predicates>
<open_queries>
  <open_query>Which tests cover the billing subsystem?</open_query>
</open_queries>`

	ir, err := Extract(response)
	if err != nil {
		t.Fatalf("Extract returned unexpected error: %v", err)
	}

	// Plugin-defined predicate is stored as-is
	if ir.Predicates["callgraph"] != "true" {
		t.Errorf("Predicates[callgraph] = %q, want %q", ir.Predicates["callgraph"], "true")
	}
	if ir.Predicates["go-test-runner"] != "true" {
		t.Errorf("Predicates[go-test-runner] = %q, want %q", ir.Predicates["go-test-runner"], "true")
	}

	if err := ir.Validate(); err != nil {
		t.Errorf("Validate() returned unexpected error: %v", err)
	}
}

// ── Additional edge cases ──────────────────────────────────────────────────

func TestExtractMaxLoopsClamping(t *testing.T) {
	// extractInt clamps to [min, max]: 99 → 20 (max_loops max), 200 → 100 (k_limit max)
	response := `<mode>thinking</mode>
<anchors>
  <anchor type="namespace" id="internal/billing" confidence="high"/>
</anchors>
<open_queries>
  <open_query>Question?</open_query>
</open_queries>
<max_loops>99</max_loops>
<k_limit>200</k_limit>`

	ir, err := Extract(response)
	if err != nil {
		t.Fatalf("Extract returned unexpected error: %v", err)
	}
	if err := ir.Validate(); err != nil {
		t.Fatalf("Validate() returned unexpected error: %v", err)
	}
	// extractInt clamps to the declared max bounds, not 0
	if ir.MaxLoops != 20 {
		t.Errorf("MaxLoops = %d, want 20 (clamped to extractInt max from 99)", ir.MaxLoops)
	}
	if ir.KLimit != 100 {
		t.Errorf("KLimit = %d, want 100 (clamped to extractInt max from 200)", ir.KLimit)
	}
}

func TestExtractUnknownModeDefaultsToThinking(t *testing.T) {
	response := `<mode>freeform</mode>
<anchors>
  <anchor type="namespace" id="internal/billing" confidence="high"/>
</anchors>
<open_queries>
  <open_query>Question?</open_query>
</open_queries>`

	ir, err := Extract(response)
	if err != nil {
		t.Fatalf("Extract returned unexpected error: %v", err)
	}
	if ir.Mode != core.IRModeThinking {
		t.Errorf("Mode = %q, want %q (unknown mode → thinking)", ir.Mode, core.IRModeThinking)
	}
}

func TestValidateCoercesConfidence(t *testing.T) {
	ir := &core.IR{
		Mode: core.IRModeThinking,
		Anchors: []core.AnchorRef{
			{Type: "namespace", ID: "internal/billing", Confidence: "superconfident"},
		},
		OpenQueries: []string{"Question?"},
	}
	if err := ir.Validate(); err != nil {
		t.Fatalf("Validate() returned unexpected error: %v", err)
	}
	// Bad confidence coerced to "medium"
	if ir.Anchors[0].Confidence != "medium" {
		t.Errorf("Anchors[0].Confidence = %q, want %q (coerced)", ir.Anchors[0].Confidence, "medium")
	}
}

func TestValidateDropsNonTruePredicates(t *testing.T) {
	ir := &core.IR{
		Mode: core.IRModeThinking,
		Anchors: []core.AnchorRef{
			{Type: "namespace", ID: "internal/billing", Confidence: "high"},
		},
		Predicates: map[string]string{
			"callgraph":    "true",
			"crossproject": "false",
			"summary":      "yes",
		},
		OpenQueries: []string{"Question?"},
	}
	if err := ir.Validate(); err != nil {
		t.Fatalf("Validate() returned unexpected error: %v", err)
	}
	if _, ok := ir.Predicates["crossproject"]; ok {
		t.Error("crossproject (false) should be dropped")
	}
	if _, ok := ir.Predicates["summary"]; ok {
		t.Error("summary (yes) should be dropped")
	}
	if ir.Predicates["callgraph"] != "true" {
		t.Error("callgraph (true) should be kept")
	}
}

// ── Helpers ────────────────────────────────────────────────────────────────

func anchorsEqual(a, b []core.AnchorRef) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
