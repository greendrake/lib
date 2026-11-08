package format

import (
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"

	"greendrake.nz/go/translate/internal/files"
)

// xcstrings: an iOS or macOS app's String Catalog, one file holding every
// locale. Each string is an entry under its key, each locale's words under
// the entry's localizations; the master's are the manifest's default locale's,
// or the key itself where the source has none, and the entry's comment is the
// context. An entry marked shouldTranslate: false, or one Xcode found no longer
// in the code (extractionState: stale), is none of the translator's business.
// A plural variation is a keyed plural; each device variation is a unit of its
// own, keyed <key>#<device>, plural or not; and each substitution's plural is a
// keyed plural keyed <key>%<name>, the string carrying %#@name@ checked as
// printf. An entry's units are one bundle, written only whole.
//
// A key is often the English text itself and may hold any character — `@`,
// which the engine joins a unit id to a form's category with, among them. A
// key that is not an identifier is addressed by its digest, k and 12 hex
// digits, the suffixes following that; the words reach the model as the unit's
// text and context, never as its id.
//
// Every locale writes the one file, so the store is shared: one lock over it,
// and a locale's write reads the file again, replaces that locale's part — its
// localization of each entry, its values marked translated — and writes it
// back whole, laid out as Xcode saves it so that a save from Xcode moves no
// byte.

// XCStrings is the xcstrings format's name.
const XCStrings = "xcstrings"

// catalogVersion is the String Catalog version this reader knows.
const catalogVersion = "1.0"

var catalogIdentifier = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

type xcstrings struct {
	pass    string
	spec    Spec
	env     Env
	path    string
	units   []Unit
	entries []catalogEntry
	// mu is held over every read and write of the file.
	mu sync.Mutex
}

// catalogEntry is one translatable string of the catalog: its key, its
// master's localization as the shape every locale's is written in, and where
// in it each of its units lives.
type catalogEntry struct {
	key    string
	source map[string]any
	slots  []slot
}

// slot is where a unit lives in a localization: the path to the node holding
// its stringUnit, or its plural variation.
type slot struct {
	unit   Unit
	path   []string
	plural bool
}

func openXCStrings(pass string, spec Spec, env Env) (Store, error) {
	c := &xcstrings{pass: pass, spec: spec, env: env, path: env.path(spec.Path, "")}
	doc, err := c.read()
	if err != nil {
		return nil, err
	}
	if language, _ := doc["sourceLanguage"].(string); language != env.Default {
		return nil, fmt.Errorf("%s: %s is written in %q, and the manifest's master is %s", pass, c.path, language, env.Default)
	}
	catalog := doc["strings"].(map[string]any)
	for _, key := range slices.Sorted(maps.Keys(catalog)) {
		entry, ok := catalog[key].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: %s: %q is not an object", pass, c.path, key)
		}
		if entry["shouldTranslate"] == false || entry["extractionState"] == "stale" {
			continue
		}
		e, err := c.entry(key, entry)
		if err != nil {
			return nil, fmt.Errorf("%s: %s: %q: %w", pass, c.path, key, err)
		}
		c.entries = append(c.entries, e)
		for _, s := range e.slots {
			c.units = append(c.units, s.unit)
		}
	}
	return c, nil
}

// read reads the catalog, refusing one it does not know how to read.
func (c *xcstrings) read() (map[string]any, error) {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return nil, fmt.Errorf("%s: read %s: %w", c.pass, c.path, err)
	}
	value, err := files.ReadJSON(data)
	if err != nil {
		return nil, fmt.Errorf("%s: parse %s: %w", c.pass, c.path, err)
	}
	doc, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: %s is not a String Catalog", c.pass, c.path)
	}
	if version, _ := doc["version"].(string); version != catalogVersion {
		return nil, fmt.Errorf("%s: %s is String Catalog version %q; this reader knows %s", c.pass, c.path, version, catalogVersion)
	}
	if _, ok := doc["strings"].(map[string]any); !ok {
		return nil, fmt.Errorf("%s: %s has no strings", c.pass, c.path)
	}
	return doc, nil
}

// entry reads one entry's units from its master's localization.
func (c *xcstrings) entry(key string, entry map[string]any) (catalogEntry, error) {
	unitKey := key
	if !catalogIdentifier.MatchString(key) {
		unitKey = "k" + files.Digest(key)[:12]
	}
	comment, _ := entry["comment"].(string)
	e := catalogEntry{key: key}
	add := func(suffix string, path []string, text string, forms map[string]string) error {
		unit := Unit{
			ID: UnitID(c.pass, unitKey+suffix), Pass: c.pass, Key: unitKey + suffix,
			Text: text, Context: comment, Checks: checksOf(c.spec, c.spec.Syntax, Field{}),
			File: c.spec.Path, Bundle: unitKey,
		}
		if forms != nil {
			var err error
			if unit, err = pluralUnit(unit, forms); err != nil {
				return err
			}
		}
		e.slots = append(e.slots, slot{unit: unit, path: path, plural: forms != nil})
		return nil
	}
	source, ok := lookupObject(entry, "localizations", c.env.Default)
	if !ok {
		e.source = map[string]any{}
		return e, add("", nil, key, nil)
	}
	e.source = source
	// shaped adds the unit of a node holding a stringUnit or a plural
	// variation, and refuses one holding anything else.
	shaped := func(suffix string, path []string, node map[string]any) error {
		if value, ok := stringValue(node); ok {
			return add(suffix, path, value, nil)
		}
		if forms, ok := pluralForms(node); ok {
			return add(suffix, path, EncodeForms(forms), forms)
		}
		return fmt.Errorf("%s holds neither a string nor a plural variation", strings.Join(append([]string{"localizations", c.env.Default}, path...), "."))
	}
	if devices, ok := lookupObject(source, "variations", "device"); ok {
		for _, device := range slices.Sorted(maps.Keys(devices)) {
			node, ok := devices[device].(map[string]any)
			if !ok {
				return catalogEntry{}, fmt.Errorf("device variation %q is not an object", device)
			}
			if err := shaped("#"+device, []string{"variations", "device", device}, node); err != nil {
				return catalogEntry{}, err
			}
		}
	} else if err := shaped("", nil, source); err != nil {
		return catalogEntry{}, err
	}
	substitutions, _ := lookupObject(source, "substitutions")
	for _, name := range slices.Sorted(maps.Keys(substitutions)) {
		node, ok := substitutions[name].(map[string]any)
		forms, plural := pluralForms(node)
		if !ok || !plural {
			return catalogEntry{}, fmt.Errorf("substitution %q has no plural variation", name)
		}
		if err := add("%"+name, []string{"substitutions", name}, EncodeForms(forms), forms); err != nil {
			return catalogEntry{}, err
		}
	}
	return e, nil
}

