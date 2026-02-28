package parser

import (
	"fmt"
	"os"
	"sync"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	sitter_tsx "github.com/smacker/go-tree-sitter/typescript/tsx"
	sitter_ts "github.com/smacker/go-tree-sitter/typescript/typescript"
)

// GrammarSource identifies where a grammar was registered from.
type GrammarSource string

const (
	GrammarBuiltIn GrammarSource = "builtin"
	GrammarPlugin  GrammarSource = "plugin"
)

// Grammar is a loaded tree-sitter language with metadata.
type Grammar struct {
	Language *sitter.Language
	Name     string
	Source   GrammarSource
	PluginID string // non-empty if Source == GrammarPlugin
}

// GrammarRegistry maps file extensions to grammars.
// Plugin-registered grammars override built-in grammars for the same extension.
// Thread-safe.
type GrammarRegistry struct {
	mu       sync.RWMutex
	grammars map[string]*Grammar // extension (lowercase, with dot) → Grammar
}

// NewGrammarRegistry creates a registry pre-populated with built-in grammars.
func NewGrammarRegistry() *GrammarRegistry {
	r := &GrammarRegistry{
		grammars: make(map[string]*Grammar),
	}
	r.registerBuiltIns()
	return r
}

// registerBuiltIns loads the CGO-compiled built-in grammars.
// Extraction is always via plugins — grammars only handle parsing to CST.
func (r *GrammarRegistry) registerBuiltIns() {
	builtIns := map[string]*Grammar{
		".go": {
			Language: golang.GetLanguage(),
			Name:     "go",
			Source:   GrammarBuiltIn,
		},
		".ts": {
			Language: sitter_ts.GetLanguage(),
			Name:     "typescript",
			Source:   GrammarBuiltIn,
		},
		".tsx": {
			Language: sitter_tsx.GetLanguage(),
			Name:     "tsx",
			Source:   GrammarBuiltIn,
		},
		".js": {
			Language: javascript.GetLanguage(),
			Name:     "javascript",
			Source:   GrammarBuiltIn,
		},
		".jsx": {
			Language: javascript.GetLanguage(),
			Name:     "javascript",
			Source:   GrammarBuiltIn,
		},
		".mjs": {
			Language: javascript.GetLanguage(),
			Name:     "javascript",
			Source:   GrammarBuiltIn,
		},
		".py": {
			Language: python.GetLanguage(),
			Name:     "python",
			Source:   GrammarBuiltIn,
		},
	}

	for ext, g := range builtIns {
		r.grammars[ext] = g
	}
}

// RegisterPluginGrammar loads a tree-sitter grammar from a WASM file
// and registers it for the given extensions. Plugin grammars override built-ins.
// Note: loading grammars from WASM requires the tree-sitter WASM runtime
// which is not available in the Go CGO binding. This is a stub that returns
// an error — plugin-provided grammars will fall back to built-in grammars.
func (r *GrammarRegistry) RegisterPluginGrammar(grammarWASMPath string, extensions []string, pluginID string) error {
	if grammarWASMPath == "" {
		return nil // no grammar declared — use built-in
	}

	lang, err := loadWASMGrammar(grammarWASMPath)
	if err != nil {
		// Non-fatal: fall back to built-in grammar for these extensions.
		return fmt.Errorf("load grammar wasm %s (will use built-in fallback): %w", grammarWASMPath, err)
	}

	grammar := &Grammar{
		Language: lang,
		Name:     pluginID,
		Source:   GrammarPlugin,
		PluginID: pluginID,
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, ext := range extensions {
		r.grammars[ext] = grammar
	}

	return nil
}

// ForExtension returns the grammar for a file extension, or nil if none registered.
// ext should include the leading dot (e.g. ".go").
func (r *GrammarRegistry) ForExtension(ext string) *Grammar {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.grammars[ext]
}

// loadWASMGrammar attempts to load a tree-sitter grammar from a WASM file.
// The smacker/go-tree-sitter CGO binding does not support loading grammars
// from WASM files — this requires the tree-sitter WASM runtime (JS/Node only).
// This function is a stub that returns an error until a pure-Go WASM grammar
// loader is available.
func loadWASMGrammar(wasmPath string) (*sitter.Language, error) {
	if _, err := os.Stat(wasmPath); err != nil {
		return nil, fmt.Errorf("grammar file not found: %s", wasmPath)
	}
	// TODO: implement WASM grammar loading when a pure-Go tree-sitter WASM
	// runtime is available. Currently the CGO binding only supports grammars
	// compiled as native shared libraries or linked at build time.
	return nil, fmt.Errorf("WASM grammar loading not yet supported in Go CGO binding")
}
