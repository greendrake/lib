package rpcserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
)

func TestHTTPHandler_PublicMethod(t *testing.T) {
	d := NewDispatcher(testMethods())
	handler := NewHTTPHandler(d)

	body, _ := json.Marshal(Request{Method: "app.ping"})
	req := httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Success || resp.Data != "pong" {
		t.Fatalf("expected pong, got %+v", resp)
	}
	if resp.Type != "" {
		t.Fatalf("HTTP response should not have Type field, got %q", resp.Type)
	}
}

func TestHTTPHandler_AuthRequired(t *testing.T) {
	authResolver := newMockAuthResolver()
	authResolver.users["valid_token"] = &AuthInfo{UserID: "user1"}
	d := NewDispatcher(testMethods(), WithAuthResolver(authResolver))
	handler := NewHTTPHandler(d)

	body, _ := json.Marshal(Request{Method: "user.whoami"})
	req := httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer valid_token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Success || resp.Data != "user1" {
		t.Fatalf("expected user1, got %+v", resp)
	}
}

// A credential is a Bearer one: a header without the scheme, or with another,
// carries none.
func TestHTTPHandler_NonBearerAuthorizationRejected(t *testing.T) {
	authResolver := newMockAuthResolver()
	authResolver.users["raw_key"] = &AuthInfo{UserID: "admin"}
	d := NewDispatcher(testMethods(), WithAuthResolver(authResolver))
	handler := NewHTTPHandler(d)

	for _, header := range []string{"raw_key", "Basic raw_key"} {
		body, _ := json.Marshal(Request{Method: "user.whoami"})
		req := httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader(body))
		req.Header.Set("Authorization", header)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		var resp Response
		json.NewDecoder(w.Body).Decode(&resp)
		if resp.Success || resp.Error != CodeAuthRequired {
			t.Fatalf("%q: expected AUTH_REQUIRED, got %+v", header, resp)
		}
	}
}

func TestHTTPHandler_BodyLimit(t *testing.T) {
	d := NewDispatcher(testMethods())
	post := func(handler http.Handler, body []byte) Response {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader(body)))
		var resp Response
		json.NewDecoder(w.Body).Decode(&resp)
		return resp
	}
	small, _ := json.Marshal(Request{Method: "echo.args", Arguments: json.RawMessage(`["` + strings.Repeat("x", 100) + `"]`)})
	if resp := post(NewHTTPHandler(d, WithHTTPBodyLimit(int64(len(small)))), small); !resp.Success {
		t.Fatalf("a body at the limit: %+v", resp)
	}
	if resp := post(NewHTTPHandler(d, WithHTTPBodyLimit(int64(len(small)-1))), small); resp.Error != CodeRequestTooLarge {
		t.Fatalf("a body over the limit: %+v", resp)
	}
	big, _ := json.Marshal(Request{Method: "echo.args", Arguments: json.RawMessage(`["` + strings.Repeat("x", DefaultHTTPBodyLimit) + `"]`)})
	if resp := post(NewHTTPHandler(d), big); resp.Error != CodeRequestTooLarge {
		t.Fatalf("a body over the default limit: %+v", resp)
	}
}

// A body that breaks off mid-read is no request.
func TestHTTPHandler_BodyReadFailure(t *testing.T) {
	handler := NewHTTPHandler(NewDispatcher(testMethods()))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api", iotest.ErrReader(errors.New("reset"))))
	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Success || resp.Error != CodeInvalidJSON {
		t.Fatalf("got %+v", resp)
	}
}

