package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureLog redirects the stdlib logger for the duration of fn and returns
// what was written to it, without flags so assertions match message text.
func captureLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	}()
	fn()
	return buf.String()
}

var boolSchema = json.RawMessage(`{
	"type": "object",
	"properties": {"ok": {"type": "boolean"}},
	"required": ["ok"],
	"additionalProperties": false
}`)

func boolRequest(tier Tier) Request {
	return Request{System: "sys", User: "usr", Schema: boolSchema, Tier: tier}
}

func TestWithValidation_PassesValidCompletion(t *testing.T) {
	fake := &Fake{Handler: func(Request) (json.RawMessage, error) {
		return json.RawMessage(`{"ok": true}`), nil
	}}
	out, err := WithValidation(fake, nil).Complete(context.Background(), boolRequest(TierFast))
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok": true}`, string(out.JSON))
	assert.Len(t, fake.Calls(), 1)
}

func TestWithValidation_RetriesOnceOnSchemaMiss(t *testing.T) {
	responses := []json.RawMessage{
		json.RawMessage(`{"ok": "not-a-bool"}`),
		json.RawMessage(`{"ok": false}`),
	}
	fake := &Fake{Handler: func(Request) (json.RawMessage, error) {
		out := responses[0]
		responses = responses[1:]
		return out, nil
	}}
	out, err := WithValidation(fake, nil).Complete(context.Background(), boolRequest(TierFast))
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok": false}`, string(out.JSON))
	calls := fake.Calls()
	require.Len(t, calls, 2)
	// Asked again unchanged, a model writes the same answer again: the retry
	// is told what was wrong.
	assert.Equal(t, "sys", calls[0].System)
	assert.True(t, strings.HasPrefix(calls[1].System, "sys\n\nYour previous answer was rejected: "), calls[1].System)
	assert.Contains(t, calls[1].System, "expected boolean")
}

// truncating answers as a backend does whose completion ran into its cap.
type truncating struct{ calls []Request }

func (c *truncating) Complete(_ context.Context, req Request) (Completion, error) {
	c.calls = append(c.calls, req)
	return Completion{JSON: json.RawMessage(`{"ok": tr`), Truncated: true}, nil
}

// A completion cut off at the cap is said to be, which is what the model can
// act on — not the parse error its missing tail produces.
func TestWithValidation_SaysACompletionWasCutOff(t *testing.T) {
	inner := &truncating{}
	_, err := WithValidation(inner, nil).Complete(context.Background(), boolRequest(TierFast))
	assert.ErrorContains(t, err, "cut off at the length limit before it was complete")
	require.Len(t, inner.calls, 2)
	assert.Contains(t, inner.calls[1].System, "Your previous answer was rejected: cut off at the length limit before it was complete")
}

// A request that keeps a cut-off completion gets it back at once, as far as it
// got, rather than paying for the same answer again whole.
func TestWithValidation_ReturnsACutOffCompletionKept(t *testing.T) {
	inner := &truncating{}
	req := boolRequest(TierFast)
	req.KeepTruncated = true
	_, err := WithValidation(inner, nil).Complete(context.Background(), req)
	var cut *TruncatedError
	require.ErrorAs(t, err, &cut)
	assert.Equal(t, `{"ok": tr`, string(cut.Completion.JSON))
	assert.True(t, cut.Completion.Truncated)
	assert.Len(t, inner.calls, 1)
}

func TestWithValidation_ErrsAfterSecondSchemaMiss(t *testing.T) {
	fake := &Fake{Handler: func(Request) (json.RawMessage, error) {
		return json.RawMessage(`{"unexpected": 1}`), nil
	}}
	_, err := WithValidation(fake, nil).Complete(context.Background(), boolRequest(TierFast))
	assert.ErrorContains(t, err, "does not satisfy schema")
	assert.Len(t, fake.Calls(), 2)
}

