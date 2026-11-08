# greendrake.nz/go/llm

One capability behind one interface: a prompt in, JSON conforming to a schema
out. A call site asks for a capability tier, never a model; which provider
serves it, reached how and mapped to which models, is configuration — so
swapping a remote API for a local model changes no caller.

```go
cfg, err := llm.ConfigFromEnv("LLM_")
client, err := llm.New(cfg, nil) // nil: the provider is off
completion, err := client.Complete(ctx, llm.Request{
	System: "Classify the listing.",
	User:   text,
	Schema: schema, // JSON Schema the answer must satisfy
	Tier:   llm.TierFast,
	Label:  "classify " + id,
})
```

## Requests and completions

- `Request` — `System` and `User` prompts, the `Schema` the completion must
  satisfy, the `Tier` (`TierFast` for the bulk of calls, `TierStrong` to
  escalate), a `Label` naming the call in the debug log, `MaxTokens` capping
  the answer (zero is a classification-sized default), and `KeepTruncated` for
  a caller that can use what an answer cut off at the cap finished.
- `Completion` — the `JSON`, the `Model` that produced it, the `Usage` the
  backend reported, and whether it was `Truncated`.
- `*TruncatedError` — what a request that keeps a cut-off answer gets instead
  of a retry: the completion as far as it got.

## Clients

`New(cfg, debug)` builds the client for `cfg.Provider` — one of the
`Provider*` constants below, `<prefix>PROVIDER` when the configuration is read
from the environment — wrapped in `WithValidation`; `ProviderOff` (`off`, the
unset default) yields a nil client, which callers treat as the feature
unconfigured. `debug` gates the verbose exchange log per call, and may be nil.
The providers:

- `anthropic` (`ProviderAnthropic`) — Anthropic's API through its structured
  outputs; models default to the current fast and strong picks.
- `openai_compat` (`ProviderOpenAICompat`) — any OpenAI-compatible
  chat-completions endpoint, selected
  by its base URL: the OpenAI API, or a local runtime (llama.cpp, Ollama,
  vLLM, LM Studio). With `Grammar`, a vLLM endpoint is sent the grammar the
  schema compiles to — strings without `\u` escapes, no free whitespace —
  rather than the schema, which the backend would compile into a grammar
  allowing both.
- `fake` (`ProviderFake`) — `Fake`, the deterministic in-process client:
  scripted by its `Handler`, its `Calls` recorded; unscripted, every call errs.

`WithValidation(client, debug)` holds every completion to its request's schema
whatever the backend's constrained decoding did: a miss is retried once, told
what was wrong, and a second miss is an error. It is also where the exchange
log is written — prompt, completion and token usage under the debug gate,
schema misses always.

## Configuration

`Config` is one deployment: `Provider`, `BaseURL`, the credential (`APIKey`,
or `AuthToken`, which wins), `ModelFast` and `ModelStrong` (the strong tier
defaults to the fast one), the request `Timeout`, the `Reasoning` room a model
that thinks first spends against the cap, `Extra` fields merged into every
OpenAI-compatible request (sampling, a runtime's thinking toggle), `Grammar`,
and the HTTP `Transport` every request goes through.

`ConfigFromEnv(prefix)` reads one from the environment variables under a
prefix, and validates it:

| Variable | |
|---|---|
| `<prefix>PROVIDER` | `off` (unset), `fake`, `anthropic` or `openai_compat` |
| `<prefix>BASE_URL` | required by `openai_compat`, version prefix included |
| `<prefix>API_KEY`, `<prefix>AUTH_TOKEN` | the credential; `anthropic` requires one, or `CLAUDE_CODE_OAUTH_TOKEN` |
| `<prefix>MODEL_FAST`, `<prefix>MODEL_STRONG` | the tiers' models; `openai_compat` requires the fast one |
| `<prefix>TIMEOUT` | a Go duration bounding one request |
| `<prefix>REASONING_TOKENS` | the room a thinking model spends before it answers |
| `<prefix>REQUEST_EXTRA` | a JSON object merged into every OpenAI-compatible request |
| `<prefix>GRAMMAR` | `true` for a vLLM endpoint taking the grammar |

One prefix is one deployment, so several coexist: a running service
classifying through one while a build tool translates through another.
