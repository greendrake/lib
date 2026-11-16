# greendrake.nz/go/llm

For a program that uses a language model as a function: a prompt goes in, and
JSON that satisfies a schema comes out. Which provider answers, and with which
models, is configuration, so the calling code is the same against a remote API
and against a model running locally.

```sh
go get greendrake.nz/go/llm
```

## Why

A program that classifies, extracts or translates with a model does not want a
conversation. It wants data it can decode, in a shape it chose. Calling a
provider's API at each call site gets that, but ties every call site to one
provider and its model names. Each backend also constrains output its own way,
and how strictly it enforces the schema varies. And an answer that reaches its
token cap is cut off, which leaves broken JSON for the caller to notice.

With this package, a call site states the prompts, a JSON Schema and a
capability tier. Configuration picks the provider and maps the tiers to
models. Every completion is validated against the schema, so `Complete`
returns JSON that satisfies it, or an error.

## Trade-offs

- **One shape of call.** A system prompt and a user prompt go in, and one JSON
  value comes out. There is no conversation, no tool use, no image input, no
  streaming to the caller and no free-text answer. For those, use the
  provider's SDK.
- **Two tiers, and no model names.** A call site asks for `TierFast` or
  `TierStrong`, and cannot name a model. A program that chooses among many
  models call by call does not fit.
- **Two kinds of backend.** Anthropic's API, and OpenAI-compatible chat
  completions. A provider with an API of another shape is reached only through
  an OpenAI-compatible endpoint in front of it.

## Overview

A call site states a `Request`: the prompts, the JSON Schema, and a capability
tier, `TierFast` or `TierStrong`. Configuration picks the provider and maps
the tiers to models. Every completion is validated against the schema,
whatever the backend did: a server that does not implement the constraint may
ignore it. A completion that fails the validation is asked for once more, with
the model told what was wrong. So `Complete` returns JSON that satisfies the
schema, or an error. In tests, `Fake` stands in for the model, so there is no
provider's SDK to fake.

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

- `Request` — the `System` and `User` prompts; the `Schema` the completion
  must satisfy; the `Tier`, `TierFast` for the bulk of calls and `TierStrong`
  for the calls that need a stronger model; a `Label` that names the call in
  the log; `MaxTokens`, the token cap on the answer, where zero means a
  default sized for a small answer such as a classification; and
  `KeepTruncated`. Set `KeepTruncated` when the caller can use the part of an
  answer that was finished before the cap cut it off.
- `Completion` — the `JSON`, the `Model` that produced it, the `Usage` the
  backend reported, and whether it was `Truncated`.
- `*TruncatedError` — the error a request with `KeepTruncated` gets when its
  answer is cut off at the cap. It takes the place of a retry, and holds the
  completion as far as it got.

## Clients

`New(cfg, debug)` builds the client for `cfg.Provider`, wrapped in
`WithValidation`. The provider is one of the `Provider*` constants below. When
the configuration is read from the environment, it is `<prefix>PROVIDER`.
`ProviderOff` is `off`, which an unset `<prefix>PROVIDER` also means. It
yields a nil client, which callers treat as the feature not being configured.
`debug` is a function that says, call by call, whether the whole exchange is
logged. It may be nil. The providers:

- `anthropic` (`ProviderAnthropic`) — Anthropic's API, through its structured
  outputs. When the configuration is read from the environment, the models
  default to a current fast model and a current strong one.
- `openai_compat` (`ProviderOpenAICompat`) — any OpenAI-compatible
  chat-completions endpoint, selected by its base URL: the OpenAI API, or a
  local runtime (llama.cpp, Ollama, vLLM, LM Studio). The schema is sent as
  `response_format`. With `Grammar`, a vLLM endpoint is sent a grammar in
  place of the schema. The grammar is the one the schema compiles to here,
  with no `\u` escape in a string and no free whitespace. Given the schema,
  the backend would compile it into a grammar that allows both. The grammar
  covers a narrow schema: objects whose every property is required and that
  allow no others, arrays, strings with or without an enum, and booleans. Any
  other type or keyword, numbers included, fails the request.
- `fake` (`ProviderFake`) — `Fake`, the deterministic in-process client. Its
  `Handler` produces its answers, and `Calls` returns the requests it
  received. Without a `Handler`, every call returns an error.

`WithValidation(client, debug)` validates every completion against its
request's schema, whatever the backend's constrained decoding did. A
completion that fails is asked for once more, with the model told what was
wrong. A second failure is an error. That is the only retry this package
makes: when a request itself fails, the caller gets the error.

`WithValidation` is also where the log is written. It goes through Go's
standard `log` package: there is no logger to inject. A completion that fails
the validation is always logged. The prompt, the completion and the token
usage are logged when `debug` says so.

## Configuration

`Config` is the configuration of one provider:

- `Provider` and `BaseURL`.
- The credential: `APIKey`, or `AuthToken`, which wins.
- `ModelFast` and `ModelStrong`. Read from the environment, the strong tier
  defaults to the fast one.
- `Timeout`, which bounds one request to an `openai_compat` backend.
- `Reasoning`: for a model that reasons before it answers, the number of
  tokens its reasoning may take. A backend counts those tokens against the
  same cap as the answer, so they are added to every request's cap.
- `Extra`: fields merged into every OpenAI-compatible request, such as
  sampling settings or a runtime's switch for thinking.
- `Grammar`.
- `Transport`: the HTTP transport every request goes through.

`ConfigFromEnv(prefix)` reads a `Config` from the environment variables under
a prefix, and validates it:

| Variable | |
|---|---|
| `<prefix>PROVIDER` | `off` (unset), `fake`, `anthropic` or `openai_compat` |
| `<prefix>BASE_URL` | required by `openai_compat`, version prefix included |
| `<prefix>API_KEY`, `<prefix>AUTH_TOKEN` | the credential; `anthropic` requires one, or `CLAUDE_CODE_OAUTH_TOKEN` |
| `<prefix>MODEL_FAST`, `<prefix>MODEL_STRONG` | the tiers' models; `openai_compat` requires the fast one |
| `<prefix>TIMEOUT` | a Go duration bounding one request to an `openai_compat` backend |
| `<prefix>REASONING_TOKENS` | the tokens a reasoning model may spend before it answers |
| `<prefix>REQUEST_EXTRA` | a JSON object merged into every OpenAI-compatible request |
| `<prefix>GRAMMAR` | `true` for a vLLM endpoint taking the grammar |

One prefix is one configuration, so several can coexist: a running service
classifies through one while a build tool translates through another.
