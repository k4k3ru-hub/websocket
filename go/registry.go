//
// registry.go
//
package websocket

import (
    "fmt"
    "sync"
)

var (
    defaultRegistry = NewRegistry()
)

//
// Parameter:
//   - byKey: key -> session IDs
//   - byID: session ID -> session context
//   - keysByID: session ID -> keys
//
type Registry struct {
    mu       sync.RWMutex
    byKey    map[string]map[uint64]struct{}
    keysByID map[uint64]map[string]struct{}
    byID     map[uint64]SessionContext
}

//
// Parameter:
//   - Bound: true if the key and session ID were newly bound.
//   - First: true if this binding became the first session for the key.
//
type BindResult struct {
    Bound bool
    First bool
}

//
// Parameter:
//   - Unbound: true if the binding existed and was removed.
//   - KeyBecameEmpty: true if the key has no remaining sessions after removal.
//
type UnbindResult struct {
    Unbound        bool
    KeyBecameEmpty bool
}


//
// Create new registry.
//
// Version:
//   - 2026-07-04: Added.
//
func NewRegistry() *Registry {
    return &Registry{
        byKey:    make(map[string]map[uint64]struct{}),
        keysByID: make(map[uint64]map[string]struct{}),
        byID:     make(map[uint64]SessionContext),
    }
}


func DefaultRegistry() *Registry {
    return defaultRegistry
}


//
// Register session context by session ID.
//
// Notes:
//   - This method is idempotent for the same session ID.
//   - If the same session ID is registered again, the session context is replaced.
//
// Version:
//   - 2026-07-04: Added.
//
func (r *Registry) Register(sess SessionContext) error {
    // Guard.
    if r == nil {
        return fmt.Errorf("failed to register session to registry: missing required parameter: registry=null")
    }
    if sess == nil {
        return fmt.Errorf("failed to register session to registry: missing required parameter: session=null")
    }

    sessionID := sess.ID()
    if sessionID == 0 {
        return fmt.Errorf("failed to register session to registry: missing required parameter: session_id=0")
    }

    r.mu.Lock()
    defer r.mu.Unlock()

    r.byID[sessionID] = sess

    return nil
}


//
// Bind registered session ID to subscription key.
//
// Notes:
//   - This method is idempotent for the same key and session ID pair.
//   - The session ID must already be registered by Register.
//
// Return:
//   - Added=true if the session ID was newly added to the key.
//   - First=true if the key had no sessions before this Bind.
//
// Version:
//   - 2026-07-04: Added.
//
func (r *Registry) Bind(key string, sessionID uint64) (*BindResult, error) {
    // Guard.
    if r == nil {
        return nil, fmt.Errorf("failed to bind session to registry: missing required parameter: registry=null")
    }
    if key == "" {
        return nil, fmt.Errorf("failed to bind session to registry: missing required parameter: key=%q", "empty")
    }
    if sessionID == 0 {
        return nil, fmt.Errorf("failed to bind session to registry: missing required parameter: session_id=0")
    }

    r.mu.Lock()
    defer r.mu.Unlock()

    if _, exists := r.byID[sessionID]; !exists {
        return nil, fmt.Errorf("failed to bind session to registry: missing required parameter: session_id=%d not registered", sessionID)
    }

    sessionIDs, exists := r.byKey[key]
    if !exists {
        sessionIDs = make(map[uint64]struct{})
        r.byKey[key] = sessionIDs
    }

    if _, exists := sessionIDs[sessionID]; exists {
        return &BindResult{}, nil
    }

    first := len(sessionIDs) == 0

    sessionIDs[sessionID] = struct{}{}

    keys := r.keysByID[sessionID]
    if keys == nil {
        keys = make(map[string]struct{})
        r.keysByID[sessionID] = keys
    }

    keys[key] = struct{}{}

    return &BindResult{
        Bound: true,
        First: first,
    }, nil
}


//
// Register and bind session.
//
// Notes:
//   - This method is idempotent for the same key and session pair.
//
// Return:
//   - Added=true if the session was newly added to the key.
//   - First=true if the key had no sessions before this Add.
//
// Version:
//   - 2026-04-22: Added.
//
func (r *Registry) RegisterAndBind(key string, sess SessionContext) (*BindResult, error) {
    // Guard.
    if r == nil {
        return nil, fmt.Errorf("failed to register and bind session: missing required parameter: registry=null")
    }
    if key == "" {
        return nil, fmt.Errorf("failed to register and bind session: missing required parameter: key=%q", "empty")
    }
    if sess == nil {
        return nil, fmt.Errorf("failed to register and bind session: missing required parameter: session=null")
    }

    sessionID := sess.ID()
    if sessionID == 0 {
        return nil, fmt.Errorf("failed to register and bind session: missing required parameter: session_id=0")
    }

    if err := r.Register(sess); err != nil {
        return nil, fmt.Errorf("failed to register and bind session: %w", err)
    }

    result, err := r.Bind(key, sessionID)
    if err != nil {
        return nil, fmt.Errorf("failed to register and bind session: %w", err)
    }

    return result, nil
}


//
// GetKeys returns a snapshot of keys bound to the session ID.
//
// Version:
//   - 2026-07-05: Added.
//
func (r *Registry) GetKeys(sessionID uint64) []string {
    // Guard.
    if r == nil || sessionID == 0 {
        return nil
    }

    r.mu.RLock()
    defer r.mu.RUnlock()

    keys := r.keysByID[sessionID]
	if len(keys) == 0 {
		return nil
	}

	result := make([]string, 0, len(keys))

	for key := range keys {
		result = append(result, key)
	}

	return result
}


