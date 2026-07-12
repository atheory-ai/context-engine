package iir

import (
	"strings"
	"testing"
)

// Pure code-generation tests (no source extraction). The generate→extract→verify
// round-trip tests live in internal/runner (they need the plugin lift).

func TestRenderTSCondition(t *testing.T) {
	cases := []struct {
		name string
		expr *Expr
		want string
		ok   bool
	}{
		{"null check", bin("===", path("id"), lit("null")), "id === null", true},
		{"nil alias to null", bin("===", path("id"), lit("nil")), "id === null", true},
		{"comparison", bin("<", path("amount.cents"), path("min.cents")), "amount.cents < min.cents", true},
		{"strict inequality string", bin("!==", path("name"), lit(`"hi"`)), `name !== "hi"`, true},
		{"logical with grouping", bin("&&", bin(">", path("a"), lit("0")), bin(">", path("b"), lit("0"))), "(a > 0) && (b > 0)", true},
		{"negation of compound", &Expr{Op: "!", Args: []*Expr{bin("===", path("x"), lit("null"))}}, "!(x === null)", true},
		{"negation of path", &Expr{Op: "!", Args: []*Expr{path("ready")}}, "!ready", true},
		{"nil expr", nil, "", false},
		{"unknown op", &Expr{Op: "??", Args: []*Expr{path("a"), path("b")}}, "", false},
		{"unsafe path rejected", path("a; drop"), "", false},
		{"unknown literal rejected", lit("__weird__"), "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := renderTSCondition(tc.expr)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (got %q)", ok, tc.ok, got)
			}
			if ok && got != tc.want {
				t.Errorf("rendered %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGenerate_Deterministic(t *testing.T) {
	intent := mustLoad(t, `
kind: FunctionIntent
name: f
language: typescript
inputs:
  - name: a
    type: number
returns:
  type: void
sideEffects:
  - analytics.track
  - db.save
`)
	first, err := GenerateFunction(intent)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		again, err := GenerateFunction(intent)
		if err != nil {
			t.Fatal(err)
		}
		if again != first {
			t.Fatalf("generation not deterministic:\n%s\n---\n%s", first, again)
		}
	}
}

func TestGenerate_RejectsNonFunctionIntent(t *testing.T) {
	if _, err := GenerateFunction(nil); err == nil {
		t.Error("expected error for nil intent")
	}
	if _, err := GenerateFunction(&FunctionIntent{Kind: "Other", Name: "x"}); err == nil {
		t.Error("expected error for unsupported kind")
	}
}

func TestGenerate_RejectsInvalidName(t *testing.T) {
	for _, bad := range []string{"has space", "a;b()", "1abc", "a\nb", ""} {
		intent := &FunctionIntent{Kind: KindFunctionIntent, Name: bad, Language: "typescript"}
		if _, err := GenerateFunction(intent); err == nil {
			t.Errorf("expected error for invalid name %q", bad)
		}
	}
}

func TestBuiltinEmitter_SupportsAndEmits(t *testing.T) {
	intent := &FunctionIntent{Kind: KindFunctionIntent, Language: "typescript", Name: "f"}
	em := BuiltinEmitter()
	if !em.Supports(intent) {
		t.Error("expected support for a TypeScript FunctionIntent")
	}

	resolved, ok := DefaultRegistry().EmitterFor(intent)
	if !ok {
		t.Fatal("registry should resolve the built-in emitter")
	}
	src, err := resolved.Emit(intent)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if !strings.Contains(src, "function f(") {
		t.Errorf("emitted source missing function f:\n%s", src)
	}
}
