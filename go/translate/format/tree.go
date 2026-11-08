package format

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"greendrake.nz/go/translate/internal/files"
)

// yaml-tree: vocabularies, one file each — a key, a label, and a tree of nodes
// each carrying a code and its names per locale, nested under a children key.
// A node's id is composed from the tree's key and its code; its ancestry is
// the authored nesting. The master is the files' own default-locale names; a
// locale's names may sit inline beside them, and the translator writes them to
// a sidecar per locale shaped {<key>: {label, nodes: {<code>: name}}}, which
// wins over the inline ones.

// TreeKeys name the YAML keys a tree file is written with.
type TreeKeys struct {
	Key      string `yaml:"key"`
	Label    string `yaml:"label"`
	Nodes    string `yaml:"nodes"`
	Code     string `yaml:"code"`
	Names    string `yaml:"names"`
	Children string `yaml:"children"`
	// SkipNodeNames names a boolean a file sets when its node names are names
	// rather than words — brands, models — so its label is translated and its
	// nodes are not.
	SkipNodeNames string `yaml:"skip_node_names"`
}

func (k TreeKeys) validate(pass string) error {
	for name, value := range map[string]string{"key": k.Key, "label": k.Label, "nodes": k.Nodes, "code": k.Code, "names": k.Names, "children": k.Children} {
		if value == "" {
			return fmt.Errorf("%s: `keys.%s` is required", pass, name)
		}
	}
	return nil
}

// Library wording, for a host that declares none. The ancestry is appended to
// a node's context by the format, since only it knows the nesting.
const (
	defaultLabelContext = `The name of the "{key}" vocabulary.`
	defaultNodeContext  = `A value of the "{key}" vocabulary (label: "{label}").`
)

type tree struct {
	pass  string
	spec  Spec
	env   Env
	units []Unit
	// trees are the master's vocabularies, in file order.
	trees []vocabulary
}

// vocabulary is one parsed tree file.
type vocabulary struct {
	key   string
	label map[string]string
	skip  bool
	nodes []treeNode
}

// treeNode is one node, flattened in authored order.
type treeNode struct {
	id, code  string
	names     map[string]string
	ancestors []string // the ids above it, outermost first
}

func openTree(pass string, spec Spec, env Env) (Store, error) {
	t := &tree{pass: pass, spec: spec, env: env}
	paths, err := filepath.Glob(filepath.Join(env.Root, spec.Path))
	if err != nil {
		return nil, fmt.Errorf("%s: glob %s: %w", pass, spec.Path, err)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("%s: no files match %s", pass, spec.Path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		v, err := t.read(path)
		if err != nil {
			return nil, err
		}
		t.trees = append(t.trees, v)
	}

	labelContext := firstNonEmpty(spec.LabelContext, defaultLabelContext)
	nodeContext := firstNonEmpty(spec.Context, defaultNodeContext)
	for _, v := range t.trees {
		label := v.label[env.Default]
		if strings.TrimSpace(label) == "" {
			return nil, fmt.Errorf("%s: %s has no %s label", pass, v.key, env.Default)
		}
		t.units = append(t.units, t.unit(v.key, v.key, label, expand(labelContext, map[string]string{"key": v.key})))
		if v.skip {
			continue
		}
		byID := map[string]string{}
		for _, node := range v.nodes {
			byID[node.id] = node.names[env.Default]
		}
		for _, node := range v.nodes {
			name := node.names[env.Default]
			if strings.TrimSpace(name) == "" {
				return nil, fmt.Errorf("%s: node %s has no %s name", pass, node.id, env.Default)
			}
			context := expand(nodeContext, map[string]string{"key": v.key, "label": label})
			if len(node.ancestors) > 0 {
				chain := make([]string, len(node.ancestors))
				for i, ancestor := range node.ancestors {
					chain[i] = byID[ancestor]
				}
				context += " Inside: " + strings.Join(chain, " › ") + "."
			}
			t.units = append(t.units, t.unit(v.key, node.id, name, context))
		}
	}
	return t, nil
}

func (t *tree) unit(file, key, text, context string) Unit {
	return Unit{
		ID:      UnitID(t.pass, key),
		Pass:    t.pass,
		Key:     key,
		Text:    text,
		Context: context,
		Checks:  checksOf(t.spec, t.spec.Syntax, Field{}),
		File:    file,
	}
}

// read parses one tree file under the declared keys. Scalars are read as
// written — a name that happens to look like a number is still that text.
func (t *tree) read(path string) (vocabulary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return vocabulary{}, fmt.Errorf("%s: read %s: %w", t.pass, path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return vocabulary{}, fmt.Errorf("%s: parse %s: %w", t.pass, path, err)
	}
	if len(doc.Content) == 0 {
		return vocabulary{}, fmt.Errorf("%s: %s is empty", t.pass, path)
	}
	root := doc.Content[0]
	keys := t.spec.Keys
	fail := func(format string, args ...any) (vocabulary, error) {
		return vocabulary{}, fmt.Errorf("%s: %s: %s", t.pass, path, fmt.Sprintf(format, args...))
	}
	key := lookup(root, keys.Key)
	if key == nil || key.Kind != yaml.ScalarNode || key.Value == "" {
		return fail("no `%s`", keys.Key)
	}
	v := vocabulary{key: key.Value}
	if v.label, err = names(lookup(root, keys.Label)); err != nil {
		return fail("`%s`: %v", keys.Label, err)
	}
	if skip := lookup(root, keys.SkipNodeNames); keys.SkipNodeNames != "" && skip != nil {
		if err := skip.Decode(&v.skip); err != nil {
			return fail("`%s` is not a boolean", keys.SkipNodeNames)
		}
	}
	var walk func(list *yaml.Node, ancestors []string) error
	walk = func(list *yaml.Node, ancestors []string) error {
		if list == nil {
			return nil
		}
		if list.Kind != yaml.SequenceNode {
			return fmt.Errorf("%s: %s: `%s` and `%s` must be lists", t.pass, path, keys.Nodes, keys.Children)
		}
		for _, node := range list.Content {
			code := lookup(node, keys.Code)
			if node.Kind != yaml.MappingNode || code == nil || code.Kind != yaml.ScalarNode || code.Value == "" {
				return fmt.Errorf("%s: %s: every node is a mapping with a `%s`", t.pass, path, keys.Code)
			}
			id := expand(t.spec.NodeID, map[string]string{"key": v.key, "code": code.Value})
			nodeNames, err := names(lookup(node, keys.Names))
			if err != nil {
				return fmt.Errorf("%s: %s: node %s `%s`: %w", t.pass, path, id, keys.Names, err)
			}
			v.nodes = append(v.nodes, treeNode{id: id, code: code.Value, names: nodeNames, ancestors: ancestors})
			if err := walk(lookup(node, keys.Children), append(append([]string{}, ancestors...), id)); err != nil {
				return err
			}
		}
		return nil
	}
	return v, walk(lookup(root, keys.Nodes), nil)
}

// lookup is a mapping's value under key, or nil.
func lookup(mapping *yaml.Node, key string) *yaml.Node {
	if mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

// names reads a {locale: name} mapping.
func names(node *yaml.Node) (map[string]string, error) {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("not a mapping of locale to name")
	}
	out := make(map[string]string, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i+1].Kind != yaml.ScalarNode {
			return nil, fmt.Errorf("the %s name is not text", node.Content[i].Value)
		}
		out[node.Content[i].Value] = node.Content[i+1].Value
	}
	return out, nil
}

