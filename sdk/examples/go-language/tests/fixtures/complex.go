package runner

import (
	"context"
	"fmt"
	"sync"
)

// Engine manages concurrent task execution.
type Engine struct {
	mu      sync.Mutex
	workers int
	tasks   chan Task
}

// Task is a unit of work.
type Task struct {
	ID   string
	Run  func(ctx context.Context) error
}

// Result holds the outcome of a task.
type Result struct {
	TaskID string
	Err    error
}

func NewEngine(workers int) *Engine {
	return &Engine{
		workers: workers,
		tasks:   make(chan Task, 100),
	}
}

// Submit adds a task to the queue.
func (e *Engine) Submit(t Task) {
	e.tasks <- t
}

// Start launches worker goroutines.
func (e *Engine) Start(ctx context.Context, results chan<- Result) {
	for i := 0; i < e.workers; i++ {
		go e.worker(ctx, results)
	}
}

func (e *Engine) worker(ctx context.Context, results chan<- Result) {
	for {
		select {
		case t, ok := <-e.tasks:
			if !ok {
				return
			}
			err := t.Run(ctx)
			results <- Result{TaskID: t.ID, Err: err}
		case <-ctx.Done():
			return
		}
	}
}

// Shutdown closes the task channel and waits.
func (e *Engine) Shutdown() {
	e.mu.Lock()
	defer e.mu.Unlock()
	close(e.tasks)
}

func formatError(taskID string, err error) string {
	return fmt.Sprintf("task %s failed: %v", taskID, err)
}
