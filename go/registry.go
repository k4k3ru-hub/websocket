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

type Registry struct {
    mu    sync.RWMutex
    byKey map[string]map[*Session]struct{}
    bySes map[*Session]map[string]struct{}
}

type AddResult struct {
    Added bool
    First bool
}

type RemoveResult struct {
    Removed bool
    Empty   bool
}

func NewRegistry() *Registry {
    return &Registry{
        byKey: make(map[string]map[*Session]struct{}),
        bySes: make(map[*Session]map[string]struct{}),
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
func (r *Registry) Add(key string, sess *Session) (*AddResult, error) {
    // Guard.
    if r == nil {
        return nil, fmt.Errorf("failed to add session to registry: missing required parameter: receiver=null")
    }
    if key == "" {
        return nil, fmt.Errorf("failed to add session to registry: missing required parameter: key=empty")
    }
    if sess == nil {
        return nil, fmt.Errorf("failed to add session to registry: missing required parameter: session=null")
    }

    r.mu.Lock()
    defer r.mu.Unlock()

    sessions, exists := r.byKey[key]
    if !exists {
        sessions = make(map[*Session]struct{})
        r.byKey[key] = sessions
    }

    if _, exists := sessions[sess]; exists {
        return &AddResult{
            Added: false,
            First: false,
        }, nil
    }

    first := len(sessions) == 0
    sessions[sess] = struct{}{}

    keys, exists := r.bySes[sess]
    if !exists {
        keys = make(map[string]struct{})
        r.bySes[sess] = keys
    }
    keys[key] = struct{}{}

    return &AddResult{
        Added: true,
        First: first,
    }, nil
}


//
// Remove session from subscription key.
//
// Notes:
//   - This method is idempotent for the same key and session pair.
//   - If the key becomes empty, it is removed from the registry.
//
// Version:
//   - 2026-04-22: Added.
//
func (r *Registry) Remove(key string, sess *Session) (*RemoveResult, error) {
    // Guard.
    if r == nil {
        return nil, fmt.Errorf("failed to remove session from registry: missing required parameter: receiver=null")
    }
    if key == "" {
        return nil, fmt.Errorf("failed to remove session from registry: missing required parameter: key=empty")
    }
    if sess == nil {
        return nil, fmt.Errorf("failed to remove session from registry: missing required parameter: session=null")
    }

    r.mu.Lock()
    defer r.mu.Unlock()

    sessions, exists := r.byKey[key]
    if !exists {
        return &RemoveResult{
            Removed: false,
            Empty:   true,
        }, nil
    }

    if _, exists := sessions[sess]; !exists {
        return &RemoveResult{
            Removed: false,
            Empty:   len(sessions) == 0,
        }, nil
    }

    delete(sessions, sess)
    empty := len(sessions) == 0
    if empty {
        delete(r.byKey, key)
    }

    if keys, exists := r.bySes[sess]; exists {
        delete(keys, key)
        if len(keys) == 0 {
            delete(r.bySes, sess)
        }
    }

    return &RemoveResult{
        Removed: true,
        Empty:   empty,
    }, nil
}

//
// GetSessions returns a snapshot of sessions bound to the key.
//
// Version:
//   - 2026-04-22: Added.
//
func (r *Registry) GetSessions(key string) []*Session {
    // Guard.
    if r == nil || key == "" {
        return nil
    }

    r.mu.RLock()
    defer r.mu.RUnlock()

    sessions, ok := r.byKey[key]
    if !ok || len(sessions) == 0 {
        return nil
    }

    out := make([]*Session, 0, len(sessions))
    for sess := range sessions {
        out = append(out, sess)
    }

    return out
}

//
// GetKeys returns a snapshot of keys bound to the session.
//
// Version:
//   - 2026-04-22: Added.
//
func (r *Registry) GetKeys(sess *Session) []string {
    // Guard.
    if r == nil || sess == nil {
        return nil
    }

    r.mu.RLock()
    defer r.mu.RUnlock()

    keys, ok := r.bySes[sess]
    if !ok || len(keys) == 0 {
        return nil
    }

    out := make([]string, 0, len(keys))
    for key := range keys {
        out = append(out, key)
    }

    return out
}

//
// RemoveSession removes all keys bound to the session.
//
// Return:
//   - Keys that became empty after removal.
//
// Notes:
//   - Safe to call multiple times for the same session.
//
// Version:
//   - 2026-04-22: Added.
//
func (r *Registry) RemoveSession(sess *Session) []string {
    // Guard.
    if r == nil || sess == nil {
        return nil
    }

    r.mu.Lock()
    defer r.mu.Unlock()

    keys, ok := r.bySes[sess]
    if !ok {
        return nil
    }

    emptyKeys := make([]string, 0, len(keys))
    for key := range keys {
        sessions, ok := r.byKey[key]
        if !ok {
            continue
        }

        delete(sessions, sess)
        if len(sessions) == 0 {
            delete(r.byKey, key)
            emptyKeys = append(emptyKeys, key)
        }
    }

    delete(r.bySes, sess)

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

    return len(r.bySes)
}
