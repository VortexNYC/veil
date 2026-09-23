package crypto

import (
	"bytes"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("sk_live_do_not_leak")
	blob, err := Seal(key, plain)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Open(key, blob)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("got %q", got)
	}
}

func TestOpenWrongKeyFails(t *testing.T) {
	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	blob, err := Seal(key, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(other, blob); err != ErrAuth {
		t.Fatalf("err=%v", err)
	}
}

func TestOpenTruncatedFails(t *testing.T) {
	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(key, []byte("short")); err != ErrAuth {
		t.Fatalf("err=%v", err)
	}
}

func TestNewKeySize(t *testing.T) {
	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != KeySize {
		t.Fatalf("len=%d", len(key))
	}
}

func TestEpochRoundTrip(t *testing.T) {
	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	aad := []byte("veil/item/v1\x00org\x00i1")
	plain := []byte("sk_live_do_not_leak")

	blob, err := SealEpoch(key, plain, aad)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(blob, epochMark) {
		t.Fatal("epoch blob missing marker")
	}
	if got, err := OpenEpoch(key, blob, aad); err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("open: %v %q", err, got)
	}
	// Wrong context fails closed — no legacy retry on a marked blob.
	if _, err := OpenEpoch(key, blob, []byte("veil/item/v1\x00org\x00i2")); err != ErrAuth {
		t.Fatalf("wrong aad: %v", err)
	}
	// Legacy nil-AAD blobs still open; aad is ignored for them.
	legacy, err := Seal(key, plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.HasPrefix(legacy, epochMark) {
		t.Fatal("legacy blob carries marker")
	}
	if got, err := OpenEpoch(key, legacy, aad); err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("legacy open: %v %q", err, got)
	}
	// A crafted blob = mark||legacy-seal opens only under a matching aad —
	// the marker is authoritative for marked bytes, never advisory.
	crafted := append(append([]byte(nil), epochMark...), legacy...)
	if got, err := OpenEpoch(key, crafted, nil); err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("crafted open: %v %q", err, got)
	}
	if _, err := OpenEpoch(key, crafted, aad); err != ErrAuth {
		t.Fatalf("crafted wrong aad: %v", err)
	}
}
