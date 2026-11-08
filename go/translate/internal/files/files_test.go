package files

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// collect is a sink that keeps what it is handed.
func collect(into *string) Sink {
	return func(path, body string) ([]string, error) {
		*into = body
		return []string{path}, nil
	}
}

// A YAML file opens with its header as `#` lines.
func TestWriteYAML(t *testing.T) {
	var body string
	_, err := WriteYAML(collect(&body), "x.yaml", "GENERATED\nby the tests", map[string]string{"a": "b"})
	require.NoError(t, err)
	assert.Equal(t, "# GENERATED\n# by the tests\na: b\n", body)
}

// An XML file opens with its declaration and the header as one comment, and
// is written only once it parses; a header carrying `--` is refused.
func TestWriteXML(t *testing.T) {
	var body string
	_, err := WriteXML(collect(&body), "x.xml", "GENERATED\nby the tests", "<resources/>\n")
	require.NoError(t, err)
	assert.Equal(t, "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<!--\n    GENERATED\n    by the tests\n-->\n<resources/>\n", body)
	_, err = WriteXML(collect(&body), "x.xml", "h", "<resources>")
	assert.ErrorContains(t, err, "x.xml: rendered XML does not parse back")
	_, err = WriteXML(collect(&body), "x.xml", "run translate --check", "<resources/>")
	assert.ErrorContains(t, err, "an XML comment cannot")
}

// JSON is laid out as Apple's pretty printer lays it out: two-space indents,
// `"key" : value`, keys in code-point order, an empty object or array as its
// brackets around a blank line, every character as itself but `"`, `\` and the
// C0 controls, and no newline at the end.
func TestWriteJSON(t *testing.T) {
	value, err := ReadJSON([]byte(`{"b":[1,"two",[],{}],"a":{"z":true,"y":null,"Ü":"é /\"\\\n\t\r\b\f\u0001"},"c":{}}`))
	require.NoError(t, err)
	var body string
	_, err = WriteJSON(collect(&body), "x.json", value)
	require.NoError(t, err)
	assert.Equal(t, "{\n"+
		"  \"a\" : {\n"+
		"    \"y\" : null,\n"+
		"    \"z\" : true,\n"+
		"    \"Ü\" : \"é /\\\"\\\\\\n\\t\\r\\b\\f\\u0001\"\n"+
		"  },\n"+
		"  \"b\" : [\n"+
		"    1,\n"+
		"    \"two\",\n"+
		"    [\n\n    ],\n"+
		"    {\n\n    }\n"+
		"  ],\n"+
		"  \"c\" : {\n\n  }\n"+
		"}", body)

	_, err = WriteJSON(collect(&body), "x.json", map[string]any{"n": 1})
	assert.EqualError(t, err, "x.json: int is not a JSON value")
	_, err = ReadJSON([]byte(`{} {}`))
	assert.EqualError(t, err, "more than one JSON value")
	_, err = ReadJSON([]byte(`{`))
	assert.Error(t, err)
	number, err := ReadJSON([]byte(`1.50`))
	require.NoError(t, err)
	assert.Equal(t, json.Number("1.50"), number, "a number as written")
}

// Write writes only bytes that differ, and Compare only says so.
func TestWriteAndCompare(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b.txt")
	differs, err := Compare(path, "x")
	require.NoError(t, err)
	assert.Equal(t, []string{path}, differs, "a missing file differs")
	written, err := Write(path, "x")
	require.NoError(t, err)
	assert.Equal(t, []string{path}, written)
	written, err = Write(path, "x")
	require.NoError(t, err)
	assert.Empty(t, written)
	removed, err := Remove([]string{path, path + ".missing"})
	require.NoError(t, err)
	assert.Equal(t, []string{path}, removed)
	assert.Len(t, Digest("a", "b"), 16)
	assert.NotEqual(t, Digest("ab"), Digest("a", "b"))
}
