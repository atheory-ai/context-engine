package handlers

import (
	"net/http"
	"strconv"

	"github.com/atheory-ai/context-engine/internal/runner"
)

// NodeResponse is a single node in the substrate nodes response.
type NodeResponse struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	Label       string         `json:"label"`
	CanonicalID string         `json:"canonical_id"`
	SourceClass string         `json:"source_class"`
	Properties  map[string]any `json:"properties"`
}

// NodesResponse is the body of GET /api/v1/substrate/nodes.
type NodesResponse struct {
	Nodes  []NodeResponse `json:"nodes"`
	Total  int            `json:"total"`
	Offset int            `json:"offset"`
}

// GetNodes handles GET /api/v1/substrate/nodes.
// Supported query params: type, search, limit, offset.
func GetNodes(engine *runner.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		search := q.Get("search")
		nodeType := q.Get("type")
		limit, _ := strconv.Atoi(q.Get("limit")) //nolint:errcheck // malformed limit → 0 → clamped to default 50 below
		if limit <= 0 || limit > 200 {
			limit = 50
		}

		nodes, err := engine.SearchSubstrate(r.Context(), runner.SearchOptions{
			Query: search,
			Type:  nodeType,
			Limit: limit,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		resp := NodesResponse{
			Nodes:  make([]NodeResponse, 0, len(nodes)),
			Total:  len(nodes),
			Offset: 0,
		}
		for _, n := range nodes {
			resp.Nodes = append(resp.Nodes, NodeResponse{
				ID:          n.ID,
				Type:        n.Type,
				Label:       n.Label,
				CanonicalID: n.CanonicalID,
				SourceClass: n.SourceClass,
				Properties:  map[string]any{},
			})
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

// GetEdges handles GET /api/v1/substrate/edges.
func GetEdges(_ *runner.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"edges":  []any{},
			"total":  0,
			"offset": 0,
		})
	}
}
