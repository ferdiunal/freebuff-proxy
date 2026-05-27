package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ferdiunal/freebuff-proxy/internal/credentials"
	"github.com/ferdiunal/freebuff-proxy/internal/freebuff"
)

const defaultPollInterval = 2 * time.Second

var errRetryBootstrap = errors.New("retry freebuff bootstrap with caller context")

// Client, oturum yöneticisinin Freebuff oturum API'siyle konuşmak için beklediği bağımlılığı tanımlar.
//
// ## Kullanım örneği
//
// ```go
// manager := session.NewManager(store, freebuffClient, "instance-1")
// current, err := manager.EnsureActive(ctx, "claude-sonnet")
//
//	if err != nil {
//		return err
//	}
//
// fmt.Println(current.Status)
// ```
type Client interface {
	GetSession(ctx context.Context, token string, instanceID string) (freebuff.Session, error)
	StartSession(ctx context.Context, token string, instanceID string, model string) (freebuff.Session, error)
	EndSession(ctx context.Context, token string, instanceID string) (freebuff.Session, error)
}

// StatusError, tekrar denemeden döndürülmesi gereken oturum durumlarını çağırana taşır.
//
// ## Kullanım örneği
//
// ```go
// var statusErr *session.StatusError
//
//	if errors.As(err, &statusErr) {
//		fmt.Println(statusErr.Status)
//	}
//
// ```
type StatusError struct {
	Status  freebuff.SessionStatus
	Message string
}

func (e *StatusError) Error() string {
	if e == nil {
		return ""
	}

	if e.Message != "" {
		return fmt.Sprintf("freebuff session status %q: %s", e.Status, e.Message)
	}

	return fmt.Sprintf("freebuff session status %q", e.Status)
}

// Manager, Freebuff oturumunun aktif olmasını sağlayan ve başlatma yarışını tekilleştiren bileşendir.
//
// ## Kullanım örneği
//
// ```go
// manager := session.NewManager(store, freebuffClient, "instance-1")
// manager.PollInterval = 250 * time.Millisecond
//
// current, err := manager.EnsureActive(ctx, "claude-sonnet")
//
//	if err != nil {
//		return err
//	}
//
// fmt.Println(current.InstanceID, current.Status)
// ```
type Manager struct {
	Store        credentials.Store
	Client       Client
	InstanceID   string
	PollInterval time.Duration
	MaxPolls     int

	mu        sync.Mutex
	bootstrap *bootstrapCall
}

type bootstrapCall struct {
	done    chan struct{}
	model   string
	session freebuff.Session
	err     error
}

// NewManager, varsayılan anket aralığıyla yeni bir oturum yöneticisi oluşturur.
func NewManager(store credentials.Store, client Client, instanceID string) *Manager {
	return &Manager{
		Store:        store,
		Client:       client,
		InstanceID:   instanceID,
		PollInterval: defaultPollInterval,
	}
}

// EnsureActive, oturum aktifse yeniden kullanır; gerekirse başlatır ve kuyruktan çıkmasını bekler.
func (m *Manager) EnsureActive(ctx context.Context, model string) (freebuff.Session, error) {
	cred, err := m.Store.Load(ctx)
	if err != nil {
		return freebuff.Session{}, fmt.Errorf("load credentials: %w", err)
	}

	for {
		if err := ctx.Err(); err != nil {
			return freebuff.Session{}, err
		}

		session, err := m.Client.GetSession(ctx, cred.AuthToken, m.InstanceID)
		if err != nil {
			return freebuff.Session{}, fmt.Errorf("get freebuff session: %w", err)
		}

		session, err = m.resolveSession(ctx, cred.AuthToken, model, session)
		if errors.Is(err, errRetryBootstrap) {
			continue
		}

		return session, err
	}
}

