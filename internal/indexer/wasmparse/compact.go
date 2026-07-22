package wasmparse

import (
	"encoding/binary"
	"fmt"
)

// CompactTreeMagic identifies the binary CST passed to language plugins. It is
// deliberately a node table rather than JSON: node text lives only in the
// source buffer, and repeated node metadata is dictionary encoded.
var CompactTreeMagic = [4]byte{'C', 'E', 'C', 'T'}

const compactTreeVersion = uint32(1)

// EncodeCompactTree encodes a parsed tree without serializing source spans or
// allocating a JSON representation. The layout is little endian:
//
//	magic(4), version, nodeCount, childCount, typeCount, fieldCount,
//	type strings, field strings, fixed-size node records, child-index table.
//
// A node record is eleven u32 values: type index, field index (+1; zero is
// absent), flags, byte range, start/end positions, child offset and count.
func EncodeCompactTree(tree *SyntaxTree) ([]byte, error) {
	if tree == nil || tree.Root == nil {
		return nil, nil
	}
	type nodeRecord struct {
		typeIndex, fieldIndex, flags             uint32
		startByte, endByte                       uint32
		startRow, startColumn, endRow, endColumn uint32
		childOffset, childCount                  uint32
	}
	types := []string{}
	fields := []string{}
	typeIndex := map[string]uint32{}
	fieldIndex := map[string]uint32{}
	indexType := func(value string) uint32 {
		if i, ok := typeIndex[value]; ok {
			return i
		}
		i := uint32(len(types))
		typeIndex[value] = i
		types = append(types, value)
		return i
	}
	indexField := func(value string) uint32 {
		if value == "" {
			return 0
		}
		if i, ok := fieldIndex[value]; ok {
			return i + 1
		}
		i := uint32(len(fields))
		fieldIndex[value] = i
		fields = append(fields, value)
		return i + 1
	}

	records := make([]nodeRecord, 0, 256)
	children := make([]uint32, 0, 256)
	var visit func(*SyntaxNode) uint32
	visit = func(node *SyntaxNode) uint32 {
		index := uint32(len(records))
		records = append(records, nodeRecord{})
		childIDs := make([]uint32, 0, len(node.Children))
		for _, child := range node.Children {
			childIDs = append(childIDs, visit(child))
		}
		field := ""
		if node.FieldName != nil {
			field = *node.FieldName
		}
		flags := uint32(0)
		if node.IsNamed {
			flags = 1
		}
		records[index] = nodeRecord{
			typeIndex: indexType(node.Type), fieldIndex: indexField(field), flags: flags,
			startByte: node.StartByte, endByte: node.EndByte,
			startRow: node.StartPosition.Row, startColumn: node.StartPosition.Column,
			endRow: node.EndPosition.Row, endColumn: node.EndPosition.Column,
			childOffset: uint32(len(children)), childCount: uint32(len(childIDs)),
		}
		children = append(children, childIDs...)
		return index
	}
	visit(tree.Root)

	size := 24 + stringTableSize(types) + stringTableSize(fields) + len(records)*44 + len(children)*4
	buf := make([]byte, size)
	copy(buf[:4], CompactTreeMagic[:])
	off := 4
	putU32 := func(value uint32) { binary.LittleEndian.PutUint32(buf[off:], value); off += 4 }
	putU32(compactTreeVersion)
	putU32(uint32(len(records)))
	putU32(uint32(len(children)))
	putU32(uint32(len(types)))
	putU32(uint32(len(fields)))
	for _, values := range [][]string{types, fields} {
		for _, value := range values {
			putU32(uint32(len(value)))
			copy(buf[off:], value)
			off += len(value)
		}
	}
	for _, record := range records {
		for _, value := range [...]uint32{record.typeIndex, record.fieldIndex, record.flags, record.startByte, record.endByte, record.startRow, record.startColumn, record.endRow, record.endColumn, record.childOffset, record.childCount} {
			putU32(value)
		}
	}
	for _, child := range children {
		putU32(child)
	}
	if off != len(buf) {
		return nil, fmt.Errorf("compact tree encoding size mismatch: wrote %d, allocated %d", off, len(buf))
	}
	return buf, nil
}

