package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	extism "github.com/extism/go-sdk"

	"github.com/atheory/context-engine/internal/core"
)

// HostDeps are the engine dependencies injected into ce.* host functions.
// Substrate is nil during indexing (Phase 1) and during startup.
// PluginConfig holds the per-plugin config from ce.yaml.
type HostDeps struct {
	Channels     *core.AppChannels
	Substrate    core.SubstrateReader // nil during indexing / Phase 1
	PluginConfig map[string]any
}

// pluginPermittedChannels defines which engine channels plugins may write to.
// Plugins cannot hijack the message or system channels.
var pluginPermittedChannels = map[core.ChannelType]bool{
	core.ChanThinking: true,
	core.ChanAction:   true,
	core.ChanDebug:    true,
	core.ChanWarning:  true,
}

// buildHostFunctions creates all ce.* host functions for the Extism plugin runtime.
// All functions are registered under the "ce" module namespace.
func buildHostFunctions(deps HostDeps) []extism.HostFunction {
	funcs := []extism.HostFunction{
		makeHostLog(deps),
		makeHostEmit(deps),
		makeHostSubstrateQuery(deps),
		makeHostGetConfig(deps),
		makeHostNodeID(),
		makeHostEdgeID(),
	}
	for i := range funcs {
		funcs[i].SetNamespace("ce")
	}
	return funcs
}

// makeHostLog creates ce.log(level_ptr, msg_ptr).
// Routes plugin log output to the debug channel.
func makeHostLog(deps HostDeps) extism.HostFunction {
	return extism.NewHostFunctionWithStack(
		"log",
		func(ctx context.Context, p *extism.CurrentPlugin, stack []uint64) {
			if deps.Channels == nil {
				return
			}
			level, _ := p.ReadString(stack[0])
			msg, _ := p.ReadString(stack[1])
			deps.Channels.Emit(core.Emission{
				Source:  "plugin",
				Channel: core.ChanDebug,
				Content: fmt.Sprintf("[plugin:%s] %s", level, msg),
			})
		},
		[]extism.ValueType{extism.ValueTypePTR, extism.ValueTypePTR},
		nil,
	)
}

// makeHostEmit creates ce.emit(channel_ptr, content_ptr).
// Plugins can emit to thinking, action, debug, and warning channels only.
func makeHostEmit(deps HostDeps) extism.HostFunction {
	return extism.NewHostFunctionWithStack(
		"emit",
		func(ctx context.Context, p *extism.CurrentPlugin, stack []uint64) {
			if deps.Channels == nil {
				return
			}
			channel, _ := p.ReadString(stack[0])
			content, _ := p.ReadString(stack[1])
			ch := core.ChannelType(channel)
			if !pluginPermittedChannels[ch] {
				return // silently drop — plugins cannot hijack restricted channels
			}
			deps.Channels.Emit(core.Emission{
				Source:  "plugin",
				Channel: ch,
				Content: content,
			})
		},
		[]extism.ValueType{extism.ValueTypePTR, extism.ValueTypePTR},
		nil,
	)
}

// substrateQuery dispatches a SubstrateQuery to the appropriate SubstrateReader method.
// The Query method no longer exists on SubstrateReader; this helper bridges plugin
// JSON queries to the typed Get* methods.
func substrateQuery(ctx context.Context, sub core.SubstrateReader, q core.SubstrateQuery) ([]core.Node, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}

	// Properties-based lookup: canonical_id → GetNodeByCanonicalID.
	if canonicalID, ok := q.Properties["canonical_id"]; ok {
		node, err := sub.GetNodeByCanonicalID(ctx, q.ProjectID, canonicalID)
		if err != nil || node == nil {
			return nil, err
		}
		return []core.Node{*node}, nil
	}

	// NodeType-based dispatch.
	for _, nt := range q.NodeTypes {
		switch nt {
		case core.NodeTypeConcept:
			return sub.GetConceptNodes(ctx, q.ProjectID, "")
		case core.NodeTypeNamespace:
			return sub.GetNodesByNamespacePrefix(ctx, q.ProjectID, "", limit)
		}
	}

	// Fallback: return top-K activated nodes.
	topK, err := sub.GetTopKActivated(ctx, q.ProjectID, limit)
	if err != nil {
		return nil, err
	}
	nodes := make([]core.Node, len(topK))
	for i, nwa := range topK {
		nodes[i] = nwa.Node
	}
	return nodes, nil
}

