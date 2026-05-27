package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ferdiunal/freebuff-proxy/internal/credentials"
)

// Bu testler, doğrulanmış Codebuff CLI OAuth endpoint biçimlerine göre Freebuff login/logout akışını doğrular.
//
// ## Kullanım örneği
//
// ```bash
// go test ./internal/oauth
// go test ./internal/oauth -run TestFlowLogin
// ```
func TestFlowRequestLoginCodeDoesNotSendAuthorizationHeader(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/cli/code" {
			t.Fatalf("path = %q, beklenen /api/auth/cli/code", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, beklenen POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization header beklenen boş değerle eşleşmedi")
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("istek gövdesi çözümlenemedi: %v", err)
		}
		if body["fingerprintId"] != "fp_test" {
			t.Fatalf("fingerprintId beklenen değerle eşleşmedi")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"fingerprintId":"fp_test","fingerprintHash":"hash_test","loginUrl":"https://example.test/login","expiresAt":1893456000000}`))
	}))
	defer server.Close()

	flow := Flow{
		BaseURL:       server.URL,
		HTTPClient:    server.Client(),
		Store:         &memoryStore{},
		FingerprintID: func() (string, error) { return "fp_test", nil },
		PollInterval:  time.Millisecond,
		PollTimeout:   time.Second,
	}

	code, err := flow.RequestLoginCode(context.Background())
	if err != nil {
		t.Fatalf("RequestLoginCode hata döndürdü: %v", err)
	}

	if code.LoginURL != "https://example.test/login" {
		t.Fatalf("LoginURL = %q, beklenen doğrulama URL'i", code.LoginURL)
	}
	if code.ExpiresAt != "1893456000000" {
		t.Fatalf("ExpiresAt = %q, beklenen 1893456000000", code.ExpiresAt)
	}
}

func TestFlowLoginStopsPollingOnSuccessAndSavesCredential(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	statusCalls := 0
	store := &memoryStore{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/auth/cli/code":
			_, _ = w.Write([]byte(`{"fingerprintId":"fp_success","fingerprintHash":"hash_success","loginUrl":"https://example.test/login","expiresAt":"1893456000000"}`))
		case "/api/auth/cli/status":
			mu.Lock()
			statusCalls++
			call := statusCalls
			mu.Unlock()

			if r.Header.Get("Authorization") != "" {
				t.Fatalf("status Authorization header beklenen boş değerle eşleşmedi")
			}
			query := r.URL.Query()
			if query.Get("fingerprintId") != "fp_success" || query.Get("fingerprintHash") != "hash_success" || query.Get("expiresAt") != "1893456000000" {
				t.Fatalf("status query beklenen fingerprintId/fingerprintHash/expiresAt değerleriyle eşleşmedi")
			}
			if call == 1 {
				http.Error(w, `{"error":"Authentication failed"}`, http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"user":{"id":"user_123","name":"Ada Lovelace","email":"ada@example.com","authToken":"token_secret","fingerprintId":"fp_success","fingerprintHash":"hash_success"},"message":"Authentication successful!"}`))
		default:
			t.Fatalf("beklenmeyen path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	flow := Flow{
		BaseURL:       server.URL,
		HTTPClient:    server.Client(),
		Store:         store,
		FingerprintID: func() (string, error) { return "fp_success", nil },
		PollInterval:  time.Millisecond,
		PollTimeout:   time.Second,
		Sleep:         func(context.Context, time.Duration) error { return nil },
	}

	cred, err := flow.Login(context.Background())
	if err != nil {
		t.Fatalf("Login hata döndürdü: %v", err)
	}

	mu.Lock()
	calls := statusCalls
	mu.Unlock()
	if calls != 2 {
		t.Fatalf("status çağrı sayısı = %d, beklenen 2", calls)
	}
	if cred.AuthToken != "token_secret" {
		t.Fatalf("AuthToken kaydedilmeden döndü")
	}
	if store.saved.AuthToken != "token_secret" || store.saved.FingerprintID != "fp_success" || store.saved.FingerprintHash != "hash_success" {
		t.Fatalf("kaydedilen credential beklenen değerlerle eşleşmedi")
	}
}

