package oauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/ferdiunal/freebuff-proxy/internal/credentials"
)

const (
	loginCodePath       = "/api/auth/cli/code"
	loginStatusPath     = "/api/auth/cli/status"
	logoutPath          = "/api/auth/cli/logout"
	pendingStatusError  = "Authentication failed"
	oauthTransportError = "oauth transport error"
)

// Flow, doğrulanmış CLI OAuth endpointleri üzerinden login/logout işlemlerini yürütür.
//
// ## Kullanım örneği
//
// ```go
//
//	flow := oauth.Flow{
//	    BaseURL: "https://www.codebuff.com",
//	    Store:   credentials.FileStore{Path: "/tmp/credentials.json"},
//	}
//
// code, err := flow.RequestLoginCode(context.Background())
//
//	if err != nil {
//	    return err
//	}
//
// fmt.Println(code.LoginURL)
// _, err = flow.PollLoginStatus(context.Background(), code)
// ```
type Flow struct {
	BaseURL       string
	HTTPClient    *http.Client
	Store         credentials.Store
	FingerprintID func() (string, error)
	PollInterval  time.Duration
	PollTimeout   time.Duration
	Sleep         func(context.Context, time.Duration) error
}

// LoginCode, /api/auth/cli/code yanıtındaki kullanıcıya gösterilecek URL ve polling metadata'sıdır.
type LoginCode struct {
	FingerprintID   string
	FingerprintHash string
	LoginURL        string
	ExpiresAt       string
}

type clearStore interface {
	Clear(ctx context.Context) error
}

type loginCodeResponse struct {
	FingerprintID   string          `json:"fingerprintId"`
	FingerprintHash string          `json:"fingerprintHash"`
	LoginURL        string          `json:"loginUrl"`
	ExpiresAt       json.RawMessage `json:"expiresAt"`
}

type loginStatusResponse struct {
	User  credentialResponse `json:"user"`
	Error string             `json:"error"`
}

type credentialResponse struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Email           string `json:"email"`
	AuthToken       string `json:"authToken"`
	FingerprintID   string `json:"fingerprintId"`
	FingerprintHash string `json:"fingerprintHash"`
}

// RequestLoginCode, Authorization header kullanmadan fingerprintId ile CLI login URL'i ister.
func (f Flow) RequestLoginCode(ctx context.Context) (LoginCode, error) {
	fingerprintID, err := f.fingerprintID()
	if err != nil {
		return LoginCode{}, err
	}

	var response loginCodeResponse
	if err := f.doJSON(ctx, http.MethodPost, loginCodePath, nil, map[string]string{
		"fingerprintId": fingerprintID,
	}, "", &response); err != nil {
		return LoginCode{}, err
	}

	expiresAt, err := stringifyExpiresAt(response.ExpiresAt)
	if err != nil {
		return LoginCode{}, err
	}
	if response.FingerprintHash == "" || response.LoginURL == "" || expiresAt == "" {
		return LoginCode{}, errors.New("login code response missing required fields")
	}
	if response.FingerprintID != "" {
		fingerprintID = response.FingerprintID
	}

	return LoginCode{
		FingerprintID:   fingerprintID,
		FingerprintHash: response.FingerprintHash,
		LoginURL:        response.LoginURL,
		ExpiresAt:       expiresAt,
	}, nil
}

// Login, login code alır, status endpointini başarıya kadar poll eder ve credential'ı kaydeder.
func (f Flow) Login(ctx context.Context) (credentials.Credential, error) {
	code, err := f.RequestLoginCode(ctx)
	if err != nil {
		return credentials.Credential{}, err
	}

	return f.PollLoginStatus(ctx, code)
}

// PollLoginStatus, Authorization header kullanmadan status endpointini başarı gelene kadar sorgular.
func (f Flow) PollLoginStatus(ctx context.Context, code LoginCode) (credentials.Credential, error) {
	if f.Store == nil {
		return credentials.Credential{}, errors.New("credentials store is required")
	}

	deadline := time.Now().Add(f.pollTimeout())
	pollCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	for {
		if err := pollCtx.Err(); err != nil {
			return credentials.Credential{}, err
		}

		cred, retry, err := f.requestLoginStatus(pollCtx, code)
		if err != nil {
			return credentials.Credential{}, err
		}
		if !retry {
			if err := f.Store.Save(pollCtx, cred); err != nil {
				return credentials.Credential{}, err
			}

			return cred, nil
		}

		if err := f.sleep(pollCtx, f.pollInterval()); err != nil {
			return credentials.Credential{}, err
		}
	}
}

// Logout, kayıtlı credential metadata'sı ile API logout çağrısı yapar ve başarılıysa yerel dosyayı temizler.
func (f Flow) Logout(ctx context.Context) error {
	if f.Store == nil {
		return errors.New("credentials store is required")
	}

	cred, err := f.Store.Load(ctx)
	if err != nil {
		return err
	}
	if cred.ID == "" || cred.FingerprintID == "" || cred.FingerprintHash == "" {
		return errors.New("credentials missing logout metadata")
	}

	var response struct{}
	if err := f.doJSON(ctx, http.MethodPost, logoutPath, nil, map[string]string{
		"userId":          cred.ID,
		"fingerprintId":   cred.FingerprintID,
		"fingerprintHash": cred.FingerprintHash,
	}, cred.AuthToken, &response); err != nil {
		return err
	}

	if clearer, ok := f.Store.(clearStore); ok {
		return clearer.Clear(ctx)
	}

	return nil
}

