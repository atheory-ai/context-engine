package indexer

import (
	"context"
	"sync"
)

const admissionUnit = 64 * 1024

// byteLimiter is a weighted, all-or-nothing admission gate. A channel token
// loop is not sufficient here: several workers can each acquire a fraction of
// a large reservation and deadlock while none has enough capacity to proceed.
type byteLimiter struct {
	mu       sync.Mutex
	capacity int
	used     int
	notify   chan struct{}
}

func newByteLimiter(bytes int) *byteLimiter {
	n := bytes / admissionUnit
	if n < 1 {
		n = 1
	}
	return &byteLimiter{capacity: n, notify: make(chan struct{})}
}

func (l *byteLimiter) acquire(ctx context.Context, bytes int) (func(), error) {
	n := (bytes + admissionUnit - 1) / admissionUnit
	if n < 1 {
		n = 1
	}
	l.mu.Lock()
	capacity := l.capacity
	l.mu.Unlock()
	if n > capacity {
		n = capacity
	}
	for {
		l.mu.Lock()
		if l.used+n <= l.capacity {
			l.used += n
			l.mu.Unlock()
			break
		}
		notify := l.notify
		l.mu.Unlock()
		select {
		case <-notify:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return func() {
		l.mu.Lock()
		l.used -= n
		close(l.notify)
		l.notify = make(chan struct{})
		l.mu.Unlock()
	}, nil
}
