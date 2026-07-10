#!/usr/bin/env bash
# Rebuild the embedded tree-sitter WASM: the core (compiled once, embedded and
# reused across every platform binary) and the default grammar side modules.
#
# Reproducible: the tree-sitter core (v0.24.7) and grammar sources come from the
# pinned malivvan/tree-sitter module, fetched into the Go module cache. Requires
# `zig` (0.13.x) and `go` on PATH.
#
# Usage:  ./build.sh          # rebuilds all wasm in this directory
#         VERIFY=1 ./build.sh # rebuild to a temp dir and diff checksums only
#
# See docs/specs/18-spec-wasm-grammar-loader.md for the architecture.
set -euo pipefail

SRC_MODULE="github.com/malivvan/tree-sitter@v0.0.1"
HERE="$(cd "$(dirname "$0")" && pwd)"
OUT="${VERIFY:+$(mktemp -d)}"; OUT="${OUT:-$HERE}"

command -v zig >/dev/null || { echo "zig not found (need 0.13.x)"; exit 1; }
command -v go  >/dev/null || { echo "go not found"; exit 1; }

# Fetch the pinned tree-sitter core + grammar sources.
go mod download "$SRC_MODULE" 2>/dev/null || true
SRC="$(go env GOMODCACHE)/github.com/malivvan/tree-sitter@v0.0.1/src"
[ -d "$SRC" ] || { echo "sources not found at $SRC"; exit 1; }

WORK="$(mktemp -d)"; trap 'rm -rf "$WORK"' EXIT

# Table-growability patch: wasm-ld emits the indirect function table with a fixed
# max; drop the max so the dynamic linker can grow it for grammar side modules.
cat > "$WORK/patch.go" <<'GO'
package main
import ("fmt";"os")
func uleb(b []byte,p *int) uint64 { var r uint64; var s uint; for { x:=b[*p]; *p++; r|=uint64(x&0x7f)<<s; if x&0x80==0{break}; s+=7 }; return r }
func enc(v uint64) []byte { var o []byte; for { c:=byte(v&0x7f); v>>=7; if v!=0{c|=0x80}; o=append(o,c); if v==0{break} }; return o }
func main(){
	b,_:=os.ReadFile(os.Args[1]); p:=8; out:=append([]byte{},b[:8]...)
	for p<len(b){ s0:=p; sec:=b[p]; p++; n:=int(uleb(b,&p)); body:=b[p:p+n]; p+=n
		if sec!=4 { out=append(out,b[s0:p]...); continue }
		q:=0; cnt:=int(uleb(body,&q)); nb:=append([]byte{},enc(uint64(cnt))...)
		for i:=0;i<cnt;i++{ rt:=body[q]; q++; fl:=body[q]; q++; mn:=uleb(body,&q); if fl&1!=0{uleb(body,&q)}; nb=append(nb,rt,0x00); nb=append(nb,enc(mn)...) }
		out=append(out,sec); out=append(out,enc(uint64(len(nb)))...); out=append(out,nb...)
	}
	os.WriteFile(os.Args[2],out,0644); fmt.Println("patched",os.Args[2])
}
GO

zig_cc() { zig cc --target=wasm32-wasi-musl "$@"; }

echo "→ core"
zig_cc -mexec-model=reactor "$SRC/lib.c" -I "$SRC" -O2 -o "$WORK/core.wasm" -Wl,--strip-debug -Wl,--export-table \
  -Wl,--export=malloc -Wl,--export=calloc -Wl,--export=free -Wl,--export=realloc \
  -Wl,--export=memcpy -Wl,--export=memset -Wl,--export=memmove \
  -Wl,--export=iswalpha -Wl,--export=iswspace -Wl,--export=iswdigit -Wl,--export=iswalnum -Wl,--export=towupper -Wl,--export=towlower \
  -Wl,--export=__stack_pointer \
  -Wl,--export=ts_current_malloc -Wl,--export=ts_current_free -Wl,--export=ts_current_realloc -Wl,--export=ts_current_calloc \
  -Wl,--export=ts_parser_new -Wl,--export=ts_parser_delete -Wl,--export=ts_parser_set_language -Wl,--export=ts_parser_parse_string \
  -Wl,--export=ts_tree_root_node -Wl,--export=ts_tree_delete \
  -Wl,--export=ts_node_type -Wl,--export=ts_node_is_named \
  -Wl,--export=ts_node_start_byte -Wl,--export=ts_node_end_byte \
  -Wl,--export=ts_node_start_point -Wl,--export=ts_node_end_point \
  -Wl,--export=ts_node_child_count -Wl,--export=ts_node_child -Wl,--export=ts_node_field_name_for_child
go run "$WORK/patch.go" "$WORK/core.wasm" "$OUT/tree-sitter-core.wasm"

# grammar <out-name> <entry> <src-dir> [scanner]
grammar() {
  local name="$1" entry="$2" dir="$3" scanner="${4:-}"
  zig_cc -fPIC -O2 -I "$dir" -I "$SRC" -c "$dir/parser.c" -o "$WORK/${name}_p.o"
  local objs="$WORK/${name}_p.o"
  if [ -n "$scanner" ]; then
    zig_cc -fPIC -O2 -I "$dir" -I "$SRC" -c "$dir/scanner.c" -o "$WORK/${name}_s.o"
    objs="$objs $WORK/${name}_s.o"
  fi
  # --strip-debug removes .debug_* sections (which embed absolute source paths),
  # making the output reproducible and much smaller.
  zig wasm-ld --experimental-pic -shared --no-entry --strip-debug --export="tree_sitter_$entry" --allow-undefined $objs -o "$OUT/$name.wasm"
  echo "→ $name.wasm"
}

grammar go         go         "$SRC/golang"
grammar python     python     "$SRC/python"              "$SRC/python/scanner.c"
grammar javascript javascript "$SRC/javascript"          "$SRC/javascript/scanner.c"
grammar typescript typescript "$SRC/typescript/typescript" "$SRC/typescript/typescript/scanner.c"
grammar tsx        tsx        "$SRC/typescript/tsx"       "$SRC/typescript/tsx/scanner.c"

echo
echo "checksums:"
( cd "$OUT" && shasum -a 256 *.wasm )

if [ -n "${VERIFY:-}" ]; then
  echo; echo "VERIFY: diffing against committed blobs"
  for f in "$OUT"/*.wasm; do
    b="$(basename "$f")"
    if cmp -s "$f" "$HERE/$b"; then echo "  ok    $b"; else echo "  DIFF  $b"; fi
  done
fi