// A transport-level error propagates as-is — the schema retry is for near-miss
// completions, not for transport failures (adapters own those).
func TestWithValidation_PropagatesInnerError(t *testing.T) {
	fake := &Fake{}
	_, err := WithValidation(fake, nil).Complete(context.Background(), boolRequest(TierFast))
	assert.ErrorIs(t, err, ErrFakeUnscripted)
	assert.Len(t, fake.Calls(), 1)
}

func TestWithValidation_DebugGateLogsTheExchange(t *testing.T) {
	fake := &Fake{Handler: func(Request) (json.RawMessage, error) {
		return json.RawMessage(`{"ok": true}`), nil
	}}
	req := boolRequest(TierFast)
	req.Label = "auto-tag ABC123 category"
	// Distinctive values: the line's own "--- system ---" header contains
	// "sys", so the default fixture would pass this test with the prompt
	// dropped from the log entirely.
	req.System = "SYSTEM-PROMPT-MARKER"
	req.User = "USER-PROMPT-MARKER"

	on := captureLog(t, func() {
		_, err := WithValidation(fake, func(context.Context) bool { return true }).Complete(context.Background(), req)
		require.NoError(t, err)
	})
	assert.Contains(t, on, "auto-tag ABC123 category")
	assert.Contains(t, on, "SYSTEM-PROMPT-MARKER")
	assert.Contains(t, on, "USER-PROMPT-MARKER")
	assert.Contains(t, on, `{"ok": true}`)
	assert.Contains(t, on, "fast-fake")

	off := captureLog(t, func() {
		_, err := WithValidation(fake, func(context.Context) bool { return false }).Complete(context.Background(), req)
		require.NoError(t, err)
	})
	assert.Empty(t, off)
}

// A schema miss is rare by design, so it is logged with the offending payload
// whether or not the debug gate is on — otherwise the silent retry hides the
// only evidence that the model is drifting.
func TestWithValidation_SchemaMissLogsWithoutDebugGate(t *testing.T) {
	fake := &Fake{Handler: func(Request) (json.RawMessage, error) {
		return json.RawMessage(`{"ok": "not-a-bool"}`), nil
	}}
	out := captureLog(t, func() {
		_, err := WithValidation(fake, nil).Complete(context.Background(), boolRequest(TierFast))
		assert.ErrorContains(t, err, "does not satisfy schema")
	})
	assert.Contains(t, out, "schema miss on attempt 1")
	assert.Contains(t, out, "schema miss on attempt 2")
	assert.Contains(t, out, `{"ok": "not-a-bool"}`)
}

func TestNew_OffYieldsNilClient(t *testing.T) {
	client, err := New(Config{Provider: ProviderOff}, nil)
	require.NoError(t, err)
	assert.Nil(t, client)
}

func TestNew_UnknownProviderErrs(t *testing.T) {
	_, err := New(Config{Provider: "hallucinated"}, nil)
	assert.ErrorContains(t, err, "unknown llm provider")
}

func TestModelFor_UnknownTierErrs(t *testing.T) {
	_, err := modelFor(map[Tier]string{TierFast: "m"}, TierStrong)
	assert.ErrorContains(t, err, "no model configured")
}

// The Anthropic adapter must carry the prompt, the tier's model, and the
// schema as structured outputs (output_config.format), and return the text
// block verbatim.
func TestAnthropicAdapter_RequestShapeAndResponse(t *testing.T) {
	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/messages", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_1", "type": "message", "role": "assistant",
			"model": "fast-model",
			"content": [{"type": "text", "text": "{\"ok\": true}"}],
			"stop_reason": "end_turn", "stop_sequence": null,
			"usage": {"input_tokens": 1, "output_tokens": 1}
		}`))
	}))
	defer server.Close()

	client := newAnthropic(Config{APIKey: "test-key", BaseURL: server.URL, ModelFast: "fast-model", ModelStrong: "strong-model"})
	out, err := client.Complete(context.Background(), boolRequest(TierFast))
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok": true}`, string(out.JSON))
	assert.Equal(t, "fast-model", out.Model)
	assert.Equal(t, Usage{InputTokens: 1, OutputTokens: 1}, out.Usage)

	assert.Equal(t, "fast-model", captured["model"])
	system := captured["system"].([]interface{})[0].(map[string]interface{})
	assert.Equal(t, "sys", system["text"])
	message := captured["messages"].([]interface{})[0].(map[string]interface{})
	assert.Equal(t, "user", message["role"])
	format := captured["output_config"].(map[string]interface{})["format"].(map[string]interface{})
	assert.Equal(t, "json_schema", format["type"])
	assert.Equal(t, "object", format["schema"].(map[string]interface{})["type"])
}