// A handler sees the socket peer's address, unless the host derives it
// otherwise — from a proxy it trusts.
func TestHTTPHandler_ClientIP(t *testing.T) {
	methods := map[string]MethodDef{"ip": {Handler: func(hc *HandlerContext, _ json.RawMessage) (any, error) { return hc.ClientIP, nil }}}
	call := func(d *Dispatcher) any {
		body, _ := json.Marshal(Request{Method: "ip"})
		req := httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader(body))
		req.RemoteAddr = "192.0.2.7:4321"
		req.Header.Set("X-Forwarded-For", "203.0.113.9")
		w := httptest.NewRecorder()
		NewHTTPHandler(d).ServeHTTP(w, req)
		var resp Response
		json.NewDecoder(w.Body).Decode(&resp)
		return resp.Data
	}
	if got := call(NewDispatcher(methods)); got != "192.0.2.7" {
		t.Fatalf("the peer: %v", got)
	}
	forwarded := WithClientIP(func(r *http.Request) string { return r.Header.Get("X-Forwarded-For") })
	if got := call(NewDispatcher(methods, forwarded)); got != "203.0.113.9" {
		t.Fatalf("as the host derives it: %v", got)
	}
}

func TestPeerIP(t *testing.T) {
	for addr, want := range map[string]string{"192.0.2.7:4321": "192.0.2.7", "[2001:db8::1]:80": "2001:db8::1", "@": "@"} {
		if got := PeerIP(&http.Request{RemoteAddr: addr}); got != want {
			t.Errorf("%s: %s, want %s", addr, got, want)
		}
	}
}

func TestHTTPHandler_AuthRequired_NoToken(t *testing.T) {
	authResolver := newMockAuthResolver()
	d := NewDispatcher(testMethods(), WithAuthResolver(authResolver))
	handler := NewHTTPHandler(d)

	body, _ := json.Marshal(Request{Method: "user.whoami"})
	req := httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Success || resp.Error != "AUTH_REQUIRED" {
		t.Fatalf("expected AUTH_REQUIRED, got %+v", resp)
	}
}

func TestHTTPHandler_InvalidJSON(t *testing.T) {
	d := NewDispatcher(testMethods())
	handler := NewHTTPHandler(d)

	req := httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Success || resp.Error != CodeInvalidJSON {
		t.Fatalf("expected parse error, got %+v", resp)
	}
}

func TestHTTPHandler_MethodNotAllowed(t *testing.T) {
	d := NewDispatcher(testMethods())
	handler := NewHTTPHandler(d)

	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestHTTPHandler_NonceDedup(t *testing.T) {
	ns := newMockNonceStore()
	d := NewDispatcher(testMethods(), WithNonceStore(ns))
	handler := NewHTTPHandler(d)

	body, _ := json.Marshal(Request{Method: "app.ping", Nonce: "n1"})

	req1 := httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader(body))
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)

	var resp1 Response
	json.NewDecoder(w1.Body).Decode(&resp1)
	if !resp1.Success {
		t.Fatalf("first request should succeed: %+v", resp1)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	var resp2 Response
	json.NewDecoder(w2.Body).Decode(&resp2)
	if resp2.Success || resp2.Error != "DUPLICATE_REQUEST" {
		t.Fatalf("duplicate nonce should fail: %+v", resp2)
	}
}

func TestHTTPHandler_IDEchoed(t *testing.T) {
	d := NewDispatcher(testMethods())
	handler := NewHTTPHandler(d)

	body, _ := json.Marshal(Request{ID: "req_99", Method: "app.ping"})
	req := httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.ID != "req_99" {
		t.Fatalf("expected ID req_99, got %s", resp.ID)
	}
}

func TestHTTPHandler_PositionalArgs(t *testing.T) {
	d := NewDispatcher(testMethods())
	handler := NewHTTPHandler(d)

	body, _ := json.Marshal(Request{Method: "echo.args", Arguments: json.RawMessage(`["hello world"]`)})
	req := httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Success || resp.Data != "hello world" {
		t.Fatalf("expected hello world, got %+v", resp)
	}
}

func TestHTTPHandler_DecodeRequest(t *testing.T) {
	d := NewDispatcher(testMethods())
	handler := NewHTTPHandler(d, WithHTTPEncoding(Encoding{Codec: testCodec{}, RequireEncodedRequest: true}))

	rawBody, _ := json.Marshal(Request{Method: "app.ping"})
	encoded := encodeBody(string(rawBody))

	req := httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader([]byte(encoded)))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response body should be plain JSON when EncodeResponse is unset: %v", err)
	}
	if !resp.Success || resp.Data != "pong" {
		t.Fatalf("expected pong, got %+v", resp)
	}
}

