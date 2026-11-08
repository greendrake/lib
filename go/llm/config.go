package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"
)

// defaultTimeout bounds one openai_compat HTTP request when a caller states
// none. Local models on modest hardware can take a while to fill a constrained
// completion; remote OpenAI-compatible endpoints are far under this.
const defaultTimeout = 120 * time.Second

// Default models for the anthropic adapter, one per tier — what a deployment
// that names the provider and nothing else gets.
const (
	defaultAnthropicFast   = "claude-haiku-4-5"
	defaultAnthropicStrong = "claude-opus-4-8"
)

// Config is one LLM deployment: which adapter, reached how, mapped to which
// models. Every consumer configures a client this way, so the rules about what
// a provider requires live here rather than at each consumer's env block.
type Config struct {
	// Provider selects the adapter — one of the Provider* constants.
	Provider string
	// BaseURL is required by openai_compat (it includes the version prefix,
	// e.g. https://api.openai.com/v1) and an optional override for anthropic.
	BaseURL string
	// APIKey and AuthToken are the two ways a backend accepts a credential, and
	// they are not interchangeable: an API key rides in its own header, a token
	// rides as a bearer. AuthToken wins where both are set — a key sits in a
	// committed env file as the default, while a token is something a person
	// exported into this shell deliberately.
	APIKey    string
	AuthToken string
	// The capability-tier mapping. ModelStrong defaults to ModelFast: one
	// model may serve both tiers, in which case escalation is a retry.
	ModelFast   string
	ModelStrong string
	// Timeout bounds one openai_compat HTTP request. Zero means defaultTimeout;
	// a consumer whose completions are large (a translated content page) states
	// its own, or the deployment does through <prefix>TIMEOUT.
	Timeout time.Duration
	// Reasoning is the room, in tokens, the deployment's model spends reasoning
	// before it answers, which bills against the same cap as the answer. Every
	// adapter adds it to the cap a request states for its answer. Zero — a
	// model that answers directly — keeps the cap to the answer, which is what
	// ends a completion looping inside its answer quickly.
	Reasoning int
	// Extra is merged into every openai_compat chat request body: the
	// per-request fields the OpenAI dialect leaves to the deployment —
	// temperature, sampling, a runtime's thinking toggle
	// (chat_template_kwargs). One mechanism for any runtime rather than a knob
	// per field. The adapter's own fields (model, messages, the output
	// constraint) win over a key here; anthropic ignores it.
	Extra map[string]any
	// Grammar says the openai_compat endpoint takes vLLM's
	// structured_outputs.grammar: each request's schema then goes as the
	// grammar grammarOf compiles it to — strings with no \u escape, no free
	// whitespace — rather than as response_format json_schema, which the
	// backend would compile into a grammar allowing both. Only openai_compat
	// has it.
	Grammar bool
	// Transport carries every request either adapter makes; nil is
	// http.DefaultTransport. It is how a host puts its own instrumentation —
	// an outbound request log, say — around the calls without this package
	// knowing about it.
	Transport http.RoundTripper
}

// transport is the round tripper a configuration's requests go through.
func (c Config) transport() http.RoundTripper {
	if c.Transport == nil {
		return http.DefaultTransport
	}
	return c.Transport
}

// oauthEnv names the ambient OAuth credential this machine may already hold.
// It carries no prefix because it belongs to the person rather than to any one
// deployment: whatever they run here authenticates as them. It is an Anthropic
// credential, so it is folded in for the anthropic provider and no other — an
// openai_compat base URL is whatever the deployment says, often a local
// plain-HTTP runtime, and the token must never be sent there.
const oauthEnv = "CLAUDE_CODE_OAUTH_TOKEN"

