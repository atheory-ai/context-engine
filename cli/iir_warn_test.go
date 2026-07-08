package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
)

// Plugin load/parse failures during rule-pack resolution must reach the user
// (the CLI has no channel consumer), matching the engine-backed verify path.
func TestDrainWarnings_SurfacesEmissions(t *testing.T) {
	ch := core.NewAppChannels()
	ch.Emit(core.Emission{Channel: core.ChanWarning, Content: "plugin foo: boom"})
	ch.Emit(core.Emission{Channel: core.ChanError, Content: "bad"})

	var buf bytes.Buffer
	drainWarnings(&ch, &buf)

	out := buf.String()
	if !strings.Contains(out, "warning: plugin foo: boom") {
		t.Errorf("warning not surfaced: %q", out)
	}
	if !strings.Contains(out, "error: bad") {
		t.Errorf("error not surfaced: %q", out)
	}
}

func TestDrainWarnings_QuietWhenEmpty(t *testing.T) {
	ch := core.NewAppChannels()
	var buf bytes.Buffer
	drainWarnings(&ch, &buf)
	if buf.Len() != 0 {
		t.Errorf("expected no output for no emissions, got %q", buf.String())
	}
}
