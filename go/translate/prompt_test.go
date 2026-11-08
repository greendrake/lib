package translate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"greendrake.nz/go/translate/format"
)

// promptFor builds the system prompt the fixture's manifest gives a batch of
// units bound for a locale.
func promptFor(t *testing.T, pass Pass, locale string, glossary Glossary, units ...format.Unit) string {
	t.Helper()
	m := load(t, fixture(t, "de", "ru", "fr"))
	target := writingFor(locale)
	var items []wireItem
	for _, unit := range units {
		wired, err := wireItems(unit, writingFor(m.Default), target)
		require.NoError(t, err)
		items = append(items, wired...)
	}
	return m.systemPrompt(batchPrompt{
		target: locale, glossary: glossary,
		categories: target.categories, pass: pass, units: units, items: items,
	})
}

// The source language is the master's, whichever locale that is: told it is
// reading American English, the model has no reason to keep a British
// spelling it could as well "correct".
func TestSystemPrompt_NamesTheMastersLanguage(t *testing.T) {
	prompt := promptFor(t, Pass{}, "en-US", Glossary{}, withID(unitOf(vueHTML, "Colour"), "ui:a.b"))
	assert.Contains(t, prompt, "from British English (BCP-47 tag en-GB) into American English (BCP-47 tag en-US)")
}

// A batch asked for plural forms is told which ids they are, with this
// locale's own categories; a batch with none is told nothing about them.
func TestSystemPrompt_NamesTheFormItems(t *testing.T) {
	plural := promptFor(t, Pass{}, "ru", Glossary{}, withID(unitOf(vueHTML, "{n} listing | {n} listings"), "ui:a.n"))
	assert.Contains(t, plural, "Plural forms in Russian: one, few, many, other.")
	assert.Contains(t, plural, "ends in @one, @few, @many, @other")

	assert.NotContains(t, promptFor(t, Pass{}, "ru", Glossary{}, withID(unitOf(vueHTML, "Submit"), "ui:a.b")), "Plural forms")
}

// The checked list claims exactly the checks the batch's units resolve to:
// placeholders where the syntax has them, shown on the batch's own; markup
// where there is markup; one line where a unit is one line.
func TestSystemPrompt_ChecksAreTheResolvedOnes(t *testing.T) {
	ui := promptFor(t, Pass{}, "de", Glossary{},
		withID(unitOf(vueHTML, "Hello, {name}"), "ui:a.b"), withID(unitOf(vueHTML, "{n} new, {total} in all, {more}"), "ui:a.c"))
	assert.Contains(t, ui, "with the same name between the braces: {name} stays {name}, {n} stays {n}. Never translate, rename, add or drop one.")
	assert.Contains(t, ui, "repeat one where the sentence names the same thing again", "a repeat is checked as allowed, so stated as allowed")
	assert.Contains(t, ui, "Reproduce every HTML tag exactly")
	assert.NotContains(t, ui, "markdown")
	assert.NotContains(t, ui, "One line.")

	// A batch without a placeholder still hears that it may not invent one —
	// which is what stops a preposition acquiring the product's name.
	title := promptFor(t, Pass{}, "de", Glossary{}, withID(unitOf(icuPlain, "About"), "ui:content.about.title"))
	assert.Contains(t, title, "with the same name between the braces. Never translate, rename, add or drop one.")

	labels := promptFor(t, Pass{}, "de", Glossary{}, withID(unitOf(label, "Cars"), "tax:category:cars"))
	assert.NotContains(t, labels, "placeholder")
	assert.NotContains(t, labels, "Reproduce every HTML tag exactly")
	assert.Contains(t, labels, `An item whose context says "One line." takes no line break.`)
	assert.Contains(t, labels, "Add no HTML tag, no `@:` reference, no `|` and no `\\` that the item does not have", "held whatever a unit declares, so stated to every batch")
	assert.Contains(t, labels, `Write every character as itself, never as a \u escape`, "held of every answer, so stated to every batch")

	pages := promptFor(t, Pass{}, "de", Glossary{}, withID(unitOf(prose, "## How"), "content:/about#x"))
	assert.Contains(t, pages, "Keep every markdown link target, and each block's structure")
	assert.Contains(t, pages, "numbered 1., 2., 3. in ASCII digits whatever digits the language otherwise writes")

	const limitRule = `An item whose context says "At most N characters." is no longer than that`
	assert.NotContains(t, labels, limitRule, "stated only where a unit carries a limit")
	store := promptFor(t, Pass{}, "de", Glossary{}, withID(unitOf(subtitle, "List anything, anywhere"), "store:listing.subtitle"))
	assert.Contains(t, store, limitRule)
}

// A batch of one short label is mostly JSON framing, which no source length
// predicts: sized by its source alone, "Brand" got 30 tokens — less than the
// answer around its one word once a model indents it.
func TestMaxTokensFor_CoversTheFraming(t *testing.T) {
	answer, err := json.MarshalIndent(completion{Items: []completionItem{
		{ID: "tax:brand", Text: json.RawMessage(`"Marque de fabrique"`)},
	}}, "", "  ")
	require.NoError(t, err)
	// No token is shorter than a byte, so an answer whose bytes fit fits.
	assert.GreaterOrEqual(t, maxTokensFor([]wireItem{{ID: "tax:brand", Text: "Brand"}}), len(answer))
}

