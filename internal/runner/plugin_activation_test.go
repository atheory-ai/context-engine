package runner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atheory-ai/context-engine/internal/indexer"
	pluginruntime "github.com/atheory-ai/context-engine/internal/plugins/runtime"
)

func TestProjectExtensionsSkipsDependencyDirectories(t *testing.T) {
	root := t.TempDir()
	for path := range map[string]string{
		"main.php":                "<?php",
		"node_modules/library.ts": "export {}",
		"vendor/library.go":       "package library",
		".git/ignored.py":         "pass",
	} {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	extensions := projectExtensions(root)
	if _, ok := extensions[".php"]; !ok {
		t.Fatalf("extensions = %#v, want php", extensions)
	}
	for _, extension := range []string{".ts", ".go", ".py"} {
		if _, ok := extensions[extension]; ok {
			t.Fatalf("extensions = %#v, unexpectedly includes %s", extensions, extension)
		}
	}
}

func TestCandidateMatchesExtensions(t *testing.T) {
	manifest := pluginruntime.PluginManifest{Language: &pluginruntime.PluginLanguageInfo{Extensions: []string{".php", ".phtml"}}}
	if !candidateMatchesExtensions(manifest, map[string]struct{}{".php": {}}) {
		t.Fatal("expected php candidate to match")
	}
	if candidateMatchesExtensions(manifest, map[string]struct{}{".go": {}}) {
		t.Fatal("unexpected go match")
	}
}

func TestEmbeddedDefaultPluginCatalogsHaveRoutingExtensions(t *testing.T) {
	dataDir := t.TempDir()
	if err := indexer.ExtractDefaults(dataDir); err != nil {
		t.Fatal(err)
	}
	candidates, err := catalogPluginCandidates(filepath.Join(dataDir, "plugins", "defaults"), nil)
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]pluginCandidate, len(candidates))
	for _, candidate := range candidates {
		byID[candidate.manifest.ID] = candidate
	}
	typescript, ok := byID["com.atheory-ai.typescript"]
	if !ok {
		t.Skip("default plugin WASM is not embedded; run make bundle-default-plugins")
	}
	if !candidateMatchesExtensions(typescript.manifest, map[string]struct{}{".ts": {}}) {
		t.Fatalf("TypeScript extensions = %#v, want .ts routing", typescript.manifest.Language)
	}
	if typescript.manifest.ABI == nil || typescript.manifest.ABI.Version != 4 {
		t.Fatalf("TypeScript ABI = %#v, want v4", typescript.manifest.ABI)
	}
}
