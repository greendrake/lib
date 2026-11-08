// Package i18n is what a Go service serving a localised product needs at run
// time, one package each: messageformat renders a message, plurals says how a
// locale counts, negotiate and locales resolve the locale a request asks for
// against a shipped set, and catalogue renders on the server from the message
// catalogue a frontend is built from.
package i18n
