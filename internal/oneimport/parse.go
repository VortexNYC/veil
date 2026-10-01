// Package oneimport turns a 1Password .1pux zip, an Apple Passwords or
// Bitwarden CSV, or a Bitwarden unencrypted JSON export into draft items.
// One shot onto origin. Not Connect. Not MCP. Not sync.
package oneimport

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/VortexNYC/veil/internal/material"
	"github.com/VortexNYC/veil/internal/protocol"
)

const maxFile = 32 << 20

// Row is one item ready for PutItem. Token is the password or a packed
// card/identity envelope. Secrets stay here until create seals them.
type Row struct {
	Name     string
	Kind     protocol.ItemKind
	URIs     []string
	Login    string
	Token    []byte
	TOTPSeed []byte
	File     []byte
	FileName string
	MIME     string
}

func Parse(name string, raw []byte) ([]Row, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("import: empty file")
	}
	if len(raw) > maxFile {
		return nil, fmt.Errorf("import: file too large")
	}
	lower := strings.ToLower(name)
	if bytes.HasPrefix(raw, []byte("PK")) || strings.HasSuffix(lower, ".1pux") {
		return parse1pux(raw)
	}
	sniff := bytes.TrimSpace(bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF}))
	if len(sniff) > 0 && sniff[0] == '{' || strings.HasSuffix(lower, ".json") {
		return parseBitwardenJSON(raw)
	}
	return parseCSV(raw)
}

func parse1pux(raw []byte) ([]Row, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("import: not a 1pux zip")
	}
	var data []byte
	for _, f := range zr.File {
		if strings.EqualFold(f.Name, "export.data") || strings.HasSuffix(strings.ToLower(f.Name), "/export.data") {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("import: 1pux export.data")
			}
			data, err = io.ReadAll(io.LimitReader(rc, maxFile))
			_ = rc.Close()
			if err != nil {
				return nil, fmt.Errorf("import: 1pux export.data")
			}
			break
		}
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("import: 1pux missing export.data")
	}
	var dump struct {
		Accounts []struct {
			Vaults []struct {
				Items []puxItem `json:"items"`
			} `json:"vaults"`
		} `json:"accounts"`
	}
	if json.Unmarshal(data, &dump) != nil {
		return nil, fmt.Errorf("import: 1pux json")
	}
	var out []Row
	for _, acct := range dump.Accounts {
		for _, vault := range acct.Vaults {
			for _, it := range vault.Items {
				row, ok := puxRow(it)
				if ok {
					out = append(out, row)
				}
			}
		}
	}
	return out, nil
}

type puxItem struct {
	Category string `json:"categoryUuid"`
	State    string `json:"state"`
	Overview struct {
		Title string `json:"title"`
		URL   string `json:"url"`
		URLs  []struct {
			URL string `json:"url"`
		} `json:"urls"`
	} `json:"overview"`
	Details struct {
		LoginFields []struct {
			Designation string `json:"designation"`
			Value       string `json:"value"`
			Type        string `json:"type"`
		} `json:"loginFields"`
		Password string `json:"password"`
		Notes    string `json:"notesPlain"`
		Sections []struct {
			Fields []struct {
				ID    string          `json:"id"`
				Value json.RawMessage `json:"value"`
			} `json:"fields"`
		} `json:"sections"`
	} `json:"details"`
}

func puxRow(it puxItem) (Row, bool) {
	if strings.EqualFold(it.State, "archived") || strings.EqualFold(it.State, "deleted") {
		return Row{}, false
	}
	name := strings.TrimSpace(it.Overview.Title)
	if name == "" {
		return Row{}, false
	}
	uris := puxURIs(it)
	switch it.Category {
	case "002":
		return puxCard(name, uris, it)
	case "003":
		return puxNote(name, uris, it)
	case "004":
		return puxIdentity(name, uris, it)
	case "114":
		return puxSSH(name, uris, it)
	case "001", "005", "110", "112", "":
		return puxLogin(name, uris, it)
	default:
		return Row{}, false
	}
}

