package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ferdiunal/freebuff-proxy/internal/openai"
)

const (
	messageType       = "message"
	assistantRole     = "assistant"
	textBlockType     = "text"
	stopReasonEndTurn = "end_turn"
)

func ToOpenAI(req MessageRequest) (openai.ChatCompletionRequest, error) {
	messages := make([]openai.ChatMessage, 0, len(req.Messages)+1)
	if len(req.System) > 0 && string(req.System) != "null" {
		systemText, err := textFromContent(req.System)
		if err != nil {
			return openai.ChatCompletionRequest{}, err
		}
		if strings.TrimSpace(systemText) != "" {
			messages = append(messages, openai.ChatMessage{Role: "system", Content: systemText})
		}
	}

	for _, msg := range req.Messages {
		content, err := textFromContent(msg.Content)
		if err != nil {
			return openai.ChatCompletionRequest{}, err
		}
		messages = append(messages, openai.ChatMessage{Role: msg.Role, Content: content})
	}

	return openai.ChatCompletionRequest{
		Model:       req.Model,
		Messages:    messages,
		Stream:      req.Stream,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Tools:       convertTools(req.Tools),
		ToolChoice:  convertToolChoice(req.ToolChoice),
	}, nil
}

func MessageFromText(model string, text string, inputTokens int) MessageResponse {
	stopReason := stopReasonEndTurn
	return MessageResponse{
		ID:    newMessageID(),
		Type:  messageType,
		Role:  assistantRole,
		Model: model,
		Content: []ContentBlock{{
			Type: textBlockType,
			Text: text,
		}},
		StopReason: &stopReason,
		Usage: Usage{
			InputTokens:  normalizeTokenCount(inputTokens),
			OutputTokens: estimateTokens(text),
		},
	}
}

func StreamStartMessage(model string, inputTokens int) MessageResponse {
	return MessageResponse{
		ID:           newMessageID(),
		Type:         messageType,
		Role:         assistantRole,
		Model:        model,
		Content:      []ContentBlock{},
		StopReason:   nil,
		StopSequence: nil,
		Usage: Usage{
			InputTokens:  normalizeTokenCount(inputTokens),
			OutputTokens: 1,
		},
	}
}

func CountTokens(req MessageRequest) int {
	total := estimateTokens(req.Model)
	if len(req.System) > 0 && string(req.System) != "null" {
		if text, err := textFromContent(req.System); err == nil {
			total += estimateTokens(text)
		}
	}
	for _, msg := range req.Messages {
		total += estimateTokens(msg.Role)
		if text, err := textFromContent(msg.Content); err == nil {
			total += estimateTokens(text)
		}
	}
	for _, tool := range req.Tools {
		total += estimateTokens(string(tool))
	}

	return normalizeTokenCount(total)
}

func Error(errorType string, message string) ErrorResponse {
	if errorType == "" {
		errorType = "api_error"
	}
	return ErrorResponse{
		Type: "error",
		Error: ErrorObject{
			Type:    errorType,
			Message: message,
		},
	}
}

func textFromContent(raw json.RawMessage) (string, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}

	var blocks []struct {
		Type    string          `json:"type"`
		Text    string          `json:"text"`
		Name    string          `json:"name"`
		Input   json.RawMessage `json:"input"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", fmt.Errorf("anthropic content must be a string or content block array")
	}

	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if block.Text != "" {
				parts = append(parts, block.Text)
			}
		case "tool_result":
			if len(block.Content) == 0 || string(block.Content) == "null" {
				continue
			}
			text, err := textFromContent(block.Content)
			if err != nil {
				return "", err
			}
			if text != "" {
				parts = append(parts, text)
			}
		case "tool_use":
			if block.Name != "" {
				parts = append(parts, fmt.Sprintf("Tool use %s: %s", block.Name, string(block.Input)))
			}
		}
	}

	return strings.Join(parts, "\n"), nil
}

func convertTools(tools []json.RawMessage) []json.RawMessage {
	if len(tools) == 0 {
		return nil
	}

	converted := make([]json.RawMessage, 0, len(tools))
	for _, raw := range tools {
		var tool struct {
			Name        string          `json:"name"`
			Description string          `json:"description,omitempty"`
			InputSchema json.RawMessage `json:"input_schema,omitempty"`
		}
		if err := json.Unmarshal(raw, &tool); err != nil || tool.Name == "" {
			continue
		}

		payload := map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  json.RawMessage(defaultSchema(tool.InputSchema)),
			},
		}
		encoded, err := json.Marshal(payload)
		if err == nil {
			converted = append(converted, encoded)
		}
	}

	return converted
}

func convertToolChoice(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}

	var choice struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &choice); err != nil {
		return nil
	}

	switch choice.Type {
	case "auto":
		return "auto"
	case "any":
		return "required"
	case "tool":
		if choice.Name == "" {
			return nil
		}
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": choice.Name,
			},
		}
	default:
		return nil
	}
}

func defaultSchema(raw json.RawMessage) []byte {
	if len(raw) == 0 || string(raw) == "null" {
		return []byte(`{"type":"object","properties":{}}`)
	}

	return raw
}

func estimateTokens(text string) int {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 1
	}

	runes := utf8.RuneCountInString(trimmed)
	return normalizeTokenCount((runes + 3) / 4)
}

func normalizeTokenCount(value int) int {
	if value < 1 {
		return 1
	}

	return value
}

func newMessageID() string {
	return fmt.Sprintf("msg_%d", time.Now().UnixNano())
}