func TestHTTPHandler_EncodeResponse(t *testing.T) {
	d := NewDispatcher(testMethods())
	handler := NewHTTPHandler(d, WithHTTPEncoding(Encoding{Codec: testCodec{}, EncodeResponse: true}))

	body, _ := json.Marshal(Request{Method: "app.ping"})
	req := httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if ct := w.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Fatalf("encoded response should advertise text/plain, got %q", ct)
	}
	decoded := decodeBody(w.Body.String())
	var resp Response
	if err := json.Unmarshal([]byte(decoded), &resp); err != nil {
		t.Fatalf("decoded body should be valid JSON, got %q: %v", decoded, err)
	}
	if !resp.Success || resp.Data != "pong" {
		t.Fatalf("expected pong, got %+v (decoded body=%q)", resp, decoded)
	}
}

// A principal the resolver flags PlainTransport opts out of response encoding
// even with EncodeResponse on: it gets plain JSON + application/json. Models a
// static admin key (non-browser callers that don't run the client's decoder).
func TestHTTPHandler_PlainTransportPrincipalBypassesEncoding(t *testing.T) {
	authResolver := newMockAuthResolver()
	authResolver.users["admin_key"] = &AuthInfo{UserID: "admin", PlainTransport: true}
	d := NewDispatcher(testMethods(), WithAuthResolver(authResolver))
	handler := NewHTTPHandler(d, WithHTTPEncoding(Encoding{Codec: testCodec{}, EncodeResponse: true}))

	body, _ := json.Marshal(Request{Method: "app.ping"})
	req := httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer admin_key")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("plain-response principal should get application/json, got %q", ct)
	}
	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body should be plain JSON for a plain-response principal, got %q: %v", w.Body.String(), err)
	}
	if !resp.Success || resp.Data != "pong" {
		t.Fatalf("expected pong, got %+v", resp)
	}
}

// An ordinary principal (PlainTransport unset) still gets the encoded response
// when EncodeResponse is on — the bypass is strictly opt-in.
func TestHTTPHandler_OrdinaryPrincipalStillEncoded(t *testing.T) {
	authResolver := newMockAuthResolver()
	authResolver.users["user_token"] = &AuthInfo{UserID: "user1"}
	d := NewDispatcher(testMethods(), WithAuthResolver(authResolver))
	handler := NewHTTPHandler(d, WithHTTPEncoding(Encoding{Codec: testCodec{}, EncodeResponse: true}))

	body, _ := json.Marshal(Request{Method: "app.ping"})
	req := httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer user_token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if ct := w.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Fatalf("ordinary principal should still get an encoded response, got %q", ct)
	}
	decoded := decodeBody(w.Body.String())
	var resp Response
	if err := json.Unmarshal([]byte(decoded), &resp); err != nil {
		t.Fatalf("decoded body should be valid JSON, got %q: %v", decoded, err)
	}
	if !resp.Success || resp.Data != "pong" {
		t.Fatalf("expected pong, got %+v", resp)
	}
}

func TestHTTPHandler_PlainResponseContentType(t *testing.T) {
	d := NewDispatcher(testMethods())
	handler := NewHTTPHandler(d)

	body, _ := json.Marshal(Request{Method: "app.ping"})
	req := httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("plain response should advertise application/json, got %q", ct)
	}
}

// Default (no option) leaves bodies untouched — the safe mode for unit tests
// that exercise the handler with httptest+plain JSON.
func TestHTTPHandler_EncodingDefaultIsOff(t *testing.T) {
	d := NewDispatcher(testMethods())
	handler := NewHTTPHandler(d)

	body, _ := json.Marshal(Request{Method: "app.ping"})
	req := httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("default handler should accept plain JSON and return plain JSON: %v", err)
	}
	if !resp.Success || resp.Data != "pong" {
		t.Fatalf("expected pong, got %+v", resp)
	}
}

