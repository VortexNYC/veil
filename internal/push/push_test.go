package push

import (
	"context"
	"errors"
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
