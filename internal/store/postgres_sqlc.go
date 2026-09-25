package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func useAuthSessionToUseAuthRow(row sqlc.UseAuthSessionRow) sqlc.UseAuthRow {
	return sqlc.UseAuthRow{
		AgentID:           row.AgentID,
		AgentOrgID:        row.AgentOrgID,
		AgentOwnerKind:    row.AgentOwnerKind,
		AgentOwnerID:      row.AgentOwnerID,
		AgentRevokedAt:    row.AgentRevokedAt,
		ItemID:            row.ItemID,
		ItemOrgID:         row.ItemOrgID,
		ItemName:          row.ItemName,
		ItemKind:          row.ItemKind,
		ItemOwnerKind:     row.ItemOwnerKind,
		ItemOwnerID:       row.ItemOwnerID,
		ItemUris:          row.ItemUris,
		ItemHasTotp:       row.ItemHasTotp,
		ItemTags:          row.ItemTags,
		ItemArchived:      row.ItemArchived,
		ItemHasFile:       row.ItemHasFile,
		ItemLogin:         row.ItemLogin,
		GrantID:           row.GrantID,
		GrantOrgID:        row.GrantOrgID,
		GrantAgentID:      row.GrantAgentID,
		GrantItemID:       row.GrantItemID,
		GrantLevel:        row.GrantLevel,
		GrantActions:      row.GrantActions,
		GrantExpiresAt:    row.GrantExpiresAt,
		ApprovalID:        row.ApprovalID,
		ApprovalGrantID:   row.ApprovalGrantID,
		ApprovalHumanID:   row.ApprovalHumanID,
		ApprovalExpiresAt: row.ApprovalExpiresAt,
	}
}

func useAuthFromSqlcRow(r *sqlc.UseAuthRow) (UseAuth, error) {
	var out UseAuth
	if r.AgentID.Valid && r.AgentID.String != "" {
		out.Agent = protocol.Principal{Kind: protocol.PrincipalAgent, ID: r.AgentID.String, OrgID: r.AgentOrgID.String}
		out.Agent.Owner.Kind = protocol.OwnerKind(r.AgentOwnerKind.String)
		out.Agent.Owner.ID = r.AgentOwnerID.String
		if r.AgentRevokedAt.Valid {
			t := r.AgentRevokedAt.Time.UTC()
			out.Agent.RevokedAt = &t
		}
	}
	if r.ItemID.Valid && r.ItemID.String != "" {
		out.Item = protocol.Item{ID: r.ItemID.String, OrgID: r.ItemOrgID.String, Name: r.ItemName.String, Kind: protocol.ItemKind(r.ItemKind.String)}
		out.Item.Owner.Kind = protocol.OwnerKind(r.ItemOwnerKind.String)
		out.Item.Owner.ID = r.ItemOwnerID.String
		if r.ItemUris.Valid && r.ItemUris.String != "" {
			_ = json.Unmarshal([]byte(r.ItemUris.String), &out.Item.URIs)
		}
		if r.ItemTags.Valid && r.ItemTags.String != "" {
			_ = json.Unmarshal([]byte(r.ItemTags.String), &out.Item.Tags)
		}
		out.Item.HasTOTP = r.ItemHasTotp.Bool
		out.Item.Archived = r.ItemArchived.Bool
		out.Item.HasFile = r.ItemHasFile.Bool
		out.Item.Login = r.ItemLogin.String
	}
	if r.GrantID.Valid && r.GrantID.String != "" {
		g := &protocol.Grant{ID: r.GrantID.String, OrgID: r.GrantOrgID.String, AgentID: r.GrantAgentID.String, ItemID: r.GrantItemID.String, Level: protocol.GrantLevel(r.GrantLevel.String)}
		if r.GrantActions.Valid && r.GrantActions.String != "" {
			_ = json.Unmarshal([]byte(r.GrantActions.String), &g.Actions)
		}
		if r.GrantExpiresAt.Valid {
			t := r.GrantExpiresAt.Time.UTC()
			g.ExpiresAt = &t
		}
		out.Grant = g
	}
	if r.ApprovalID.Valid && r.ApprovalID.String != "" {
		if r.ApprovalExpiresAt.Valid {
			out.Approval = &protocol.Approval{ID: r.ApprovalID.String, GrantID: r.ApprovalGrantID.String, HumanID: r.ApprovalHumanID.String, ExpiresAt: r.ApprovalExpiresAt.Time.UTC()}
		}
	}
	return out, nil
}

func (p *Postgres) UseAuth(agentID, itemID string, now time.Time) (UseAuth, error) {
	ctx := context.Background()
	row, err := p.sqlc.UseAuth(ctx, sqlc.UseAuthParams{
		AgentID: agentID,
		ItemID:  itemID,
		Now:     now.UTC(),
	})
	if err != nil {
		return UseAuth{}, err
	}
	return useAuthFromSqlcRow(&row)
}

// retryOnDeadConn runs fn once more when the error is a connection-level
// failure that provably never reached the server (pgconn.SafeToRetry) — e.g.
// a pooled conn silently blackholed by a host reschedule. Exactly one retry.
func retryOnDeadConn[T any](fn func() (T, error)) (T, error) {
	v, err := fn()
	if err != nil && pgconn.SafeToRetry(err) {
		v, err = fn()
	}
	return v, err
}

func (p *Postgres) UseAuthSession(sessionHash []byte, itemID string, now time.Time) (UseAuth, error) {
	ctx := context.Background()
	row, err := retryOnDeadConn(func() (sqlc.UseAuthSessionRow, error) {
		return p.sqlc.UseAuthSession(ctx, sqlc.UseAuthSessionParams{
			SessionHash: sessionHash,
			ItemID:      itemID,
			Now:         now.UTC(),
		})
	})
	if err != nil {
		return UseAuth{}, err
	}
	if !row.SessionID.Valid || row.SessionID.String == "" || !row.AgentID.Valid || row.AgentID.String == "" {
		return UseAuth{}, ErrNotFound
	}
	r := useAuthSessionToUseAuthRow(row)
	return useAuthFromSqlcRow(&r)
}

func (p *Postgres) ConsumeSession(sessionHash []byte, now time.Time) (protocol.Principal, error) {
	return p.consumeSession(sessionHash, now, nil)
}

func (p *Postgres) ConsumeSessionAudited(sessionHash []byte, now time.Time, e protocol.AuditEvent) (protocol.Principal, error) {
	return p.consumeSession(sessionHash, now, &e)
}

