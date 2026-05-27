// Bu testler, HTTP API uç noktalarının OpenAI uyumlu yanıt ve hata zarflarını doğrular.
//
// ## Kullanım örneği
//
// ```bash
// go test ./internal/httpapi
// go test ./internal/httpapi -run TestStreamChatCompletions
// ```
package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ferdiunal/freebuff-proxy/internal/openai"
	"github.com/gofiber/fiber/v3"
)

const testModel = "deepseek-v3.1-terminus"

type fakeChatService struct {
	completeText string
	deltas       []string
	err          error
}

func (f fakeChatService) Complete(ctx context.Context, req openai.ChatCompletionRequest) (string, error) {
	_ = ctx
	_ = req

	if f.err != nil {
		return "", f.err
	}

	return f.completeText, nil
}

func (f fakeChatService) Stream(ctx context.Context, req openai.ChatCompletionRequest) (<-chan string, <-chan error) {
	_ = ctx
	_ = req

	deltas := make(chan string, len(f.deltas))
	for _, delta := range f.deltas {
		deltas <- delta
	}
	close(deltas)

	errs := make(chan error, 1)
	if f.err != nil {
		errs <- f.err
	}
	close(errs)

	return deltas, errs
}

type delayedStreamService struct {
	err error
}

func (d delayedStreamService) Complete(ctx context.Context, req openai.ChatCompletionRequest) (string, error) {
	_ = ctx
	_ = req

	return "", d.err
}

func (d delayedStreamService) Stream(ctx context.Context, req openai.ChatCompletionRequest) (<-chan string, <-chan error) {
	_ = req

	deltas := make(chan string)
	errs := make(chan error)
	go func() {
		defer close(deltas)
		defer close(errs)

		select {
		case <-ctx.Done():
			return
		case deltas <- "Merhaba":
		}
		select {
		case <-ctx.Done():
			return
		case errs <- d.err:
		}
	}()

	return deltas, errs
}

type recordingHTTPChatService struct {
	completeText    string
	streamDeltas    []string
	completeRequest openai.ChatCompletionRequest
	streamRequest   openai.ChatCompletionRequest
}

func (r *recordingHTTPChatService) Complete(ctx context.Context, req openai.ChatCompletionRequest) (string, error) {
	_ = ctx
	r.completeRequest = req
	return r.completeText, nil
}

func (r *recordingHTTPChatService) Stream(ctx context.Context, req openai.ChatCompletionRequest) (<-chan string, <-chan error) {
	_ = ctx
	r.streamRequest = req

	deltas := make(chan string, len(r.streamDeltas))
	for _, delta := range r.streamDeltas {
		deltas <- delta
	}
	close(deltas)

	errs := make(chan error)
	close(errs)

	return deltas, errs
}

func TestHealth(t *testing.T) {
	app := newTestApp(nil, "")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	resp := performRequest(t, app, req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, beklenen %d", resp.StatusCode, http.StatusOK)
	}

	var payload map[string]string
	decodeJSON(t, resp, &payload)
	if payload["status"] != "ok" {
		t.Fatalf("status payload = %q, beklenen %q", payload["status"], "ok")
	}
}

func TestModels(t *testing.T) {
	app := newTestApp(nil, "")
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)

	resp := performRequest(t, app, req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, beklenen %d", resp.StatusCode, http.StatusOK)
	}

	var payload openai.ModelListResponse
	decodeJSON(t, resp, &payload)
	if payload.Object != "list" {
		t.Fatalf("object = %q, beklenen %q", payload.Object, "list")
	}
	if len(payload.Data) != 1 || payload.Data[0].ID != testModel {
		t.Fatalf("model listesi = %#v, beklenen %s", payload.Data, testModel)
	}
}

func TestProxyAPIKeyMissingReturns401(t *testing.T) {
	app := newTestApp(nil, "secret")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	resp := performRequest(t, app, req)

	assertOpenAIError(t, resp, http.StatusUnauthorized)
}

func TestProxyAPIKeyMismatchReturns401(t *testing.T) {
	app := newTestApp(nil, "secret")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Authorization", "Bearer wrong")

	resp := performRequest(t, app, req)

	assertOpenAIError(t, resp, http.StatusUnauthorized)
}

