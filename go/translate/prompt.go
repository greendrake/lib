package translate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"

	"greendrake.nz/go/translate/format"
)

// What the model is told, and what it is allowed to answer with.
//
// Most of what is stated here the validator also enforces: the prompt makes a
// correct answer likely, and the validator exists because "likely" is not a
// guarantee — a mistranslated placeholder reaches a user as a broken string.
// The rules are therefore stated in two lists, and the checked one is built
// from the checks the batch's units actually resolve to: a model told a
// rejection concerns "mechanically checked" rules must be able to tell which
// those are, and a list claiming a check that does not run is a lie it acts on.

// responseSchema constrains the completion to exactly the ids asked for — the
// shape, and the ids themselves as an enum. The adapters hand it to their
// backend's constrained-output mechanism, so a model cannot answer under an id
// nobody asked for: reproducing a long id is the one part of the job a small
// model gets wrong outright, echoing `ui:account.identity.disclosure_intro`
// back as `uiк:account.identity.disclosure_intro` and losing the item along
// with its translation. The llm package validates the result against the same
// schema regardless, so a backend that ignores constrained decoding still
// cannot widen the shape.
func responseSchema(items []wireItem) (json.RawMessage, error) {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	enum, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(fmt.Sprintf(schemaTemplate, enum)), nil
}

// schemaTemplate is that schema, less the ids.
const schemaTemplate = `{
	"type": "object",
	"properties": {
		"items": {
			"type": "array",
			"items": {
				"type": "object",
				"properties": {
					"id": {"type": "string", "enum": %s},
					"text": {"type": "string"}
				},
				"required": ["id", "text"],
				"additionalProperties": false
			}
		}
	},
	"required": ["items"],
	"additionalProperties": false
}`

// completion is the shape the schema describes. Each text is kept as written,
// so how it was spelled can be judged before it is decoded.
type completion struct {
	Items []completionItem `json:"items"`
}

type completionItem struct {
	ID   string          `json:"id"`
	Text json.RawMessage `json:"text"`
}

// batchPrompt is everything one request's instructions are built from.
type batchPrompt struct {
	target     string
	glossary   Glossary
	categories []string
	pass       Pass
	units      []format.Unit
	items      []wireItem
}

// systemPrompt states the job once per request: who the product is, what the
// source and target languages are, what must survive verbatim, and how the
// target language counts.
func (m *Manifest) systemPrompt(p batchPrompt) string {
	var b strings.Builder
	name := languageName(p.target)
	fmt.Fprintf(&b, "You translate product copy from %s (BCP-47 tag %s) into %s (BCP-47 tag %s).\n\n", languageName(m.Default), m.Default, name, p.target)
	fmt.Fprintf(&b, "About the product:\n%s\n\n", strings.TrimSpace(m.policy.Brief))

	fmt.Fprintf(&b, "How to address the reader in %s: %s\n\n", name, p.glossary.Register)
	if terms := p.glossary.sortedTerms(); len(terms) > 0 {
		fmt.Fprintf(&b, "Use these renderings for the product's own vocabulary every time it appears, inflected as the sentence requires. Where the source uses one of these words as a different part of speech — \"list\" the verb against \"listing\" the noun — use the corresponding form in %s rather than forcing the noun:\n", name)
		for _, pair := range terms {
			fmt.Fprintf(&b, "- %s → %s\n", pair[0], pair[1])
		}
		b.WriteString("\n")
	}

	b.WriteString("Rules, all of them checked mechanically before your answer is accepted:\n")
	for _, rule := range checkedRules(p) {
		b.WriteString("- " + rule + "\n")
	}

	b.WriteString("\nMore that nothing checks for you, so they are yours to get right:\n")
	for _, rule := range m.uncheckedRules(p) {
		b.WriteString("- " + rule + "\n")
	}

	if plural := pluralCategories(p); plural != "" {
		b.WriteString("\n" + plural + "\n")
	}
	if guidance := strings.TrimSpace(p.pass.Guidance); guidance != "" {
		b.WriteString("\n" + strings.ReplaceAll(guidance, "{language}", name) + "\n")
	}
	return b.String()
}

