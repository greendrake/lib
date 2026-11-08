package files

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
)

// XMLPreamble is what an XML file the translator writes opens with: the
// declaration, then the header as one comment. A comment cannot carry `--`,
// so a header that does is refused.
func XMLPreamble(header string) (string, error) {
	if strings.Contains(header, "--") {
		return "", fmt.Errorf("the generated-file header carries `--`, which an XML comment cannot: %q", header)
	}
	var out strings.Builder
	out.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n<!--\n")
	for _, line := range strings.Split(header, "\n") {
		out.WriteString("    " + line + "\n")
	}
	out.WriteString("-->\n")
	return out.String(), nil
}

// WriteXML writes an XML document — its preamble under header, then body —
// into a sink, once it has parsed back whole.
func WriteXML(sink Sink, path, header, body string) ([]string, error) {
	preamble, err := XMLPreamble(header)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	out := preamble + body
	decoder := xml.NewDecoder(strings.NewReader(out))
	for {
		_, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: rendered XML does not parse back: %w", path, err)
		}
	}
	return sink(path, out)
}

// WriteJSON writes a JSON document into a sink as Apple's pretty printer lays
// one out — the layout Xcode saves a String Catalog in, so that a save from
// Xcode moves no byte of it: two-space indents, `"key" : value`, every
// object's keys in code-point order, an empty object or array as its brackets
// with a blank line between, every character as itself but `"`, `\` and the C0
// controls, and no newline at the end. value is what encoding/json decodes a
// document into, numbers as json.Number. It is written once it has parsed
// back.
func WriteJSON(sink Sink, path string, value any) ([]string, error) {
	var out strings.Builder
	if err := pretty(&out, value, ""); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var readBack any
	if err := json.Unmarshal([]byte(out.String()), &readBack); err != nil {
		return nil, fmt.Errorf("%s: rendered JSON does not parse back: %w", path, err)
	}
	return sink(path, out.String())
}

// ReadJSON decodes a JSON document as WriteJSON takes one, numbers as written.
func ReadJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("more than one JSON value")
	}
	return value, nil
}

func pretty(out *strings.Builder, value any, indent string) error {
	inner := indent + "  "
	switch v := value.(type) {
	case map[string]any:
		keys := slices.Sorted(maps.Keys(v))
		return bracketed(out, "{", "}", indent, len(keys), func(i int) error {
			out.WriteString(inner + quote(keys[i]) + " : ")
			return pretty(out, v[keys[i]], inner)
		})
	case []any:
		return bracketed(out, "[", "]", indent, len(v), func(i int) error {
			out.WriteString(inner)
			return pretty(out, v[i], inner)
		})
	case string:
		out.WriteString(quote(v))
	case json.Number:
		out.WriteString(v.String())
	case bool:
		out.WriteString(fmt.Sprint(v))
	case nil:
		out.WriteString("null")
	default:
		return fmt.Errorf("%T is not a JSON value", value)
	}
	return nil
}

// bracketed writes n members between brackets, one per line.
func bracketed(out *strings.Builder, open, close, indent string, n int, member func(i int) error) error {
	out.WriteString(open + "\n")
	if n == 0 {
		out.WriteString("\n")
	}
	for i := range n {
		if err := member(i); err != nil {
			return err
		}
		if i < n-1 {
			out.WriteString(",")
		}
		out.WriteString("\n")
	}
	out.WriteString(indent + close)
	return nil
}

// shortEscapes are the controls written as a letter.
var shortEscapes = map[rune]string{'\n': `\n`, '\r': `\r`, '\t': `\t`, '\b': `\b`, '\f': `\f`}

// quote writes a string as Apple's pretty printer does.
func quote(s string) string {
	var out strings.Builder
	out.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			out.WriteRune('\\')
			out.WriteRune(r)
		case shortEscapes[r] != "":
			out.WriteString(shortEscapes[r])
		case r < 0x20:
			fmt.Fprintf(&out, `\u%04x`, r)
		default:
			out.WriteRune(r)
		}
	}
	out.WriteByte('"')
	return out.String()
}