// The id is the only thing that says what a short string is for, and a
// string's mood is nothing the validator can see. Both are stated — outside
// the list that claims its rules are checked — along with the host's own
// rules, and the pass's guidance closes the prompt.
func TestSystemPrompt_StatesWhatNothingChecks(t *testing.T) {
	prompt := promptFor(t, Pass{Guidance: "These are menu entries in {language}."}, "de",
		Glossary{Register: "Address the reader as du.", Terms: map[string]string{"listing": "Anzeige", "lister": "Inserent"}},
		withID(unitOf(vueHTML, "Submit"), "ui:landing.tagline"))

	assert.Contains(t, prompt, "How to address the reader in German: Address the reader as du.")
	assert.Contains(t, prompt, "inflected as the sentence requires", "a glossary term may change form")
	assert.Less(t, strings.Index(prompt, "- lister → Inserent"), strings.Index(prompt, "- listing → Anzeige"), "terms in a stable order")
	assert.Contains(t, prompt, "<kind>:<path>", "the id's shape is explained")
	assert.Contains(t, prompt, "ui:landing.tagline, for one", "on an id of the batch's own")
	assert.Contains(t, prompt, "(App Store, Google Play) stay as their owners write them")
	assert.Contains(t, prompt, "- Keep user-interface copy about as short as the source.")
	assert.True(t, strings.HasSuffix(prompt, "These are menu entries in German.\n"))

	checked := strings.Index(prompt, "checked mechanically before your answer is accepted")
	unchecked := strings.Index(prompt, "nothing checks for you")
	require.Positive(t, checked)
	require.Positive(t, unchecked)
	assert.Less(t, checked, unchecked, "the unchecked rules come after, and outside, the checked list")
	for _, rule := range []string{"grammatical mood is part of it", "App Store", "Keep user-interface copy", "Write every word in German;"} {
		assert.Greater(t, strings.Index(prompt, rule), unchecked, "%q is declared unchecked", rule)
	}
	// What the script check sees is a letter's script, never its language:
	// the checked rule says as much and no more.
	script := strings.Index(prompt, "Use no letter of any script but German's own, Latin, and one the item's own text already carries.")
	assert.Greater(t, script, checked)
	assert.Less(t, script, unchecked)
}

// Markup reaches the model as it is written. Shown a tag JSON-escaped, the
// model answers with an escape too, and a small one does not always get the
// hex right.
func TestUserPrompt_ShowsMarkupLiterally(t *testing.T) {
	prompt, err := userPrompt([]wireItem{{ID: "ui:a", Text: `Open your <a href="{url}" target="_blank">profile</a> & wait.<br>{code}`}})
	require.NoError(t, err)
	assert.Contains(t, prompt, `<a href=\"{url}\" target=\"_blank\">profile</a> & wait.<br>`)
	escape := `\` + "u00"
	assert.NotContains(t, prompt, escape+"3c", "no escaped <")
	assert.NotContains(t, prompt, escape+"26", "no escaped &")
}

// A row's fields are one message and share a request; several rows still
// share one, since the instructions cost more than the strings.
func TestBatches_KeepBundlesWhole(t *testing.T) {
	m := load(t, fixture(t, "de"))
	var units []format.Unit
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		for _, field := range []string{"subject", "body"} {
			units = append(units, format.Unit{ID: "email:" + id + "." + field, Pass: "email", Key: id + "." + field, Bundle: id})
		}
	}
	batches := m.batches(units)
	assert.Len(t, batches, 2, "14 fields are two requests of ten or fewer, not seven")
	for _, b := range batches {
		assert.LessOrEqual(t, len(b.units), 10)
		seen := map[string]int{}
		for _, unit := range b.units {
			seen[unit.Bundle]++
		}
		for id, count := range seen {
			assert.Equal(t, 2, count, "row %s is split across requests", id)
		}
	}
}

// A catalogue is batched by its top-level section, a tree by its file.
func TestBatches_GroupAsDeclared(t *testing.T) {
	m := load(t, fixture(t, "de"))
	batches := m.batches([]format.Unit{
		{ID: "ui:common.a", Pass: "ui", Key: "common.a"},
		{ID: "tax:category", Pass: "tax", Key: "category", File: "category"},
		{ID: "ui:seo.b", Pass: "ui", Key: "seo.b"},
		{ID: "ui:common.c", Pass: "ui", Key: "common.c"},
		{ID: "tax:brand", Pass: "tax", Key: "brand", File: "brand"},
	})
	var got []string
	for _, b := range batches {
		got = append(got, b.pass.ID+"/"+b.group+":"+ids(b.units))
	}
	assert.Equal(t, []string{"ui/common:ui:common.a ui:common.c", "tax/category:tax:category", "ui/seo:ui:seo.b", "tax/brand:tax:brand"}, got)
}

func ids(units []format.Unit) string {
	out := make([]string, len(units))
	for i, unit := range units {
		out[i] = unit.ID
	}
	return strings.Join(out, " ")
}
