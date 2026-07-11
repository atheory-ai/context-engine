package preflight

import (
	"context"
	"testing"
)

func TestRun_EmptyQuery(t *testing.T) {
	// The empty-query guard fires before any database access, so the node needs
	// no wired dependencies to exercise it.
	n := New(nil, nil)
	_, err := n.Run(context.Background(), "", nil, nil)
	if err == nil {
		t.Fatal("expected an error for an empty query")
	}
}
