package freebuff

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Bu testler, Freebuff oturum istemcisinin yol, başlık ve hata eşleme davranışlarını doğrular.
//
// ## Kullanım örneği
//
// ```bash
// go test ./internal/freebuff
// go test ./internal/freebuff -run TestClient
// ```
func TestNewClientRejectsInvalidBaseURL(t *testing.T) {
	t.Parallel()

	testCases := []string{
		"example.com",
		"http://",
		"://broken",
	}

	for _, baseURL := range testCases {
		t.Run(baseURL, func(t *testing.T) {
			_, err := NewClient(baseURL, nil)
			if err == nil {
				t.Fatalf("NewClient(%q) hata döndürmedi", baseURL)
			}
		})
	}
}

func TestNewClientUsesBoundedDefaultHTTPClient(t *testing.T) {
	t.Parallel()

	client, err := NewClient("https://freebuff.example.test", nil)
	if err != nil {
		t.Fatalf("NewClient hata döndürdü: %v", err)
	}
	if client.httpClient == http.DefaultClient {
		t.Fatal("varsayılan istemci http.DefaultClient olmamalı")
	}
	if client.httpClient.Timeout != 0 {
		t.Fatalf("client timeout = %v, stream gövdesini kesmemek için 0 olmalı", client.httpClient.Timeout)
	}

	transport, ok := client.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, beklenen *http.Transport", client.httpClient.Transport)
	}
	if transport.ResponseHeaderTimeout <= 0 {
		t.Fatal("ResponseHeaderTimeout pozitif olmalı")
	}
	if transport.TLSHandshakeTimeout <= 0 {
		t.Fatal("TLSHandshakeTimeout pozitif olmalı")
	}
}

func TestNewClientPreservesCustomHTTPClient(t *testing.T) {
	t.Parallel()

	custom := &http.Client{Timeout: time.Second}
	client, err := NewClient("https://freebuff.example.test", custom)
	if err != nil {
		t.Fatalf("NewClient hata döndürdü: %v", err)
	}
	if client.httpClient != custom {
		t.Fatal("özel HTTP client aynen korunmalı")
	}
}

func TestClientGetSessionUsesEndpointAndHeaders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, beklenen %s", r.Method, http.MethodGet)
		}

		if r.URL.Path != sessionEndpointPath {
			t.Fatalf("path = %s, beklenen %s", r.URL.Path, sessionEndpointPath)
		}

		if got := r.Header.Get(headerAuthorization); got != "Bearer test-token" {
			t.Fatal("Authorization header beklenen bearer token ile eşleşmedi")
		}

		if got := r.Header.Get(headerInstanceID); got != "instance-123" {
			t.Fatalf("x-freebuff-instance-id = %q, beklenen %q", got, "instance-123")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"active","instanceId":"instance-123"}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewClient hata döndürdü: %v", err)
	}

	session, err := client.GetSession(context.Background(), "test-token", "instance-123")
	if err != nil {
		t.Fatalf("GetSession hata döndürdü: %v", err)
	}

	if session.Status != SessionActive {
		t.Fatalf("status = %q, beklenen %q", session.Status, SessionActive)
	}
}

func TestClientStartSessionUsesPostAndModelHeader(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, beklenen %s", r.Method, http.MethodPost)
		}

		if r.URL.Path != sessionEndpointPath {
			t.Fatalf("path = %s, beklenen %s", r.URL.Path, sessionEndpointPath)
		}

		if got := r.Header.Get(headerModel); got != "claude-sonnet" {
			t.Fatalf("x-freebuff-model = %q, beklenen %q", got, "claude-sonnet")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"queued","model":"claude-sonnet"}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewClient hata döndürdü: %v", err)
	}

	session, err := client.StartSession(context.Background(), "test-token", "instance-123", "claude-sonnet")
	if err != nil {
		t.Fatalf("StartSession hata döndürdü: %v", err)
	}

	if session.Status != SessionQueued {
		t.Fatalf("status = %q, beklenen %q", session.Status, SessionQueued)
	}
}

func TestClientEndSessionReturnsEndedStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("method = %s, beklenen %s", r.Method, http.MethodDelete)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ended"}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewClient hata döndürdü: %v", err)
	}

	session, err := client.EndSession(context.Background(), "test-token", "instance-123")
	if err != nil {
		t.Fatalf("EndSession hata döndürdü: %v", err)
	}

	if session.Status != SessionEnded {
		t.Fatalf("status = %q, beklenen %q", session.Status, SessionEnded)
	}
}

func TestClientMapsRateLimitResponseToAPIError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate_limited","message":"try again later"}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewClient hata döndürdü: %v", err)
	}

	_, err = client.GetSession(context.Background(), "test-token", "instance-123")
	if err == nil {
		t.Fatal("GetSession hata döndürmedi")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("hata tipi = %T, beklenen *APIError", err)
	}

	if apiErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("StatusCode = %d, beklenen %d", apiErr.StatusCode, http.StatusTooManyRequests)
	}

	if apiErr.Code != "rate_limited" {
		t.Fatalf("Code = %q, beklenen %q", apiErr.Code, "rate_limited")
	}

	if apiErr.Message != "try again later" {
		t.Fatalf("Message = %q, beklenen %q", apiErr.Message, "try again later")
	}
}

func TestClientReturnsDecodeErrorForBadSuccessJSON(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewClient hata döndürdü: %v", err)
	}

	_, err = client.GetSession(context.Background(), "test-token", "instance-123")
	if err == nil {
		t.Fatal("GetSession hata döndürmedi")
	}

	if !strings.Contains(err.Error(), "decode freebuff session response") {
		t.Fatalf("hata = %v, decode sarmalaması bekleniyordu", err)
	}
}
