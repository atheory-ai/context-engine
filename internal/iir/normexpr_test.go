package iir

import (
	"encoding/json"
	"strings"
	"testing"
)

// Expr builders keep the expected trees readable.
func path(s string) *Expr             { return &Expr{Op: "path", Text: s} }
func lit(s string) *Expr              { return &Expr{Op: "lit", Text: s} }
func bin(op string, l, r *Expr) *Expr { return &Expr{Op: op, Args: []*Expr{l, r}} }

// whenExpr extracts the single behavior clause's normalized condition, failing
// if the source did not yield exactly one clause.
func whenExpr(t *testing.T, src string) *Expr {
	t.Helper()
	got := extract(t, src, "f")
	if len(got.Behavior) != 1 {
		t.Fatalf("expected exactly one behavior clause, got %+v", got.Behavior)
	}
	return got.Behavior[0].WhenExpr
}

func TestNormalize_ComparisonOfPaths(t *testing.T) {
	// The donations.ts fixture condition, in miniature.
	src := `export function f(amount: any, campaign: any): any {
  if (amount.cents < campaign.minimumDonation.cents) { return err("below"); }
  return ok(amount);
}`
	got := whenExpr(t, src)
	want := bin("<", path("amount.cents"), path("campaign.minimumDonation.cents"))
	if !got.Equal(want) {
		t.Errorf("whenExpr = %+v, want %+v", got, want)
	}
}

func TestNormalize_LogicalConnective(t *testing.T) {
	src := `export function f(a: number, b: number): number {
  if (a > 0 && b > 0) { return 1; }
  return 0;
}`
	got := whenExpr(t, src)
	want := bin("&&", bin(">", path("a"), lit("0")), bin(">", path("b"), lit("0")))
	if !got.Equal(want) {
		t.Errorf("whenExpr = %+v, want %+v", got, want)
	}
}

func TestNormalize_Negation(t *testing.T) {
	src := `export function f(ready: boolean): number {
  if (!ready) { return 0; }
  return 1;
}`
	got := whenExpr(t, src)
	want := &Expr{Op: "!", Args: []*Expr{path("ready")}}
	if !got.Equal(want) {
		t.Errorf("whenExpr = %+v, want %+v", got, want)
	}
}

func TestNormalize_Literals(t *testing.T) {
	cases := map[string]*Expr{
		`x === 0`:       bin("===", path("x"), lit("0")),
		`name !== "hi"`: bin("!==", path("name"), lit(`"hi"`)),
		`flag === true`: bin("===", path("flag"), lit("true")),
		`v === null`:    bin("===", path("v"), lit("null")),
		`a < -1`:        bin("<", path("a"), lit("-1")), // unary-minus folded into the literal
	}
	for cond, want := range cases {
		src := "export function f(): number {\n  if (" + cond + ") { return 1; }\n  return 0;\n}"
		got := whenExpr(t, src)
		if !got.Equal(want) {
			t.Errorf("cond %q: whenExpr = %+v, want %+v", cond, got, want)
		}
	}
}

func TestNormalize_ParensUnwrapped(t *testing.T) {
	src := `export function f(a: number): number {
  if (((a < 1))) { return 0; }
  return 1;
}`
	got := whenExpr(t, src)
	want := bin("<", path("a"), lit("1"))
	if !got.Equal(want) {
		t.Errorf("whenExpr = %+v, want %+v", got, want)
	}
}

// Conditions outside the v1 grammar yield no structured form; the raw string is
// unaffected so the clause is otherwise identical to today.
func TestNormalize_OutOfGrammarIsNil(t *testing.T) {
	cases := map[string]string{
		"call":            `isValid(x)`,
		"computed-access": `arr[0] < 1`,
		"arithmetic":      `a + b > 0`,
		"ternary-operand": `(a ? b : c) < 1`,
	}
	for name, cond := range cases {
		src := "export function f(): number {\n  if (" + cond + ") { return 1; }\n  return 0;\n}"
		got := extract(t, src, "f")
		if len(got.Behavior) != 1 {
			t.Fatalf("%s: expected one clause, got %+v", name, got.Behavior)
		}
		if got.Behavior[0].WhenExpr != nil {
			t.Errorf("%s: expected nil WhenExpr, got %+v", name, got.Behavior[0].WhenExpr)
		}
		if got.Behavior[0].When == "" {
			t.Errorf("%s: raw When should still be populated", name)
		}
	}
}

