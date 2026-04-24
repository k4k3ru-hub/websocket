//
// server.go
//
package websocket

import (
    "context"
    "fmt"
    "net/http"
    "time"

    gorillaWebsocket "github.com/gorilla/websocket"
)


type Server struct {
    rootCtx context.Context

    sessionHandler SessionHandler
    sessionOption  *SessionOption

    upgrader gorillaWebsocket.Upgrader
}


type ServerOption struct {
    HandshakeTimeout  time.Duration
    ReadBufferSize    int
    WriteBufferSize   int
    WriteBufferPool   gorillaWebsocket.BufferPool
    Subprotocols      []string
    Error             func(w http.ResponseWriter, r *http.Request, status int, reason error)
    CheckOrigin       func(r *http.Request) bool
    EnableCompression bool
    SessionOption     *SessionOption
}


//
// Create default server option.
//
// Version:
//   - 2026-04-22: Added.
//
func DefaultServerOption() *ServerOption {
    return &ServerOption{
        HandshakeTimeout: 5 * time.Second,
        ReadBufferSize: 4096,
        WriteBufferSize: 4096,
        CheckOrigin: func(r *http.Request) bool {
            return true
        },
        SessionOption: DefaultSessionOption(),
    }
}


//
// Check whether the request is a WebSocket upgrade request.
//
// Version:
//   - 2026-04-23: Added.
//
func IsWebSocketUpgrade(r *http.Request) bool {
    // Guard.
    if r == nil {
        return false
    }

    return gorillaWebsocket.IsWebSocketUpgrade(r)
}


//
// Create new websocket server.
//
// Version:
//   - 2026-04-23: Added.
//
func NewServer(rootCtx context.Context, h SessionHandler, o *ServerOption) (*Server, error) {
    // Guard.
    if rootCtx == nil {
        rootCtx = context.Background()
    }
    if h == nil {
        return nil, fmt.Errorf("failed to create websocket server: missing required parameter: session_handler=null")
    }
    if o == nil {
        o = DefaultServerOption()
    }

    upgrader := gorillaWebsocket.Upgrader{
        HandshakeTimeout:  o.HandshakeTimeout,
        ReadBufferSize:    o.ReadBufferSize,
        WriteBufferSize:   o.WriteBufferSize,
        WriteBufferPool:   o.WriteBufferPool,
        Error:             o.Error,
        CheckOrigin:       o.CheckOrigin,
        EnableCompression: o.EnableCompression,
    }
    if len(o.Subprotocols) > 0 {
        upgrader.Subprotocols = append([]string(nil), o.Subprotocols...)
    }

    return &Server{
        rootCtx:        rootCtx,
        sessionHandler: h,
        sessionOption:  o.SessionOption.Clone(),
        upgrader:       upgrader,
    }, nil
}


//
// Start session.
//
// Version:
//   - 2026-04-23: Added.
//
func (s *Server) StartSession(w http.ResponseWriter, r *http.Request) error {
    // Guard.
    if s == nil {
        return fmt.Errorf("failed to start websocket server: missing required parameter: receiver=null")
    }
    if w == nil {
        return fmt.Errorf("failed to start websocket server: missing required parameter: response_writer=null")
    }
    if r == nil {
        return fmt.Errorf("failed to start websocket server: missing required parameter: request=null")
    }

    // Upgrade the connection to the websocket.
    conn, err := s.upgrader.Upgrade(w, r, nil)
    if err != nil {
        return fmt.Errorf("failed to start websocket server: %w", err)
    }

    // Create new session.
    sess, err := NewSession(conn, s.sessionHandler, s.sessionOption)
    if err != nil {
        _ = conn.Close()
        return fmt.Errorf("failed to start websocket server: %w", err)
    }

    // Start websocket session.
    if err := sess.Start(s.rootCtx); err != nil {
        _ = conn.Close()
        return fmt.Errorf("failed to start websocket server: %w", err)
    }

    return nil
}
