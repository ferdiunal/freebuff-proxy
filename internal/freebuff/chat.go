package freebuff

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ferdiunal/freebuff-proxy/internal/openai"
)

const (
	chatEndpointPath       = "/api/v1/chat/completions"
	agentRunsEndpointPath  = "/api/v1/agent-runs"
	freebuffCostMode       = "free"
	defaultFreeAgentID     = "base2-free"
	chatErrorStageAgentRun = "agent_run"
	chatErrorStageChat     = "chat"
)

var freebuffAgentIDsByModel = map[string]string{
	"minimax/minimax-m2.7":       "base2-free",
	"moonshotai/kimi-k2.6":       "base2-free-kimi",
	"deepseek/deepseek-v4-pro":   "base2-free-deepseek",
	"deepseek/deepseek-v4-flash": "base2-free-deepseek-flash",
	"freebuff-chat-verified":     "base2-free",
}

var canonicalFreebuffModelsByAlias = map[string]string{
	"deepseek-v4-pro":        "deepseek/deepseek-v4-pro",
	"deepseek-v4-flash":      "deepseek/deepseek-v4-flash",
	"deepseek-v3.1-terminus": "deepseek/deepseek-v4-pro",
}

// Complete, Freebuff upstream sohbet uç noktasına Codebuff CLI uyumlu non-stream istek gönderir.
//
// ## Kullanım örneği
//
// ```go
//
//	session := freebuff.Session{InstanceID: "freebuff-proxy"}
//	text, err := client.Complete(ctx, token, session, openai.ChatCompletionRequest{
//		Model:    "deepseek/deepseek-v4-pro",
//		Messages: []openai.ChatMessage{{Role: "user", Content: "Merhaba"}},
//	})
//
//	if err != nil {
//		return err
//	}
//
// fmt.Println(text)
// ```
func (c *Client) Complete(ctx context.Context, token string, activeSession Session, req openai.ChatCompletionRequest) (string, error) {
	upstreamChatReq := normalizeChatCompletionRequest(req)
	upstreamChatReq.Model = modelForActiveSession(activeSession, upstreamChatReq.Model)
	runID, err := c.startAgentRun(ctx, token, upstreamChatReq.Model)
	if err != nil {
		return "", err
	}

	upstreamReq, err := buildUpstreamChatRequest(upstreamChatReq, false, runID, activeSession)
	if err != nil {
		return "", err
	}

	resp, err := c.doChatRequest(ctx, token, upstreamReq, "application/json")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", chatStatusError(resp, chatErrorStageChat)
	}

	var payload openai.ChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", chatDecodeError()
	}

	if len(payload.Choices) == 0 || payload.Choices[0].Message == nil {
		return "", chatDecodeError()
	}

	return payload.Choices[0].Message.Content, nil
}

