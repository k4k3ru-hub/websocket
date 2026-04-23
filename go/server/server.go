//
// server.go
//
package server

import (
    "context"
    "fmt"
    "net/http"

    "github.com/k4k3ru-hub/websocket/go"

    gorillaWebsocket "github.com/gorilla/websocket"
)



type Server struct {
    rootCtx context.Context

    upgrader Upgrader

    option *ServerOption

    session        *websocket.Session
    sessionHandler websocket.SessionHandler

}

type ServerOption struct {
    SessionOption *websocket.SessionOption
}

type Upgrader = gorillaWebsocket.Upgrader


func IsWebSocketUpgrade(r *http.Request) bool {
    // Guard.
    if r == nil {
        return false
    }

    return gorillaWebsocket.IsWebSocketUpgrade(r)
}


//
// Create default server option.
//
// Version:
//   - 2026-04-22: Added.
//
func DefaultServerOption() *ServerOption {
    return &ServerOption{
        SessionOption: websocket.DefaultSessionOption(),
    }
}


//
// Create default upgrader.
//
func DefaultUpgrader() Upgrader {
    return Upgrader{
        ReadBufferSize:  4096,
        WriteBufferSize: 4096,
        CheckOrigin: func(r *http.Request) bool {
            return true
        },
    }
}


func NewServer(rootCtx context.Context, upgrader Upgrader, h websocket.SessionHandler, o *ServerOption) (*Server, error) {
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

    return &Server{
        rootCtx:        rootCtx,
        upgrader:       upgrader,
        sessionHandler: h,
        option:         o,
    }, nil
}


func (s *Server) Start(w http.ResponseWriter, r *http.Request) error {
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

    if !IsWebSocketUpgrade(r) {
        return fmt.Errorf("failed to start websocket server: invalid websocket upgrade request")
    }

    // Upgrade the connection.
    conn, err := s.upgrader.Upgrade(w, r, nil)
    if err != nil {
        return fmt.Errorf("failed to start websocket server: %w", err)
    }

    // Create new session.
    sess, err := websocket.NewSession(conn, s.sessionHandler, s.option.SessionOption)
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
