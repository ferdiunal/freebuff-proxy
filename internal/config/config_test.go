package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var freebuffEnvKeys = []string{
	"FREEBUFF_PROXY_ADDR",
	"FREEBUFF_API_BASE_URL",
	"FREEBUFF_MODEL",
	"FREEBUFF_PROXY_API_KEY",
	"FREEBUFF_CREDENTIALS_PATH",
}

// Bu testler, yapılandırma yükleme davranışının varsayılan, .env ve override akışlarını doğrular.
//
// ## Kullanım örneği
//
// ```bash
// go test ./internal/config
// go test ./internal/config -run TestLoadEnvOverrides
// ```
func TestLoadDefaults(t *testing.T) {
	clearFreebuffEnv(t)
	t.Chdir(t.TempDir())

	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home dir alınamadı: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load hata döndürdü: %v", err)
	}

	if cfg.Addr != "127.0.0.1:1455" {
		t.Fatalf("Addr = %q, beklenen %q", cfg.Addr, "127.0.0.1:1455")
	}

	if cfg.APIBaseURL != "https://www.codebuff.com" {
		t.Fatalf("APIBaseURL = %q, beklenen %q", cfg.APIBaseURL, "https://www.codebuff.com")
	}

	if cfg.Model != "deepseek/deepseek-v4-pro" {
		t.Fatalf("Model = %q, beklenen %q", cfg.Model, "deepseek/deepseek-v4-pro")
	}

	if cfg.ProxyAPIKey != "" {
		t.Fatalf("ProxyAPIKey beklenen boş değerle eşleşmedi")
	}

	expectedCredentialsPath := filepath.Join(homeDir, ".config", "manicode", "credentials.json")
	if cfg.CredentialsPath == "" {
		t.Fatal("CredentialsPath boş olmamalı")
	}

	if cfg.CredentialsPath != expectedCredentialsPath {
		t.Fatalf("CredentialsPath = %q, beklenen %q", cfg.CredentialsPath, expectedCredentialsPath)
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	clearFreebuffEnv(t)
	stubLoadDotenv(t, nil)

	t.Setenv("FREEBUFF_PROXY_ADDR", "0.0.0.0:9999")
	t.Setenv("FREEBUFF_API_BASE_URL", "https://example.com")
	t.Setenv("FREEBUFF_MODEL", "custom-model")
	t.Setenv("FREEBUFF_PROXY_API_KEY", "test-api-key")
	t.Setenv("FREEBUFF_CREDENTIALS_PATH", "/tmp/custom-credentials.json")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load hata döndürdü: %v", err)
	}

	if cfg.Addr != "0.0.0.0:9999" {
		t.Fatalf("Addr = %q, beklenen %q", cfg.Addr, "0.0.0.0:9999")
	}

	if cfg.APIBaseURL != "https://example.com" {
		t.Fatalf("APIBaseURL = %q, beklenen %q", cfg.APIBaseURL, "https://example.com")
	}

	if cfg.Model != "custom-model" {
		t.Fatalf("Model = %q, beklenen %q", cfg.Model, "custom-model")
	}

	if cfg.ProxyAPIKey != "test-api-key" {
		t.Fatalf("ProxyAPIKey beklenen env override değeriyle eşleşmedi")
	}

	if cfg.CredentialsPath != "/tmp/custom-credentials.json" {
		t.Fatalf("CredentialsPath = %q, beklenen %q", cfg.CredentialsPath, "/tmp/custom-credentials.json")
	}
}

func TestLoadNormalizesBareCodebuffAPIBaseURL(t *testing.T) {
	clearFreebuffEnv(t)
	stubLoadDotenv(t, nil)

	t.Setenv("FREEBUFF_API_BASE_URL", "https://codebuff.com/")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load hata döndürdü: %v", err)
	}

	if cfg.APIBaseURL != "https://www.codebuff.com" {
		t.Fatalf("APIBaseURL = %q, beklenen %q", cfg.APIBaseURL, "https://www.codebuff.com")
	}
}

