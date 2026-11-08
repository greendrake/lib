package format

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"maps"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/language"

	"greendrake.nz/go/i18n/plurals"
	"greendrake.nz/go/translate/internal/files"
)

// android-strings: an Android app's string resources. The master is the
// authored file — app/src/main/res/values/strings.xml — and each locale's
// translations go to a file of their own, in the directory named for its
// resource qualifier. A <string> is a unit keyed by its name, a <plurals> a
// keyed plural, each item of a <string-array> a unit keyed name[i], the items
// of one array a bundle no batch splits. The comments above a resource are its
// context. A resource marked translatable="false" is none of the translator's
// business; one whose whole value is a resource reference (@string/…) is copied
// as it is; one marked formatted="false" is literal text.
//
// A unit's text is the string Android resolves a value to — aapt's quoting,
// whitespace and backslash escapes applied — with its markup kept: its tags as
// written, and its own `&` and `<` written as entities, so that markup and text
// stay apart.

// Android is the android-strings format's name.
const Android = "android-strings"

// Resource kinds the translator reads.
const (
	resourceString = "string"
	resourcePlural = "plurals"
	resourceArray  = "string-array"
)

type android struct {
	pass  string
	spec  Spec
	env   Env
	units []Unit
	byID  map[string]Unit
	// root is the master's <resources> tag as written — the namespaces its
	// values' markup uses are declared there — and entries its translatable
	// resources, in order.
	root    string
	entries []resource
}

// resource is one resource of a file.
type resource struct {
	kind, name              string
	translatable, formatted string
	comments                []string
	// value is a string's content as written; items are a plural's or an
	// array's.
	value string
	items []resourceItem
}

type resourceItem struct{ quantity, value string }

// key is the unit key of a resource, or of an array's ith item.
func (r resource) key(i int) string {
	if r.kind == resourceArray {
		return r.name + "[" + strconv.Itoa(i) + "]"
	}
	return r.name
}

var resourceReference = regexp.MustCompile(`^@(?:[\w.]+:)?[\w-]+/[\w.]+$`)

func openAndroid(pass string, spec Spec, env Env) (Store, error) {
	if _, err := files.XMLPreamble(env.Header); err != nil {
		return nil, fmt.Errorf("%s: %w", pass, err)
	}
	path := env.path(spec.Master, "")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: read the master %s: %w", pass, path, err)
	}
	root, resources, err := readResources(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %s: %w", pass, path, err)
	}
	a := &android{pass: pass, spec: spec, env: env, root: root, byID: map[string]Unit{}}
	named := map[string]bool{}
	for _, r := range resources {
		if r.translatable == "false" {
			continue
		}
		if named[r.name] {
			return nil, fmt.Errorf("%s: %s has two resources named %s — a product variant, as the translator keys each by its name", pass, path, r.name)
		}
		named[r.name] = true
		units, err := a.unitsOf(r)
		if err != nil {
			return nil, fmt.Errorf("%s: %s: %w", pass, path, err)
		}
		a.entries = append(a.entries, r)
		for _, unit := range units {
			a.units = append(a.units, unit)
			a.byID[unit.ID] = unit
		}
	}
	return a, nil
}

// unitsOf are a master resource's units.
func (a *android) unitsOf(r resource) ([]Unit, error) {
	syntax := a.spec.Syntax
	if r.formatted == "false" {
		syntax = SyntaxNone
	}
	unit := func(key string) Unit {
		return Unit{
			ID: UnitID(a.pass, key), Pass: a.pass, Key: key,
			Context: strings.Join(r.comments, "\n"),
			Checks:  checksOf(a.spec, syntax, Field{}),
			File:    a.spec.Master,
		}
	}
	if r.kind == resourcePlural {
		forms, err := r.forms()
		if err != nil {
			return nil, err
		}
		u, err := pluralUnit(unit(r.key(0)), forms)
		return []Unit{u}, err
	}
	var out []Unit
	for _, value := range r.values() {
		u := unit(value.key)
		if r.kind == resourceArray {
			u.Bundle = r.name
		}
		u.Copy = resourceReference.MatchString(strings.TrimSpace(value.raw))
		text, err := resourceText(value.raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", value.key, err)
		}
		u.Text = text
		out = append(out, u)
	}
	return out, nil
}

