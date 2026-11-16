package persist

import (
	"errors"
	"slices"
	"time"
)

// ErrInvalidSortField is returned by a list read whose ListArgs.SortBy is
// not in the entity's sortable set. SQL adapters interpolate the resolved
// field into ORDER BY, so set membership is a hard precondition, not advice.
var ErrInvalidSortField = errors.New("invalid sort field")

// Entity describes one listable entity's storage surface: its
// collection/table name, the field substring-searched by ListArgs.Q, the
// default sort field (descending), and the set of sortable fields. Field
// names are the storage names, identical across a library's adapters.
type Entity struct {
	Name        string
	QField      string
	DefaultSort string
	sortable    map[string]bool
}

// NewEntity declares an entity; sortable lists every field ListArgs.SortBy
// may name.
func NewEntity(name, qField, defaultSort string, sortable ...string) Entity {
	set := make(map[string]bool, len(sortable))
	for _, f := range sortable {
		set[f] = true
	}
	return Entity{Name: name, QField: qField, DefaultSort: defaultSort, sortable: set}
}

// ListArgs bundles the inputs of a paginated list read. Q is a
// case-insensitive substring match on the entity's QField, with the
// metacharacters of the underlying matcher (regex, LIKE) taken literally;
// SortBy is a field from the entity's sortable set ("" = the entity's
// default, descending); SortDir is "asc", anything else means descending.
// Offset/Limit <= 0 mean unbounded.
type ListArgs struct {
	Q       string
	SortBy  string
	SortDir string
	Offset  int
	Limit   int
}

// Sort resolves the args' sort against the entity: applies the default,
// validates set membership, and folds the direction to a flag. Every adapter
// calls this first — resolution and validation live here so the contract
// cannot drift between engines.
func (a ListArgs) Sort(e Entity) (field string, desc bool, err error) {
	if a.SortBy == "" {
		return e.DefaultSort, true, nil
	}
	if !e.sortable[a.SortBy] {
		return "", false, ErrInvalidSortField
	}
	return a.SortBy, a.SortDir != "asc", nil
}

// Page is one page of a list read: its rows plus the unpaginated total of
// rows matching the read's filter.
type Page[T any] struct {
	Rows  []T
	Total int64
}

// Slice is the page offset and limit (<= 0 unbounded) ask for of rows, all
// the rows matching a read in its order: how an in-memory adapter answers
// one.
func Slice[T any](rows []T, offset, limit int) Page[T] {
	total := int64(len(rows))
	rows = rows[min(max(offset, 0), len(rows)):]
	if limit > 0 {
		rows = rows[:min(limit, len(rows))]
	}
	return Page[T]{Rows: rows, Total: total}
}

// Newest is a copy of rows, stored in order, the latest by at first — of
// two at the same instant, the later stored: how an in-memory adapter
// answers a newest-first read.
func Newest[T any](rows []T, at func(T) time.Time) []T {
	out := slices.Clone(rows)
	slices.Reverse(out)
	slices.SortStableFunc(out, func(a, b T) int { return at(b).Compare(at(a)) })
	return out
}
