package jwt

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"

	"greendrake.nz/go/rpcserver"
)

const (
	secret   = "token-secret"
	issuer   = "https://api.example.com"
	audience = "example"
	key      = "operator-key"
)

var tokens = New(secret, issuer, audience, time.Hour)

// operator is the principal the operator key resolves to.
var operator = rpcserver.KeyAuthResolver{Key: key, AuthInfo: rpcserver.AuthInfo{UserID: -1, PlainTransport: true}}

// resolver resolves subjects as ints.
var resolver = NewResolver(tokens, operator, strconv.Atoi)

func mint(t *testing.T, tk *Tokens, subject string, admin, phantom bool) string {
	t.Helper()
	token, err := tk.Mint(subject, admin, phantom)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// sign is a token minted outside Tokens: claims as given, signed by method
// with signingKey.
func sign(t *testing.T, method gojwt.SigningMethod, signingKey any, claims gojwt.Claims) string {
	t.Helper()
	token, err := gojwt.NewWithClaims(method, claims).SignedString(signingKey)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// ours is claims as Tokens would mint them for subject, bar the expiry.
func ours(subject string, expires *gojwt.NumericDate) *Claims {
	return &Claims{
		Issuer:    issuer,
		Audience:  gojwt.ClaimStrings{audience},
		Subject:   subject,
		ExpiresAt: expires}
}

func TestMintVerify(t *testing.T) {
	before := time.Now().Truncate(time.Second)
	claims, err := tokens.Verify(mint(t, tokens, "42", true, true))
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "42" || !claims.IsAdmin || !claims.Phantom {
		t.Errorf("claims %+v", claims)
	}
	if claims.Issuer != issuer || len(claims.Audience) != 1 || claims.Audience[0] != audience {
		t.Errorf("issuer %q, audience %v", claims.Issuer, claims.Audience)
	}
	if claims.IssuedAt.Before(before) || !claims.ExpiresAt.Equal(claims.IssuedAt.Add(time.Hour)) {
		t.Errorf("issued %v, expires %v", claims.IssuedAt, claims.ExpiresAt)
	}
}

// The claims a token carries are its verifiers' contract: a token signed with
// exactly these resolves, and Mint writes exactly these.
func TestWireClaims(t *testing.T) {
	at := time.Now().Unix()
	wire := gojwt.MapClaims{
		"sub": "42", "is_admin": true, "phantom": true,
		"iss": issuer, "aud": []any{audience}, "iat": float64(at), "exp": float64(at + 3600),
	}
	info, err := resolver.ResolveToken(t.Context(), sign(t, gojwt.SigningMethodHS256, []byte(secret), wire))
	if err != nil || info.UserID != 42 {
		t.Fatalf("a token of the wire claims: %+v, %v", info, err)
	}
	if id, phantom := PhantomOf[int](info); !phantom || id != 42 || !info.Claims.(*Claims).IsAdmin {
		t.Fatalf("a token of the wire claims stands as %+v", info.Claims)
	}

	payload, err := base64.RawURLEncoding.DecodeString(strings.Split(mint(t, tokens, "42", true, true), ".")[1])
	if err != nil {
		t.Fatal(err)
	}
	minted := gojwt.MapClaims{}
	if err := json.Unmarshal(payload, &minted); err != nil {
		t.Fatal(err)
	}
	wire["iat"], wire["exp"] = minted["iat"], minted["exp"]
	if !reflect.DeepEqual(minted, wire) {
		t.Fatalf("minted %v, want %v", minted, wire)
	}
}

func TestVerifyRejects(t *testing.T) {
	inAnHour := gojwt.NewNumericDate(time.Now().Add(time.Hour))
	for _, tc := range []struct {
		name  string
		token string
		want  error
	}{
		{"foreign secret", mint(t, New("other-secret", issuer, audience, time.Hour), "42", false, false), gojwt.ErrTokenSignatureInvalid},
		{"foreign issuer", mint(t, New(secret, "https://other.example.com", audience, time.Hour), "42", false, false), gojwt.ErrTokenInvalidIssuer},
		{"foreign audience", mint(t, New(secret, issuer, "other", time.Hour), "42", false, false), gojwt.ErrTokenInvalidAudience},
		{"expired", mint(t, New(secret, issuer, audience, -time.Minute), "42", false, false), gojwt.ErrTokenExpired},
		{"no expiry", sign(t, gojwt.SigningMethodHS256, []byte(secret), ours("42", nil)), gojwt.ErrTokenRequiredClaimMissing},
		{"another algorithm", sign(t, gojwt.SigningMethodHS512, []byte(secret), ours("42", inAnHour)), gojwt.ErrTokenSignatureInvalid},
		{"unsigned", sign(t, gojwt.SigningMethodNone, gojwt.UnsafeAllowNoneSignatureType, ours("42", inAnHour)), gojwt.ErrTokenSignatureInvalid},
		{"malformed", "not-a-token", gojwt.ErrTokenMalformed},
	} {
		if claims, err := tokens.Verify(tc.token); !errors.Is(err, tc.want) || claims != nil {
			t.Errorf("%s: %v, %v", tc.name, claims, err)
		}
	}
}

func TestResolverKey(t *testing.T) {
	info, err := resolver.ResolveToken(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	if info.UserID != -1 || !info.PlainTransport || !info.Key || info.Claims != nil {
		t.Fatalf("key principal %+v", info)
	}
	if _, phantom := PhantomOf[int](info); phantom {
		t.Fatal("a key principal is no phantom")
	}
}

func TestResolverToken(t *testing.T) {
	for _, phantom := range []bool{false, true} {
		info, err := resolver.ResolveToken(t.Context(), mint(t, tokens, "42", false, phantom))
		if err != nil {
			t.Fatal(err)
		}
		claims, _ := info.Claims.(*Claims)
		if info.UserID != 42 || claims == nil || claims.Subject != "42" || claims.Phantom != phantom || info.Key || info.PlainTransport {
			t.Fatalf("token principal %+v", info)
		}
		if id, ok := PhantomOf[int](info); ok != phantom || (phantom && id != 42) {
			t.Errorf("phantom %v: PhantomOf %v, %v", phantom, id, ok)
		}
	}
	if _, phantom := PhantomOf[int](nil); phantom {
		t.Fatal("no principal is no phantom")
	}
}

func TestResolverUnauthenticated(t *testing.T) {
	inAnHour := gojwt.NewNumericDate(time.Now().Add(time.Hour))
	for name, tc := range map[string]struct {
		key, token string
	}{
		"rejected token":     {key, "not-a-token"},
		"unreadable subject": {key, sign(t, gojwt.SigningMethodHS256, []byte(secret), ours("not-a-number", inAnHour))},
		"no key configured":  {"", ""},
	} {
		r := NewResolver(tokens, rpcserver.KeyAuthResolver{Key: tc.key, AuthInfo: operator.AuthInfo}, strconv.Atoi)
		info, err := r.ResolveToken(t.Context(), tc.token)
		var apiErr *rpcserver.APIError
		if !errors.As(err, &apiErr) || apiErr.Code != rpcserver.CodeUnauthenticated || info != nil {
			t.Errorf("%s: %+v, %v", name, info, err)
		}
	}
}
