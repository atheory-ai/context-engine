package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/atheory/context-engine/internal/server/mcp/protocol"
	"github.com/google/uuid"
)

// sseSession tracks an active SSE connection.
type sseSession struct {
	id      string
	writer  http.ResponseWriter
	flusher http.Flusher
	sendCh  chan string  // JSON-encoded responses to push to client
	done    chan struct{} // closed when the session ends
}

var sessions sync.Map // sessionID → *sseSession

// StartSSE starts the MCP SSE transport on the given address.
//
//   /mcp/sse      — client connects here to receive server-sent events
//   /mcp/messages — client POSTs JSON-RPC requests here
func (s *Server) StartSSE(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp/sse", s.handleSSEConnect)
	mux.HandleFunc("/mcp/messages", s.handleSSEMessage)

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	s.httpSrv = srv

	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background()) //nolint:errcheck
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// handleSSEConnect handles the initial SSE connection from the client.
// It sends the /mcp/messages endpoint URL and then streams responses.
func (s *Server) handleSSEConnect(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	sessionID := uuid.New().String()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	session := &sseSession{
		id:      sessionID,
		writer:  w,
		flusher: flusher,
		sendCh:  make(chan string, 32),
		done:    make(chan struct{}),
	}
	sessions.Store(sessionID, session)
	defer func() {
		sessions.Delete(sessionID)
		close(session.done)
	}()

	// Send the POST endpoint URL so the client knows where to send requests.
	fmt.Fprintf(w, "event: endpoint\ndata: /mcp/messages?sessionId=%s\n\n", sessionID)
	flusher.Flush()

	for {
		select {
		case msg := <-session.sendCh:
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// handleSSEMessage handles POST /mcp/messages — the client's JSON-RPC request.
// The response is sent back over the SSE stream identified by sessionId.
func (s *Server) handleSSEMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	val, ok := sessions.Load(sessionID)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	session := val.(*sseSession)

	var req protocol.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON-RPC", http.StatusBadRequest)
		return
	}

	// Notifications require no response.
	if req.ID == nil {
		s.handleNotification(r.Context(), req)
		w.WriteHeader(http.StatusAccepted)
		return
	}

	resp := s.handleRequest(r.Context(), req)

	data, _ := json.Marshal(resp)
	select {
	case session.sendCh <- string(data):
	case <-session.done:
	}

	w.WriteHeader(http.StatusAccepted)
}
