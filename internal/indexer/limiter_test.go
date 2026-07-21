package indexer

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestCompactCSTDropsDuplicatedTextAndSource(t *testing.T) {
	compact, err := compactCST([]byte(`{"source":"package p","root":{"text":"package p","startByte":0,"endByte":9,"children":[{"text":"p","startByte":8,"endByte":9}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(compact, &value); err != nil {
		t.Fatal(err)
	}
	if _, ok := value["source"]; ok {
		t.Fatal("source retained")
	}
	root := value["root"].(map[string]any)
	if _, ok := root["text"]; ok {
		t.Fatal("node text retained")
	}
	if root["startByte"] != float64(0) || root["endByte"] != float64(9) {
		t.Fatal("offsets changed")
	}
}

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
