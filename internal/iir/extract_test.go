package iir

import (
	"context"
	"reflect"
	"testing"
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

func TestExtract_NoFunctionsErrors(t *testing.T) {
	if _, err := ExtractFunction(context.Background(), []byte(`const x = 1;`), "f"); err == nil {
		t.Error("expected error when no functions present")
	}
}
