package rpcserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"greendrake.nz/go/rpcserver/rpcservertest"
)

func wsConnect(t *testing.T, server *httptest.Server, queryParams string) *websocket.Conn {
	t.Helper()
	url := server.URL
	if queryParams != "" {
		url += "?" + queryParams
	}
	return rpcservertest.Dial(t, url)
}

func wsRequest(t *testing.T, conn *websocket.Conn, req *Request) Response {
	t.Helper()
	sendRequest(t, conn, req)
	return readResponse(t, conn)
}

// readResponse reads a single WS message and unmarshals it as a Response.
// Unlike wsRequest, it does not send anything — it only consumes.
func readResponse(t *testing.T, conn *websocket.Conn) Response {
	t.Helper()
	var resp Response
	rpcservertest.Read(t, conn, &resp)
	return resp
}

// sendRequest writes a Request to the WS connection without reading a response.
func sendRequest(t *testing.T, conn *websocket.Conn, req *Request) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	msg, _ := json.Marshal(req)
	if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
		t.Fatalf("WS write failed: %v", err)
	}
}

// expectNoMessage asserts that no WS message arrives within the given duration.
func expectNoMessage(t *testing.T, conn *websocket.Conn, dur time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), dur)
	defer cancel()
	_, _, err := conn.Read(ctx)
	if err == nil {
		t.Fatal("expected no message, but received one")
	}
}

func TestWS_PublicMethod(t *testing.T) {
	d := NewDispatcher(testMethods())
	registry := NewConnRegistry()
	handler := NewWSHandler(d, registry)
	server := httptest.NewServer(handler)
	defer server.Close()

	conn := wsConnect(t, server, "")
	defer conn.Close(websocket.StatusNormalClosure, "")

	resp := wsRequest(t, conn, &Request{ID: "r1", Method: "app.ping"})
	if resp.Type != "response" {
		t.Fatalf("expected type=response, got %q", resp.Type)
	}
	if resp.ID != "r1" {
		t.Fatalf("expected id=r1, got %q", resp.ID)
	}
	if !resp.Success || resp.Data != "pong" {
		t.Fatalf("expected pong, got %+v", resp)
	}
}

func TestWS_EncodedRoundTrip(t *testing.T) {
	d := NewDispatcher(testMethods())
	registry := NewConnRegistry()
	cfg := Encoding{Codec: testCodec{}, RequireEncodedRequest: true, EncodeResponse: true}
	handler := NewWSHandler(d, registry, WithWSEncoding(cfg))
	server := httptest.NewServer(handler)
	defer server.Close()

	conn := wsConnect(t, server, "")
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Encode the request the way a client with the codec does.
	rawReq, _ := json.Marshal(&Request{ID: "r1", Method: "app.ping"})
	encodedReq := []byte(encodeBody(string(rawReq)))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, encodedReq); err != nil {
		t.Fatalf("WS write failed: %v", err)
	}

	_, respBytes, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("WS read failed: %v", err)
	}
	decoded := decodeBody(string(respBytes))
	var resp Response
	if err := json.Unmarshal([]byte(decoded), &resp); err != nil {
		t.Fatalf("decoded WS body should be valid JSON, got %q: %v", decoded, err)
	}
	if !resp.Success || resp.Data != "pong" || resp.ID != "r1" {
		t.Fatalf("expected pong/id=r1, got %+v (decoded=%q)", resp, decoded)
	}
}

// Lenient mode (RequireEncodedRequest off) accepts an encoded WS frame — the
// form is auto-detected, whatever the strict flag says.
func TestWS_LenientMode_EncodedBody(t *testing.T) {
	d := NewDispatcher(testMethods())
	registry := NewConnRegistry()
	handler := NewWSHandler(d, registry, WithWSEncoding(Encoding{Codec: testCodec{}, EncodeResponse: true}))
	server := httptest.NewServer(handler)
	defer server.Close()

	conn := wsConnect(t, server, "")
	defer conn.Close(websocket.StatusNormalClosure, "")

	rawReq, _ := json.Marshal(&Request{ID: "r1", Method: "app.ping"})
	encodedReq := []byte(encodeBody(string(rawReq)))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, encodedReq); err != nil {
		t.Fatalf("WS write failed: %v", err)
	}

	_, respBytes, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("WS read failed: %v", err)
	}
	decoded := decodeBody(string(respBytes))
	var resp Response
	if err := json.Unmarshal([]byte(decoded), &resp); err != nil {
		t.Fatalf("decoded WS body should be valid JSON, got %q: %v", decoded, err)
	}
	if !resp.Success || resp.Data != "pong" || resp.ID != "r1" {
		t.Fatalf("expected pong/id=r1, got %+v (decoded=%q)", resp, decoded)
	}
}

