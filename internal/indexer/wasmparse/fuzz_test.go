package wasmparse

import "testing"

// The functions below parse raw bytes from UNTRUSTED plugin grammar wasm
// (registered at runtime via RegisterGrammar). A malformed or hostile module
// must never crash the indexer, so these fuzz targets assert the byte-parsers
// never panic on arbitrary input. RegisterGrammar also wraps them in a recover
// guard, but the parsers themselves are hardened here so the guard is a backstop,
// not the primary defense.
//
// Run one target:  go test ./internal/indexer/wasmparse -run=^$ -fuzz=FuzzParseImports -fuzztime=20s
// The seed corpus (real grammar bytes + degenerate headers) also runs as an
// ordinary unit test on every `go test`.

// wasmHeader is a minimal valid wasm module preamble ("\0asm" + version 1).
var wasmHeader = []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}

// seedCorpus feeds each fuzz target the real grammar bytes plus degenerate
// inputs that historically tripped bounds bugs (empty, header-only, truncated).
func seedCorpus(f *testing.F) {
	f.Helper()
	for _, s := range [][]byte{
		nil,
		{},
		wasmHeader,
		wasmHeader[:4],
		append(append([]byte{}, wasmHeader...), 0x02, 0x01, 0x10), // import section id, len=1, but only 1 byte
		goGrammarWASM,     // scanner-less grammar (env imports only)
		pythonGrammarWASM, // scanner grammar (also imports GOT.mem + libc funcs)
		coreWASM,          // the tree-sitter core module (many sections/exports)
	} {
		f.Add(s)
	}
}

func FuzzParseImports(f *testing.F) {
	seedCorpus(f)
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = parseImports(b) // must not panic
	})
}

func FuzzGrammarEntryName(f *testing.F) {
	seedCorpus(f)
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = grammarEntryName(b)
	})
}

func FuzzDylinkMemInfo(f *testing.F) {
	seedCorpus(f)
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _, _, _ = dylinkMemInfo(b)
	})
}

func FuzzPatchImports(f *testing.F) {
	seedCorpus(f)
	f.Fuzz(func(t *testing.T, b []byte) {
		// identity rename — exercises the section/name walk, not the callback
		_, _ = patchImports(b, func(module, name string) string { return module })
	})
}