// keyedValue is a value as written, under its unit key.
type keyedValue struct{ key, raw string }

// values are a string's or an array's values, in order.
func (r resource) values() []keyedValue {
	if r.kind == resourceString {
		return []keyedValue{{r.key(0), r.value}}
	}
	out := make([]keyedValue, len(r.items))
	for i, item := range r.items {
		out[i] = keyedValue{r.key(i), item.value}
	}
	return out
}

// forms are a plural's forms, by quantity.
func (r resource) forms() (map[string]string, error) {
	forms := map[string]string{}
	for _, item := range r.items {
		form, err := resourceText(item.value)
		if err != nil {
			return nil, fmt.Errorf("%s %s: %w", r.name, item.quantity, err)
		}
		forms[item.quantity] = form
	}
	return forms, nil
}

func (a *android) Units() []Unit { return a.units }

// file is a locale's file.
func (a *android) file(locale string) (string, error) {
	q, err := qualifier(locale)
	if err != nil {
		return "", fmt.Errorf("%s: %w", a.pass, err)
	}
	return a.env.path(a.spec.Path, q), nil
}

// Existing reads the locale's file for the master's resources; a resource the
// master no longer has names nothing.
func (a *android) Existing(locale string, _ map[string][]string) (map[string]string, error) {
	path, err := a.file(locale)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: read %s: %w", a.pass, path, err)
	}
	_, resources, err := readResources(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %s: %w", a.pass, path, err)
	}
	kinds := map[string]string{}
	for _, r := range a.entries {
		kinds[r.name] = r.kind
	}
	out := map[string]string{}
	for _, r := range resources {
		if kinds[r.name] != r.kind {
			continue
		}
		texts, err := r.texts()
		if err != nil {
			return nil, fmt.Errorf("%s: %s: %w", a.pass, path, err)
		}
		for key, text := range texts {
			if unit, ok := a.byID[UnitID(a.pass, key)]; ok {
				out[unit.ID] = text
			}
		}
	}
	return out, nil
}

// texts are what a resource holds, by unit key.
func (r resource) texts() (map[string]string, error) {
	if r.kind == resourcePlural {
		forms, err := r.forms()
		if err != nil {
			return nil, err
		}
		return map[string]string{r.key(0): EncodeForms(forms)}, nil
	}
	out := map[string]string{}
	for _, value := range r.values() {
		text, err := resourceText(value.raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", value.key, err)
		}
		out[value.key] = text
	}
	return out, nil
}

