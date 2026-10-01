package oneimport

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/VortexNYC/veil/internal/material"
	"github.com/VortexNYC/veil/internal/protocol"
)

func TestParseCSVChromeLogin(t *testing.T) {
	raw := []byte("name,url,username,password\nGitHub,https://github.com,ada,s3cret\n")
	rows, err := Parse("chrome.csv", raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "GitHub" || rows[0].Login != "ada" || string(rows[0].Token) != "s3cret" {
		t.Fatalf("%+v", rows)
	}
	if rows[0].Kind != protocol.ItemAPIKey || len(rows[0].URIs) != 1 || rows[0].URIs[0] != "https://github.com" {
		t.Fatalf("%+v", rows[0])
	}
}

func TestParseCSV1PasswordTOTP(t *testing.T) {
	raw := []byte("Title,Url,Username,Password,OTPAuth\nMail,https://mail.example,ada,pw,otpauth://totp/Mail?secret=JBSWY3DPEHPK3PXP\n")
	rows, err := Parse("1p.csv", raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || string(rows[0].TOTPSeed) != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("%+v", rows)
	}
}

func TestParseCSVBitwardenLogin(t *testing.T) {
	raw := []byte("folder,favorite,type,name,notes,fields,reprompt,login_uri,login_username,login_password,login_totp\n" +
		"work,false,login,GitHub,,,false,https://github.com,ada@example.com,s3cret,\n" +
		",false,login,Steam,,,false,https://store.steampowered.com,gabe,hunter2,otpauth://totp/Steam?secret=jbswy3dp\n" +
		",false,note,wifi passphrase,,,false,,,,\n")
	rows, err := Parse("bitwarden.csv", raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("%+v", rows)
	}
	if rows[0].Name != "GitHub" || rows[0].Login != "ada@example.com" || string(rows[0].Token) != "s3cret" || rows[0].URIs[0] != "https://github.com" {
		t.Fatalf("%+v", rows[0])
	}
	if string(rows[1].TOTPSeed) != "JBSWY3DP" {
		t.Fatalf("totp %+v", rows[1])
	}
}

func TestParseCSVApplePasswords(t *testing.T) {
	raw := []byte("Title,URL,Username,Password,Notes,OTPAuth\n" +
		"GitHub,https://github.com,ada,s3cret,,otpauth://totp/GitHub?secret=JBSWY3DPEHPK3PXP\n" +
		"Bank,https://bank.example,ada,pass2,a note,\n")
	rows, err := Parse("Passwords.csv", raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Name != "GitHub" || rows[0].Login != "ada" || string(rows[0].Token) != "s3cret" {
		t.Fatalf("%+v", rows)
	}
	if string(rows[0].TOTPSeed) != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("totp %+v", rows[0])
	}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseApplePasswordsFixture(t *testing.T) {
	rows, err := Parse("Passwords.csv", fixture(t, "apple-passwords.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("n=%d %+v", len(rows), rows)
	}
	if rows[0].Name != "GitHub" || rows[0].Kind != protocol.ItemAPIKey || rows[0].Login != "ada" || string(rows[0].Token) != "s3cret" {
		t.Fatalf("%+v", rows[0])
	}
	if len(rows[0].URIs) != 1 || rows[0].URIs[0] != "https://github.com" {
		t.Fatalf("uris %+v", rows[0])
	}
	if string(rows[0].TOTPSeed) != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("totp %+v", rows[0])
	}
	if rows[1].Name != "Bank" || len(rows[1].TOTPSeed) != 0 {
		t.Fatalf("%+v", rows[1])
	}
	if string(rows[2].TOTPSeed) != "JBSWY3DP" {
		t.Fatalf("totp %+v", rows[2])
	}
}

func TestParseBitwardenCSVFixture(t *testing.T) {
	rows, err := Parse("bitwarden.csv", fixture(t, "bitwarden.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("n=%d %+v", len(rows), rows)
	}
	if rows[0].Name != "GitHub" || rows[0].Login != "ada@example.com" || string(rows[0].Token) != "s3cret" {
		t.Fatalf("%+v", rows[0])
	}
	if string(rows[0].TOTPSeed) != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("totp %+v", rows[0])
	}
	if string(rows[1].TOTPSeed) != "JBSWY3DP" {
		t.Fatalf("totp %+v", rows[1])
	}
	if rows[2].Name != "wifi passphrase" || rows[2].Kind != protocol.ItemFile || string(rows[2].File) != "synthetic-note-body" {
		t.Fatalf("note %+v", rows[2])
	}
}

func TestParseBitwardenJSONFixture(t *testing.T) {
	rows, err := Parse("bitwarden_export.json", fixture(t, "bitwarden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 {
		t.Fatalf("n=%d %+v", len(rows), rows)
	}
	login := rows[0]
	if login.Name != "GitHub" || login.Kind != protocol.ItemAPIKey || login.Login != "ada@example.com" || string(login.Token) != "s3cret" {
		t.Fatalf("login %+v", login)
	}
	if len(login.URIs) != 2 || login.URIs[0] != "https://github.com" || login.URIs[1] != "https://github.com/login" {
		t.Fatalf("uris %+v", login.URIs)
	}
	if string(login.TOTPSeed) != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("totp %+v", login)
	}
	if string(rows[1].TOTPSeed) != "JBSWY3DP" {
		t.Fatalf("totp %+v", rows[1])
	}
	if rows[2].Kind != protocol.ItemFile || string(rows[2].File) != "synthetic-note-body" || rows[2].MIME != "text/plain" {
		t.Fatalf("note %+v", rows[2])
	}
	if rows[3].Kind != protocol.ItemCard {
		t.Fatalf("card kind %s", rows[3].Kind)
	}
	card := material.Unpack(rows[3].Token)
	if card.Number != "4111111111111111" || card.CVV != "123" || card.ExpMonth != "12" || card.ExpYear != "2030" || card.GivenName != "Ada Lovelace" {
		t.Fatalf("card %+v", card)
	}
	if rows[4].Kind != protocol.ItemIdentity {
		t.Fatalf("identity kind %s", rows[4].Kind)
	}
	ident := material.Unpack(rows[4].Token)
	if ident.GivenName != "Ada" || ident.FamilyName != "Lovelace" || ident.Address != "1 Street, Flat 2" || ident.City != "London" || ident.Region != "LDN" || ident.Postal != "E1" || ident.Country != "UK" || ident.Phone != "+44" || ident.Email != "ada@example.com" {
		t.Fatalf("identity %+v", ident)
	}
	for _, r := range rows {
		if r.Name == "Trashed" || r.Name == "Empty" {
			t.Fatalf("deleted/empty row imported: %+v", r)
		}
	}
}

func TestParseBitwardenJSONSniffedWithoutExtension(t *testing.T) {
	rows, err := Parse("dump", fixture(t, "bitwarden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 {
		t.Fatalf("n=%d", len(rows))
	}
}

func TestParseBitwardenJSONEncryptedRejected(t *testing.T) {
	raw := []byte(`{"encrypted":true,"passwordProtected":true,"salt":"x","kdfIterations":600000,"data":"abc"}`)
	if _, err := Parse("bitwarden_export.json", raw); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseJSONNotBitwardenRejected(t *testing.T) {
	if _, err := Parse("x.json", []byte(`{"hello":"world"}`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseCSVSkipsEmptyPassword(t *testing.T) {
	raw := []byte("name,url,username,password\nGitHub,https://github.com,ada,\n")
	rows, err := Parse("chrome.csv", raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("%+v", rows)
	}
}

func TestParseCSVRejectsNoPasswordColumn(t *testing.T) {
	if _, err := Parse("x.csv", []byte("foo,bar\n1,2\n")); err == nil {
		t.Fatal("expected error")
	}
}

func TestParse1PUXLoginCardIdentity(t *testing.T) {
	const pan = "4111111111111111"
	const cvv = "123"
	const pass = "s3cret"
	data := `{
  "accounts": [{
    "vaults": [{
      "items": [
        {
          "categoryUuid": "001",
          "overview": {"title": "GitHub", "url": "https://github.com", "urls": [{"url": "https://github.com/login"}]},
          "details": {
            "loginFields": [
              {"designation": "username", "value": "ada"},
              {"designation": "password", "value": "` + pass + `"}
            ]
          }
        },
        {
          "categoryUuid": "002",
          "overview": {"title": "Amex"},
          "details": {
            "sections": [{
              "fields": [
                {"id": "ccnum", "value": "` + pan + `"},
                {"id": "cvv", "value": "` + cvv + `"},
                {"id": "expiry", "value": "12/2030"},
                {"id": "cardholder", "value": "Ada"}
              ]
            }]
          }
        },
        {
          "categoryUuid": "004",
          "overview": {"title": "Home"},
          "details": {
            "sections": [{
              "fields": [
                {"id": "firstname", "value": "Ada"},
                {"id": "lastname", "value": "Lovelace"},
                {"id": "address1", "value": "1 Street"},
                {"id": "defphone", "value": "+44"}
              ]
            }]
          }
        },
        {
          "categoryUuid": "001",
          "state": "archived",
          "overview": {"title": "Old"},
          "details": {"loginFields": [{"designation": "password", "value": "nope"}]}
        }
      ]
    }]
  }]
}`
	raw := zipBytes(t, "export.data", []byte(data))
	rows, err := Parse("export.1pux", raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("n=%d", len(rows))
	}
	if rows[0].Name != "GitHub" || rows[0].Login != "ada" || string(rows[0].Token) != pass {
		t.Fatalf("login %+v", rows[0])
	}
	if rows[1].Kind != protocol.ItemCard {
		t.Fatalf("card kind %s", rows[1].Kind)
	}
	card := material.Unpack(rows[1].Token)
	if card.Number != pan || card.CVV != cvv || card.ExpMonth != "12" || card.GivenName != "Ada" {
		t.Fatalf("card %+v", card)
	}
	if rows[2].Kind != protocol.ItemIdentity {
		t.Fatalf("identity kind %s", rows[2].Kind)
	}
	ident := material.Unpack(rows[2].Token)
	if ident.GivenName != "Ada" || ident.FamilyName != "Lovelace" || ident.Phone != "+44" {
		t.Fatalf("identity %+v", ident)
	}
}

func TestParseCSVSkipsArchived(t *testing.T) {
	raw := []byte("Title,Url,Username,Password,OTPAuth,Archived\nOld,https://x,ada,pw,,true\nKeep,https://y,ada,pw,,false\n")
	rows, err := Parse("1p.csv", raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "Keep" {
		t.Fatalf("%+v", rows)
	}
}

func TestParseCSVTOTPOnly(t *testing.T) {
	raw := []byte("Title,Url,Username,Password,OTPAuth\nGitHub,https://github.com,ada,,otpauth://totp/GitHub?secret=JBSWY3DPEHPK3PXP\n")
	rows, err := Parse("1p.csv", raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "GitHub" || string(rows[0].Token) != "" || string(rows[0].TOTPSeed) != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("%+v", rows)
	}
}

func TestParse1PUXCardExpiryYYYYMM(t *testing.T) {
	data := `{
  "accounts": [{"vaults": [{"items": [{
    "categoryUuid": "002",
    "overview": {"title": "Amex"},
    "details": {"sections": [{"fields": [
      {"id": "ccnum", "value": "4111111111111111"},
      {"id": "expiry", "value": "203012"}
    ]}]}
  }]}]}]
}`
	rows, err := Parse("export.1pux", zipBytes(t, "export.data", []byte(data)))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("n=%d", len(rows))
	}
	card := material.Unpack(rows[0].Token)
	if card.ExpMonth != "12" || card.ExpYear != "2030" {
		t.Fatalf("expiry %+v", card)
	}
}

func TestParse1PUXIdentityNestedAddress(t *testing.T) {
	data := `{
  "accounts": [{"vaults": [{"items": [{
    "categoryUuid": "004",
    "overview": {"title": "Home"},
    "details": {"sections": [{"fields": [
      {"id": "firstname", "value": "Ada"},
      {"id": "address", "value": {"address": {"street": "1 Street", "city": "London", "state": "LDN", "zip": "E1", "country": "UK"}}}
    ]}]}
  }]}]}]
}`
	rows, err := Parse("export.1pux", zipBytes(t, "export.data", []byte(data)))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("n=%d", len(rows))
	}
	ident := material.Unpack(rows[0].Token)
	if ident.Address != "1 Street" || ident.City != "London" || ident.Postal != "E1" || ident.Country != "UK" {
		t.Fatalf("identity %+v", ident)
	}
}

func TestParse1PUXSSHAndNote(t *testing.T) {
	const pem = "-----BEGIN OPENSSH PRIVATE KEY-----\nfake\n-----END OPENSSH PRIVATE KEY-----"
	const note = "ssn-not-a-password"
	data := fmt.Sprintf(`{
  "accounts": [{"vaults": [{"items": [
    {
      "categoryUuid": "114",
      "overview": {"title": "laptop"},
      "details": {"sections": [{"fields": [{"id": "private_key", "value": %q}]}]}
    },
    {
      "categoryUuid": "003",
      "overview": {"title": "memo"},
      "details": {"notesPlain": %q}
    }
  ]}]}]
}`, pem, note)
	rows, err := Parse("export.1pux", zipBytes(t, "export.data", []byte(data)))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("n=%d", len(rows))
	}
	if rows[0].Kind != protocol.ItemSSH || string(rows[0].Token) != pem {
		t.Fatalf("ssh %+v", rows[0])
	}
	if rows[1].Kind != protocol.ItemFile || string(rows[1].File) != note || rows[1].MIME != "text/plain" {
		t.Fatalf("note %+v", rows[1])
	}
}

func TestParseEmptyRejected(t *testing.T) {
	if _, err := Parse("x.csv", nil); err == nil {
		t.Fatal("expected error")
	}
}

func zipBytes(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