func puxURIs(it puxItem) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(u string) {
		u = strings.TrimSpace(u)
		if u == "" {
			return
		}
		if _, ok := seen[u]; ok {
			return
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	add(it.Overview.URL)
	for _, u := range it.Overview.URLs {
		add(u.URL)
	}
	return out
}

func puxLogin(name string, uris []string, it puxItem) (Row, bool) {
	login, pass, totp := "", "", ""
	for _, f := range it.Details.LoginFields {
		switch strings.ToLower(strings.TrimSpace(f.Designation)) {
		case "username":
			login = f.Value
		case "password":
			pass = f.Value
		}
		if strings.EqualFold(f.Type, "OTP") {
			totp = otpSeed(f.Value)
		}
	}
	if pass == "" {
		pass = it.Details.Password
	}
	if totp == "" {
		totp = otpSeed(puxField(it, "TOTP", "totp", "one-time password"))
	}
	if strings.TrimSpace(pass) == "" && totp == "" {
		return Row{}, false
	}
	return Row{
		Name:     name,
		Kind:     protocol.ItemAPIKey,
		URIs:     uris,
		Login:    strings.TrimSpace(login),
		Token:    []byte(pass),
		TOTPSeed: []byte(totp),
	}, true
}

func puxCard(name string, uris []string, it puxItem) (Row, bool) {
	number := puxField(it, "ccnum", "cardNumber", "creditCardNumber", "cc-number")
	cvv := puxField(it, "cvv", "cvc", "scid", "cc-csc")
	holder := puxField(it, "cardholder", "cardholderName", "nameoncard", "cc-name")
	month, year := splitExpiry(puxField(it, "expiry", "expdate", "expires", "cc-exp"))
	blob, err := material.PackCard(number, month, year, cvv, holder)
	if err != nil {
		return Row{}, false
	}
	return Row{Name: name, Kind: protocol.ItemCard, URIs: uris, Token: blob}, true
}

func puxIdentity(name string, uris []string, it puxItem) (Row, bool) {
	street, city, state, zip, country := puxAddress(it)
	blob, err := material.PackIdentity(
		puxField(it, "firstname", "firstName", "given-name"),
		puxField(it, "lastname", "lastName", "family-name"),
		street,
		city,
		state,
		zip,
		country,
		puxField(it, "defphone", "phone", "tel"),
		puxField(it, "email"),
	)
	if err != nil {
		return Row{}, false
	}
	return Row{Name: name, Kind: protocol.ItemIdentity, URIs: uris, Token: blob}, true
}

func puxNote(name string, uris []string, it puxItem) (Row, bool) {
	return noteRow(name, uris, it.Details.Notes)
}

func puxSSH(name string, uris []string, it puxItem) (Row, bool) {
	key := puxField(it, "private_key", "privateKey", "ssh_private_key")
	if strings.TrimSpace(key) == "" {
		return Row{}, false
	}
	return Row{Name: name, Kind: protocol.ItemSSH, URIs: uris, Token: []byte(key)}, true
}

func puxField(it puxItem, ids ...string) string {
	want := map[string]struct{}{}
	for _, id := range ids {
		want[strings.ToLower(id)] = struct{}{}
	}
	for _, sec := range it.Details.Sections {
		for _, f := range sec.Fields {
			if _, ok := want[strings.ToLower(strings.TrimSpace(f.ID))]; !ok {
				continue
			}
			if v := fieldString(f.Value); v != "" {
				return v
			}
		}
	}
	return ""
}

func fieldString(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var obj map[string]any
	if json.Unmarshal(raw, &obj) == nil {
		if v, ok := obj["string"]; ok {
			if s, ok := v.(string); ok {
				return strings.TrimSpace(s)
			}
		}
		month, _ := obj["month"].(string)
		year, _ := obj["year"].(string)
		if month == "" {
			if n, ok := obj["month"].(float64); ok {
				month = fmt.Sprintf("%02.0f", n)
			}
		}
		if year == "" {
			if n, ok := obj["year"].(float64); ok {
				year = fmt.Sprintf("%.0f", n)
			}
		}
		if month != "" || year != "" {
			return strings.TrimSpace(month + "/" + year)
		}
	}
	return ""
}

func puxAddress(it puxItem) (street, city, state, zip, country string) {
	street = puxField(it, "address1", "street", "address-line1")
	city = puxField(it, "city", "address-level2")
	state = puxField(it, "state", "address-level1")
	zip = puxField(it, "zip", "postal-code")
	country = puxField(it, "country", "country-name")
	if street != "" || city != "" {
		return street, city, state, zip, country
	}
	for _, sec := range it.Details.Sections {
		for _, f := range sec.Fields {
			if !strings.EqualFold(strings.TrimSpace(f.ID), "address") {
				continue
			}
			return addressParts(f.Value)
		}
	}
	return "", "", "", "", ""
}

func addressParts(raw json.RawMessage) (street, city, state, zip, country string) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return "", "", "", "", ""
	}
	var obj map[string]any
	if json.Unmarshal(raw, &obj) == nil {
		src := obj
		if inner, ok := obj["address"].(map[string]any); ok {
			src = inner
		}
		street = anyString(src, "street", "address1", "line1")
		city = anyString(src, "city")
		state = anyString(src, "state", "province", "region")
		zip = anyString(src, "zip", "postalCode", "postal_code")
		country = anyString(src, "country")
		if street != "" || city != "" {
			return street, city, state, zip, country
		}
	}
	s := fieldString(raw)
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	switch len(parts) {
	case 5:
		return parts[0], parts[1], parts[2], parts[3], parts[4]
	case 4:
		return parts[0], parts[1], parts[2], parts[3], ""
	default:
		return s, "", "", "", ""
	}
}