func TestProxyAPIKeyAccepted(t *testing.T) {
	app := newTestApp(nil, "secret")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Authorization", "Bearer secret")

	resp := performRequest(t, app, req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, beklenen %d", resp.StatusCode, http.StatusOK)
	}
}

func TestProxyAPIKeyOptionalWhenEmpty(t *testing.T) {
	app := newTestApp(nil, "")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	resp := performRequest(t, app, req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, beklenen %d", resp.StatusCode, http.StatusOK)
	}
}

func TestMalformedChatJSONReturnsOpenAI400(t *testing.T) {
	app := newTestApp(fakeChatService{completeText: "tamam"}, "")
	req := newChatRequest("{")

	resp := performRequest(t, app, req)

	assertOpenAIError(t, resp, http.StatusBadRequest)
}

func TestMultipleChatJSONValuesReturnsOpenAI400(t *testing.T) {
	app := newTestApp(fakeChatService{completeText: "tamam"}, "")
	req := newChatRequest(`{"model":"deepseek-v3.1-terminus","messages":[{"role":"user","content":"Merhaba"}]} {}`)

	resp := performRequest(t, app, req)

	assertOpenAIError(t, resp, http.StatusBadRequest)
}

func TestEmptyMessagesReturnsOpenAI400(t *testing.T) {
	app := newTestApp(fakeChatService{completeText: "tamam"}, "")
	req := newChatRequest(`{"model":"deepseek-v3.1-terminus","messages":[]}`)

	resp := performRequest(t, app, req)

	assertOpenAIError(t, resp, http.StatusBadRequest)
}

func TestMissingModelReturnsOpenAI400(t *testing.T) {
	app := newTestApp(fakeChatService{completeText: "tamam"}, "")
	req := newChatRequest(`{"messages":[{"role":"user","content":"Merhaba"}]}`)

	resp := performRequest(t, app, req)

	assertOpenAIError(t, resp, http.StatusBadRequest)
}

func TestNonStreamChatCompletions(t *testing.T) {
	app := newTestApp(fakeChatService{completeText: "Merhaba dünya"}, "")
	req := newChatRequest(`{"model":"deepseek-v3.1-terminus","messages":[{"role":"user","content":"Merhaba"}]}`)

	resp := performRequest(t, app, req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, beklenen %d", resp.StatusCode, http.StatusOK)
	}

	var payload openai.ChatCompletionResponse
	decodeJSON(t, resp, &payload)
	if payload.Object != "chat.completion" {
		t.Fatalf("object = %q, beklenen %q", payload.Object, "chat.completion")
	}
	if payload.Model != testModel {
		t.Fatalf("model = %q, beklenen %q", payload.Model, testModel)
	}
	if len(payload.Choices) != 1 || payload.Choices[0].Message == nil || payload.Choices[0].Message.Content != "Merhaba dünya" {
		t.Fatalf("choices = %#v, beklenen assistant içeriği", payload.Choices)
	}
}

