package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/atheory-ai/context-engine/internal/runner"
)

// QueryRequest is the body of POST /api/v1/query.
type QueryRequest struct {
	Query    string `json:"query"`
	MaxLoops int    `json:"max_loops,omitempty"`
}

// QueryResponse is the response body for POST /api/v1/query.
type QueryResponse struct {
	RunID      string  `json:"run_id"`
	Answer     string  `json:"answer"`
	TokensIn   int     `json:"tokens_in"`
	TokensOut  int     `json:"tokens_out"`
	CostUSD    float64 `json:"cost_usd"`
	LoopsUsed  int     `json:"loops_used"`
	DurationMS int64   `json:"duration_ms"`
	Partial    bool    `json:"partial"`
}

// Query handles POST /api/v1/query — synchronous query execution.
func Query(engine *runner.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req QueryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if req.Query == "" {
			writeError(w, http.StatusBadRequest, "query is required")
			return
		}

		result, err := engine.QuerySync(r.Context(), runner.QueryOptions{
			Query:    req.Query,
			MaxLoops: req.MaxLoops,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, QueryResponse{
			RunID:      result.RunID,
			Answer:     result.Answer,
			TokensIn:   result.TokensIn,
			TokensOut:  result.TokensOut,
			CostUSD:    result.CostUSD,
			LoopsUsed:  result.LoopsUsed,
			DurationMS: result.DurationMS,
			Partial:    result.Partial,
		})
	}
}
