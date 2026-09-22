// Package app is the product facade. CLI and MCP are thin wrappers around it.
package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/VortexNYC/veil/internal/audit"
	"github.com/VortexNYC/veil/internal/broker"
	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/device"
	"github.com/VortexNYC/veil/internal/grant"
	"github.com/VortexNYC/veil/internal/human"
	"github.com/VortexNYC/veil/internal/id"
	"github.com/VortexNYC/veil/internal/inject"
	"github.com/VortexNYC/veil/internal/material"
	"github.com/VortexNYC/veil/internal/oneimport"
	"github.com/VortexNYC/veil/internal/passkey"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store"
	"github.com/VortexNYC/veil/internal/workload"
)

const (
	DefaultOrg   = protocol.LocalOrgID
	DefaultHuman = "self"
	dbFile       = "vault.db"
	cfgFile      = "config.json"
)

// MemberCheck is identity-plane owner/member. Keto via glue. Not grants.
type MemberCheck interface {
	IsMember(ctx context.Context, orgID, identityID string) (bool, error)
	IsOwner(ctx context.Context, orgID, identityID string) (bool, error)
}

// Provisioner plants a human's membership in their org: Keto tuples plus the
// Kratos organization_id stamp. glue.Glue satisfies it; nil in local vaults.
type Provisioner interface {
	ProvisionMember(ctx context.Context, orgID, identityID string) error
	SetIdentityOrg(ctx context.Context, identityID, orgID string) error
}

var ErrExists = errors.New("app: vault already exists")

type config struct {
	OrgID   string `json:"org_id"`
	HumanID string `json:"human_id"`
}

// HumanVerifier verifies a human bearer token down to its subject.
// *human.Verifier is the production impl.
type HumanVerifier interface {
	Subject(ctx context.Context, rawToken string) (string, error)
}

type App struct {
	Dir       string
	OrgID     string
	HumanID   string
	Store     store.Store
	Auditor   audit.Auditor
	Broker    *broker.Broker
	Human     HumanVerifier
	Workload  *workload.Checker
	Members   MemberCheck
	Provision Provisioner
}

func Init(dir string) (*App, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(dir, dbFile)); err == nil {
		return nil, ErrExists
	}
	key, err := crypto.NewKey()
	if err != nil {
		return nil, err
	}
	_, priv, err := device.Generate()
	if err != nil {
		return nil, err
	}
	if err := wrapMaster(dir, key, priv); err != nil {
		return nil, err
	}
	cfg := config{OrgID: DefaultOrg, HumanID: DefaultHuman}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, cfgFile), raw, 0o600); err != nil {
		return nil, err
	}
	s, err := store.OpenSQLite(filepath.Join(dir, dbFile), key)
	if err != nil {
		return nil, err
	}
	if err := s.PutHuman(protocol.Principal{Kind: protocol.PrincipalHuman, ID: cfg.HumanID, OrgID: cfg.OrgID}); err != nil {
		_ = s.Close()
		return nil, err
	}
	return finish(dir, cfg, s, &audit.Sync{Store: s})
}

func Open(dir string) (*App, error) {
	key, err := loadMaster(dir)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, cfgFile))
	if err != nil {
		return nil, err
	}
	var cfg config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	s, err := store.OpenSQLite(filepath.Join(dir, dbFile), key)
	if err != nil {
		return nil, err
	}
	return finish(dir, cfg, s, &audit.Sync{Store: s})
}

func hasKeyMaterial(dir string) bool {
	if hasWraps(dir) {
		return true
	}
	_, err := os.Stat(filepath.Join(dir, keyFile))
	return err == nil
}

