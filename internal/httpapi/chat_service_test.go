package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ferdiunal/freebuff-proxy/internal/credentials"
	"github.com/ferdiunal/freebuff-proxy/internal/freebuff"
	"github.com/ferdiunal/freebuff-proxy/internal/openai"
	"github.com/ferdiunal/freebuff-proxy/internal/session"
)

// Bu testler, Freebuff sohbet adaptörü sözleşmesini sahte istemcilerle doğrular.
//
// ## Kullanım örneği
//
// ```bash
// go test ./internal/httpapi -run TestFreebuffChatService
// go test ./internal/httpapi -run TestForwardStream
// ```
func TestFreebuffChatServiceComplete(t *testing.T) {
	req := richChatRequest()
	events := []string{}
	store := &recordingCredentialStore{credential: credentials.Credential{AuthToken: "freebuff-token"}, events: &events}
	activeSession := freebuff.Session{Status: freebuff.SessionActive, InstanceID: "freebuff-proxy"}
	sessions := &recordingSessionEnsurer{session: activeSession, events: &events}
	upstream := &recordingUpstreamChatClient{completeText: "Merhaba upstream", events: &events}
	service := FreebuffChatService{Store: store, Sessions: sessions, Upstream: upstream}

	text, err := service.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete hata döndürdü: %v", err)
	}

	if text != "Merhaba upstream" {
		t.Fatalf("text = %q, beklenen upstream yanıtı", text)
	}
	assertEvents(t, &events, []string{"ensure:deepseek/deepseek-v4-pro", "load", "complete"})
	if upstream.completeToken != "freebuff-token" {
		t.Fatal("upstream token beklenen credential auth token ile eşleşmedi")
	}
	if !reflect.DeepEqual(upstream.completeSession, activeSession) {
		t.Fatalf("upstream session = %#v, beklenen %#v", upstream.completeSession, activeSession)
	}
	if !reflect.DeepEqual(upstream.completeRequest, req) {
		t.Fatalf("upstream request değişti:\n got: %#v\nwant: %#v", upstream.completeRequest, req)
	}
}

func TestFreebuffChatServiceCompleteSanitizesEnsureActiveError(t *testing.T) {
	expectedErr := errors.New("session blocked with Authorization Bearer secret-token fingerprintHash secret-prompt")
	events := []string{}
	store := &recordingCredentialStore{credential: credentials.Credential{AuthToken: "freebuff-token"}, events: &events}
	sessions := &recordingSessionEnsurer{err: expectedErr, events: &events}
	upstream := &recordingUpstreamChatClient{completeText: "yanıt", events: &events}
	service := FreebuffChatService{Store: store, Sessions: sessions, Upstream: upstream}

	_, err := service.Complete(context.Background(), richChatRequest())
	assertServiceError(t, err, http.StatusServiceUnavailable, "freebuff_session_unavailable")
	assertServiceErrorMessage(t, err, "Freebuff oturumu hazırlanamadı")
	for _, sensitive := range []string{"secret-token", "Bearer", "Authorization", "fingerprintHash", "secret-prompt"} {
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("ensure hatası hassas değeri %q sızdırıyor: %v", sensitive, err)
		}
	}
	assertEvents(t, &events, []string{"ensure:deepseek/deepseek-v4-pro"})
	if store.loadCount != 0 {
		t.Fatalf("loadCount = %d, beklenen 0", store.loadCount)
	}
	if upstream.completeCount != 0 {
		t.Fatalf("completeCount = %d, beklenen 0", upstream.completeCount)
	}
}

