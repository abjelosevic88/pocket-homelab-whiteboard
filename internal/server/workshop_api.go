package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/abjelosevic88/pocket-homelab-whiteboard/internal/store"
)

// ---- anonymous boards: nobody sees who wrote what ----

const someone = "Someone"

// anonymise drops who made and changed each thing (for boards in anonymous mode).
func anonymise(objs []store.Object) []store.Object {
	for i := range objs {
		objs[i].Author, objs[i].UpdatedBy, objs[i].UserID = "", "", nil
	}
	return objs
}

func anonOps(ops []store.Op) []store.Op {
	out := make([]store.Op, len(ops))
	for i, op := range ops {
		if op.Obj != nil {
			o := *op.Obj
			o.Author, o.UpdatedBy, o.UserID = "", "", nil
			op.Obj = &o
		}
		out[i] = op
	}
	return out
}

func (s *Server) handleAnonymous(w http.ResponseWriter, r *http.Request) {
	a, ok := s.editorParam(w, r)
	if !ok {
		return
	}
	var in struct {
		On bool `json:"on"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a.b.Anonymous = in.On
	if err := s.store.UpdateBoard(r.Context(), a.b); err != nil {
		s.fail(w, err, "anonymous notes")
		return
	}
	s.tellBoard(a, "")
	// everyone loads the board again, so names go (or come back) at once
	s.hub.send(a.b.ID, "", map[string]any{"type": "reload", "by": a.name, "why": "anonymous"})
	writeJSON(w, http.StatusOK, map[string]any{"board": boardOut(a.b, a)})
}

// ---- planning poker ----

var pokerValues = map[string]bool{"0": true, "½": true, "1": true, "2": true, "3": true, "5": true, "8": true, "13": true, "20": true, "21": true, "40": true, "100": true, "?": true, "☕": true}

// pokerOut is what everyone sees of the round: who has picked, and the picks
// only once they are shown.
func pokerOut(b *store.Board, a *actor) map[string]any {
	p := b.PokerOf()
	if p.Object == "" {
		return nil
	}
	picked := []string{}
	for v := range p.Picks {
		picked = append(picked, p.Names[v])
	}
	sort.Strings(picked)
	out := map[string]any{"object": p.Object, "by": p.By, "started_at": p.Started, "revealed": p.Revealed, "picked": picked}
	if p.Revealed {
		type pick struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}
		res := []pick{}
		for v, val := range p.Picks {
			name := p.Names[v]
			if b.Anonymous {
				name = ""
			}
			res = append(res, pick{name, val})
		}
		sort.Slice(res, func(i, j int) bool {
			return res[i].Value < res[j].Value || (res[i].Value == res[j].Value && res[i].Name < res[j].Name)
		})
		out["picks"] = res
		if avg, n := pokerAverage(p.Picks); n > 0 {
			out["average"] = avg
		}
	}
	if b.Anonymous {
		out["picked_n"] = len(picked)
		out["picked"] = []string{}
	}
	if a != nil {
		out["mine"] = p.Picks[a.voter()]
	}
	return out
}

func pokerAverage(picks map[string]string) (float64, int) {
	sum, n := 0.0, 0
	for _, v := range picks {
		if v == "½" {
			sum += 0.5
			n++
			continue
		}
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			sum += f
			n++
		}
	}
	if n == 0 {
		return 0, 0
	}
	return float64(int(sum/float64(n)*10+0.5)) / 10, n
}

var pokerMu sync.Mutex

// changePoker loads the board afresh, changes its round and tells everyone.
func (s *Server) changePoker(w http.ResponseWriter, r *http.Request, a *actor, change func(p *store.Poker) string) {
	pokerMu.Lock()
	defer pokerMu.Unlock()
	ctx := r.Context()
	b, err := s.store.GetBoard(ctx, a.b.ID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	a.b = b
	p := b.PokerOf()
	if msg := change(&p); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if p.Object == "" {
		b.Poker = json.RawMessage(`{}`)
	} else {
		b.Poker = mustJSON(p)
	}
	if err := s.store.UpdateBoard(ctx, b); err != nil {
		s.fail(w, err, "planning poker")
		return
	}
	s.tellBoard(a, "")
	writeJSON(w, http.StatusOK, map[string]any{"board": boardOut(b, a)})
}

func (s *Server) handlePokerStart(w http.ResponseWriter, r *http.Request) {
	a, ok := s.editorParam(w, r)
	if !ok {
		return
	}
	var in struct {
		Object string `json:"object"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	o, err := s.store.GetObject(r.Context(), a.b.ID, in.Object)
	if err != nil || (o.HiddenBy != "" && o.HiddenBy != a.voter()) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	s.changePoker(w, r, a, func(p *store.Poker) string {
		*p = store.Poker{Object: o.ID, By: a.name, Started: store.Now(), Picks: map[string]string{}, Names: map[string]string{}}
		return ""
	})
}

func (s *Server) pokerPick(w http.ResponseWriter, r *http.Request, a *actor) {
	var in struct {
		Value string `json:"value"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	s.changePoker(w, r, a, func(p *store.Poker) string {
		if p.Object == "" {
			return "there is no round to pick in"
		}
		if p.Revealed {
			return "the picks are already shown"
		}
		if in.Value == "" {
			delete(p.Picks, a.voter())
			return ""
		}
		if !pokerValues[in.Value] {
			return "pick one of the cards"
		}
		p.Picks[a.voter()] = in.Value
		p.Names[a.voter()] = a.name
		return ""
	})
}

func (s *Server) handlePokerPick(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.boardParam(w, r); ok {
		s.pokerPick(w, r, a)
	}
}

func (s *Server) handlePublicPokerPick(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.guestParam(w, r); ok {
		if !s.guestLimit(r) {
			writeErr(w, http.StatusTooManyRequests, "slow down a little")
			return
		}
		s.pokerPick(w, r, a)
	}
}

func (s *Server) handlePokerReveal(w http.ResponseWriter, r *http.Request) {
	a, ok := s.editorParam(w, r)
	if !ok {
		return
	}
	var in struct {
		Revealed bool `json:"revealed"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	s.changePoker(w, r, a, func(p *store.Poker) string {
		if p.Object == "" {
			return "there is no round to show"
		}
		p.Revealed = in.Revealed
		if !in.Revealed {
			// again: a fresh round on the same thing
			p.Picks, p.Names = map[string]string{}, map[string]string{}
		}
		return ""
	})
}

func (s *Server) handlePokerStop(w http.ResponseWriter, r *http.Request) {
	a, ok := s.editorParam(w, r)
	if !ok {
		return
	}
	s.changePoker(w, r, a, func(p *store.Poker) string { *p = store.Poker{}; return "" })
}

// ---- pictures pasted from another board come along ----

// handleAdoptUploads copies files from other boards (that this person may
// see) onto this board, so everyone here can see a pasted picture.
func (s *Server) handleAdoptUploads(w http.ResponseWriter, r *http.Request) {
	a, ok := s.editorParam(w, r)
	if !ok {
		return
	}
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(in.IDs) > 100 {
		in.IDs = in.IDs[:100]
	}
	ctx := r.Context()
	out := map[string]int64{}
	for _, id := range in.IDs {
		up, err := s.store.GetUpload(ctx, id)
		if err != nil {
			continue
		}
		if up.BoardID == a.b.ID {
			out[strconv.FormatInt(id, 10)] = id
			continue
		}
		if !s.mayGetUpload(ctx, a.u, up) {
			continue
		}
		cp := *up
		cp.UserID, cp.BoardID = a.u.ID, a.b.ID
		if err := s.store.SaveUpload(ctx, &cp); err != nil {
			continue
		}
		out[strconv.FormatInt(id, 10)] = cp.ID
	}
	writeJSON(w, http.StatusOK, map[string]any{"ids": out})
}

// ---- cards in Pocket Projects and Pocket Calendar ----

type cardLinks struct {
	Text   string `json:"text"`
	Body   string `json:"body"`
	Due    string `json:"due"`
	Done   bool   `json:"done"`
	Column string `json:"column"`
	Issue  *struct {
		ID         int64  `json:"id"`
		Identifier string `json:"identifier"`
		URL        string `json:"url"`
	} `json:"issue"`
	Event *struct {
		ID  int64  `json:"id"`
		URL string `json:"url"`
	} `json:"event"`
}

func (s *Server) cardFor(w http.ResponseWriter, r *http.Request, a *actor) (*store.Object, cardLinks, bool) {
	o, err := s.store.GetObject(r.Context(), a.b.ID, chiParam(r, "oid"))
	if err != nil || o.Kind != "card" || (o.HiddenBy != "" && o.HiddenBy != a.voter()) {
		writeErr(w, http.StatusNotFound, "not found")
		return nil, cardLinks{}, false
	}
	var d cardLinks
	_ = json.Unmarshal(o.Data, &d)
	if !validDate(d.Due) {
		d.Due = ""
	}
	return o, d, true
}

// saveCardData puts one key into a card's data and tells everyone.
func (s *Server) saveCardData(ctx context.Context, a *actor, o *store.Object, key string, val any) error {
	var m map[string]any
	_ = json.Unmarshal(o.Data, &m)
	if m == nil {
		m = map[string]any{}
	}
	m[key] = val
	o.Data = mustJSON(m)
	ops := []store.Op{{Op: "put", ID: o.ID, Obj: o}}
	v, err := s.store.ApplyOps(ctx, a.b.ID, ops, a.uid(), a.name, "")
	if err != nil {
		return err
	}
	s.hub.send(a.b.ID, "", map[string]any{"type": "ops", "by": a.name, "ops": ops, "version": v})
	return nil
}

// issueState is the Pocket Projects state for a card's Kanban column.
func issueState(d cardLinks) string {
	switch {
	case d.Done || d.Column == "done":
		return "completed"
	case d.Column == "doing":
		return "started"
	case d.Column == "todo":
		return "unstarted"
	}
	return ""
}

func (s *Server) handleProjectTeams(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.editorParam(w, r); !ok {
		return
	}
	sib, ok := s.sib(w, r, appProjects, scopeProjectsRead)
	if !ok {
		return
	}
	var raw json.RawMessage
	if err := sib.client.Do(r.Context(), http.MethodGet, "/api/v1/teams", nil, nil, &raw); err != nil {
		s.appFail(w, appProjects, err)
		return
	}
	type team struct {
		ID   int64  `json:"id"`
		Key  string `json:"key"`
		Name string `json:"name"`
	}
	var teams []team
	if json.Unmarshal(raw, &teams) != nil {
		var wrap struct {
			Teams []team `json:"teams"`
		}
		_ = json.Unmarshal(raw, &wrap)
		teams = wrap.Teams
	}
	if teams == nil {
		teams = []team{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"teams": teams})
}

// handleCardIssue makes a card an issue in Pocket Projects, or sends the
// card's title, due date and column to the issue it already has.
func (s *Server) handleCardIssue(w http.ResponseWriter, r *http.Request) {
	a, ok := s.editorParam(w, r)
	if !ok {
		return
	}
	var in struct {
		Team string `json:"team"`
	}
	_ = decode(r, &in)
	o, d, ok := s.cardFor(w, r, a)
	if !ok {
		return
	}
	sib, ok := s.sib(w, r, appProjects, scopeProjectsWrite)
	if !ok {
		return
	}
	ctx := r.Context()
	title := clip(firstNonEmpty(firstLine(d.Text), "Card"), 200)
	link := s.boardURL(a.b) + "?o=" + url.QueryEscape(o.ID)
	body := map[string]any{"title": title}
	if d.Due != "" {
		body["due_date"] = d.Due
	}
	if st := issueState(d); st != "" {
		body["state"] = st
	}
	if d.Issue == nil || d.Issue.ID == 0 {
		if strings.TrimSpace(in.Team) == "" {
			writeErr(w, http.StatusBadRequest, "pick a team")
			return
		}
		body["team"] = strings.TrimSpace(in.Team)
		body["description"] = firstNonEmpty(strings.TrimSpace(d.Body), "A card on the board “"+a.b.Title+"”.") + "\n\n" + link
		body["source_url"] = link
		var is struct {
			ID         int64  `json:"id"`
			Identifier string `json:"identifier"`
		}
		var raw json.RawMessage
		if err := sib.client.Do(ctx, http.MethodPost, "/api/v1/issues", nil, body, &raw); err != nil {
			s.appFail(w, appProjects, err)
			return
		}
		if json.Unmarshal(raw, &is) != nil || is.ID == 0 {
			var wrap struct {
				Issue json.RawMessage `json:"issue"`
			}
			_ = json.Unmarshal(raw, &wrap)
			_ = json.Unmarshal(wrap.Issue, &is)
		}
		u := linkTo(sib, "/issue/"+is.Identifier)
		if err := s.saveCardData(ctx, a, o, "issue", map[string]any{"id": is.ID, "identifier": is.Identifier, "url": u}); err != nil {
			s.fail(w, err, "card")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": o, "text": "Now " + firstNonEmpty(is.Identifier, "an issue") + " in Pocket Projects", "url": u})
		return
	}
	ref := firstNonEmpty(d.Issue.Identifier, strconv.FormatInt(d.Issue.ID, 10))
	if err := sib.client.Do(ctx, http.MethodPatch, "/api/v1/issues/"+url.PathEscape(ref), nil, body, nil); err != nil {
		s.appFail(w, appProjects, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": o, "text": "Pocket Projects is up to date", "url": d.Issue.URL})
}

// handleCardEvent puts a card's due date on the calendar (or moves the event
// it already has).
func (s *Server) handleCardEvent(w http.ResponseWriter, r *http.Request) {
	a, ok := s.editorParam(w, r)
	if !ok {
		return
	}
	o, d, ok := s.cardFor(w, r, a)
	if !ok {
		return
	}
	if d.Due == "" {
		writeErr(w, http.StatusBadRequest, "give the card a due date first")
		return
	}
	sib, ok := s.sib(w, r, appCalendar, scopeCalendarWrite)
	if !ok {
		return
	}
	ctx := r.Context()
	title := clip(firstNonEmpty(firstLine(d.Text), "Card"), 200)
	link := s.boardURL(a.b) + "?o=" + url.QueryEscape(o.ID)
	body := map[string]any{"title": title, "all_day": true, "start": d.Due, "end": d.Due, "url": link,
		"description": firstNonEmpty(strings.TrimSpace(d.Body), "A card on the board “"+a.b.Title+"”.")}
	if d.Event != nil && d.Event.ID > 0 {
		if err := sib.client.Do(ctx, http.MethodPut, fmt.Sprintf("/api/v1/events/%d", d.Event.ID), nil, body, nil); err == nil {
			writeJSON(w, http.StatusOK, map[string]any{"object": o, "text": "The calendar is up to date", "url": d.Event.URL})
			return
		} else if !strings.Contains(err.Error(), "404") {
			s.appFail(w, appCalendar, err)
			return
		}
		// the event was deleted there: make it again
	}
	var raw json.RawMessage
	if err := sib.client.Do(ctx, http.MethodPost, "/api/v1/events", nil, body, &raw); err != nil {
		s.appFail(w, appCalendar, err)
		return
	}
	var ev struct {
		ID int64 `json:"id"`
	}
	if json.Unmarshal(raw, &ev) != nil || ev.ID == 0 {
		var wrap struct {
			Event json.RawMessage `json:"event"`
		}
		_ = json.Unmarshal(raw, &wrap)
		_ = json.Unmarshal(wrap.Event, &ev)
	}
	u := linkTo(sib, "/event/"+strconv.FormatInt(ev.ID, 10))
	if err := s.saveCardData(ctx, a, o, "event", map[string]any{"id": ev.ID, "url": u}); err != nil {
		s.fail(w, err, "card")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": o, "text": "On your calendar on " + d.Due, "url": u})
}
