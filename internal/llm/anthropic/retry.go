package anthropic

import (
	"context"
	"errors"
	"math/rand"
	"time"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
)

// Retrier executes a function with exponential backoff retry for transient API errors.
type Retrier struct {
	maxRetries int
}

// NewRetrier creates a Retrier that will retry up to maxRetries times.
func NewRetrier(maxRetries int) *Retrier {
	return &Retrier{maxRetries: maxRetries}
}

// Do executes fn with exponential backoff retry on retryable errors.
// Backoff schedule: 1s, 2s, 4s — each with ±20% jitter.
func (r *Retrier) Do(ctx context.Context, fn func() error) error {
	var lastErr error

	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			// Add jitter: ±20%
			jitter := time.Duration(rand.Int63n(int64(backoff / 5)))
			delay := backoff + jitter

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		// Never retry context errors.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}

		if !isRetryable(err) {
			return err
		}
	}

	return lastErr
}

// isRetryable returns true for errors that warrant a retry.
// Retries on rate limits (429), server errors (500, 502, 503), and overloaded (529).
func isRetryable(err error) bool {
	var apiErr *anthropicsdk.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 429, 500, 502, 503, 529:
			return true
		default:
			return false
		}
	}
	// Network-level errors (no HTTP status): retry.
	return true
}
