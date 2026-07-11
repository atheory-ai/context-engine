package progress

import (
	"strings"
	"testing"
)

func TestRender_WithKnownTotal(t *testing.T) {
	p := &IndexProgress{FilesProcessed: 5, FilesTotal: 10, NodesCreated: 42}
	got := p.Render()
	if !strings.Contains(got, "50%") || !strings.Contains(got, "5/10") || !strings.Contains(got, "42 nodes") {
		t.Errorf("Render = %q", got)
	}
}

func TestRender_UnknownTotal(t *testing.T) {
	p := &IndexProgress{FilesProcessed: 7, FilesTotal: -1, NodesCreated: 20, EdgesCreated: 8}
	got := p.Render()
	if strings.Contains(got, "%") {
		t.Errorf("streaming walk should not show a percentage: %q", got)
	}
	if !strings.Contains(got, "7 files") || !strings.Contains(got, "8 edges") {
		t.Errorf("Render = %q", got)
	}
}
