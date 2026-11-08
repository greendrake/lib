// Package format reads a host's translatable files into units and writes a
// locale's translations back in the same shape. Six formats cover what a
// product ships — on the web a message catalogue, a vocabulary tree, prose
// split into blocks and rows of templated messages; natively an Android app's
// string resources and an iOS app's String Catalog — and a manifest picks one
// per source and names its files; nothing about a host is coded here.
package format

import (
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"greendrake.nz/go/translate/internal/files"
)

// Unit is one translatable thing: what it says, where it goes, what the
// translator needs to know to render it, and what its translation is held to.
type Unit struct {
	// ID is stable across runs and unique within a locale — <pass>:<Key>, the
	// handle the state records and the model echoes back.
	ID   string
	Pass string
	// Key addresses the unit inside its source: a dotted catalogue key, a
	// tree's key or node id, "<row>.<field>", a page id.
	Key string
	// Text is the master's source.
	Text string
	// Forms are a plural's forms keyed by CLDR category, where the source keys
	// them: an Android <plurals>, a String Catalog's plural variation. Text is
	// then the forms encoded (EncodeForms), and so is the unit's translation.
	Forms map[string]string
	// Context is what the model is told about this unit beyond its text. Never
	// itself translated.
	Context string
	Checks  Checks
	// Copy marks a unit reproduced verbatim rather than translated: a block
	// that is a marker rather than prose. It still carries a digest, so an
	// edit to it still propagates; it is simply never sent to a model.
	Copy bool
	// File is the master file the unit came from, which is what a batch
	// declared `by: file` groups on.
	File string
	// Bundle is shared by units no batch may split: the fields of one row,
	// which are one message written in several parts.
	Bundle string
	// Sequence is the positional file a block belongs to. Its translated blocks
	// are addressed by order alone, so the state records that order.
	Sequence string
}

// Checks are what a unit's translation is held to beyond being non-empty.
type Checks struct {
	// Syntax is the renderer's: what a placeholder looks like, how a plural
	// is written, what else is syntax rather than language.
	Syntax string
	// Markup is what the text is marked up in.
	Markup string
	// Shape is whether a line break is allowed.
	Shape string
	// Limit is the most characters the translation may have as it is written,
	// zero for no limit: a field the text is bound for caps it — a store's
	// subtitle, a form's title.
	Limit int
}

// Syntaxes.
const (
	// SyntaxICU is ICU MessageFormat: {name} placeholders, {n, plural, …}.
	SyntaxICU = "icu"
	// SyntaxVueI18n is vue-i18n's: {name} placeholders, `|`-separated plural
	// forms, @:key references to other messages.
	SyntaxVueI18n = "vue-i18n"
	// SyntaxBraces is {name} placeholders and nothing else — prose a renderer
	// substitutes into.
	SyntaxBraces = "braces"
	// SyntaxPrintf is a C, Objective-C or Java format string: %d, %1$@, %%,
	// and iOS's %#@name@ substitutions.
	SyntaxPrintf = "printf"
	// SyntaxNone is literal text.
	SyntaxNone = "none"
)

// Markups.
const (
	MarkupHTML     = "html"
	MarkupMarkdown = "markdown"
	MarkupNone     = "none"
)

// Shapes.
const (
	ShapeLine = "line"
	ShapeText = "text"
)

// syntaxList names the syntaxes in a refusal.
const syntaxList = "icu, vue-i18n, braces, printf, none"

var (
	syntaxes = map[string]bool{SyntaxICU: true, SyntaxVueI18n: true, SyntaxBraces: true, SyntaxPrintf: true, SyntaxNone: true}
	markups  = map[string]bool{MarkupHTML: true, MarkupMarkdown: true, MarkupNone: true}
	shapes   = map[string]bool{ShapeLine: true, ShapeText: true}
)

// Placeholders reports whether a syntax has placeholders the caller fills: a
// printf conversion crosses the wire as one.
func (c Checks) Placeholders() bool {
	return c.Syntax != SyntaxNone
}

// Tagged reports whether the text may carry HTML tags.
func (c Checks) Tagged() bool {
	return c.Markup == MarkupHTML || c.Markup == MarkupMarkdown
}

