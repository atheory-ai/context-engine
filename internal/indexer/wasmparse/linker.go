package wasmparse

import (
	"context"
	"fmt"

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

// parseImports reads a module's import section.
func parseImports(b []byte) ([]wimport, error) {
	if len(b) < 8 {
		return nil, fmt.Errorf("not a wasm module")
	}
	p := 8
	rd := func() uint64 {
		var r uint64
		var s uint
		for {
			x := b[p]
			p++
			r |= uint64(x&0x7f) << s
			if x&0x80 == 0 {
				break
			}
			s += 7
		}
		return r
	}
	var imports []wimport
	for p < len(b) {
		sec := b[p]
		p++
		size := int(rd())
		end := p + size
		if sec == 2 {
			n := int(rd())
			for i := 0; i < n; i++ {
				ml := int(rd())
				mod := string(b[p : p+ml])
				p += ml
				nl := int(rd())
				nm := string(b[p : p+nl])
				p += nl
				kind := b[p]
				p++
				imp := wimport{module: mod, name: nm, kind: kind}
				switch kind {
				case 0:
					rd() // typeidx
				case 1:
					p++ // reftype
					fl := b[p]
					p++
					rd()
					if fl&1 != 0 {
						rd()
					}
				case 2:
					fl := b[p]
					p++
					rd()
					if fl&1 != 0 {
						rd()
					}
				case 3:
					p++ // valtype
					imp.gmut = b[p]
					p++
				}
				imports = append(imports, imp)
			}
		}
		p = end
	}
	return imports, nil
}

// linkGrammar resolves a grammar's imports, instantiates a per-grammar glue
// module for the base/GOT globals, and returns the grammar wasm with its import
// module names rewritten (base/GOT globals → glue, everything else → core).
func (p *Parser) linkGrammar(ctx context.Context, name string, wasmBytes []byte, memBase, tableBase int32) ([]byte, error) {
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
			e := p.core.ExportedGlobal(im.name)
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
	if _, err := p.rt.InstantiateWithConfig(ctx, glue, wazero.NewModuleConfig().WithName(glueName)); err != nil {
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