// Strict mode (RequireEncodedRequest on) rejects a plain WS frame as a
// generic malformed message — never signalling that encoding was required.
func TestWS_StrictMode_PlainBody_Rejected(t *testing.T) {
	d := NewDispatcher(testMethods())
	registry := NewConnRegistry()
	handler := NewWSHandler(d, registry, WithWSEncoding(Encoding{Codec: testCodec{}, RequireEncodedRequest: true}))
	server := httptest.NewServer(handler)
	defer server.Close()

	conn := wsConnect(t, server, "")
	defer conn.Close(websocket.StatusNormalClosure, "")

	resp := wsRequest(t, conn, &Request{ID: "r1", Method: "app.ping"})
	if resp.Success || resp.Error != CodeInvalidJSON {
		t.Fatalf("expected generic malformed error, got %+v", resp)
	}
}

// A PlainTransport principal (a static admin key) may send a plain frame
// even under the strict encoding requirement — the request-encoding gate is
// bypassed, symmetric with the response. wsRequest reads plain JSON back
// (PlainTransport opts the whole connection out of response-encoding too).
func TestWS_StrictMode_PlainTransportPrincipalAccepted(t *testing.T) {
	authResolver := newMockAuthResolver()
	authResolver.users["admin_key"] = &AuthInfo{UserID: "admin", PlainTransport: true}
	d := NewDispatcher(testMethods(), WithAuthResolver(authResolver))
	registry := NewConnRegistry()
	handler := NewWSHandler(d, registry, WithWSEncoding(Encoding{Codec: testCodec{}, RequireEncodedRequest: true, EncodeResponse: true}))
	server := httptest.NewServer(handler)
	defer server.Close()

	conn := wsConnect(t, server, "token=admin_key")
	defer conn.Close(websocket.StatusNormalClosure, "")

	resp := wsRequest(t, conn, &Request{ID: "r1", Method: "app.ping"})
	if !resp.Success || resp.Data != "pong" {
		t.Fatalf("plain frame from admin key should dispatch, got %+v", resp)
	}
}

func TestWS_AuthViaQueryParam(t *testing.T) {
	authResolver := newMockAuthResolver()
	authResolver.users["tok1"] = &AuthInfo{UserID: "user1"}
	d := NewDispatcher(testMethods(), WithAuthResolver(authResolver))
	registry := NewConnRegistry()
	handler := NewWSHandler(d, registry)
	server := httptest.NewServer(handler)
	defer server.Close()

	conn := wsConnect(t, server, "token=tok1")
	defer conn.Close(websocket.StatusNormalClosure, "")

	resp := wsRequest(t, conn, &Request{Method: "user.whoami"})
	if !resp.Success || resp.Data != "user1" {
		t.Fatalf("expected user1, got %+v", resp)
	}
}

// A PlainTransport principal (e.g. a static admin key) opts the whole
// connection out of response encoding: frames come back as plain JSON
// even with EncodeResponse on.
func TestWS_PlainTransportPrincipalBypassesEncoding(t *testing.T) {
	authResolver := newMockAuthResolver()
	authResolver.users["admin_key"] = &AuthInfo{UserID: "admin", PlainTransport: true}
	d := NewDispatcher(testMethods(), WithAuthResolver(authResolver))
	registry := NewConnRegistry()
	handler := NewWSHandler(d, registry, WithWSEncoding(Encoding{Codec: testCodec{}, EncodeResponse: true}))
	server := httptest.NewServer(handler)
	defer server.Close()

	conn := wsConnect(t, server, "token=admin_key")
	defer conn.Close(websocket.StatusNormalClosure, "")

	sendRequest(t, conn, &Request{ID: "r1", Method: "app.ping"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, respBytes, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("WS read failed: %v", err)
	}
	if !isLikelyJSON(respBytes) {
		t.Fatalf("plain-response principal should receive un-encoded JSON, got %q", string(respBytes))
	}
	var resp Response
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("response should be plain JSON, got %q: %v", string(respBytes), err)
	}
	if !resp.Success || resp.Data != "pong" || resp.ID != "r1" {
		t.Fatalf("expected pong/id=r1, got %+v", resp)
	}
}

func TestWS_AuthViaMessage(t *testing.T) {
	authResolver := newMockAuthResolver()
	authResolver.users["tok2"] = &AuthInfo{UserID: "user2"}
	d := NewDispatcher(testMethods(), WithAuthResolver(authResolver))
	registry := NewConnRegistry()
	handler := NewWSHandler(d, registry)
	server := httptest.NewServer(handler)
	defer server.Close()

	conn := wsConnect(t, server, "")
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Initially unauthenticated
	resp := wsRequest(t, conn, &Request{Method: "user.whoami"})
	if resp.Success || resp.Error != "AUTH_REQUIRED" {
		t.Fatalf("expected AUTH_REQUIRED before auth, got %+v", resp)
	}

	// Authenticate via auth.token
	resp = wsRequest(t, conn, &Request{ID: "a1", Method: "auth.token", Arguments: json.RawMessage(`["tok2"]`)})
	if !resp.Success {
		t.Fatalf("auth.token should succeed, got %+v", resp)
	}
	if resp.ID != "a1" {
		t.Fatalf("expected id=a1, got %q", resp.ID)
	}

	// Now authenticated
	resp = wsRequest(t, conn, &Request{Method: "user.whoami"})
	if !resp.Success || resp.Data != "user2" {
		t.Fatalf("expected user2 after auth, got %+v", resp)
	}
}

