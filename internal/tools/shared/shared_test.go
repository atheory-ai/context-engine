package shared

import (
	"strings"
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
)

func TestTruncate(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	got, note := Truncate(items, 3)
	if len(got) != 3 || note == "" {
		t.Errorf("truncated = %v, note = %q", got, note)
	}
	if !strings.Contains(note, "3 of 5") {
		t.Errorf("note = %q", note)
	}
	// No truncation when within limit.
	got, note = Truncate(items, 10)
	if len(got) != 5 || note != "" {
		t.Errorf("no-op truncate = %v, note = %q", got, note)
	}
}

func TestTruncateContent(t *testing.T) {
	if got := TruncateContent("hello", 10); got != "hello" {
		t.Errorf("short content changed: %q", got)
	}
	got := TruncateContent("hello world", 5)
	if !strings.HasPrefix(got, "hello") || !strings.Contains(got, "truncated at 5") {
		t.Errorf("TruncateContent = %q", got)
	}
}

func TestThinking(t *testing.T) {
	req := core.ToolRequest{RunID: "r", TurnID: "t", LoopIndex: 3}
	e := Thinking(req, "callgraph", "reasoning", map[string]any{"k": "v"})
	if e.Channel != core.ChanThinking {
		t.Errorf("channel = %q, want thinking", e.Channel)
	}
	if e.Source != "tool:callgraph" || e.Content != "reasoning" {
		t.Errorf("emission = %+v", e)
	}
	if e.RunID != "r" || e.TurnID != "t" || e.LoopIndex != 3 {
		t.Errorf("request fields not carried: %+v", e)
	}
	if e.Metadata["k"] != "v" {
		t.Errorf("metadata not carried: %+v", e.Metadata)
	}
}
