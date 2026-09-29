// Package replica is the fill host's local cache. One sealed box.
// Names, URIs, logins, and material all live inside the AEAD.
// The wrapping key is never a file next to the box. Agents who copy
// replica.box get ciphertext they cannot grind (random 256-bit key,
// not a password). 1Password/Bitwarden encrypt the whole local vault
// the same way; they do not leave a device.key beside plaintext sqlite
// metadata.
package replica

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/protocol"
)

const (
	FileName = "replica.box"
	Version  = 1
	magic    = "veil-replica-v1\n"
)

type Row struct {
	Item     protocol.Item `json:"item"`
	Material string        `json:"material"`
}

type disk struct {
	V     int   `json:"v"`
	Items []Row `json:"items"`
}

type Vault struct {
	path string
	key  []byte
	rows []Row
}

func Path(dir string) string {
	return filepath.Join(dir, FileName)
}

func Open(path string, key []byte) (*Vault, error) {
	if len(key) != crypto.KeySize {
		return nil, fmt.Errorf("replica: key")
	}
	v := &Vault{path: path, key: append([]byte(nil), key...)}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return v, nil
		}
		return nil, err
	}
	plain, err := crypto.Open(key, raw)
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(plain, []byte(magic)) {
		return nil, crypto.ErrAuth
	}
	var d disk
	if json.Unmarshal(plain[len(magic):], &d) != nil || d.V != Version {
		return nil, fmt.Errorf("replica: box")
	}
	v.rows = d.Items
	if v.rows == nil {
		v.rows = []Row{}
	}
	return v, nil
}

// Attach opens the replica vault for dir under the platform keystore. A
// volatile keystore (VEIL_REPLICA_KEYSTORE=mem) gets an in-memory vault that
// never reads or writes replica.box — its key dies with the process, so any
// box it sealed would be unreadable next boot. On disk, a box that fails to
// open (stale key, corrupt bytes) is removed and re-created; origin is truth
// and the next pull repopulates.
func Attach(dir string) *Vault {
	ks := Platform()
	if ks == nil {
		return nil
	}
	if _, volatile := ks.(*mem); volatile {
		return &Vault{rows: []Row{}}
	}
	key, err := Unlock(ks)
	if err != nil {
		return nil
	}
	v, err := openOrRecover(Path(dir), key)
	if err != nil {
		return nil
	}
	return v
}

func openOrRecover(path string, key []byte) (*Vault, error) {
	v, err := Open(path, key)
	if err == nil {
		return v, nil
	}
	if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
		return nil, err
	}
	return Open(path, key)
}

func (v *Vault) Close() {
	if v == nil {
		return
	}
	for i := range v.key {
		v.key[i] = 0
	}
	v.rows = nil
}

func (v *Vault) Len() int {
	if v == nil {
		return 0
	}
	return len(v.rows)
}

func (v *Vault) Items() []protocol.Item {
	if v == nil {
		return nil
	}
	out := make([]protocol.Item, 0, len(v.rows))
	for _, r := range v.rows {
		out = append(out, r.Item)
	}
	return out
}

func (v *Vault) Material(id string) string {
	if v == nil {
		return ""
	}
	for _, r := range v.rows {
		if r.Item.ID == id {
			return r.Material
		}
	}
	return ""
}

func (v *Vault) Put(item protocol.Item, material []byte) error {
	if v == nil || item.ID == "" || len(material) == 0 {
		return fmt.Errorf("replica: put")
	}
	row := Row{Item: item, Material: string(material)}
	prev := append([]Row(nil), v.rows...)
	next := append([]Row(nil), v.rows...)
	replaced := false
	for i, r := range next {
		if r.Item.ID == item.ID {
			next[i] = row
			replaced = true
			break
		}
	}
	if !replaced {
		next = append(next, row)
	}
	v.rows = next
	if err := v.flush(); err != nil {
		v.rows = prev
		return err
	}
	return nil
}

func (v *Vault) flush() error {
	if v.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(v.path), 0o700); err != nil {
		return err
	}
	body, err := json.Marshal(disk{V: Version, Items: v.rows})
	if err != nil {
		return err
	}
	plain := append([]byte(magic), body...)
	box, err := crypto.Seal(v.key, plain)
	if err != nil {
		return err
	}
	tmp := v.path + ".tmp"
	if err := os.WriteFile(tmp, box, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, v.path)
}
