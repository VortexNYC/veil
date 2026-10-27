package scrub

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestBytesRedactsPlainAndBase64(t *testing.T) {
	secret := []byte("sk_live_super_secret")
	body := []byte(`got sk_live_super_secret and ` + base64.StdEncoding.EncodeToString(secret))
	got := Bytes(body, secret)
	if Contains(got, secret) {
		t.Fatalf("plain secret leaked: %s", got)
	}
	if bytes.Contains(got, []byte(base64.StdEncoding.EncodeToString(secret))) {
		t.Fatalf("base64 secret leaked: %s", got)
	}
	if !bytes.Contains(got, []byte(Redacted)) {
		t.Fatalf("missing redaction marker: %s", got)
	}
}

func TestEmptySecretIsNoop(t *testing.T) {
	in := []byte("hello")
	if !bytes.Equal(Bytes(in, nil), in) {
		t.Fatal("empty secret changed input")
	}
}

func FuzzBytes(f *testing.F) {
	f.Add([]byte("got sk_live_super_secret"), []byte("sk_live_super_secret"))
	f.Add([]byte("hello"), []byte(""))
	f.Fuzz(func(t *testing.T, body, secret []byte) {
		if len(secret) == 0 || bytes.Contains([]byte(Redacted), secret) {
			return
		}
		got := Bytes(body, secret)
		if Contains(got, secret) {
			t.Fatal("secret survived scrub")
		}
	})
}

func TestNewWriterRedactsStream(t *testing.T) {
	var buf bytes.Buffer
	secret := []byte("sk_live_stream_secret_123")
	w := NewWriter(&buf, secret)
	msg := []byte("connecting with token sk_live_stream_secret_123\n")
	n, err := w.Write(msg)
	if err != nil || n != len(msg) {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if Contains(buf.Bytes(), secret) {
		t.Fatalf("secret leaked in stream: %s", buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte(Redacted)) {
		t.Fatalf("missing redaction marker: %s", buf.String())
	}
}
