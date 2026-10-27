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
		if err == nil && len(advs) > 0 {
			findings = append(findings, fmt.Sprintf(
				"ory-deps: %s pinned %s has %d known advisories: %s",
				p.repo, p.version, len(advs), strings.Join(advs, ", ")))
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