// Spec is what a manifest declares about one source beyond what every pass
// declares. Which keys a source takes depends on its format; Validate refuses
// the rest.
type Spec struct {
	Format string `yaml:"format"`
	// Path is where each locale's copy of the source lives, with {locale} in
	// it — for android-strings standing for the locale's resource qualifier;
	// for yaml-tree, the glob of the master's files, for text-blocks a glob
	// whose `*` is a page's name, and for xcstrings the one catalog.
	Path string `yaml:"path"`
	// Master is the authored file of yaml-rows and android-strings, whose
	// translations go to Path.
	Master string `yaml:"master"`
	// Sidecar is where yaml-tree writes a locale's names.
	Sidecar string `yaml:"sidecar"`
	// Keys names the YAML keys a yaml-tree file is written with.
	Keys TreeKeys `yaml:"keys"`
	// NodeID composes a yaml-tree node's id from {key} and {code}.
	NodeID string `yaml:"node_id"`
	// PageID composes a text-blocks page's id from {name}.
	PageID string `yaml:"page_id"`
	// Row names the YAML keys a yaml-rows row carries its id and locale in.
	Row RowKeys `yaml:"row"`
	// Fields are a yaml-rows row's translatable fields, in order.
	Fields Fields `yaml:"fields"`

	Syntax          string           `yaml:"syntax"`
	SyntaxOverrides []SyntaxOverride `yaml:"syntax_overrides"`
	Markup          string           `yaml:"markup"`
	Shape           string           `yaml:"shape"`
	// CopyBlocks are text-blocks blocks reproduced rather than translated.
	CopyBlocks []string `yaml:"copy_blocks"`
	// Context is the template a unit's context is expanded from, and
	// LabelContext a yaml-tree label's.
	Context      string `yaml:"context"`
	LabelContext string `yaml:"label_context"`
}

// SyntaxOverride gives the catalogue keys under a prefix another renderer's
// syntax.
type SyntaxOverride struct {
	Prefix string `yaml:"prefix"`
	Syntax string `yaml:"syntax"`
}

// Store is one opened source: its master read into units, and what each locale
// holds of it.
//
// A store is used by every locale of a run at once. Most formats give each
// locale files of its own; one whose file every locale shares holds one lock
// over it, and its Existing, Write and Drop each read, replace or remove only
// the locale's part of it under that lock.
type Store interface {
	Units() []Unit
	// Existing is what a locale already holds, keyed by unit id. order is the
	// recorded block order per sequence, which a positional format needs to
	// say which block is which.
	Existing(locale string, order map[string][]string) (map[string]string, error)
	// Write emits a locale's files from the translations in hand into a sink,
	// and reports what the sink did. A file is written once it has something
	// in it; a shared file is handed to the sink whole, with the locale's part
	// replaced, so a comparison sees that locale's difference alone.
	Write(locale string, translations map[string]string, sink files.Sink) ([]string, error)
	// Drop removes everything a de-listed locale has of the source — its files,
	// or its part of a shared one — and reports the files it changed.
	Drop(locale string) ([]string, error)
}

// Env is what every store is opened with.
type Env struct {
	// Root is the manifest's directory: every path resolves against it.
	Root string
	// Default is the master locale.
	Default string
	// Header is the text every file a store writes opens with, one line per
	// line: each writer frames it in its file's comment syntax, and a file
	// that has none carries none.
	Header string
}

func (e Env) path(template, locale string) string {
	return filepath.Join(e.Root, strings.ReplaceAll(template, localePlaceholder, locale))
}

const localePlaceholder = "{locale}"

type opener func(pass string, spec Spec, env Env) (Store, error)

// Catalogue is the yaml-catalogue format's name.
const Catalogue = "yaml-catalogue"

// formats are the built-ins: how each opens, and which keys it takes beyond
// the checks every format takes. Required keys are each format's own to demand.
var formats = map[string]struct {
	open opener
	keys []string
}{
	Catalogue:     {openCatalogue, []string{"path", "syntax_overrides"}},
	"yaml-tree":   {openTree, []string{"path", "sidecar", "keys", "node_id", "context", "label_context"}},
	"text-blocks": {openBlocks, []string{"path", "page_id", "copy_blocks", "context"}},
	"yaml-rows":   {openRows, []string{"master", "path", "row", "fields", "context"}},
	Android:       {openAndroid, []string{"master", "path"}},
	XCStrings:     {openXCStrings, []string{"path"}},
}

