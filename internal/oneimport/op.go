package oneimport

import (
	"bytes"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/VortexNYC/veil/internal/material"
	"github.com/VortexNYC/veil/internal/protocol"
)

// OPDump is the in-memory envelope produced by `veil item import --from-op`
// when streaming items from the local `op` CLI into oneimport.Parse.
type OPDump struct {
	Version string   `json:"veil_op_export"`
	Items   []OPItem `json:"items"`
}

type OPItem struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Category string    `json:"category"`
	URLs     []OPURL   `json:"urls,omitempty"`
	Fields   []OPField `json:"fields,omitempty"`
}

type OPURL struct {
	Href    string `json:"href"`
	Primary bool   `json:"primary,omitempty"`
}

type OPField struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Purpose string `json:"purpose,omitempty"`
	Label   string `json:"label,omitempty"`
	Value   string `json:"value,omitempty"`
}

func isOPDump(raw []byte) bool {
	var peek struct {
		Version string `json:"veil_op_export"`
	}
	return json.Unmarshal(raw, &peek) == nil && peek.Version != ""
}

func parseOPJSON(raw []byte) ([]Row, error) {
	var dump OPDump
	if err := json.Unmarshal(raw, &dump); err != nil || dump.Version == "" {
		return nil, fmt.Errorf("import: invalid op export json")
	}
	var out []Row
	for _, it := range dump.Items {
		if r, ok := opItemToRow(it); ok {
			out = append(out, r)
		}
	}
	return out, nil
}

func opItemToRow(it OPItem) (Row, bool) {
	name := strings.TrimSpace(it.Title)
	if name == "" {
		return Row{}, false
	}
	var uris []string
	for _, u := range it.URLs {
		if h := strings.TrimSpace(u.Href); h != "" {
			uris = append(uris, h)
		}
	}
	cat := strings.ToUpper(strings.TrimSpace(it.Category))
	if cat == "CREDIT_CARD" {
		var num, cvv, expMonth, expYear, holder string
		for _, f := range it.Fields {
			val := strings.TrimSpace(f.Value)
			if val == "" {
				continue
			}
			switch strings.ToLower(f.ID) {
			case "ccnum", "number":
				num = val
			case "cvv", "verification_number":
				cvv = val
			case "cardholder", "holder":
				holder = val
			case "expiry":
				if len(val) >= 6 {
					expYear = val[:4]
					expMonth = strings.TrimLeft(val[len(val)-2:], "0")
				}
			}
		}
		if num == "" {
			return Row{}, false
		}
		blob, err := material.PackCard(num, expMonth, expYear, cvv, holder)
		if err != nil {
			return Row{}, false
		}
		return Row{
			Name:  name,
			Kind:  protocol.ItemCard,
			URIs:  uris,
			Token: blob,
		}, true
	}

	var login, secret, totp string
	for _, f := range it.Fields {
		val := strings.TrimSpace(f.Value)
		if val == "" {
			continue
		}
		idLower := strings.ToLower(f.ID)
		labelLower := strings.ToLower(f.Label)
		purpose := strings.ToUpper(f.Purpose)
		ftype := strings.ToUpper(f.Type)

		switch {
		case ftype == "OTP" || strings.HasPrefix(idLower, "totp"):
			if s := validBase32Seed(otpSeed(val)); s != "" {
				totp = s
			}
		case purpose == "USERNAME" || idLower == "username":
			if login == "" {
				login = val
			}
		case purpose == "PASSWORD" || idLower == "password" || idLower == "credential":
			if secret == "" {
				secret = val
			}
		case (idLower == "hostname" || idLower == "url" || labelLower == "api url" || labelLower == "website") && strings.Contains(val, "://"):
			uris = append(uris, val)
		case ftype == "CONCEALED" && secret == "":
			secret = val
		}
	}
	if secret == "" && totp == "" {
		return Row{}, false
	}
	return Row{
		Name:     name,
		Kind:     protocol.ItemAPIKey,
		URIs:     uris,
		Login:    login,
		Token:    []byte(secret),
		TOTPSeed: []byte(totp),
	}, true
}

func isOTPAuthFile(sniff []byte) bool {
	for _, line := range bytes.Split(sniff, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		return bytes.HasPrefix(bytes.ToLower(line), []byte("otpauth://"))
	}
	return false
}

// parseOTPAuthLines imports newline-delimited otpauth://totp/... URIs (from
// Google Authenticator / Raivo / Aegis / 2FAS exports). Every seed is verified
// as valid base32 before acceptance.
func parseOTPAuthLines(raw []byte) ([]Row, error) {
	var out []Row
	for _, rawLine := range strings.Split(string(raw), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		u, err := url.Parse(line)
		if err != nil || !strings.EqualFold(u.Scheme, "otpauth") || !strings.EqualFold(u.Host, "totp") {
			continue
		}
		seed := validBase32Seed(otpSeed(line))
		if seed == "" {
			continue
		}
		label := strings.Trim(u.Path, "/")
		issuer := strings.TrimSpace(u.Query().Get("issuer"))
		var name, login string
		if parts := strings.SplitN(label, ":", 2); len(parts) == 2 {
			if issuer == "" {
				issuer = strings.TrimSpace(parts[0])
			}
			login = strings.TrimSpace(parts[1])
		} else if label != "" {
			login = label
		}
		name = issuer
		if name == "" {
			name = login
		}
		if name == "" {
			name = "TOTP"
		}
		out = append(out, Row{
			Name:     name,
			Kind:     protocol.ItemAPIKey,
			Login:    login,
			TOTPSeed: []byte(seed),
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("import: no valid otpauth:// URIs found")
	}
	return out, nil
}

func validBase32Seed(s string) string {
	s = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
	s = strings.ReplaceAll(s, "-", "")
	if s == "" {
		return ""
	}
	if _, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s); err != nil {
		if _, err := base32.StdEncoding.DecodeString(s); err != nil {
			return ""
		}
	}
	return s
}
