package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atheory-ai/context-engine/internal/core"
)

const validManifestJSON = `{"id":"com.example.fixture","name":"Fixture Plugin","version":"0.1.0","abi":{"name":"ce-plugin","version":4,"callConvention":"extism-input-output"},"capabilities":{"language":true,"role":false,"analyzers":[],"tools":[]},"language":{"extensions":[".fixture"]}}`

var emptyWASMModule = []byte{
	0x00, 0x61, 0x73, 0x6d,
	0x01, 0x00, 0x00, 0x00,
}

var manifestExportOnlyWASM = []byte{
	0x00, 0x61, 0x73, 0x6d,
	0x01, 0x00, 0x00, 0x00,
	0x01, 0x05, 0x01, 0x60, 0x00, 0x01, 0x7f,
	0x03, 0x02, 0x01, 0x00,
	0x07, 0x16, 0x01, 0x12,
	0x63, 0x65, 0x5f, 0x70, 0x6c, 0x75, 0x67, 0x69, 0x6e,
	0x5f, 0x6d, 0x61, 0x6e, 0x69, 0x66, 0x65, 0x73, 0x74,
	0x00, 0x00,
	0x0a, 0x06, 0x01, 0x04, 0x00, 0x41, 0x00, 0x0b,
}

func TestValidateExportsFixtures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		wasm    []byte
		wantErr string
	}{
		{
			name:    "invalid wasm",
			wasm:    []byte("not wasm"),
			wantErr: "invalid wasm",
		},
		{
			name:    "missing manifest export",
			wasm:    emptyWASMModule,
			wantErr: "missing required export: ce_plugin_manifest",
		},
		{
			name: "manifest export present",
			wasm: manifestExportOnlyWASM,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateExports(tt.wasm)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateExports() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateExports() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestCollectExportsValidFixture(t *testing.T) {
	t.Parallel()

	wasmBytes := extismManifestWASM(validManifestJSON)
	if err := validateExports(wasmBytes); err != nil {
		t.Fatalf("validateExports(valid fixture): %v", err)
	}

	exports, err := collectExports(wasmBytes)
	if err != nil {
		t.Fatalf("collectExports(valid fixture): %v", err)
	}
	if !exports["ce_plugin_manifest"] {
		t.Fatalf("exports missing ce_plugin_manifest: %#v", exports)
	}
}

func TestPluginABIInfoUnmarshalAcceptsCamelAndSnakeCallConvention(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		`{"name":"ce-plugin","version":4,"callConvention":"javy-stream-io"}`,
		`{"name":"ce-plugin","version":4,"call_convention":"javy-stream-io"}`,
	} {
		var abi PluginABIInfo
		if err := json.Unmarshal([]byte(raw), &abi); err != nil {
			t.Fatalf("UnmarshalJSON(%s): %v", raw, err)
		}
		if abi.CallConvention != callConventionJavyStreamIO {
			t.Fatalf("CallConvention = %q, want %q", abi.CallConvention, callConventionJavyStreamIO)
		}
	}
}

func TestValidateManifestABI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		abi     *PluginABIInfo
		wantErr string
	}{
		{name: "missing ABI", wantErr: "missing ABI declaration"},
		{
			name: "stream io",
			abi:  &PluginABIInfo{Name: "ce-plugin", Version: 4, CallConvention: callConventionJavyStreamIO},
		},
		{
			name: "extism input output",
			abi:  &PluginABIInfo{Name: "ce-plugin", Version: 4, CallConvention: callConventionExtismInputOutput},
		},
		{
			name:    "stale v2 plugin",
			abi:     &PluginABIInfo{Name: "ce-plugin", Version: 2, CallConvention: callConventionExtismInputOutput},
			wantErr: "rebuild with the CE Plugin SDK",
		},
		{
			name:    "unsupported convention",
			abi:     &PluginABIInfo{Name: "ce-plugin", Version: 4, CallConvention: "unknown"},
			wantErr: "call convention",
		},
		{
			name:    "unsupported name",
			abi:     &PluginABIInfo{Name: "argent-plugin", Version: 4, CallConvention: callConventionJavyStreamIO},
			wantErr: "name",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateManifestABI(tt.abi)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateManifestABI() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateManifestABI() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadRejectsStreamPluginsUnlessDevelopmentEnabled(t *testing.T) {
	ch := core.NewAppChannels()
	rt, err := New(t.TempDir(), &ch)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	defer rt.Close()

	streamManifest := strings.Replace(validManifestJSON, callConventionExtismInputOutput, callConventionJavyStreamIO, 1)
	path := writeWASMFixture(t, "stream-manifest.wasm", extismManifestWASM(streamManifest))
	if _, err := rt.Load(context.Background(), path, nil); err == nil || !strings.Contains(err.Error(), "development-only Javy") {
		t.Fatalf("Load() error = %v, want production stream rejection", err)
	}

	rt.SetAllowDevStreamPlugins(true)
	plugin, err := rt.Load(context.Background(), path, nil)
	if err != nil {
		t.Fatalf("Load() with development stream enabled: %v", err)
	}
	defer plugin.Close()
}

func TestNewExtismManifestSandboxLimits(t *testing.T) {
	t.Parallel()

	manifest := newExtismManifest(manifestExportOnlyWASM)
	if len(manifest.Wasm) != 1 {
		t.Fatalf("manifest.Wasm len = %d, want 1", len(manifest.Wasm))
	}
	if len(manifest.AllowedHosts) != 0 {
		t.Fatalf("AllowedHosts = %#v, want empty", manifest.AllowedHosts)
	}
	if len(manifest.AllowedPaths) != 0 {
		t.Fatalf("AllowedPaths = %#v, want empty", manifest.AllowedPaths)
	}
}

func TestLoadValidFixtureCachesAndReloads(t *testing.T) {
	ch := core.NewAppChannels()
	rt, err := New(t.TempDir(), &ch)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	defer func() {
		if err := rt.Close(); err != nil {
			t.Fatalf("Close(): %v", err)
		}
	}()

	wasmBytes := extismManifestWASM(validManifestJSON)
	wasmPath := writeWASMFixture(t, "valid-manifest.wasm", wasmBytes)
	wasmHash := WASMHash(wasmBytes)

	plugin, err := rt.Load(context.Background(), wasmPath, map[string]any{"enabled": true})
	if err != nil {
		t.Fatalf("Load(%s): %v", wasmPath, err)
	}
	defer plugin.Close()

	if got := string(plugin.ID()); got != "com.example.fixture" {
		t.Fatalf("plugin.ID() = %q", got)
	}
	if got := plugin.Name(); got != "Fixture Plugin" {
		t.Fatalf("plugin.Name() = %q", got)
	}
	if got := plugin.Version(); got != "0.1.0" {
		t.Fatalf("plugin.Version() = %q", got)
	}
	if !rt.cache.IsCached(wasmHash) {
		t.Fatalf("cache miss for loaded fixture hash %s", wasmHash)
	}

	metaBefore := readCacheMeta(t, rt.cache, wasmHash)
	oldLastUsed := time.Now().Add(-time.Hour).UnixMilli()
	metaBefore.LastUsed = oldLastUsed
	if err := rt.cache.writeMeta(wasmHash, metaBefore); err != nil {
		t.Fatalf("writeMeta(old last_used): %v", err)
	}

	reloaded, err := rt.Load(context.Background(), wasmPath, nil)
	if err != nil {
		t.Fatalf("second Load(%s): %v", wasmPath, err)
	}
	defer reloaded.Close()

	metaAfter := readCacheMeta(t, rt.cache, wasmHash)
	if metaAfter.LastUsed <= oldLastUsed {
		t.Fatalf("cache LastUsed = %d, want > %d", metaAfter.LastUsed, oldLastUsed)
	}
}

func TestLanguagePluginPoolExpandsToConcurrentIndexWorkers(t *testing.T) {
	ch := core.NewAppChannels()
	rt, err := New(t.TempDir(), &ch)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	defer rt.Close()

	path := writeWASMFixture(t, "valid-manifest.wasm", extismManifestWASM(validManifestJSON))
	loaded, err := rt.Load(context.Background(), path, nil)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	defer loaded.Close()

	plugin := loaded.(*pluginInstance)
	pool := plugin.indexPool
	workers := pool.max
	if workers > 4 {
		workers = 4
	}
	if workers < 2 {
		t.Skip("single-core runtime cannot demonstrate parallel extraction")
	}

	started := make(chan struct{}, workers)
	release := make(chan struct{})
	var wg sync.WaitGroup
	seen := map[*pluginInstance]struct{}{}
	var seenMu sync.Mutex
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := pool.withInstance(context.Background(), func(instance *pluginInstance) error {
				seenMu.Lock()
				seen[instance] = struct{}{}
				seenMu.Unlock()
				started <- struct{}{}
				<-release
				return nil
			})
			if err != nil {
				t.Errorf("withInstance: %v", err)
			}
		}()
	}
	for range workers {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("pool did not provision all concurrent instances")
		}
	}
	close(release)
	wg.Wait()

	if len(seen) != workers {
		t.Fatalf("independent instances = %d, want %d", len(seen), workers)
	}

	oldBase := plugin.wasm
	if err := pool.Trim(); err != nil {
		t.Fatalf("Trim(): %v", err)
	}
	if pool.created != 1 || len(pool.instances) != 1 {
		t.Fatalf("trimmed pool = %d created, %d instances; want one", pool.created, len(pool.instances))
	}
	if plugin.wasm == oldBase {
		t.Fatal("Trim retained the bulk-grown base instance")
	}
	if err := pool.withInstance(context.Background(), func(instance *pluginInstance) error {
		if instance != plugin {
			t.Fatal("trimmed pool did not serve its replacement base instance")
		}
		return nil
	}); err != nil {
		t.Fatalf("withInstance after Trim(): %v", err)
	}
}

