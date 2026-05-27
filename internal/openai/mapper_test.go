package openai

import (
	"encoding/json"
	"testing"
)

// Bu testler, OpenAI eşleme yardımcılarının beklenen zarf ve alan adlarını doğrular.
//
// ## Kullanım örneği
//
// ```bash
// go test ./internal/openai
// go test ./internal/openai -run TestModels
// ```
func TestModels(t *testing.T) {
	t.Parallel()

	models := Models("x")
	if models.Object != "list" {
		t.Fatalf("object = %q, beklenen %q", models.Object, "list")
	}

	if len(models.Data) != 1 {
		t.Fatalf("data uzunluğu = %d, beklenen %d", len(models.Data), 1)
	}

	if models.Data[0].ID != "x" {
		t.Fatalf("ilk model id = %q, beklenen %q", models.Data[0].ID, "x")
	}

	payload, err := json.Marshal(models)
	if err != nil {
		t.Fatalf("json.Marshal hata döndürdü: %v", err)
	}

	assertJSONContains(t, string(payload), `"object":"list"`)
	assertJSONContains(t, string(payload), `"owned_by":"freebuff"`)
}

func TestCompletionFromText(t *testing.T) {
	t.Parallel()

	response := CompletionFromText("demo-model", "Merhaba dünya")
	if response.Object != "chat.completion" {
		t.Fatalf("object = %q, beklenen %q", response.Object, "chat.completion")
	}

	if response.Model != "demo-model" {
		t.Fatalf("model = %q, beklenen %q", response.Model, "demo-model")
	}

	if len(response.Choices) != 1 {
		t.Fatalf("choice uzunluğu = %d, beklenen %d", len(response.Choices), 1)
	}

	choice := response.Choices[0]
	if choice.Message == nil {
		t.Fatal("message alanı nil döndü")
	}

	if choice.Message.Role != "assistant" {
		t.Fatalf("message.role = %q, beklenen %q", choice.Message.Role, "assistant")
	}

	if choice.Message.Content != "Merhaba dünya" {
		t.Fatalf("message.content = %q, beklenen %q", choice.Message.Content, "Merhaba dünya")
	}

	payload, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("json.Marshal hata döndürdü: %v", err)
	}

	assertJSONContains(t, string(payload), `"object":"chat.completion"`)
	assertJSONContains(t, string(payload), `"message":{"role":"assistant","content":"Merhaba dünya"}`)
}

func TestChunkFromDelta(t *testing.T) {
	t.Parallel()

	chunk := ChunkFromDelta("demo-model", "Parça")
	if chunk.Object != "chat.completion.chunk" {
		t.Fatalf("object = %q, beklenen %q", chunk.Object, "chat.completion.chunk")
	}

	if chunk.Model != "demo-model" {
		t.Fatalf("model = %q, beklenen %q", chunk.Model, "demo-model")
	}

	if len(chunk.Choices) != 1 {
		t.Fatalf("choice uzunluğu = %d, beklenen %d", len(chunk.Choices), 1)
	}

	choice := chunk.Choices[0]
	if choice.Delta == nil {
		t.Fatal("delta alanı nil döndü")
	}

	if choice.Delta.Content != "Parça" {
		t.Fatalf("delta.content = %q, beklenen %q", choice.Delta.Content, "Parça")
	}

	payload, err := json.Marshal(chunk)
	if err != nil {
		t.Fatalf("json.Marshal hata döndürdü: %v", err)
	}

	assertJSONContains(t, string(payload), `"object":"chat.completion.chunk"`)
	assertJSONContains(t, string(payload), `"delta":{"role":"assistant","content":"Parça"}`)
}

func TestChunkFromDeltaWithMetadataKeepsProvidedStreamIdentity(t *testing.T) {
	t.Parallel()

	metadata := &StreamMetadata{ID: "chatcmplchunk-test", Created: 12345}
	first := ChunkFromDeltaWithMetadata("demo-model", "Mer", metadata)
	second := ChunkFromDeltaWithMetadata("demo-model", "haba", metadata)

	assertSameStreamMetadata(t, first, second)

	if first.ID != metadata.ID {
		t.Fatalf("id = %q, beklenen %q", first.ID, metadata.ID)
	}

	if first.Created != metadata.Created {
		t.Fatalf("created = %d, beklenen %d", first.Created, metadata.Created)
	}

	assertChunkContent(t, first, "Mer")
	assertChunkContent(t, second, "haba")
}