func TestLoadReadsDotenvFile(t *testing.T) {
	clearFreebuffEnv(t)

	tempDir := t.TempDir()
	t.Chdir(tempDir)

	expectedProxyAPIKey := "dotenv-secret"
	writeDotenv(t, tempDir, strings.Join([]string{
		"FREEBUFF_PROXY_ADDR=127.0.0.1:7777",
		"FREEBUFF_API_BASE_URL=https://dotenv.example.com",
		"FREEBUFF_MODEL=dotenv-model",
		"FREEBUFF_PROXY_API_KEY=" + expectedProxyAPIKey,
		"FREEBUFF_CREDENTIALS_PATH=/tmp/dotenv-credentials.json",
	}, "\n"))

	calls := 0
	originalUserHomeDir := userHomeDir
	userHomeDir = func() (string, error) {
		calls++
		return "", errors.New("userHomeDir çağrılmamalı")
	}
	t.Cleanup(func() {
		userHomeDir = originalUserHomeDir
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load hata döndürdü: %v", err)
	}

	if cfg.Addr != "127.0.0.1:7777" {
		t.Fatalf("Addr = %q, beklenen %q", cfg.Addr, "127.0.0.1:7777")
	}

	if cfg.APIBaseURL != "https://dotenv.example.com" {
		t.Fatalf("APIBaseURL = %q, beklenen %q", cfg.APIBaseURL, "https://dotenv.example.com")
	}

	if cfg.Model != "dotenv-model" {
		t.Fatalf("Model = %q, beklenen %q", cfg.Model, "dotenv-model")
	}

	if cfg.ProxyAPIKey != expectedProxyAPIKey {
		t.Fatal("ProxyAPIKey .env değerinden yüklenmedi")
	}

	if cfg.CredentialsPath != "/tmp/dotenv-credentials.json" {
		t.Fatalf("CredentialsPath = %q, beklenen %q", cfg.CredentialsPath, "/tmp/dotenv-credentials.json")
	}

	if calls != 0 {
		t.Fatalf("userHomeDir %d kez çağrıldı, beklenen 0", calls)
	}
}

func TestLoadProcessEnvPrecedenceOverDotenv(t *testing.T) {
	clearFreebuffEnv(t)

	tempDir := t.TempDir()
	t.Chdir(tempDir)
	writeDotenv(t, tempDir, strings.Join([]string{
		"FREEBUFF_PROXY_ADDR=127.0.0.1:7777",
		"FREEBUFF_API_BASE_URL=https://dotenv.example.com",
		"FREEBUFF_MODEL=dotenv-model",
		"FREEBUFF_PROXY_API_KEY=dotenv-secret",
		"FREEBUFF_CREDENTIALS_PATH=/tmp/dotenv-credentials.json",
	}, "\n"))

	t.Setenv("FREEBUFF_PROXY_ADDR", "0.0.0.0:9999")
	t.Setenv("FREEBUFF_API_BASE_URL", "https://process.example.com")
	t.Setenv("FREEBUFF_MODEL", "process-model")
	t.Setenv("FREEBUFF_PROXY_API_KEY", "process-secret")
	t.Setenv("FREEBUFF_CREDENTIALS_PATH", "/tmp/process-credentials.json")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load hata döndürdü: %v", err)
	}

	if cfg.Addr != "0.0.0.0:9999" {
		t.Fatalf("Addr = %q, beklenen %q", cfg.Addr, "0.0.0.0:9999")
	}

	if cfg.APIBaseURL != "https://process.example.com" {
		t.Fatalf("APIBaseURL = %q, beklenen %q", cfg.APIBaseURL, "https://process.example.com")
	}

	if cfg.Model != "process-model" {
		t.Fatalf("Model = %q, beklenen %q", cfg.Model, "process-model")
	}

	if cfg.ProxyAPIKey != "process-secret" {
		t.Fatal("ProxyAPIKey process env önceliğini korumadı")
	}

	if cfg.CredentialsPath != "/tmp/process-credentials.json" {
		t.Fatalf("CredentialsPath = %q, beklenen %q", cfg.CredentialsPath, "/tmp/process-credentials.json")
	}
}

func TestLoadReturnsSanitizedDotenvErrors(t *testing.T) {
	clearFreebuffEnv(t)

	secret := "secret-dotenv-api-key"
	stubLoadDotenv(t, errors.New("parse FREEBUFF_PROXY_API_KEY="+secret))

	_, err := Load()
	if err == nil {
		t.Fatal("Load hata döndürmedi")
	}

	if !errors.Is(err, errDotenvLoad) {
		t.Fatalf("Load hatası beklenen güvenli dotenv hatasını sarmalamadı: %v", err)
	}

	if !strings.Contains(err.Error(), "load dotenv: ") {
		t.Fatalf("Load hatası beklenen bağlamı içermiyor: %v", err)
	}

	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "FREEBUFF_PROXY_API_KEY=") {
		t.Fatal("Load hatası hassas dotenv içeriği sızdırıyor")
	}
}

