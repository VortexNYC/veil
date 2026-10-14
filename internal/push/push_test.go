package push

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store"
)

type fakeSender struct {
	sent  []string
	fails map[string]error // per-token send error
}

func (f *fakeSender) Send(_ context.Context, d protocol.PushDevice, title, body string, data map[string]string) error {
	if err, ok := f.fails[d.Token]; ok {
		return err
	}
	f.sent = append(f.sent, d.Token)
	return nil
}

func TestNotifyRequestFansOutAndPrunesDead(t *testing.T) {
	s := store.NewMemory()
	_ = s.PutPushDevice(protocol.PushDevice{OrgID: "org", Platform: "apns", Token: "live"})
	_ = s.PutPushDevice(protocol.PushDevice{OrgID: "org", Platform: "apns", Token: "dead"})
	_ = s.PutPushDevice(protocol.PushDevice{OrgID: "other", Platform: "apns", Token: "other-org-token"})

	sender := &fakeSender{fails: map[string]error{
		// APNs' BadDeviceToken reason = durable dead token → prune.
		"dead": &apnsResponse{Reason: "BadDeviceToken"},
	}}
	p := New(s, sender)

	err := p.NotifyRequest(context.Background(), protocol.ApprovalRequest{ID: "req-1", OrgID: "org"})
	if err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 1 || sender.sent[0] != "live" {
		t.Fatalf("want only the live token sent, got %v", sender.sent)
	}
	devs, _ := s.PushDevices("org")
	if len(devs) != 1 || devs[0].Token != "live" {
		t.Fatalf("dead token not pruned: %+v", devs)
	}
	// The other org's device was never touched.
	devs2, _ := s.PushDevices("other")
	if len(devs2) != 1 {
		t.Fatalf("other org's device disturbed: %+v", devs2)
	}
}

func TestNilSenderIsNoOp(t *testing.T) {
	p := New(store.NewMemory(), nil)
	if err := p.NotifyRequest(context.Background(), protocol.ApprovalRequest{OrgID: "org"}); err != nil {
		t.Fatal(err)
	}
	if err := p.NotifyOps(context.Background(), "org", []string{"x"}); err != nil {
		t.Fatal(err)
	}
	var nilP *Pusher
	if err := nilP.NotifyOps(context.Background(), "org", []string{"x"}); err != nil {
		t.Fatal("nil pusher must no-op")
	}
}

func TestNonDeadSendErrorKeepsToken(t *testing.T) {
	s := store.NewMemory()
	_ = s.PutPushDevice(protocol.PushDevice{OrgID: "org", Platform: "apns", Token: "flaky"})
	p := New(s, &fakeSender{fails: map[string]error{"flaky": errors.New("apns send: timeout")}})
	_ = p.NotifyOps(context.Background(), "org", []string{"backup beat is stale"})
	devs, _ := s.PushDevices("org")
	if len(devs) != 1 {
		t.Fatal("transient send failure must not prune the token")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFCMSendPostsV1AndSkipsNonFCM(t *testing.T) {
	var gotBody []byte
	var gotAuth string
	s := &fcmSender{
		projectID: "proj",
		source:    func() (string, error) { return "tok", nil },
		httpc: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			gotBody, _ = io.ReadAll(r.Body)
			gotAuth = r.Header.Get("Authorization")
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header)}, nil
		})},
	}
	dev := protocol.PushDevice{OrgID: "org", Platform: "fcm", Token: "tok-123"}
	if err := s.Send(context.Background(), dev, "t", "b", map[string]string{"type": "approve_request"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if gotAuth != "Bearer tok" {
		t.Fatalf("auth %q", gotAuth)
	}
	var m map[string]any
	if err := json.Unmarshal(gotBody, &m); err != nil {
		t.Fatalf("body: %v", err)
	}
	msg := m["message"].(map[string]any)
	if msg["token"] != "tok-123" {
		t.Fatalf("token %v", msg["token"])
	}
	if err := s.Send(context.Background(), protocol.PushDevice{Platform: "apns"}, "t", "b", nil); err != nil {
		t.Fatalf("apns passthrough should be nil, got %v", err)
	}
}

func TestFCMSendUnregistered(t *testing.T) {
	s := &fcmSender{
		projectID: "proj",
		source:    func() (string, error) { return "tok", nil },
		httpc: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(`{"error":{"status":"NOT_FOUND"}}`)), Header: make(http.Header)}, nil
		})},
	}
	err := s.Send(context.Background(), protocol.PushDevice{Platform: "fcm", Token: "dead"}, "t", "b", nil)
	if !IsUnregisteredFCM(err) {
		t.Fatalf("expected unregistered, got %v", err)
	}
}

func TestMultiSenderRoutesByPlatform(t *testing.T) {
	apns := &fakeSender{}
	fcm := &fakeSender{}
	m := &MultiSender{APNS: apns, FCM: fcm}
	_ = m.Send(context.Background(), protocol.PushDevice{Platform: "apns", Token: "a"}, "t", "b", nil)
	_ = m.Send(context.Background(), protocol.PushDevice{Platform: "fcm", Token: "f"}, "t", "b", nil)
	if len(apns.sent) != 1 || apns.sent[0] != "a" || len(fcm.sent) != 1 || fcm.sent[0] != "f" {
		t.Fatalf("routing apns=%v fcm=%v", apns.sent, fcm.sent)
	}
}
