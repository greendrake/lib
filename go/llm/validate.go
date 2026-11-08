package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// logPayloadMax bounds a completion quoted into a schema-miss log line — enough
// to see what the model actually emitted, not enough to bury the log.
const logPayloadMax = 500

// validatingClient enforces the package contract — a completion either
// satisfies Request.Schema or Complete errors — uniformly across adapters,
// independent of how strictly a given backend constrains its output. A
// completion that fails validation is retried once (models occasionally emit
// near-miss JSON), told what was wrong with it — asked again unchanged, a model
// writes the same answer again; a second miss is an error the caller treats as
// abstention. One cut off at the cap, for a request that keeps it, is that
// request's *TruncatedError at once.
//
// It is also the single place every backend call passes through, so it owns the
// exchange log: prompt, raw completion and token usage under the debug gate,
// schema misses unconditionally (they are rare by design and always worth
// knowing about).
type validatingClient struct {
	inner Client
	debug DebugGate
}

// WithValidation wraps a client with the schema-validation contract. debug may
// be nil, in which case only schema misses are logged.
func WithValidation(inner Client, debug DebugGate) Client {
	return &validatingClient{inner: inner, debug: debug}
}

func (c *validatingClient) Complete(ctx context.Context, req Request) (Completion, error) {
	schema, err := jsonschema.CompileString("request.schema.json", string(req.Schema))
	if err != nil {
		return Completion{}, fmt.Errorf("compile completion schema: %w", err)
	}

	verbose := c.debug != nil && c.debug(ctx)
	if verbose {
		log.Printf("llm[%s] %s request:\n--- system ---\n%s\n--- user ---\n%s", req.Label, req.Tier, req.System, req.User)
	}

	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		out, err := c.inner.Complete(ctx, req)
		if err != nil {
			return Completion{}, err
		}
		if verbose {
			log.Printf("llm[%s] %s/%s completion (in=%d out=%d tokens):\n%s",
				req.Label, req.Tier, out.Model, out.Usage.InputTokens, out.Usage.OutputTokens, out.JSON)
		}
		lastErr = validateAgainst(schema, out.JSON)
		if lastErr == nil {
			return out, nil
		}
		if out.Truncated {
			lastErr = fmt.Errorf("cut off at the length limit before it was complete: %w", lastErr)
		}
		log.Printf("llm[%s] %s/%s schema miss on attempt %d: %v | %s",
			req.Label, req.Tier, out.Model, attempt, lastErr, truncate(string(out.JSON), logPayloadMax))
		if out.Truncated && req.KeepTruncated {
			return Completion{}, &TruncatedError{Completion: out}
		}
		req.System += fmt.Sprintf("\n\nYour previous answer was rejected: %v. Answer again, in full.", lastErr)
	}
	return Completion{}, fmt.Errorf("completion does not satisfy schema after retry: %w", lastErr)
}

func validateAgainst(schema *jsonschema.Schema, data json.RawMessage) error {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return fmt.Errorf("completion is not valid JSON: %w", err)
	}
	return schema.Validate(v)
}
