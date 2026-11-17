package web

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/chinny-labs/zigrt/internal/auth"
	"github.com/chinny-labs/zigrt/internal/service"
	"github.com/chinny-labs/zigrt/internal/store"
)

// --- first-run setup (P0) ---------------------------------------------------

func (s *Server) setupPage(w http.ResponseWriter, r *http.Request) {
	if s.setUp.Load() {
		http.Redirect(w, r, "/parent/login", http.StatusSeeOther)
		return
	}
	v := s.newView(w, r, "Set up Zigrt", nil)
	v.Form.Set("timezone", s.cfg.Timezone)
	s.render(w, r, http.StatusOK, "setup", v)
}

func (s *Server) setupSubmit(w http.ResponseWriter, r *http.Request) {
	if s.setUp.Load() {
		http.Redirect(w, r, "/parent/login", http.StatusSeeOther)
		return
	}
	f := r.PostForm
	fail := func(msg string) {
		v := s.newView(w, r, "Set up Zigrt", nil)
		v.Form, v.Err = f, msg
		v.Form.Del("password")
		v.Form.Del("setup_token")
		s.render(w, r, http.StatusUnprocessableEntity, "setup", v)
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(f.Get("setup_token"))), []byte(s.setupToken)) != 1 {
		fail("That setup token doesn't match. Copy it from the server log.")
		return
	}
	ctx := r.Context()
	_, err := s.svc.Setup(ctx, service.SetupInput{
		Timezone: f.Get("timezone"), DisplayName: f.Get("display_name"), Email: f.Get("email"), Password: f.Get("password"),
	})
	if errors.Is(err, service.ErrAlreadySetUp) {
		s.setUp.Store(true)
		http.Redirect(w, r, "/parent/login", http.StatusSeeOther)
		return
	}
	if err != nil {
		if msg := userMessage(err); msg != "" {
			fail(msg)
			return
		}
		s.serverError(w, r, err)
		return
	}
	s.setUp.Store(true)
	s.log.Info("setup complete")
	res, err := s.svc.LoginParent(ctx, f.Get("email"), f.Get("password"), deviceLabel(r))
	if err != nil {
		http.Redirect(w, r, "/parent/login", http.StatusSeeOther)
		return
	}
	s.setSession(w, res)
	s.flash(w, "ok", "You're set up. Now add your first student.")
	http.Redirect(w, r, "/parent/students/new", http.StatusSeeOther)
}

// --- student sign-in (S1, S2) and home (S3) ---------------------------------

func (s *Server) root(w http.ResponseWriter, r *http.Request) {
	c := rc(r)
	switch {
	case c.User == nil:
		students, err := s.svc.Store.ListUsers(r.Context(), store.RoleStudent)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		s.render(w, r, http.StatusOK, "who", s.newView(w, r, "Who's climbing today?", students))
	case c.User.IsParent():
		http.Redirect(w, r, "/parent", http.StatusSeeOther)
	default:
		s.studentHome(w, r, c)
	}
}

type pinData struct {
	Student store.User
	MaxLen  int
	Locked  bool
}

func (s *Server) pinPage(w http.ResponseWriter, r *http.Request) {
	s.renderPIN(w, r, http.StatusOK, "")
}

func (s *Server) renderPIN(w http.ResponseWriter, r *http.Request, status int, msg string) {
	st, err := s.svc.Student(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.notFound(w, r)
			return
		}
		s.serverError(w, r, err)
		return
	}
	v := s.newView(w, r, "Hi "+st.DisplayName, pinData{Student: st.User, MaxLen: auth.MaxPINLen, Locked: st.Locked})
	v.Back, v.BackLabel, v.Err = "/", "Not me", msg
	s.render(w, r, status, "pin", v)
}

