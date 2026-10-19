// Package failures tells the failures of something retried apart, which is what keeps a
// failure that recurs at every retry to one report.
package failures

import "errors"

// Last is the last failure: one is news where its cause is another than the last one's,
// and a success forgets the last.
type Last struct {
	cause string
}

// News says whether err is news, and keeps its cause as the last: the error it wraps, as
// far down as it goes, which an attempt's own details (a path, an address) are not part of.
func (l *Last) News(err error) bool {
	cause := err
	for next := errors.Unwrap(cause); next != nil; next = errors.Unwrap(cause) {
		cause = next
	}
	news := cause.Error() != l.cause
	l.cause = cause.Error()
	return news
}

// Succeeded forgets the last failure.
func (l *Last) Succeeded() {
	l.cause = ""
}
