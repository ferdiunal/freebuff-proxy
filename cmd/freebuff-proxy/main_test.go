package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Bu testler, CLI login/logout komutlarının OAuth akışını güvenli terminal çıktısı ve yerel credential dosyasıyla bağladığını doğrular.
//
// ## Kullanım örneği
//
// ```bash
// go test ./cmd/freebuff-proxy
// go test ./cmd/freebuff-proxy -run TestRunLogin
// ```
func TestRunLoginPrintsLoginURLAndDoesNotPrintAuthToken(t *testing.T) {
	credentialPath := filepath.Join(t.TempDir(), "credentials.json")
	var requestedFingerprint string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/auth/cli/code":
			if r.Header.Get("Authorization") != "" {
				t.Fatalf("login code Authorization header beklenen boş değerle eşleşmedi")
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("login code gövdesi çözümlenemedi: %v", err)
			}
			requestedFingerprint = body["fingerprintId"]
			_, _ = w.Write([]byte(`{"fingerprintHash":"hash_cli","loginUrl":"https://example.test/login?auth_code=visible-code","expiresAt":"1893456000000"}`))
		case "/api/auth/cli/status":
			if r.Header.Get("Authorization") != "" {
				t.Fatalf("login status Authorization header beklenen boş değerle eşleşmedi")
			}
			if r.URL.Query().Get("fingerprintId") != requestedFingerprint || r.URL.Query().Get("fingerprintHash") != "hash_cli" {
				t.Fatalf("login status query beklenen değerlerle eşleşmedi")
			}
			_, _ = w.Write([]byte(`{"user":{"id":"user_cli","name":"CLI User","email":"cli@example.com","authToken":"secret_cli_token","fingerprintId":"fp_cli","fingerprintHash":"hash_cli"}}`))
		default:
			t.Fatalf("beklenmeyen path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv("FREEBUFF_API_BASE_URL", server.URL)
	t.Setenv("FREEBUFF_CREDENTIALS_PATH", credentialPath)

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"login"}); err != nil {
			t.Fatalf("run login hata döndürdü: %v", err)
		}
	})

	if !strings.Contains(output, "https://example.test/login?auth_code=visible-code") {
		t.Fatalf("login çıktısı doğrulama URL'ini içermiyor")
	}
	if strings.Contains(output, "secret_cli_token") || strings.Contains(output, "hash_cli") {
		t.Fatalf("login çıktısı hassas değer içeriyor")
	}

	data, err := os.ReadFile(credentialPath)
	if err != nil {
		t.Fatalf("credential dosyası okunamadı: %v", err)
	}
	if !strings.Contains(string(data), "secret_cli_token") {
		t.Fatalf("credential dosyası auth token içermiyor")
	}
}

func TestRunLogoutCallsAPIAndClearsCredentialFile(t *testing.T) {
	credentialPath := filepath.Join(t.TempDir(), "credentials.json")
	credentialJSON := `{"default":{"id":"user_cli","name":"CLI User","email":"cli@example.com","authToken":"secret_cli_token","fingerprintId":"fp_cli","fingerprintHash":"hash_cli"}}`
	if err := os.WriteFile(credentialPath, []byte(credentialJSON), 0o600); err != nil {
		t.Fatalf("credential fixture yazılamadı: %v", err)
	}

	logoutCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/cli/logout" {
			t.Fatalf("beklenmeyen path: %s", r.URL.Path)
		}
		logoutCalled = true
		if r.Header.Get("Authorization") != "Bearer secret_cli_token" {
			t.Fatalf("Authorization header beklenen bearer token ile eşleşmedi")
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("logout gövdesi çözümlenemedi: %v", err)
		}
		if body["userId"] != "user_cli" || body["fingerprintId"] != "fp_cli" || body["fingerprintHash"] != "hash_cli" {
			t.Fatalf("logout gövdesi beklenen değerlerle eşleşmedi")
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	t.Setenv("FREEBUFF_API_BASE_URL", server.URL)
	t.Setenv("FREEBUFF_CREDENTIALS_PATH", credentialPath)

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"logout"}); err != nil {
			t.Fatalf("run logout hata döndürdü: %v", err)
		}
	})

	if !logoutCalled {
		t.Fatalf("logout endpoint çağrılmadı")
	}
	if strings.Contains(output, "secret_cli_token") || strings.Contains(output, "hash_cli") {
		t.Fatalf("logout çıktısı hassas değer içeriyor")
	}
	if _, err := os.Stat(credentialPath); !os.IsNotExist(err) {
		t.Fatalf("credential dosyası temizlenmedi, stat hatası = %v", err)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe oluşturulamadı: %v", err)
	}
	os.Stdout = writer
	defer func() {
		os.Stdout = original
	}()

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("stdout pipe kapatılamadı: %v", err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("stdout okunamadı: %v", err)
	}
	return string(data)
}
