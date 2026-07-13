package iir

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Severity ranks a finding. Errors fail verification; warnings and info do not.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// MismatchKind classifies how extracted IIR diverges from intended IIR.
type MismatchKind string

const (
	MismatchName               MismatchKind = "mismatched_name"
	MismatchChangedContract    MismatchKind = "changed_public_contract"
	MismatchMissingInput       MismatchKind = "missing_input"
	MismatchExtraInput         MismatchKind = "extra_input"
	MismatchInputType          MismatchKind = "mismatched_input_type"
	MismatchMissingReturnType  MismatchKind = "missing_return_type"
	MismatchReturnType         MismatchKind = "mismatched_return_type"
	MismatchUndeclaredEffect   MismatchKind = "undeclared_side_effect"
	MismatchUndetectedEffect   MismatchKind = "undetected_side_effect"
	MismatchChangedFailureMode MismatchKind = "changed_failure_mode"
	MismatchMissingBehavior    MismatchKind = "missing_behavior"
	MismatchExtraBehavior      MismatchKind = "extra_behavior"
	// MismatchBehaviorContent marks a positionally-aligned behavior clause whose
	// normalized condition differs in content (e.g. `<` vs `>`) — a divergence
	// the count-only comparison cannot see. Only raised when both sides expose a
	// normalized WhenExpr.
	MismatchBehaviorContent MismatchKind = "mismatched_behavior"
	// MismatchUnsupported marks an aspect the engine cannot yet verify. It is
	// reported (never a silent pass) at info severity so it does not fail
	// verification.
	MismatchUnsupported MismatchKind = "unsupported_comparison"
)

// MatchKind distinguishes an exact agreement from an acceptable equivalent
// (e.g. types that differ only in insignificant formatting).
type MatchKind string

const (
	MatchExact      MatchKind = "exact_match"
	MatchEquivalent MatchKind = "acceptable_equivalent"
)

// Match records an aspect of the contract that intended and extracted IIR agree
// on. Matches make a passing report auditable, not just empty.
type Match struct {
	Kind    MatchKind `json:"kind"`
	Path    string    `json:"path"`
	Message string    `json:"message"`
}

// Mismatch is a machine-readable divergence with a concrete repair target.
type Mismatch struct {
	Kind         MismatchKind `json:"kind"`
	Severity     Severity     `json:"severity"`
	Path         string       `json:"path"`
	Message      string       `json:"message"`
	Expected     any          `json:"expected"`
	Actual       any          `json:"actual"`
	RepairTarget string       `json:"repairTarget"`
}

// Compare diffs intended IIR against extracted IIR, producing stable matches and
// mismatches. Formatting-only differences (e.g. whitespace in types) do not
// produce mismatches.
func Compare(intended, extracted *FunctionIntent) (matches []Match, mismatches []Mismatch) {
	matches = []Match{}
	mismatches = []Mismatch{}

	compareName(intended, extracted, &matches, &mismatches)
	compareVisibility(intended, extracted, &matches, &mismatches)
	compareInputs(intended, extracted, &matches, &mismatches)
	compareReturn(intended, extracted, &matches, &mismatches)
	compareSideEffects(intended, extracted, &matches, &mismatches)
	compareFailureModes(intended, extracted, &matches, &mismatches)
	compareBehavior(intended, extracted, &matches, &mismatches)

	return matches, mismatches
}

// compareVisibility detects a changed public contract: a function the intent
// declares public that source makes private (or vice versa) changes the API
// surface and is an error.
func compareVisibility(intended, extracted *FunctionIntent, matches *[]Match, mismatches *[]Mismatch) {
	if intended.Visibility == extracted.Visibility {
		*matches = append(*matches, Match{
			Kind:    MatchExact,
			Path:    "FunctionIntent.visibility",
			Message: fmt.Sprintf("visibility %q matches", intended.Visibility),
		})
		return
	}
	*mismatches = append(*mismatches, Mismatch{
		Kind:     MismatchChangedContract,
		Severity: SeverityError,
		Path:     "FunctionIntent.visibility",
		Message: fmt.Sprintf("intended %q visibility but source is %q",
			intended.Visibility, extracted.Visibility),
		Expected:     intended.Visibility,
		Actual:       extracted.Visibility,
		RepairTarget: fmt.Sprintf("Make the function %s, or update the intent's visibility.", intended.Visibility),
	})
}

