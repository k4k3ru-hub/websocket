//
// client.go
//
package client

import (
    "context"
    "fmt"
    "net"
    "net/http"
    "sync"
    "sync/atomic"
    "time"

    "github.com/k4k3ru-hub/websocket/go"

    gorillaWebsocket "github.com/gorilla/websocket"
)


//
// Client.
//
// Notes:
//   - Generic websocket client for one connection with multiple subscriptions.
//   - Subscriptions are stored as raw wire payloads.
//   - Inbound message decode/routing is delegated to the caller-provided
//     SessionHandler.
//   - Reconnect and resubscribe are handled internally when enabled.
//
// Version:
//   - 2026-04-22: Added.
//
type Client struct {
    rootCtx context.Context

    endpointURL string
    httpHeader  http.Header
    dialer      *gorillaWebsocket.Dialer

    sessionMu      sync.RWMutex
    session        *websocket.Session
    sessionOption  *websocket.SessionOption
    sessionHandler websocket.SessionHandler

    connectMu sync.Mutex

    subscriptionsMu sync.RWMutex
    subscriptions   map[string][]byte

    closeOnce sync.Once
    closed    atomic.Bool

    reconnecting atomic.Bool
}


//
// ClientOption.
//
// Notes:
//   - Transport-level options for one websocket client.
//   - SessionOption is passed to shared websocket session.
//
// Version:
//   - 2026-04-22: Added.
//
type ClientOption struct {
    EndpointURL      string
    HTTPHeader       http.Header
    ConnectTimeout   time.Duration
    HandshakeTimeout time.Duration
    SessionOption    *websocket.SessionOption
}


func (o *ClientOption) WithHTTPHeader(httpHeader http.Header) *ClientOption {
    // Guard.
    if o == nil {
        o = DefaultClientOption()
    }

    if len(httpHeader) == 0 {
        o.HTTPHeader = make(http.Header)
        return o
    }

    cloned := make(http.Header, len(httpHeader))
    for k, v := range httpHeader {
        copied := make([]string, len(v))
        copy(copied, v)
        cloned[k] = copied
    }
    o.HTTPHeader = cloned

    return o
}


//
// DefaultClientOption.
//
// Version:
//   - 2026-04-22: Added.
//
func DefaultClientOption() *ClientOption {
    return &ClientOption{
        ConnectTimeout:   3 * time.Second,
        HandshakeTimeout: 5 * time.Second,
        SessionOption:    websocket.DefaultSessionOption(),
    }
}


//
// NewClient.
//
// Parameters:
//   - o: Client options.
//   - sessionHandler: Required handler for inbound websocket messages.
//
// Notes:
//   - sessionHandler is mandatory because decode/routing responsibility is
//     delegated to the caller side.
//
// Version:
//   - 2026-04-22: Added.
//
func NewClient(rootCtx context.Context, o *ClientOption, sessionHandler websocket.SessionHandler) (*Client, error) {
    // Guard.
    if rootCtx == nil {
        rootCtx = context.Background()
    }
    if o == nil {
        o = DefaultClientOption()
    }
    if sessionHandler == nil {
        return nil, fmt.Errorf("failed to create websocket client: missing required parameter: session_handler=nil")
    }

    // Normalize the client option.
    if o.EndpointURL == "" {
        return nil, fmt.Errorf("failed to create websocket client: missing required parameter: endpoint_url=empty")
    }
    if o.HTTPHeader == nil {
        o.HTTPHeader = make(http.Header)
    }
    if o.ConnectTimeout <= 0 {
        o.ConnectTimeout = 3 * time.Second
    }
    if o.HandshakeTimeout <= 0 {
        o.HandshakeTimeout = 5 * time.Second
    }
    if o.SessionOption == nil {
        o.SessionOption = websocket.DefaultSessionOption()
    }

    // Create new dialer.
    dialer := &gorillaWebsocket.Dialer{
        HandshakeTimeout: o.HandshakeTimeout,
        NetDialContext: (&net.Dialer{
            Timeout: o.ConnectTimeout,
        }).DialContext,
    }

    return &Client{
        rootCtx:              rootCtx,
        endpointURL:          o.EndpointURL,
        httpHeader:           o.HTTPHeader,
        dialer:               dialer,
        sessionHandler:       sessionHandler,
        sessionOption:        o.SessionOption,
        subscriptions:        make(map[string][]byte),
    }, nil
}