func anyString(obj map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := obj[k]; ok {
			if s, ok := v.(string); ok {
				if s = strings.TrimSpace(s); s != "" {
					return s
				}
			}
		}
	}
	return ""
}

func splitExpiry(v string) (string, string) {
	v = strings.TrimSpace(v)
	v = strings.ReplaceAll(v, "-", "/")
	parts := strings.Split(v, "/")
	if len(parts) >= 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	if len(v) == 6 && isDigits(v) {
		// 1Password CLI MONTH_YEAR is YYYYMM. MMYYYY only when month is 01-12.
		if v[0:2] > "12" {
			return v[4:6], v[0:4]
		}
		return v[0:2], v[2:]
	}
	return v, ""
}

func isDigits(v string) bool {
	for i := 0; i < len(v); i++ {
		if v[i] < '0' || v[i] > '9' {
			return false
		}
	}
	return true
}

func parseCSV(raw []byte) ([]Row, error) {
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	r := csv.NewReader(bytes.NewReader(raw))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	rows, err := r.ReadAll()
	if err != nil || len(rows) == 0 {
		return nil, fmt.Errorf("import: csv")
	}
	idx := headerIndex(rows[0])
	if _, ok := idx["password"]; !ok {
		if _, ok := idx["login_password"]; !ok {
			return nil, fmt.Errorf("import: csv password column")
		}
	}
	var out []Row
	for _, rec := range rows[1:] {
		row, ok := csvRow(idx, rec)
		if ok {
			out = append(out, row)
		}
	}
	return out, nil
}

func headerIndex(header []string) map[string]int {
	out := map[string]int{}
	for i, h := range header {
		out[csvKey(h)] = i
	}
	return out
}

func csvKey(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	h = strings.ReplaceAll(h, " ", "")
	h = strings.ReplaceAll(h, "_", "")
	switch h {
	case "title", "name":
		return "name"
	case "url", "uri", "loginuri", "login_uri":
		return "url"
	case "username", "loginusername", "login":
		return "username"
	case "password", "loginpassword":
		return "password"
	case "otpauth", "totp", "logintotp":
		return "totp"
	case "archived":
		return "archived"
	default:
		return h
	}
}

func csvRow(idx map[string]int, rec []string) (Row, bool) {
	cell := func(key string) string {
		i, ok := idx[key]
		if !ok || i < 0 || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}
	name := cell("name")
	if name == "" {
		return Row{}, false
	}
	switch strings.ToLower(cell("archived")) {
	case "true", "1", "yes":
		return Row{}, false
	}
	// Bitwarden CSV marks rows by type; notes carry their body in notes.
	switch strings.ToLower(cell("type")) {
	case "note", "securenote", "secure note":
		return noteRow(name, nil, cell("notes"))
	}
	pass := cell("password")
	totp := otpSeed(cell("totp"))
	if pass == "" && totp == "" {
		return Row{}, false
	}
	var uris []string
	if u := cell("url"); u != "" {
		uris = []string{u}
	}
	return Row{
		Name:     name,
		Kind:     protocol.ItemAPIKey,
		URIs:     uris,
		Login:    cell("username"),
		Token:    []byte(pass),
		TOTPSeed: []byte(totp),
	}, true
}

// Bitwarden's unencrypted JSON export is one object with items[].type:
// 1 login, 2 secure note, 3 card, 4 identity. deletedDate non-null is
// their trash. Encrypted and password-protected exports are refused —
// we never hold the vault key.
type bwExport struct {
	Encrypted         bool     `json:"encrypted"`
	PasswordProtected bool     `json:"passwordProtected"`
	Items             []bwItem `json:"items"`
}

