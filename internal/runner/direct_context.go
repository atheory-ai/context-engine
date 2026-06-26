package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/atheory-ai/context-engine/internal/core"
)

type SourceRangeOptions struct {
	NodeID      string `json:"node_id"`
	CanonicalID string `json:"canonical_id"`
	Query       string `json:"query"`
	Type        string `json:"type"`
	Limit       int    `json:"limit"`
	Context     int    `json:"context"`
}

type InvestigateOptions struct {
	NodeID         string `json:"node_id"`
	CanonicalID    string `json:"canonical_id"`
	Query          string `json:"query"`
	Type           string `json:"type"`
	Limit          int    `json:"limit"`
	Depth          int    `json:"depth"`
	IncludeTests   bool   `json:"include_tests"`
	IncludeHooks   bool   `json:"include_hooks"`
	IncludeSources bool   `json:"include_sources"`
}

type RelatedContextOptions struct {
	NodeID      string `json:"node_id"`
	CanonicalID string `json:"canonical_id"`
	Query       string `json:"query"`
	Type        string `json:"type"`
	Limit       int    `json:"limit"`
}

func searchTokens(query string) []string {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	parts := regexp.MustCompile(`[^\pL\pN_\\.$:/-]+`).Split(query, -1)
	var tokens []string
	seen := make(map[string]struct{})
	for _, part := range parts {
		part = strings.Trim(strings.ToLower(part), " .:/-")
		if len(part) < 2 {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		tokens = append(tokens, part)
	}
	return tokens
}

func scoreSearchNode(n SearchNode, tokens []string) (int, string) {
	label := strings.ToLower(n.Label)
	canonical := strings.ToLower(n.CanonicalID)
	filePath := strings.ToLower(n.FilePath)

	score := 0
	var reasons []string
	for _, token := range tokens {
		switch {
		case label == token:
			score += 90
			reasons = append(reasons, "label exact:"+token)
		case strings.Contains(label, token):
			score += 45
			reasons = append(reasons, "label:"+token)
		}
		switch {
		case canonical == token:
			score += 80
			reasons = append(reasons, "canonical exact:"+token)
		case strings.HasSuffix(canonical, token):
			score += 55
			reasons = append(reasons, "canonical suffix:"+token)
		case strings.Contains(canonical, token):
			score += 35
			reasons = append(reasons, "canonical:"+token)
		}
		if filePath != "" && strings.Contains(filePath, token) {
			score += 20
			reasons = append(reasons, "file:"+token)
		}
	}
	if n.Type == core.NodeTypeSymbol {
		score += 8
	}
	if n.FilePath != "" {
		score += 5
	}
	if n.LineStart > 0 {
		score += 3
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "matched query")
	}
	return score, strings.Join(reasons, ", ")
}

func nodeLineRange(props map[string]any) (int, int) {
	start, ok := intProperty(props, "line_start")
	if !ok {
		start, ok = intProperty(props, "start_line")
	}
	if !ok || start <= 0 {
		return 0, 0
	}
	end, ok := intProperty(props, "line_end")
	if !ok {
		end, ok = intProperty(props, "end_line")
	}
	if !ok || end < start {
		end = start
	}
	return start, end
}

func stringProperty(props map[string]any, key string) string {
	v, ok := props[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func intProperty(props map[string]any, key string) (int, bool) {
	v, ok := props[key]
	if !ok || v == nil {
		return 0, false
	}
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case string:
		n, err := strconv.Atoi(x)
		return n, err == nil
	default:
		return 0, false
	}
}

func nodeFilePath(n *core.Node) string {
	if n == nil {
		return ""
	}
	if n.Type == core.NodeTypeFile {
		return n.CanonicalID
	}
	return stringProperty(n.Properties, "file_path")
}

func describeNode(n *core.Node) string {
	if n == nil {
		return ""
	}
	filePath := nodeFilePath(n)
	start, end := nodeLineRange(n.Properties)
	loc := ""
	if filePath != "" {
		loc = " file: " + filePath
		if start > 0 {
			loc += fmt.Sprintf(":%d-%d", start, end)
		}
	}
	return fmt.Sprintf("[%s] %s\n  id: %s\n  label: %s%s", n.Type, n.CanonicalID, n.ID, n.Label, loc)
}

func (e *Engine) SourceRanges(ctx context.Context, opts SourceRangeOptions) (*DirectToolResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 3
	}
	if limit > 12 {
		limit = 12
	}
	contextLines := opts.Context
	if contextLines < 0 {
		contextLines = 0
	}
	if contextLines == 0 {
		contextLines = 3
	}

	var nodes []*core.Node
	if opts.NodeID != "" || opts.CanonicalID != "" {
		node, err := e.resolveDirectToolNode(ctx, DirectToolOptions{
			NodeID: opts.NodeID, CanonicalID: opts.CanonicalID,
		})
		if err != nil {
			return nil, err
		}
		if node != nil {
			nodes = append(nodes, node)
		}
	} else {
		found, err := e.SearchSubstrate(ctx, SearchOptions{Query: opts.Query, Type: opts.Type, Limit: limit})
		if err != nil {
			return nil, err
		}
		for _, searchNode := range found {
			node, err := e.substrate.GetNode(ctx, core.ProjectID("local"), core.NodeID(searchNode.ID))
			if err != nil {
				return nil, err
			}
			if node != nil {
				nodes = append(nodes, node)
			}
		}
	}

	if len(nodes) == 0 {
		return &DirectToolResult{Content: "No source ranges found."}, nil
	}

	var b strings.Builder
	for _, node := range nodes {
		filePath := nodeFilePath(node)
		if filePath == "" {
			b.WriteString(describeNode(node) + "\n  source: no file_path metadata\n\n")
			continue
		}
		start, end := nodeLineRange(node.Properties)
		if start <= 0 {
			b.WriteString(describeNode(node) + "\n  source: no line range metadata\n\n")
			continue
		}
		snippet, err := sourceSnippet(filePath, start, end, contextLines)
		if err != nil {
			b.WriteString(describeNode(node) + "\n  source: " + err.Error() + "\n\n")
			continue
		}
		b.WriteString(fmt.Sprintf("## %s\n\n%s\n", node.CanonicalID, snippet))
	}
	return &DirectToolResult{Content: strings.TrimSpace(b.String())}, nil
}

