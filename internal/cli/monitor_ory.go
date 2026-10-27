package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// oryPins is the Ory image set the origin ships — .railway/railway.ts
// (hydra) and identity/{kratos,keto}/Dockerfile. The binary cannot read the
// repo at runtime, so bump these with the image pins; a stale entry is
// exactly what the >1-minor-behind finding exists to catch.
var oryPins = []struct {
	repo    string // GitHub owner/repo; Go module is github.com/<repo>
	version string // pinned tag
}{
	{"ory/hydra", "v26.2.0"},
	{"ory/kratos", "v26.2.0"},
	{"ory/keto", "v26.2.0"},
}

// oryAdvisorySuppress is the triage list — advisories OSV reports against
// our pins that do not affect the deployed images. Keyed by repo, then by
// every alias the advisory might surface as (the check emits the first
// CVE alias when present, so list the CVE, GO-, and GHSA- ids for each).
// The value is the docker tag that first shipped the fix: suppression only
// holds while the pin is at or past it, so a rollback to a vulnerable tag
// pages again. Entries are advisory-scoped — a suppressed id never hides
// a different vuln, and a genuinely new advisory still pages. Every entry
// carries the evidence for why the pin is unaffected.
var oryAdvisorySuppress = map[string]map[string]string{
	"ory/hydra": {
		// CVE-2026-33504 / GO-2026-4807 / GHSA-r9w3-57w2-gch2 — SQL
		// injection via forged pagination tokens in admin list APIs.
		// The upstream fix commit 0b84568fffcc IS the v26.2.0 release
		// commit (verified: compare 0b84568fffcc...v26.2.0 = identical),
		// so our pin ships the fix. OSV records the fix as a hydra/v2
		// module pseudo-version and leaves module github.com/ory/hydra
		// on an unbounded [0,∞) range, which matches every docker tag
		// forever. Defense in depth: secrets.pagination is unset, so
		// tokens fall back to the random SECRETS_SYSTEM an attacker
		// does not know — the forge precondition fails either way.
		"CVE-2026-33504":      "26.2.0",
		"GO-2026-4807":        "26.2.0",
		"GHSA-r9w3-57w2-gch2": "26.2.0",
		// CVE-2019-8400 / GO-2026-4861 / GHSA-7v6r-w4r6-mhch —
		// reflected XSS via error_hint on the OAuth2 error path, fixed
		// in hydra 1.4.8 in 2019 (fix commit 9b5bbd48). v26.2.0 is
		// seven years past the fix; another unbounded OSV range on the
		// v1 module path that matches modern tags.
		"CVE-2019-8400":       "1.4.8",
		"GO-2026-4861":        "1.4.8",
		"GHSA-7v6r-w4r6-mhch": "1.4.8",
	},
}

// orySuppressed reports whether a triaged advisory stays quiet at this
// pin. True only when the id is on the list AND the pin parses at or past
// the tag that shipped the fix — an unparseable pin pages, because "can't
// prove fixed" is not "fixed".
func orySuppressed(repo, pin, id string) bool {
	fixed, ok := oryAdvisorySuppress[repo][id]
	if !ok {
		return false
	}
	pv, ok := semverTriple(pin)
	if !ok {
		return false
	}
	fv, ok := semverTriple(fixed)
	if !ok {
		return false
	}
	for i := 0; i < 3; i++ {
		if pv[i] != fv[i] {
			return pv[i] > fv[i]
		}
	}
	return true
}

// oryDepSource is the two external reads the check needs — GitHub latest
// release and OSV advisories for the pinned version. Small interface so
// tests fake both.
type oryDepSource interface {
	LatestRelease(ctx context.Context, repo string) (string, error)
	Advisories(ctx context.Context, repo, version string) ([]string, error)
}

type oryDepHTTP struct {
	client *http.Client
}

func newOryDepHTTP() *oryDepHTTP {
	return &oryDepHTTP{client: &http.Client{Timeout: 10 * time.Second}}
}

func (s *oryDepHTTP) LatestRelease(ctx context.Context, repo string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "veil-monitor")
	res, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github %d", res.StatusCode)
	}
	var out struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&out); err != nil {
		return "", err
	}
	return out.TagName, nil
}

func (s *oryDepHTTP) Advisories(ctx context.Context, repo, version string) ([]string, error) {
	payload, err := json.Marshal(map[string]any{
		"package": map[string]string{
			"name":      "github.com/" + repo,
			"ecosystem": "Go",
		},
		"version": strings.TrimPrefix(strings.TrimSpace(version), "v"),
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.osv.dev/v1/query", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("osv %d", res.StatusCode)
	}
	var out struct {
		Vulns []struct {
			ID      string   `json:"id"`
			Aliases []string `json:"aliases"`
		} `json:"vulns"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&out); err != nil {
		return nil, err
	}
	var ids []string
	for _, v := range out.Vulns {
		if len(v.Aliases) > 0 && strings.HasPrefix(v.Aliases[0], "CVE-") {
			ids = append(ids, v.Aliases[0])
			continue
		}
		ids = append(ids, v.ID)
	}
	return ids, nil
}

// checkOryDeps returns advisory findings for the pinned Ory set. A fetch
// failure is not a finding — paging on a GitHub/OSV blip is worse than
// waiting one day for the next tick.
func checkOryDeps(ctx context.Context, src oryDepSource) []string {
	var findings []string
	for _, p := range oryPins {
		advs, err := src.Advisories(ctx, p.repo, p.version)
		if err == nil {
			var open []string
			for _, a := range advs {
				if !orySuppressed(p.repo, p.version, a) {
					open = append(open, a)
				}
			}
			if len(open) > 0 {
				findings = append(findings, fmt.Sprintf(
					"ory-deps: %s pinned %s has %d known advisories: %s",
					p.repo, p.version, len(open), strings.Join(open, ", ")))
			}
		}
		latest, err := src.LatestRelease(ctx, p.repo)
		if err == nil && behindByMinors(p.version, latest) > 1 {
			findings = append(findings, fmt.Sprintf(
				"ory-deps: %s pinned %s is more than one minor behind latest %s",
				p.repo, p.version, latest))
		}
	}
	return findings
}

// behindByMinors reports how many minor releases (or majors, counted
// coarsely as +2 minors each) the pinned tag trails the latest tag.
// Unparseable tags compare as 0 — a weird upstream tag is not a page.
func behindByMinors(pinned, latest string) int {
	pm, ok := semverTriple(pinned)
	if !ok {
		return 0
	}
	lm, ok := semverTriple(latest)
	if !ok {
		return 0
	}
	if lm[0] > pm[0] {
		return (lm[0]-pm[0])*2 + lm[1] // any major bump is >1 behind
	}
	if lm[0] < pm[0] || lm[1] <= pm[1] {
		return 0
	}
	return lm[1] - pm[1]
}

func semverTriple(tag string) ([3]int, bool) {
	v := strings.TrimPrefix(strings.TrimSpace(tag), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	var out [3]int
	for i := 0; i < len(parts) && i < 3; i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	if len(parts) < 2 {
		return out, false
	}
	return out, true
}
