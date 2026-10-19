package ipc

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"agent-telegram/internal/gateway"
	"agent-telegram/internal/operations"
	"github.com/coder/websocket"
)

// EnableGateway enables /ws and durable request receipts on the existing HTTP API.
// Account must identify the authorized Telegram user, not a mutable session profile.
func (s *HTTPServer) EnableGateway(store *gateway.Store, account func(context.Context) string) {
	s.gateway = store
	s.account = account
}

func (s *HTTPServer) handleReady(w http.ResponseWriter, r *http.Request) {
	account := ""
	if s.account != nil {
		account = s.account(r.Context())
	}
	status := http.StatusOK
	if account == "" {
		status = http.StatusServiceUnavailable
	}
	writeJSONResponse(w, status, map[string]any{"ready": account != "", "account": account})
}

type wsRequest struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Token   string          `json:"token,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	Confirm bool            `json:"confirm,omitempty"`
}

// rpcBuffer lets both network transports execute the exact HTTP validation,
// file-access, policy, confirmation and audit pipeline without a shell or loopback call.
type rpcBuffer struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (b *rpcBuffer) Header() http.Header { return b.header }
func (b *rpcBuffer) WriteHeader(n int)   { b.status = n }
func (b *rpcBuffer) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = 200
	}
	return b.body.Write(p)
}
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *HTTPServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	s.wsWG.Add(1)
	defer s.wsWG.Done()
	var workers sync.WaitGroup
	defer workers.Wait()
	if s.gateway == nil || s.secret == "" {
		http.Error(w, "authenticated gateway not enabled", http.StatusServiceUnavailable)
		return
	}
	// Browser origins must be explicitly allowed; non-browser clients omit Origin.
	if origin := r.Header.Get("Origin"); origin != "" && (s.cors == "*" || allowedCORSOrigin(s.cors, origin) == "") {
		http.Error(w, "origin forbidden", http.StatusForbidden)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(maxAPIBodySize)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	authCtx, authCancel := context.WithTimeout(ctx, 5*time.Second)
	_, first, err := conn.Read(authCtx)
	authCancel()
	if err != nil {
		return
	}
	var auth wsRequest
	if json.Unmarshal(first, &auth) != nil || auth.Type != "auth" || subtle.ConstantTimeCompare([]byte(auth.Token), []byte(s.secret)) != 1 {
		_ = conn.Close(websocket.StatusPolicyViolation, "authentication failed")
		return
	}
	account := s.account(ctx)
	if account == "" {
		_ = conn.Close(websocket.StatusTryAgainLater, "Telegram not ready")
		return
	}
	out := make(chan any, 64)
	send := func(v any) bool {
		select {
		case out <- v:
			return true
		default:
			cancel()
			return false
		}
	}
	workers.Add(1)
	go func() {
		defer workers.Done()
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case value := <-out:
				body, err := json.Marshal(value)
				if err != nil {
					return
				}
				writeCtx, done := context.WithTimeout(ctx, 10*time.Second)
				err = conn.Write(writeCtx, websocket.MessageText, body)
				done()
				if err != nil {
					return
				}
			}
		}
	}()
	workers.Add(1)
	go func() {
		defer workers.Done()
		defer cancel()
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pingCtx, done := context.WithTimeout(ctx, 10*time.Second)
				err := conn.Ping(pingCtx)
				done()
				if err != nil {
					return
				}
			}
		}
	}()
	send(map[string]any{"type": "ready", "schema": 1, "account": account})
	slots := make(chan struct{}, 8)
	var subscriptionCancel context.CancelFunc
	defer func() {
		if subscriptionCancel != nil {
			subscriptionCancel()
		}
	}()
	for {
		_, body, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var req wsRequest
		if err = json.Unmarshal(body, &req); err != nil || req.Type != "request" || req.ID == "" || len(req.ID) > 128 {
			send(wsError(req.ID, "validation", "request requires type=request and id (1..128 characters)"))
			continue
		}
		if req.Method == "events.subscribe" {
			var p struct {
				Cursor *int64   `json:"cursor"`
				Kinds  []string `json:"kinds"`
				Peers  []string `json:"peers"`
			}
			if len(req.Params) > 0 && json.Unmarshal(req.Params, &p) != nil {
				send(wsError(req.ID, "validation", "invalid subscription"))
				continue
			}
			if err := s.checkHTTPPolicy(WithSurface(ctx, SurfaceHTTP), "get_updates", json.RawMessage(`{}`)); err != nil {
				send(wsError(req.ID, "forbidden", err.Message))
				continue
			}
			cursor := int64(0)
			if p.Cursor == nil {
				cursor, err = s.gateway.Latest(ctx)
			} else {
				cursor = *p.Cursor
			}
			if cursor < 0 || err != nil {
				send(wsError(req.ID, "validation", "invalid cursor"))
				continue
			}
			if subscriptionCancel != nil {
				subscriptionCancel()
			}
			subCtx, stopSubscription := context.WithCancel(ctx)
			subscriptionCancel = stopSubscription
			send(map[string]any{"type": "response", "id": req.ID, "ok": true, "data": map[string]any{"cursor": cursor}})
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer stopSubscription()
				s.streamEvents(subCtx, account, cursor, p.Kinds, p.Peers, send)
			}()
			continue
		}
		if req.Method == "events.unsubscribe" {
			if subscriptionCancel != nil {
				subscriptionCancel()
				subscriptionCancel = nil
			}
			send(map[string]any{"type": "response", "id": req.ID, "ok": true})
			continue
		}
		if s.account(ctx) != account {
			_ = conn.Close(websocket.StatusPolicyViolation, "account changed; reconnect")
			return
		}
		if req.Method == "reload_session" {
			send(wsError(req.ID, "forbidden", "restart gateway to change account"))
			continue
		}
		if _, ok := s.lookupHandler(req.Method); !ok {
			send(wsError(req.ID, "method_not_found", "unknown operation"))
			continue
		}
		select {
		case slots <- struct{}{}:
			workers.Add(1)
			go func(req wsRequest) {
				defer workers.Done()
				defer func() { <-slots }()
				s.executeWebSocketRequest(ctx, req, send)
			}(req)
		default:
			send(wsError(req.ID, "busy", "at most eight requests may be in flight"))
		}
	}
}

func (s *HTTPServer) executeWebSocketRequest(ctx context.Context, req wsRequest, send func(any) bool) {
	// Requests use the same endpoint and same method names as HTTP /rpc/{method}.
	params := req.Params
	if len(params) == 0 {
		params = json.RawMessage(`{}`)
	}
	callCtx, done := context.WithTimeout(ctx, ClientTimeout())
	request, err := http.NewRequestWithContext(callCtx, http.MethodPost, "http://gateway/rpc/"+req.Method, bytes.NewReader(params))
	if err != nil {
		done()
		send(wsError(req.ID, "validation", "invalid method"))
		return
	}
	request.SetPathValue("method", req.Method)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-Id", req.ID)
	if req.Confirm {
		request.Header.Set("X-Confirm-Action", "true")
	}
	buffer := &rpcBuffer{header: make(http.Header)}
	s.handleGatewayRPC(buffer, request)
	done()
	var result map[string]any
	if json.Unmarshal(buffer.body.Bytes(), &result) != nil {
		send(wsError(req.ID, "internal", "invalid operation response"))
		return
	}
	result["type"] = "response"
	result["id"] = req.ID
	send(result)
}

func wsError(id, kind, message string) map[string]any {
	return map[string]any{"type": "response", "id": id, "ok": false, "error": map[string]any{"type": kind, "message": message}}
}

func gatewayReadMethod(method string) bool {
	if method == "status" || method == "ping" || method == "pong" {
		return true
	}
	op, ok := operations.Get(method)
	return ok && op.Safety == operations.SafetyRead
}

func (s *HTTPServer) streamEvents(ctx context.Context, account string, cursor int64, kinds, peers []string, send func(any) bool) {
	for {
		events, next, gap, changed, err := s.gateway.Read(ctx, account, cursor)
		if err != nil {
			send(wsError("", "event_store", "event storage unavailable"))
			return
		}
		if gap {
			send(map[string]any{"type": "resync_required", "cursor": cursor, "account": account})
			return
		}
		for _, event := range events {
			if ctx.Err() != nil {
				return
			}
			// Also normalize events journaled by builds before the flat schema.
			var legacy map[string]json.RawMessage
			if json.Unmarshal(event.Data, &legacy) == nil && len(legacy["message"]) > 0 && legacy["message"][0] == '{' {
				event.Data = legacy["message"]
			}
			if s.checkHTTPPolicy(WithSurface(ctx, SurfaceHTTP), "get_updates", event.Data) != nil {
				continue
			}
			if len(peers) > 0 {
				var data struct {
					Peer string `json:"peer"`
				}
				_ = json.Unmarshal(event.Data, &data)
				matched := false
				for _, peer := range peers {
					if peer == data.Peer {
						matched = true
					}
				}
				if !matched {
					continue
				}
			}
			match := len(kinds) == 0
			for _, kind := range kinds {
				if kind == event.Event {
					match = true
				}
			}
			if match && !send(event) {
				return
			}
		}
		if next > cursor {
			cursor = next
			if !send(map[string]any{"type": "checkpoint", "cursor": cursor}) {
				return
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-changed:
		}
	}
}

// handleGatewayRPC adds durable deduplication to HTTP and WS writes. Supplying
// X-Request-Id is mandatory for writes while the gateway is enabled.
func (s *HTTPServer) handleGatewayRPC(w http.ResponseWriter, r *http.Request) {
	if s.gateway == nil {
		s.handleRPC(w, r)
		return
	}
	method := r.PathValue("method")
	if method == "reload_session" {
		writeJSONResponse(w, 403, wsError("", "forbidden", "restart gateway to change account"))
		return
	}
	// Read methods do not need durable receipts.
	if gatewayReadMethod(method) {
		s.handleRPC(w, r)
		return
	}
	id := strings.TrimSpace(r.Header.Get("X-Request-Id"))
	if id == "" || len(id) > 128 {
		writeJSONResponse(w, 400, wsError(id, "validation", "X-Request-Id required for writes (1..128 characters)"))
		return
	}
	account := s.account(r.Context())
	if account == "" {
		writeJSONResponse(w, 503, wsError(id, "not_ready", "Telegram not ready"))
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeJSONResponse(w, 415, wsError(id, "validation", "gateway writes require application/json"))
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxAPIBodySize+1))
	if err != nil || len(body) > maxAPIBodySize {
		writeJSONResponse(w, 413, wsError(id, "validation", "request body too large"))
		return
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil || !json.Valid(body) {
		writeJSONResponse(w, 400, wsError(id, "validation", "invalid JSON"))
		return
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		writeJSONResponse(w, 400, wsError(id, "validation", "invalid JSON value"))
		return
	}
	policyCtx := WithConfirmation(WithSurface(r.Context(), SurfaceHTTP), httpRequestConfirmed(r))
	if rpcErr := s.checkHTTPPolicy(policyCtx, method, body); rpcErr != nil {
		writeJSONResponse(w, errorToHTTPStatus(rpcErr), wsError(id, "forbidden", rpcErr.Message))
		return
	}
	fingerprint := []byte(fmt.Sprintf("%s\n%t\n%s\n%s", method, httpRequestConfirmed(r), r.URL.RawQuery, canonical))
	previous, fresh, err := s.gateway.Begin(r.Context(), account, id, fingerprint)
	if err != nil {
		kind := "request_conflict"
		if errors.Is(err, gateway.ErrUnknown) {
			kind = "unknown"
		}
		writeJSONResponse(w, 409, wsError(id, kind, err.Error()))
		return
	}
	if !fresh {
		var cached struct {
			Status int             `json:"status"`
			Body   json.RawMessage `json:"body"`
		}
		if json.Unmarshal(previous, &cached) != nil || cached.Status == 0 {
			writeJSONResponse(w, 503, wsError(id, "unknown", "invalid cached receipt"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(cached.Status)
		_, _ = w.Write(cached.Body)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	b := &rpcBuffer{header: make(http.Header)}
	s.handleRPC(b, r)
	status := b.status
	if status == 0 {
		status = 200
	}
	receipt, err := json.Marshal(map[string]any{"status": status, "body": json.RawMessage(b.body.Bytes())})
	if err != nil {
		writeJSONResponse(w, 503, wsError(id, "unknown", "cannot encode operation receipt"))
		return
	}
	if err = s.gateway.Complete(context.WithoutCancel(r.Context()), account, id, receipt); err != nil {
		writeJSONResponse(w, 503, wsError(id, "unknown", "operation may have completed; reconcile before retry"))
		return
	}
	for k, v := range b.header {
		w.Header()[k] = v
	}
	if b.status != 0 {
		w.WriteHeader(b.status)
	}
	_, _ = w.Write(b.body.Bytes())
}
