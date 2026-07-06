package iir

import (
	"context"
	"reflect"
	"strings"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	ts "github.com/smacker/go-tree-sitter/typescript/typescript"
)

func extract(t *testing.T, src, name string) *FunctionIntent {
	t.Helper()
	intent, err := ExtractFunction(context.Background(), []byte(src), name)
	if err != nil {
		t.Fatalf("ExtractFunction: %v", err)
	}
	return intent
}

func TestExtract_BasicContract(t *testing.T) {
	src := `
export function validateDonationAmount(amount: Money, campaign: Campaign): ValidationResult<Money> {
  return ok(amount);
}
`
	got := extract(t, src, "validateDonationAmount")

	if got.Name != "validateDonationAmount" {
		t.Errorf("name = %q", got.Name)
	}
	if got.Visibility != VisibilityPublic {
		t.Errorf("visibility = %q, want public (exported)", got.Visibility)
	}
	wantInputs := []Param{{Name: "amount", Type: "Money"}, {Name: "campaign", Type: "Campaign"}}
	if !reflect.DeepEqual(got.Inputs, wantInputs) {
		t.Errorf("inputs = %+v, want %+v", got.Inputs, wantInputs)
	}
	if !got.Returns.Explicit || got.Returns.Type != "ValidationResult<Money>" {
		t.Errorf("returns = %+v, want explicit ValidationResult<Money>", got.Returns)
	}
}

func TestExtract_NonExportedIsPrivate(t *testing.T) {
	got := extract(t, `function helper(x: number): number { return x; }`, "helper")
	if got.Visibility != VisibilityPrivate {
		t.Errorf("visibility = %q, want private", got.Visibility)
	}
}

func TestExtract_MissingReturnTypeIsAbsent(t *testing.T) {
	got := extract(t, `export function f(x: number) { return x; }`, "f")
	if got.Returns.Explicit {
		t.Errorf("expected absent return type, got %+v", got.Returns)
	}
}

func TestExtract_MissingParamTypeIsUnknown(t *testing.T) {
	got := extract(t, `export function f(a, b: string): void {}`, "f")
	if got.Inputs[0].Type != TypeUnknown {
		t.Errorf("param a type = %q, want %q", got.Inputs[0].Type, TypeUnknown)
	}
	if got.Inputs[1].Type != "string" {
		t.Errorf("param b type = %q, want string", got.Inputs[1].Type)
	}
}

func TestExtract_SideEffectFromImportedClient(t *testing.T) {
	src := `
import { analytics } from "./analytics";
import { ok, err } from "./result";
export function f(): void {
  analytics.track("x");
  ok(1);
  err("e");
}
`
	got := extract(t, src, "f")
	// analytics.track: imported client member call → side effect.
	// ok/err: bare imported helper calls → NOT side effects.
	want := []string{"analytics.track"}
	if !reflect.DeepEqual(got.SideEffects, want) {
		t.Errorf("sideEffects = %v, want %v", got.SideEffects, want)
	}
}

func TestExtract_SideEffectFromVerbName(t *testing.T) {
	src := `export function f(): void { saveRecord(); computeTotal(); }`
	got := extract(t, src, "f")
	// saveRecord contains the "save" verb; computeTotal matches no verb.
	want := []string{"saveRecord"}
	if !reflect.DeepEqual(got.SideEffects, want) {
		t.Errorf("sideEffects = %v, want %v", got.SideEffects, want)
	}
}

func TestExtract_NoSideEffectsIsEmptyNotNil(t *testing.T) {
	got := extract(t, `export function f(): void { const x = 1; }`, "f")
	if got.SideEffects == nil {
		t.Error("expected empty non-nil SideEffects")
	}
	if len(got.SideEffects) != 0 {
		t.Errorf("sideEffects = %v, want empty", got.SideEffects)
	}
}

func TestExtract_ArrowFunction(t *testing.T) {
	src := `export const add = (a: number, b: number): number => a + b;`
	got := extract(t, src, "add")
	if got.Name != "add" || got.Visibility != VisibilityPublic {
		t.Errorf("got %+v", got)
	}
	if got.Returns.Type != "number" {
		t.Errorf("return type = %q, want number", got.Returns.Type)
	}
}