//
// Connect websocket.
//
// Notes:
//   - Safe to call repeatedly.
//   - Creates a new underlying transport session when not connected.
//   - Starts session read/write loops.
//
// Version:
//   - 2026-04-22: Added.
//
func (c *Client) Connect(ctx context.Context) error {
    // Guard.
    if c == nil {
        return fmt.Errorf("failed to connect websocket: missing required parameter: receiver=null")
    }
    if c.closed.Load() {
        return fmt.Errorf("failed to connect websocket: client already closed")
    }
    if ctx == nil {
        ctx = context.Background()
    }

    // Lock the whole connect function.
    c.connectMu.Lock()
    defer c.connectMu.Unlock()

    // Already has been opened the session.
    c.sessionMu.RLock()
    if c.session != nil {
        c.sessionMu.RUnlock()
        return nil
    }
    c.sessionMu.RUnlock()

    // Connect websocket. 
    conn, _, err := c.dialer.DialContext(ctx, c.endpointURL, c.httpHeader)
    if err != nil {
        return fmt.Errorf("failed to connect websocket: %w", err)
    }

    // Create new session.
    sess, err := websocket.NewSession(conn, c.sessionHandler, c.sessionOption)
    if err != nil {
        _ = conn.Close()
        return fmt.Errorf("failed to connect websocket: %w", err)
    }

    // Start websocket session.
    if err := sess.Start(c.rootCtx); err != nil {
        _ = conn.Close()
        return fmt.Errorf("failed to connect websocket: %w", err)
    }

    // Replace the session if the session has already been opened just in case, 
    c.sessionMu.Lock()
    if c.session != nil {
        sess.Close()
        return nil
    }
    c.session = sess
    c.sessionMu.Unlock()

    return nil
}


//
// Close websocket.
//
// Notes:
//   - Safe to call multiple times.
//   - Disables reconnect loop.
//   - Keeps stored subscriptions intact. If the caller wants to discard them,
//     call ClearSubscriptions separately.
//
// Version:
//   - 2026-04-22: Added.
//
func (c *Client) Close() error {
    // Guard.
    if c == nil {
        return fmt.Errorf("failed to close websocket: missing required parameter: receiver=null")
    }

    c.closeOnce.Do(func() {
        c.closed.Store(true)

        c.sessionMu.Lock()
        sess := c.session
        c.session = nil
        c.sessionMu.Unlock()

        c.subscriptionsMu.Lock()
        clear(c.subscriptions)
        c.subscriptionsMu.Unlock()

        if sess != nil {
            sess.Close()
        }
    })

    return nil
}


//
// Subscribe registers one subscription payload and sends it.
//
// Parameters:
//   - key: Local subscription identifier used for resubscribe tracking.
//   - payload: Raw websocket message to send.
//
// Notes:
//   - Lazy-connects on first use.
//   - Subscription operations are intentionally serialized using subscriptionsMu.
//   - Duplicate subscribe requests for the same key are ignored (no-op).
//   - Payload is copied and stored only after a successful send.
//   - Stored payload is used for resubscribe tracking.
//
// Version:
//   - 2026-04-22: Added.
//
func (c *Client) Subscribe(ctx context.Context, key string, payload []byte) error {
    // Guard.
    if c == nil {
        return fmt.Errorf("failed to subscribe to websocket: missing required parameter: receiver=null")
    }
    if key == "" {
        return fmt.Errorf("failed to subscribe to websocket: missing required parameter: key=empty")
    }
    if len(payload) == 0 {
        return fmt.Errorf("failed to subscribe to websocket: missing required parameter: payload=empty")
    }
    if c.closed.Load() {
        return fmt.Errorf("failed to subscribe to websocket: client already closed")
    }
    if ctx == nil {
        ctx = context.Background()
    }

    c.subscriptionsMu.Lock()
    defer c.subscriptionsMu.Unlock()

    if _, exists := c.subscriptions[key]; exists {
        return nil
    }

    payloadCopy := append([]byte(nil), payload...)

    if err := c.Connect(ctx); err != nil {
        return err
    }

    c.sessionMu.RLock()
    sess := c.session
    c.sessionMu.RUnlock()
    if sess == nil {
        return fmt.Errorf("failed to subscribe to websocket: connection closed")
    }

    if err := sess.Send(payloadCopy); err != nil {
        return fmt.Errorf("failed to subscribe websocket client: %w", err)
    }

    c.subscriptions[key] = payloadCopy

    return nil
}


//
// Unsubscribe removes one stored subscription and sends an unsubscribe payload.
//
// Parameters:
//   - key: Local subscription identifier to remove.
//   - payload: Raw websocket message to send for unsubscribe.
//
// Notes:
//   - Subscription operations are intentionally serialized using subscriptionsMu.
//   - Duplicate unsubscribe requests for a missing key are ignored (no-op).
//   - Payload is copied before send.
//   - Local state is removed only after a successful send.
//
// Version:
//   - 2026-04-22: Added.
//
func (c *Client) Unsubscribe(ctx context.Context, key string, payload []byte) error {
    // Guard.
    if c == nil {
        return fmt.Errorf("failed to unsubscribe to websocket: missing required parameter: receiver=null")
    }
    if key == "" {
        return fmt.Errorf("failed to unsubscribe to websocket: missing required parameter: key=empty")
    }
    if len(payload) == 0 {
        return fmt.Errorf("failed to unsubscribe to websocket: missing required parameter: payload=empty")
    }
    if c.closed.Load() {
        return fmt.Errorf("failed to unsubscribe to websocket: client already closed")
    }
    if ctx == nil {
        ctx = context.Background()
    }

    c.subscriptionsMu.Lock()
    defer c.subscriptionsMu.Unlock()

    if _, exists := c.subscriptions[key]; !exists {
        return nil
    }

    payloadCopy := append([]byte(nil), payload...)

    if err := c.Connect(ctx); err != nil {
        return err
    }

    c.sessionMu.RLock()
    sess := c.session
    c.sessionMu.RUnlock()
    if sess == nil {
        return fmt.Errorf("failed to unsubscribe to websocket: connection closed")
    }

    if err := sess.Send(payloadCopy); err != nil {
        return fmt.Errorf("failed to unsubscribe to websocket: %w", err)
    }

    delete(c.subscriptions, key)

    return nil
}


