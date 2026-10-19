// Package webcast casts live camera streams (greendrake.nz/go/camstream) to browsers:
// H.265 in fragmented MP4 with the camera's G.711 audio beside it, any number of streams
// over one WebSocket, each pulled off its camera only while someone watches. Another host
// takes a stream off one as it is, rather than off the camera (Relay).
package webcast

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/coder/websocket"
	"greendrake.nz/go/camstream"
)

// Lookup finds the source of the stream key names, for a viewer, or for another host
// relaying it if raw. Nil means the stream is not served so, or has nothing to give right
// now.
type Lookup func(key string, raw bool) *camstream.Source

// Option configures a Handler.
type Option func(*handler)

// WithOriginPatterns admits browser sockets from the origins patterns match, beside the
// endpoint's own, which is always admitted; a socket from any other origin is refused at
// the handshake. A pattern holding a scheme ("https://app.example.com") is matched against
// the origin's scheme and host, any other against its host alone, both with path.Match. A
// client sending no Origin, another host relaying, is not checked.
func WithOriginPatterns(patterns ...string) Option {
	return func(h *handler) { h.originPatterns = patterns }
}

// WithLogger is where the handler logs a refused handshake, and a stream it cannot cast;
// the default is slog.Default().
func WithLogger(l *slog.Logger) Option {
	return func(h *handler) { h.logger = l }
}

// Handler serves the streams lookup finds by their keys, each socket until ctx ends. The
// host mounts it where it chooses, its GET.
func Handler(ctx context.Context, lookup Lookup, opts ...Option) http.Handler {
	h := &handler{ctx: ctx, lookup: lookup, logger: slog.Default()}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

type handler struct {
	ctx            context.Context
	lookup         Lookup
	originPatterns []string
	logger         *slog.Logger
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: h.originPatterns})
	if err != nil {
		// Accept has answered the client; a refused origin is worth an operator's notice,
		// the more so when the patterns are what is wrong.
		h.logger.Warn("webcast: WebSocket handshake refused", "origin", r.Header.Get("Origin"), "err", err)
		return
	}
	newConnection(h.ctx, h.lookup, h.logger).serve(ws)
}
