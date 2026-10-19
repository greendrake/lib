package apiserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const app = "https://app.example"

var hostOptions = CORSOptions{AllowOrigin: []string{app, "app://localhost"}, AllowHeaders: []string{"X-Trace", "X-Host"}, ExposeHeaders: []string{"X-Out"}}

const allowHeaders = "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With"

// request is a request from origin ("" for none), answered by handler.
func request(handler http.Handler, method, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/v1/api", nil)
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	return rec
}

func assertHeaders(t *testing.T, rec *httptest.ResponseRecorder, want map[string]string) {
	t.Helper()
	for name, value := range want {
		if got, set := rec.Header()[name]; value == "" && set || value != "" && rec.Header().Get(name) != value {
			t.Errorf("%s: got %q, want %q", name, got, value)
		}
	}
}

func TestCORS_AdmittedOriginIsEchoed(t *testing.T) {
	handler := CORS(hostOptions, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	for _, origin := range []string{app, "app://localhost"} {
		rec := request(handler, http.MethodPost, origin)
		if rec.Code != http.StatusTeapot {
			t.Fatalf("status: %d", rec.Code)
		}
		assertHeaders(t, rec, map[string]string{
			"Access-Control-Allow-Origin":      origin,
			"Access-Control-Allow-Credentials": "",
			"Access-Control-Allow-Headers":     allowHeaders + ", X-Trace, X-Host",
			"Access-Control-Expose-Headers":    "X-Out",
			"Access-Control-Allow-Methods":     "POST, OPTIONS, GET, PUT, DELETE",
			"Vary":                             "Origin",
		})
	}
}

// An origin outside the policy, or a request with none, reaches the handler
// with no CORS headers: its browser keeps the response from the page.
func TestCORS_OtherOriginsGetNoHeaders(t *testing.T) {
	handler := CORS(hostOptions, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	for _, origin := range []string{"https://evil.example", "http://app.example", ""} {
		assertHeaders(t, request(handler, http.MethodPost, origin), map[string]string{
			"Access-Control-Allow-Origin":  "",
			"Access-Control-Allow-Headers": "",
			"Vary":                         "Origin",
		})
	}
}

func TestCORS_ShortCircuitsPreflight(t *testing.T) {
	handler := CORS(hostOptions, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("OPTIONS preflight must not reach the wrapped handler")
	}))
	rec := request(handler, http.MethodOptions, app)
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("status %d, body %q", rec.Code, rec.Body)
	}
	assertHeaders(t, rec, map[string]string{"Access-Control-Allow-Origin": app})
}

// A host with no headers of its own gets the base allow list and no expose
// header; credentials are offered where the host asks.
func TestCORS_BaseOnlyWithCredentials(t *testing.T) {
	handler := CORS(CORSOptions{AllowOrigin: []string{app}, Credentials: true}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	assertHeaders(t, request(handler, http.MethodPost, app), map[string]string{
		"Access-Control-Allow-Headers":     allowHeaders,
		"Access-Control-Expose-Headers":    "",
		"Access-Control-Allow-Credentials": "true",
	})
}

func TestCORS_AnyOrigin(t *testing.T) {
	handler := CORS(CORSOptions{AllowOrigin: []string{"*"}}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	assertHeaders(t, request(handler, http.MethodPost, "https://anyone.example"), map[string]string{"Access-Control-Allow-Origin": "*"})

	defer func() {
		if recover() == nil {
			t.Fatal("credentials for any origin must panic")
		}
	}()
	CORS(CORSOptions{AllowOrigin: []string{"*"}, Credentials: true}, http.NotFoundHandler())
}
