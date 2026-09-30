package fill

import (
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/grant"
	"github.com/VortexNYC/veil/internal/material"
	"github.com/VortexNYC/veil/internal/passgen"
	"github.com/VortexNYC/veil/internal/protocol"
)

// indexTTL bounds how long a cached item index may serve match/fill before a
// reload. Items created on origin from another surface (CLI, SPA) must appear
// without a bridge restart.
const indexTTL = time.Minute

type jsonMatchEntry struct {
	UUID       string   `json:"uuid"`
	Name       string   `json:"name"`
	Login      string   `json:"login,omitempty"`
	Kind       string   `json:"kind"`
	URIs       []string `json:"uris,omitempty"`
	HasTOTP    bool     `json:"hasTotp,omitempty"`
	HasPasskey bool     `json:"hasPasskey,omitempty"`
	SavedFor   string   `json:"savedFor,omitempty"`
	Affiliated bool     `json:"affiliated,omitempty"`
	// Passkey identity fields — `list` only, for the AutoFill appex building
	// ASPasskeyCredentialIdentity. Metadata, never the private key.
	CredID     string `json:"credId,omitempty"`
	RpID       string `json:"rpId,omitempty"`
	UserHandle string `json:"userHandle,omitempty"`
}

type jsonFillEntry struct {
	Kind       string `json:"kind"`
	Login      string `json:"login,omitempty"`
	Password   string `json:"password,omitempty"`
	TOTP       string `json:"totp,omitempty"`
	UUID       string `json:"uuid"`
	Name       string `json:"name"`
	Number     string `json:"number,omitempty"`
	ExpMonth   string `json:"expMonth,omitempty"`
	ExpYear    string `json:"expYear,omitempty"`
	CVV        string `json:"cvv,omitempty"`
	GivenName  string `json:"givenName,omitempty"`
	FamilyName string `json:"familyName,omitempty"`
	Address    string `json:"address,omitempty"`
	City       string `json:"city,omitempty"`
	Region     string `json:"region,omitempty"`
	Postal     string `json:"postal,omitempty"`
	Country    string `json:"country,omitempty"`
	Phone      string `json:"phone,omitempty"`
	Email      string `json:"email,omitempty"`
}

type jsonRequest struct {
	Action         string          `json:"action"`
	URL            string          `json:"url"`
	App            string          `json:"app"`
	UUID           string          `json:"uuid"`
	Login          string          `json:"login"`
	Password       string          `json:"password"`
	PasswordRules  string          `json:"passwordRules"`
	Create         bool            `json:"create"`
	OTPAuth        string          `json:"otpauth"`
	Origin         string          `json:"origin"`
	PublicKey      json.RawMessage `json:"publicKey"`
	RelatedOrigins []string        `json:"relatedOrigins"`
	// ReqID correlates replies to requests on the browser side — a late
	// reply must never resolve a newer request. Optional: native clients
	// read synchronously and leave it empty.
	ReqID string `json:"reqId"`
}

func (h *Host) handleJSON(raw []byte) []byte {
	var in jsonRequest
	if json.Unmarshal(raw, &in) != nil {
		return jsonFillReply(nil, "")
	}
	return jsonEcho(in.ReqID, h.dispatchJSON(in))
}

func (h *Host) dispatchJSON(in jsonRequest) []byte {
	switch in.Action {
	case "ping":
		if h.Replica != nil {
			_ = h.PullReplica()
		}
		h.ensureIndex()
		out := struct {
			Version string `json:"version"`
			Error   string `json:"error,omitempty"`
		}{Version: JSONVersion}
		if h.loginNeeded() {
			out.Error = "need_login"
		}
		return jsonBytes(out)
	case "login":
		return h.jsonLogin()
	case "match":
		return jsonBytes(struct {
			Entries []jsonMatchEntry `json:"entries"`
		}{Entries: h.jsonMatch(in.URL)})
	case "list":
		return jsonBytes(struct {
			Entries []jsonMatchEntry `json:"entries"`
		}{Entries: h.jsonList()})
	case "fill":
		entries := h.jsonFill(in.URL, in.UUID)
		err := ""
		if len(entries) == 0 && h.loginNeeded() {
			err = "need_login"
		}
		return jsonFillReply(entries, err)
	case "generate":
		return h.jsonGenerate(in.URL, in.Login, in.PasswordRules, in.UUID)
	case "save":
		return h.jsonSave(in.URL, in.Login, in.Password, in.UUID, in.Create)
	case "enrollTotp":
		return h.jsonEnrollTotp(in.URL, in.OTPAuth, in.UUID)
	case "passkeyCreate":
		return h.jsonPasskeyCreate(in.Origin, in.PublicKey, in.RelatedOrigins)
	case "passkeyGet":
		return h.jsonPasskeyGet(in.Origin, in.PublicKey)
	case "relock":
		h.InvalidateConfirm()
		return jsonBytes(struct {
			OK bool `json:"ok"`
		}{OK: true})
	default:
		return jsonFillReply(nil, "")
	}
}