// consumeSession runs the atomic consume UPDATE — and, when e is set, the
// audit INSERT — in one transaction. A failed audit insert rolls the consume
// back, so an allow can never leave the origin unaudited.
func (p *Postgres) consumeSession(sessionHash []byte, now time.Time, e *protocol.AuditEvent) (protocol.Principal, error) {
	ctx := context.Background()
	row, err := retryOnDeadConn(func() (sqlc.ConsumeSessionRow, error) {
		tx, err := p.pool.Begin(ctx)
		if err != nil {
			return sqlc.ConsumeSessionRow{}, err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		qtx := p.sqlc.WithTx(tx)
		row, err := qtx.ConsumeSession(ctx, sqlc.ConsumeSessionParams{
			SessionHash: sessionHash,
			Now:         now.UTC(),
		})
		if err != nil {
			return row, err
		}
		if e != nil {
			e.OrgID = row.AgentOrgID
			e.AgentID = row.AgentID
			// The audit insert runs under a savepoint: a failed statement
			// aborts the whole tx in Postgres, so the outbox fallback needs
			// the savepoint rolled back first — then the event still commits
			// with the consume, durably queued for the relay.
			sp, err := tx.Begin(ctx)
			if err != nil {
				return sqlc.ConsumeSessionRow{}, err
			}
			err = p.sqlc.WithTx(sp).InsertAudit(ctx, sqlc.InsertAuditParams{
				At: e.Time.UTC(), OrgID: e.OrgID, AgentID: e.AgentID, ItemID: e.ItemID,
				Action: string(e.Action), Decision: string(e.Decision), Reason: e.Reason, ApprovalID: e.ApprovalID,
			})
			if err == nil {
				if err := sp.Commit(ctx); err != nil {
					return sqlc.ConsumeSessionRow{}, err
				}
			} else {
				_ = sp.Rollback(ctx)
				if oerr := qtx.InsertAuditOutbox(ctx, sqlc.InsertAuditOutboxParams{
					At: e.Time.UTC(), OrgID: e.OrgID, AgentID: e.AgentID, ItemID: e.ItemID,
					Action: string(e.Action), Decision: string(e.Decision), Reason: e.Reason, ApprovalID: e.ApprovalID,
				}); oerr != nil {
					return sqlc.ConsumeSessionRow{}, fmt.Errorf("%w (outbox: %v)", err, oerr)
				}
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return sqlc.ConsumeSessionRow{}, err
		}
		return row, nil
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return protocol.Principal{}, ErrNotFound
		}
		return protocol.Principal{}, err
	}
	a := protocol.Principal{
		Kind:  protocol.PrincipalAgent,
		ID:    row.AgentID,
		OrgID: row.AgentOrgID,
		Owner: protocol.Owner{
			Kind: protocol.OwnerKind(row.AgentOwnerKind),
			ID:   row.AgentOwnerID,
		},
	}
	if row.AgentRevokedAt.Valid {
		t := row.AgentRevokedAt.Time.UTC()
		a.RevokedAt = &t
	}
	return a, nil
}

func sessionFromSqlc(s *sqlc.Session) protocol.Session {
	sess := protocol.Session{
		ID:        s.ID,
		OrgID:     s.OrgID,
		AgentID:   s.AgentID,
		ExpiresAt: s.ExpiresAt.UTC(),
		CreatedAt: s.CreatedAt.UTC(),
		TTL:       s.Ttl,
		MaxTTL:    s.MaxTtl,
		MaxUses:   int(s.MaxUses),
		Uses:      int(s.Uses),
	}
	if s.RevokedAt.Valid {
		t := s.RevokedAt.Time.UTC()
		sess.RevokedAt = &t
	}
	if s.RenewedAt.Valid {
		t := s.RenewedAt.Time.UTC()
		sess.RenewedAt = &t
	}
	return sess
}

func nullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t.UTC(), Valid: true}
}

func (p *Postgres) PutSession(sess protocol.Session, secretHash []byte) error {
	ctx := context.Background()
	return p.sqlc.PutSession(ctx, sqlc.PutSessionParams{
		ID:         sess.ID,
		OrgID:      sess.OrgID,
		AgentID:    sess.AgentID,
		SecretHash: secretHash,
		ExpiresAt:  sess.ExpiresAt.UTC(),
		CreatedAt:  sess.CreatedAt.UTC(),
		RevokedAt:  nullTime(sess.RevokedAt),
		RenewedAt:  nullTime(sess.RenewedAt),
		Ttl:        sess.TTL,
		MaxTtl:     sess.MaxTTL,
		MaxUses:    int32(sess.MaxUses),
		Uses:       int32(sess.Uses),
	})
}

func (p *Postgres) SessionByHash(secretHash []byte) (protocol.Session, error) {
	ctx := context.Background()
	s, err := p.sqlc.SessionByHash(ctx, secretHash)
	if err == pgx.ErrNoRows {
		return protocol.Session{}, ErrNotFound
	}
	if err != nil {
		return protocol.Session{}, err
	}
	return sessionFromSqlc(&s), nil
}

func (p *Postgres) SessionByID(id string) (protocol.Session, error) {
	ctx := context.Background()
	s, err := p.sqlc.SessionByID(ctx, id)
	if err == pgx.ErrNoRows {
		return protocol.Session{}, ErrNotFound
	}
	if err != nil {
		return protocol.Session{}, err
	}
	return sessionFromSqlc(&s), nil
}

func (p *Postgres) ListSessions() ([]protocol.Session, error) {
	ctx := context.Background()
	rows, err := p.sqlc.ListSessions(ctx, maxListResults)
	if err != nil {
		return nil, err
	}
	out := make([]protocol.Session, 0, len(rows))
	for i := range rows {
		out = append(out, sessionFromSqlc(&rows[i]))
	}
	return out, nil
}

func (p *Postgres) RevokeSession(id string, at time.Time) error {
	ctx := context.Background()
	n, err := p.sqlc.RevokeSession(ctx, sqlc.RevokeSessionParams{At: at.UTC(), ID: id})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) RenewSession(id string, at time.Time) (protocol.Session, error) {
	ctx := context.Background()
	sess, err := p.SessionByID(id)
	if err != nil {
		return protocol.Session{}, err
	}
	if sess.RevokedAt != nil {
		return protocol.Session{}, ErrSessionRevoked
	}
	if !sess.ExpiresAt.After(at) {
		return protocol.Session{}, ErrSessionExpired
	}
	maxExpires := sess.CreatedAt.Add(time.Duration(sess.MaxTTL) * time.Second)
	newExpires := sess.ExpiresAt.Add(time.Duration(sess.TTL) * time.Second)
	if newExpires.After(maxExpires) {
		newExpires = maxExpires
	}
	if !newExpires.After(sess.ExpiresAt) {
		newExpires = sess.ExpiresAt
	}
	rn := at.UTC()
	sess.ExpiresAt = newExpires.UTC()
	sess.RenewedAt = &rn
	err = p.sqlc.RenewSession(ctx, sqlc.RenewSessionParams{
		ExpiresAt: sess.ExpiresAt.UTC(),
		RenewedAt: *sess.RenewedAt,
		ID:        id,
	})
	if err != nil {
		return protocol.Session{}, err
	}
	return sess, nil
}

func itemByNameToRow(r sqlc.ItemByNameRow) sqlc.ItemByIDRow {
	return sqlc.ItemByIDRow{
		ID: r.ID, OrgID: r.OrgID, Name: r.Name, Kind: r.Kind,
		OwnerKind: r.OwnerKind, OwnerID: r.OwnerID, Uris: r.Uris,
		HasTotp: r.HasTotp, Tags: r.Tags, Archived: r.Archived,
		HasFile: r.HasFile, Login: r.Login,
	}
}