func TestAnthropicMessagesNonStream(t *testing.T) {
	chat := &recordingHTTPChatService{completeText: "Merhaba dünya"}
	app := newTestApp(chat, "secret")
	req := newAnthropicMessagesRequest(`{
		"model":"deepseek/deepseek-v4-pro",
		"max_tokens":128,
		"system":"Türkçe yanıt ver",
		"temperature":0.2,
		"messages":[{"role":"user","content":[{"type":"text","text":"Merhaba"}]}]
	}`)
	req.Header.Set("x-api-key", "secret")

	resp := performRequest(t, app, req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, beklenen %d", resp.StatusCode, http.StatusOK)
	}

	var payload struct {
		Type       string `json:"type"`
		Role       string `json:"role"`
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	decodeJSON(t, resp, &payload)

	if payload.Type != "message" || payload.Role != "assistant" {
		t.Fatalf("payload type/role = %q/%q", payload.Type, payload.Role)
	}
	if payload.Model != "deepseek/deepseek-v4-pro" {
		t.Fatalf("model = %q, beklenen deepseek/deepseek-v4-pro", payload.Model)
	}
	if payload.StopReason != "end_turn" {
		t.Fatalf("stop_reason = %q, beklenen end_turn", payload.StopReason)
	}
	if len(payload.Content) != 1 || payload.Content[0].Type != "text" || payload.Content[0].Text != "Merhaba dünya" {
		t.Fatalf("content = %#v, beklenen text content", payload.Content)
	}
	if payload.Usage.InputTokens <= 0 || payload.Usage.OutputTokens <= 0 {
		t.Fatalf("usage = %#v, beklenen pozitif token sayıları", payload.Usage)
	}
	if chat.completeRequest.Model != "deepseek/deepseek-v4-pro" {
		t.Fatalf("upstream model = %q", chat.completeRequest.Model)
	}
	if len(chat.completeRequest.Messages) != 2 {
		t.Fatalf("upstream messages = %#v, beklenen system+user", chat.completeRequest.Messages)
	}
	if chat.completeRequest.Messages[0].Role != "system" || chat.completeRequest.Messages[0].Content != "Türkçe yanıt ver" {
		t.Fatalf("system message = %#v", chat.completeRequest.Messages[0])
	}
	if chat.completeRequest.Messages[1].Role != "user" || chat.completeRequest.Messages[1].Content != "Merhaba" {
		t.Fatalf("user message = %#v", chat.completeRequest.Messages[1])
	}
	if chat.completeRequest.MaxTokens == nil || *chat.completeRequest.MaxTokens != 128 {
		t.Fatalf("max_tokens = %v, beklenen 128", chat.completeRequest.MaxTokens)
	}
}

func TestAnthropicMessagesStream(t *testing.T) {
	chat := &recordingHTTPChatService{streamDeltas: []string{"Mer", "haba"}}
	app := newTestApp(chat, "")
	req := newAnthropicMessagesRequest(`{
		"model":"deepseek/deepseek-v4-pro",
		"max_tokens":128,
		"stream":true,
		"messages":[{"role":"user","content":"Merhaba"}]
	}`)

	resp := performRequest(t, app, req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, beklenen %d", resp.StatusCode, http.StatusOK)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("content-type = %q, beklenen text/event-stream", resp.Header.Get("Content-Type"))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("stream gövdesi okunamadı: %v", err)
	}
	body := string(bodyBytes)
	for _, want := range []string{
		"event: message_start\n",
		"event: content_block_start\n",
		"event: content_block_delta\n",
		`"text":"Mer"`,
		`"text":"haba"`,
		"event: content_block_stop\n",
		"event: message_delta\n",
		"event: message_stop\n",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("stream body %q içermiyor:\n%s", want, body)
		}
	}
	if !chat.streamRequest.Stream {
		t.Fatal("upstream stream true olmalı")
	}
}

