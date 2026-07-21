package indexer

import (
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
)

type layerPlugin struct {
	id       core.PluginID
	contract core.PluginIndexContract
}

func (p layerPlugin) ID() core.PluginID                       { return p.id }
func (p layerPlugin) Name() string                            { return string(p.id) }
func (p layerPlugin) Version() string                         { return "test" }
func (p layerPlugin) Language() core.LanguageHandler          { return nil }
func (p layerPlugin) Roles() []core.RoleDefinition            { return nil }
func (p layerPlugin) Analyzers() []core.Analyzer              { return nil }
func (p layerPlugin) Tools() []core.Tool                      { return nil }
func (p layerPlugin) Close() error                            { return nil }
func (p layerPlugin) IndexContract() core.PluginIndexContract { return p.contract }

func TestPluginExtractionLayersFansOutIndependentPlugins(t *testing.T) {
	php := layerPlugin{id: "php", contract: core.PluginIndexContract{
		Provides: []string{"language:php", "cst:php", "facts:php-structure"},
	}}
	security := layerPlugin{id: "security", contract: core.PluginIndexContract{
		Provides: []string{"facts:security"},
	}}
	wordpress := layerPlugin{id: "wordpress", contract: core.PluginIndexContract{
		Requires: []string{"cst:php", "facts:php-structure"}, Enriches: []string{"php"},
	}}

	layers := pluginExtractionLayers([]core.Plugin{php, security, wordpress})
	if len(layers) != 2 {
		t.Fatalf("layers = %d, want 2", len(layers))
	}
	assertLayerIDs(t, layers[0], "php", "security")
	assertLayerIDs(t, layers[1], "wordpress")
}

func TestPluginExtractionLayersKeepsLegacyPluginsSerial(t *testing.T) {
	first := layerPlugin{id: "first", contract: core.PluginIndexContract{Provides: []string{"facts:first"}}}
	legacy := layerPlugin{id: "legacy"}
	last := layerPlugin{id: "last", contract: core.PluginIndexContract{Provides: []string{"facts:last"}}}

	layers := pluginExtractionLayers([]core.Plugin{first, legacy, last})
	if len(layers) != 3 {
		t.Fatalf("layers = %d, want 3", len(layers))
	}
	assertLayerIDs(t, layers[0], "first")
	assertLayerIDs(t, layers[1], "legacy")
	assertLayerIDs(t, layers[2], "last")
}

func assertLayerIDs(t *testing.T, layer []core.Plugin, want ...string) {
	t.Helper()
	if len(layer) != len(want) {
		t.Fatalf("layer length = %d, want %d", len(layer), len(want))
	}
	for i, plugin := range layer {
		if got := string(plugin.ID()); got != want[i] {
			t.Fatalf("layer[%d] = %q, want %q", i, got, want[i])
		}
	}
}
