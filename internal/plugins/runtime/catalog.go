package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

// CatalogManifest is the build-time, hash-bound form of PluginManifest. It is
// read without instantiating WASM so CE can plan activation cheaply.
type CatalogManifest struct {
	PluginManifest
	WASMSHA256 string `json:"wasm_sha256"`
}

// ReadCatalogManifest reads <plugin>.wasm.manifest.json and verifies that it
// describes exactly the supplied module. A missing sidecar is reported with
// os.ErrNotExist so legacy plugins can retain their eager-load compatibility.
func ReadCatalogManifest(wasmPath string) (CatalogManifest, error) {
	data, err := os.ReadFile(wasmPath + ".manifest.json")
	if err != nil {
		return CatalogManifest{}, err
	}
	var manifest CatalogManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return CatalogManifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	if manifest.ID == "" || manifest.WASMSHA256 == "" {
		return CatalogManifest{}, fmt.Errorf("manifest requires id and wasm_sha256")
	}
	wasm, err := os.ReadFile(wasmPath)
	if err != nil {
		return CatalogManifest{}, err
	}
	digest := sha256.Sum256(wasm)
	if manifest.WASMSHA256 != hex.EncodeToString(digest[:]) {
		return CatalogManifest{}, fmt.Errorf("manifest wasm_sha256 does not match %s", wasmPath)
	}
	if err := validateManifestABI(manifest.ABI); err != nil {
		return CatalogManifest{}, fmt.Errorf("manifest ABI: %w", err)
	}
	return manifest, nil
}