func (e *Engine) Investigate(ctx context.Context, opts InvestigateOptions) (*DirectToolResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 12 {
		limit = 12
	}

	seeds, err := e.resolveInvestigationSeeds(ctx, opts, limit)
	if err != nil {
		return nil, err
	}
	if len(seeds) == 0 {
		return &DirectToolResult{Content: "No investigation anchors found."}, nil
	}

	var b strings.Builder
	b.WriteString("## Investigation packet\n\n")
	b.WriteString("### Anchors\n")
	for _, node := range seeds {
		b.WriteString("- " + compactNode(node) + "\n")
	}

	for _, node := range seeds {
		b.WriteString("\n### Context for " + node.CanonicalID + "\n\n")
		for _, tool := range []string{"filecontext", "references", "callgraph", "summary", "concepts"} {
			result, err := e.RunDirectTool(ctx, DirectToolOptions{Tool: tool, NodeID: string(node.ID), Limit: 1})
			if err != nil {
				continue
			}
			content := strings.TrimSpace(result.Content)
			if content == "" || strings.Contains(content, "found no context") {
				continue
			}
			b.WriteString("#### " + tool + "\n")
			b.WriteString(limitSection(content, 2200) + "\n\n")
		}
	}

	if opts.IncludeTests {
		// Optional context expansion; failures yield empty result, which is rendered as missing section.
		tests, _ := e.RelatedTests(ctx, RelatedContextOptions{Query: opts.Query, Limit: 10}) //nolint:errcheck
		if tests != nil && strings.TrimSpace(tests.Content) != "" {
			b.WriteString("### Related tests\n")
			b.WriteString(limitSection(tests.Content, 3000) + "\n\n")
		}
	}

	if opts.IncludeHooks {
		entrypoints, _ := e.Entrypoints(ctx, RelatedContextOptions{Query: opts.Query, Limit: 10}) //nolint:errcheck // see comment above
		if entrypoints != nil && strings.TrimSpace(entrypoints.Content) != "" {
			b.WriteString("### Entrypoints and framework signals\n")
			b.WriteString(limitSection(entrypoints.Content, 3000) + "\n\n")
		}
	}

	if opts.IncludeSources {
		for _, node := range seeds {
			src, _ := e.SourceRanges(ctx, SourceRangeOptions{NodeID: string(node.ID), Context: 2}) //nolint:errcheck // see comment above
			if src != nil && strings.TrimSpace(src.Content) != "" {
				b.WriteString("### Source range\n")
				b.WriteString(limitSection(src.Content, 1800) + "\n\n")
			}
		}
	}

	b.WriteString("### Suggested next CE calls\n")
	for _, node := range seeds {
		b.WriteString(fmt.Sprintf("- ce_source_ranges {\"node_id\":\"%s\"}\n", node.ID))
		b.WriteString(fmt.Sprintf("- ce_references {\"node_id\":\"%s\"}\n", node.ID))
	}

	return &DirectToolResult{Content: strings.TrimSpace(b.String())}, nil
}

func (e *Engine) RelatedTests(ctx context.Context, opts RelatedContextOptions) (*DirectToolResult, error) {
	return e.relatedByPatterns(ctx, opts, "related tests", []string{"test", "tests", "spec", "fixture", "fixtures", "phpunit", "__tests__"})
}

func (e *Engine) Entrypoints(ctx context.Context, opts RelatedContextOptions) (*DirectToolResult, error) {
	return e.relatedByPatterns(ctx, opts, "entrypoints", []string{"route", "routes", "endpoint", "controller", "add_action", "add_filter", "do_action", "apply_filters", "register", "bootstrap", "init", "handler"})
}

func (e *Engine) Lifecycle(ctx context.Context, opts RelatedContextOptions) (*DirectToolResult, error) {
	return e.relatedByPatterns(ctx, opts, "lifecycle", []string{"init", "load", "session", "auth", "authenticate", "validate", "persist", "save", "merge", "handle", "callback"})
}

