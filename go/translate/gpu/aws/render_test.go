package aws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"greendrake.nz/go/llm"
	"greendrake.nz/go/translate"
)

const hostManifest = `command: go tool translate -manifest translate.yaml
default: en-GB
shipped: [en-GB, de]
state: translation/state
policy:
  path: translation/policy.yaml
  brief: translation/brief.yaml
  register: translation/register.yaml
  glossary: translation/glossary
vocabulary: {id: gl, batch: {size: 100}, tier: strong}
llm: {env_prefix: TRANSLATE_LLM_, timeout: 7m, parallel: 2}
sources:
  - id: ui
    format: yaml-catalogue
    path: catalogue/{locale}.yaml
    syntax: icu
    markup: none
    tier: fast
`

// host is a host with two strings to translate into German.
func host(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range map[string]string{
		"translate.yaml":            hostManifest,
		"catalogue/en-GB.yaml":      "greeting: 'Hello, {name}'\nfarewell: Goodbye\n",
		"translation/policy.yaml":   "# No terms.\n",
		"translation/brief.yaml":    "brief: An app.\n",
		"translation/register.yaml": "de: Address the reader informally (du).\n",
	} {
		path := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}
	return dir
}

// vllm is a stand-in for vLLM behind the tunnel: it answers every item with
// its source, and keeps what it was asked.
type vllm struct {
	mu       sync.Mutex
	requests []map[string]any
	paths    []string
}

func (v *vllm) serve(t *testing.T) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			return
		}
		var request map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		v.mu.Lock()
		v.requests = append(v.requests, request)
		v.paths = append(v.paths, r.URL.Path)
		v.mu.Unlock()
		messages := request["messages"].([]any)
		var asked struct {
			Items []struct {
				ID   string `json:"id"`
				Text string `json:"text"`
			} `json:"items"`
		}
		require.NoError(t, json.Unmarshal([]byte(messages[len(messages)-1].(map[string]any)["content"].(string)), &asked))
		var answer struct {
			Items []map[string]string `json:"items"`
		}
		for _, item := range asked.Items {
			answer.Items = append(answer.Items, map[string]string{"id": item.ID, "text": item.Text})
		}
		content, err := json.Marshal(answer)
		require.NoError(t, err)
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": string(content)}, "finish_reason": "stop"}},
		}))
	}))
	t.Cleanup(server.Close)
	return server
}

// renderer renders the host's run, leasing through lease.
func renderer(dir string, log *lines, lease func(context.Context, Job) error) Renderer {
	return Renderer{
		Runs:    []Run{{Manifest: filepath.Join(dir, "translate.yaml")}},
		Options: translate.Options{Log: log.log},
		Profile: builtins["gemma4-31b"],
		Lease:   lease,
	}
}