func TestFreebuffChatServiceCompletePropagatesStoreLoadError(t *testing.T) {
	expectedErr := errors.New("credentials missing")
	events := []string{}
	store := &recordingCredentialStore{err: expectedErr, events: &events}
	sessions := &recordingSessionEnsurer{session: freebuff.Session{Status: freebuff.SessionActive}, events: &events}
	upstream := &recordingUpstreamChatClient{completeText: "yanıt", events: &events}
	service := FreebuffChatService{Store: store, Sessions: sessions, Upstream: upstream}

	_, err := service.Complete(context.Background(), richChatRequest())
	if !errors.Is(err, expectedErr) {
		t.Fatalf("err = %v, beklenen store hatası", err)
	}
	assertEvents(t, &events, []string{"ensure:deepseek/deepseek-v4-pro", "load"})
	if upstream.completeCount != 0 {
		t.Fatalf("completeCount = %d, beklenen 0", upstream.completeCount)
	}
}

func TestFreebuffChatServiceCompleteNormalizesUpstreamAPIError(t *testing.T) {
	upstreamErr := &freebuff.APIError{
		StatusCode: http.StatusTooManyRequests,
		Code:       "freebuff_rate_limited",
		Message:    "Freebuff sohbet limiti aşıldı",
	}
	events := []string{}
	store := &recordingCredentialStore{credential: credentials.Credential{AuthToken: "freebuff-token"}, events: &events}
	sessions := &recordingSessionEnsurer{session: freebuff.Session{Status: freebuff.SessionActive}, events: &events}
	upstream := &recordingUpstreamChatClient{completeErr: upstreamErr, events: &events}
	service := FreebuffChatService{Store: store, Sessions: sessions, Upstream: upstream}

	_, err := service.Complete(context.Background(), richChatRequest())
	assertServiceError(t, err, http.StatusTooManyRequests, "freebuff_rate_limited")
	assertServiceErrorMessage(t, err, "Freebuff sohbet limiti aşıldı")
	assertEvents(t, &events, []string{"ensure:deepseek/deepseek-v4-pro", "load", "complete"})
}

func TestFreebuffChatServiceCompleteSanitizesEnsureActiveStatusError(t *testing.T) {
	upstreamErr := &session.StatusError{
		Status:  freebuff.SessionRateLimited,
		Message: "Authorization Bearer secret-token fingerprintHash secret-prompt",
	}
	store := &recordingCredentialStore{credential: credentials.Credential{AuthToken: "freebuff-token"}}
	sessions := &recordingSessionEnsurer{err: upstreamErr}
	upstream := &recordingUpstreamChatClient{completeText: "yanıt"}
	service := FreebuffChatService{Store: store, Sessions: sessions, Upstream: upstream}

	_, err := service.Complete(context.Background(), richChatRequest())
	assertServiceError(t, err, http.StatusTooManyRequests, "freebuff_rate_limited")
	assertServiceErrorMessage(t, err, "Freebuff oturum limiti aşıldı")
	for _, sensitive := range []string{"secret-token", "Bearer", "Authorization", "fingerprintHash", "secret-prompt"} {
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("ensure status hatası hassas değeri %q sızdırıyor: %v", sensitive, err)
		}
	}
	if upstream.completeCount != 0 {
		t.Fatalf("completeCount = %d, beklenen 0", upstream.completeCount)
	}
}

func TestFreebuffChatServiceCompleteSanitizesEnsureActiveAPIError(t *testing.T) {
	upstreamErr := &freebuff.APIError{
		StatusCode: http.StatusTooManyRequests,
		Code:       "raw_upstream_code",
		Message:    "Authorization Bearer secret-token fingerprintHash secret-prompt",
	}
	store := &recordingCredentialStore{credential: credentials.Credential{AuthToken: "freebuff-token"}}
	sessions := &recordingSessionEnsurer{err: upstreamErr}
	upstream := &recordingUpstreamChatClient{completeText: "yanıt"}
	service := FreebuffChatService{Store: store, Sessions: sessions, Upstream: upstream}

	_, err := service.Complete(context.Background(), richChatRequest())
	assertServiceError(t, err, http.StatusTooManyRequests, "freebuff_rate_limited")
	assertServiceErrorMessage(t, err, "Freebuff oturum limiti aşıldı")
	for _, sensitive := range []string{"secret-token", "Bearer", "Authorization", "fingerprintHash", "secret-prompt"} {
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("ensure hatası hassas değeri %q sızdırıyor: %v", sensitive, err)
		}
	}
	if upstream.completeCount != 0 {
		t.Fatalf("completeCount = %d, beklenen 0", upstream.completeCount)
	}
}