func listItemsToRow(r sqlc.ListItemsRow) sqlc.ItemByIDRow {
	return sqlc.ItemByIDRow{
		ID: r.ID, OrgID: r.OrgID, Name: r.Name, Kind: r.Kind,
		OwnerKind: r.OwnerKind, OwnerID: r.OwnerID, Uris: r.Uris,
		HasTotp: r.HasTotp, Tags: r.Tags, Archived: r.Archived,
		HasFile: r.HasFile, Login: r.Login,
	}
}

func itemFromSqlc(r *sqlc.ItemByIDRow) protocol.Item {
	item := protocol.Item{
		ID:    r.ID,
		OrgID: r.OrgID,
		Name:  r.Name,
		Kind:  protocol.ItemKind(r.Kind),
		Owner: protocol.Owner{Kind: protocol.OwnerKind(r.OwnerKind), ID: r.OwnerID},
	}
	if len(r.Uris) > 0 {
		_ = json.Unmarshal([]byte(r.Uris), &item.URIs)
	}
	if len(r.Tags) > 0 {
		_ = json.Unmarshal([]byte(r.Tags), &item.Tags)
	}
	item.HasTOTP = r.HasTotp
	item.Archived = r.Archived
	item.HasFile = r.HasFile
	item.Login = r.Login
	return item
}

