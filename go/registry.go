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
//
type Registry struct {
    mu    sync.RWMutex
    byKey map[string]map[uint64]struct{}
    byID  map[uint64]SessionContext
}

//
// Parameter:
//   - byKey: key -> downstream session IDs
//   - byUpstreamSessionID: upstream session ID -> keys
//   - byDownstreamSessionID: downstream session ID -> keys
//
type BridgeBindingRegistry struct {
    mu                    sync.RWMutex
    byKey                 map[string]map[uint64]struct{}
    byUpstreamSessionID   map[uint64]map[string]struct{}
    byDownstreamSessionID map[uint64]map[string]struct{}
}

type AddResult struct {
    Added bool
    First bool
}

type RemoveResult struct {
    Removed bool
    Empty   bool
}


//
// Create new registry.
//
// Version:
//   - 2026-07-04: Added.
//
func NewRegistry() *Registry {
    return &Registry{
        byKey: make(map[string]map[uint64]struct{}),
        byID:  make(map[uint64]SessionContext),
    }
}


//
// Create new bridge binding registry.
//
// Version:
//   - 2026-07-04: Added.
//
func NewBridgeBindingRegistry() *BridgeBindingRegistry {
	return &BridgeBindingRegistry{
		byKey:                 make(map[string]map[uint64]struct{}),
		byUpstreamSessionID:   make(map[uint64]map[string]struct{}),
		byDownstreamSessionID: make(map[uint64]map[string]struct{}),
	}
}


func DefaultRegistry() *Registry {
    return defaultRegistry
}


//
// Add session to subscription key.
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
func (r *Registry) Add(key string, sess SessionContext) (*AddResult, error) {
    // Guard.
    if r == nil {
        return nil, fmt.Errorf("failed to add session to registry: missing required parameter: registry=null")
    }
    if key == "" {
        return nil, fmt.Errorf("failed to add session to registry: missing required parameter: key=empty")
    }
    if sess == nil {
        return nil, fmt.Errorf("failed to add session to registry: missing required parameter: session=null")
    }

    sessionID := sess.ID()
    if sessionID == 0 {
        return nil, fmt.Errorf("failed to add session to registry: missing required parameter: session_id=0")
    }

    r.mu.Lock()
    defer r.mu.Unlock()

    sessionIDs, exists := r.byKey[key]
    if !exists {
        sessionIDs = make(map[uint64]struct{})
        r.byKey[key] = sessionIDs
    }

    if _, exists := sessionIDs[sessionID]; exists {
        r.byID[sessionID] = sess
        return &AddResult{
            Added: false,
            First: false,
        }, nil
    }

    first := len(sessionIDs) == 0
    sessionIDs[sessionID] = struct{}{}
    r.byID[sessionID] = sess

    return &AddResult{
        Added: true,
        First: first,
    }, nil
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

    sessionIDs, ok := r.byKey[key]
    if !ok || len(sessionIDs) == 0 {
        return nil
    }

    out := make([]SessionContext, 0, len(sessionIDs))
    for sessionID := range sessionIDs {
        sess := r.byID[sessionID]
        if sess == nil {
            continue
        }

        out = append(out, sess)
    }

    return out
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

    sessionIDs, ok := r.byKey[key]
    if !ok || len(sessionIDs) == 0 {
        return nil
    }

    out := make([]uint64, 0, len(sessionIDs))
    for sessionID := range sessionIDs {
        out = append(out, sessionID)
    }

    return out
}