func TestWS_AuthTokenInvalid(t *testing.T) {
	authResolver := newMockAuthResolver()
	d := NewDispatcher(testMethods(), WithAuthResolver(authResolver))
	registry := NewConnRegistry()
	handler := NewWSHandler(d, registry)
	server := httptest.NewServer(handler)
	defer server.Close()

	conn := wsConnect(t, server, "")
	defer conn.Close(websocket.StatusNormalClosure, "")

	resp := wsRequest(t, conn, &Request{Method: "auth.token", Arguments: json.RawMessage(`["bad_token"]`)})
	if resp.Success || resp.Error != "UNAUTHENTICATED" {
		t.Fatalf("expected UNAUTHENTICATED, got %+v", resp)
	}
}

func TestWS_AuthLogout(t *testing.T) {
	authResolver := newMockAuthResolver()
	authResolver.users["tok_l"] = &AuthInfo{UserID: "userL"}
	d := NewDispatcher(testMethods(), WithAuthResolver(authResolver))
	registry := NewConnRegistry()
	handler := NewWSHandler(d, registry)
	server := httptest.NewServer(handler)
	defer server.Close()

	conn := wsConnect(t, server, "")
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Authenticate
	resp := wsRequest(t, conn, &Request{Method: "auth.token", Arguments: json.RawMessage(`["tok_l"]`)})
	if !resp.Success {
		t.Fatalf("auth.token should succeed: %+v", resp)
	}

	// Verify authenticated
	resp = wsRequest(t, conn, &Request{Method: "user.whoami"})
	if !resp.Success || resp.Data != "userL" {
		t.Fatalf("expected userL, got %+v", resp)
	}

	// Logout
	resp = wsRequest(t, conn, &Request{ID: "lo1", Method: "auth.logout"})
	if !resp.Success {
		t.Fatalf("auth.logout should succeed: %+v", resp)
	}
	if resp.ID != "lo1" {
		t.Fatalf("expected id=lo1, got %q", resp.ID)
	}

	// Verify unauthenticated
	resp = wsRequest(t, conn, &Request{Method: "user.whoami"})
	if resp.Success || resp.Error != "AUTH_REQUIRED" {
		t.Fatalf("expected AUTH_REQUIRED after logout, got %+v", resp)
	}

	// Verify unregistered from ConnRegistry (event should not be received)
	registry.Emit(context.Background(), "userL", "test.event", map[string]string{"for": "L"})
	ctxTimeout, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, _, err := conn.Read(ctxTimeout)
	if err == nil {
		t.Fatal("should not receive events after logout")
	}
}

func TestWS_EventPush(t *testing.T) {
	authResolver := newMockAuthResolver()
	authResolver.users["tok3"] = &AuthInfo{UserID: "user3"}
	d := NewDispatcher(testMethods(), WithAuthResolver(authResolver))
	registry := NewConnRegistry()
	handler := NewWSHandler(d, registry)
	server := httptest.NewServer(handler)
	defer server.Close()

	conn := wsConnect(t, server, "token=tok3")
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Consume initial — make a request to ensure connection is registered
	_ = wsRequest(t, conn, &Request{Method: "app.ping"})

	// Emit an event
	registry.Emit(context.Background(), "user3", "thing.changed", map[string]string{"id": "T1"})

	// Read the event
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, msgBytes, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("failed to read event: %v", err)
	}

	var evt eventFrame
	if err := json.Unmarshal(msgBytes, &evt); err != nil {
		t.Fatalf("failed to unmarshal event: %v", err)
	}
	if evt.Type != "event" || evt.Event != "thing.changed" {
		t.Fatalf("unexpected event: %+v", evt)
	}
	data, ok := evt.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", evt.Data)
	}
	if data["id"] != "T1" {
		t.Fatalf("expected id=T1, got %v", data["id"])
	}
}