// compareBehavior performs a basic behavior comparison. Because behavior
// extraction from source is not yet implemented (a later slice), a declared
// behavior with no extracted counterpart is reported as unsupported rather than
// silently passed or falsely flagged as missing. When both sides carry behavior
// clauses, a count-based comparison surfaces missing/extra behavior.
func compareBehavior(intended, extracted *FunctionIntent, matches *[]Match, mismatches *[]Mismatch) {
	if len(intended.Behavior) == 0 {
		return // intent makes no behavioral claim
	}
	if len(extracted.Behavior) == 0 {
		// Behavior extraction only sees conditional branches; a function that
		// expresses its logic without them yields nothing to map declared
		// behavior onto. Report as unsupported (info) rather than guess.
		*mismatches = append(*mismatches, Mismatch{
			Kind:         MismatchUnsupported,
			Severity:     SeverityInfo,
			Path:         "FunctionIntent.behavior",
			Message:      fmt.Sprintf("declared %d behavior clause(s) could not be verified: no conditional branches found in source", len(intended.Behavior)),
			Expected:     intended.Behavior,
			Actual:       extracted.Behavior,
			RepairTarget: "Review the declared behavior against the source manually; only branch-based behavior is compared automatically.",
		})
		return
	}

	if len(extracted.Behavior) < len(intended.Behavior) {
		*mismatches = append(*mismatches, Mismatch{
			Kind:         MismatchMissingBehavior,
			Severity:     SeverityWarning,
			Path:         "FunctionIntent.behavior",
			Message:      fmt.Sprintf("intended %d behavior clause(s) but source expresses %d", len(intended.Behavior), len(extracted.Behavior)),
			Expected:     len(intended.Behavior),
			Actual:       len(extracted.Behavior),
			RepairTarget: "Implement the missing behavior or remove it from the intent.",
		})
		return
	}
	if len(extracted.Behavior) > len(intended.Behavior) {
		*mismatches = append(*mismatches, Mismatch{
			Kind:         MismatchExtraBehavior,
			Severity:     SeverityInfo,
			Path:         "FunctionIntent.behavior",
			Message:      fmt.Sprintf("source expresses %d behavior clause(s) but intent declares %d", len(extracted.Behavior), len(intended.Behavior)),
			Expected:     len(intended.Behavior),
			Actual:       len(extracted.Behavior),
			RepairTarget: "Declare the additional behavior in the intent or remove it from the source.",
		})
		return
	}
	// Counts align. Where both sides expose a normalized condition, compare the
	// structured content positionally — this catches a flipped or altered
	// condition (e.g. `<` vs `>`) that the count-only check passes silently.
	// A clause missing WhenExpr on either side falls back to the count match.
	contentMismatch := false
	for i := range intended.Behavior {
		want, got := intended.Behavior[i].WhenExpr, extracted.Behavior[i].WhenExpr
		if want == nil || got == nil || want.Equal(got) {
			continue
		}
		contentMismatch = true
		*mismatches = append(*mismatches, Mismatch{
			Kind:     MismatchBehaviorContent,
			Severity: SeverityWarning,
			Path:     fmt.Sprintf("FunctionIntent.behavior[%d].when", i),
			Message: fmt.Sprintf("behavior clause %d condition differs: intended %q, source %q",
				i, intended.Behavior[i].When, extracted.Behavior[i].When),
			Expected:     want,
			Actual:       got,
			RepairTarget: "Align the condition in source with the intent, or update the intent to match.",
		})
	}
	if contentMismatch {
		return
	}
	*matches = append(*matches, Match{
		Kind:    MatchExact,
		Path:    "FunctionIntent.behavior",
		Message: fmt.Sprintf("behavior clause count matches (%d)", len(intended.Behavior)),
	})
}