// The Anthropic API requires a cap, so the adapter always sends one: the
// caller's where it states one, and the classification-sized default
// otherwise. A translated content page needs the former; sending the latter
// would truncate it into invalid JSON.
//
// A cap large enough that one response could not carry it is served over a
// stream instead — the API declines it otherwise — and the caller sees no
// difference.
func TestAnthropicAdapter_MaxTokensFromRequest(t *testing.T) {
	var sent []int64
	var streamed []bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			MaxTokens int64 `json:"max_tokens"`
			Stream    bool  `json:"stream"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		sent = append(sent, body.MaxTokens)
		streamed = append(streamed, body.Stream)
		if !body.Stream {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"id": "msg_1", "type": "message", "role": "assistant", "model": "fast-model",
				"content": [{"type": "text", "text": "{\"ok\": true}"}],
				"stop_reason": "end_turn", "stop_sequence": null,
				"usage": {"input_tokens": 1, "output_tokens": 1}
			}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []string{
			`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"fast-model","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":2,"output_tokens":0}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"{\"ok\": "}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"true}"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":7}}`,
			`{"type":"message_stop"}`,
		} {
			_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", event)
		}
	}))
	defer server.Close()

	client := newAnthropic(Config{APIKey: "test-key", BaseURL: server.URL, ModelFast: "fast-model", ModelStrong: "strong-model"})
	out, err := client.Complete(context.Background(), boolRequest(TierFast))
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok": true}`, string(out.JSON))

	// Past the size one response may carry, so the adapter streams and
	// reassembles — same JSON, same reported usage.
	large := boolRequest(TierFast)
	large.MaxTokens = 64000
	out, err = client.Complete(context.Background(), large)
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok": true}`, string(out.JSON))
	assert.Equal(t, Usage{InputTokens: 2, OutputTokens: 7}, out.Usage)

	// A deployment whose model reasons first has that room added to the
	// answer's cap.
	reasoning := newAnthropic(Config{APIKey: "test-key", BaseURL: server.URL, ModelFast: "fast-model", ModelStrong: "strong-model", Reasoning: 1000})
	_, err = reasoning.Complete(context.Background(), boolRequest(TierFast))
	require.NoError(t, err)

	assert.Equal(t, []int64{defaultMaxTokens, 64000, defaultMaxTokens + 1000}, sent)
	assert.Equal(t, []bool{false, true, false}, streamed)
}

// Each adapter reports a completion its backend ended at the cap: the stop
// reason that says so, in each API's own words.
func TestAdapters_ReportACompletionCutOffAtTheCap(t *testing.T) {
	for _, stop := range []struct {
		reason    string
		truncated bool
	}{{"max_tokens", true}, {"end_turn", false}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{
				"id": "msg_1", "type": "message", "role": "assistant", "model": "m",
				"content": [{"type": "text", "text": "{\"ok\": tr"}],
				"stop_reason": %q, "stop_sequence": null,
				"usage": {"input_tokens": 1, "output_tokens": 1}
			}`, stop.reason)
		}))
		out, err := newAnthropic(Config{APIKey: "k", BaseURL: server.URL, ModelFast: "m", ModelStrong: "m"}).Complete(context.Background(), boolRequest(TierFast))
		server.Close()
		require.NoError(t, err)
		assert.Equal(t, stop.truncated, out.Truncated, "anthropic %s", stop.reason)
	}
	for _, finish := range []struct {
		reason    string
		truncated bool
	}{{"length", true}, {"stop", false}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"choices": [{"message": {"content": "{\"ok\": tr"}, "finish_reason": %q}]}`, finish.reason)
		}))
		out, err := newOpenAICompat(Config{BaseURL: server.URL, ModelFast: "m", ModelStrong: "m"}).Complete(context.Background(), boolRequest(TierFast))
		server.Close()
		require.NoError(t, err)
		assert.Equal(t, finish.truncated, out.Truncated, "openai_compat %s", finish.reason)
	}
}

