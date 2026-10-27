package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/VortexNYC/veil/internal/protocol"
)

type hookSink struct {
	t        *testing.T
	bodies   [][]byte
	requests int
	status   int
}

func (s *hookSink) handler(w http.ResponseWriter, r *http.Request) {
	s.requests++
	if r.Method != http.MethodPost {
		s.t.Errorf("hook method %s, want POST", r.Method)
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		s.t.Errorf("hook content-type %q", ct)
	}
	b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	s.bodies = append(s.bodies, b)
	w.WriteHeader(s.status)
}

func hookEvents(t *testing.T, a *App) []protocol.AuditEvent {
	t.Helper()
	rows, err := a.Store.Audit()
	if err != nil {
		t.Fatal(err)
	}
	var out []protocol.AuditEvent
	for _, e := range rows {
		if e.Action == protocol.ActionRotationHookFired {
			out = append(out, e)
		}
	}
	return out
}

func TestRotateHookFiresOnSuccess(t *testing.T) {
	a, _, _, _, owner := matrixApp(t)
	sink := &hookSink{t: t, status: 200}
	srv := httptest.NewServer(http.HandlerFunc(sink.handler))
	t.Cleanup(srv.Close)

	item, err := a.PutItemFor(owner, ItemOpts{Name: "stripe-live", Token: []byte(secret)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetRotateHooks(owner, item.Name, []protocol.RotateHook{
		{Name: "ci-resecrets", Type: protocol.RotateHookTypeWebhook, URL: srv.URL + "/hook"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RotateItemWith(owner, item.Name, RotateOpts{ReplacementSecret: []byte("rotated_secret")}); err != nil {
		t.Fatal(err)
	}
	if sink.requests != 1 {
		t.Fatalf("hook fired %d times, want 1", sink.requests)
	}
	var payload RotateHookPayload
	if err := json.Unmarshal(sink.bodies[0], &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Event != "item_rotated" || payload.Item != "stripe-live" || payload.ItemID != item.ID {
		t.Fatalf("payload %+v", payload)
	}
	if payload.OrgID != owner.OrgID || payload.Actor != owner.ID || payload.RotatedAt <= 0 {
		t.Fatalf("payload metadata %+v", payload)
	}
	if payload.Replay {
		t.Fatal("initial firing must not be marked replay")
	}
	// The secret never leaves the broker — not the old value, not the new.
	body := string(sink.bodies[0])
	if strings.Contains(body, secret) || strings.Contains(body, "rotated_secret") {
		t.Fatalf("hook payload carried secret material: %s", body)
	}
	events := hookEvents(t, a)
	if len(events) != 1 {
		t.Fatalf("rotation_hook_fired rows: %d", len(events))
	}
	if !strings.Contains(events[0].Reason, "hook=ci-resecrets") || !strings.Contains(events[0].Reason, "outcome=ok") {
		t.Fatalf("audit reason %q", events[0].Reason)
	}
	// Audit carries the host, never the full URL — capability tokens may
	// ride in path/query and must not leak into the durable feed.
	if strings.Contains(events[0].Reason, "/hook") {
		t.Fatalf("audit leaked URL path: %q", events[0].Reason)
	}
}

func TestRotateHookNotFiredOnFailedRotate(t *testing.T) {
	a, _, _, _, owner := matrixApp(t)
	sink := &hookSink{t: t, status: 200}
	srv := httptest.NewServer(http.HandlerFunc(sink.handler))
	t.Cleanup(srv.Close)

	item, err := a.PutItemFor(owner, ItemOpts{Name: "stripe-live", Token: []byte(secret)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetRotateHooks(owner, item.Name, []protocol.RotateHook{
		{Name: "ci-resecrets", Type: protocol.RotateHookTypeWebhook, URL: srv.URL},
	}); err != nil {
		t.Fatal(err)
	}
	// Blank replacement seals nothing — rotate fails before commit.
	if _, err := a.RotateItemWith(owner, item.Name, RotateOpts{ReplacementSecret: []byte("   ")}); err == nil {
		t.Fatal("expected rotate to fail on empty replacement")
	}
	if sink.requests != 0 {
		t.Fatalf("hook fired %d times on a failed rotate", sink.requests)
	}
	if n := len(hookEvents(t, a)); n != 0 {
		t.Fatalf("rotation_hook_fired rows on failure: %d", n)
	}
}

func TestRotateHookFailureDoesNotBreakRotate(t *testing.T) {
	a, _, _, _, owner := matrixApp(t)
	sink := &hookSink{t: t, status: 500}
	srv := httptest.NewServer(http.HandlerFunc(sink.handler))
	t.Cleanup(srv.Close)

	item, err := a.PutItemFor(owner, ItemOpts{Name: "stripe-live", Token: []byte(secret)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetRotateHooks(owner, item.Name, []protocol.RotateHook{
		{Name: "ci-resecrets", Type: protocol.RotateHookTypeWebhook, URL: srv.URL},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RotateItemWith(owner, item.Name, RotateOpts{ReplacementSecret: []byte("rotated_secret")}); err != nil {
		t.Fatalf("hook failure broke the rotate: %v", err)
	}
	if sink.requests != 1 {
		t.Fatalf("hook fired %d times", sink.requests)
	}
	events := hookEvents(t, a)
	if len(events) != 1 || !strings.Contains(events[0].Reason, "outcome=failed") {
		t.Fatalf("missing failed outcome row: %+v", events)
	}
	// The secret still rotated — verify the sealed token changed.
	raw, err := a.Store.Secret(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "rotated_secret") == false {
		t.Fatal("rotation did not commit")
	}
}

func TestRotateHooksReplay(t *testing.T) {
	a, _, _, _, owner := matrixApp(t)
	sink := &hookSink{t: t, status: 200}
	srv := httptest.NewServer(http.HandlerFunc(sink.handler))
	t.Cleanup(srv.Close)

	item, err := a.PutItemFor(owner, ItemOpts{Name: "stripe-live", Token: []byte(secret)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetRotateHooks(owner, item.Name, []protocol.RotateHook{
		{Name: "ci-resecrets", Type: protocol.RotateHookTypeWebhook, URL: srv.URL},
	}); err != nil {
		t.Fatal(err)
	}
	results, err := a.FireRotateHooks(context.Background(), owner, item.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("replay results %+v", results)
	}
	var payload RotateHookPayload
	if err := json.Unmarshal(sink.bodies[0], &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Replay {
		t.Fatal("replay firing must be marked replay")
	}
	events := hookEvents(t, a)
	if len(events) != 1 || !strings.Contains(events[0].Reason, "replay=1") {
		t.Fatalf("replay audit rows %+v", events)
	}
}

func TestSetRotateHooksValidation(t *testing.T) {
	a, _, _, _, owner := matrixApp(t)
	item, err := a.PutItemFor(owner, ItemOpts{Name: "stripe-live", Token: []byte(secret)})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		hooks []protocol.RotateHook
	}{
		{"empty name", []protocol.RotateHook{{Type: "webhook", URL: "https://h.test/x"}}},
		{"bad scheme", []protocol.RotateHook{{Name: "h", Type: "webhook", URL: "ftp://h.test/x"}}},
		{"no url", []protocol.RotateHook{{Name: "h", Type: "webhook"}}},
		{"dup name", []protocol.RotateHook{
			{Name: "h", Type: "webhook", URL: "https://a.test/x"},
			{Name: "h", Type: "webhook", URL: "https://b.test/x"},
		}},
		{"unknown type", []protocol.RotateHook{{Name: "h", Type: "file", URL: "https://h.test/x"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := a.SetRotateHooks(owner, item.Name, tc.hooks); err == nil {
				t.Fatalf("accepted %s", tc.name)
			}
		})
	}
	hooks, err := a.RotateHooks(owner, item.Name)
	if err != nil || len(hooks) != 0 {
		t.Fatalf("rejected configs should not persist: %+v %v", hooks, err)
	}
}

func TestRotateHooksOwnerOnly(t *testing.T) {
	a, prov, _, roles, owner := matrixApp(t)
	item, err := a.PutItemFor(owner, ItemOpts{Name: "stripe-live", Token: []byte(secret)})
	if err != nil {
		t.Fatal(err)
	}
	member := provisioned(t, a, prov, "member")
	roles.members[member.OrgID+"|"+member.ID] = true
	if _, err := a.SetRotateHooks(member, item.Name, []protocol.RotateHook{
		{Name: "h", Type: "webhook", URL: "https://h.test/x"},
	}); err != ErrForbidden {
		t.Fatalf("member set hooks: %v", err)
	}
	if _, err := a.RotateHooks(member, item.Name); err != ErrForbidden {
		t.Fatalf("member list hooks: %v", err)
	}
	if _, err := a.FireRotateHooks(context.Background(), member, item.Name); err != ErrForbidden {
		t.Fatalf("member fire hooks: %v", err)
	}
}
