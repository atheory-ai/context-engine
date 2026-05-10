package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/atheory-ai/context-engine/internal/config"
	"github.com/atheory-ai/context-engine/internal/runner"
	"github.com/atheory-ai/context-engine/internal/server/api/handlers"
	"github.com/atheory-ai/context-engine/internal/server/api/ws"
)

// Server is the REST + WebSocket API server.
type Server struct {
	cfg    *config.Config
	engine *runner.Engine
	srv    *http.Server
}

// New creates and configures the API server.
// The HTTP server is not started until Start() is called.
func New(cfg *config.Config, engine *runner.Engine) *Server {
	s := &Server{cfg: cfg, engine: engine}

	mux := http.NewServeMux()

	// Unauthenticated health check.
	mux.HandleFunc("GET /health", handlers.Health)

	// All /api/v1 routes require authentication.
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("GET /api/v1/projects", handlers.ListProjects(engine))
	apiMux.HandleFunc("GET /api/v1/projects/{id}", handlers.GetProject(engine))
	apiMux.HandleFunc("POST /api/v1/query", handlers.Query(engine))
	apiMux.HandleFunc("GET /api/v1/substrate/nodes", handlers.GetNodes(engine))
	apiMux.HandleFunc("GET /api/v1/substrate/edges", handlers.GetEdges(engine))
	apiMux.HandleFunc("GET /api/v1/execlog", handlers.ListExecLog(engine))
	apiMux.HandleFunc("GET /api/v1/execlog/{runId}", handlers.GetExecRun(engine))
	apiMux.HandleFunc("POST /api/v1/tokens", handlers.CreateToken(engine))
	apiMux.HandleFunc("GET /api/v1/tokens", handlers.ListTokens(engine))
	apiMux.HandleFunc("DELETE /api/v1/tokens/{id}", handlers.RevokeToken(engine))
	apiMux.HandleFunc("GET /api/v1/ws", ws.Handler(engine))

	mux.Handle("/api/", authMiddleware(cfg)(apiMux))

	s.srv = &http.Server{
		Handler:      corsMiddleware(cfg)(mux),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return s
}

// Start begins serving on addr. Blocks until the context is cancelled.
func (s *Server) Start(ctx context.Context, addr string) error {
	s.srv.Addr = addr

	go func() {
		<-ctx.Done()
		s.srv.Shutdown(context.Background()) //nolint:errcheck
	}()

	return s.srv.ListenAndServe()
}

// Shutdown gracefully stops the API server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

// corsMiddleware adds CORS headers for allowed origins.
func corsMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if isAllowedOrigin(origin, cfg.Server.CORSOrigins) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, CE-Token, Content-Type")
				w.Header().Set("Access-Control-Max-Age", "86400")
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// isAllowedOrigin checks whether origin matches any pattern in the allowed list.
// Supports a trailing wildcard port: "http://localhost:*".
func isAllowedOrigin(origin string, allowed []string) bool {
	if origin == "" {
		return false
	}
	for _, pattern := range allowed {
		if matchOriginPattern(pattern, origin) {
			return true
		}
	}
	return false
}

// matchOriginPattern matches an origin against a pattern.
// Patterns may end with ":*" to allow any port on that host.
func matchOriginPattern(pattern, origin string) bool {
	if strings.HasSuffix(pattern, ":*") {
		// "http://localhost:*" matches "http://localhost:3000"
		prefix := strings.TrimSuffix(pattern, ":*")
		if strings.HasPrefix(origin, prefix+":") {
			return true
		}
		// Also match without port: "http://localhost"
		return origin == prefix
	}
	return pattern == origin
}
