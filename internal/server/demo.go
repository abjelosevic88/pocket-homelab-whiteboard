package server

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"math/rand"
	"time"

	"github.com/abjelosevic88/pocket-homelab-whiteboard/internal/store"
)

// seedDemo fills the owner's account with boards people really make: a
// team retrospective with a finished vote, a garden brainstorm with arrows
// and a sketch, a launch plan on a kanban shared with Ana, a flowchart of a
// booking, and Ana's trip mind map that the owner is on.
func (s *Server) seedDemo(ctx context.Context, owner int64) error {
	ctx = context.WithoutCancel(ctx)
	if n, _ := s.store.OwnBoards(ctx, owner); len(n) > 0 {
		return nil
	}
	rng := rand.New(rand.NewSource(42))
	u, err := s.store.GetUser(ctx, owner)
	if err != nil {
		return err
	}
	me := s.actorName(u)
	ana := s.demoUser(ctx, "ana@example.com", "Ana Kovač")
	marko := s.demoUser(ctx, "marko@example.com", "Marko Petrović")
	who := map[int64]string{owner: me, ana: "Ana Kovač", marko: "Marko Petrović"}

	type item struct {
		o  store.Object
		by int64
	}
	put := func(bid int64, items []item) {
		groups := map[int64][]store.Op{}
		for i := range items {
			o := items[i].o
			groups[items[i].by] = append(groups[items[i].by], store.Op{Op: "put", ID: o.ID, Obj: &o})
		}
		for by, ops := range groups {
			id := by
			_, _ = s.store.ApplyOps(ctx, bid, ops, &id, who[by])
		}
	}
	board := func(uid int64, title, about, tpl, bg string, daysAgo int) *store.Board {
		b := &store.Board{UserID: uid, Title: title, About: about, Template: tpl, Background: bg}
		if err := s.store.CreateBoard(ctx, b); err != nil {
			return nil
		}
		return b
	}
	age := func(b *store.Board, d time.Duration) {
		at := time.Now().Add(-d).UTC().Format(time.RFC3339Nano)
		_, _ = s.store.DB.ExecContext(ctx, `UPDATE boards SET created_at = ?, updated_at = ? WHERE id = ?`, time.Now().Add(-d-72*time.Hour).UTC().Format(time.RFC3339Nano), at, b.ID)
		_, _ = s.store.DB.ExecContext(ctx, `UPDATE objects SET created_at = ?, updated_at = ? WHERE board_id = ?`, at, at, b.ID)
	}
	seq := 0
	nid := func() string { seq++; return "d" + itoa(int64(seq)) + store.RandomToken(3) }
	jitter := func(v float64) float64 { return v + (rng.Float64()-0.5)*14 }
	sticky := func(x, y float64, text, c string, by int64) item {
		return item{store.Object{ID: nid(), Kind: "sticky", X: jitter(x), Y: jitter(y), W: 200, H: 200, Z: float64(seq), Data: mustJSON(map[string]any{"text": text, "color": c})}, by}
	}
	tplItems := func(key string, by int64) []item {
		t, _ := templateByKey(key)
		out := []item{}
		for _, o := range t.Objects() {
			o.ID = nid()
			out = append(out, item{o, by})
		}
		return out
	}

	// 1. the retrospective, with a finished vote
	if b := board(owner, "Sprint 42 retrospective", "Two weeks of the new checkout. Everyone adds notes first, then we vote.", "retro", "dots", 0); b != nil {
		items := tplItems("retro", owner)
		well := []string{"Checkout went live without a single rollback", "Pairing on the payment bug saved days", "Design reviews were short and useful", "Customer support loved the new order page", "Daily stand-up under 10 minutes"}
		better := []string{"Too many meetings on Wednesday", "Tests on CI are slow (25 min)", "We found the tax bug late", "Unclear who owns the release notes", "Staging was down twice"}
		next := []string{"Split CI tests in parallel", "One owner for release notes per sprint", "No-meeting Wednesday afternoon", "Add tax cases to the test plan"}
		people := []int64{owner, ana, marko}
		for i, t := range well {
			items = append(items, sticky(30+float64(i%3)*205, 60+float64(i/3)*215, t, "green", people[i%3]))
		}
		for i, t := range better {
			items = append(items, sticky(700+30+float64(i%3)*205, 60+float64(i/3)*215, t, "pink", people[(i+1)%3]))
		}
		for i, t := range next {
			items = append(items, sticky(1400+30+float64(i%3)*205, 60+float64(i/3)*215, t, "blue", people[(i+2)%3]))
		}
		put(b.ID, items)
		_ = s.store.SetMember(ctx, b.ID, ana, "editor")
		_ = s.store.SetMember(ctx, b.ID, marko, "editor")
		v := store.Vote{Round: 1, PerPerson: 3, Done: true, StartedAt: store.Now(), StartedBy: me}
		b.Vote = mustJSON(v)
		_ = s.store.UpdateBoard(ctx, b)
		voteFor := map[string][]int{"Tests on CI are slow (25 min)": {0, 1, 2}, "Split CI tests in parallel": {0, 1, 2}, "Too many meetings on Wednesday": {1, 2},
			"Checkout went live without a single rollback": {0}, "No-meeting Wednesday afternoon": {1}, "We found the tax bug late": {0, 2}}
		for _, it := range items {
			var d struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(it.o.Data, &d)
			for _, p := range voteFor[d.Text] {
				_ = s.store.CastVote(ctx, b.ID, 1, it.o.ID, "u"+itoa(people[p]), 1)
			}
		}
		age(b, 26*time.Hour)
		f := true
		_ = s.store.Mark(ctx, owner, b.ID, &f, true)
	}

	// 2. the garden brainstorm: a question, clusters, arrows, a sketch and a photo
	if b := board(owner, "Garden makeover", "What we want from the back garden this spring.", "brainstorm", "grid", 3); b != nil {
		items := []item{}
		q := item{store.Object{ID: nid(), Kind: "shape", X: -220, Y: -110, W: 440, H: 220, Z: 1, Data: mustJSON(map[string]any{"shape": "ellipse", "text": "What should the garden be for?", "fill": "yellow", "size": 26})}, owner}
		items = append(items, q)
		items = append(items, item{store.Object{ID: nid(), Kind: "text", X: -500, Y: -640, W: 1000, H: 72, Z: 2, Data: mustJSON(map[string]any{"text": "Garden makeover", "size": 48, "bold": true})}, owner})
		clusters := []struct {
			title  string
			fill   string
			x, y   float64
			colour string
			notes  []string
		}{
			{"Eat from it", "green", -1300, -520, "lime", []string{"Raised beds for tomatoes", "Herb spiral by the kitchen door", "Two apple trees", "Strawberries in pots"}},
			{"Play", "orange", 500, -520, "orange", []string{"Swing on the old walnut", "Small lawn for football", "Sandpit with a lid"}},
			{"Rest", "blue", -1300, 260, "blue", []string{"Shade sail over the terrace", "Bench under the pergola", "Outdoor lights on a timer", "Hammock"}},
			{"Less work", "violet", 500, 260, "violet", []string{"Drip watering on a timer", "Gravel instead of the side lawn", "Compost bin near the beds"}},
		}
		for _, c := range clusters {
			fr := item{store.Object{ID: nid(), Kind: "frame", X: c.x, Y: c.y, W: 860, H: 520, Z: -100 + float64(seq), Data: mustJSON(map[string]any{"title": c.title, "fill": c.fill})}, owner}
			items = append(items, fr)
			for i, t := range c.notes {
				by := []int64{owner, ana}[i%2]
				items = append(items, sticky(c.x+40+float64(i%4)*205, c.y+70+float64(i/4)*215, t, c.colour, by))
			}
			cx, cy := c.x+430, c.y+260
			if c.y > 0 {
				cy = c.y
			} else {
				cy = c.y + 520
			}
			x1, y1 := 0.0, 0.0
			items = append(items, item{store.Object{ID: nid(), Kind: "line", X: math.Min(x1, cx), Y: math.Min(y1, cy), W: math.Abs(cx - x1), H: math.Abs(cy - y1), Z: float64(seq),
				Data: mustJSON(map[string]any{"x1": x1, "y1": y1, "x2": cx, "y2": cy, "from": q.o.ID, "to": fr.o.ID, "end": "arrow", "curve": "curve", "color": "#5b6472", "width": 3})}, owner})
		}
		// a sketch of the beds
		pts := [][]float64{}
		for i := 0; i <= 40; i++ {
			a := float64(i) / 40 * 2 * math.Pi
			pts = append(pts, []float64{math.Round(110 + 100*math.Cos(a) + rng.Float64()*4), math.Round(70 + 60*math.Sin(a) + rng.Float64()*4)})
		}
		items = append(items, item{store.Object{ID: nid(), Kind: "pen", X: -110, Y: 560, W: 220, H: 140, Z: float64(seq + 50), Data: mustJSON(map[string]any{"points": pts, "color": "#2b8a3e", "width": 5})}, ana})
		items = append(items, item{store.Object{ID: nid(), Kind: "text", X: -110, Y: 720, W: 260, H: 30, Z: float64(seq + 51), Data: mustJSON(map[string]any{"text": "Round bed? (Ana)", "size": 20})}, ana})
		if pic := demoPicture(rng, 2, 640, 420); len(pic) > 0 {
			up := &store.Upload{UserID: owner, BoardID: b.ID, Name: "garden-now.jpg", Mime: "image/jpeg", Width: 640, Height: 420, Data: pic}
			if s.store.SaveUpload(ctx, up) == nil {
				items = append(items, item{store.Object{ID: nid(), Kind: "image", X: 300, Y: 540, W: 384, H: 252, Z: float64(seq + 52), Data: mustJSON(map[string]any{"upload": up.ID, "src": "/api/v1/uploads/" + itoa(up.ID), "name": up.Name})}, owner})
			}
		}
		put(b.ID, items)
		_ = s.store.SetMember(ctx, b.ID, ana, "editor")
		age(b, 3*24*time.Hour)
	}

	// 3. the launch plan, a kanban shared with Ana; a timer running
	if b := board(owner, "Website launch plan", "Everything before the new site goes live on the 1st.", "kanban", "dots", 1); b != nil {
		items := tplItems("kanban", owner)
		items = items[:len(items)-1] // the template's sample note
		todo := []string{"Write the About page", "Redirect old URLs", "Cookie banner text", "Photos of the team", "Check forms on phones"}
		doing := []string{"New pricing table", "Speed: compress images"}
		done := []string{"Pick the domain", "Menu structure", "Logo in SVG", "Contact form to email"}
		for i, t := range todo {
			items = append(items, sticky(30+float64(i%2)*205, 60+float64(i/2)*215, t, "yellow", []int64{owner, ana}[i%2]))
		}
		for i, t := range doing {
			items = append(items, sticky(520+30+float64(i%2)*205, 60, t, "orange", []int64{ana, owner}[i%2]))
		}
		for i, t := range done {
			items = append(items, sticky(1040+30+float64(i%2)*205, 60+float64(i/2)*215, t, "green", owner))
		}
		put(b.ID, items)
		_ = s.store.SetMember(ctx, b.ID, ana, "editor")
		b.ShareMode = "view"
		_ = s.store.UpdateBoard(ctx, b)
		age(b, 5*time.Hour)
	}

	// 4. a flowchart: booking a table
	if b := board(owner, "Booking a table — how it works", "The steps from opening the page to a confirmed table.", "blank", "plain", 9); b != nil {
		items := []item{}
		type st struct {
			form, text, fill string
			x, y, w, h       float64
		}
		steps := []st{
			{"round", "Opens the booking page", "blue", 0, 0, 260, 110},
			{"rect", "Picks a day, time and how many", "white", 0, 200, 260, 110},
			{"diamond", "Table free?", "yellow", 20, 400, 220, 160},
			{"rect", "Shows other times", "pink", 380, 425, 240, 110},
			{"rect", "Name and phone number", "white", 0, 650, 260, 110},
			{"round", "Confirmed by text message", "green", 0, 850, 260, 110},
		}
		ids := make([]string, len(steps))
		for i, x := range steps {
			o := store.Object{ID: nid(), Kind: "shape", X: x.x, Y: x.y, W: x.w, H: x.h, Z: float64(10 + i), Data: mustJSON(map[string]any{"shape": x.form, "text": x.text, "fill": x.fill, "size": 20})}
			ids[i] = o.ID
			items = append(items, item{o, owner})
		}
		join := func(a, b int, label string) {
			A, B := steps[a], steps[b]
			x1, y1, x2, y2 := A.x+A.w/2, A.y+A.h/2, B.x+B.w/2, B.y+B.h/2
			items = append(items, item{store.Object{ID: nid(), Kind: "line", X: math.Min(x1, x2), Y: math.Min(y1, y2), W: math.Abs(x2 - x1), H: math.Abs(y2 - y1), Z: float64(30 + seq),
				Data: mustJSON(map[string]any{"x1": x1, "y1": y1, "x2": x2, "y2": y2, "from": ids[a], "to": ids[b], "end": "arrow", "curve": "elbow", "text": label})}, owner})
		}
		join(0, 1, "")
		join(1, 2, "")
		join(2, 4, "yes")
		join(2, 3, "no")
		join(3, 1, "")
		join(4, 5, "")
		items = append(items, sticky(700, 120, "Ask for allergies here too?", "yellow", marko))
		items = append(items, sticky(700, 680, "Send a reminder the day before", "cyan", ana))
		put(b.ID, items)
		_ = s.store.SetMember(ctx, b.ID, marko, "viewer")
		_ = s.store.SetMember(ctx, b.ID, ana, "editor")
		age(b, 9*24*time.Hour)
	}

	// 5. Ana's trip, with the owner on it
	if ana > 0 {
		if b := board(ana, "Weekend in Mostar", "Ana's plan — add what you want to see.", "mindmap", "dots", 2); b != nil {
			items := []item{}
			center := store.Object{ID: nid(), Kind: "shape", X: -170, Y: -65, W: 340, H: 130, Z: 1, Data: mustJSON(map[string]any{"shape": "round", "text": "Weekend in Mostar", "fill": "violet", "size": 26})}
			items = append(items, item{center, ana})
			branches := []struct {
				text, fill string
				x, y       float64
				notes      []string
			}{
				{"See", "blue", -760, -420, []string{"Old Bridge at sunset", "Kravica waterfalls", "Blagaj tekija"}},
				{"Eat", "orange", 420, -420, []string{"Ćevapi at Tima-Irma", "Baklava by the bridge"}},
				{"Sleep", "green", -760, 300, []string{"Guesthouse in the old town", "Parking included?"}},
				{"Pack", "gray", 420, 300, []string{"Swimsuits", "Sun hats", "Cash in KM"}},
			}
			for _, br := range branches {
				o := store.Object{ID: nid(), Kind: "shape", X: br.x, Y: br.y, W: 260, H: 100, Z: float64(seq), Data: mustJSON(map[string]any{"shape": "round", "text": br.text, "fill": br.fill, "size": 22})}
				items = append(items, item{o, ana})
				x2, y2 := br.x+130, br.y+50
				items = append(items, item{store.Object{ID: nid(), Kind: "line", X: math.Min(0, x2), Y: math.Min(0, y2), W: math.Abs(x2), H: math.Abs(y2), Z: float64(seq),
					Data: mustJSON(map[string]any{"x1": 0, "y1": 0, "x2": x2, "y2": y2, "from": center.ID, "to": o.ID, "end": "none", "curve": "curve", "color": "#8e4ec6", "width": 4})}, ana})
				for i, t := range br.notes {
					dx := br.x - 20 + float64(i)*210
					dy := br.y + 140
					if br.y < 0 {
						dy = br.y - 240
					}
					by := ana
					if i == 1 {
						by = owner
					}
					items = append(items, sticky(dx, dy, t, map[string]string{"blue": "cyan", "orange": "orange", "green": "lime", "gray": "white"}[br.fill], by))
				}
			}
			put(b.ID, items)
			_ = s.store.SetMember(ctx, b.ID, owner, "editor")
			age(b, 2*24*time.Hour)
		}
	}
	s.log.Info("demo data added", "boards", 5)
	return nil
}

