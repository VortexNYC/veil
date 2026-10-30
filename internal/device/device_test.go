package device

import (
	"bytes"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/scrub"
)

func TestOfferAcceptRoundTrip(t *testing.T) {
	master, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	bPub, bPriv, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	blob, err := Offer(master, bPub)
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains(blob, master) {
		t.Fatal("master in pairing blob")
	}
	if scrub.Contains(blob, bPriv) {
		t.Fatal("private key in pairing blob")
	}
	got, err := Accept(blob, bPriv)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, master) {
		t.Fatal("master mismatch")
	}
	pub, err := Public(bPriv)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pub, bPub) {
		t.Fatal("public mismatch")
	}
}

func TestAcceptWrongKeyFails(t *testing.T) {
	master, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	bPub, _, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	_, cPriv, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	blob, err := Offer(master, bPub)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Accept(blob, cPriv); err != crypto.ErrAuth {
		t.Fatalf("err=%v", err)
	}
}

func TestAcceptTruncatedFails(t *testing.T) {
	_, priv, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Accept([]byte("short"), priv); err != crypto.ErrAuth {
		t.Fatalf("err=%v", err)
	}
}

// TestAcceptJSSealedBoxInterop verifies tweetnacl-sealedbox-js (phone handoff)
// matches device.Accept. Generate fixtures from apps/phone:
//
//	node -e "const n=require('tweetnacl'),sb=require('tweetnacl-sealedbox-js');..."
//
// Skips when VEIL_JS_BOX / VEIL_JS_PRIV_HEX are unset.
func TestAcceptJSSealedBoxInterop(t *testing.T) {
	boxPath := os.Getenv("VEIL_JS_BOX")
	privHex := os.Getenv("VEIL_JS_PRIV_HEX")
	if boxPath == "" || privHex == "" {
		t.Skip("set VEIL_JS_BOX and VEIL_JS_PRIV_HEX for JS interop")
	}
	boxed, err := os.ReadFile(boxPath)
	if err != nil {
		t.Fatal(err)
	}
	priv, err := hex.DecodeString(strings.TrimSpace(privHex))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Accept(boxed, priv)
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Repeat([]byte{7}, crypto.KeySize)
	if !bytes.Equal(got, want) {
		t.Fatalf("master mismatch")
	}
}
