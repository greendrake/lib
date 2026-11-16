# greendrake.nz/go/rpckit

The conventions an RPC server and the libraries it is built from agree on,
on top of `greendrake.nz/go/rpcserver`: how a list is paged, how a failed
read is answered, how ids and times are written, who may call what, and the
tokens that say who is calling.

```sh
go get greendrake.nz/go/rpckit
```

## Why

`rpcserver` dispatches methods and stops there. It does not know what a list
is, where data is kept, what an id looks like or who is an admin. That keeps
it small, and leaves every server built on it to settle those questions.

It matters most for a server assembled from libraries, each contributing
methods to one dispatcher. When each library settles the questions for
itself, one's lists take `page` and another's `offset`, ids and timestamps
come in several spellings, and each library has its own idea of an admin.
The client then needs code per library, and the server cannot apply one
access policy to all of them. rpckit settles each question once, for the
server and its libraries alike.

## Trade-offs

- **It is opinionated.** One paging shape, one timestamp format, one kind of
  id and one model of who calls: accounts, which may be admins or
  provisional, and operators holding a pre-shared key. The ids are
  `greendrake.nz/go/sixid` ids, and the id helpers serve no other kind. A
  server that wants cursor paging, or roles beyond admin, gets nothing here
  for that part, and uses `rpcserver` directly.
- **A list method's arguments are the store's.** `PageArgs` maps one-for-one
  onto `greendrake.nz/go/persist`'s `ListArgs`, with no layer between. There
  is nothing to keep in sync, and a change to one is a change to the API. A
  list also inherits persist's limits: offset paging, and search by
  substring in one field.
- **Tokens are HS256 and stateless.** One secret signs and verifies. That
  fits a server issuing and checking its own tokens, and does not fit a
  third party that must verify tokens without being able to issue them. A
  token stays valid until it expires: there is no revocation and no refresh
  here.

## Overview

The host is the server binary, whose dispatcher takes the methods each
library contributes. rpckit gives the host and its libraries one answer to
each question:

- **Lists** take one set of arguments and answer in one shape, so a client's
  table code is written once.
- **Failures** of a read or write become API errors in one place: not found,
  a bad sort field, or an error to log.
- **Ids** are `greendrake.nz/go/sixid` ids, written through the host's
  `sixid/prefix` codec. One that does not parse is refused the same way
  everywhere.
- **Times** are written in one format.
- **Access** is one small policy — admin, account, provisional account,
  operator key — which one `rpcserver.AuthGate` applies to every method.
- **Tokens** (`rpckit/jwt`) say who is calling, and whether the caller is an
  admin or a provisional account.

## Surface

A principal is an authenticated caller. A phantom is a provisional account,
held by someone who has not registered yet.

- **Lists.** `PageArgs` is the arguments of every paged list method: `q`,
  `limit` (50 by default, at most 1000), `offset`, `sort_by` and `sort_dir`.
  A method that honours only some of them ignores the rest. `ListArgs()`
  turns them into a `persist.ListArgs`. `PageHandler(method, list, toResp)`
  is a whole list method, for one whose arguments are `PageArgs` alone: it
  reads the page with `list`, and `toResp` maps each row to the shape it has
  in the response. A method with filters of its own parses its arguments
  itself and answers through `PageResponse`. `Envelope(data, total)` is the
  `{data, total}` answer both give.
- **Failures.** `Fail(method, err, notFoundCode)` answers a handler's failed
  read or write. An `APIError` passes as it is. `persist.ErrNotFound`
  becomes `notFoundCode`. `persist.ErrInvalidSortField` becomes
  `INVALID_ARGUMENT` on `sort_by`. Any other error is returned under
  `method`, for the dispatcher to log and answer `INTERNAL_ERROR`.