// demoUser is a second person on the box for the shared boards (no password: cannot sign in).
func (s *Server) demoUser(ctx context.Context, email, name string) int64 {
	if u, err := s.store.GetUserByEmail(ctx, email); err == nil {
		return u.ID
	}
	u := &store.User{Email: email, Name: name, Role: "member"}
	if err := s.store.CreateUser(ctx, u); err != nil {
		return 0
	}
	return u.ID
}

func mix(a, b color.RGBA, t float64) color.RGBA {
	t = math.Max(0, math.Min(1, t))
	return color.RGBA{uint8(float64(a.R) + (float64(b.R)-float64(a.R))*t), uint8(float64(a.G) + (float64(b.G)-float64(a.G))*t), uint8(float64(a.B) + (float64(b.B)-float64(a.B))*t), 255}
}

// demoPicture draws a small landscape: a sky, a sun or moon, hills, a
// little house — different colours for each style.
func demoPicture(rng *rand.Rand, style, w, h int) []byte {
	palettes := [][4]color.RGBA{
		{{255, 196, 140, 255}, {255, 236, 200, 255}, {104, 140, 96, 255}, {64, 96, 70, 255}},
		{{150, 200, 240, 255}, {225, 240, 252, 255}, {120, 170, 100, 255}, {80, 130, 80, 255}},
		{{250, 220, 170, 255}, {255, 248, 230, 255}, {200, 170, 120, 255}, {150, 120, 90, 255}},
		{{120, 170, 210, 255}, {200, 225, 240, 255}, {90, 140, 120, 255}, {60, 100, 90, 255}},
		{{240, 170, 190, 255}, {255, 225, 230, 255}, {170, 120, 150, 255}, {120, 80, 110, 255}},
		{{20, 24, 60, 255}, {70, 60, 120, 255}, {40, 40, 70, 255}, {25, 25, 45, 255}},
		{{140, 190, 230, 255}, {230, 240, 250, 255}, {170, 160, 150, 255}, {120, 110, 100, 255}},
	}
	p := palettes[(style-1)%len(palettes)]
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	sunX, sunY, sunR := float64(w)*(0.2+0.6*rng.Float64()), float64(h)*(0.2+0.15*rng.Float64()), float64(h)*0.09
	phase1, phase2 := rng.Float64()*6, rng.Float64()*6
	for y := 0; y < h; y++ {
		f := float64(y) / float64(h)
		sky := mix(p[0], p[1], f*1.4)
		for x := 0; x < w; x++ {
			c := sky
			fx := float64(x) / float64(w)
			if d := math.Hypot(float64(x)-sunX, float64(y)-sunY); d < sunR {
				c = color.RGBA{255, 250, 225, 255}
				if style == 6 {
					c = color.RGBA{235, 235, 215, 255}
				}
			} else if d < sunR*1.6 {
				c = mix(c, color.RGBA{255, 250, 230, 255}, (sunR*1.6-d)/(sunR*0.6)*0.35)
			}
			hill1 := 0.62 + 0.08*math.Sin(fx*5+phase1)
			hill2 := 0.75 + 0.06*math.Sin(fx*8+phase2)
			if f > hill1 {
				c = mix(p[2], p[3], (f-hill1)*2)
			}
			if f > hill2 {
				c = mix(p[3], color.RGBA{30, 40, 30, 255}, (f-hill2)*1.5)
			}
			img.SetRGBA(x, y, c)
		}
	}
	// a little house on the hill
	hx, hy, hw := int(float64(w)*0.62), int(float64(h)*0.6), w/12
	for y := hy; y < hy+hw; y++ {
		for x := hx; x < hx+hw; x++ {
			img.SetRGBA(x, y, color.RGBA{240, 232, 220, 255})
		}
	}
	for i := 0; i < hw/2+2; i++ {
		for x := hx - 2 + i; x < hx+hw+2-i; x++ {
			img.SetRGBA(x, hy-i, color.RGBA{170, 70, 60, 255})
		}
	}
	for y := hy + hw/3; y < hy+hw/3+hw/4; y++ {
		for x := hx + hw/4; x < hx+hw/4+hw/4; x++ {
			img.SetRGBA(x, y, color.RGBA{250, 215, 120, 255})
		}
	}
	var b bytes.Buffer
	_ = jpeg.Encode(&b, img, &jpeg.Options{Quality: 82})
	return b.Bytes()
}
