package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var updateGolden = flag.Bool("update", false, "update golden files")

func assertGolden(t *testing.T, name string, value any) {
	t.Helper()

	got, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden value: %v", err)
	}
	got = append(got, '\n')

	path := filepath.Join("testdata", name+".golden.json")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create golden dir: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden file: %v", err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden file %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("golden mismatch for %s\nwant:\n%s\ngot:\n%s", name, want, got)
	}
}

func TestIRValidationGolden(t *testing.T) {
	cases := []struct {
		name string
		ir   IR
	}{
		{
			name: "valid_with_soft_corrections",
			ir: IR{
				Mode: IRModeThinking,
				Anchors: []AnchorRef{
					{Type: NodeTypeSymbol, ID: "github.com/atheory-ai/context-engine/internal/core.IR", Confidence: "certain"},
					{Type: NodeTypeFile, ID: "internal/core/ir.go", Confidence: "high"},
				},
				Predicates: map[string]string{
					"callgraph": "true",
					"concepts":  "false",
					"summary":   "maybe",
				},
				OpenQueries: []string{"How is IR validation normalized?"},
				MaxLoops:    99,
				KLimit:      -1,
				RoleHint:    "architect",
				ModelTier:   TierThinking,
			},
		},
		{
			name: "missing_anchors",
			ir: IR{
				OpenQueries: []string{"What should fail?"},
			},
		},
		{
			name: "missing_open_queries",
			ir: IR{
				Anchors: []AnchorRef{{Type: NodeTypeConcept, ID: "activation", Confidence: "low"}},
			},
		},
		{
			name: "invalid_anchor_type",
			ir: IR{
				Anchors:     []AnchorRef{{Type: "package", ID: "core", Confidence: "medium"}},
				OpenQueries: []string{"What should fail?"},
			},
		},
		{
			name: "empty_anchor_id",
			ir: IR{
				Anchors:     []AnchorRef{{Type: NodeTypeNamespace, Confidence: "medium"}},
				OpenQueries: []string{"What should fail?"},
			},
		},
	}

	out := make([]map[string]any, 0, len(cases))
	for _, tc := range cases {
		ir := tc.ir
		err := ir.Validate()
		row := map[string]any{
			"name":       tc.name,
			"valid":      err == nil,
			"invalid_ir": errors.Is(err, ErrInvalidIR),
			"ir":         ir,
		}
		if err != nil {
			row["error"] = err.Error()
		}
		out = append(out, row)
	}

	assertGolden(t, "ir_validation", out)
}

func TestChannelEmissionsGolden(t *testing.T) {
	ch := NewAppChannels()
	emissions := []Emission{
		{RunID: "run-1", TurnID: "turn-1", LoopIndex: 1, Source: "strategizer", Channel: ChanThinking, Content: "thinking"},
		{RunID: "run-1", TurnID: "turn-1", LoopIndex: 1, Source: "tool:callgraph", Channel: ChanAction, Content: "action"},
		{RunID: "run-1", TurnID: "turn-1", LoopIndex: 1, Source: "synthesizer", Channel: ChanMessage, Content: "message", Markdown: true},
		{RunID: "run-1", TurnID: "turn-1", LoopIndex: 1, Source: "debug", Channel: ChanDebug, Content: "debug"},
		{RunID: "run-1", TurnID: "turn-1", LoopIndex: 1, Source: "runner", Channel: ChanError, Content: "error"},
		{RunID: "run-1", TurnID: "turn-1", LoopIndex: 1, Source: "runner", Channel: ChanWarning, Content: "warning"},
		{RunID: "run-1", TurnID: "turn-1", LoopIndex: 1, Source: "indexer", Channel: ChanProgress, Content: "progress"},
		{RunID: "run-1", TurnID: "turn-1", LoopIndex: 1, Source: "reviewer", Channel: ChanCoverage, Content: "coverage"},
		{RunID: "run-1", TurnID: "turn-1", LoopIndex: 1, Source: "budget", Channel: ChanCost, Content: "cost"},
		{RunID: "run-1", TurnID: "turn-1", LoopIndex: 1, Source: "runner", Channel: ChanSystem, Content: "system"},
		{RunID: "run-1", TurnID: "turn-1", Source: "unknown", Channel: ChannelType("unknown"), Content: "dropped"},
	}
	for _, e := range emissions {
		ch.Emit(e)
	}

	messageCap := cap(ch.Message)
	for i := 0; i < messageCap; i++ {
		ch.Emit(Emission{Channel: ChanMessage, Content: "overflow"})
	}

	out := map[string]any{
		"capacities": map[string]int{
			"thinking": cap(ch.Thinking),
			"action":   cap(ch.Action),
			"message":  cap(ch.Message),
			"debug":    cap(ch.Debug),
			"error":    cap(ch.Error),
			"warning":  cap(ch.Warning),
			"progress": cap(ch.Progress),
			"coverage": cap(ch.Coverage),
			"cost":     cap(ch.Cost),
			"system":   cap(ch.System),
		},
		"lengths_after_emit": map[string]int{
			"thinking": len(ch.Thinking),
			"action":   len(ch.Action),
			"message":  len(ch.Message),
			"debug":    len(ch.Debug),
			"error":    len(ch.Error),
			"warning":  len(ch.Warning),
			"progress": len(ch.Progress),
			"coverage": len(ch.Coverage),
			"cost":     len(ch.Cost),
			"system":   len(ch.System),
		},
		"routed": map[string]Emission{
			"thinking": <-ch.Thinking,
			"action":   <-ch.Action,
			"message":  <-ch.Message,
			"debug":    <-ch.Debug,
			"error":    <-ch.Error,
			"warning":  <-ch.Warning,
			"progress": <-ch.Progress,
			"coverage": <-ch.Coverage,
			"cost":     <-ch.Cost,
			"system":   <-ch.System,
		},
		"message_len_after_first_drain": len(ch.Message),
	}

	assertGolden(t, "channel_emissions", out)
}

