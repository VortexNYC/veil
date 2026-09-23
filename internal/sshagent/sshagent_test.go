package sshagent

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/scrub"
)

func pemKey(t *testing.T) (pemBytes []byte, pub ssh.PublicKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	pemBytes = pem.EncodeToMemory(block)
	signer, err := ssh.ParsePrivateKey(pemBytes)
	if err != nil {
		t.Fatal(err)
	}
	return pemBytes, signer.PublicKey()
}

func TestSignDoesNotReturnPrivateKey(t *testing.T) {
	pemBytes, want := pemKey(t)
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if _, err := a.PutItem(app.ItemOpts{Name: "github", Kind: protocol.ItemSSH, Token: pemBytes}); err != nil {
		t.Fatal(err)
	}

	dir, err := os.MkdirTemp("/tmp", "veil")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	srv, err := Listen(a, filepath.Join(dir, Name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	c, err := net.Dial("unix", srv.Path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	client := agent.NewClient(c)

	keys, err := client.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Fatalf("%d keys", len(keys))
	}
	listed, err := json.Marshal(keys)
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains(listed, pemBytes) || scrub.Contains(listed, []byte("PRIVATE KEY")) {
		t.Fatalf("private key leaked in list: %s", listed)
	}
	if keys[0].Comment != "github" {
		t.Fatalf("comment %q", keys[0].Comment)
	}

	data := []byte("git-upload-pack")
	sig, err := client.Sign(want, data)
	if err != nil {
		t.Fatal(err)
	}
	if err := want.Verify(data, sig); err != nil {
		t.Fatal(err)
	}

	_, injected, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Add(agent.AddedKey{PrivateKey: injected, Comment: "wire"}); err == nil {
		t.Fatal("accepted a key from the wire")
	}
	keys, err = client.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Fatalf("add mutated the vault: %d keys", len(keys))
	}

	items, err := a.Store.ListItems()
	if err != nil {
		t.Fatal(err)
	}
	meta, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains(meta, pemBytes) {
		t.Fatal("item list leaked pem")
	}
}

func TestNonSSHItemsAreNotListed(t *testing.T) {
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if _, err := a.AddItem("stripe", "https://api.stripe.com", []byte("sk_live_not_an_ssh_key")); err != nil {
		t.Fatal(err)
	}
	v := &vault{app: a}
	keys, err := v.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Fatalf("%d", len(keys))
	}
}