func (e *Engine) resolveInvestigationSeeds(ctx context.Context, opts InvestigateOptions, limit int) ([]*core.Node, error) {
	if opts.NodeID != "" || opts.CanonicalID != "" {
		node, err := e.resolveDirectToolNode(ctx, DirectToolOptions{NodeID: opts.NodeID, CanonicalID: opts.CanonicalID})
		if err != nil {
			return nil, err
		}
		if node == nil {
			return nil, nil
		}
		return []*core.Node{node}, nil
	}

	found, err := e.SearchSubstrate(ctx, SearchOptions{Query: opts.Query, Type: opts.Type, Limit: limit})
	if err != nil {
		return nil, err
	}
	var nodes []*core.Node
	for _, searchNode := range found {
		node, err := e.substrate.GetNode(ctx, core.ProjectID("local"), core.NodeID(searchNode.ID))
		if err != nil {
			return nil, err
		}
		if node != nil {
			nodes = append(nodes, node)
		}
	}
	return nodes, nil
}

func (e *Engine) relatedByPatterns(ctx context.Context, opts RelatedContextOptions, title string, patterns []string) (*DirectToolResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 30 {
		limit = 30
	}

	var candidates []SearchNode
	if opts.Query != "" {
		found, err := e.SearchSubstrate(ctx, SearchOptions{Query: opts.Query, Type: opts.Type, Limit: limit})
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, found...)
	}

	if opts.NodeID != "" || opts.CanonicalID != "" {
		node, err := e.resolveDirectToolNode(ctx, DirectToolOptions{NodeID: opts.NodeID, CanonicalID: opts.CanonicalID, Type: opts.Type})
		if err != nil {
			return nil, err
		}
		if node != nil {
			candidates = append(candidates, SearchNode{
				ID: string(node.ID), Type: node.Type, Label: node.Label, CanonicalID: node.CanonicalID,
				SourceClass: string(node.SourceClass), FilePath: nodeFilePath(node),
			})
		}
	}

	queryTerms := append(searchTokens(opts.Query), patterns...)
	seen := make(map[string]struct{})
	var matches []SearchNode
	for _, term := range queryTerms {
		found, err := e.SearchSubstrate(ctx, SearchOptions{Query: term, Limit: limit})
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, found...)
	}

	for _, c := range candidates {
		haystack := strings.ToLower(c.CanonicalID + " " + c.Label + " " + c.FilePath)
		for _, p := range patterns {
			if strings.Contains(haystack, strings.ToLower(p)) {
				if _, ok := seen[c.ID]; ok {
					continue
				}
				seen[c.ID] = struct{}{}
				c.MatchReason = "matched " + title + " pattern: " + p
				matches = append(matches, c)
				break
			}
		}
		if len(matches) >= limit {
			break
		}
	}

	if len(matches) == 0 {
		return &DirectToolResult{Content: "No " + title + " found."}, nil
	}

	var b strings.Builder
	b.WriteString("## " + title + "\n\n")
	for _, m := range matches {
		b.WriteString(fmt.Sprintf("- [%s] `%s`\n  id: %s\n", m.Type, m.CanonicalID, m.ID))
		if m.FilePath != "" {
			b.WriteString("  file: " + m.FilePath)
			if m.LineStart > 0 {
				b.WriteString(fmt.Sprintf(":%d-%d", m.LineStart, m.LineEnd))
			}
			b.WriteString("\n")
		}
		if m.MatchReason != "" {
			b.WriteString("  reason: " + m.MatchReason + "\n")
		}
	}
	return &DirectToolResult{Content: strings.TrimSpace(b.String())}, nil
}

func sourceSnippet(filePath string, start, end, contextLines int) (string, error) {
	path := filePath
	if !filepath.IsAbs(path) {
		path = filepath.Clean(filePath)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", filePath, err)
	}
	lines := strings.Split(string(content), "\n")
	if start > len(lines) {
		return "", fmt.Errorf("line %d beyond file length %d", start, len(lines))
	}
	if end > len(lines) {
		end = len(lines)
	}
	from := start - contextLines
	if from < 1 {
		from = 1
	}
	to := end + contextLines
	if to > len(lines) {
		to = len(lines)
	}
	if to-from > 80 {
		to = from + 80
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("`%s:%d-%d`\n", filePath, start, end))
	b.WriteString("```text\n")
	for i := from; i <= to; i++ {
		b.WriteString(fmt.Sprintf("%5d  %s\n", i, lines[i-1]))
	}
	b.WriteString("```")
	return b.String(), nil
}

func compactNode(n *core.Node) string {
	filePath := nodeFilePath(n)
	loc := ""
	start, end := nodeLineRange(n.Properties)
	if filePath != "" {
		loc = " (" + filePath
		if start > 0 {
			loc += fmt.Sprintf(":%d-%d", start, end)
		}
		loc += ")"
	}
	return fmt.Sprintf("[%s] `%s` id: `%s`%s", n.Type, n.CanonicalID, n.ID, loc)
}

func limitSection(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "\n...[truncated]"
}
