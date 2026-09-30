package app

import (
	"context"
	"fmt"

	"github.com/VortexNYC/veil/internal/health"
	"github.com/VortexNYC/veil/internal/material"
	"github.com/VortexNYC/veil/internal/protocol"
)

// SecurityReport is the vault health report — weak, reused, and optionally
// breached passwords across what the principal can see. Human only, like
// fill: the report reads sealed material in-process and emits metadata only.
// check is nil for the default offline report; pass health.HIBP{}.Check to
// opt into k-anonymity breach lookup.
func (a *App) SecurityReport(ctx context.Context, p protocol.Principal, check health.Checker) (health.Report, error) {
	if p.Kind != protocol.PrincipalHuman {
		return health.Report{}, fmt.Errorf("app: report is human")
	}
	items, err := a.ItemsForPrincipal(p)
	if err != nil {
		return health.Report{}, err
	}
	entries := make([]health.Entry, 0, len(items))
	for _, item := range items {
		if item.Archived {
			continue
		}
		sec, err := a.Store.Secret(item.ID)
		if err != nil {
			return health.Report{}, err
		}
		env := material.Unpack([]byte(sec))
		tok := env.Token
		if tok == "" {
			tok = string(sec)
		}
		uri := ""
		if len(item.URIs) > 0 {
			uri = item.URIs[0]
		}
		entries = append(entries, health.Entry{
			ID:    item.ID,
			Name:  item.Name,
			Kind:  string(item.Kind),
			URI:   uri,
			Login: item.Login,
			Token: tok,
		})
	}
	return health.Analyze(ctx, entries, check), nil
}
