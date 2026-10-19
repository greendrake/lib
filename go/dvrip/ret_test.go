package dvrip

import (
	"errors"
	"strings"
	"testing"
)

func TestSofiaHash(t *testing.T) {
	if got := SofiaHash("admin"); got != "6QNMIQGe" {
		t.Fatalf("SofiaHash(admin) = %q", got)
	}
}

func TestRetError(t *testing.T) {
	credentials := []Ret{106, 203, 204, 205}
	for _, ret := range credentials {
		if !(&RetError{Ret: ret}).IsCredentials() {
			t.Fatalf("%d should be a credentials failure", ret)
		}
	}
	if (&RetError{Ret: 107}).IsCredentials() {
		t.Fatal("107 is not a credentials failure")
	}
	if !(&RetError{Ret: 607}).IsAbsent() || (&RetError{Ret: 608}).IsAbsent() {
		t.Fatal("607 alone says a configuration does not exist")
	}
	if got := (&RetError{Request: "login", Ret: 203}).Error(); got != "dvrip: login: 203 wrong password" {
		t.Fatalf("message %q", got)
	}
	if got := (&RetError{Request: "x", Ret: 999}).Error(); !strings.Contains(got, "unknown status") {
		t.Fatalf("message %q", got)
	}
}

func TestDecodeReply(t *testing.T) {
	r, err := decodeReply([]byte(`{"Name":"X","Ret":603,"X":1}`+"\n\x00"), "X")
	if err != nil {
		t.Fatal(err)
	}
	if !r.ret.restart() {
		t.Fatal("603 should report a restart")
	}
	if _, err := r.field("Y"); err == nil {
		t.Fatal("a missing field should be an error")
	}
	var retErr *RetError
	if _, err := decodeReply([]byte(`{"Ret":607}`), "X"); !errors.As(err, &retErr) || retErr.Ret != 607 {
		t.Fatalf("got %v, want a RetError 607", err)
	}
	for _, body := range []string{`{"Name":"X"}`, `not json`} {
		if _, err := decodeReply([]byte(body), "X"); err == nil {
			t.Fatalf("%s should not decode", body)
		}
	}
}