// Strings to render take the lease, and every run renders against the model
// it serves: the profile's, through vLLM's grammar, with nothing of the
// manifest's llm block nor of the environment block it names.
func TestRender_StringsDueTakeTheLease(t *testing.T) {
	t.Setenv("TRANSLATE_LLM_PROVIDER", "anthropic")
	t.Setenv("TRANSLATE_LLM_REASONING_TOKENS", "50000")
	t.Setenv("TRANSLATE_LLM_MODEL_FAST", "claude-haiku-4-5")
	dir := host(t)
	model := &vllm{}
	server := model.serve(t)
	var log lines
	leases := 0
	err := renderer(dir, &log, func(ctx context.Context, job Job) error {
		leases++
		return job(ctx, server.URL)
	}).Render(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, leases)
	catalogue, err := os.ReadFile(filepath.Join(dir, "catalogue/de.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(catalogue), "greeting: Hello, {name}")
	require.NotEmpty(t, model.requests)
	for i, request := range model.requests {
		assert.Equal(t, "/v1/chat/completions", model.paths[i])
		assert.Equal(t, "gemma-4-31b", request["model"])
		assert.Equal(t, 1.0, request["temperature"])
		assert.Equal(t, 0.1, request["min_p"])
		assert.Contains(t, request, "structured_outputs")
		assert.Less(t, request["max_completion_tokens"], 50000.0, "no reasoning budget from the host's environment")
	}
	assert.Contains(t, log.get(), "check "+filepath.Join(dir, "translate.yaml"))
	assert.Contains(t, log.get(), filepath.Join(dir, "translate.yaml")+": 2 file(s) written", "the catalogue and the record")
}

// With nothing due, nothing launches; with files alone behind, every run
// renders here, without a model.
func TestRender_OnlyWhatIsDue(t *testing.T) {
	dir := host(t)
	model := &vllm{}
	server := model.serve(t)
	require.NoError(t, renderer(dir, &lines{}, func(ctx context.Context, job Job) error { return job(ctx, server.URL) }).Render(context.Background()))

	noLease := func(context.Context, Job) error { t.Fatal("nothing needed a model"); return nil }
	var log lines
	require.NoError(t, renderer(dir, &log, noLease).Render(context.Background()))
	assert.Contains(t, log.get(), "nothing to render — no instance launched")

	master := filepath.Join(dir, "catalogue/en-GB.yaml")
	require.NoError(t, os.WriteFile(master, []byte("farewell: Goodbye\ngreeting: 'Hello, {name}'\n"), 0o644))
	log = lines{}
	require.NoError(t, renderer(dir, &log, noLease).Render(context.Background()))
	assert.Contains(t, log.get(), "nothing needs a model — rendering here, without one")
	catalogue, err := os.ReadFile(filepath.Join(dir, "catalogue/de.yaml"))
	require.NoError(t, err)
	assert.Less(t, strings.Index(string(catalogue), "farewell"), strings.Index(string(catalogue), "greeting"), "the file follows the master")
}

// A run that cannot be read, or fails, is named.
func TestRender_Failures(t *testing.T) {
	missing := Renderer{Runs: []Run{{Manifest: filepath.Join(t.TempDir(), "translate.yaml")}}, Options: translate.Options{Log: (&lines{}).log}}
	assert.ErrorContains(t, missing.Render(context.Background()), "translate.yaml")

	dir := host(t)
	err := renderer(dir, &lines{}, func(ctx context.Context, job Job) error {
		return job(ctx, "http://127.0.0.1:1")
	}).Render(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), filepath.Join(dir, "translate.yaml")+": ")

	leaseErr := errors.New("no capacity")
	err = renderer(host(t), &lines{}, func(context.Context, Job) error { return leaseErr }).Render(context.Background())
	assert.ErrorIs(t, err, leaseErr)
}

// A run's model on the rented GPU is the tunnel's and the profile's, its
// request timeout among it.
func TestServed(t *testing.T) {
	profile := builtins["gemma4-26b"]
	assert.Equal(t, llm.Config{
		Provider:    llm.ProviderOpenAICompat,
		BaseURL:     "http://127.0.0.1:41234/v1",
		ModelFast:   "gemma-4-26b",
		ModelStrong: "gemma-4-26b",
		Extra:       profile.RequestExtra,
		Grammar:     true,
		Timeout:     60 * time.Minute,
	}, served(profile, "http://127.0.0.1:41234"))
}

func TestParseRun(t *testing.T) {
	run, err := ParseRun("translate.yaml")
	require.NoError(t, err)
	assert.Equal(t, Run{Manifest: "translate.yaml"}, run)
	assert.Equal(t, "translate.yaml", run.String())
	run, err = ParseRun("lib/translate.yaml,translate.yaml")
	require.NoError(t, err)
	assert.Equal(t, Run{Manifest: "lib/translate.yaml", Project: "translate.yaml"}, run)
	assert.Equal(t, "lib/translate.yaml for translate.yaml", run.String())
	for _, bad := range []string{"", ",translate.yaml", "a,b,c"} {
		_, err := ParseRun(bad)
		assert.ErrorContains(t, err, "want <manifest>[,<project manifest>]", bad)
	}
}