func jsonFillReply(entries []jsonFillEntry, err string) []byte {
	if entries == nil {
		entries = []jsonFillEntry{}
	}
	return jsonBytes(struct {
		Entries []jsonFillEntry `json:"entries"`
		Error   string          `json:"error,omitempty"`
	}{Entries: entries, Error: err})
}

func (h *Host) jsonMatch(rawURL string) []jsonMatchEntry {
	out := []jsonMatchEntry{}
	if strings.TrimSpace(rawURL) == "" {
		return out
	}
	h.ensureIndex()
	h.mu.Lock()
	items := append([]protocol.Item(nil), h.index...)
	h.mu.Unlock()
	pageHost := ""
	if u, err := grant.ParseDest(rawURL); err == nil {
		pageHost = grant.CanonicalHost(u)
	}
	matched := []protocol.Item{}
	for _, item := range items {
		if item.Archived || !item.Kind.Fillable() {
			continue
		}
		unbound := (item.Kind == protocol.ItemCard || item.Kind == protocol.ItemIdentity) && len(item.URIs) == 0
		if !unbound && !grant.HostAllowed(item, rawURL) {
			continue
		}
		matched = append(matched, item)
	}
	// Host-bound items outrank unbound ones — a card or identity with no URI
	// matches everywhere, so the login saved for this exact host leads.
	// Stable: vault order breaks ties.
	exact := func(it protocol.Item) bool {
		for _, raw := range it.URIs {
			if u, err := grant.ParseDest(raw); err == nil && grant.CanonicalHost(u) == pageHost {
				return true
			}
		}
		return false
	}
	sort.SliceStable(matched, func(i, j int) bool {
		return exact(matched[i]) && !exact(matched[j])
	})
	for _, item := range matched {
		out = append(out, matchEntry(item))
	}
	return out
}

func (h *Host) jsonFill(rawURL, uuid string) []jsonFillEntry {
	empty := []jsonFillEntry{}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		fillDebug("fill empty url")
		return empty
	}
	matches := h.jsonMatch(rawURL)
	uuid = strings.TrimSpace(uuid)
	var hit *jsonMatchEntry
	if uuid == "" {
		if len(matches) != 1 || matches[0].Kind != "login" || matches[0].Affiliated {
			return empty
		}
		hit = &matches[0]
	} else {
		for i := range matches {
			if matches[i].UUID == uuid {
				hit = &matches[i]
				break
			}
		}
		if hit == nil {
			fillDebug("fill miss n=" + strconv.Itoa(len(matches)) + " url=" + rawURL)
			return empty
		}
	}
	scope := grant.Registrable(rawURL)
	switch hit.Kind {
	case "login":
		if hit.Affiliated {
			return empty
		}
		if err := h.confirm("Veil wants to fill a saved sign-in", scope, true); err != nil {
			return empty
		}
		got, ok := h.unlockJSONFill(hit.UUID, hit.HasTOTP)
		if !ok {
			return empty
		}
		if got.TOTP == totpPresent || len(got.TOTP) > 8 {
			got.TOTP = ""
		}
		return []jsonFillEntry{{
			Kind:     "login",
			Login:    got.Login,
			Password: got.Password,
			TOTP:     got.TOTP,
			UUID:     got.UUID,
			Name:     got.Name,
		}}
	case "card":
		item, env, ok := h.originEnvelope(hit.UUID)
		if !ok || env.Number == "" {
			item, env, ok = h.fillEnvelope(hit.UUID)
		}
		if !ok || env.Number == "" {
			fillDebug("fill empty card")
			return empty
		}
		if err := h.confirm("Veil wants to fill a card", scope, env.CVV == ""); err != nil {
			return empty
		}
		return []jsonFillEntry{{
			Kind:      "card",
			UUID:      item.ID,
			Name:      item.Name,
			Number:    env.Number,
			ExpMonth:  env.ExpMonth,
			ExpYear:   env.ExpYear,
			CVV:       env.CVV,
			GivenName: env.GivenName,
		}}
	case "identity":
		item, env, ok := h.fillEnvelope(hit.UUID)
		if !ok {
			return empty
		}
		if err := h.confirm("Veil wants to fill an identity", scope, true); err != nil {
			return empty
		}
		return []jsonFillEntry{{
			Kind:       "identity",
			UUID:       item.ID,
			Name:       item.Name,
			GivenName:  env.GivenName,
			FamilyName: env.FamilyName,
			Address:    env.Address,
			City:       env.City,
			Region:     env.Region,
			Postal:     env.Postal,
			Country:    env.Country,
			Phone:      env.Phone,
			Email:      env.Email,
		}}
	default:
		return empty
	}
}