func compareName(intended, extracted *FunctionIntent, matches *[]Match, mismatches *[]Mismatch) {
	if intended.Name == extracted.Name {
		*matches = append(*matches, Match{
			Kind:    MatchExact,
			Path:    "FunctionIntent.name",
			Message: fmt.Sprintf("function name %q matches", intended.Name),
		})
		return
	}
	*mismatches = append(*mismatches, Mismatch{
		Kind:     MismatchName,
		Severity: SeverityError,
		Path:     "FunctionIntent.name",
		Message: fmt.Sprintf("intended function %q but source defines %q",
			intended.Name, extracted.Name),
		Expected:     intended.Name,
		Actual:       extracted.Name,
		RepairTarget: fmt.Sprintf("Rename the source function to %q or update the intent name.", intended.Name),
	})
}

func compareInputs(intended, extracted *FunctionIntent, matches *[]Match, mismatches *[]Mismatch) {
	actualByName := map[string]Param{}
	for _, p := range extracted.Inputs {
		actualByName[p.Name] = p
	}
	intendedNames := map[string]bool{}

	for i, want := range intended.Inputs {
		intendedNames[want.Name] = true
		path := fmt.Sprintf("FunctionIntent.inputs[%d]", i)
		got, ok := actualByName[want.Name]
		if !ok {
			*mismatches = append(*mismatches, Mismatch{
				Kind:         MismatchMissingInput,
				Severity:     SeverityError,
				Path:         path,
				Message:      fmt.Sprintf("intended input %q is missing from source", want.Name),
				Expected:     want.Name,
				Actual:       nil,
				RepairTarget: fmt.Sprintf("Add parameter %q to the function or remove it from the intent.", want.Name),
			})
			continue
		}
		if want.Type != TypeUnknown && got.Type != TypeUnknown && !typesEqual(want.Type, got.Type, intended.Language) {
			*mismatches = append(*mismatches, Mismatch{
				Kind:     MismatchInputType,
				Severity: SeverityError,
				Path:     path + ".type",
				Message: fmt.Sprintf("input %q intended type %q but source declares %q",
					want.Name, want.Type, got.Type),
				Expected:     want.Type,
				Actual:       got.Type,
				RepairTarget: fmt.Sprintf("Align the type of %q (%q vs %q).", want.Name, want.Type, got.Type),
			})
			continue
		}
		*matches = append(*matches, Match{
			Kind:    inputMatchKind(want.Type, got.Type),
			Path:    path,
			Message: fmt.Sprintf("input %q matches", want.Name),
		})
	}

	for _, got := range extracted.Inputs {
		if intendedNames[got.Name] {
			continue
		}
		*mismatches = append(*mismatches, Mismatch{
			Kind:         MismatchExtraInput,
			Severity:     SeverityError,
			Path:         "FunctionIntent.inputs",
			Message:      fmt.Sprintf("source declares undeclared input %q", got.Name),
			Expected:     nil,
			Actual:       got.Name,
			RepairTarget: fmt.Sprintf("Declare input %q in the intent or remove it from the function.", got.Name),
		})
	}
}

