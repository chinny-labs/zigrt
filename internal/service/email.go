package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chinny-labs/zigrt/internal/auth"
	"github.com/chinny-labs/zigrt/internal/learning"
	"github.com/chinny-labs/zigrt/internal/store"
)

// Email (plan §2, §7.5, §10): password reset links and the weekly summary.
// Messages go to an outbox in the transaction that decides to send them;
// the sender job delivers them and retries.

// Email kinds.
const (
	EmailReset  = "password_reset"
	EmailWeekly = "weekly"
)

// Email timing.
const (
	ResetLinkLife    = time.Hour
	ResetEvery       = 5 * time.Minute // at most one reset email per parent this often
	MaxEmailAttempts = 6
	EmailKeep        = 30 * 24 * time.Hour // the outbox forgets messages this old
	WeeklyHour       = 18                  // Sunday, household time
)

// retryAfter is the wait before each retry of a failed message.
var retryAfter = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 12 * time.Hour}

// ErrMailOff means no SMTP server is set up.
var ErrMailOff = errors.New("email is not set up on this server")

// MailOn reports whether email can be sent.
func (s *Service) MailOn() bool { return s.Mail != nil && s.BaseURL != "" }

func weeklyOffKey(userID string) string { return "weekly_email_off:" + userID }

// WeeklyEmailOn reports whether a parent gets the weekly summary.
func (s *Service) WeeklyEmailOn(ctx context.Context, parent store.User) (bool, error) {
	v, err := s.Store.GetSetting(ctx, weeklyOffKey(parent.ID))
	if errors.Is(err, store.ErrNotFound) {
		return true, nil
	}
	return v != "1", err
}

// SetWeeklyEmail turns a parent's weekly summary on or off.
func (s *Service) SetWeeklyEmail(ctx context.Context, parent store.User, on bool) error {
	if err := requireParent(parent); err != nil {
		return err
	}
	v := "1"
	if on {
		v = "0"
	}
	return s.Store.Tx(ctx, "", func(tx store.Tx) error {
		if err := tx.PutSetting(ctx, weeklyOffKey(parent.ID), v, false); err != nil {
			return err
		}
		return s.audit(ctx, tx, parent.ID, "user.weekly_email", parent.ID, map[string]any{"on": on})
	})
}

func (s *Service) queue(ctx context.Context, tx store.Tx, kind, to, subject, body, key string) error {
	now := s.now()
	_, err := tx.QueueEmail(ctx, store.Email{ID: store.NewID(), Kind: kind, To: to, Subject: subject, Body: body,
		DedupeKey: key, CreatedAt: now, NextAttemptAt: now})
	return err
}

// --- password reset -------------------------------------------------------------

// RequestPasswordReset emails a parent a one-hour link to set a new
// password. It says nothing about whether the address belongs to a parent,
// and sends at most one link every five minutes.
func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	if !s.MailOn() {
		return ErrMailOff
	}
	email = strings.TrimSpace(email)
	return s.Store.Tx(ctx, "", func(tx store.Tx) error {
		u, err := tx.GetUserByEmail(ctx, email)
		if errors.Is(err, store.ErrNotFound) || (err == nil && !u.IsParent()) {
			return nil
		}
		if err != nil {
			return err
		}
		now := s.now()
		if last, err := tx.LatestPasswordReset(ctx, u.ID); err == nil && now.Sub(last.CreatedAt) < ResetEvery {
			return nil
		} else if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		token := auth.NewToken()
		r := store.PasswordReset{ID: store.NewID(), UserID: u.ID, TokenHash: auth.HashToken(token), CreatedAt: now, ExpiresAt: now.Add(ResetLinkLife)}
		if err := tx.CreatePasswordReset(ctx, r); err != nil {
			return err
		}
		body := "Someone asked to reset the Zigrt password for this address. If it was you, open this link within an hour to choose a new one:\n\n" +
			s.BaseURL + "/parent/reset/" + token + "\n\n" +
			"If it wasn't you, ignore this email: your password stays as it is.\n"
		if err := s.queue(ctx, tx, EmailReset, u.Email, "Reset your Zigrt password", body, "reset:"+r.ID); err != nil {
			return err
		}
		return s.audit(ctx, tx, "", "user.reset_requested", u.ID, nil)
	})
}

var errResetLink = invalid("token", "That link has expired or was already used. Ask for a new one.")

func (s *Service) openReset(ctx context.Context, r store.Reader, token string) (store.PasswordReset, error) {
	p, err := r.GetPasswordReset(ctx, auth.HashToken(token))
	if errors.Is(err, store.ErrNotFound) {
		return p, errResetLink
	}
	if err != nil {
		return p, err
	}
	if p.UsedAt != nil || !s.now().Before(p.ExpiresAt) {
		return p, errResetLink
	}
	return p, nil
}

// CheckResetLink reports whether a reset link can still be used.
func (s *Service) CheckResetLink(ctx context.Context, token string) error {
	_, err := s.openReset(ctx, s.Store, token)
	return err
}

// ResetWithLink sets a new password from an emailed link. Every link the
// parent has is used up and every session ends.
func (s *Service) ResetWithLink(ctx context.Context, token, password string) error {
	hash, err := newPasswordHash(password)
	if err != nil {
		return err
	}
	return s.Store.Tx(ctx, "", func(tx store.Tx) error {
		p, err := s.openReset(ctx, tx, token)
		if err != nil {
			return err
		}
		u, err := tx.GetUser(ctx, p.UserID)
		if err != nil {
			return err
		}
		if err := tx.UsePasswordResets(ctx, u.ID, s.now()); err != nil {
			return err
		}
		return s.setPassword(ctx, tx, u, hash, "", map[string]any{"via": "email"})
	})
}