func TestIDsGolden(t *testing.T) {
	inputs := []map[string]string{
		{
			"project_id":   "project-alpha",
			"node_type":    NodeTypeSymbol,
			"canonical_id": "github.com/atheory-ai/context-engine/internal/core.IR.Validate",
		},
		{
			"project_id":   "project-alpha",
			"node_type":    NodeTypeFile,
			"canonical_id": "internal/core/ir.go",
		},
	}

	nodes := make([]map[string]string, 0, len(inputs))
	for _, input := range inputs {
		nodes = append(nodes, map[string]string{
			"project_id":   input["project_id"],
			"node_type":    input["node_type"],
			"canonical_id": input["canonical_id"],
			"node_id":      MakeNodeID(input["project_id"], input["node_type"], input["canonical_id"]),
		})
	}

	edgeSource := nodes[0]["node_id"]
	edgeTarget := nodes[1]["node_id"]
	out := map[string]any{
		"nodes": nodes,
		"edge": map[string]string{
			"source_id": edgeSource,
			"edge_type": EdgeTypeDefines,
			"target_id": edgeTarget,
			"edge_id":   MakeEdgeID(edgeSource, EdgeTypeDefines, edgeTarget),
		},
	}

	assertGolden(t, "ids", out)
}

func TestBudgetGolden(t *testing.T) {
	b := NewBudget(1_000)
	b.Record(300, 200, 0.012345)

	exitBudget := NewBudget(10)
	exitBudget.Record(8, 1, 0.000001)

	defaultBudget := NewBudget(0)

	out := map[string]any{
		"recorded": map[string]any{
			"tokens_in":        b.TokensIn(),
			"tokens_out":       b.TokensOut(),
			"total_cost_usd":   b.TotalCostUSD(),
			"context_used_pct": b.ContextUsedPct(),
			"should_exit":      b.ShouldExit(),
			"summary": b.Summary(&RunContext{
				RunID:  "run-budget",
				TurnID: "turn-budget",
			}),
		},
		"exit_budget": map[string]any{
			"context_used_pct": exitBudget.ContextUsedPct(),
			"should_exit":      exitBudget.ShouldExit(),
		},
		"default_limit_budget": map[string]any{
			"context_used_pct": defaultBudget.ContextUsedPct(),
			"should_exit":      defaultBudget.ShouldExit(),
		},
	}

	assertGolden(t, "budget", out)
}

