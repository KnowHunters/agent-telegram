package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"agent-telegram/internal/gateway"
	"github.com/coder/websocket"
)

func gatewayFixture(t *testing.T) (*HTTPServer, *gateway.Store, string) {
	t.Helper()
	store, err := gateway.Open(filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close journal: %v", err)
		}
	})
	srv := NewHTTPServer(0, "test-secret", "")
	srv.EnableGateway(store, func(context.Context) string { return "alice" })
	server := httptest.NewServer(srv.srv.Handler)
	t.Cleanup(func() { server.Close(); srv.wsWG.Wait() })
	return srv, store, "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
}
func wsConnect(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, handshake, err := websocket.Dial(ctx, url, nil)
	if handshake != nil && handshake.Body != nil {
		_ = handshake.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	wsWrite(t, c, map[string]any{"type": "auth", "token": "test-secret"})
	ready := wsRead(t, c)
	if ready["type"] != "ready" {
		t.Fatal(ready)
	}
	return c
}
func wsWrite(t *testing.T, c *websocket.Conn, value any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
}
func wsRead(t *testing.T, c *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, b, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err = json.Unmarshal(b, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestWebSocketReceiptsSharedWithHTTP(t *testing.T) {
	srv, _, url := gatewayFixture(t)
	var calls atomic.Int32
	srv.Register("send_message", func(context.Context, json.RawMessage) (any, *ErrorObject) {
		return map[string]any{"id": calls.Add(1)}, nil
	})
	c := wsConnect(t, url)
	request := map[string]any{"type": "request", "id": "send-1", "method": "send_message", "params": map[string]any{"peer": "@bot", "message": "hello"}}
	wsWrite(t, c, request)
	first := wsRead(t, c)
	if first["ok"] != true {
		t.Fatal(first)
	}
	wsWrite(t, c, request)
	second := wsRead(t, c)
	if second["ok"] != true || calls.Load() != 1 {
		t.Fatalf("duplicate side effect: %v %d", second, calls.Load())
	}
	body := []byte(`{"message":"hello","peer":"@bot"}`)
	req := httptest.NewRequest(http.MethodPost, "/rpc/send_message", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-secret")
	req.Header.Set("X-Request-Id", "send-1")
	rec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != 200 || calls.Load() != 1 {
		t.Fatalf("HTTP dedupe failed: %d %s", rec.Code, rec.Body.String())
	}
	request["params"] = map[string]any{"peer": "@bot", "message": "changed"}
	wsWrite(t, c, request)
	if response := wsRead(t, c); response["ok"] != false {
		t.Fatal(response)
	}
	if calls.Load() != 1 {
		t.Fatal("conflicting ID executed")
	}
}

func TestWebSocketEventsResumeAndPeerFilter(t *testing.T) {
	_, store, url := gatewayFixture(t)
	if err := store.Append(context.Background(), "alice", "new_message", map[string]any{"message": map[string]any{"peer": "user:1", "text": "one"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(context.Background(), "bob", "new_message", map[string]any{"text": "private"}); err != nil {
		t.Fatal(err)
	}
	c := wsConnect(t, url)
	wsWrite(t, c, map[string]any{"type": "request", "id": "subscribe", "method": "events.subscribe", "params": map[string]any{"cursor": 0, "peers": []string{"user:1"}}})
	if ack := wsRead(t, c); ack["ok"] != true {
		t.Fatal(ack)
	}
	event := wsRead(t, c)
	if event["event"] != "new_message" || event["account"] != "alice" {
		t.Fatal(event)
	}
	checkpoint := wsRead(t, c)
	if checkpoint["cursor"] != float64(2) {
		t.Fatal(checkpoint)
	}
	if err := store.Append(context.Background(), "alice", "edit_message", map[string]any{"message": map[string]any{"peer": "user:1", "text": "edited"}}); err != nil {
		t.Fatal(err)
	}
	if event = wsRead(t, c); event["event"] != "edit_message" {
		t.Fatal(event)
	}
}

type blockedEventPeer struct{}

func (blockedEventPeer) Check(_ context.Context, method string, params json.RawMessage) error {
	var p struct {
		Peer string `json:"peer"`
	}
	_ = json.Unmarshal(params, &p)
	if p.Peer == "user:blocked" {
		return NewPolicyDeniedError(method, "peer denied")
	}
	return nil
}

func TestWebSocketChecksPolicyOnNestedTelegramMessage(t *testing.T) {
	srv, store, url := gatewayFixture(t)
	srv.SetPolicyChecker(blockedEventPeer{})
	for _, peer := range []string{"user:blocked", "user:allowed"} {
		if err := store.Append(context.Background(), "alice", "new_message", map[string]any{"message": map[string]any{"peer": peer}}); err != nil {
			t.Fatal(err)
		}
	}
	c := wsConnect(t, url)
	wsWrite(t, c, map[string]any{"type": "request", "id": "sub", "method": "events.subscribe", "params": map[string]any{"cursor": 0}})
	if ack := wsRead(t, c); ack["ok"] != true {
		t.Fatal(ack)
	}
	event := wsRead(t, c)
	if event["type"] != "event" || event["data"].(map[string]any)["peer"] != "user:allowed" {
		t.Fatal("peer policy bypass", event)
	}
	if checkpoint := wsRead(t, c); checkpoint["type"] != "checkpoint" {
		t.Fatal(checkpoint)
	}
}

func TestWebSocketRejectsAuthAndOrigin(t *testing.T) {
	_, _, url := gatewayFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, handshake, err := websocket.Dial(ctx, url, nil)
	if handshake != nil && handshake.Body != nil {
		_ = handshake.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.CloseNow() }()
	wsWrite(t, c, map[string]any{"type": "auth", "token": "wrong"})
	if _, _, err = c.Read(ctx); websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("auth error: %v", err)
	}
	_, response, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"https://untrusted.example"}}})
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err == nil || response.StatusCode != 403 {
		t.Fatalf("origin accepted: %v", err)
	}
}

func TestWebSocketLongRequestDoesNotBlockReceiveLoop(t *testing.T) {
	srv, _, url := gatewayFixture(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	srv.Register("get_me", func(ctx context.Context, _ json.RawMessage) (any, *ErrorObject) {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return map[string]any{"id": 1}, nil
	})
	c := wsConnect(t, url)
	wsWrite(t, c, map[string]any{"type": "request", "id": "long", "method": "get_me"})
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not start")
	}
	wsWrite(t, c, map[string]any{"type": "request", "id": "sub", "method": "events.subscribe"})
	if ack := wsRead(t, c); ack["id"] != "sub" || ack["ok"] != true {
		t.Fatal("receive loop blocked", ack)
	}
}

func TestWebSocketRespectsReadPolicyAndMissingWriteID(t *testing.T) {
	srv, _, url := gatewayFixture(t)
	srv.SetPolicyChecker(denyPolicyChecker{})
	c := wsConnect(t, url)
	wsWrite(t, c, map[string]any{"type": "request", "id": "sub", "method": "events.subscribe"})
	if response := wsRead(t, c); response["ok"] != false {
		t.Fatal("read policy bypass", response)
	}
	srv.SetPolicyChecker(nil)
	req := httptest.NewRequest(http.MethodPost, "/rpc/send_message", strings.NewReader(`{"peer":"@bot","message":"hi"}`))
	req.Header.Set("Authorization", "Bearer test-secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("write without id: %d", rec.Code)
	}
}
