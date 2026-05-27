package freebuff

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ferdiunal/freebuff-proxy/internal/openai"
)

const verifiedRunID = "run-verified"

// Bu testler, doğrulanmış Freebuff sohbet upstream sözleşmesini istemci seviyesinde sabitler.
//
// ## Kullanım örneği
//
// ```bash
// go test ./internal/freebuff -run 'TestClientCompleteStartsAgentRunAndSendsCodebuffMetadata|TestClientStreamStartsAgentRunAndSendsCodebuffMetadata|TestClientCompleteMapsUpstreamErrorWithoutLeakingToken|TestClientCompleteTransportErrorDoesNotLeakAuthorization|TestClientStreamMalformedEventReturnsSanitizedError|TestClientStreamClosesChannelsOnDone'
// ```
func TestClientCompleteStartsAgentRunAndSendsCodebuffMetadata(t *testing.T) {
	t.Parallel()

	authToken := "verified-auth-token"
	reqBody := verifiedChatRequest()

	client, transport := newVerifiedChatClient(t, authToken, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("method = %s, beklenen %s", req.Method, http.MethodPost)
		}

		if got := req.Header.Get("Content-Type"); !strings.Contains(got, "application/json") {
			t.Fatalf("Content-Type = %q, application/json bekleniyordu", got)
		}

		assertVerifiedUpstreamChatRequest(t, req, reqBody, false)

		return jsonHTTPResponse(req, http.StatusOK, verifiedCompleteSuccessFixture()), nil
	})

	got, err := client.Complete(context.Background(), authToken, verifiedChatSession(), reqBody)
	if err != nil {
		t.Fatalf("Complete hata döndürdü: %v", err)
	}

	if got != "Doğrulanmış Freebuff yanıtı." {
		t.Fatalf("Complete = %q, beklenen doğrulanmış içerik", got)
	}
	if transport.startCount != 1 || transport.chatCount != 1 {
		t.Fatalf("startCount = %d, chatCount = %d, beklenen birer çağrı", transport.startCount, transport.chatCount)
	}
	if transport.startAgentID != "base2-free" {
		t.Fatalf("agentId = %q, beklenen base2-free", transport.startAgentID)
	}
}

func TestClientCompleteNormalizesShortDeepSeekModelForCodebuff(t *testing.T) {
	t.Parallel()

	authToken := "alias-auth-token"
	reqBody := verifiedChatRequest()
	reqBody.Model = "deepseek-v4-pro"

	client, transport := newVerifiedChatClient(t, authToken, func(req *http.Request) (*http.Response, error) {
		var gotBody upstreamChatRequest
		if err := json.NewDecoder(req.Body).Decode(&gotBody); err != nil {
			t.Fatalf("chat request JSON çözülemedi: %v", err)
		}
		if gotBody.Model != "deepseek/deepseek-v4-pro" {
			t.Fatalf("model = %q, beklenen canonical DeepSeek modeli", gotBody.Model)
		}
		if gotBody.CodebuffMetadata.RunID != verifiedRunID {
			t.Fatalf("run_id = %q, beklenen %q", gotBody.CodebuffMetadata.RunID, verifiedRunID)
		}
		if gotBody.CodebuffMetadata.ClientID == "" {
			t.Fatal("client_id boş olmamalı")
		}
		if gotBody.CodebuffMetadata.CostMode != freebuffCostMode {
			t.Fatalf("cost_mode = %q, beklenen %q", gotBody.CodebuffMetadata.CostMode, freebuffCostMode)
		}
		if gotBody.CodebuffMetadata.FreebuffInstanceID != "verified-instance" {
			t.Fatalf("freebuff_instance_id = %q, beklenen verified-instance", gotBody.CodebuffMetadata.FreebuffInstanceID)
		}

		return jsonHTTPResponse(req, http.StatusOK, verifiedCompleteSuccessFixture()), nil
	})

	_, err := client.Complete(context.Background(), authToken, verifiedChatSession(), reqBody)
	if err != nil {
		t.Fatalf("Complete hata döndürdü: %v", err)
	}

	if transport.startAgentID != "base2-free-deepseek" {
		t.Fatalf("agentId = %q, beklenen base2-free-deepseek", transport.startAgentID)
	}
}