func TestFreebuffChatServiceStreamSanitizesEnsureActiveAPIError(t *testing.T) {
	upstreamErr := &freebuff.APIError{
		StatusCode: http.StatusUnauthorized,
		Code:       "raw_auth_code",
		Message:    "Authorization Bearer secret-token fingerprintHash secret-prompt",
	}
	store := &recordingCredentialStore{credential: credentials.Credential{AuthToken: "freebuff-token"}}
	sessions := &recordingSessionEnsurer{err: upstreamErr}
	upstream := &recordingUpstreamChatClient{streamDeltas: []string{"yanıt"}}
	service := FreebuffChatService{Store: store, Sessions: sessions, Upstream: upstream}

	deltas, errs := service.Stream(context.Background(), richChatRequest())
	if gotDeltas := collectDeltas(deltas); len(gotDeltas) != 0 {
		t.Fatalf("deltas = %#v, beklenen boş", gotDeltas)
	}
	gotErrs := collectErrors(errs)
	if len(gotErrs) != 1 {
		t.Fatalf("errs = %#v, beklenen tek sanitize hata", gotErrs)
	}
	assertServiceError(t, gotErrs[0], http.StatusUnauthorized, "freebuff_auth_failed")
	assertServiceErrorMessage(t, gotErrs[0], "Freebuff kimlik doğrulaması başarısız oldu")
	for _, sensitive := range []string{"secret-token", "Bearer", "Authorization", "fingerprintHash", "secret-prompt"} {
		if strings.Contains(gotErrs[0].Error(), sensitive) {
			t.Fatalf("stream ensure hatası hassas değeri %q sızdırıyor: %v", sensitive, gotErrs[0])
		}
	}
	if upstream.streamCount != 0 {
		t.Fatalf("streamCount = %d, beklenen 0", upstream.streamCount)
	}
}

func TestFreebuffChatServiceCompleteDoesNotLeakProxyAPIKey(t *testing.T) {
	const proxyAPIKey = "proxy-secret"
	req := richChatRequest()
	events := []string{}
	store := &recordingCredentialStore{credential: credentials.Credential{AuthToken: "freebuff-token"}, events: &events}
	sessions := &recordingSessionEnsurer{session: freebuff.Session{Status: freebuff.SessionActive}, events: &events}
	upstream := &recordingUpstreamChatClient{completeText: "yanıt", events: &events}
	service := FreebuffChatService{Store: store, Sessions: sessions, Upstream: upstream}

	_, err := service.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete hata döndürdü: %v", err)
	}

	encoded, err := json.Marshal(upstream.completeRequest)
	if err != nil {
		t.Fatalf("request marshal hata döndürdü: %v", err)
	}
	if strings.Contains(string(encoded), proxyAPIKey) {
		t.Fatal("upstream request proxy API anahtarı içeriyor")
	}
	if upstream.completeToken != "freebuff-token" {
		t.Fatal("upstream token beklenen credential token ile eşleşmedi")
	}
}

func TestFreebuffChatServiceCompleteNilDependenciesReturnServiceError(t *testing.T) {
	tests := []struct {
		name    string
		service FreebuffChatService
		code    string
	}{
		{
			name:    "sessions nil",
			service: FreebuffChatService{Store: &recordingCredentialStore{}, Upstream: &recordingUpstreamChatClient{}},
			code:    "session_manager_unavailable",
		},
		{
			name:    "store nil",
			service: FreebuffChatService{Sessions: &recordingSessionEnsurer{}, Upstream: &recordingUpstreamChatClient{}},
			code:    "credential_store_unavailable",
		},
		{
			name:    "upstream nil",
			service: FreebuffChatService{Store: &recordingCredentialStore{}, Sessions: &recordingSessionEnsurer{}},
			code:    "upstream_chat_unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.service.Complete(context.Background(), richChatRequest())
			assertServiceError(t, err, http.StatusServiceUnavailable, tt.code)
		})
	}
}

