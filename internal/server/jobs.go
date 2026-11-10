package server

import (
	"context"
	"net/http"

	"github.com/abjelosevic88/pocket-homelab-whiteboard/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-whiteboard/internal/store"
)

const notifyTestBody = "This is how Pocket Whiteboard tells you someone put you on a board."

// push sends a phone message through the person's own notification URL.
func (s *Server) push(ctx context.Context, uid int64, st store.Settings, title, body, path string) bool {
	if st.NotifyURL == "" {
		return false
	}
	n := notify.New(st.NotifyURL, st.NotifyFormat)
	if err := n.Send(ctx, notify.Message{Title: title, Body: body, Priority: "info", Tags: []string{"memo"}, Link: s.link(path)}); err != nil {
		s.log.Debug("notify failed", "user", uid, "err", err)
		return false
	}
	return true
}

func (s *Server) handleNotifyTest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st := s.settingsOf(ctx, actorID(ctx))
	if st.NotifyURL == "" {
		writeErr(w, http.StatusBadRequest, "add a notification URL first")
		return
	}
	n := notify.New(st.NotifyURL, st.NotifyFormat)
	if err := n.Send(ctx, notify.Message{Title: "Pocket Whiteboard", Body: notifyTestBody, Priority: "info", Tags: []string{"memo"}}); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
