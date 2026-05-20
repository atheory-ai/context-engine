package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/atheory-ai/context-engine/internal/runner"
	"github.com/atheory-ai/context-engine/internal/storage/queries"
	"github.com/google/uuid"
)

// CreateTokenRequest is the body of POST /api/v1/tokens.
type CreateTokenRequest struct {
	Name      string `json:"name"`
	Scope     string `json:"scope"` // "read" | "read-write" | "admin"
	ExpiresIn *int64 `json:"expires_in_seconds,omitempty"`
}

// CreateToken handles POST /api/v1/tokens.
func CreateToken(engine *runner.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateTokenRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if req.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}

		scope := req.Scope
		if scope == "" {
			scope = "read-write"
		}

		tokenID := uuid.New().String()
		now := time.Now().UnixMilli()

		var expiresAt *int64
		if req.ExpiresIn != nil && *req.ExpiresIn > 0 {
			exp := now + (*req.ExpiresIn * 1000)
			expiresAt = &exp
		}

		t := queries.Token{
			ID:         tokenID,
			Name:       req.Name,
			Scope:      scope,
			CreatedAt:  now,
			Revoked:    0,
			Properties: "{}",
		}
		if expiresAt != nil {
			t.ExpiresAt.Valid = true
			t.ExpiresAt.Int64 = *expiresAt
		}

		if err := engine.InsertToken(r.Context(), t); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{
			"id":         tokenID,
			"name":       req.Name,
			"scope":      scope,
			"created_at": now,
			"expires_at": expiresAt,
			"token":      tokenID, // raw token value (only shown at creation)
		})
	}
}

// ListTokens handles GET /api/v1/tokens.
func ListTokens(engine *runner.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokens, err := engine.ListTokens(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		type tokenItem struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Scope     string `json:"scope"`
			CreatedAt int64  `json:"created_at"`
			ExpiresAt *int64 `json:"expires_at,omitempty"`
			Revoked   bool   `json:"revoked"`
		}

		items := make([]tokenItem, 0, len(tokens))
		for _, t := range tokens {
			item := tokenItem{
				ID:        t.ID,
				Name:      t.Name,
				Scope:     t.Scope,
				CreatedAt: t.CreatedAt,
				Revoked:   t.Revoked == 1,
			}
			if t.ExpiresAt.Valid {
				item.ExpiresAt = &t.ExpiresAt.Int64
			}
			items = append(items, item)
		}

		writeJSON(w, http.StatusOK, map[string]any{"tokens": items})
	}
}

// RevokeToken handles DELETE /api/v1/tokens/{id}.
func RevokeToken(engine *runner.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "token id is required")
			return
		}

		if err := engine.RevokeToken(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
