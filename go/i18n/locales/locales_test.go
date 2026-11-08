package locales

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func manifest(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "translate.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

func TestLoad_ReadsTheDeclarationAndIgnoresTheRest(t *testing.T) {
	set, err := Load(manifest(t, "command: bin/x translate\ndefault: en-GB\nshipped: [en-GB, es-419, zh-Hant, nb]\nsources: []\n"))
	require.NoError(t, err)
	assert.Equal(t, "en-GB", set.Default)
	assert.Equal(t, []string{"en-GB", "es-419", "zh-Hant", "nb"}, set.Shipped)
}

func TestLoad_Rejections(t *testing.T) {
	cases := map[string]string{
		"default not shipped": "default: fr\nshipped: [en-GB]\n",
		"nothing shipped":     "default: en-GB\n",
		"non-canonical tag":   "default: en-GB\nshipped: [en-GB, en_US]\n",
		"not YAML":            "default: [\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(manifest(t, body))
			assert.Error(t, err)
		})
	}
	_, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	assert.Error(t, err)
}

func TestResolve(t *testing.T) {
	set, err := New([]string{"en-GB", "es-419", "zh-Hant", "nb"}, "en-GB")
	require.NoError(t, err)
	for requested, want := range map[string]string{
		"es-MX":    "es-419",
		"zh-TW":    "zh-Hant",
		"no":       "nb",
		"da":       "en-GB",
		"":         "en-GB",
		"garbage!": "en-GB",
	} {
		assert.Equal(t, want, set.Resolve(requested), "resolving %q", requested)
	}
}
