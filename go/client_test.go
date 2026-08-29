package websocket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	gorillaWebsocket "github.com/gorilla/websocket"
)

func TestClientDisconnectAllowsReconnect(t *testing.T) {
	t.Parallel()

	var connections atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := (&gorillaWebsocket.Upgrader{}).Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		connections.Add(1)
		defer connection.Close()
		for {
			if _, _, err := connection.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	client, err := NewClient(
		context.Background(),
		"ws"+strings.TrimPrefix(server.URL, "http"),
		disconnectTestHandler{},
		nil,
	)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer client.Close()

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("first Connect() error = %v", err)
	}
	if err := client.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if err := client.Disconnect(); err != nil {
		t.Fatalf("repeated Disconnect() error = %v", err)
	}
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("second Connect() error = %v", err)
	}
	if got := connections.Load(); got != 2 {
		t.Fatalf("connections = %d, want 2", got)
	}
}

func TestClientDisconnectPreservesSubscriptions(t *testing.T) {
	t.Parallel()

	client := &Client{subscriptions: map[string][]byte{"subscription": []byte("payload")}}
	if err := client.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if got := string(client.SnapshotSubscriptions()["subscription"]); got != "payload" {
		t.Fatalf("subscription payload = %q, want payload", got)
	}
}

func TestClientDisconnectValidatesState(t *testing.T) {
	t.Parallel()

	var client *Client
	if err := client.Disconnect(); err == nil || !strings.Contains(err.Error(), "receiver=null") {
		t.Fatalf("nil client Disconnect() error = %v", err)
	}

	client = &Client{subscriptions: make(map[string][]byte)}
	client.closed.Store(true)
	if err := client.Disconnect(); err == nil || !strings.Contains(err.Error(), "client already closed") {
		t.Fatalf("closed client Disconnect() error = %v", err)
	}
}

type disconnectTestHandler struct{}

func (disconnectTestHandler) HandleMessage(SessionContext, []byte) {}
func (disconnectTestHandler) HandleClose(SessionContext)           {}
