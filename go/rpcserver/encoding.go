package rpcserver

import "bytes"

// Codec is what a payload's JSON becomes on the wire, and back: the server end
// of `@greendrake/rpc`'s Codec, for a wire that is obscured or encrypted.
type Codec interface {
	Encode(body []byte) []byte
	Decode(body []byte) ([]byte, error)
}

// Encoding puts a Codec on a transport: pass it with WithHTTPEncoding or
// WithWSEncoding. A request that is structurally JSON is taken as it is, and
// any other is decoded, so both forms are accepted unless
// RequireEncodedRequest says otherwise. A transport without one carries plain
// JSON both ways.
type Encoding struct {
	Codec Codec
	// RequireEncodedRequest refuses a plain request: it is rejected as a
	// generic malformed request, so the response never signals that encoding
	// was the missing ingredient. A PlainTransport principal is exempt, and so,
	// over HTTP, is a method marked AllowPlain.
	RequireEncodedRequest bool
	// EncodeResponse encodes every response body, but a PlainTransport
	// principal's.
	EncodeResponse bool
}

// mustHaveCodec is an Encoding as an option takes it: one without a Codec is a
// mistake in the program.
func (e Encoding) mustHaveCodec() Encoding {
	if e.Codec == nil {
		panic("rpcserver: an Encoding without a Codec")
	}
	return e
}

// decodeRequest is the transport-agnostic half of a request's handling: a
// body that is structurally JSON is returned untouched (plain); any other is
// decoded, where the transport has a Codec. RequireEncodedRequest does not
// gate the decode — it gates whether a plain body is acceptable, which each
// transport enforces.
func (e Encoding) decodeRequest(body []byte) (ready []byte, plain bool, err error) {
	if e.Codec == nil || isLikelyJSON(body) {
		return body, true, nil
	}
	ready, err = e.Codec.Decode(body)
	return ready, false, err
}

// isLikelyJSON is a cheap structural sniff telling a plain JSON body from an
// encoded one — not a validity check: after trimming, the payload must open
// with `{`/`[` and close with the matching `}`/`]`. A codec's output must not
// pass it.
func isLikelyJSON(b []byte) bool {
	t := bytes.TrimSpace(b)
	if len(t) == 0 {
		return false
	}
	first, last := t[0], t[len(t)-1]
	return (first == '{' && last == '}') || (first == '[' && last == ']')
}
