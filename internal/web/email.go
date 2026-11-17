package web

import (
	"errors"
	"net/http"

	"github.com/chinny-labs/zigrt/internal/service"
)

// Password reset by email (plan §2) and the weekly email switch.

type forgotData struct {
	Mail bool
	Sent bool
}

func (s *Server) forgotPage(w http.ResponseWriter, r *http.Request) {
	v := s.newView(w, r, "Forgot your password?", forgotData{Mail: s.svc.MailOn()})
	v.Back, v.BackLabel = "/parent/login", "Sign in"
	s.render(w, r, http.StatusOK, "forgot", v)
}

func (s *Server) forgotSubmit(w http.ResponseWriter, r *http.Request) {
	err := s.svc.RequestPasswordReset(r.Context(), r.PostFormValue("email"))
	if err != nil && !errors.Is(err, service.ErrMailOff) {
		s.serverError(w, r, err)
		return
	}
	// The same answer whether or not the address is a parent's.
	v := s.newView(w, r, "Check your email", forgotData{Mail: err == nil, Sent: err == nil})
	v.Back, v.BackLabel = "/parent/login", "Sign in"
	s.render(w, r, http.StatusOK, "forgot", v)
}

type resetData struct {
	Token string
	OK    bool // the link can still be used
}

func (s *Server) resetPage(w http.ResponseWriter, r *http.Request) {
	tok := r.PathValue("token")
	err := s.svc.CheckResetLink(r.Context(), tok)
	if err != nil && userMessage(err) == "" {
		s.serverError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	v := s.newView(w, r, "Choose a new password", resetData{Token: tok, OK: err == nil})
	v.Back, v.BackLabel = "/parent/login", "Sign in"
	s.render(w, r, http.StatusOK, "reset", v)
}

func (s *Server) resetSubmit(w http.ResponseWriter, r *http.Request) {
	tok := r.PathValue("token")
	if err := s.svc.ResetWithLink(r.Context(), tok, r.PostFormValue("password")); err != nil {
		msg := userMessage(err)
		if msg == "" {
			s.serverError(w, r, err)
			return
		}
		v := s.newView(w, r, "Choose a new password", resetData{Token: tok, OK: s.svc.CheckResetLink(r.Context(), tok) == nil})
		v.Back, v.BackLabel, v.Err = "/parent/login", "Sign in", msg
		s.render(w, r, http.StatusUnprocessableEntity, "reset", v)
		return
	}
	s.flash(w, "ok", "Your password is changed. Sign in with the new one.")
	http.Redirect(w, r, "/parent/login", http.StatusSeeOther)
}

func (s *Server) weeklyEmailSave(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	on := r.PostFormValue("weekly") != ""
	if err := s.svc.SetWeeklyEmail(r.Context(), *c.User, on); err != nil {
		s.fail(w, r, "/parent/household#email-h", err)
		return
	}
	msg := "You won't get the weekly email."
	if on {
		msg = "You'll get the weekly email on Sunday evenings."
	}
	s.flash(w, "ok", msg)
	http.Redirect(w, r, "/parent/household#email-h", http.StatusSeeOther)
}
