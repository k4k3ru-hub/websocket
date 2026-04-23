//
// session.go
//
package websocket

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "sync"
    "sync/atomic"
    "time"

    gorillaWebsocket "github.com/gorilla/websocket"
)

const (
    DefaultWriteWait       = 10 * time.Second
    DefaultPongWait        = 70 * time.Second
    DefaultPingPeriod      = 25 * time.Second
    DefaultInitialReadWait = DefaultPongWait
    DefaultMaxMessageSize  = 256 * 1024 // 256 KiB
    DefaultSendQueueSize   = 256
)

var (
    ErrSessionClosed   = errors.New("websocket session is closed")
    ErrSendQueueFull   = errors.New("websocket send queue is full")
    ErrSessionNotReady = errors.New("websocket session is not ready")
)

//
// SessionOption.
//
// Notes:
//   - Transport-only option set for one websocket connection.
//   - PingMessageType defaults to websocket.PingMessage.
//   - For application-level ping, set PingMessageType=websocket.TextMessage
//     and provide PingPayload.
//
// Version:
//   - 2026-04-22: Added.
//
type SessionOption struct {
    WriteWait       time.Duration
    PongWait        time.Duration
    PingPeriod      time.Duration
    InitialReadWait time.Duration
    MaxMessageSize  int64
    SendQueueSize   int
    PingMessageType int
    PingPayload     []byte
}


//
// DefaultSessionOption.
//
// Version:
//   - 2026-04-22: Added.
//
func DefaultSessionOption() *SessionOption {
    return &SessionOption{
        WriteWait:       DefaultWriteWait,
        PongWait:        DefaultPongWait,
        PingPeriod:      DefaultPingPeriod,
        InitialReadWait: DefaultInitialReadWait,
        MaxMessageSize:  DefaultMaxMessageSize,
        SendQueueSize:   DefaultSendQueueSize,
        PingMessageType: gorillaWebsocket.PingMessage,
        PingPayload:     nil,
    }
}


//
// Session.
//
// Notes:
//   - Generic websocket transport session.
//   - Owns one websocket connection lifecycle, read/write loops, deadlines,
//     ping handling, and outbound send queue.
//   - Business-specific logic is delegated to SessionHandler.
//   - Optional cleanup callback is delegated to SessionCleanupHandler.
//
// Version:
//   - 2026-04-22: Added.
//
type Session struct {
    conn           *gorillaWebsocket.Conn
    handler        SessionHandler
//    cleanupHandler SessionCleanupHandler

    sendCh    chan []byte
    doneCh    chan struct{}
    closeOnce sync.Once
    started   atomic.Bool

    writeWait       time.Duration
    pongWait        time.Duration
    pingPeriod      time.Duration
    initialReadWait time.Duration
    maxMessageSize  int64
    pingMessageType int
    pingPayload     []byte
}

//
// NewSession.
//
// Version:
//   - 2026-04-22: Added.
//
func NewSession(conn *gorillaWebsocket.Conn, handler SessionHandler, o *SessionOption) (*Session, error) {
    // Guard.
    if conn == nil {
        return nil, fmt.Errorf("failed to create websocket session: missing required parameter: conn=nil")
    }
    if handler == nil {
        return nil, fmt.Errorf("failed to create websocket session: missing required parameter: handler=nil")
    }
    if o == nil {
        o = DefaultSessionOption()
    }

    normalized := normalizeSessionOption(o)

    return &Session{
        conn:            conn,
        handler:         handler,
        sendCh:          make(chan []byte, normalized.SendQueueSize),
        doneCh:          make(chan struct{}),
        writeWait:       normalized.WriteWait,
        pongWait:        normalized.PongWait,
        pingPeriod:      normalized.PingPeriod,
        initialReadWait: normalized.InitialReadWait,
        maxMessageSize:  normalized.MaxMessageSize,
        pingMessageType: normalized.PingMessageType,
        pingPayload:     append([]byte(nil), normalized.PingPayload...),
    }, nil
}

func normalizeSessionOption(o *SessionOption) *SessionOption {
    if o == nil {
        o = DefaultSessionOption()
    }

    out := *o

    if out.WriteWait <= 0 {
        out.WriteWait = DefaultWriteWait
    }
    if out.PongWait <= 0 {
        out.PongWait = DefaultPongWait
    }
    if out.PingPeriod <= 0 {
        out.PingPeriod = DefaultPingPeriod
    }
    if out.InitialReadWait <= 0 {
        out.InitialReadWait = out.PongWait
    }
    if out.MaxMessageSize <= 0 {
        out.MaxMessageSize = DefaultMaxMessageSize
    }
    if out.SendQueueSize <= 0 {
        out.SendQueueSize = DefaultSendQueueSize
    }
    if out.PingMessageType == 0 {
        out.PingMessageType = gorillaWebsocket.PingMessage
    }

    return &out
}

//
// Start websocket session.
//
// Notes:
//   - Safe to call only once for one Session instance.
//   - Starts read/write loops bound to the existing connection.
//   - Automatically closes the session when ctx is done.
//
// Version:
//   - 2026-04-22: Added.
//
func (s *Session) Start(ctx context.Context) error {
    // Guard.
    if s == nil {
        return fmt.Errorf("failed to start websocket session: missing required parameter: receiver=nil")
    }
    if s.conn == nil {
        return fmt.Errorf("failed to start websocket session: missing required parameter: conn=nil")
    }
    if !s.started.CompareAndSwap(false, true) {
        return fmt.Errorf("failed to start websocket session: session already started")
    }
    if ctx == nil {
        ctx = context.Background()
    }

    s.conn.SetReadLimit(s.maxMessageSize)

    if err := s.conn.SetReadDeadline(time.Now().Add(s.initialReadWait)); err != nil {
        return fmt.Errorf("failed to start websocket session: %w", err)
    }

    s.conn.SetPongHandler(func(string) error {
        return s.conn.SetReadDeadline(time.Now().Add(s.pongWait))
    })

    go func() {
        <-ctx.Done()
        s.Close()
    }()

    go s.runWriteLoop()
    go s.runReadLoop()

    return nil
}