func TestFlowLoginStopsPollingWhenContextIsCanceled(t *testing.T) {
	t.Parallel()

	statusCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/auth/cli/code":
			_, _ = w.Write([]byte(`{"fingerprintHash":"hash_cancel","loginUrl":"https://example.test/login","expiresAt":"1893456000000"}`))
		case "/api/auth/cli/status":
			statusCalls++
			http.Error(w, `{"error":"Authentication failed"}`, http.StatusUnauthorized)
		default:
			t.Fatalf("beklenmeyen path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	flow := Flow{
		BaseURL:       server.URL,
		HTTPClient:    server.Client(),
		Store:         &memoryStore{},
		FingerprintID: func() (string, error) { return "fp_cancel", nil },
		PollInterval:  time.Millisecond,
		PollTimeout:   time.Second,
		Sleep: func(context.Context, time.Duration) error {
			cancel()
			return context.Canceled
		},
	}

	_, err := flow.Login(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Login hata = %v, beklenen context.Canceled", err)
	}
	if statusCalls != 1 {
		t.Fatalf("status çağrı sayısı = %d, beklenen 1", statusCalls)
	}
}

func TestFlowPollLoginStatusTransportErrorDoesNotLeakQuery(t *testing.T) {
	t.Parallel()

	secretHash := "hash_secret_transport"
	flow := Flow{
		BaseURL: "https://example.test",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("dial " + req.URL.String())
		})},
		Store:        &memoryStore{},
		PollInterval: time.Millisecond,
		PollTimeout:  time.Second,
	}

	_, err := flow.PollLoginStatus(context.Background(), LoginCode{
		FingerprintID:   "fp_transport",
		FingerprintHash: secretHash,
		ExpiresAt:       "1893456000000",
	})
	if err == nil {
		t.Fatalf("PollLoginStatus hata döndürmedi")
	}
	if strings.Contains(err.Error(), secretHash) || strings.Contains(err.Error(), "fingerprintHash=") || strings.Contains(err.Error(), "expiresAt=") {
		t.Fatalf("transport hatası hassas query içeriyor")
	}
}

func TestFlowPollLoginStatusCancelsBlockedRequestAtPollTimeout(t *testing.T) {
	t.Parallel()

	flow := Flow{
		BaseURL: "https://example.test",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			<-req.Context().Done()
			return nil, req.Context().Err()
		})},
		Store:        &memoryStore{},
		PollInterval: time.Hour,
		PollTimeout:  20 * time.Millisecond,
	}

	_, err := flow.PollLoginStatus(context.Background(), LoginCode{
		FingerprintID:   "fp_timeout",
		FingerprintHash: "hash_timeout",
		ExpiresAt:       "1893456000000",
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("PollLoginStatus hata = %v, beklenen context deadline exceeded", err)
	}
}

func TestFlowPollLoginStatusReturnsTerminalUnauthorizedError(t *testing.T) {
	t.Parallel()

	statusCalls := 0
	rawUpstreamError := "expired fingerprintHash hash_secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		statusCalls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"` + rawUpstreamError + `"}`))
	}))
	defer server.Close()

	flow := Flow{
		BaseURL:      server.URL,
		HTTPClient:   server.Client(),
		Store:        &memoryStore{},
		PollInterval: time.Millisecond,
		PollTimeout:  time.Second,
		Sleep:        func(context.Context, time.Duration) error { return nil },
	}

	_, err := flow.PollLoginStatus(context.Background(), LoginCode{
		FingerprintID:   "fp_expired",
		FingerprintHash: "hash_expired",
		ExpiresAt:       "1893456000000",
	})
	if err == nil {
		t.Fatalf("PollLoginStatus hata döndürmedi")
	}
	if err.Error() != "login status unauthorized" {
		t.Fatalf("PollLoginStatus hata beklenen generic terminal unauthorized hatasıyla eşleşmedi")
	}
	for _, leaked := range []string{rawUpstreamError, "expired", "fingerprintHash", "hash_secret"} {
		if strings.Contains(err.Error(), leaked) {
			t.Fatalf("PollLoginStatus hatası hassas upstream değer içeriyor")
		}
	}
	if statusCalls != 1 {
		t.Fatalf("status çağrı sayısı = %d, beklenen 1", statusCalls)
	}
}

