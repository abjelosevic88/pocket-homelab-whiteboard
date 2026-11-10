package server

import (
	"context"
	"hash/fnv"
	"net/http"
	"net/url"
	"regexp"

	"github.com/abjelosevic88/pocket-homelab-whiteboard/internal/store"
)

// Who may do what with a board.
//
//   - The maker (owner) and the people on it (members: editors and viewers)
//     reach it; anyone else gets 404 — box admins included.
//   - Editors change what is on it, the title, the timer and the vote;
//     viewers look, point and vote. Only the maker shares, renames people's
//     roles and deletes the board.
//   - Through the share link a person without an account sees the board
//     (view) or also changes it (edit), as the maker chose. Never the
//     people on it, the link itself or anything else on this box.

// actor is someone on a board: a person with an account, or a guest
// through the share link.
type actor struct {
	b     *store.Board
	u     *store.User // nil for a guest
	guest string      // the guest's own id (made by their browser)
	name  string
	role  string // owner | editor | viewer
}

func (a *actor) owner() bool   { return a.role == "owner" }
func (a *actor) canEdit() bool { return a.role == "owner" || a.role == "editor" }

// voter is how the person's dot votes are kept.
func (a *actor) voter() string {
	if a.u != nil {
		return "u" + itoa(a.u.ID)
	}
	return "g" + a.guest
}

func (a *actor) uid() *int64 {
	if a.u == nil {
		return nil
	}
	id := a.u.ID
	return &id
}

// colour is the person's pointer colour, the same on every screen.
func (a *actor) colour() string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(a.voter()))
	return peopleColours[int(h.Sum32())%len(peopleColours)]
}

var peopleColours = []string{"#e5484d", "#2f80ed", "#12a594", "#8e4ec6", "#f76b15", "#d6409f", "#0d74ce", "#46a758", "#b5850b", "#6e56cf"}

// boardAccess finds a board the person may reach.
func (s *Server) boardAccess(ctx context.Context, u *store.User, id int64) (*actor, error) {
	if u == nil {
		return nil, store.ErrNotFound
	}
	b, err := s.store.GetBoard(ctx, id)
	if err != nil {
		return nil, err
	}
	a := &actor{b: b, u: u, name: s.actorName(u)}
	if b.UserID == u.ID {
		a.role = "owner"
		return a, nil
	}
	if role := s.store.Membership(ctx, b.ID, u.ID); role != "" {
		a.role = role
		return a, nil
	}
	return nil, store.ErrNotFound
}

func (s *Server) boardParam(w http.ResponseWriter, r *http.Request) (*actor, bool) {
	a, err := s.boardAccess(r.Context(), userFrom(r.Context()), idParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return nil, false
	}
	return a, true
}

func (s *Server) editorParam(w http.ResponseWriter, r *http.Request) (*actor, bool) {
	a, ok := s.boardParam(w, r)
	if !ok {
		return nil, false
	}
	if !a.canEdit() {
		writeErr(w, http.StatusForbidden, "you can look at this board, not change it")
		return nil, false
	}
	return a, true
}

func (s *Server) ownerParam(w http.ResponseWriter, r *http.Request) (*actor, bool) {
	a, ok := s.boardParam(w, r)
	if !ok {
		return nil, false
	}
	if !a.owner() {
		writeErr(w, http.StatusForbidden, "only "+firstNonEmpty(a.b.OwnerName, "its maker")+" can do that")
		return nil, false
	}
	return a, true
}

var reGuest = regexp.MustCompile(`^[A-Za-z0-9_-]{8,40}$`)

// guestParam finds the board behind a share link and the guest using it.
func (s *Server) guestParam(w http.ResponseWriter, r *http.Request) (*actor, bool) {
	b, err := s.store.BoardByShareKey(r.Context(), chiParam(r, "key"))
	if err != nil || b.ShareMode == "off" || b.ShareMode == "" {
		writeErr(w, http.StatusNotFound, "this link does not work any more")
		return nil, false
	}
	g := firstNonEmpty(r.Header.Get("X-Guest-Id"), r.URL.Query().Get("guest"))
	if !reGuest.MatchString(g) {
		g = "anonymous"
	}
	hn := r.Header.Get("X-Guest-Name")
	if v, err := url.QueryUnescape(hn); err == nil {
		hn = v
	}
	name := cleanText(firstNonEmpty(hn, r.URL.Query().Get("name")), 40)
	if name == "" {
		name = "Guest"
	}
	role := "viewer"
	if b.ShareMode == "edit" {
		role = "editor"
	}
	return &actor{b: b, guest: g, name: name, role: role}, true
}

func (s *Server) actorName(u *store.User) string {
	if u == nil {
		return "Someone"
	}
	return firstNonEmpty(u.Name, u.Email)
}
