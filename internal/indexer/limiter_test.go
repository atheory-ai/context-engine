package indexer

import (
	"context"
	"testing"
	"time"
)

func TestByteLimiterBlocksAndReleases(t *testing.T) {
	l := newByteLimiter(admissionUnit)
	release, err := l.acquire(context.Background(), admissionUnit)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		r, err := l.acquire(context.Background(), admissionUnit)
		if err == nil {
			r()
		}
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("second admission bypassed limit")
	case <-time.After(10 * time.Millisecond):
	}
	release()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("admission did not resume")
	}
}

func TestByteLimiterOversizedAndCancelled(t *testing.T) {
	l := newByteLimiter(admissionUnit)
	r, err := l.acquire(context.Background(), admissionUnit*32)
	if err != nil {
		t.Fatal(err)
	}
	r()
	r, err = l.acquire(context.Background(), admissionUnit)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := l.acquire(ctx, admissionUnit); err == nil {
		t.Fatal("cancelled acquisition succeeded")
	}
	r()
}

func TestByteLimiterLargeWaiterDoesNotHoldPartialCapacity(t *testing.T) {
	l := newByteLimiter(admissionUnit * 4)
	firstRelease, err := l.acquire(context.Background(), admissionUnit*3)
	if err != nil {
		t.Fatal(err)
	}

	largeAdmitted := make(chan func(), 1)
	go func() {
		release, err := l.acquire(context.Background(), admissionUnit*3)
		if err == nil {
			largeAdmitted <- release
		}
	}()

	// The large waiter must remain queued without consuming a partial token,
	// leaving the last unit available to a small request.
	smallRelease, err := l.acquire(context.Background(), admissionUnit)
	if err != nil {
		t.Fatal(err)
	}
	smallRelease()
	select {
	case release := <-largeAdmitted:
		release()
		t.Fatal("large waiter acquired while only one unit was free")
	case <-time.After(10 * time.Millisecond):
	}

	firstRelease()
	select {
	case release := <-largeAdmitted:
		release()
	case <-time.After(time.Second):
		t.Fatal("large waiter remained blocked after full capacity became free")
	}
}

func TestEstimateInFlightBytesAccountsForSerializedTreeAndPluginCopies(t *testing.T) {
	idx := &Indexer{}
	// With no registry, the estimator still reserves for CE and one possible
	// ABI consumer: 16x the source for each compact-binary copy.
	if got, want := idx.estimateInFlightBytes(1024), 1024*16*2; got != want {
		t.Fatalf("estimate = %d, want %d", got, want)
	}
	if got := idx.estimateInFlightBytes(1 << 62); got <= 0 {
		t.Fatalf("overflow estimate = %d", got)
	}
}
