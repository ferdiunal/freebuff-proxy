// Bu dosya, OpenAI biçimli Fiber handler'larını ve test edilebilir sohbet arayüzünü içerir.
//
// ## Kullanım örneği
//
// ```go
// app := httpapi.NewApp(httpapi.Options{Model: "deepseek-v3.1-terminus", Chat: chatService})
// req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
// resp, err := app.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
// _ = resp
// _ = err
// ```
package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/ferdiunal/freebuff-proxy/internal/anthropic"
	"github.com/ferdiunal/freebuff-proxy/internal/openai"
	"github.com/gofiber/fiber/v3"
)

// ChatService, HTTP katmanının gerçek Freebuff sohbet adaptöründen beklediği küçük arayüzdür.
//
// ## Kullanım örneği
//
// ```go
// type fakeChat struct{}
// func (fakeChat) Complete(ctx context.Context, req openai.ChatCompletionRequest) (string, error) { return "Merhaba", nil }
// func (fakeChat) Stream(ctx context.Context, req openai.ChatCompletionRequest) (<-chan string, <-chan error) { return deltas, errs }
// ```
type ChatService interface {
	Complete(ctx context.Context, req openai.ChatCompletionRequest) (string, error)
	Stream(ctx context.Context, req openai.ChatCompletionRequest) (<-chan string, <-chan error)
}

// ServiceError, sohbet servisinden gelen HTTP durumuna çevrilebilir hatayı taşır.
//
// ## Kullanım örneği
//
// ```go
// return "", &httpapi.ServiceError{Status: http.StatusServiceUnavailable, Code: "service_unavailable", Message: "adaptör hazır değil"}
// ```
type ServiceError struct {
	Status  int
	Code    string
	Message string
}

func (e *ServiceError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Code != "" {
		return e.Code
	}

	return http.StatusText(e.Status)
}

const streamHeartbeatInterval = 15 * time.Second

type handlers struct {
	model string
	chat  ChatService
}

type notConfiguredChatService struct{}

func newHandlers(model string, chat ChatService) *handlers {
	if chat == nil {
		chat = notConfiguredChatService{}
	}

	return &handlers{model: model, chat: chat}
}

// Health, proxy sürecinin ayakta olduğunu JSON olarak bildirir.
//
// ## Kullanım örneği
//
// ```bash
// curl http://127.0.0.1:1455/healthz
// ```
func (h *handlers) Health(c fiber.Ctx) error {
	return c.Status(http.StatusOK).JSON(map[string]string{"status": "ok"})
}

// Models, yapılandırılmış modeli OpenAI model listeleme biçiminde döndürür.
//
// ## Kullanım örneği
//
// ```bash
// curl http://127.0.0.1:1455/v1/models
// ```
func (h *handlers) Models(c fiber.Ctx) error {
	return c.Status(http.StatusOK).JSON(openai.Models(h.model))
}

// ChatCompletions, OpenAI uyumlu sohbet tamamlama isteğini tek yanıt veya SSE akışı olarak işler.
//
// ## Kullanım örneği
//
// ```bash
//
//	curl -X POST http://127.0.0.1:1455/v1/chat/completions \
//	  -H 'Content-Type: application/json' \
//	  -d '{"model":"deepseek-v3.1-terminus","messages":[{"role":"user","content":"Merhaba"}]}'
//
// ```
func (h *handlers) ChatCompletions(c fiber.Ctx) error {
	if !hasJSONBody(c.Get(fiber.HeaderContentType)) {
		return writeOpenAIError(c, http.StatusUnsupportedMediaType, "invalid_request_error", "Content-Type application/json olmalı")
	}

	var req openai.ChatCompletionRequest
	decoder := json.NewDecoder(bytes.NewReader(c.Body()))
	if err := decoder.Decode(&req); err != nil {
		return writeOpenAIError(c, http.StatusBadRequest, "invalid_request_error", "İstek gövdesi geçerli JSON olmalı")
	}
	if err := ensureSingleJSONValue(decoder); err != nil {
		return writeOpenAIError(c, http.StatusBadRequest, "invalid_request_error", "İstek gövdesi tek bir JSON nesnesi içermeli")
	}
	if req.Model == "" {
		return writeOpenAIError(c, http.StatusBadRequest, "invalid_request_error", "model alanı zorunludur")
	}
	if len(req.Messages) == 0 {
		return writeOpenAIError(c, http.StatusBadRequest, "invalid_request_error", "messages alanı en az bir mesaj içermelidir")
	}

	if req.Stream {
		return h.streamChatCompletions(c, req)
	}

	text, err := h.chat.Complete(c, req)
	if err != nil {
		return writeServiceError(c, err)
	}

	return c.Status(http.StatusOK).JSON(openai.CompletionFromText(req.Model, text))
}

