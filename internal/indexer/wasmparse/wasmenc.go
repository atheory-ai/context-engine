package wasmparse

import "fmt"

// Minimal WASM binary encoding for the two glue modules the dynamic linker
// synthesizes: a table.grow helper and the per-load env module. Hand-emitting
// these avoids depending on a wasm toolchain at engine build time.

func uleb(v uint64) []byte {
	var out []byte
	for {
		c := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			c |= 0x80
		}
		out = append(out, c)
		if v == 0 {
			break
		}
	}
	return out
}

func sleb(v int32) []byte {
	var out []byte
	for more := true; more; {
		b := byte(v & 0x7f)
		v >>= 7
		if (v == 0 && b&0x40 == 0) || (v == -1 && b&0x40 != 0) {
			more = false
		} else {
			b |= 0x80
		}
		out = append(out, b)
	}
	return out
}

func vec(items [][]byte) []byte {
	out := uleb(uint64(len(items)))
	for _, it := range items {
		out = append(out, it...)
	}
	return out
}

func wname(s string) []byte { return append(uleb(uint64(len(s))), []byte(s)...) }

func wsection(id byte, body []byte) []byte {
	return append(append([]byte{id}, uleb(uint64(len(body)))...), body...)
}

func wmodule(sections ...[]byte) []byte {
	out := []byte{0, 'a', 's', 'm', 1, 0, 0, 0}
	for _, s := range sections {
		out = append(out, s...)
	}
	return out
}

// dlModule imports core."__indirect_function_table" and exports
// dl_table_size()->i32 and dl_table_grow(i32)->i32 (table.grow, reference-types).
func dlModule() []byte {
	t0 := append([]byte{0x60}, append(uleb(0), append(uleb(1), byte(0x7f))...)...)         // ()->(i32)
	t1 := append([]byte{0x60}, append(append(uleb(1), 0x7f), append(uleb(1), 0x7f)...)...) // (i32)->(i32)
	typesec := wsection(1, vec([][]byte{t0, t1}))

	imp := append(wname("core"), wname("__indirect_function_table")...)
	imp = append(imp, 0x01, 0x70, 0x00)
	imp = append(imp, uleb(0)...) // limits flag=0 min=0
	importsec := wsection(2, vec([][]byte{imp}))

	funcsec := wsection(3, vec([][]byte{uleb(0), uleb(1)}))

	e0 := append(wname("dl_table_size"), 0x00)
	e0 = append(e0, uleb(0)...)
	e1 := append(wname("dl_table_grow"), 0x00)
	e1 = append(e1, uleb(1)...)
	exportsec := wsection(7, vec([][]byte{e0, e1}))

	body0 := []byte{0x00, 0xFC, 0x10, 0x00, 0x0B}                         // table.size 0; end
	body1 := []byte{0x00, 0xD0, 0x70, 0x20, 0x00, 0xFC, 0x0F, 0x00, 0x0B} // ref.null func; local.get 0; table.grow 0; end
	code0 := append(uleb(uint64(len(body0))), body0...)
	code1 := append(uleb(uint64(len(body1))), body1...)
	codesec := wsection(10, vec([][]byte{code0, code1}))

	return wmodule(typesec, importsec, funcsec, exportsec, codesec)
}

// patchImports rewrites each import's module name via rename(module, name),
// re-emitting the import section (and only it). This routes a grammar's "env"/
// "GOT.mem" imports to the core and its per-grammar glue.
func patchImports(wasm []byte, rename func(module, name string) string) ([]byte, error) {
	if len(wasm) < 8 {
		return nil, fmt.Errorf("not a wasm module")
	}
	p := 8
	read := func() uint64 {
		var r uint64
		var s uint
		for {
			x := wasm[p]
			p++
			r |= uint64(x&0x7f) << s
			if x&0x80 == 0 {
				break
			}
			s += 7
		}
		return r
	}
	out := append([]byte{}, wasm[:8]...)
	for p < len(wasm) {
		sec := wasm[p]
		p++
		size := int(read())
		body := wasm[p : p+size]
		p += size
		if sec != 2 { // not the import section — copy verbatim
			out = append(out, sec)
			out = append(out, uleb(uint64(size))...)
			out = append(out, body...)
			continue
		}
		// rebuild import section, renaming matching module names
		q := 0
		bread := func() uint64 {
			var r uint64
			var s uint
			for {
				x := body[q]
				q++
				r |= uint64(x&0x7f) << s
				if x&0x80 == 0 {
					break
				}
				s += 7
			}
			return r
		}
		n := int(bread())
		entries := make([][]byte, 0, n)
		for i := 0; i < n; i++ {
			ml := int(bread())
			mod := string(body[q : q+ml])
			q += ml
			nl := int(bread())
			nm := body[q : q+nl]
			q += nl
			kind := body[q]
			q++
			var desc []byte
			switch kind {
			case 0: // func: typeidx
				ds := q
				bread()
				desc = body[ds:q]
			case 1: // table: reftype + limits
				ds := q
				q++ // reftype
				fl := body[q]
				q++
				bread()
				if fl&1 != 0 {
					bread()
				}
				desc = body[ds:q]
			case 2: // mem: limits
				ds := q
				fl := body[q]
				q++
				bread()
				if fl&1 != 0 {
					bread()
				}
				desc = body[ds:q]
			case 3: // global: valtype + mut
				desc = body[q : q+2]
				q += 2
			default:
				return nil, fmt.Errorf("unknown import kind %d", kind)
			}
			useMod := rename(mod, string(nm))
			e := append(wname(useMod), wname(string(nm))...)
			e = append(e, kind)
			e = append(e, desc...)
			entries = append(entries, e)
		}
		nb := vec(entries)
		out = append(out, sec)
		out = append(out, uleb(uint64(len(nb)))...)
		out = append(out, nb...)
	}
	return out, nil
}

// dylinkMemInfo parses the dylink.0 MEM_INFO subsection of a grammar side
// module, returning mem_size, mem_align (bytes), and table_size.
func dylinkMemInfo(b []byte) (memSize, memAlign, tableSize int32, err error) {
	if len(b) < 8 {
		return 0, 0, 0, fmt.Errorf("not a wasm module")
	}
	p := 8
	readU := func() uint64 {
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
	for p < len(b) {
		sec := b[p]
		p++
		size := int(readU())
		end := p + size
		if sec == 0 { // custom
			np := p
			nlen := int(func() uint64 {
				var r uint64
				var s uint
				for {
					x := b[np]
					np++
					r |= uint64(x&0x7f) << s
					if x&0x80 == 0 {
						break
					}
					s += 7
				}
				return r
			}())
			nm := string(b[np : np+nlen])
			np += nlen
			if nm == "dylink.0" {
				q := np
				for q < end {
					sub := b[q]
					q++
					// subsection length
					var sl uint64
					var s uint
					for {
						x := b[q]
						q++
						sl |= uint64(x&0x7f) << s
						if x&0x80 == 0 {
							break
						}
						s += 7
					}
					sq := q
					if sub == 1 { // WASM_DYLINK_MEM_INFO
						rd := func() uint64 {
							var r uint64
							var s uint
							for {
								x := b[sq]
								sq++
								r |= uint64(x&0x7f) << s
								if x&0x80 == 0 {
									break
								}
								s += 7
							}
							return r
						}
						ms := rd()
						maExp := rd()
						ts := rd()
						_ = rd() // table_align exp
						return int32(ms), int32(1) << maExp, int32(ts), nil
					}
					q += int(sl)
				}
			}
		}
		p = end
	}
	return 0, 0, 0, fmt.Errorf("no dylink.0 MEM_INFO")
}