//
// RemoveSession removes the session from registry.
//
// Return:
//   - Keys that became empty after removal.
//
// Notes:
//   - Safe to call multiple times for the same session.
//
// Version:
//   - 2026-04-22: Added.
//   - 2026-07-04: Changed registry index from session context to session ID.
//
func (r *Registry) RemoveSession(sess SessionContext) []string {
	// Guard.
	if r == nil || sess == nil {
		return nil
	}

	return r.RemoveSessionByID(sess.ID())
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
func (r *Registry) RemoveSessionByID(sessionID uint64) []string {
    // Guard.
    if r == nil || sessionID == 0 {
        return nil
    }

    r.mu.Lock()
    defer r.mu.Unlock()

    emptyKeys := make([]string, 0)
    for key, sessionIDs := range r.byKey {
        if _, exists := sessionIDs[sessionID]; !exists {
            continue
        }

        delete(sessionIDs, sessionID)
        if len(sessionIDs) == 0 {
            delete(r.byKey, key)
            emptyKeys = append(emptyKeys, key)
        }
    }

    delete(r.byID, sessionID)

    return emptyKeys
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


//
// Bind bridge sessions.
//
// Version:
//   - 2026-07-04: Added.
//
func (r *BridgeBindingRegistry) Bind(key string, downstreamSessionID uint64, upstreamSessionID uint64) {
	if r == nil || key == "" || downstreamSessionID == 0 || upstreamSessionID == 0 {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.byKey[key] == nil {
		r.byKey[key] = make(map[uint64]struct{})
	}
	r.byKey[key][downstreamSessionID] = struct{}{}

	if r.byUpstreamSessionID[upstreamSessionID] == nil {
		r.byUpstreamSessionID[upstreamSessionID] = make(map[string]struct{})
	}
	r.byUpstreamSessionID[upstreamSessionID][key] = struct{}{}

	if r.byDownstreamSessionID[downstreamSessionID] == nil {
		r.byDownstreamSessionID[downstreamSessionID] = make(map[string]struct{})
	}
	r.byDownstreamSessionID[downstreamSessionID][key] = struct{}{}
}


//
// Find downstream session IDs by key.
//
// Version:
//   - 2026-07-04: Added.
//
func (r *BridgeBindingRegistry) FindDownstreamSessionIDsByKey(key string) []uint64 {
	if r == nil || key == "" {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	items := r.byKey[key]
	if len(items) == 0 {
		return nil
	}

	result := make([]uint64, 0, len(items))
	for downstreamSessionID := range items {
		result = append(result, downstreamSessionID)
	}

	return result
}

//
// Find keys by upstream session ID.
//
// Version:
//   - 2026-07-04: Added.
//
func (r *BridgeBindingRegistry) FindKeysByUpstreamSessionID(upstreamSessionID uint64) []string {
	if r == nil || upstreamSessionID == 0 {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	items := r.byUpstreamSessionID[upstreamSessionID]
	if len(items) == 0 {
		return nil
	}

	result := make([]string, 0, len(items))
	for key := range items {
		result = append(result, key)
	}

	return result
}

//
// Find keys by downstream session ID.
//
// Version:
//   - 2026-07-04: Added.
//
func (r *BridgeBindingRegistry) FindKeysByDownstreamSessionID(downstreamSessionID uint64) []string {
	if r == nil || downstreamSessionID == 0 {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	items := r.byDownstreamSessionID[downstreamSessionID]
	if len(items) == 0 {
		return nil
	}

	result := make([]string, 0, len(items))
	for key := range items {
		result = append(result, key)
	}

	return result
}

//
// Unbind bridge sessions.
//
// Version:
//   - 2026-07-04: Added.
//
func (r *BridgeBindingRegistry) Unbind(key string, downstreamSessionID uint64, upstreamSessionID uint64) {
	if r == nil || key == "" || downstreamSessionID == 0 || upstreamSessionID == 0 {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.byKey[key], downstreamSessionID)
	if len(r.byKey[key]) == 0 {
		delete(r.byKey, key)
	}

	delete(r.byUpstreamSessionID[upstreamSessionID], key)
	if len(r.byUpstreamSessionID[upstreamSessionID]) == 0 {
		delete(r.byUpstreamSessionID, upstreamSessionID)
	}

	delete(r.byDownstreamSessionID[downstreamSessionID], key)
	if len(r.byDownstreamSessionID[downstreamSessionID]) == 0 {
		delete(r.byDownstreamSessionID, downstreamSessionID)
	}
}