- **Ids.** `ParseID(ctx, raw, codec, entity)` parses a method's single
  argument as an entity's id. `DecodeID(codec, entity, field, s)` decodes
  one field of an argument. An id that does not decode is refused as
  `INVALID_ARGUMENT`, naming the field and the rule `sixid`.
  `SixIDRule(codec)` is the same check as a validation tag,
  `sixid=<entity>`. `EntityAccount` (`user`) is the entity name of account
  ids, shared so that one codec reads them whichever library wrote them.
- **Times.** `TimeFormat` is RFC 3339 with milliseconds. `Stamp(t)` writes a
  time in UTC in that format, and `StampOf` does the same for an optional
  one.
- **Access.** `Access` is what a method asks of its principal, set as
  `MethodDef.Access`: `Admin`, `AllowPhantom`, or nothing more than a
  principal. `Admit(access, admin, phantom)` is the policy, given whether an
  account is an admin and whether it is a phantom. A method asking `Admin`
  refuses an account that is not an admin, with `ADMIN_REQUIRED`. A phantom
  is refused with `PHANTOM_NOT_ALLOWED` unless the method allows one.
  `Gate(accounts)` is the `rpcserver.AuthGate` that applies the policy. A
  principal holding the operator key passes every check. All operators share
  that key, so it does not tell one operator from another. Any other
  principal goes to the host's `AccountGate`. It finds whether the account
  is an admin and whether it is a phantom, and has `Admit` decide. Without
  an `AccountGate` (nil), only the key is admitted. `OperatorKey(key)` and
  `OperatorKeyAs(key, principal)` are the dispatcher options of a host whose
  only principal is its operators' key.

```go
methods := map[string]rpcserver.MethodDef{
	"admin.order.list": {
		Auth:    rpcserver.AuthRequired,
		Access:  rpckit.Admin,
		Handler: rpckit.PageHandler("admin.order.list", orders.List, toOrderResponse),
	},
}
dispatcher := rpcserver.NewDispatcher(methods, rpckit.OperatorKey(os.Getenv("ADMIN_KEY"))...)
```

## `jwt`

The tokens a host issues its principals, and the resolver that reads them.
The package imports only `rpcserver` and `github.com/golang-jwt/jwt/v5`.

- `New(secret, issuer, audience, ttl)` makes HS256 tokens of one issuer for
  one audience. `Mint(subject, admin, phantom)` signs a token that expires
  `ttl` from now. `Verify` returns a token's `Claims` when it was signed
  with the secret, has not expired, and names the issuer and the audience. A
  token without an expiry is refused.
- `Claims` are the registered claims, with the subject in `sub`, plus
  `is_admin` and `phantom`.
- `NewResolver(tokens, key, parse)` is the host's `rpcserver.AuthResolver`.
  It tries the operator key first (`rpcserver.KeyAuthResolver`). Otherwise
  it verifies the token, and `parse` turns the token's subject into the
  principal's `UserID`, in the host's id type. The token's `Claims` become
  the principal's. Anything else is `UNAUTHENTICATED`.
- `PhantomOf[ID](auth)` gives the principal's id when its token says it is a
  phantom. It is for a method that no gate guards, which checks for itself
  whether its caller is a phantom.

```go
tokens := jwt.New(secret, "https://api.example.com", "https://api.example.com", 24*time.Hour)
resolver := jwt.NewResolver(tokens, rpcserver.KeyAuthResolver{Key: adminKey, AuthInfo: rpcserver.AuthInfo{UserID: "admin", PlainTransport: true}}, sixid.ParseSixID)
dispatcher := rpcserver.NewDispatcher(methods, rpcserver.WithAuthResolver(resolver), rpcserver.WithAuthGate(rpckit.Gate(accountGate)))
```

## `rpckittest`

For a library's own tests, which dispatch its methods as the host would.
`Claims{Admin, Phantom}` goes in a test principal's `AuthInfo.Claims`.
`Gate` admits by those claims. `NewDispatcher(methods, opts...)` is a
dispatcher behind that gate, with rpcargs' validator. `Validating(ctx,
rules...)` is for parsing arguments outside a dispatcher.
