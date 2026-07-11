package wasmparse

import (
	"context"
	"fmt"
	"strings"

	"github.com/tetratelabs/wazero"
)

// This file implements the dynamic linker for tree-sitter grammar side modules
// (emscripten/LLVM dylink). A grammar imports from module "env" (memory, table,
// libc funcs, __stack_pointer, and the per-load __memory_base/__table_base) and,
// for scanner grammars, "GOT.mem" (addresses of core data symbols).
//
// The core exports memory, __indirect_function_table, the libc functions, and
// __stack_pointer under the same names a grammar imports, so those imports are
// routed straight to "core". Only the per-load base globals and the GOT.mem
// address globals can't come from core directly — a small per-grammar glue
// module supplies those as constants. See docs/specs/18-spec-wasm-grammar-loader.md.

type wimport struct {
	module, name string
	kind         byte // 0 func, 1 table, 2 mem, 3 global
	gmut         byte // global mutability (kind 3)
}

// parseImports reads a module's import section. Input is untrusted (a plugin
// grammar), so it must never panic — see reader and fuzz_test.go.
func parseImports(b []byte) ([]wimport, error) {
	if len(b) < 8 {
		return nil, fmt.Errorf("not a wasm module")
	}
	r := &reader{b: b, p: 8}
	var imports []wimport
	for r.p < len(b) && !r.bad {
		sec := r.u8()
		size := int(r.uleb())
		end := r.p + size
		if sec == 2 {
			n := int(r.uleb())
			for i := 0; i < n && !r.bad; i++ {
				mod := r.str()
				nm := r.str()
				kind := r.u8()
				imp := wimport{module: mod, name: nm, kind: kind}
				switch kind {
				case 0:
					r.uleb() // typeidx
				case 1:
					r.skip(1) // reftype
					fl := r.u8()
					r.uleb()
					if fl&1 != 0 {
						r.uleb()
					}
				case 2:
					fl := r.u8()
					r.uleb()
					if fl&1 != 0 {
						r.uleb()
					}
				case 3:
					r.skip(1) // valtype
					imp.gmut = r.u8()
				}
				imports = append(imports, imp)
			}
		}
		if r.bad || end < r.p || end > len(b) {
			break
		}
		r.seek(end)
	}
	if r.bad {
		return nil, errMalformedWASM
	}
	return imports, nil
}

// grammarEntryName finds the grammar's language name by scanning its export
// section for a function named "tree_sitter_<name>" and returning <name>.
func grammarEntryName(b []byte) (string, error) {
	if len(b) < 8 {
		return "", fmt.Errorf("not a wasm module")
	}
	r := &reader{b: b, p: 8}
	const prefix = "tree_sitter_"
	for r.p < len(b) && !r.bad {
		sec := r.u8()
		size := int(r.uleb())
		end := r.p + size
		if sec == 7 { // export section
			n := int(r.uleb())
			for i := 0; i < n && !r.bad; i++ {
				name := r.str()
				kind := r.u8()
				r.uleb() // index
				if kind == 0 && strings.HasPrefix(name, prefix) {
					return name[len(prefix):], nil
				}
			}
		}
		if r.bad || end < r.p || end > len(b) {
			break
		}
		r.seek(end)
	}
	return "", fmt.Errorf("no tree_sitter_* export (not a tree-sitter grammar?)")
}

// linkGrammar resolves a grammar's imports, instantiates a per-grammar glue
// module for the base/GOT globals, and returns the grammar wasm with its import
// module names rewritten (base/GOT globals → glue, everything else → core).
func (in *instance) linkGrammar(ctx context.Context, name string, wasmBytes []byte, memBase, tableBase int32) ([]byte, error) {
	imports, err := parseImports(wasmBytes)
	if err != nil {
		return nil, err
	}
	glueName := "glue__" + name

	type gg struct {
		name string
		val  int32
		mut  bool
	}
	var globals []gg
	seen := map[string]bool{}
	for _, im := range imports {
		if im.kind != 3 {
			continue
		}
		var g gg
		switch {
		case im.name == "__memory_base":
			g = gg{"__memory_base", memBase, false}
		case im.name == "__table_base":
			g = gg{"__table_base", tableBase, false}
		case im.module == "GOT.mem":
			e := in.core.ExportedGlobal(im.name)
			if e == nil {
				return nil, fmt.Errorf("grammar %s: core does not export GOT.mem symbol %q", name, im.name)
			}
			g = gg{im.name, int32(e.Get()), im.gmut == 1}
		default:
			continue // shared globals (e.g. __stack_pointer) come from core
		}
		if seen[g.name] {
			continue
		}
		seen[g.name] = true
		globals = append(globals, g)
	}

	// Build and instantiate the glue module (const globals only).
	defs := make([][]byte, 0, len(globals))
	exports := make([][]byte, 0, len(globals))
	for i, g := range globals {
		defs = append(defs, constI32Global(g.val, g.mut))
		e := append(wname(g.name), 0x03)
		exports = append(exports, append(e, uleb(uint64(i))...))
	}
	glue := wmodule(wsection(6, vec(defs)), wsection(7, vec(exports)))
	if _, err := in.rt.InstantiateWithConfig(ctx, glue, wazero.NewModuleConfig().WithName(glueName)); err != nil {
		return nil, fmt.Errorf("grammar %s glue: %w", name, err)
	}

	// Route base/GOT globals to the glue; everything else (memory, table, libc
	// funcs, __stack_pointer) to the core by matching export name.
	rename := func(mod, nm string) string {
		if nm == "__memory_base" || nm == "__table_base" || mod == "GOT.mem" {
			return glueName
		}
		return "core"
	}
	return patchImports(wasmBytes, rename)
}

func constI32Global(v int32, mutable bool) []byte {
	mut := byte(0)
	if mutable {
		mut = 1
	}
	g := []byte{0x7f, mut, 0x41}
	g = append(g, sleb(v)...)
	return append(g, 0x0b)
}