//
// Get session by session ID.
//
// Version:
//   - 2026-07-04: Added.
//
func (r *Registry) GetSession(sessionID uint64) SessionContext {
    // Guard.
    if r == nil || sessionID == 0 {
        return nil
    }

    r.mu.RLock()
    defer r.mu.RUnlock()

    return r.byID[sessionID]
}


//
// GetSessions returns a snapshot of sessions bound to the key.
//
// Version:
//   - 2026-04-22: Added.
//
func (r *Registry) GetSessions(key string) []SessionContext {
    // Guard.
    if r == nil || key == "" {
        return nil
    }

    r.mu.RLock()
    defer r.mu.RUnlock()

    sessionIDs := r.byKey[key]
    if len(sessionIDs) == 0 {
        return nil
    }

    ids := make([]uint64, 0, len(sessionIDs))
    for sessionID := range sessionIDs {
        ids = append(ids, sessionID)
    }

    sessions := make([]SessionContext, 0, len(ids))

    for _, sessionID := range ids {
        sess := r.byID[sessionID]
        if sess == nil {
            continue
        }

        sessions = append(sessions, sess)
    }

    if len(sessions) == 0 {
        return nil
    }

    return sessions
}


//
// GetSessionIDs returns a snapshot of session IDs bound to the key.
//
// Version:
//   - 2026-07-04: Added.
//
func (r *Registry) GetSessionIDs(key string) []uint64 {
    // Guard.
    if r == nil || key == "" {
        return nil
    }

    r.mu.RLock()
    defer r.mu.RUnlock()

    sessionIDs := r.byKey[key]
    if len(sessionIDs) == 0 {
        return nil
    }

    result := make([]uint64, 0, len(sessionIDs))

    for sessionID := range sessionIDs {
        result = append(result, sessionID)
    }

    return result
}


//
// Unregister a session and all of its key bindings.
//
// Version:
//   - 2026-07-16: Added.
//
func (r *Registry) Unregister(sess SessionContext) []string {
    // Guard.
    if r == nil || sess == nil {
        return nil
    }

    return r.UnregisterByID(sess.ID())
}


//
// RemoveSessionByID removes the session ID from all keys.
//
// Return:
//   - Keys that became empty after removal.
//
// Notes:
//   - Safe to call multiple times for the same session ID.
//
// Version:
//   - 2026-07-04: Added.
//
func (r *Registry) UnregisterByID(sessionID uint64) []string {
    // Guard.
    if r == nil || sessionID == 0 {
        return nil
    }

    r.mu.Lock()
    defer r.mu.Unlock()

    keys := r.keysByID[sessionID]
    if len(keys) == 0 {
        delete(r.keysByID, sessionID)
        delete(r.byID, sessionID)

        return nil
    }

    emptyKeys := make([]string, 0)

    for key := range keys {
        sessionIDs := r.byKey[key]
        if sessionIDs == nil {
            continue
        }

        delete(sessionIDs, sessionID)

        if len(sessionIDs) == 0 {
            delete(r.byKey, key)
            emptyKeys = append(emptyKeys, key)
        }
    }

    delete(r.keysByID, sessionID)
    delete(r.byID, sessionID)

    if len(emptyKeys) == 0 {
        return nil
    }

    return emptyKeys
}


//
// RemoveSessionByKeyAndID removes the session ID binding from the specified key.
//
// Notes:
//   - Only the specified key and session ID pair is removed.
//   - If the session ID has no remaining keys, the session context is also
//     removed from the registry.
//   - This method is idempotent for the same key and session ID pair.
//
// Return:
//   - Removed=true if the key and session ID binding existed and was removed.
//   - Last=true if the key has no remaining sessions after removal.
//
// Version:
//   - 2026-07-11: Added.
//
func (r *Registry) Unbind(key string, sessionID uint64) *UnbindResult {
    // Guard.
    if r == nil || key == "" || sessionID == 0 {
        return &UnbindResult{}
    }

    r.mu.Lock()
    defer r.mu.Unlock()

    result := &UnbindResult{}

    sessionIDs := r.byKey[key]
    if sessionIDs == nil {
        return result
    }

    if _, exists := sessionIDs[sessionID]; !exists {
        return result
    }

    delete(sessionIDs, sessionID)

    result.Unbound = true

    if len(sessionIDs) == 0 {
        delete(r.byKey, key)
        result.KeyBecameEmpty = true
    }

    keys := r.keysByID[sessionID]
    if keys != nil {
        delete(keys, key)

        if len(keys) == 0 {
            delete(r.keysByID, sessionID)
        }
    }

    return result
}


//
// LenKeys returns the number of active subscription keys.
//
// Version:
//   - 2026-04-22: Added.
//
func (r *Registry) LenKeys() int {
    if r == nil {
        return 0
    }

    r.mu.RLock()
    defer r.mu.RUnlock()

    return len(r.byKey)
}


//
// LenSessions returns the number of active sessions tracked by reverse index.
//
// Version:
//   - 2026-04-22: Added.
//
func (r *Registry) LenSessions() int {
    if r == nil {
        return 0
    }

    r.mu.RLock()
    defer r.mu.RUnlock()

    return len(r.byID)
}