func TestFreebuffChatServiceStreamForwardsDeltasAndErrors(t *testing.T) {
	req := richChatRequest()
	events := []string{}
	store := &recordingCredentialStore{credential: credentials.Credential{AuthToken: "freebuff-token"}, events: &events}
	activeSession := freebuff.Session{Status: freebuff.SessionActive, InstanceID: "freebuff-proxy"}
	sessions := &recordingSessionEnsurer{session: activeSession, events: &events}
	upstreamErr := errors.New("upstream stream warning")
	upstream := &recordingUpstreamChatClient{streamDeltas: []string{"Mer", "haba"}, streamErr: upstreamErr, events: &events}
	service := FreebuffChatService{Store: store, Sessions: sessions, Upstream: upstream}

	deltas, errs := service.Stream(context.Background(), req)
	gotDeltas := collectDeltas(deltas)
	gotErrs := collectErrors(errs)

	assertEvents(t, &events, []string{"ensure:deepseek/deepseek-v4-pro", "load", "stream"})
	if upstream.streamToken != "freebuff-token" {
		t.Fatal("stream token beklenen credential auth token ile eşleşmedi")
	}
	if !reflect.DeepEqual(upstream.streamSession, activeSession) {
		t.Fatalf("stream session = %#v, beklenen %#v", upstream.streamSession, activeSession)
	}
	if !reflect.DeepEqual(upstream.streamRequest, req) {
		t.Fatalf("stream request değişti:\n got: %#v\nwant: %#v", upstream.streamRequest, req)
	}
	if !reflect.DeepEqual(gotDeltas, []string{"Mer", "haba"}) {
		t.Fatalf("deltas = %#v, beklenen tüm upstream deltaları", gotDeltas)
	}
	if len(gotErrs) != 1 || !errors.Is(gotErrs[0], upstreamErr) {
		t.Fatalf("errs = %#v, beklenen upstream hatası", gotErrs)
	}
}

func TestFreebuffChatServiceStreamNormalizesForwardedUpstreamAPIError(t *testing.T) {
	upstreamErr := &freebuff.APIError{
		StatusCode: http.StatusTooManyRequests,
		Code:       "freebuff_rate_limited",
		Message:    "Freebuff sohbet limiti aşıldı",
	}
	events := []string{}
	store := &recordingCredentialStore{credential: credentials.Credential{AuthToken: "freebuff-token"}, events: &events}
	sessions := &recordingSessionEnsurer{session: freebuff.Session{Status: freebuff.SessionActive}, events: &events}
	upstream := delayedStreamErrorUpstream{err: upstreamErr, events: &events}
	service := FreebuffChatService{Store: store, Sessions: sessions, Upstream: upstream}

	deltas, errs := service.Stream(context.Background(), richChatRequest())
	if gotDeltas := collectDeltas(deltas); !reflect.DeepEqual(gotDeltas, []string{"Merhaba"}) {
		t.Fatalf("deltas = %#v, beklenen ilk stream parçası", gotDeltas)
	}
	gotErrs := collectErrors(errs)
	if len(gotErrs) != 1 {
		t.Fatalf("errs = %#v, beklenen tek normalize hata", gotErrs)
	}
	assertServiceError(t, gotErrs[0], http.StatusTooManyRequests, "freebuff_rate_limited")
	assertServiceErrorMessage(t, gotErrs[0], "Freebuff sohbet limiti aşıldı")
	assertEvents(t, &events, []string{"ensure:deepseek/deepseek-v4-pro", "load", "stream"})
}