// ConfigFromEnv reads one configuration from the environment variables under
// prefix (e.g. "LLM_") and validates it: "off" and "fake" carry no credential
// requirements, "anthropic" demands a credential (models default to the
// current tier picks), "openai_compat" demands the endpoint and at least the
// fast model (nothing sensible to default to).
//
// One prefix is one deployment, which is why several may coexist: a running
// service's LLM_* beside a build tool's TOOL_LLM_*, each at the provider that
// suits its work.
func ConfigFromEnv(prefix string) (Config, error) {
	get := func(name string) string { return os.Getenv(prefix + name) }
	timeout, err := optional(prefix, "TIMEOUT", get("TIMEOUT"), time.ParseDuration)
	if err != nil {
		return Config{}, err
	}
	extra, err := optional(prefix, "REQUEST_EXTRA", get("REQUEST_EXTRA"), func(value string) (map[string]any, error) {
		var fields map[string]any
		return fields, json.Unmarshal([]byte(value), &fields)
	})
	if err != nil {
		return Config{}, err
	}
	reasoning, err := optional(prefix, "REASONING_TOKENS", get("REASONING_TOKENS"), func(value string) (int, error) {
		n, err := strconv.Atoi(value)
		if err == nil && n < 0 {
			err = errors.New("a count of tokens cannot be negative")
		}
		return n, err
	})
	if err != nil {
		return Config{}, err
	}
	grammar, err := optional(prefix, "GRAMMAR", get("GRAMMAR"), strconv.ParseBool)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Timeout:     timeout,
		Extra:       extra,
		Reasoning:   reasoning,
		Grammar:     grammar,
		Provider:    get("PROVIDER"),
		BaseURL:     get("BASE_URL"),
		APIKey:      get("API_KEY"),
		AuthToken:   get("AUTH_TOKEN"),
		ModelFast:   get("MODEL_FAST"),
		ModelStrong: get("MODEL_STRONG"),
	}
	if cfg.Provider == "" {
		cfg.Provider = ProviderOff
	}
	switch cfg.Provider {
	case ProviderOff, ProviderFake:
	case ProviderAnthropic:
		if cfg.AuthToken == "" {
			cfg.AuthToken = os.Getenv(oauthEnv)
		}
		if cfg.APIKey == "" && cfg.AuthToken == "" {
			return Config{}, fmt.Errorf("%sPROVIDER=anthropic requires %sAPI_KEY, %sAUTH_TOKEN or %s", prefix, prefix, prefix, oauthEnv)
		}
		if cfg.ModelFast == "" {
			cfg.ModelFast = defaultAnthropicFast
		}
		if cfg.ModelStrong == "" {
			cfg.ModelStrong = defaultAnthropicStrong
		}
	case ProviderOpenAICompat:
		if cfg.BaseURL == "" {
			return Config{}, fmt.Errorf("%sPROVIDER=openai_compat requires %sBASE_URL", prefix, prefix)
		}
		if cfg.ModelFast == "" {
			return Config{}, fmt.Errorf("%sPROVIDER=openai_compat requires %sMODEL_FAST", prefix, prefix)
		}
	default:
		return Config{}, fmt.Errorf("invalid %sPROVIDER %q (off|fake|anthropic|openai_compat)", prefix, cfg.Provider)
	}
	if cfg.Grammar && cfg.Provider != ProviderOpenAICompat {
		return Config{}, fmt.Errorf("%sGRAMMAR is for an openai_compat endpoint that takes vLLM's grammar, not %sPROVIDER=%s", prefix, prefix, cfg.Provider)
	}
	if cfg.ModelStrong == "" {
		cfg.ModelStrong = cfg.ModelFast
	}
	return cfg, nil
}

// optional parses a variable a deployment may leave unset, naming it when it is
// set to something the parser refuses.
func optional[T any](prefix, name, value string, parse func(string) (T, error)) (T, error) {
	var zero T
	if value == "" {
		return zero, nil
	}
	parsed, err := parse(value)
	if err != nil {
		return zero, fmt.Errorf("%s%s %q: %w", prefix, name, value, err)
	}
	return parsed, nil
}

// firstNonEmpty is the credential precedence: the first source that has
// anything to say wins.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
