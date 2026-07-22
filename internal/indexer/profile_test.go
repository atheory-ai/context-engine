package indexer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProfileWritesInspectableArtifacts(t *testing.T) {
	dir := t.TempDir()
	p, err := startProfile(dir, false)
	if err != nil {
		t.Fatalf("startProfile: %v", err)
	}
	p.Record("parse", time.Millisecond)
	p.Add("files", 1)
	if err := p.Stop(Stats{FilesIndexed: 1}, errors.New("expected test result")); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	for _, name := range []string{
		"cpu.pprof", "heap.pprof", "mutex.pprof", "block.pprof", "summary.json",
	} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
		if info.Size() == 0 {
			t.Fatalf("empty %s", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "trace.out")); !os.IsNotExist(err) {
		t.Fatalf("trace created without profile trace flag: %v", err)
	}
}
