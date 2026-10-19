package rpcserver

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// DefaultHTTPBodyLimit is the largest request body an HTTP handler reads, in
// bytes, unless WithHTTPBodyLimit says otherwise.
const DefaultHTTPBodyLimit = 1 << 20

// HTTPOption configures NewHTTPHandler.
type HTTPOption func(*httpHandler)

// WithHTTPEncoding puts an Encoding on the HTTP handler.
func WithHTTPEncoding(e Encoding) HTTPOption {
	e = e.mustHaveCodec()
	return func(h *httpHandler) { h.encoding = e }
}

// WithHTTPBodyLimit is the largest request body the handler reads, in bytes:
// a larger one is answered CodeRequestTooLarge.
func WithHTTPBodyLimit(n int64) HTTPOption {
	return func(h *httpHandler) { h.bodyLimit = n }
}

// NewHTTPHandler returns an http.Handler serving the envelope over POST.
// Responses omit the envelope's "type", which only a socket needs to tell a
// response from a push.
func NewHTTPHandler(d *Dispatcher, opts ...HTTPOption) http.Handler {
	h := &httpHandler{dispatcher: d, bodyLimit: DefaultHTTPBodyLimit}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

type httpHandler struct {
	dispatcher *Dispatcher
	encoding   Encoding
	bodyLimit  int64
}

func (h *httpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Resolve auth up front: the encoding decisions (plain vs encoded, in both
	// directions) can depend on the principal — a resolver may flag a trusted
	// caller (e.g. a static admin key) to opt out of encoding — and that must
	// hold for the request-acceptance gate and the early error responses below
	// too, not just the dispatched result.
	authInfo := h.dispatcher.principal(r, bearerToken(r))
	plainTransport := authInfo != nil && authInfo.PlainTransport

	bodyBytes, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.bodyLimit))
	if err != nil {
		code := CodeInvalidJSON
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			code = CodeRequestTooLarge
		}
		h.writeResponse(w, &Response{Success: false, Error: code}, plainTransport)
		return
	}

	// Auto-detect the request form: a structurally-JSON body is taken plain,
	// anything else is decoded. A body that does not decode is as malformed as
	// one that does not parse. Strict-mode enforcement happens after parsing,
	// below.
	ready, plain, err := h.encoding.decodeRequest(bodyBytes)
	var req Request
	if err == nil {
		err = json.Unmarshal(ready, &req)
	}
	if err != nil {
		h.writeResponse(w, &Response{Success: false, Error: CodeInvalidJSON}, plainTransport)
		return
	}

	// Strict mode (RequireEncodedRequest): a plain body is a protocol
	// violation. Accept it only from a PlainTransport principal (a trusted
	// non-browser caller — an admin key — that never runs the encoder), or for
	// methods explicitly flagged AllowPlain — early-boot endpoints whose caller
	// can't reach the client's encoder. Everything else is rejected with the
	// exact generic error a parse failure yields, so the response never betrays
	// that encoding was required. Response encoding is independent:
	// writeResponse always honours EncodeResponse, so a client decoding
	// whatever is not plain JSON gets a consistent shape.
	if plain && h.encoding.RequireEncodedRequest && !plainTransport {
		def, ok := h.dispatcher.MethodDef(req.Method)
		if !ok || !def.AllowPlain {
			h.writeResponse(w, &Response{Success: false, Error: CodeInvalidJSON}, plainTransport)
			return
		}
	}

	hc := &HandlerContext{
		Ctx:      r.Context(),
		ClientIP: h.dispatcher.clientIP(r),
	}

	h.writeResponse(w, h.dispatcher.Dispatch(hc, authInfo, &req), plainTransport)
}

// bearerToken is the credential of an "Authorization: Bearer <token>" header,
// or "" for none and for any other scheme.
func bearerToken(r *http.Request) string {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "bearer") {
		return ""
	}
	return token
}

// writeResponse marshals and writes resp. plain, when true, forces a plain JSON
// body regardless of Encoding.EncodeResponse — set per request from the
// resolved principal (see AuthInfo.PlainTransport).
func (h *httpHandler) writeResponse(w http.ResponseWriter, resp *Response, plain bool) {
	body := h.dispatcher.marshal(resp)
	// Content-Type tracks the body's actual shape: JSON only when we ship
	// JSON, plain text when EncodeResponse swaps the body for its encoded form
	// (which is opaque to anything expecting valid JSON — devtools, CDN
	// sniffers, intermediaries).
	if h.encoding.EncodeResponse && !plain {
		body = h.encoding.Codec.Encode(body)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	// A client gone before its answer is no fault of the server's.
	_, _ = w.Write(body)
}
