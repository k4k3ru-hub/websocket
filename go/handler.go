//
// handler.go
//
package websocket


//
// SessionContext.
//
// Notes:
//   - Provides the minimal operations required by a websocket session handler.
//   - This interface helps keep handlers decoupled from the concrete Session implementation.
//
// Version:
//   - 2026-04-24: Added.
//
type SessionContext interface {
    Close()
    Send(message []byte) error
    SendJSON(v any) error
}


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
	HandleMessage(SessionContext, []byte)
	HandleClose(SessionContext)
}