func TestWS_MultipleRequestsBeforeResponses(t *testing.T) {
	d := NewDispatcher(testMethods())
	registry := NewConnRegistry()
	handler := NewWSHandler(d, registry)
	server := httptest.NewServer(handler)
	defer server.Close()

	conn := wsConnect(t, server, "")
	defer conn.Close(websocket.StatusNormalClosure, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Send two requests rapidly
	for _, id := range []string{"r1", "r2"} {
		msg, _ := json.Marshal(Request{ID: id, Method: "app.ping"})
		if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
			t.Fatalf("write %s failed: %v", id, err)
		}
	}

	// Responses may arrive in any order since dispatch is concurrent.
	ids := make(map[string]bool)
	for range 2 {
		_, respBytes, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read failed: %v", err)
		}
		var resp Response
		json.Unmarshal(respBytes, &resp)
		if !resp.Success {
			t.Fatalf("expected success: %+v", resp)
		}
		ids[resp.ID] = true
	}
	if !ids["r1"] || !ids["r2"] {
		t.Fatalf("expected both r1 and r2, got %v", ids)
	}
}

func TestWS_InvalidJSON(t *testing.T) {
	d := NewDispatcher(testMethods())
	registry := NewConnRegistry()
	handler := NewWSHandler(d, registry)
	server := httptest.NewServer(handler)
	defer server.Close()

	conn := wsConnect(t, server, "")
	defer conn.Close(websocket.StatusNormalClosure, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn.Write(ctx, websocket.MessageText, []byte("not json"))

	_, respBytes, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	var resp Response
	json.Unmarshal(respBytes, &resp)
	if resp.Success || resp.Error != CodeInvalidJSON {
		t.Fatalf("expected parse error, got %+v", resp)
	}
}

func TestWS_ReAuth(t *testing.T) {
	authResolver := newMockAuthResolver()
	authResolver.users["tok_a"] = &AuthInfo{UserID: "userA"}
	authResolver.users["tok_b"] = &AuthInfo{UserID: "userB"}
	d := NewDispatcher(testMethods(), WithAuthResolver(authResolver))
	registry := NewConnRegistry()
	handler := NewWSHandler(d, registry)
	server := httptest.NewServer(handler)
	defer server.Close()

	conn := wsConnect(t, server, "token=tok_a")
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Verify initial auth
	resp := wsRequest(t, conn, &Request{Method: "user.whoami"})
	if !resp.Success || resp.Data != "userA" {
		t.Fatalf("expected userA, got %+v", resp)
	}

	// Re-authenticate as userB
	resp = wsRequest(t, conn, &Request{Method: "auth.token", Arguments: json.RawMessage(`["tok_b"]`)})
	if !resp.Success {
		t.Fatalf("re-auth should succeed: %+v", resp)
	}

	// Verify new auth
	resp = wsRequest(t, conn, &Request{Method: "user.whoami"})
	if !resp.Success || resp.Data != "userB" {
		t.Fatalf("expected userB after re-auth, got %+v", resp)
	}
}

// --- Concurrent dispatch and abort tests ---

// TestWS_ConcurrentDispatch verifies that API method dispatch is concurrent:
// a fast request completes even while a slow request is still blocking.
// This is the fundamental property that enables abort to work — the read loop
// must stay responsive to process incoming messages while handlers are running.
func TestWS_ConcurrentDispatch(t *testing.T) {
	d := NewDispatcher(testMethods())
	registry := NewConnRegistry()
	handler := NewWSHandler(d, registry)
	server := httptest.NewServer(handler)
	defer server.Close()

	conn := wsConnect(t, server, "")
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Send a slow request (blocks until context cancelled), then a fast one.
	sendRequest(t, conn, &Request{ID: "slow1", Nonce: "n_slow", Method: "slow.op"})
	sendRequest(t, conn, &Request{ID: "fast1", Method: "app.ping"})

	// The fast request must complete promptly even though the slow one is
	// still in flight, proving concurrent dispatch.
	resp := readResponse(t, conn)
	if resp.ID != "fast1" || !resp.Success || resp.Data != "pong" {
		t.Fatalf("expected fast1/pong first, got %+v", resp)
	}

	// Abort the slow request to allow the test to exit cleanly.
	// No response expected for the abort or the aborted request.
	sendRequest(t, conn, &Request{Abort: "n_slow"})

	// Verify no further messages arrive (aborted request produces no response).
	expectNoMessage(t, conn, 300*time.Millisecond)
}

// TestWS_AbortInflightRequest verifies the abort flow:
//  1. Client sends a request that blocks in a handler.
//  2. Client sends an abort-only message referencing that request's nonce.
//  3. The handler's context is cancelled, causing it to exit.
//  4. No response is sent for the aborted request.
func TestWS_AbortInflightRequest(t *testing.T) {
	d := NewDispatcher(testMethods())
	registry := NewConnRegistry()
	handler := NewWSHandler(d, registry)
	server := httptest.NewServer(handler)
	defer server.Close()

	conn := wsConnect(t, server, "")
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Dispatch a slow request that blocks until its context is cancelled.
	sendRequest(t, conn, &Request{ID: "target", Nonce: "n1", Method: "slow.op"})

	// Give the goroutine a moment to start and register in the inflight map.
	time.Sleep(50 * time.Millisecond)

	// Abort it. Fire-and-forget — no response expected.
	sendRequest(t, conn, &Request{Abort: "n1"})

	// The aborted request should produce no response.
	expectNoMessage(t, conn, 300*time.Millisecond)
}

// TestWS_AbortMergedWithNewRequest verifies the debounce scenario: a single
// message both aborts a previous request and dispatches a new one. This is the
// primary use case — e.g. a search input where each keystroke cancels the
// previous query and starts a new one.
func TestWS_AbortMergedWithNewRequest(t *testing.T) {
	d := NewDispatcher(testMethods())
	registry := NewConnRegistry()
	handler := NewWSHandler(d, registry)
	server := httptest.NewServer(handler)
	defer server.Close()

	conn := wsConnect(t, server, "")
	defer conn.Close(websocket.StatusNormalClosure, "")

	// First search (slow — blocks until cancelled).
	sendRequest(t, conn, &Request{ID: "s1", Nonce: "n1", Method: "slow.op"})
	time.Sleep(50 * time.Millisecond)

	// Second search: abort the first and dispatch a new (fast) one in one message.
	sendRequest(t, conn, &Request{
		Abort:  "n1",
		ID:     "s2",
		Nonce:  "n2",
		Method: "app.ping",
	})

	// Only the second request's response should arrive.
	resp := readResponse(t, conn)
	if resp.ID != "s2" || !resp.Success || resp.Data != "pong" {
		t.Fatalf("expected s2/pong, got %+v", resp)
	}

	// No further messages (the aborted s1 is silent).
	expectNoMessage(t, conn, 300*time.Millisecond)
}

// TestWS_AbortUnknownNonce verifies that aborting a nonce that does not exist
// (never sent or already completed) is a harmless no-op. Abort is idempotent
// so clients don't need to track whether a request has already completed.
func TestWS_AbortUnknownNonce(t *testing.T) {
	d := NewDispatcher(testMethods())
	registry := NewConnRegistry()
	handler := NewWSHandler(d, registry)
	server := httptest.NewServer(handler)
	defer server.Close()

	conn := wsConnect(t, server, "")
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Abort a nonexistent nonce — should be silently ignored.
	sendRequest(t, conn, &Request{Abort: "nonexistent"})

	// Follow with a normal request to prove the connection is still healthy.
	resp := wsRequest(t, conn, &Request{ID: "r1", Method: "app.ping"})
	if !resp.Success || resp.Data != "pong" {
		t.Fatalf("expected pong, got %+v", resp)
	}
}

// TestWS_AbortCompletedRequest verifies that aborting a request that has
// already completed is idempotent.
func TestWS_AbortCompletedRequest(t *testing.T) {
	d := NewDispatcher(testMethods())
	registry := NewConnRegistry()
	handler := NewWSHandler(d, registry)
	server := httptest.NewServer(handler)
	defer server.Close()

	conn := wsConnect(t, server, "")
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Send a fast request and wait for it to complete.
	resp := wsRequest(t, conn, &Request{ID: "done1", Nonce: "n_done", Method: "app.ping"})
	if !resp.Success {
		t.Fatalf("ping should succeed: %+v", resp)
	}

	// Abort it after completion — no-op, no response.
	sendRequest(t, conn, &Request{Abort: "n_done"})

	// Connection still works.
	resp = wsRequest(t, conn, &Request{ID: "r2", Method: "app.ping"})
	if !resp.Success || resp.Data != "pong" {
		t.Fatalf("expected pong, got %+v", resp)
	}
}

// TestWS_RequestWithoutNonce_NotAbortable verifies that a request sent without
// a nonce executes normally — it just cannot be aborted because there is no
// identifier for a subsequent message to reference.
func TestWS_RequestWithoutNonce_NotAbortable(t *testing.T) {
	d := NewDispatcher(testMethods())
	registry := NewConnRegistry()
	handler := NewWSHandler(d, registry)
	server := httptest.NewServer(handler)
	defer server.Close()

	conn := wsConnect(t, server, "")
	defer conn.Close(websocket.StatusNormalClosure, "")

	resp := wsRequest(t, conn, &Request{Method: "app.ping"})
	if !resp.Success || resp.Data != "pong" {
		t.Fatalf("expected pong, got %+v", resp)
	}
}

// TestWS_MultipleAbortsSameNonce verifies that sending multiple abort messages
// for the same nonce is harmless. The first abort cancels the request;
// subsequent ones are no-ops. We verify this by sending a ping after the
// double abort and checking that the connection is still functional.
// (We cannot use expectNoMessage mid-test because the WebSocket library closes
// the connection when a read context times out.)
func TestWS_MultipleAbortsSameNonce(t *testing.T) {
	d := NewDispatcher(testMethods())
	registry := NewConnRegistry()
	handler := NewWSHandler(d, registry)
	server := httptest.NewServer(handler)
	defer server.Close()

	conn := wsConnect(t, server, "")
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Dispatch a slow request.
	sendRequest(t, conn, &Request{ID: "dup_target", Nonce: "n_dup", Method: "slow.op"})
	time.Sleep(50 * time.Millisecond)

	// First abort: cancels the in-flight request. Merged with a ping so we
	// can read a response to prove the connection survived.
	sendRequest(t, conn, &Request{Abort: "n_dup", ID: "ping1", Method: "app.ping"})
	resp := readResponse(t, conn)
	if resp.ID != "ping1" || !resp.Success {
		t.Fatalf("expected ping1/success, got %+v", resp)
	}

	// Second abort of the same nonce: should be a no-op. Again merged with a
	// ping to verify the connection is still healthy.
	sendRequest(t, conn, &Request{Abort: "n_dup", ID: "ping2", Method: "app.ping"})
	resp = readResponse(t, conn)
	if resp.ID != "ping2" || !resp.Success {
		t.Fatalf("expected ping2/success after double abort, got %+v", resp)
	}
}

// TestWS_AbortOnlyMessage verifies that an abort-only message (no method)
// produces no response at all — it is pure fire-and-forget.
func TestWS_AbortOnlyMessage(t *testing.T) {
	d := NewDispatcher(testMethods())
	registry := NewConnRegistry()
	handler := NewWSHandler(d, registry)
	server := httptest.NewServer(handler)
	defer server.Close()

	conn := wsConnect(t, server, "")
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Dispatch a slow request, then abort it with an abort-only message.
	sendRequest(t, conn, &Request{Nonce: "n_ao", Method: "slow.op"})
	time.Sleep(50 * time.Millisecond)
	sendRequest(t, conn, &Request{Abort: "n_ao"})

	// No response for the abort-only message or the cancelled request.
	expectNoMessage(t, conn, 300*time.Millisecond)
}

func TestWS_EventToCorrectUser(t *testing.T) {
	authResolver := newMockAuthResolver()
	authResolver.users["tok_x"] = &AuthInfo{UserID: "userX"}
	authResolver.users["tok_y"] = &AuthInfo{UserID: "userY"}
	d := NewDispatcher(testMethods(), WithAuthResolver(authResolver))
	registry := NewConnRegistry()
	handler := NewWSHandler(d, registry)
	server := httptest.NewServer(handler)
	defer server.Close()

	connX := wsConnect(t, server, "token=tok_x")
	defer connX.Close(websocket.StatusNormalClosure, "")
	connY := wsConnect(t, server, "token=tok_y")
	defer connY.Close(websocket.StatusNormalClosure, "")

	// Ensure both connections are established
	_ = wsRequest(t, connX, &Request{Method: "app.ping"})
	_ = wsRequest(t, connY, &Request{Method: "app.ping"})

	// Emit event to userX only
	registry.Emit(context.Background(), "userX", "test.event", map[string]string{"for": "X"})

	// userX should receive it
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, msgBytes, err := connX.Read(ctx)
	if err != nil {
		t.Fatalf("userX should receive event: %v", err)
	}
	var evt eventFrame
	json.Unmarshal(msgBytes, &evt)
	if evt.Event != "test.event" {
		t.Fatalf("unexpected event for userX: %+v", evt)
	}

	// userY should NOT receive it (read should timeout)
	ctxY, cancelY := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancelY()
	_, _, err = connY.Read(ctxY)
	if err == nil {
		t.Fatal("userY should not receive event meant for userX")
	}
}

func TestWS_OpenCloseHooks(t *testing.T) {
	var opens, closes atomic.Int32
	closed := make(chan struct{}, 2)
	d := NewDispatcher(testMethods())
	handler := NewWSHandler(d, NewConnRegistry(),
		WithWSOnOpen(func(*WSConn) { opens.Add(1) }),
		WithWSOnClose(func(*WSConn) {
			closes.Add(1)
			closed <- struct{}{}
		}),
	)
	server := httptest.NewServer(handler)
	defer server.Close()

	conn := wsConnect(t, server, "")
	if resp := wsRequest(t, conn, &Request{Method: "app.ping"}); !resp.Success {
		t.Fatalf("ping failed: %+v", resp)
	}
	if got := opens.Load(); got != 1 {
		t.Fatalf("expected open hook to fire once, got %d", got)
	}
	if got := closes.Load(); got != 0 {
		t.Fatalf("close hook fired while connection was open: %d", got)
	}

	conn.Close(websocket.StatusNormalClosure, "")

	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("close hook never fired")
	}
	if got := closes.Load(); got != 1 {
		t.Fatalf("expected close hook to fire once, got %d", got)
	}
	if got := opens.Load(); got != 1 {
		t.Fatalf("expected open hook to fire once, got %d", got)
	}
}

