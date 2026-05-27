package session

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ferdiunal/freebuff-proxy/internal/credentials"
	"github.com/ferdiunal/freebuff-proxy/internal/freebuff"
)

// Bu testler, oturum yöneticisinin yeniden kullanım, kuyruk bekleme ve eşzamanlı başlatma davranışlarını doğrular.
//
// ## Kullanım örneği
//
// ```bash
// go test ./internal/session
// go test ./internal/session -run TestManager
// ```
func TestManagerEnsureActiveReusesActiveSession(t *testing.T) {
	t.Parallel()

	client := &stubClient{
		getSessions: []freebuff.Session{{Status: freebuff.SessionActive, InstanceID: "instance-1", Model: "claude-sonnet"}},
	}
	manager := NewManager(stubStore{cred: credentials.Credential{AuthToken: "token-1"}}, client, "instance-1")
	manager.PollInterval = time.Millisecond

	session, err := manager.EnsureActive(context.Background(), "claude-sonnet")
	if err != nil {
		t.Fatalf("EnsureActive hata döndürdü: %v", err)
	}

	if session.Status != freebuff.SessionActive {
		t.Fatalf("status = %q, beklenen %q", session.Status, freebuff.SessionActive)
	}

	if client.getCalls != 1 {
		t.Fatalf("GetSession çağrısı = %d, beklenen %d", client.getCalls, 1)
	}

	if client.startCalls != 0 {
		t.Fatalf("StartSession çağrısı = %d, beklenen %d", client.startCalls, 0)
	}
}

func TestManagerEnsureActiveReusesActiveSessionWithCanonicalModelFields(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		session freebuff.Session
		model   string
	}{
		{
			name:    "short_requested_alias_matches_canonical_model",
			session: freebuff.Session{Status: freebuff.SessionActive, InstanceID: "instance-1", Model: "deepseek/deepseek-v4-pro"},
			model:   "deepseek-v4-pro",
		},
		{
			name:    "current_model_matches",
			session: freebuff.Session{Status: freebuff.SessionActive, InstanceID: "instance-1", CurrentModel: "deepseek/deepseek-v4-pro"},
			model:   "deepseek/deepseek-v4-pro",
		},
		{
			name:    "requested_model_matches",
			session: freebuff.Session{Status: freebuff.SessionActive, InstanceID: "instance-1", RequestedModel: "deepseek-v4-pro"},
			model:   "deepseek/deepseek-v4-pro",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := &stubClient{getSessions: []freebuff.Session{testCase.session}}
			manager := NewManager(stubStore{cred: credentials.Credential{AuthToken: "token-1"}}, client, "instance-1")
			manager.PollInterval = time.Millisecond

			session, err := manager.EnsureActive(context.Background(), testCase.model)
			if err != nil {
				t.Fatalf("EnsureActive hata döndürdü: %v", err)
			}

			if session.InstanceID != "instance-1" {
				t.Fatalf("InstanceID = %q, beklenen instance-1", session.InstanceID)
			}
			if client.endCalls != 0 {
				t.Fatalf("EndSession çağrısı = %d, beklenen %d", client.endCalls, 0)
			}
			if client.startCalls != 0 {
				t.Fatalf("StartSession çağrısı = %d, beklenen %d", client.startCalls, 0)
			}
		})
	}
}