// A plain JSON body for an AllowPlain-flagged method is accepted even when the
// encoding requirement is on; the response still follows EncodeResponse — the
// client decodes uniformly whatever is not plain JSON, so the bootstrap caller
// is no special case.
func TestHTTPHandler_PlainBody_AllowedMethod(t *testing.T) {
	d := NewDispatcher(testMethods())
	handler := NewHTTPHandler(d, WithHTTPEncoding(Encoding{Codec: testCodec{}, RequireEncodedRequest: true, EncodeResponse: true}))

	body, _ := json.Marshal(Request{Method: "plain.allowed"})
	req := httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	decoded := decodeBody(w.Body.String())
	var resp Response
	if err := json.Unmarshal([]byte(decoded), &resp); err != nil {
		t.Fatalf("response should be encoded when EncodeResponse is on: %v", err)
	}
	if !resp.Success || resp.Data != "plain_ok" {
		t.Fatalf("expected plain_ok, got %+v", resp)
	}
}

// A plain body for a method without AllowPlain must be rejected when
// the encoding requirement is on — otherwise any client could
// bypass it just by sending plain JSON. The rejection is the same generic
// error a malformed body yields, so it never reveals that encoding was the
// missing ingredient.
func TestHTTPHandler_PlainBody_DisallowedMethod(t *testing.T) {
	d := NewDispatcher(testMethods())
	handler := NewHTTPHandler(d, WithHTTPEncoding(Encoding{Codec: testCodec{}, RequireEncodedRequest: true, EncodeResponse: true}))

	body, _ := json.Marshal(Request{Method: "app.ping"})
	req := httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	decoded := decodeBody(w.Body.String())
	var resp Response
	if err := json.Unmarshal([]byte(decoded), &resp); err != nil {
		t.Fatalf("rejection should still honour EncodeResponse: %v", err)
	}
	if resp.Success || resp.Error != CodeInvalidJSON {
		t.Fatalf("expected generic malformed error, got %+v", resp)
	}
}

// A PlainTransport principal (a static admin key) may send a plain JSON body
// even for a method without AllowPlain while the encoding requirement is on —
// the request-encoding gate is bypassed symmetrically with the response.
// The reply comes back plain too (PlainTransport opts out in both directions).
func TestHTTPHandler_PlainTransportPrincipalBypassesRequireEncoded(t *testing.T) {
	authResolver := newMockAuthResolver()
	authResolver.users["admin_key"] = &AuthInfo{UserID: "admin", PlainTransport: true}
	d := NewDispatcher(testMethods(), WithAuthResolver(authResolver))
	handler := NewHTTPHandler(d, WithHTTPEncoding(Encoding{Codec: testCodec{}, RequireEncodedRequest: true, EncodeResponse: true}))

	body, _ := json.Marshal(Request{Method: "app.ping"})
	req := httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer admin_key")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("plain-transport principal should get application/json, got %q", ct)
	}
	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("plain-transport principal should get plain JSON, got %q: %v", w.Body.String(), err)
	}
	if !resp.Success || resp.Data != "pong" {
		t.Fatalf("plain body from admin key should dispatch, got %+v", resp)
	}
}

// An ordinary authenticated principal stays subject to the request-encoding
// gate: a plain body for a non-AllowPlain method is rejected even though the
// caller is authenticated. The PlainTransport bypass is the only exemption
// beyond per-method AllowPlain.
func TestHTTPHandler_OrdinaryPrincipalStillRequiresEncoded(t *testing.T) {
	authResolver := newMockAuthResolver()
	authResolver.users["user_token"] = &AuthInfo{UserID: "user1"}
	d := NewDispatcher(testMethods(), WithAuthResolver(authResolver))
	handler := NewHTTPHandler(d, WithHTTPEncoding(Encoding{Codec: testCodec{}, RequireEncodedRequest: true, EncodeResponse: true}))

	body, _ := json.Marshal(Request{Method: "app.ping"})
	req := httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer user_token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	decoded := decodeBody(w.Body.String())
	var resp Response
	if err := json.Unmarshal([]byte(decoded), &resp); err != nil {
		t.Fatalf("ordinary principal rejection should still honour EncodeResponse: %v", err)
	}
	if resp.Success || resp.Error != CodeInvalidJSON {
		t.Fatalf("expected generic malformed error for ordinary principal, got %+v", resp)
	}
}

