// Package ws implements WebSocket streaming for real-time query output.
package ws

import (
	"net/http"

	"github.com/atheory/context-engine/internal/runner"
	"github.com/gorilla/websocket"
)

// WSEvent is a single event frame sent over the WebSocket connection.
type WSEvent struct {
	Type     string         `json:"type"` // "thinking"|"action"|"message"|"warning"|"error"|"cost"|"done"
	Content  string         `json:"content"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// wsQueryRequest is the initial message the client sends after connecting.
type wsQueryRequest struct {
	Query    string `json:"query"`
	MaxLoops int    `json:"max_loops,omitempty"`
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	// Allow all origins for now; production should validate against cfg.Server.CORSOrigins.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Handler returns an http.HandlerFunc that upgrades to WebSocket and streams query output.
// Each connection gets its own AppChannels set so multiple concurrent clients can each
// run independent queries.
func Handler(engine *runner.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Read the query from the client.
		var req wsQueryRequest
		if err := conn.ReadJSON(&req); err != nil {
			conn.WriteJSON(WSEvent{Type: "error", Content: "invalid request"}) //nolint:errcheck
			return
		}

		if req.Query == "" {
			conn.WriteJSON(WSEvent{Type: "error", Content: "query is required"}) //nolint:errcheck
			return
		}

		// Create a dedicated channel set for this connection.
		ch := engine.NewChannels()
		defer engine.CloseChannels(ch)

		// Run the query in a background goroutine.
		queryErr := make(chan error, 1)
		go func() {
			queryErr <- engine.QueryWithChannels(
				r.Context(),
				req.Query,
				ch,
				runner.QueryOptions{MaxLoops: req.MaxLoops},
			)
		}()

		// Stream channel events to the client until the query completes.
		for {
			select {
			case e := <-ch.Thinking:
				conn.WriteJSON(WSEvent{Type: "thinking", Content: e.Content}) //nolint:errcheck
			case e := <-ch.Action:
				conn.WriteJSON(WSEvent{Type: "action", Content: e.Content}) //nolint:errcheck
			case e := <-ch.Message:
				conn.WriteJSON(WSEvent{Type: "message", Content: e.Content}) //nolint:errcheck
			case e := <-ch.Warning:
				conn.WriteJSON(WSEvent{Type: "warning", Content: e.Content}) //nolint:errcheck
			case e := <-ch.Error:
				conn.WriteJSON(WSEvent{Type: "error", Content: e.Content}) //nolint:errcheck
			case e := <-ch.Cost:
				conn.WriteJSON(WSEvent{Type: "cost", Content: e.Content}) //nolint:errcheck
			case e := <-ch.System:
				conn.WriteJSON(WSEvent{Type: "system", Content: e.Content}) //nolint:errcheck
			case err := <-queryErr:
				if err != nil {
					conn.WriteJSON(WSEvent{Type: "error", Content: err.Error()}) //nolint:errcheck
				}
				conn.WriteJSON(WSEvent{Type: "done"}) //nolint:errcheck
				return
			case <-r.Context().Done():
				return
			}
		}
	}
}
