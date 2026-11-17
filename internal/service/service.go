// Package service holds every rule (plan §7.3). Handlers parse input, call
// one method here and render; this package opens the transaction, reads and
// writes through the store, and decides what is allowed.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/chinny-labs/zigrt/internal/learning"
	"github.com/chinny-labs/zigrt/internal/store"
)

// Service is the application: rules over a store and a clock.
type Service struct {
	Store store.Store
	Now   func() time.Time
	Log   *slog.Logger
	// Seed supplies the seed for a new problem set; nil means random.
	Seed func() uint64
	// Learn is the mastery tuning (plan section 3).
	Learn learning.Config

	// Set by the server at start: the app's version (for a pack's
	// min_app_version), the content folder uploads go to, where the running
	// pack came from, and the database's own backups (nil for Postgres).
	AppVersion string
	ContentDir string
	PackSource string
	Backups    Backups

	lib atomic.Pointer[library]
}

// New wires a service to a store. A nil logger discards.
func New(st store.Store, log *slog.Logger) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{Store: st, Now: time.Now, Log: log, Learn: learning.Default()}
}

func (s *Service) now() time.Time { return s.Now().UTC() }

var (
	ErrNotFound       = store.ErrNotFound
	ErrForbidden      = errors.New("you can't do that")
	ErrAlreadySetUp   = errors.New("this Zigrt is already set up")
	ErrBadCredentials = errors.New("that email and password don't match")
	ErrBadPIN         = errors.New("that PIN isn't right")
	ErrLocked         = errors.New("too many wrong tries, so this is locked. Ask a parent to unlock it")
)

// UserError is a validation failure the UI shows next to a field.
type UserError struct {
	Field string
	Msg   string
}

func (e *UserError) Error() string { return e.Msg }

func invalid(field, format string, args ...any) error {
	return &UserError{Field: field, Msg: fmt.Sprintf(format, args...)}
}

// ThrottledError means a sign-in must wait before another try.
type ThrottledError struct{ Wait time.Duration }

func (e *ThrottledError) Error() string {
	secs := int(e.Wait.Round(time.Second) / time.Second)
	if secs < 1 {
		secs = 1
	}
	if secs < 120 {
		return fmt.Sprintf("Too many wrong tries. Wait %d seconds and try again.", secs)
	}
	return fmt.Sprintf("Too many wrong tries. Wait %d minutes and try again.", (secs+59)/60)
}

// NeedsSetup reports whether first-run setup still has to happen.
func (s *Service) NeedsSetup(ctx context.Context) (bool, error) {
	n, err := s.Store.CountParents(ctx)
	return n == 0, err
}

// MaxNameLen bounds display names.
const MaxNameLen = 40

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", invalid("display_name", "Enter a name.")
	}
	if utf8.RuneCountInString(name) > MaxNameLen {
		return "", invalid("display_name", "Keep names under %d characters.", MaxNameLen)
	}
	return name, nil
}

func cleanEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	at := strings.IndexByte(email, '@')
	if at < 1 || at == len(email)-1 || strings.ContainsAny(email, " \t\r\n") || len(email) > 254 {
		return "", invalid("email", "That doesn't look like an email address.")
	}
	return email, nil
}

func cleanTimezone(tz string) (string, error) {
	tz = strings.TrimSpace(tz)
	if _, err := time.LoadLocation(tz); err != nil || tz == "" {
		return "", invalid("timezone", "Use a time zone name like America/New_York.")
	}
	return tz, nil
}

// Avatars are the tone names a student tile can have; the UI maps each to a
// colour pair.
var Avatars = []string{"sun", "blue", "sky"}

func cleanAvatar(a string) string {
	for _, ok := range Avatars {
		if a == ok {
			return a
		}
	}
	return ""
}

func (s *Service) audit(ctx context.Context, tx store.Tx, actorID, action, targetID string, detail map[string]any) error {
	var raw json.RawMessage
	if len(detail) > 0 {
		b, err := json.Marshal(detail)
		if err != nil {
			return err
		}
		raw = b
	}
	return tx.InsertAudit(ctx, store.AuditEntry{
		ID: store.NewID(), ActorID: actorID, Action: action, TargetID: targetID, Detail: raw, CreatedAt: s.now(),
	})
}

func requireParent(u store.User) error {
	if !u.IsParent() {
		return ErrForbidden
	}
	return nil
}

func getStudent(ctx context.Context, r store.Reader, id string) (store.User, error) {
	u, err := r.GetUser(ctx, id)
	if err != nil {
		return u, err
	}
	if !u.IsStudent() {
		return u, ErrNotFound
	}
	return u, nil
}
