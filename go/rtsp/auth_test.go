package rtsp

import (
	"encoding/base64"
	"strings"
	"testing"

	"greendrake.nz/go/rtsp/internal/wire"
)

func TestChallengeChoice(t *testing.T) {
	c, err := challengeOf([]string{`Basic realm="cam"`, `Digest realm="IP Camera(10E46)", nonce="a1, b2", stale="FALSE"`})
	if err != nil || !c.digest || c.params["realm"] != "IP Camera(10E46)" || c.params["nonce"] != "a1, b2" || c.params["stale"] != "FALSE" {
		t.Fatalf("%+v, %v", c, err)
	}
	if c, err := challengeOf([]string{`Basic realm="cam"`}); err != nil || c.digest {
		t.Fatalf("%+v, %v", c, err)
	}
	// A Digest of an algorithm it cannot answer leaves Basic.
	if c, err := challengeOf([]string{`Digest realm="x", nonce="y", algorithm=SHA-256`, `Basic realm="x"`}); err != nil || c.digest {
		t.Fatalf("%+v, %v", c, err)
	}
	if _, err := challengeOf([]string{`Bearer realm="x"`}); err == nil {
		t.Fatal("answered a challenge of no scheme it has")
	}
}

func TestAuthorization(t *testing.T) {
	basic := authorizer{user: "admin", password: "pass", challenge: &challenge{}}
	if got := basic.authorization("DESCRIBE", "rtsp://cam/0"); got != "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:pass")) {
		t.Fatalf("basic %q", got)
	}
	digest := authorizer{user: "admin", password: "pass", challenge: &challenge{digest: true, params: map[string]string{"realm": "r", "nonce": "n", "opaque": "o"}}}
	want := `Digest username="admin", realm="r", nonce="n", uri="rtsp://cam/0", response="` + wire.Digest("admin", "r", "pass", "DESCRIBE", "rtsp://cam/0", "n", "", "", "") + `", opaque="o"`
	if got := digest.authorization("DESCRIBE", "rtsp://cam/0"); got != want {
		t.Fatalf("digest %q", got)
	}
	// With qop, each request counts.
	digest.challenge.params["qop"] = "auth-int, auth"
	for _, nc := range []string{"00000001", "00000002"} {
		fields := wire.Params(strings.TrimPrefix(digest.authorization("PLAY", "rtsp://cam/0/"), "Digest "))
		if fields["nc"] != nc || fields["qop"] != "auth" || fields["response"] != wire.Digest("admin", "r", "pass", "PLAY", "rtsp://cam/0/", "n", "auth", nc, fields["cnonce"]) {
			t.Fatalf("digest with qop %q", fields)
		}
	}
}
