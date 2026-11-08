package messageformat

import (
	"html"
	"strings"
)

// SafeHTML marks an argument value as trusted, pre-rendered HTML that EscapeArgs
// passes through verbatim (e.g. a server-built list of links). Wrap a value in
// SafeHTML only after escaping its own untrusted parts yourself; plain string
// args are escaped automatically.
type SafeHTML string

// Alternative is an argument value with a rendering for each format: Text for
// a text render, which Render takes as it is, and HTML for an HTML body, which
// EscapeArgs takes as it takes a SafeHTML value. A message someone wrote
// carries both: as written for a push, escaped with its links linked for an
// email.
type Alternative struct {
	Text string
	HTML SafeHTML
}

// EscapeArgs returns a copy of args prepared for rendering into an HTML email
// body: plain string values go through EscapeText; SafeHTML values, and the
// HTML of Alternative ones, pass through untouched; other types (numbers,
// currency amounts) are unchanged. Rendering the subject uses the raw args,
// not these — the subject is a plain header, not HTML.
func EscapeArgs(args map[string]any) map[string]any {
	out := make(map[string]any, len(args))
	for k, v := range args {
		switch s := v.(type) {
		case SafeHTML:
			out[k] = string(s)
		case Alternative:
			out[k] = string(s.HTML)
		case string:
			out[k] = EscapeText(s)
		default:
			out[k] = v
		}
	}
	return out
}

// EscapeText renders untrusted plain text as HTML: escaped, with newlines
// becoming <br> so line breaks survive. Callers assembling their own SafeHTML
// out of untrusted parts use it directly.
func EscapeText(s string) string {
	return strings.ReplaceAll(html.EscapeString(s), "\n", "<br>")
}

// Anchor renders a link to href reading text, both untrusted: escaped, the
// href for its quoted attribute.
func Anchor(href, text string) string {
	return `<a href="` + html.EscapeString(href) + `">` + html.EscapeString(text) + `</a>`
}
