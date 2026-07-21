package indexer

import "context"

const admissionUnit = 64 * 1024

type byteLimiter struct{ slots chan struct{} }

func newByteLimiter(bytes int) *byteLimiter {
	n := bytes / admissionUnit
	if n < 1 {
		n = 1
	}
	return &byteLimiter{slots: make(chan struct{}, n)}
}

func (l *byteLimiter) acquire(ctx context.Context, bytes int) (func(), error) {
	n := (bytes + admissionUnit - 1) / admissionUnit
	if n < 1 {
		n = 1
	}
	if n > cap(l.slots) {
		n = cap(l.slots)
	}
	for i := 0; i < n; i++ {
		select {
		case l.slots <- struct{}{}:
		case <-ctx.Done():
			for ; i > 0; i-- {
				<-l.slots
			}
			return nil, ctx.Err()
		}
	}
	return func() {
		for ; n > 0; n-- {
			<-l.slots
		}
	}, nil
}