func (h *Host) unlockJSONFill(uuid string, mintTotp bool) (app.FillEntry, bool) {
	if h.replicaWarm() {
		if got, ok := h.replicaFill(uuid, mintTotp); ok {
			return got, true
		}
	}
	if h.Origin != "" {
		payload, err := json.Marshal(struct {
			UUID     string `json:"uuid"`
			MintTOTP bool   `json:"mintTotp,omitempty"`
		}{UUID: uuid, MintTOTP: mintTotp})
		if err != nil {
			return app.FillEntry{}, false
		}
		raw, err := h.originPOST("/v1/fill/logins", payload)
		if err != nil {
			return app.FillEntry{}, false
		}
		var out struct {
			Entries []app.FillEntry `json:"entries"`
		}
		if json.Unmarshal(raw, &out) != nil || len(out.Entries) != 1 {
			return app.FillEntry{}, false
		}
		return out.Entries[0], true
	}
	if h.App == nil {
		return app.FillEntry{}, false
	}
	human := protocol.Principal{Kind: protocol.PrincipalHuman, ID: h.App.HumanID, OrgID: h.App.OrgID}
	got, err := h.App.FillLogin(human, uuid, mintTotp)
	if err != nil {
		return app.FillEntry{}, false
	}
	return got, true
}

func (h *Host) ensureIndex() {
	h.mu.Lock()
	ok := h.indexOK && time.Since(h.indexAt) < indexTTL
	h.mu.Unlock()
	if ok {
		return
	}
	h.reloadIndex()
}

func (h *Host) invalidateIndex() {
	h.mu.Lock()
	h.index = nil
	h.indexOK = false
	h.mu.Unlock()
}

func (h *Host) reloadIndex() {
	var items []protocol.Item
	switch {
	case h.replicaWarm():
		items = h.Replica.Items()
	case h.Origin != "":
		raw, err := h.originGET("/v1/items")
		if err != nil {
			return
		}
		var out struct {
			Items []protocol.Item `json:"items"`
		}
		if json.Unmarshal(raw, &out) != nil {
			return
		}
		items = out.Items
	case h.App != nil:
		human := protocol.Principal{Kind: protocol.PrincipalHuman, ID: h.App.HumanID, OrgID: h.App.OrgID}
		got, err := h.App.ItemsForPrincipal(human)
		if err != nil {
			return
		}
		items = got
	default:
		return
	}
	if items == nil {
		items = []protocol.Item{}
	}
	h.mu.Lock()
	h.index = items
	h.indexOK = true
	h.indexAt = time.Now()
	h.mu.Unlock()
}