func (p *Postgres) PutItem(item protocol.Item, secret Secret) error {
	uris, err := json.Marshal(item.URIs)
	if err != nil {
		return err
	}
	if item.URIs == nil {
		uris = []byte("[]")
	}
	tags, err := json.Marshal(item.Tags)
	if err != nil {
		return err
	}
	if item.Tags == nil {
		tags = []byte("[]")
	}
	dek, err := p.ownerDEK(item.OrgID, item.Owner)
	if err != nil {
		return err
	}
	blob, err := crypto.SealEpoch(dek, secret, itemAAD(item.OrgID, item.ID))
	if err != nil {
		return err
	}

	ctx := context.Background()
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	qtx := p.sqlc.WithTx(tx)

	owner, err := qtx.ItemOwner(ctx, item.ID)
	if err == nil {
		if owner.OwnerKind != string(item.Owner.Kind) || owner.OwnerID != item.Owner.ID || owner.OrgID != item.OrgID {
			return fmt.Errorf("store: cannot change item owner")
		}
	} else if err != pgx.ErrNoRows {
		return err
	}

	if err := qtx.SnapshotItem(ctx, sqlc.SnapshotItemParams{ItemID: item.ID, At: time.Now().UTC()}); err != nil {
		return err
	}
	rows, err := qtx.PutItem(ctx, sqlc.PutItemParams{
		ID: item.ID, OrgID: item.OrgID, Name: item.Name, Kind: string(item.Kind),
		OwnerKind: string(item.Owner.Kind), OwnerID: item.Owner.ID,
		Uris: string(uris), Secret: blob, HasTotp: item.HasTOTP,
		Tags: string(tags), Archived: item.Archived, HasFile: item.HasFile,
		Login: item.Login,
	})
	if err != nil {
		return err
	}
	// The DO UPDATE WHERE clause is the atomic backstop: a row created between
	// ItemOwner and this upsert under another owner or org matches zero rows.
	if rows == 0 {
		return fmt.Errorf("store: cannot change item owner")
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

func (p *Postgres) Item(id string) (protocol.Item, error) {
	r, err := p.sqlc.ItemByID(context.Background(), id)
	if err == pgx.ErrNoRows {
		return protocol.Item{}, ErrNotFound
	}
	if err != nil {
		return protocol.Item{}, err
	}
	return itemFromSqlc(&r), nil
}

func (p *Postgres) ItemByName(orgID, name string) (protocol.Item, error) {
	r, err := p.sqlc.ItemByName(context.Background(), sqlc.ItemByNameParams{OrgID: orgID, Name: name})
	if err == pgx.ErrNoRows {
		return protocol.Item{}, ErrNotFound
	}
	if err != nil {
		return protocol.Item{}, err
	}
	row := itemByNameToRow(r)
	return itemFromSqlc(&row), nil
}

func (p *Postgres) ListItems() ([]protocol.Item, error) {
	rows, err := p.sqlc.ListItems(context.Background(), maxListResults)
	if err != nil {
		return nil, err
	}
	out := make([]protocol.Item, 0, len(rows))
	for i := range rows {
		r := listItemsToRow(rows[i])
		out = append(out, itemFromSqlc(&r))
	}
	return out, nil
}

func (p *Postgres) ArchiveItem(id string) error {
	n, err := p.sqlc.ArchiveItem(context.Background(), id)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) DeleteItem(id string) error {
	ctx := context.Background()
	if err := p.sqlc.DeleteItemVersions(ctx, id); err != nil {
		return err
	}
	if err := p.sqlc.DeleteItemGrants(ctx, id); err != nil {
		return err
	}
	n, err := p.sqlc.DeleteItem(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) Versions(itemID string) ([]protocol.ItemVersion, error) {
	rows, err := p.sqlc.ItemVersions(context.Background(), sqlc.ItemVersionsParams{ItemID: itemID, MaxResults: maxListResults})
	if err != nil {
		return nil, err
	}
	out := make([]protocol.ItemVersion, 0, len(rows))
	for i := range rows {
		out = append(out, protocol.ItemVersion{ID: rows[i].ID, ItemID: rows[i].ItemID, Time: rows[i].At.UTC()})
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func (p *Postgres) RestoreVersion(itemID string, versionID int64) error {
	ctx := context.Background()
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	qtx := p.sqlc.WithTx(tx)

	if err := qtx.SnapshotItem(ctx, sqlc.SnapshotItemParams{ItemID: itemID, At: time.Now().UTC()}); err != nil {
		return err
	}
	blob, err := qtx.ItemVersionSecret(ctx, sqlc.ItemVersionSecretParams{ID: versionID, ItemID: itemID})
	if err == pgx.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := qtx.RestoreItemSecret(ctx, sqlc.RestoreItemSecretParams{Secret: blob, ID: itemID}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

func (p *Postgres) Secret(id string) (Secret, error) {
	r, err := retryOnDeadConn(func() (sqlc.ItemSecretOwnerRow, error) {
		return p.sqlc.ItemSecretOwner(context.Background(), id)
	})
	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	dek, err := p.ownerDEK(r.OrgID, protocol.Owner{Kind: protocol.OwnerKind(r.OwnerKind), ID: r.OwnerID})
	if err != nil {
		return nil, err
	}
	plain, err := crypto.OpenEpoch(dek, r.Secret, itemAAD(r.OrgID, id))
	if err != nil {
		return nil, err
	}
	return Secret(plain), nil
}

func grantFromSqlc(r *sqlc.Grant) *protocol.Grant {
	g := &protocol.Grant{
		ID: r.ID, OrgID: r.OrgID, AgentID: r.AgentID, ItemID: r.ItemID,
		Level: protocol.GrantLevel(r.Level),
	}
	if len(r.Actions) > 0 {
		_ = json.Unmarshal([]byte(r.Actions), &g.Actions)
	}
	if r.ExpiresAt.Valid {
		t := r.ExpiresAt.Time.UTC()
		g.ExpiresAt = &t
	}
	return g
}

func (p *Postgres) PutGrant(g protocol.Grant) error {
	actions, err := json.Marshal(g.Actions)
	if err != nil {
		return err
	}
	return p.sqlc.PutGrant(context.Background(), sqlc.PutGrantParams{
		ID: g.ID, OrgID: g.OrgID, AgentID: g.AgentID, ItemID: g.ItemID,
		Level: string(g.Level), Actions: string(actions), ExpiresAt: nullTime(g.ExpiresAt),
	})
}

func (p *Postgres) Grant(id string) (*protocol.Grant, error) {
	r, err := p.sqlc.GrantByID(context.Background(), id)
	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return grantFromSqlc(&r), nil
}

func (p *Postgres) GrantFor(agentID, itemID string) (*protocol.Grant, error) {
	r, err := p.sqlc.GrantFor(context.Background(), sqlc.GrantForParams{AgentID: agentID, ItemID: itemID})
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return grantFromSqlc(&r), nil
}

func (p *Postgres) ListGrants() ([]protocol.Grant, error) {
	rows, err := p.sqlc.ListGrants(context.Background(), maxListResults)
	if err != nil {
		return nil, err
	}
	out := make([]protocol.Grant, 0, len(rows))
	for i := range rows {
		out = append(out, *grantFromSqlc(&rows[i]))
	}
	return out, nil
}

func (p *Postgres) PutApproval(a protocol.Approval) error {
	return p.sqlc.PutApproval(context.Background(), sqlc.PutApprovalParams{
		GrantID: a.GrantID, ID: a.ID, HumanID: a.HumanID, ExpiresAt: a.ExpiresAt.UTC(),
	})
}

func (p *Postgres) LiveApproval(grantID string, now time.Time) (*protocol.Approval, error) {
	r, err := p.sqlc.ApprovalByGrant(context.Background(), grantID)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a := protocol.Approval{GrantID: r.GrantID, ID: r.ID, HumanID: r.HumanID, ExpiresAt: r.ExpiresAt.UTC()}
	if !now.Before(a.ExpiresAt) {
		return nil, nil
	}
	return &a, nil
}

func requestFromSqlc(r *sqlc.ApprovalRequest) protocol.ApprovalRequest {
	out := protocol.ApprovalRequest{
		ID: r.ID, OrgID: r.OrgID, AgentID: r.AgentID, ItemID: r.ItemID,
		GrantID: r.GrantID, Action: protocol.ActionKind(r.Action),
		Status:    protocol.RequestStatus(r.Status),
		CreatedAt: r.CreatedAt.UTC(), ExpiresAt: r.ExpiresAt.UTC(),
		ResolvedBy: r.ResolvedBy.String, ApprovalID: r.ApprovalID.String,
	}
	if r.ResolvedAt.Valid {
		at := r.ResolvedAt.Time.UTC()
		out.ResolvedAt = &at
	}
	return out
}

// FileRequest expires a stale open on (grant, action) then inserts —
// the dedupe partial index makes a live open swallow the insert, so a
// missing RETURNING row means "already filed": select and reuse it.
func (p *Postgres) FileRequest(req protocol.ApprovalRequest) (FileOutcome, error) {
	ctx := context.Background()
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return FileOutcome{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := p.sqlc.WithTx(tx)
	var expiredID string
	exp, err := q.ExpireOpenRequest(ctx, sqlc.ExpireOpenRequestParams{
		GrantID: req.GrantID, Action: string(req.Action), Now: req.CreatedAt.UTC(),
	})
	switch {
	case err == nil:
		expiredID = exp
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return FileOutcome{}, err
	}
	row, err := q.InsertRequest(ctx, sqlc.InsertRequestParams{
		ID: req.ID, OrgID: req.OrgID, AgentID: req.AgentID, ItemID: req.ItemID,
		GrantID: req.GrantID, Action: string(req.Action),
		CreatedAt: req.CreatedAt.UTC(), ExpiresAt: req.ExpiresAt.UTC(),
	})
	created := true
	if err == pgx.ErrNoRows {
		created = false
		row, err = q.OpenRequestByGrant(ctx, sqlc.OpenRequestByGrantParams{
			GrantID: req.GrantID, Action: string(req.Action),
		})
	}
	if err != nil {
		return FileOutcome{}, err
	}
	// Audit inside the same commit: the expired predecessor's event and the
	// fresh file's event land with the rows they describe — a filed or
	// expired ask can never exist without its line in the log.
	if expiredID != "" {
		if err := auditInTx(ctx, tx, sqlc.InsertAuditParams{
			At: req.CreatedAt.UTC(), OrgID: req.OrgID, AgentID: req.AgentID, ItemID: req.ItemID,
			Action: string(protocol.ActionRequestExpired), Decision: string(protocol.DecisionNeedApproval),
			Reason: expiredID,
		}); err != nil {
			return FileOutcome{}, err
		}
	}
	if created {
		if err := auditInTx(ctx, tx, sqlc.InsertAuditParams{
			At: req.CreatedAt.UTC(), OrgID: req.OrgID, AgentID: req.AgentID, ItemID: req.ItemID,
			Action: string(protocol.ActionRequestFiled), Decision: string(protocol.DecisionNeedApproval),
			Reason: req.ID,
		}); err != nil {
			return FileOutcome{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return FileOutcome{}, err
	}
	return FileOutcome{Request: requestFromSqlc(&row), Created: created, ExpiredID: expiredID}, nil
}

func (p *Postgres) Request(id string) (protocol.ApprovalRequest, error) {
	r, err := p.sqlc.RequestByID(context.Background(), id)
	if err == pgx.ErrNoRows {
		return protocol.ApprovalRequest{}, ErrNotFound
	}
	if err != nil {
		return protocol.ApprovalRequest{}, err
	}
	return requestFromSqlc(&r), nil
}

func (p *Postgres) ListRequests(orgID string, status protocol.RequestStatus, now time.Time) ([]protocol.ApprovalRequest, error) {
	ctx := context.Background()
	var rows []sqlc.ApprovalRequest
	var err error
	if status == protocol.RequestOpen {
		rows, err = p.sqlc.ListOpenRequests(ctx, sqlc.ListOpenRequestsParams{OrgID: orgID, Now: now.UTC(), MaxResults: maxListResults})
	} else {
		rows, err = p.sqlc.ListRequestsByStatus(ctx, sqlc.ListRequestsByStatusParams{OrgID: orgID, Status: string(status), MaxResults: maxListResults})
	}
	if err != nil {
		return nil, err
	}
	out := make([]protocol.ApprovalRequest, 0, len(rows))
	for i := range rows {
		out = append(out, requestFromSqlc(&rows[i]))
	}
	return out, nil
}

func (p *Postgres) ResolveRequest(id string, status protocol.RequestStatus, humanID, approvalID string, at time.Time) (protocol.ApprovalRequest, bool, error) {
	ctx := context.Background()
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return protocol.ApprovalRequest{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := p.sqlc.WithTx(tx).ResolveOpenRequest(ctx, sqlc.ResolveOpenRequestParams{
		ID: id, Status: string(status), HumanID: humanID, At: at.UTC(),
		ApprovalID: sql.NullString{String: approvalID, Valid: approvalID != ""},
	})
	if err == pgx.ErrNoRows {
		cur, err := p.Request(id)
		return cur, false, err
	}
	if err != nil {
		return protocol.ApprovalRequest{}, false, err
	}
	out := requestFromSqlc(&row)
	// Audit in the same commit — a denied/cancelled ask never exists
	// without the line recording it.
	if err := auditInTx(ctx, tx, requestAuditEvent(out, at, approvalID)); err != nil {
		return protocol.ApprovalRequest{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return protocol.ApprovalRequest{}, false, err
	}
	return out, true, nil
}

func (p *Postgres) ApproveRequest(id string, appr protocol.Approval, at time.Time) ([]protocol.ApprovalRequest, bool, error) {
	ctx := context.Background()
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := p.sqlc.WithTx(tx)
	target, err := q.ApproveOpenRequest(ctx, sqlc.ApproveOpenRequestParams{
		ID: id, HumanID: appr.HumanID, ApprovalID: appr.ID, At: at.UTC(),
	})
	if err == pgx.ErrNoRows {
		// Ask already resolved, expired, or its grant died — roll back so
		// nothing is written.
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if err := q.PutApproval(ctx, sqlc.PutApprovalParams{
		GrantID: target.GrantID, ID: appr.ID, HumanID: appr.HumanID,
		ExpiresAt: appr.ExpiresAt.UTC(),
	}); err != nil {
		return nil, false, err
	}
	sibs, err := q.ApproveRequestsForGrant(ctx, sqlc.ApproveRequestsForGrantParams{
		GrantID: target.GrantID, HumanID: appr.HumanID, ApprovalID: appr.ID, At: at.UTC(),
	})
	if err != nil {
		return nil, false, err
	}
	out := make([]protocol.ApprovalRequest, 0, len(sibs)+1)
	out = append(out, requestFromSqlc(&target))
	for i := range sibs {
		out = append(out, requestFromSqlc(&sibs[i]))
	}
	// Audit in the same commit: every ask this approval resolves gets a
	// request_approved line — the decision never exists without its record.
	for i := range out {
		if err := auditInTx(ctx, tx, requestAuditEvent(out[i], at, appr.ID)); err != nil {
			return nil, false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func (p *Postgres) ApproveGrant(grantID string, appr protocol.Approval, at time.Time) ([]protocol.ApprovalRequest, error) {
	ctx := context.Background()
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := p.sqlc.WithTx(tx)
	if _, err := q.LiveGrantForApprove(ctx, sqlc.LiveGrantForApproveParams{
		GrantID: grantID, At: at.UTC(),
	}); err == pgx.ErrNoRows {
		return nil, ErrGrantNotLive
	} else if err != nil {
		return nil, err
	}
	if err := q.PutApproval(ctx, sqlc.PutApprovalParams{
		GrantID: grantID, ID: appr.ID, HumanID: appr.HumanID, ExpiresAt: appr.ExpiresAt.UTC(),
	}); err != nil {
		return nil, err
	}
	rows, err := q.ApproveRequestsForGrant(ctx, sqlc.ApproveRequestsForGrantParams{
		GrantID: grantID, HumanID: appr.HumanID, ApprovalID: appr.ID, At: at.UTC(),
	})
	if err != nil {
		return nil, err
	}
	out := make([]protocol.ApprovalRequest, 0, len(rows))
	for i := range rows {
		out = append(out, requestFromSqlc(&rows[i]))
	}
	for i := range out {
		if err := auditInTx(ctx, tx, requestAuditEvent(out[i], at, appr.ID)); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

// CancelRequestsForItem resolves every open ask on an archived/deleted item
// as cancelled and writes each request_cancelled event in the same commit —
// an ask must never vanish without an audit line saying why.
func (p *Postgres) CancelRequestsForItem(itemID string, at time.Time) error {
	return p.cancelRequests(context.Background(), itemID, true, at)
}

func (p *Postgres) CancelRequestsForAgent(agentID string, at time.Time) error {
	return p.cancelRequests(context.Background(), agentID, false, at)
}

func (p *Postgres) cancelRequests(ctx context.Context, id string, byItem bool, at time.Time) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := p.sqlc.WithTx(tx)
	var rows []sqlc.ApprovalRequest
	if byItem {
		rows, err = q.CancelRequestsForItem(ctx, sqlc.CancelRequestsForItemParams{ItemID: id, At: at.UTC()})
	} else {
		rows, err = q.CancelRequestsForAgent(ctx, sqlc.CancelRequestsForAgentParams{AgentID: id, At: at.UTC()})
	}
	if err != nil {
		return err
	}
	for i := range rows {
		if err := auditInTx(ctx, tx, requestAuditEvent(requestFromSqlc(&rows[i]), at, "")); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (p *Postgres) ExpireStaleRequests(now time.Time) ([]protocol.ApprovalRequest, error) {
	rows, err := p.sqlc.ExpireStaleRequests(context.Background(), now.UTC())
	if err != nil {
		return nil, err
	}
	out := make([]protocol.ApprovalRequest, 0, len(rows))
	for i := range rows {
		out = append(out, requestFromSqlc(&rows[i]))
	}
	return out, nil
}

func (p *Postgres) loadOwnerWrapped(ctx context.Context, orgID string, o protocol.Owner) ([]byte, error) {
	wrapped, err := retryOnDeadConn(func() ([]byte, error) {
		return p.sqlc.OwnerWrapped(ctx, sqlc.OwnerWrappedParams{
			OrgID:     orgID,
			OwnerKind: string(o.Kind),
			OwnerID:   o.ID,
		})
	})
	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	return wrapped, err
}

// mintOwnerWrapped seals dek under the org's committed master and inserts
// the wrap, in one transaction. The FOR SHARE read of org_keys serializes
// against RotateOrgKey's FOR UPDATE: either the mint lands first (rotation
// rewraps it) or it waits and seals under the new master — a wrap can never
// commit under a master rotation just retired, even from a stale replica.
func (p *Postgres) mintOwnerWrapped(ctx context.Context, orgID string, o protocol.Owner, dek []byte) error {
	// kekMu.RLock outermost — before any DB lock (see RotateKEK). A caller
	// holding FOR SHARE can never wait on kekMu while RotateKEK holds it,
	// which is exactly the cycle that would deadlock.
	p.kekMu.RLock()
	defer p.kekMu.RUnlock()
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := p.sqlc.WithTx(tx)
	row, err := q.OrgKeyForShare(ctx, orgID)
	if err == pgx.ErrNoRows {
		return ErrOrgKeyMissing
	}
	if err != nil {
		return err
	}
	master, err := crypto.OpenAAD(p.kek, row.Wrapped, []byte(orgID))
	if err != nil {
		return fmt.Errorf("store: org_keys row for %s does not unwrap under this KEK: %w", orgID, err)
	}
	sealed, err := crypto.SealEpoch(master, dek, ownerWrapAAD(orgID, o))
	if err != nil {
		return err
	}
	if err := q.PutOwnerWrapped(ctx, sqlc.PutOwnerWrappedParams{
		OrgID:     orgID,
		OwnerKind: string(o.Kind),
		OwnerID:   o.ID,
		Wrapped:   sealed,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func agentFromSqlc(a *sqlc.Agent) protocol.Principal {
	p := protocol.Principal{
		Kind:  protocol.PrincipalAgent,
		ID:    a.ID,
		OrgID: a.OrgID,
		Owner: protocol.Owner{Kind: protocol.OwnerKind(a.OwnerKind), ID: a.OwnerID},
	}
	if a.RevokedAt.Valid {
		t := a.RevokedAt.Time.UTC()
		p.RevokedAt = &t
	}
	return p
}

func (p *Postgres) PutAgent(agent protocol.Principal) error {
	return p.sqlc.PutAgent(context.Background(), sqlc.PutAgentParams{
		ID:        agent.ID,
		OrgID:     agent.OrgID,
		OwnerKind: string(agent.Owner.Kind),
		OwnerID:   agent.Owner.ID,
		RevokedAt: nullTime(agent.RevokedAt),
	})
}

func (p *Postgres) Agent(id string) (protocol.Principal, error) {
	a, err := p.sqlc.AgentByID(context.Background(), id)
	if err == pgx.ErrNoRows {
		return protocol.Principal{}, ErrNotFound
	}
	if err != nil {
		return protocol.Principal{}, err
	}
	return agentFromSqlc(&a), nil
}

func (p *Postgres) ListAgents() ([]protocol.Principal, error) {
	rows, err := p.sqlc.ListAgents(context.Background(), maxListResults)
	if err != nil {
		return nil, err
	}
	out := make([]protocol.Principal, 0, len(rows))
	for i := range rows {
		out = append(out, agentFromSqlc(&rows[i]))
	}
	return out, nil
}

func (p *Postgres) RevokeAgent(id string, at time.Time, audit ...protocol.AuditEvent) error {
	ctx := context.Background()
	if len(audit) == 0 {
		n, err := p.sqlc.RevokeAgent(ctx, sqlc.RevokeAgentParams{At: at.UTC(), ID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		return nil
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	qtx := p.sqlc.WithTx(tx)

	n, err := qtx.RevokeAgent(ctx, sqlc.RevokeAgentParams{At: at.UTC(), ID: id})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	for _, e := range audit {
		if err := qtx.InsertAudit(ctx, sqlc.InsertAuditParams{
			At: e.Time.UTC(), OrgID: e.OrgID, AgentID: e.AgentID, ItemID: e.ItemID,
			Action: string(e.Action), Decision: string(e.Decision), Reason: e.Reason, ApprovalID: e.ApprovalID,
		}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func humanFromSqlc(h *sqlc.Human) protocol.Principal {
	return protocol.Principal{Kind: protocol.PrincipalHuman, ID: h.ID, OrgID: h.OrgID}
}

func (p *Postgres) PlantHuman(h protocol.Principal) (bool, error) {
	n, err := retryOnDeadConn(func() (int64, error) {
		return p.sqlc.PlantHuman(context.Background(), sqlc.PlantHumanParams{ID: h.ID, OrgID: h.OrgID})
	})
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func (p *Postgres) PutHuman(h protocol.Principal) error {
	return p.sqlc.PutHuman(context.Background(), sqlc.PutHumanParams{ID: h.ID, OrgID: h.OrgID})
}

func (p *Postgres) Human(id string) (protocol.Principal, error) {
	h, err := p.sqlc.HumanByID(context.Background(), id)
	if err == pgx.ErrNoRows {
		return protocol.Principal{}, ErrNotFound
	}
	if err != nil {
		return protocol.Principal{}, err
	}
	return humanFromSqlc(&h), nil
}

func (p *Postgres) ListHumans() ([]protocol.Principal, error) {
	rows, err := p.sqlc.ListHumans(context.Background(), maxListResults)
	if err != nil {
		return nil, err
	}
	out := make([]protocol.Principal, 0, len(rows))
	for i := range rows {
		out = append(out, humanFromSqlc(&rows[i]))
	}
	return out, nil
}

func (p *Postgres) PutWorkload(w protocol.Workload) error {
	return p.sqlc.PutWorkload(context.Background(), sqlc.PutWorkloadParams{
		Issuer: w.Issuer, Subject: w.Subject, AgentID: w.AgentID, Audience: w.Audience,
	})
}

func (p *Postgres) Workload(issuer, subject string) (*protocol.Workload, error) {
	r, err := p.sqlc.WorkloadByKey(context.Background(), sqlc.WorkloadByKeyParams{Issuer: issuer, Subject: subject})
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &protocol.Workload{Issuer: r.Issuer, Subject: r.Subject, AgentID: r.AgentID, Audience: r.Audience}, nil
}

func (p *Postgres) WorkloadsForIssuer(issuer string) ([]protocol.Workload, error) {
	rows, err := p.sqlc.WorkloadsForIssuer(context.Background(), sqlc.WorkloadsForIssuerParams{Issuer: issuer, MaxResults: maxListResults})
	if err != nil {
		return nil, err
	}
	out := make([]protocol.Workload, 0, len(rows))
	for i := range rows {
		out = append(out, protocol.Workload{Issuer: rows[i].Issuer, Subject: rows[i].Subject, AgentID: rows[i].AgentID, Audience: rows[i].Audience})
	}
	return out, nil
}

func (p *Postgres) AppendAudit(e protocol.AuditEvent) error {
	// Single events take a plain INSERT on the request path — no COPY stream
	// setup for one row.
	_, err := retryOnDeadConn(func() (struct{}, error) {
		return struct{}{}, p.sqlc.InsertAudit(context.Background(), sqlc.InsertAuditParams{
			At: e.Time.UTC(), OrgID: e.OrgID, AgentID: e.AgentID, ItemID: e.ItemID,
			Action: string(e.Action), Decision: string(e.Decision), Reason: e.Reason, ApprovalID: e.ApprovalID,
		})
	})
	if err == nil {
		return nil
	}
	if !shouldOutbox(err) {
		// Ambiguous failure — the row may already be committed. Returning
		// the error beats a possible duplicate write through the outbox.
		return err
	}
	// Durable fallback: the event lands in audit_outbox for the relay to
	// re-land in `audit`. An error is only returned when both stores failed.
	_, oerr := retryOnDeadConn(func() (struct{}, error) {
		return struct{}{}, p.sqlc.InsertAuditOutbox(context.Background(), sqlc.InsertAuditOutboxParams{
			At: e.Time.UTC(), OrgID: e.OrgID, AgentID: e.AgentID, ItemID: e.ItemID,
			Action: string(e.Action), Decision: string(e.Decision), Reason: e.Reason, ApprovalID: e.ApprovalID,
		})
	})
	if oerr != nil {
		return fmt.Errorf("audit write failed (%w) and outbox fallback failed (%v)", err, oerr)
	}
	return nil
}

// shouldOutbox reports whether a failed write provably did not commit, so
// queueing the event cannot double-write it. SQLSTATE-class errors are
// deterministic failures; connection errors qualify only when pgconn marks
// them SafeToRetry. Anything else (ambiguous timeouts) stays a loud error.
func shouldOutbox(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return true
	}
	return pgconn.SafeToRetry(err)
}

func (p *Postgres) AppendAudits(events []protocol.AuditEvent) error {
	if len(events) == 0 {
		return nil
	}
	cols := []string{"at", "org_id", "agent_id", "item_id", "action", "decision", "reason", "approval_id"}
	_, err := retryOnDeadConn(func() (int64, error) {
		return p.auditPool.CopyFrom(context.Background(), pgx.Identifier{"audit"}, cols, &auditCopySource{events: events})
	})
	if err == nil {
		return nil
	}
	if !shouldOutbox(err) {
		return err
	}
	_, oerr := retryOnDeadConn(func() (int64, error) {
		return p.auditPool.CopyFrom(context.Background(), pgx.Identifier{"audit_outbox"}, cols, &auditCopySource{events: events})
	})
	if oerr != nil {
		return fmt.Errorf("audit batch write failed (%w) and outbox fallback failed (%v)", err, oerr)
	}
	return nil
}

// FlushAuditOutbox claims up to limit queued events and lands them in `audit`,
// in one transaction: a crash mid-relay leaves the rows queued for retry, and
// concurrent relays take disjoint rows via FOR UPDATE SKIP LOCKED. Returns the
// number of events relayed.
func (p *Postgres) FlushAuditOutbox(limit int) (int, error) {
	ctx := context.Background()
	tx, err := p.auditPool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	qtx := p.sqlc.WithTx(tx)
	rows, err := qtx.ClaimAuditOutbox(ctx, int64(limit))
	if err != nil {
		return 0, err
	}
	if len(rows) > 0 {
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"audit"}, []string{
			"at", "org_id", "agent_id", "item_id", "action", "decision", "reason", "approval_id",
		}, pgx.CopyFromRows(outboxRows(rows))); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	committed = true
	return len(rows), nil
}

// outboxRows adapts claimed outbox rows to pgx.CopyFromSource.
func outboxRows(rows []sqlc.AuditOutbox) [][]interface{} {
	out := make([][]interface{}, len(rows))
	for i, r := range rows {
		out[i] = []interface{}{r.At.UTC(), r.OrgID, r.AgentID, r.ItemID, r.Action, r.Decision, r.Reason, r.ApprovalID}
	}
	return out
}

type auditCopySource struct {
	events []protocol.AuditEvent
	idx    int
}

func (s *auditCopySource) Next() bool {
	return s.idx < len(s.events)
}

func (s *auditCopySource) Values() ([]interface{}, error) {
	e := s.events[s.idx]
	s.idx++
	return []interface{}{e.Time.UTC(), e.OrgID, e.AgentID, e.ItemID, e.Action, e.Decision, e.Reason, e.ApprovalID}, nil
}

func (s *auditCopySource) Err() error {
	return nil
}

func (p *Postgres) Audit() ([]protocol.AuditEvent, error) {
	rows, err := p.sqlc.ListAudit(context.Background(), maxListResults)
	if err != nil {
		return nil, err
	}
	out := make([]protocol.AuditEvent, len(rows))
	for i := range rows {
		r := rows[len(rows)-1-i]
		out[i] = protocol.AuditEvent{
			Time: r.At.UTC(), OrgID: r.OrgID, AgentID: r.AgentID, ItemID: r.ItemID,
			Action: protocol.ActionKind(r.Action), Decision: protocol.Decision(r.Decision), Reason: r.Reason, ApprovalID: r.ApprovalID,
		}
	}
	return out, nil
}

// SweepPostgres deletes terminally-expired rows older than before: sessions
// past expiry or revoked, and grants/approvals past expiry. Open asks past
// their own TTL mark expired and each expiry writes a request_expired audit
// event. It takes a bare DBTX so callers that never touch ciphertext (e.g.
// `veil sweep`) do not need the master key.
//
// When db can open a transaction (pool, conn) the whole sweep runs in one:
// request expirations and their audit rows commit together or not at all —
// a mid-sweep crash can never strand an expired ask with no audit line.
// Each audit insert runs under a savepoint so a failed insert falls back to
// audit_outbox inside the same transaction instead of aborting it. A
// caller-passed pgx.Tx is already atomic and gets the same treatment; a
// DBTX that cannot begin a transaction runs autocommitted with the per-row
// outbox fallback.
func SweepPostgres(ctx context.Context, db sqlc.DBTX, before time.Time) (SweepReport, error) {
	if tx, ok := db.(pgx.Tx); ok {
		return sweepPostgresRun(ctx, tx, before, sweepAuditSavepoint(tx))
	}
	if txer, ok := db.(interface {
		Begin(context.Context) (pgx.Tx, error)
	}); ok {
		tx, err := txer.Begin(ctx)
		if err != nil {
			return SweepReport{}, fmt.Errorf("sweep begin: %w", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		rep, err := sweepPostgresRun(ctx, tx, before, sweepAuditSavepoint(tx))
		if err != nil {
			return rep, err
		}
		if err := tx.Commit(ctx); err != nil {
			return rep, fmt.Errorf("sweep commit: %w", err)
		}
		return rep, nil
	}
	return sweepPostgresRun(ctx, db, before, sweepAuditAutocommit(db))
}

func sweepPostgresRun(ctx context.Context, db sqlc.DBTX, before time.Time, auditFn func(context.Context, sqlc.InsertAuditParams) error) (SweepReport, error) {
	q := sqlc.New(db)
	var rep SweepReport
	var err error
	if rep.Sessions, err = q.SweepExpiredSessions(ctx, before.UTC()); err != nil {
		return rep, fmt.Errorf("sweep sessions: %w", err)
	}
	if rep.Grants, err = q.SweepExpiredGrants(ctx, before.UTC()); err != nil {
		return rep, fmt.Errorf("sweep grants: %w", err)
	}
	if rep.Approvals, err = q.SweepExpiredApprovals(ctx, before.UTC()); err != nil {
		return rep, fmt.Errorf("sweep approvals: %w", err)
	}
	// Open asks mark expired at expiry — the row stays auditable, the ask
	// dies, and each expiry is itself a security event: "nobody answered in
	// time" lands in audit, not just in the row.
	expired, err := q.ExpireStaleRequests(ctx, time.Now().UTC())
	if err != nil {
		return rep, fmt.Errorf("sweep requests: %w", err)
	}
	rep.Requests = int64(len(expired))
	for i := range expired {
		ev := sqlc.InsertAuditParams{
			At: time.Now().UTC(), OrgID: expired[i].OrgID, AgentID: expired[i].AgentID,
			ItemID: expired[i].ItemID, Action: string(protocol.ActionRequestExpired),
			Decision: string(protocol.DecisionNeedApproval), Reason: expired[i].ID,
		}
		if err := auditFn(ctx, ev); err != nil {
			return rep, fmt.Errorf("sweep request audit: %w", err)
		}
	}
	return rep, nil
}

// auditInTx writes one audit event inside tx under a savepoint: a failed
// statement aborts the whole transaction in Postgres, so rolling back to
// the savepoint first keeps the tx alive and the outbox row commits with
// it — the state change can never land without its audit line or a durable
// queue entry. Resolution paths use this so the event is atomic with the
// decision it records.
func auditInTx(ctx context.Context, tx pgx.Tx, ev sqlc.InsertAuditParams) error {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return err
	}
	err = sqlc.New(sp).InsertAudit(ctx, ev)
	if err == nil {
		return sp.Commit(ctx)
	}
	_ = sp.Rollback(ctx)
	if oerr := sqlc.New(tx).InsertAuditOutbox(ctx, sqlc.InsertAuditOutboxParams{
		At: ev.At, OrgID: ev.OrgID, AgentID: ev.AgentID, ItemID: ev.ItemID,
		Action: ev.Action, Decision: ev.Decision, Reason: ev.Reason, ApprovalID: ev.ApprovalID,
	}); oerr != nil {
		return fmt.Errorf("%w (outbox: %v)", err, oerr)
	}
	return nil
}

// requestAuditEvent maps a resolved ask to its audit row: the action follows
// the resolution status and Reason names the request so the event joins back
// to the authoritative row.
func requestAuditEvent(r protocol.ApprovalRequest, at time.Time, approvalID string) sqlc.InsertAuditParams {
	action := protocol.ActionRequestApproved
	decision := protocol.DecisionAllow
	switch r.Status {
	case protocol.RequestDenied:
		action, decision = protocol.ActionRequestDenied, protocol.DecisionDeny
	case protocol.RequestCancelled:
		action, decision = protocol.ActionRequestCancelled, protocol.DecisionDeny
	case protocol.RequestExpired:
		action, decision = protocol.ActionRequestExpired, protocol.DecisionNeedApproval
	}
	return sqlc.InsertAuditParams{
		At: at.UTC(), OrgID: r.OrgID, AgentID: r.AgentID, ItemID: r.ItemID,
		Action: string(action), Decision: string(decision), Reason: r.ID,
		ApprovalID: approvalID,
	}
}

func sweepAuditSavepoint(tx pgx.Tx) func(context.Context, sqlc.InsertAuditParams) error {
	return func(ctx context.Context, ev sqlc.InsertAuditParams) error {
		return auditInTx(ctx, tx, ev)
	}
}

// sweepAuditAutocommit is the no-transaction path: direct insert, outbox on
// provable failure — the pre-atomic contract for exotic DBTX wrappers.
func sweepAuditAutocommit(db sqlc.DBTX) func(context.Context, sqlc.InsertAuditParams) error {
	q := sqlc.New(db)
	return func(ctx context.Context, ev sqlc.InsertAuditParams) error {
		if err := q.InsertAudit(ctx, ev); err != nil {
			if oerr := q.InsertAuditOutbox(ctx, sqlc.InsertAuditOutboxParams{
				At: ev.At, OrgID: ev.OrgID, AgentID: ev.AgentID, ItemID: ev.ItemID,
				Action: ev.Action, Decision: ev.Decision, Reason: ev.Reason, ApprovalID: ev.ApprovalID,
			}); oerr != nil {
				return fmt.Errorf("%w (outbox: %v)", err, oerr)
			}
		}
		return nil
	}
}

func (p *Postgres) Sweep(olderThan time.Time) (SweepReport, error) {
	return SweepPostgres(context.Background(), p.pool, olderThan)
}

func (p *Postgres) Billing(orgID string) (OrgBilling, error) {
	row, err := p.sqlc.GetOrgBilling(context.Background(), orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return OrgBilling{OrgID: orgID, Plan: "free"}, nil
	}
	if err != nil {
		return OrgBilling{}, err
	}
	return OrgBilling{
		OrgID:            row.OrgID,
		Plan:             row.Plan,
		CustomerID:       row.CustomerID,
		BillingAccountID: row.BillingAccountID,
		UpdatedAt:        row.UpdatedAt.UTC(),
	}, nil
}

func (p *Postgres) SetBilling(ob OrgBilling) error {
	return p.sqlc.UpsertOrgBilling(context.Background(), sqlc.UpsertOrgBillingParams{
		OrgID:            ob.OrgID,
		Plan:             ob.Plan,
		CustomerID:       ob.CustomerID,
		BillingAccountID: ob.BillingAccountID,
		UpdatedAt:        ob.UpdatedAt.UTC(),
	})
}

func (p *Postgres) OrgByBillingCustomer(customerID string) (string, error) {
	orgID, err := p.sqlc.GetOrgIDByBillingCustomer(context.Background(), customerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return orgID, err
}

func (p *Postgres) ConsumeUse(orgID string, window time.Time, cap int64) (int64, bool, error) {
	used, err := retryOnDeadConn(func() (int64, error) {
		return p.sqlc.ConsumeUse(context.Background(), sqlc.ConsumeUseParams{
			OrgID:       orgID,
			WindowStart: window.UTC(),
		})
	})
	if err != nil {
		return 0, false, err
	}
	return used, cap <= 0 || used <= cap, nil
}

func (p *Postgres) Usage(orgID string, window time.Time) (int64, error) {
	used, err := p.sqlc.GetUsage(context.Background(), sqlc.GetUsageParams{
		OrgID:       orgID,
		WindowStart: window.UTC(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return used, err
}

func (p *Postgres) UsageReportPending(limit int) ([]UsageReportRow, error) {
	rows, err := p.sqlc.GetUsageReportPending(context.Background(), int64(limit))
	if err != nil {
		return nil, err
	}
	out := make([]UsageReportRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, UsageReportRow{
			OrgID:            r.OrgID,
			WindowStart:      r.WindowStart.UTC(),
			Used:             r.Used,
			Reported:         r.Reported,
			CustomerID:       r.CustomerID,
			BillingAccountID: r.BillingAccountID,
		})
	}
	return out, nil
}

func (p *Postgres) MarkUsageReported(orgID string, window time.Time, amount int64) error {
	return p.sqlc.MarkUsageReported(context.Background(), sqlc.MarkUsageReportedParams{
		OrgID:       orgID,
		WindowStart: window.UTC(),
		Amount:      amount,
	})
}