func TestAnthropicAdapter_StrongTierUsesStrongModel(t *testing.T) {
	var model string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		model = body["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "strong-model",
			"content": [{"type": "text", "text": "{}"}],
			"stop_reason": "end_turn", "stop_sequence": null,
			"usage": {"input_tokens": 1, "output_tokens": 1}
		}`))
	}))
	defer server.Close()

	client := newAnthropic(Config{APIKey: "test-key", BaseURL: server.URL, ModelFast: "fast-model", ModelStrong: "strong-model"})
	_, err := client.Complete(context.Background(), boolRequest(TierStrong))
	require.NoError(t, err)
	assert.Equal(t, "strong-model", model)
}

// The openai_compat adapter must POST the chat-completions shape with
// response_format json_schema and return the choice content verbatim.
func TestOpenAICompatAdapter_RequestShapeAndResponse(t *testing.T) {
	var captured map[string]interface{}
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/chat/completions", r.URL.Path)
		authHeader = r.Header.Get("Authorization")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices": [{"message": {"role": "assistant", "content": "{\"ok\": false}"}}], "usage": {"prompt_tokens": 11, "completion_tokens": 3}}`))
	}))
	defer server.Close()

	client := newOpenAICompat(Config{BaseURL: server.URL + "/v1", APIKey: "local-key", ModelFast: "local-model", ModelStrong: "local-model"})
	out, err := client.Complete(context.Background(), boolRequest(TierFast))
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok": false}`, string(out.JSON))
	assert.Equal(t, "local-model", out.Model)
	assert.Equal(t, Usage{InputTokens: 11, OutputTokens: 3}, out.Usage)

	assert.Equal(t, "Bearer local-key", authHeader)
	assert.Equal(t, "local-model", captured["model"])
	messages := captured["messages"].([]interface{})
	require.Len(t, messages, 2)
	assert.Equal(t, "system", messages[0].(map[string]interface{})["role"])
	assert.Equal(t, "user", messages[1].(map[string]interface{})["role"])
	rf := captured["response_format"].(map[string]interface{})
	assert.Equal(t, "json_schema", rf["type"])
	js := rf["json_schema"].(map[string]interface{})
	assert.Equal(t, true, js["strict"])
	assert.Equal(t, "object", js["schema"].(map[string]interface{})["type"])
}

// On a vLLM endpoint the schema goes as the grammar it compiles to, in place
// of response_format; a schema the grammar cannot express fails the request
// before anything is sent.
func TestOpenAICompatAdapter_GrammarInPlaceOfTheSchema(t *testing.T) {
	var captured map[string]interface{}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices": [{"message": {"content": "{\"ok\": true}"}}]}`))
	}))
	defer server.Close()

	client := newOpenAICompat(Config{BaseURL: server.URL, ModelFast: "m", ModelStrong: "m", Grammar: true,
		Extra: map[string]any{"structured_outputs": map[string]any{"json": "{}"}}})
	_, err := client.Complete(context.Background(), boolRequest(TierFast))
	require.NoError(t, err)
	grammar, err := grammarOf(boolSchema)
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"grammar": grammar}, captured["structured_outputs"], "the adapter's own constraint wins")
	assert.NotContains(t, captured, "response_format")

	open := boolRequest(TierFast)
	open.Schema = json.RawMessage(`{"type": "object", "properties": {"ok": {"type": "boolean"}}}`)
	_, err = client.Complete(context.Background(), open)
	assert.ErrorContains(t, err, "openai_compat: grammar: root: an object must set additionalProperties to false")
	assert.Equal(t, 1, requests, "a schema without a grammar never reaches the endpoint")
}

// What REQUEST_EXTRA carries reaches the wire beside the adapter's own fields,
// never in place of them.
func TestOpenAICompatAdapter_ExtraFieldsReachTheWire(t *testing.T) {
	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices": [{"message": {"content": "{\"ok\": true}"}}]}`))
	}))
	defer server.Close()

	client := newOpenAICompat(Config{BaseURL: server.URL, ModelFast: "local-model", ModelStrong: "local-model", Extra: map[string]any{
		"temperature":          0.2,
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
		"model":                "hijacked",
	}})
	_, err := client.Complete(context.Background(), boolRequest(TierFast))
	require.NoError(t, err)
	assert.Equal(t, 0.2, captured["temperature"])
	assert.Equal(t, map[string]interface{}{"enable_thinking": false}, captured["chat_template_kwargs"])
	assert.Equal(t, "local-model", captured["model"], "the adapter's own fields win")
	assert.NotNil(t, captured["response_format"])
	assert.NotContains(t, captured, "structured_outputs", "no grammar where the deployment does not take one")
}

