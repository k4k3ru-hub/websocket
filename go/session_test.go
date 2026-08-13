package websocket

import (
	"testing"
	"time"
)

type initialMessageReadWaitTimeoutTestHandler struct {
	closedCh chan struct{}
}

func (h *initialMessageReadWaitTimeoutTestHandler) HandleMessage(SessionContext, []byte) {}

func (h *initialMessageReadWaitTimeoutTestHandler) HandleClose(SessionContext) {
	select {
	case <-h.closedCh:
	default:
		close(h.closedCh)
	}
}

func TestSessionInitialMessageReadWaitTimeoutClosesSession(t *testing.T) {
	sess := newInitialMessageReadWaitTimeoutTestSession(20 * time.Millisecond)
	go sess.runInitialMessageReadWaitTimeout()

	select {
	case <-sess.Done():
	case <-time.After(time.Second):
		t.Fatal("session did not close after initial message read wait timeout")
	}
}

func TestSessionInitialMessageReadPreventsTimeout(t *testing.T) {
	sess := newInitialMessageReadWaitTimeoutTestSession(20 * time.Millisecond)
	go sess.runInitialMessageReadWaitTimeout()
	defer sess.Close()

	sess.markInitialMessageRead()

	select {
	case <-sess.Done():
		t.Fatal("session closed after initial message was read")
	case <-time.After(60 * time.Millisecond):
	}
}

func newInitialMessageReadWaitTimeoutTestSession(timeout time.Duration) *Session {
	return &Session{
		handler:                       &initialMessageReadWaitTimeoutTestHandler{closedCh: make(chan struct{})},
		doneCh:                        make(chan struct{}),
		initialMessageReadCh:          make(chan struct{}),
		initialMessageReadWaitTimeout: timeout,
	}
}

func TestDefaultServerOptionEnablesInitialMessageReadWaitTimeout(t *testing.T) {
	option := DefaultServerOption()

	if option.SessionOption.InitialMessageReadWaitTimeout != DefaultInitialMessageReadWaitTimeout {
		t.Fatalf(
			"unexpected initial message read wait timeout: got=%s want=%s",
			option.SessionOption.InitialMessageReadWaitTimeout,
			DefaultInitialMessageReadWaitTimeout,
		)
	}
}
