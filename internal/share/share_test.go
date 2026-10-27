package share

import (
	"bytes"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	plain := []byte("sk_live_API_SECRET")
	blob, meta, key, err := Seal(plain, "")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, plain) {
		t.Fatal("sealed blob carries the plaintext")
	}
	got, err := Open(blob, meta, key, "")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("got %q", got)
	}
}

func TestPassphraseRoundTrip(t *testing.T) {
	plain := []byte("correct horse battery staple")
	blob, meta, key, err := Seal(plain, "hunter2")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Open(blob, meta, key, "hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("got %q", got)
	}
	if _, err := Open(blob, meta, key, ""); err == nil {
		t.Fatal("missing passphrase opened a pw share")
	}
	if _, err := Open(blob, meta, key, "wrong"); err == nil {
		t.Fatal("wrong passphrase opened a pw share")
	}
}

func TestWrongKeyFails(t *testing.T) {
	blob, meta, _, err := Seal([]byte("secret"), "")
	if err != nil {
		t.Fatal(err)
	}
	_, _, other, err := Seal([]byte("other"), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(blob, meta, other, ""); err == nil {
		t.Fatal("wrong key opened the share")
	}
}