func TestClientCompleteUsesActiveSessionModelWhenCodebuffSelectsFallback(t *testing.T) {
	t.Parallel()

	authToken := "fallback-model-token"
	reqBody := verifiedChatRequest()
	reqBody.Model = "deepseek/deepseek-v4-pro"
	activeSession := Session{
		Status:     SessionActive,
		InstanceID: "verified-instance",
		Model:      "deepseek/deepseek-v4-flash",
	}

	client, transport := newVerifiedChatClient(t, authToken, func(req *http.Request) (*http.Response, error) {
		var gotBody upstreamChatRequest
		if err := json.NewDecoder(req.Body).Decode(&gotBody); err != nil {
			t.Fatalf("chat request JSON çözülemedi: %v", err)
		}
		if gotBody.Model != "deepseek/deepseek-v4-flash" {
			t.Fatalf("model = %q, beklenen aktif oturum modeli", gotBody.Model)
		}
		if gotBody.CodebuffMetadata.FreebuffInstanceID != "verified-instance" {
			t.Fatalf("freebuff_instance_id = %q, beklenen verified-instance", gotBody.CodebuffMetadata.FreebuffInstanceID)
		}

		return jsonHTTPResponse(req, http.StatusOK, verifiedCompleteSuccessFixture()), nil
	})

	_, err := client.Complete(context.Background(), authToken, activeSession, reqBody)
	if err != nil {
		t.Fatalf("Complete hata döndürdü: %v", err)
	}

	if transport.startAgentID != "base2-free-deepseek-flash" {
		t.Fatalf("agentId = %q, beklenen base2-free-deepseek-flash", transport.startAgentID)
	}
}

func TestClientStreamStartsAgentRunAndSendsCodebuffMetadata(t *testing.T) {
	t.Parallel()

	authToken := "verified-stream-token"
	reqBody := verifiedStreamChatRequest()

	client, transport := newVerifiedChatClient(t, authToken, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("method = %s, beklenen %s", req.Method, http.MethodPost)
		}

		assertVerifiedUpstreamChatRequest(t, req, reqBody, true)

		body := strings.Join([]string{
			verifiedStreamDeltaFixture("Mer"),
			verifiedStreamDeltaFixture("haba"),
			verifiedStreamDoneFixture(),
		}, "")

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})

	deltas, errs := client.Stream(context.Background(), authToken, verifiedChatSession(), reqBody)

	gotDeltas := collectStringChannel(t, deltas)
	gotErrs := collectErrorChannel(t, errs)

	if !reflect.DeepEqual(gotDeltas, []string{"Mer", "haba"}) {
		t.Fatalf("deltas = %#v, beklenen %#v", gotDeltas, []string{"Mer", "haba"})
	}
	if len(gotErrs) != 0 {
		t.Fatalf("errs = %#v, hata beklenmiyordu", gotErrs)
	}
	if transport.startCount != 1 || transport.chatCount != 1 {
		t.Fatalf("startCount = %d, chatCount = %d, beklenen birer çağrı", transport.startCount, transport.chatCount)
	}
}

func TestClientCompleteReportsAgentRunStatusWithoutLeakingBody(t *testing.T) {
	t.Parallel()

	authToken := "secret-agent-run-token"
	client := &Client{
		baseURL: mustParseURL(t, "https://freebuff.example.test/base"),
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != agentRunsEndpointPath {
				t.Fatalf("path = %s, beklenen %s", req.URL.Path, agentRunsEndpointPath)
			}

			return jsonHTTPResponse(req, http.StatusBadRequest, `{"message":"Authorization Bearer secret-agent-run-token fingerprintHash secret-prompt"}`), nil
		})},
	}

	_, err := client.Complete(context.Background(), authToken, verifiedChatSession(), verifiedChatRequest())
	if err == nil {
		t.Fatal("Complete hata döndürmedi")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("hata tipi = %T, beklenen *APIError", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d, beklenen %d", apiErr.StatusCode, http.StatusBadRequest)
	}
	if apiErr.Code != "upstream_agent_run_error" {
		t.Fatalf("Code = %q, beklenen upstream_agent_run_error", apiErr.Code)
	}
	if apiErr.Message != "Freebuff agent run upstream hatası: status 400" {
		t.Fatalf("Message = %q, beklenen agent run status mesajı", apiErr.Message)
	}
	for _, sensitive := range []string{authToken, "Bearer", headerAuthorization, "fingerprintHash", "secret-prompt"} {
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("agent run hatası hassas değeri %q sızdırıyor: %v", sensitive, err)
		}
	}
}

