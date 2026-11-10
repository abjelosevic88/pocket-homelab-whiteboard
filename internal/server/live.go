package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Live boards: every open board keeps one event stream. Changes, pointers,
// the timer and the vote reach everyone on that board straight away; the
// sender knows its own changes by its client id and skips them.

type sub struct {
	ch     chan []byte
	client string
	name   string
	colour string
	voter  string
	guest  bool
	since  time.Time
}

type hub struct {
	mu   sync.Mutex
	subs map[int64]map[*sub]bool
}

func newHub() *hub { return &hub{subs: map[int64]map[*sub]bool{}} }

func (h *hub) join(bid int64, sb *sub) {
	h.mu.Lock()
	if h.subs[bid] == nil {
		h.subs[bid] = map[*sub]bool{}
	}
	h.subs[bid][sb] = true
	h.mu.Unlock()
}

func (h *hub) leave(bid int64, sb *sub) {
	h.mu.Lock()
	delete(h.subs[bid], sb)
	if len(h.subs[bid]) == 0 {
		delete(h.subs, bid)
	}
	h.mu.Unlock()
}

// person is someone on a board right now (one per voter, however many tabs).
type person struct {
	Client string `json:"client"`
	Name   string `json:"name"`
	Colour string `json:"colour"`
	Voter  string `json:"voter"`
	Guest  bool   `json:"guest"`
}

func (h *hub) people(bid int64) []person {
	h.mu.Lock()
	defer h.mu.Unlock()
	seen := map[string]bool{}
	subs := make([]*sub, 0, len(h.subs[bid]))
	for sb := range h.subs[bid] {
		subs = append(subs, sb)
	}
	sort.Slice(subs, func(i, j int) bool { return subs[i].since.Before(subs[j].since) })
	out := []person{}
	for _, sb := range subs {
		if seen[sb.voter] {
			continue
		}
		seen[sb.voter] = true
		out = append(out, person{Client: sb.client, Name: sb.name, Colour: sb.colour, Voter: sb.voter, Guest: sb.guest})
	}
	return out
}

// count is how many tabs have the board open.
func (h *hub) count(bid int64) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs[bid])
}

// send delivers an event to everyone on a board but the sending client (a
// full buffer drops it: that page catches up when it reconnects).
func (h *hub) send(bid int64, except string, ev map[string]any) {
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for sb := range h.subs[bid] {
		if except != "" && sb.client == except {
			continue
		}
		select {
		case sb.ch <- b:
		default:
		}
	}
}

// sendVoter delivers an event only to one person's tabs (their private notes).
func (h *hub) sendVoter(bid int64, voter, except string, ev map[string]any) {
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for sb := range h.subs[bid] {
		if sb.voter != voter || (except != "" && sb.client == except) {
			continue
		}
		select {
		case sb.ch <- b:
		default:
		}
	}
}

// stream keeps one board's event stream open for a person or a guest.
func (s *Server) stream(w http.ResponseWriter, r *http.Request, a *actor) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming is not supported here")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	client := cleanText(r.URL.Query().Get("client"), 40)
	sb := &sub{ch: make(chan []byte, 256), client: client, name: a.name, colour: a.colour(), voter: a.voter(), guest: a.u == nil, since: time.Now()}
	bid := a.b.ID
	s.hub.join(bid, sb)
	s.hub.send(bid, "", map[string]any{"type": "presence", "people": s.hub.people(bid)})
	defer func() {
		s.hub.leave(bid, sb)
		s.hub.send(bid, "", map[string]any{"type": "presence", "people": s.hub.people(bid)})
		s.hub.send(bid, "", map[string]any{"type": "gone", "client": client})
	}()
	hello, _ := json.Marshal(map[string]any{"version": a.b.Version, "people": s.hub.people(bid), "me": person{Client: client, Name: a.name, Colour: a.colour(), Voter: a.voter(), Guest: a.u == nil}})
	fmt.Fprintf(w, "event: hello\ndata: %s\n\n", hello)
	fl.Flush()
	beat := time.NewTicker(25 * time.Second)
	defer beat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case b := <-sb.ch:
			fmt.Fprintf(w, "data: %s\n\n", b)
			fl.Flush()
		case <-beat.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

// cursor passes a pointer position on to the others on the board.
func (s *Server) cursor(w http.ResponseWriter, r *http.Request, a *actor) {
	var in struct {
		Client string   `json:"client"`
		X      float64  `json:"x"`
		Y      float64  `json:"y"`
		Sel    []string `json:"sel"`
		Gone   bool     `json:"gone"`
		View   *struct {
			X float64 `json:"x"`
			Y float64 `json:"y"`
			W float64 `json:"w"`
			H float64 `json:"h"`
		} `json:"view"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(in.Sel) > 50 {
		in.Sel = in.Sel[:50]
	}
	if !finite(in.X) || !finite(in.Y) {
		writeErr(w, http.StatusBadRequest, "bad position")
		return
	}
	ev := map[string]any{"type": "cursor", "client": in.Client, "x": in.X, "y": in.Y, "sel": in.Sel, "gone": in.Gone, "name": a.name, "colour": a.colour(), "voter": a.voter()}
	if v := in.View; v != nil && finite(v.X) && finite(v.Y) && finite(v.W) && finite(v.H) && v.W > 0 && v.H > 0 {
		ev["view"] = v
	}
	s.hub.send(a.b.ID, in.Client, ev)
	w.WriteHeader(http.StatusNoContent)
}

// tellBoard tells everyone on the board that its settings, timer or vote changed.
func (s *Server) tellBoard(a *actor, except string) {
	s.hub.send(a.b.ID, except, map[string]any{"type": "board", "board": boardOut(a.b, nil), "by": a.name})
}
