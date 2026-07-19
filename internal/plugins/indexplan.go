package plugins

import (
	"fmt"
	"sort"

	"github.com/atheory-ai/context-engine/internal/core"
)

type indexPlanItem struct {
	plugin   core.Plugin
	contract core.PluginIndexContract
	explicit bool
}

// IndexPlanForFile returns plugins in a deterministic dependency order. Legacy
// plugins retain registration order. Once any matching plugin declares an
// index contract, every declared requirement must be supplied by exactly one
// matching plugin; load order can no longer accidentally select a provider.
func (r *Registry) IndexPlanForFile(filePath string) ([]core.Plugin, error) {
	matched := r.PluginsForFile(filePath)
	if len(matched) == 0 {
		return matched, nil
	}

	items := make([]indexPlanItem, len(matched))
	explicit := false
	providers := map[string][]int{}
	for i, plugin := range matched {
		contract, ok := plugin.(core.IndexContractContributor)
		if ok {
			items[i].contract = contract.IndexContract()
			items[i].explicit = len(items[i].contract.Provides)+len(items[i].contract.Requires)+len(items[i].contract.Enriches) > 0
		}
		items[i].plugin = plugin
		explicit = explicit || items[i].explicit
		for _, capability := range items[i].contract.Provides {
			providers[capability] = append(providers[capability], i)
		}
	}
	if !explicit {
		return matched, nil
	}

	deps := make([]map[int]struct{}, len(items))
	for i := range deps {
		deps[i] = make(map[int]struct{})
	}
	for i, item := range items {
		for _, required := range item.contract.Requires {
			candidates := providers[required]
			if len(candidates) == 0 {
				return nil, fmt.Errorf("plugin %s requires %q for %s, but no matching provider is loaded", item.plugin.ID(), required, filePath)
			}
			if len(candidates) > 1 {
				return nil, fmt.Errorf("plugin %s requires %q for %s, but providers are ambiguous: %s", item.plugin.ID(), required, filePath, itemIDs(items, candidates))
			}
			if candidates[0] == i {
				return nil, fmt.Errorf("plugin %s cannot require its own capability %q", item.plugin.ID(), required)
			}
			deps[i][candidates[0]] = struct{}{}
		}
		for _, language := range item.contract.Enriches {
			capability := "language:" + language
			candidates := providers[capability]
			if len(candidates) == 0 {
				return nil, fmt.Errorf("plugin %s enriches %q for %s, but no language provider is loaded", item.plugin.ID(), language, filePath)
			}
			if len(candidates) > 1 {
				return nil, fmt.Errorf("plugin %s enriches %q for %s, but language providers are ambiguous: %s", item.plugin.ID(), language, filePath, itemIDs(items, candidates))
			}
			deps[i][candidates[0]] = struct{}{}
		}
	}

	// Kahn's algorithm, selecting ready plugins by ID so equivalent manifests
	// execute identically regardless of YAML registration order.
	ready := make([]int, 0, len(items))
	done := make([]bool, len(items))
	for i := range items {
		if len(deps[i]) == 0 {
			ready = append(ready, i)
		}
	}
	result := make([]core.Plugin, 0, len(items))
	for len(ready) > 0 {
		sort.Slice(ready, func(i, j int) bool { return string(items[ready[i]].plugin.ID()) < string(items[ready[j]].plugin.ID()) })
		n := ready[0]
		ready = ready[1:]
		if done[n] {
			continue
		}
		done[n] = true
		result = append(result, items[n].plugin)
		for child := range items {
			if _, waits := deps[child][n]; waits {
				delete(deps[child], n)
				if len(deps[child]) == 0 && !done[child] {
					ready = append(ready, child)
				}
			}
		}
	}
	if len(result) != len(items) {
		return nil, fmt.Errorf("plugin indexing dependency cycle for %s", filePath)
	}
	return result, nil
}

func itemIDs(items []indexPlanItem, indexes []int) string {
	ids := make([]string, 0, len(indexes))
	for _, i := range indexes {
		ids = append(ids, string(items[i].plugin.ID()))
	}
	sort.Strings(ids)
	return fmt.Sprint(ids)
}