func TestManagerEnsureActiveRestartsActiveSessionWithDifferentModel(t *testing.T) {
	t.Parallel()

	client := &stubClient{
		getSessions: []freebuff.Session{
			{Status: freebuff.SessionActive, InstanceID: "instance-1", Model: "minimax/minimax-m2.7"},
			{Status: freebuff.SessionNone},
		},
		startSessions: []freebuff.Session{{Status: freebuff.SessionActive, InstanceID: "instance-1", Model: "deepseek/deepseek-v4-pro"}},
	}
	manager := NewManager(stubStore{cred: credentials.Credential{AuthToken: "token-1"}}, client, "instance-1")
	manager.PollInterval = time.Millisecond

	session, err := manager.EnsureActive(context.Background(), "deepseek/deepseek-v4-pro")
	if err != nil {
		t.Fatalf("EnsureActive hata döndürdü: %v", err)
	}

	if session.Model != "deepseek/deepseek-v4-pro" {
		t.Fatalf("model = %q, beklenen deepseek/deepseek-v4-pro", session.Model)
	}
	if client.endCalls != 1 {
		t.Fatalf("EndSession çağrısı = %d, beklenen %d", client.endCalls, 1)
	}
	if client.startCalls != 1 {
		t.Fatalf("StartSession çağrısı = %d, beklenen %d", client.startCalls, 1)
	}
	if len(client.startModels) != 1 || client.startModels[0] != "deepseek/deepseek-v4-pro" {
		t.Fatalf("startModels = %#v, beklenen deepseek/deepseek-v4-pro", client.startModels)
	}
}

func TestManagerEnsureActiveRestartsActiveSessionWithoutModel(t *testing.T) {
	t.Parallel()

	client := &stubClient{
		getSessions: []freebuff.Session{
			{Status: freebuff.SessionActive, InstanceID: "instance-1"},
			{Status: freebuff.SessionNone},
		},
		startSessions: []freebuff.Session{{Status: freebuff.SessionActive, InstanceID: "instance-1", Model: "deepseek/deepseek-v4-pro"}},
	}
	manager := NewManager(stubStore{cred: credentials.Credential{AuthToken: "token-1"}}, client, "instance-1")
	manager.PollInterval = time.Millisecond

	session, err := manager.EnsureActive(context.Background(), "deepseek/deepseek-v4-pro")
	if err != nil {
		t.Fatalf("EnsureActive hata döndürdü: %v", err)
	}

	if session.Model != "deepseek/deepseek-v4-pro" {
		t.Fatalf("model = %q, beklenen deepseek/deepseek-v4-pro", session.Model)
	}
	if client.endCalls != 1 {
		t.Fatalf("EndSession çağrısı = %d, beklenen %d", client.endCalls, 1)
	}
	if client.startCalls != 1 {
		t.Fatalf("StartSession çağrısı = %d, beklenen %d", client.startCalls, 1)
	}
}

func TestManagerEnsureActiveRestartsMismatchedBootstrapRecheck(t *testing.T) {
	t.Parallel()

	client := &stubClient{
		getSessions: []freebuff.Session{
			{Status: freebuff.SessionNone},
			{Status: freebuff.SessionActive, InstanceID: "instance-1", Model: "minimax/minimax-m2.7"},
		},
		startSessions: []freebuff.Session{{Status: freebuff.SessionActive, InstanceID: "instance-1", Model: "deepseek/deepseek-v4-pro"}},
	}
	manager := NewManager(stubStore{cred: credentials.Credential{AuthToken: "token-1"}}, client, "instance-1")
	manager.PollInterval = time.Millisecond

	session, err := manager.EnsureActive(context.Background(), "deepseek/deepseek-v4-pro")
	if err != nil {
		t.Fatalf("EnsureActive hata döndürdü: %v", err)
	}

	if session.Model != "deepseek/deepseek-v4-pro" {
		t.Fatalf("model = %q, beklenen deepseek/deepseek-v4-pro", session.Model)
	}
	if client.endCalls != 1 {
		t.Fatalf("EndSession çağrısı = %d, beklenen %d", client.endCalls, 1)
	}
	if client.startCalls != 1 {
		t.Fatalf("StartSession çağrısı = %d, beklenen %d", client.startCalls, 1)
	}
}

