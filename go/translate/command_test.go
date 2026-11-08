package translate

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"greendrake.nz/go/llm"
)

// A project's manifest opens on its own; a library's only on behalf of the
// project it runs for, in that project's locales.
func TestOpen(t *testing.T) {
	host, lib := fixture(t, "de", "fr"), libraryTree(t)
	project, err := Open(filepath.Join(host, "translate.yaml"), "")
	require.NoError(t, err)
	assert.Equal(t, []string{"en-GB", "de", "fr"}, project.Shipped)

	library, err := Open(filepath.Join(lib, "translate.yaml"), filepath.Join(host, "translate.yaml"))
	require.NoError(t, err)
	assert.Equal(t, project.Shipped, library.Shipped)

	_, err = Open(filepath.Join(lib, "translate.yaml"), "")
	assert.ErrorContains(t, err, "declares no locales")
	_, err = Open(filepath.Join(lib, "translate.yaml"), filepath.Join(host, "missing.yaml"))
	assert.ErrorContains(t, err, "missing.yaml")
}

// A check's verdict: strings to render while any are owed, files alone once
// only a run's rewriting is, and current after that — each file behind named.
func TestCheck_Verdicts(t *testing.T) {
	dir := fixture(t, "de")
	verdict, err := Check(context.Background(), load(t, dir), Options{})
	require.NoError(t, err)
	assert.Equal(t, StringsDue, verdict)

	c, _ := scriptedClient(t)
	mustRun(t, dir, Options{Client: c})
	verdict, err = Check(context.Background(), load(t, dir), Options{})
	require.NoError(t, err)
	assert.Equal(t, Current, verdict)

	replaceInFile(t, filepath.Join(dir, "catalogue/en-GB.yaml"), "    submit: Submit\n    cancel: Cancel\n", "    cancel: Cancel\n    submit: Submit\n")
	var lines []string
	verdict, err = Check(context.Background(), load(t, dir), Options{Log: func(line string) { lines = append(lines, line) }})
	require.NoError(t, err)
	assert.Equal(t, FilesBehind, verdict)
	assert.Contains(t, lines, filepath.Join(dir, "catalogue/de.yaml")+" is not what a run would write")

	_, err = Check(context.Background(), load(t, dir), Options{Adopt: true})
	assert.ErrorContains(t, err, "different questions")
}

// The manifest's own model is the provider its environment block names; with
// none named there is no client, and a block that cannot be read is refused.
func TestManifest_Client(t *testing.T) {
	m := load(t, fixture(t, "de"))

	t.Setenv("TRANSLATE_LLM_PROVIDER", "fake")
	client, err := m.Client(false)
	require.NoError(t, err)
	assert.NotNil(t, client)

	t.Setenv("TRANSLATE_LLM_PROVIDER", "off")
	client, err = m.Client(true)
	require.NoError(t, err)
	assert.Nil(t, client)

	t.Setenv("TRANSLATE_LLM_PROVIDER", "openai_compat")
	t.Setenv("TRANSLATE_LLM_BASE_URL", "")
	_, err = m.Client(false)
	assert.ErrorContains(t, err, "TRANSLATE_LLM_PROVIDER=openai_compat requires TRANSLATE_LLM_BASE_URL")

	_, err = NewClient(llm.Config{Provider: "carrier pigeon"}, false)
	assert.ErrorContains(t, err, `unknown llm provider "carrier pigeon"`)
}