//
// Close websocket session.
//
// Notes:
//   - Safe to call multiple times.
//   - cleanupHandler.RemoveSession is called before handler.HandleClose.
//   - HandleClose is called only once.
//
// Version:
//   - 2026-04-22: Added.
//
func (s *Session) Close() {
    // Guard.
    if s == nil {
        return
    }

    s.closeOnce.Do(func() {
//        if s.cleanupHandler != nil {
//            _ = s.cleanupHandler.RemoveSession(s)
//        }

        close(s.doneCh)

        if s.conn != nil {
            _ = s.conn.Close()
        }

        if s.handler != nil {
            s.handler.HandleClose(s)
        }
    })
}

//
// Send enqueues one websocket text message.
//
// Notes:
//   - This method does not write directly to the websocket connection.
//   - Actual write is handled by runWriteLoop().
//   - The message is copied before enqueue.
//
// Version:
//   - 2026-04-22: Added.
//
func (s *Session) Send(message []byte) error {
    // Guard.
    if s == nil {
        return fmt.Errorf("failed to send websocket message: missing required parameter: receiver=nil")
    }
    if len(message) == 0 {
        return fmt.Errorf("failed to send websocket message: missing required parameter: message=empty")
    }

    msg := append([]byte(nil), message...)

    select {
    case <-s.doneCh:
        return ErrSessionClosed
    case s.sendCh <- msg:
        return nil
    default:
        return ErrSendQueueFull
    }
}

//
// SendJSON marshals v as JSON and enqueues it as one websocket text message.
//
// Version:
//   - 2026-04-22: Added.
//
func (s *Session) SendJSON(v any) error {
    // Guard.
    if s == nil {
        return fmt.Errorf("failed to send websocket json message: missing required parameter: receiver=nil")
    }
    if v == nil {
        return fmt.Errorf("failed to send websocket json message: missing required parameter: payload=nil")
    }

    b, err := json.Marshal(v)
    if err != nil {
        return fmt.Errorf("failed to send websocket json message: %w", err)
    }

    if err := s.Send(b); err != nil {
        return fmt.Errorf("failed to send websocket json message: %w", err)
    }

    return nil
}

//
// Done returns a channel that is closed when the session closes.
//
// Version:
//   - 2026-04-22: Added.
//
func (s *Session) Done() <-chan struct{} {
    if s == nil {
        return nil
    }

    return s.doneCh
}

//
// IsStarted reports whether Start has already been called.
//
// Version:
//   - 2026-04-22: Added.
//
func (s *Session) IsStarted() bool {
    if s == nil {
        return false
    }

    return s.started.Load()
}

//
// Conn returns the underlying websocket connection.
//
// Notes:
//   - Exposed only when upper layers need direct control.
//
// Version:
//   - 2026-04-22: Added.
//
func (s *Session) Conn() *gorillaWebsocket.Conn {
    if s == nil {
        return nil
    }

    return s.conn
}

func (s *Session) runReadLoop() {
    // Guard.
    if s == nil || s.conn == nil {
        return
    }

    defer s.Close()

    for {
        _, msg, err := s.conn.ReadMessage()
        if err != nil {
            return
        }
        if len(msg) == 0 {
            continue
        }

        s.handler.HandleMessage(s, msg)
    }
}

func (s *Session) runWriteLoop() {
    // Guard.
    if s == nil || s.conn == nil {
        return
    }

    ticker := time.NewTicker(s.pingPeriod)
    defer func() {
        ticker.Stop()
        s.Close()
    }()

    for {
        select {
        case <-s.doneCh:
            return

        case msg, ok := <-s.sendCh:
            if !ok {
                return
            }
            if len(msg) == 0 {
                continue
            }

            if err := s.conn.SetWriteDeadline(time.Now().Add(s.writeWait)); err != nil {
                return
            }
            if err := s.conn.WriteMessage(gorillaWebsocket.TextMessage, msg); err != nil {
                return
            }

        case <-ticker.C:
            if err := s.conn.SetWriteDeadline(time.Now().Add(s.writeWait)); err != nil {
                return
            }
            if err := s.conn.WriteMessage(s.pingMessageType, s.pingPayload); err != nil {
                return
            }
        }
    }
}


//
// Clone returns a deep-copied session option.
//
// Notes:
//   - Value fields are copied as-is.
//   - PingPayload is copied to avoid sharing the backing array.
//
// Version:
//   - 2026-04-23: Added.
//
func (o *SessionOption) Clone() *SessionOption {
    // Guard.
    if o == nil {
        return nil
    }

    out := &SessionOption{
        WriteWait:       o.WriteWait,
        PongWait:        o.PongWait,
        PingPeriod:      o.PingPeriod,
        InitialReadWait: o.InitialReadWait,
        MaxMessageSize:  o.MaxMessageSize,
        SendQueueSize:   o.SendQueueSize,
        PingMessageType: o.PingMessageType,
    }

    if len(o.PingPayload) > 0 {
        out.PingPayload = append([]byte(nil), o.PingPayload...)
    }

    return out
}