func (h *handlers) AnthropicMessages(c fiber.Ctx) error {
	if !hasJSONBody(c.Get(fiber.HeaderContentType)) {
		return writeAnthropicError(c, http.StatusUnsupportedMediaType, "invalid_request_error", "Content-Type application/json olmalı")
	}

	var req anthropic.MessageRequest
	decoder := json.NewDecoder(bytes.NewReader(c.Body()))
	if err := decoder.Decode(&req); err != nil {
		return writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", "İstek gövdesi geçerli JSON olmalı")
	}
	if err := ensureSingleJSONValue(decoder); err != nil {
		return writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", "İstek gövdesi tek bir JSON nesnesi içermeli")
	}
	if req.Model == "" {
		return writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", "model alanı zorunludur")
	}
	if len(req.Messages) == 0 {
		return writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", "messages alanı en az bir mesaj içermelidir")
	}

	upstreamReq, err := anthropic.ToOpenAI(req)
	if err != nil {
		return writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
	}
	inputTokens := anthropic.CountTokens(req)
	if req.Stream {
		return h.streamAnthropicMessages(c, req, upstreamReq, inputTokens)
	}

	text, err := h.chat.Complete(c, upstreamReq)
	if err != nil {
		return writeAnthropicServiceError(c, err)
	}

	return c.Status(http.StatusOK).JSON(anthropic.MessageFromText(req.Model, text, inputTokens))
}

func (h *handlers) AnthropicCountTokens(c fiber.Ctx) error {
	if !hasJSONBody(c.Get(fiber.HeaderContentType)) {
		return writeAnthropicError(c, http.StatusUnsupportedMediaType, "invalid_request_error", "Content-Type application/json olmalı")
	}

	var req anthropic.MessageRequest
	decoder := json.NewDecoder(bytes.NewReader(c.Body()))
	if err := decoder.Decode(&req); err != nil {
		return writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", "İstek gövdesi geçerli JSON olmalı")
	}
	if err := ensureSingleJSONValue(decoder); err != nil {
		return writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", "İstek gövdesi tek bir JSON nesnesi içermeli")
	}

	return c.Status(http.StatusOK).JSON(anthropic.CountTokensResponse{InputTokens: anthropic.CountTokens(req)})
}

func (h *handlers) streamChatCompletions(c fiber.Ctx, req openai.ChatCompletionRequest) error {
	streamCtx, cancel := context.WithCancel(c)
	deltas, errs := h.chat.Stream(streamCtx, req)
	if err, ok, closed := receiveImmediateError(errs); ok {
		cancel()
		return writeServiceError(c, err)
	} else if closed {
		errs = nil
	}

	c.Set(fiber.HeaderContentType, "text/event-stream")
	c.Set(fiber.HeaderCacheControl, "no-cache")
	c.Set(fiber.HeaderConnection, "keep-alive")

	metadata := openai.NewStreamMetadata()
	return c.SendStreamWriter(func(w *bufio.Writer) {
		defer cancel()

		heartbeat := time.NewTicker(streamHeartbeatInterval)
		defer heartbeat.Stop()

		for deltas != nil || errs != nil {
			select {
			case <-streamCtx.Done():
				return
			case <-heartbeat.C:
				if !writeStreamComment(w) {
					return
				}
			case delta, ok := <-deltas:
				if !ok {
					deltas = nil
					continue
				}

				chunk := openai.ChunkFromDeltaWithMetadata(req.Model, delta, &metadata)
				if !writeStreamData(w, chunk) {
					return
				}
			case err, ok := <-errs:
				if !ok {
					errs = nil
					continue
				}
				if err == nil {
					continue
				}

				_ = writeSSE(w, openai.Error(http.StatusServiceUnavailable, serviceErrorCode(err), serviceErrorMessage(err)))
				_, _ = io.WriteString(w, "data: [DONE]\n\n")
				_ = w.Flush()
				return
			}
		}

		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		_ = w.Flush()
	})
}

