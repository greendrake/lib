package llm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The string and character rules every grammar ends with: every escape JSON
// has but \u, and nothing unescaped that JSON forbids raw.
const stringRules = `string ::= "\"" char* "\""
char ::= [^"\\\x00-\x1F] | "\\" ["\\/bfnrt]
`

// Schemas in the shapes callers send: an answer listing items by an id enum,
// a verdict with a boolean and an enum, free strings beside a boolean, and an
// enum carrying "" beside an array of enum strings.
func TestGrammarOf_CompilesTheSchemasCallersSend(t *testing.T) {
	cases := map[string]struct{ schema, want string }{
		"items by id": {`{
			"type": "object",
			"properties": {
				"items": {
					"type": "array",
					"items": {
						"type": "object",
						"properties": {
							"id": {"type": "string", "enum": ["ui:a.b", "content:/FAQ#3a"]},
							"text": {"type": "string"}
						},
						"required": ["id", "text"],
						"additionalProperties": false
					}
				}
			},
			"required": ["items"],
			"additionalProperties": false
		}`, `root ::= "{\"items\": " "[" (item1 (", " item1)*)? "]" "}"
item1 ::= "{\"id\": " ("\"ui:a.b\"" | "\"content:/FAQ#3a\"") ", \"text\": " string "}"
`},
		"verdict": {`{
			"type": "object",
			"properties": {
				"violates": {"type": "boolean", "description": "True only when the text breaches the stated policy."},
				"category": {"type": "string", "description": "The breached policy category, or \"\" when nothing is breached."},
				"rationale": {"type": "string"},
				"confidence": {"type": "string", "enum": ["low", "high"]}
			},
			"required": ["category", "confidence", "rationale", "violates"],
			"additionalProperties": false
		}`, `root ::= "{\"violates\": " ("true" | "false") ", \"category\": " string ", \"rationale\": " string ", \"confidence\": " ("\"low\"" | "\"high\"") "}"
`},
		"free strings": {`{"additionalProperties":false,"properties":{"brand":{"description":"The brand, or \"\".","type":"string"},"category":{"type":"string"},"confident":{"type":"boolean"}},"required":["brand","category","confident"],"type":"object"}`,
			`root ::= "{\"brand\": " string ", \"category\": " string ", \"confident\": " ("true" | "false") "}"
`},
		"options": {`{"additionalProperties":false,"properties":{"colour":{"items":{"enum":["colour:red","colour:blue"],"type":"string"},"type":"array"},"condition":{"enum":["","condition:new"],"type":"string"}},"required":["colour","condition"],"type":"object"}`,
			`root ::= "{\"colour\": " "[" (item1 (", " item1)*)? "]" ", \"condition\": " ("\"\"" | "\"condition:new\"") "}"
item1 ::= ("\"colour:red\"" | "\"colour:blue\"")
`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := grammarOf(json.RawMessage(tc.schema))
			require.NoError(t, err)
			assert.Equal(t, tc.want+stringRules, got)
		})
	}
}

// A schema the grammar cannot express exactly is refused before a request is
// sent, rather than compiled into a grammar looser or stricter than the
// schema a completion is validated against.
func TestGrammarOf_RefusesWhatItCannotExpress(t *testing.T) {
	cases := map[string]struct{ schema, want string }{
		"unknown keyword":     {`{"type": "string", "maxLength": 3}`, `schema keyword "maxLength" is not supported`},
		"unknown type":        {`{"type": "number"}`, `root: type "number" is not supported`},
		"no type":             {`{"description": "anything"}`, `root: type "" is not supported`},
		"optional property":   {`{"type": "object", "properties": {"a": {"type": "string"}, "b": {"type": "string"}}, "required": ["a"], "additionalProperties": false}`, "root: every property must be required, and nothing else"},
		"open object":         {`{"type": "object", "properties": {"a": {"type": "string"}}, "required": ["a"]}`, "root: an object must set additionalProperties to false"},
		"enum on a boolean":   {`{"type": "boolean", "enum": [true]}`, "enum"},
		"empty enum":          {`{"type": "string", "enum": []}`, "root: an empty enum admits nothing"},
		"array without items": {`{"type": "array"}`, "root: an array needs items"},
		"nested refusal":      {`{"type": "object", "properties": {"a": {"type": "array", "items": {"type": "integer"}}}, "required": ["a"], "additionalProperties": false}`, `root.a[]: type "integer" is not supported`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := grammarOf(json.RawMessage(tc.schema))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// An enum value is written as a completion writes it: JSON with nothing
// HTML-escaped, since the string grammar has no \u escape to spell it with.
func TestGrammarOf_EnumLiteralsAreWrittenPlainly(t *testing.T) {
	got, err := grammarOf(json.RawMessage(`{"type": "string", "enum": ["a<b", "say \"hi\"", "C:\\"]}`))
	require.NoError(t, err)
	assert.Contains(t, got, `root ::= ("\"a<b\"" | "\"say \\\"hi\\\"\"" | "\"C:\\\\\"")`)
}
