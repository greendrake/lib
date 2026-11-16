package rpckit

import "time"

// TimeFormat is the millisecond-precision RFC 3339 spelling of a timestamp a
// method answers as a string.
const TimeFormat = "2006-01-02T15:04:05.000Z07:00"

// Stamp is t as a method answers it: in UTC, as TimeFormat spells it.
func Stamp(t time.Time) string { return t.UTC().Format(TimeFormat) }

// StampOf is Stamp of t, null for none.
func StampOf(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := Stamp(*t)
	return &s
}