// Write emits the locale's file: the master's translatable resources in its
// order, each translated one — an array only whole — and each plural in the
// target locale's quantities. It is read back before it is written, every
// value held to reading as the text it was written from.
func (a *android) Write(locale string, translations map[string]string, sink files.Sink) ([]string, error) {
	path, err := a.file(locale)
	if err != nil {
		return nil, err
	}
	// translated is the unit under a key, and its translation where it has one.
	translated := func(key string) (Unit, string, bool) {
		unit := a.byID[UnitID(a.pass, key)]
		text, ok := translations[unit.ID]
		return unit, text, ok
	}
	var body strings.Builder
	body.WriteString(a.root + "\n")
	want := map[string]string{}
	for _, r := range a.entries {
		attrs := ` name="` + attribute(r.name) + `"`
		if r.formatted != "" {
			attrs += ` formatted="` + attribute(r.formatted) + `"`
		}
		switch r.kind {
		case resourceString:
			unit, text, ok := translated(r.key(0))
			if !ok {
				continue
			}
			fmt.Fprintf(&body, "    <string%s>%s</string>\n", attrs, resourceValue(text, unit.Copy))
			want[unit.Key] = canonical(text, unit.Copy)
		case resourcePlural:
			unit, text, ok := translated(r.key(0))
			if !ok {
				continue
			}
			forms, err := DecodeForms(text)
			if err != nil {
				return nil, fmt.Errorf("%s: %s: %w", a.pass, unit.ID, err)
			}
			fmt.Fprintf(&body, "    <plurals%s>\n", attrs)
			for _, category := range plurals.All() {
				if form, ok := forms[category]; ok {
					fmt.Fprintf(&body, "        <item quantity=\"%s\">%s</item>\n", category, resourceValue(form, false))
					forms[category] = canonical(form, false)
				}
			}
			body.WriteString("    </plurals>\n")
			want[unit.Key] = EncodeForms(forms)
		case resourceArray:
			var items []string
			whole := map[string]string{}
			for i := range r.items {
				unit, text, ok := translated(r.key(i))
				if !ok {
					items = nil
					break
				}
				items = append(items, resourceValue(text, unit.Copy))
				whole[unit.Key] = canonical(text, unit.Copy)
			}
			if items == nil {
				continue
			}
			maps.Copy(want, whole)
			fmt.Fprintf(&body, "    <string-array%s>\n", attrs)
			for _, item := range items {
				fmt.Fprintf(&body, "        <item>%s</item>\n", item)
			}
			body.WriteString("    </string-array>\n")
		}
	}
	if len(want) == 0 {
		return nil, nil
	}
	body.WriteString("</resources>\n")
	if err := readsAs(body.String(), want); err != nil {
		return nil, fmt.Errorf("%s: %s: %w", a.pass, path, err)
	}
	return files.WriteXML(sink, path, a.env.Header, body.String())
}

// readsAs holds what a file is about to say to the texts it was written from.
func readsAs(body string, want map[string]string) error {
	_, resources, err := readResources([]byte(body))
	if err != nil {
		return fmt.Errorf("does not parse back: %w", err)
	}
	got := map[string]string{}
	for _, r := range resources {
		texts, err := r.texts()
		if err != nil {
			return err
		}
		for key, text := range texts {
			got[key] = text
		}
	}
	for key, text := range want {
		if got[key] != text {
			return fmt.Errorf("%s reads back as %q, written from %q", key, got[key], text)
		}
	}
	return nil
}

func (a *android) Drop(locale string) ([]string, error) {
	path, err := a.file(locale)
	if err != nil {
		return nil, err
	}
	return files.Remove([]string{path})
}

// qualifier is the resource qualifier Android names a locale's directory with,
// after `values-`: a language alone, de; with a region, en-rGB; anything else —
// a script, a numeric region, a three-letter language, a variant — in BCP 47
// form, b+zh+Hans. A tag with an extension has none.
func qualifier(locale string) (string, error) {
	tag, err := language.Parse(locale)
	if err != nil {
		return "", fmt.Errorf("%s is no BCP 47 tag: %w", locale, err)
	}
	if len(tag.Extensions()) > 0 {
		return "", fmt.Errorf("Android names no resource directory for %s: a tag with an extension has no qualifier", locale)
	}
	base, script, region := tag.Raw()
	parts := []string{base.String()}
	if script != (language.Script{}) {
		parts = append(parts, script.String())
	}
	if region != (language.Region{}) {
		parts = append(parts, region.String())
	}
	for _, variant := range tag.Variants() {
		parts = append(parts, variant.String())
	}
	legacy := len(parts[0]) == 2 && script == (language.Script{}) && len(tag.Variants()) == 0
	switch {
	case legacy && len(parts) == 1:
		return parts[0], nil
	case legacy && len(parts[1]) == 2:
		return parts[0] + "-r" + parts[1], nil
	}
	return "b+" + strings.Join(parts, "+"), nil
}

