package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-whiteboard/internal/store"
)

// Router builds the chi router.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	if s.cfg.TrustProxy {
		r.Use(middleware.RealIP)
	}
	r.Use(middleware.RequestID)
	r.Use(s.logging)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))

	r.Get("/pocket-ask.js", s.core.AskWidget("whiteboard"))
	r.Get("/healthz", s.handleHealth)
	r.Get("/readyz", s.handleReady)
	r.Get("/api/health", s.handleHealth)
	if s.cfg.MetricsEnabled {
		r.Get("/metrics", s.handleMetrics)
	}

	r.Route("/api/v1", func(r chi.Router) {
		// live streams stay open: no timeout
		r.With(s.requireAuth).Get("/boards/{id}/stream", s.handleStream)
		r.Get("/public/b/{key}/stream", s.handlePublicStream)

		// the share link: no account needed
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(60 * time.Second))
			r.Get("/public/b/{key}", s.handlePublicBoard)
			r.Post("/public/b/{key}/ops", s.handlePublicOps)
			r.Post("/public/b/{key}/cursor", s.handlePublicCursor)
			r.Get("/public/b/{key}/votes", s.handlePublicVotes)
			r.Post("/public/b/{key}/vote", s.handlePublicVote)
			r.Post("/public/b/{key}/poker/pick", s.handlePublicPokerPick)
			r.Get("/public/b/{key}/uploads/{id}", s.handlePublicUpload)
			r.Post("/public/b/{key}/uploads", s.handlePublicNewUpload)
			r.Get("/public/b/{key}/comments", s.handlePublicComments)
			r.Post("/public/b/{key}/comments", s.handlePublicAddComment)
			r.Post("/public/b/{key}/react", s.handlePublicReact)
			r.Post("/public/b/{key}/float", s.handlePublicFloat)
		})

		// uploads, imports and AI can take longer
		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Post("/uploads", s.handleUpload)
			r.Post("/import", s.handleImport)
			r.Post("/import/miro", s.handleImportMiro)
			r.Get("/export.json", s.handleExportJSON)
			r.Post("/boards/{id}/ai/group", s.handleAIGroup)
			r.Post("/boards/{id}/ai/summary", s.handleAISummary)
			r.Post("/boards/{id}/ai/ideas", s.handleAIIdeas)
			r.Post("/boards/{id}/files", s.handleToFiles)
			r.Post("/boards/{id}/tasks", s.handleToTasks)
			r.Post("/boards/{id}/to-notes", s.handleToNotes)
		})

		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(120 * time.Second))
			r.Post("/auth/login", s.handleLogin)
			r.Post("/auth/setup", s.handleSetup)
			r.Get("/auth/status", s.handleAuthStatus)
			r.Get("/auth/oidc/start", s.handleOIDCStart)
			r.Get("/auth/oidc/callback", s.handleOIDCCallback)

			r.Group(func(r chi.Router) {
				r.Use(s.requireAuth)
				r.Post("/auth/logout", s.handleLogout)
				r.Get("/auth/me", s.handleMe)
				r.Put("/auth/me", s.handleUpdateMe)

				// for the person, and for the rest of the box: Pocket Day, Search, AI
				r.Get("/home", s.handleHome)
				r.Get("/summary", s.handleSummary)
				r.Get("/day/{date}", s.handleDay)
				r.Get("/search", s.handleSearch)
				r.Get("/templates", s.handleTemplates)
				r.Get("/people", s.handlePeople)
				r.Get("/uploads/{id}", s.handleGetUpload)

				// boards
				r.Get("/boards", s.handleBoards)
				r.Post("/boards", s.handleCreateBoard)
				r.Get("/boards/{id}", s.handleGetBoard)
				r.Put("/boards/{id}", s.handleUpdateBoard)
				r.Delete("/boards/{id}", s.handleDeleteBoard)
				r.Post("/boards/{id}/duplicate", s.handleDuplicate)
				r.Put("/boards/{id}/star", s.handleStar)
				r.Post("/boards/{id}/share-key", s.handleNewShareKey)
				r.Post("/boards/{id}/members", s.handleAddMember)
				r.Delete("/boards/{id}/members/{uid}", s.handleRemoveMember)
				r.Post("/boards/{id}/ops", s.handleOps)
				r.Post("/boards/{id}/cursor", s.handleCursor)
				r.Post("/boards/{id}/notes", s.handleAddNotes)
				r.Put("/boards/{id}/timer", s.handleTimer)
				r.Put("/boards/{id}/vote", s.handleSetVote)
				r.Post("/boards/{id}/vote", s.handleCastVote)
				r.Get("/boards/{id}/votes", s.handleVotes)
				r.Post("/boards/{id}/chat", s.handleShareChat)
				r.Put("/boards/{id}/private", s.handlePrivate)
				r.Post("/boards/{id}/summon", s.handleSummon)
				r.Post("/boards/{id}/float", s.handleFloat)
				r.Post("/boards/{id}/react", s.handleReact)
				r.Get("/boards/{id}/comments", s.handleComments)
				r.Post("/boards/{id}/comments", s.handleAddComment)
				r.Put("/boards/{id}/comments/{cid}", s.handleUpdateComment)
				r.Delete("/boards/{id}/comments/{cid}", s.handleDeleteComment)
				r.Get("/boards/{id}/history", s.handleHistory)
				r.Post("/boards/{id}/versions", s.handleSaveVersion)
				r.Get("/boards/{id}/versions/{vid}", s.handleVersion)
				r.Post("/boards/{id}/versions/{vid}/restore", s.handleRestoreVersion)
				r.Post("/boards/{id}/trash/{tid}/restore", s.handleRestoreTrashed)
				r.Get("/folders", s.handleFolders)
				r.Post("/folders", s.handleSaveFolder)
				r.Put("/folders/{fid}", s.handleSaveFolder)
				r.Delete("/folders/{fid}", s.handleDeleteFolder)
				r.Put("/boards/{id}/folder", s.handleBoardFolder)
				r.Post("/boards/{id}/template", s.handleSaveTemplate)
				r.Delete("/templates/mine/{tid}", s.handleDeleteTemplate)
				r.Get("/unfurl", s.handleUnfurl)
				r.Post("/boards/{id}/cards/{oid}/task", s.handleCardTask)
				r.Post("/boards/{id}/cards/pull", s.handleCardsPull)
				r.Get("/boards/{id}/cards/teams", s.handleProjectTeams)
				r.Post("/boards/{id}/cards/{oid}/issue", s.handleCardIssue)
				r.Post("/boards/{id}/cards/{oid}/event", s.handleCardEvent)
				r.Put("/boards/{id}/anonymous", s.handleAnonymous)
				r.Post("/boards/{id}/poker", s.handlePokerStart)
				r.Post("/boards/{id}/poker/pick", s.handlePokerPick)
				r.Put("/boards/{id}/poker/reveal", s.handlePokerReveal)
				r.Delete("/boards/{id}/poker", s.handlePokerStop)
				r.Post("/boards/{id}/uploads/adopt", s.handleAdoptUploads)

				// the other Pocket apps
				r.Get("/apps", s.handleApps)
				r.Get("/apps/chats", s.handleChats)

				// AI
				r.Get("/ai/status", s.handleAIStatus)
				r.Get("/ai/models", s.handleAIModels)

				r.Post("/integrations/test", s.handleIntegrationTest)
				r.Post("/notify/test", s.handleNotifyTest)

				r.Get("/users", s.handleListUsers)
				r.Post("/users", s.handleCreateUser)
				r.Put("/users/{id}", s.handleUpdateUser)
				r.Delete("/users/{id}", s.handleDeleteUser)

				r.Get("/settings", s.handleGetSettings)
				r.Put("/settings", s.handlePutSettings)
				r.Get("/settings/system", s.handleSystemInfo)

				r.Get("/tokens", s.handleListTokens)
				r.Post("/tokens", s.handleCreateToken)
				r.Delete("/tokens/{id}", s.handleDeleteToken)

				r.Get("/backup", s.handleBackup)
				r.Get("/export.csv", s.handleExportCSV)
			})
		})
	})

	r.NotFound(s.handleSPA)
	return r
}

