package aws

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func profileFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "profiles.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

const customProfile = `
  instance_type: g6e.xlarge
  image: vllm/vllm-openai:v0.31.0
  model: acme/model
  revision: 0123abcd
  served_name: acme
  server_args: [--max-model-len, "32768"]
  request_extra: {temperature: 0.7}
  parallel: 8
  request_timeout_min: 15
  max_lifetime_min: 120
  idle_min: 10
  boot_deadline_min: 20
`

// The built-ins are the Gemma 4 profiles; a profile file adds one and
// overrides another whole.
func TestProfiles(t *testing.T) {
	builtin, err := Profiles("")
	require.NoError(t, err)
	assert.Equal(t, []string{"gemma4-26b", "gemma4-31b"}, sortedNames(builtin))
	gemma := builtin["gemma4-31b"]
	assert.Equal(t, "google/gemma-4-31B-it", gemma.Model)
	assert.Equal(t, "842da3794eaa0b77d5f08bae87a17459d91ff475", gemma.Revision)
	assert.Equal(t, "gemma-4-31b", gemma.ServedName)
	assert.Equal(t, "vllm/vllm-openai:v0.30.0", gemma.Image)
	assert.Equal(t, map[string]any{"temperature": 1.0, "top_p": 0.95, "top_k": 64, "min_p": 0.1}, gemma.RequestExtra)
	assert.Equal(t, 22, gemma.Parallel)
	assert.Equal(t, 60, gemma.RequestTimeoutMin)
	assert.Equal(t, "gemma-4-26b", builtin["gemma4-26b"].ServedName)

	profiles, err := Profiles(profileFile(t, "acme:"+customProfile+"gemma4-26b:"+customProfile))
	require.NoError(t, err)
	assert.Equal(t, []string{"acme", "gemma4-26b", "gemma4-31b"}, sortedNames(profiles))
	assert.Equal(t, "acme/model", profiles["gemma4-26b"].Model, "overridden whole")
	assert.Equal(t, map[string]any{"temperature": 0.7}, profiles["acme"].RequestExtra)
	assert.Equal(t, gemma, profiles["gemma4-31b"])
	assert.Equal(t, []string{"g6e.xlarge", "g7e.2xlarge"}, InstanceTypes(profiles))

	empty, err := Profiles(profileFile(t, ""))
	require.NoError(t, err)
	assert.Len(t, empty, 2)
}

// A profile file is validated as it is read.
func TestProfiles_Refusals(t *testing.T) {
	for name, tc := range map[string]struct{ body, want string }{
		"an unknown key":       {"acme:" + customProfile + "  gpu: h100\n", "field gpu not found"},
		"a field missing":      {"acme:" + strings.Replace(customProfile, "  served_name: acme\n", "", 1), "profile acme: `served_name` is required"},
		"a count not positive": {"acme:" + strings.Replace(customProfile, "parallel: 8", "parallel: 0", 1), "profile acme: `parallel` must be positive"},
		"no request timeout":   {"acme:" + strings.Replace(customProfile, "  request_timeout_min: 15\n", "", 1), "profile acme: `request_timeout_min` must be positive"},
		"a boot past the life": {"acme:" + strings.Replace(customProfile, "boot_deadline_min: 20", "boot_deadline_min: 120", 1), "`boot_deadline_min` must be shorter than `max_lifetime_min`"},
		"not a mapping":        {"- acme\n", "cannot unmarshal"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Profiles(profileFile(t, tc.body))
			assert.ErrorContains(t, err, tc.want)
		})
	}
	_, err := Profiles(filepath.Join(t.TempDir(), "missing.yaml"))
	assert.ErrorContains(t, err, "missing.yaml")
}

// A profile is picked by name, and a name none has is refused with the names
// there are.
func TestPick(t *testing.T) {
	profile, err := Pick(builtins, "gemma4-26b")
	require.NoError(t, err)
	assert.Equal(t, builtins["gemma4-26b"], profile)
	_, err = Pick(builtins, "llama")
	assert.EqualError(t, err, `no profile "llama": the profiles are gemma4-26b, gemma4-31b`)
}

// The boot script runs with the profile's fields it reads set first, each as
// the shell reads it back exactly.
func TestUserData(t *testing.T) {
	p := builtins["gemma4-31b"]
	p.ServedName = "it's"
	data := p.userData()
	assert.True(t, strings.HasPrefix(data, "#!/bin/bash\n"+
		"IMAGE='vllm/vllm-openai:v0.30.0'\n"+
		"MODEL='google/gemma-4-31B-it'\n"+
		"REVISION='842da3794eaa0b77d5f08bae87a17459d91ff475'\n"+
		`SERVED_NAME='it'"'"'s'`+"\n"+
		"SERVER_ARGS=('--max-model-len' '65536' '--gpu-memory-utilization' '0.92')\n"+
		"MAX_LIFETIME_MIN=360\nIDLE_MIN=20\nBOOT_DEADLINE_MIN=30\n"+
		"# shellcheck shell=bash\n"), data)
	assert.True(t, strings.HasSuffix(data, instanceScript))
	assert.Contains(t, instanceScript, `sed -u 's/^/`+consoleTag+`/'`, "the script tags its console lines as the lease reads them")
	assert.Contains(t, instanceScript, "127.0.0.1:8000", "vLLM listens where the tunnel forwards")
}

func sortedNames(profiles map[string]Profile) []string { return slices.Sorted(maps.Keys(profiles)) }