// Stream, Freebuff upstream sohbet uç noktasından Codebuff CLI metadata'lı SSE delta akışı okur.
//
// ## Kullanım örneği
//
// ```go
//
//	session := freebuff.Session{InstanceID: "freebuff-proxy"}
//	deltas, errs := client.Stream(ctx, token, session, openai.ChatCompletionRequest{
//		Model:    "deepseek/deepseek-v4-pro",
//		Messages: []openai.ChatMessage{{Role: "user", Content: "Merhaba"}},
//	})
//
//	for delta := range deltas {
//		fmt.Print(delta)
//	}
//
//	if err := <-errs; err != nil {
//		return err
//	}
//
// ```
func (c *Client) Stream(ctx context.Context, token string, activeSession Session, req openai.ChatCompletionRequest) (<-chan string, <-chan error) {
	upstreamChatReq := normalizeChatCompletionRequest(req)
	upstreamChatReq.Model = modelForActiveSession(activeSession, upstreamChatReq.Model)
	runID, err := c.startAgentRun(ctx, token, upstreamChatReq.Model)
	if err != nil {
		return failedChatStream(err)
	}

	upstreamReq, err := buildUpstreamChatRequest(upstreamChatReq, true, runID, activeSession)
	if err != nil {
		return failedChatStream(err)
	}

	resp, err := c.doChatRequest(ctx, token, upstreamReq, "text/event-stream")
	if err != nil {
		return failedChatStream(err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		defer resp.Body.Close()
		return failedChatStream(chatStatusError(resp, chatErrorStageChat))
	}

	deltas := make(chan string)
	errs := make(chan error, 1)

	go func() {
		defer close(deltas)
		defer close(errs)
		defer resp.Body.Close()

		if err := scanChatStream(ctx, resp.Body, deltas); err != nil {
			sendChatError(ctx, errs, err)
		}
	}()

	return deltas, errs
}

type startAgentRunRequest struct {
	Action         string   `json:"action"`
	AgentID        string   `json:"agentId"`
	AncestorRunIDs []string `json:"ancestorRunIds"`
}

type startAgentRunResponse struct {
	RunID string `json:"runId"`
}

type upstreamChatRequest struct {
	Model            string               `json:"model"`
	Messages         []openai.ChatMessage `json:"messages"`
	Stream           bool                 `json:"stream,omitempty"`
	Temperature      *float64             `json:"temperature,omitempty"`
	MaxTokens        *int                 `json:"max_tokens,omitempty"`
	Tools            []json.RawMessage    `json:"tools,omitempty"`
	ToolChoice       any                  `json:"tool_choice,omitempty"`
	CodebuffMetadata codebuffMetadata     `json:"codebuff_metadata"`
}

type codebuffMetadata struct {
	RunID              string `json:"run_id"`
	ClientID           string `json:"client_id"`
	CostMode           string `json:"cost_mode"`
	FreebuffInstanceID string `json:"freebuff_instance_id,omitempty"`
}

func (c *Client) startAgentRun(ctx context.Context, token string, model string) (string, error) {
	resp, err := c.doJSONRequest(ctx, token, agentRunsEndpointPath, startAgentRunRequest{
		Action:         "START",
		AgentID:        agentIDForModel(model),
		AncestorRunIDs: []string{},
	}, "application/json")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", chatStatusError(resp, chatErrorStageAgentRun)
	}

	var payload startAgentRunResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", chatDecodeError()
	}
	if strings.TrimSpace(payload.RunID) == "" {
		return "", chatDecodeError()
	}

	return payload.RunID, nil
}

func normalizeChatCompletionRequest(req openai.ChatCompletionRequest) openai.ChatCompletionRequest {
	req.Model = CanonicalModelName(req.Model)
	return req
}

func modelForActiveSession(session Session, fallback string) string {
	for _, model := range []string{session.Model, session.CurrentModel} {
		if strings.TrimSpace(model) != "" {
			return CanonicalModelName(model)
		}
	}

	return fallback
}

// CanonicalModelName, Freebuff model alias'ını upstream'in beklediği kanonik model adına çevirir.
//
// ## Kullanım örneği
//
// ```go
// model := freebuff.CanonicalModelName("deepseek-v4-pro")
// fmt.Println(model) // deepseek/deepseek-v4-pro
// ```
func CanonicalModelName(model string) string {
	if canonicalModel, ok := canonicalFreebuffModelsByAlias[model]; ok {
		return canonicalModel
	}

	return model
}

func buildUpstreamChatRequest(req openai.ChatCompletionRequest, stream bool, runID string, activeSession Session) (upstreamChatRequest, error) {
	clientID, err := newClientSessionID()
	if err != nil {
		return upstreamChatRequest{}, err
	}

	return upstreamChatRequest{
		Model:       req.Model,
		Messages:    req.Messages,
		Stream:      stream,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Tools:       req.Tools,
		ToolChoice:  req.ToolChoice,
		CodebuffMetadata: codebuffMetadata{
			RunID:              runID,
			ClientID:           clientID,
			CostMode:           freebuffCostMode,
			FreebuffInstanceID: activeSession.InstanceID,
		},
	}, nil
}

