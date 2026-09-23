package app

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store"
	"github.com/jackc/pgx/v5"
)

type fakeVerifier struct{ sub string }

func (f fakeVerifier) Subject(_ context.Context, _ string) (string, error) { return f.sub, nil }

type fakeProvision struct {
	mu     sync.Mutex
	tuples [][2]string
	stamps [][2]string
}

func (f *fakeProvision) ProvisionMember(_ context.Context, orgID, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tuples = append(f.tuples, [2]string{orgID, id})
	return nil
}

func (f *fakeProvision) SetIdentityOrg(_ context.Context, id, orgID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stamps = append(f.stamps, [2]string{id, orgID})
	return nil
}

// IdentityOrg mirrors Kratos: the last stamp wins, none is "".
func (f *fakeProvision) IdentityOrg(_ context.Context, id string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	org := ""
	for _, s := range f.stamps {
		if s[0] == id {
			org = s[1]
		}
	}
	return org, nil
}

// orgMembers keys membership on org|id — cross-org isolation needs the org leg.
type orgMembers map[string]bool

func (m orgMembers) IsMember(_ context.Context, orgID, id string) (bool, error) {
	return m[orgID+"|"+id], nil
}

func (m orgMembers) IsOwner(_ context.Context, orgID, id string) (bool, error) {
	return m[orgID+"|"+id], nil
}

func provisioned(t *testing.T, a *App, prov *fakeProvision, sub string) protocol.Principal {
	t.Helper()
	a.Human = fakeVerifier{sub: sub}
	p, err := a.ProvisionHuman(context.Background(), "tok-"+sub)
	if err != nil {
		t.Fatalf("provision %s: %v", sub, err)
	}
	if p.OrgID == "" {
		t.Fatalf("provision %s: empty org", sub)
	}
	return p
}

