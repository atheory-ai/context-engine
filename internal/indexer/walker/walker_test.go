package walker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func walk(t *testing.T, root string, cfg Config) map[string]bool {
	t.Helper()
	w, err := New(root, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	results := make(chan WalkResult, 64)
	go func() { _ = w.Walk(context.Background(), results) }()
	seen := map[string]bool{}
	for r := range results {
		seen[filepath.ToSlash(r.RelPath)] = true
	}
	return seen
}

func TestWalk_DiscoversFilesRespectingExcludes(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "main.go"), "package main")
	write(t, filepath.Join(root, "pkg", "util.go"), "package pkg")
	write(t, filepath.Join(root, "node_modules", "dep.js"), "x")  // common skip dir
	write(t, filepath.Join(root, "build", "out.txt"), "artifact") // excluded by pattern

	seen := walk(t, root, Config{ExcludePatterns: []string{"build/**"}})

	if !seen["main.go"] || !seen["pkg/util.go"] {
		t.Errorf("expected source files, got %v", seen)
	}
	if seen["node_modules/dep.js"] {
		t.Error("node_modules should be skipped")
	}
	if seen["build/out.txt"] {
		t.Error("excluded pattern build/** should be skipped")
	}
}

func TestWalk_SkipsOversizeFiles(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "small.go"), "package main")
	write(t, filepath.Join(root, "big.go"), string(make([]byte, 2048)))

	seen := walk(t, root, Config{MaxFileSizeBytes: 1024})
	if !seen["small.go"] {
		t.Error("small file should be indexed")
	}
	if seen["big.go"] {
		t.Error("file over MaxFileSizeBytes should be skipped")
	}
}

func TestWalkPaths_OnlyReturnsRequestedFilesAndReportsDeletion(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "kept.go"), "package kept")
	write(t, filepath.Join(root, "ignored", "skip.go"), "package ignored")
	w, err := New(root, Config{ExcludePatterns: []string{"ignored/**"}})
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan WalkResult, 8)
	go func() {
		if err := w.WalkPaths(context.Background(), []string{
			"kept.go",                      // root-relative paths are accepted
			filepath.Join(root, "kept.go"), // deduplicated
			filepath.Join(root, "deleted.go"),
			filepath.Join(root, "ignored", "skip.go"),
			filepath.Dir(root), // outside root
		}, results); err != nil {
			t.Errorf("WalkPaths: %v", err)
		}
	}()

	got := map[string]bool{}
	for result := range results {
		got[filepath.ToSlash(result.RelPath)] = result.Deleted
	}
	if deleted, ok := got["kept.go"]; !ok || deleted {
		t.Fatalf("kept.go result = (%v, %v), want present", deleted, ok)
	}
	if deleted, ok := got["deleted.go"]; !ok || !deleted {
		t.Fatalf("deleted.go result = (%v, %v), want deleted", deleted, ok)
	}
	if _, ok := got["ignored/skip.go"]; ok {
		t.Fatal("ignored file was returned")
	}
}

func TestStatDir(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.go"), "package a")
	write(t, filepath.Join(root, "b.go"), "package b")
	n, err := StatDir(root)
	if err != nil {
		t.Fatalf("StatDir: %v", err)
	}
	if n < 2 {
		t.Errorf("StatDir counted %d, want >= 2", n)
	}
}