func TestClientCompleteReportsFreebuffGateCodeWithoutLeakingBody(t *testing.T) {
	t.Parallel()

	authToken := "secret-gate-token"
	client, _ := newVerifiedChatClient(t, authToken, func(req *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(req, http.StatusConflict, `{"error":"session_model_mismatch","message":"Authorization Bearer secret-gate-token fingerprintHash secret-prompt"}`), nil
	})

	_, err := client.Complete(context.Background(), authToken, verifiedChatSession(), verifiedChatRequest())
	if err == nil {
		t.Fatal("Complete hata döndürmedi")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("hata tipi = %T, beklenen *APIError", err)
	}
	if apiErr.StatusCode != http.StatusConflict {
		t.Fatalf("StatusCode = %d, beklenen %d", apiErr.StatusCode, http.StatusConflict)
	}
	if apiErr.Code != "session_model_mismatch" {
		t.Fatalf("Code = %q, beklenen session_model_mismatch", apiErr.Code)
	}
	if apiErr.Message != "Freebuff oturum modeli istek modeliyle eşleşmiyor" {
		t.Fatalf("Message = %q, beklenen sanitize edilmiş session mismatch mesajı", apiErr.Message)
	}
	for _, sensitive := range []string{authToken, "Bearer", headerAuthorization, "fingerprintHash", "secret-prompt"} {
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("chat gate hatası hassas değeri %q sızdırıyor: %v", sensitive, err)
		}
	}
}

