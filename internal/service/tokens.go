package service

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/chinny-labs/zigrt/internal/auth"
	"github.com/chinny-labs/zigrt/internal/store"
)

// API token scopes (plan §11). Every token can read; manage also changes a
// student's settings and makes worksheets. Nothing answers problems: the
// learning history stays the student's own work.
const (
	ScopeRead   = "read"
	ScopeManage = "manage"
)

// TokenPrefix marks a Zigrt API token, so a leaked one is easy to spot and
// secret scanners can match it.
const TokenPrefix = "zg_"

// MaxTokensPerParent caps live tokens per parent.
const MaxTokensPerParent = 20

// tokenTouchEvery is how often a token's last-used time is written.
const tokenTouchEvery = time.Minute

// TokenInput is a new token's label and scopes.
type TokenInput struct {
	Label  string
	Scopes []string
}

// HasScope reports whether a token grants a scope.
func HasScope(t store.APIToken, scope string) bool { return slices.Contains(t.Scopes, scope) }

func cleanScopes(in []string) ([]string, error) {
	got := map[string]bool{ScopeRead: true}
	for _, sc := range in {
		switch sc = strings.TrimSpace(sc); sc {
		case ScopeRead, ScopeManage:
			got[sc] = true
		case "":
		default:
			return nil, invalid("scopes", "Unknown scope %q.", sc)
		}
	}
	var out []string
	for _, sc := range []string{ScopeRead, ScopeManage} {
		if got[sc] {
			out = append(out, sc)
		}
	}
	return out, nil
}

// CreateAPIToken mints a token owned by a parent and returns its secret,
// shown once and never stored.
func (s *Service) CreateAPIToken(ctx context.Context, parent store.User, in TokenInput) (string, store.APIToken, error) {
	if err := requireParent(parent); err != nil {
		return "", store.APIToken{}, err
	}
	label := strings.TrimSpace(in.Label)
	if label == "" || len(label) > 60 {
		return "", store.APIToken{}, invalid("label", "Give the token a name of up to 60 characters, like \"Home Assistant\".")
	}
	scopes, err := cleanScopes(in.Scopes)
	if err != nil {
		return "", store.APIToken{}, err
	}
	secret := TokenPrefix + auth.NewToken()
	t := store.APIToken{ID: store.NewID(), UserID: parent.ID, Label: label, TokenHash: auth.HashToken(secret), Scopes: scopes, CreatedAt: s.now()}
	err = s.Store.Tx(ctx, "", func(tx store.Tx) error {
		live, err := tx.ListAPITokens(ctx, parent.ID)
		if err != nil {
			return err
		}
		if len(live) >= MaxTokensPerParent {
			return invalid("label", "You already have %d tokens. Revoke one first.", MaxTokensPerParent)
		}
		if err := tx.CreateAPIToken(ctx, t); err != nil {
			return err
		}
		return s.audit(ctx, tx, parent.ID, "token.create", parent.ID, map[string]any{"token": t.ID, "label": label, "scopes": scopes})
	})
	if err != nil {
		return "", store.APIToken{}, err
	}
	return secret, t, nil
}

// APITokens lists a parent's live tokens.
func (s *Service) APITokens(ctx context.Context, parent store.User) ([]store.APIToken, error) {
	if err := requireParent(parent); err != nil {
		return nil, err
	}
	return s.Store.ListAPITokens(ctx, parent.ID)
}

// RevokeAPIToken revokes one of a parent's own tokens.
func (s *Service) RevokeAPIToken(ctx context.Context, parent store.User, id string) error {
	if err := requireParent(parent); err != nil {
		return err
	}
	return s.Store.Tx(ctx, "", func(tx store.Tx) error {
		t, err := tx.GetAPIToken(ctx, id)
		if err != nil {
			return err
		}
		if t.UserID != parent.ID {
			return ErrNotFound
		}
		if t.RevokedAt != nil {
			return nil
		}
		if err := tx.RevokeAPIToken(ctx, t.ID, s.now()); err != nil {
			return err
		}
		return s.audit(ctx, tx, parent.ID, "token.revoke", parent.ID, map[string]any{"token": t.ID, "label": t.Label})
	})
}

// AuthenticateToken resolves a bearer token to the parent who minted it.
// Unknown and revoked tokens look the same: ErrNotFound.
func (s *Service) AuthenticateToken(ctx context.Context, secret string) (store.User, store.APIToken, error) {
	if !strings.HasPrefix(secret, TokenPrefix) {
		return store.User{}, store.APIToken{}, ErrNotFound
	}
	t, err := s.Store.GetAPITokenByHash(ctx, auth.HashToken(secret))
	if err != nil {
		return store.User{}, store.APIToken{}, err
	}
	if t.RevokedAt != nil {
		return store.User{}, store.APIToken{}, ErrNotFound
	}
	u, err := s.Store.GetUser(ctx, t.UserID)
	if err != nil || !u.IsParent() {
		return store.User{}, store.APIToken{}, ErrNotFound
	}
	now := s.now()
	if t.LastUsedAt == nil || now.Sub(*t.LastUsedAt) >= tokenTouchEvery {
		t.LastUsedAt = &now
		if err := s.Store.Tx(ctx, "", func(tx store.Tx) error { return tx.TouchAPIToken(ctx, t.ID, now) }); err != nil {
			s.Log.Warn("touch api token", "err", err)
		}
	}
	return u, t, nil
}

type tokenKey struct{}

// WithToken marks ctx as a call made with a token, so the audit rows it
// writes name the token (plan §11).
func WithToken(ctx context.Context, tokenID string) context.Context {
	return context.WithValue(ctx, tokenKey{}, tokenID)
}

func tokenFrom(ctx context.Context) string {
	id, _ := ctx.Value(tokenKey{}).(string)
	return id
}