func TestWSConn_SendGivesUpOnAFullBuffer(t *testing.T) {
	var cancels int
	logger, buf := logged()
	ws := newWSConn(nil, func() { cancels++ }, logger)
	ws.send = make(chan []byte, 1)

	ws.Send([]byte("first"))
	if cancels != 0 || ws.overflowed.Load() {
		t.Fatal("a send with room in the buffer gave up on the socket")
	}
	ws.Send([]byte("second"))
	ws.Send([]byte("third"))
	if cancels != 1 || !ws.overflowed.Load() {
		t.Fatalf("a full buffer: cancels %d, overflowed %v; want the socket cancelled once", cancels, ws.overflowed.Load())
	}
	if got := string(<-ws.send); got != "first" || len(ws.send) != 0 {
		t.Fatalf("the buffer holds %q and %d more; want only what fitted", got, len(ws.send))
	}
	if strings.Count(buf.String(), "send buffer full") != 1 {
		t.Fatalf("giving up is logged once: %s", buf)
	}
}

// A client that stops reading: the server's writes back up behind it, its send buffer
// fills, and the socket is torn down rather than left to drop what it is sent.
func TestWS_AClientThatStopsReadingIsDisconnected(t *testing.T) {
	closed := make(chan struct{})
	// Large enough that a few of them fill the connection's socket buffers, so the
	// write loop stops draining the send buffer.
	message := make([]byte, 1<<20)
	handler := NewWSHandler(NewDispatcher(testMethods()), NewConnRegistry(),
		WithWSOnOpen(func(ws *WSConn) {
			for range 200 {
				ws.Send(message)
			}
		}),
		WithWSOnClose(func(*WSConn) { close(closed) }),
	)
	server := httptest.NewServer(handler)
	defer server.Close()

	conn := wsConnect(t, server, "")
	defer conn.CloseNow()

	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("a socket whose client stopped reading was kept")
	}
}