// readResources reads a resources file: its root tag as written, and every
// resource in it, a value's content as written.
func readResources(data []byte) (string, []resource, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var root string
	var resources []resource
	var comments []string
	for {
		before := decoder.InputOffset()
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", nil, err
		}
		after := decoder.InputOffset()
		switch t := token.(type) {
		case xml.StartElement:
			if root == "" {
				if t.Name.Local != "resources" {
					return "", nil, fmt.Errorf("<%s> is not <resources>", t.Name.Local)
				}
				root = string(data[before:after])
				continue
			}
			r, err := readResource(decoder, data, t, after)
			if err != nil {
				return "", nil, err
			}
			r.comments, comments = comments, nil
			if r.kind != "" {
				resources = append(resources, r)
			}
		case xml.Comment:
			if text := strings.TrimSpace(string(t)); text != "" && root != "" {
				comments = append(comments, text)
			}
		}
	}
	if root == "" {
		return "", nil, errors.New("no <resources>")
	}
	return root, resources, nil
}

// readResource reads one resource from its start tag; a kind the translator
// does not read is consumed and left without one.
func readResource(decoder *xml.Decoder, data []byte, start xml.StartElement, contentStart int64) (resource, error) {
	r := resource{name: attr(start, "name"), translatable: attr(start, "translatable"), formatted: attr(start, "formatted")}
	switch start.Name.Local {
	case resourceString:
		value, err := content(decoder, data, contentStart)
		r.kind, r.value = resourceString, value
		return r, err
	case resourcePlural, resourceArray:
		r.kind = start.Name.Local
	default:
		_, err := content(decoder, data, contentStart)
		return resource{}, err
	}
	for {
		token, err := decoder.Token()
		if err != nil {
			return resource{}, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			value, err := content(decoder, data, decoder.InputOffset())
			if err != nil {
				return resource{}, err
			}
			if t.Name.Local == "item" {
				r.items = append(r.items, resourceItem{quantity: attr(t, "quantity"), value: value})
			}
		case xml.EndElement:
			if r.kind == resourcePlural && len(r.items) == 0 {
				return resource{}, fmt.Errorf("<plurals name=%q> has no item", r.name)
			}
			return r, nil
		}
	}
}

// content consumes an element's content up to its closing tag, and returns it
// as written.
func content(decoder *xml.Decoder, data []byte, start int64) (string, error) {
	depth := 0
	for {
		before := decoder.InputOffset()
		token, err := decoder.Token()
		if err != nil {
			return "", err
		}
		switch token.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			if depth == 0 {
				return string(data[start:before]), nil
			}
			depth--
		}
	}
}

