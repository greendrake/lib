package dvrip

import "greendrake.nz/go/dvrip/internal/wire"

// SofiaHash is the form a password takes on the wire: its MD5 digest, read as eight
// byte pairs, each pair's sum indexing a 62-character alphabet.
func SofiaHash(password string) string {
	return wire.SofiaHash(password)
}