func TestFreebuffChatServiceStreamSetupErrorSendsImmediateErrorAndCloses(t *testing.T) {
	expectedErr := errors.New("session unavailable with Authorization Bearer secret-token fingerprintHash secret-prompt")
	events := []string{}
	store := &recordingCredentialStore{credential: credentials.Credential{AuthToken: "freebuff-token"}, events: &events}
	sessions := &recordingSessionEnsurer{err: expectedErr, events: &events}
	upstream := &recordingUpstreamChatClient{streamDeltas: []string{"yanıt"}, events: &events}
	service := FreebuffChatService{Store: store, Sessions: sessions, Upstream: upstream}

	deltas, errs := service.Stream(context.Background(), richChatRequest())
	select {
	case _, ok := <-deltas:
		if ok {
			t.Fatal("deltas açık kaldı, setup hatasında kapalı olmalı")
		}
	default:
		t.Fatal("deltas hemen kapalı olmalı")
	}

	gotErr, ok, closed := receiveImmediateError(errs)
	if !ok {
		t.Fatalf("immediate err = %v, ok = %v, beklenen setup hatası", gotErr, ok)
	}
	assertServiceError(t, gotErr, http.StatusServiceUnavailable, "freebuff_session_unavailable")
	assertServiceErrorMessage(t, gotErr, "Freebuff oturumu hazırlanamadı")
	for _, sensitive := range []string{"secret-token", "Bearer", "Authorization", "fingerprintHash", "secret-prompt"} {
		if strings.Contains(gotErr.Error(), sensitive) {
			t.Fatalf("stream setup hatası hassas değeri %q sızdırıyor: %v", sensitive, gotErr)
		}
	}
	if closed {
		t.Fatal("hata okunurken errs beklenmedik şekilde kapalı raporlandı")
	}
	if gotErrs := collectErrors(errs); len(gotErrs) != 0 {
		t.Fatalf("errs = %#v, beklenen tek immediate setup hatası", gotErrs)
	}
	assertEvents(t, &events, []string{"ensure:deepseek/deepseek-v4-pro"})
	if store.loadCount != 0 {
		t.Fatalf("loadCount = %d, beklenen 0", store.loadCount)
	}
	if upstream.streamCount != 0 {
		t.Fatalf("streamCount = %d, beklenen 0", upstream.streamCount)
	}
}

func TestFreebuffChatServiceStreamUpstreamImmediateErrorIsReady(t *testing.T) {
	events := []string{}
	store := &recordingCredentialStore{credential: credentials.Credential{AuthToken: "freebuff-token"}, events: &events}
	sessions := &recordingSessionEnsurer{session: freebuff.Session{Status: freebuff.SessionActive}, events: &events}
	upstreamErr := serviceUnavailable("upstream_chat_unavailable", "Freebuff upstream sohbet istemcisi kullanılamıyor")
	upstream := &recordingUpstreamChatClient{streamErr: upstreamErr, events: &events}
	service := FreebuffChatService{Store: store, Sessions: sessions, Upstream: upstream}

	deltas, errs := service.Stream(context.Background(), richChatRequest())
	select {
	case _, ok := <-deltas:
		if ok {
			t.Fatal("deltas açık kaldı, upstream immediate hatasında kapalı olmalı")
		}
	default:
		t.Fatal("deltas hemen kapalı olmalı")
	}

	gotErr, ok, closed := receiveImmediateError(errs)
	if !ok {
		t.Fatalf("immediate upstream err okunamadı, closed = %v", closed)
	}
	assertServiceError(t, gotErr, http.StatusServiceUnavailable, "upstream_chat_unavailable")
	assertEvents(t, &events, []string{"ensure:deepseek/deepseek-v4-pro", "load", "stream"})
}

