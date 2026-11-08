package llm

import (
	"maps"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// envSet points the prefixed variables at the given values for one test, and
// clears the ambient OAuth credential unless the case asks for it — a machine
// that happens to hold one would otherwise change what these tests assert.
func envSet(t *testing.T, prefix string, values map[string]string) {
	t.Helper()
	for _, name := range []string{"PROVIDER", "BASE_URL", "API_KEY", "AUTH_TOKEN", "MODEL_FAST", "MODEL_STRONG", "TIMEOUT", "REQUEST_EXTRA", "REASONING_TOKENS", "GRAMMAR"} {
		t.Setenv(prefix+name, values[name])
	}
	t.Setenv(oauthEnv, values[oauthEnv])
}

func TestConfigFromEnv_UnsetIsOff(t *testing.T) {
	envSet(t, "TEST_LLM_", nil)
	cfg, err := ConfigFromEnv("TEST_LLM_")
	require.NoError(t, err)
	assert.Equal(t, ProviderOff, cfg.Provider)
}

// The prefix is what lets two deployments coexist — a service's and a build
// tool's — so it must be the only thing that decides which block is read.
func TestConfigFromEnv_ReadsItsOwnPrefixOnly(t *testing.T) {
	envSet(t, "TEST_LLM_", map[string]string{"PROVIDER": ProviderFake})
	envSet(t, "OTHER_LLM_", map[string]string{"PROVIDER": ProviderOpenAICompat, "BASE_URL": "http://x/v1", "MODEL_FAST": "m"})

	mine, err := ConfigFromEnv("TEST_LLM_")
	require.NoError(t, err)
	assert.Equal(t, ProviderFake, mine.Provider)
	assert.Empty(t, mine.BaseURL)

	theirs, err := ConfigFromEnv("OTHER_LLM_")
	require.NoError(t, err)
	assert.Equal(t, ProviderOpenAICompat, theirs.Provider)
	assert.Equal(t, "http://x/v1", theirs.BaseURL)
}

func TestConfigFromEnv_AnthropicDefaultsModelsAndDemandsKey(t *testing.T) {
	envSet(t, "TEST_LLM_", map[string]string{"PROVIDER": ProviderAnthropic})
	_, err := ConfigFromEnv("TEST_LLM_")
	assert.ErrorContains(t, err, "requires TEST_LLM_API_KEY, TEST_LLM_AUTH_TOKEN or "+oauthEnv)

	envSet(t, "TEST_LLM_", map[string]string{"PROVIDER": ProviderAnthropic, "API_KEY": "k"})
	cfg, err := ConfigFromEnv("TEST_LLM_")
	require.NoError(t, err)
	assert.Equal(t, defaultAnthropicFast, cfg.ModelFast)
	assert.Equal(t, defaultAnthropicStrong, cfg.ModelStrong)
}

func TestConfigFromEnv_OpenAICompatDemandsEndpointAndModel(t *testing.T) {
	envSet(t, "TEST_LLM_", map[string]string{"PROVIDER": ProviderOpenAICompat})
	_, err := ConfigFromEnv("TEST_LLM_")
	assert.ErrorContains(t, err, "requires TEST_LLM_BASE_URL")

	envSet(t, "TEST_LLM_", map[string]string{"PROVIDER": ProviderOpenAICompat, "BASE_URL": "http://x/v1"})
	_, err = ConfigFromEnv("TEST_LLM_")
	assert.ErrorContains(t, err, "requires TEST_LLM_MODEL_FAST")
}

// One model may serve both tiers, and naming it once is how a deployment says so.
func TestConfigFromEnv_StrongDefaultsToFast(t *testing.T) {
	envSet(t, "TEST_LLM_", map[string]string{"PROVIDER": ProviderOpenAICompat, "BASE_URL": "http://x/v1", "MODEL_FAST": "only"})
	cfg, err := ConfigFromEnv("TEST_LLM_")
	require.NoError(t, err)
	assert.Equal(t, "only", cfg.ModelStrong)
}

func TestConfigFromEnv_UnknownProviderErrs(t *testing.T) {
	envSet(t, "TEST_LLM_", map[string]string{"PROVIDER": "hallucinated"})
	_, err := ConfigFromEnv("TEST_LLM_")
	assert.ErrorContains(t, err, `invalid TEST_LLM_PROVIDER "hallucinated"`)
}

func TestNewOpenAICompat_TimeoutFromConfig(t *testing.T) {
	stated := newOpenAICompat(Config{BaseURL: "http://x/v1", ModelFast: "m", Timeout: 9 * time.Minute})
	assert.Equal(t, 9*time.Minute, stated.(*openAICompatClient).httpClient.Timeout)

	unstated := newOpenAICompat(Config{BaseURL: "http://x/v1", ModelFast: "m"})
	assert.Equal(t, defaultTimeout, unstated.(*openAICompatClient).httpClient.Timeout)
}

// A person who has signed in to this machine should not have to restate the
// credential in a config file, so the ambient OAuth token counts for the
// provider it belongs to — and either token outranks an API key, which is what
// a committed env file holds as its default.
func TestConfigFromEnv_Credentials(t *testing.T) {
	configure := func(provider string, values map[string]string) Config {
		values["PROVIDER"] = provider
		if provider == ProviderOpenAICompat {
			values["BASE_URL"], values["MODEL_FAST"] = "http://mac:1234/v1", "m"
		}
		envSet(t, "TEST_LLM_", values)
		cfg, err := ConfigFromEnv("TEST_LLM_")
		require.NoError(t, err)
		return cfg
	}

	t.Run("ambient token alone authenticates", func(t *testing.T) {
		cfg := configure(ProviderAnthropic, map[string]string{oauthEnv: "oat-ambient"})
		assert.Equal(t, "oat-ambient", cfg.AuthToken)
		assert.Empty(t, cfg.APIKey)
	})

	t.Run("a stated token outranks the ambient one", func(t *testing.T) {
		cfg := configure(ProviderAnthropic, map[string]string{"AUTH_TOKEN": "oat-stated", oauthEnv: "oat-ambient"})
		assert.Equal(t, "oat-stated", cfg.AuthToken)
	})

	t.Run("a token outranks a key", func(t *testing.T) {
		cfg := configure(ProviderAnthropic, map[string]string{"API_KEY": "sk-committed", oauthEnv: "oat-ambient"})
		assert.Equal(t, "oat-ambient", cfg.AuthToken)
		assert.Equal(t, "sk-committed", cfg.APIKey, "the key is still read; the adapter decides which to send")
	})

	t.Run("a key alone still authenticates", func(t *testing.T) {
		cfg := configure(ProviderAnthropic, map[string]string{"API_KEY": "sk-committed"})
		assert.Empty(t, cfg.AuthToken)
		assert.Equal(t, "sk-committed", cfg.APIKey)
	})

	// The ambient token is an Anthropic credential. An openai_compat base URL
	// is whatever the deployment says — a local runtime over plain HTTP — and
	// the token must not be sent there.
	t.Run("the ambient token goes to anthropic only", func(t *testing.T) {
		cfg := configure(ProviderOpenAICompat, map[string]string{oauthEnv: "oat-ambient"})
		assert.Empty(t, cfg.AuthToken)
	})

	t.Run("a stated token applies to any provider", func(t *testing.T) {
		cfg := configure(ProviderOpenAICompat, map[string]string{"AUTH_TOKEN": "oat-stated", oauthEnv: "oat-ambient"})
		assert.Equal(t, "oat-stated", cfg.AuthToken)
	})
}

// The per-request fields a runtime needs — temperature, a thinking toggle —
// ride in one JSON object rather than a variable each.
func TestConfigFromEnv_RequestExtra(t *testing.T) {
	base := map[string]string{"PROVIDER": ProviderOpenAICompat, "BASE_URL": "http://mac:1234/v1", "MODEL_FAST": "m"}
	with := func(extra string) map[string]string {
		values := maps.Clone(base)
		values["REQUEST_EXTRA"] = extra
		return values
	}

	envSet(t, "TEST_LLM_", with(`{"temperature": 0.2, "chat_template_kwargs": {"enable_thinking": false}}`))
	cfg, err := ConfigFromEnv("TEST_LLM_")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"temperature": 0.2, "chat_template_kwargs": map[string]any{"enable_thinking": false}}, cfg.Extra)

	envSet(t, "TEST_LLM_", base)
	cfg, err = ConfigFromEnv("TEST_LLM_")
	require.NoError(t, err)
	assert.Nil(t, cfg.Extra)

	for _, bad := range []string{`{"temperature": }`, `[0.2]`} {
		envSet(t, "TEST_LLM_", with(bad))
		_, err = ConfigFromEnv("TEST_LLM_")
		assert.ErrorContains(t, err, "TEST_LLM_REQUEST_EXTRA", bad)
	}
}