func matchEntry(item protocol.Item) jsonMatchEntry {
	kind := "login"
	switch item.Kind {
	case protocol.ItemPasskey:
		kind = "passkey"
	case protocol.ItemCard:
		kind = "card"
	case protocol.ItemIdentity:
		kind = "identity"
	}
	saved := ""
	if len(item.URIs) > 0 {
		if u, err := grant.ParseDest(item.URIs[0]); err == nil {
			saved = grant.CanonicalHost(u)
		}
	}
	return jsonMatchEntry{
		UUID:       item.ID,
		Name:       item.Name,
		Login:      item.Login,
		Kind:       kind,
		HasTOTP:    item.HasTOTP,
		HasPasskey: item.Kind == protocol.ItemPasskey,
		SavedFor:   saved,
	}
}

// jsonList is the AutoFill identity-store sync feed: every fillable item's
// metadata plus its URIs. The AutoFill appex and helper app are trusted fill
// clients on the same socket boundary; URIs stay off `match` replies because
// those reach page-adjacent extension contexts.
func (h *Host) jsonList() []jsonMatchEntry {
	out := []jsonMatchEntry{}
	h.ensureIndex()
	h.mu.Lock()
	items := append([]protocol.Item(nil), h.index...)
	h.mu.Unlock()
	for _, item := range items {
		if item.Archived || !item.Kind.Fillable() {
			continue
		}
		e := matchEntry(item)
		e.URIs = item.URIs
		if item.Kind == protocol.ItemPasskey && h.Replica != nil {
			if raw := h.Replica.Material(item.ID); raw != "" {
				env := material.Unpack([]byte(raw))
				e.CredID, e.RpID, e.UserHandle = env.CredID, env.RpID, env.UserHandle
			}
		}
		out = append(out, e)
	}
	return out
}

func (h *Host) jsonGenerate(rawURL, login, rules, uuid string) []byte {
	rawURL = strings.TrimSpace(rawURL)
	login = strings.TrimSpace(login)
	uuid = strings.TrimSpace(uuid)
	if rawURL == "" {
		return jsonGenerateErr("failed")
	}
	uri, host, ok := generateURI(rawURL)
	if !ok {
		return jsonGenerateErr("failed")
	}
	matches := h.jsonMatch(rawURL)
	if h.loginNeeded() {
		return jsonGenerateErr("need_login")
	}
	if uuid != "" {
		return h.jsonRotate(rawURL, uuid, login, rules, matches)
	}
	for _, e := range matches {
		if e.Kind == "login" {
			return jsonGenerateErr("choose")
		}
	}
	if err := h.confirm("Veil wants to save a new password", grant.Registrable(rawURL), true); err != nil {
		return jsonGenerateErr("canceled")
	}
	secret, err := passgen.FromRules(rules)
	if err != nil {
		return jsonGenerateErr("failed")
	}
	name := host
	item, err := h.createGeneratedLogin(name, uri, login, string(secret))
	if err != nil {
		if h.loginNeeded() {
			return jsonGenerateErr("need_login")
		}
		return jsonGenerateErr("failed")
	}
	h.invalidateIndex()
	h.replicaSyncSoon()
	h.rememberCreated(rawURL, item.ID)
	return jsonBytes(struct {
		UUID     string `json:"uuid"`
		Name     string `json:"name"`
		Login    string `json:"login,omitempty"`
		Password string `json:"password"`
	}{UUID: item.ID, Name: item.Name, Login: login, Password: string(secret)})
}

// jsonRotate is change-password: the chooser sent an existing login's uuid.
// Mint a new secret and PATCH that item — never a second item on the host.
func (h *Host) jsonRotate(rawURL, uuid, login, rules string, matches []jsonMatchEntry) []byte {
	var hit *jsonMatchEntry
	for i := range matches {
		if matches[i].UUID == uuid && matches[i].Kind == "login" && !matches[i].Affiliated {
			hit = &matches[i]
			break
		}
	}
	if hit == nil {
		return jsonGenerateErr("choose")
	}
	if err := h.confirm("Veil wants to update a saved password", grant.Registrable(rawURL), true); err != nil {
		return jsonGenerateErr("canceled")
	}
	secret, err := passgen.FromRules(rules)
	if err != nil {
		return jsonGenerateErr("failed")
	}
	if err := h.rotateLogin(uuid, login, string(secret)); err != nil {
		if h.loginNeeded() {
			return jsonGenerateErr("need_login")
		}
		return jsonGenerateErr("failed")
	}
	h.invalidateIndex()
	h.replicaSyncSoon()
	if login == "" {
		login = hit.Login
	}
	return jsonBytes(struct {
		UUID     string `json:"uuid"`
		Name     string `json:"name"`
		Login    string `json:"login,omitempty"`
		Password string `json:"password"`
	}{UUID: hit.UUID, Name: hit.Name, Login: login, Password: string(secret)})
}