func TestChunkFromDeltaWithMetadataNormalizesZeroValuePointer(t *testing.T) {
	t.Parallel()

	metadata := &StreamMetadata{}
	first := ChunkFromDeltaWithMetadata("demo-model", "Mer", metadata)
	second := ChunkFromDeltaWithMetadata("demo-model", "haba", metadata)

	assertSameStreamMetadata(t, first, second)

	if metadata.ID == "" {
		t.Fatal("metadata.ID doldurulmadı")
	}

	if metadata.Created == 0 {
		t.Fatal("metadata.Created sıfır kaldı")
	}

	if first.ID != metadata.ID || first.Created != metadata.Created {
		t.Fatalf("chunk metadata = (%q, %d), beklenen (%q, %d)", first.ID, first.Created, metadata.ID, metadata.Created)
	}

	assertChunkContent(t, first, "Mer")
	assertChunkContent(t, second, "haba")
}

func TestChunkFromDeltaWithMetadataAcceptsNilMetadata(t *testing.T) {
	t.Parallel()

	chunk := ChunkFromDeltaWithMetadata("demo-model", "Mer", nil)
	if chunk.ID == "" {
		t.Fatal("chunk.ID boş döndü")
	}

	if chunk.Created == 0 {
		t.Fatal("chunk.Created sıfır döndü")
	}

	assertChunkContent(t, chunk, "Mer")
}

func TestError(t *testing.T) {
	t.Parallel()

	response := Error(429, "rate_limit_error", "Daha sonra tekrar deneyin")
	if response.Error.Message != "Daha sonra tekrar deneyin" {
		t.Fatalf("message = %q, beklenen %q", response.Error.Message, "Daha sonra tekrar deneyin")
	}

	if response.Error.Type != "rate_limit_error" {
		t.Fatalf("type = %q, beklenen %q", response.Error.Type, "rate_limit_error")
	}

	if response.Error.Code != "rate_limit_error" {
		t.Fatalf("code = %q, beklenen %q", response.Error.Code, "rate_limit_error")
	}

	payload, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("json.Marshal hata döndürdü: %v", err)
	}

	assertJSONContains(t, string(payload), `"error":{"message":"Daha sonra tekrar deneyin","type":"rate_limit_error","code":"rate_limit_error"}`)
}

func assertSameStreamMetadata(t *testing.T, first ChatCompletionChunk, second ChatCompletionChunk) {
	t.Helper()

	if first.ID != second.ID {
		t.Fatalf("chunk id değerleri eşit değil: %q != %q", first.ID, second.ID)
	}

	if first.Created != second.Created {
		t.Fatalf("created değerleri eşit değil: %d != %d", first.Created, second.Created)
	}
}

func assertChunkContent(t *testing.T, chunk ChatCompletionChunk, expected string) {
	t.Helper()

	if len(chunk.Choices) != 1 {
		t.Fatalf("choice uzunluğu = %d, beklenen %d", len(chunk.Choices), 1)
	}

	if chunk.Choices[0].Delta == nil {
		t.Fatal("delta alanı nil döndü")
	}

	if chunk.Choices[0].Delta.Content != expected {
		t.Fatalf("delta.content = %q, beklenen %q", chunk.Choices[0].Delta.Content, expected)
	}
}

func assertJSONContains(t *testing.T, payload string, expected string) {
	t.Helper()

	if !contains(payload, expected) {
		t.Fatalf("json = %s, beklenen parça = %s", payload, expected)
	}
}

func contains(value string, expected string) bool {
	return len(value) >= len(expected) && jsonContains(value, expected)
}

func jsonContains(value string, expected string) bool {
	return stringIndex(value, expected) >= 0
}

func stringIndex(value string, expected string) int {
	for i := 0; i+len(expected) <= len(value); i++ {
		if value[i:i+len(expected)] == expected {
			return i
		}
	}

	return -1
}