func TestWS_ReadLimit(t *testing.T) {
	// Twice the WebSocket library's default limit.
	request, _ := json.Marshal(Request{ID: "r1", Method: "app.ping", Arguments: json.RawMessage(`["` + strings.Repeat("x", 64<<10) + `"]`)})

	t.Run("a message over the default closes the socket", func(t *testing.T) {
		server := httptest.NewServer(NewWSHandler(NewDispatcher(testMethods()), NewConnRegistry()))
		defer server.Close()
		conn := wsConnect(t, server, "")
		defer conn.CloseNow()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := conn.Write(ctx, websocket.MessageText, request); err != nil {
			t.Fatalf("write: %v", err)
		}
		if _, _, err := conn.Read(ctx); websocket.CloseStatus(err) != websocket.StatusMessageTooBig {
			t.Fatalf("read: got %v, want the socket closed as too big", err)
		}
	})

	t.Run("a raised limit takes it", func(t *testing.T) {
		server := httptest.NewServer(NewWSHandler(NewDispatcher(testMethods()), NewConnRegistry(), WithWSReadLimit(1<<20)))
		defer server.Close()
		conn := wsConnect(t, server, "")
		defer conn.CloseNow()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := conn.Write(ctx, websocket.MessageText, request); err != nil {
			t.Fatalf("write: %v", err)
		}
		if resp := readResponse(t, conn); !resp.Success || resp.Data != "pong" {
			t.Fatalf("response: %+v", resp)
		}
	})
}

