package apiserver

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// WriteJSON answers a plain-HTTP endpoint with status and body as JSON; a
// body that does not encode is logged and answered 500.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	data, err := json.Marshal(body)
	if err != nil {
		slog.Error("apiserver: encode response", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	// A client gone before its answer is no fault of the server's.
	_, _ = w.Write(data)
}