func (m *Manager) resolveSession(ctx context.Context, token string, model string, session freebuff.Session) (freebuff.Session, error) {
	switch session.Status {
	case freebuff.SessionActive:
		if sessionMatchesModel(session, model) {
			return session, nil
		}

		return m.restartSessionForModel(ctx, token, model)
	case freebuff.SessionQueued:
		if inflight := m.lookupBootstrap(); inflight != nil {
			return m.waitForBootstrap(ctx, inflight, model)
		}

		return m.pollUntilActive(ctx, token, model, session)
	case freebuff.SessionNone, freebuff.SessionEnded, freebuff.SessionSuperseded:
		return m.ensureBootstrap(ctx, token, model)
	case freebuff.SessionDisabled,
		freebuff.SessionCountryBlocked,
		freebuff.SessionModelLocked,
		freebuff.SessionModelUnavailable,
		freebuff.SessionBanned,
		freebuff.SessionRateLimited:
		return freebuff.Session{}, &StatusError{Status: session.Status, Message: session.Message}
	default:
		return freebuff.Session{}, fmt.Errorf("unknown freebuff session status %q", session.Status)
	}
}

func sessionMatchesModel(session freebuff.Session, model string) bool {
	expected := freebuff.CanonicalModelName(model)
	if expected == "" {
		return true
	}

	for _, actual := range []string{session.Model, session.CurrentModel, session.RequestedModel} {
		if actual == "" {
			continue
		}
		if freebuff.CanonicalModelName(actual) == expected {
			return true
		}
	}

	return false
}

func (m *Manager) restartSessionForModel(ctx context.Context, token string, model string) (freebuff.Session, error) {
	if _, err := m.Client.EndSession(ctx, token, m.InstanceID); err != nil {
		return freebuff.Session{}, fmt.Errorf("end mismatched freebuff session: %w", err)
	}

	return m.ensureBootstrap(ctx, token, model)
}

func (m *Manager) ensureBootstrap(ctx context.Context, token string, model string) (freebuff.Session, error) {
	call, leader := m.startBootstrap(model)
	if !leader {
		return m.waitForBootstrap(ctx, call, model)
	}

	defer m.finishBootstrap(call)

	current, err := m.Client.GetSession(ctx, token, m.InstanceID)
	if err != nil {
		call.err = fmt.Errorf("recheck freebuff session: %w", err)
		return freebuff.Session{}, call.err
	}

	switch current.Status {
	case freebuff.SessionActive:
		if sessionMatchesModel(current, model) {
			call.session = current
			return current, nil
		}
		if _, err := m.Client.EndSession(ctx, token, m.InstanceID); err != nil {
			call.err = fmt.Errorf("end mismatched freebuff session: %w", err)
			return freebuff.Session{}, call.err
		}
	case freebuff.SessionQueued:
		call.session, call.err = m.pollUntilActive(ctx, token, model, current)
		return call.session, call.err
	case freebuff.SessionDisabled,
		freebuff.SessionCountryBlocked,
		freebuff.SessionModelLocked,
		freebuff.SessionModelUnavailable,
		freebuff.SessionBanned,
		freebuff.SessionRateLimited:
		call.err = &StatusError{Status: current.Status, Message: current.Message}
		return freebuff.Session{}, call.err
	case freebuff.SessionNone, freebuff.SessionEnded, freebuff.SessionSuperseded:
	default:
		call.err = fmt.Errorf("unknown freebuff session status %q", current.Status)
		return freebuff.Session{}, call.err
	}

	session, err := m.Client.StartSession(ctx, token, m.InstanceID, model)
	if err != nil {
		call.err = fmt.Errorf("start freebuff session: %w", err)
		return freebuff.Session{}, call.err
	}

	switch session.Status {
	case freebuff.SessionActive:
		call.session = session
		return session, nil
	case freebuff.SessionQueued:
		call.session, call.err = m.pollUntilActive(ctx, token, model, session)
		return call.session, call.err
	case freebuff.SessionDisabled,
		freebuff.SessionCountryBlocked,
		freebuff.SessionModelLocked,
		freebuff.SessionModelUnavailable,
		freebuff.SessionBanned,
		freebuff.SessionRateLimited:
		call.err = &StatusError{Status: session.Status, Message: session.Message}
		return freebuff.Session{}, call.err
	case freebuff.SessionNone, freebuff.SessionEnded, freebuff.SessionSuperseded:
		call.err = fmt.Errorf("start freebuff session returned non-active status %q", session.Status)
		return freebuff.Session{}, call.err
	default:
		call.err = fmt.Errorf("unknown freebuff session status %q", session.Status)
		return freebuff.Session{}, call.err
	}
}

