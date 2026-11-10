package server

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-whiteboard/internal/store"
)

var (
	backgrounds  = []string{"dots", "grid", "plain"}
	stickyColors = []string{"light-yellow", "yellow", "orange", "light-green", "green", "dark-green", "cyan", "light-pink", "pink", "violet", "red", "light-blue", "blue", "dark-blue", "black", "gray", "white", "lime"}
	reObjID      = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)
)

const (
	maxObjects    = 20000   // things on one board
	maxOps        = 2000    // changes in one batch
	maxObjectData = 400_000 // bytes of one object's data (long drawings)
	maxCoord      = 1e7
)

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && math.Abs(v) < maxCoord }

// boardOut is a board as the person asking sees it: the share link only
// for its maker.
func boardOut(b *store.Board, a *actor) map[string]any {
	out := map[string]any{"id": b.ID, "title": b.Title, "about": b.About, "template": b.Template, "background": b.Background,
		"share_mode": b.ShareMode, "timer_ends": b.TimerEnds, "timer_secs": b.TimerSecs, "vote": b.VoteOf(), "version": b.Version,
		"owner_name": b.OwnerName, "owner_id": b.UserID, "private": b.PrivateOf(), "created_at": b.CreatedAt, "updated_at": b.UpdatedAt}
	if a != nil {
		out["role"] = a.role
		if a.owner() {
			out["share_key"] = b.ShareKey
		}
	}
	return out
}

// preview is a board in miniature for the list: where things are and
// their colour.
type previewShape struct {
	K string  `json:"k"`
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
	C string  `json:"c,omitempty"`
	S string  `json:"s,omitempty"` // a shape's form
}

func preview(objs []store.Object) []previewShape {
	out := []previewShape{}
	for _, o := range objs {
		if o.Kind == "line" || o.Kind == "pen" {
			continue
		}
		var d struct {
			Color string `json:"color"`
			Fill  string `json:"fill"`
			Shape string `json:"shape"`
		}
		_ = json.Unmarshal(o.Data, &d)
		out = append(out, previewShape{K: o.Kind, X: math.Round(o.X), Y: math.Round(o.Y), W: math.Round(o.W), H: math.Round(o.H), C: firstNonEmpty(d.Fill, d.Color), S: d.Shape})
		if len(out) >= 400 {
			break
		}
	}
	return out
}

// ---- boards ----

func (s *Server) handleBoards(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	list, err := s.store.BoardsFor(ctx, u.ID)
	if err != nil {
		s.fail(w, err, "boards")
		return
	}
	out := []map[string]any{}
	for i := range list {
		b := &list[i]
		m := boardOut(b, &actor{b: b, u: u, role: b.Role})
		m["starred"], m["opened_at"], m["objects_n"], m["members_n"], m["folder_id"] = b.Starred, b.OpenedAt, b.Objects, b.Members, b.FolderID
		m["live"] = s.hub.count(b.ID)
		if !queryBool(r, "plain") {
			objs, _ := s.store.Objects(ctx, b.ID)
			m["preview"] = preview(objs)
		}
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"boards": out})
}

