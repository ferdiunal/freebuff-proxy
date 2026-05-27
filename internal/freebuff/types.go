package freebuff

import "time"

// SessionStatus, Freebuff oturum API'sinden dönen yaşam döngüsü durumlarını tanımlar.
//
// ## Kullanım örneği
//
// ```go
//
//	if session.Status == freebuff.SessionActive {
//		fmt.Println("oturum aktif")
//	}
//
// ```
type SessionStatus string

const (
	SessionDisabled         SessionStatus = "disabled"
	SessionNone             SessionStatus = "none"
	SessionQueued           SessionStatus = "queued"
	SessionActive           SessionStatus = "active"
	SessionEnded            SessionStatus = "ended"
	SessionSuperseded       SessionStatus = "superseded"
	SessionCountryBlocked   SessionStatus = "country_blocked"
	SessionModelLocked      SessionStatus = "model_locked"
	SessionModelUnavailable SessionStatus = "model_unavailable"
	SessionBanned           SessionStatus = "banned"
	SessionRateLimited      SessionStatus = "rate_limited"
)

// Session, proxy'nin Freebuff oturum kararları için ihtiyaç duyduğu alanları taşır.
//
// ## Kullanım örneği
//
// ```go
// session, err := client.GetSession(ctx, token, "instance-1")
//
//	if err != nil {
//		return err
//	}
//
// fmt.Println(session.Status, session.Model)
// ```
type Session struct {
	Status         SessionStatus `json:"status"`
	AccessTier     string        `json:"accessTier,omitempty"`
	InstanceID     string        `json:"instanceId,omitempty"`
	Model          string        `json:"model,omitempty"`
	Position       int           `json:"position,omitempty"`
	QueueDepth     int           `json:"queueDepth,omitempty"`
	AdmittedAt     time.Time     `json:"admittedAt,omitempty"`
	ExpiresAt      time.Time     `json:"expiresAt,omitempty"`
	RemainingMS    int64         `json:"remainingMs,omitempty"`
	Message        string        `json:"message,omitempty"`
	RequestedModel string        `json:"requestedModel,omitempty"`
	CurrentModel   string        `json:"currentModel,omitempty"`
	RetryAfterMS   int64         `json:"retryAfterMs,omitempty"`
}

// APIError, başarısız Freebuff yanıtlarını HTTP durumu ve sunucu mesajıyla temsil eder.
//
// ## Kullanım örneği
//
// ```go
// var apiErr *freebuff.APIError
//
//	if errors.As(err, &apiErr) {
//		fmt.Println(apiErr.StatusCode, apiErr.Message)
//	}
//
// ```
type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}

	if e.Message != "" {
		return e.Message
	}

	return e.Code
}
