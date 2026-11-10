package server

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/abjelosevic88/pocket-homelab-whiteboard/internal/store"
)

// Working together: private mode, comments with @mentions, the board's
// history, reactions, floating emoji, and bringing everyone to one place.

// ---- private mode ----

func (s *Server) handlePrivate(w http.ResponseWriter, r *http.Request) {
	a, ok := s.editorParam(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	var in struct {
		Client string `json:"client"`
		On     bool   `json:"on"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	p := a.b.PrivateOf()
	revealed := int64(0)
	if in.On {
		if !p.On {
			p = store.Private{On: true, By: a.name, StartedAt: store.Now()}
		}
	} else {
		n, err := s.store.Reveal(ctx, a.b.ID)
		if err != nil {
			s.fail(w, err, "reveal")
			return
		}
		revealed = n
		p = store.Private{}
	}
	a.b.Private = mustJSON(p)
	if err := s.store.UpdateBoard(ctx, a.b); err != nil {
		s.fail(w, err, "private mode")
		return
	}
	s.tellBoard(a, "")
	if !in.On {
		s.hub.send(a.b.ID, "", map[string]any{"type": "reload", "by": a.name, "why": "revealed", "n": revealed})
	}
	writeJSON(w, http.StatusOK, map[string]any{"board": boardOut(a.b, a), "revealed": revealed})
}

// ---- bring everyone here, floating emoji ----

type viewRect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

func (v viewRect) ok() bool {
	return finite(v.X) && finite(v.Y) && finite(v.W) && finite(v.H) && v.W > 0 && v.H > 0
}

func (s *Server) handleSummon(w http.ResponseWriter, r *http.Request) {
	a, ok := s.boardParam(w, r)
	if !ok {
		return
	}
	var in struct {
		Client string   `json:"client"`
		View   viewRect `json:"view"`
	}
	if err := decode(r, &in); err != nil || !in.View.ok() {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	s.hub.send(a.b.ID, in.Client, map[string]any{"type": "summon", "client": in.Client, "by": a.name, "colour": a.colour(), "view": in.View})
	w.WriteHeader(http.StatusNoContent)
}

// cleanEmoji accepts a short emoji (no letters or markup).
func cleanEmoji(e string) (string, bool) {
	e = strings.TrimSpace(e)
	if e == "" || len(e) > 32 || utf8.RuneCountInString(e) > 8 {
		return "", false
	}
	for _, r := range e {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("<>&\"'", r)) {
			return "", false
		}
	}
	return e, true
}

func (s *Server) float(w http.ResponseWriter, r *http.Request, a *actor) {
	var in struct {
		Client string `json:"client"`
		Emoji  string `json:"emoji"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	e, ok := cleanEmoji(in.Emoji)
	if !ok {
		writeErr(w, http.StatusBadRequest, "one emoji")
		return
	}
	s.hub.send(a.b.ID, in.Client, map[string]any{"type": "float", "emoji": e, "name": a.name, "colour": a.colour()})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleFloat(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.boardParam(w, r); ok {
		s.float(w, r, a)
	}
}

// ---- reactions ----

func (s *Server) react(w http.ResponseWriter, r *http.Request, a *actor) {
	ctx := r.Context()
	var in struct {
		Client   string `json:"client"`
		ObjectID string `json:"object_id"`
		Emoji    string `json:"emoji"`
	}
	if err := decode(r, &in); err != nil || !reObjID.MatchString(in.ObjectID) {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	e, ok := cleanEmoji(in.Emoji)
	if !ok {
		writeErr(w, http.StatusBadRequest, "one emoji")
		return
	}
	o, err := s.store.GetObject(ctx, a.b.ID, in.ObjectID)
	if err != nil || (o.HiddenBy != "" && o.HiddenBy != a.voter()) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.store.ToggleReaction(ctx, a.b.ID, in.ObjectID, e, a.voter(), a.name); err != nil {
		s.fail(w, err, "react")
		return
	}
	all, _ := s.store.Reactions(ctx, a.b.ID, in.ObjectID)
	list := all[in.ObjectID]
	if list == nil {
		list = []store.Reaction{}
	}
	s.hub.send(a.b.ID, in.Client, map[string]any{"type": "reactions", "object_id": in.ObjectID, "reactions": list})
	writeJSON(w, http.StatusOK, map[string]any{"object_id": in.ObjectID, "reactions": list})
}

func (s *Server) handleReact(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.boardParam(w, r); ok {
		s.react(w, r, a)
	}
}

// ---- comments ----

// boardPeople are the accounts on a board: its maker and its members.
func (s *Server) boardPeople(ctx context.Context, b *store.Board) map[int64]string {
	out := map[int64]string{b.UserID: b.OwnerName}
	members, _ := s.store.Members(ctx, b.ID)
	for _, m := range members {
		out[m.UserID] = firstNonEmpty(m.Name, m.Email)
	}
	return out
}

func (s *Server) comments(w http.ResponseWriter, r *http.Request, a *actor) {
	list, err := s.store.Comments(r.Context(), a.b.ID)
	if err != nil {
		s.fail(w, err, "comments")
		return
	}
	people := []map[string]any{}
	if a.u != nil {
		for id, name := range s.boardPeople(r.Context(), a.b) {
			people = append(people, map[string]any{"id": id, "name": name})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"comments": list, "people": people})
}

func (s *Server) handleComments(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.boardParam(w, r); ok {
		s.comments(w, r, a)
	}
}

func (s *Server) addComment(w http.ResponseWriter, r *http.Request, a *actor) {
	ctx := r.Context()
	var in struct {
		Client   string  `json:"client"`
		ParentID int64   `json:"parent_id"`
		ObjectID string  `json:"object_id"`
		X        float64 `json:"x"`
		Y        float64 `json:"y"`
		Body     string  `json:"body"`
		Mentions []int64 `json:"mentions"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	body := cleanText(in.Body, 4000)
	if body == "" {
		writeErr(w, http.StatusBadRequest, "write something")
		return
	}
	if !finite(in.X) || !finite(in.Y) || (in.ObjectID != "" && !reObjID.MatchString(in.ObjectID)) {
		writeErr(w, http.StatusBadRequest, "bad place")
		return
	}
	c := &store.Comment{BoardID: a.b.ID, ObjectID: in.ObjectID, X: math.Round(in.X), Y: math.Round(in.Y), UserID: a.uid(), Author: a.name, Voter: a.voter(), Body: body}
	var root *store.Comment
	if in.ParentID > 0 {
		p, err := s.store.GetComment(ctx, a.b.ID, in.ParentID)
		if err != nil {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		if p.ParentID != nil {
			p, err = s.store.GetComment(ctx, a.b.ID, *p.ParentID)
			if err != nil {
				writeErr(w, http.StatusNotFound, "not found")
				return
			}
		}
		root = p
		c.ParentID = &p.ID
		c.ObjectID, c.X, c.Y = p.ObjectID, p.X, p.Y
	}
	people := s.boardPeople(ctx, a.b)
	seen := map[int64]bool{}
	c.Mentions = []int64{}
	for _, id := range in.Mentions {
		if _, on := people[id]; on && !seen[id] {
			seen[id] = true
			c.Mentions = append(c.Mentions, id)
		}
	}
	if err := s.store.AddComment(ctx, c); err != nil {
		s.fail(w, err, "comment")
		return
	}
	s.hub.send(a.b.ID, in.Client, map[string]any{"type": "comments", "by": a.name})
	go s.tellAboutComment(context.WithoutCancel(ctx), a, c, root)
	writeJSON(w, http.StatusCreated, map[string]any{"comment": c})
}

// tellAboutComment pushes to the people mentioned and, for a reply, to
// the others in the thread.
func (s *Server) tellAboutComment(ctx context.Context, a *actor, c *store.Comment, root *store.Comment) {
	thread := c.ID
	if root != nil {
		thread = root.ID
	}
	path := fmt.Sprintf("/b/%d?c=%d", a.b.ID, thread)
	told := map[int64]bool{}
	if a.u != nil {
		told[a.u.ID] = true
	}
	for _, id := range c.Mentions {
		if told[id] {
			continue
		}
		told[id] = true
		if st := s.settingsOf(ctx, id); st.NotifyMentions {
			s.push(ctx, id, st, a.name+" mentioned you on “"+a.b.Title+"”", clip(c.Body, 200), path)
		}
	}
	if root == nil {
		return
	}
	all, _ := s.store.Comments(ctx, a.b.ID)
	for _, x := range all {
		if x.UserID == nil || (x.ID != root.ID && (x.ParentID == nil || *x.ParentID != root.ID)) || told[*x.UserID] {
			continue
		}
		told[*x.UserID] = true
		if st := s.settingsOf(ctx, *x.UserID); st.NotifyComments {
			s.push(ctx, *x.UserID, st, a.name+" replied on “"+a.b.Title+"”", clip(c.Body, 200), path)
		}
	}
}

func (s *Server) handleAddComment(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.boardParam(w, r); ok {
		s.addComment(w, r, a)
	}
}

// handleUpdateComment: its writer changes the text; anyone who can edit
// the board (or the writer) resolves the thread or moves its pin.
func (s *Server) handleUpdateComment(w http.ResponseWriter, r *http.Request) {
	a, ok := s.boardParam(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	c, err := s.store.GetComment(ctx, a.b.ID, idParam(r, "cid"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	var in struct {
		Client   string  `json:"client"`
		Body     *string `json:"body"`
		Resolved *bool   `json:"resolved"`
		X        *float64
		Y        *float64
		ObjectID *string `json:"object_id"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	mine := c.Voter == a.voter()
	if in.Body != nil {
		if !mine {
			writeErr(w, http.StatusForbidden, "only "+c.Author+" can change what they wrote")
			return
		}
		if b := cleanText(*in.Body, 4000); b != "" {
			c.Body = b
		}
	}
	if in.Resolved != nil || in.X != nil || in.Y != nil || in.ObjectID != nil {
		if !mine && !a.canEdit() {
			writeErr(w, http.StatusForbidden, "you can look at this board, not change it")
			return
		}
		if in.Resolved != nil {
			c.Resolved = *in.Resolved
		}
		if in.X != nil && finite(*in.X) {
			c.X = math.Round(*in.X)
		}
		if in.Y != nil && finite(*in.Y) {
			c.Y = math.Round(*in.Y)
		}
		if in.ObjectID != nil && (*in.ObjectID == "" || reObjID.MatchString(*in.ObjectID)) {
			c.ObjectID = *in.ObjectID
		}
	}
	if err := s.store.UpdateComment(ctx, c); err != nil {
		s.fail(w, err, "comment")
		return
	}
	s.hub.send(a.b.ID, in.Client, map[string]any{"type": "comments", "by": a.name})
	writeJSON(w, http.StatusOK, map[string]any{"comment": c})
}

// handleDeleteComment: its writer or the board's maker.
func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	a, ok := s.boardParam(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	c, err := s.store.GetComment(ctx, a.b.ID, idParam(r, "cid"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if c.Voter != a.voter() && !a.owner() {
		writeErr(w, http.StatusForbidden, "only "+c.Author+" or the board's maker can delete this")
		return
	}
	if err := s.store.DeleteComment(ctx, a.b.ID, c.ID); err != nil {
		s.fail(w, err, "comment")
		return
	}
	s.hub.send(a.b.ID, r.URL.Query().Get("client"), map[string]any{"type": "comments", "by": a.name})
	w.WriteHeader(http.StatusNoContent)
}

// ---- history ----

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	a, ok := s.boardParam(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	versions, err := s.store.Versions(ctx, a.b.ID)
	if err != nil {
		s.fail(w, err, "history")
		return
	}
	trash, _ := s.store.Trash(ctx, a.b.ID)
	writeJSON(w, http.StatusOK, map[string]any{"versions": versions, "trash": trash})
}

func (s *Server) handleSaveVersion(w http.ResponseWriter, r *http.Request) {
	a, ok := s.editorParam(w, r)
	if !ok {
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	_ = decode(r, &in)
	id, err := s.store.SaveVersion(r.Context(), a.b.ID, firstNonEmpty(cleanText(in.Name, 80), "Saved by hand"), a.name)
	if err != nil {
		s.fail(w, err, "save version")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	a, ok := s.boardParam(w, r)
	if !ok {
		return
	}
	v, objs, err := s.store.VersionObjects(r.Context(), a.b.ID, idParam(r, "vid"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"version": v, "preview": preview(objs), "objects": objs})
}

func (s *Server) handleRestoreVersion(w http.ResponseWriter, r *http.Request) {
	a, ok := s.editorParam(w, r)
	if !ok {
		return
	}
	v, err := s.store.RestoreVersion(r.Context(), a.b.ID, idParam(r, "vid"), a.name)
	if err != nil {
		s.fail(w, err, "go back")
		return
	}
	s.hub.send(a.b.ID, "", map[string]any{"type": "reload", "by": a.name, "why": "restored", "version": v})
	writeJSON(w, http.StatusOK, map[string]any{"version": v})
}

// handleRestoreTrashed puts a deleted thing back (under a new id if its
// old one is taken again).
func (s *Server) handleRestoreTrashed(w http.ResponseWriter, r *http.Request) {
	a, ok := s.editorParam(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	tid := idParam(r, "tid")
	o, err := s.store.TrashedObject(ctx, a.b.ID, tid)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if _, err := s.store.GetObject(ctx, a.b.ID, o.ID); err == nil {
		o.ID = "r" + store.RandomToken(6)
	}
	o.Hidden, o.HiddenBy = false, ""
	ops := []store.Op{{Op: "put", ID: o.ID, Obj: o}}
	v, err := s.store.ApplyOps(ctx, a.b.ID, ops, a.uid(), a.name, "")
	if err != nil {
		s.fail(w, err, "put back")
		return
	}
	_ = s.store.DropTrashed(ctx, a.b.ID, tid)
	s.hub.send(a.b.ID, "", map[string]any{"type": "ops", "by": a.name, "ops": ops, "version": v})
	writeJSON(w, http.StatusOK, map[string]any{"object": o, "version": v})
}

// ---- for guests on the share link ----

func (s *Server) handlePublicComments(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.guestParam(w, r); ok {
		s.comments(w, r, a)
	}
}

func (s *Server) handlePublicAddComment(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.guestEditor(w, r); ok {
		s.addComment(w, r, a)
	}
}

func (s *Server) handlePublicReact(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.guestParam(w, r); ok {
		if !s.guestLimit(r) {
			writeErr(w, http.StatusTooManyRequests, "slow down a little")
			return
		}
		s.react(w, r, a)
	}
}

func (s *Server) handlePublicFloat(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.guestParam(w, r); ok {
		if !s.guestLimit(r) {
			writeErr(w, http.StatusTooManyRequests, "slow down a little")
			return
		}
		s.float(w, r, a)
	}
}
