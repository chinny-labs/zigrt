package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chinny-labs/zigrt/internal/service"
	"github.com/chinny-labs/zigrt/internal/store"
)

// testAPITokens covers a token's life on each engine: minted, found by its
// hash, touched, revoked, and gone with its parent.
func testAPITokens(t *testing.T, open Opener) {
	ctx := context.Background()
	f := NewFixture(t, open)
	svc := f.Svc

	secret, tk, err := svc.CreateAPIToken(ctx, f.Sam, service.TokenInput{Label: "Home Assistant", Scopes: []string{service.ScopeManage}})
	must(t, err)
	_, other, err := svc.CreateAPIToken(ctx, f.Alex, service.TokenInput{Label: "Alex's"})
	must(t, err)

	u, got, err := svc.AuthenticateToken(ctx, secret)
	must(t, err)
	if u.ID != f.Sam.ID || got.ID != tk.ID || len(got.Scopes) != 2 || !got.CreatedAt.Equal(tk.CreatedAt) || got.LastUsedAt == nil {
		t.Fatalf("authenticated as %s with %+v", u.ID, got)
	}
	// Touched at most once a minute.
	first := *got.LastUsedAt
	f.Clock.Advance(30 * time.Second)
	_, _, _ = svc.AuthenticateToken(ctx, secret)
	again, _ := f.Store.GetAPIToken(ctx, tk.ID)
	if !again.LastUsedAt.Equal(first) {
		t.Fatalf("touched again within a minute: %v then %v", first, again.LastUsedAt)
	}

	list, err := svc.APITokens(ctx, f.Sam)
	must(t, err)
	if len(list) != 1 || list[0].ID != tk.ID {
		t.Fatalf("Sam's tokens: %+v", list)
	}
	if err := svc.RevokeAPIToken(ctx, f.Sam, other.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Sam revoking Alex's: %v", err)
	}
	must(t, svc.RevokeAPIToken(ctx, f.Sam, tk.ID))
	if _, _, err := svc.AuthenticateToken(ctx, secret); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revoked token: %v", err)
	}
	if list, _ := svc.APITokens(ctx, f.Sam); len(list) != 0 {
		t.Fatalf("revoked token still listed: %+v", list)
	}
	if r, _ := f.Store.GetAPIToken(ctx, tk.ID); r.RevokedAt == nil {
		t.Fatal("no revoked time")
	}

	// Deleting a parent takes their tokens.
	must(t, f.Store.Tx(ctx, "", func(tx store.Tx) error { return tx.DeleteUser(ctx, f.Alex.ID) }))
	if _, err := f.Store.GetAPIToken(ctx, other.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Alex's token after deleting Alex: %v", err)
	}
}
