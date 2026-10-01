package app

import (
	"encoding/json"
	"testing"

	"github.com/VortexNYC/veil/internal/material"
	"github.com/VortexNYC/veil/internal/oneimport"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/scrub"
)

func TestImportItemsCreatesLoginAndCard(t *testing.T) {
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	const pan = "4111111111111111"
	const pass = "s3cret"
	blob, err := material.PackCard(pan, "12", "2030", "123", "Ada")
	if err != nil {
		t.Fatal(err)
	}
	human := protocol.Principal{Kind: protocol.PrincipalHuman, ID: "self", OrgID: protocol.LocalOrgID}
	got, err := a.ImportItems(human, []oneimport.Row{
		{Name: "GitHub", Kind: protocol.ItemAPIKey, URIs: []string{"https://github.com"}, Login: "ada", Token: []byte(pass)},
		{Name: "Amex", Kind: protocol.ItemCard, Token: blob},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Count != 2 || got.Names[0] != "GitHub" || got.Names[1] != "Amex" {
		t.Fatalf("%+v", got)
	}
	items, err := a.Store.ListItems()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jsonNames(items)
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains(raw, []byte(pass)) || scrub.Contains(raw, []byte(pan)) {
		t.Fatal("list leaked secret")
	}
}

func TestImportItemsDuplicateNames(t *testing.T) {
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	human := protocol.Principal{Kind: protocol.PrincipalHuman, ID: "self", OrgID: protocol.LocalOrgID}
	got, err := a.ImportItems(human, []oneimport.Row{
		{Name: "Microsoft", Login: "ada", Token: []byte("a")},
		{Name: "Microsoft", Login: "bob", Token: []byte("b")},
		{Name: "Microsoft", Login: "carol", Token: []byte("c")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Count != 3 || got.Names[0] != "Microsoft" || got.Names[1] != "Microsoft" || got.Names[2] != "Microsoft" {
		t.Fatalf("%+v", got)
	}
	items, err := a.Store.ListItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("n=%d", len(items))
	}
	ids := map[string]struct{}{}
	for _, it := range items {
		if it.Name != "Microsoft" || it.ID == "" || it.ID == "microsoft" {
			t.Fatalf("%+v", it)
		}
		ids[it.ID] = struct{}{}
	}
	if len(ids) != 3 {
		t.Fatalf("ids %d", len(ids))
	}
}

func TestImportItemsDoesNotClobberExistingID(t *testing.T) {
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	const planted = "planted-github-secret"
	if _, err := a.PutItem(ItemOpts{Name: "github", Token: []byte(planted)}); err != nil {
		t.Fatal(err)
	}
	human := protocol.Principal{Kind: protocol.PrincipalHuman, ID: "self", OrgID: protocol.LocalOrgID}
	got, err := a.ImportItems(human, []oneimport.Row{{Name: "github", Login: "ada", Token: []byte("imported-github-secret")}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Count != 1 || len(got.Skipped) != 0 {
		t.Fatalf("%+v", got)
	}
	secret, err := a.Store.Secret("github")
	if err != nil {
		t.Fatal(err)
	}
	if string(secret) != planted {
		t.Fatal("import reused github id")
	}
	items, err := a.Store.ListItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("n=%d", len(items))
	}
}

// Dedup is skip, never update: an export row matching an existing item on
// name+kind+login+URIs keeps the sealed secret as is — a stale export
// cannot roll back a rotated password.
func TestImportItemsSkipsExistingMatch(t *testing.T) {
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	const planted = "planted-github-secret"
	if _, err := a.PutItem(ItemOpts{Name: "github", Token: []byte(planted)}); err != nil {
		t.Fatal(err)
	}
	human := protocol.Principal{Kind: protocol.PrincipalHuman, ID: "self", OrgID: protocol.LocalOrgID}
	got, err := a.ImportItems(human, []oneimport.Row{{Name: "github", Token: []byte("stale-export-secret")}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Count != 0 || len(got.Skipped) != 1 || got.Skipped[0] != "github" {
		t.Fatalf("%+v", got)
	}
	secret, err := a.Store.Secret("github")
	if err != nil {
		t.Fatal(err)
	}
	if string(secret) != planted {
		t.Fatal("skip rewrote the secret")
	}
	items, err := a.Store.ListItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("n=%d", len(items))
	}
}

func TestImportItemsRepeatImportIsIdempotent(t *testing.T) {
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	human := protocol.Principal{Kind: protocol.PrincipalHuman, ID: "self", OrgID: protocol.LocalOrgID}
	rows := []oneimport.Row{
		{Name: "GitHub", Kind: protocol.ItemAPIKey, URIs: []string{"https://github.com"}, Login: "ada", Token: []byte("s3cret")},
		{Name: "memo", Kind: protocol.ItemFile, File: []byte("body"), FileName: "memo.txt", MIME: "text/plain"},
	}
	got, err := a.ImportItems(human, rows)
	if err != nil {
		t.Fatal(err)
	}
	if got.Count != 2 {
		t.Fatalf("%+v", got)
	}
	again, err := a.ImportItems(human, rows)
	if err != nil {
		t.Fatal(err)
	}
	if again.Count != 0 || len(again.Names) != 0 || len(again.Skipped) != 2 {
		t.Fatalf("%+v", again)
	}
	items, err := a.Store.ListItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("n=%d", len(items))
	}
}

// Same row twice inside one file dedups too — the first insert lands in
// the seen set before the second row is checked.
func TestImportItemsSkipsDupWithinFile(t *testing.T) {
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	human := protocol.Principal{Kind: protocol.PrincipalHuman, ID: "self", OrgID: protocol.LocalOrgID}
	got, err := a.ImportItems(human, []oneimport.Row{
		{Name: "GitHub", Login: "ada", Token: []byte("a")},
		{Name: "GitHub", Login: "ada", Token: []byte("b")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Count != 1 || len(got.Skipped) != 1 {
		t.Fatalf("%+v", got)
	}
	items, err := a.Store.ListItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("n=%d", len(items))
	}
}

// Different login or URI set under the same name is a different item —
// both land under fresh ids.
func TestImportItemsSameNameDifferentIdentityNotDup(t *testing.T) {
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	human := protocol.Principal{Kind: protocol.PrincipalHuman, ID: "self", OrgID: protocol.LocalOrgID}
	got, err := a.ImportItems(human, []oneimport.Row{
		{Name: "GitHub", Login: "ada", URIs: []string{"https://github.com"}, Token: []byte("a")},
		{Name: "GitHub", Login: "ada", URIs: []string{"https://github.com", "https://gist.github.com"}, Token: []byte("b")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Count != 2 {
		t.Fatalf("%+v", got)
	}
}

func TestImportItemsSSHAndNoteNoPEMInList(t *testing.T) {
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	const pem = "-----BEGIN OPENSSH PRIVATE KEY-----\nfake\n-----END OPENSSH PRIVATE KEY-----"
	const note = "ssn-must-not-list"
	human := protocol.Principal{Kind: protocol.PrincipalHuman, ID: "self", OrgID: protocol.LocalOrgID}
	got, err := a.ImportItems(human, []oneimport.Row{
		{Name: "laptop", Kind: protocol.ItemSSH, Token: []byte(pem)},
		{Name: "memo", Kind: protocol.ItemFile, File: []byte(note), FileName: "memo.txt", MIME: "text/plain"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Count != 2 {
		t.Fatalf("%+v", got)
	}
	items, err := a.Store.ListItems()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains(raw, []byte(pem)) || scrub.Contains(raw, []byte(note)) {
		t.Fatal("list leaked ssh or note")
	}
	kinds := map[protocol.ItemKind]int{}
	for _, it := range items {
		kinds[it.Kind]++
	}
	if kinds[protocol.ItemSSH] != 1 || kinds[protocol.ItemFile] != 1 {
		t.Fatalf("%v", kinds)
	}
}

func TestImportItemsRejectsAgent(t *testing.T) {
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	agent := protocol.Principal{Kind: protocol.PrincipalAgent, ID: "claude", OrgID: protocol.LocalOrgID}
	if _, err := a.ImportItems(agent, []oneimport.Row{{Name: "x", Token: []byte("p")}}); err == nil {
		t.Fatal("expected error")
	}
}

func jsonNames(items []protocol.Item) ([]byte, error) {
	var b []byte
	for _, it := range items {
		b = append(b, it.Name...)
		b = append(b, it.Login...)
		for _, u := range it.URIs {
			b = append(b, u...)
		}
	}
	return b, nil
}