func TestFlowEndpointAppendsPathBelowBaseAndDropsQueryFragment(t *testing.T) {
	t.Parallel()

	flow := Flow{BaseURL: "https://example.test/base?tenant=x#frag"}
	query := url.Values{}
	query.Set("q", "1")

	got, err := flow.endpoint("/api/auth/cli/code", query)
	if err != nil {
		t.Fatalf("endpoint hata döndürdü: %v", err)
	}

	want := "https://example.test/base/api/auth/cli/code?q=1"
	if got != want {
		t.Fatalf("endpoint = %q, beklenen %q", got, want)
	}
}

func TestFlowPollLoginStatusRequiresUserIDBeforeSaving(t *testing.T) {
	t.Parallel()

	store := &memoryStore{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"user":{"name":"Ada Lovelace","email":"ada@example.com","authToken":"token_secret","fingerprintId":"fp_missing_id","fingerprintHash":"hash_missing_id"}}`))
	}))
	defer server.Close()

	flow := Flow{
		BaseURL:      server.URL,
		HTTPClient:   server.Client(),
		Store:        store,
		PollInterval: time.Millisecond,
		PollTimeout:  time.Second,
	}

	_, err := flow.PollLoginStatus(context.Background(), LoginCode{
		FingerprintID:   "fp_missing_id",
		FingerprintHash: "hash_missing_id",
		ExpiresAt:       "1893456000000",
	})
	if err == nil || !strings.Contains(err.Error(), "user id") {
		t.Fatalf("PollLoginStatus hata beklenen eksik user id hatasıyla eşleşmedi")
	}
	if store.saveCount != 0 {
		t.Fatalf("Store.Save çağrı sayısı = %d, beklenen 0", store.saveCount)
	}
}

func TestFlowLogoutUsesStoredCredentialMetadataAndClearsLocalState(t *testing.T) {
	t.Parallel()

	store := &memoryStore{
		loaded: credentials.Credential{
			ID:              "user_123",
			Name:            "Ada Lovelace",
			Email:           "ada@example.com",
			AuthToken:       "token_secret",
			FingerprintID:   "fp_logout",
			FingerprintHash: "hash_logout",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/cli/logout" {
			t.Fatalf("path = %q, beklenen /api/auth/cli/logout", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, beklenen POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token_secret" {
			t.Fatalf("Authorization header beklenen bearer token ile eşleşmedi")
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("logout gövdesi çözümlenemedi: %v", err)
		}
		if body["authToken"] != "" {
			t.Fatalf("logout gövdesi authToken içeriyor")
		}
		if body["userId"] != "user_123" || body["fingerprintId"] != "fp_logout" || body["fingerprintHash"] != "hash_logout" {
			t.Fatalf("logout gövdesi beklenen değerlerle eşleşmedi")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	flow := Flow{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
		Store:      store,
	}

	if err := flow.Logout(context.Background()); err != nil {
		t.Fatalf("Logout hata döndürdü: %v", err)
	}
	if !store.cleared {
		t.Fatalf("logout başarılı olunca local credential temizlenmedi")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

type memoryStore struct {
	loaded    credentials.Credential
	saved     credentials.Credential
	saveCount int
	cleared   bool
}

func (m *memoryStore) Load(context.Context) (credentials.Credential, error) {
	return m.loaded, nil
}

func (m *memoryStore) Save(_ context.Context, cred credentials.Credential) error {
	m.saveCount++
	m.saved = cred
	m.loaded = cred
	return nil
}

func (m *memoryStore) Clear(context.Context) error {
	m.cleared = true
	m.loaded = credentials.Credential{}
	return nil
}
