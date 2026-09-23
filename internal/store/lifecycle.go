package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// OrgLifecycle is org teardown and membership surgery — Postgres-only ops
// surface, asserted via interface at the call site so the sqlite/memory
// stores stay untouched.
type OrgLifecycle interface {
	// DeleteHuman removes a humans row — after this their token resolves to
	// "not provisioned" on every call. Caller removes the Keto tuple.
	DeleteHuman(ctx context.Context, id string) error
	// PurgeOrg deletes every vault row owned by orgID inside one
	// transaction — secrets, grants, sessions, keys, humans. Audit rows are
	// kept deliberately: teardown must not erase the forensic record; the
	// 90-day partition retention still applies.
	PurgeOrg(ctx context.Context, orgID string) (PurgeReport, error)
}

type PurgeReport struct {
	Items    int64 `json:"items"`
	Grants   int64 `json:"grants"`
	Sessions int64 `json:"sessions"`
	Agents   int64 `json:"agents"`
	Humans   int64 `json:"humans"`
	Keys     int64 `json:"keys"`
}

func (p *Postgres) DeleteHuman(ctx context.Context, id string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM humans WHERE id = $1`, id)
	return err
}

func (p *Postgres) PurgeOrg(ctx context.Context, orgID string) (PurgeReport, error) {
	var rep PurgeReport
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		n := func(q string, dst *int64) error {
			tag, err := tx.Exec(ctx, q, orgID)
			if err != nil {
				return err
			}
			*dst = tag.RowsAffected()
			return nil
		}
		steps := []struct {
			q   string
			dst *int64
		}{
			{`DELETE FROM item_versions WHERE item_id IN (SELECT id FROM items WHERE org_id = $1)`, new(int64)},
			{`DELETE FROM approvals WHERE grant_id IN (SELECT id FROM grants WHERE org_id = $1)`, new(int64)},
			{`DELETE FROM sessions WHERE org_id = $1`, &rep.Sessions},
			{`DELETE FROM grants WHERE org_id = $1`, &rep.Grants},
			{`DELETE FROM workloads WHERE agent_id IN (SELECT id FROM agents WHERE org_id = $1)`, new(int64)},
			{`DELETE FROM recovery_wraps WHERE org_id = $1`, new(int64)},
			{`DELETE FROM owner_keys WHERE org_id = $1`, new(int64)},
			{`DELETE FROM items WHERE org_id = $1`, &rep.Items},
			{`DELETE FROM agents WHERE org_id = $1`, &rep.Agents},
			{`DELETE FROM humans WHERE org_id = $1`, &rep.Humans},
			{`DELETE FROM org_keys WHERE org_id = $1`, &rep.Keys},
		}
		for _, s := range steps {
			if err := n(s.q, s.dst); err != nil {
				return fmt.Errorf("purge org: %w", err)
			}
		}
		return nil
	})
	return rep, err
}
