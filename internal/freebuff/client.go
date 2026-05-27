package freebuff

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	sessionEndpointPath          = "/api/v1/freebuff/session"
	headerAuthorization          = "Authorization"
	headerInstanceID             = "x-freebuff-instance-id"
	headerModel                  = "x-freebuff-model"
	defaultResponseHeaderTimeout = 30 * time.Second
)

// Client, Freebuff oturum uç noktasına istek gönderen HTTP istemcisidir.
//
// ## Kullanım örneği
//
// ```go
// client, err := freebuff.NewClient("https://proxy.example.com", nil)
//
//	if err != nil {
//		return err
//	}
//
// session, err := client.StartSession(ctx, token, "instance-1", "claude-sonnet")
//
//	if err != nil {
//		return err
//	}
//
// fmt.Println(session.Status)
// ```
type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
}

// NewClient, temel Freebuff proxy adresinden yeni bir oturum istemcisi oluşturur.
func NewClient(baseURL string, httpClient *http.Client) (*Client, error) {
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse freebuff base url: %w", err)
	}

	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return nil, fmt.Errorf("parse freebuff base url: missing scheme or host")
	}

	if httpClient == nil {
		httpClient = defaultHTTPClient()
	}

	return &Client{
		baseURL:    parsedURL,
		httpClient: httpClient,
	}, nil
}

// GetSession, mevcut Freebuff oturum durumunu getirir.
func defaultHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = defaultResponseHeaderTimeout

	return &http.Client{Transport: transport}
}

func (c *Client) GetSession(ctx context.Context, token string, instanceID string) (Session, error) {
	return c.doSessionRequest(ctx, http.MethodGet, token, instanceID, "")
}

// StartSession, istenen model için yeni Freebuff oturumu başlatır.
func (c *Client) StartSession(ctx context.Context, token string, instanceID string, model string) (Session, error) {
	return c.doSessionRequest(ctx, http.MethodPost, token, instanceID, model)
}

// EndSession, mevcut Freebuff oturumunu sonlandırır.
func (c *Client) EndSession(ctx context.Context, token string, instanceID string) (Session, error) {
	return c.doSessionRequest(ctx, http.MethodDelete, token, instanceID, "")
}

func (c *Client) doSessionRequest(ctx context.Context, method string, token string, instanceID string, model string) (Session, error) {
	requestURL := c.baseURL.ResolveReference(&url.URL{Path: sessionEndpointPath})

	req, err := http.NewRequestWithContext(ctx, method, requestURL.String(), nil)
	if err != nil {
		return Session{}, fmt.Errorf("build freebuff session request: %w", err)
	}

	if token != "" {
		req.Header.Set(headerAuthorization, "Bearer "+token)
	}
	if instanceID != "" {
		req.Header.Set(headerInstanceID, instanceID)
	}
	if model != "" {
		req.Header.Set(headerModel, model)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Session{}, fmt.Errorf("send freebuff session request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		apiErr, err := decodeAPIError(resp)
		if err != nil {
			return Session{}, fmt.Errorf("decode freebuff error response: %w", err)
		}

		return Session{}, apiErr
	}

	var session Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return Session{}, fmt.Errorf("decode freebuff session response: %w", err)
	}

	return session, nil
}

func decodeAPIError(resp *http.Response) (*APIError, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read freebuff error response: %w", err)
	}

	apiErr := &APIError{
		StatusCode: resp.StatusCode,
		Message:    http.StatusText(resp.StatusCode),
	}

	if len(body) == 0 {
		return apiErr, nil
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return apiErr, nil
	}

	if code, ok := payload["code"].(string); ok {
		apiErr.Code = code
	}
	if message, ok := payload["message"].(string); ok {
		apiErr.Message = message
	}
	if apiErr.Code == "" {
		if code, ok := payload["error"].(string); ok {
			apiErr.Code = code
		}
	}
	if apiErr.Message == "" {
		if message, ok := payload["error"].(string); ok {
			apiErr.Message = message
		}
	}

	return apiErr, nil
}