//
// Sends one raw websocket message through the current session.
//
// Notes:
//   - Lazy-connects on first use.
//
// Version:
//   - 2026-04-22: Added.
//
func (c *Client) SendRaw(ctx context.Context, payload []byte) error {
    // Guard.
    if c == nil {
        return fmt.Errorf("failed to send message to websocket: missing required parameter: receiver=nil")
    }
    if len(payload) == 0 {
        return fmt.Errorf("failed to send message to websocket: missing required parameter: payload=empty")
    }
    if c.closed.Load() {
        return fmt.Errorf("failed to send message to websocket: client already closed")
    }
    if ctx == nil {
        ctx = context.Background()
    }

    payloadCopy := append([]byte(nil), payload...)

    if err := c.Connect(ctx); err != nil {
        return err
    }

    c.sessionMu.RLock()
    sess := c.session
    c.sessionMu.RUnlock()
    if sess == nil {
        return fmt.Errorf("failed to send message to websocket: connection closed")
    }

    if err := sess.Send(payloadCopy); err != nil {
        return fmt.Errorf("failed to send message to websocket: %w", err)
    }

    return nil
}


//
// Resubscribes all saved subscription payloads.
//
// Notes:
//   - Uses lazy Connect() internally, so an active session is established if needed.
//   - Concurrent resubscribe attempts are rejected.
//
// Version:
//   - 2026-04-22: Added.
//
func (c *Client) ResubscribeAll(ctx context.Context) error {
    // Guard.
    if c == nil {
        return fmt.Errorf("failed to resubscribe to websocket: missing required parameter: receiver=nil")
    }
    if c.closed.Load() {
        return fmt.Errorf("failed to resubscribe to websocket: client already closed")
    }
    if ctx == nil {
        ctx = context.Background()
    }
    if !c.reconnecting.CompareAndSwap(false, true) {
        return fmt.Errorf("failed to resubscribe to websocket: recovery already in progress")
    }
    defer c.reconnecting.Store(false)

    // Snapshot subscriptions before reconnect/send.
    snapshot := c.SnapshotSubscriptions()
    if len(snapshot) == 0 {
        return nil
    }

    // Ensure session is available by lazy connect.
    if err := c.Connect(ctx); err != nil {
        return fmt.Errorf("failed to resubscribe to websocket: %w", err)
    }

    c.sessionMu.RLock()
    sess := c.session
    c.sessionMu.RUnlock()
    if sess == nil {
        return fmt.Errorf("failed to resubscribe to websocket: connection closed")
    }

    for _, payload := range snapshot {
        if len(payload) == 0 {
            continue
        }

        payloadCopy := append([]byte(nil), payload...)

        select {
        case <-ctx.Done():
            return fmt.Errorf("failed to resubscribe to websocket: %w", ctx.Err())
        case <-c.rootCtx.Done():
            return fmt.Errorf("failed to resubscribe to websocket: root context canceled: %w", c.rootCtx.Err())
        default:
        }

        if err := sess.Send(payloadCopy); err != nil {
            return fmt.Errorf("failed to resubscribe to websocket: %w", err)
        }
    }

    return nil
}


//
// SnapshotSubscriptions returns a copy of all stored subscription payloads.
//
// Notes:
//   - Returned payloads are deep-copied.
//
// Version:
//   - 2026-04-22: Added.
//
func (c *Client) SnapshotSubscriptions() map[string][]byte {
    // Guard.
    if c == nil {
        return nil
    }

    c.subscriptionsMu.RLock()
    defer c.subscriptionsMu.RUnlock()

    out := make(map[string][]byte, len(c.subscriptions))
    for k, v := range c.subscriptions {
        out[k] = append([]byte(nil), v...)
    }

    return out
}


//
// ClearSubscriptions removes all locally stored subscription payloads.
//
// Notes:
//   - This does not send unsubscribe messages.
//   - Intended for caller-side state reset.
//
// Version:
//   - 2026-04-22: Added.
//
func (c *Client) ClearSubscriptions() {
    // Guard.
    if c == nil {
        return
    }

    c.subscriptionsMu.Lock()
    defer c.subscriptionsMu.Unlock()

    clear(c.subscriptions)
}