func stringTableSize(values []string) int {
	size := 0
	for _, value := range values {
		size += 4 + len(value)
	}
	return size
}

// DecodeCompactTree is intentionally small and is used by tests and tooling to
// validate the wire representation. Extraction plugins decode the same format
// lazily in the TypeScript SDK, so this decoder must not become part of the hot
// indexing path.
func DecodeCompactTree(data []byte) (*SyntaxTree, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if len(data) < 24 || string(data[:4]) != string(CompactTreeMagic[:]) {
		return nil, fmt.Errorf("invalid compact tree header")
	}
	off := 4
	readU32 := func() (uint32, error) {
		if len(data)-off < 4 {
			return 0, fmt.Errorf("truncated compact tree")
		}
		value := binary.LittleEndian.Uint32(data[off:])
		off += 4
		return value, nil
	}
	version, err := readU32()
	if err != nil || version != compactTreeVersion {
		return nil, fmt.Errorf("unsupported compact tree version %d", version)
	}
	nodeCount, err := readU32()
	if err != nil {
		return nil, err
	}
	childCount, err := readU32()
	if err != nil {
		return nil, err
	}
	typeCount, err := readU32()
	if err != nil {
		return nil, err
	}
	fieldCount, err := readU32()
	if err != nil {
		return nil, err
	}
	readStrings := func(count uint32) ([]string, error) {
		values := make([]string, count)
		for i := range values {
			length, err := readU32()
			if err != nil || uint64(length) > uint64(len(data)-off) {
				return nil, fmt.Errorf("truncated compact tree string table")
			}
			values[i] = string(data[off : off+int(length)])
			off += int(length)
		}
		return values, nil
	}
	types, err := readStrings(typeCount)
	if err != nil {
		return nil, err
	}
	fields, err := readStrings(fieldCount)
	if err != nil {
		return nil, err
	}
	type record struct {
		values [11]uint32
	}
	records := make([]record, nodeCount)
	for i := range records {
		for j := range records[i].values {
			value, err := readU32()
			if err != nil {
				return nil, err
			}
			records[i].values[j] = value
		}
	}
	children := make([]uint32, childCount)
	for i := range children {
		value, err := readU32()
		if err != nil {
			return nil, err
		}
		children[i] = value
	}
	if off != len(data) {
		return nil, fmt.Errorf("trailing compact tree bytes")
	}
	var build func(uint32, map[uint32]bool) (*SyntaxNode, error)
	build = func(index uint32, stack map[uint32]bool) (*SyntaxNode, error) {
		if index >= uint32(len(records)) || stack[index] {
			return nil, fmt.Errorf("invalid compact tree child index %d", index)
		}
		r := records[index].values
		if r[0] >= uint32(len(types)) || r[1] > uint32(len(fields)) || uint64(r[9])+uint64(r[10]) > uint64(len(children)) {
			return nil, fmt.Errorf("invalid compact tree node %d", index)
		}
		node := &SyntaxNode{
			Type:          types[r[0]],
			IsNamed:       r[2]&1 != 0,
			StartByte:     r[3],
			EndByte:       r[4],
			StartPosition: Position{Row: r[5], Column: r[6]},
			EndPosition:   Position{Row: r[7], Column: r[8]},
		}
		if r[1] != 0 {
			field := fields[r[1]-1]
			node.FieldName = &field
		}
		if r[10] != 0 {
			node.Children = make([]*SyntaxNode, 0, r[10])
			stack[index] = true
			for _, childIndex := range children[r[9] : r[9]+r[10]] {
				child, err := build(childIndex, stack)
				if err != nil {
					return nil, err
				}
				node.Children = append(node.Children, child)
			}
			delete(stack, index)
		}
		return node, nil
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("compact tree has no root")
	}
	root, err := build(0, map[uint32]bool{})
	if err != nil {
		return nil, err
	}
	return &SyntaxTree{Root: root}, nil
}
