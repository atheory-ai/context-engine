package core

// Node types — built-in. Plugins can define additional types as plain strings.
const (
	NodeTypeSymbol    = "symbol"
	NodeTypeNamespace = "namespace"
	NodeTypeConcept   = "concept"
	NodeTypeFile      = "file"
	NodeTypeDirectory = "directory"
)

// Edge types — built-in.
const (
	EdgeTypeCalls       = "calls"
	EdgeTypeImports     = "imports"
	EdgeTypeImplements  = "implements"
	EdgeTypeExtends     = "extends"
	EdgeTypeContains    = "contains"
	EdgeTypeReferences  = "references"
	EdgeTypeDefines     = "defines"
	EdgeTypeBelongsTo   = "belongs_to"
	EdgeTypeSynonymOf   = "synonym_of"
	EdgeTypeCoActivates = "co_activates"
	EdgeTypeAnnotates   = "annotates"
)

// Token scopes
const (
	ScopeRead      = "read"
	ScopeReadWrite = "read-write"
	ScopeAdmin     = "admin"
)

// Model tiers
const (
	TierFast     = "fast"
	TierStandard = "standard"
	TierThinking = "thinking"
)

// Activation thresholds and loop defaults
const (
	DefaultKLimit              = 30
	DefaultMaxLoops            = 8
	ContextWindowSafetyMargin  = 0.85 // exit loop at 85% of model context limit
)