func TestManagerEnsureActiveStartsSessionWhenMissing(t *testing.T) {
	t.Parallel()

	client := &stubClient{
		getSessions: []freebuff.Session{
			{Status: freebuff.SessionNone},
			{Status: freebuff.SessionNone},
		},
		startSessions: []freebuff.Session{{Status: freebuff.SessionActive, InstanceID: "instance-1"}},
	}
	manager := NewManager(stubStore{cred: credentials.Credential{AuthToken: "token-1"}}, client, "instance-1")
	manager.PollInterval = time.Millisecond

	session, err := manager.EnsureActive(context.Background(), "claude-sonnet")
	if err != nil {
		t.Fatalf("EnsureActive hata döndürdü: %v", err)
	}

	if session.Status != freebuff.SessionActive {
		t.Fatalf("status = %q, beklenen %q", session.Status, freebuff.SessionActive)
	}

	if client.startCalls != 1 {
		t.Fatalf("StartSession çağrısı = %d, beklenen %d", client.startCalls, 1)
	}
}

func TestManagerEnsureActiveStartsSessionWhenEndedOrSuperseded(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		status freebuff.SessionStatus
	}{
		{name: "ended", status: freebuff.SessionEnded},
		{name: "superseded", status: freebuff.SessionSuperseded},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			client := &stubClient{
				getSessions: []freebuff.Session{
					{Status: testCase.status},
					{Status: testCase.status},
				},
				startSessions: []freebuff.Session{{Status: freebuff.SessionActive, InstanceID: "instance-1"}},
			}
			manager := NewManager(stubStore{cred: credentials.Credential{AuthToken: "token-1"}}, client, "instance-1")

			session, err := manager.EnsureActive(context.Background(), "claude-sonnet")
			if err != nil {
				t.Fatalf("EnsureActive hata döndürdü: %v", err)
			}

			if session.Status != freebuff.SessionActive {
				t.Fatalf("status = %q, beklenen %q", session.Status, freebuff.SessionActive)
			}

			if client.startCalls != 1 {
				t.Fatalf("StartSession çağrısı = %d, beklenen %d", client.startCalls, 1)
			}
		})
	}
}

func TestManagerEnsureActivePollsAfterStartReturnsQueued(t *testing.T) {
	t.Parallel()

	client := &stubClient{
		getSessions: []freebuff.Session{
			{Status: freebuff.SessionNone},
			{Status: freebuff.SessionNone},
			{Status: freebuff.SessionActive, InstanceID: "instance-1", Model: "claude-sonnet"},
		},
		startSessions: []freebuff.Session{{Status: freebuff.SessionQueued, Position: 1}},
	}
	manager := NewManager(stubStore{cred: credentials.Credential{AuthToken: "token-1"}}, client, "instance-1")
	manager.PollInterval = time.Millisecond

	session, err := manager.EnsureActive(context.Background(), "claude-sonnet")
	if err != nil {
		t.Fatalf("EnsureActive hata döndürdü: %v", err)
	}

	if session.Status != freebuff.SessionActive {
		t.Fatalf("status = %q, beklenen %q", session.Status, freebuff.SessionActive)
	}

	if client.startCalls != 1 {
		t.Fatalf("StartSession çağrısı = %d, beklenen %d", client.startCalls, 1)
	}

	if client.getCalls != 3 {
		t.Fatalf("GetSession çağrısı = %d, beklenen %d", client.getCalls, 3)
	}
}

func TestManagerEnsureActivePollsQueuedSessionUntilActive(t *testing.T) {
	t.Parallel()

	client := &stubClient{
		getSessions: []freebuff.Session{
			{Status: freebuff.SessionQueued, Position: 2},
			{Status: freebuff.SessionQueued, Position: 1},
			{Status: freebuff.SessionActive, InstanceID: "instance-1", Model: "claude-sonnet"},
		},
	}
	manager := NewManager(stubStore{cred: credentials.Credential{AuthToken: "token-1"}}, client, "instance-1")
	manager.PollInterval = time.Millisecond

	session, err := manager.EnsureActive(context.Background(), "claude-sonnet")
	if err != nil {
		t.Fatalf("EnsureActive hata döndürdü: %v", err)
	}

	if session.Status != freebuff.SessionActive {
		t.Fatalf("status = %q, beklenen %q", session.Status, freebuff.SessionActive)
	}

	if client.getCalls != 3 {
		t.Fatalf("GetSession çağrısı = %d, beklenen %d", client.getCalls, 3)
	}

	if client.startCalls != 0 {
		t.Fatalf("StartSession çağrısı = %d, beklenen %d", client.startCalls, 0)
	}
}

