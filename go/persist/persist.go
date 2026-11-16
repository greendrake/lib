// Package persist is the contract storage ports share: the sentinel errors
// adapters translate their driver's failures into, the paginated-list
// vocabulary (ListArgs, Entity, Page), and Transactor, the unit of work
// through which writes to several libraries' stores land together.
//
// It is for code built as ports and adapters: a library's core declares the
// store it needs as an interface (its port), adapters implement it over a
// database driver or in memory, and the application assembling the libraries
// (the host) picks the adapters.
//
// It has no dependencies, so a core can declare its port in these terms
// without pulling in any driver.
package persist

import (
	"context"
	"errors"
)

var (
	// ErrNotFound is returned by a store read or targeted write when no row
	// matches. Libraries wrap it with the entity's name
	// (fmt.Errorf("order %w", persist.ErrNotFound)); callers test it
	// with errors.Is.
	ErrNotFound = errors.New("not found")

	// ErrDuplicate is returned by a store write that would violate a
	// uniqueness guarantee of the port (an existing id, a unique field).
	ErrDuplicate = errors.New("already exists")
)

// Transactor runs fn as one atomic unit. Every store method fn calls with
// the ctx it is handed participates in the unit: all of their writes commit
// together when fn returns nil, and none of them do when fn returns an error
// (which RunInTransaction returns). A call made with any other ctx runs
// outside the unit.
//
// fn may run more than once — an adapter retries a unit its engine aborted
// for a transient conflict — so it must not have effects outside the stores
// it writes through; work that must happen exactly once after the commit
// belongs after RunInTransaction returns.
type Transactor interface {
	RunInTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// PassThrough is a Transactor running a unit as plain calls, for stores with
// no transactions to join — in-memory stores.
type PassThrough struct{}

func (PassThrough) RunInTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}
