package wasmparse

import "errors"

// errMalformedWASM is returned by the hand-written wasm parsers when untrusted
// plugin-grammar bytes are truncated or structurally invalid. These parsers run
// on modules supplied at runtime via RegisterGrammar, so they must reject bad
// input with an error — never panic (see fuzz_test.go).
var errMalformedWASM = errors.New("malformed or truncated wasm module")

// reader is a bounds-checked cursor over untrusted wasm bytes. After any
// out-of-range access it sets a sticky bad flag; all subsequent reads return
// zero values. Callers walk the structure and check bad once at the end instead
// of guarding every access, which keeps the parsers panic-free on hostile input.
type reader struct {
	b   []byte
	p   int
	bad bool
}

// u8 reads one byte.
func (r *reader) u8() byte {
	if r.bad || r.p >= len(r.b) {
		r.bad = true
		return 0
	}
	v := r.b[r.p]
	r.p++
	return v
}

// uleb reads an unsigned LEB128, guarding against a run that never terminates.
func (r *reader) uleb() uint64 {
	var res uint64
	var s uint
	for {
		if r.bad || r.p >= len(r.b) {
			r.bad = true
			return 0
		}
		x := r.b[r.p]
		r.p++
		res |= uint64(x&0x7f) << s
		if x&0x80 == 0 {
			return res
		}
		if s += 7; s > 63 {
			r.bad = true
			return 0
		}
	}
}

// take returns the next n bytes (a sub-slice of the backing array), or nil if n
// is negative or runs past the end.
func (r *reader) take(n int) []byte {
	if r.bad || n < 0 || r.p+n > len(r.b) || r.p+n < r.p {
		r.bad = true
		return nil
	}
	v := r.b[r.p : r.p+n]
	r.p += n
	return v
}

// str reads a length-prefixed UTF-8 name.
func (r *reader) str() string { return string(r.take(int(r.uleb()))) }

// skip advances n bytes.
func (r *reader) skip(n int) {
	if r.bad || n < 0 || r.p+n > len(r.b) || r.p+n < r.p {
		r.bad = true
		return
	}
	r.p += n
}

// seek moves to an absolute offset that must lie within [0, len]. len itself is
// valid — it marks a clean end-of-module.
func (r *reader) seek(p int) {
	if p < 0 || p > len(r.b) {
		r.bad = true
		return
	}
	r.p = p
}
