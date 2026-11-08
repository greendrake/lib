package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// anthropicClient renders completions through the Claude API with structured
// outputs (output_config.format json_schema), so the completion is
// schema-constrained server-side.
type anthropicClient struct {
	api       anthropic.Client
	models    map[Tier]string
	reasoning int
}

// oauthBeta is what the API wants alongside a bearer token: an OAuth
// credential reaches it through a different door than an API key, and the door
// is opted into by header.
const oauthBeta = "oauth-2025-04-20"

func newAnthropic(cfg Config) Client {
	// A token and a key are not the same credential in a different wrapper: the
	// key rides in X-Api-Key, the token as a bearer. Sending a token as a key
	// authenticates as nobody.
	opts := []option.RequestOption{option.WithHTTPClient(&http.Client{Transport: cfg.transport()})}
	if cfg.AuthToken != "" {
		opts = append(opts, option.WithAuthToken(cfg.AuthToken), option.WithHeader("anthropic-beta", oauthBeta))
	} else {
		opts = append(opts, option.WithAPIKey(cfg.APIKey))
	}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	return &anthropicClient{
		api:       anthropic.NewClient(opts...),
		models:    map[Tier]string{TierFast: cfg.ModelFast, TierStrong: cfg.ModelStrong},
		reasoning: cfg.Reasoning,
	}
}

func (c *anthropicClient) Complete(ctx context.Context, req Request) (Completion, error) {
	model, err := modelFor(c.models, req.Tier)
	if err != nil {
		return Completion{}, err
	}
	var schema map[string]any
	if err := json.Unmarshal(req.Schema, &schema); err != nil {
		return Completion{}, fmt.Errorf("anthropic: request schema is not a JSON object: %w", err)
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: int64(capOf(req, c.reasoning)),
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(req.User))},
		OutputConfig: anthropic.OutputConfigParam{
			Format: anthropic.JSONOutputFormatParam{Schema: schema},
		},
	}
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}

	msg, err := c.message(ctx, params)
	if err != nil {
		return Completion{}, fmt.Errorf("anthropic: %w", explainRefusal(err))
	}
	for _, block := range msg.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			return Completion{
				JSON:      json.RawMessage(tb.Text),
				Model:     model,
				Usage:     Usage{InputTokens: int(msg.Usage.InputTokens), OutputTokens: int(msg.Usage.OutputTokens)},
				Truncated: msg.StopReason == anthropic.StopReasonMaxTokens,
			}, nil
		}
	}
	return Completion{}, errors.New("anthropic: response carries no text block")
}

// message renders one completion, streaming it when a single response could
// not carry it. A large MaxTokens is a legitimate request — a content page
// translated into a script that tokenises poorly is simply big — but the API
// declines to serve one non-streamed, so the transport follows the cap rather
// than the cap following the transport. The SDK's own estimate decides, so no
// threshold is restated here; the accumulated message is the same shape the
// non-streaming call returns, which is why the caller reads one type.
func (c *anthropicClient) message(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error) {
	if _, err := anthropic.CalculateNonStreamingTimeout(int(params.MaxTokens), params.Model, nil); err == nil {
		return c.api.Messages.New(ctx, params)
	}
	stream := c.api.Messages.NewStreaming(ctx, params)
	defer stream.Close()
	msg := &anthropic.Message{}
	for stream.Next() {
		if err := msg.Accumulate(stream.Current()); err != nil {
			return nil, fmt.Errorf("accumulate streamed message: %w", err)
		}
	}
	if err := stream.Err(); err != nil {
		return nil, err
	}
	return msg, nil
}

// diagnosticHeaders are what the API puts in headers and not in the body: which
// limit a 429 hit, how much of it is left, when it comes back, and — the one
// that answers "whose budget was that?" — the workspace the credential resolved
// to. An API key and an OAuth token can resolve to different workspaces with
// different limits, so the same code can be under its limit on one credential
// and over it on the other.
var diagnosticHeaders = []string{
	"retry-after",
	"anthropic-workspace-id",
	"anthropic-ratelimit-requests-remaining",
	"anthropic-ratelimit-requests-limit",
	"anthropic-ratelimit-input-tokens-remaining",
	"anthropic-ratelimit-input-tokens-limit",
	"anthropic-ratelimit-output-tokens-remaining",
	"anthropic-ratelimit-output-tokens-limit",
	"anthropic-ratelimit-output-tokens-reset",
}

// explainRefusal appends those headers to an API error.
//
// A rate-limited response often carries the body {"type":"rate_limit_error",
// "message":"Error"}, which says neither which limit nor for how long. Everything
// needed to answer both rides in the headers, and an error that drops them
// leaves a person guessing at their own concurrency.
func explainRefusal(err error) error {
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) || apiErr.Response == nil {
		return err
	}
	var facts []string
	for _, name := range diagnosticHeaders {
		if value := apiErr.Response.Header.Get(name); value != "" {
			facts = append(facts, name+"="+value)
		}
	}
	if len(facts) == 0 {
		return err
	}
	return fmt.Errorf("%w [%s]", err, strings.Join(facts, " "))
}
