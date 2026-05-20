package api

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/atheory-ai/context-engine/internal/config"
	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/storage/db"
	"github.com/atheory-ai/context-engine/internal/storage/migrations"
	"github.com/atheory-ai/context-engine/internal/storage/queries"
)

func TestAuthMiddlewareReadScopedTokenAllowsReadsAndBlocksWrites(t *testing.T) {
	dataDir := t.TempDir()
	metaDB := setupMetaDB(t, dataDir)
	insertAPIToken(t, metaDB, "read-token", core.ScopeRead, sql.NullInt64{})
	insertAPIToken(t, metaDB, "write-token", core.ScopeReadWrite, sql.NullInt64{})

	cfg := &config.Config{DataDir: dataDir}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token, ok := r.Context().Value(tokenContextKey).(*queries.Token); !ok || token == nil {
			t.Fatalf("token missing from request context")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	handler := authMiddleware(cfg)(next)

	tests := []struct {
		name       string
		method     string
		token      string
		wantStatus int
	}{
		{name: "read token get", method: http.MethodGet, token: "read-token", wantStatus: http.StatusNoContent},
		{name: "read token post", method: http.MethodPost, token: "read-token", wantStatus: http.StatusForbidden},
		{name: "write token post", method: http.MethodPost, token: "write-token", wantStatus: http.StatusNoContent},
		{name: "missing token", method: http.MethodGet, wantStatus: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "http://ce.test/api/v1/projects", nil)
			req.RemoteAddr = "203.0.113.10:12345"
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestAuthMiddlewareRejectsExpiredAndRevokedTokens(t *testing.T) {
	dataDir := t.TempDir()
	metaDB := setupMetaDB(t, dataDir)
	insertAPIToken(t, metaDB, "expired-token", core.ScopeRead, sql.NullInt64{Int64: 1, Valid: true})
	insertAPIToken(t, metaDB, "revoked-token", core.ScopeRead, sql.NullInt64{})
	if err := queries.RevokeToken(context.Background(), metaDB, "revoked-token", 2); err != nil {
		t.Fatalf("revoke token: %v", err)
	}

	handler := authMiddleware(&config.Config{DataDir: dataDir})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, token := range []string{"expired-token", "revoked-token"} {
		req := httptest.NewRequest(http.MethodGet, "http://ce.test/api/v1/projects", nil)
		req.RemoteAddr = "203.0.113.10:12345"
		req.Header.Set("CE-Token", token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want %d", token, rec.Code, http.StatusUnauthorized)
		}
	}
}

func setupMetaDB(t *testing.T, dataDir string) *sql.DB {
	t.Helper()
	metaDB, err := db.Open(filepath.Join(dataDir, "meta.db"))
	if err != nil {
		t.Fatalf("open meta db: %v", err)
	}
	t.Cleanup(func() { metaDB.Close() })
	if err := migrations.RunMeta(metaDB); err != nil {
		t.Fatalf("migrate meta db: %v", err)
	}
	return metaDB
}

func insertAPIToken(t *testing.T, metaDB *sql.DB, id, scope string, expiresAt sql.NullInt64) {
	t.Helper()
	if err := queries.InsertToken(context.Background(), metaDB, queries.Token{
		ID:         id,
		Name:       id,
		Scope:      scope,
		CreatedAt:  1,
		ExpiresAt:  expiresAt,
		Properties: "{}",
	}); err != nil {
		t.Fatalf("insert token: %v", err)
	}
}
