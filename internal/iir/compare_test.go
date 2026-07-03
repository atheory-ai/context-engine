package iir

import "testing"

// baseIntent returns a minimal, matching intended/extracted pair.
func baseIntent() *FunctionIntent {
	return &FunctionIntent{
		Kind:         KindFunctionIntent,
		Name:         "f",
		Language:     "typescript",
		Visibility:   VisibilityPublic,
		Inputs:       []Param{{Name: "a", Type: "number"}},
		Returns:      Return{Type: "number", Explicit: true},
		SideEffects:  []string{},
		FailureModes: []string{},
	}
}

func findMismatch(ms []Mismatch, kind MismatchKind) *Mismatch {
	for i := range ms {
		if ms[i].Kind == kind {
			return &ms[i]
		}
	}
	return nil
}

func TestCompare_AllMatch(t *testing.T) {
	_, mismatches := Compare(baseIntent(), baseIntent())
	if len(mismatches) != 0 {
		t.Errorf("expected no mismatches, got %+v", mismatches)
	}
}

func TestCompare_NameMismatch(t *testing.T) {
	extracted := baseIntent()
	extracted.Name = "g"
	_, mismatches := Compare(baseIntent(), extracted)
	m := findMismatch(mismatches, MismatchName)
	if m == nil || m.Severity != SeverityError {
		t.Fatalf("expected error name mismatch, got %+v", mismatches)
	}
	if m.RepairTarget == "" {
		t.Error("name mismatch must carry a repair target")
	}
}

func TestCompare_MissingInput(t *testing.T) {
	extracted := baseIntent()
	extracted.Inputs = nil
	_, mismatches := Compare(baseIntent(), extracted)
	if findMismatch(mismatches, MismatchMissingInput) == nil {
		t.Errorf("expected missing_input, got %+v", mismatches)
	}
}

func TestCompare_ExtraInput(t *testing.T) {
	extracted := baseIntent()
	extracted.Inputs = append(extracted.Inputs, Param{Name: "b", Type: "string"})
	_, mismatches := Compare(baseIntent(), extracted)
	if findMismatch(mismatches, MismatchExtraInput) == nil {
		t.Errorf("expected extra_input, got %+v", mismatches)
	}
}

func TestCompare_InputTypeMismatch(t *testing.T) {
	extracted := baseIntent()
	extracted.Inputs[0].Type = "string"
	_, mismatches := Compare(baseIntent(), extracted)
	m := findMismatch(mismatches, MismatchInputType)
	if m == nil || m.Severity != SeverityError {
		t.Errorf("expected error input type mismatch, got %+v", mismatches)
	}
}

func TestCompare_UnknownTypesDoNotMismatch(t *testing.T) {
	// If either side's type is unknown, we cannot assert a type conflict.
	extracted := baseIntent()
	extracted.Inputs[0].Type = TypeUnknown
	_, mismatches := Compare(baseIntent(), extracted)
	if findMismatch(mismatches, MismatchInputType) != nil {
		t.Errorf("unknown type should not produce a type mismatch: %+v", mismatches)
	}
}

func TestCompare_MissingReturnType(t *testing.T) {
	extracted := baseIntent()
	extracted.Returns = Return{Explicit: false}
	_, mismatches := Compare(baseIntent(), extracted)
	if findMismatch(mismatches, MismatchMissingReturnType) == nil {
		t.Errorf("expected missing_return_type, got %+v", mismatches)
	}
}

func TestCompare_ReturnTypeWhitespaceEquivalent(t *testing.T) {
	intended := baseIntent()
	intended.Returns.Type = "Map<string, number>"
	extracted := baseIntent()
	extracted.Returns.Type = "Map<string,number>"
	_, mismatches := Compare(intended, extracted)
	if findMismatch(mismatches, MismatchReturnType) != nil {
		t.Errorf("whitespace-only type difference must not fail: %+v", mismatches)
	}
}