// checkedRules are the rules the batch's units are validated against, and no
// others.
func checkedRules(p batchPrompt) []string {
	var placeholdered, tagged, markdown, oneLine, limited, printf bool
	for _, unit := range p.units {
		placeholdered = placeholdered || unit.Checks.Placeholders()
		tagged = tagged || unit.Checks.Tagged()
		markdown = markdown || unit.Checks.Markup == format.MarkupMarkdown
		oneLine = oneLine || unit.Checks.Shape == format.ShapeLine
		limited = limited || unit.Checks.Limit > 0
		printf = printf || unit.Checks.Syntax == format.SyntaxPrintf
	}
	var rules []string
	if placeholdered {
		rule := "Reproduce every placeholder exactly as written, with the same name between the braces"
		if examples := batchPlaceholders(p.items); len(examples) > 0 {
			var pairs []string
			for _, example := range examples {
				pairs = append(pairs, example+" stays "+example)
			}
			rule += ": " + strings.Join(pairs, ", ")
		}
		rules = append(rules,
			rule+". Never translate, rename, add or drop one. You may move one to where the sentence needs it, and repeat one where the sentence names the same thing again.",
			"Never spell out the product's name: where a placeholder stands for it, the placeholder stays.")
	}
	if tagged {
		rules = append(rules, "Reproduce every HTML tag exactly as written: <a attr1> stays <a attr1>, the attrN in it naming which element it is. An element may move to where the sentence needs it, its tags intact and properly nested.")
	}
	if markdown {
		rules = append(rules, "Keep every markdown link target, and each block's structure: a heading stays a heading of the same level, a list stays a list, and a numbered list is numbered 1., 2., 3. in ASCII digits whatever digits the language otherwise writes.")
	}
	if oneLine {
		rules = append(rules, `An item whose context says "One line." takes no line break.`)
	}
	if limited {
		rules = append(rules, `An item whose context says "At most N characters." is no longer than that, every letter, space and punctuation mark counted: say it more briefly rather than cut it off.`)
	}
	held := "no `|` and no `\\`"
	if printf {
		held = "no `|`, no `\\` and no `%`"
	}
	return append(rules,
		"Add no HTML tag, no `@:` reference, "+held+" that the item does not have, and keep every one it has.",
		fmt.Sprintf("Use no letter of any script but %s's own, Latin, and one the item's own text already carries.", languageName(p.target)),
		"Write every character as itself, never as a \\u escape: the only escapes in your answer are the ones JSON requires — \\\", \\\\ and \\n.",
		`Answer with one item per id you were given, and no others, as {"items": [{"id": "…", "text": "…"}]}.`)
}

// placeholderExamples bounds how many of the batch's placeholders illustrate
// the rule.
const placeholderExamples = 2

// batchPlaceholders are the first few distinct placeholders the batch's items
// carry — the rule shown on what it is about, rather than on names from
// another product.
func batchPlaceholders(items []wireItem) []string {
	var out []string
	for _, item := range items {
		for _, name := range placeholders(item.Text) {
			if !slices.Contains(out, name) {
				out = append(out, name)
			}
			if len(out) == placeholderExamples {
				return out
			}
		}
	}
	return out
}