func (h *Host) rotateLogin(uuid, login, secret string) error {
	if h.Origin != "" {
		payload, err := json.Marshal(struct {
			Secret string `json:"secret"`
			Login  string `json:"login,omitempty"`
		}{Secret: secret, Login: login})
		if err != nil {
			return err
		}
		_, err = h.originCall(http.MethodPatch, "/v1/items/"+url.PathEscape(uuid), payload)
		return err
	}
	if h.App == nil {
		return errGenerateCreate
	}
	_, err := h.App.UpdateItem(uuid, nil, nil, nil, login, []byte(secret))
	return err
}

// jsonTypedRotate is change-password for a typed (not generated) secret:
// the save prompt matched the login to an existing item and sent its uuid.
// Same rules as jsonRotate — a real match on this URL, confirm, then PATCH;
// a uuid that does not match here is "choose", never an unrelated write.
func (h *Host) jsonTypedRotate(rawURL, uuid, login, password string, matches []jsonMatchEntry) []byte {
	var hit *jsonMatchEntry
	for i := range matches {
		if matches[i].UUID == uuid && matches[i].Kind == "login" && !matches[i].Affiliated {
			hit = &matches[i]
			break
		}
	}
	if hit == nil {
		return jsonGenerateErr("choose")
	}
	if err := h.confirm("Veil wants to update a saved password", grant.Registrable(rawURL), true); err != nil {
		return jsonGenerateErr("canceled")
	}
	if err := h.rotateLogin(uuid, login, password); err != nil {
		if h.loginNeeded() {
			return jsonGenerateErr("need_login")
		}
		return jsonGenerateErr("failed")
	}
	h.invalidateIndex()
	h.replicaSyncSoon()
	if login == "" {
		login = hit.Login
	}
	return jsonBytes(struct {
		UUID  string `json:"uuid"`
		Name  string `json:"name"`
		Login string `json:"login,omitempty"`
	}{UUID: hit.UUID, Name: hit.Name, Login: login})
}

func loginMatch(entries []jsonMatchEntry) bool {
	for _, e := range entries {
		if e.Kind == "login" {
			return true
		}
	}
	return false
}

func (h *Host) jsonSave(rawURL, login, password, uuid string, create bool) []byte {
	rawURL = strings.TrimSpace(rawURL)
	login = strings.TrimSpace(login)
	password = strings.TrimSpace(password)
	if rawURL == "" || password == "" {
		return jsonGenerateErr("failed")
	}
	uri, host, ok := generateURI(rawURL)
	if !ok {
		return jsonGenerateErr("failed")
	}
	matches := h.jsonMatch(rawURL)
	if h.loginNeeded() {
		return jsonGenerateErr("need_login")
	}
	if uuid != "" {
		return h.jsonTypedRotate(rawURL, uuid, login, password, matches)
	}
	// Existing logins make a bare save ambiguous — rotate or new item? The
	// chooser must say so: uuid rotates, create:true mints a new login.
	// Neither means the caller never chose, so stay fail-closed.
	if loginMatch(matches) && !create {
		return jsonGenerateErr("choose")
	}
	if err := h.confirm("Veil wants to save this sign-in", grant.Registrable(rawURL), false); err != nil {
		return jsonGenerateErr("canceled")
	}
	item, err := h.createGeneratedLogin(host, uri, login, password)
	if err != nil {
		if h.loginNeeded() {
			return jsonGenerateErr("need_login")
		}
		return jsonGenerateErr("failed")
	}
	h.invalidateIndex()
	h.replicaSyncSoon()
	h.rememberCreated(rawURL, item.ID)
	return jsonBytes(struct {
		UUID  string `json:"uuid"`
		Name  string `json:"name"`
		Login string `json:"login,omitempty"`
	}{UUID: item.ID, Name: item.Name, Login: login})
}

