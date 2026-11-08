// Package catalogue is the backend's side of a product's UI catalogue: the
// subtrees of it the backend renders from, read from the files the frontend
// serves — one YAML file per shipped locale — and merged the way the frontend
// build merges them: each locale is the default's messages overlaid with its
// own, so a key a translation lacks reads in the default language exactly as
// it does in the app.
package catalogue

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"

	"greendrake.nz/go/i18n/locales"
	"greendrake.nz/go/i18n/messageformat"
)

// Options is what a host's catalogue is loaded with.
type Options struct {
	// Dir holds one <locale>.yaml per shipped locale.
	Dir string
	// Locales is the shipped set: each of them is read, and the default is the
	// one every other is merged over.
	Locales *locales.Set
	// Prefixes name the subtrees the backend renders (`seo` is every `seo.*`
	// key, `a.b` every `a.b.*` one). Only these are loaded, validated and
	// fingerprinted; the rest of the catalogue is the frontend's.
	Prefixes []string
	// Constants are values every render is given under its own arguments — a
	// brand name, stated once at load rather than at each call.
	Constants map[string]string
}

// Catalogue is the backend-rendered messages of every shipped locale, merged
// and validated.
type Catalogue struct {
	editions    map[string]edition
	constants   map[string]any
	fingerprint string
}

// edition is one locale's merged messages and the tag they render under.
type edition struct {
	tag      language.Tag
	messages map[string]string
}

// Load reads every shipped locale's file and validates what the backend
// renders from it, at startup rather than at the first render. A prefix the
// default locale defines nothing under is refused with the rest: every render
// from it would fail. Presence across the other locales needs no check — the
// merge gives each of them every key the default defines.
func Load(opts Options) (*Catalogue, error) {
	def := opts.Locales.Default
	defaults, err := opts.read(def)
	if err != nil {
		return nil, err
	}
	c := &Catalogue{
		editions:  make(map[string]edition, len(opts.Locales.Shipped)),
		constants: make(map[string]any, len(opts.Constants)),
	}
	for name, value := range opts.Constants {
		c.constants[name] = value
	}
	for _, locale := range opts.Locales.Shipped {
		messages := maps.Clone(defaults)
		if locale != def {
			own, err := opts.read(locale)
			if err != nil {
				return nil, err
			}
			maps.Copy(messages, own)
		}
		// The set's tags parse: locales.New refuses one that does not.
		c.editions[locale] = edition{tag: language.MustParse(locale), messages: messages}
	}

	// The fingerprint is an order-independent digest of everything a render
	// reads: the default locale, every edition's messages and the constants.
	entries := []string{"default\x00" + def}
	for locale, ed := range c.editions {
		for key, text := range ed.messages {
			entries = append(entries, locale+"\x00"+key+"\x00"+text)
		}
	}
	for name, value := range opts.Constants {
		entries = append(entries, "constant\x00"+name+"\x00"+value)
	}
	slices.Sort(entries)
	digest := sha256.New()
	for _, entry := range entries {
		digest.Write([]byte(entry))
		digest.Write([]byte{0})
	}
	c.fingerprint = hex.EncodeToString(digest.Sum(nil))
	return c, nil
}

// read flattens one locale's rendered subtrees into dotted keys, validating
// each message. A translation may lack a subtree — the default's fills it — but
// the default may not.
func (o Options) read(locale string) (map[string]string, error) {
	path := filepath.Join(o.Dir, locale+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("catalogue: %w", err)
	}
	var tree map[string]any
	if err := yaml.Unmarshal(data, &tree); err != nil {
		return nil, fmt.Errorf("catalogue: parse %s: %w", path, err)
	}
	out := map[string]string{}
	for _, prefix := range o.Prefixes {
		node := tree
		for _, segment := range strings.Split(prefix, ".") {
			node, _ = node[segment].(map[string]any)
		}
		if len(node) == 0 && locale == o.Locales.Default {
			return nil, fmt.Errorf("catalogue: %s defines nothing under %s", path, prefix)
		}
		if err := flatten(path, prefix, node, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// flatten adds a subtree's messages to out under their dotted keys, refusing
// any the backend would not render as written.
//
// Two constructs are rejected outright: vue-i18n's `|` plural form and its
// `@:linked` references. messageformat's parser treats both as literal text —
// it branches on `{`, `}` and `#` and nothing else — so neither fails at render
// time; they would ship verbatim into whatever the backend renders. Rejecting
// them needs an explicit scan, not a render, which is also why the ICU check
// parses rather than renders: Render demands every argument a message
// references, so rendering with none would reject every legitimate `{name}`
// message.
func flatten(file, prefix string, node map[string]any, out map[string]string) error {
	for name, value := range node {
		key := prefix + "." + name
		switch typed := value.(type) {
		case map[string]any:
			if err := flatten(file, key, typed, out); err != nil {
				return err
			}
		case string:
			if strings.Contains(typed, "|") || strings.Contains(typed, "@:") {
				return fmt.Errorf("catalogue: %s key %s uses a vue-i18n construct the backend renderer treats as literal text (| or @:)", file, key)
			}
			if err := messageformat.Validate(typed); err != nil {
				return fmt.Errorf("catalogue: %s key %s: %w", file, key, err)
			}
			out[key] = typed
		default:
			return fmt.Errorf("catalogue: %s key %s is a %T, not a message", file, key, value)
		}
	}
	return nil
}

// Message renders one key in one shipped locale, with the constants under
// args. Every failure — a locale not shipped, an unknown key (any outside the
// prefixes is one), a message that will not render against the arguments
// given — is a fault in the caller, and it is returned rather than degraded
// through: the degraded forms are text in another language than the one
// asked for, the key itself and the raw ICU source, and each would be
// published.
func (c *Catalogue) Message(locale, key string, args map[string]any) (string, error) {
	ed, ok := c.editions[locale]
	if !ok {
		return "", fmt.Errorf("catalogue: locale %s is not shipped", locale)
	}
	text, ok := ed.messages[key]
	if !ok {
		return "", fmt.Errorf("catalogue: no key %s", key)
	}
	merged := maps.Clone(c.constants)
	maps.Copy(merged, args)
	rendered, err := messageformat.Render(ed.tag, text, merged)
	if err != nil {
		return "", fmt.Errorf("catalogue: render %s in %s: %w", key, locale, err)
	}
	return rendered, nil
}

// Fingerprint digests everything a render reads: the shipped locales, their
// messages and the constants. Shipping a locale, fixing a string or changing a
// constant moves it, which is how a host learns that what it rendered and kept
// predates the change.
func (c *Catalogue) Fingerprint() string {
	return c.fingerprint
}
