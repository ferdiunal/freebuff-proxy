package anthropic

import "encoding/json"

type MessageRequest struct {
	Model       string            `json:"model"`
	MaxTokens   *int              `json:"max_tokens,omitempty"`
	Messages    []Message         `json:"messages"`
	System      json.RawMessage   `json:"system,omitempty"`
	Stream      bool              `json:"stream,omitempty"`
	Temperature *float64          `json:"temperature,omitempty"`
	Tools       []json.RawMessage `json:"tools,omitempty"`
	ToolChoice  json.RawMessage   `json:"tool_choice,omitempty"`
}

type Message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type MessageResponse struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role"`
	Content      []ContentBlock `json:"content"`
	Model        string         `json:"model"`
	StopReason   *string        `json:"stop_reason"`
	StopSequence *string        `json:"stop_sequence"`
	Usage        Usage          `json:"usage"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type CountTokensResponse struct {
	InputTokens int `json:"input_tokens"`
}

type ErrorResponse struct {
	Type  string      `json:"type"`
	Error ErrorObject `json:"error"`
}

type ErrorObject struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}