func (h *Host) jsonEnrollTotp(rawURL, otpauth, uuid string) []byte {
	seed := parseOTPAuth(otpauth)
	if seed == "" {
		return jsonGenerateErr("failed")
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return jsonGenerateErr("failed")
	}
	// The chooser sends a uuid when the human picked an existing item; a
	// bare call attaches to the login just saved on this URL.
	if uuid == "" {
		uuid = h.createdFor(rawURL)
	}
	if uuid == "" {
		return jsonGenerateErr("choose")
	}
	item, ok := h.enrollTarget(uuid, rawURL)
	if h.loginNeeded() {
		return jsonGenerateErr("need_login")
	}
	if !ok {
		return jsonGenerateErr("choose")
	}
	if err := h.confirm("Veil wants to save this authenticator", grant.Registrable(rawURL), false); err != nil {
		return jsonGenerateErr("canceled")
	}
	if err := h.attachTOTP(item.ID, seed); err != nil {
		if h.loginNeeded() {
			return jsonGenerateErr("need_login")
		}
		return jsonGenerateErr("failed")
	}
	h.forgetCreated()
	h.invalidateIndex()
	h.replicaSyncSoon()
	return jsonBytes(struct {
		UUID    string `json:"uuid"`
		HasTOTP bool   `json:"hasTotp"`
	}{UUID: item.ID, HasTOTP: true})
}

func (h *Host) enrollTarget(uuid, rawURL string) (protocol.Item, bool) {
	h.ensureIndex()
	h.mu.Lock()
	items := append([]protocol.Item(nil), h.index...)
	h.mu.Unlock()
	for _, item := range items {
		if item.ID != uuid || item.Archived || item.HasTOTP {
			continue
		}
		if !item.Kind.Fillable() || item.Kind == protocol.ItemCard || item.Kind == protocol.ItemIdentity || item.Kind == protocol.ItemPasskey {
			continue
		}
		if !grant.HostAllowed(item, rawURL) {
			continue
		}
		return item, true
	}
	return protocol.Item{}, false
}

func (h *Host) attachTOTP(uuid, seed string) error {
	if h.Origin != "" {
		payload, err := json.Marshal(struct {
			UUID     string `json:"uuid"`
			TOTPSeed string `json:"totp_seed"`
		}{UUID: uuid, TOTPSeed: seed})
		if err != nil {
			return err
		}
		raw, err := h.originPOST("/v1/fill/totp/enroll", payload)
		if err != nil {
			return err
		}
		var out struct {
			UUID    string `json:"uuid"`
			HasTOTP bool   `json:"has_totp"`
		}
		if json.Unmarshal(raw, &out) != nil || !out.HasTOTP || out.UUID == "" {
			return errGenerateCreate
		}
		return nil
	}
	if h.App == nil {
		return errGenerateCreate
	}
	human := protocol.Principal{Kind: protocol.PrincipalHuman, ID: h.App.HumanID, OrgID: h.App.OrgID}
	return h.App.AttachTOTP(human, uuid, seed)
}

func (h *Host) rememberCreated(rawURL, uuid string) {
	h.mu.Lock()
	h.lastScope = grant.Registrable(rawURL)
	h.lastUUID = uuid
	h.mu.Unlock()
}

func (h *Host) createdFor(rawURL string) string {
	scope := grant.Registrable(rawURL)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.lastUUID == "" || h.lastScope == "" || h.lastScope != scope {
		return ""
	}
	return h.lastUUID
}

func (h *Host) forgetCreated() {
	h.mu.Lock()
	h.lastScope = ""
	h.lastUUID = ""
	h.mu.Unlock()
}

func parseOTPAuth(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "otpauth") || !strings.EqualFold(u.Host, "totp") {
		return ""
	}
	secret := strings.ToUpper(strings.ReplaceAll(u.Query().Get("secret"), " ", ""))
	secret = strings.ReplaceAll(secret, "-", "")
	if secret == "" {
		return ""
	}
	if _, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret); err != nil {
		if _, err := base32.StdEncoding.DecodeString(secret); err != nil {
			return ""
		}
	}
	return secret
}