// Whether the endpoint takes a grammar is the deployment's to say, and only an
// openai_compat deployment can say it.
func TestConfigFromEnv_Grammar(t *testing.T) {
	openAI := map[string]string{"PROVIDER": ProviderOpenAICompat, "BASE_URL": "http://gpu:8000/v1", "MODEL_FAST": "m"}
	with := func(values map[string]string, grammar string) map[string]string {
		values = maps.Clone(values)
		values["GRAMMAR"] = grammar
		return values
	}

	envSet(t, "TEST_LLM_", openAI)
	cfg, err := ConfigFromEnv("TEST_LLM_")
	require.NoError(t, err)
	assert.False(t, cfg.Grammar)

	envSet(t, "TEST_LLM_", with(openAI, "true"))
	cfg, err = ConfigFromEnv("TEST_LLM_")
	require.NoError(t, err)
	assert.True(t, cfg.Grammar)

	envSet(t, "TEST_LLM_", with(openAI, "sometimes"))
	_, err = ConfigFromEnv("TEST_LLM_")
	assert.ErrorContains(t, err, `TEST_LLM_GRAMMAR "sometimes"`)

	envSet(t, "TEST_LLM_", with(map[string]string{"PROVIDER": ProviderAnthropic, "API_KEY": "k"}, "true"))
	_, err = ConfigFromEnv("TEST_LLM_")
	assert.ErrorContains(t, err, "TEST_LLM_GRAMMAR is for an openai_compat endpoint that takes vLLM's grammar, not TEST_LLM_PROVIDER=anthropic")
}

// How much a model reasons before it answers is the deployment's to say; one
// that says nothing answers directly.
func TestConfigFromEnv_ReasoningTokens(t *testing.T) {
	base := map[string]string{"PROVIDER": ProviderOpenAICompat, "BASE_URL": "http://mac:1234/v1", "MODEL_FAST": "m"}
	with := func(reasoning string) map[string]string {
		values := maps.Clone(base)
		values["REASONING_TOKENS"] = reasoning
		return values
	}

	envSet(t, "TEST_LLM_", base)
	cfg, err := ConfigFromEnv("TEST_LLM_")
	require.NoError(t, err)
	assert.Zero(t, cfg.Reasoning)

	envSet(t, "TEST_LLM_", with("32768"))
	cfg, err = ConfigFromEnv("TEST_LLM_")
	require.NoError(t, err)
	assert.Equal(t, 32768, cfg.Reasoning)

	for bad, why := range map[string]string{"lots": "invalid syntax", "-1": "cannot be negative"} {
		envSet(t, "TEST_LLM_", with(bad))
		_, err = ConfigFromEnv("TEST_LLM_")
		assert.ErrorContains(t, err, "TEST_LLM_REASONING_TOKENS", bad)
		assert.ErrorContains(t, err, why, bad)
	}
}
