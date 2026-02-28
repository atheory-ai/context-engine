// Package progress defines the IndexProgress type emitted during indexing.
package progress

import "fmt"

// IndexProgress is emitted periodically during indexing.
// Rendered to a string and sent to core.ChanProgress.
type IndexProgress struct {
	FilesProcessed int
	FilesTotal     int // -1 if unknown (streaming walk)
	NodesCreated   int
	EdgesCreated   int
	CurrentFile    string
	ElapsedMS      int64
}

// Render returns a human-readable progress string.
func (p *IndexProgress) Render() string {
	if p.FilesTotal > 0 {
		pct := float64(p.FilesProcessed) / float64(p.FilesTotal) * 100
		return fmt.Sprintf("indexing: %.0f%% (%d/%d files, %d nodes)",
			pct, p.FilesProcessed, p.FilesTotal, p.NodesCreated)
	}
	return fmt.Sprintf("indexing: %d files, %d nodes, %d edges",
		p.FilesProcessed, p.NodesCreated, p.EdgesCreated)
}
