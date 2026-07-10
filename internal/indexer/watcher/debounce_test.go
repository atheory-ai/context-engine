package watcher

import (
	"testing"
	"time"
)

func TestDebouncer_CoalescesAndFlushes(t *testing.T) {
	d := NewDebouncer(20 * time.Millisecond)
	d.Add("a.go")
	d.Add("a.go") // duplicate — coalesced
	d.Add("b.go")

	select {
	case <-d.Ready():
	case <-time.After(2 * time.Second):
		t.Fatal("debouncer never signaled ready")
	}

	paths := d.Flush()
	if len(paths) != 2 {
		t.Fatalf("flushed %v, want 2 unique paths", paths)
	}
	set := map[string]bool{paths[0]: true, paths[1]: true}
	if !set["a.go"] || !set["b.go"] {
		t.Errorf("flushed paths = %v", paths)
	}

	// A second flush after draining is empty.
	if again := d.Flush(); len(again) != 0 {
		t.Errorf("second flush = %v, want empty", again)
	}
}

func TestDebouncer_ResetExtendsWindow(t *testing.T) {
	d := NewDebouncer(40 * time.Millisecond)
	d.Add("x")
	// Adding again before the window elapses resets the timer; it should still
	// eventually fire.
	time.Sleep(15 * time.Millisecond)
	d.Add("y")
	select {
	case <-d.Ready():
	case <-time.After(2 * time.Second):
		t.Fatal("debouncer never signaled ready after reset")
	}
	if paths := d.Flush(); len(paths) != 2 {
		t.Errorf("flushed %v, want 2", paths)
	}
}
