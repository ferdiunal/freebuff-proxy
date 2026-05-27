// Bu testler, production uygulama kurulumunun gerçek Freebuff chat istemcisine bağlandığını doğrular.
//
// ## Kullanım örneği
//
// ```bash
// go test ./internal/app -run TestNewAppUsesRealFreebuffChatClient
// go test ./internal/app
// ```
package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ferdiunal/freebuff-proxy/internal/config"
	"github.com/ferdiunal/freebuff-proxy/internal/credentials"
	"github.com/ferdiunal/freebuff-proxy/internal/openai"
	"github.com/gofiber/fiber/v3"
)

func TestNewAppUsesRealFreebuffChatClient(t *testing.T) {
	var agentRunStarted atomic.Bool
	var chatCalled atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/v1/freebuff/session":
			_, _ = w.Write([]byte(`{"status":"active","instanceId":"freebuff-proxy"}`))
		case "/api/v1/agent-runs":
			agentRunStarted.Store(true)
			if got := r.Header.Get("Authorization"); got != "Bearer freebuff-token" {
				http.Error(w, `{"error":"unexpected authorization header"}`, http.StatusUnauthorized)
				return
			}

			var body struct {
				Action         string   `json:"action"`
				AgentID        string   `json:"agentId"`
				AncestorRunIDs []string `json:"ancestorRunIds"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("agent run body decode hata döndürdü: %v", err)
			}
			if body.Action != "START" || body.AgentID != "base2-free-deepseek" || len(body.AncestorRunIDs) != 0 {
				t.Fatalf("agent run body = %#v", body)
			}

			_, _ = w.Write([]byte(`{"runId":"run-app-test"}`))
		case "/api/v1/chat/completions":
			chatCalled.Store(true)
			if !agentRunStarted.Load() {
				t.Fatal("chat çağrısından önce agent run başlatılmadı")
			}
			if got := r.Header.Get("Authorization"); got != "Bearer freebuff-token" {
				http.Error(w, `{"error":"unexpected authorization header"}`, http.StatusUnauthorized)
				return
			}

			var body struct {
				Model            string `json:"model"`
				CodebuffMetadata struct {
					RunID              string `json:"run_id"`
					CostMode           string `json:"cost_mode"`
					FreebuffInstanceID string `json:"freebuff_instance_id"`
				} `json:"codebuff_metadata"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("chat body decode hata döndürdü: %v", err)
			}
			if body.Model != "deepseek/deepseek-v4-pro" {
				t.Fatalf("model = %q, beklenen canonical DeepSeek modeli", body.Model)
			}
			if body.CodebuffMetadata.RunID != "run-app-test" || body.CodebuffMetadata.CostMode != "free" || body.CodebuffMetadata.FreebuffInstanceID != "freebuff-proxy" {
				t.Fatalf("codebuff_metadata = %#v", body.CodebuffMetadata)
			}

			_, _ = w.Write([]byte(`{"id":"chatcmpl-test","object":"chat.completion","created":0,"model":"deepseek/deepseek-v4-pro","choices":[{"index":0,"message":{"role":"assistant","content":"Merhaba upstream"},"finish_reason":"stop"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	credentialsPath := filepath.Join(t.TempDir(), "credentials.json")
	store := credentials.FileStore{Path: credentialsPath}
	if err := store.Save(context.Background(), credentials.Credential{AuthToken: "freebuff-token"}); err != nil {
		t.Fatalf("credential kaydı hata döndürdü: %v", err)
	}

	fiberApp, err := NewApp(config.Config{
		APIBaseURL:      server.URL,
		Model:           "deepseek-v4-pro",
		CredentialsPath: credentialsPath,
	})
	if err != nil {
		t.Fatalf("NewApp hata döndürdü: %v", err)
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/chat/completions",
		strings.NewReader(`{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"Merhaba"}]}`),
	)
	req.Header.Set("Content-Type", "application/json")

	resp, err := fiberApp.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatalf("chat request hata döndürdü: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, beklenen %d", resp.StatusCode, http.StatusOK)
	}
	if !chatCalled.Load() {
		t.Fatal("upstream chat endpoint çağrılmadı")
	}

	var payload openai.ChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("response JSON decode hata döndürdü: %v", err)
	}
	if len(payload.Choices) != 1 || payload.Choices[0].Message == nil || payload.Choices[0].Message.Content != "Merhaba upstream" {
		t.Fatalf("choices = %#v, beklenen content %q", payload.Choices, "Merhaba upstream")
	}
}