func (m *Manager) pollUntilActive(ctx context.Context, token string, model string, session freebuff.Session) (freebuff.Session, error) {
	interval := m.PollInterval
	if interval <= 0 {
		interval = defaultPollInterval
	}

	polls := 0
	current := session
	for {
		if err := ctx.Err(); err != nil {
			return freebuff.Session{}, err
		}

		if current.Status != freebuff.SessionQueued {
			return m.resolvePolledSession(ctx, token, model, current)
		}

		if m.MaxPolls > 0 && polls >= m.MaxPolls {
			return freebuff.Session{}, fmt.Errorf("freebuff session did not become active after %d polls", polls)
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return freebuff.Session{}, ctx.Err()
		case <-timer.C:
		}

		polls++
		next, err := m.Client.GetSession(ctx, token, m.InstanceID)
		if err != nil {
			return freebuff.Session{}, fmt.Errorf("poll freebuff session: %w", err)
		}

		current = next
	}
}

func (m *Manager) resolvePolledSession(ctx context.Context, token string, model string, session freebuff.Session) (freebuff.Session, error) {
	switch session.Status {
	case freebuff.SessionActive:
		if !sessionMatchesModel(session, model) {
			if _, err := m.Client.EndSession(ctx, token, m.InstanceID); err != nil {
				return freebuff.Session{}, fmt.Errorf("end mismatched freebuff session: %w", err)
			}

			return freebuff.Session{}, errRetryBootstrap
		}

		return session, nil
	case freebuff.SessionDisabled,
		freebuff.SessionCountryBlocked,
		freebuff.SessionModelLocked,
		freebuff.SessionModelUnavailable,
		freebuff.SessionBanned,
		freebuff.SessionRateLimited:
		return freebuff.Session{}, &StatusError{Status: session.Status, Message: session.Message}
	case freebuff.SessionNone, freebuff.SessionEnded, freebuff.SessionSuperseded:
		return freebuff.Session{}, fmt.Errorf("freebuff session left queue with status %q", session.Status)
	case freebuff.SessionQueued:
		return freebuff.Session{}, fmt.Errorf("freebuff session is still queued")
	default:
		return freebuff.Session{}, fmt.Errorf("unknown freebuff session status %q", session.Status)
	}
}

func (m *Manager) lookupBootstrap() *bootstrapCall {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.bootstrap
}

func (m *Manager) startBootstrap(model string) (*bootstrapCall, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.bootstrap != nil {
		return m.bootstrap, false
	}

	call := &bootstrapCall{done: make(chan struct{}), model: model}
	m.bootstrap = call
	return call, true
}

func (m *Manager) finishBootstrap(call *bootstrapCall) {
	m.mu.Lock()
	if m.bootstrap == call {
		m.bootstrap = nil
	}
	close(call.done)
	m.mu.Unlock()
}

func (m *Manager) waitForBootstrap(ctx context.Context, call *bootstrapCall, model string) (freebuff.Session, error) {
	select {
	case <-ctx.Done():
		return freebuff.Session{}, ctx.Err()
	case <-call.done:
		if isContextError(call.err) {
			if err := ctx.Err(); err != nil {
				return freebuff.Session{}, err
			}

			return freebuff.Session{}, errRetryBootstrap
		}
		if call.err != nil {
			return call.session, call.err
		}
		if !bootstrapMatchesModel(call, model) {
			return freebuff.Session{}, errRetryBootstrap
		}

		return call.session, nil
	}
}

func bootstrapMatchesModel(call *bootstrapCall, model string) bool {
	return model == "" || call.model == model || sessionMatchesModel(call.session, model)
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