type bwItem struct {
	Type        int    `json:"type"`
	Name        string `json:"name"`
	Notes       string `json:"notes"`
	DeletedDate string `json:"deletedDate"`
	Login       *struct {
		URIs []struct {
			URI string `json:"uri"`
		} `json:"uris"`
		Username string `json:"username"`
		Password string `json:"password"`
		TOTP     string `json:"totp"`
	} `json:"login"`
	Card *struct {
		Holder string `json:"cardholderName"`
		Number string `json:"number"`
		ExpM   string `json:"expMonth"`
		ExpY   string `json:"expYear"`
		Code   string `json:"code"`
	} `json:"card"`
	Identity *struct {
		First   string `json:"firstName"`
		Last    string `json:"lastName"`
		Addr1   string `json:"address1"`
		Addr2   string `json:"address2"`
		Addr3   string `json:"address3"`
		City    string `json:"city"`
		State   string `json:"state"`
		Postal  string `json:"postalCode"`
		Country string `json:"country"`
		Phone   string `json:"phone"`
		Email   string `json:"email"`
	} `json:"identity"`
}

func parseBitwardenJSON(raw []byte) ([]Row, error) {
	var dump bwExport
	if json.Unmarshal(bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF}), &dump) != nil || dump.Items == nil {
		return nil, fmt.Errorf("import: not a bitwarden json export")
	}
	if dump.Encrypted || dump.PasswordProtected {
		return nil, fmt.Errorf("import: encrypted bitwarden export — export unencrypted")
	}
	var out []Row
	for _, it := range dump.Items {
		row, ok := bwRow(it)
		if ok {
			out = append(out, row)
		}
	}
	return out, nil
}

func bwRow(it bwItem) (Row, bool) {
	if strings.TrimSpace(it.DeletedDate) != "" {
		return Row{}, false
	}
	name := strings.TrimSpace(it.Name)
	if name == "" {
		return Row{}, false
	}
	switch it.Type {
	case 1:
		return bwLogin(name, it)
	case 2:
		return noteRow(name, nil, it.Notes)
	case 3:
		return bwCard(name, it)
	case 4:
		return bwIdentity(name, it)
	default:
		return Row{}, false
	}
}

func bwLogin(name string, it bwItem) (Row, bool) {
	if it.Login == nil {
		return Row{}, false
	}
	seen := map[string]struct{}{}
	var uris []string
	for _, u := range it.Login.URIs {
		u.URI = strings.TrimSpace(u.URI)
		if u.URI == "" {
			continue
		}
		if _, ok := seen[u.URI]; ok {
			continue
		}
		seen[u.URI] = struct{}{}
		uris = append(uris, u.URI)
	}
	pass := it.Login.Password
	totp := otpSeed(it.Login.TOTP)
	if strings.TrimSpace(pass) == "" && totp == "" {
		return Row{}, false
	}
	return Row{
		Name:     name,
		Kind:     protocol.ItemAPIKey,
		URIs:     uris,
		Login:    strings.TrimSpace(it.Login.Username),
		Token:    []byte(pass),
		TOTPSeed: []byte(totp),
	}, true
}

func bwCard(name string, it bwItem) (Row, bool) {
	if it.Card == nil {
		return Row{}, false
	}
	blob, err := material.PackCard(it.Card.Number, it.Card.ExpM, it.Card.ExpY, it.Card.Code, it.Card.Holder)
	if err != nil {
		return Row{}, false
	}
	return Row{Name: name, Kind: protocol.ItemCard, Token: blob}, true
}

func bwIdentity(name string, it bwItem) (Row, bool) {
	if it.Identity == nil {
		return Row{}, false
	}
	var street []string
	for _, l := range []string{it.Identity.Addr1, it.Identity.Addr2, it.Identity.Addr3} {
		if l = strings.TrimSpace(l); l != "" {
			street = append(street, l)
		}
	}
	blob, err := material.PackIdentity(
		it.Identity.First,
		it.Identity.Last,
		strings.Join(street, ", "),
		it.Identity.City,
		it.Identity.State,
		it.Identity.Postal,
		it.Identity.Country,
		it.Identity.Phone,
		it.Identity.Email,
	)
	if err != nil {
		return Row{}, false
	}
	return Row{Name: name, Kind: protocol.ItemIdentity, Token: blob}, true
}

// noteRow is the shared secure-note shape: body lands in File so it
// seals like a document and never lists.
func noteRow(name string, uris []string, notes string) (Row, bool) {
	notes = strings.TrimSpace(notes)
	if notes == "" {
		return Row{}, false
	}
	return Row{
		Name:     name,
		Kind:     protocol.ItemFile,
		URIs:     uris,
		File:     []byte(notes),
		FileName: name + ".txt",
		MIME:     "text/plain",
	}, true
}

func otpSeed(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(v), "otpauth:") {
		u, err := url.Parse(v)
		if err != nil {
			return ""
		}
		return strings.ToUpper(strings.TrimSpace(u.Query().Get("secret")))
	}
	return strings.ToUpper(v)
}
