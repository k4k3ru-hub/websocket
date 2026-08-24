package websocket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gorillaWebsocket "github.com/gorilla/websocket"
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

type applicationPongTestHandler struct {
	messages chan string
	closed   chan struct{}
}

func (h *applicationPongTestHandler) HandleMessage(_ SessionContext, message []byte) {
	h.messages <- string(message)
}

func (h *applicationPongTestHandler) HandleClose(SessionContext) {
	select {
	case <-h.closed:
	default:
		close(h.closed)
	}
}

func TestSessionConsumesApplicationPongAndRefreshesReadDeadline(t *testing.T) {
	upgrader := gorillaWebsocket.Upgrader{}
	serverErrors := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			serverErrors <- err
			return
		}
		defer connection.Close()
		if err := connection.WriteMessage(gorillaWebsocket.TextMessage, []byte("pong")); err != nil {
			serverErrors <- err
			return
		}
		time.Sleep(80 * time.Millisecond)
		if err := connection.WriteMessage(gorillaWebsocket.TextMessage, []byte("market-data")); err != nil {
			serverErrors <- err
		}
	}))
	defer server.Close()

	endpoint := "ws" + strings.TrimPrefix(server.URL, "http")
	connection, _, err := gorillaWebsocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	handler := &applicationPongTestHandler{messages: make(chan string, 1), closed: make(chan struct{})}
	option := DefaultSessionOption()
	option.InitialReadWait = 40 * time.Millisecond
	option.PongWait = 200 * time.Millisecond
	option.PingPeriod = time.Hour
	option.PingMessageType = gorillaWebsocket.TextMessage
	option.PingPayload = []byte("ping")
	option.PongPayload = []byte("pong")
	session, err := NewSession(connection, handler, option)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	select {
	case message := <-handler.messages:
		if message != "market-data" {
			t.Fatalf("HandleMessage() message = %q, want market-data", message)
		}
	case err := <-serverErrors:
		t.Fatal(err)
	case <-handler.closed:
		t.Fatal("session closed before market data arrived")
	case <-time.After(time.Second):
		t.Fatal("market data was not delivered")
	}
}

func TestSessionOptionCloneCopiesApplicationHeartbeatPayloads(t *testing.T) {
	option := &SessionOption{PingPayload: []byte("ping"), PongPayload: []byte("pong")}
	clone := option.Clone()
	option.PingPayload[0] = 'x'
	option.PongPayload[0] = 'x'
	if string(clone.PingPayload) != "ping" || string(clone.PongPayload) != "pong" {
		t.Fatalf("Clone() heartbeat payloads = %q/%q", clone.PingPayload, clone.PongPayload)
	}
}
