// Package health is the vault health report — weak, reused, and breached
// passwords. It analyzes plaintext inside the process but never emits it:
// findings carry item metadata and counts only. Breach checks use the
// HaveIBeenPwned range API with k-anonymity — the origin or CLI sends the
// first 5 chars of the SHA-1 and receives suffixes; the full hash and the
// password never leave the process.
package health

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Entry is one scorable item. Token is plaintext — internal use only.
type Entry struct {
	ID    string
	Name  string
	Kind  string
	URI   string
	Login string
	Token string
}

// Finding is one flagged item. No secret material — the scrub contract.
type Finding struct {
	ItemID string   `json:"item_id"`
	Name   string   `json:"name"`
	Kind   string   `json:"kind"`
	URI    string   `json:"uri,omitempty"`
	Weak   []string `json:"weak,omitempty"`
	// Reused is the size of the token-sharing group this item belongs to.
	// Zero means unique.
	Reused int `json:"reused,omitempty"`
	// Pwned is the HIBP occurrence count. -1 means not checked or unknown.
	Pwned int `json:"pwned"`
}

// Report is the JSON contract for `veil report` and GET /v1/report.
type Report struct {
	Items    int       `json:"items"`
	Weak     int       `json:"weak"`
	Reused   int       `json:"reused"`
	Pwned    int       `json:"pwned"`
	HIBP     string    `json:"hibp"` // "off" | "ok" | "error"
	Findings []Finding `json:"findings"`
}

// Checker returns the HIBP count for a full lowercase-or-uppercase SHA-1 hex
// of a password. Implementations must send only the k-anonymity prefix.
type Checker func(ctx context.Context, sha1Hex string) (int, error)

// scoredKinds are the kinds a human picks a secret for. OAuth tokens rotate,
// passkeys are keys, cards/files/identities are not passwords.
func scored(kind string) bool {
	return kind == "" || kind == "login" || kind == "api_key"
}

// Analyze scores entries. checker is nil to stay offline (default).
func Analyze(ctx context.Context, entries []Entry, check Checker) Report {
	rep := Report{HIBP: "off", Findings: []Finding{}}
	groups := map[[32]byte][]int{} // sha256(token) → entry indices
	for i, e := range entries {
		if !scored(e.Kind) || e.Token == "" {
			continue
		}
		sum := sha256.Sum256([]byte(e.Token))
		groups[sum] = append(groups[sum], i)
		rep.Items++
	}
	hibpErr := false
	for _, e := range entries {
		if !scored(e.Kind) || e.Token == "" {
			continue
		}
		f := Finding{ItemID: e.ID, Name: e.Name, Kind: e.Kind, URI: e.URI, Pwned: -1}
		sum := sha256.Sum256([]byte(e.Token))
		if n := len(groups[sum]); n > 1 {
			f.Reused = n
		}
		f.Weak = weakReasons(e)
		if check != nil {
			sum1 := sha1.Sum([]byte(e.Token))
			n, err := check(ctx, strings.ToUpper(hex.EncodeToString(sum1[:])))
			if err != nil {
				hibpErr = true
			} else {
				f.Pwned = n
			}
		}
		if f.Reused == 0 && len(f.Weak) == 0 && f.Pwned <= 0 {
			continue
		}
		if len(f.Weak) > 0 {
			rep.Weak++
		}
		if f.Reused > 0 {
			rep.Reused++
		}
		if f.Pwned > 0 {
			rep.Pwned++
		}
		rep.Findings = append(rep.Findings, f)
	}
	if check != nil {
		if hibpErr {
			rep.HIBP = "error"
		} else {
			rep.HIBP = "ok"
		}
	}
	return rep
}

// weakReasons is the deterministic weakness rule set. Deliberately
// transparent — a finding a user can't reproduce is noise.
func weakReasons(e Entry) []string {
	tok := e.Token
	var reasons []string
	if len(tok) < 12 {
		reasons = append(reasons, "short")
	}
	if common[strings.ToLower(tok)] || trivialRun(tok) {
		reasons = append(reasons, "common")
	}
	if classes(tok) < 3 && len(tok) < 16 {
		reasons = append(reasons, "low-variety")
	}
	if e.Login != "" && len(e.Login) >= 4 && strings.Contains(strings.ToLower(tok), strings.ToLower(e.Login)) {
		reasons = append(reasons, "contains-login")
	}
	return reasons
}

func classes(s string) int {
	var lower, upper, digit, sym bool
	for _, r := range s {
		switch {
		case unicode.IsLower(r):
			lower = true
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsDigit(r):
			digit = true
		default:
			sym = true
		}
	}
	n := 0
	for _, b := range []bool{lower, upper, digit, sym} {
		if b {
			n++
		}
	}
	return n
}

// trivialRun is true when the token is one repeated char or a monotone
// sequence (abcd, 9876, qwer rows are covered by common via keyboard order).
func trivialRun(s string) bool {
	if len(s) < 4 {
		return false
	}
	rs := []rune(s)
	repeat, asc, desc := true, true, true
	for i := 1; i < len(rs); i++ {
		if rs[i] != rs[0] {
			repeat = false
		}
		if rs[i] != rs[i-1]+1 {
			asc = false
		}
		if rs[i] != rs[i-1]-1 {
			desc = false
		}
	}
	return repeat || asc || desc
}

// common is the worst-of list — the top hits in every breach corpus. The
// point is catching the catastrophic few, not maintaining a dictionary.
var common = map[string]bool{
	"password": true, "123456": true, "12345678": true, "123456789": true,
	"1234567890": true, "qwerty": true, "qwerty123": true, "letmein": true,
	"iloveyou": true, "admin": true, "welcome": true, "monkey": true,
	"dragon": true, "football": true, "baseball": true, "abc123": true,
	"111111": true, "1qaz2wsx": true, "trustno1": true, "sunshine": true,
	"master": true, "shadow": true, "superman": true, "qazwsx": true,
	"michael": true, "password1": true, "password123": true, "p@ssw0rd": true,
	"changeme": true, "secret": true, "login": true, "passw0rd": true,
}

// HIBP is the k-anonymity breach checker. Client may be nil (10s default);
// Base is overridable for tests.
type HIBP struct {
	Client *http.Client
	Base   string
}

// Check looks up one password by SHA-1. Only the 5-char prefix is sent;
// Add-Padding asks HIBP to pad the response so response size leaks nothing.
func (h HIBP) Check(ctx context.Context, sha1Hex string) (int, error) {
	sha1Hex = strings.ToUpper(sha1Hex)
	if len(sha1Hex) != 40 {
		return 0, fmt.Errorf("health: sha1 hex")
	}
	base := h.Base
	if base == "" {
		base = "https://api.pwnedpasswords.com"
	}
	client := h.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/range/"+sha1Hex[:5], nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Add-Padding", "true")
	req.Header.Set("User-Agent", "veil-vault-health")
	res, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("health: hibp %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return 0, err
	}
	suffix := sha1Hex[5:]
	for line := range strings.Lines(string(body)) {
		sfx, cnt, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok || !strings.EqualFold(sfx, suffix) {
			continue
		}
		n, err := strconv.Atoi(cnt)
		if err != nil {
			return 0, fmt.Errorf("health: hibp count")
		}
		return n, nil
	}
	return 0, nil
}