func TestAnthropicCountTokens(t *testing.T) {
	app := newTestApp(fakeChatService{completeText: "tamam"}, "")
	req := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(`{
		"model":"deepseek/deepseek-v4-pro",
		"system":"Türkçe yanıt ver",
		"messages":[{"role":"user","content":"Merhaba"}]
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")

	resp := performRequest(t, app, req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, beklenen %d", resp.StatusCode, http.StatusOK)
	}

	var payload struct {
		InputTokens int `json:"input_tokens"`
	}
	decodeJSON(t, resp, &payload)
	if payload.InputTokens <= 0 {
		t.Fatalf("input_tokens = %d, beklenen pozitif değer", payload.InputTokens)
	}
}

func TestStreamChatCompletions(t *testing.T) {
	app := newTestApp(fakeChatService{deltas: []string{"Mer", "haba"}}, "")
	req := newChatRequest(`{"model":"deepseek-v3.1-terminus","stream":true,"messages":[{"role":"user","content":"Merhaba"}]}`)

	resp := performRequest(t, app, req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, beklenen %d", resp.StatusCode, http.StatusOK)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("content-type = %q, beklenen text/event-stream", resp.Header.Get("Content-Type"))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("stream gövdesi okunamadı: %v", err)
	}
	body := string(bodyBytes)
	if !strings.Contains(body, "data: [DONE]\n\n") {
		t.Fatalf("stream gövdesi DONE içermiyor: %q", body)
	}

	chunks := parseSSEChunks(t, body)
	if len(chunks) == 0 {
		t.Fatal("en az bir data JSON chunk bekleniyordu")
	}
	if chunks[0].Choices[0].Delta == nil || chunks[0].Choices[0].Delta.Content == "" {
		t.Fatalf("ilk chunk delta içeriği boş: %#v", chunks[0])
	}
	if len(chunks) > 1 {
		if chunks[0].ID != chunks[1].ID {
			t.Fatalf("chunk id değerleri sabit değil: %q != %q", chunks[0].ID, chunks[1].ID)
		}
		if chunks[0].Created != chunks[1].Created {
			t.Fatalf("chunk created değerleri sabit değil: %d != %d", chunks[0].Created, chunks[1].Created)
		}
	}
}

func TestStreamChatCompletionsWritesServiceErrorEventAfterStart(t *testing.T) {
	app := newTestApp(delayedStreamService{err: &ServiceError{
		Status:  http.StatusTooManyRequests,
		Code:    "freebuff_rate_limited",
		Message: "Freebuff sohbet limiti aşıldı",
	}}, "")
	req := newChatRequest(`{"model":"deepseek-v3.1-terminus","stream":true,"messages":[{"role":"user","content":"Merhaba"}]}`)

	resp := performRequest(t, app, req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, beklenen stream başlangıcı için %d", resp.StatusCode, http.StatusOK)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("stream gövdesi okunamadı: %v", err)
	}
	body := string(bodyBytes)
	if !strings.Contains(body, `"code":"freebuff_rate_limited"`) {
		t.Fatalf("stream hata kodu içermiyor: %q", body)
	}
	if !strings.Contains(body, "data: [DONE]\n\n") {
		t.Fatalf("stream gövdesi DONE içermiyor: %q", body)
	}
}

func TestStreamChatCompletionsWithImmediateServiceErrorReturnsOpenAI503(t *testing.T) {
	app := newTestApp(fakeChatService{err: &ServiceError{
		Status:  http.StatusServiceUnavailable,
		Code:    "upstream_chat_unavailable",
		Message: "Freebuff upstream sohbet istemcisi kullanılamıyor",
	}}, "")
	req := newChatRequest(`{"model":"deepseek-v3.1-terminus","stream":true,"messages":[{"role":"user","content":"Merhaba"}]}`)

	resp := performRequest(t, app, req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, beklenen %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("content-type = %q, beklenen application/json", resp.Header.Get("Content-Type"))
	}

	var payload openai.ErrorResponse
	decodeJSON(t, resp, &payload)
	if payload.Error.Code != "upstream_chat_unavailable" {
		t.Fatalf("error code = %q, beklenen upstream_chat_unavailable", payload.Error.Code)
	}
	if payload.Error.Message == "" {
		t.Fatal("OpenAI hata mesajı boş olmamalı")
	}
}

func newTestApp(chat ChatService, proxyAPIKey string) *fiber.App {
	return NewApp(Options{
		Model:       testModel,
		ProxyAPIKey: proxyAPIKey,
		Chat:        chat,
	})
}

func performRequest(t *testing.T, app *fiber.App, req *http.Request) *http.Response {
	t.Helper()

	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatalf("request hata döndürdü: %v", err)
	}

	return resp
}

func newChatRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func newAnthropicMessagesRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	return req
}

func decodeJSON(t *testing.T, resp *http.Response, dst any) {
	t.Helper()

	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("JSON decode hata döndürdü: %v", err)
	}
}

func assertOpenAIError(t *testing.T, resp *http.Response, expectedStatus int) {
	t.Helper()
	defer resp.Body.Close()

	if resp.StatusCode != expectedStatus {
		t.Fatalf("status = %d, beklenen %d", resp.StatusCode, expectedStatus)
	}

	var payload openai.ErrorResponse
	decodeJSON(t, resp, &payload)
	if payload.Error.Message == "" {
		t.Fatal("OpenAI hata mesajı boş olmamalı")
	}
	if payload.Error.Type == "" {
		t.Fatal("OpenAI hata type alanı boş olmamalı")
	}
}

func parseSSEChunks(t *testing.T, body string) []openai.ChatCompletionChunk {
	t.Helper()

	var chunks []openai.ChatCompletionChunk
	for _, event := range strings.Split(body, "\n\n") {
		if event == "" || event == "data: [DONE]" {
			continue
		}
		if !strings.HasPrefix(event, "data: ") {
			t.Fatalf("beklenmeyen SSE satırı: %q", event)
		}

		var chunk openai.ChatCompletionChunk
		if err := json.Unmarshal([]byte(strings.TrimPrefix(event, "data: ")), &chunk); err != nil {
			t.Fatalf("chunk JSON decode hata döndürdü: %v", err)
		}
		chunks = append(chunks, chunk)
	}

	return chunks
}
