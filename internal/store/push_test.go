package store

import (
	"testing"

	"github.com/VortexNYC/veil/internal/protocol"
)

func TestPushDevices(t *testing.T) {
	for _, s := range testStores(t) {
		dev := protocol.PushDevice{
			OrgID:    "org",
			Platform: "apns",
			Token:    "tok-1",
			Label:    "iphone",
		}
		if err := s.PutPushDevice(dev); err != nil {
			t.Fatalf("put: %v", err)
		}
		got, err := s.PushDevices("org")
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(got) != 1 || got[0].Token != "tok-1" || got[0].Platform != "apns" {
			t.Fatalf("want one apns row, got %+v", got)
		}
		if got[0].UpdatedAt.IsZero() {
			t.Fatal("updated_at unset")
		}

		// Re-register rotates the token's metadata in place — same token,
		// new label, fresh stamp; the row count stays 1.
		dev.Label = "iphone-16"
		if err := s.PutPushDevice(dev); err != nil {
			t.Fatalf("re-put: %v", err)
		}
		got, _ = s.PushDevices("org")
		if len(got) != 1 || got[0].Label != "iphone-16" {
			t.Fatalf("upsert expected one row with new label, got %+v", got)
		}

		// Org scoping: a second org's token never bleeds into the first's list.
		if err := s.PutPushDevice(protocol.PushDevice{
			OrgID: "org2", Platform: "fcm", Token: "tok-2",
		}); err != nil {
			t.Fatalf("put org2: %v", err)
		}
		got, _ = s.PushDevices("org")
		if len(got) != 1 {
			t.Fatalf("org2 device leaked into org list: %+v", got)
		}

		if err := s.DeletePushDevice("org", "tok-1"); err != nil {
			t.Fatalf("delete: %v", err)
		}
		got, _ = s.PushDevices("org")
		if len(got) != 0 {
			t.Fatalf("deleted row still listed: %+v", got)
		}
		// Idempotent: a second delete of an absent row is a no-op.
		if err := s.DeletePushDevice("org", "tok-1"); err != nil {
			t.Fatalf("second delete: %v", err)
		}
	}
}