func TestManagerEnsureActiveRestartsQueuedSessionWithDifferentModel(t *testing.T) {
	t.Parallel()

	client := &stubClient{
		getSessions: []freebuff.Session{
			{Status: freebuff.SessionQueued, Position: 2},
			{Status: freebuff.SessionQueued, Position: 1},
			{Status: freebuff.SessionActive, InstanceID: "instance-1", Model: "minimax/minimax-m2.7"},
			{Status: freebuff.SessionNone},
			{Status: freebuff.SessionNone},
		},
		startSessions: []freebuff.Session{{Status: freebuff.SessionActive, InstanceID: "instance-1", Model: "deepseek/deepseek-v4-pro"}},
	}
	manager := NewManager(stubStore{cred: credentials.Credential{AuthToken: "token-1"}}, client, "instance-1")
	manager.PollInterval = time.Millisecond

	session, err := manager.EnsureActive(context.Background(), "deepseek/deepseek-v4-pro")
	if err != nil {
		t.Fatalf("EnsureActive hata döndürdü: %v", err)
	}

	if session.Model != "deepseek/deepseek-v4-pro" {
		t.Fatalf("model = %q, beklenen deepseek/deepseek-v4-pro", session.Model)
	}
	if client.endCalls != 1 {
		t.Fatalf("EndSession çağrısı = %d, beklenen %d", client.endCalls, 1)
	}
	if client.startCalls != 1 {
		t.Fatalf("StartSession çağrısı = %d, beklenen %d", client.startCalls, 1)
	}
}

func TestManagerEnsureActiveRetriesStartedQueuedSessionWithDifferentModel(t *testing.T) {
	t.Parallel()

	client := &stubClient{
		getSessions: []freebuff.Session{
			{Status: freebuff.SessionNone},
			{Status: freebuff.SessionNone},
			{Status: freebuff.SessionActive, InstanceID: "instance-1", Model: "minimax/minimax-m2.7"},
			{Status: freebuff.SessionNone},
			{Status: freebuff.SessionNone},
		},
		startSessions: []freebuff.Session{
			{Status: freebuff.SessionQueued, Position: 1},
			{Status: freebuff.SessionActive, InstanceID: "instance-1", Model: "deepseek/deepseek-v4-pro"},
		},
	}
	manager := NewManager(stubStore{cred: credentials.Credential{AuthToken: "token-1"}}, client, "instance-1")
	manager.PollInterval = time.Millisecond

	session, err := manager.EnsureActive(context.Background(), "deepseek/deepseek-v4-pro")
	if err != nil {
		t.Fatalf("EnsureActive hata döndürdü: %v", err)
	}

	if session.Model != "deepseek/deepseek-v4-pro" {
		t.Fatalf("model = %q, beklenen deepseek/deepseek-v4-pro", session.Model)
	}
	if client.endCalls != 1 {
		t.Fatalf("EndSession çağrısı = %d, beklenen %d", client.endCalls, 1)
	}
	if client.startCalls != 2 {
		t.Fatalf("StartSession çağrısı = %d, beklenen %d", client.startCalls, 2)
	}
}

func TestManagerEnsureActiveQueuedSessionHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	client := &stubClient{
		getSessions: []freebuff.Session{{Status: freebuff.SessionQueued, Position: 5}},
	}
	manager := NewManager(stubStore{cred: credentials.Credential{AuthToken: "token-1"}}, client, "instance-1")
	manager.PollInterval = 25 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	_, err := manager.EnsureActive(ctx, "claude-sonnet")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("hata = %v, beklenen %v", err, context.DeadlineExceeded)
	}

	if client.startCalls != 0 {
		t.Fatalf("StartSession çağrısı = %d, beklenen %d", client.startCalls, 0)
	}
}

func TestManagerEnsureActiveConcurrentCallsShareBootstrap(t *testing.T) {
	t.Parallel()

	client := &stubClient{
		getSessions: []freebuff.Session{
			{Status: freebuff.SessionNone},
			{Status: freebuff.SessionNone},
			{Status: freebuff.SessionNone},
		},
		startSessions:     []freebuff.Session{{Status: freebuff.SessionActive, InstanceID: "instance-1", Model: "claude-sonnet"}},
		sessionAfterStart: freebuff.Session{Status: freebuff.SessionActive, InstanceID: "instance-1", Model: "claude-sonnet"},
		startBlock:        make(chan struct{}),
		startEntered:      make(chan struct{}, 1),
	}
	manager := NewManager(stubStore{cred: credentials.Credential{AuthToken: "token-1"}}, client, "instance-1")
	manager.PollInterval = time.Millisecond

	var wg sync.WaitGroup
	results := make(chan error, 2)

	callEnsureActive := func() {
		defer wg.Done()
		_, err := manager.EnsureActive(context.Background(), "claude-sonnet")
		results <- err
	}

	wg.Add(2)
	go callEnsureActive()
	go callEnsureActive()

	select {
	case <-client.startEntered:
	case <-time.After(time.Second):
		t.Fatal("StartSession beklenen sürede başlamadı")
	}

	close(client.startBlock)
	wg.Wait()
	close(results)

	for err := range results {
		if err != nil {
			t.Fatalf("EnsureActive hata döndürdü: %v", err)
		}
	}

	if client.startCalls != 1 {
		t.Fatalf("StartSession çağrısı = %d, beklenen %d", client.startCalls, 1)
	}
}

func TestManagerEnsureActiveConcurrentCallsWithDifferentModelsRetryBootstrap(t *testing.T) {
	t.Parallel()

	client := &stubClient{
		getSessions: []freebuff.Session{
			{Status: freebuff.SessionNone},
			{Status: freebuff.SessionNone},
			{Status: freebuff.SessionNone},
			{Status: freebuff.SessionNone},
		},
		startSessions: []freebuff.Session{
			{Status: freebuff.SessionActive, InstanceID: "instance-1", Model: "claude-sonnet"},
			{Status: freebuff.SessionActive, InstanceID: "instance-1", Model: "claude-opus"},
		},
		sessionAfterStart: freebuff.Session{Status: freebuff.SessionActive, InstanceID: "instance-1", Model: "claude-sonnet"},
		startBlock:        make(chan struct{}),
		startEntered:      make(chan struct{}, 1),
	}
	manager := NewManager(stubStore{cred: credentials.Credential{AuthToken: "token-1"}}, client, "instance-1")
	manager.PollInterval = time.Millisecond

	leaderResults := make(chan ensureActiveResult, 1)
	go func() {
		session, err := manager.EnsureActive(context.Background(), "claude-sonnet")
		leaderResults <- ensureActiveResult{session: session, err: err}
	}()

	select {
	case <-client.startEntered:
	case <-time.After(time.Second):
		t.Fatal("StartSession beklenen sürede başlamadı")
	}

	followerResults := make(chan ensureActiveResult, 1)
	go func() {
		session, err := manager.EnsureActive(context.Background(), "claude-opus")
		followerResults <- ensureActiveResult{session: session, err: err}
	}()

	close(client.startBlock)

	leader := <-leaderResults
	if leader.err != nil {
		t.Fatalf("lider EnsureActive hata döndürdü: %v", leader.err)
	}
	if leader.session.Model != "claude-sonnet" {
		t.Fatalf("lider model = %q, beklenen claude-sonnet", leader.session.Model)
	}

	follower := <-followerResults
	if follower.err != nil {
		t.Fatalf("follower EnsureActive hata döndürdü: %v", follower.err)
	}
	if follower.session.Model != "claude-opus" {
		t.Fatalf("follower model = %q, beklenen claude-opus", follower.session.Model)
	}

	if client.endCalls != 1 {
		t.Fatalf("EndSession çağrısı = %d, beklenen %d", client.endCalls, 1)
	}
	if client.startCalls != 2 {
		t.Fatalf("StartSession çağrısı = %d, beklenen %d", client.startCalls, 2)
	}
}

