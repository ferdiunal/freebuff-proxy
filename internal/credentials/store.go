package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrMissingToken = errors.New("freebuff auth token not found")

// Credential, Manicode kimlik dosyasındaki varsayılan hesabı temsil eder.
//
// ## Kullanım örneği
//
// ```go
// store := credentials.FileStore{Path: "/tmp/credentials.json"}
// cred, err := store.Load(context.Background())
//
//	if err != nil {
//		return err
//	}
//
// fmt.Println(cred.Email)
// ```
type Credential struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Email           string `json:"email"`
	AuthToken       string `json:"authToken"`
	FingerprintID   string `json:"fingerprintId"`
	FingerprintHash string `json:"fingerprintHash"`
}

// Store, kimlik bilgilerinin yüklenip kaydedilebildiği depoyu tanımlar.
type Store interface {
	Load(ctx context.Context) (Credential, error)
	Save(ctx context.Context, cred Credential) error
}

// FileStore, Manicode kimlik bilgisini JSON dosyası üzerinden saklar.
type FileStore struct {
	Path string
}

type filePayload struct {
	Default Credential `json:"default"`
}

// Load, dosyadaki varsayılan kimlik bilgisini okur.
func (s FileStore) Load(ctx context.Context) (Credential, error) {
	if err := ctx.Err(); err != nil {
		return Credential{}, err
	}

	data, err := os.ReadFile(s.Path)
	if err != nil {
		return Credential{}, fmt.Errorf("read credentials file: %w", err)
	}

	var payload filePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return Credential{}, fmt.Errorf("decode credentials file: %w", err)
	}

	if payload.Default.AuthToken == "" {
		return Credential{}, ErrMissingToken
	}

	return payload.Default, nil
}

// Save, varsayılan kimlik bilgisini Manicode dosya biçiminde kaydeder.
func (s FileStore) Save(ctx context.Context, cred Credential) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if cred.AuthToken == "" {
		return ErrMissingToken
	}

	dir := filepath.Dir(s.Path)
	if info, err := os.Stat(dir); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("credentials directory path is not a directory: %s", dir)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create credentials directory: %w", err)
		}
	} else {
		return fmt.Errorf("stat credentials directory: %w", err)
	}

	data, err := json.MarshalIndent(filePayload{Default: cred}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode credentials file: %w", err)
	}

	if err := writeFileAtomic(s.Path, data); err != nil {
		return err
	}

	return nil
}

// Clear, başarılı logout sonrası yerel kimlik dosyasını güvenli şekilde kaldırır.
func (s FileStore) Clear(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := os.Remove(s.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove credentials file: %w", err)
	}

	return nil
}

// writeFileAtomic, kimlik JSON'unu aynı dizindeki geçici dosya üzerinden atomik değiştirir.
func writeFileAtomic(path string, data []byte) (err error) {
	tempFile, err := os.CreateTemp(filepath.Dir(path), ".credentials-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp credentials file: %w", err)
	}

	tempPath := tempFile.Name()
	cleanupTemp := true
	defer func() {
		if !cleanupTemp {
			return
		}

		if removeErr := os.Remove(tempPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			cleanupErr := fmt.Errorf("remove temp credentials file: %w", removeErr)
			if err != nil {
				err = errors.Join(err, cleanupErr)
				return
			}

			err = cleanupErr
		}
	}()

	if err := tempFile.Chmod(0o600); err != nil {
		if closeErr := closeTempFile(tempFile); closeErr != nil {
			return errors.Join(fmt.Errorf("chmod temp credentials file: %w", err), closeErr)
		}

		return fmt.Errorf("chmod temp credentials file: %w", err)
	}

	if _, err := tempFile.Write(data); err != nil {
		if closeErr := closeTempFile(tempFile); closeErr != nil {
			return errors.Join(fmt.Errorf("write temp credentials file: %w", err), closeErr)
		}

		return fmt.Errorf("write temp credentials file: %w", err)
	}

	if err := tempFile.Sync(); err != nil {
		if closeErr := closeTempFile(tempFile); closeErr != nil {
			return errors.Join(fmt.Errorf("sync temp credentials file: %w", err), closeErr)
		}

		return fmt.Errorf("sync temp credentials file: %w", err)
	}

	if err := closeTempFile(tempFile); err != nil {
		return err
	}

	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace credentials file: %w", err)
	}
	cleanupTemp = false

	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod credentials file: %w", err)
	}

	return nil
}

func closeTempFile(file *os.File) error {
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temp credentials file: %w", err)
	}

	return nil
}
