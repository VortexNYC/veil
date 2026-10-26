// Package sshagent is the OpenSSH agent. It uses golang.org/x/crypto/ssh/agent.
// The broker signs. The private key never leaves the vault.
package sshagent

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/material"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/replica"
)

const Name = "ssh.sock"

// KeyDir is the drop-in directory under the user home — ~/.veil/ssh holds
// private keys the agent serves even before a replica pull lands them and
// while origin is unreachable.
const KeyDir = "ssh"

var errReadOnly = fmt.Errorf("sshagent: keys live in the vault")

type Server struct {
	Path string
	ln   net.Listener
}

// Options wires the agent's key sources. App is the local-vault broker;
// Replica is the sealed fill cache on an origin-mode device; Confirm gates
// each signature behind the human (Touch ID) — nil means no gate.
type Options struct {
	App     *app.App
	Replica *replica.Vault
	Confirm func(reason string) error
}

func DefaultPath(home string) string {
	return filepath.Join(home, Name)
}

func Listen(a *app.App, path string) (*Server, error) {
	return ListenOptions(Options{App: a}, path)
}

func ListenOptions(opts Options, path string) (*Server, error) {
	if path == "" {
		dir := ""
		if opts.App != nil {
			dir = opts.App.Dir
		} else if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, ".veil")
		}
		path = DefaultPath(dir)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := removeStale(path); err != nil {
		return nil, err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		_ = os.Remove(path)
		return nil, err
	}
	s := &Server{Path: path, ln: ln}
	go s.serve(opts)
	return s, nil
}

func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	err := s.ln.Close()
	if s.Path != "" {
		_ = os.Remove(s.Path)
	}
	return err
}

func (s *Server) serve(opts Options) {
	v := &vault{opts: opts}
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			_ = agent.ServeAgent(v, c)
			_ = c.Close()
		}(c)
	}
}

type vault struct {
	opts Options
}

func (v *vault) List() ([]*agent.Key, error) {
	signers, err := v.Signers()
	if err != nil {
		return nil, err
	}
	out := make([]*agent.Key, 0, len(signers))
	for _, s := range signers {
		pub := s.PublicKey()
		out = append(out, &agent.Key{
			Format:  pub.Type(),
			Blob:    pub.Marshal(),
			Comment: comment(s),
		})
	}
	return out, nil
}

func comment(s ssh.Signer) string {
	if w, ok := s.(*namedSigner); ok {
		return w.name
	}
	return ""
}

func (v *vault) Sign(key ssh.PublicKey, data []byte) (*ssh.Signature, error) {
	signers, err := v.Signers()
	if err != nil {
		return nil, err
	}
	want := string(key.Marshal())
	for _, s := range signers {
		if string(s.PublicKey().Marshal()) != want {
			continue
		}
		if v.opts.Confirm != nil {
			if err := v.opts.Confirm("Veil wants to sign an SSH request with " + comment(s)); err != nil {
				return nil, err
			}
		}
		return s.Sign(nil, data)
	}
	return nil, fmt.Errorf("sshagent: key not in vault")
}

func (v *vault) Add(agent.AddedKey) error   { return errReadOnly }
func (v *vault) Remove(ssh.PublicKey) error { return errReadOnly }
func (v *vault) RemoveAll() error           { return errReadOnly }
func (v *vault) Lock([]byte) error          { return errReadOnly }
func (v *vault) Unlock([]byte) error        { return errReadOnly }

// Signers collects every SSH signer the agent may serve: the local vault
// (App), the sealed origin replica, and drop-in keys under ~/.veil/ssh.
// Dedup is by public key — the same private key sealed in two places signs
// once per List.
func (v *vault) Signers() ([]ssh.Signer, error) {
	var out []ssh.Signer
	seen := map[string]bool{}
	add := func(s ssh.Signer, err error) {
		if err != nil {
			return
		}
		k := string(s.PublicKey().Marshal())
		if seen[k] {
			return
		}
		seen[k] = true
		out = append(out, s)
	}
	if v.opts.App != nil {
		items, err := v.opts.App.Store.ListItems()
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if item.Kind != protocol.ItemSSH {
				continue
			}
			add(signerFor(v.opts.App, item))
		}
	}
	if v.opts.Replica != nil {
		for _, item := range v.opts.Replica.Items() {
			if item.Kind != protocol.ItemSSH || item.Archived {
				continue
			}
			raw := v.opts.Replica.Material(item.ID)
			if raw == "" {
				continue
			}
			s, err := ssh.ParsePrivateKey(pemBytes([]byte(raw)))
			add(&namedSigner{Signer: s, name: item.Name}, err)
		}
	}
	for _, s := range homeSigners() {
		add(s, nil)
	}
	return out, nil
}

// homeSigners loads private keys dropped in ~/.veil/ssh — *.pem and id_*,
// mode 0600. Unreadable or non-key files are skipped; a bad drop-in must
// never break the agent.
func homeSigners() []ssh.Signer {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dir := filepath.Join(home, ".veil", KeyDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []ssh.Signer
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".pub") ||
			(!strings.HasSuffix(name, ".pem") && !strings.HasPrefix(name, "id_")) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		s, err := ssh.ParsePrivateKey(pemBytes(raw))
		if err != nil {
			continue
		}
		out = append(out, &namedSigner{Signer: s, name: name})
	}
	return out
}

type namedSigner struct {
	ssh.Signer
	name string
}

func signerFor(a *app.App, item protocol.Item) (ssh.Signer, error) {
	sec, err := a.Store.Secret(item.ID)
	if err != nil {
		return nil, err
	}
	raw := pemBytes(sec)
	s, err := ssh.ParsePrivateKey(raw)
	if err != nil {
		return nil, err
	}
	return &namedSigner{Signer: s, name: item.Name}, nil
}

func pemBytes(sec []byte) []byte {
	env := material.Unpack(sec)
	if strings.Contains(env.Token, "PRIVATE KEY") {
		return []byte(env.Token)
	}
	return sec
}

func removeStale(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if fi.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("sshagent: %s exists and is not a socket", path)
	}
	return os.Remove(path)
}