// OpenOrInit opens an existing vault, or creates one when there is no key material.
// A stray vault.db without wraps or master.key is not "empty" — Init would hit ErrExists
// and Open would print the misleading master.key miss that Railway crash-looped on.
func OpenOrInit(dir string) (*App, error) {
	if hasKeyMaterial(dir) {
		return Open(dir)
	}
	if _, err := os.Stat(filepath.Join(dir, dbFile)); err == nil {
		return nil, fmt.Errorf("app: %s exists without wraps/ or master.key", dbFile)
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return Init(dir)
}

// OpenPostgres opens a stateless origin backed by a Postgres DSN.
// The deployment KEK is read from the VEIL_KEK environment variable (hex) and
// seals per-org master keys in org_keys. VEIL_MASTER_KEY is the pre-multi-org
// legacy: when set, it is seeded once as this org's wrapped master and can be
// removed afterward.
func OpenPostgres(dsn string) (*App, error) {
	kek, err := loadKeyEnv("VEIL_KEK", true)
	if err != nil {
		return nil, err
	}
	s, err := store.OpenPostgres(dsn, kek)
	if err != nil {
		return nil, err
	}
	cfg := config{OrgID: DefaultOrg, HumanID: DefaultHuman}
	// Legacy seed: a pre-multi-tenant deployment still carries VEIL_MASTER_KEY.
	// Wrap it under the KEK as this org's row. An existing row is verified, not
	// overwritten — a mismatched VEIL_MASTER_KEY fails boot loudly rather than
	// stranding the org's ciphertext under the wrong key.
	ctx := context.Background()
	if master, err := loadKeyEnv("VEIL_MASTER_KEY", false); err != nil {
		_ = s.Close()
		return nil, err
	} else if master != nil {
		if err := s.EnsureOrgKey(ctx, cfg.OrgID, master); err != nil {
			_ = s.Close()
			return nil, fmt.Errorf("app: VEIL_MASTER_KEY does not match org_keys: %w", err)
		}
	} else if has, err := s.HasOrgKey(ctx, cfg.OrgID); err != nil {
		_ = s.Close()
		return nil, err
	} else if !has {
		// Fresh origin: mint the default org's master. It lands only as a
		// wrapped org_keys row — never persisted in plaintext. A concurrent
		// replica's winning insert is authoritative; our discarded mint is
		// fine because resolution always reads the committed row.
		fresh, err := crypto.NewKey()
		if err != nil {
			_ = s.Close()
			return nil, err
		}
		if err := s.EnsureOrgKey(ctx, cfg.OrgID, fresh); err != nil && !errors.Is(err, store.ErrOrgKeyMismatch) {
			_ = s.Close()
			return nil, err
		}
	}
	if _, err := s.Human(cfg.HumanID); errors.Is(err, store.ErrNotFound) {
		if err := s.PutHuman(protocol.Principal{Kind: protocol.PrincipalHuman, ID: cfg.HumanID, OrgID: cfg.OrgID}); err != nil {
			_ = s.Close()
			return nil, err
		}
	} else if err != nil {
		_ = s.Close()
		return nil, err
	}
	// The origin audits synchronously: every Use decision is durable in
	// Postgres before the response returns. The buffered Async path traded a
	// few hundred ms of crash-window loss for batch COPY efficiency — wrong
	// trade for an authorization system. A single-row INSERT costs ~0.6ms
	// against a measured ~10x DB headroom.
	return finish("", cfg, s, &audit.Sync{Store: s})
}

func loadKeyEnv(name string, required bool) ([]byte, error) {
	env := os.Getenv(name)
	if env == "" {
		if !required {
			return nil, nil
		}
		return nil, fmt.Errorf("app: %s is required for stateless origin", name)
	}
	key, err := hex.DecodeString(env)
	if err != nil {
		return nil, fmt.Errorf("app: %s is not valid hex: %w", name, err)
	}
	if len(key) != crypto.KeySize {
		return nil, fmt.Errorf("app: %s must be %d bytes (got %d)", name, crypto.KeySize, len(key))
	}
	return key, nil
}

func finish(dir string, cfg config, s store.Store, auditor audit.Auditor) (*App, error) {
	a := &App{
		Dir:      dir,
		OrgID:    cfg.OrgID,
		HumanID:  cfg.HumanID,
		Store:    s,
		Auditor:  auditor,
		Broker:   broker.New(s),
		Workload: workload.New(s),
	}
	a.Broker.Auditor = auditor
	if err := a.attachHydra(); err != nil {
		_ = s.Close()
		return nil, err
	}
	return a, nil
}

func (a *App) attachHydra() error {
	iss := os.Getenv("VEIL_HYDRA_ISSUER")
	if iss == "" {
		return nil
	}
	v, err := human.New(human.Config{
		Issuer:      iss,
		Audience:    os.Getenv("VEIL_HYDRA_CLIENT_ID"),
		RedirectURL: firstEnv("VEIL_HYDRA_REDIRECT", "BROKER_REDIRECT_URL"),
	})
	if err != nil {
		return err
	}
	a.Human = v
	return nil
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

func (a *App) Close() error {
	var errs []error
	if a.Auditor != nil {
		if err := a.Auditor.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if a.Store != nil {
		if err := a.Store.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

type ItemOpts struct {
	ID           string
	Name         string
	URI          string
	URIs         []string
	Tags         []string
	Kind         protocol.ItemKind
	Token        []byte
	Login        string
	TOTPSeed     []byte
	Refresh      []byte
	TokenURL     string
	ClientID     string
	ClientSecret []byte
	FileName     string
	MIME         string
	File         []byte
	Passkey      []byte
	Owner        protocol.Owner
	// OrgID is the caller's org. Empty falls back to the deployment default —
	// the local vault path; the origin always sets it from the principal.
	OrgID string
}

func (a *App) AddItem(name, uri string, secret []byte) (protocol.Item, error) {
	return a.PutItem(ItemOpts{Name: name, URI: uri, Token: secret})
}

func (a *App) PutItemFor(p protocol.Principal, opts ItemOpts) (protocol.Item, error) {
	if p.Kind != protocol.PrincipalHuman {
		return protocol.Item{}, fmt.Errorf("app: create is human")
	}
	ok, err := a.ownsVault(p)
	if err != nil {
		return protocol.Item{}, err
	}
	if !ok {
		opts.Owner = protocol.Owner{Kind: protocol.OwnerUser, ID: p.ID}
	}
	opts.OrgID = p.OrgID
	return a.PutItem(opts)
}

func (a *App) PutItem(opts ItemOpts) (protocol.Item, error) {
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		return protocol.Item{}, fmt.Errorf("app: empty item name")
	}
	itemID := strings.TrimSpace(opts.ID)
	if itemID == "" {
		itemID = name
		if !id.Valid(name) {
			generated, err := id.NewItem()
			if err != nil {
				return protocol.Item{}, err
			}
			itemID = generated
		}
	}
	if len(opts.Token) == 0 && len(opts.TOTPSeed) == 0 && len(opts.Refresh) == 0 && len(opts.File) == 0 && len(opts.Passkey) == 0 {
		return protocol.Item{}, fmt.Errorf("app: empty secret")
	}
	kind := opts.Kind
	if kind == "" {
		switch {
		case len(opts.Passkey) > 0:
			kind = protocol.ItemPasskey
		case len(opts.File) > 0:
			kind = protocol.ItemFile
		case len(opts.Refresh) > 0:
			kind = protocol.ItemOAuth
		default:
			kind = protocol.ItemAPIKey
		}
	}
	uris := opts.URIs
	if opts.URI != "" {
		uris = append([]string{opts.URI}, uris...)
	}
	org := opts.OrgID
	if org == "" {
		org = a.OrgID
	}
	owner := opts.Owner
	if owner.Kind == "" {
		owner = protocol.Owner{Kind: protocol.OwnerOrg, ID: org}
	}
	item := protocol.Item{
		ID:      itemID,
		OrgID:   org,
		Name:    name,
		Kind:    kind,
		Owner:   owner,
		URIs:    uris,
		Tags:    opts.Tags,
		HasTOTP: len(opts.TOTPSeed) > 0,
		HasFile: len(opts.File) > 0,
		Login:   strings.TrimSpace(opts.Login),
	}
	var blob []byte
	var err error
	switch {
	case len(opts.Passkey) > 0:
		blob = opts.Passkey
	case len(opts.File) > 0:
		blob, err = material.PackFile(opts.FileName, opts.MIME, opts.File)
	case len(opts.Refresh) > 0:
		blob, err = material.PackOAuth(opts.Refresh, []byte(opts.TokenURL), []byte(opts.ClientID), opts.ClientSecret)
	case len(opts.TOTPSeed) > 0:
		blob, err = material.Pack(opts.Token, opts.TOTPSeed)
	default:
		blob = opts.Token
	}
	if err != nil {
		return protocol.Item{}, err
	}
	blob, err = material.WithLogin(blob, opts.Login)
	if err != nil {
		return protocol.Item{}, err
	}
	if err := a.Store.PutItem(item, store.Secret(blob)); err != nil {
		return protocol.Item{}, err
	}
	return item, nil
}

type ImportResult struct {
	Names []string `json:"names"`
	Count int      `json:"count"`
}

func (a *App) ImportItems(p protocol.Principal, rows []oneimport.Row) (ImportResult, error) {
	if p.Kind != protocol.PrincipalHuman {
		return ImportResult{}, fmt.Errorf("app: import is human")
	}
	ok, err := a.ownsVault(p)
	if err != nil {
		return ImportResult{}, err
	}
	if !ok {
		return ImportResult{}, fmt.Errorf("app: import is owner")
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		itemID, err := id.NewItem()
		if err != nil {
			return ImportResult{}, err
		}
		item, err := a.PutItemFor(p, ItemOpts{
			ID:       itemID,
			Name:     row.Name,
			URIs:     row.URIs,
			Kind:     row.Kind,
			Token:    row.Token,
			Login:    row.Login,
			TOTPSeed: row.TOTPSeed,
			FileName: row.FileName,
			MIME:     row.MIME,
			File:     row.File,
		})
		if err != nil {
			return ImportResult{}, err
		}
		names = append(names, item.Name)
	}
	return ImportResult{Names: names, Count: len(names)}, nil
}

func unionURIs(have, add []string) []string {
	out := append([]string{}, have...)
	seen := make(map[string]struct{}, len(out)+len(add))
	for _, u := range out {
		seen[u] = struct{}{}
	}
	for _, u := range add {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	return out
}

func (a *App) UpdateItem(name string, replaceURIs, addURIs, tags []string, login string) (protocol.Item, error) {
	item, err := a.Store.Item(name)
	if err != nil {
		return protocol.Item{}, err
	}
	secret, err := a.Store.Secret(item.ID)
	if err != nil {
		return protocol.Item{}, err
	}
	if replaceURIs != nil {
		item.URIs = replaceURIs
	}
	if len(addURIs) > 0 {
		item.URIs = unionURIs(item.URIs, addURIs)
	}
	if tags != nil {
		item.Tags = tags
	}
	raw := []byte(secret)
	login = strings.TrimSpace(login)
	if login != "" {
		item.Login = login
		raw, err = material.WithLogin([]byte(secret), login)
		if err != nil {
			return protocol.Item{}, err
		}
	}
	if err := a.Store.PutItem(item, store.Secret(raw)); err != nil {
		return protocol.Item{}, err
	}
	return item, nil
}

func (a *App) ArchiveItem(name string) error {
	return a.Store.ArchiveItem(name)
}

func (a *App) DeleteItem(name string) error {
	return a.Store.DeleteItem(name)
}

func (a *App) WriteFile(name, dest string) error {
	item, err := a.Store.Item(name)
	if err != nil {
		return err
	}
	if !item.HasFile && item.Kind != protocol.ItemFile {
		return fmt.Errorf("app: not a file")
	}
	raw, err := a.Store.Secret(item.ID)
	if err != nil {
		return err
	}
	body, err := material.FileBytes(material.Unpack(secretBytes(raw)))
	if err != nil {
		return err
	}
	return os.WriteFile(dest, body, 0o600)
}

func secretBytes(s store.Secret) []byte { return []byte(s) }

// AddAgent registers an agent under the local vault's planted human.
func (a *App) AddAgent(name string) (protocol.Principal, error) {
	return a.addAgent(name, a.OrgID, a.HumanID)
}

// AddAgentFor registers an agent owned by the calling human, in their org.
func (a *App) AddAgentFor(owner protocol.Principal, name string) (protocol.Principal, error) {
	if owner.Kind != protocol.PrincipalHuman || owner.OrgID == "" {
		return protocol.Principal{}, fmt.Errorf("app: agent owner is a provisioned human")
	}
	return a.addAgent(name, owner.OrgID, owner.ID)
}

func (a *App) addAgent(name, orgID, humanID string) (protocol.Principal, error) {
	if !id.Valid(name) {
		return protocol.Principal{}, fmt.Errorf("app: invalid agent name %q", name)
	}
	// Agent ids are the global name namespace — a same-name row owned by a
	// different org or human is a collision, not an upsert.
	if existing, err := a.Store.Agent(name); err == nil {
		if existing.OrgID != orgID || existing.Owner.ID != humanID {
			return protocol.Principal{}, fmt.Errorf("app: agent name taken")
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return protocol.Principal{}, err
	}
	p := protocol.Principal{
		Kind:  protocol.PrincipalAgent,
		ID:    name,
		OrgID: orgID,
		Owner: protocol.Owner{Kind: protocol.OwnerUser, ID: humanID},
	}
	if err := a.Store.PutAgent(p); err != nil {
		return protocol.Principal{}, err
	}
	got, err := a.Store.Agent(name)
	if err != nil {
		return protocol.Principal{}, err
	}
	if got.RevokedAt != nil {
		return protocol.Principal{}, fmt.Errorf("%w: %s", ErrAgentRevoked, name)
	}
	return got, nil
}

func (a *App) RevokeAgent(actor protocol.Principal, agentID string) error {
	ok, err := a.ownsVault(actor)
	if err != nil {
		return err
	}
	if !ok {
		return ErrForbidden
	}
	if !id.Valid(agentID) {
		return fmt.Errorf("app: invalid agent name %q", agentID)
	}
	agent, err := a.Store.Agent(agentID)
	if err != nil {
		return err
	}
	if agent.OrgID != actor.OrgID {
		return ErrForbidden
	}
	if agent.Owner.Kind != protocol.OwnerUser || agent.Owner.ID != actor.ID {
		// An org owner may revoke a member's agent; a member only their own.
		own, err := a.ownsVault(actor)
		if err != nil {
			return err
		}
		if !own {
			return ErrForbidden
		}
	}
	if agent.RevokedAt != nil {
		return nil
	}
	now := time.Now().UTC()
	event := protocol.AuditEvent{
		Time:     now,
		OrgID:    a.OrgID,
		AgentID:  agentID,
		ItemID:   "",
		Action:   protocol.ActionRevoke,
		Decision: protocol.DecisionAllow,
		Reason:   "",
	}
	return a.Store.RevokeAgent(agentID, now, event)
}

func (a *App) BindWorkload(agentID, issuer, subject, audience string) (protocol.Workload, error) {
	if !id.Valid(agentID) {
		return protocol.Workload{}, fmt.Errorf("app: invalid agent name %q", agentID)
	}
	if issuer == "" || subject == "" || audience == "" {
		return protocol.Workload{}, fmt.Errorf("app: issuer, subject, and audience are required")
	}
	agent, err := a.Store.Agent(agentID)
	if err != nil {
		return protocol.Workload{}, err
	}
	if agent.RevokedAt != nil {
		return protocol.Workload{}, ErrAgentRevoked
	}
	w := protocol.Workload{
		AgentID:  agentID,
		Issuer:   issuer,
		Subject:  subject,
		Audience: audience,
	}
	if err := a.Store.PutWorkload(w); err != nil {
		return protocol.Workload{}, err
	}
	return w, nil
}

func (a *App) AgentFromOIDC(ctx context.Context, rawToken string) (protocol.Principal, error) {
	if IsSessionToken(rawToken) {
		return a.PrincipalFromSession(rawToken)
	}
	w := a.Workload
	if w == nil {
		w = workload.New(a.Store)
	}
	return w.Agent(ctx, rawToken)
}

// PrincipalFromOIDC is origin identity. Session lease first. Bound agent
// next. Else Hydra human plus Keto membership. Grants stay in the vault.
func (a *App) PrincipalFromOIDC(ctx context.Context, rawToken string) (protocol.Principal, error) {
	if IsSessionToken(rawToken) {
		return a.PrincipalFromSession(rawToken)
	}
	agent, err := a.AgentFromOIDC(ctx, rawToken)
	if err == nil {
		return agent, nil
	}
	if a.Human == nil {
		return protocol.Principal{}, err
	}
	sub, herr := a.Human.Subject(ctx, rawToken)
	if herr != nil {
		return protocol.Principal{}, err
	}
	// The humans row is org of record — a valid token without one is a signup
	// that has not provisioned yet, not a member of the default org.
	h, herr := a.Store.Human(sub)
	if errors.Is(herr, store.ErrNotFound) {
		return protocol.Principal{}, fmt.Errorf("app: not provisioned")
	}
	if herr != nil {
		return protocol.Principal{}, herr
	}
	p := protocol.Principal{Kind: protocol.PrincipalHuman, ID: sub, OrgID: h.OrgID}
	if a.Members == nil {
		return protocol.Principal{}, fmt.Errorf("app: not a member")
	}
	ok, merr := a.Members.IsMember(ctx, p.OrgID, p.ID)
	if merr != nil {
		return protocol.Principal{}, merr
	}
	if !ok {
		return protocol.Principal{}, fmt.Errorf("app: not a member")
	}
	return p, nil
}

// ProvisionHuman is signup. A verified human with no vault row gets an org
// (new UUID), an org master minted and sealed under the KEK via EnsureOrgKey,
// Keto owner+member tuples, and the Kratos organization_id stamp — all the
// same join key. Idempotent: a planted human returns their existing org, and
// a retry after a mid-flight failure resumes from the anchored humans row
// rather than minting a second org.
func (a *App) ProvisionHuman(ctx context.Context, rawToken string) (protocol.Principal, error) {
	if a.Human == nil {
		return protocol.Principal{}, fmt.Errorf("app: human verifier not configured")
	}
	sub, err := a.Human.Subject(ctx, rawToken)
	if err != nil {
		return protocol.Principal{}, err
	}

	orgID := ""
	if h, herr := a.Store.Human(sub); herr == nil {
		orgID = h.OrgID
	} else if errors.Is(herr, store.ErrNotFound) {
		orgID, err = id.NewOrg()
		if err != nil {
			return protocol.Principal{}, err
		}
		planted, perr := a.Store.PlantHuman(protocol.Principal{
			Kind: protocol.PrincipalHuman, ID: sub, OrgID: orgID,
		})
		if perr != nil {
			return protocol.Principal{}, perr
		}
		if !planted {
			// Concurrent provision won the anchor — converge on its org.
			h, rerr := a.Store.Human(sub)
			if rerr != nil {
				return protocol.Principal{}, rerr
			}
			orgID = h.OrgID
		}
	} else {
		return protocol.Principal{}, herr
	}

	// The key check is what makes "already provisioned" honest — a humans row
	// without a sealed master is a dead attempt, not a finished signup.
	if has, herr := a.Store.HasOrgKey(ctx, orgID); herr != nil {
		return protocol.Principal{}, herr
	} else if !has {
		master, kerr := crypto.NewKey()
		if kerr != nil {
			return protocol.Principal{}, kerr
		}
		if err := a.Store.EnsureOrgKey(ctx, orgID, master); err != nil {
			// A racing provision sealed a different master first — that is the
			// org's key now. Converge on it; only a truly absent row is an error.
			if !errors.Is(err, store.ErrOrgKeyMismatch) {
				return protocol.Principal{}, err
			}
			if has, herr := a.Store.HasOrgKey(ctx, orgID); herr != nil || !has {
				return protocol.Principal{}, err
			}
		}
	}
	// External tuples always run — they are idempotent (Keto 409 → nil) and a
	// prior attempt may have died after the key landed but before they did.
	if a.Provision != nil {
		if err := a.Provision.ProvisionMember(ctx, orgID, sub); err != nil {
			return protocol.Principal{}, err
		}
		if err := a.Provision.SetIdentityOrg(ctx, sub, orgID); err != nil {
			return protocol.Principal{}, err
		}
	}
	return protocol.Principal{Kind: protocol.PrincipalHuman, ID: sub, OrgID: orgID}, nil
}

func (a *App) ownsVault(p protocol.Principal) (bool, error) {
	if p.Kind != protocol.PrincipalHuman {
		return false, nil
	}
	if p.ID == a.HumanID {
		return true, nil
	}
	if a.Members == nil {
		return false, nil
	}
	return a.Members.IsOwner(context.Background(), p.OrgID, p.ID)
}

func (a *App) CanCreateGrant(p protocol.Principal) (bool, error) {
	return a.ownsVault(p)
}

func (a *App) OwnsVault(p protocol.Principal) (bool, error) {
	return a.ownsVault(p)
}

func (a *App) MayWriteItem(p protocol.Principal, item protocol.Item) (bool, error) {
	if p.Kind != protocol.PrincipalHuman {
		return false, nil
	}
	if item.OrgID != "" && item.OrgID != p.OrgID {
		return false, nil
	}
	if item.Owner.Kind == protocol.OwnerUser && item.Owner.ID == p.ID {
		return true, nil
	}
	ok, err := a.ownsVault(p)
	if err != nil {
		return false, err
	}
	return ok && item.Owner.Kind == protocol.OwnerOrg, nil
}

func (a *App) ItemsForPrincipal(p protocol.Principal) ([]protocol.Item, error) {
	granted, err := a.ItemsForAgent(p.ID)
	if err != nil {
		return nil, err
	}
	if p.Kind != protocol.PrincipalHuman {
		return granted, nil
	}
	ok, err := a.ownsVault(p)
	if err != nil {
		return nil, err
	}
	all, err := a.Store.ListItems()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(all))
	out := make([]protocol.Item, 0, len(all))
	add := func(item protocol.Item) {
		if _, dup := seen[item.ID]; dup {
			return
		}
		seen[item.ID] = struct{}{}
		out = append(out, item)
	}
	for _, item := range all {
		if item.OrgID != p.OrgID {
			continue
		}
		if item.Owner.Kind == protocol.OwnerUser && item.Owner.ID == p.ID {
			add(item)
			continue
		}
		if ok && item.Owner.Kind == protocol.OwnerOrg {
			add(item)
		}
	}
	for _, item := range granted {
		if item.OrgID != p.OrgID {
			continue
		}
		add(item)
	}
	return out, nil
}

// Match lists fill candidates for a URL. Metadata only. Never Secret().
func (a *App) Match(p protocol.Principal, rawURL string) ([]protocol.Item, error) {
	if p.Kind != protocol.PrincipalHuman {
		return nil, fmt.Errorf("app: fill is human")
	}
	items, err := a.ItemsForPrincipal(p)
	if err != nil {
		return nil, err
	}
	out := make([]protocol.Item, 0)
	for _, item := range items {
		if item.Archived || !item.Kind.Fillable() {
			continue
		}
		unbound := (item.Kind == protocol.ItemCard || item.Kind == protocol.ItemIdentity) && len(item.URIs) == 0
		if !unbound && !grant.HostAllowed(item, rawURL) {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

// FillEntry is native-host material. Not protocol.Item. Not MCP.
// Login is the fill username from the sealed envelope. Empty if unset.
// Never fall back to item.Name on the URL form.
// TOTP is "*" when a seed exists so KeePassXC-Browser will call get-totp.
// FillLogin(mintTotp) puts a 6-digit code instead. Never the seed.
type FillEntry struct {
	Login    string
	Name     string
	Password string
	UUID     string
	TOTP     string
}

func (a *App) FillLogins(p protocol.Principal, rawURL string) ([]FillEntry, error) {
	if p.Kind != protocol.PrincipalHuman {
		return nil, fmt.Errorf("app: fill is human")
	}
	items, err := a.ItemsForPrincipal(p)
	if err != nil {
		return nil, err
	}
	var out []FillEntry
	for _, item := range items {
		if !grant.HostAllowed(item, rawURL) {
			continue
		}
		e, ok := a.fillEntry(item)
		if !ok {
			continue
		}
		out = append(out, e)
	}
	if out == nil {
		out = []FillEntry{}
	}
	return out, nil
}

// FillLogin decrypts one item. mintTotp puts a 6-digit code in TOTP instead of "*".
func (a *App) FillLogin(p protocol.Principal, uuid string, mintTotp bool) (FillEntry, error) {
	if p.Kind != protocol.PrincipalHuman {
		return FillEntry{}, fmt.Errorf("app: fill is human")
	}
	uuid = strings.TrimSpace(uuid)
	if uuid == "" {
		return FillEntry{}, fmt.Errorf("app: fill uuid")
	}
	if !a.mayFillItem(p, uuid) {
		return FillEntry{}, fmt.Errorf("app: fill")
	}
	item, err := a.Store.Item(uuid)
	if err != nil {
		return FillEntry{}, fmt.Errorf("app: fill")
	}
	e, env, ok := a.unlockFill(item)
	if !ok {
		return FillEntry{}, fmt.Errorf("app: fill")
	}
	if e.Login == "" {
		e.Login = item.Login
	}
	if !mintTotp {
		return e, nil
	}
	if env.TOTP == "" {
		return FillEntry{}, fmt.Errorf("app: no totp")
	}
	code, err := material.Mint(env.TOTP, time.Now())
	if err != nil || code == "" {
		return FillEntry{}, fmt.Errorf("app: no totp")
	}
	e.TOTP = code
	return e, nil
}

// FillSyncRow is one replica pull record. Material is the envelope JSON.
// Not protocol.Item. Not MCP. Not OpenAPI.
type FillSyncRow struct {
	Item     protocol.Item
	Material []byte
}

// FillSync is the replica pull. Human only. since is reserved; this cut is a full pull.
func (a *App) FillSync(p protocol.Principal, since string) ([]FillSyncRow, string, error) {
	_ = since
	if p.Kind != protocol.PrincipalHuman {
		return nil, "", fmt.Errorf("app: fill is human")
	}
	items, err := a.ItemsForPrincipal(p)
	if err != nil {
		return nil, "", err
	}
	out := make([]FillSyncRow, 0, len(items))
	for _, item := range items {
		if item.Archived {
			continue
		}
		switch item.Kind {
		case protocol.ItemAPIKey, protocol.ItemPasskey, protocol.ItemCard, protocol.ItemIdentity:
		default:
			continue
		}
		sec, err := a.Store.Secret(item.ID)
		if err != nil {
			return nil, "", err
		}
		out = append(out, FillSyncRow{Item: item, Material: []byte(sec)})
	}
	return out, time.Now().UTC().Format(time.RFC3339), nil
}

func (a *App) fillEntry(item protocol.Item) (FillEntry, bool) {
	e, _, ok := a.unlockFill(item)
	return e, ok
}

func (a *App) unlockFill(item protocol.Item) (FillEntry, material.Envelope, bool) {
	if !item.Kind.Injects() {
		return FillEntry{}, material.Envelope{}, false
	}
	sec, err := a.Store.Secret(item.ID)
	if err != nil {
		return FillEntry{}, material.Envelope{}, false
	}
	env := material.Unpack([]byte(sec))
	if env.PasskeyPEM != "" {
		return FillEntry{}, material.Envelope{}, false
	}
	pass := env.Token
	if pass == "" {
		pass = string(sec)
	}
	totp := ""
	if env.TOTP != "" {
		totp = "*"
	}
	return FillEntry{
		Login:    env.Login,
		Name:     item.Name,
		Password: pass,
		UUID:     item.ID,
		TOTP:     totp,
	}, env, true
}

func (a *App) FillTOTP(p protocol.Principal, itemID string, now time.Time) (string, error) {
	if p.Kind != protocol.PrincipalHuman {
		return "", fmt.Errorf("app: fill is human")
	}
	if !a.mayFillItem(p, itemID) {
		return "", fmt.Errorf("app: no totp")
	}
	item, err := a.Store.Item(itemID)
	if err != nil || !item.HasTOTP {
		return "", fmt.Errorf("app: no totp")
	}
	sec, err := a.Store.Secret(item.ID)
	if err != nil {
		return "", err
	}
	env := material.Unpack([]byte(sec))
	code, err := material.Mint(env.TOTP, now)
	if err != nil || code == "" {
		return "", fmt.Errorf("app: no totp")
	}
	return code, nil
}

var (
	ErrTOTPEnrollDenied = errors.New("app: totp enroll denied")
	errTOTPEnrollSeed   = errors.New("app: totp seed")
	errTOTPEnrollExists = errors.New("app: totp already enrolled")
	errTOTPEnrollKind   = errors.New("app: totp enroll login")

	ErrAgentRevoked = errors.New("app: agent revoked")
)

func (a *App) AttachTOTP(p protocol.Principal, itemID, seed string) error {
	if p.Kind != protocol.PrincipalHuman {
		return ErrTOTPEnrollDenied
	}
	seed = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(seed), " ", ""))
	if seed == "" {
		return errTOTPEnrollSeed
	}
	item, err := a.Store.Item(itemID)
	if err != nil {
		return err
	}
	ok, err := a.MayWriteItem(p, item)
	if err != nil {
		return err
	}
	if !ok {
		return ErrTOTPEnrollDenied
	}
	if item.HasTOTP {
		return errTOTPEnrollExists
	}
	if item.Kind != protocol.ItemAPIKey && item.Kind != "" {
		return errTOTPEnrollKind
	}
	sec, err := a.Store.Secret(item.ID)
	if err != nil {
		return err
	}
	env := material.Unpack([]byte(sec))
	blob, err := material.Pack([]byte(env.Token), []byte(seed))
	if err != nil {
		return err
	}
	login := env.Login
	if login == "" {
		login = item.Login
	}
	blob, err = material.WithLogin(blob, login)
	if err != nil {
		return err
	}
	item.HasTOTP = true
	return a.Store.PutItem(item, store.Secret(blob))
}

func (a *App) FillPasskeyRegister(p protocol.Principal, origin string, publicKey json.RawMessage, extraURIs []string) (json.RawMessage, error) {
	if p.Kind != protocol.PrincipalHuman {
		return nil, fmt.Errorf("app: fill is human")
	}
	existing, err := a.passkeyRecords(p)
	if err != nil {
		return nil, err
	}
	cred, rec, code := passkey.Register(origin, publicKey, existing, true)
	if code != 0 {
		return passkey.ErrorResponse(code), nil
	}
	blob, err := material.PackPasskey(rec.PEM, rec.CredID, rec.RpID, rec.UserHandle)
	if err != nil {
		return passkey.ErrorResponse(passkey.ErrUnknown), nil
	}
	uris := []string{"https://" + rec.RpID}
	if origin != "" {
		uris = unionURIs(uris, []string{origin})
	}
	uris = unionURIs(uris, extraURIs)
	if _, err := a.PutItemFor(p, ItemOpts{
		Name:    rec.RpID,
		Kind:    protocol.ItemPasskey,
		URIs:    uris,
		Login:   rec.UserName,
		Passkey: blob,
	}); err != nil {
		return passkey.ErrorResponse(passkey.ErrUnknown), nil
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		return passkey.ErrorResponse(passkey.ErrUnknown), nil
	}
	return raw, nil
}

func (a *App) FillPasskeyGet(p protocol.Principal, origin string, publicKey json.RawMessage) (json.RawMessage, error) {
	if p.Kind != protocol.PrincipalHuman {
		return nil, fmt.Errorf("app: fill is human")
	}
	recs, err := a.passkeyRecords(p)
	if err != nil {
		return nil, err
	}
	cred, code := passkey.Assert(origin, publicKey, recs, true)
	if code != 0 {
		return passkey.ErrorResponse(code), nil
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		return passkey.ErrorResponse(passkey.ErrUnknown), nil
	}
	return raw, nil
}

func (a *App) passkeyRecords(p protocol.Principal) ([]passkey.Record, error) {
	items, err := a.ItemsForPrincipal(p)
	if err != nil {
		return nil, err
	}
	var out []passkey.Record
	for _, item := range items {
		if item.Kind != protocol.ItemPasskey {
			continue
		}
		sec, err := a.Store.Secret(item.ID)
		if err != nil {
			continue
		}
		env := material.Unpack([]byte(sec))
		if env.PasskeyPEM == "" || env.CredID == "" || env.RpID == "" {
			continue
		}
		out = append(out, passkey.Record{
			PEM:        env.PasskeyPEM,
			CredID:     env.CredID,
			RpID:       env.RpID,
			UserHandle: env.UserHandle,
			UserName:   env.Login,
		})
	}
	return out, nil
}

func (a *App) AddGrant(agentID, itemID string, level protocol.GrantLevel) (protocol.Grant, error) {
	self := protocol.Principal{Kind: protocol.PrincipalHuman, ID: a.HumanID, OrgID: a.OrgID}
	return a.GrantUntil(self, agentID, itemID, level, nil)
}

// GrantUntil grants grantee access to itemID on behalf of actor. The grant
// lives in the item's org; a grantee outside it is not a grantee, and an
// agent owned by a different human is not the actor's to delegate.
func (a *App) GrantUntil(actor protocol.Principal, grantee, itemID string, level protocol.GrantLevel, expires *time.Time) (protocol.Grant, error) {
	if !id.Principal(grantee) || !id.Valid(itemID) {
		return protocol.Grant{}, fmt.Errorf("app: invalid agent or item")
	}
	if level != protocol.Level1 && level != protocol.Level2 {
		return protocol.Grant{}, fmt.Errorf("app: level must be level1 or level2")
	}
	item, err := a.Store.Item(itemID)
	if err != nil {
		return protocol.Grant{}, err
	}
	if item.Archived {
		return protocol.Grant{}, fmt.Errorf("app: item archived")
	}
	// The grant lives in the item's org; a grantee outside it is not a grantee,
	// and an actor outside it is not its owner.
	org := item.OrgID
	if actor.OrgID != "" && org != actor.OrgID {
		return protocol.Grant{}, fmt.Errorf("app: unknown item")
	}
	agent, err := a.Store.Agent(grantee)
	switch {
	case err == nil:
		if agent.RevokedAt != nil {
			return protocol.Grant{}, ErrAgentRevoked
		}
		if agent.OrgID != org {
			return protocol.Grant{}, fmt.Errorf("app: unknown grantee")
		}
		if agent.Owner.Kind == protocol.OwnerUser && agent.Owner.ID != actor.ID {
			return protocol.Grant{}, fmt.Errorf("app: unknown grantee")
		}
	case errors.Is(err, store.ErrNotFound):
		if a.Members == nil {
			return protocol.Grant{}, fmt.Errorf("app: unknown grantee")
		}
		ok, merr := a.Members.IsMember(context.Background(), org, grantee)
		if merr != nil {
			return protocol.Grant{}, merr
		}
		if !ok {
			return protocol.Grant{}, fmt.Errorf("app: unknown grantee")
		}
	default:
		return protocol.Grant{}, err
	}
	g := protocol.Grant{
		ID:        id.Grant(grantee, itemID),
		OrgID:     org,
		AgentID:   grantee,
		ItemID:    itemID,
		Level:     level,
		Actions:   []protocol.ActionKind{protocol.ActionFetch},
		ExpiresAt: expires,
	}
	if err := a.Store.PutGrant(g); err != nil {
		return protocol.Grant{}, err
	}
	return g, nil
}

func (a *App) Use(ctx context.Context, agentID, itemID, method, rawURL string) (protocol.UseResult, error) {
	return a.UseFetch(ctx, agentID, itemID, protocol.Fetch{Method: method, URL: rawURL})
}

func (a *App) UseFetch(ctx context.Context, agentID, itemID string, fetch protocol.Fetch) (protocol.UseResult, error) {
	// Broker.Use loads the agent through UseAuth and reloads it before secret
	// access; an extra Store.Agent call here is redundant and adds a hot-path
	// round trip.
	return a.Broker.Use(ctx, protocol.Principal{ID: agentID}, protocol.UseRequest{
		ItemID: itemID,
		Action: protocol.ActionFetch,
		Fetch:  &fetch,
	})
}

func (a *App) UseSession(ctx context.Context, rawToken, itemID, method, rawURL string) (protocol.UseResult, error) {
	return a.UseFetchSession(ctx, rawToken, itemID, protocol.Fetch{Method: method, URL: rawURL})
}

func (a *App) UseFetchSession(ctx context.Context, rawToken, itemID string, fetch protocol.Fetch) (protocol.UseResult, error) {
	hash := sessionHash(rawToken)
	return a.Broker.UseSession(ctx, hash, protocol.UseRequest{
		ItemID: itemID,
		Action: protocol.ActionFetch,
		Fetch:  &fetch,
	})
}

func (a *App) ChildEnv(ctx context.Context, agentID string) ([]string, error) {
	agent, err := a.Store.Agent(agentID)
	if err != nil {
		return nil, err
	}
	return a.Broker.ChildEnv(ctx, agent)
}

func (a *App) InjectFile(src, dest string, pairs []string) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	out, err := inject.Expand(raw, inject.Map(pairs))
	if err != nil {
		return err
	}
	return os.WriteFile(dest, out, 0o600)
}

func (a *App) Approve(grantID string, ttl time.Duration) (protocol.Approval, error) {
	if a.Human != nil {
		return protocol.Approval{}, fmt.Errorf("app: use ApproveOIDC")
	}
	humanP, err := a.Store.Human(a.HumanID)
	if err != nil {
		return protocol.Approval{}, err
	}
	if _, err := a.Store.Grant(grantID); err != nil {
		return protocol.Approval{}, err
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return a.Broker.Approve(humanP, grantID, ttl)
}

// ApproveOIDC is Approve with a Hydra ID token. Membership is Keto, not sqlite.
// Planted `self` is the laptop stand-in when no issuer is configured.
func (a *App) ApproveOIDC(ctx context.Context, grantID, rawToken string, ttl time.Duration) (protocol.Approval, error) {
	if a.Human == nil {
		return protocol.Approval{}, fmt.Errorf("app: hydra issuer not configured")
	}
	if a.Members == nil {
		return protocol.Approval{}, fmt.Errorf("app: not a member")
	}
	sub, err := a.Human.Subject(ctx, rawToken)
	if err != nil {
		return protocol.Approval{}, err
	}
	h, err := a.Store.Human(sub)
	if err != nil {
		return protocol.Approval{}, fmt.Errorf("app: not provisioned")
	}
	p := protocol.Principal{Kind: protocol.PrincipalHuman, ID: sub, OrgID: h.OrgID}
	ok, err := a.Members.IsMember(ctx, p.OrgID, p.ID)
	if err != nil {
		return protocol.Approval{}, err
	}
	if !ok {
		return protocol.Approval{}, fmt.Errorf("app: not a member")
	}
	g, err := a.Store.Grant(grantID)
	if err != nil {
		return protocol.Approval{}, err
	}
	if g.OrgID != "" && g.OrgID != p.OrgID {
		return protocol.Approval{}, fmt.Errorf("app: not a member")
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return a.Broker.Approve(p, grantID, ttl)
}

// Offer wraps master to a second device's public key. nacl box.
// The blob is not JSON. The grant does not get a copy. Master is not a file.
func (a *App) Offer(peerPub []byte) ([]byte, error) {
	master, err := loadMaster(a.Dir)
	if err != nil {
		return nil, err
	}
	blob, err := device.Offer(master, peerPub)
	if err != nil {
		return nil, err
	}
	if err := persistWrap(a.Dir, peerPub, blob); err != nil {
		return nil, err
	}
	return blob, nil
}

// Accept writes device.key and a wrap. Not a second vault. Not plaintext master.
// Copy vault.db and config.json yourself. This is not sync.
func Accept(dir string, priv, blob []byte) error {
	master, err := device.Accept(blob, priv)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, deviceFile)
	existing, err := os.ReadFile(path)
	if err == nil && !bytes.Equal(existing, priv) {
		return fmt.Errorf("app: device.key exists")
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	pub, err := device.Public(priv)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, priv, 0o600); err != nil {
		return err
	}
	if err := persistWrap(dir, pub, blob); err != nil {
		return err
	}
	legacy := filepath.Join(dir, keyFile)
	if got, err := os.ReadFile(legacy); err == nil {
		if !bytes.Equal(got, master) {
			return fmt.Errorf("app: master.key exists")
		}
		if err := os.Remove(legacy); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (a *App) ItemsForAgent(agentID string) ([]protocol.Item, error) {
	agent, aerr := a.Store.Agent(agentID)
	if aerr != nil && !errors.Is(aerr, store.ErrNotFound) {
		return nil, aerr
	}
	agentFound := aerr == nil
	if agentFound && agent.RevokedAt != nil {
		return []protocol.Item{}, nil
	}
	grants, err := a.Store.ListGrants()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var out []protocol.Item
	for _, g := range grants {
		if g.AgentID != agentID {
			continue
		}
		// Grantee may be a human (no agent row) — the item-org filter in
		// ItemsForPrincipal covers that path. For a real agent row the grant
		// must live in the agent's org.
		if agentFound && g.OrgID != agent.OrgID {
			continue
		}
		if g.ExpiresAt != nil && !now.Before(*g.ExpiresAt) {
			continue
		}
		item, err := a.Store.Item(g.ItemID)
		if err != nil {
			return nil, err
		}
		if item.Archived {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func (a *App) mayFillItem(p protocol.Principal, itemID string) bool {
	items, err := a.ItemsForPrincipal(p)
	if err != nil {
		return false
	}
	for _, item := range items {
		if item.ID == itemID {
			return true
		}
	}
	return false
}