// ---------- helpers ----------

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		s.requests.Add(1)
		if ww.Status() >= 500 {
			s.errors.Add(1)
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			return
		}
		s.log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "dur", time.Since(start).String(), "ip", r.RemoteAddr)
	})
}

type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

func (s *Server) fail(w http.ResponseWriter, err error, context string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
		return
	case errors.Is(err, store.ErrConflict), errors.Is(err, core.ErrOperationConflict):
		writeErr(w, http.StatusConflict, "already exists")
		return
	}
	s.log.Error(context, "err", err)
	writeErr(w, http.StatusInternalServerError, context+": "+err.Error())
}

func (s *Server) readBody(r *http.Request) ([]byte, error) {
	return io.ReadAll(http.MaxBytesReader(nil, r.Body, s.cfg.MaxBodyBytes))
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 32<<20))
	return dec.Decode(v)
}

// decodeKeys decodes JSON into v and also reports which top-level keys were
// present, so PUT handlers can tell "not sent" from "sent empty".
func decodeKeys(r *http.Request, v any) (map[string]bool, error) {
	b, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	keys := make(map[string]bool, len(raw))
	for k := range raw {
		keys[k] = true
	}
	return keys, json.Unmarshal(b, v)
}

func idParam(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	return n
}

func queryInt(r *http.Request, name string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(name))
	return n
}

