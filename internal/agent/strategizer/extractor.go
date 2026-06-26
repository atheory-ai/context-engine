package strategizer

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/atheory-ai/context-engine/internal/core"
)

// Extract parses the Strategizer's response text and returns an IR.
// Returns an error only if required tags are absent or have invalid values.
// Missing optional tags use defaults. Extra text between tags is ignored.
//
// This is NOT a full XML parser. It searches for known tags by name,
// extracts their content, and parses attributes with a simple scanner.
// A lenient extractor is more reliable than a strict one for LLM output.
func Extract(response string) (*core.IR, error) {
	ir := &core.IR{}

	// ── mode ──────────────────────────────────────────────────────────────
	mode, err := extractText(response, "mode")
	if err != nil {
		// mode is required — default to thinking if absent
		ir.Mode = core.IRModeThinking
	} else {
		switch strings.TrimSpace(mode) {
		case "thinking":
			ir.Mode = core.IRModeThinking
		case "direct":
			ir.Mode = core.IRModeDirect
		case "audit":
			ir.Mode = core.IRModeAudit
		default:
			ir.Mode = core.IRModeThinking // unknown value → thinking
		}
	}

	// ── anchors ───────────────────────────────────────────────────────────
	ir.Anchors = extractAnchors(response)

	// ── predicates ────────────────────────────────────────────────────────
	ir.Predicates = extractPredicates(response)

	// ── open_queries ──────────────────────────────────────────────────────
	ir.OpenQueries = extractOpenQueries(response)

	// ── max_loops ─────────────────────────────────────────────────────────
	if v, err := extractInt(response, "max_loops", 1, 20); err == nil {
		ir.MaxLoops = v
	}
	// 0 = use project default (resolved in runner)

	// ── k_limit ───────────────────────────────────────────────────────────
	if v, err := extractInt(response, "k_limit", 10, 100); err == nil {
		ir.KLimit = v
	}

	// ── role_hint ─────────────────────────────────────────────────────────
	if v, err := extractText(response, "role_hint"); err == nil {
		ir.RoleHint = strings.TrimSpace(v)
	}

	// ── model_tier ────────────────────────────────────────────────────────
	if v, err := extractText(response, "model_tier"); err == nil {
		tier := strings.TrimSpace(v)
		switch tier {
		case "fast", "standard", "thinking":
			ir.ModelTier = tier
		}
	}

	return ir, nil
}

// ── Extraction helpers ─────────────────────────────────────────────────────

// extractText finds the content of a simple tag: <tag>content</tag>
// Returns error if tag not found.
func extractText(s, tag string) (string, error) {
	openTag := "<" + tag + ">"
	closeTag := "</" + tag + ">"
	start := strings.Index(s, openTag)
	if start == -1 {
		return "", fmt.Errorf("tag <%s> not found", tag)
	}
	start += len(openTag)
	end := strings.Index(s[start:], closeTag)
	if end == -1 {
		return "", fmt.Errorf("tag </%s> not found", tag)
	}
	return s[start : start+end], nil
}

// extractInt finds a tag with integer content, clamped to [lo, hi].
func extractInt(s, tag string, lo, hi int) (int, error) {
	v, err := extractText(s, tag)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0, fmt.Errorf("tag <%s> not an integer: %s", tag, v)
	}
	if n < lo {
		n = lo
	}
	if n > hi {
		n = hi
	}
	return n, nil
}

// anchorRegex matches <anchor .../> and <anchor ...> (open tag only).
// Using (?:/>|>) as the close to handle both self-closing and bare open tags,
// making the extractor lenient against malformed LLM output that omits </anchor>.
var anchorRegex = regexp.MustCompile(`<anchor\s+([^>]*?)(?:/>|>)`)

var attrRegex = regexp.MustCompile(`(\w[\w-]*)="([^"]*)"`)

func extractAnchors(s string) []core.AnchorRef {
	var anchors []core.AnchorRef
	matches := anchorRegex.FindAllStringSubmatch(s, -1)
	for _, m := range matches {
		attrs := parseAttrs(m[1])
		anchorType, ok1 := attrs["type"]
		anchorID, ok2 := attrs["id"]
		if !ok1 || !ok2 || anchorID == "" {
			continue
		}
		confidence := attrs["confidence"]
		if confidence == "" {
			confidence = "medium"
		}
		// Validate type
		switch anchorType {
		case "symbol", "namespace", "concept", "file":
			// valid
		default:
			continue // skip unknown types
		}
		anchors = append(anchors, core.AnchorRef{
			Type:       anchorType,
			ID:         anchorID,
			Confidence: confidence,
		})
	}
	return anchors
}

func extractPredicates(s string) map[string]string {
	predicates := make(map[string]string)

	predRegex := regexp.MustCompile(`<predicate\s+([^>]*?)(?:/>|>)`)
	matches := predRegex.FindAllStringSubmatch(s, -1)
	for _, m := range matches {
		attrs := parseAttrs(m[1])
		name, ok1 := attrs["name"]
		value, ok2 := attrs["value"]
		if !ok1 || !ok2 || name == "" {
			continue
		}
		predicates[name] = value
	}
	return predicates
}

func extractOpenQueries(s string) []string {
	var queries []string

	// Find the <open_queries> container first
	container, err := extractText(s, "open_queries")
	if err != nil {
		return queries
	}

	// Extract individual <open_query> tags within the container
	oqRegex := regexp.MustCompile(`<open_query>(.*?)</open_query>`)
	matches := oqRegex.FindAllStringSubmatch(container, -1)
	for _, m := range matches {
		q := strings.TrimSpace(m[1])
		if q != "" {
			queries = append(queries, q)
		}
	}
	return queries
}

func parseAttrs(attrStr string) map[string]string {
	attrs := make(map[string]string)
	matches := attrRegex.FindAllStringSubmatch(attrStr, -1)
	for _, m := range matches {
		attrs[m[1]] = m[2]
	}
	return attrs
}
