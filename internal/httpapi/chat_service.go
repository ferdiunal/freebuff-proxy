package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/ferdiunal/freebuff-proxy/internal/credentials"
	"github.com/ferdiunal/freebuff-proxy/internal/freebuff"
	"github.com/ferdiunal/freebuff-proxy/internal/openai"
	"github.com/ferdiunal/freebuff-proxy/internal/session"
)

// SessionEnsurer, sohbet öncesinde Freebuff oturumunun aktif olduğunu garanti eden bağımlılığı tanımlar.
//
// ## Kullanım örneği
//
// ```go
// session, err := ensurer.EnsureActive(ctx, req.Model)
// if err != nil { return err }
// _ = session
// ```
type SessionEnsurer interface {
	EnsureActive(ctx context.Context, model string) (freebuff.Session, error)
}

// CredentialStore, upstream sohbet çağrısında kullanılacak Freebuff kimliğini yükler.
//
// ## Kullanım örneği
//
// ```go
// cred, err := store.Load(ctx)
// if err != nil { return err }
// token := cred.AuthToken
// ```
type CredentialStore interface {
	Load(ctx context.Context) (credentials.Credential, error)
}

// UpstreamChatClient, gerçek Freebuff sohbet istemcisinin HTTP katmanına sunduğu sözleşmedir.
//
// ## Kullanım örneği
//
// ```go
// text, err := upstream.Complete(ctx, token, req)
// if err != nil { return err }
// fmt.Println(text)
// ```
type UpstreamChatClient interface {
	Complete(ctx context.Context, token string, activeSession freebuff.Session, req openai.ChatCompletionRequest) (string, error)
	Stream(ctx context.Context, token string, activeSession freebuff.Session, req openai.ChatCompletionRequest) (<-chan string, <-chan error)
}

// FreebuffChatService, HTTP sohbet isteklerini oturum, kimlik ve upstream istemcisiyle birleştirir.
//
// ## Kullanım örneği
//
// ```go
// service := httpapi.FreebuffChatService{Store: store, Sessions: manager, Upstream: freebuffClient}
// text, err := service.Complete(ctx, req)
// ```
type FreebuffChatService struct {
	Store    CredentialStore
	Sessions SessionEnsurer
	Upstream UpstreamChatClient
}

// Complete, oturumu aktif eder, kimlik bilgisini yükler ve isteği upstream sohbet istemcisine aktarır.
func (s FreebuffChatService) Complete(ctx context.Context, req openai.ChatCompletionRequest) (string, error) {
	if err := s.validate(); err != nil {
		return "", err
	}

	activeSession, err := s.Sessions.EnsureActive(ctx, freebuff.CanonicalModelName(req.Model))
	if err != nil {
		return "", normalizeSessionSetupError(err)
	}

	cred, err := s.Store.Load(ctx)
	if err != nil {
		return "", fmt.Errorf("load freebuff credentials: %w", err)
	}

	text, err := s.Upstream.Complete(ctx, cred.AuthToken, activeSession, req)
	if err != nil {
		return "", normalizeUpstreamChatError(err)
	}

	return text, nil
}

// Stream, setup adımlarını senkron tamamlar ve yalnızca upstream akışını goroutine içinde taşır.
func (s FreebuffChatService) Stream(ctx context.Context, req openai.ChatCompletionRequest) (<-chan string, <-chan error) {
	if err := s.validate(); err != nil {
		return failedStream(err)
	}

	activeSession, err := s.Sessions.EnsureActive(ctx, freebuff.CanonicalModelName(req.Model))
	if err != nil {
		return failedStream(normalizeSessionSetupError(err))
	}

	cred, err := s.Store.Load(ctx)
	if err != nil {
		return failedStream(fmt.Errorf("load freebuff credentials: %w", err))
	}

	streamCtx, cancel := context.WithCancel(ctx)
	upstreamDeltas, upstreamErrs := s.Upstream.Stream(streamCtx, cred.AuthToken, activeSession, req)
	if upstreamDeltas == nil && upstreamErrs == nil {
		cancel()
		return failedStream(serviceUnavailable("upstream_chat_unavailable", "Upstream sohbet akışı kanal döndürmedi"))
	}

	start, err := inspectStreamStart(upstreamDeltas, upstreamErrs)
	if err != nil {
		cancel()
		return failedStream(normalizeUpstreamChatError(err))
	}

	deltas := make(chan string)
	errs := make(chan error, 1)
	go func() {
		defer cancel()
		defer close(deltas)
		defer close(errs)

		if start.hasFirstDelta && !sendStreamDelta(streamCtx, deltas, start.firstDelta) {
			return
		}
		if start.firstErr != nil && !sendStreamError(streamCtx, errs, normalizeUpstreamChatError(start.firstErr)) {
			return
		}

		forwardStream(streamCtx, deltas, errs, start.deltas, start.errs)
	}()

	return deltas, errs
}

func (s FreebuffChatService) validate() error {
	if s.Sessions == nil {
		return serviceUnavailable("session_manager_unavailable", "Freebuff oturum yöneticisi yapılandırılmadı")
	}
	if s.Store == nil {
		return serviceUnavailable("credential_store_unavailable", "Freebuff kimlik deposu yapılandırılmadı")
	}
	if s.Upstream == nil {
		return serviceUnavailable("upstream_chat_unavailable", "Freebuff upstream sohbet istemcisi yapılandırılmadı")
	}

	return nil
}