func queryBool(r *http.Request, name string) bool {
	v := strings.ToLower(r.URL.Query().Get(name))
	return v == "1" || v == "true" || v == "yes"
}

func validDate(s string) bool {
	if s == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// ---------- health / metrics ----------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"core_outbox": s.coreOutboxHealth(r.Context()), "status": "ok", "version": Version, "uptime": time.Since(s.started).Round(time.Second).String()})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DB.PingContext(r.Context()); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	counts := s.store.Counts(ctx)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP pocket_whiteboard_info Build info.\n# TYPE pocket_whiteboard_info gauge\npocket_whiteboard_info{version=%q} 1\n", Version)
	fmt.Fprintf(w, "# HELP pocket_whiteboard_uptime_seconds Process uptime.\n# TYPE pocket_whiteboard_uptime_seconds gauge\npocket_whiteboard_uptime_seconds %d\n", int(time.Since(s.started).Seconds()))
	fmt.Fprintf(w, "# HELP pocket_whiteboard_http_requests_total HTTP requests served.\n# TYPE pocket_whiteboard_http_requests_total counter\npocket_whiteboard_http_requests_total %d\n", s.requests.Load())
	fmt.Fprintf(w, "# HELP pocket_whiteboard_http_errors_total HTTP 5xx responses.\n# TYPE pocket_whiteboard_http_errors_total counter\npocket_whiteboard_http_errors_total %d\n", s.errors.Load())
	fmt.Fprintf(w, "# HELP pocket_whiteboard_rows Rows per table.\n# TYPE pocket_whiteboard_rows gauge\n")
	for k, v := range counts {
		fmt.Fprintf(w, "pocket_whiteboard_rows{table=%q} %d\n", k, v)
	}
}

// ---------- static / SPA ----------

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if s.webFS == nil {
		http.Error(w, "web UI not built (run `make web`)", http.StatusNotFound)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if f, err := s.webFS.Open(path); err == nil {
		f.Close()
		if strings.HasPrefix(path, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.FileServerFS(s.webFS).ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	r.URL.Path = "/"
	http.FileServerFS(s.webFS).ServeHTTP(w, r)
}