func TestManagerEnsureActiveFollowerRetriesAfterLeaderContextCancellation(t *testing.T) {
	t.Parallel()

	client := newLeaderCancelRetryClient()
	manager := NewManager(stubStore{cred: credentials.Credential{AuthToken: "token-1"}}, client, "instance-1")
	manager.PollInterval = time.Millisecond

	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	defer cancelLeader()

	leaderErrors := make(chan error, 1)
	go func() {
		_, err := manager.EnsureActive(leaderCtx, "leader-model")
		leaderErrors <- err
	}()

	select {
	case <-client.firstStartEntered:
	case <-time.After(time.Second):
		t.Fatal("ilk StartSession beklenen sürede başlamadı")
	}

	followerResults := make(chan ensureActiveResult, 1)
	go func() {
		session, err := manager.EnsureActive(context.Background(), "follower-model")
		followerResults <- ensureActiveResult{session: session, err: err}
	}()

	select {
	case <-client.followerInitialGet:
	case <-time.After(time.Second):
		t.Fatal("follower ilk GetSession çağrısına ulaşmadı")
	}

	cancelLeader()

	select {
	case <-client.firstStartContextDone:
	case <-time.After(time.Second):
		t.Fatal("lider context iptali StartSession tarafından görülmedi")
	}

	close(client.releaseFirstStart)

	select {
	case err := <-leaderErrors:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("lider hata = %v, beklenen %v", err, context.Canceled)
		}
	case <-time.After(time.Second):
		t.Fatal("lider beklenen sürede tamamlanmadı")
	}

	select {
	case result := <-followerResults:
		if result.err != nil {
			t.Fatalf("follower hata döndürdü: %v", result.err)
		}

		if result.session.Status != freebuff.SessionActive {
			t.Fatalf("follower status = %q, beklenen %q", result.session.Status, freebuff.SessionActive)
		}
	case <-time.After(time.Second):
		t.Fatal("follower beklenen sürede tamamlanmadı")
	}

	if client.startCalls != 2 {
		t.Fatalf("StartSession çağrısı = %d, beklenen %d", client.startCalls, 2)
	}
}

func TestManagerEnsureActiveInitialUnknownStatusDoesNotStart(t *testing.T) {
	t.Parallel()

	client := &stubClient{
		getSessions: []freebuff.Session{{Status: freebuff.SessionStatus("weird")}},
	}
	manager := NewManager(stubStore{cred: credentials.Credential{AuthToken: "token-1"}}, client, "instance-1")

	_, err := manager.EnsureActive(context.Background(), "model")
	if err == nil {
		t.Fatal("EnsureActive hata döndürmedi")
	}

	if !strings.Contains(err.Error(), "weird") {
		t.Fatalf("hata = %v, status bilgisini içermesi bekleniyordu", err)
	}

	if client.startCalls != 0 {
		t.Fatalf("StartSession çağrısı = %d, beklenen %d", client.startCalls, 0)
	}
}

