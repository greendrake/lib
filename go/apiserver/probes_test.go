package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func probeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type: %q", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal probe body %q: %v", rec.Body.String(), err)
	}
	return body
}

func TestHealthz(t *testing.T) {
	var draining atomic.Bool
	handler := healthz(&draining)

	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	if body := probeBody(t, rec); body["status"] != "ok" {
		t.Fatalf("body: %v", body)
	}

	draining.Store(true)
	rec = httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("draining status: %d", rec.Code)
	}
	if body := probeBody(t, rec); body["status"] != "draining" {
		t.Fatalf("draining body: %v", body)
	}
}

func TestReadyz_AllOK(t *testing.T) {
	var draining atomic.Bool
	checks := []DependencyCheck{
		{Name: "mongo", Check: func(context.Context) error { return nil }},
		{Name: "redis", Check: func(context.Context) error { return nil }},
	}
	rec := httptest.NewRecorder()
	readyz(checks, time.Second, &draining)(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	body := probeBody(t, rec)
	if body["mongo"] != "ok" || body["redis"] != "ok" {
		t.Fatalf("body: %v", body)
	}
	if _, present := body["draining"]; present {
		t.Fatalf("draining key must be absent when not draining: %v", body)
	}
}

func TestReadyz_FailingCheck(t *testing.T) {
	var draining atomic.Bool
	checks := []DependencyCheck{
		{Name: "mongo", Check: func(context.Context) error { return nil }},
		{Name: "redis", Check: func(context.Context) error { return errors.New("boom") }},
	}
	rec := httptest.NewRecorder()
	readyz(checks, time.Second, &draining)(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: %d", rec.Code)
	}
	body := probeBody(t, rec)
	if body["mongo"] != "ok" || body["redis"] != "fail: boom" {
		t.Fatalf("body: %v", body)
	}
}

func TestReadyz_Draining(t *testing.T) {
	var draining atomic.Bool
	draining.Store(true)
	checks := []DependencyCheck{
		{Name: "mongo", Check: func(context.Context) error { return nil }},
	}
	rec := httptest.NewRecorder()
	readyz(checks, time.Second, &draining)(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: %d", rec.Code)
	}
	body := probeBody(t, rec)
	if body["draining"] != true || body["mongo"] != "ok" {
		t.Fatalf("body: %v", body)
	}
}

// A body that does not encode is a 500, with no body.
func TestWriteJSON_Unencodable(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusOK, func() {})
	if rec.Code != http.StatusInternalServerError || rec.Body.Len() != 0 {
		t.Fatalf("got %d %q", rec.Code, rec.Body)
	}
}

// ProbeMux routes both probes by method and path and leaves every other
// route to the host.
func TestProbeMux(t *testing.T) {
	var draining atomic.Bool
	mux := ProbeMux(&draining, []DependencyCheck{
		{Name: "mongo", Check: func(context.Context) error { return nil }},
	}, time.Second)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || probeBody(t, rec)["status"] != "ok" {
		t.Fatalf("healthz: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK || probeBody(t, rec)["mongo"] != "ok" {
		t.Fatalf("readyz: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /healthz: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /: %d", rec.Code)
	}
}
