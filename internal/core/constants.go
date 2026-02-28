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
	DefaultKLimit             = 30
	DefaultMaxLoops           = 8
	ContextWindowSafetyMargin = 0.85 // exit loop at 85% of model context limit

	// Activation values by anchor confidence
	ActivationHighConfidence   = 1.0
	ActivationMediumConfidence = 0.7
	ActivationLowConfidence    = 0.4

	// ActivationThreshold — propagation stops when activation falls below this.
	ActivationThreshold = 0.1

	// MaxPropagationDepth — safety cap on propagation depth regardless of activation level.
	MaxPropagationDepth = 6

	// ActivationDecay — activation decay per hop, multiplied by edge weight.
	// activation_at_neighbor = source_activation * decay * edge_weight
	ActivationDecay = 0.6

	// DefaultEdgeWeight — default weight for new structural edges.
	DefaultEdgeWeight = 0.5

	// Edge weight range
	MinEdgeWeight = 0.01
	MaxEdgeWeight = 1.0

	// HebbianLearningRate — how much weight increases per co-activation.
	HebbianLearningRate = 0.1

	// HebbianDecayRate — how much weight decreases when not co-activated.
	// Applied to all edges at the end of each cognitive loop.
	HebbianDecayRate = 0.01
)