func (s *Server) handleCreateBoard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	var in struct {
		Title      string `json:"title"`
		About      string `json:"about"`
		Template   string `json:"template"`
		Background string `json:"background"`
		FolderID   int64  `json:"folder_id"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	// one of the person's own templates ("u:ID")
	var mine []store.Object
	tpl, ok := templateByKey(firstNonEmpty(in.Template, "blank"))
	if strings.HasPrefix(in.Template, "u:") {
		tid, _ := strconv.ParseInt(strings.TrimPrefix(in.Template, "u:"), 10, 64)
		t, objs, err := s.store.Template(ctx, u.ID, tid)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "unknown template")
			return
		}
		tpl = template{Key: "blank", Title: t.Title, About: t.About, Objects: func() []store.Object { return nil }}
		if in.Background == "" {
			in.Background = t.Background
		}
		if in.About == "" {
			in.About = t.About
		}
		mine, ok = objs, true
	}
	if !ok {
		writeErr(w, http.StatusBadRequest, "unknown template")
		return
	}
	st := s.settingsOf(ctx, u.ID)
	b := &store.Board{UserID: u.ID, Title: firstNonEmpty(cleanText(in.Title, maxName), tpl.Title), About: cleanText(in.About, maxLine), Template: tpl.Key,
		Background: firstNonEmpty(in.Background, st.Background, "dots"), Source: sourceOf(ctx)}
	if !oneOf(b.Background, backgrounds) {
		b.Background = "dots"
	}
	if err := s.store.CreateBoard(ctx, b); err != nil {
		s.fail(w, err, "create board")
		return
	}
	if objs := tpl.Objects(); len(objs) > 0 {
		ops := make([]store.Op, 0, len(objs))
		for i := range objs {
			objs[i].Source = b.Source
			ops = append(ops, store.Op{Op: "put", Obj: &objs[i]})
		}
		if _, err := s.store.ApplyOps(ctx, b.ID, ops, &u.ID, s.actorName(u), ""); err != nil {
			s.fail(w, err, "create board")
			return
		}
	}
	if len(mine) > 0 {
		if err := s.copyObjects(ctx, u.ID, b.ID, mine, nil); err != nil {
			s.fail(w, err, "create board")
			return
		}
	}
	if in.FolderID > 0 {
		_ = s.store.SetFolder(ctx, u.ID, b.ID, in.FolderID)
	}
	b, _ = s.store.GetBoard(ctx, b.ID)
	s.publish(ctx, u.ID, "whiteboard.board_created", b.Title, map[string]any{"board_id": b.ID, "title": b.Title, "template": b.Template, "url": s.boardURL(b)})
	writeJSON(w, http.StatusCreated, map[string]any{"board": boardOut(b, &actor{b: b, u: u, role: "owner"})})
}

func (s *Server) boardURL(b *store.Board) string { return s.link("/b/" + itoa(b.ID)) }

// boardDoc is everything a page needs to show a board.
func (s *Server) boardDoc(ctx context.Context, a *actor) (map[string]any, error) {
	objs, err := s.store.ObjectsFor(ctx, a.b.ID, a.voter())
	if err != nil {
		return nil, err
	}
	reactions, _ := s.store.Reactions(ctx, a.b.ID, "")
	counts, _ := s.store.PrivateCounts(ctx, a.b.ID)
	out := map[string]any{"board": boardOut(a.b, a), "objects": objs, "people": s.hub.people(a.b.ID), "reactions": reactions, "private_counts": counts,
		"me":    map[string]any{"role": a.role, "can_edit": a.canEdit(), "voter": a.voter(), "name": a.name, "colour": a.colour(), "guest": a.u == nil},
		"votes": s.voteState(ctx, a)}
	if a.u != nil {
		members, _ := s.store.Members(ctx, a.b.ID)
		out["members"] = members
		out["starred"] = s.store.Starred(ctx, a.u.ID, a.b.ID)
		if a.owner() {
			out["share_url"] = s.link("/s/" + a.b.ShareKey)
		}
	}
	return out, nil
}

func (s *Server) handleGetBoard(w http.ResponseWriter, r *http.Request) {
	a, ok := s.boardParam(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	doc, err := s.boardDoc(ctx, a)
	if err != nil {
		s.fail(w, err, "board")
		return
	}
	if linkApp(ctx) == "" {
		_ = s.store.Mark(ctx, a.u.ID, a.b.ID, nil, true)
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) handleUpdateBoard(w http.ResponseWriter, r *http.Request) {
	a, ok := s.editorParam(w, r)
	if !ok {
		return
	}
	var in struct {
		Title      string `json:"title"`
		About      string `json:"about"`
		Background string `json:"background"`
		ShareMode  string `json:"share_mode"`
	}
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	b := a.b
	if keys["title"] {
		if t := cleanText(in.Title, maxName); t != "" {
			b.Title = t
		}
	}
	if keys["about"] {
		b.About = cleanText(in.About, maxLine)
	}
	if keys["background"] && oneOf(in.Background, backgrounds) {
		b.Background = in.Background
	}
	if keys["share_mode"] {
		if !a.owner() {
			writeErr(w, http.StatusForbidden, "only "+firstNonEmpty(b.OwnerName, "its maker")+" can share the board")
			return
		}
		if !oneOf(in.ShareMode, []string{"off", "view", "edit"}) {
			writeErr(w, http.StatusBadRequest, "the link is off, view or edit")
			return
		}
		b.ShareMode = in.ShareMode
	}
	if err := s.store.UpdateBoard(r.Context(), b); err != nil {
		s.fail(w, err, "save board")
		return
	}
	s.tellBoard(a, r.Header.Get("X-Client"))
	writeJSON(w, http.StatusOK, map[string]any{"board": boardOut(b, a)})
}

func (s *Server) handleDeleteBoard(w http.ResponseWriter, r *http.Request) {
	a, ok := s.ownerParam(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteBoard(r.Context(), a.b.ID); err != nil {
		s.fail(w, err, "delete board")
		return
	}
	s.hub.send(a.b.ID, "", map[string]any{"type": "deleted"})
	w.WriteHeader(http.StatusNoContent)
}

// handleDuplicate makes a copy of a board for the person asking (pictures
// copied too, so the copy stands on its own).
func (s *Server) handleDuplicate(w http.ResponseWriter, r *http.Request) {
	a, ok := s.boardParam(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	var in struct {
		Title string `json:"title"`
	}
	_ = decode(r, &in)
	nb := &store.Board{UserID: a.u.ID, Title: firstNonEmpty(cleanText(in.Title, maxName), clip("Copy of "+a.b.Title, maxName)), About: a.b.About, Template: a.b.Template, Background: a.b.Background}
	if err := s.store.CreateBoard(ctx, nb); err != nil {
		s.fail(w, err, "copy board")
		return
	}
	objs, _ := s.store.Objects(ctx, a.b.ID)
	if err := s.copyObjects(ctx, a.u.ID, nb.ID, objs, nil); err != nil {
		s.fail(w, err, "copy board")
		return
	}
	nb, _ = s.store.GetBoard(ctx, nb.ID)
	writeJSON(w, http.StatusCreated, map[string]any{"board": boardOut(nb, &actor{b: nb, u: a.u, role: "owner"})})
}

// copyObjects puts objects on another board, copying the pictures they
// show (files maps old upload ids to new ones when they were made already).
func (s *Server) copyObjects(ctx context.Context, uid, bid int64, objs []store.Object, files map[int64]int64) error {
	if files == nil {
		files = map[int64]int64{}
	}
	ops := make([]store.Op, 0, len(objs))
	for i := range objs {
		o := objs[i]
		if o.Kind == "image" {
			var d map[string]any
			_ = json.Unmarshal(o.Data, &d)
			old := int64(num(d, "upload"))
			if old > 0 {
				nid, done := files[old]
				if !done {
					if up, err := s.store.GetUpload(ctx, old); err == nil {
						cp := *up
						cp.UserID, cp.BoardID = uid, bid
						if err := s.store.SaveUpload(ctx, &cp); err == nil {
							nid = cp.ID
							files[old] = nid
						}
					}
				}
				if nid > 0 {
					d["upload"] = nid
					d["src"] = "/api/v1/uploads/" + itoa(nid)
					o.Data = mustJSON(d)
				}
			}
		}
		ops = append(ops, store.Op{Op: "put", Obj: &o})
	}
	if len(ops) == 0 {
		return nil
	}
	_, err := s.store.ApplyOps(ctx, bid, ops, &uid, "", "")
	return err
}

func (s *Server) handleStar(w http.ResponseWriter, r *http.Request) {
	a, ok := s.boardParam(w, r)
	if !ok {
		return
	}
	var in struct {
		Starred bool `json:"starred"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.store.Mark(r.Context(), a.u.ID, a.b.ID, &in.Starred, false); err != nil {
		s.fail(w, err, "star")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"starred": in.Starred})
}