func TestNormalize_Deterministic(t *testing.T) {
	src := `export function f(a: number, b: number): number {
  if (a < b && b < 10) { return 1; }
  return 0;
}`
	first := whenExpr(t, src)
	second := whenExpr(t, src)
	if !first.Equal(second) {
		t.Errorf("normalization not deterministic: %+v vs %+v", first, second)
	}
}

// --- comparison ---

// behaviorIntent returns a matching pair carrying one structured behavior clause
// with the given condition.
func behaviorIntent(when string, expr *Expr) *FunctionIntent {
	f := baseIntent()
	f.Behavior = []BehaviorClause{{When: when, Then: "return 0", WhenExpr: expr}}
	return f
}

func TestCompare_BehaviorContentMismatch(t *testing.T) {
	intended := behaviorIntent("a < 1", bin("<", path("a"), lit("1")))
	extracted := behaviorIntent("a > 1", bin(">", path("a"), lit("1")))
	_, mismatches := Compare(intended, extracted)
	m := findMismatch(mismatches, MismatchBehaviorContent)
	if m == nil {
		t.Fatalf("expected a mismatched_behavior finding, got %+v", mismatches)
	}
	if m.Severity != SeverityWarning {
		t.Errorf("severity = %q, want warning", m.Severity)
	}
	// The count-only path must NOT also report a spurious match.
	if findMismatch(mismatches, MismatchMissingBehavior) != nil {
		t.Error("content mismatch should not coexist with a count mismatch")
	}
}

func TestCompare_BehaviorContentMatches(t *testing.T) {
	intended := behaviorIntent("a < 1", bin("<", path("a"), lit("1")))
	extracted := behaviorIntent("a<1", bin("<", path("a"), lit("1"))) // whitespace differs, structure same
	matches, mismatches := Compare(intended, extracted)
	if findMismatch(mismatches, MismatchBehaviorContent) != nil {
		t.Errorf("structurally-equal conditions should not mismatch: %+v", mismatches)
	}
	found := false
	for _, mt := range matches {
		if mt.Path == "FunctionIntent.behavior" {
			found = true
		}
	}
	if !found {
		t.Error("expected a behavior match")
	}
}

// When either side lacks a WhenExpr, comparison falls back to the count-based
// behavior exactly — no regression, even if the raw strings differ.
func TestCompare_BehaviorFallsBackWhenUnstructured(t *testing.T) {
	intended := behaviorIntent("a < 1", bin("<", path("a"), lit("1")))
	extracted := behaviorIntent("something opaque", nil) // no structured form
	_, mismatches := Compare(intended, extracted)
	if findMismatch(mismatches, MismatchBehaviorContent) != nil {
		t.Errorf("unstructured side must not produce a content mismatch: %+v", mismatches)
	}
}

// --- serialization ---

func TestBehaviorClause_JSONOmitsWhenExprWhenAbsent(t *testing.T) {
	b, err := json.Marshal(BehaviorClause{When: "x < 1", Then: "return 0"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "whenExpr") {
		t.Errorf("absent WhenExpr must be omitted, got %s", b)
	}
}

func TestBehaviorClause_JSONRoundTrip(t *testing.T) {
	orig := BehaviorClause{When: "a < 1", Then: "return 0", WhenExpr: bin("<", path("a"), lit("1"))}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "whenExpr") {
		t.Errorf("present WhenExpr must serialize, got %s", b)
	}
	var back BehaviorClause
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if !back.WhenExpr.Equal(orig.WhenExpr) {
		t.Errorf("round-trip changed WhenExpr: %+v", back.WhenExpr)
	}
}
