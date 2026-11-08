package llm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// grammarOf compiles a request's JSON Schema into the EBNF grammar (the GBNF
// dialect xgrammar and llama.cpp read) of the JSON it admits, written one way:
// keys in the order the schema declares them, `, ` and `: ` the only
// whitespace, and strings with every escape JSON has but `\u`.
//
// That last is the point. A backend compiling the schema itself allows a
// string to spell any character as a \u escape, and a model that drifts into
// writing its text that way spends several tokens a character, loses its words
// doing so and runs into the cap mid-string. Every text can be written
// without one — a character as itself — so the grammar forbids nothing a
// completion needs. Free whitespace goes for the same reason: a model
// indenting its answer spends tokens on nothing.
//
// It covers the schema a caller of this package writes — objects whose every
// property is required and nothing else allowed, arrays, strings with or
// without an enum, booleans — and refuses any other keyword rather than
// compile a grammar looser or stricter than the schema WithValidation holds a
// completion to.
func grammarOf(schema json.RawMessage) (string, error) {
	var root schemaNode
	if err := json.Unmarshal(schema, &root); err != nil {
		return "", fmt.Errorf("grammar: %w", err)
	}
	g := &grammar{}
	expr, err := g.compile(root, "root")
	if err != nil {
		return "", fmt.Errorf("grammar: %w", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "root ::= %s\n", expr)
	for _, rule := range g.rules {
		b.WriteString(rule + "\n")
	}
	b.WriteString(`string ::= "\"" char* "\""` + "\n")
	b.WriteString(`char ::= [^"\\\x00-\x1F] | "\\" ["\\/bfnrt]` + "\n")
	return b.String(), nil
}

// schemaNode is one schema, as far as grammarOf reads one.
type schemaNode struct {
	Type                 string
	Properties           []property
	Required             []string
	AdditionalProperties *bool
	Items                *schemaNode
	Enum                 []string
	hasEnum              bool
}

type property struct {
	name   string
	schema schemaNode
}

// schemaKeywords are the keywords grammarOf compiles; description is read past,
// since it shapes no completion.
var schemaKeywords = []string{"type", "properties", "required", "additionalProperties", "items", "enum", "description"}

func (n *schemaNode) UnmarshalJSON(data []byte) error {
	var keywords map[string]json.RawMessage
	if err := json.Unmarshal(data, &keywords); err != nil {
		return err
	}
	for keyword := range keywords {
		if !slices.Contains(schemaKeywords, keyword) {
			return fmt.Errorf("schema keyword %q is not supported", keyword)
		}
	}
	fields := []struct {
		keyword string
		into    any
	}{
		{"type", &n.Type},
		{"required", &n.Required},
		{"additionalProperties", &n.AdditionalProperties},
		{"items", &n.Items},
		{"enum", &n.Enum},
	}
	for _, field := range fields {
		if raw, ok := keywords[field.keyword]; ok {
			if err := json.Unmarshal(raw, field.into); err != nil {
				return fmt.Errorf("schema keyword %q: %w", field.keyword, err)
			}
		}
	}
	_, n.hasEnum = keywords["enum"]
	if raw, ok := keywords["properties"]; ok {
		properties, err := orderedProperties(raw)
		if err != nil {
			return err
		}
		n.Properties = properties
	}
	return nil
}

// orderedProperties reads a properties object in the order it is written,
// which is the order the grammar writes the keys in.
func orderedProperties(raw json.RawMessage) ([]property, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if token, err := dec.Token(); err != nil || token != json.Delim('{') {
		return nil, errors.New(`schema keyword "properties" is not an object`)
	}
	var out []property
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var p property
		p.name = token.(string)
		if err := dec.Decode(&p.schema); err != nil {
			return nil, fmt.Errorf("property %q: %w", p.name, err)
		}
		out = append(out, p)
	}
	return out, nil
}

// grammar collects the rules a schema compiles to beyond root: one per array's
// item, which the array's expression names twice.
type grammar struct {
	rules []string
}

// compile is the expression a schema's JSON is written as. at says where the
// schema is, for a refusal to point at.
func (g *grammar) compile(n schemaNode, at string) (string, error) {
	if n.hasEnum && n.Type != "string" {
		return "", fmt.Errorf("%s: an enum is supported on strings only", at)
	}
	switch n.Type {
	case "string":
		if !n.hasEnum {
			return "string", nil
		}
		if len(n.Enum) == 0 {
			return "", fmt.Errorf("%s: an empty enum admits nothing", at)
		}
		alternatives := make([]string, len(n.Enum))
		for i, value := range n.Enum {
			alternatives[i] = literal(jsonText(value))
		}
		return "(" + strings.Join(alternatives, " | ") + ")", nil
	case "boolean":
		return `("true" | "false")`, nil
	case "array":
		if n.Items == nil {
			return "", fmt.Errorf("%s: an array needs items", at)
		}
		item, err := g.compile(*n.Items, at+"[]")
		if err != nil {
			return "", err
		}
		name := fmt.Sprintf("item%d", len(g.rules)+1)
		g.rules = append(g.rules, name+" ::= "+item)
		return fmt.Sprintf(`"[" (%s (", " %s)*)? "]"`, name, name), nil
	case "object":
		if n.AdditionalProperties == nil || *n.AdditionalProperties {
			return "", fmt.Errorf("%s: an object must set additionalProperties to false", at)
		}
		var names []string
		for _, p := range n.Properties {
			names = append(names, p.name)
		}
		if !sameSet(names, n.Required) {
			return "", fmt.Errorf("%s: every property must be required, and nothing else", at)
		}
		var s sequence
		s.text("{")
		for i, p := range n.Properties {
			if i > 0 {
				s.text(", ")
			}
			value, err := g.compile(p.schema, at+"."+p.name)
			if err != nil {
				return "", err
			}
			s.text(jsonText(p.name) + ": ")
			s.expr(value)
		}
		s.text("}")
		return s.String(), nil
	}
	return "", fmt.Errorf("%s: type %q is not supported", at, n.Type)
}

// sequence is an expression built part by part, adjacent literal text merged
// into one literal.
type sequence struct {
	parts   []string
	pending strings.Builder
}

func (s *sequence) text(t string) { s.pending.WriteString(t) }

func (s *sequence) expr(e string) {
	s.flush()
	s.parts = append(s.parts, e)
}

func (s *sequence) flush() {
	if s.pending.Len() > 0 {
		s.parts = append(s.parts, literal(s.pending.String()))
		s.pending.Reset()
	}
}

func (s *sequence) String() string {
	s.flush()
	return strings.Join(s.parts, " ")
}

// jsonText is a string as a completion writes it: JSON, with no \u escape the
// string grammar would refuse — so no HTML escaping.
func jsonText(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // a string always encodes
	return strings.TrimSuffix(b.String(), "\n")
}

// literal quotes text as an EBNF string literal.
func literal(text string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(text) + `"`
}

func sameSet(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}
