package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
)

var (
	userHomeDir = os.UserHomeDir
	loadDotenv  = godotenv.Load

	errDotenvLoad = errors.New("dotenv dosyası yüklenemedi")
)

// Config, proxy çalışması için gereken temel yapılandırmayı taşır.
//
// ## Kullanım örneği
//
// ```go
// cfg, err := config.Load()
//
//	if err != nil {
//		return err
//	}
//
// fmt.Println(cfg.Addr)
// fmt.Println(cfg.CredentialsPath)
// ```
type Config struct {
	Addr            string
	APIBaseURL      string
	Model           string
	ProxyAPIKey     string
	CredentialsPath string
}

// Load, .env dosyasını, varsayılan değerleri ve ortam değişkeni geçersiz kılmalarını yükler.
func Load() (Config, error) {
	if err := loadDotenv(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("load dotenv: %w", errDotenvLoad)
	}

	credentialsPath := os.Getenv("FREEBUFF_CREDENTIALS_PATH")
	if credentialsPath == "" {
		homeDir, err := userHomeDir()
		if err != nil {
			return Config{}, err
		}

		credentialsPath = filepath.Join(homeDir, ".config", "manicode", "credentials.json")
	}

	return Config{
		Addr:            envOr("FREEBUFF_PROXY_ADDR", "127.0.0.1:1455"),
		APIBaseURL:      normalizeAPIBaseURL(envOr("FREEBUFF_API_BASE_URL", "https://www.codebuff.com")),
		Model:           envOr("FREEBUFF_MODEL", "deepseek/deepseek-v4-pro"),
		ProxyAPIKey:     envOr("FREEBUFF_PROXY_API_KEY", ""),
		CredentialsPath: credentialsPath,
	}, nil
}

// envOr, boş olmayan ortam değişkeni değerini; yoksa varsayılanı döndürür.
func envOr(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	return value
}

func normalizeAPIBaseURL(value string) string {
	if strings.TrimRight(value, "/") == "https://codebuff.com" {
		return "https://www.codebuff.com"
	}

	return value
}
