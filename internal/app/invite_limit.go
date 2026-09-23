package app

import (
	"errors"
	"strings"
	"sync"
	"time"
)

// ErrInviteLimit is returned when an inviter or a recipient address has
// hit the invite ceiling for the window. The API maps it to 429.
var ErrInviteLimit = errors.New("app: invite rate limit")

const (
	// A small team onboards a handful of people; twenty a day per human is
	// generous and still caps the blast radius of a compromised account.
	invitePerInviterPerDay = 20
	// Resends for "I didn't get it" need two or three — not twenty. Three a
	// day per address keeps a human from mail-bombing one victim.
	invitePerRecipientPerDay = 3
	inviteWindow           = 24 * time.Hour
)

// inviteLimiter is a per-process sliding-window counter. Postgres does not
// need to know about it — a restart only widens the window, never shrinks a
// legit team's day.
type inviteLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
	now  func() time.Time
}

// allowInvite gates on both dimensions — the inviter's identity and the
// normalized recipient address — and records hits only when both pass, so
// a refused resend never burns the inviter's allowance.
func (l *inviteLimiter) allowInvite(inviter, email string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hits == nil {
		l.hits = map[string][]time.Time{}
	}
	now := l.now
	if now == nil {
		now = time.Now
	}
	inv := l.fresh("i:"+inviter, now())
	rec := l.fresh("r:"+strings.ToLower(strings.TrimSpace(email)), now())
	if len(inv) >= invitePerInviterPerDay || len(rec) >= invitePerRecipientPerDay {
		return false
	}
	l.hits["i:"+inviter] = append(inv, now())
	l.hits["r:"+strings.ToLower(strings.TrimSpace(email))] = append(rec, now())
	return true
}

// fresh prunes expired hits for key in place and returns the live tail.
// Caller holds the lock.
func (l *inviteLimiter) fresh(key string, now time.Time) []time.Time {
	cutoff := now.Add(-inviteWindow)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	l.hits[key] = kept
	return kept
}