// A browser's socket is admitted from the endpoint's own origin, and from
// another only where a pattern admits it; a client sending no Origin is not a
// browser, and is not checked.
func TestWS_Origin(t *testing.T) {
	dial := func(server *httptest.Server, origin string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		header := http.Header{}
		if origin != "" {
			header.Set("Origin", origin)
		}
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &websocket.DialOptions{HTTPHeader: header})
		if err == nil {
			conn.Close(websocket.StatusNormalClosure, "")
		}
		return err
	}
	logger, buf := logged()
	d := NewDispatcher(testMethods(), WithLogger(logger))

	sameOrigin := httptest.NewServer(NewWSHandler(d, NewConnRegistry()))
	defer sameOrigin.Close()
	for origin, admitted := range map[string]bool{
		"":                      true,
		sameOrigin.URL:          true,
		"https://evil.example":  false,
		"app://localhost":       false,
		"http://localhost:5173": false,
	} {
		if err := dial(sameOrigin, origin); (err == nil) != admitted {
			t.Errorf("same origin only, %q: admitted %v, got %v", origin, admitted, err)
		}
	}
	if !strings.Contains(buf.String(), "evil.example") {
		t.Fatalf("a refused origin is not logged: %s", buf)
	}

	listed := httptest.NewServer(NewWSHandler(d, NewConnRegistry(),
		WithWSOriginPatterns("https://app.example", "app://localhost", "localhost:5173")))
	defer listed.Close()
	for origin, admitted := range map[string]bool{
		listed.URL:              true,
		"https://app.example":   true,
		"http://app.example":    false,
		"app://localhost":       true,
		"http://localhost:5173": true,
		"https://evil.example":  false,
	} {
		if err := dial(listed, origin); (err == nil) != admitted {
			t.Errorf("listed, %q: admitted %v, got %v", origin, admitted, err)
		}
	}
}

