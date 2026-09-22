package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/VortexNYC/veil/internal/id"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store"
)

const (
	SessionPrefix     = "ses_"
	sessionSecretLen  = 32
	SessionTTLDefault = 15 * time.Minute
	SessionTTLMax     = time.Hour
)

var (
	ErrForbidden        = errors.New("app: forbidden")
	ErrSessionExpired   = errors.New("app: session expired")
	ErrSessionRevoked   = errors.New("app: session revoked")
	ErrSessionExhausted = errors.New("app: session uses exhausted")
)

func IsSessionToken(raw string) bool {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, SessionPrefix) {
		return false
	}
	rest := strings.TrimPrefix(raw, SessionPrefix)
	if len(rest) != sessionSecretLen*2 {
		return false
	}
	_, err := hex.DecodeString(rest)
	return err == nil
}

func sessionHash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func newSessionToken() (string, error) {
	var b [sessionSecretLen]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return SessionPrefix + hex.EncodeToString(b[:]), nil
}

func (a *App) PrincipalFromSession(rawToken string) (protocol.Principal, error) {
	raw := strings.TrimSpace(rawToken)
	if !IsSessionToken(raw) {
		return protocol.Principal{}, fmt.Errorf("app: not a session")
	}
	now := time.Now()
	sess, err := a.Store.SessionByHash(sessionHash(raw))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return protocol.Principal{}, fmt.Errorf("app: unauthorized")
		}
		return protocol.Principal{}, err
	}
	if sess.RevokedAt != nil {
		return protocol.Principal{}, ErrSessionRevoked
	}
	if !sess.ExpiresAt.After(now) {
		return protocol.Principal{}, ErrSessionExpired
	}
	if sess.MaxUses > 0 && sess.Uses >= sess.MaxUses {
		return protocol.Principal{}, ErrSessionExhausted
	}
	agent, err := a.Store.Agent(sess.AgentID)
	if err != nil {
		return protocol.Principal{}, err
	}
	if agent.RevokedAt != nil {
		return protocol.Principal{}, ErrAgentRevoked
	}
	return agent, nil
}

func (a *App) CreateSession(actor protocol.Principal, agentID string, ttl time.Duration, maxUses int) (protocol.Session, string, error) {
	ok, err := a.ownsVault(actor)
	if err != nil {
		return protocol.Session{}, "", err
	}
	if !ok {
		return protocol.Session{}, "", ErrForbidden
	}
	if ttl <= 0 {
		ttl = SessionTTLDefault
	}
	if ttl > SessionTTLMax {
		return protocol.Session{}, "", fmt.Errorf("app: ttl exceeds 1h")
	}
	if maxUses < 0 {
		return protocol.Session{}, "", fmt.Errorf("app: max_uses cannot be negative")
	}
	agent, err := a.Store.Agent(agentID)
	if err != nil {
		return protocol.Session{}, "", err
	}
	if agent.OrgID != actor.OrgID {
		return protocol.Session{}, "", ErrForbidden
	}
	if agent.RevokedAt != nil {
		return protocol.Session{}, "", ErrForbidden
	}
	token, err := newSessionToken()
	if err != nil {
		return protocol.Session{}, "", err
	}
	sid, err := id.NewSession()
	if err != nil {
		return protocol.Session{}, "", err
	}
	now := time.Now().UTC()
	sess := protocol.Session{
		ID:        sid,
		OrgID:     actor.OrgID,
		AgentID:   agent.ID,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl).UTC(),
		TTL:       int64(ttl.Seconds()),
		MaxTTL:    int64(SessionTTLMax.Seconds()),
		MaxUses:   maxUses,
	}
	if err := a.Store.PutSession(sess, sessionHash(token)); err != nil {
		return protocol.Session{}, "", err
	}
	return sess, token, nil
}

func (a *App) RevokeSession(actor protocol.Principal, id string) (protocol.Session, error) {
	ok, err := a.ownsVault(actor)
	if err != nil {
		return protocol.Session{}, err
	}
	if !ok {
		return protocol.Session{}, ErrForbidden
	}
	if err := a.Store.RevokeSession(id, time.Now()); err != nil {
		return protocol.Session{}, err
	}
	return a.Store.SessionByID(id)
}

func (a *App) RenewSession(actor protocol.Principal, id string) (protocol.Session, error) {
	ok, err := a.ownsVault(actor)
	if err != nil {
		return protocol.Session{}, err
	}
	if !ok {
		return protocol.Session{}, ErrForbidden
	}
	return a.Store.RenewSession(id, time.Now())
}

func (a *App) ListSessions(actor protocol.Principal) ([]protocol.Session, error) {
	ok, err := a.ownsVault(actor)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrForbidden
	}
	all, err := a.Store.ListSessions()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]protocol.Session, 0, len(all))
	for _, sess := range all {
		if sess.OrgID == actor.OrgID && sess.ExpiresAt.After(now) {
			out = append(out, sess)
		}
	}
	return out, nil
}