func TestClientCompleteMapsUpstreamErrorWithoutLeakingToken(t *testing.T) {
	t.Parallel()

	authToken := "secret-upstream-token"
	client, _ := newVerifiedChatClient(t, authToken, func(req *http.Request) (*http.Response, error) {
		upstreamMessage := fmt.Sprintf(
			"rate limited for Authorization Bearer %s with fingerprintHash=fp-sensitive and secret-prompt=system-secret",
			authToken,
		)

		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{
				"error": {
					"message": %q,
					"type": "rate_limit_error",
					"code": "freebuff_rate_limited"
				}
			}`, upstreamMessage))),
			Request: req,
		}, nil
	})

	_, err := client.Complete(context.Background(), authToken, verifiedChatSession(), verifiedChatRequest())
	if err == nil {
		t.Fatal("Complete hata döndürmedi")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("hata tipi = %T, beklenen *APIError", err)
	}
	if apiErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("StatusCode = %d, beklenen %d", apiErr.StatusCode, http.StatusTooManyRequests)
	}
	if apiErr.Code != "freebuff_rate_limited" {
		t.Fatalf("Code = %q, beklenen %q", apiErr.Code, "freebuff_rate_limited")
	}
	if apiErr.Message != "Freebuff sohbet limiti aşıldı" {
		t.Fatalf("Message = %q, beklenen sanitize edilmiş genel mesaj", apiErr.Message)
	}

	for _, sensitive := range []string{authToken, "Bearer", headerAuthorization, "fingerprintHash", "secret-prompt"} {
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("hata hassas değeri %q sızdırıyor: %v", sensitive, err)
		}
	}
}

func TestClientCompleteTransportErrorDoesNotLeakAuthorization(t *testing.T) {
	t.Parallel()

	authToken := "secret-transport-token"
	client, _ := newVerifiedChatClient(t, authToken, func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get(headerAuthorization); got != "Bearer "+authToken {
			t.Fatal("transport doğrulaması beklenen Authorization header'ı görmedi")
		}

		return nil, fmt.Errorf("transport failed after %s=%q", headerAuthorization, req.Header.Get(headerAuthorization))
	})

	_, err := client.Complete(context.Background(), authToken, verifiedChatSession(), verifiedChatRequest())
	if err == nil {
		t.Fatal("Complete hata döndürmedi")
	}

	if strings.Contains(err.Error(), authToken) || strings.Contains(err.Error(), "Bearer") || strings.Contains(err.Error(), headerAuthorization) {
		t.Fatalf("transport hatası hassas Authorization bilgisini sızdırıyor: %v", err)
	}
}

func TestClientStreamErrorEventReturnsSanitizedError(t *testing.T) {
	t.Parallel()

	authToken := "secret-stream-error-token"
	client, _ := newVerifiedChatClient(t, authToken, func(req *http.Request) (*http.Response, error) {
		body := fmt.Sprintf(`data: {"error":{"message":"Authorization Bearer %s fingerprintHash secret-prompt","type":"rate_limit_error","code":"freebuff_rate_limited"}}

`, authToken)

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})

	deltas, errs := client.Stream(context.Background(), authToken, verifiedChatSession(), verifiedStreamChatRequest())

	gotDeltas := collectStringChannel(t, deltas)
	gotErrs := collectErrorChannel(t, errs)

	if len(gotDeltas) != 0 {
		t.Fatalf("deltas = %#v, upstream error event için delta beklenmiyordu", gotDeltas)
	}
	if len(gotErrs) != 1 {
		t.Fatalf("errs = %#v, tek sanitize edilmiş hata bekleniyordu", gotErrs)
	}

	var apiErr *APIError
	if !errors.As(gotErrs[0], &apiErr) {
		t.Fatalf("hata tipi = %T, beklenen *APIError", gotErrs[0])
	}
	if apiErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("StatusCode = %d, beklenen %d", apiErr.StatusCode, http.StatusTooManyRequests)
	}
	if apiErr.Code != "freebuff_rate_limited" {
		t.Fatalf("Code = %q, beklenen freebuff_rate_limited", apiErr.Code)
	}
	if apiErr.Message != "Freebuff sohbet limiti aşıldı" {
		t.Fatalf("Message = %q, beklenen sanitize edilmiş mesaj", apiErr.Message)
	}
	for _, sensitive := range []string{authToken, "Bearer", headerAuthorization, "fingerprintHash", "secret-prompt"} {
		if strings.Contains(gotErrs[0].Error(), sensitive) {
			t.Fatalf("stream error event hatası hassas değeri %q sızdırıyor: %v", sensitive, gotErrs[0])
		}
	}
}

func TestClientStreamMalformedEventReturnsSanitizedError(t *testing.T) {
	t.Parallel()

	authToken := "secret-stream-token"
	client, _ := newVerifiedChatClient(t, authToken, func(req *http.Request) (*http.Response, error) {
		body := "data: {\"choices\":[{\"delta\":\n\n" + verifiedStreamDoneFixture()

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})

	deltas, errs := client.Stream(context.Background(), authToken, verifiedChatSession(), verifiedStreamChatRequest())

	gotDeltas := collectStringChannel(t, deltas)
	gotErrs := collectErrorChannel(t, errs)

	if len(gotDeltas) != 0 {
		t.Fatalf("deltas = %#v, malformed event için delta beklenmiyordu", gotDeltas)
	}
	if len(gotErrs) != 1 {
		t.Fatalf("errs = %#v, tek sanitize edilmiş hata bekleniyordu", gotErrs)
	}
	if strings.Contains(gotErrs[0].Error(), authToken) || strings.Contains(gotErrs[0].Error(), "Bearer") || strings.Contains(gotErrs[0].Error(), headerAuthorization) {
		t.Fatalf("stream parse hatası hassas Authorization bilgisini sızdırıyor: %v", gotErrs[0])
	}
}

func TestClientStreamClosesChannelsOnDone(t *testing.T) {
	t.Parallel()

	client, _ := newVerifiedChatClient(t, "done-token", func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(verifiedStreamDoneFixture())),
			Request:    req,
		}, nil
	})

	deltas, errs := client.Stream(context.Background(), "done-token", verifiedChatSession(), verifiedStreamChatRequest())

	gotDeltas := collectStringChannel(t, deltas)
	gotErrs := collectErrorChannel(t, errs)

	if len(gotDeltas) != 0 {
		t.Fatalf("deltas = %#v, done marker için delta beklenmiyordu", gotDeltas)
	}
	if len(gotErrs) != 0 {
		t.Fatalf("errs = %#v, done marker için hata beklenmiyordu", gotErrs)
	}
}

func verifiedChatSession() Session {
	return Session{Status: SessionActive, InstanceID: "verified-instance"}
}

func verifiedChatRequest() openai.ChatCompletionRequest {
	temperature := 0.25
	maxTokens := 128

	return openai.ChatCompletionRequest{
		Model: "freebuff-chat-verified",
		Messages: []openai.ChatMessage{
			{Role: "system", Content: "Kısa cevap ver."},
			{Role: "user", Content: "Merhaba"},
		},
		Temperature: &temperature,
		MaxTokens:   &maxTokens,
		Tools: []json.RawMessage{
			json.RawMessage(`{"type":"function","function":{"name":"lookup_order"}}`),
		},
		ToolChoice: "auto",
	}
}

func verifiedStreamChatRequest() openai.ChatCompletionRequest {
	req := verifiedChatRequest()
	req.Stream = true

	return req
}

func verifiedCompleteSuccessFixture() string {
	return `{
		"id": "chatcmpl-verified",
		"object": "chat.completion",
		"created": 1710000000,
		"model": "freebuff-chat-verified",
		"choices": [
			{
				"index": 0,
				"message": {"role": "assistant", "content": "Doğrulanmış Freebuff yanıtı."},
				"finish_reason": "stop"
			}
		]
	}`
}

func verifiedStreamDeltaFixture(content string) string {
	payload := openai.ChatCompletionChunk{
		ID:      "chatcmpl-verified-stream",
		Object:  "chat.completion.chunk",
		Created: 1710000000,
		Model:   "freebuff-chat-verified",
		Choices: []openai.ChatCompletionChoice{{
			Index: 0,
			Delta: &openai.ChatMessage{Role: "assistant", Content: content},
		}},
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}

	return "data: " + string(encoded) + "\n\n"
}

func verifiedStreamDoneFixture() string {
	return "data: [DONE]\n\n"
}

func assertVerifiedUpstreamChatRequest(t *testing.T, req *http.Request, want openai.ChatCompletionRequest, wantStream bool) {
	t.Helper()

	if req.URL.Path != chatEndpointPath {
		t.Fatalf("path = %s, beklenen %s", req.URL.Path, chatEndpointPath)
	}
	if got := req.Header.Get(headerAuthorization); got == "" {
		t.Fatal("Authorization header boş olmamalı")
	}

	var gotBody upstreamChatRequest
	if err := json.NewDecoder(req.Body).Decode(&gotBody); err != nil {
		t.Fatalf("chat request JSON çözülemedi: %v", err)
	}

	if !reflect.DeepEqual(gotBody.Messages, want.Messages) {
		t.Fatalf("messages = %#v, beklenen %#v", gotBody.Messages, want.Messages)
	}
	if gotBody.Model != want.Model {
		t.Fatalf("model = %q, beklenen %q", gotBody.Model, want.Model)
	}
	if gotBody.Stream != wantStream {
		t.Fatalf("stream = %v, beklenen %v", gotBody.Stream, wantStream)
	}
	if gotBody.Temperature == nil || want.Temperature == nil || *gotBody.Temperature != *want.Temperature {
		t.Fatalf("temperature = %v, beklenen %v", gotBody.Temperature, want.Temperature)
	}
	if gotBody.MaxTokens == nil || want.MaxTokens == nil || *gotBody.MaxTokens != *want.MaxTokens {
		t.Fatalf("max_tokens = %v, beklenen %v", gotBody.MaxTokens, want.MaxTokens)
	}
	if len(gotBody.Tools) != len(want.Tools) || string(gotBody.Tools[0]) != string(want.Tools[0]) {
		t.Fatalf("tools = %s, beklenen %s", gotBody.Tools, want.Tools)
	}
	if gotBody.ToolChoice != want.ToolChoice {
		t.Fatalf("tool_choice = %#v, beklenen %#v", gotBody.ToolChoice, want.ToolChoice)
	}
	if gotBody.CodebuffMetadata.RunID != verifiedRunID {
		t.Fatalf("run_id = %q, beklenen %q", gotBody.CodebuffMetadata.RunID, verifiedRunID)
	}
	if gotBody.CodebuffMetadata.ClientID == "" {
		t.Fatal("client_id boş olmamalı")
	}
	if gotBody.CodebuffMetadata.CostMode != freebuffCostMode {
		t.Fatalf("cost_mode = %q, beklenen %q", gotBody.CodebuffMetadata.CostMode, freebuffCostMode)
	}
	if gotBody.CodebuffMetadata.FreebuffInstanceID != "verified-instance" {
		t.Fatalf("freebuff_instance_id = %q, beklenen verified-instance", gotBody.CodebuffMetadata.FreebuffInstanceID)
	}
}

func newVerifiedChatClient(t *testing.T, authToken string, chat func(*http.Request) (*http.Response, error)) (*Client, *verifiedChatTransport) {
	t.Helper()

	transport := &verifiedChatTransport{t: t, authToken: authToken, chat: chat}
	return &Client{baseURL: mustParseURL(t, "https://freebuff.example.test/base"), httpClient: &http.Client{Transport: transport}}, transport
}

type verifiedChatTransport struct {
	t            *testing.T
	authToken    string
	chat         func(*http.Request) (*http.Response, error)
	startCount   int
	chatCount    int
	startAgentID string
}

func (v *verifiedChatTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	v.t.Helper()

	if got := req.Header.Get(headerAuthorization); got != "Bearer "+v.authToken {
		v.t.Fatalf("Authorization = %q, beklenen bearer token", got)
	}

	switch req.URL.Path {
	case agentRunsEndpointPath:
		return v.handleStartAgentRun(req), nil
	case chatEndpointPath:
		if v.startCount == 0 {
			v.t.Fatal("chat çağrısından önce agent run başlatılmadı")
		}
		v.chatCount++
		return v.chat(req)
	default:
		v.t.Fatalf("beklenmeyen path: %s", req.URL.Path)
		return nil, nil
	}
}

func (v *verifiedChatTransport) handleStartAgentRun(req *http.Request) *http.Response {
	v.t.Helper()

	if req.Method != http.MethodPost {
		v.t.Fatalf("agent run method = %s, beklenen %s", req.Method, http.MethodPost)
	}

	var payload startAgentRunRequest
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		v.t.Fatalf("agent run request JSON çözülemedi: %v", err)
	}
	if payload.Action != "START" {
		v.t.Fatalf("agent run action = %q, beklenen START", payload.Action)
	}
	if len(payload.AncestorRunIDs) != 0 {
		v.t.Fatalf("ancestorRunIds = %#v, beklenen boş", payload.AncestorRunIDs)
	}
	if payload.AgentID == "" {
		v.t.Fatal("agentId boş olmamalı")
	}

	v.startCount++
	v.startAgentID = payload.AgentID

	return jsonHTTPResponse(req, http.StatusOK, `{"runId":"`+verifiedRunID+`"}`)
}

func jsonHTTPResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func collectStringChannel(t *testing.T, ch <-chan string) []string {
	t.Helper()

	if ch == nil {
		t.Fatal("string kanalı nil döndü")
	}

	var values []string
	for {
		select {
		case value, ok := <-ch:
			if !ok {
				return values
			}
			values = append(values, value)
		case <-time.After(500 * time.Millisecond):
			t.Fatal("string kanalı kapanmadan zaman aşımına uğradı")
		}
	}
}

func collectErrorChannel(t *testing.T, ch <-chan error) []error {
	t.Helper()

	if ch == nil {
		t.Fatal("error kanalı nil döndü")
	}

	var values []error
	for {
		select {
		case value, ok := <-ch:
			if !ok {
				return values
			}
			values = append(values, value)
		case <-time.After(500 * time.Millisecond):
			t.Fatal("error kanalı kapanmadan zaman aşımına uğradı")
		}
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("URL parse edilemedi: %v", err)
	}

	return parsed
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