func compareReturn(intended, extracted *FunctionIntent, matches *[]Match, mismatches *[]Mismatch) {
	if !intended.Returns.Explicit {
		return // intent makes no claim about the return type
	}
	if !extracted.Returns.Explicit {
		*mismatches = append(*mismatches, Mismatch{
			Kind:         MismatchMissingReturnType,
			Severity:     SeverityError,
			Path:         "FunctionIntent.returns.type",
			Message:      fmt.Sprintf("intended return type %q but source has no explicit return type", intended.Returns.Type),
			Expected:     intended.Returns.Type,
			Actual:       nil,
			RepairTarget: fmt.Sprintf("Add an explicit return type %q to the function.", intended.Returns.Type),
		})
		return
	}
	if !typesEqual(intended.Returns.Type, extracted.Returns.Type, intended.Language) {
		*mismatches = append(*mismatches, Mismatch{
			Kind:         MismatchReturnType,
			Severity:     SeverityError,
			Path:         "FunctionIntent.returns.type",
			Message:      fmt.Sprintf("intended return type %q but source declares %q", intended.Returns.Type, extracted.Returns.Type),
			Expected:     intended.Returns.Type,
			Actual:       extracted.Returns.Type,
			RepairTarget: fmt.Sprintf("Align the return type (%q vs %q).", intended.Returns.Type, extracted.Returns.Type),
		})
		return
	}
	*matches = append(*matches, Match{
		Kind:    typeMatchKind(intended.Returns.Type, extracted.Returns.Type),
		Path:    "FunctionIntent.returns.type",
		Message: fmt.Sprintf("return type %q matches", intended.Returns.Type),
	})
}

// typeMatchKind reports whether two agreeing types are identical or merely
// equivalent after normalizing insignificant formatting.
func typeMatchKind(a, b string) MatchKind {
	if a == b {
		return MatchExact
	}
	return MatchEquivalent
}

// inputMatchKind classifies an input match. When either type is unknown the
// types were never actually compared, so the agreement is on name alone — an
// exact match, not an equivalence claim.
func inputMatchKind(want, got string) MatchKind {
	if want == TypeUnknown || got == TypeUnknown {
		return MatchExact
	}
	return typeMatchKind(want, got)
}

func compareSideEffects(intended, extracted *FunctionIntent, matches *[]Match, mismatches *[]Mismatch) {
	declared := toSet(effectNames(intended.SideEffects))
	found := toSet(effectNames(extracted.SideEffects))

	// Effects in source but not declared. Severity is graded by confidence: a
	// resolved (recognized) effect is an error, a heuristic (guessed) one is a
	// warning — so an over-eager heuristic detection doesn't fail verification
	// outright.
	var undeclaredHigh, undeclaredLow []string
	for _, e := range extracted.SideEffects {
		if declared[e.Name] {
			continue
		}
		if effectBasis(e) == BasisResolved {
			undeclaredHigh = append(undeclaredHigh, e.Name)
		} else {
			undeclaredLow = append(undeclaredLow, e.Name)
		}
	}
	if len(undeclaredHigh) > 0 {
		sort.Strings(undeclaredHigh)
		*mismatches = append(*mismatches, Mismatch{
			Kind:         MismatchUndeclaredEffect,
			Severity:     SeverityError,
			Path:         "FunctionIntent.sideEffects",
			Message:      fmt.Sprintf("source performs undeclared side effects: %s", strings.Join(undeclaredHigh, ", ")),
			Expected:     intended.SideEffects,
			Actual:       extracted.SideEffects,
			RepairTarget: fmt.Sprintf("Either remove %s or declare the side effect(s) in intended IIR.", strings.Join(undeclaredHigh, ", ")),
		})
	}
	if len(undeclaredLow) > 0 {
		sort.Strings(undeclaredLow)
		*mismatches = append(*mismatches, Mismatch{
			Kind:         MismatchUndeclaredEffect,
			Severity:     SeverityWarning,
			Path:         "FunctionIntent.sideEffects",
			Message:      fmt.Sprintf("source may perform undeclared side effects (low confidence): %s", strings.Join(undeclaredLow, ", ")),
			Expected:     intended.SideEffects,
			Actual:       extracted.SideEffects,
			RepairTarget: fmt.Sprintf("If %s is a real effect, declare it in intended IIR; otherwise ignore.", strings.Join(undeclaredLow, ", ")),
		})
	}

	// Effects declared but not detected: a warning — extraction is conservative
	// and may not observe every declared effect.
	var undetected []string
	for e := range declared {
		if !found[e] {
			undetected = append(undetected, e)
		}
	}
	if len(undetected) > 0 {
		sort.Strings(undetected)
		*mismatches = append(*mismatches, Mismatch{
			Kind:         MismatchUndetectedEffect,
			Severity:     SeverityWarning,
			Path:         "FunctionIntent.sideEffects",
			Message:      fmt.Sprintf("intended side effects not observed in source: %s", strings.Join(undetected, ", ")),
			Expected:     intended.SideEffects,
			Actual:       extracted.SideEffects,
			RepairTarget: fmt.Sprintf("Confirm %s is implemented, or remove it from the intent.", strings.Join(undetected, ", ")),
		})
	}

	if len(undeclaredHigh) == 0 && len(undeclaredLow) == 0 && len(undetected) == 0 {
		*matches = append(*matches, Match{
			Kind:    MatchExact,
			Path:    "FunctionIntent.sideEffects",
			Message: "declared side effects match source",
		})
	}
}

