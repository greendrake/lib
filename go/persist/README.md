# greendrake.nz/go/persist

The few types and errors that storage code agrees on when a program is built
from several libraries, each with stores of its own: two errors, one shape
for list reads, and one way to make writes atomic. No dependencies.

```sh
go get greendrake.nz/go/persist
```

## Why

Take a program built from several libraries. Each library declares the
stores it needs as interfaces, and adapters implement them over a database
driver or in memory. Left alone, each library answers the same questions its
own way, and the code above the libraries pays for it. An API layer cannot
answer "not found" without knowing every library and every driver. Every
list needs a handler and table code of its own. And two libraries' writes
have no way to commit together: neither library knows the other, or the
database.

persist answers those questions once: two errors that every adapter
translates its driver's errors into, one shape for list reads, and a
transaction carried in the `context.Context`. It imports nothing, so a
library uses all of this without pulling in a driver.

## Trade-offs

- **Offset paging, with a total.** A list read answers any range of the
  rows, and the count of all rows matching. That suits a table with a
  scrollbar or page numbers. It costs a count on every read, a deep offset
  gets slower as the table grows, and rows shift between two reads when the
  data changes. There is no cursor paging here.
- **Search is one field, by substring.** `Q` matches literally, ignoring
  case, anywhere in one field of the entity. It is not full-text search, and
  an ordinary index does not serve it.
- **The transaction is carried in the context.** That keeps the libraries
  unaware of each other, but it is implicit: a store method called with any
  other context runs outside the transaction, without complaint. A
  transaction covers the stores of one database, the one its `Transactor`
  belongs to.
- **A contract, not a toolkit.** There is no query builder, no mapping and
  no migrations: each library still writes its adapters by hand.

## Overview

persist is for code built as ports and adapters. A library's core declares
the store it needs as an interface: its port. Adapters implement the port,
over a database driver or in memory. The application that assembles the
libraries — the host — picks the adapters.

- **What "not found" looks like.** Every driver has its own error for a
  missing row and for a broken uniqueness rule. An adapter translates its
  driver's errors into `ErrNotFound` and `ErrDuplicate`, and callers test
  with `errors.Is`.
- **How a list is asked for.** Search text, an order, a range of rows, and
  the total count that page numbers need. With one `ListArgs` in and one
  `Page[T]` out, one generic handler and one table component serve every
  list.
- **Which fields a client may sort by.** A SQL adapter puts the sort field
  into `ORDER BY` as text, so a field name that came from a client has to be
  checked against a fixed set first. `ListArgs.Sort` does that in one place,
  for every adapter.
- **How two libraries' writes commit together.** Neither library knows the
  other, or the database. A `Transactor` carries the unit of work in the
  `context.Context`: the host runs both writes inside one
  `RunInTransaction`, and every store method called with that context joins
  the unit.

```go
// A library's port, in persist's terms.
type Orders interface {
	Get(ctx context.Context, id string) (*Order, error) // persist.ErrNotFound
	Insert(ctx context.Context, o *Order) error         // persist.ErrDuplicate
	List(ctx context.Context, args persist.ListArgs) (persist.Page[Order], error)
}

// What its adapters list by: the table, the searched field, the default
// sort, and every field a client may sort by.
var OrderEntity = persist.NewEntity("orders", "reference", "created_at", "created_at", "total")

// The host commits two libraries' writes together.
err := tx.RunInTransaction(ctx, func(ctx context.Context) error {
	if err := orders.Insert(ctx, order); err != nil {
		return err
	}
	return stock.Reserve(ctx, order.Items)
})
```

## Surface

- `ErrNotFound` and `ErrDuplicate` are the sentinel errors that adapters
  translate their driver's failures into. A library wraps them with the
  entity's name (`fmt.Errorf("order %w", persist.ErrNotFound)`), and callers
  test with `errors.Is`. These two are the outcomes callers branch on. Any
  other error an adapter returns is a failure.
- `ListArgs` is what a list read is asked: `Q`, the search text; `SortBy`,
  one of the entity's sortable fields, or empty for its default sort,
  descending; `SortDir`, `asc` or anything else for descending; `Offset` and
  `Limit`, where zero or less means unbounded.
- `Entity`, from `NewEntity`, describes what an adapter lists: the table or
  collection, the field `Q` searches, the default sort field, and the fields
  a client may sort by.
- `ListArgs.Sort(entity)` resolves the order to a field and a direction, and
  answers `ErrInvalidSortField` for a field outside the entity's set. Every
  adapter calls it first.
- `Page[T]` is a list read's result: the rows, and the total of all rows
  matching. For in-memory adapters, `Slice(rows, offset, limit)` pages rows
  already in order, and `Newest(rows, at)` orders them latest first. Of two
  rows at the same instant, the one stored later comes first.
- `Transactor` has one method, `RunInTransaction(ctx, fn)`. Every store
  method that `fn` calls with the context it is handed joins one atomic
  unit: all of the writes commit when `fn` returns nil, and none when it
  returns an error. An adapter may run `fn` again after a transient
  conflict. So `fn` must do nothing outside those stores that cannot be
  repeated, and work that must happen exactly once belongs after
  `RunInTransaction` returns. `PassThrough` runs a unit as plain calls, for
  in-memory stores.
- `persisttest.Faults` is for tests. It sets in advance what named
  operations do: an outcome queued for each call, an error for every call,
  or a concurrent write that runs just before the operation. A library's
  test helpers wrap the methods of its in-memory stores in
  `persisttest.Call` or `persisttest.Run`, which ask the `Faults` first.