func TestProvisionHumanEndToEnd(t *testing.T) {
	dir := t.TempDir()
	a, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	prov := &fakeProvision{}
	a.Provision = prov
	a.Members = orgMembers{}

	p := provisioned(t, a, prov, "sub-1")
	if p.OrgID == a.OrgID {
		t.Fatal("provisioned org must not be the vault default")
	}
	if len(prov.tuples) != 1 || prov.tuples[0] != [2]string{p.OrgID, "sub-1"} {
		t.Fatalf("keto tuples %+v", prov.tuples)
	}
	if len(prov.stamps) != 1 || prov.stamps[0] != [2]string{"sub-1", p.OrgID} {
		t.Fatalf("kratos stamps %+v", prov.stamps)
	}
	a.Members = orgMembers{p.OrgID + "|sub-1": true}

	// The fresh signup can item → agent → grant → use with no operator.
	item, err := a.PutItemFor(p, ItemOpts{
		Name: "stripe", URI: "https://api.stripe.com", Token: []byte("sk-test"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.OrgID != p.OrgID {
		t.Fatalf("item org %q", item.OrgID)
	}
	agent, err := a.AddAgentFor(p, "bot")
	if err != nil {
		t.Fatal(err)
	}
	if agent.OrgID != p.OrgID {
		t.Fatalf("agent org %q", agent.OrgID)
	}
	if _, err := a.GrantUntil(p, agent.ID, item.ID, protocol.Level2, nil); err != nil {
		t.Fatal(err)
	}
	res, err := a.Use(context.Background(), agent.ID, item.ID, "GET", "https://api.stripe.com/v1/charges")
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision != protocol.DecisionAllow {
		t.Fatalf("use %s: %s", res.Decision, res.Reason)
	}
}

func TestProvisionHumanIdempotent(t *testing.T) {
	dir := t.TempDir()
	a, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	prov := &fakeProvision{}
	a.Provision = prov
	a.Members = orgMembers{}

	first := provisioned(t, a, prov, "sub-1")
	second := provisioned(t, a, prov, "sub-1")
	if first.OrgID != second.OrgID {
		t.Fatalf("reprovision minted a second org: %q vs %q", first.OrgID, second.OrgID)
	}
}

// An invited human carries the inviter's org in the Kratos stamp plus a Keto
// member tuple — provisioning must join that org, not mint a fresh one. The
// stamp alone (no member tuple) is not an invitation: it could be stale.
func TestProvisionJoinsInvitedOrg(t *testing.T) {
	dir := t.TempDir()
	a, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	prov := &fakeProvision{}
	a.Provision = prov

	owner := provisioned(t, a, prov, "sub-owner")
	a.Members = orgMembers{owner.OrgID + "|sub-owner": true}

	// Invite: stamps the identity + plants the member tuple (glue does both).
	if err := prov.SetIdentityOrg(context.Background(), "sub-inv", owner.OrgID); err != nil {
		t.Fatal(err)
	}
	a.Members.(orgMembers)[owner.OrgID+"|sub-inv"] = true

	inv := provisioned(t, a, prov, "sub-inv")
	if inv.OrgID != owner.OrgID {
		t.Fatalf("invitee got %q, want inviter's org %q", inv.OrgID, owner.OrgID)
	}

	// Stamp without membership is not an invitation — fresh org.
	if err := prov.SetIdentityOrg(context.Background(), "sub-stale", owner.OrgID); err != nil {
		t.Fatal(err)
	}
	stale := provisioned(t, a, prov, "sub-stale")
	if stale.OrgID == owner.OrgID {
		t.Fatal("stale stamp joined the org without a member tuple")
	}

	// No stamp at all — fresh org (self-signup).
	fresh := provisioned(t, a, prov, "sub-fresh")
	if fresh.OrgID == owner.OrgID || fresh.OrgID == stale.OrgID {
		t.Fatal("unstamped signup joined an org")
	}
}

func TestProvisionHumanConcurrentConverges(t *testing.T) {
	dir := t.TempDir()
	a, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Provision = &fakeProvision{}
	a.Members = orgMembers{}
	a.Human = fakeVerifier{sub: "sub-race"}

	res := make(chan protocol.Principal, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() {
			p, err := a.ProvisionHuman(context.Background(), "tok")
			if err != nil {
				errs <- err
				return
			}
			res <- p
		}()
	}
	org := ""
	for i := 0; i < 8; i++ {
		select {
		case p := <-res:
			if org == "" {
				org = p.OrgID
			} else if p.OrgID != org {
				t.Fatalf("racing provisions diverged: %q vs %q", org, p.OrgID)
			}
		case err := <-errs:
			t.Fatalf("provision race errored: %v", err)
		}
	}
}

func TestProvisionCrossOrgIsolation(t *testing.T) {
	dir := t.TempDir()
	a, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Provision = &fakeProvision{}

	p1 := provisioned(t, a, a.Provision.(*fakeProvision), "sub-1")
	p2 := provisioned(t, a, a.Provision.(*fakeProvision), "sub-2")
	if p1.OrgID == p2.OrgID {
		t.Fatal("two signups must not share an org")
	}
	a.Members = orgMembers{
		p1.OrgID + "|sub-1": true,
		p2.OrgID + "|sub-2": true,
	}

	item, err := a.PutItemFor(p1, ItemOpts{Name: "stripe", URI: "https://api.stripe.com", Token: []byte("sk")})
	if err != nil {
		t.Fatal(err)
	}
	// Org-2's agent cannot be granted org-1's item.
	ag2, err := a.AddAgentFor(p2, "bot")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.GrantUntil(p2, ag2.ID, item.ID, protocol.Level2, nil); err == nil {
		t.Fatal("cross-org grant succeeded")
	}
	// An org-2 owner cannot grant org-1's item to an org-1 human — the actor's
	// org binds the grant, not just the grantee's.
	if _, err := a.GrantUntil(p2, "sub-1", item.ID, protocol.Level2, nil); err == nil {
		t.Fatal("cross-org human grant succeeded")
	}
	// Org-2's list does not see org-1's item.
	items, err := a.ItemsForPrincipal(p2)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.ID == item.ID {
			t.Fatal("cross-org item visible")
		}
	}
	// Org-2 cannot write org-1's item even as an org owner.
	if ok, err := a.MayWriteItem(p2, item); err != nil || ok {
		t.Fatalf("cross-org MayWriteItem: %v %v", ok, err)
	}
	// Org-2 cannot squat org-1's agent name.
	ag1, err := a.AddAgentFor(p1, "shared-name")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddAgentFor(p2, "shared-name"); err == nil {
		t.Fatal("cross-org agent name hijack succeeded")
	}
	if _, _, err := a.CreateSession(p2, ag2.ID, 0, 1); err != nil {
		t.Fatalf("same-org session failed: %v", err)
	}
	// Org-2 cannot revoke org-1's session by id.
	sess, _, err := a.CreateSession(p1, ag1.ID, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.RevokeSession(p2, sess.ID); err == nil {
		t.Fatal("cross-org session revoke succeeded")
	}
	// Org-2 cannot approve org-1's grant — ApproveOIDC binds grant→approver org.
	g, err := a.GrantUntil(p1, ag1.ID, item.ID, protocol.Level1, nil)
	if err != nil {
		t.Fatal(err)
	}
	a.Human = fakeVerifier{sub: "sub-2"}
	if _, err := a.ApproveOIDC(context.Background(), g.ID, "tok-2", 0); err == nil {
		t.Fatal("cross-org approve succeeded")
	}
}

// The real EnsureOrgKey race: N goroutines provision the same subject against
// Postgres. PlantHuman is the anchor; exactly one org must emerge, and exactly
// one wrapped master — losers hit ErrOrgKeyMismatch and converge. Memory and
// sqlite cannot produce this path (their EnsureOrgKey is a no-op).
func TestProvisionHumanConcurrentPostgres(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	schema := "test_" + strings.NewReplacer("/", "_", "-", "_").Replace(t.Name())
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, schema)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %s`, schema)); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(ctx); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	kek, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.OpenPostgres(u.String(), kek)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	prov := &fakeProvision{}
	a := &App{Store: s, Human: fakeVerifier{sub: "sub-race"}, Provision: prov}

	const n = 16
	res := make(chan protocol.Principal, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			p, err := a.ProvisionHuman(ctx, "tok")
			if err != nil {
				errs <- err
				return
			}
			res <- p
		}()
	}
	org := ""
	for i := 0; i < n; i++ {
		select {
		case p := <-res:
			if p.OrgID == "" {
				t.Fatal("empty org")
			}
			if org == "" {
				org = p.OrgID
			} else if p.OrgID != org {
				t.Fatalf("racing provisions diverged: %q vs %q", org, p.OrgID)
			}
		case err := <-errs:
			t.Fatalf("provision errored: %v", err)
		}
	}
	if has, err := s.HasOrgKey(ctx, org); err != nil || !has {
		t.Fatalf("org key missing after race: %v %v", has, err)
	}

	// Two distinct subjects get two distinct orgs with independent keys —
	// each org's item seals and opens under its own master on real pg.
	a.Members = orgMembers{}
	for _, sub := range []string{"sub-a", "sub-b"} {
		a.Human = fakeVerifier{sub: sub}
		p, err := a.ProvisionHuman(ctx, "tok-"+sub)
		if err != nil {
			t.Fatal(err)
		}
		a.Members.(orgMembers)[p.OrgID+"|"+sub] = true
		item, err := a.PutItemFor(p, ItemOpts{Name: "k-" + sub, Token: []byte("secret-" + sub)})
		if err != nil {
			t.Fatalf("put %s: %v", sub, err)
		}
		sec, err := s.Secret(item.ID)
		if err != nil {
			t.Fatalf("secret %s: %v", sub, err)
		}
		if string(sec) != "secret-"+sub {
			t.Fatalf("roundtrip %s: %q", sub, sec)
		}
		if item.OrgID != p.OrgID {
			t.Fatalf("item org %q want %q", item.OrgID, p.OrgID)
		}
	}
}
