package format

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"greendrake.nz/go/translate/internal/files"
)

// yaml-rows: a list of rows, each one message written in several fields — a
// subject and a body — under an id and a locale. The master file holds the
// default locale's rows, and may hold others'; each locale's translations go
// to a file of its own, shaped like the master. A row's fields are one message,
// so no batch splits them.

// RowKeys name the YAML keys a row carries its id and its locale under.
type RowKeys struct {
	ID     string `yaml:"id"`
	Locale string `yaml:"locale"`
}

// Field is one translatable field of a row.
type Field struct {
	Name string `yaml:"-"`
	// Describe says what the field is, for the context template's {describe}.
	Describe string `yaml:"describe"`
	Markup   string `yaml:"markup"`
	Shape    string `yaml:"shape"`
	// Limit is the most characters the field's translation may have, zero for
	// none. It reaches the prompt, so it is policy like the rest.
	Limit int `yaml:"limit"`
}

// Fields are a row's translatable fields in the order the manifest declares
// them — which is the order they are written in, so they decode from a
// mapping without losing it.
type Fields []Field

func (f *Fields) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("fields must be a mapping of field name to declaration")
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		var field Field
		if err := node.Content[i+1].Decode(&field); err != nil {
			return fmt.Errorf("field %s: %w", node.Content[i].Value, err)
		}
		field.Name = node.Content[i].Value
		*f = append(*f, field)
	}
	return nil
}

// defaultRowContext is the library's wording for a host that declares none.
const defaultRowContext = `{describe} of row "{id}". Every field of the same row is in this batch — keep them consistent.`

type rows struct {
	pass  string
	spec  Spec
	env   Env
	units []Unit
	// ids are the master's row ids, in file order, and inline its rows in
	// other locales.
	ids    []string
	inline []map[string]string
}

func openRows(pass string, spec Spec, env Env) (Store, error) {
	r := &rows{pass: pass, spec: spec, env: env}
	master, err := r.read(env.path(spec.Master, env.Default))
	if err != nil {
		return nil, err
	}
	if master == nil {
		return nil, fmt.Errorf("%s: the master %s does not exist", pass, spec.Master)
	}
	context := firstNonEmpty(spec.Context, defaultRowContext)
	for _, row := range master {
		id := row[spec.Row.ID]
		if row[spec.Row.Locale] != env.Default {
			r.inline = append(r.inline, row)
			continue
		}
		r.ids = append(r.ids, id)
		for _, field := range spec.Fields {
			text, ok := row[field.Name]
			if !ok || text == "" {
				return nil, fmt.Errorf("%s: row %s has no %s", pass, id, field.Name)
			}
			key := id + "." + field.Name
			r.units = append(r.units, Unit{
				ID:      UnitID(pass, key),
				Pass:    pass,
				Key:     key,
				Text:    text,
				Context: expand(context, map[string]string{"describe": field.Describe, "id": id, "field": field.Name}),
				Checks:  checksOf(spec, spec.Syntax, field),
				File:    spec.Master,
				Bundle:  id,
			})
		}
	}
	if len(r.ids) == 0 {
		return nil, fmt.Errorf("%s: %s has no %s row", pass, spec.Master, env.Default)
	}
	return r, nil
}

// read parses a rows file into its rows' scalar values, as written. A file
// that does not exist is nil.
func (r *rows) read(path string) ([]map[string]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: read %s: %w", r.pass, path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s: parse %s: %w", r.pass, path, err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("%s: %s is not a list of rows", r.pass, path)
	}
	var out []map[string]string
	for _, node := range doc.Content[0].Content {
		if node.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("%s: %s: every row is a mapping", r.pass, path)
		}
		row := map[string]string{}
		for i := 0; i+1 < len(node.Content); i += 2 {
			row[node.Content[i].Value] = node.Content[i+1].Value
		}
		if row[r.spec.Row.ID] == "" || row[r.spec.Row.Locale] == "" {
			return nil, fmt.Errorf("%s: %s: every row carries `%s` and `%s`", r.pass, path, r.spec.Row.ID, r.spec.Row.Locale)
		}
		out = append(out, row)
	}
	return out, nil
}

func (r *rows) Units() []Unit { return r.units }

// Existing is the locale's rows inline in the master and in its own file. A
// row the master no longer defines names nothing and is passed over.
func (r *rows) Existing(locale string, _ map[string][]string) (map[string]string, error) {
	own, err := r.read(r.env.path(r.spec.Path, locale))
	if err != nil {
		return nil, err
	}
	for _, row := range own {
		if row[r.spec.Row.Locale] != locale {
			return nil, fmt.Errorf("%s: %s: row %s declares locale %q, which is not the file's", r.pass, r.env.path(r.spec.Path, locale), row[r.spec.Row.ID], row[r.spec.Row.Locale])
		}
	}
	defined := map[string]bool{}
	for _, id := range r.ids {
		defined[id] = true
	}
	out := map[string]string{}
	for _, row := range append(append([]map[string]string{}, r.inline...), own...) {
		if row[r.spec.Row.Locale] != locale || !defined[row[r.spec.Row.ID]] {
			continue
		}
		for _, field := range r.spec.Fields {
			if text := row[field.Name]; text != "" {
				out[UnitID(r.pass, row[r.spec.Row.ID]+"."+field.Name)] = text
			}
		}
	}
	return out, nil
}

// Write emits every row all of whose fields are translated, in master order:
// half a row would be a message sent in two languages at once.
func (r *rows) Write(locale string, translations map[string]string, sink files.Sink) ([]string, error) {
	list := &yaml.Node{Kind: yaml.SequenceNode}
	for _, id := range r.ids {
		row := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
			files.Key(r.spec.Row.ID), files.Scalar(id),
			files.Key(r.spec.Row.Locale), files.Scalar(locale),
		}}
		complete := true
		for _, field := range r.spec.Fields {
			text, ok := translations[UnitID(r.pass, id+"."+field.Name)]
			if !ok {
				complete = false
				break
			}
			row.Content = append(row.Content, files.Key(field.Name), files.Scalar(text))
		}
		if complete {
			list.Content = append(list.Content, row)
		}
	}
	if len(list.Content) == 0 {
		return nil, nil
	}
	return files.WriteYAML(sink, r.env.path(r.spec.Path, locale), r.env.Header, list)
}

func (r *rows) Drop(locale string) ([]string, error) {
	return files.Remove([]string{r.env.path(r.spec.Path, locale)})
}
