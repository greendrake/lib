package format

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"greendrake.nz/go/translate/internal/files"
)

// yaml-catalogue: one message catalogue per locale, nested keys addressed by
// their dotted path. The comments above a key are the authoring notes — "the
// store names as each vendor writes them", "must be closed by whitespace" —
// and are handed to the translator as context, which is what they already
// were for a human one; a section's notes reach every key under it.

type catalogue struct {
	pass   string
	spec   Spec
	env    Env
	master *yaml.Node
	units  []Unit
}

func openCatalogue(pass string, spec Spec, env Env) (Store, error) {
	c := &catalogue{pass: pass, spec: spec, env: env}
	master, entries, err := readCatalogue(env.path(spec.Path, env.Default))
	if err != nil {
		return nil, err
	}
	if master == nil {
		return nil, fmt.Errorf("%s: the master catalogue %s does not exist", pass, env.path(spec.Path, env.Default))
	}
	c.master = master
	for _, entry := range entries {
		c.units = append(c.units, Unit{
			ID:      UnitID(pass, entry.key),
			Pass:    pass,
			Key:     entry.key,
			Text:    entry.text,
			Context: entry.notes,
			Checks:  checksOf(spec, c.syntaxOf(entry.key), Field{}),
		})
	}
	return c, nil
}

// syntaxOf is the syntax of the renderer that reads a key: an override's where
// the key falls under its prefix, the source's otherwise.
func (c *catalogue) syntaxOf(key string) string {
	for _, override := range c.spec.SyntaxOverrides {
		if strings.HasPrefix(key, override.Prefix) {
			return override.Syntax
		}
	}
	return c.spec.Syntax
}

func (c *catalogue) Units() []Unit { return c.units }

func (c *catalogue) Existing(locale string, _ map[string][]string) (map[string]string, error) {
	_, entries, err := readCatalogue(c.env.path(c.spec.Path, locale))
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, entry := range entries {
		out[UnitID(c.pass, entry.key)] = entry.text
	}
	return out, nil
}

// Write clones the master's node tree and substitutes the locale's strings.
// Cloning rather than rebuilding is what keeps the key order and the nesting
// identical to the master's, so the two files diff against each other line
// for line; the master's comments are authoring notes and stay behind.
func (c *catalogue) Write(locale string, translations map[string]string, sink files.Sink) ([]string, error) {
	root := c.translateNode(c.master.Content[0], "", translations)
	if root == nil {
		return nil, nil
	}
	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
	return files.WriteYAML(sink, c.env.path(c.spec.Path, locale), c.env.Header, doc)
}

// translateNode returns the locale's copy of one mapping, or nil when nothing
// under it is translated.
func (c *catalogue) translateNode(node *yaml.Node, prefix string, translations map[string]string) *yaml.Node {
	out := &yaml.Node{Kind: yaml.MappingNode}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		path := dotted(prefix, key.Value)
		switch value.Kind {
		case yaml.ScalarNode:
			if text, ok := translations[UnitID(c.pass, path)]; ok {
				out.Content = append(out.Content, files.Key(key.Value), files.Scalar(text))
			}
		case yaml.MappingNode:
			if child := c.translateNode(value, path, translations); child != nil {
				out.Content = append(out.Content, files.Key(key.Value), child)
			}
		}
	}
	if len(out.Content) == 0 {
		return nil
	}
	return out
}

func (c *catalogue) Drop(locale string) ([]string, error) {
	return files.Remove([]string{c.env.path(c.spec.Path, locale)})
}

// catalogueEntry is one leaf string of a catalogue.
type catalogueEntry struct{ key, text, notes string }

// readCatalogue parses one catalogue into its document node and its leaf
// strings in file order. A catalogue that does not exist is a nil node and no
// entries: a locale not yet produced.
func readCatalogue(path string) (*yaml.Node, []catalogueEntry, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read catalogue: %w", err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("parse catalogue %s: %w", path, err)
	}
	if len(doc.Content) == 0 {
		return nil, nil, fmt.Errorf("%s: empty catalogue", path)
	}
	var entries []catalogueEntry
	var walk func(node *yaml.Node, prefix, inherited string)
	walk = func(node *yaml.Node, prefix, inherited string) {
		if node.Kind != yaml.MappingNode {
			return
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			path := dotted(prefix, key.Value)
			notes := strings.TrimSpace(strings.Join([]string{inherited, comments(key, value)}, "\n"))
			switch value.Kind {
			case yaml.ScalarNode:
				entries = append(entries, catalogueEntry{key: path, text: value.Value, notes: notes})
			case yaml.MappingNode:
				walk(value, path, notes)
			}
		}
	}
	walk(doc.Content[0], "", "")
	return &doc, entries, nil
}

func dotted(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

// comments gathers whatever was written above or beside a catalogue entry.
func comments(key, value *yaml.Node) string {
	var parts []string
	for _, c := range []string{key.HeadComment, key.LineComment, value.LineComment, value.HeadComment} {
		if c = strings.TrimSpace(strings.ReplaceAll(c, "#", "")); c != "" {
			parts = append(parts, c)
		}
	}
	return strings.Join(parts, " ")
}