func TestManagerEnsureActiveRecheckUnknownStatusDoesNotStart(t *testing.T) {
	t.Parallel()

	client := &stubClient{
		getSessions: []freebuff.Session{
			{Status: freebuff.SessionNone},
			{Status: freebuff.SessionStatus("unknown_recheck")},
		},
	}
	manager := NewManager(stubStore{cred: credentials.Credential{AuthToken: "token-1"}}, client, "instance-1")

	_, err := manager.EnsureActive(context.Background(), "claude-sonnet")
	if err == nil {
		t.Fatal("EnsureActive hata döndürmedi")
	}

	if !strings.Contains(err.Error(), "unknown_recheck") {
		t.Fatalf("hata = %v, status bilgisini içermesi bekleniyordu", err)
	}

	if client.startCalls != 0 {
		t.Fatalf("StartSession çağrısı = %d, beklenen %d", client.startCalls, 0)
	}

	if client.getCalls != 2 {
		t.Fatalf("GetSession çağrısı = %d, beklenen %d", client.getCalls, 2)
	}
}

func TestManagerEnsureActiveStartUnknownStatusReturnsError(t *testing.T) {
	t.Parallel()

	client := &stubClient{
		getSessions: []freebuff.Session{
			{Status: freebuff.SessionNone},
			{Status: freebuff.SessionNone},
		},
		startSessions: []freebuff.Session{{Status: freebuff.SessionStatus("unknown_start")}},
	}
	manager := NewManager(stubStore{cred: credentials.Credential{AuthToken: "token-1"}}, client, "instance-1")

	_, err := manager.EnsureActive(context.Background(), "claude-sonnet")
	if err == nil {
		t.Fatal("EnsureActive hata döndürmedi")
	}

	if !strings.Contains(err.Error(), "unknown_start") {
		t.Fatalf("hata = %v, status bilgisini içermesi bekleniyordu", err)
	}

	if client.startCalls != 1 {
		t.Fatalf("StartSession çağrısı = %d, beklenen %d", client.startCalls, 1)
	}
}

func TestManagerEnsureActiveReturnsTerminalStatusErrorsWithoutStart(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		status freebuff.SessionStatus
	}{
		{name: "disabled", status: freebuff.SessionDisabled},
		{name: "country_blocked", status: freebuff.SessionCountryBlocked},
		{name: "model_locked", status: freebuff.SessionModelLocked},
		{name: "model_unavailable", status: freebuff.SessionModelUnavailable},
		{name: "banned", status: freebuff.SessionBanned},
		{name: "rate_limited", status: freebuff.SessionRateLimited},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			client := &stubClient{
				getSessions: []freebuff.Session{{Status: testCase.status, Message: "erişim kapalı"}},
			}
			manager := NewManager(stubStore{cred: credentials.Credential{AuthToken: "token-1"}}, client, "instance-1")

			_, err := manager.EnsureActive(context.Background(), "claude-sonnet")
			if err == nil {
				t.Fatal("EnsureActive hata döndürmedi")
			}

			var statusErr *StatusError
			if !errors.As(err, &statusErr) {
				t.Fatalf("hata tipi = %T, beklenen *StatusError", err)
			}

			if statusErr.Status != testCase.status {
				t.Fatalf("status = %q, beklenen %q", statusErr.Status, testCase.status)
			}

			if client.startCalls != 0 {
				t.Fatalf("StartSession çağrısı = %d, beklenen %d", client.startCalls, 0)
			}

			if client.getCalls != 1 {
				t.Fatalf("GetSession çağrısı = %d, beklenen %d", client.getCalls, 1)
			}
		})
	}
}

func TestManagerEnsureActivePropagatesStoreLoadError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("kimlik bulunamadı")
	manager := NewManager(stubStore{err: expectedErr}, &stubClient{}, "instance-1")

	_, err := manager.EnsureActive(context.Background(), "claude-sonnet")
	if !errors.Is(err, expectedErr) {
		t.Fatalf("hata = %v, beklenen %v", err, expectedErr)
	}
}

