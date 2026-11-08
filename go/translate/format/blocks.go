package format

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"greendrake.nz/go/translate/internal/files"
)

// text-blocks: prose, one file per page per locale, split into blocks —
// maximal runs of non-blank lines, which is exactly the unit a markdown
// renderer treats as a paragraph, a heading or a list. The pages are whatever
// the path's `*` matches among the default locale's files.
//
// A block's id is its own digest, so a block that moves keeps its translation
// and an edited one loses it — no positional bookkeeping to go stale in the
// master. A locale's file is positional, though: its blocks are addressed by
// order alone, so the order they were written in is recorded, and a file
// whose block count no longer matches that record cannot be aligned by
// anything.

// defaultPageContext is the library's wording for a host that declares none.
const defaultPageContext = "One block of the {page} page."

type blocks struct {
	pass  string
	spec  Spec
	env   Env
	units []Unit
	// pages are the pages' names and ids, in name order.
	pages []page
}

type page struct{ name, id string }

func openBlocks(pass string, spec Spec, env Env) (Store, error) {
	b := &blocks{pass: pass, spec: spec, env: env}
	master := env.path(spec.Path, env.Default)
	paths, err := filepath.Glob(master)
	if err != nil {
		return nil, fmt.Errorf("%s: glob %s: %w", pass, spec.Path, err)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("%s: no page matches %s", pass, master)
	}
	sort.Strings(paths)
	prefix, suffix, _ := strings.Cut(master, "*")
	copies := map[string]bool{}
	for _, block := range spec.CopyBlocks {
		copies[block] = true
	}
	context := firstNonEmpty(spec.Context, defaultPageContext)
	for _, path := range paths {
		name := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
		p := page{name: name, id: expand(spec.PageID, map[string]string{"name": name})}
		b.pages = append(b.pages, p)
		prose, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("%s: read %s: %w", pass, path, err)
		}
		seen := map[string]int{}
		for _, block := range SplitBlocks(string(prose)) {
			// A page that repeats a block verbatim disambiguates by occurrence.
			digest := files.Digest(block)
			seen[digest]++
			key := p.id + "#" + digest
			if seen[digest] > 1 {
				key = fmt.Sprintf("%s~%d", key, seen[digest])
			}
			b.units = append(b.units, Unit{
				ID:       UnitID(pass, key),
				Pass:     pass,
				Key:      key,
				Text:     block,
				Context:  expand(context, map[string]string{"page": p.id, "name": name}),
				Checks:   checksOf(spec, spec.Syntax, Field{}),
				Copy:     copies[strings.TrimSpace(block)],
				File:     p.id,
				Sequence: p.id,
			})
		}
	}
	return b, nil
}

func (b *blocks) file(p page, locale string) string {
	return strings.Replace(b.env.path(b.spec.Path, locale), "*", p.name, 1)
}

func (b *blocks) Units() []Unit { return b.units }

func (b *blocks) Existing(locale string, order map[string][]string) (map[string]string, error) {
	out := map[string]string{}
	for _, p := range b.pages {
		path := b.file(p, locale)
		prose, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%s: read %s: %w", b.pass, path, err)
		}
		ids := order[p.id]
		if len(ids) == 0 {
			// Nothing recorded about this page — a locale being produced for
			// the first time, or a state file deleted to force a re-render.
			// The file says nothing without the order that addresses it, so it
			// is simply rewritten.
			continue
		}
		found := SplitBlocks(string(prose))
		if len(ids) != len(found) {
			return nil, fmt.Errorf("%s has %d blocks but the translator recorded %d — restore the file or delete it and re-run", path, len(found), len(ids))
		}
		for i, id := range ids {
			out[id] = found[i]
		}
	}
	return out, nil
}

// Write emits every page all of whose blocks are translated.
func (b *blocks) Write(locale string, translations map[string]string, sink files.Sink) ([]string, error) {
	var written []string
	for _, p := range b.pages {
		var parts []string
		for _, unit := range b.units {
			if unit.Sequence != p.id {
				continue
			}
			text, ok := translations[unit.ID]
			if !ok {
				parts = nil
				break
			}
			parts = append(parts, text)
		}
		if parts == nil {
			continue
		}
		changed, err := sink(b.file(p, locale), strings.Join(parts, "\n\n")+"\n")
		if err != nil {
			return nil, err
		}
		written = append(written, changed...)
	}
	return written, nil
}

func (b *blocks) Drop(locale string) ([]string, error) {
	paths := make([]string, len(b.pages))
	for i, p := range b.pages {
		paths[i] = b.file(p, locale)
	}
	return files.Remove(paths)
}

// SplitBlocks cuts prose into the blocks a translation is aligned by: runs of
// non-blank lines. Blank lines are the separator and carry no content, so the
// emitted file rejoins with one blank line between blocks.
func SplitBlocks(prose string) []string {
	var out []string
	var current []string
	flush := func() {
		if len(current) > 0 {
			out = append(out, strings.Join(current, "\n"))
			current = nil
		}
	}
	for _, line := range strings.Split(strings.ReplaceAll(prose, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		current = append(current, line)
	}
	flush()
	return out
}