// makeHostSubstrateQuery creates ce.substrate_query(query_json_ptr) → result_json_ptr.
// Plugins query the substrate read-only. Returns "[]" if substrate unavailable.
func makeHostSubstrateQuery(deps HostDeps) extism.HostFunction {
	return extism.NewHostFunctionWithStack(
		"substrate_query",
		func(ctx context.Context, p *extism.CurrentPlugin, stack []uint64) {
			if deps.Substrate == nil {
				offset, _ := p.WriteString("[]")
				stack[0] = offset
				return
			}
			queryJSON, _ := p.ReadString(stack[0])
			var q core.SubstrateQuery
			if err := json.Unmarshal([]byte(queryJSON), &q); err != nil {
				offset, _ := p.WriteString("[]")
				stack[0] = offset
				return
			}
			nodes, err := substrateQuery(ctx, deps.Substrate, q)
			if err != nil {
				offset, _ := p.WriteString("[]")
				stack[0] = offset
				return
			}
			result, _ := json.Marshal(nodes)
			offset, _ := p.WriteString(string(result))
			stack[0] = offset
		},
		[]extism.ValueType{extism.ValueTypePTR},
		[]extism.ValueType{extism.ValueTypePTR},
	)
}

// makeHostGetConfig creates ce.get_config(key_ptr) → value_json_ptr.
// Plugins read their own config values from the ce.yaml plugins section.
func makeHostGetConfig(deps HostDeps) extism.HostFunction {
	return extism.NewHostFunctionWithStack(
		"get_config",
		func(ctx context.Context, p *extism.CurrentPlugin, stack []uint64) {
			key, _ := p.ReadString(stack[0])
			var value any
			if deps.PluginConfig != nil {
				value = deps.PluginConfig[key]
			}
			result, _ := json.Marshal(value)
			offset, _ := p.WriteString(string(result))
			stack[0] = offset
		},
		[]extism.ValueType{extism.ValueTypePTR},
		[]extism.ValueType{extism.ValueTypePTR},
	)
}

// makeHostNodeID creates ce.node_id(project_id_ptr, type_ptr, canonical_ptr) → id_ptr.
// Deterministic node ID generation — plugins use this to produce consistent IDs
// without reimplementing the SHA-256 algorithm.
func makeHostNodeID() extism.HostFunction {
	return extism.NewHostFunctionWithStack(
		"node_id",
		func(ctx context.Context, p *extism.CurrentPlugin, stack []uint64) {
			projectID, _ := p.ReadString(stack[0])
			nodeType, _ := p.ReadString(stack[1])
			canonicalID, _ := p.ReadString(stack[2])
			id := core.MakeNodeID(projectID, nodeType, canonicalID)
			offset, _ := p.WriteString(id)
			stack[0] = offset
		},
		[]extism.ValueType{extism.ValueTypePTR, extism.ValueTypePTR, extism.ValueTypePTR},
		[]extism.ValueType{extism.ValueTypePTR},
	)
}

// makeHostEdgeID creates ce.edge_id(source_ptr, type_ptr, target_ptr) → id_ptr.
// Deterministic edge ID generation matching core.MakeEdgeID.
func makeHostEdgeID() extism.HostFunction {
	return extism.NewHostFunctionWithStack(
		"edge_id",
		func(ctx context.Context, p *extism.CurrentPlugin, stack []uint64) {
			sourceID, _ := p.ReadString(stack[0])
			edgeType, _ := p.ReadString(stack[1])
			targetID, _ := p.ReadString(stack[2])
			id := core.MakeEdgeID(sourceID, edgeType, targetID)
			offset, _ := p.WriteString(id)
			stack[0] = offset
		},
		[]extism.ValueType{extism.ValueTypePTR, extism.ValueTypePTR, extism.ValueTypePTR},
		[]extism.ValueType{extism.ValueTypePTR},
	)
}