// everyFormat are the keys any format takes.
var everyFormat = []string{"format", "syntax", "markup", "shape"}

// Validate refuses a declaration the format cannot honour, in the locales it
// is to write, without reading anything: an unknown format or check, a key the
// format does not take, a per-locale file declared without {locale}, a
// template naming what the format does not capture, a locale the format has
// no way to name.
func Validate(pass string, spec Spec, locales []string) error {
	f, ok := formats[spec.Format]
	if !ok {
		return fmt.Errorf("%s: unknown format %q", pass, spec.Format)
	}
	taken := map[string]bool{}
	for _, key := range append(append([]string{}, everyFormat...), f.keys...) {
		taken[key] = true
	}
	for _, key := range declaredKeys(spec) {
		if !taken[key] {
			return fmt.Errorf("%s: %s takes no `%s`", pass, spec.Format, key)
		}
	}
	if !syntaxes[spec.Syntax] {
		return fmt.Errorf("%s: syntax %q is none of %s", pass, spec.Syntax, syntaxList)
	}
	if err := checkMarkupShape(pass, spec.Markup, spec.Shape, true); err != nil {
		return err
	}
	for _, override := range spec.SyntaxOverrides {
		if override.Prefix == "" || !syntaxes[override.Syntax] {
			return fmt.Errorf("%s: a syntax override needs a prefix and one of %s", pass, syntaxList)
		}
	}
	for _, field := range spec.Fields {
		if err := checkMarkupShape(pass+" field "+field.Name, field.Markup, field.Shape, false); err != nil {
			return err
		}
		if field.Limit < 0 {
			return fmt.Errorf("%s field %s: limit %d is not a number of characters", pass, field.Name, field.Limit)
		}
	}
	return validators[spec.Format](pass, spec, locales)
}

func checkMarkupShape(what, markup, shape string, required bool) error {
	if (required || markup != "") && !markups[markup] {
		return fmt.Errorf("%s: markup %q is none of html, markdown, none", what, markup)
	}
	if shape != "" && !shapes[shape] {
		return fmt.Errorf("%s: shape %q is none of line, text", what, shape)
	}
	return nil
}

// validators are each format's own demands.
var validators = map[string]func(pass string, spec Spec, locales []string) error{
	Catalogue: func(pass string, spec Spec, _ []string) error {
		return perLocale(pass, "path", spec.Path)
	},
	"yaml-tree": func(pass string, spec Spec, _ []string) error {
		if spec.Path == "" || strings.Contains(spec.Path, localePlaceholder) {
			return fmt.Errorf("%s: `path` is the glob of the master's files, without %s", pass, localePlaceholder)
		}
		if err := perLocale(pass, "sidecar", spec.Sidecar); err != nil {
			return err
		}
		if err := spec.Keys.validate(pass); err != nil {
			return err
		}
		if !strings.Contains(spec.NodeID, "{code}") {
			return fmt.Errorf("%s: `node_id` must carry {code}, or two nodes of one tree would share an id", pass)
		}
		return templates(pass, map[string][]string{
			"node_id":       {spec.NodeID, "key", "code"},
			"context":       {spec.Context, "key", "label"},
			"label_context": {spec.LabelContext, "key"},
		})
	},
	"text-blocks": func(pass string, spec Spec, _ []string) error {
		if err := perLocale(pass, "path", spec.Path); err != nil {
			return err
		}
		if strings.Count(spec.Path, "*") != 1 {
			return fmt.Errorf("%s: `path` must carry exactly one `*`, standing for a page's name", pass)
		}
		if spec.PageID == "" {
			return fmt.Errorf("%s: `page_id` is required", pass)
		}
		return templates(pass, map[string][]string{
			"page_id": {spec.PageID, "name"},
			"context": {spec.Context, "page", "name"},
		})
	},
	"yaml-rows": func(pass string, spec Spec, _ []string) error {
		if spec.Master == "" || strings.Contains(spec.Master, localePlaceholder) {
			return fmt.Errorf("%s: `master` is the authored file, without %s", pass, localePlaceholder)
		}
		if err := perLocale(pass, "path", spec.Path); err != nil {
			return err
		}
		if spec.Row.ID == "" || spec.Row.Locale == "" {
			return fmt.Errorf("%s: `row` must name the id and locale keys", pass)
		}
		if len(spec.Fields) == 0 {
			return fmt.Errorf("%s: `fields` must name at least one", pass)
		}
		return templates(pass, map[string][]string{
			"context": {spec.Context, "describe", "id", "field"},
		})
	},
	Android: func(pass string, spec Spec, locales []string) error {
		if spec.Master == "" || strings.Contains(spec.Master, localePlaceholder) {
			return fmt.Errorf("%s: `master` is the authored strings.xml, without %s", pass, localePlaceholder)
		}
		if err := perLocale(pass, "path", spec.Path); err != nil {
			return err
		}
		if spec.Syntax != SyntaxPrintf || spec.Markup != MarkupHTML {
			return fmt.Errorf("%s: an Android string resource is a format string with HTML markup: `syntax: printf`, `markup: html`", pass)
		}
		for _, locale := range locales {
			if _, err := qualifier(locale); err != nil {
				return fmt.Errorf("%s: %w", pass, err)
			}
		}
		return nil
	},
	XCStrings: func(pass string, spec Spec, _ []string) error {
		if spec.Path == "" || strings.Contains(spec.Path, localePlaceholder) {
			return fmt.Errorf("%s: `path` is the one catalog every locale shares, without %s", pass, localePlaceholder)
		}
		if spec.Syntax != SyntaxPrintf {
			return fmt.Errorf("%s: a String Catalog's strings are format strings: `syntax: printf`", pass)
		}
		return nil
	},
}