// handleNewShareKey makes a new share link; the old one stops working.
func (s *Server) handleNewShareKey(w http.ResponseWriter, r *http.Request) {
	a, ok := s.ownerParam(w, r)
	if !ok {
		return
	}
	a.b.ShareKey = store.RandomToken(12)
	if err := s.store.UpdateBoard(r.Context(), a.b); err != nil {
		s.fail(w, err, "share link")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"share_key": a.b.ShareKey, "share_url": s.link("/s/" + a.b.ShareKey)})
}

// ---- people on a board ----

func (s *Server) handlePeople(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := userFrom(ctx)
	users, err := s.store.ListUsers(ctx)
	if err != nil {
		s.fail(w, err, "people")
		return
	}
	out := []map[string]any{}
	for _, u := range users {
		if u.ID == me.ID || u.Email == "" || strings.HasPrefix(u.Email, "system@") {
			continue
		}
		out = append(out, map[string]any{"id": u.ID, "name": firstNonEmpty(u.Name, u.Email), "email": u.Email})
	}
	writeJSON(w, http.StatusOK, map[string]any{"people": out})
}

func (s *Server) handleAddMember(w http.ResponseWriter, r *http.Request) {
	a, ok := s.ownerParam(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	var in struct {
		Email  string `json:"email"`
		UserID int64  `json:"user_id"`
		Role   string `json:"role"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	role := firstNonEmpty(in.Role, "editor")
	if !oneOf(role, []string{"editor", "viewer"}) {
		writeErr(w, http.StatusBadRequest, "people can change the board (editor) or look at it (viewer)")
		return
	}
	var who *store.User
	var err error
	if in.UserID > 0 {
		who, err = s.store.GetUser(ctx, in.UserID)
	} else {
		who, err = s.store.GetUserByEmail(ctx, strings.ToLower(strings.TrimSpace(in.Email)))
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, "there is nobody with that email on this box — for others, switch on the share link")
		return
	}
	if who.ID == a.b.UserID {
		writeErr(w, http.StatusBadRequest, "that is the board's maker")
		return
	}
	isNew := s.store.Membership(ctx, a.b.ID, who.ID) == ""
	if err := s.store.SetMember(ctx, a.b.ID, who.ID, role); err != nil {
		s.fail(w, err, "add person")
		return
	}
	if isNew {
		if st := s.settingsOf(ctx, who.ID); st.NotifyShared {
			s.push(ctx, who.ID, st, a.name+" put you on a board", a.b.Title, "/b/"+itoa(a.b.ID))
		}
		s.publish(ctx, a.u.ID, "whiteboard.shared", a.b.Title, map[string]any{"board_id": a.b.ID, "title": a.b.Title, "with": who.Email, "role": role, "url": s.boardURL(a.b)})
	}
	members, _ := s.store.Members(ctx, a.b.ID)
	writeJSON(w, http.StatusOK, map[string]any{"members": members})
}

// handleRemoveMember: the maker takes someone off, or someone leaves.
func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	a, ok := s.boardParam(w, r)
	if !ok {
		return
	}
	uid := idParam(r, "uid")
	if !a.owner() && uid != a.u.ID {
		writeErr(w, http.StatusForbidden, "only "+firstNonEmpty(a.b.OwnerName, "its maker")+" can do that")
		return
	}
	if err := s.store.RemoveMember(r.Context(), a.b.ID, uid); err != nil {
		s.fail(w, err, "remove person")
		return
	}
	members, _ := s.store.Members(r.Context(), a.b.ID)
	writeJSON(w, http.StatusOK, map[string]any{"members": members})
}

// ---- changes ----

// cleanOp checks one change.
func cleanOp(op *store.Op) string {
	switch op.Op {
	case "del":
		if !reObjID.MatchString(op.ID) {
			return "bad id"
		}
		return ""
	case "put":
		o := op.Obj
		if o == nil || !reObjID.MatchString(o.ID) {
			return "bad id"
		}
		op.ID = o.ID
		if !oneOf(o.Kind, store.Kinds) {
			return "unknown kind of thing: " + o.Kind
		}
		if !finite(o.X) || !finite(o.Y) || !finite(o.W) || !finite(o.H) || !finite(o.Z) || o.W < 0 || o.H < 0 {
			return "bad position or size"
		}
		if len(o.Data) > maxObjectData {
			return "that is too big for one thing on the board"
		}
		if len(o.Data) == 0 {
			o.Data = json.RawMessage(`{}`)
		}
		var d map[string]any
		if json.Unmarshal(o.Data, &d) != nil {
			return "bad data"
		}
		if t, ok := d["text"].(string); ok && len(t) > maxNotes {
			return "that text is too long"
		}
		return ""
	}
	return "a change is put or del"
}

// applyOps saves a batch of changes and passes them on to everyone else on
// the board. While private mode is on, a new note stays with whoever made
// it: only their own tabs hear of it, the others only how many there are.
func (s *Server) applyOps(w http.ResponseWriter, r *http.Request, a *actor) {
	ctx := r.Context()
	var in struct {
		Client string     `json:"client"`
		Ops    []store.Op `json:"ops"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(in.Ops) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"version": a.b.Version})
		return
	}
	if len(in.Ops) > maxOps {
		writeErr(w, http.StatusBadRequest, "too many changes at once")
		return
	}
	app := linkApp(ctx)
	voter := a.voter()
	private := a.b.PrivateOf().On
	hideAs := ""
	if private {
		hideAs = voter
	}
	puts := 0
	hidden := map[string]bool{} // what stays private after this batch
	for i := range in.Ops {
		if msg := cleanOp(&in.Ops[i]); msg != "" {
			writeErr(w, http.StatusBadRequest, msg)
			return
		}
		op := &in.Ops[i]
		cur, err := s.store.GetObject(ctx, a.b.ID, op.ID)
		exists := err == nil
		if exists && cur.HiddenBy != "" && cur.HiddenBy != voter {
			// someone else's private note: not there for this person
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		if op.Op == "put" {
			if !exists {
				puts++
			}
			op.Obj.Source = app
			op.Obj.UpdatedBy = a.name
			if (exists && cur.HiddenBy != "") || (!exists && hideAs != "") {
				hidden[op.ID] = true
				op.Obj.Hidden = true
			}
		} else if exists && cur.HiddenBy != "" {
			hidden[op.ID] = true
		}
		if app != "" && exists && cur.Source != app {
			// a linked app changes only what it put there itself
			writeErr(w, http.StatusForbidden, "this Pocket Core link may change only what "+app+" put on the board")
			return
		}
	}
	if puts > 0 {
		if n := s.objectCount(ctx, a.b.ID); n+puts > maxObjects {
			writeErr(w, http.StatusBadRequest, "a board holds up to 20,000 things — start another board")
			return
		}
	}
	v, err := s.store.ApplyOps(ctx, a.b.ID, in.Ops, a.uid(), a.name, hideAs)
	if err != nil {
		s.fail(w, err, "save changes")
		return
	}
	var open, mine []store.Op
	for _, op := range in.Ops {
		if hidden[op.ID] {
			mine = append(mine, op)
		} else {
			open = append(open, op)
		}
	}
	if len(open) > 0 {
		s.hub.send(a.b.ID, in.Client, map[string]any{"type": "ops", "client": in.Client, "by": a.name, "ops": open, "version": v})
	}
	if len(mine) > 0 {
		s.hub.sendVoter(a.b.ID, voter, in.Client, map[string]any{"type": "ops", "client": in.Client, "by": a.name, "ops": mine, "version": v})
		counts, _ := s.store.PrivateCounts(ctx, a.b.ID)
		s.hub.send(a.b.ID, "", map[string]any{"type": "private_counts", "counts": counts})
	}
	writeJSON(w, http.StatusOK, map[string]any{"version": v})
}