func TestRunContextGolden(t *testing.T) {
	ch := NewAppChannels()
	budget := NewBudget(4_000)
	rc := &RunContext{
		Ctx:       context.Background(),
		RunID:     "run-core",
		TurnID:    "turn-core",
		SessionID: "session-core",
		ProjectID: "project-core",
		Query:     "How does core state flow?",
		Budget:    budget,
		MaxLoops:  3,
		Ch:        &ch,
	}

	firstLoop := rc.IncrementLoop()
	secondLoop := rc.IncrementLoop()
	rc.AppendEmissions([]Emission{
		{RunID: rc.RunID, TurnID: rc.TurnID, LoopIndex: firstLoop, Source: "strategizer", Channel: ChanThinking, Content: "compiled IR"},
		{RunID: rc.RunID, TurnID: rc.TurnID, LoopIndex: secondLoop, Source: "tool:summary", Channel: ChanAction, Content: "summarized namespace"},
	})
	rc.SetAnchors([]Anchor{
		{
			Ref:        AnchorRef{Type: NodeTypeSymbol, ID: "core.RunContext", Confidence: "high"},
			Node:       &Node{ID: "node-run-context", ProjectID: rc.ProjectID, Type: NodeTypeSymbol, Label: "RunContext"},
			Activation: 0.95,
		},
	})

	anchors := rc.ReadAnchors()
	anchors[0].Activation = 0.1
	agent := rc.AgentContext()

	out := map[string]any{
		"loop_values": []int{firstLoop, secondLoop, rc.CurrentLoop()},
		"emissions":   rc.Emissions,
		"anchors": map[string]any{
			"first_read_mutated": anchors,
			"second_read":        rc.ReadAnchors(),
		},
		"agent_context": map[string]any{
			"run_id":       agent.RunID,
			"turn_id":      agent.TurnID,
			"project_id":   agent.ProjectID,
			"query":        agent.Query,
			"loop_index":   agent.LoopIndex,
			"has_budget":   agent.Budget != nil,
			"has_channels": agent.Ch != nil,
			"has_context":  agent.Ctx != nil,
		},
	}

	assertGolden(t, "run_context", out)
}

func TestToolContractGolden(t *testing.T) {
	var _ Tool = goldenTool{}

	tool := goldenTool{}
	activeIR := IR{Predicates: map[string]string{"golden": "true"}}
	inactiveIR := IR{Predicates: map[string]string{"golden": "false"}}
	req := ToolRequest{
		RunID:     "run-tool",
		TurnID:    "turn-tool",
		LoopIndex: 2,
		ProjectID: "project-tool",
		IR:        activeIR,
		Anchors: []Anchor{
			{Ref: AnchorRef{Type: NodeTypeFile, ID: "internal/core/interfaces.go", Confidence: "medium"}, Activation: 0.7},
		},
	}

	result, err := tool.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("execute golden tool: %v", err)
	}

	out := map[string]any{
		"name":                 tool.Name(),
		"description":          tool.Description(),
		"active":               tool.Activate(activeIR),
		"inactive":             tool.Activate(inactiveIR),
		"request_identity":     map[string]any{"run_id": req.RunID, "turn_id": req.TurnID, "loop_index": req.LoopIndex, "project_id": req.ProjectID},
		"request_anchor_count": len(req.Anchors),
		"result":               result,
	}

	assertGolden(t, "tool_contract", out)
}

type goldenTool struct{}

func (goldenTool) Name() string { return "golden" }

func (goldenTool) Description() string { return "Golden test tool contract" }

func (goldenTool) Activate(ir IR) bool {
	return ir.Predicates["golden"] == "true"
}

func (goldenTool) Execute(_ context.Context, req ToolRequest) (ToolResult, error) {
	return ToolResult{
		Emissions: []Emission{
			{
				RunID:     req.RunID,
				TurnID:    req.TurnID,
				LoopIndex: req.LoopIndex,
				Source:    "tool:golden",
				Channel:   ChanAction,
				Content:   "processed 1 anchor",
			},
		},
		ProposedNodes: []Node{
			{
				ID:          "node-proposed",
				ProjectID:   req.ProjectID,
				Type:        NodeTypeConcept,
				Label:       "golden contract",
				CanonicalID: "concept:golden-contract",
				SourceClass: SourceDerived,
			},
		},
		ProposedEdges: []Edge{
			{
				ID:          "edge-proposed",
				ProjectID:   req.ProjectID,
				SourceID:    "node-proposed",
				TargetID:    "node-existing",
				Type:        EdgeTypeAnnotates,
				SourceClass: SourceDerived,
				Weight:      0.5,
			},
		},
	}, nil
}