func TestFreebuffChatServiceStreamNormalizesImmediateUpstreamAPIError(t *testing.T) {
	upstreamErr := &freebuff.APIError{
		StatusCode: http.StatusTooManyRequests,
		Code:       "freebuff_rate_limited",
		Message:    "Freebuff sohbet limiti aşıldı",
	}
	events := []string{}
	store := &recordingCredentialStore{credential: credentials.Credential{AuthToken: "freebuff-token"}, events: &events}
	sessions := &recordingSessionEnsurer{session: freebuff.Session{Status: freebuff.SessionActive}, events: &events}
	upstream := &recordingUpstreamChatClient{streamErr: upstreamErr, events: &events}
	service := FreebuffChatService{Store: store, Sessions: sessions, Upstream: upstream}

	deltas, errs := service.Stream(context.Background(), richChatRequest())
	if gotDeltas := collectDeltas(deltas); len(gotDeltas) != 0 {
		t.Fatalf("deltas = %#v, beklenen boş", gotDeltas)
	}
	gotErrs := collectErrors(errs)
	if len(gotErrs) != 1 {
		t.Fatalf("errs = %#v, beklenen tek immediate normalize hata", gotErrs)
	}
	assertServiceError(t, gotErrs[0], http.StatusTooManyRequests, "freebuff_rate_limited")
	assertServiceErrorMessage(t, gotErrs[0], "Freebuff sohbet limiti aşıldı")
	assertEvents(t, &events, []string{"ensure:deepseek/deepseek-v4-pro", "load", "stream"})
}

func TestForwardStreamReturnsOnCanceledContextWithFullErrorBuffer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	deltas := make(chan string)
	errs := make(chan error, 1)
	errs <- errors.New("buffer full")
	upstreamDeltas := make(chan string)
	upstreamErrs := make(chan error)
	done := make(chan struct{})

	go func() {
		defer close(done)
		forwardStream(ctx, deltas, errs, upstreamDeltas, upstreamErrs)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("forwardStream iptal edilmiş context ile bloklandı")
	}
}

func richChatRequest() openai.ChatCompletionRequest {
	temperature := 0.2
	maxTokens := 128

	return openai.ChatCompletionRequest{
		Model: "deepseek-v3.1-terminus",
		Messages: []openai.ChatMessage{
			{Role: "system", Content: "Türkçe yanıt ver"},
			{Role: "user", Content: "Merhaba"},
		},
		Stream:      true,
		Temperature: &temperature,
		MaxTokens:   &maxTokens,
		Tools: []json.RawMessage{
			json.RawMessage(`{"type":"function","function":{"name":"lookup_stock"}}`),
		},
		ToolChoice: map[string]any{"type": "function", "function": map[string]any{"name": "lookup_stock"}},
	}
}

type recordingSessionEnsurer struct {
	session freebuff.Session
	err     error
	events  *[]string
}

func (r *recordingSessionEnsurer) EnsureActive(ctx context.Context, model string) (freebuff.Session, error) {
	_ = ctx
	r.record("ensure:" + model)
	if r.err != nil {
		return freebuff.Session{}, r.err
	}

	return r.session, nil
}

func (r *recordingSessionEnsurer) record(event string) {
	if r.events == nil {
		r.events = &[]string{}
	}
	*r.events = append(*r.events, event)
}

type recordingCredentialStore struct {
	credential credentials.Credential
	err        error
	loadCount  int
	events     *[]string
}

func (r *recordingCredentialStore) Load(ctx context.Context) (credentials.Credential, error) {
	_ = ctx
	r.loadCount++
	r.record("load")
	if r.err != nil {
		return credentials.Credential{}, r.err
	}

	return r.credential, nil
}

func (r *recordingCredentialStore) record(event string) {
	if r.events == nil {
		r.events = &[]string{}
	}
	*r.events = append(*r.events, event)
}

type recordingUpstreamChatClient struct {
	completeText    string
	completeErr     error
	streamDeltas    []string
	streamErr       error
	completeCount   int
	streamCount     int
	completeToken   string
	streamToken     string
	completeSession freebuff.Session
	streamSession   freebuff.Session
	completeRequest openai.ChatCompletionRequest
	streamRequest   openai.ChatCompletionRequest
	events          *[]string
}