func (c *xcstrings) Units() []Unit { return c.units }

// Existing is the locale's localization of each entry, read under the lock.
func (c *xcstrings) Existing(locale string, _ map[string][]string) (map[string]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	doc, err := c.read()
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, e := range c.entries {
		localization, ok := lookupObject(doc["strings"].(map[string]any), e.key, "localizations", locale)
		if !ok {
			continue
		}
		for _, s := range e.slots {
			node, ok := lookupObject(localization, s.path...)
			if !ok {
				continue
			}
			if s.plural {
				if forms, ok := pluralForms(node); ok {
					out[s.unit.ID] = EncodeForms(forms)
				}
			} else if value, ok := stringValue(node); ok {
				out[s.unit.ID] = value
			}
		}
	}
	return out, nil
}

// Write replaces the locale's localization of each entry with what the
// translations hold — an entry only whole — and hands the file to the sink.
func (c *xcstrings) Write(locale string, translations map[string]string, sink files.Sink) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	doc, err := c.read()
	if err != nil {
		return nil, err
	}
	catalog := doc["strings"].(map[string]any)
	for _, e := range c.entries {
		entry, ok := catalog[e.key].(map[string]any)
		if !ok {
			continue
		}
		localization, err := e.localization(translations)
		if err != nil {
			return nil, fmt.Errorf("%s: %q: %w", c.pass, e.key, err)
		}
		localizations, _ := entry["localizations"].(map[string]any)
		if localizations == nil {
			localizations = map[string]any{}
		}
		if localization == nil {
			delete(localizations, locale)
		} else {
			localizations[locale] = localization
		}
		setOrDrop(entry, "localizations", localizations)
	}
	return files.WriteJSON(sink, c.path, doc)
}

// localization is an entry's localization from the translations, in its
// master's shape, or nil where any of its units is untranslated.
func (e catalogEntry) localization(translations map[string]string) (map[string]any, error) {
	out := deepCopy(e.source).(map[string]any)
	for _, s := range e.slots {
		text, ok := translations[s.unit.ID]
		if !ok {
			return nil, nil
		}
		node := out
		for _, step := range s.path {
			node = node[step].(map[string]any)
		}
		if !s.plural {
			node["stringUnit"] = translated(text)
			continue
		}
		forms, err := DecodeForms(text)
		if err != nil {
			return nil, err
		}
		plural := map[string]any{}
		for category, form := range forms {
			plural[category] = map[string]any{"stringUnit": translated(form)}
		}
		node["variations"].(map[string]any)["plural"] = plural
	}
	return out, nil
}

// translated is a stringUnit holding a translation.
func translated(value string) map[string]any {
	return map[string]any{"state": "translated", "value": value}
}

// Drop removes the locale's localization from every entry.
func (c *xcstrings) Drop(locale string) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	doc, err := c.read()
	if err != nil {
		return nil, err
	}
	for _, value := range doc["strings"].(map[string]any) {
		entry, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if localizations, ok := entry["localizations"].(map[string]any); ok {
			delete(localizations, locale)
			setOrDrop(entry, "localizations", localizations)
		}
	}
	return files.WriteJSON(files.Write, c.path, doc)
}

// setOrDrop sets an object's key to a map, or removes the key where the map
// is empty: Xcode writes no empty localizations.
func setOrDrop(object map[string]any, key string, value map[string]any) {
	if len(value) == 0 {
		delete(object, key)
		return
	}
	object[key] = value
}

// lookupObject walks objects by key.
func lookupObject(node map[string]any, path ...string) (map[string]any, bool) {
	for _, key := range path {
		next, ok := node[key].(map[string]any)
		if !ok {
			return nil, false
		}
		node = next
	}
	return node, true
}

// stringValue is a node's stringUnit value, where it has a non-empty one.
func stringValue(node map[string]any) (string, bool) {
	unit, ok := lookupObject(node, "stringUnit")
	if !ok {
		return "", false
	}
	value, ok := unit["value"].(string)
	return value, ok && value != ""
}

// pluralForms are a node's plural variation, by category, where it has one.
func pluralForms(node map[string]any) (map[string]string, bool) {
	plural, ok := lookupObject(node, "variations", "plural")
	if !ok {
		return nil, false
	}
	forms := map[string]string{}
	for category, value := range plural {
		form, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if text, ok := stringValue(form); ok {
			forms[category] = text
		}
	}
	return forms, len(forms) > 0
}

// deepCopy copies a decoded JSON value.
func deepCopy(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			out[key] = deepCopy(child)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = deepCopy(child)
		}
		return out
	}
	return value
}