func TestCompare_UndeclaredSideEffect(t *testing.T) {
	extracted := baseIntent()
	extracted.SideEffects = []string{"analytics.track"}
	_, mismatches := Compare(baseIntent(), extracted)
	m := findMismatch(mismatches, MismatchUndeclaredEffect)
	if m == nil || m.Severity != SeverityError {
		t.Fatalf("expected error undeclared_side_effect, got %+v", mismatches)
	}
	if m.RepairTarget == "" {
		t.Error("undeclared side effect must carry a repair target")
	}
}

func findMatch(ms []Match, path string) *Match {
	for i := range ms {
		if ms[i].Path == path {
			return &ms[i]
		}
	}
	return nil
}

func TestCompare_ChangedPublicContract(t *testing.T) {
	extracted := baseIntent()
	extracted.Visibility = VisibilityPrivate // intent is public
	_, mismatches := Compare(baseIntent(), extracted)
	m := findMismatch(mismatches, MismatchChangedContract)
	if m == nil || m.Severity != SeverityError {
		t.Fatalf("expected error changed_public_contract, got %+v", mismatches)
	}
	if m.RepairTarget == "" {
		t.Error("contract change must carry a repair target")
	}
}

func TestCompare_ExactMatchKind(t *testing.T) {
	matches, _ := Compare(baseIntent(), baseIntent())
	m := findMatch(matches, "FunctionIntent.returns.type")
	if m == nil || m.Kind != MatchExact {
		t.Errorf("expected exact_match for identical return type, got %+v", m)
	}
}

func TestCompare_AcceptableEquivalentType(t *testing.T) {
	intended := baseIntent()
	intended.Returns.Type = "Map<string, number>"
	extracted := baseIntent()
	extracted.Returns.Type = "Map<string,number>" // formatting differs only
	matches, mismatches := Compare(intended, extracted)
	if len(mismatches) != 0 {
		t.Fatalf("formatting-only diff must not mismatch: %+v", mismatches)
	}
	m := findMatch(matches, "FunctionIntent.returns.type")
	if m == nil || m.Kind != MatchEquivalent {
		t.Errorf("expected acceptable_equivalent for whitespace-only diff, got %+v", m)
	}
}

func TestCompare_BehaviorUnsupportedWhenNotExtracted(t *testing.T) {
	intended := baseIntent()
	intended.Behavior = []BehaviorClause{{When: "x", Then: "y"}}
	extracted := baseIntent() // no behavior extracted
	_, mismatches := Compare(intended, extracted)
	m := findMismatch(mismatches, MismatchUnsupported)
	if m == nil {
		t.Fatalf("expected unsupported_comparison for unverifiable behavior, got %+v", mismatches)
	}
	// Unsupported must not fail verification.
	if m.Severity != SeverityInfo {
		t.Errorf("unsupported comparison severity = %s, want info", m.Severity)
	}
}

func TestCompare_BehaviorCountMismatch(t *testing.T) {
	intended := baseIntent()
	intended.Behavior = []BehaviorClause{{When: "a", Then: "b"}, {When: "c", Then: "d"}}
	extracted := baseIntent()
	extracted.Behavior = []BehaviorClause{{When: "a", Then: "b"}}
	_, mismatches := Compare(intended, extracted)
	if findMismatch(mismatches, MismatchMissingBehavior) == nil {
		t.Errorf("expected missing_behavior, got %+v", mismatches)
	}
}

func TestCompare_DeclaredButUndetectedEffectIsWarning(t *testing.T) {
	intended := baseIntent()
	intended.SideEffects = []string{"db.save"}
	extracted := baseIntent() // no effects detected
	_, mismatches := Compare(intended, extracted)
	m := findMismatch(mismatches, MismatchUndetectedEffect)
	if m == nil || m.Severity != SeverityWarning {
		t.Errorf("expected warning undetected_side_effect, got %+v", mismatches)
	}
}
