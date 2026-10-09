// Package grant evaluates whether a principal may Use an item.
//
// Shape follows Infisical's agent-vs-role split and OneCLI's per-agent rules.
// Ours: the grant is the object (level, actions, expiry), not a vault role.
package grant

import (
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"

	"github.com/VortexNYC/veil/internal/protocol"
)

var errEmptyDest = errors.New("grant: empty destination")

type Input struct {
	Principal protocol.Principal
	Item      protocol.Item
	Grant     *protocol.Grant
	// Groups is the principal's resolved group set — a group-subject grant
	// matches only when its id is in it. The store resolves membership at
	// read time; a nil set means group grants never match.
	Groups    map[string]struct{}
	Action    protocol.ActionKind
	TargetURL string
	Approval  *protocol.Approval
	Now       time.Time
}

func Evaluate(in Input) protocol.UseResult {
	if in.Principal.Kind != protocol.PrincipalAgent {
		return deny("human_cannot_use")
	}
	if in.Principal.RevokedAt != nil {
		return deny("agent_revoked")
	}
	if in.Item.Archived {
		return deny("item_archived")
	}
	if in.Grant == nil {
		return deny("no_grant")
	}
	g := in.Grant
	if g.OrgID != in.Principal.OrgID || g.OrgID != in.Item.OrgID {
		return deny("wrong_org")
	}
	if g.Subject() == protocol.SubjectGroup {
		if _, ok := in.Groups[g.AgentID]; !ok {
			return deny("wrong_group")
		}
	} else if g.AgentID != in.Principal.ID {
		return deny("wrong_agent")
	}
	if g.ItemID != in.Item.ID {
		return deny("wrong_item")
	}
	if g.ExpiresAt != nil && !in.Now.Before(*g.ExpiresAt) {
		return deny("grant_expired")
	}
	if g.Level == protocol.LevelDeny {
		return deny("grant_denied")
	}
	if !actionAllowed(g.Actions, in.Action) {
		return deny("action_not_allowed")
	}
	if in.Action == protocol.ActionFetch {
		if err := hostAllowed(in.Item, in.TargetURL); err != "" {
			return deny(err)
		}
	}
	switch g.Level {
	case protocol.Level2:
		return protocol.UseResult{Decision: protocol.DecisionAllow}
	case protocol.Level1:
		if in.Approval == nil || in.Approval.GrantID != g.ID {
			return protocol.UseResult{Decision: protocol.DecisionNeedApproval, Reason: "need_approval"}
		}
		if !in.Now.Before(in.Approval.ExpiresAt) {
			return protocol.UseResult{Decision: protocol.DecisionNeedApproval, Reason: "approval_expired"}
		}
		return protocol.UseResult{
			Decision:   protocol.DecisionAllow,
			ApprovalID: in.Approval.ID,
		}
	default:
		return deny("unknown_level")
	}
}

// Select picks the one grant that governs (principal, item) among
// candidates already filtered to the item and to matching subjects. The
// order is the documented precedence: an expired edge is dead weight and
// ranks last; then direct shadows group; then deny beats allow; and among
// allows a grant that resolves immediately (level2, or level1 with a live
// approval) beats a bare level1 — "any allow wins" made deterministic.
func Select(candidates []protocol.Grant, now time.Time, approved map[string]bool) *protocol.Grant {
	if len(candidates) == 0 {
		return nil
	}
	rank := func(g protocol.Grant) (int, int, int) {
		expired := g.ExpiresAt != nil && !now.Before(*g.ExpiresAt)
		exp := 0
		if expired {
			exp = 1
		}
		tier := 0
		if g.Subject() == protocol.SubjectGroup {
			tier = 1
		}
		res := 2
		if g.Level == protocol.LevelDeny {
			res = 0
		} else if g.Level == protocol.Level2 || approved[g.ID] {
			res = 1
		}
		return exp, tier, res
	}
	best := &candidates[0]
	be, bt, br := rank(*best)
	for i := 1; i < len(candidates); i++ {
		e, t, r := rank(candidates[i])
		if e < be || (e == be && (t < bt || (t == bt && (r < br || (r == br && candidates[i].ID < best.ID))))) {
			best, be, bt, br = &candidates[i], e, t, r
		}
	}
	return best
}

func actionAllowed(actions []protocol.ActionKind, want protocol.ActionKind) bool {
	if slices.Contains(actions, want) {
		return true
	}
	// Env into a child is Use of a granted item. Fetch on the grant is that Use.
	return want == protocol.ActionEnv && slices.Contains(actions, protocol.ActionFetch)
}

func deny(reason string) protocol.UseResult {
	return protocol.UseResult{Decision: protocol.DecisionDeny, Reason: reason}
}

func hostAllowed(item protocol.Item, rawURL string) string {
	if rawURL == "" {
		return "missing_url"
	}
	u, err := ParseDest(rawURL)
	if err != nil {
		return "invalid_url"
	}
	want := CanonicalHost(u)
	if want == "" {
		return "invalid_url"
	}
	for _, raw := range item.URIs {
		iu, err := ParseDest(raw)
		if err != nil {
			continue
		}
		if CanonicalHost(iu) == want {
			return ""
		}
		// Login-style URIs match on the registrable domain — a credential
		// saved for capitalone.com fills www.capitalone.com, the 1Password
		// default. Scoped to http(s) so app:// and other bindings stay exact.
		if (iu.Scheme == "https" || iu.Scheme == "http") &&
			(u.Scheme == "https" || u.Scheme == "http") &&
			Registrable(raw) == Registrable(rawURL) {
			return ""
		}
	}
	return "host_not_allowed"
}

// ParseDest accepts an absolute URL or a CONNECT host:port.
func ParseDest(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, errEmptyDest
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Host == "" {
		return nil, errEmptyDest
	}
	return u, nil
}

// CanonicalHost is hostname, plus port when it is not the scheme default.
func CanonicalHost(u *url.URL) string {
	if u == nil {
		return ""
	}
	h := strings.ToLower(u.Hostname())
	if h == "" {
		return ""
	}
	port := u.Port()
	scheme := strings.ToLower(u.Scheme)
	if port == "" || (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		return h
	}
	return h + ":" + port
}

func HostAllowed(item protocol.Item, rawURL string) bool {
	return hostAllowed(item, rawURL) == ""
}

// Registrable is eTLD+1 for confirm scope. GitHub fill must not waive Amazon CVV.
func Registrable(rawURL string) string {
	u, err := ParseDest(rawURL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return ""
	}
	etld, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil || etld == "" {
		return host
	}
	return strings.ToLower(etld)
}
