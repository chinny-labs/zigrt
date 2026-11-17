package web

import (
	"context"
	"io/fs"
	"net/http"
	"time"
)

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	static, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", cacheStatic(http.FileServerFS(static))))
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/static/icon.svg", http.StatusMovedPermanently)
	})

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /readyz", s.readyz)
	if s.api != nil {
		mux.Handle("/api/", s.api)
	}
	if s.mcp != nil {
		mux.Handle("/mcp", s.mcp)
	}

	mux.HandleFunc("GET /setup", s.setupPage)
	mux.HandleFunc("POST /setup", s.setupSubmit)

	// The root is the student's: the picker when signed out, home when in.
	mux.HandleFunc("GET /{$}", s.root)
	mux.HandleFunc("GET /pin/{id}", s.pinPage)
	mux.HandleFunc("POST /pin/{id}", s.pinSubmit)
	mux.HandleFunc("POST /logout", s.signedIn(s.logout))

	// Lessons and reviews: the engine offers them, only students do them.
	mux.HandleFunc("POST /tasks", s.studentOnly(s.taskStart))
	mux.HandleFunc("GET /tasks/{id}", s.studentOnly(s.taskPage))
	mux.HandleFunc("POST /tasks/{id}/next", s.studentOnly(s.taskNext))
	mux.HandleFunc("POST /tasks/{id}/answer", s.studentOnly(s.taskAnswer))
	mux.HandleFunc("POST /tasks/{id}/not-learned", s.studentOnly(s.taskNotLearned))
	mux.HandleFunc("POST /placement/skip", s.studentOnly(s.placementSkip))
	mux.HandleFunc("GET /tower", s.studentOnly(s.towerPage))
	mux.HandleFunc("POST /assignments/{id}/start", s.studentOnly(s.assignmentStart))

	// Free practice: both roles build sets; only students answer on screen.
	mux.HandleFunc("GET /drills", s.signedIn(s.drillBuilder))
	mux.HandleFunc("POST /drills", s.signedIn(s.drillCreate))
	mux.HandleFunc("GET /drills/{id}", s.signedIn(s.drillPage))
	mux.HandleFunc("POST /drills/{id}/finish", s.signedIn(s.drillFinish))
	mux.HandleFunc("GET /drills/{id}/results", s.signedIn(s.drillResults))
	mux.HandleFunc("POST /drills/{id}/retry", s.signedIn(s.drillRetry))
	mux.HandleFunc("POST /drills/{id}/again", s.signedIn(s.drillAgain))
	mux.HandleFunc("GET /worksheets/{id}", s.signedIn(s.worksheetPage))
	mux.HandleFunc("POST /worksheets/{id}/again", s.signedIn(s.drillAgain))

	mux.HandleFunc("GET /parent/login", s.parentLoginPage)
	mux.HandleFunc("POST /parent/login", s.parentLoginSubmit)
	mux.HandleFunc("GET /parent", s.parent(s.dashboard))
	mux.HandleFunc("GET /parent/students/new", s.parent(s.studentNewPage))
	mux.HandleFunc("POST /parent/students", s.parent(s.studentCreate))
	mux.HandleFunc("GET /parent/students/{id}", s.parent(s.studentPage))
	mux.HandleFunc("GET /parent/students/{id}/settings", s.parent(s.studentSettingsPage))
	mux.HandleFunc("POST /parent/students/{id}/settings", s.parent(s.studentSettingsSave))
	mux.HandleFunc("POST /parent/students/{id}/placement", s.parent(s.studentPlacement))
	mux.HandleFunc("GET /parent/students/{id}/blocks", s.parent(s.studentBlocksPage))
	mux.HandleFunc("POST /parent/students/{id}/blocks/{topic}", s.parent(s.studentBlockSave))
	mux.HandleFunc("GET /parent/students/{id}/delete", s.parent(s.studentDeletePage))
	mux.HandleFunc("POST /parent/students/{id}/delete", s.parent(s.studentDelete))
	mux.HandleFunc("GET /parent/students/{id}/tower", s.parent(s.studentTowerPage))
	mux.HandleFunc("GET /parent/household", s.parent(s.householdPage))
	mux.HandleFunc("POST /parent/household", s.parent(s.householdSave))
	mux.HandleFunc("POST /parent/password", s.parent(s.passwordSave))
	mux.HandleFunc("GET /parent/parents/new", s.parent(s.parentNewPage))
	mux.HandleFunc("POST /parent/parents", s.parent(s.parentCreate))
	mux.HandleFunc("POST /parent/backup", s.parent(s.backupNow))
	mux.HandleFunc("GET /parent/export", s.parent(s.exportAll))
	mux.HandleFunc("POST /parent/pack", s.parent(s.packUpload))
	mux.HandleFunc("GET /parent/notifications", s.parent(s.notificationsPage))
	mux.HandleFunc("POST /parent/tokens", s.parent(s.tokenCreate))
	mux.HandleFunc("POST /parent/tokens/{id}/revoke", s.parent(s.tokenRevoke))
	mux.HandleFunc("POST /parent/notifications/read", s.parent(s.notificationsRead))
	mux.HandleFunc("POST /parent/students/{id}/pin", s.parent(s.studentResetPIN))
	mux.HandleFunc("POST /parent/students/{id}/unlock", s.parent(s.studentUnlock))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { s.notFound(w, r) })
	return mux
}

// readyz reports whether the app can serve: a content pack is loaded and
// the database answers. A pack in the content folder that needs a newer
// Zigrt keeps the app unready rather than running an older pack (plan 4.7).
func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	if err, _ := s.contentErr.Load().(error); err != nil {
		http.Error(w, "content pack: "+err.Error()+"\n", http.StatusServiceUnavailable)
		return
	}
	if s.svc.Pack() == nil {
		http.Error(w, "no content pack loaded\n", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.svc.Store.Ping(ctx); err != nil {
		s.log.Error("readyz", "err", err)
		http.Error(w, "database unavailable\n", http.StatusServiceUnavailable)
		return
	}
	_, _ = w.Write([]byte("ready\n"))
}

// cacheStatic lets browsers keep embedded assets for an hour; they change
// only with a new release.
func cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		next.ServeHTTP(w, r)
	})
}