func (s *Server) objectCount(ctx context.Context, bid int64) int {
	var n int
	_ = s.store.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM objects WHERE board_id = ?`, bid).Scan(&n)
	return n
}

func (s *Server) handleOps(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.editorParam(w, r); ok {
		s.applyOps(w, r, a)
	}
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.boardParam(w, r); ok {
		s.stream(w, r, a)
	}
}

func (s *Server) handleCursor(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.boardParam(w, r); ok {
		s.cursor(w, r, a)
	}
}

// handleAddNotes puts sticky notes on a board below what is there (for
// other apps and Pocket AI: no positions to work out).
func (s *Server) handleAddNotes(w http.ResponseWriter, r *http.Request) {
	a, ok := s.editorParam(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	var in struct {
		Notes []string `json:"notes"`
		Color string   `json:"color"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(in.Notes) == 0 || len(in.Notes) > 200 {
		writeErr(w, http.StatusBadRequest, "send 1 to 200 notes")
		return
	}
	colour := in.Color
	if !oneOf(colour, stickyColors) {
		colour = firstNonEmpty(s.settingsOf(ctx, a.u.ID).StickyColor, "light-yellow")
	}
	objs, _ := s.store.Objects(ctx, a.b.ID)
	minX, maxY, z := 0.0, 0.0, 0.0
	for i, o := range objs {
		if i == 0 || o.X < minX {
			minX = o.X
		}
		maxY = math.Max(maxY, o.Y+o.H)
		z = math.Max(z, o.Z)
	}
	ops := []store.Op{}
	for i, t := range in.Notes {
		t = cleanText(t, 2000)
		if t == "" {
			continue
		}
		o := store.Object{ID: "n" + store.RandomToken(6), Kind: "sticky", X: minX + float64(i%6)*220, Y: maxY + 80 + float64(i/6)*220, W: 200, H: 200, Z: z + 1 + float64(i),
			Data: mustJSON(map[string]any{"text": t, "color": colour})}
		ops = append(ops, store.Op{Op: "put", ID: o.ID, Obj: &o})
	}
	app := linkApp(ctx)
	for _, op := range ops {
		op.Obj.Source = app
		op.Obj.UpdatedBy = a.name
	}
	v, err := s.store.ApplyOps(ctx, a.b.ID, ops, a.uid(), a.name, "")
	if err != nil {
		s.fail(w, err, "add notes")
		return
	}
	s.hub.send(a.b.ID, "", map[string]any{"type": "ops", "by": a.name, "ops": ops, "version": v})
	writeJSON(w, http.StatusCreated, map[string]any{"added": len(ops), "version": v, "url": s.boardURL(a.b)})
}