// --- the weekly summary ------------------------------------------------------------

// WeeklyEmail queues each parent's summary of the week, once a week, on
// Sunday from six in the evening household time. A Sunday evening the
// server was off is skipped, not sent late.
func (s *Service) WeeklyEmail(ctx context.Context) error {
	if !s.MailOn() {
		return nil
	}
	h, err := s.Store.GetHousehold(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	loc := h.Location()
	local := s.now().In(loc)
	if local.Weekday() != time.Sunday || local.Hour() < WeeklyHour {
		return nil
	}
	monday := weekStart(learning.DateOf(s.now(), loc))
	if last, err := s.Store.GetJobRun(ctx, "weekly_email"); err == nil && last == monday.String() {
		return nil
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}

	parents, err := s.Store.ListUsers(ctx, store.RoleParent)
	if err != nil {
		return err
	}
	students, err := s.Students(ctx)
	if err != nil {
		return err
	}
	var body strings.Builder
	body.WriteString("Here is how the week went.\n")
	for _, sv := range students {
		if err := s.weekLines(ctx, &body, sv); err != nil {
			return err
		}
	}
	if len(students) == 0 {
		body.WriteString("\nNo students yet.\n")
	}
	week, _ := time.Parse("2006-01-02", monday.String())
	subject := "Zigrt: the week of " + week.Format("2 January")
	footer := "\nSee each child's week: " + s.BaseURL + "/parent\n\n" +
		"You get this each Sunday evening. To stop, untick \"Weekly email\" on the Household page: " + s.BaseURL + "/parent/household\n"

	return s.Store.Tx(ctx, "", func(tx store.Tx) error {
		for _, p := range parents {
			if on, err := s.WeeklyEmailOn(ctx, p); err != nil {
				return err
			} else if !on || p.Email == "" {
				continue
			}
			if err := s.queue(ctx, tx, EmailWeekly, p.Email, subject, body.String()+footer, "weekly:"+p.ID+":"+monday.String()); err != nil {
				return err
			}
		}
		return tx.PutJobRun(ctx, "weekly_email", monday.String())
	})
}

func (s *Service) weekLines(ctx context.Context, b *strings.Builder, sv StudentView) error {
	c, err := s.studentCard(ctx, sv)
	if err != nil {
		return err
	}
	met := 0
	for _, d := range c.Week {
		if d.Met {
			met++
		}
	}
	fmt.Fprintf(b, "\n%s\n", sv.DisplayName)
	fmt.Fprintf(b, "  %d XP of a %d XP goal for the week; the daily goal was met on %s.\n", c.WeekXP, c.WeekGoal, plural(met, "day", "days"))
	if c.Today.Streak > 1 {
		fmt.Fprintf(b, "  Streak: %d days.\n", c.Today.Streak)
	}
	fmt.Fprintf(b, "  %s placed this week, %d in all.\n", plural(c.NewBlocks, "new block", "new blocks"), c.Placed)
	if c.Stuck != nil {
		fmt.Fprintf(b, "  Stuck on %s: the lesson stopped twice, so earlier blocks are being mended first.\n", c.Stuck.Title.In(Locale))
	}
	hard, err := s.struggles(ctx, sv.ID, 7)
	if err != nil && !errors.Is(err, ErrNoContent) {
		return err
	}
	var names []string
	for _, h := range hard {
		if c.Stuck == nil || h.Topic.ID != c.Stuck.ID {
			names = append(names, h.Topic.Title.In(Locale))
		}
	}
	if len(names) > 0 {
		fmt.Fprintf(b, "  Finding hard: %s.\n", strings.Join(names, ", "))
	}
	if c.LastActive == nil || s.now().Sub(*c.LastActive) > 7*24*time.Hour {
		b.WriteString("  No practice this week.\n")
	}
	return nil
}

// --- sending -----------------------------------------------------------------------

// SendOutbox delivers the messages that are due and reports how many went
// and how many failed this time. A failure is retried later with backoff,
// up to MaxEmailAttempts tries.
func (s *Service) SendOutbox(ctx context.Context) (sent, failed int, err error) {
	if s.Mail == nil {
		return 0, 0, nil
	}
	due, err := s.Store.DueEmails(ctx, s.now(), MaxEmailAttempts, 20)
	if err != nil {
		return 0, 0, err
	}
	for _, e := range due {
		if ctx.Err() != nil {
			return sent, failed, ctx.Err()
		}
		sendErr := s.Mail.Send(ctx, e.To, e.Subject, e.Body)
		e.Attempts++
		now := s.now()
		if sendErr == nil {
			e.SentAt, e.LastError = &now, ""
			sent++
		} else {
			e.LastError = sendErr.Error()
			e.NextAttemptAt = now.Add(retryAfter[min(e.Attempts-1, len(retryAfter)-1)])
			failed++
			level := "will retry"
			if e.Attempts >= MaxEmailAttempts {
				level = "giving up"
			}
			s.Log.Warn("email failed", "kind", e.Kind, "id", e.ID, "attempt", e.Attempts, "outcome", level, "err", sendErr)
		}
		if err := s.Store.Tx(ctx, "", func(tx store.Tx) error { return tx.UpdateEmail(ctx, e) }); err != nil {
			return sent, failed, err
		}
	}
	return sent, failed, nil
}

// PruneOutbox forgets messages older than EmailKeep.
func (s *Service) PruneOutbox(ctx context.Context) (int64, error) {
	var n int64
	err := s.Store.Tx(ctx, "", func(tx store.Tx) (err error) {
		n, err = tx.PruneEmails(ctx, s.now().Add(-EmailKeep))
		return err
	})
	return n, err
}
