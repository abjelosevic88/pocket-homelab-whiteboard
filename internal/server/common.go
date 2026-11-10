package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-whiteboard/internal/store"
)

var reColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// userErr is a reason the person can act on (400, not 502).
type userErr struct{ msg string }

func (e *userErr) Error() string { return e.msg }

// today is the local day (the box's zone).
func today() string { return time.Now().Format("2006-01-02") }

func (s *Server) settingsOf(ctx context.Context, uid int64) store.Settings {
	st, _ := s.store.GetSettings(ctx, uid)
	return st
}

func (s *Server) userFail(w http.ResponseWriter, err error, what string) {
	var ue *userErr
	if errors.As(err, &ue) {
		writeErr(w, http.StatusBadRequest, ue.msg)
		return
	}
	if isNotConnected(err) {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.fail(w, err, what)
}

// link is a page of Whiteboard as a full address (for messages and events).
func (s *Server) link(path string) string {
	return strings.TrimRight(s.cfg.BaseURL, "/") + path
}

func niceDate(d string) string {
	t, err := store.ParseDate(d)
	if err != nil {
		return d
	}
	return t.Format("Mon 2 Jan")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func photoURL(id *int64) (string, string) {
	if id == nil || *id == 0 {
		return "", ""
	}
	u := fmt.Sprintf("/api/v1/photos/%d", *id)
	return u, u + "?thumb=1"
}

// sourceOf is who adds a row: the acting app for a link token, "" here.
func sourceOf(ctx context.Context) string { return linkApp(ctx) }

// mayChange says whether the caller may change a row with this source: a
// link token only what its own app added.
func mayChange(ctx context.Context, source string) bool {
	app := linkApp(ctx)
	return app == "" || app == source
}

// text limits for what people type
const (
	maxName  = 120
	maxLine  = 300
	maxNotes = 40000
)

func cleanText(v string, n int) string { return clip(strings.TrimSpace(v), n) }

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func firstLines(text string, n int) []string {
	out := []string{}
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" && len(out) < n {
			out = append(out, clip(l, 200))
		}
	}
	return out
}

func newToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// duration is a solving time in words: 4:05, 1:02:10.
func duration(ms int64) string {
	sec := ms / 1000
	h, m, x := sec/3600, (sec/60)%60, sec%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, x)
	}
	return fmt.Sprintf("%d:%02d", m, x)
}

func firstPos(v ...int) int {
	for _, x := range v {
		if x > 0 {
			return x
		}
	}
	return 0
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