func TestWS_AuthTokenArguments(t *testing.T) {
	authResolver := newMockAuthResolver()
	authResolver.users["tok"] = &AuthInfo{UserID: "u1"}
	server := httptest.NewServer(NewWSHandler(NewDispatcher(testMethods(), WithAuthResolver(authResolver)), NewConnRegistry()))
	defer server.Close()
	conn := wsConnect(t, server, "")
	defer conn.Close(websocket.StatusNormalClosure, "")

	for _, arguments := range []string{``, `[]`, `["tok","tok"]`, `{"token":"tok"}`, `[7]`} {
		resp := wsRequest(t, conn, &Request{ID: "a", Method: MethodAuthToken, Arguments: json.RawMessage(arguments)})
		if resp.Success || resp.Error != CodeInvalidArgument || resp.ID != "a" {
			t.Fatalf("%s: expected INVALID_ARGUMENT, got %+v", arguments, resp)
		}
	}
}

func TestWS_AuthTokenWithoutResolver(t *testing.T) {
	server := httptest.NewServer(NewWSHandler(NewDispatcher(testMethods()), NewConnRegistry()))
	defer server.Close()
	conn := wsConnect(t, server, TokenParam+"=tok")
	defer conn.Close(websocket.StatusNormalClosure, "")

	resp := wsRequest(t, conn, &Request{Method: MethodAuthToken, Arguments: json.RawMessage(`["tok"]`)})
	if resp.Success || resp.Error != CodeAuthNotConfigured {
		t.Fatalf("expected AUTH_NOT_CONFIGURED, got %+v", resp)
	}
}

// A socket opened with a token that names nobody stays anonymous.
func TestWS_QueryTokenUnresolved(t *testing.T) {
	server := httptest.NewServer(NewWSHandler(NewDispatcher(testMethods(), WithAuthResolver(newMockAuthResolver())), NewConnRegistry()))
	defer server.Close()
	conn := wsConnect(t, server, TokenParam+"=nobody")
	defer conn.Close(websocket.StatusNormalClosure, "")

	if resp := wsRequest(t, conn, &Request{Method: "user.whoami"}); resp.Error != CodeAuthRequired {
		t.Fatalf("expected AUTH_REQUIRED, got %+v", resp)
	}
}

// A socket's calls see the client address the dispatcher derives.
func TestWS_ClientIP(t *testing.T) {
	methods := map[string]MethodDef{"ip": {Handler: func(hc *HandlerContext, _ json.RawMessage) (any, error) { return hc.ClientIP, nil }}}
	server := httptest.NewServer(NewWSHandler(NewDispatcher(methods), NewConnRegistry()))
	defer server.Close()
	conn := wsConnect(t, server, "")
	defer conn.Close(websocket.StatusNormalClosure, "")

	if resp := wsRequest(t, conn, &Request{Method: "ip"}); resp.Data != "127.0.0.1" {
		t.Fatalf("expected the peer's address, got %+v", resp)
	}
}

func TestWS_SendDataFrame(t *testing.T) {
	d := NewDispatcher(testMethods())
	handler := NewWSHandler(d, NewConnRegistry(),
		WithWSOnOpen(func(ws *WSConn) {
			ws.sendData("status", map[string]any{"mode": 3})
		}),
	)
	server := httptest.NewServer(handler)
	defer server.Close()

	conn := wsConnect(t, server, "")
	defer conn.Close(websocket.StatusNormalClosure, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, msgBytes, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("failed to read data frame: %v", err)
	}

	// The frame must be bare {type,data}: an "event" key would route it down the
	// consumer's event path instead of its data path.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(msgBytes, &raw); err != nil {
		t.Fatalf("failed to unmarshal data frame: %v", err)
	}
	if len(raw) != 2 {
		t.Fatalf("expected exactly type and data keys, got %v", raw)
	}

	var frame dataFrame
	if err := json.Unmarshal(msgBytes, &frame); err != nil {
		t.Fatalf("failed to unmarshal data frame: %v", err)
	}
	if frame.Type != "status" {
		t.Fatalf("expected type=status, got %q", frame.Type)
	}
	data, ok := frame.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", frame.Data)
	}
	if data["mode"] != float64(3) {
		t.Fatalf("expected mode=3, got %v", data["mode"])
	}
}