type ensureActiveResult struct {
	session freebuff.Session
	err     error
}

type leaderCancelRetryClient struct {
	mu                    sync.Mutex
	getCalls              int
	startCalls            int
	firstStartEntered     chan struct{}
	followerInitialGet    chan struct{}
	firstStartContextDone chan struct{}
	releaseFirstStart     chan struct{}
}

func newLeaderCancelRetryClient() *leaderCancelRetryClient {
	return &leaderCancelRetryClient{
		firstStartEntered:     make(chan struct{}),
		followerInitialGet:    make(chan struct{}),
		firstStartContextDone: make(chan struct{}),
		releaseFirstStart:     make(chan struct{}),
	}
}

func (c *leaderCancelRetryClient) GetSession(ctx context.Context, token string, instanceID string) (freebuff.Session, error) {
	c.mu.Lock()
	c.getCalls++
	getCalls := c.getCalls
	c.mu.Unlock()

	if getCalls == 3 {
		close(c.followerInitialGet)
	}

	return freebuff.Session{Status: freebuff.SessionNone}, nil
}

func (c *leaderCancelRetryClient) StartSession(ctx context.Context, token string, instanceID string, model string) (freebuff.Session, error) {
	c.mu.Lock()
	c.startCalls++
	startCalls := c.startCalls
	c.mu.Unlock()

	if startCalls == 1 {
		close(c.firstStartEntered)
		<-ctx.Done()
		close(c.firstStartContextDone)
		<-c.releaseFirstStart
		return freebuff.Session{}, ctx.Err()
	}

	return freebuff.Session{Status: freebuff.SessionActive, InstanceID: instanceID, Model: model}, nil
}

func (c *leaderCancelRetryClient) EndSession(ctx context.Context, token string, instanceID string) (freebuff.Session, error) {
	return freebuff.Session{}, nil
}

type stubStore struct {
	cred credentials.Credential
	err  error
}

func (s stubStore) Load(ctx context.Context) (credentials.Credential, error) {
	if s.err != nil {
		return credentials.Credential{}, s.err
	}

	return s.cred, nil
}

func (s stubStore) Save(ctx context.Context, cred credentials.Credential) error {
	return nil
}

type stubClient struct {
	mu                sync.Mutex
	getSessions       []freebuff.Session
	startSessions     []freebuff.Session
	sessionAfterStart freebuff.Session
	started           bool
	getCalls          int
	startCalls        int
	endCalls          int
	startModels       []string
	startBlock        chan struct{}
	startEntered      chan struct{}
}

func (c *stubClient) GetSession(ctx context.Context, token string, instanceID string) (freebuff.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	index := c.getCalls
	c.getCalls++

	if c.started && c.sessionAfterStart.Status != "" {
		return c.sessionAfterStart, nil
	}

	if index >= len(c.getSessions) {
		return freebuff.Session{}, errors.New("beklenmeyen GetSession çağrısı")
	}

	return c.getSessions[index], nil
}

func (c *stubClient) StartSession(ctx context.Context, token string, instanceID string, model string) (freebuff.Session, error) {
	c.mu.Lock()
	index := c.startCalls
	c.startCalls++
	block := c.startBlock
	entered := c.startEntered
	if entered == nil && block != nil {
		entered = make(chan struct{}, 1)
		c.startEntered = entered
	}
	c.mu.Unlock()

	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
	}

	if block != nil {
		select {
		case <-ctx.Done():
			return freebuff.Session{}, ctx.Err()
		case <-block:
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.startModels = append(c.startModels, model)

	if index >= len(c.startSessions) {
		return freebuff.Session{}, errors.New("beklenmeyen StartSession çağrısı")
	}

	c.started = true
	return c.startSessions[index], nil
}

func (c *stubClient) EndSession(ctx context.Context, token string, instanceID string) (freebuff.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.endCalls++
	c.started = false

	return freebuff.Session{}, nil
}