func (t *tree) Units() []Unit { return t.units }

// sidecarEntry is one tree's names in a locale's sidecar.
type sidecarEntry struct {
	Label string            `yaml:"label,omitempty"`
	Nodes map[string]string `yaml:"nodes,omitempty"`
}

// Existing is the locale's inline names under its sidecar's. A sidecar entry
// for a tree or node the master no longer has names nothing and is passed
// over: it is gone at the next write.
func (t *tree) Existing(locale string, _ map[string][]string) (map[string]string, error) {
	out := map[string]string{}
	for _, v := range t.trees {
		if name := v.label[locale]; name != "" {
			out[UnitID(t.pass, v.key)] = name
		}
		if v.skip {
			continue
		}
		for _, node := range v.nodes {
			if name := node.names[locale]; name != "" {
				out[UnitID(t.pass, node.id)] = name
			}
		}
	}

	data, err := os.ReadFile(t.env.path(t.spec.Sidecar, locale))
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: read sidecar %s: %w", t.pass, locale, err)
	}
	var sidecar map[string]sidecarEntry
	if err := yaml.Unmarshal(data, &sidecar); err != nil {
		return nil, fmt.Errorf("%s: parse sidecar %s: %w", t.pass, locale, err)
	}
	for _, v := range t.trees {
		entry, ok := sidecar[v.key]
		if !ok {
			continue
		}
		if entry.Label != "" {
			out[UnitID(t.pass, v.key)] = entry.Label
		}
		if v.skip {
			continue
		}
		for _, node := range v.nodes {
			if name := entry.Nodes[node.code]; name != "" {
				out[UnitID(t.pass, node.id)] = name
			}
		}
	}
	return out, nil
}

func (t *tree) Write(locale string, translations map[string]string, sink files.Sink) ([]string, error) {
	sidecar := map[string]sidecarEntry{}
	for _, v := range t.trees {
		entry := sidecarEntry{Label: translations[UnitID(t.pass, v.key)]}
		if !v.skip {
			for _, node := range v.nodes {
				if name, ok := translations[UnitID(t.pass, node.id)]; ok {
					if entry.Nodes == nil {
						entry.Nodes = map[string]string{}
					}
					entry.Nodes[node.code] = name
				}
			}
		}
		if entry.Label != "" || entry.Nodes != nil {
			sidecar[v.key] = entry
		}
	}
	if len(sidecar) == 0 {
		return nil, nil
	}
	return files.WriteYAML(sink, t.env.path(t.spec.Sidecar, locale), t.env.Header, sidecar)
}

func (t *tree) Drop(locale string) ([]string, error) {
	return files.Remove([]string{t.env.path(t.spec.Sidecar, locale)})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
