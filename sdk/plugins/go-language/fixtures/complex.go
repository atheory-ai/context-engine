package service

import (
	"context"
	"github.com/atheory-ai/context-engine/internal/core"
	"sync"
)

// Engine orchestrates the cognitive loop.
type Engine struct {
	mu      sync.RWMutex
	plugins map[string]Plugin
	cache   Cache
}

// Plugin defines the contract for engine plugins.
type Plugin interface {
	Name() string
	Execute(ctx context.Context, ir *core.IR) (*core.Result, error)
	Close() error
}

// NewEngine creates a new Engine instance.
func NewEngine(cache Cache) *Engine {
	return &Engine{
		plugins: make(map[string]Plugin),
		cache:   cache,
	}
}

// Register adds a plugin to the engine.
func (e *Engine) Register(p Plugin) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.plugins[p.Name()] = p
}

// Run executes all registered plugins for the given IR.
func (e *Engine) Run(ctx context.Context, ir *core.IR) ([]*core.Result, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	results := make([]*core.Result, 0, len(e.plugins))
	for _, p := range e.plugins {
		r, err := p.Execute(ctx, ir)
		if err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, nil
}

// Close shuts down all plugins.
func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, p := range e.plugins {
		if err := p.Close(); err != nil {
			return err
		}
	}
	return nil
}

// Result wraps engine output.
type Result struct {
	Data     map[string]interface{}
	Metadata map[string]string
}