// perLocale requires a file every locale has its own copy of to say where.
func perLocale(pass, key, template string) error {
	if !strings.Contains(template, localePlaceholder) {
		return fmt.Errorf("%s: `%s` must carry %s — every locale has its own copy", pass, key, localePlaceholder)
	}
	return nil
}

var placeholder = regexp.MustCompile(`\{([a-z_]+)\}`)

// templates requires each template to name only what its format captures.
// Each entry is the template followed by the names it may use.
func templates(pass string, declared map[string][]string) error {
	for key, entry := range declared {
		allowed := map[string]bool{}
		for _, name := range entry[1:] {
			allowed[name] = true
		}
		for _, match := range placeholder.FindAllStringSubmatch(entry[0], -1) {
			if !allowed[match[1]] {
				return fmt.Errorf("%s: `%s` names {%s}, which is none of {%s}", pass, key, match[1], strings.Join(entry[1:], "}, {"))
			}
		}
	}
	return nil
}

// expand fills a template's placeholders.
func expand(template string, values map[string]string) string {
	return placeholder.ReplaceAllStringFunc(template, func(token string) string {
		return values[token[1:len(token)-1]]
	})
}

// declaredKeys are the YAML keys a spec sets.
func declaredKeys(spec Spec) []string {
	var keys []string
	value := reflect.ValueOf(spec)
	for i := range value.NumField() {
		if !value.Field(i).IsZero() {
			keys = append(keys, strings.Split(value.Type().Field(i).Tag.Get("yaml"), ",")[0])
		}
	}
	return keys
}

// Open reads a validated source's master.
func Open(pass string, spec Spec, env Env) (Store, error) {
	return formats[spec.Format].open(pass, spec, env)
}

// checksOf resolves the checks a unit is held to: the source's, with a field's
// own markup and shape over them where it states one, its limit, and text where
// nothing states a shape. A unit of no field passes the zero Field.
func checksOf(spec Spec, syntax string, field Field) Checks {
	checks := Checks{Syntax: syntax, Markup: spec.Markup, Shape: spec.Shape, Limit: field.Limit}
	if field.Markup != "" {
		checks.Markup = field.Markup
	}
	if field.Shape != "" {
		checks.Shape = field.Shape
	}
	if checks.Shape == "" {
		checks.Shape = ShapeText
	}
	return checks
}

// UnitID is <pass>:<key>.
func UnitID(pass, key string) string { return pass + ":" + key }
