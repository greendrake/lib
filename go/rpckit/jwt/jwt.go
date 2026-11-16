// Package jwt is the tokens a host issues its principals: HMAC-signed JWTs
// naming a subject and its admin and phantom standing, for an issuer and
// audience; and the rpcserver.AuthResolver that reads them after the host's
// operator key.
//
// The host is the server binary issuing them; a phantom is a provisional
// account, held by someone not yet registered.
package jwt

import (
	"context"
	"fmt"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"

	"greendrake.nz/go/rpcserver"
)

// Claims are what a token asserts: its subject (the registered sub claim),
// and the subject's admin and phantom standing.
type Claims struct {
	IsAdmin bool `json:"is_admin"`
	Phantom bool `json:"phantom"`
	gojwt.RegisteredClaims
}

// Tokens mints and verifies tokens.
type Tokens struct {
	secret   []byte
	issuer   string
	audience string
	ttl      time.Duration
}

func New(secret, issuer, audience string, ttl time.Duration) *Tokens {
	return &Tokens{secret: []byte(secret), issuer: issuer, audience: audience, ttl: ttl}
}

// Mint is subject's token asserting admin and phantom, expiring ttl from now.
func (t *Tokens) Mint(subject string, admin, phantom bool) (string, error) {
	now := time.Now()
	token := gojwt.NewWithClaims(gojwt.SigningMethodHS256, &Claims{
		IsAdmin:   admin,
		Phantom:   phantom,
		Issuer:    t.issuer,
		Audience:  gojwt.ClaimStrings{t.audience},
		Subject:   subject,
		IssuedAt:  gojwt.NewNumericDate(now),
		ExpiresAt: gojwt.NewNumericDate(now.Add(t.ttl)),
	})
	signed, err := token.SignedString(t.secret)
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}
	return signed, nil
}

// Verify is token's claims when it is ours — signed with the secret,
// unexpired, from the issuer, for the audience.
func (t *Tokens) Verify(token string) (*Claims, error) {
	claims := &Claims{}
	_, err := gojwt.ParseWithClaims(token, claims, func(*gojwt.Token) (any, error) { return t.secret, nil },
		gojwt.WithValidMethods([]string{gojwt.SigningMethodHS256.Alg()}),
		gojwt.WithIssuer(t.issuer),
		gojwt.WithAudience(t.audience),
		gojwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, fmt.Errorf("token: %w", err)
	}
	return claims, nil
}

// Resolver is the rpcserver.AuthResolver of a host whose principals are its
// operators, by their pre-shared key, and the subjects of its tokens. The key
// is tried first; otherwise a verified token's subject, parsed into the
// host's id type ID, is the principal's UserID and its claims the
// principal's Claims. Anything else is UNAUTHENTICATED.
type Resolver[ID any] struct {
	tokens *Tokens
	key    rpcserver.KeyAuthResolver
	parse  func(subject string) (ID, error)
}

func NewResolver[ID any](tokens *Tokens, key rpcserver.KeyAuthResolver, parse func(subject string) (ID, error)) *Resolver[ID] {
	return &Resolver[ID]{tokens: tokens, key: key, parse: parse}
}

func (r *Resolver[ID]) ResolveToken(_ context.Context, token string) (*rpcserver.AuthInfo, error) {
	if info, ok := r.key.Match(token); ok {
		return info, nil
	}
	claims, err := r.tokens.Verify(token)
	if err != nil {
		return nil, rpcserver.NewAPIError(rpcserver.CodeUnauthenticated)
	}
	id, err := r.parse(claims.Subject)
	if err != nil {
		return nil, rpcserver.NewAPIError(rpcserver.CodeUnauthenticated)
	}
	return &rpcserver.AuthInfo{UserID: id, Claims: claims}, nil
}

// PhantomOf is the id of auth's principal where a Resolver resolved it from
// a token saying it is a phantom. No principal, a key's, and a token's that
// is no phantom are none. A method no gate guards reads its caller's
// standing here.
func PhantomOf[ID any](auth *rpcserver.AuthInfo) (ID, bool) {
	if auth != nil {
		if claims, ok := auth.Claims.(*Claims); ok && claims.Phantom {
			return auth.UserID.(ID), true
		}
	}
	var none ID
	return none, false
}
