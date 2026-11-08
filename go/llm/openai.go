package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strings"
)

// openAICompatClient renders completions through any OpenAI-compatible
// chat-completions endpoint — the OpenAI API itself, or a local llama.cpp
// `llama-server`, Ollama, vLLM or LM Studio runtime — selected purely by base
// URL (which includes the version prefix, e.g. https://api.openai.com/v1).
// Structured output is requested via response_format json_schema, or on a vLLM
// endpoint (Config.Grammar) as the grammar the schema compiles to; servers
// that ignore it are caught by the WithValidation wrapper.
type openAICompatClient struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	models     map[Tier]string
	extra      map[string]any
	reasoning  int
	grammar    bool
}

func newOpenAICompat(cfg Config) Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &openAICompatClient{
		httpClient: &http.Client{Timeout: timeout, Transport: cfg.transport()},
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		// One bearer slot, whichever credential filled it — this wire format
		// draws no distinction between the two.
		apiKey:    firstNonEmpty(cfg.AuthToken, cfg.APIKey),
		models:    map[Tier]string{TierFast: cfg.ModelFast, TierStrong: cfg.ModelStrong},
		extra:     cfg.Extra,
		reasoning: cfg.Reasoning,
		grammar:   cfg.Grammar,
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type       string             `json:"type"`
	JSONSchema responseJSONSchema `json:"json_schema"`
}

type responseJSONSchema struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
	Strict bool            `json:"strict"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (c *openAICompatClient) Complete(ctx context.Context, req Request) (Completion, error) {
	model, err := modelFor(c.models, req.Tier)
	if err != nil {
		return Completion{}, err
	}

	messages := make([]chatMessage, 0, 2)
	if req.System != "" {
		messages = append(messages, chatMessage{Role: "system", Content: req.System})
	}
	messages = append(messages, chatMessage{Role: "user", Content: req.User})

	// The deployment's fields go in first and the adapter's own over them, so
	// nothing in REQUEST_EXTRA can point a request at another model or strip
	// the constraint. The cap goes under the name the OpenAI API takes today,
	// which the local runtimes honour as well.
	request := make(map[string]any, len(c.extra)+4)
	maps.Copy(request, c.extra)
	request["model"] = model
	request["messages"] = messages
	request["max_completion_tokens"] = capOf(req, c.reasoning)
	if c.grammar {
		grammar, err := grammarOf(req.Schema)
		if err != nil {
			return Completion{}, fmt.Errorf("openai_compat: %w", err)
		}
		request["structured_outputs"] = map[string]string{"grammar": grammar}
	} else {
		request["response_format"] = responseFormat{
			Type:       "json_schema",
			JSONSchema: responseJSONSchema{Name: "result", Schema: req.Schema, Strict: true},
		}
	}
	body, err := json.Marshal(request)
	if err != nil {
		return Completion{}, fmt.Errorf("openai_compat: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Completion{}, fmt.Errorf("openai_compat: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return Completion{}, fmt.Errorf("openai_compat: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Completion{}, fmt.Errorf("openai_compat: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Completion{}, fmt.Errorf("openai_compat: status %d: %s", resp.StatusCode, truncate(string(respBody), logPayloadMax))
	}

	var parsed chatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Completion{}, fmt.Errorf("openai_compat: decode response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return Completion{}, fmt.Errorf("openai_compat: response carries no choices")
	}
	return Completion{
		JSON:      json.RawMessage(parsed.Choices[0].Message.Content),
		Model:     model,
		Usage:     Usage{InputTokens: parsed.Usage.PromptTokens, OutputTokens: parsed.Usage.CompletionTokens},
		Truncated: parsed.Choices[0].FinishReason == "length",
	}, nil
}
