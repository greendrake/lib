// Package rpckit is the conventions an RPC server built from libraries and
// those libraries share on top of greendrake.nz/go/rpcserver: paged lists
// over persist, failures answered by persist's sentinels, sixid ids through a
// prefix codec, timestamps, and the access policy of hosts whose principals
// are accounts and operators' pre-shared keys.
//
// The host is that server binary, whose dispatcher takes the methods each
// library contributes.
package rpckit

import (
	"context"
	"encoding/json"

	"greendrake.nz/go/persist"
	"greendrake.nz/go/rpcserver"
	"greendrake.nz/go/rpcserver/rpcargs"
)

// PageArgs is the argument shape of every paginated list method: q, limit
// (default 50, at most 1000), offset, sort_by, sort_dir. A method that
// honours only some of them ignores the rest.
type PageArgs struct {
	Q       string `json:"q,omitempty"        validate:"omitempty"`
	Limit   int    `json:"limit,omitempty"    validate:"omitempty,min=1,max=1000"`
	Offset  int    `json:"offset,omitempty"   validate:"omitempty,min=0"`
	SortBy  string `json:"sort_by,omitempty"`
	SortDir string `json:"sort_dir,omitempty"`
}

// ListArgs is the page asked for, the default limit applied.
func (a PageArgs) ListArgs() persist.ListArgs {
	limit := a.Limit
	if limit == 0 {
		limit = 50
	}
	return persist.ListArgs{Q: a.Q, SortBy: a.SortBy, SortDir: a.SortDir, Offset: a.Offset, Limit: limit}
}

// PageHandler is a list method whose arguments are PageArgs alone: it reads
// the page and answers PageResponse.
func PageHandler[Row, Resp any](method string, list func(ctx context.Context, args persist.ListArgs) (persist.Page[Row], error), toResp func(*Row) *Resp) rpcserver.HandlerFunc {
	return func(hc *rpcserver.HandlerContext, raw json.RawMessage) (any, error) {
		page, err := rpcargs.ParseAndValidate[PageArgs](hc.Ctx, raw)
		if err != nil {
			return nil, err
		}
		rows, err := list(hc.Ctx, page.ListArgs())
		return PageResponse(method, rows, err, toResp)
	}
}

// PageResponse is Envelope of page, the rows mapped to their wire shape; a
// read that failed is answered by Fail. A method with filters of its own
// parses its arguments and answers through this.
func PageResponse[Row, Resp any](method string, page persist.Page[Row], err error, toResp func(*Row) *Resp) (any, error) {
	if err != nil {
		return nil, Fail(method, err, rpcserver.CodeInternal)
	}
	out := make([]*Resp, len(page.Rows))
	for i := range page.Rows {
		out[i] = toResp(&page.Rows[i])
	}
	return Envelope(out, page.Total), nil
}

// Envelope is the {data, total} answer of every list method: a page of rows
// in their wire shape, and the count of all rows matching.
func Envelope[T any](data []T, total int64) map[string]any {
	return map[string]any{"data": data, "total": total}
}