func compareFailureModes(intended, extracted *FunctionIntent, matches *[]Match, mismatches *[]Mismatch) {
	if len(intended.FailureModes) == 0 {
		return
	}
	declared := toSet(intended.FailureModes)
	found := toSet(extracted.FailureModes)

	var undetected []string
	for m := range declared {
		if !found[m] {
			undetected = append(undetected, m)
		}
	}
	if len(undetected) == 0 {
		*matches = append(*matches, Match{
			Kind:    MatchExact,
			Path:    "FunctionIntent.failureModes",
			Message: "declared failure modes observed in source",
		})
		return
	}
	sort.Strings(undetected)
	// Warning, not error: Slice 1 failure-mode extraction only sees thrown
	// string literals, so an unobserved mode is a soft signal.
	*mismatches = append(*mismatches, Mismatch{
		Kind:         MismatchChangedFailureMode,
		Severity:     SeverityWarning,
		Path:         "FunctionIntent.failureModes",
		Message:      fmt.Sprintf("intended failure modes not observed in source: %s", strings.Join(undetected, ", ")),
		Expected:     intended.FailureModes,
		Actual:       extracted.FailureModes,
		RepairTarget: fmt.Sprintf("Confirm the source can produce: %s.", strings.Join(undetected, ", ")),
	})
}

// typesEqual compares two type strings for the given language, ignoring
// insignificant whitespace and canonicalizing well-known equivalent spellings
// (e.g. Go interface{} vs any, TS Array<T> vs T[], Python Optional[T] vs
// T | None) so that a purely-syntactic difference doesn't fail verification.
func typesEqual(a, b, language string) bool {
	return canonicalType(a, language) == canonicalType(b, language)
}

func canonicalType(t, language string) string {
	s := stripTypeWhitespace(t)
	switch language {
	case "go":
		// The empty interface and its 1.18+ alias name the same type.
		s = strings.ReplaceAll(s, "interface{}", "any")
	case "typescript", "tsx", "javascript":
		// Array<T> and T[] are the same type; canonicalize to the shorthand.
		s = tsArrayRE.ReplaceAllString(s, "$1[]")
	case "python":
		// typing's capitalized generics alias the lowercase builtins (PEP 585),
		// and Optional[T] is T | None (PEP 604).
		s = pyGenericRE.ReplaceAllStringFunc(s, strings.ToLower)
		s = pyOptionalRE.ReplaceAllString(s, "$1|None")
	}
	return s
}

var (
	// Array<T> -> T[] (single, non-nested generic argument).
	tsArrayRE = regexp.MustCompile(`Array<([^<>]+)>`)
	// Capitalized typing generics immediately before their subscript.
	pyGenericRE = regexp.MustCompile(`\b(List|Dict|Set|Tuple|FrozenSet|Type)\[`)
	// Optional[T] -> T|None.
	pyOptionalRE = regexp.MustCompile(`Optional\[([^\[\]]+)\]`)
)

func stripTypeWhitespace(t string) string {
	var b strings.Builder
	for _, r := range t {
		if r == ' ' || r == '\t' || r == '\n' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func toSet(items []string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, it := range items {
		s[it] = true
	}
	return s
}
