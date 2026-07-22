package runtime

import "encoding/binary"

// compactExtractionInput is ABI-v4's binary envelope. It prevents the host
// from embedding source and the CST in a JSON object that QuickJS must then
// parse into a second, fully materialized object graph.
//
// Layout: magic("CEIN"), version, file-path length, source-anchor length,
// source length, compact-tree length, followed by those four byte ranges.
var compactExtractionMagic = [4]byte{'C', 'E', 'I', 'N'}

const compactExtractionVersion = uint32(1)

func compactExtractionInput(filePath string, content, tree []byte) []byte {
	anchor := filePath
	total := 24 + len(filePath) + len(anchor) + len(content) + len(tree)
	input := make([]byte, total)
	copy(input[:4], compactExtractionMagic[:])
	off := 4
	put := func(value uint32) {
		binary.LittleEndian.PutUint32(input[off:], value)
		off += 4
	}
	put(compactExtractionVersion)
	put(uint32(len(filePath)))
	put(uint32(len(anchor)))
	put(uint32(len(content)))
	put(uint32(len(tree)))
	for _, value := range [][]byte{[]byte(filePath), []byte(anchor), content, tree} {
		copy(input[off:], value)
		off += len(value)
	}
	return input
}