func TestLoadCredentialsOverrideWithoutHome(t *testing.T) {
	clearFreebuffEnv(t)
	stubLoadDotenv(t, nil)

	t.Setenv("FREEBUFF_CREDENTIALS_PATH", "/tmp/override-credentials.json")

	calls := 0
	originalUserHomeDir := userHomeDir
	userHomeDir = func() (string, error) {
		calls++
		return "", errors.New("userHomeDir çağrılmamalı")
	}
	defer func() {
		userHomeDir = originalUserHomeDir
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load hata döndürdü: %v", err)
	}

	if cfg.CredentialsPath != "/tmp/override-credentials.json" {
		t.Fatalf("CredentialsPath = %q, beklenen %q", cfg.CredentialsPath, "/tmp/override-credentials.json")
	}

	if calls != 0 {
		t.Fatalf("userHomeDir %d kez çağrıldı, beklenen 0", calls)
	}
}

func TestLoadDefaultCredentialsUsesUserHomeDir(t *testing.T) {
	clearFreebuffEnv(t)
	stubLoadDotenv(t, nil)

	calls := 0
	originalUserHomeDir := userHomeDir
	userHomeDir = func() (string, error) {
		calls++
		return "/tmp/freebuff-home", nil
	}
	defer func() {
		userHomeDir = originalUserHomeDir
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load hata döndürdü: %v", err)
	}

	expectedCredentialsPath := filepath.Join("/tmp/freebuff-home", ".config", "manicode", "credentials.json")
	if cfg.CredentialsPath != expectedCredentialsPath {
		t.Fatalf("CredentialsPath = %q, beklenen %q", cfg.CredentialsPath, expectedCredentialsPath)
	}

	if calls != 1 {
		t.Fatalf("userHomeDir %d kez çağrıldı, beklenen 1", calls)
	}
}

// clearFreebuffEnv, testin .env ve process env ayrımını temiz bir başlangıçla doğrulaması için FREEBUFF_* değerlerini kaldırır.
//
// ## Kullanım örneği
//
// ```go
// clearFreebuffEnv(t)
// t.Setenv("FREEBUFF_MODEL", "test-model")
// ```
func clearFreebuffEnv(t *testing.T) {
	t.Helper()

	originalValues := make(map[string]string, len(freebuffEnvKeys))
	originalPresence := make(map[string]bool, len(freebuffEnvKeys))
	for _, key := range freebuffEnvKeys {
		value, ok := os.LookupEnv(key)
		originalValues[key] = value
		originalPresence[key] = ok
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("%s env temizlenemedi: %v", key, err)
		}
	}

	t.Cleanup(func() {
		for _, key := range freebuffEnvKeys {
			var err error
			if originalPresence[key] {
				err = os.Setenv(key, originalValues[key])
			} else {
				err = os.Unsetenv(key)
			}
			if err != nil {
				t.Fatalf("%s env geri yüklenemedi: %v", key, err)
			}
		}
	})
}

// stubLoadDotenv, dotenv yükleyicisinin test içinde kontrollü sonuç üretmesini sağlar.
//
// ## Kullanım örneği
//
// ```go
// stubLoadDotenv(t, nil)
// stubLoadDotenv(t, errors.New("parse"))
// ```
func stubLoadDotenv(t *testing.T, err error) {
	t.Helper()

	originalLoadDotenv := loadDotenv
	loadDotenv = func(...string) error {
		return err
	}
	t.Cleanup(func() {
		loadDotenv = originalLoadDotenv
	})
}

// writeDotenv, geçici test dizinine .env içeriği yazar.
//
// ## Kullanım örneği
//
// ```go
// writeDotenv(t, dir, "FREEBUFF_MODEL=test-model")
// ```
func writeDotenv(t *testing.T, dir string, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(content+"\n"), 0o600); err != nil {
		t.Fatalf(".env yazılamadı: %v", err)
	}
}