// When the encoding requirement is on and the body is encoded,
// an AllowPlain method behaves like any other — decoded, dispatched,
// response encoded. AllowPlain is a permission to accept plain, not a
// mandate to be plain.
func TestHTTPHandler_EncodedBody_AllowPlainMethod(t *testing.T) {
	d := NewDispatcher(testMethods())
	handler := NewHTTPHandler(d, WithHTTPEncoding(Encoding{Codec: testCodec{}, RequireEncodedRequest: true, EncodeResponse: true}))

	rawBody, _ := json.Marshal(Request{Method: "plain.allowed"})
	encoded := encodeBody(string(rawBody))
	req := httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader([]byte(encoded)))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	decoded := decodeBody(w.Body.String())
	var resp Response
	if err := json.Unmarshal([]byte(decoded), &resp); err != nil {
		t.Fatalf("encoded-body response should be encoded: %v", err)
	}
	if !resp.Success || resp.Data != "plain_ok" {
		t.Fatalf("expected plain_ok, got %+v", resp)
	}
}

// Lenient mode (RequireEncodedRequest off) accepts an encoded body — the form
// is auto-detected, not gated on the strict flag: a lenient request with an
// encoded response.
func TestHTTPHandler_LenientMode_EncodedBody(t *testing.T) {
	d := NewDispatcher(testMethods())
	handler := NewHTTPHandler(d, WithHTTPEncoding(Encoding{Codec: testCodec{}, EncodeResponse: true}))

	rawBody, _ := json.Marshal(Request{Method: "app.ping"})
	encoded := encodeBody(string(rawBody))
	req := httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader([]byte(encoded)))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	decoded := decodeBody(w.Body.String())
	var resp Response
	if err := json.Unmarshal([]byte(decoded), &resp); err != nil {
		t.Fatalf("response should be encoded when EncodeResponse is on: %v", err)
	}
	if !resp.Success || resp.Data != "pong" {
		t.Fatalf("expected pong, got %+v (decoded=%q)", resp, decoded)
	}
}

// Lenient mode also accepts a plain body for ANY method — AllowPlain is
// irrelevant when encoding isn't required. Pairs with the encoded-body case
// above to assert the "either form" guarantee under the prod config.
func TestHTTPHandler_LenientMode_PlainBody(t *testing.T) {
	d := NewDispatcher(testMethods())
	handler := NewHTTPHandler(d, WithHTTPEncoding(Encoding{Codec: testCodec{}, EncodeResponse: true}))

	body, _ := json.Marshal(Request{Method: "app.ping"})
	req := httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	decoded := decodeBody(w.Body.String())
	var resp Response
	if err := json.Unmarshal([]byte(decoded), &resp); err != nil {
		t.Fatalf("response should be encoded when EncodeResponse is on: %v", err)
	}
	if !resp.Success || resp.Data != "pong" {
		t.Fatalf("expected pong, got %+v (decoded=%q)", resp, decoded)
	}
}

func TestHTTPHandler_InvalidBearerToken(t *testing.T) {
	authResolver := newMockAuthResolver()
	d := NewDispatcher(testMethods(), WithAuthResolver(authResolver))
	handler := NewHTTPHandler(d)

	body, _ := json.Marshal(Request{Method: "user.whoami"})
	req := httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer bad_token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Success || resp.Error != "AUTH_REQUIRED" {
		t.Fatalf("expected AUTH_REQUIRED for bad token, got %+v", resp)
	}
}
