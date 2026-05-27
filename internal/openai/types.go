package openai

import "encoding/json"

// ChatCompletionRequest, OpenAI uyumlu sohbet tamamlama isteğini taşır.
//
// ## Kullanım örneği
//
// ```go
//
//	req := openai.ChatCompletionRequest{
//		Model: "gpt-4o-mini",
//		Messages: []openai.ChatMessage{{Role: "user", Content: "Merhaba"}},
//		Stream: true,
//	}
//
// ```
type ChatCompletionRequest struct {
	Model       string            `json:"model"`
	Messages    []ChatMessage     `json:"messages"`
	Stream      bool              `json:"stream,omitempty"`
	Temperature *float64          `json:"temperature,omitempty"`
	MaxTokens   *int              `json:"max_tokens,omitempty"`
	Tools       []json.RawMessage `json:"tools,omitempty"`
	ToolChoice  any               `json:"tool_choice,omitempty"`
}

// ChatMessage, sohbet rollerine ait içerik taşıyıcısıdır.
//
// ## Kullanım örneği
//
// ```go
// msg := openai.ChatMessage{Role: "assistant", Content: "Hazırım."}
// ```
type ChatMessage struct {
	Role       string `json:"role"`
	Content    string `json:"content,omitempty"`
	Name       string `json:"name,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// ChatCompletionResponse, metin tabanlı OpenAI tamamlanmış yanıtını temsil eder.
//
// ## Kullanım örneği
//
// ```go
// resp := openai.CompletionFromText("gpt-4o-mini", "Tamamlandı")
// ```
type ChatCompletionResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []ChatCompletionChoice `json:"choices"`
}

// ChatCompletionChoice, standart veya akış yanıtındaki seçim alanını temsil eder.
//
// ## Kullanım örneği
//
// ```go
// choice := openai.ChatCompletionChoice{Index: 0, FinishReason: "stop"}
// ```
type ChatCompletionChoice struct {
	Index        int          `json:"index"`
	Message      *ChatMessage `json:"message,omitempty"`
	Delta        *ChatMessage `json:"delta,omitempty"`
	FinishReason string       `json:"finish_reason,omitempty"`
}

// ChatCompletionChunk, akış modunda parça parça dönen OpenAI yanıtını taşır.
//
// ## Kullanım örneği
//
// ```go
// chunk := openai.ChunkFromDelta("gpt-4o-mini", "Mer")
// ```
type ChatCompletionChunk struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []ChatCompletionChoice `json:"choices"`
}

// ModelListResponse, model listeleme uç noktası için OpenAI biçimini sağlar.
//
// ## Kullanım örneği
//
// ```go
// models := openai.Models("gpt-4o-mini")
// ```
type ModelListResponse struct {
	Object string      `json:"object"`
	Data   []ModelInfo `json:"data"`
}

// ModelInfo, listelenen tek bir model kaydını temsil eder.
//
// ## Kullanım örneği
//
// ```go
// model := openai.ModelInfo{ID: "gpt-4o-mini", Object: "model"}
// ```
type ModelInfo struct {
	ID          string `json:"id"`
	Object      string `json:"object"`
	Created     int64  `json:"created,omitempty"`
	OwnedBy     string `json:"owned_by,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

// ErrorResponse, OpenAI hata zarfını taşır.
//
// ## Kullanım örneği
//
// ```go
// resp := openai.Error(429, "rate_limit", "Daha sonra tekrar deneyin")
// ```
type ErrorResponse struct {
	Error APIErrorObject `json:"error"`
}

// APIErrorObject, istemciye dönen hata ayrıntılarını temsil eder.
//
// ## Kullanım örneği
//
// ```go
// apiErr := openai.APIErrorObject{Message: "geçersiz istek", Type: "invalid_request_error"}
// ```
type APIErrorObject struct {
	Message string `json:"message"`
	Type    string `json:"type,omitempty"`
	Code    string `json:"code,omitempty"`
}
