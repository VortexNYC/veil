package sshagent

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/replica"
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
	t.Setenv("HOME", t.TempDir()) // ~/.veil/ssh drop-ins must not leak into the count
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

func TestReplicaBackedSigning(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // ~/.veil/ssh drop-ins must not leak into the count
	pemBytes, want := pemKey(t)
	key, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	v, err := replica.Open(filepath.Join(t.TempDir(), replica.FileName), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(v.Close)
	if err := v.Put(protocol.Item{ID: "ssh1", Name: "mini", Kind: protocol.ItemSSH}, pemBytes); err != nil {
		t.Fatal(err)
	}
	archived, _ := pemKey(t)
	if err := v.Put(protocol.Item{ID: "ssh2", Name: "retired", Kind: protocol.ItemSSH, Archived: true}, archived); err != nil {
		t.Fatal(err)
	}
	login, _ := pemKey(t)
	if err := v.Put(protocol.Item{ID: "pw1", Name: "github", Kind: protocol.ItemAPIKey}, login); err != nil {
		t.Fatal(err)
	}

	dir, err := os.MkdirTemp("/tmp", "veil")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	srv, err := ListenOptions(Options{Replica: v}, filepath.Join(dir, Name))
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
	if len(keys) != 1 || keys[0].Comment != "mini" {
		t.Fatalf("keys %+v", keys)
	}
	data := []byte("git-upload-pack")
	sig, err := client.Sign(want, data)
	if err != nil {
		t.Fatal(err)
	}
	if err := want.Verify(data, sig); err != nil {
		t.Fatal(err)
	}
}

func TestConfirmGatesSignNotList(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	pemBytes, want := pemKey(t)
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if _, err := a.PutItem(app.ItemOpts{Name: "github", Kind: protocol.ItemSSH, Token: pemBytes}); err != nil {
		t.Fatal(err)
	}

	calls := 0
	v := &vault{opts: Options{App: a, Confirm: func(reason string) error {
		calls++
		if !strings.Contains(reason, "github") {
			t.Errorf("reason %q missing item name", reason)
		}
		return nil
	}}}
	if _, err := v.List(); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("List prompted for Touch ID (%d calls)", calls)
	}
	data := []byte("session-bound")
	sig, err := v.Sign(want, data)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("confirm calls %d", calls)
	}
	if err := want.Verify(data, sig); err != nil {
		t.Fatal(err)
	}

	// A denied confirm fails closed — no signature leaves the vault.
	denied := &vault{opts: Options{App: a, Confirm: func(string) error {
		return errors.New("denied")
	}}}
	if _, err := denied.Sign(want, data); err == nil {
		t.Fatal("signed after confirm denied")
	}

	// A key the vault does not hold never reaches confirm.
	_, other := pemKey(t)
	before := calls
	if _, err := v.Sign(other, data); err == nil {
		t.Fatal("signed an unknown key")
	}
	if calls != before {
		t.Fatal("unknown key prompted for Touch ID")
	}
}

func TestNonSSHItemsAreNotListed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if _, err := a.AddItem("stripe", "https://api.stripe.com", []byte("sk_live_not_an_ssh_key")); err != nil {
		t.Fatal(err)
	}
	v := &vault{opts: Options{App: a}}
	keys, err := v.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Fatalf("%d", len(keys))
	}
}
