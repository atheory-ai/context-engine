package handlers

import (
	"net/http"

	"github.com/atheory-ai/context-engine/internal/runner"
)

// ListProjects handles GET /api/v1/projects.
func ListProjects(engine *runner.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status, err := engine.ProjectStatus(r.Context())
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"projects": []any{}})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"projects": []any{
				map[string]any{
					"id":           "local",
					"git_url":      status.GitURL,
					"status":       status.IndexState,
					"node_count":   status.NodeCount,
					"edge_count":   status.EdgeCount,
					"last_indexed": status.LastIndexed,
				},
			},
		})
	}
}

// GetProject handles GET /api/v1/projects/{id}.
func GetProject(engine *runner.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id != "local" {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}

		status, err := engine.ProjectStatus(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"id":           "local",
			"git_url":      status.GitURL,
			"status":       status.IndexState,
			"node_count":   status.NodeCount,
			"edge_count":   status.EdgeCount,
			"last_indexed": status.LastIndexed,
		})
	}
}
