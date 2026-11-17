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
	mux.HandleFunc("POST /parent/students/{id}/pin", s.parent(s.studentResetPIN))
	mux.HandleFunc("POST /parent/students/{id}/unlock", s.parent(s.studentUnlock))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { s.notFound(w, r) })
	return mux
}

// readyz reports whether the app can serve: the database answers.
func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
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
