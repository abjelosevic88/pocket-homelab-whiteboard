package server

import (
	"fmt"
	"net/http"

	"github.com/abjelosevic88/pocket-homelab-whiteboard/internal/store"
)

// Templates are boards to start from: frames to fill, a heading and a line
// on how to use it. What they put on the board is ordinary — move it,
// delete it, add to it.

type template struct {
	Key     string                `json:"key"`
	Title   string                `json:"title"`
	About   string                `json:"about"`
	Emoji   string                `json:"emoji"`
	Objects func() []store.Object `json:"-"`
}

type tplBuilder struct {
	objs []store.Object
	n    int
}

func (t *tplBuilder) id() string { t.n++; return fmt.Sprintf("t%03d", t.n) }

func (t *tplBuilder) frame(x, y, w, h float64, title, fill string) string {
	id := t.id()
	t.objs = append(t.objs, store.Object{ID: id, Kind: "frame", X: x, Y: y, W: w, H: h, Z: -1000 + float64(t.n), Data: mustJSON(map[string]any{"title": title, "fill": fill})})
	return id
}

func (t *tplBuilder) text(x, y, w float64, text string, size int, bold bool) string {
	id := t.id()
	t.objs = append(t.objs, store.Object{ID: id, Kind: "text", X: x, Y: y, W: w, H: float64(size) * 1.5, Z: float64(t.n), Data: mustJSON(map[string]any{"text": text, "size": size, "bold": bold})})
	return id
}

func (t *tplBuilder) sticky(x, y float64, text, colour string) string {
	id := t.id()
	t.objs = append(t.objs, store.Object{ID: id, Kind: "sticky", X: x, Y: y, W: 200, H: 200, Z: float64(t.n), Data: mustJSON(map[string]any{"text": text, "color": colour})})
	return id
}

func (t *tplBuilder) shape(x, y, w, h float64, form, text, fill string) string {
	id := t.id()
	t.objs = append(t.objs, store.Object{ID: id, Kind: "shape", X: x, Y: y, W: w, H: h, Z: float64(t.n), Data: mustJSON(map[string]any{"shape": form, "text": text, "fill": fill, "size": 22})})
	return id
}

// line joins two things (the page works the ends out from where they are).
func (t *tplBuilder) line(from, to string, x1, y1, x2, y2 float64) string {
	id := t.id()
	minX, minY := min(x1, x2), min(y1, y2)
	t.objs = append(t.objs, store.Object{ID: id, Kind: "line", X: minX, Y: minY, W: max(x1, x2) - minX, H: max(y1, y2) - minY, Z: float64(t.n),
		Data: mustJSON(map[string]any{"x1": x1, "y1": y1, "x2": x2, "y2": y2, "from": from, "to": to, "end": "arrow", "curve": "curve"})})
	return id
}

func columns(title, hint string, cols []struct{ name, fill string }, w, h float64) func() []store.Object {
	return func() []store.Object {
		t := &tplBuilder{}
		t.text(0, -150, 1200, title, 48, true)
		t.text(0, -80, 1400, hint, 20, false)
		for i, c := range cols {
			t.frame(float64(i)*(w+60), 0, w, h, c.name, c.fill)
		}
		return t.objs
	}
}

type col = struct{ name, fill string }

