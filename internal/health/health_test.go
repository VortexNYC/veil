package health

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func entry(id, token string) Entry {
	return Entry{ID: id, Name: id, Token: token}
}

func TestAnalyzeCleanVault(t *testing.T) {
	rep := Analyze(context.Background(), []Entry{
		entry("a", "Xk9#mQ2$vLp8!zRw"),
		entry("b", "T7&yU3*jN4%hG9@cK"),
	}, nil)
	if rep.Items != 2 || rep.Weak != 0 || rep.Reused != 0 || rep.Pwned != 0 {
		t.Fatalf("unexpected %+v", rep)
	}
	if rep.HIBP != "off" {
		t.Fatalf("hibp %q", rep.HIBP)
	}
	if len(rep.Findings) != 0 {
		t.Fatalf("clean vault must emit no findings: %+v", rep.Findings)
	}
}

func TestAnalyzeSkipsEmptyAndNonScoredKinds(t *testing.T) {
	rep := Analyze(context.Background(), []Entry{
		{ID: "p", Name: "p", Kind: "passkey", Token: ""},
		{ID: "c", Name: "c", Kind: "card", Token: ""},
		{ID: "f", Name: "f", Kind: "file"},
		entry("g", "N9!qR4#wE7&tY2*uI"),
	}, nil)
	if rep.Items != 1 {
		t.Fatalf("only the login scored, got %d", rep.Items)
	}
}

func TestAnalyzeReused(t *testing.T) {
	rep := Analyze(context.Background(), []Entry{
		entry("a", "dup3#Strong9!xx"),
		entry("b", "dup3#Strong9!xx"),
		entry("c", "dup3#Strong9!xx"),
		entry("d", "uniq7$Power2@@zz"),
	}, nil)
	if rep.Reused != 3 {
		t.Fatalf("3 items share one token: %d", rep.Reused)
	}
	counts := map[string]int{}
	for _, f := range rep.Findings {
		counts[f.ItemID] = f.Reused
	}
	if counts["a"] != 3 || counts["b"] != 3 || counts["c"] != 3 {
		t.Fatalf("each reused item reports group size: %+v", counts)
	}
	if _, ok := counts["d"]; ok {
		t.Fatal("unique item must not be flagged")
	}
}

func TestAnalyzeWeakShort(t *testing.T) {
	rep := Analyze(context.Background(), []Entry{entry("a", "Ab1!x")}, nil)
	f := rep.Findings[0]
	if len(f.Weak) == 0 || f.Weak[0] != "short" {
		t.Fatalf("got %+v", f.Weak)
	}
	if rep.Weak != 1 {
		t.Fatal()
	}
}

func TestAnalyzeWeakCommon(t *testing.T) {
	for _, pw := range []string{"password", "123456", "qwerty123", "letmein"} {
		rep := Analyze(context.Background(), []Entry{entry("a", pw)}, nil)
		if rep.Weak != 1 {
			t.Fatalf("%q not flagged weak", pw)
		}
	}
}

func TestAnalyzeWeakLowVariety(t *testing.T) {
	rep := Analyze(context.Background(), []Entry{entry("a", "abcdefghijklm")}, nil)
	vars := rep.Findings[0].Weak
	found := false
	for _, r := range vars {
		if r == "low-variety" {
			found = true
		}
	}
	if !found {
		t.Fatalf("13 lower-only chars must flag low-variety: %v", vars)
	}
}

func TestAnalyzeLongPassphraseNotWeak(t *testing.T) {
	rep := Analyze(context.Background(), []Entry{entry("a", "correct horse battery staple nine tree")}, nil)
	if rep.Weak != 0 {
		t.Fatalf("long passphrase must not flag: %+v", rep.Findings)
	}
}

func TestAnalyzeWeakContainsLogin(t *testing.T) {
	rep := Analyze(context.Background(), []Entry{
		{ID: "a", Name: "a", Login: "shlomo.kabareti", Token: "shlomo.kabareti123!"},
	}, nil)
	found := false
	for _, r := range rep.Findings[0].Weak {
		if r == "contains-login" {
			found = true
		}
	}
	if !found {
		t.Fatalf("token containing login must flag: %+v", rep.Findings[0])
	}
}

func TestAnalyzePwned(t *testing.T) {
	check := func(_ context.Context, sha1Hex string) (int, error) {
		if strings.HasPrefix(sha1Hex, strings.ToUpper(sha1Of("bad-password1!"))) {
			return 42, nil
		}
		return 0, nil
	}
	rep := Analyze(context.Background(), []Entry{
		entry("a", "bad-password1!"),
		entry("b", "N9!qR4#wE7&tY2*uI"),
	}, check)
	if rep.HIBP != "ok" {
		t.Fatalf("hibp %q", rep.HIBP)
	}
	var a, b *Finding
	for i := range rep.Findings {
		if rep.Findings[i].ItemID == "a" {
			a = &rep.Findings[i]
		}
		if rep.Findings[i].ItemID == "b" {
			b = &rep.Findings[i]
		}
	}
	if a == nil || a.Pwned != 42 {
		t.Fatalf("a %+v", a)
	}
	if b != nil && b.Pwned != 0 && b.Pwned != -1 {
		t.Fatalf("b %+v", b)
	}
	if rep.Pwned != 1 {
		t.Fatalf("pwned count %d", rep.Pwned)
	}
}

func TestAnalyzeCheckerErrorMarksReportNotItems(t *testing.T) {
	check := func(_ context.Context, _ string) (int, error) {
		return 0, errors.New("hibp down")
	}
	rep := Analyze(context.Background(), []Entry{entry("a", "bad-password1!")}, check)
	if rep.HIBP != "error" {
		t.Fatalf("hibp %q", rep.HIBP)
	}
}

func TestFindingsNeverCarrySecrets(t *testing.T) {
	rep := Analyze(context.Background(), []Entry{
		entry("a", "password"),
		entry("b", "password"),
	}, nil)
	for _, f := range rep.Findings {
		out := fmt.Sprintf("%+v", f)
		if strings.Contains(out, "password") {
			t.Fatalf("finding leaked token: %s", out)
		}
	}
}

func sha1Of(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestHIBPCheckerKAnonymity(t *testing.T) {
	var gotPrefix string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPrefix = strings.TrimPrefix(r.URL.Path, "/range/")
		if r.Header.Get("Add-Padding") != "true" {
			t.Error("Add-Padding header missing — padding defeats traffic analysis")
		}
		fmt.Fprintln(w, "0018A45C4D1DEF81644B54AB7F969B88D65:1")
		fmt.Fprintln(w, "DEADBEEF0000000000000000000000000000:9")
		fmt.Fprintln(w, "1E4C9B93F3F0682250B6CF8331B7EE68FD8:37")
	}))
	defer srv.Close()
	h := HIBP{Client: srv.Client(), Base: srv.URL}
	n, err := h.Check(context.Background(), sha1Of("password"))
	if err != nil {
		t.Fatal(err)
	}
	if len(gotPrefix) != 5 {
		t.Fatalf("k-anonymity: prefix must be 5 chars, sent %q", gotPrefix)
	}
	if n != 37 {
		t.Fatalf("sha1(password) ends 5BAA61E4... count must be 37, got %d", n)
	}
}

func TestHIBPCheckerUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()
	h := HIBP{Client: srv.Client(), Base: srv.URL}
	if _, err := h.Check(context.Background(), sha1Of("x")); err == nil {
		t.Fatal("expected error")
	}
}