func (s *Server) pinSubmit(w http.ResponseWriter, r *http.Request) {
	res, err := s.svc.LoginStudent(r.Context(), r.PathValue("id"), strings.TrimSpace(r.PostFormValue("pin")), deviceLabel(r))
	if err != nil {
		if msg := userMessage(err); msg != "" {
			if errors.Is(err, service.ErrLocked) {
				msg = "" // the locked state says it on the page
			}
			s.renderPIN(w, r, http.StatusUnauthorized, msg)
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			s.notFound(w, r)
			return
		}
		s.serverError(w, r, err)
		return
	}
	s.setSession(w, res)
	s.clearCookie(w, preCookie)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) setSession(w http.ResponseWriter, res service.LoginResult) {
	http.SetCookie(w, s.cookie(sessionCookie, res.Token, res.Session.ExpiresAt.Sub(res.Session.CreatedAt)))
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	if err := s.svc.Logout(r.Context(), c.token); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.clearCookie(w, sessionCookie)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// --- parent sign-in (P1) ----------------------------------------------------

func (s *Server) parentLoginPage(w http.ResponseWriter, r *http.Request) {
	if c := rc(r); c.User != nil && c.User.IsParent() {
		http.Redirect(w, r, "/parent", http.StatusSeeOther)
		return
	}
	v := s.newView(w, r, "Parent sign-in", safeNext(r.URL.Query().Get("next"), ""))
	v.Back, v.BackLabel = "/", "Back"
	s.render(w, r, http.StatusOK, "login", v)
}

func (s *Server) parentLoginSubmit(w http.ResponseWriter, r *http.Request) {
	next := safeNext(r.PostFormValue("next"), "")
	res, err := s.svc.LoginParent(r.Context(), r.PostFormValue("email"), r.PostFormValue("password"), deviceLabel(r))
	if err != nil {
		msg := userMessage(err)
		if msg == "" {
			s.serverError(w, r, err)
			return
		}
		v := s.newView(w, r, "Parent sign-in", next)
		v.Back, v.BackLabel, v.Err = "/", "Back", msg
		v.Form.Set("email", r.PostFormValue("email"))
		s.render(w, r, http.StatusUnauthorized, "login", v)
		return
	}
	s.setSession(w, res)
	s.clearCookie(w, preCookie)
	http.Redirect(w, r, safeNext(next, "/parent"), http.StatusSeeOther) //nolint:gosec // safeNext allows same-site paths only
}

// --- parent pages (P2, P4) --------------------------------------------------

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request, _ *reqCtx) {
	students, err := s.svc.Students(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	v := s.newView(w, r, "Everyone", students)
	v.Shell = "parent"
	s.render(w, r, http.StatusOK, "dashboard", v)
}

func (s *Server) studentNewPage(w http.ResponseWriter, r *http.Request, _ *reqCtx) {
	v := s.newView(w, r, "Add a student", nil)
	v.Shell, v.Back, v.BackLabel = "parent", "/parent", "Everyone"
	s.render(w, r, http.StatusOK, "student_new", v)
}

func (s *Server) studentCreate(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	f := r.PostForm
	st, err := s.svc.CreateStudent(r.Context(), *c.User, service.StudentInput{
		DisplayName: f.Get("display_name"), Avatar: f.Get("avatar"), PIN: strings.TrimSpace(f.Get("pin")),
	})
	if err != nil {
		msg := userMessage(err)
		if msg == "" {
			s.serverError(w, r, err)
			return
		}
		v := s.newView(w, r, "Add a student", nil)
		v.Shell, v.Back, v.BackLabel, v.Err = "parent", "/parent", "Everyone", msg
		v.Form = f
		v.Form.Del("pin")
		s.render(w, r, http.StatusUnprocessableEntity, "student_new", v)
		return
	}
	s.flash(w, "ok", st.DisplayName+" is added. They can sign in with their PIN.")
	http.Redirect(w, r, "/parent", http.StatusSeeOther)
}

func (s *Server) studentPage(w http.ResponseWriter, r *http.Request, _ *reqCtx) {
	st, err := s.svc.Student(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.notFound(w, r)
			return
		}
		s.serverError(w, r, err)
		return
	}
	v := s.newView(w, r, st.DisplayName, st)
	v.Shell, v.Back, v.BackLabel = "parent", "/parent", "Everyone"
	s.render(w, r, http.StatusOK, "student", v)
}

func (s *Server) studentResetPIN(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	id := r.PathValue("id")
	back := "/parent/students/" + url.PathEscape(id)
	if err := s.svc.ResetPIN(r.Context(), *c.User, id, strings.TrimSpace(r.PostFormValue("pin"))); err != nil {
		s.fail(w, r, back, err)
		return
	}
	s.flash(w, "ok", "The new PIN is set.")
	http.Redirect(w, r, back, http.StatusSeeOther) //nolint:gosec // a same-site path built from one escaped URL segment
}

func (s *Server) studentUnlock(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	id := r.PathValue("id")
	back := "/parent/students/" + url.PathEscape(id)
	if err := s.svc.Unlock(r.Context(), *c.User, id); err != nil {
		s.fail(w, r, back, err)
		return
	}
	s.flash(w, "ok", "Unlocked. They can sign in again.")
	http.Redirect(w, r, back, http.StatusSeeOther) //nolint:gosec // a same-site path built from one escaped URL segment
}

// deviceLabel is a rough name for the browser, kept with the session.
func deviceLabel(r *http.Request) string {
	ua := r.UserAgent()
	for _, d := range []struct{ needle, label string }{
		{"iPad", "iPad"}, {"iPhone", "iPhone"}, {"Android", "Android"}, {"CrOS", "Chromebook"},
		{"Macintosh", "Mac"}, {"Windows", "Windows PC"}, {"Linux", "Linux"},
	} {
		if strings.Contains(ua, d.needle) {
			return d.label
		}
	}
	return "Browser"
}
