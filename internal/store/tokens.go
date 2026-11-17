package store

import (
	"context"
	"strings"
	"time"
)

// APIToken is a bearer token a parent minted for the API and MCP (plan
// §11). Only its hash is stored.
type APIToken struct {
	ID         string
	UserID     string
	Label      string
	TokenHash  string
	Scopes     []string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

const tokenCols = `id, user_id, label, token_hash, scopes, created_at, last_used_at, revoked_at` //nolint:gosec // column names, not a credential

func scanToken(r interface{ Scan(...any) error }) (APIToken, error) {
	var t APIToken
	var scopes string
	err := r.Scan(&t.ID, &t.UserID, &t.Label, &t.TokenHash, &scopes, rtime{&t.CreatedAt}, ntime{&t.LastUsedAt}, ntime{&t.RevokedAt})
	if scopes != "" {
		t.Scopes = strings.Split(scopes, ",")
	}
	return t, err
}

func (x queries) CreateAPIToken(ctx context.Context, t APIToken) error {
	_, err := x.exec(ctx, `INSERT INTO api_tokens (`+tokenCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.UserID, t.Label, t.TokenHash, strings.Join(t.Scopes, ","), x.t(t.CreatedAt), x.tp(t.LastUsedAt), x.tp(t.RevokedAt))
	return err
}

func (x queries) GetAPIToken(ctx context.Context, id string) (APIToken, error) {
	t, err := scanToken(x.row(ctx, `SELECT `+tokenCols+` FROM api_tokens WHERE id = ?`, id))
	return t, notFound(err)
}

func (x queries) GetAPITokenByHash(ctx context.Context, hash string) (APIToken, error) {
	t, err := scanToken(x.row(ctx, `SELECT `+tokenCols+` FROM api_tokens WHERE token_hash = ?`, hash))
	return t, notFound(err)
}

func (x queries) ListAPITokens(ctx context.Context, userID string) ([]APIToken, error) {
	rows, err := x.rows(ctx, `SELECT `+tokenCols+` FROM api_tokens WHERE user_id = ? AND revoked_at IS NULL ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIToken
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (x queries) RevokeAPIToken(ctx context.Context, id string, at time.Time) error {
	_, err := x.exec(ctx, `UPDATE api_tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, x.t(at), id)
	return err
}

func (x queries) TouchAPIToken(ctx context.Context, id string, at time.Time) error {
	_, err := x.exec(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, x.t(at), id)
	return err
}