func (h *handlers) streamAnthropicMessages(c fiber.Ctx, req anthropic.MessageRequest, upstreamReq openai.ChatCompletionRequest, inputTokens int) error {
	streamCtx, cancel := context.WithCancel(c)
	deltas, errs := h.chat.Stream(streamCtx, upstreamReq)
	if err, ok, closed := receiveImmediateError(errs); ok {
		cancel()
		return writeAnthropicServiceError(c, err)
	} else if closed {
		errs = nil
	}

	c.Set(fiber.HeaderContentType, "text/event-stream")
	c.Set(fiber.HeaderCacheControl, "no-cache")
	c.Set(fiber.HeaderConnection, "keep-alive")

	return c.SendStreamWriter(func(w *bufio.Writer) {
		defer cancel()

		if !writeAnthropicEvent(w, "message_start", map[string]any{"type": "message_start", "message": anthropic.StreamStartMessage(req.Model, inputTokens)}) {
			return
		}
		if !writeAnthropicEvent(w, "content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": anthropic.ContentBlock{Type: "text", Text: ""}}) {
			return
		}

		outputTokens := 0
		heartbeat := time.NewTicker(streamHeartbeatInterval)
		defer heartbeat.Stop()

		for deltas != nil || errs != nil {
			select {
			case <-streamCtx.Done():
				return
			case <-heartbeat.C:
				if !writeStreamComment(w) {
					return
				}
			case delta, ok := <-deltas:
				if !ok {
					deltas = nil
					continue
				}

				outputTokens += 1
				if !writeAnthropicEvent(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "text_delta", "text": delta}}) {
					return
				}
			case err, ok := <-errs:
				if !ok {
					errs = nil
					continue
				}
				if err == nil {
					continue
				}

				_ = writeNamedSSE(w, "error", anthropic.Error(serviceErrorCode(err), serviceErrorMessage(err)))
				_ = w.Flush()
				return
			}
		}

		stopReason := "end_turn"
		if !writeAnthropicEvent(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0}) {
			return
		}
		if !writeAnthropicEvent(w, "message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil}, "usage": anthropic.Usage{OutputTokens: outputTokens}}) {
			return
		}
		_ = writeNamedSSE(w, "message_stop", map[string]string{"type": "message_stop"})
		_ = w.Flush()
	})
}

func (notConfiguredChatService) Complete(ctx context.Context, req openai.ChatCompletionRequest) (string, error) {
	_ = ctx
	_ = req

	return "", &ServiceError{
		Status:  http.StatusServiceUnavailable,
		Code:    "service_unavailable",
		Message: "Chat servisi henüz yapılandırılmadı",
	}
}

func (notConfiguredChatService) Stream(ctx context.Context, req openai.ChatCompletionRequest) (<-chan string, <-chan error) {
	_ = ctx
	_ = req

	deltas := make(chan string)
	close(deltas)

	errs := make(chan error, 1)
	errs <- &ServiceError{
		Status:  http.StatusServiceUnavailable,
		Code:    "service_unavailable",
		Message: "Chat servisi henüz yapılandırılmadı",
	}
	close(errs)

	return deltas, errs
}

func hasJSONBody(contentType string) bool {
	if contentType == "" {
		return true
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}

	return mediaType == "application/json"
}

func ensureSingleJSONValue(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return err
	}

	return fmt.Errorf("unexpected extra JSON value")
}

func receiveImmediateError(errs <-chan error) (error, bool, bool) {
	if errs == nil {
		return nil, false, true
	}

	select {
	case err, ok := <-errs:
		if !ok {
			return nil, false, true
		}
		if err == nil {
			return nil, false, false
		}

		return err, true, false
	default:
		return nil, false, false
	}
}

func writeOpenAIError(c fiber.Ctx, status int, code string, message string) error {
	return c.Status(status).JSON(openai.Error(status, code, message))
}

func writeServiceError(c fiber.Ctx, err error) error {
	status := serviceErrorStatus(err)
	return writeOpenAIError(c, status, serviceErrorCode(err), serviceErrorMessage(err))
}

func writeAnthropicServiceError(c fiber.Ctx, err error) error {
	return writeAnthropicError(c, serviceErrorStatus(err), serviceErrorCode(err), serviceErrorMessage(err))
}

func writeAnthropicError(c fiber.Ctx, status int, code string, message string) error {
	return c.Status(status).JSON(anthropic.Error(code, message))
}

func serviceErrorStatus(err error) int {
	var serviceErr *ServiceError
	if errors.As(err, &serviceErr) && serviceErr.Status > 0 {
		return serviceErr.Status
	}

	return http.StatusServiceUnavailable
}

func serviceErrorCode(err error) string {
	var serviceErr *ServiceError
	if errors.As(err, &serviceErr) && serviceErr.Code != "" {
		return serviceErr.Code
	}

	return "service_unavailable"
}

func serviceErrorMessage(err error) string {
	if err == nil {
		return http.StatusText(http.StatusServiceUnavailable)
	}

	return err.Error()
}

func writeStreamData(w *bufio.Writer, payload any) bool {
	if err := writeSSE(w, payload); err != nil {
		return false
	}

	return w.Flush() == nil
}

func writeStreamComment(w *bufio.Writer) bool {
	if _, err := io.WriteString(w, ": keep-alive\n\n"); err != nil {
		return false
	}

	return w.Flush() == nil
}

func writeSSE(w io.Writer, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}

func writeAnthropicEvent(w *bufio.Writer, event string, payload any) bool {
	if err := writeNamedSSE(w, event, payload); err != nil {
		return false
	}

	return w.Flush() == nil
}

func writeNamedSSE(w io.Writer, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	return err
}