func TestLoadInvalidManifestFixtureFailsAfterExportValidation(t *testing.T) {
	ch := core.NewAppChannels()
	rt, err := New(t.TempDir(), &ch)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	defer rt.Close()

	wasmBytes := extismManifestWASM("not-json")
	path := writeWASMFixture(t, "invalid-manifest.wasm", wasmBytes)

	if err := validateExports(wasmBytes); err != nil {
		t.Fatalf("fixture should pass export validation: %v", err)
	}

	_, err = rt.Load(context.Background(), path, nil)
	if err == nil {
		t.Fatal("Load() error = nil, want invalid manifest/runtime call failure")
	}
	msg := err.Error()
	if strings.Contains(msg, "missing required export") || strings.Contains(msg, "invalid wasm") {
		t.Fatalf("Load() error = %v, want failure after export validation", err)
	}
	if !strings.Contains(msg, "create extism plugin") &&
		!strings.Contains(msg, "call ce_plugin_manifest") &&
		!strings.Contains(msg, "parse plugin manifest") {
		t.Fatalf("Load() error = %v, want Extism load/call or manifest parse failure", err)
	}
}

func TestCompilationCacheBehavior(t *testing.T) {
	t.Parallel()

	cache, err := NewCompilationCache(t.TempDir())
	if err != nil {
		t.Fatalf("NewCompilationCache(): %v", err)
	}
	defer cache.Close()

	hash := WASMHash([]byte("cache-behavior"))
	if cache.IsCached(hash) {
		t.Fatal("IsCached() = true before metadata write")
	}

	staleLastUsed := time.Now().AddDate(0, 0, -(cacheTTLDays + 1)).UnixMilli()
	if err := cache.writeMeta(hash, CacheMeta{
		WASMHash:   hash,
		PluginName: "fixture",
		Version:    "0.0.1",
		CachedAt:   staleLastUsed,
		LastUsed:   staleLastUsed,
	}); err != nil {
		t.Fatalf("writeMeta(): %v", err)
	}
	if !cache.IsCached(hash) {
		t.Fatal("IsCached() = false after metadata write")
	}

	cache.TouchLastUsed(hash)
	touched := readCacheMeta(t, cache, hash)
	if touched.LastUsed <= staleLastUsed {
		t.Fatalf("TouchLastUsed LastUsed = %d, want > %d", touched.LastUsed, staleLastUsed)
	}

	touched.LastUsed = staleLastUsed
	if err := cache.writeMeta(hash, touched); err != nil {
		t.Fatalf("writeMeta(stale): %v", err)
	}
	cache.Evict()
	eventually(t, time.Second, func() bool {
		return !cache.IsCached(hash)
	})
}

