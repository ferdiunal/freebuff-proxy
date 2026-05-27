package openai

import (
	"fmt"
	"strings"
	"time"
)

const (
	ownerFreebuff = "freebuff"
)

// Models, tek bir model adı için OpenAI uyumlu liste yanıtı üretir.
//
// ## Kullanım örneği
//
// ```go
// payload := openai.Models("gpt-4o-mini")
// ```
func Models(model string) ModelListResponse {
	created := time.Now().Unix()

	return ModelListResponse{
		Object: "list",
		Data: []ModelInfo{{
			ID:          model,
			Object:      "model",
			Created:     created,
			OwnedBy:     ownerFreebuff,
			DisplayName: model,
		}},
	}
}

// Error, OpenAI hata zarfını kod ve mesaj ile doldurur.
//
// ## Kullanım örneği
//
// ```go
// payload := openai.Error(400, "invalid_request_error", "model alanı zorunludur")
// ```
func Error(status int, code string, message string) ErrorResponse {
	_ = status

	return ErrorResponse{
		Error: APIErrorObject{
			Message: message,
			Type:    code,
			Code:    code,
		},
	}
}

// CompletionFromText, düz metni tamamlanmış OpenAI yanıtına dönüştürür.
//
// ## Kullanım örneği
//
// ```go
// payload := openai.CompletionFromText("gpt-4o-mini", "Merhaba")
// ```
func CompletionFromText(model string, text string) ChatCompletionResponse {
	return ChatCompletionResponse{
		ID:      newResponseID("chatcmpl"),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []ChatCompletionChoice{{
			Index: 0,
			Message: &ChatMessage{
				Role:    "assistant",
				Content: text,
			},
			FinishReason: "stop",
		}},
	}
}

// StreamMetadata, aynı SSE akışındaki chunk'lar için ortak kimlik ve zaman bilgisini taşır.
//
// ## Kullanım örneği
//
// ```go
// metadata := openai.NewStreamMetadata()
// first := openai.ChunkFromDeltaWithMetadata("gpt-4o-mini", "Mer", &metadata)
// second := openai.ChunkFromDeltaWithMetadata("gpt-4o-mini", "haba", &metadata)
// ```
type StreamMetadata struct {
	ID      string
	Created int64
}

// NewStreamMetadata, tek bir OpenAI uyumlu stream için tekrar kullanılacak metadata üretir.
//
// ## Kullanım örneği
//
// ```go
// metadata := openai.NewStreamMetadata()
// ```
func NewStreamMetadata() StreamMetadata {
	return StreamMetadata{
		ID:      newResponseID("chatcmplchunk"),
		Created: time.Now().Unix(),
	}
}

// ChunkFromDelta, tek parça kullanım için metin parçasını akış yanıtına dönüştürür.
//
// ## Kullanım örneği
//
// ```go
// payload := openai.ChunkFromDelta("gpt-4o-mini", "Mer")
// ```
func ChunkFromDelta(model string, delta string) ChatCompletionChunk {
	return ChunkFromDeltaWithMetadata(model, delta, nil)
}

// ChunkFromDeltaWithMetadata, aynı stream içindeki chunk'larda ortak metadata kullanılmasını sağlar.
//
// ## Kullanım örneği
//
// ```go
// metadata := &openai.StreamMetadata{}
// first := openai.ChunkFromDeltaWithMetadata("gpt-4o-mini", "Mer", metadata)
// second := openai.ChunkFromDeltaWithMetadata("gpt-4o-mini", "haba", metadata)
// ```
func ChunkFromDeltaWithMetadata(model string, delta string, metadata *StreamMetadata) ChatCompletionChunk {
	streamMetadata := normalizeStreamMetadata(metadata)

	return ChatCompletionChunk{
		ID:      streamMetadata.ID,
		Object:  "chat.completion.chunk",
		Created: streamMetadata.Created,
		Model:   model,
		Choices: []ChatCompletionChoice{{
			Index: 0,
			Delta: &ChatMessage{
				Role:    "assistant",
				Content: delta,
			},
		}},
	}
}

func normalizeStreamMetadata(metadata *StreamMetadata) StreamMetadata {
	if metadata == nil {
		return NewStreamMetadata()
	}

	if metadata.ID == "" {
		metadata.ID = newResponseID("chatcmplchunk")
	}
	if metadata.Created == 0 {
		metadata.Created = time.Now().Unix()
	}

	return *metadata
}

func newResponseID(prefix string) string {
	cleanPrefix := strings.TrimSpace(prefix)
	if cleanPrefix == "" {
		cleanPrefix = "resp"
	}

	return fmt.Sprintf("%s-%d", cleanPrefix, time.Now().UnixNano())
}
