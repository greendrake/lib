package rtsp

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"greendrake.nz/go/rtsp/internal/wire"
)

// challenge is what a server asks requests to authenticate with.
type challenge struct {
	digest bool
	// Digest's: realm, nonce, and maybe qop, opaque, algorithm.
	params map[string]string
}

// challengeOf picks what to answer of a refusal's WWW-Authenticate fields: Digest where
// it is offered, which keeps the password off the wire, and Basic otherwise.
func challengeOf(values []string) (*challenge, error) {
	var basic *challenge
	for _, value := range values {
		scheme, rest, _ := strings.Cut(strings.TrimSpace(value), " ")
		switch strings.ToLower(scheme) {
		case "digest":
			params := wire.Params(rest)
			if algorithm := params["algorithm"]; algorithm != "" && !strings.EqualFold(algorithm, "MD5") {
				continue
			}
			return &challenge{digest: true, params: params}, nil
		case "basic":
			basic = &challenge{}
		}
	}
	if basic == nil {
		return nil, fmt.Errorf("rtsp: no challenge answerable among %q", values)
	}
	return basic, nil
}

// authorizer answers a server's challenge as user with password.
type authorizer struct {
	user, password string
	challenge      *challenge
	// How many requests the challenge's nonce has been answered for, where qop counts
	// them.
	count int
}

// authorization is what a request of method on uri carries as its Authorization.
func (a *authorizer) authorization(method, uri string) string {
	if !a.challenge.digest {
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(a.user+":"+a.password))
	}
	p := a.challenge.params
	fields := []string{quoted("username", a.user), quoted("realm", p["realm"]), quoted("nonce", p["nonce"]), quoted("uri", uri)}
	var qop, nc, cnonce string
	if slices.Contains(strings.FieldsFunc(p["qop"], func(r rune) bool { return r == ',' || r == ' ' }), "auth") {
		a.count++
		random := make([]byte, 8)
		rand.Read(random)
		qop, nc, cnonce = "auth", fmt.Sprintf("%08x", a.count), hex.EncodeToString(random)
		fields = append(fields, "qop=auth", "nc="+nc, quoted("cnonce", cnonce))
	}
	fields = append(fields, quoted("response", wire.Digest(a.user, p["realm"], a.password, method, uri, p["nonce"], qop, nc, cnonce)))
	if opaque, has := p["opaque"]; has {
		fields = append(fields, quoted("opaque", opaque))
	}
	return "Digest " + strings.Join(fields, ", ")
}

// quoted is a parameter whose value is a quoted string.
func quoted(key, value string) string {
	return key + `="` + value + `"`
}
