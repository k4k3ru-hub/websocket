//
// handler.go
//
package websocket

//
// SessionHandler.
//
// Notes:
//   - Handles websocket session lifecycle events.
//   - HandleMessage handles one raw websocket message.
//   - HandleClose is called only once when the session is closed.
//   - Application-specific logic should be implemented outside Session.
//
// Version:
//   - 2026-04-22: Added.
//
type SessionHandler interface {
	HandleMessage(*Session, []byte)
	HandleClose(*Session)
}
