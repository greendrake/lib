package rpckit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"greendrake.nz/go/persist"
	"greendrake.nz/go/rpcserver"
	"greendrake.nz/go/rpcserver/rpcargs"
	"greendrake.nz/go/sixid"
	"greendrake.nz/go/sixid/prefix"
)

var codec = prefix.New(prefix.Table{"thing": 'T', "other": 'O'})

// validating is a context carrying a validator with SixIDRule, as a host's
// dispatcher gives its requests.
func validating() context.Context {
	return rpcserver.ContextWithValidator(context.Background(), rpcargs.NewValidator(SixIDRule(codec)))
}

// apiError fails the test unless err is an APIError, and returns it.
func apiError(t *testing.T, err error) *rpcserver.APIError {
	t.Helper()
	var apiErr *rpcserver.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("not an APIError: %v", err)
	}
	return apiErr
}

type pageRow struct{ N int }
type pageResp struct {
	Double int `json:"double"`
}

func double(r *pageRow) *pageResp { return &pageResp{Double: 2 * r.N} }

func TestPageHandler(t *testing.T) {
	var got persist.ListArgs
	h := PageHandler("thing.list", func(_ context.Context, args persist.ListArgs) (persist.Page[pageRow], error) {
		got = args
		return persist.Page[pageRow]{Rows: []pageRow{{1}, {2}}, Total: 7}, nil
	}, double)
	hc := &rpcserver.HandlerContext{Ctx: validating()}

	out, err := h(hc, json.RawMessage(`[{"q":"x","offset":10,"sort_by":"n","sort_dir":"desc"}]`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(out)
	if string(body) != `{"data":[{"double":2},{"double":4}],"total":7}` {
		t.Fatalf("envelope: %s", body)
	}
	if got != (persist.ListArgs{Q: "x", SortBy: "n", SortDir: "desc", Offset: 10, Limit: 50}) {
		t.Fatalf("args: %+v", got)
	}

	if _, err := h(hc, json.RawMessage(`[{"limit":5000}]`)); apiError(t, err).Code != rpcserver.CodeInvalidArgument {
		t.Fatal("a limit over 1000 must fail validation")
	}
	if _, err := h(hc, json.RawMessage(`[{"limit":5}]`)); err != nil || got.Limit != 5 {
		t.Fatalf("a limit given: %v, %+v", err, got)
	}
}

func TestPageResponse_Failure(t *testing.T) {
	fault := errors.New("down")
	if _, err := PageResponse("thing.list", persist.Page[pageRow]{}, fault, double); !errors.Is(err, fault) {
		t.Fatalf("a fault goes to the dispatcher: %v", err)
	}
}

func TestFail(t *testing.T) {
	if got := apiError(t, Fail("m", fmt.Errorf("thing %w", persist.ErrNotFound), "THING_NOT_FOUND")); got.Code != "THING_NOT_FOUND" {
		t.Errorf("not found: %s", got.Code)
	}
	if got := apiError(t, Fail("m", fmt.Errorf("wrapped: %w", rpcserver.NewAPIError("TOO_LATE")), "X")); got.Code != "TOO_LATE" {
		t.Errorf("an APIError answers as itself: %s", got.Code)
	}
	sort := apiError(t, Fail("m", fmt.Errorf("list: %w", persist.ErrInvalidSortField), "X"))
	if sort.Code != rpcserver.CodeInvalidArgument || !reflect.DeepEqual(sort.Details, []rpcargs.FieldError{{Field: "sort_by", Rule: "oneof"}}) {
		t.Errorf("a sort field outside the set: %+v", sort)
	}
	fault := errors.New("db down")
	if err := Fail("thing.get", fault, "X"); !errors.Is(err, fault) || err.Error() != "thing.get: db down" {
		t.Errorf("a fault goes to the dispatcher, under the method: %v", err)
	}
}

func TestParseID(t *testing.T) {
	id := sixid.NewSixID()
	ctx := validating()

	got, err := ParseID(ctx, json.RawMessage(`["`+codec.Encode("thing", id)+`"]`), codec, "thing")
	if err != nil || got != id {
		t.Fatalf("got %v, %v", got, err)
	}

	for name, tc := range map[string]struct {
		raw  string
		want rpcargs.FieldError
	}{
		"missing":      {`[""]`, rpcargs.FieldError{Rule: "required"}},
		"other entity": {`["` + codec.Encode("other", id) + `"]`, rpcargs.FieldError{Rule: "sixid", Param: "thing"}},
		"garbage":      {`["nope"]`, rpcargs.FieldError{Rule: "sixid", Param: "thing"}},
	} {
		_, err := ParseID(ctx, json.RawMessage(tc.raw), codec, "thing")
		apiErr := apiError(t, err)
		if apiErr.Code != rpcserver.CodeInvalidArgument || !reflect.DeepEqual(apiErr.Details, []rpcargs.FieldError{tc.want}) {
			t.Fatalf("%s: got %+v", name, apiErr)
		}
	}
}

func TestDecodeIDNamesTheField(t *testing.T) {
	id := sixid.NewSixID()
	if got, err := DecodeID(codec, "thing", "ids[0]", codec.Encode("thing", id)); err != nil || got != id {
		t.Fatalf("got %v, %v", got, err)
	}
	_, err := DecodeID(codec, "thing", "ids[1]", codec.Encode("other", id))
	if got := apiError(t, err); !reflect.DeepEqual(got.Details, []rpcargs.FieldError{{Field: "ids[1]", Rule: "sixid", Param: "thing"}}) {
		t.Fatalf("got %+v", got)
	}
}

func TestSixIDRule(t *testing.T) {
	type idArgs struct {
		ID string `json:"id" validate:"required,sixid=thing"`
	}
	id := sixid.NewSixID()
	for raw, ok := range map[string]bool{
		`{"id":"` + codec.Encode("thing", id) + `"}`: true,
		`{"id":"` + codec.Encode("other", id) + `"}`: false,
		`{"id":"nope"}`: false,
	} {
		_, err := rpcargs.ParseAndValidate[idArgs](validating(), json.RawMessage(raw))
		if (err == nil) != ok {
			t.Errorf("%s: ok %v, got %v", raw, ok, err)
		}
	}
}

func TestOperatorKey(t *testing.T) {
	// whom answers the principal's id.
	whom := func(hc *rpcserver.HandlerContext, _ json.RawMessage) (any, error) {
		return rpcserver.UserID[string](hc)
	}
	methods := map[string]rpcserver.MethodDef{"admin": {Handler: whom, Auth: rpcserver.AuthRequired, Access: Admin}}
	for want, options := range map[string][]rpcserver.DispatcherOption{
		"admin": OperatorKey("secret"),
		"P1":    OperatorKeyAs("secret", rpcserver.AuthInfo{UserID: "P1"}),
	} {
		server := httptest.NewServer(rpcserver.NewHTTPHandler(rpcserver.NewDispatcher(methods, options...)))
		for token, code := range map[string]string{"secret": "", "wrong": rpcserver.CodeAuthRequired, "": rpcserver.CodeAuthRequired} {
			req, _ := http.NewRequest(http.MethodPost, server.URL, strings.NewReader(`{"method":"admin"}`))
			req.Header.Set("Authorization", "Bearer "+token)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			var resp rpcserver.Response
			if err := json.NewDecoder(res.Body).Decode(&resp); err != nil || resp.Error != code || (code == "" && resp.Data != want) {
				t.Errorf("%s %q: %+v, %v", want, token, resp, err)
			}
			res.Body.Close()
		}
		server.Close()
	}
}

func TestStamp(t *testing.T) {
	at := time.Date(2026, 10, 29, 22, 41, 20, 87_091_263, time.FixedZone("NZDT", 13*3600))
	if got := Stamp(at); got != "2026-10-29T09:41:20.087Z" {
		t.Fatalf("Stamp = %s", got)
	}
	if got := StampOf(&at); got == nil || *got != Stamp(at) {
		t.Fatalf("StampOf = %v", got)
	}
	if StampOf(nil) != nil {
		t.Fatal("stamped no time")
	}
}

func TestAdmit(t *testing.T) {
	for _, tc := range []struct {
		access         Access
		admin, phantom bool
		code           string
	}{
		{0, false, false, ""},
		{Admin, false, false, CodeAdminRequired},
		{Admin, true, false, ""},
		{0, false, true, CodePhantomNotAllowed},
		{AllowPhantom, false, true, ""},
		{AllowPhantom, false, false, ""},
		{Admin, false, true, CodeAdminRequired},
	} {
		code := ""
		if err := Admit(tc.access, tc.admin, tc.phantom); err != nil {
			code = apiError(t, err).Code
		}
		if code != tc.code {
			t.Errorf("%+v: %q", tc, code)
		}
	}
}

// accountsSeeing is an AccountGate recording the access it was asked for.
func accountsSeeing(seen *Access) AccountGate {
	return func(ctx context.Context, _ *rpcserver.AuthInfo, access Access) (context.Context, error) {
		*seen = access
		return ctx, nil
	}
}

func TestGate(t *testing.T) {
	ctx := context.Background()
	key := &rpcserver.AuthInfo{UserID: "ops", Key: true}
	account := &rpcserver.AuthInfo{UserID: 7}
	for _, access := range []any{nil, Admin, AllowPhantom} {
		def := &rpcserver.MethodDef{Auth: rpcserver.AuthRequired, Access: access}
		if _, err := Gate(nil)(ctx, key, def); err != nil {
			t.Errorf("%v: a key principal is an operator: %v", access, err)
		}
		if _, err := Gate(nil)(ctx, account, def); apiError(t, err).Code != rpcserver.CodeUnauthenticated {
			t.Errorf("%v: a host without accounts admits keys alone", access)
		}
		seen := Access(-1)
		if _, err := Gate(accountsSeeing(&seen))(ctx, account, def); err != nil {
			t.Fatal(err)
		}
		if want, _ := access.(Access); seen != want {
			t.Errorf("%v: the accounts were asked for %v", access, seen)
		}
	}

	_, err := Gate(nil)(ctx, key, &rpcserver.MethodDef{Auth: rpcserver.AuthRequired, Access: "admin"})
	var apiErr *rpcserver.APIError
	if err == nil || errors.As(err, &apiErr) {
		t.Fatalf("an Access of another type is a fault: %v", err)
	}
}
