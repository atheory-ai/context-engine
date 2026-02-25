package core

import "fmt"

// IR is the Intermediate Representation produced by the Strategizer.
// It is the compiled form of the user's query — the engine's understanding
// of what is being asked and what to look for.
//
// The Strategizer emits XML tags; the tag extractor parses them into this
// struct. Downstream nodes operate on the IR, not on the raw query text.
type IR struct {
	// Mode controls how the engine approaches the query.
	Mode IRMode // thinking | direct | audit

	// Anchors are the substrate entry points.
	// The activation layer resolves these to actual nodes.
	Anchors []AnchorRef

	// Predicates are boolean flags that activate specific tools.
	// Tool.Activate(ir) returns true if the tool should run.
	Predicates map[string]string

	// OpenQueries are unresolved sub-questions the engine identified.
	// The cognitive loop works through these until they're answered
	// or the loop converges.
	OpenQueries []string

	// MaxLoops caps the cognitive loop iteration count for this query.
	// Overrides the project default. 0 = use project default.
	MaxLoops int

	// KLimit caps the number of nodes returned per activation query.
	// Overrides the project default. 0 = use project default.
	KLimit int

	// RoleHint is an optional suggestion for which agent role to use.
	// Empty = use the default role for this project.
	RoleHint string

	// ModelTier is an optional request for a specific LLM tier.
	// Empty = let the router decide.
	ModelTier string // fast | standard | thinking
}

// IRMode controls how the engine approaches a query.
type IRMode string

const (
	IRModeThinking IRMode = "thinking"
	IRModeDirect   IRMode = "direct"
	IRModeAudit    IRMode = "audit"
)

// Validate checks that the IR is structurally sound.
// Called immediately after tag extraction. Returns the first validation error found.
//
// Hard errors (return error): no anchors, no open queries.
// Soft corrections (coerce silently): bad confidence, out-of-range numbers,
// non-true predicate values.
func (ir *IR) Validate() error {
	// Required: at least one anchor
	if len(ir.Anchors) == 0 {
		return fmt.Errorf("%w: no anchors — investigation has no entry point", ErrInvalidIR)
	}

	// Required: at least one open query
	if len(ir.OpenQueries) == 0 {
		return fmt.Errorf("%w: no open_queries — investigation has nothing to resolve", ErrInvalidIR)
	}

	// Anchor validation
	for i, a := range ir.Anchors {
		if a.ID == "" {
			return fmt.Errorf("%w: anchor[%d] has empty ID", ErrInvalidIR, i)
		}
		switch a.Type {
		case "symbol", "namespace", "concept", "file":
			// valid
		default:
			return fmt.Errorf("%w: anchor[%d] has unknown type %q", ErrInvalidIR, i, a.Type)
		}
		switch a.Confidence {
		case "high", "medium", "low":
			// valid
		default:
			ir.Anchors[i].Confidence = "medium" // coerce rather than reject
		}
	}

	// Predicate value validation — only "true" is meaningful
	for name, value := range ir.Predicates {
		if value != "true" {
			delete(ir.Predicates, name)
		}
	}

	// MaxLoops range
	if ir.MaxLoops < 0 || ir.MaxLoops > 20 {
		ir.MaxLoops = 0 // coerce to default
	}

	// KLimit range
	if ir.KLimit < 0 || ir.KLimit > 100 {
		ir.KLimit = 0 // coerce to default
	}

	return nil
}