func (f Flow) requestLoginStatus(ctx context.Context, code LoginCode) (credentials.Credential, bool, error) {
	query := url.Values{}
	query.Set("fingerprintId", code.FingerprintID)
	query.Set("fingerprintHash", code.FingerprintHash)
	query.Set("expiresAt", code.ExpiresAt)

	var response loginStatusResponse
	status, err := f.doJSONStatus(ctx, http.MethodGet, loginStatusPath, query, nil, "", &response)
	if err != nil {
		return credentials.Credential{}, false, err
	}
	if status == http.StatusUnauthorized {
		if response.Error == pendingStatusError {
			return credentials.Credential{}, true, nil
		}

		return credentials.Credential{}, false, errors.New("login status unauthorized")
	}
	if status < 200 || status >= 300 {
		return credentials.Credential{}, false, fmt.Errorf("login status failed with status %d", status)
	}

	cred := credentials.Credential{
		ID:              response.User.ID,
		Name:            response.User.Name,
		Email:           response.User.Email,
		AuthToken:       response.User.AuthToken,
		FingerprintID:   response.User.FingerprintID,
		FingerprintHash: response.User.FingerprintHash,
	}
	if cred.FingerprintID == "" {
		cred.FingerprintID = code.FingerprintID
	}
	if cred.FingerprintHash == "" {
		cred.FingerprintHash = code.FingerprintHash
	}
	if cred.AuthToken == "" {
		return credentials.Credential{}, false, credentials.ErrMissingToken
	}
	if cred.ID == "" {
		return credentials.Credential{}, false, errors.New("login status response missing user id")
	}

	return cred, false, nil
}

func (f Flow) doJSON(ctx context.Context, method, path string, query url.Values, body any, bearerToken string, out any) error {
	status, err := f.doJSONStatus(ctx, method, path, query, body, bearerToken, out)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("%s %s failed with status %d", method, path, status)
	}

	return nil
}

func (f Flow) doJSONStatus(ctx context.Context, method, path string, query url.Values, body any, bearerToken string, out any) (int, error) {
	requestURL, err := f.endpoint(path, query)
	if err != nil {
		return 0, err
	}

	var bodyReader *bytes.Reader
	if body == nil {
		bodyReader = bytes.NewReader(nil)
	} else {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("encode request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, requestURL, bodyReader)
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}

	resp, err := f.httpClient().Do(req)
	if err != nil {
		return 0, safeTransportError(req.Context(), method, path, err)
	}
	defer resp.Body.Close()

	if out == nil {
		return resp.StatusCode, nil
	}

	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if err := decoder.Decode(out); err != nil && !errors.Is(err, io.EOF) {
			return resp.StatusCode, nil
		}

		return resp.StatusCode, nil
	}
	if err := decoder.Decode(out); err != nil {
		return 0, fmt.Errorf("decode response body: %w", err)
	}

	return resp.StatusCode, nil
}

func (f Flow) endpoint(endpointPath string, query url.Values) (string, error) {
	base := strings.TrimRight(f.BaseURL, "/")
	if base == "" {
		return "", errors.New("base URL is required")
	}

	parsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse endpoint URL: %w", err)
	}

	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = "/" + strings.TrimPrefix(path.Join(parsed.Path, endpointPath), "/")
	if len(query) > 0 {
		parsed.RawQuery = query.Encode()
	}

	return parsed.String(), nil
}

func safeTransportError(ctx context.Context, method, path string, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("%s %s %s: %w", method, path, oauthTransportError, ctxErr)
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("%s %s %s: %w", method, path, oauthTransportError, context.Canceled)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s %s %s: %w", method, path, oauthTransportError, context.DeadlineExceeded)
	}

	return fmt.Errorf("%s %s %s", method, path, oauthTransportError)
}

func (f Flow) fingerprintID() (string, error) {
	if f.FingerprintID != nil {
		return f.FingerprintID()
	}

	random := make([]byte, 9)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate fingerprint: %w", err)
	}

	return "freebuff-cli-" + base64.RawURLEncoding.EncodeToString(random), nil
}

func (f Flow) httpClient() *http.Client {
	if f.HTTPClient != nil {
		return f.HTTPClient
	}

	return http.DefaultClient
}

func (f Flow) pollInterval() time.Duration {
	if f.PollInterval > 0 {
		return f.PollInterval
	}

	return 5 * time.Second
}

func (f Flow) pollTimeout() time.Duration {
	if f.PollTimeout > 0 {
		return f.PollTimeout
	}

	return 5 * time.Minute
}

func (f Flow) sleep(ctx context.Context, duration time.Duration) error {
	if f.Sleep != nil {
		return f.Sleep(ctx, duration)
	}

	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func stringifyExpiresAt(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", errors.New("login code response missing expiresAt")
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}

	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		return number.String(), nil
	}

	return "", errors.New("login code response has invalid expiresAt")
}
