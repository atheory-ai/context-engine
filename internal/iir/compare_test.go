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
		SideEffects:  []SideEffect{},
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
	// A resolved effect that isn't declared is a hard error.
	extracted.SideEffects = []SideEffect{{Name: "analytics.track", Kind: EffectMutation, Basis: BasisResolved}}
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

func TestTypesEqual_LanguageEquivalences(t *testing.T) {
	cases := []struct {
		lang, a, b string
		want       bool
	}{
		// whitespace-only (language-agnostic)
		{"typescript", "Map<string, number>", "Map<string,number>", true},
		// Go: interface{} ≡ any, including inside compound types
		{"go", "interface{}", "any", true},
		{"go", "[]interface{}", "[]any", true},
		{"go", "map[string]interface{}", "map[string]any", true},
		{"go", "int", "string", false},
		// TS: Array<T> ≡ T[]
		{"typescript", "Array<string>", "string[]", true},
		{"typescript", "Array<User>", "User[]", true},
		{"typescript", "string", "number", false},
		// Python: typing generics ≡ builtin generics; Optional[T] ≡ T | None
		{"python", "List[int]", "list[int]", true},
		{"python", "Dict[str, int]", "dict[str,int]", true},
		{"python", "Optional[str]", "str | None", true},
		{"python", "list[int]", "dict[int]", false},
	}
	for _, c := range cases {
		if got := typesEqual(c.a, c.b, c.lang); got != c.want {
			t.Errorf("typesEqual(%q, %q, %q) = %v, want %v", c.a, c.b, c.lang, got, c.want)
		}
	}
}

// An equivalence (not a whitespace-only diff) is reported as acceptable_equivalent
// rather than a mismatch.
func TestCompare_LanguageEquivalentTypeIsAcceptable(t *testing.T) {
	intended := baseIntent() // typescript
	intended.Returns.Type = "Array<string>"
	extracted := baseIntent()
	extracted.Returns.Type = "string[]"
	matches, mismatches := Compare(intended, extracted)
	if findMismatch(mismatches, MismatchReturnType) != nil {
		t.Fatalf("Array<string> vs string[] must not mismatch: %+v", mismatches)
	}
	if m := findMatch(matches, "FunctionIntent.returns.type"); m == nil || m.Kind != MatchEquivalent {
		t.Errorf("expected acceptable_equivalent, got %+v", m)
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

func TestCompare_ExtraBehaviorIsInfo(t *testing.T) {
	intended := baseIntent()
	intended.Behavior = []BehaviorClause{{When: "a", Then: "b"}}
	extracted := baseIntent()
	extracted.Behavior = []BehaviorClause{{When: "a", Then: "b"}, {When: "c", Then: "d"}}
	_, mismatches := Compare(intended, extracted)
	m := findMismatch(mismatches, MismatchExtraBehavior)
	if m == nil || m.Severity != SeverityInfo {
		t.Errorf("expected info extra_behavior, got %+v", mismatches)
	}
}

func TestCompare_UnknownInputTypeIsExactNotEquivalent(t *testing.T) {
	// When a type is unknown it was never compared, so the match must be exact
	// (name agreement), not falsely labeled acceptable_equivalent.
	intended := baseIntent() // input "a" has type "number"
	extracted := baseIntent()
	extracted.Inputs[0].Type = TypeUnknown
	matches, _ := Compare(intended, extracted)
	m := findMatch(matches, "FunctionIntent.inputs[0]")
	if m == nil || m.Kind != MatchExact {
		t.Errorf("unknown input type should be exact match, got %+v", m)
	}
}

func TestCompare_DeclaredButUndetectedEffectIsWarning(t *testing.T) {
	intended := baseIntent()
	intended.SideEffects = stringEffects("db.save")
	extracted := baseIntent() // no effects detected
	_, mismatches := Compare(intended, extracted)
	m := findMismatch(mismatches, MismatchUndetectedEffect)
	if m == nil || m.Severity != SeverityWarning {
		t.Errorf("expected warning undetected_side_effect, got %+v", mismatches)
	}
}