func TestExtract_PicksTargetAmongMany(t *testing.T) {
	src := `
export function a(): void {}
export function b(): number { return 1; }
`
	got := extract(t, src, "b")
	if got.Name != "b" {
		t.Errorf("name = %q, want b", got.Name)
	}
}

func TestExtract_Deterministic(t *testing.T) {
	src := `
import { analytics } from "./a";
export function f(): void { analytics.emit("x"); analytics.send("y"); }
`
	first := extract(t, src, "f")
	for i := 0; i < 5; i++ {
		again := extract(t, src, "f")
		if !reflect.DeepEqual(first, again) {
			t.Fatalf("extraction not deterministic:\n%+v\n%+v", first, again)
		}
	}
}

func TestExtract_ThrownFailureModes(t *testing.T) {
	src := `export function f(): void { throw new Error("bad_input"); }`
	got := extract(t, src, "f")
	want := []string{"bad_input"}
	if !reflect.DeepEqual(got.FailureModes, want) {
		t.Errorf("failureModes = %v, want %v", got.FailureModes, want)
	}
}

func TestExtract_BehaviorFromIf(t *testing.T) {
	src := `export function f(x: number): number {
  if (x < 0) {
    throw new Error("negative");
  }
  return x;
}`
	got := extract(t, src, "f")
	if len(got.Behavior) != 1 {
		t.Fatalf("behavior = %+v, want 1 clause", got.Behavior)
	}
	if got.Behavior[0].When != "x < 0" {
		t.Errorf("when = %q, want %q", got.Behavior[0].When, "x < 0")
	}
	if got.Behavior[0].Then != `throw new Error("negative")` {
		t.Errorf("then = %q", got.Behavior[0].Then)
	}
}

func TestExtract_BehaviorMultipleInOrder(t *testing.T) {
	src := `export function f(x: number): string {
  if (x < 0) { return "neg"; }
  if (x === 0) { return "zero"; }
  return "pos";
}`
	got := extract(t, src, "f")
	if len(got.Behavior) != 2 {
		t.Fatalf("want 2 behavior clauses, got %+v", got.Behavior)
	}
	if got.Behavior[0].When != "x < 0" || got.Behavior[1].When != "x === 0" {
		t.Errorf("wrong order/conditions: %+v", got.Behavior)
	}
	if got.Behavior[0].Then != `return "neg"` {
		t.Errorf("then = %q, want %q", got.Behavior[0].Then, `return "neg"`)
	}
}

func TestExtract_BehaviorIgnoresNestedFunctions(t *testing.T) {
	// The `if` inside the filter callback belongs to that closure, not to f, so
	// only the outer branch is counted.
	src := `export function f(xs: number[]): number[] {
  if (xs.length === 0) { return []; }
  return xs.filter((x) => {
    if (x > 0) { return true; }
    return false;
  });
}`
	got := extract(t, src, "f")
	if len(got.Behavior) != 1 {
		t.Fatalf("expected only the outer branch, got %d: %+v", len(got.Behavior), got.Behavior)
	}
	if got.Behavior[0].When != "xs.length === 0" {
		t.Errorf("when = %q, want %q", got.Behavior[0].When, "xs.length === 0")
	}
}

func TestExtract_NoBranchesEmptyBehavior(t *testing.T) {
	got := extract(t, `export function f(x: number): number { return x * 2; }`, "f")
	if len(got.Behavior) != 0 {
		t.Errorf("expected no behavior clauses, got %+v", got.Behavior)
	}
	if got.Behavior == nil {
		t.Error("behavior should be an empty slice, not nil")
	}
}

func TestExtract_MultilineConditionNormalized(t *testing.T) {
	src := `export function f(a: number, b: number): number {
  if (a > 0 &&
      b > 0) {
    return a + b;
  }
  return 0;
}`
	got := extract(t, src, "f")
	if len(got.Behavior) != 1 || got.Behavior[0].When != "a > 0 && b > 0" {
		t.Errorf("multiline condition not normalized: %+v", got.Behavior)
	}
}