func attr(element xml.StartElement, name string) string {
	for _, a := range element.Attr {
		if a.Name.Space == "" && a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

var attribute = strings.NewReplacer(`&`, "&amp;", `<`, "&lt;", `"`, "&quot;").Replace

// resourceText is a value as Android resolves it, its markup kept. Outside
// double quotes a run of whitespace is one space and the value is trimmed;
// inside them whitespace is kept; the quotes themselves are syntax. A
// backslash escapes the next character — \n and \t a line break and a tab,
// \uXXXX a character by its code.
func resourceText(raw string) (string, error) {
	decoder := xml.NewDecoder(strings.NewReader("<v>" + raw + "</v>"))
	if _, err := decoder.Token(); err != nil {
		return "", err
	}
	var out strings.Builder
	quoted := false
	// collapsed is where the last run of whitespace outside quotes was
	// written as one space, -1 when text has come since.
	collapsed := -1
	write := func(s string) {
		out.WriteString(s)
		collapsed = -1
	}
	depth := 0
	for {
		before := decoder.InputOffset()
		token, err := decoder.Token()
		if err != nil {
			return "", err
		}
		switch t := token.(type) {
		case xml.StartElement:
			depth++
			out.WriteString(raw[before-3 : decoder.InputOffset()-3])
			continue
		case xml.EndElement:
			if depth == 0 {
				if collapsed >= 0 {
					return out.String()[:collapsed] + out.String()[collapsed+1:], nil
				}
				return out.String(), nil
			}
			depth--
			out.WriteString(raw[before-3 : decoder.InputOffset()-3])
			continue
		case xml.CharData:
			text := string(t)
			for i := 0; i < len(text); {
				r, size := utf8.DecodeRuneInString(text[i:])
				i += size
				switch {
				case r == '\\' && i < len(text):
					escaped, size := utf8.DecodeRuneInString(text[i:])
					i += size
					switch escaped {
					case 'n':
						write("\n")
					case 't':
						write("\t")
					case 'u':
						code, err := strconv.ParseUint(text[i:min(i+4, len(text))], 16, 32)
						if err != nil || i+4 > len(text) {
							return "", fmt.Errorf("\\u is not followed by four hex digits")
						}
						i += 4
						write(markupText(string(rune(code))))
					default:
						write(markupText(string(escaped)))
					}
				case r == '"':
					quoted = !quoted
				case !quoted && strings.ContainsRune(" \t\n\r\v\f", r):
					if out.Len() > 0 && collapsed < 0 {
						collapsed = out.Len()
						out.WriteByte(' ')
					}
				default:
					write(markupText(string(r)))
				}
			}
		}
	}
}

// markupText writes text among markup: its `&` and `<` as entities.
var markupText = strings.NewReplacer("&", "&amp;", "<", "&lt;").Replace

// resourceTag matches a tag in a unit's text.
var resourceTag = regexp.MustCompile(`</?[A-Za-z][\w:.-]*(?:\s[^<>]*)?/?>`)

// resourceValue writes a text as a value Android resolves back to it: its
// tags as written, its text escaped for aapt and for XML, the whole quoted
// where aapt would otherwise collapse its whitespace. A reference is copied as
// it is.
func resourceValue(text string, reference bool) string {
	if reference {
		return text
	}
	var out, literal strings.Builder
	end := 0
	escapeRun := func(run string) {
		for _, r := range html.UnescapeString(run) {
			literal.WriteRune(r)
			switch {
			case r == '\\' || r == '\'' || r == '"':
				out.WriteRune('\\')
				out.WriteRune(r)
			case r == '\n':
				out.WriteString(`\n`)
			case r == '\t':
				out.WriteString(`\t`)
			case r < 0x20:
				fmt.Fprintf(&out, `\u%04x`, r)
			case (r == '@' || r == '?') && out.Len() == 0:
				out.WriteRune('\\')
				out.WriteRune(r)
			case r == '&':
				out.WriteString("&amp;")
			case r == '<':
				out.WriteString("&lt;")
			case r == '>':
				out.WriteString("&gt;")
			default:
				out.WriteRune(r)
			}
		}
	}
	for _, at := range resourceTag.FindAllStringIndex(text, -1) {
		escapeRun(text[end:at[0]])
		out.WriteString(text[at[0]:at[1]])
		end = at[1]
	}
	escapeRun(text[end:])
	plain := literal.String()
	if strings.HasPrefix(plain, " ") || strings.HasSuffix(plain, " ") || strings.Contains(plain, "  ") {
		return `"` + out.String() + `"`
	}
	return out.String()
}

// canonical is a text as resourceText reads back what resourceValue writes
// of it: its tags as written, its text with every entity resolved and its own
// `&` and `<` written as entities.
func canonical(text string, reference bool) string {
	if reference {
		return text
	}
	var out strings.Builder
	end := 0
	for _, at := range resourceTag.FindAllStringIndex(text, -1) {
		out.WriteString(markupText(html.UnescapeString(text[end:at[0]])))
		out.WriteString(text[at[0]:at[1]])
		end = at[1]
	}
	out.WriteString(markupText(html.UnescapeString(text[end:])))
	return out.String()
}
