// Package llm is the pluggable LLM boundary: exactly one capability — prompt
// in, schema-conforming JSON out — behind one interface, so swapping the
// remote API for a local model is a config change, never a caller change. Call
// sites request capability tiers, not models; each adapter maps tiers to
// concrete models. Every constructed client is wrapped in WithValidation,
// which enforces the schema uniformly regardless of how well a given backend's
// constrained decoding works.
//
// Each consumer configures its own Config — a service classifying at runtime
// and a build tool translating need not agree on a provider.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
)

// Tier is a capability request — never a model name. Adapters map tiers to
// concrete models via configuration; a deployment may map both tiers to the
// same model, in which case escalation degenerates to a retry.
type Tier string

const (
	// TierFast serves the bulk classification calls.
	TierFast Tier = "fast"
	// TierStrong serves escalations when a fast-tier result reports low
	// confidence.
	TierStrong Tier = "strong"
)

// Request is one structured completion: a prompt plus the JSON Schema the
// completion must satisfy.
type Request struct {
	System string
	User   string
	// Schema is the JSON Schema the completion must conform to. Adapters pass
	// it to their backend's constrained-output mechanism; WithValidation
	// additionally verifies the result against it.
	Schema json.RawMessage
	Tier   Tier
	// Label says what this completion is for (e.g. "auto-tag MC8NXF8DD6
	// category"). It never reaches the backend: it is the correlation handle in
	// the debug log, without which concurrent pipelines interleave into mush.
	Label string
	// MaxTokens caps this completion's answer; the adapter adds the room the
	// deployment's model reasons in (Config.Reasoning). The Anthropic API
	// requires a cap, and the classification-sized default a caller stating
	// none gets would truncate a translated content page into invalid JSON.
	// The OpenAI-compatible API requires none and is sent one all the same, as
	// a ceiling: a schema-constrained completion normally ends when the schema
	// closes, but a model that loops inside a string runs until the server or
	// the request timeout gives up, and the cap turns that into truncated JSON
	// — a validation failure and a retry, not a wait for the timeout. Zero
	// means the default.
	MaxTokens int
	// KeepTruncated says the caller can use what a cut-off answer finished: a
	// completion cut off at the cap is then not asked for again whole but
	// returned as a *TruncatedError carrying it. A caller asking for many
	// independent items in one completion keeps those written before the cut
	// and asks again for the rest, where a whole retry would pay for the same
	// loop over again.
	KeepTruncated bool
}

// defaultMaxTokens bounds one structured completion's answer where the caller
// states no Request.MaxTokens. Sized for the classification outputs — small
// JSON objects — so a completion that hits it is truncated JSON, fails schema
// validation, and surfaces as abstention.
const defaultMaxTokens = 4096

// capOf is the cap a request goes out with: its answer's, or the default, and
// the room the deployment's model reasons in before it.
func capOf(req Request, reasoning int) int {
	answer := req.MaxTokens
	if answer <= 0 {
		answer = defaultMaxTokens
	}
	return answer + reasoning
}

// Usage is what a backend reports about producing one completion. Zero when the
// backend reports nothing.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// Completion is one backend response: the JSON the caller asked for, plus the
// facts only the adapter knows about how it was produced.
type Completion struct {
	JSON  json.RawMessage
	Model string
	Usage Usage
	// Truncated reports a completion the backend ended at the cap rather than
	// where the model finished it: cut off, and as a rule invalid JSON.
	Truncated bool
}

// TruncatedError is a completion cut off at the cap before it satisfied the
// schema, returned for a request that keeps one (Request.KeepTruncated): its
// JSON as far as the model got.
type TruncatedError struct {
	Completion Completion
}

func (e *TruncatedError) Error() string {
	return "cut off at the length limit before it was complete"
}

// Client renders structured completions. Implementations: the anthropic and
// openAICompat adapters (production), and Fake (tests / dev).
type Client interface {
	Complete(ctx context.Context, req Request) (Completion, error)
}

// DebugGate reports whether verbose logging of prompts, completions and token
// usage is on. Evaluated per call, so flipping the dynamic-config switch
// behind it takes effect without a restart.
type DebugGate func(ctx context.Context) bool

// Provider values accepted by New (config LLM_PROVIDER).
const (
	ProviderOff          = "off"
	ProviderFake         = "fake"
	ProviderAnthropic    = "anthropic"
	ProviderOpenAICompat = "openai_compat"
)

// New constructs the configured provider's client wrapped in schema
// validation. ProviderOff yields (nil, nil) — the feature is unconfigured and
// callers must treat a nil client as such. debug gates the verbose exchange
// log and may be nil (never verbose).
func New(cfg Config, debug DebugGate) (Client, error) {
	switch cfg.Provider {
	case ProviderOff:
		return nil, nil
	case ProviderFake:
		return WithValidation(&Fake{}, debug), nil
	case ProviderAnthropic:
		return WithValidation(newAnthropic(cfg), debug), nil
	case ProviderOpenAICompat:
		return WithValidation(newOpenAICompat(cfg), debug), nil
	default:
		return nil, fmt.Errorf("unknown llm provider %q", cfg.Provider)
	}
}

// truncate bounds a payload quoted into a log line or an error.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// modelFor resolves a tier to the adapter's configured model. An unknown tier
// is a programming error and errs hard.
func modelFor(models map[Tier]string, tier Tier) (string, error) {
	m, ok := models[tier]
	if !ok || m == "" {
		return "", fmt.Errorf("no model configured for llm tier %q", tier)
	}
	return m, nil
}
