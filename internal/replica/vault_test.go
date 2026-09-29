package replica

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/scrub"
)

type errStore struct{ err error }

func (e errStore) Get() ([]byte, error) { return nil, e.err }
func (e errStore) Put([]byte) error     { return nil }

func TestSealedBoxHidesCatalogAndSecret(t *testing.T) {
	dir := t.TempDir()
	key, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	path := Path(dir)
	v, err := Open(path, key)
	if err != nil {
		t.Fatal(err)
	}
	const secret = "sk_live_REPLICA_SECRET"
	const host = "https://gitlab.com"
	if err := v.Put(protocol.Item{
		ID:    "gitlab.com",
		Name:  "gitlab.com",
		Kind:  protocol.ItemAPIKey,
		URIs:  []string{host},
		Login: "throwaway@veil.nyc",
	}, []byte(`{"v":1,"token":"`+secret+`"}`)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range [][]byte{
		[]byte(secret),
		[]byte("gitlab.com"),
		[]byte(host),
		[]byte("throwaway@veil.nyc"),
		[]byte("veil-replica-v1"),
		[]byte(key),
	} {
		if bytes.Contains(raw, needle) {
			t.Fatalf("plaintext on disk: %q", needle)
		}
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatal("secret on disk")
	}
	wrong, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path, wrong); err == nil {
		t.Fatal("opened with the wrong key")
	}
	got, err := Open(path, key)
	if err != nil {
		t.Fatal(err)
	}
	if got.Material("gitlab.com") == "" || got.Items()[0].Name != "gitlab.com" {
		t.Fatal("roundtrip")
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %s", st.Mode())
	}
}

func TestMemKeyNeverTouchesDisk(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("TMPDIR", dir)
	ks := Mem()
	key, err := Unlock(ks)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("keystore wrote the filesystem")
	}
	again, err := Unlock(ks)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(key, again) {
		t.Fatal("key rotated")
	}
}

func TestUnlockDoesNotMintOnGetError(t *testing.T) {
	if _, err := Unlock(errStore{err: errors.New("keychain down")}); err == nil {
		t.Fatal("minted over a Get error")
	}
	if _, err := Unlock(shortStore{}); err == nil {
		t.Fatal("accepted a short key")
	}
}

type shortStore struct{}

func (shortStore) Get() ([]byte, error) { return []byte("short"), nil }
func (shortStore) Put([]byte) error     { return nil }

func TestPutFlushFailureDoesNotKeepRow(t *testing.T) {
	dir := t.TempDir()
	key, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	v, err := Open(Path(dir), key)
	if err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(dir, "notdir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	v.path = filepath.Join(blocker, FileName)
	if err := v.Put(protocol.Item{ID: "x", Name: "x"}, []byte(`{"v":1}`)); err == nil {
		t.Fatal("flush")
	}
	if v.Len() != 0 {
		t.Fatalf("kept row after flush fail %d", v.Len())
	}
}

func TestUnlockRejectsNilStore(t *testing.T) {
	if _, err := Unlock(nil); err == nil {
		t.Fatal("nil keystore")
	}
}

func TestAttachMemIsVolatile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VEIL_REPLICA_KEYSTORE", "mem")
	key, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	box, err := Open(Path(dir), key)
	if err != nil {
		t.Fatal(err)
	}
	if err := box.Put(protocol.Item{ID: "old", Name: "old"}, []byte(`{"v":1}`)); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	v := Attach(dir)
	if v == nil {
		t.Fatal("mem attach")
	}
	if v.Len() != 0 {
		t.Fatalf("volatile vault read the disk box: %d", v.Len())
	}
	if err := v.Put(protocol.Item{ID: "x", Name: "x"}, []byte(`{"v":1}`)); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("volatile vault rewrote replica.box")
	}
}

func TestOpenOrRecoverStaleBox(t *testing.T) {
	dir := t.TempDir()
	keyA, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	keyB, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	v, err := Open(Path(dir), keyA)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Put(protocol.Item{ID: "stale", Name: "stale"}, []byte(`{"v":1}`)); err != nil {
		t.Fatal(err)
	}
	got, err := openOrRecover(Path(dir), keyB)
	if err != nil {
		t.Fatal(err)
	}
	if got.Len() != 0 {
		t.Fatalf("recovered vault kept stale rows: %d", got.Len())
	}
	if err := got.Put(protocol.Item{ID: "fresh", Name: "fresh"}, []byte(`{"v":1}`)); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(Path(dir), keyB)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Material("fresh") == "" || reopened.Material("stale") != "" {
		t.Fatal("reopened box")
	}
}

func TestPath(t *testing.T) {
	if filepath.Base(Path("/tmp/veil")) != FileName {
		t.Fatal(Path("/tmp/veil"))
	}
}