func (h *Host) createGeneratedLogin(name, uri, login, secret string) (protocol.Item, error) {
	if h.Origin != "" {
		payload, err := json.Marshal(struct {
			Name   string `json:"name"`
			URI    string `json:"uri"`
			Secret string `json:"secret"`
			Login  string `json:"login,omitempty"`
		}{Name: name, URI: uri, Secret: secret, Login: login})
		if err != nil {
			return protocol.Item{}, err
		}
		raw, err := h.originPOST("/v1/items", payload)
		if err != nil {
			return protocol.Item{}, err
		}
		var item protocol.Item
		if json.Unmarshal(raw, &item) != nil || item.ID == "" {
			return protocol.Item{}, errGenerateCreate
		}
		return item, nil
	}
	if h.App == nil {
		return protocol.Item{}, errGenerateCreate
	}
	return h.App.PutItem(app.ItemOpts{Name: name, URI: uri, Token: []byte(secret), Login: login})
}

func generateURI(rawURL string) (uri, host string, ok bool) {
	u, err := grant.ParseDest(rawURL)
	if err != nil {
		return "", "", false
	}
	host = grant.CanonicalHost(u)
	if host == "" {
		return "", "", false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		scheme = "https"
	}
	return scheme + "://" + host, host, true
}

func jsonGenerateErr(err string) []byte {
	return jsonBytes(struct {
		Error string `json:"error"`
	}{Error: err})
}

var errGenerateCreate = errors.New("generate create failed")

func (h *Host) jsonPasskeyCreate(origin string, publicKey json.RawMessage, extra []string) []byte {
	origin = strings.TrimSpace(origin)
	if origin == "" || len(publicKey) == 0 {
		return jsonPasskeyErr("failed")
	}
	if err := h.confirm("Veil wants to save a passkey", grant.Registrable(origin), true); err != nil {
		return jsonPasskeyErr("canceled")
	}
	out := jsonPasskeyFromHost(h.passkeysRegister(origin, publicKey, extra))
	h.replicaSyncSoon()
	return out
}

func (h *Host) jsonPasskeyGet(origin string, publicKey json.RawMessage) []byte {
	origin = strings.TrimSpace(origin)
	if origin == "" || len(publicKey) == 0 {
		return jsonPasskeyErr("failed")
	}
	if err := h.confirm("Veil wants to use a passkey", grant.Registrable(origin), true); err != nil {
		return jsonPasskeyErr("canceled")
	}
	return jsonPasskeyFromHost(h.passkeysGet(origin, publicKey))
}

func jsonPasskeyFromHost(raw []byte) []byte {
	var r passkeyReply
	if json.Unmarshal(raw, &r) != nil || r.Success != "true" || len(r.Response) == 0 {
		fmt.Fprintf(os.Stderr, "veil-fill: passkey reply malformed\n")
		return jsonPasskeyErr("failed")
	}
	var inner struct {
		ErrorCode int `json:"errorCode"`
	}
	if json.Unmarshal(r.Response, &inner) == nil && inner.ErrorCode != 0 {
		fmt.Fprintf(os.Stderr, "veil-fill: passkey reply errorCode=%d\n", inner.ErrorCode)
		return jsonPasskeyErr("failed")
	}
	return jsonBytes(struct {
		Response json.RawMessage `json:"response"`
	}{Response: r.Response})
}

func jsonPasskeyErr(err string) []byte {
	return jsonBytes(struct {
		Error string `json:"error"`
	}{Error: err})
}

// jsonEcho stamps the request's reqId onto the reply so the extension can
// drop stale responses instead of letting them resolve the wrong request.
func jsonEcho(reqID string, raw []byte) []byte {
	if reqID == "" {
		return raw
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return raw
	}
	id, err := json.Marshal(reqID)
	if err != nil {
		return raw
	}
	m["reqId"] = id
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}

func jsonBytes(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"entries":[]}`)
	}
	return raw
}
