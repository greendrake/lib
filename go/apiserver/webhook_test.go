package apiserver

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
)

var errRejected = errors.New("rejected")

func TestWebhook(t *testing.T) {
	var got []byte
	handler := Webhook("test webhook", errRejected, func(r *http.Request, body []byte) error {
		got = body
		switch string(body) {
		case "forged":
			return fmt.Errorf("signature: %w", errRejected)
		case "down":
			return errors.New("the store is down")
		}
		return nil
	})
	for _, c := range []struct {
		name string
		body io.Reader
		want int
	}{
		{"handled", strings.NewReader("ok"), http.StatusOK},
		{"not authenticating", strings.NewReader("forged"), http.StatusUnauthorized},
		{"failing", strings.NewReader("down"), http.StatusInternalServerError},
		{"unreadable", iotest.ErrReader(errors.New("reset")), http.StatusBadRequest},
		{"past the limit", bytes.NewReader(make([]byte, MaxWebhookBody+1)), http.StatusRequestEntityTooLarge},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler(rec, httptest.NewRequest(http.MethodPost, "/v1/webhooks/x", c.body))
			if rec.Code != c.want {
				t.Fatalf("answered %d, want %d", rec.Code, c.want)
			}
		})
	}
	// The body handle is given is the request's.
	handler(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", strings.NewReader("ok")))
	if string(got) != "ok" {
		t.Fatalf("handled %q", got)
	}
	// A body at the limit is read whole.
	handler(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(make([]byte, MaxWebhookBody))))
	if len(got) != MaxWebhookBody {
		t.Fatalf("handled %d bytes", len(got))
	}
}
