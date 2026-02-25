package core

import (
	"context"
	"sync"
	"sync/atomic"
)

// RunContext carries all state for a single query execution.
// Created by the preflight node, threaded through every node and goroutine.
// Thread-safe where noted — multiple tool goroutines share this.
type RunContext struct {
	// Go context — cancellation propagates to all goroutines.
	Ctx context.Context

	// Identity
	RunID     RunID
	TurnID    TurnID
	SessionID SessionID
	ProjectID ProjectID

	// Query is the raw user query text. Set by preflight.
	Query string

	// IR is the compiled intent produced by the Strategizer.
	// Written once after Strategizer returns; never modified after that.
	IR *IR

	// Budget tracks token usage across the entire query. Thread-safe.
	Budget *Budget

	// Loop state
	LoopIndex int32 // atomic — readable from concurrent goroutines
	MaxLoops  int   // resolved from IR.MaxLoops or project default

	// Accumulated emissions across all loop iterations.
	// The Synthesizer reads this at the end to build the final answer.
	Emissions []Emission
	emMu      sync.Mutex // guards Emissions

	// ForcedExit is set to true when the budget guard triggers early exit.
	// The Synthesizer checks this to produce a partial answer instead.
	ForcedExit       bool
	ForcedExitReason string

	// Ch is the engine's output channels. All nodes write here.
	Ch *AppChannels

	// Anchors are the resolved substrate nodes from the last activation pass.
	// Updated each loop iteration via SetAnchors; read by the fan-out node.
	Anchors []Anchor
	anchMu  sync.RWMutex
}

// IncrementLoop atomically increments the loop counter and returns the new value.
func (rc *RunContext) IncrementLoop() int {
	return int(atomic.AddInt32(&rc.LoopIndex, 1))
}

// CurrentLoop returns the current loop index atomically.
func (rc *RunContext) CurrentLoop() int {
	return int(atomic.LoadInt32(&rc.LoopIndex))
}

// AppendEmissions safely appends emissions to the accumulated list.
// Called from multiple goroutines during fan-out.
func (rc *RunContext) AppendEmissions(emissions []Emission) {
	rc.emMu.Lock()
	rc.Emissions = append(rc.Emissions, emissions...)
	rc.emMu.Unlock()
}

// SetAnchors replaces the current anchor set after an activation pass.
func (rc *RunContext) SetAnchors(anchors []Anchor) {
	rc.anchMu.Lock()
	rc.Anchors = anchors
	rc.anchMu.Unlock()
}

// ReadAnchors returns a snapshot of the current anchor set.
func (rc *RunContext) ReadAnchors() []Anchor {
	rc.anchMu.RLock()
	defer rc.anchMu.RUnlock()
	out := make([]Anchor, len(rc.Anchors))
	copy(out, rc.Anchors)
	return out
}

// AgentContext creates an AgentContext view of this RunContext.
// Used by agent nodes (Strategizer, Reviewer, Synthesizer) that
// take AgentContext to avoid importing runner.
func (rc *RunContext) AgentContext() *AgentContext {
	return &AgentContext{
		Ctx:       rc.Ctx,
		RunID:     rc.RunID,
		TurnID:    rc.TurnID,
		ProjectID: rc.ProjectID,
		Query:     rc.Query,
		Budget:    rc.Budget,
		Ch:        rc.Ch,
		LoopIndex: rc.CurrentLoop(),
	}
}