func newClientSessionID() (string, error) {
	var randomBytes [16]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return "", &APIError{Code: "upstream_chat_error", Message: "Freebuff sohbet isteği hazırlanamadı"}
	}

	return "freebuff-proxy-" + hex.EncodeToString(randomBytes[:]), nil
}

func agentIDForModel(model string) string {
	if agentID, ok := freebuffAgentIDsByModel[CanonicalModelName(model)]; ok {
		return agentID
	}

	return defaultFreeAgentID
}

func (c *Client) doChatRequest(ctx context.Context, token string, req upstreamChatRequest, accept string) (*http.Response, error) {
	return c.doJSONRequest(ctx, token, chatEndpointPath, req, accept)
}

func (c *Client) doJSONRequest(ctx context.Context, token string, path string, payload any, accept string) (*http.Response, error) {
	if strings.TrimSpace(token) == "" {
		return nil, &APIError{
			StatusCode: http.StatusUnauthorized,
			Code:       "freebuff_auth_missing",
			Message:    "Freebuff kimlik bilgisi bulunamadı",
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, chatEncodeError()
	}

	requestURL := c.baseURL.ResolveReference(&url.URL{Path: path})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build freebuff chat request: %w", err)
	}

	httpReq.Header.Set(headerAuthorization, "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")
	if accept != "" {
		httpReq.Header.Set("Accept", accept)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, &APIError{
			Code:    "upstream_chat_unavailable",
			Message: "Freebuff sohbet upstream isteği başarısız oldu",
		}
	}

	return resp, nil
}

func failedChatStream(err error) (<-chan string, <-chan error) {
	deltas := make(chan string)
	close(deltas)

	errs := make(chan error, 1)
	if err != nil {
		errs <- err
	}
	close(errs)

	return deltas, errs
}

func scanChatStream(ctx context.Context, body io.Reader, deltas chan<- string) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var dataLines []string
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			finished, err := handleChatStreamEvent(ctx, dataLines, deltas)
			dataLines = nil
			if err != nil || finished {
				return err
			}
			continue
		}

		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}

	if err := scanner.Err(); err != nil {
		return &APIError{Code: "upstream_chat_error", Message: "Freebuff sohbet akışı okunamadı"}
	}

	_, err := handleChatStreamEvent(ctx, dataLines, deltas)
	return err
}

func handleChatStreamEvent(ctx context.Context, dataLines []string, deltas chan<- string) (bool, error) {
	if len(dataLines) == 0 {
		return false, nil
	}

	payload := strings.Join(dataLines, "\n")
	if payload == "[DONE]" {
		return true, nil
	}

	var errorEvent struct {
		Error *openai.APIErrorObject `json:"error"`
	}
	if err := json.Unmarshal([]byte(payload), &errorEvent); err != nil {
		return false, chatDecodeError()
	}
	if errorEvent.Error != nil {
		return false, chatStreamEventError(errorEvent.Error.Code)
	}

	var chunk openai.ChatCompletionChunk
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		return false, chatDecodeError()
	}

	if len(chunk.Choices) == 0 || chunk.Choices[0].Delta == nil || chunk.Choices[0].Delta.Content == "" {
		return false, nil
	}

	select {
	case deltas <- chunk.Choices[0].Delta.Content:
	case <-ctx.Done():
		return true, &APIError{Code: "upstream_chat_unavailable", Message: "Freebuff sohbet akışı iptal edildi"}
	}

	return false, nil
}

func sendChatError(ctx context.Context, errs chan<- error, err error) {
	if err == nil {
		return
	}

	select {
	case errs <- err:
	case <-ctx.Done():
	}
}

