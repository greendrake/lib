package apiserver

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
)

// MaxWebhookBody is the most of a callback's body Webhook reads: a
// provider's event, never a payload of size.
const MaxWebhookBody = 1 << 20

// Webhook is the handler of a provider's callback whose body is only a
// trigger: handle authenticates it and acts on it. The answer matters to the
// provider — one outside 2xx has it redeliver — so handle's error decides
// it: rejected (errors.Is) is a callback that does not authenticate, a 401
// saying nothing of why; any other is a failure that may pass, a 500 rather
// than a 200 stranding what the callback names. A body past MaxWebhookBody
// is a 413, one that cannot be read a 400. Each is logged under name.
func Webhook(name string, rejected error, handle func(r *http.Request, body []byte) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		attrs := []any{"path", r.URL.Path}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxWebhookBody))
		var tooLarge *http.MaxBytesError
		switch {
		case errors.As(err, &tooLarge):
			slog.WarnContext(r.Context(), name+": a body past the limit", append(attrs, "limit", MaxWebhookBody)...)
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		case err != nil:
			slog.ErrorContext(r.Context(), name+": read the body", append(attrs, "err", err)...)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		err = handle(r, body)
		switch {
		case errors.Is(err, rejected):
			slog.WarnContext(r.Context(), name+": rejected", append(attrs, "err", err)...)
			w.WriteHeader(http.StatusUnauthorized)
		case err != nil:
			slog.ErrorContext(r.Context(), name, append(attrs, "err", err)...)
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}
}
