package cli

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type fakeDepSource struct {
	latest map[string]string
	advs   map[string][]string
	err    error
}

func (f fakeDepSource) LatestRelease(_ context.Context, repo string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.latest[repo], nil
}

func (f fakeDepSource) Advisories(_ context.Context, repo, version string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.advs[repo+"@"+version], nil
}

func TestCheckOryDepsFiresOnAdvisory(t *testing.T) {
	src := fakeDepSource{
		latest: map[string]string{"ory/hydra": "v26.2.0", "ory/kratos": "v26.2.0", "ory/keto": "v26.2.0"},
		advs:   map[string][]string{"ory/kratos@v26.2.0": {"CVE-2026-1111", "GHSA-xxxx-yyyy-zzzz"}},
	}
	findings := checkOryDeps(context.Background(), src)
	if len(findings) != 1 {
		t.Fatalf("findings %v", findings)
	}
	if !strings.Contains(findings[0], "ory/kratos") || !strings.Contains(findings[0], "CVE-2026-1111") {
		t.Fatalf("finding text %q", findings[0])
	}
}

func TestCheckOryDepsSuppressesTriagedAdvisories(t *testing.T) {
	// The live OSV picture for oryd/hydra:v26.2.0 — CVE-2026-33504 is
	// fixed AT the pinned tag, CVE-2019-8400 was fixed in 2019. Both
	// must stay quiet, under any alias OSV chooses to emit.
	for _, advs := range [][]string{
		{"CVE-2026-33504", "CVE-2019-8400"},            // CVE-first, what OSV emits today
		{"GO-2026-4807", "GO-2026-4861"},               // bare GO- ids
		{"GHSA-r9w3-57w2-gch2", "GHSA-7v6r-w4r6-mhch"}, // bare GHSA ids
	} {
		src := fakeDepSource{
			latest: map[string]string{"ory/hydra": "v26.2.0", "ory/kratos": "v26.2.0", "ory/keto": "v26.2.0"},
			advs:   map[string][]string{"ory/hydra@v26.2.0": advs},
		}
		if f := checkOryDeps(context.Background(), src); len(f) != 0 {
			t.Fatalf("triaged advisories %v still paged: %v", advs, f)
		}
	}
}

func TestCheckOryDepsNewAdvisoryStillFires(t *testing.T) {
	// Suppression is per-advisory-id: a triaged id alongside a new one
	// must not mute the new finding.
	src := fakeDepSource{
		latest: map[string]string{"ory/hydra": "v26.2.0", "ory/kratos": "v26.2.0", "ory/keto": "v26.2.0"},
		advs:   map[string][]string{"ory/hydra@v26.2.0": {"CVE-2019-8400", "CVE-2026-9999"}},
	}
	findings := checkOryDeps(context.Background(), src)
	if len(findings) != 1 || !strings.Contains(findings[0], "CVE-2026-9999") {
		t.Fatalf("new advisory muted: %v", findings)
	}
	if strings.Contains(findings[0], "CVE-2019-8400") {
		t.Fatalf("suppressed advisory leaked into finding text: %q", findings[0])
	}
}

func TestOrySuppressed(t *testing.T) {
	for _, tc := range []struct {
		repo, pin, id string
		want          bool
	}{
		// fixed AT the pin
		{"ory/hydra", "v26.2.0", "CVE-2026-33504", true},
		{"ory/hydra", "v26.2.0", "CVE-2019-8400", true},
		// pin past the fix still suppresses
		{"ory/hydra", "v26.3.0", "CVE-2026-33504", true},
		// rollback below fixedIn pages again
		{"ory/hydra", "v26.1.9", "CVE-2026-33504", false},
		{"ory/hydra", "v1.4.7", "CVE-2019-8400", false},
		// unknown advisory / repo / pin never suppresses
		{"ory/hydra", "v26.2.0", "CVE-2026-9999", false},
		{"ory/kratos", "v26.2.0", "CVE-2026-33504", false},
		{"ory/hydra", "garbage", "CVE-2026-33504", false},
	} {
		if got := orySuppressed(tc.repo, tc.pin, tc.id); got != tc.want {
			t.Errorf("orySuppressed(%q,%q,%q)=%v want %v", tc.repo, tc.pin, tc.id, got, tc.want)
		}
	}
}

func TestCheckOryDepsQuietWhenCurrent(t *testing.T) {
	src := fakeDepSource{
		latest: map[string]string{"ory/hydra": "v26.2.0", "ory/kratos": "v26.2.0", "ory/keto": "v26.2.0"},
		advs:   map[string][]string{},
	}
	if f := checkOryDeps(context.Background(), src); len(f) != 0 {
		t.Fatalf("quiet check produced findings %v", f)
	}
}

func TestCheckOryDepsFiresWhenMoreThanOneMinorBehind(t *testing.T) {
	src := fakeDepSource{
		latest: map[string]string{"ory/hydra": "v26.4.1", "ory/kratos": "v26.3.0", "ory/keto": "v26.2.5"},
		advs:   map[string][]string{},
	}
	findings := checkOryDeps(context.Background(), src)
	if len(findings) != 1 || !strings.Contains(findings[0], "ory/hydra") {
		t.Fatalf("want one stale-pin finding for hydra only, got %v", findings)
	}
}

func TestCheckOryDepsFetchErrorIsNotAFinding(t *testing.T) {
	src := fakeDepSource{err: fmt.Errorf("network down")}
	if f := checkOryDeps(context.Background(), src); len(f) != 0 {
		t.Fatalf("fetch failure paged: %v", f)
	}
}

func TestBehindByMinors(t *testing.T) {
	for _, tc := range []struct {
		pinned, latest string
		want           int
	}{
		{"v26.2.0", "v26.2.0", 0},
		{"v26.2.0", "v26.2.5", 0}, // patch only
		{"v26.2.0", "v26.3.0", 1}, // one minor — not a finding
		{"v26.2.0", "v26.4.0", 2},
		{"v26.2.0", "v27.0.0", 2}, // major bump counts >1
		{"v26.2.0", "garbage", 0},
		{"v26.2.0", "v27.0.0-rc.1", 2},
		{"v26.10.0", "v26.9.9", 0}, // pinned ahead
	} {
		if got := behindByMinors(tc.pinned, tc.latest); got != tc.want {
			t.Errorf("behindByMinors(%q,%q)=%d want %d", tc.pinned, tc.latest, got, tc.want)
		}
	}
}