// uncheckedRules are what the validator cannot see: a string's mood, what its
// id says about it, the host's own rules.
func (m *Manifest) uncheckedRules(p batchPrompt) []string {
	example := strings.SplitN(p.items[0].ID, formSeparator, 2)[0]
	rules := []string{
		`A string's grammatical mood is part of it. An imperative stays an imperative, a question stays a question, a label stays a label — "Save your changes" asks the reader to do something and must still ask it. A label is not a sentence and takes no final full stop the source does not have.`,
		fmt.Sprintf("Each item's id is <kind>:<path>, and the path is where the string appears in the product — %s, for one. Read it. For a short string it is often the only thing that says what the string is for.", example),
		"Translate the meaning, not the words. Write what a native speaker of this language would write in this product.",
		fmt.Sprintf("Write every word in %s; the only words of another language are the names the source keeps.", languageName(p.target)),
	}
	if len(m.Prompt.KeepVerbatim) > 0 {
		rules = append(rules, fmt.Sprintf("Names of other companies, platforms and products (%s) stay as their owners write them.", strings.Join(m.Prompt.KeepVerbatim, ", ")))
	}
	return append(rules, m.Prompt.Rules...)
}

// pluralCategories explains the form items, where the batch has any.
func pluralCategories(p batchPrompt) string {
	asked := false
	for _, item := range p.items {
		asked = asked || strings.Contains(item.ID, formSeparator)
	}
	if !asked {
		return ""
	}
	formIDs := make([]string, len(p.categories))
	for i, category := range p.categories {
		formIDs[i] = formSeparator + category
	}
	return fmt.Sprintf("Plural forms in %s: %s. A plural is never asked for whole: an item whose id ends in %s is one form of one, and its context says which numbers select it — write that one form, without `|`. A {count, plural, …} inside a sentence is a placeholder like any other: keep it exactly as written; its forms are separate items.",
		languageName(p.target), strings.Join(p.categories, ", "), strings.Join(formIDs, ", "))
}

// userPrompt carries the items themselves — id, source text, and whatever is
// known about where it appears, which is what stops a bare word being rendered
// as the wrong part of speech. It is pure JSON in the shape of the answer: the
// instructions are the system prompt's job, and mixing prose into the data
// leaves the model deciding which brackets are which.
func userPrompt(items []wireItem) (string, error) {
	payload := struct {
		Items []wireItem `json:"items"`
	}{Items: items}
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetIndent("", "  ")
	// Markup is shown as it is written. The model answers in the shape it is
	// shown: a tag it sees JSON-escaped comes back as an escape too, and a small
	// model does not always get the hex right — \u001c decodes to nothing the
	// validator recognises as a tag.
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(payload); err != nil {
		return "", err
	}
	return body.String(), nil
}

// languageName names a language in English, which is the language the prompt
// is written in. The tag is kept alongside it wherever a language is stated,
// so an ambiguous name (there are several kinds of Chinese) is never the only
// thing the model has to go on.
func languageName(locale string) string {
	tag, err := language.Parse(locale)
	if err != nil {
		return locale
	}
	if name := display.English.Tags().Name(tag); name != "" {
		return name
	}
	return locale
}

// maxTokensFor sizes the cap on one batch's answer, generously — a completion
// that hits it is truncated JSON that costs a whole second request to recover —
// but no further: the cap is also what ends a completion looping inside its
// answer, as truncated JSON and a retry, before the request's timeout does.
// The answer repeats every unit's id, which is longer than many of the strings
// themselves. What a model that reasons first spends on that is the
// deployment's to add (TRANSLATE_LLM_REASONING_TOKENS): it does not scale with
// the batch.
func maxTokensFor(items []wireItem) int {
	source := 0
	for _, item := range items {
		source += len(item.Text) + len(item.ID)
	}
	// Four characters to the token is the usual English ratio; ten times that
	// is the room a verbose script and the echoed ids need. The framing is on
	// top: no source length predicts it, and a batch of one short label is
	// mostly framing.
	return answerFraming + len(items)*itemFraming + source*10/4
}

// answerFraming and itemFraming are the answer's JSON around the text: the
// object and array holding the items, and each item's braces, keys and
// separators — spread over indented lines when a model pretty-prints.
const (
	answerFraming = 48
	itemFraming   = 32
)
