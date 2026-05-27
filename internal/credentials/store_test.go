package credentials

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Bu testler, Manicode kimlik dosyasının beklenen JSON biçimi ve izinlerle işlendiğini doğrular.
//
// ## Kullanım örneği
//
// ```bash
// go test ./internal/credentials
// go test ./internal/credentials -run TestFileStore
// ```
func TestFileStoreLoadReadsCredential(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "credentials.json")
	content := []byte(`{
  "default": {
    "id": "user_123",
    "name": "Ada Lovelace",
    "email": "ada@example.com",
    "authToken": "42d7350000000000000000000000a223",
    "fingerprintId": "fp_123",
    "fingerprintHash": "hash_456"
  }
}`)

	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("fixture yazılamadı: %v", err)
	}

	store := FileStore{Path: path}
	cred, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load hata döndürdü: %v", err)
	}

	if cred.ID != "user_123" {
		t.Fatalf("ID = %q, beklenen %q", cred.ID, "user_123")
	}

	if cred.Name != "Ada Lovelace" {
		t.Fatalf("Name = %q, beklenen %q", cred.Name, "Ada Lovelace")
	}

	if cred.Email != "ada@example.com" {
		t.Fatalf("Email = %q, beklenen %q", cred.Email, "ada@example.com")
	}

	if cred.AuthToken != "42d7350000000000000000000000a223" {
		t.Fatalf("AuthToken beklenen değerle eşleşmedi")
	}

	if cred.FingerprintID != "fp_123" {
		t.Fatalf("FingerprintID = %q, beklenen %q", cred.FingerprintID, "fp_123")
	}

	if cred.FingerprintHash != "hash_456" {
		t.Fatalf("FingerprintHash beklenen değerle eşleşmedi")
	}
}

func TestFileStoreLoadReturnsMissingToken(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		content string
	}{
		{
			name: "missing authToken field",
			content: `{
  "default": {
    "id": "user_123"
  }
}`,
		},
		{
			name: "empty authToken",
			content: `{
  "default": {
    "authToken": ""
  }
}`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			path := filepath.Join(tempDir, "credentials.json")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatalf("fixture yazılamadı: %v", err)
			}

			store := FileStore{Path: path}
			_, err := store.Load(context.Background())
			if !errors.Is(err, ErrMissingToken) {
				t.Fatalf("Load hata = %v, beklenen ErrMissingToken", err)
			}
		})
	}
}

func TestFileStoreSaveWritesCredentialFileWithPermissions(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	parentDir := filepath.Join(rootDir, "manicode")
	path := filepath.Join(parentDir, "credentials.json")
	store := FileStore{Path: path}
	cred := Credential{
		ID:              "user_123",
		Name:            "Ada Lovelace",
		Email:           "ada@example.com",
		AuthToken:       "42d7350000000000000000000000a223",
		FingerprintID:   "fp_123",
		FingerprintHash: "hash_456",
	}

	if err := store.Save(context.Background(), cred); err != nil {
		t.Fatalf("Save hata döndürdü: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("yazılan dosya okunamadı: %v", err)
	}

	if info.Mode().Perm() != 0o600 {
		t.Fatalf("dosya izni = %o, beklenen %o", info.Mode().Perm(), 0o600)
	}

	dirInfo, err := os.Stat(parentDir)
	if err != nil {
		t.Fatalf("üst dizin okunamadı: %v", err)
	}

	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("dizin izni = %o, beklenen %o", dirInfo.Mode().Perm(), 0o700)
	}

	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("yazılan veri tekrar okunamadı: %v", err)
	}

	if loaded != cred {
		t.Fatalf("yüklenen kayıt beklenen credential ile eşleşmedi")
	}
}

func TestFileStoreSavePreservesExistingDirectoryPermissionsAndCleansTempFiles(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	parentDir := filepath.Join(rootDir, "manicode")
	path := filepath.Join(parentDir, "credentials.json")
	if err := os.Mkdir(parentDir, 0o755); err != nil {
		t.Fatalf("üst dizin oluşturulamadı: %v", err)
	}
	if err := os.Chmod(parentDir, 0o755); err != nil {
		t.Fatalf("üst dizin izni hazırlanamadı: %v", err)
	}

	if err := os.WriteFile(path, []byte(`{"default":{"authToken":"old-token"}}`), 0o644); err != nil {
		t.Fatalf("mevcut dosya yazılamadı: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("mevcut dosya izni hazırlanamadı: %v", err)
	}

	store := FileStore{Path: path}
	cred := Credential{
		ID:              "user_123",
		Name:            "Ada Lovelace",
		Email:           "ada@example.com",
		AuthToken:       "42d7350000000000000000000000a223",
		FingerprintID:   "fp_123",
		FingerprintHash: "hash_456",
	}

	if err := store.Save(context.Background(), cred); err != nil {
		t.Fatalf("Save hata döndürdü: %v", err)
	}

	dirInfo, err := os.Stat(parentDir)
	if err != nil {
		t.Fatalf("üst dizin okunamadı: %v", err)
	}
	if dirInfo.Mode().Perm() != 0o755 {
		t.Fatalf("dizin izni = %o, beklenen %o", dirInfo.Mode().Perm(), 0o755)
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("kimlik dosyası okunamadı: %v", err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("dosya izni = %o, beklenen %o", fileInfo.Mode().Perm(), 0o600)
	}

	entries, err := os.ReadDir(parentDir)
	if err != nil {
		t.Fatalf("üst dizin listelenemedi: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != "credentials.json" {
			t.Fatalf("geçici dosya temizlenmedi: %s", entry.Name())
		}
	}
}

func TestFileStoreSaveReturnsMissingToken(t *testing.T) {
	t.Parallel()

	store := FileStore{Path: filepath.Join(t.TempDir(), "credentials.json")}
	err := store.Save(context.Background(), Credential{})
	if !errors.Is(err, ErrMissingToken) {
		t.Fatalf("Save hata = %v, beklenen ErrMissingToken", err)
	}
}

func TestFileStoreClearRemovesCredentialFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "credentials.json")
	store := FileStore{Path: path}
	cred := Credential{
		ID:              "user_123",
		Name:            "Ada Lovelace",
		Email:           "ada@example.com",
		AuthToken:       "42d7350000000000000000000000a223",
		FingerprintID:   "fp_123",
		FingerprintHash: "hash_456",
	}

	if err := store.Save(context.Background(), cred); err != nil {
		t.Fatalf("Save hata döndürdü: %v", err)
	}
	if err := store.Clear(context.Background()); err != nil {
		t.Fatalf("Clear hata döndürdü: %v", err)
	}

	_, err := os.Stat(path)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("credential dosyası temizlenmedi, stat hatası = %v", err)
	}
}

func TestFileStoreClearReturnsContextErrorWhenCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	store := FileStore{Path: filepath.Join(t.TempDir(), "credentials.json")}
	err := store.Clear(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Clear hata = %v, beklenen context.Canceled", err)
	}
}

func TestFileStoreLoadReturnsContextErrorWhenCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	store := FileStore{Path: filepath.Join(t.TempDir(), "credentials.json")}
	_, err := store.Load(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Load hata = %v, beklenen context.Canceled", err)
	}
}

func TestFileStoreSaveReturnsContextErrorWhenCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	store := FileStore{Path: filepath.Join(t.TempDir(), "credentials.json")}
	err := store.Save(ctx, Credential{AuthToken: "42d7350000000000000000000000a223"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Save hata = %v, beklenen context.Canceled", err)
	}
}