var templates = []template{
	{Key: "blank", Title: "Untitled board", About: "An empty board without edges.", Emoji: "⬜", Objects: func() []store.Object { return nil }},
	{Key: "retro", Title: "Retrospective", About: "What went well, what could be better, what we do next — then vote on what matters most.", Emoji: "🔁",
		Objects: columns("Retrospective", "Everyone adds notes on their own for 5 minutes (start the timer), then read them out, group them and vote.",
			[]col{{"What went well", "green"}, {"What could be better", "pink"}, {"What we do next", "blue"}}, 640, 900)},
	{Key: "startstop", Title: "Start, stop, continue", About: "Three columns: what to start doing, stop doing and keep doing.", Emoji: "🚦",
		Objects: columns("Start, stop, continue", "One idea per note. Group the ones that say the same, then vote.",
			[]col{{"Start", "green"}, {"Stop", "red"}, {"Continue", "blue"}}, 560, 860)},
	{Key: "brainstorm", Title: "Brainstorm", About: "A question in the middle and room for every idea around it.", Emoji: "💡",
		Objects: func() []store.Object {
			t := &tplBuilder{}
			t.text(-500, -620, 1000, "Brainstorm", 48, true)
			t.text(-500, -550, 1200, "Press N (or the note button) for a new note. Quantity first — sort them out later.", 20, false)
			t.shape(-200, -110, 400, 220, "ellipse", "What is our question?", "yellow")
			for i, c := range []string{"light-yellow", "orange", "light-pink", "light-blue", "light-green", "violet"} {
				x := []float64{-620, -100, 420, -620, -100, 420}[i]
				y := []float64{-420, -420, -420, 200, 200, 200}[i]
				t.sticky(x, y, "", c)
			}
			return t.objs
		}},
	{Key: "kanban", Title: "Kanban", About: "To do, doing, done: move the notes along as the work moves.", Emoji: "🗂️",
		Objects: func() []store.Object {
			t := &tplBuilder{}
			t.text(0, -150, 1200, "Kanban", 48, true)
			t.text(0, -80, 1400, "Drag a note into the next column when it moves on. Keep “Doing” short.", 20, false)
			for i, c := range []col{{"To do", "gray"}, {"Doing", "yellow"}, {"Done", "green"}} {
				t.frame(float64(i)*520, 0, 460, 1000, c.name, c.fill)
			}
			t.sticky(30, 60, "Write the first task here", "light-yellow")
			return t.objs
		}},
	{Key: "swot", Title: "SWOT", About: "Strengths, weaknesses, opportunities and threats in four squares.", Emoji: "🧭",
		Objects: func() []store.Object {
			t := &tplBuilder{}
			t.text(0, -150, 1200, "SWOT", 48, true)
			t.text(0, -80, 1400, "Inside (strengths, weaknesses) on top; outside (opportunities, threats) below.", 20, false)
			for i, c := range []col{{"Strengths", "green"}, {"Weaknesses", "orange"}, {"Opportunities", "blue"}, {"Threats", "red"}} {
				t.frame(float64(i%2)*760, float64(i/2)*660, 700, 600, c.name, c.fill)
			}
			return t.objs
		}},
	{Key: "week", Title: "Week plan", About: "A column for each day of the week and one for later.", Emoji: "📅",
		Objects: columns("Week plan", "One note per thing to do; move what does not fit into “Later”.",
			[]col{{"Monday", "blue"}, {"Tuesday", "blue"}, {"Wednesday", "blue"}, {"Thursday", "blue"}, {"Friday", "blue"}, {"Weekend", "green"}, {"Later", "gray"}}, 300, 760)},
	{Key: "mindmap", Title: "Mind map", About: "A topic in the middle with branches joined by lines that follow when you move things.", Emoji: "🌳",
		Objects: func() []store.Object {
			t := &tplBuilder{}
			t.text(-400, -520, 800, "Mind map", 48, true)
			c := t.shape(-160, -60, 320, 120, "round", "Main topic", "violet")
			pos := [][2]float64{{-620, -340}, {380, -340}, {-620, 240}, {380, 240}}
			for i, p := range pos {
				b := t.shape(p[0], p[1], 240, 100, "round", fmt.Sprintf("Branch %d", i+1), "white")
				cx, cy := 0.0, 0.0
				bx, by := p[0]+120, p[1]+50
				t.line(c, b, cx, cy, bx, by)
			}
			return t.objs
		}},
	{Key: "journey", Title: "Journey map", About: "Each step of someone's way through something: what they do, think and feel, where it hurts and ideas to fix it.", Emoji: "🗺️",
		Objects: func() []store.Object {
			t := &tplBuilder{}
			t.text(0, -170, 1400, "Journey map", 48, true)
			t.text(0, -100, 1600, "One column per step. Fill the rows from top to bottom; red notes for what goes wrong.", 20, false)
			rowNames := []string{"Doing", "Thinking", "Feeling", "What goes wrong", "Ideas"}
			for i, r := range rowNames {
				t.text(-260, 40+float64(i)*240, 220, r, 24, true)
			}
			for i := 0; i < 5; i++ {
				t.frame(float64(i)*380, 0, 340, 1220, fmt.Sprintf("Step %d", i+1), "gray")
			}
			return t.objs
		}},
}

func templateByKey(k string) (template, bool) {
	for _, t := range templates {
		if t.Key == k {
			return t, true
		}
	}
	return template{}, false
}

func (s *Server) handleTemplates(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	for _, t := range templates {
		objs := t.Objects()
		out = append(out, map[string]any{"key": t.Key, "title": t.Title, "about": t.About, "emoji": t.Emoji, "preview": preview(objs)})
	}
	mine := []map[string]any{}
	if u := userFrom(r.Context()); u != nil {
		list, _ := s.store.Templates(r.Context(), u.ID)
		for _, t := range list {
			mine = append(mine, map[string]any{"key": "u:" + itoa(t.ID), "id": t.ID, "title": t.Title, "about": t.About, "emoji": t.Emoji, "preview": t.Preview, "mine": true, "created_at": t.CreatedAt})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": out, "mine": mine})
}