func TestExtractAll_MultipleFunctionsInOrder(t *testing.T) {
	src := `
export function a(x: number): number { return x; }
function b(): void {}
export const c = (y: string): string => y;
`
	got, err := ExtractAll(context.Background(), []byte(src))
	if err != nil {
		t.Fatalf("ExtractAll: %v", err)
	}
	names := make([]string, len(got))
	for i, fi := range got {
		names[i] = fi.Name
	}
	if !reflect.DeepEqual(names, []string{"a", "b", "c"}) {
		t.Errorf("names = %v, want [a b c]", names)
	}
	if got[0].Visibility != VisibilityPublic || got[1].Visibility != VisibilityPrivate {
		t.Errorf("visibility: a=%s b=%s", got[0].Visibility, got[1].Visibility)
	}
}

func TestExtractAll_NoFunctionsIsEmptyNotError(t *testing.T) {
	got, err := ExtractAll(context.Background(), []byte(`const x = 1;`))
	if err != nil {
		t.Fatalf("ExtractAll: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no intents, got %d", len(got))
	}
}

func TestExtractAll_MalformedErrors(t *testing.T) {
	if _, err := ExtractAll(context.Background(), []byte(`export function f( {`)); err == nil {
		t.Error("expected error for malformed source")
	}
}

func TestExtractAllFromNode_ReusesParse(t *testing.T) {
	src := []byte("export function a(x: number): number { return x; }\nfunction b(): void {}")
	p := sitter.NewParser()
	p.SetLanguage(ts.GetLanguage())
	tree, err := p.ParseCtx(context.Background(), nil, src)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()

	got, err := ExtractAllFromNode(tree.RootNode(), src)
	if err != nil {
		t.Fatalf("ExtractAllFromNode: %v", err)
	}
	names := make([]string, len(got))
	for i, f := range got {
		names[i] = f.Intent.Name
	}
	if !reflect.DeepEqual(names, []string{"a", "b"}) {
		t.Errorf("names = %v, want [a b]", names)
	}
	// Start bytes are captured for correlation and point at the declaration
	// node (here the `function` keyword, since both are function declarations).
	if !strings.HasPrefix(string(src[got[0].StartByte:]), "function a") {
		t.Errorf("a start byte %d does not point at its declaration", got[0].StartByte)
	}
	if !strings.HasPrefix(string(src[got[1].StartByte:]), "function b") {
		t.Errorf("b start byte %d does not point at its declaration", got[1].StartByte)
	}
	// The intents must match the re-parsing ExtractAll.
	viaParse, _ := ExtractAll(context.Background(), src)
	for i := range got {
		if !reflect.DeepEqual(got[i].Intent, viaParse[i]) {
			t.Errorf("intent %d diverged between ExtractAllFromNode and ExtractAll", i)
		}
	}
}

func TestExtractAllFromNode_NilRoot(t *testing.T) {
	if _, err := ExtractAllFromNode(nil, nil); err == nil {
		t.Error("expected error for nil root")
	}
}

func TestExtract_NoFunctionsErrors(t *testing.T) {
	if _, err := ExtractFunction(context.Background(), []byte(`const x = 1;`), "f"); err == nil {
		t.Error("expected error when no functions present")
	}
}

func TestExtract_MalformedSourceErrors(t *testing.T) {
	// Unbalanced braces produce error nodes; extraction must fail fast rather
	// than build a garbled intent.
	src := `export function f(a: number: ): {{ return `
	if _, err := ExtractFunction(context.Background(), []byte(src), "f"); err == nil {
		t.Error("expected error for malformed source")
	}
}

func TestExtract_UnparenthesizedArrowParam(t *testing.T) {
	got := extract(t, `export const inc = x => x + 1;`, "inc")
	want := []Param{{Name: "x", Type: TypeUnknown}}
	if !reflect.DeepEqual(got.Inputs, want) {
		t.Errorf("inputs = %+v, want %+v", got.Inputs, want)
	}
}

func TestExtract_RestParameter(t *testing.T) {
	got := extract(t, `export function sum(...values: number[]): number { return 0; }`, "sum")
	if len(got.Inputs) != 1 {
		t.Fatalf("inputs = %+v, want one rest param", got.Inputs)
	}
	if got.Inputs[0].Name != "values" {
		t.Errorf("rest param name = %q, want values", got.Inputs[0].Name)
	}
	if got.Inputs[0].Type != "number[]" {
		t.Errorf("rest param type = %q, want number[]", got.Inputs[0].Type)
	}
}