func (r *recordingUpstreamChatClient) Complete(ctx context.Context, token string, activeSession freebuff.Session, req openai.ChatCompletionRequest) (string, error) {
	_ = ctx
	r.completeCount++
	r.completeToken = token
	r.completeSession = activeSession
	r.completeRequest = req
	r.record("complete")
	if r.completeErr != nil {
		return "", r.completeErr
	}

	return r.completeText, nil
}

func (r *recordingUpstreamChatClient) Stream(ctx context.Context, token string, activeSession freebuff.Session, req openai.ChatCompletionRequest) (<-chan string, <-chan error) {
	_ = ctx
	r.streamCount++
	r.streamToken = token
	r.streamSession = activeSession
	r.streamRequest = req
	r.record("stream")

	deltas := make(chan string, len(r.streamDeltas))
	for _, delta := range r.streamDeltas {
		deltas <- delta
	}
	close(deltas)

	errs := make(chan error, 1)
	if r.streamErr != nil {
		errs <- r.streamErr
	}
	close(errs)

	return deltas, errs
}

func (r *recordingUpstreamChatClient) record(event string) {
	if r.events == nil {
		r.events = &[]string{}
	}
	*r.events = append(*r.events, event)
}

type delayedStreamErrorUpstream struct {
	err    error
	events *[]string
}

func (d delayedStreamErrorUpstream) Complete(ctx context.Context, token string, activeSession freebuff.Session, req openai.ChatCompletionRequest) (string, error) {
	_ = ctx
	_ = token
	_ = activeSession
	_ = req

	return "", d.err
}

func (d delayedStreamErrorUpstream) Stream(ctx context.Context, token string, activeSession freebuff.Session, req openai.ChatCompletionRequest) (<-chan string, <-chan error) {
	_ = token
	_ = activeSession
	_ = req
	d.record("stream")

	deltas := make(chan string)
	errs := make(chan error)
	go func() {
		defer close(deltas)
		defer close(errs)

		select {
		case <-ctx.Done():
			return
		case deltas <- "Merhaba":
		}
		select {
		case <-ctx.Done():
			return
		case errs <- d.err:
		}
	}()

	return deltas, errs
}

func (d delayedStreamErrorUpstream) record(event string) {
	if d.events == nil {
		return
	}
	*d.events = append(*d.events, event)
}

func assertEvents(t *testing.T, events *[]string, expected []string) {
	t.Helper()

	if events == nil {
		t.Fatalf("events = nil, beklenen %#v", expected)
	}
	if !reflect.DeepEqual(*events, expected) {
		t.Fatalf("events = %#v, beklenen %#v", *events, expected)
	}
}

func assertServiceError(t *testing.T, err error, status int, code string) {
	t.Helper()

	var serviceErr *ServiceError
	if !errors.As(err, &serviceErr) {
		t.Fatalf("err = %T %[1]v, beklenen ServiceError", err)
	}
	if serviceErr.Status != status {
		t.Fatalf("status = %d, beklenen %d", serviceErr.Status, status)
	}
	if serviceErr.Code != code {
		t.Fatalf("code = %q, beklenen %q", serviceErr.Code, code)
	}
	if serviceErr.Message == "" {
		t.Fatal("message boş olmamalı")
	}
}

func assertServiceErrorMessage(t *testing.T, err error, message string) {
	t.Helper()

	var serviceErr *ServiceError
	if !errors.As(err, &serviceErr) {
		t.Fatalf("err = %T %[1]v, beklenen ServiceError", err)
	}
	if serviceErr.Message != message {
		t.Fatalf("message = %q, beklenen %q", serviceErr.Message, message)
	}
}

func collectDeltas(deltas <-chan string) []string {
	var values []string
	for delta := range deltas {
		values = append(values, delta)
	}

	return values
}

func collectErrors(errs <-chan error) []error {
	var values []error
	for err := range errs {
		values = append(values, err)
	}

	return values
}