type streamStart struct {
	deltas        <-chan string
	errs          <-chan error
	firstDelta    string
	hasFirstDelta bool
	firstErr      error
}

func failedStream(err error) (<-chan string, <-chan error) {
	deltas := make(chan string)
	close(deltas)

	errs := make(chan error, 1)
	if err != nil {
		errs <- err
	}
	close(errs)

	return deltas, errs
}

func inspectStreamStart(upstreamDeltas <-chan string, upstreamErrs <-chan error) (streamStart, error) {
	start := streamStart{deltas: upstreamDeltas, errs: upstreamErrs}
	if upstreamErrs == nil {
		return start, nil
	}

	select {
	case err, ok := <-upstreamErrs:
		if !ok {
			start.errs = nil
			return start, nil
		}
		if err == nil {
			return start, nil
		}
		if upstreamDeltas == nil {
			return start, err
		}

		select {
		case delta, ok := <-upstreamDeltas:
			if !ok {
				return start, err
			}
			start.firstDelta = delta
			start.hasFirstDelta = true
			start.firstErr = err
			return start, nil
		default:
			return start, err
		}
	default:
		return start, nil
	}
}

func serviceUnavailable(code string, message string) *ServiceError {
	return &ServiceError{Status: http.StatusServiceUnavailable, Code: code, Message: message}
}

func normalizeSessionSetupError(err error) error {
	if err == nil {
		return nil
	}

	wrapped := fmt.Errorf("ensure freebuff session active: %w", err)

	var serviceErr *ServiceError
	if errors.As(wrapped, &serviceErr) {
		return serviceErr
	}

	var apiErr *freebuff.APIError
	if errors.As(wrapped, &apiErr) {
		return sanitizedFreebuffAPIError(apiErr)
	}

	var statusErr *session.StatusError
	if errors.As(wrapped, &statusErr) {
		return sanitizedSessionStatusError(statusErr.Status)
	}

	return serviceUnavailable("freebuff_session_unavailable", "Freebuff oturumu hazırlanamadı")
}

func normalizeUpstreamChatError(err error) error {
	if err == nil {
		return nil
	}

	var serviceErr *ServiceError
	if errors.As(err, &serviceErr) {
		return serviceErr
	}

	var apiErr *freebuff.APIError
	if errors.As(err, &apiErr) {
		status := apiErr.StatusCode
		if status == 0 {
			status = http.StatusServiceUnavailable
		}

		code := apiErr.Code
		if code == "" {
			code = "upstream_chat_error"
		}

		message := apiErr.Message
		if message == "" {
			message = http.StatusText(status)
		}

		return &ServiceError{Status: status, Code: code, Message: message}
	}

	return err
}

func sanitizedSessionStatusError(status freebuff.SessionStatus) *ServiceError {
	switch status {
	case freebuff.SessionRateLimited:
		return &ServiceError{Status: http.StatusTooManyRequests, Code: "freebuff_rate_limited", Message: "Freebuff oturum limiti aşıldı"}
	case freebuff.SessionDisabled, freebuff.SessionCountryBlocked, freebuff.SessionBanned:
		return &ServiceError{Status: http.StatusForbidden, Code: "freebuff_session_unavailable", Message: "Freebuff oturumu kullanılamıyor"}
	case freebuff.SessionModelLocked, freebuff.SessionModelUnavailable:
		return &ServiceError{Status: http.StatusServiceUnavailable, Code: "freebuff_model_unavailable", Message: "Freebuff modeli kullanılamıyor"}
	default:
		return serviceUnavailable("freebuff_session_unavailable", "Freebuff oturumu hazırlanamadı")
	}
}

func sanitizedFreebuffAPIError(apiErr *freebuff.APIError) *ServiceError {
	status := apiErr.StatusCode
	if status == 0 {
		status = http.StatusServiceUnavailable
	}

	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return &ServiceError{Status: status, Code: "freebuff_auth_failed", Message: "Freebuff kimlik doğrulaması başarısız oldu"}
	case http.StatusTooManyRequests:
		return &ServiceError{Status: status, Code: "freebuff_rate_limited", Message: "Freebuff oturum limiti aşıldı"}
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return &ServiceError{Status: status, Code: "upstream_chat_unavailable", Message: "Freebuff oturum servisi şu anda kullanılamıyor"}
	default:
		return &ServiceError{Status: status, Code: "freebuff_session_error", Message: "Freebuff oturum servisi hata döndürdü"}
	}
}

func forwardStream(ctx context.Context, deltas chan<- string, errs chan<- error, upstreamDeltas <-chan string, upstreamErrs <-chan error) {
	for upstreamDeltas != nil || upstreamErrs != nil {
		select {
		case <-ctx.Done():
			return
		case delta, ok := <-upstreamDeltas:
			if !ok {
				upstreamDeltas = nil
				continue
			}
			if !sendStreamDelta(ctx, deltas, delta) {
				return
			}
		case err, ok := <-upstreamErrs:
			if !ok {
				upstreamErrs = nil
				continue
			}
			if err == nil {
				continue
			}
			if !sendStreamError(ctx, errs, normalizeUpstreamChatError(err)) {
				return
			}
		}
	}
}

func sendStreamDelta(ctx context.Context, deltas chan<- string, delta string) bool {
	select {
	case <-ctx.Done():
		return false
	case deltas <- delta:
		return true
	}
}

func sendStreamError(ctx context.Context, errs chan<- error, err error) bool {
	if err == nil {
		return true
	}

	select {
	case <-ctx.Done():
		return false
	case errs <- err:
		return true
	}
}
