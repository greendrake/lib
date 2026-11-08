package translate

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/language"
)

// scriptTables are the letters each writing system is made of, by the ISO
// 15924 code x/text gives as a locale's likely script. A script written with
// several — Japanese mixes kanji and two kana, Korean hangul and hanja — lists
// them all. A code without a row is a locale the engine cannot hold to its
// script, which the manifest refuses to ship.
var scriptTables = map[string][]*unicode.RangeTable{
	"Arab": {unicode.Arabic},
	"Armn": {unicode.Armenian},
	"Beng": {unicode.Bengali},
	"Cyrl": {unicode.Cyrillic},
	"Deva": {unicode.Devanagari},
	"Ethi": {unicode.Ethiopic},
	"Geor": {unicode.Georgian},
	"Grek": {unicode.Greek},
	"Gujr": {unicode.Gujarati},
	"Guru": {unicode.Gurmukhi},
	"Hans": {unicode.Han},
	"Hant": {unicode.Han},
	"Hebr": {unicode.Hebrew},
	"Jpan": {unicode.Han, unicode.Hiragana, unicode.Katakana},
	"Khmr": {unicode.Khmer},
	"Knda": {unicode.Kannada},
	"Kore": {unicode.Hangul, unicode.Han},
	"Laoo": {unicode.Lao},
	"Latn": {unicode.Latin},
	"Mlym": {unicode.Malayalam},
	"Mymr": {unicode.Myanmar},
	"Sinh": {unicode.Sinhala},
	"Taml": {unicode.Tamil},
	"Telu": {unicode.Telugu},
	"Thai": {unicode.Thai},
}

// scriptsOf is the letters a locale is written in.
func scriptsOf(locale string) ([]*unicode.RangeTable, error) {
	script, _ := language.Make(locale).Script()
	tables, ok := scriptTables[script.String()]
	if !ok {
		return nil, fmt.Errorf("%s is written in %s, a script the engine has no letters for", locale, script)
	}
	return tables, nil
}

// scriptOf names the script a letter belongs to, or "" for a letter of no
// script in particular: Common and Inherited hold the letters several
// languages share, such as Japanese ー, the ʻokina and the Ukrainian
// apostrophe ʼ.
func scriptOf(r rune) string {
	for name, table := range unicode.Scripts {
		if name != "Common" && name != "Inherited" && unicode.Is(table, r) {
			return name
		}
	}
	return ""
}

// scriptsHeld refuses a letter of a script the translation's language is not
// written in: a word of another language slipped into the text, which no
// other check sees. Latin passes, since names such as eBay stay as their
// owners write them, and so does any script the source itself carries. A word
// of another language in the target's own script is beyond it.
func scriptsHeld(source, candidate string, written []*unicode.RangeTable) error {
	allowed := func(r rune) bool {
		if !unicode.IsLetter(r) || unicode.In(r, unicode.Latin) || unicode.In(r, written...) {
			return true
		}
		script := scriptOf(r)
		return script == "" || strings.ContainsFunc(source, func(s rune) bool { return unicode.Is(unicode.Scripts[script], s) })
	}
	var words, scripts []string
	for _, word := range strings.FieldsFunc(candidate, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsMark(r) }) {
		if i := strings.IndexFunc(word, func(r rune) bool { return !allowed(r) }); i >= 0 && !slices.Contains(words, word) {
			words = append(words, word)
			if r, _ := utf8.DecodeRuneInString(word[i:]); !slices.Contains(scripts, scriptOf(r)) {
				scripts = append(scripts, scriptOf(r))
			}
		}
	}
	if len(words) == 0 {
		return nil
	}
	return fmt.Errorf("written in %s, which this language is not: %s — write every word in the language asked for, in its own script",
		strings.Join(scripts, " and "), strings.Join(words, " "))
}
