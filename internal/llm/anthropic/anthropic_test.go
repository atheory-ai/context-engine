package anthropic

import (
	"context"
	"errors"
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
)

func TestModelInfoFor(t *testing.T) {
	if info := ModelInfoFor(ModelSonnet4); info.Tier != core.TierStandard || info.ContextLimit != 200_000 {
		t.Errorf("sonnet info = %+v", info)
	}
	if info := ModelInfoFor(ModelHaiku45); info.Tier != core.TierFast {
		t.Errorf("haiku tier = %q, want fast", info.Tier)
	}
	if info := ModelInfoFor(ModelOpus4); info.Tier != core.TierThinking {
		t.Errorf("opus tier = %q, want thinking", info.Tier)
	}
	// Unknown models fall back to a safe standard-tier default that echoes the ID.
	if info := ModelInfoFor("some-future-model"); info.ID != "some-future-model" || info.Tier != core.TierStandard {
		t.Errorf("unknown model info = %+v", info)
	}
}

func TestRetrier_SucceedsWithoutRetry(t *testing.T) {
	calls := 0
	err := NewRetrier(3).Do(context.Background(), func() error {
		calls++
		return nil
	})
	if err != nil || calls != 1 {
		t.Errorf("Do success: err=%v calls=%d, want nil/1", err, calls)
	}
}

func TestRetrier_ReturnsErrorAfterExhaustion(t *testing.T) {
	// maxRetries=0 exercises the retry path with no backoff delay.
	sentinel := errors.New("boom")
	calls := 0
	err := NewRetrier(0).Do(context.Background(), func() error {
		calls++
		return sentinel
	})
	if !errors.Is(err, sentinel) || calls != 1 {
		t.Errorf("Do exhausted: err=%v calls=%d", err, calls)
	}
}

func TestRetrier_DoesNotRetryContextErrors(t *testing.T) {
	calls := 0
	err := NewRetrier(3).Do(context.Background(), func() error {
		calls++
		return context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if calls != 1 {
		t.Errorf("context errors must not retry, got %d calls", calls)
	}
}

func TestIsRetryable(t *testing.T) {
	// A network-level (non-HTTP) error is retryable.
	if !isRetryable(errors.New("connection reset")) {
		t.Error("network error should be retryable")
	}
}
