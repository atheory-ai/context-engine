package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestReadCatalogManifestVerifiesWASMHash(t *testing.T) {
	dir := t.TempDir()
	wasmPath := filepath.Join(dir, "plugin.wasm")
	wasm := []byte("wasm bytes")
	if err := os.WriteFile(wasmPath, wasm, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(wasm)
	manifest := `{"id":"example.plugin","name":"Example","version":"1.0.0","abi":{"name":"ce-plugin","version":4,"callConvention":"extism-input-output"},"capabilities":{"language":true,"role":false,"analyzers":[],"tools":[]},"language":{"extensions":[".example"]},"wasm_sha256":"` + hex.EncodeToString(digest[:]) + `"}`
	if err := os.WriteFile(wasmPath+".manifest.json", []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := ReadCatalogManifest(wasmPath)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.ID != "example.plugin" || catalog.Language == nil || catalog.Language.Extensions[0] != ".example" {
		t.Fatalf("catalog = %#v", catalog)
	}
	if err := os.WriteFile(wasmPath, []byte("changed wasm"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCatalogManifest(wasmPath); err == nil {
		t.Fatal("expected mismatched hash to fail")
	}
}
