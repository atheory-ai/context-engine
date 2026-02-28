// Package api implements the REST + WebSocket API server.
package api

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/atheory/context-engine/internal/config"
	"github.com/atheory/context-engine/internal/storage/db"
	"github.com/atheory/context-engine/internal/storage/queries"
)

type contextKey string

const tokenContextKey contextKey = "ce-token"

// authMiddleware validates CE API tokens on all /api/ requests.
// Local requests from 127.0.0.1 with no token are allowed when
// the server is bound to localhost (cfg.Server.Host == "127.0.0.1").
func authMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractToken(r)

			if token == "" && isLocalRequest(r) && cfg.Server.Host == "127.0.0.1" {
				next.ServeHTTP(w, r)
				return
			}

			if token == "" {
				writeUnauthorized(w, "missing token")
				return
			}

			tokenRecord, err := validateToken(cfg.DataDir, token)
			if err != nil || tokenRecord == nil {
				writeUnauthorized(w, "invalid token")
				return
			}

			if tokenRecord.ExpiresAt.Valid {
				if time.Now().UnixMilli() > tokenRecord.ExpiresAt.Int64 {
					writeUnauthorized(w, "token expired")
					return
				}
			}

			if tokenRecord.Scope == "read" && isWriteMethod(r.Method) {
				writeForbidden(w, "read-only token cannot perform write operations")
				return
			}

			ctx := context.WithValue(r.Context(), tokenContextKey, tokenRecord)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func extractToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	if t := r.Header.Get("CE-Token"); t != "" {
		return t
	}
	return r.URL.Query().Get("token")
}

func isLocalRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	return host == "127.0.0.1" || host == "::1"
}

func isWriteMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// validateToken opens audit.db read-only and looks up the token.
// Returns nil if the token is not found or is revoked.
func validateToken(dataDir, token string) (*queries.Token, error) {
	dbPath := filepath.Join(dataDir, "audit.db")
	auditDB, err := db.OpenReadOnly(dbPath)
	if err != nil {
		return nil, err
	}
	defer auditDB.Close()

	t, err := queries.GetToken(context.Background(), auditDB, token)
	if err != nil {
		return nil, err
	}
	if t == nil || t.Revoked == 1 {
		return nil, nil
	}
	return t, nil
}

func writeUnauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", "Bearer realm=\"ce\"")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"` + msg + `"}`)) //nolint:errcheck
}

func writeForbidden(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	w.Write([]byte(`{"error":"` + msg + `"}`)) //nolint:errcheck
}
