// Package parser routes files to the appropriate language handler.
// The first loaded plugin whose Match() returns true handles a given file.
package parser

import (
	"github.com/atheory/context-engine/internal/core"
)

// Parser routes files to the correct language handler from loaded plugins.
type Parser struct {
	entries []entry
}

type entry struct {
	handler  core.LanguageHandler
	pluginID core.PluginID
}

// New creates a Parser from the language handlers of the given plugins.
// Plugins are tried in order; the first matching handler wins.
func New(plugins []core.Plugin) *Parser {
	p := &Parser{}
	for _, plugin := range plugins {
		lang := plugin.Language()
		if lang == nil {
			continue
		}
		p.entries = append(p.entries, entry{
			handler:  lang,
			pluginID: plugin.ID(),
		})
	}
	return p
}

// Route returns the language handler that matches filePath.
// Returns (nil, "", false) if no handler matches.
func (p *Parser) Route(filePath string) (core.LanguageHandler, core.PluginID, bool) {
	for _, e := range p.entries {
		if e.handler.Match(filePath) {
			return e.handler, e.pluginID, true
		}
	}
	return nil, "", false
}

// HandlerCount returns the number of language handlers registered.
func (p *Parser) HandlerCount() int {
	return len(p.entries)
}
