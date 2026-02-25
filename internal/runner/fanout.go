package runner

import (
	"fmt"
	"strings"
	"sync"

	"github.com/atheory/context-engine/internal/core"
)

// fanoutNode selects which tools activate on the current IR and executes
// them concurrently. One goroutine per activating tool.
type fanoutNode struct {
	tools []core.Tool
}

// Run selects activating tools, executes them concurrently, collects results.
// Tool errors are non-fatal unless ALL tools fail.
func (f *fanoutNode) Run(rc *core.RunContext) ([]core.Emission, error) {
	anchors := rc.ReadAnchors()

	// ── Tool selection ─────────────────────────────────────────────────────
	var activating []core.Tool
	for _, tool := range f.tools {
		if tool.Activate(*rc.IR) {
			activating = append(activating, tool)
		}
	}

	if len(activating) == 0 {
		rc.Ch.Emit(core.Emission{
			RunID:   rc.RunID,
			TurnID:  rc.TurnID,
			Channel: core.ChanWarning,
			Content: "no tools activated for current IR",
		})
		return nil, nil
	}

	rc.Ch.Emit(core.Emission{
		RunID:   rc.RunID,
		TurnID:  rc.TurnID,
		Channel: core.ChanAction,
		Content: fmt.Sprintf("activating %d tools: %s", len(activating), toolNames(activating)),
	})

	// ── Concurrent execution ───────────────────────────────────────────────
	type result struct {
		emissions []core.Emission
		err       error
		toolName  string
	}

	resultCh := make(chan result, len(activating))
	var wg sync.WaitGroup

	for _, tool := range activating {
		wg.Add(1)
		go func(t core.Tool) {
			defer wg.Done()

			// Respect cancellation before starting work.
			select {
			case <-rc.Ctx.Done():
				resultCh <- result{toolName: t.Name(), err: rc.Ctx.Err()}
				return
			default:
			}

			req := core.ToolRequest{
				RunID:     rc.RunID,
				TurnID:    rc.TurnID,
				LoopIndex: rc.CurrentLoop(),
				IR:        *rc.IR,
				Anchors:   anchors,
			}

			toolResult, err := t.Execute(rc.Ctx, req)
			if err != nil {
				resultCh <- result{toolName: t.Name(), err: err}
				return
			}

			rc.Ch.Emit(core.Emission{
				RunID:   rc.RunID,
				TurnID:  rc.TurnID,
				Channel: core.ChanAction,
				Content: fmt.Sprintf("tool:%s complete (%d emissions)",
					t.Name(), len(toolResult.Emissions)),
			})

			// Proposed substrate changes are noted via thinking channel
			// for the Reviewer to evaluate.
			if len(toolResult.ProposedNodes)+len(toolResult.ProposedEdges) > 0 {
				rc.Ch.Emit(core.Emission{
					RunID:   rc.RunID,
					TurnID:  rc.TurnID,
					Channel: core.ChanThinking,
					Content: fmt.Sprintf("tool:%s proposed %d nodes, %d edges",
						t.Name(),
						len(toolResult.ProposedNodes),
						len(toolResult.ProposedEdges)),
					Metadata: map[string]any{
						"proposed_nodes": toolResult.ProposedNodes,
						"proposed_edges": toolResult.ProposedEdges,
						"tool":           t.Name(),
					},
				})
			}

			resultCh <- result{toolName: t.Name(), emissions: toolResult.Emissions}
		}(tool)
	}

	// Close results channel when all goroutines complete.
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// ── Collect results ────────────────────────────────────────────────────
	var allEmissions []core.Emission
	var errs []error

	for r := range resultCh {
		if r.err != nil {
			errs = append(errs, fmt.Errorf("tool %s: %w", r.toolName, r.err))
			rc.Ch.Emit(core.Emission{
				RunID:   rc.RunID,
				TurnID:  rc.TurnID,
				Channel: core.ChanError,
				Content: fmt.Sprintf("tool %s failed: %v", r.toolName, r.err),
			})
			continue
		}
		allEmissions = append(allEmissions, r.emissions...)
	}

	// If ALL tools failed, that's a hard error — the loop cannot continue.
	if len(errs) > 0 && len(errs) == len(activating) {
		return nil, fmt.Errorf("all %d tools failed: %v", len(activating), errs)
	}

	return allEmissions, nil
}

func toolNames(tools []core.Tool) string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name()
	}
	return strings.Join(names, ", ")
}
