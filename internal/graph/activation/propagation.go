package activation

import (
	"context"
	"fmt"

	"github.com/atheory/context-engine/internal/core"
)

// activationEntry tracks a node's current activation and propagation depth.
type activationEntry struct {
	nodeID     core.NodeID
	activation float64
	depth      int
}

// spreadActivation runs the spreading activation algorithm from seed nodes.
// Returns a map of nodeID → final activation value for all activated nodes.
//
// Algorithm (Dijkstra-style max-activation priority):
//  1. Initialize priority queue with seed nodes at their initial activation
//  2. Pop highest-activation node from queue
//  3. For each outgoing edge from that node:
//     a. Compute neighbor activation = current * decay * edge_weight
//     b. If neighbor_activation > threshold AND depth < max_depth:
//     - If neighbor not yet visited OR new activation > existing: push to queue
//  4. Record final activation for each visited node
//  5. Repeat until queue empty
func spreadActivation(
	ctx context.Context,
	substrate core.SubstrateReader,
	projectID core.ProjectID,
	seeds []seedNode,
) (map[core.NodeID]float64, error) {
	// activationMap tracks the best (highest) activation seen for each node.
	activationMap := make(map[core.NodeID]float64)

	// Priority queue — highest activation first.
	pq := newActivationQueue()

	// Initialize with seed nodes.
	for _, seed := range seeds {
		activationMap[seed.node.ID] = seed.activation
		pq.Push(activationEntry{
			nodeID:     seed.node.ID,
			activation: seed.activation,
			depth:      0,
		})
	}

	// Propagate.
	for pq.Len() > 0 {
		// Check context cancellation.
		select {
		case <-ctx.Done():
			return activationMap, ctx.Err()
		default:
		}

		current := pq.Pop()

		// Skip if we've already processed this node at higher activation
		// (stale entry in the priority queue).
		if existing, ok := activationMap[current.nodeID]; ok {
			if existing > current.activation {
				continue
			}
		}

		// Stop propagating if below threshold or at max depth.
		if current.activation < core.ActivationThreshold {
			continue
		}
		if current.depth >= core.MaxPropagationDepth {
			continue
		}

		// Fetch outgoing edges for this node.
		edges, err := substrate.GetEdgesFrom(ctx, projectID, current.nodeID)
		if err != nil {
			return nil, fmt.Errorf("get edges from %s: %w", current.nodeID, err)
		}

		for _, edge := range edges {
			weight := edge.Weight
			if weight <= 0 {
				weight = core.DefaultEdgeWeight
			}
			neighborActivation := current.activation * core.ActivationDecay * weight

			if neighborActivation < core.ActivationThreshold {
				continue
			}

			// Only update if this activation is higher than what we've seen.
			if existing, ok := activationMap[edge.TargetID]; ok {
				if neighborActivation <= existing {
					continue
				}
			}

			activationMap[edge.TargetID] = neighborActivation
			pq.Push(activationEntry{
				nodeID:     edge.TargetID,
				activation: neighborActivation,
				depth:      current.depth + 1,
			})
		}
	}

	return activationMap, nil
}

// activationQueue is a max-heap priority queue ordered by activation value.
type activationQueue struct {
	items []activationEntry
}

func newActivationQueue() *activationQueue {
	return &activationQueue{}
}

func (q *activationQueue) Len() int { return len(q.items) }

func (q *activationQueue) Push(entry activationEntry) {
	q.items = append(q.items, entry)
	q.siftUp(len(q.items) - 1)
}

func (q *activationQueue) Pop() activationEntry {
	top := q.items[0]
	last := len(q.items) - 1
	q.items[0] = q.items[last]
	q.items = q.items[:last]
	if len(q.items) > 0 {
		q.siftDown(0)
	}
	return top
}

func (q *activationQueue) siftUp(i int) {
	for i > 0 {
		parent := (i - 1) / 2
		if q.items[parent].activation >= q.items[i].activation {
			break
		}
		q.items[parent], q.items[i] = q.items[i], q.items[parent]
		i = parent
	}
}

func (q *activationQueue) siftDown(i int) {
	n := len(q.items)
	for {
		largest := i
		left, right := 2*i+1, 2*i+2
		if left < n && q.items[left].activation > q.items[largest].activation {
			largest = left
		}
		if right < n && q.items[right].activation > q.items[largest].activation {
			largest = right
		}
		if largest == i {
			break
		}
		q.items[i], q.items[largest] = q.items[largest], q.items[i]
		i = largest
	}
}
