package rpckit

import (
	"context"
	"encoding/json"

	"github.com/go-playground/validator/v10"

	"greendrake.nz/go/rpcserver"
	"greendrake.nz/go/rpcserver/rpcargs"
	"greendrake.nz/go/sixid"
	"greendrake.nz/go/sixid/prefix"
)

// EntityAccount is the entity account ids render under, whichever library
// renders them, so that one codec reads them all.
const EntityAccount prefix.Entity = "user"

// ParseID parses a method's single argument as one entity's id rendered
// through the host's codec. A missing one fails as `required`; one that does
// not decode is DecodeID's refusal.
func ParseID(ctx context.Context, raw json.RawMessage, codec prefix.Codec, entity prefix.Entity) (sixid.SixID, error) {
	s, err := rpcargs.ParseAndValidateVar[string](ctx, raw, "required")
	if err != nil {
		return sixid.SixID{}, err
	}
	return DecodeID(codec, entity, "", s)
}

// DecodeID decodes s, an argument's field (named as the client sent it; ""
// for a positional one), as one entity's id. One that does not decode is
// INVALID_ARGUMENT naming the field, rule `sixid` and the entity — the
// report SixIDRule's `sixid=<entity>` tag gives.
func DecodeID(codec prefix.Codec, entity prefix.Entity, field, s string) (sixid.SixID, error) {
	id, err := codec.Decode(s, entity)
	if err != nil {
		return sixid.SixID{}, rpcserver.NewAPIErrorWithDetails(rpcserver.CodeInvalidArgument, []rpcargs.FieldError{{Field: field, Rule: "sixid", Param: string(entity)}})
	}
	return id, nil
}

// SixIDRule is the validation tag `sixid=<entity>`: a field holding an id of
// the entity, as codec renders it. Empty fails — pair it with omitempty where
// the field is optional. An entity outside codec's table decodes nothing, so a
// misused tag fails closed.
func SixIDRule(codec prefix.Codec) rpcargs.Rule {
	return rpcargs.Rule{Tag: "sixid", Fn: func(_ context.Context, fl validator.FieldLevel) bool {
		_, err := codec.Decode(fl.Field().String(), prefix.Entity(fl.Param()))
		return err == nil
	}}
}