// The cap rides on this path too, as a ceiling against a completion that
// loops: the caller's where it states one, the classification-sized default
// otherwise, under the field name OpenAI takes today.
func TestOpenAICompatAdapter_SendsTheCap(t *testing.T) {
	var caps []float64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.NotContains(t, body, "max_tokens")
		caps = append(caps, body["max_completion_tokens"].(float64))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices": [{"message": {"content": "{\"ok\": true}"}}]}`))
	}))
	defer server.Close()

	client := newOpenAICompat(Config{BaseURL: server.URL, ModelFast: "m", ModelStrong: "m"})
	_, err := client.Complete(context.Background(), boolRequest(TierFast))
	require.NoError(t, err)
	stated := boolRequest(TierFast)
	stated.MaxTokens = 64000
	_, err = client.Complete(context.Background(), stated)
	require.NoError(t, err)
	// The room a reasoning model needs goes on top of the answer's.
	_, err = newOpenAICompat(Config{BaseURL: server.URL, ModelFast: "m", ModelStrong: "m", Reasoning: 1000}).Complete(context.Background(), stated)
	require.NoError(t, err)
	assert.Equal(t, []float64{defaultMaxTokens, 64000, 65000}, caps)
}

// A host's transport carries every request either adapter makes — how it puts
// its own instrumentation around calls this package knows nothing about.
func TestAdapters_RequestsGoThroughTheConfiguredTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/messages" {
			_, _ = w.Write([]byte(`{
				"id": "msg_1", "type": "message", "role": "assistant", "model": "m",
				"content": [{"type": "text", "text": "{\"ok\": true}"}],
				"stop_reason": "end_turn", "stop_sequence": null,
				"usage": {"input_tokens": 1, "output_tokens": 1}
			}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices": [{"message": {"content": "{\"ok\": true}"}}]}`))
	}))
	defer server.Close()

	var seen []string
	transport := roundTripper(func(r *http.Request) (*http.Response, error) {
		seen = append(seen, r.URL.Path)
		return http.DefaultTransport.RoundTrip(r)
	})
	cfg := Config{APIKey: "k", ModelFast: "m", ModelStrong: "m", Transport: transport}

	anthropicCfg := cfg
	anthropicCfg.BaseURL = server.URL
	_, err := newAnthropic(anthropicCfg).Complete(context.Background(), boolRequest(TierFast))
	require.NoError(t, err)

	openAICfg := cfg
	openAICfg.BaseURL = server.URL + "/v1"
	_, err = newOpenAICompat(openAICfg).Complete(context.Background(), boolRequest(TierFast))
	require.NoError(t, err)

	assert.Equal(t, []string{"/v1/messages", "/v1/chat/completions"}, seen)
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOpenAICompatAdapter_NonOKStatusErrs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error": "model not loaded"}`, http.StatusBadRequest)
	}))
	defer server.Close()

	client := newOpenAICompat(Config{BaseURL: server.URL, ModelFast: "m", ModelStrong: "m"})
	_, err := client.Complete(context.Background(), boolRequest(TierFast))
	assert.ErrorContains(t, err, "status 400")
}

