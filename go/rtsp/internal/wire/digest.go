package wire

import (
	"crypto/md5"
	"encoding/hex"
)

// Digest is the response of user with password to a Digest challenge (RFC 2617) in realm
// with nonce, for a request of method on uri: with no qop, as cameras ask for it, or
// with qop "auth", nc and cnonce.
func Digest(user, realm, password, method, uri, nonce, qop, nc, cnonce string) string {
	ha1 := hash(user + ":" + realm + ":" + password)
	ha2 := hash(method + ":" + uri)
	if qop == "" {
		return hash(ha1 + ":" + nonce + ":" + ha2)
	}
	return hash(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
}

func hash(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}
