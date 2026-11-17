package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/chinny-labs/zigrt/internal/auth"
	"github.com/chinny-labs/zigrt/internal/store"
)

// Session lifetimes (plan §2). Every request slides the expiry forward.
const (
	ParentSession  = 7 * 24 * time.Hour
	StudentSession = 30 * 24 * time.Hour
	failureWindow  = 24 * time.Hour
	touchEvery     = time.Minute
)

// SetupInput is the first-run form.
type SetupInput struct {
	Timezone    string
	DisplayName string
	Email       string
	Password    string
}

// Setup creates the household and its first parent. It fails with
// ErrAlreadySetUp once any parent exists.
func (s *Service) Setup(ctx context.Context, in SetupInput) (store.User, error) {
	tz, err := cleanTimezone(in.Timezone)
	if err != nil {
		return store.User{}, err
	}
	parent, err := s.newParent(in.DisplayName, in.Email, in.Password)
	if err != nil {
		return store.User{}, err
	}
	err = s.Store.Tx(ctx, "", func(tx store.Tx) error {
		if n, err := tx.CountParents(ctx); err != nil {
			return err
		} else if n > 0 {
			return ErrAlreadySetUp
		}
		if _, err := tx.GetHousehold(ctx); errors.Is(err, store.ErrNotFound) {
			if err := tx.CreateHousehold(ctx, store.Household{
				ID: store.NewID(), Name: "Household", Timezone: tz, CreatedAt: s.now(),
			}); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if err := tx.CreateUser(ctx, parent); err != nil {
			return emailTaken(err)
		}
		return s.audit(ctx, tx, parent.ID, "setup", parent.ID, nil)
	})
	return parent, err
}

func emailTaken(err error) error {
	if errors.Is(err, store.ErrConflict) {
		return invalid("email", "A parent already uses that email.")
	}
	return err
}

func (s *Service) newParent(displayName, email, password string) (store.User, error) {
	dn, err := cleanName(displayName)
	if err != nil {
		return store.User{}, err
	}
	em, err := cleanEmail(email)
	if err != nil {
		return store.User{}, err
	}
	if err := auth.CheckPassword(password); err != nil {
		return store.User{}, invalid("password", "%s.", capitalize(err.Error()))
	}
	hash, err := auth.Hash(password)
	if err != nil {
		return store.User{}, err
	}
	return store.User{ID: store.NewID(), Role: store.RoleParent, DisplayName: dn, Email: em,
		PasswordHash: hash, CreatedAt: s.now()}, nil
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// ParentInput is the "add another parent" form.
type ParentInput struct{ DisplayName, Email, Password string }

// CreateParent adds a second parent.
func (s *Service) CreateParent(ctx context.Context, actor store.User, in ParentInput) (store.User, error) {
	if err := requireParent(actor); err != nil {
		return store.User{}, err
	}
	u, err := s.newParent(in.DisplayName, in.Email, in.Password)
	if err != nil {
		return u, err
	}
	err = s.Store.Tx(ctx, "", func(tx store.Tx) error {
		if err := tx.CreateUser(ctx, u); err != nil {
			return emailTaken(err)
		}
		return s.audit(ctx, tx, actor.ID, "user.create", u.ID, map[string]any{"role": "parent"})
	})
	return u, err
}

// StudentInput is the "add a student" form.
type StudentInput struct{ DisplayName, Avatar, PIN string }

// CreateStudent adds a student with default settings.
func (s *Service) CreateStudent(ctx context.Context, actor store.User, in StudentInput) (store.User, error) {
	if err := requireParent(actor); err != nil {
		return store.User{}, err
	}
	dn, err := cleanName(in.DisplayName)
	if err != nil {
		return store.User{}, err
	}
	if err := auth.CheckPIN(in.PIN); err != nil {
		return store.User{}, invalid("pin", "%s.", capitalize(err.Error()))
	}
	hash, err := auth.Hash(in.PIN)
	if err != nil {
		return store.User{}, err
	}
	st := store.User{ID: store.NewID(), Role: store.RoleStudent, DisplayName: dn, Avatar: cleanAvatar(in.Avatar),
		PINHash: hash, CreatedAt: s.now()}
	err = s.Store.Tx(ctx, "", func(tx store.Tx) error {
		if st.Avatar == "" { // give each new student the next tone in turn
			others, err := tx.ListUsers(ctx, store.RoleStudent)
			if err != nil {
				return err
			}
			st.Avatar = Avatars[len(others)%len(Avatars)]
		}
		if err := tx.CreateUser(ctx, st); err != nil {
			return err
		}
		if err := tx.PutStudentSettings(ctx, store.StudentSettings{
			UserID: st.ID, DailyXPGoal: store.DefaultDailyXPGoal, FluencyEnabled: true,
		}); err != nil {
			return err
		}
		return s.audit(ctx, tx, actor.ID, "user.create", st.ID, map[string]any{"role": "student"})
	})
	return st, err
}

// ResetPIN sets a student's PIN and clears any lock.
func (s *Service) ResetPIN(ctx context.Context, actor store.User, studentID, pin string) error {
	if err := requireParent(actor); err != nil {
		return err
	}
	if err := auth.CheckPIN(pin); err != nil {
		return invalid("pin", "%s.", capitalize(err.Error()))
	}
	hash, err := auth.Hash(pin)
	if err != nil {
		return err
	}
	return s.Store.Tx(ctx, "", func(tx store.Tx) error {
		u, err := getStudent(ctx, tx, studentID)
		if err != nil {
			return err
		}
		u.PINHash = hash
		if err := tx.UpdateUser(ctx, u); err != nil {
			return err
		}
		if err := tx.ClearAuthAttempts(ctx, u.ID); err != nil {
			return err
		}
		if err := tx.RevokeUserSessions(ctx, u.ID, s.now()); err != nil {
			return err
		}
		return s.audit(ctx, tx, actor.ID, "user.reset_pin", u.ID, nil)
	})
}

// ResetPassword sets a parent's password. A nil actor is the command line,
// for a parent locked out with no email configured (plan §2).
func (s *Service) ResetPassword(ctx context.Context, actor *store.User, email, password string) error {
	actorID := ""
	if actor != nil {
		if err := requireParent(*actor); err != nil {
			return err
		}
		actorID = actor.ID
	}
	if err := auth.CheckPassword(password); err != nil {
		return invalid("password", "%s.", capitalize(err.Error()))
	}
	hash, err := auth.Hash(password)
	if err != nil {
		return err
	}
	return s.Store.Tx(ctx, "", func(tx store.Tx) error {
		u, err := tx.GetUserByEmail(ctx, email)
		if err != nil {
			return err
		}
		u.PasswordHash = hash
		if err := tx.UpdateUser(ctx, u); err != nil {
			return err
		}
		if err := tx.ClearAuthAttempts(ctx, u.ID); err != nil {
			return err
		}
		if err := tx.RevokeUserSessions(ctx, u.ID, s.now()); err != nil {
			return err
		}
		return s.audit(ctx, tx, actorID, "user.reset_password", u.ID, nil)
	})
}

// Unlock clears a student's failed-PIN lock.
func (s *Service) Unlock(ctx context.Context, actor store.User, studentID string) error {
	if err := requireParent(actor); err != nil {
		return err
	}
	return s.Store.Tx(ctx, "", func(tx store.Tx) error {
		if _, err := getStudent(ctx, tx, studentID); err != nil {
			return err
		}
		if err := tx.ClearAuthAttempts(ctx, studentID); err != nil {
			return err
		}
		return s.audit(ctx, tx, actor.ID, "user.unlock", studentID, nil)
	})
}

// --- sign-in ----------------------------------------------------------------

// LoginResult is a fresh session. Token goes in the cookie; only its hash is
// stored.
type LoginResult struct {
	Token   string
	User    store.User
	Session store.Session
}

// LoginParent signs a parent in with email and password.
func (s *Service) LoginParent(ctx context.Context, email, password, device string) (LoginResult, error) {
	u, err := s.Store.GetUserByEmail(ctx, email)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !u.IsParent()) {
		auth.BurnTime(password) // unknown emails cost the same time as real ones
		return LoginResult{}, ErrBadCredentials
	}
	if err != nil {
		return LoginResult{}, err
	}
	return s.login(ctx, u, password, u.PasswordHash, ErrBadCredentials, device)
}

// LoginStudent signs a student in with their PIN. The student is chosen from
// the picker, so the ID is not a secret.
func (s *Service) LoginStudent(ctx context.Context, studentID, pin, device string) (LoginResult, error) {
	u, err := getStudent(ctx, s.Store, studentID)
	if err != nil {
		return LoginResult{}, err
	}
	return s.login(ctx, u, pin, u.PINHash, ErrBadPIN, device)
}

func (s *Service) login(ctx context.Context, u store.User, secret, hash string, bad error, device string) (LoginResult, error) {
	now := s.now()
	a, err := s.Store.GetAuthAttempts(ctx, u.ID)
	if err != nil {
		return LoginResult{}, err
	}
	if th := auth.Check(a.Failures, a.NextAllowedAt, a.LockedAt, now); th.Locked {
		return LoginResult{}, ErrLocked
	} else if th.Wait > 0 {
		return LoginResult{}, &ThrottledError{Wait: th.Wait}
	}
	if !auth.Verify(secret, hash) {
		return LoginResult{}, s.recordFailure(ctx, u, bad)
	}
	token := auth.NewToken()
	sess := store.Session{IDHash: auth.HashToken(token), UserID: u.ID, DeviceLabel: device,
		CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(sessionLife(u))}
	err = s.Store.Tx(ctx, "", func(tx store.Tx) error {
		if a.Failures > 0 {
			if err := tx.ClearAuthAttempts(ctx, u.ID); err != nil {
				return err
			}
		}
		return tx.CreateSession(ctx, sess)
	})
	return LoginResult{Token: token, User: u, Session: sess}, err
}

func sessionLife(u store.User) time.Duration {
	if u.IsParent() {
		return ParentSession
	}
	return StudentSession
}

// recordFailure counts a wrong secret, and returns the error to show: bad,
// a wait, or a lock.
func (s *Service) recordFailure(ctx context.Context, u store.User, bad error) error {
	result := bad
	err := s.Store.Tx(ctx, u.ID, func(tx store.Tx) error {
		now := s.now()
		a, err := tx.GetAuthAttempts(ctx, u.ID)
		if err != nil {
			return err
		}
		if a.WindowStart == nil || now.Sub(*a.WindowStart) > failureWindow {
			a = store.AuthAttempts{UserID: u.ID, WindowStart: &now}
		}
		a.Failures++
		if d := auth.Delay(a.Failures); d > 0 {
			next := now.Add(d)
			a.NextAllowedAt = &next
			result = &ThrottledError{Wait: d}
		}
		locked := auth.ShouldLock(u.IsStudent(), a.Failures)
		if locked {
			a.LockedAt, result = &now, ErrLocked
			if err := tx.RevokeUserSessions(ctx, u.ID, now); err != nil {
				return err
			}
		}
		if err := tx.PutAuthAttempts(ctx, a); err != nil {
			return err
		}
		if locked {
			if err := s.notify(ctx, tx, NoteLocked, u.ID, u.ID+":"+now.Format(time.RFC3339Nano), map[string]any{"failures": a.Failures}, now); err != nil {
				return err
			}
			return s.audit(ctx, tx, "", "user.locked", u.ID, map[string]any{"failures": a.Failures})
		}
		return nil
	})
	if err != nil {
		return err
	}
	return result
}

// Authenticate resolves a session cookie to its user, sliding the expiry.
func (s *Service) Authenticate(ctx context.Context, token string) (store.User, store.Session, error) {
	if token == "" {
		return store.User{}, store.Session{}, ErrNotFound
	}
	now := s.now()
	sess, err := s.Store.GetSession(ctx, auth.HashToken(token))
	if err != nil {
		return store.User{}, store.Session{}, err
	}
	if sess.RevokedAt != nil || !now.Before(sess.ExpiresAt) {
		return store.User{}, store.Session{}, ErrNotFound
	}
	u, err := s.Store.GetUser(ctx, sess.UserID)
	if err != nil {
		return store.User{}, store.Session{}, ErrNotFound
	}
	if now.Sub(sess.LastSeenAt) >= touchEvery {
		sess.LastSeenAt, sess.ExpiresAt = now, now.Add(sessionLife(u))
		if err := s.Store.Tx(ctx, "", func(tx store.Tx) error {
			return tx.TouchSession(ctx, sess.IDHash, sess.LastSeenAt, sess.ExpiresAt)
		}); err != nil {
			s.Log.Warn("touch session", "err", err)
		}
	}
	return u, sess, nil
}

// Logout ends one session.
func (s *Service) Logout(ctx context.Context, token string) error {
	return s.Store.Tx(ctx, "", func(tx store.Tx) error {
		return tx.RevokeSession(ctx, auth.HashToken(token), s.now())
	})
}

// SweepSessions deletes sessions dead for more than a day (the hourly
// housekeeping job, plan §7.5).
func (s *Service) SweepSessions(ctx context.Context) (int64, error) {
	var n int64
	err := s.Store.Tx(ctx, "", func(tx store.Tx) error {
		var err error
		n, err = tx.DeleteDeadSessions(ctx, s.now().Add(-24*time.Hour))
		return err
	})
	return n, err
}

// --- views ------------------------------------------------------------------

// StudentView is a student as a parent's screens show them.
type StudentView struct {
	store.User
	Settings store.StudentSettings
	Locked   bool
}

// Students lists every student with their settings and lock state.
func (s *Service) Students(ctx context.Context) ([]StudentView, error) {
	users, err := s.Store.ListUsers(ctx, store.RoleStudent)
	if err != nil {
		return nil, err
	}
	out := make([]StudentView, 0, len(users))
	for _, u := range users {
		v, err := s.studentView(ctx, u)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// Student returns one student for a parent's screens.
func (s *Service) Student(ctx context.Context, id string) (StudentView, error) {
	u, err := getStudent(ctx, s.Store, id)
	if err != nil {
		return StudentView{}, err
	}
	return s.studentView(ctx, u)
}

func (s *Service) studentView(ctx context.Context, u store.User) (StudentView, error) {
	v := StudentView{User: u}
	a, err := s.Store.GetAuthAttempts(ctx, u.ID)
	if err != nil {
		return v, err
	}
	v.Locked = a.LockedAt != nil
	v.Settings, err = s.Store.GetStudentSettings(ctx, u.ID)
	if errors.Is(err, store.ErrNotFound) {
		v.Settings, err = store.StudentSettings{UserID: u.ID, DailyXPGoal: store.DefaultDailyXPGoal, FluencyEnabled: true}, nil
	}
	return v, err
}