// ---- timer ----

func (s *Server) setTimer(w http.ResponseWriter, r *http.Request, a *actor) {
	var in struct {
		Client  string `json:"client"`
		Seconds int    `json:"seconds"` // 0 stops it
		Add     int    `json:"add"`     // more time on a running timer
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	b := a.b
	switch {
	case in.Add != 0 && b.TimerEnds != "":
		end, err := time.Parse(time.RFC3339Nano, b.TimerEnds)
		if err != nil || end.Before(now()) {
			end = now()
		}
		end = end.Add(time.Duration(clampInt(in.Add, -3600, 3600)) * time.Second)
		b.TimerEnds = end.UTC().Format(time.RFC3339Nano)
		b.TimerSecs = clampInt(b.TimerSecs+in.Add, 1, 6*3600)
	case in.Seconds > 0:
		secs := clampInt(in.Seconds, 5, 6*3600)
		b.TimerSecs = secs
		b.TimerEnds = now().Add(time.Duration(secs) * time.Second).UTC().Format(time.RFC3339Nano)
	default:
		b.TimerEnds, b.TimerSecs = "", 0
	}
	if err := s.store.UpdateBoard(r.Context(), b); err != nil {
		s.fail(w, err, "timer")
		return
	}
	s.tellBoard(a, in.Client)
	writeJSON(w, http.StatusOK, map[string]any{"board": boardOut(b, a)})
}

func (s *Server) handleTimer(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.editorParam(w, r); ok {
		s.setTimer(w, r, a)
	}
}

// now is the clock (tests move it).
var now = time.Now

// timersDone clears timers that ran out a while ago.
func (s *Server) timersDone(ctx context.Context) {
	before := now().Add(-2 * time.Minute).UTC().Format(time.RFC3339Nano)
	_, _ = s.store.DB.ExecContext(ctx, `UPDATE boards SET timer_ends = '', timer_secs = 0 WHERE timer_ends != '' AND timer_ends < ?`, before)
}

// ---- voting ----

func (s *Server) voteState(ctx context.Context, a *actor) map[string]any {
	v := a.b.VoteOf()
	out := map[string]any{"round": v.Round, "open": v.Open, "done": v.Done, "per_person": v.PerPerson, "hide": v.Hide, "counts": map[string]int{}, "mine": map[string]int{}, "voters": 0, "left": 0}
	if v.Round == 0 {
		return out
	}
	all, mine, voters, _ := s.store.Votes(ctx, a.b.ID, v.Round, a.voter())
	used := 0
	for _, n := range mine {
		used += n
	}
	out["mine"], out["voters"], out["left"] = mine, voters, max(0, v.PerPerson-used)
	if !v.Open || !v.Hide {
		out["counts"] = all
	}
	if !v.Open && !v.Done {
		out["counts"] = map[string]int{}
	}
	return out
}

// setVote starts, ends or clears the voting round.
func (s *Server) setVote(w http.ResponseWriter, r *http.Request, a *actor) {
	var in struct {
		Client    string `json:"client"`
		Action    string `json:"action"` // start | end | clear
		PerPerson int    `json:"per_person"`
		Hide      bool   `json:"hide"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	v := a.b.VoteOf()
	switch in.Action {
	case "start":
		v.Round++
		v.Open, v.Done, v.Hide = true, false, in.Hide
		v.PerPerson = clampInt(firstPos(in.PerPerson, 3), 1, 50)
		v.StartedAt, v.StartedBy = store.Now(), a.name
	case "end":
		if !v.Open {
			writeErr(w, http.StatusBadRequest, "there is no vote going on")
			return
		}
		v.Open, v.Done = false, true
	case "clear":
		v.Open, v.Done = false, false
	default:
		writeErr(w, http.StatusBadRequest, "start, end or clear")
		return
	}
	a.b.Vote = mustJSON(v)
	if err := s.store.UpdateBoard(r.Context(), a.b); err != nil {
		s.fail(w, err, "vote")
		return
	}
	if in.Action == "end" && a.u != nil {
		all, _, voters, _ := s.store.Votes(r.Context(), a.b.ID, v.Round, "")
		s.publish(r.Context(), a.b.UserID, "whiteboard.vote_ended", a.b.Title, map[string]any{"board_id": a.b.ID, "title": a.b.Title, "voters": voters, "votes": len(all), "url": s.boardURL(a.b)})
	}
	s.tellBoard(a, in.Client)
	s.hub.send(a.b.ID, "", map[string]any{"type": "votes"})
	writeJSON(w, http.StatusOK, map[string]any{"board": boardOut(a.b, a), "votes": s.voteState(r.Context(), a)})
}

// castVote puts one of the person's dots on a thing (or takes it back).
func (s *Server) castVote(w http.ResponseWriter, r *http.Request, a *actor) {
	ctx := r.Context()
	var in struct {
		Client   string `json:"client"`
		ObjectID string `json:"object_id"`
		Delta    int    `json:"delta"`
	}
	if err := decode(r, &in); err != nil || !reObjID.MatchString(in.ObjectID) {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	v := a.b.VoteOf()
	if !v.Open {
		writeErr(w, http.StatusBadRequest, "there is no vote going on")
		return
	}
	if _, err := s.store.GetObject(ctx, a.b.ID, in.ObjectID); err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	_, mine, _, _ := s.store.Votes(ctx, a.b.ID, v.Round, a.voter())
	used := 0
	for _, n := range mine {
		used += n
	}
	delta := 1
	if in.Delta < 0 {
		delta = -1
		if mine[in.ObjectID] == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"votes": s.voteState(ctx, a)})
			return
		}
	} else if used >= v.PerPerson {
		writeErr(w, http.StatusBadRequest, "you used all your votes — take one back to move it")
		return
	}
	if err := s.store.CastVote(ctx, a.b.ID, v.Round, in.ObjectID, a.voter(), delta); err != nil {
		s.fail(w, err, "vote")
		return
	}
	s.hub.send(a.b.ID, in.Client, map[string]any{"type": "votes"})
	writeJSON(w, http.StatusOK, map[string]any{"votes": s.voteState(ctx, a)})
}

func (s *Server) handleSetVote(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.editorParam(w, r); ok {
		s.setVote(w, r, a)
	}
}

func (s *Server) handleCastVote(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.boardParam(w, r); ok {
		s.castVote(w, r, a)
	}
}

func (s *Server) handleVotes(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.boardParam(w, r); ok {
		writeJSON(w, http.StatusOK, map[string]any{"votes": s.voteState(r.Context(), a)})
	}
}
