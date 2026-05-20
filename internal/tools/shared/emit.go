package shared

import "github.com/atheory-ai/context-engine/internal/core"

// Thinking returns a ChanThinking emission for a tool.
func Thinking(req core.ToolRequest, source, content string, metadata map[string]any) core.Emission {
	return core.Emission{
		RunID:     req.RunID,
		TurnID:    req.TurnID,
		LoopIndex: req.LoopIndex,
		Source:    "tool:" + source,
		Channel:   core.ChanThinking,
		Content:   content,
		Metadata:  metadata,
	}
}