func chatStatusError(resp *http.Response, stage string) *APIError {
	statusCode := resp.StatusCode
	if code, ok := safeUpstreamErrorCode(resp.Body); ok {
		return &APIError{StatusCode: statusCode, Code: code, Message: safeUpstreamErrorMessages[code]}
	}

	apiErr := &APIError{StatusCode: statusCode}
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		apiErr.Code = "freebuff_auth_failed"
		apiErr.Message = "Freebuff sohbet yetkilendirmesi başarısız oldu"
	case http.StatusTooManyRequests:
		apiErr.Code = "freebuff_rate_limited"
		apiErr.Message = "Freebuff sohbet limiti aşıldı"
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		apiErr.Code = "upstream_chat_unavailable"
		apiErr.Message = "Freebuff sohbet upstream geçici olarak kullanılamıyor"
	default:
		if stage == chatErrorStageAgentRun {
			apiErr.Code = "upstream_agent_run_error"
			apiErr.Message = fmt.Sprintf("Freebuff agent run upstream hatası: status %d", statusCode)
		} else {
			apiErr.Code = "upstream_chat_error"
			apiErr.Message = fmt.Sprintf("Freebuff sohbet upstream hatası: status %d", statusCode)
		}
	}

	return apiErr
}

var safeUpstreamErrorMessages = map[string]string{
	"freebuff_update_required": "Freebuff oturum bilgisi eksik veya eski",
	"session_expired":          "Freebuff oturumu süresi doldu",
	"session_model_mismatch":   "Freebuff oturum modeli istek modeliyle eşleşmiyor",
	"session_superseded":       "Freebuff oturumu başka bir oturum tarafından değiştirildi",
	"waiting_room_queued":      "Freebuff oturumu hâlâ kuyrukta",
	"waiting_room_required":    "Freebuff bekleme odası oturumu gerekli",
}

func safeUpstreamErrorCode(body io.Reader) (string, bool) {
	if body == nil {
		return "", false
	}

	var payload map[string]any
	if err := json.NewDecoder(io.LimitReader(body, 8*1024)).Decode(&payload); err != nil {
		return "", false
	}

	for _, code := range candidateUpstreamErrorCodes(payload) {
		if _, ok := safeUpstreamErrorMessages[code]; ok {
			return code, true
		}
	}

	return "", false
}

func candidateUpstreamErrorCodes(payload map[string]any) []string {
	var codes []string
	if code, ok := payload["code"].(string); ok {
		codes = append(codes, code)
	}
	if code, ok := payload["error"].(string); ok {
		codes = append(codes, code)
	}
	if errorObject, ok := payload["error"].(map[string]any); ok {
		if code, ok := errorObject["code"].(string); ok {
			codes = append(codes, code)
		}
	}

	return codes
}

func chatStreamEventError(code string) *APIError {
	switch code {
	case "freebuff_auth_failed":
		return &APIError{StatusCode: http.StatusUnauthorized, Code: "freebuff_auth_failed", Message: "Freebuff sohbet yetkilendirmesi başarısız oldu"}
	case "freebuff_rate_limited":
		return &APIError{StatusCode: http.StatusTooManyRequests, Code: "freebuff_rate_limited", Message: "Freebuff sohbet limiti aşıldı"}
	case "upstream_chat_unavailable":
		return &APIError{StatusCode: http.StatusServiceUnavailable, Code: "upstream_chat_unavailable", Message: "Freebuff sohbet upstream geçici olarak kullanılamıyor"}
	default:
		return &APIError{StatusCode: http.StatusBadGateway, Code: "upstream_chat_error", Message: "Freebuff sohbet akışı hata döndürdü"}
	}
}

func chatEncodeError() *APIError {
	return &APIError{
		Code:    "upstream_chat_error",
		Message: "Freebuff sohbet isteği hazırlanamadı",
	}
}

func chatDecodeError() *APIError {
	return &APIError{
		Code:    "upstream_chat_error",
		Message: "Freebuff sohbet yanıtı çözülemedi",
	}
}