func writeWASMFixture(t *testing.T, name string, wasmBytes []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, wasmBytes, 0644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
	return path
}

func readCacheMeta(t *testing.T, cache *CompilationCache, wasmHash string) CacheMeta {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(cache.hashDir(wasmHash), "meta.json"))
	if err != nil {
		t.Fatalf("read cache meta: %v", err)
	}
	var meta CacheMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("unmarshal cache meta: %v", err)
	}
	return meta
}

func eventually(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !condition() {
		t.Fatal("condition not met before timeout")
	}
}

func extismManifestWASM(manifest string) []byte {
	var out bytes.Buffer
	out.Write([]byte{0x00, 0x61, 0x73, 0x6d})
	out.Write([]byte{0x01, 0x00, 0x00, 0x00})

	var types bytes.Buffer
	writeULEB(&types, 4)
	types.WriteByte(0x60)
	writeULEB(&types, 1)
	types.WriteByte(0x7e)
	writeULEB(&types, 1)
	types.WriteByte(0x7e)
	types.WriteByte(0x60)
	writeULEB(&types, 2)
	types.WriteByte(0x7e)
	types.WriteByte(0x7f)
	writeULEB(&types, 0)
	types.WriteByte(0x60)
	writeULEB(&types, 2)
	types.WriteByte(0x7e)
	types.WriteByte(0x7e)
	writeULEB(&types, 0)
	types.WriteByte(0x60)
	writeULEB(&types, 0)
	writeULEB(&types, 0)
	writeSection(&out, 1, types.Bytes())

	var imports bytes.Buffer
	writeULEB(&imports, 3)
	writeName(&imports, "extism:host/env")
	writeName(&imports, "alloc")
	imports.WriteByte(0x00)
	writeULEB(&imports, 0)
	writeName(&imports, "extism:host/env")
	writeName(&imports, "store_u8")
	imports.WriteByte(0x00)
	writeULEB(&imports, 1)
	writeName(&imports, "extism:host/env")
	writeName(&imports, "output_set")
	imports.WriteByte(0x00)
	writeULEB(&imports, 2)
	writeSection(&out, 2, imports.Bytes())

	var funcs bytes.Buffer
	writeULEB(&funcs, 1)
	writeULEB(&funcs, 3)
	writeSection(&out, 3, funcs.Bytes())

	var exports bytes.Buffer
	writeULEB(&exports, 1)
	writeName(&exports, "ce_plugin_manifest")
	exports.WriteByte(0x00)
	writeULEB(&exports, 3)
	writeSection(&out, 7, exports.Bytes())

	var body bytes.Buffer
	writeULEB(&body, 1)
	writeULEB(&body, 1)
	body.WriteByte(0x7e)
	body.WriteByte(0x42)
	writeSLEB(&body, int64(len(manifest)))
	body.WriteByte(0x10)
	writeULEB(&body, 0)
	body.WriteByte(0x21)
	writeULEB(&body, 0)
	for i, b := range []byte(manifest) {
		body.WriteByte(0x20)
		writeULEB(&body, 0)
		body.WriteByte(0x42)
		writeSLEB(&body, int64(i))
		body.WriteByte(0x7c)
		body.WriteByte(0x41)
		writeSLEB(&body, int64(b))
		body.WriteByte(0x10)
		writeULEB(&body, 1)
	}
	body.WriteByte(0x20)
	writeULEB(&body, 0)
	body.WriteByte(0x42)
	writeSLEB(&body, int64(len(manifest)))
	body.WriteByte(0x10)
	writeULEB(&body, 2)
	body.WriteByte(0x0b)

	var code bytes.Buffer
	writeULEB(&code, 1)
	writeULEB(&code, uint64(body.Len()))
	code.Write(body.Bytes())
	writeSection(&out, 10, code.Bytes())

	return out.Bytes()
}

func writeSection(out *bytes.Buffer, id byte, payload []byte) {
	out.WriteByte(id)
	writeULEB(out, uint64(len(payload)))
	out.Write(payload)
}

func writeName(out *bytes.Buffer, name string) {
	writeULEB(out, uint64(len(name)))
	out.WriteString(name)
}

func writeULEB(out *bytes.Buffer, v uint64) {
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		out.WriteByte(b)
		if v == 0 {
			return
		}
	}
}

func writeSLEB(out *bytes.Buffer, v int64) {
	for {
		b := byte(v & 0x7f)
		v >>= 7
		signBitSet := b&0x40 != 0
		done := (v == 0 && !signBitSet) || (v == -1 && signBitSet)
		if !done {
			b |= 0x80
		}
		out.WriteByte(b)
		if done {
			return
		}
	}
}