// The two credentials reach the API through different doors, and sending one
// through the other's authenticates as nobody: a key rides in X-Api-Key, a
// token as a bearer with the beta header that opts into OAuth.
func TestAnthropicAdapter_CredentialShape(t *testing.T) {
	var headers http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "m",
			"content": [{"type": "text", "text": "{\"ok\": true}"}],
			"stop_reason": "end_turn", "stop_sequence": null,
			"usage": {"input_tokens": 1, "output_tokens": 1}
		}`))
	}))
	defer server.Close()

	base := Config{BaseURL: server.URL, ModelFast: "m", ModelStrong: "m"}

	withKey := base
	withKey.APIKey = "sk-key"
	_, err := newAnthropic(withKey).Complete(context.Background(), boolRequest(TierFast))
	require.NoError(t, err)
	assert.Equal(t, "sk-key", headers.Get("X-Api-Key"))
	assert.Empty(t, headers.Get("Authorization"))
	assert.NotContains(t, headers.Get("Anthropic-Beta"), oauthBeta)

	// A token set alongside a key wins, and is never sent as a key.
	withToken := base
	withToken.APIKey = "sk-key"
	withToken.AuthToken = "oat-token"
	_, err = newAnthropic(withToken).Complete(context.Background(), boolRequest(TierFast))
	require.NoError(t, err)
	assert.Equal(t, "Bearer oat-token", headers.Get("Authorization"))
	assert.Empty(t, headers.Get("X-Api-Key"))
	assert.Contains(t, headers.Get("Anthropic-Beta"), oauthBeta)
}

// The OpenAI-compatible wire format has one bearer slot, so either credential
// fills it and a token set beside a key wins there too.
func TestOpenAICompatAdapter_CredentialShape(t *testing.T) {
	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices": [{"message": {"content": "{\"ok\": true}"}}]}`))
	}))
	defer server.Close()

	client := newOpenAICompat(Config{BaseURL: server.URL, APIKey: "sk-key", AuthToken: "oat-token", ModelFast: "m"})
	_, err := client.Complete(context.Background(), boolRequest(TierFast))
	require.NoError(t, err)
	assert.Equal(t, "Bearer oat-token", auth)
}

// A rate-limited response says {"type":"rate_limit_error","message":"Error"} —
// neither which limit nor for how long. Both ride in the headers, along with
// the workspace the credential resolved to, which is what distinguishes one
// budget from another.
func TestAnthropicAdapter_RefusalCarriesTheHeadersThatExplainIt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Small, because the SDK sleeps it: this handler is hit three times,
		// once per attempt the SDK makes, and a realistic value would put a
		// minute and a half into the unit suite.
		w.Header().Set("retry-after", "1")
		w.Header().Set("anthropic-workspace-id", "wrkspc_claude_code")
		w.Header().Set("anthropic-ratelimit-output-tokens-remaining", "0")
		w.Header().Set("anthropic-ratelimit-output-tokens-limit", "16000")
		w.Header().Set("anthropic-ratelimit-output-tokens-reset", "2026-09-19T10:00:00Z")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"Error"}}`))
	}))
	defer server.Close()

	client := newAnthropic(Config{APIKey: "k", BaseURL: server.URL, ModelFast: "m", ModelStrong: "m"}) // One attempt: the retries are the SDK's business, and this asserts
	// what the caller is told once they are spent.

	_, err := client.Complete(context.Background(), boolRequest(TierFast))
	require.Error(t, err)
	for _, want := range []string{
		"retry-after=1",
		"anthropic-workspace-id=wrkspc_claude_code",
		"anthropic-ratelimit-output-tokens-remaining=0",
		"anthropic-ratelimit-output-tokens-limit=16000",
	} {
		assert.Contains(t, err.Error(), want)
	}
}
