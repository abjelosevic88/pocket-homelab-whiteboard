package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-whiteboard/internal/store"
)

// The other Pocket apps. None of it is needed: when an app is not there,
// the page greys the button and says what to do.

func linkTo(sib *siblingConn, path string) string {
	if sib == nil || sib.public == "" {
		return ""
	}
	return strings.TrimRight(sib.public, "/") + path
}

func plainErr(a pocketApp, err error) string {
	if isNotConnected(err) {
		return err.Error()
	}
	return a.name + ": " + err.Error()
}

func (s *Server) appFail(w http.ResponseWriter, a pocketApp, err error) {
	if isNotConnected(err) {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if ue, ok := err.(*userErr); ok {
		writeErr(w, http.StatusBadRequest, ue.msg)
		return
	}
	writeErr(w, http.StatusBadGateway, plainErr(a, err))
}

// handleApps lists how the person reaches each app, in words they can act on.
func (s *Server) handleApps(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := s.clients(ctx, actorID(ctx))
	out := []appState{}
	for _, a := range pocketApps {
		out = append(out, c.status(ctx, a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": out, "core": s.core.Configured()})
}

func (s *Server) sib(w http.ResponseWriter, r *http.Request, a pocketApp, scopes ...string) (*siblingConn, bool) {
	ctx := r.Context()
	sib, err := s.clients(ctx, actorID(ctx)).conn(ctx, a, scopes...)
	if err != nil {
		s.appFail(w, a, err)
		return nil, false
	}
	return sib, true
}

// ---- what is written on a board ----

type note struct {
	ID    string
	Kind  string
	Text  string
	Color string
	Frame string
	X, Y  float64
	Votes int
}

func objText(o store.Object) (text, colour string) {
	var d struct {
		Text  string     `json:"text"`
		Title string     `json:"title"`
		Color string     `json:"color"`
		Fill  string     `json:"fill"`
		Cells [][]string `json:"cells"`
	}
	_ = json.Unmarshal(o.Data, &d)
	if o.Kind == "frame" {
		return strings.TrimSpace(d.Title), d.Fill
	}
	if o.Kind == "table" {
		rows := []string{}
		for _, r := range d.Cells {
			cells := []string{}
			for _, c := range r {
				if c = strings.TrimSpace(c); c != "" {
					cells = append(cells, c)
				}
			}
			if len(cells) > 0 {
				rows = append(rows, strings.Join(cells, " · "))
			}
		}
		return strings.Join(rows, "\n"), ""
	}
	return strings.TrimSpace(d.Text), firstNonEmpty(d.Color, d.Fill)
}

// notesOf is what is written on a board, each with the frame it sits in,
// in reading order (frames left to right, then top to bottom).
func notesOf(objs []store.Object, votes map[string]int) []note {
	type fr struct {
		title      string
		x, y, w, h float64
	}
	frames := []fr{}
	for _, o := range objs {
		if o.Kind == "frame" {
			t, _ := objText(o)
			frames = append(frames, fr{t, o.X, o.Y, o.W, o.H})
		}
	}
	out := []note{}
	for _, o := range objs {
		if o.Kind != "sticky" && o.Kind != "text" && o.Kind != "shape" {
			continue
		}
		t, c := objText(o)
		if t == "" {
			continue
		}
		n := note{ID: o.ID, Kind: o.Kind, Text: t, Color: c, X: o.X, Y: o.Y, Votes: votes[o.ID]}
		cx, cy := o.X+o.W/2, o.Y+o.H/2
		best := 0.0
		for _, f := range frames {
			if cx >= f.x && cx <= f.x+f.w && cy >= f.y && cy <= f.y+f.h && (best == 0 || f.w*f.h < best) {
				n.Frame, best = f.title, f.w*f.h
			}
		}
		out = append(out, n)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Frame != out[j].Frame {
			return out[i].Frame < out[j].Frame
		}
		if dy := out[i].Y - out[j].Y; dy < -40 || dy > 40 {
			return out[i].Y < out[j].Y
		}
		return out[i].X < out[j].X
	})
	return out
}

// boardMarkdown is a board as text: headings per frame, a bullet per note.
func (s *Server) boardMarkdown(ctx context.Context, a *actor) string {
	objs, _ := s.store.Objects(ctx, a.b.ID)
	v := a.b.VoteOf()
	votes := map[string]int{}
	if v.Round > 0 && (v.Done || (v.Open && !v.Hide)) {
		votes, _, _, _ = s.store.Votes(ctx, a.b.ID, v.Round, "")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", a.b.Title)
	if a.b.About != "" {
		b.WriteString(a.b.About + "\n\n")
	}
	fmt.Fprintf(&b, "From [the board in Pocket Whiteboard](%s), %s.\n", s.boardURL(a.b), time.Now().Format("2 January 2006"))
	cur := "\x00"
	for _, n := range notesOf(objs, votes) {
		if n.Frame != cur {
			cur = n.Frame
			b.WriteString("\n## " + firstNonEmpty(n.Frame, "Elsewhere on the board") + "\n\n")
		}
		line := strings.ReplaceAll(n.Text, "\n", " ")
		if n.Votes > 0 {
			line += fmt.Sprintf(" — **%d %s**", n.Votes, plural(n.Votes, "vote", "votes"))
		}
		b.WriteString("- " + line + "\n")
	}
	return b.String()
}

// ---- Pocket Tasks ----

func (s *Server) handleToTasks(w http.ResponseWriter, r *http.Request) {
	a, ok := s.boardParam(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	var in struct {
		IDs []string `json:"ids"`
		Due string   `json:"due"`
	}
	if err := decode(r, &in); err != nil || len(in.IDs) == 0 || len(in.IDs) > 100 {
		writeErr(w, http.StatusBadRequest, "pick 1 to 100 notes")
		return
	}
	if !validDate(in.Due) {
		writeErr(w, http.StatusBadRequest, "the date is YYYY-MM-DD")
		return
	}
	sib, ok := s.sib(w, r, appTasks, scopeTasksWrite)
	if !ok {
		return
	}
	n, last := 0, int64(0)
	for _, id := range in.IDs {
		o, err := s.store.GetObject(ctx, a.b.ID, id)
		if err != nil {
			continue
		}
		t, _ := objText(*o)
		if t == "" {
			continue
		}
		title := clip(firstLine(t), 200)
		notes := "From the board “" + a.b.Title + "”."
		if rest := strings.TrimSpace(strings.TrimPrefix(t, firstLine(t))); rest != "" {
			notes = rest + "\n\n" + notes
		}
		task, err := sib.client.CreateTask(ctx, title, notes, in.Due, s.boardURL(a.b)+"?o="+url.QueryEscape(id))
		if err != nil {
			s.appFail(w, appTasks, err)
			return
		}
		n++
		last = task.ID
	}
	if n == 0 {
		writeErr(w, http.StatusBadRequest, "there is nothing written on those")
		return
	}
	link := linkTo(sib, "/tasks/"+strconv.FormatInt(last, 10))
	if n > 1 {
		link = linkTo(sib, "/")
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "added": n, "text": fmt.Sprintf("%d %s added to Pocket Tasks", n, plural(n, "task", "tasks")), "url": link})
}

// ---- Pocket Notes ----

func (s *Server) handleToNotes(w http.ResponseWriter, r *http.Request) {
	a, ok := s.boardParam(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	sib, ok := s.sib(w, r, appNotes, scopeNotesWrite)
	if !ok {
		return
	}
	folder := firstNonEmpty(s.settingsOf(ctx, a.u.ID).NotesFolder, "Whiteboards")
	res, err := postJSONTo(ctx, sib, "/api/v1/notes", map[string]any{"title": clip(a.b.Title, 200), "content": s.boardMarkdown(ctx, a), "category": folder})
	if err != nil {
		s.appFail(w, appNotes, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "url": linkTo(sib, "/notes/"+idOf(res)), "text": "Kept in Pocket Notes"})
}

// ---- Pocket Chat ----

func (s *Server) handleChats(w http.ResponseWriter, r *http.Request) {
	sib, ok := s.sib(w, r, appChat, scopeChatRead)
	if !ok {
		return
	}
	v, err := getJSONFrom(r.Context(), sib, "/api/v1/conversations", nil)
	if err != nil {
		s.appFail(w, appChat, err)
		return
	}
	out := []map[string]any{}
	for _, m := range rows(v, "conversations") {
		if m["archived"] == true {
			continue
		}
		out = append(out, map[string]any{"id": idOf(m), "name": firstNonEmpty(str(m, "name"), str(m, "title")), "kind": str(m, "kind"), "emoji": str(m, "emoji")})
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversations": out})
}

// handleShareChat sends a board's link to a chat as a card.
func (s *Server) handleShareChat(w http.ResponseWriter, r *http.Request) {
	a, ok := s.boardParam(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	var in struct {
		To   string `json:"to"`
		Text string `json:"text"`
	}
	_ = decode(r, &in)
	sib, ok := s.sib(w, r, appChat, scopeChatWrite)
	if !ok {
		return
	}
	objs, _ := s.store.Objects(ctx, a.b.ID)
	stickies := 0
	for _, o := range objs {
		if o.Kind == "sticky" {
			stickies++
		}
	}
	card := map[string]any{"title": a.b.Title, "subtitle": fmt.Sprintf("%d sticky %s", stickies, plural(stickies, "note", "notes")),
		"lines": []string{clip(firstNonEmpty(a.b.About, "Open the board and add your notes"), 160)}, "icon": "🟨", "url": s.boardURL(a.b), "app": "whiteboard", "kind": "card"}
	st := s.settingsOf(ctx, a.u.ID)
	body := map[string]any{"to": firstNonEmpty(strings.TrimSpace(in.To), st.ChatTo, "me"), "text": clip(firstNonEmpty(strings.TrimSpace(in.Text), "Join me on this board."), 2000), "card": card,
		"client_id": fmt.Sprintf("whiteboard-%d-%d-%d", a.u.ID, a.b.ID, now().Unix()/30)}
	res, err := postJSONTo(ctx, sib, "/api/v1/send", body)
	if err != nil {
		s.appFail(w, appChat, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "url": linkTo(sib, "/c/"+str(res, "conversation_id")), "text": "Sent to Pocket Chat"})
}

// ---- Pocket Files ----

// handleToFiles keeps a picture of a board (made by the page) in Pocket Files.
func (s *Server) handleToFiles(w http.ResponseWriter, r *http.Request) {
	a, ok := s.boardParam(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	r.Body = http.MaxBytesReader(w, r.Body, 40<<20)
	if err := r.ParseMultipartForm(40 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "the picture is too big")
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "no picture")
		return
	}
	data, _ := io.ReadAll(io.LimitReader(f, 40<<20))
	f.Close()
	if http.DetectContentType(data) != "image/png" {
		writeErr(w, http.StatusBadRequest, "a PNG picture only")
		return
	}
	sib, ok := s.sib(w, r, appFiles, scopeFilesWrite)
	if !ok {
		return
	}
	root := strings.Trim(firstNonEmpty(s.settingsOf(ctx, a.u.ID).FilesFolder, "Whiteboards"), "/")
	name := root + "/" + fileSafe(a.b.Title) + " " + time.Now().Format("2006-01-02") + ".png"
	var res map[string]any
	q := url.Values{"space": {"home"}, "path": {"/"}, "name": {name}, "conflict": {"rename"}, "mtime": {strconv.FormatInt(time.Now().UnixMilli(), 10)}}
	if err := sib.client.PutRaw(ctx, "/api/v1/fs/upload", q, data, "image/png", &res); err != nil {
		s.appFail(w, appFiles, err)
		return
	}
	path := firstNonEmpty(str(res, "path"), "/"+name)
	dir := path
	if i := strings.LastIndex(dir, "/"); i > 0 {
		dir = dir[:i]
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "text": "Kept in Pocket Files", "url": linkTo(sib, "/f/home"+dir)})
}

func fileSafe(v string) string {
	return firstNonEmpty(strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < 32 {
			return '-'
		}
		return r
	}, clip(strings.TrimSpace(v), 90)), "board")
}
