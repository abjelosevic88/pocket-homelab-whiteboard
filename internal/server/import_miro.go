package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"image"
	_ "image/gif" // picture sizes
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/abjelosevic88/pocket-homelab-whiteboard/internal/store"
)

// Import from Miro: the person gives a board link and a Miro access token;
// the board is read through Miro's REST API (v2) and becomes a new board
// here, theirs alone. The token is used for this one request: never kept,
// never logged.

// miroAPI is where Miro's REST API lives (tests point it at a fake one).
var miroAPI = "https://api.miro.com"

const (
	miroMaxItems  = 5000 // things and connectors read from one board
	miroMaxImages = 500  // pictures fetched from one board
	miroPage      = 50   // Miro's largest page
)

var miroHTTP = &http.Client{Timeout: 45 * time.Second}

// miroErr is what Miro said, in words the person can act on.
type miroErr struct {
	status int // what we answer with
	msg    string
}

func (e *miroErr) Error() string { return e.msg }

type miroClient struct {
	base  string
	token string
}

func (m *miroClient) get(ctx context.Context, path string, q url.Values, out any) error {
	u := strings.TrimRight(m.base, "/") + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+m.token)
	req.Header.Set("Accept", "application/json")
	res, err := miroHTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &miroErr{http.StatusBadGateway, "Could not reach Miro just now. Try again in a minute."}
	}
	defer res.Body.Close()
	if err := miroStatus(res.StatusCode); err != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
		return err
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 32<<20)).Decode(out); err != nil {
		return &miroErr{http.StatusBadGateway, "Miro sent back something we could not read. Try again in a minute."}
	}
	return nil
}

func miroStatus(code int) error {
	switch {
	case code >= 200 && code < 300:
		return nil
	case code == http.StatusUnauthorized, code == http.StatusForbidden:
		return &miroErr{http.StatusBadRequest, "Miro did not accept that token. Check that you copied all of it, that the app may read boards (boards:read), and that it is installed on the team that has this board."}
	case code == http.StatusNotFound:
		return &miroErr{http.StatusNotFound, "Miro could not find that board. Check the link, and that the board is on the team the token belongs to."}
	case code == http.StatusTooManyRequests:
		return &miroErr{http.StatusTooManyRequests, "Miro asks us to slow down. Wait a minute and try again."}
	}
	return &miroErr{http.StatusBadGateway, fmt.Sprintf("Miro answered with an error (%d). Try again in a minute.", code)}
}

// miroItem is one thing on a Miro board (connectors use the same shape
// with their own fields).
type miroItem struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Data     map[string]any `json:"data"`
	Style    map[string]any `json:"style"`
	Position *struct {
		X          float64 `json:"x"`
		Y          float64 `json:"y"`
		RelativeTo string  `json:"relativeTo"`
	} `json:"position"`
	Geometry *struct {
		Width    float64 `json:"width"`
		Height   float64 `json:"height"`
		Rotation float64 `json:"rotation"`
	} `json:"geometry"`
	Parent *struct {
		ID string `json:"id"`
	} `json:"parent"`

	// connectors
	Shape     string    `json:"shape"`
	StartItem *miroLink `json:"startItem"`
	EndItem   *miroLink `json:"endItem"`
	Captions  []struct {
		Content string `json:"content"`
	} `json:"captions"`
}

type miroLink struct {
	ID string `json:"id"`
}

type miroPageOf struct {
	Data   []miroItem `json:"data"`
	Cursor string     `json:"cursor"`
}

// all reads every page of a list (items or connectors), up to room more.
func (m *miroClient) all(ctx context.Context, path string, room int) ([]miroItem, bool, error) {
	var out []miroItem
	cursor := ""
	seen := map[string]bool{}
	for {
		q := url.Values{"limit": {fmt.Sprint(miroPage)}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var p miroPageOf
		if err := m.get(ctx, path, q, &p); err != nil {
			return nil, false, err
		}
		for _, it := range p.Data {
			if len(out) >= room {
				return out, true, nil
			}
			out = append(out, it)
		}
		if p.Cursor == "" || len(p.Data) == 0 || seen[p.Cursor] {
			return out, false, nil
		}
		seen[p.Cursor] = true
		cursor = p.Cursor
	}
}

var reMiroID = regexp.MustCompile(`^[A-Za-z0-9_=\-]{4,80}$`)

// miroBoardID takes the board id out of a link (…/app/board/uXjVK…=/) or
// takes a bare id as it is.
func miroBoardID(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if i := strings.Index(v, "/board/"); i >= 0 {
		rest := v[i+len("/board/"):]
		if j := strings.IndexAny(rest, "/?#"); j >= 0 {
			rest = rest[:j]
		}
		dec, err := url.PathUnescape(rest)
		if err != nil {
			return "", false
		}
		v = dec
	}
	return v, reMiroID.MatchString(v)
}

func (s *Server) handleImportMiro(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	var in struct {
		Board string `json:"board"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	bid, ok := miroBoardID(in.Board)
	if !ok {
		writeErr(w, http.StatusBadRequest, "That does not look like a Miro board link. Open the board in Miro and copy the address from the browser (it has /app/board/ in it).")
		return
	}
	token := strings.TrimSpace(in.Token)
	token = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(token, "Bearer "), "bearer "))
	if token == "" {
		writeErr(w, http.StatusBadRequest, "Paste your Miro access token too.")
		return
	}
	if strings.ContainsAny(token, " \t\r\n") || len(token) > 4096 {
		writeErr(w, http.StatusBadRequest, "That token has spaces or line breaks in it. Copy it again from Miro.")
		return
	}
	res, err := s.importMiro(ctx, u, &miroClient{base: miroAPI, token: token}, bid)
	if err != nil {
		var me *miroErr
		if errors.As(err, &me) {
			writeErr(w, me.status, me.msg)
			return
		}
		s.fail(w, err, "import from Miro")
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

// importMiro reads the board and makes it here.
func (s *Server) importMiro(ctx context.Context, u *store.User, mc *miroClient, id string) (map[string]any, error) {
	base := "/v2/boards/" + url.PathEscape(id)
	var info struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := mc.get(ctx, base, nil, &info); err != nil {
		return nil, err
	}
	items, capped, err := mc.all(ctx, base+"/items", miroMaxItems)
	if err != nil {
		return nil, err
	}
	var conns []miroItem
	if !capped {
		var c2 bool
		if conns, c2, err = mc.all(ctx, base+"/connectors", miroMaxItems-len(items)); err != nil {
			return nil, err
		}
		capped = c2
	}

	b := &store.Board{UserID: u.ID, Title: firstNonEmpty(cleanText(info.Name, maxName), "From Miro"), About: cleanText(info.Description, maxLine), Template: "blank",
		Background: firstNonEmpty(s.settingsOf(ctx, u.ID).Background, "dots"), Source: sourceOf(ctx)}
	if !oneOf(b.Background, backgrounds) {
		b.Background = "dots"
	}
	if err := s.store.CreateBoard(ctx, b); err != nil {
		return nil, err
	}
	conv := newMiroConv(items)
	objs, pics := conv.objects()
	s.miroPictures(ctx, mc, u.ID, b.ID, pics, conv)
	objs = append(objs, conv.pictures(pics)...)
	objs = append(objs, conv.lines(conns, len(items))...)

	ops := make([]store.Op, 0, len(objs))
	for i := range objs {
		objs[i].Source = b.Source
		op := store.Op{Op: "put", Obj: &objs[i]}
		if cleanOp(&op) != "" {
			conv.skip(objs[i].Kind)
			continue
		}
		ops = append(ops, op)
	}
	if len(ops) > 0 {
		if _, err := s.store.ApplyOps(ctx, b.ID, ops, &u.ID, s.actorName(u), ""); err != nil {
			_ = s.store.DeleteBoard(context.WithoutCancel(ctx), b.ID)
			return nil, err
		}
	}
	b, err = s.store.GetBoard(ctx, b.ID)
	if err != nil {
		return nil, err
	}
	s.publish(ctx, u.ID, "whiteboard.board_created", b.Title, map[string]any{"board_id": b.ID, "title": b.Title, "template": "miro", "url": s.boardURL(b)})
	return map[string]any{"board": boardOut(b, &actor{b: b, u: u, role: "owner"}), "imported": len(ops), "skipped": conv.skipped, "capped": capped}, nil
}

// ---- turning Miro's things into ours ----

type miroBox struct{ x, y, w, h float64 } // top-left, on the board

type miroConv struct {
	items   []miroItem
	byID    map[string]*miroItem
	boxes   map[string]miroBox
	ours    map[string]string // Miro id → our id
	skipped map[string]int
	mu      sync.Mutex
}

type miroPic struct {
	item  *miroItem
	id    string
	z     float64
	box   miroBox
	up    *store.Upload
	place bool
}

func newMiroConv(items []miroItem) *miroConv {
	c := &miroConv{items: items, byID: map[string]*miroItem{}, boxes: map[string]miroBox{}, ours: map[string]string{}, skipped: map[string]int{}}
	for i := range items {
		c.byID[items[i].ID] = &items[i]
	}
	return c
}

func (c *miroConv) skip(kind string) {
	c.mu.Lock()
	c.skipped[firstNonEmpty(kind, "unknown")]++
	c.mu.Unlock()
}

func newObjID() string { return "m" + store.RandomToken(6) }

// size is a thing's width and height, with Miro's usual sizes when the
// API leaves one out.
func (c *miroConv) size(it *miroItem) (float64, float64) {
	var w, h float64
	if it.Geometry != nil {
		w, h = it.Geometry.Width, it.Geometry.Height
	}
	def := map[string][2]float64{"sticky_note": {199, 228}, "text": {300, 0}, "shape": {200, 200}, "card": {320, 94}, "image": {320, 240}, "embed": {400, 300}, "frame": {800, 600}}
	d, ok := def[it.Type]
	if !ok {
		d = [2]float64{200, 200}
	}
	if w <= 0 && h > 0 && it.Type == "sticky_note" {
		w = h * 199 / 228
	}
	if w <= 0 {
		w = d[0]
	}
	if h <= 0 {
		switch it.Type {
		case "sticky_note":
			h = w * 228 / 199
		case "text":
			size := clampF(num(it.Style, "fontSize"), 8, 400, 14)
			h = size * 1.4 * float64(max(1, strings.Count(miroText(str(it.Data, "content")), "\n")+1))
		default:
			h = d[1]
		}
	}
	return w, h
}

// box is where a thing is on the board: Miro gives the centre, relative to
// the board's centre or, inside a frame, to the frame's top-left corner.
func (c *miroConv) box(it *miroItem, depth int) miroBox {
	if b, ok := c.boxes[it.ID]; ok {
		return b
	}
	w, h := c.size(it)
	var cx, cy float64
	if it.Position != nil {
		cx, cy = it.Position.X, it.Position.Y
		if it.Position.RelativeTo == "parent_top_left" && it.Parent != nil && depth < 8 {
			if p := c.byID[it.Parent.ID]; p != nil {
				pb := c.box(p, depth+1)
				cx += pb.x
				cy += pb.y
			}
		}
	}
	b := miroBox{x: cx - w/2, y: cy - h/2, w: w, h: h}
	c.boxes[it.ID] = b
	return b
}

func clampF(v, lo, hi, def float64) float64 {
	if v <= 0 || math.IsNaN(v) {
		return def
	}
	return math.Max(lo, math.Min(hi, v))
}

func numOr(m map[string]any, k string, def float64) float64 {
	if _, ok := m[k]; !ok {
		return def
	}
	return num(m, k)
}

var (
	reMiroBreak  = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</div>|</li>|</h[1-6]>`)
	reMiroLi     = regexp.MustCompile(`(?i)<li[^>]*>`)
	reMiroTag    = regexp.MustCompile(`<[^>]*>`)
	reMiroFormat = regexp.MustCompile(`(?i)<(b|strong|i|em|u|s|strike|del|ul|ol|li|a)[\s>]`)
	reMiroLines  = regexp.MustCompile(`\n{3,}`)
	reHex        = regexp.MustCompile(`^#?([0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
)

// miroText is the plain text of Miro's HTML, line breaks kept.
func miroText(s string) string {
	if s == "" {
		return ""
	}
	s = reMiroBreak.ReplaceAllString(s, "\n")
	s = reMiroLi.ReplaceAllString(s, "• ")
	s = reMiroTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, " ", " ")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	s = reMiroLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n")
	return clip(strings.TrimSpace(s), maxNotes)
}

// miroRich keeps Miro's HTML when it has formatting (the page cleans it to
// the few tags it allows before showing it).
func miroRich(s string) string {
	if !reMiroFormat.MatchString(s) || len(s) > maxNotes {
		return ""
	}
	return s
}

// hexColour is a colour as #rrggbb ("" when it is none or not a colour).
func hexColour(v string) string {
	m := reHex.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return ""
	}
	h := strings.ToLower(m[1])
	switch len(h) {
	case 3:
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	case 8:
		if h[6:] == "00" {
			return ""
		}
		h = h[:6]
	}
	return "#" + h
}

var stickyKeys = map[string]bool{"light-yellow": true, "yellow": true, "orange": true, "light-green": true, "green": true, "dark-green": true, "cyan": true,
	"light-pink": true, "pink": true, "violet": true, "red": true, "light-blue": true, "blue": true, "dark-blue": true, "gray": true, "black": true}

// stickyColour turns Miro's note colours (light_yellow, dark_blue …) into ours.
func stickyColour(v string) string {
	k := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(v)), "_", "-")
	if k == "grey" {
		k = "gray"
	}
	if stickyKeys[k] {
		return k
	}
	if h := hexColour(v); h != "" {
		return h
	}
	return "light-yellow"
}

// miroShapes maps Miro's shapes to the nearest of ours.
var miroShapes = map[string]string{
	"rectangle": "rect", "round_rectangle": "round", "circle": "ellipse", "triangle": "triangle", "rhombus": "diamond", "parallelogram": "parallelogram",
	"trapezoid": "manual", "pentagon": "pentagon", "hexagon": "hexagon", "octagon": "octagon", "star": "star", "cloud": "cloud", "cross": "cross", "can": "cylinder",
	"right_arrow": "arrow-right", "left_arrow": "arrow-right", "left_right_arrow": "arrow-right", "wedge_round_rectangle_callout": "callout",
	"left_brace": "rect", "right_brace": "rect",
	"flow_chart_process": "rect", "flow_chart_decision": "diamond", "flow_chart_terminator": "stadium", "flow_chart_input_output": "parallelogram",
	"flow_chart_document": "document", "flow_chart_multidocuments": "document", "flow_chart_predefined_process": "predefined", "flow_chart_predefined_process_2": "predefined",
	"flow_chart_magnetic_disk": "cylinder", "flow_chart_magnetic_drum": "cylinder", "flow_chart_online_storage": "cylinder", "flow_chart_internal_storage": "predefined",
	"flow_chart_manual_input": "manual", "flow_chart_manual_operation": "merge", "flow_chart_merge": "merge", "flow_chart_delay": "delay", "flow_chart_display": "display",
	"flow_chart_offpage_connector": "offpage", "flow_chart_connector": "ellipse", "flow_chart_or": "ellipse", "flow_chart_summing_junction": "ellipse",
	"flow_chart_preparation": "hexagon", "flow_chart_note_curly_left": "note", "flow_chart_note_curly_right": "note", "flow_chart_note_square": "note",
}

func miroShape(v string) string {
	if f, ok := miroShapes[strings.ToLower(v)]; ok {
		return f
	}
	return "rect"
}

func miroAlign(v string) string {
	if oneOf(v, []string{"left", "center", "right"}) {
		return v
	}
	return ""
}

// objects maps everything but pictures (which are fetched first) and
// connectors (which need both ends mapped). Frames go first so what is in
// them can be placed, and to the back.
func (c *miroConv) objects() ([]store.Object, []*miroPic) {
	var out []store.Object
	var pics []*miroPic
	order := make([]int, 0, len(c.items))
	for i := range c.items {
		if c.items[i].Type == "frame" {
			order = append(order, i)
		}
	}
	for i := range c.items {
		if c.items[i].Type != "frame" {
			order = append(order, i)
		}
	}
	for _, i := range order {
		it := &c.items[i]
		if it.Data == nil {
			it.Data = map[string]any{}
		}
		if it.Style == nil {
			it.Style = map[string]any{}
		}
		bx := c.box(it, 0)
		z := float64(i)
		d := map[string]any{}
		var kind string
		switch it.Type {
		case "frame":
			kind, z = "frame", -100000+float64(i)
			d["title"] = clip(miroText(str(it.Data, "title")), maxName)
			if f := hexColour(str(it.Style, "fillColor")); f != "" {
				d["fill"] = f
			}
		case "sticky_note":
			kind = "sticky"
			content := str(it.Data, "content")
			d["text"] = miroText(content)
			if h := miroRich(content); h != "" {
				d["html"] = h
			}
			d["color"] = stickyColour(str(it.Style, "fillColor"))
			if a := miroAlign(str(it.Style, "textAlign")); a != "" && a != "center" {
				d["align"] = a
			}
		case "text":
			kind = "text"
			content := str(it.Data, "content")
			d["text"] = miroText(content)
			if h := miroRich(content); h != "" {
				d["html"] = h
			}
			d["size"] = math.Round(clampF(num(it.Style, "fontSize"), 8, 400, 14))
			if col := hexColour(str(it.Style, "color")); col != "" {
				d["color"] = col
			}
			if a := miroAlign(str(it.Style, "textAlign")); a != "" {
				d["align"] = a
			}
		case "shape":
			kind = "shape"
			content := str(it.Data, "content")
			d["shape"] = miroShape(str(it.Data, "shape"))
			d["text"] = miroText(content)
			if h := miroRich(content); h != "" {
				d["html"] = h
			}
			fill := hexColour(str(it.Style, "fillColor"))
			if fill == "" || numOr(it.Style, "fillOpacity", 1) == 0 {
				fill = "none"
			}
			d["fill"] = fill
			if numOr(it.Style, "borderOpacity", 1) == 0 || numOr(it.Style, "borderWidth", 2) == 0 {
				d["stroke"] = "none"
			} else {
				if st := hexColour(str(it.Style, "borderColor")); st != "" {
					d["stroke"] = st
				}
				if bw := num(it.Style, "borderWidth"); bw > 0 {
					d["width"] = math.Min(bw, 24)
				}
				if bs := str(it.Style, "borderStyle"); bs == "dashed" || bs == "dotted" || bs == "dotted_dashed" {
					d["dash"] = true
				}
			}
			if a := miroAlign(str(it.Style, "textAlign")); a != "" && a != "center" {
				d["align"] = a
			}
		case "card":
			kind = "card"
			text := miroText(str(it.Data, "title"))
			if desc := miroText(str(it.Data, "description")); desc != "" {
				text = strings.TrimSpace(text + "\n" + desc)
			}
			d["text"] = clip(text, maxNotes)
			if col := hexColour(str(it.Style, "cardTheme")); col != "" {
				d["color"] = col
			}
			if due := str(it.Data, "dueDate"); len(due) >= 10 && validDate(due[:10]) {
				d["due"] = due[:10]
			}
		case "embed":
			link, ok := cleanURL(str(it.Data, "url"))
			if !ok || link == "" {
				c.skip(it.Type)
				continue
			}
			kind = "embed"
			d["url"] = link
			site := ""
			if pu, err := url.Parse(link); err == nil {
				site = strings.TrimPrefix(pu.Hostname(), "www.")
			}
			d["site"] = firstNonEmpty(str(it.Data, "providerName"), site)
			d["title"] = clip(firstNonEmpty(miroText(str(it.Data, "title")), str(it.Data, "providerName"), link), maxLine)
			if img, ok := cleanURL(str(it.Data, "previewUrl")); ok && img != "" {
				d["image"] = img
			}
			if m := reYT.FindStringSubmatch(link); m != nil {
				d["embed"], d["video"], d["site"] = "youtube", m[1], "YouTube"
				d["image"] = "https://i.ytimg.com/vi/" + m[1] + "/hqdefault.jpg"
				if d["title"] == link {
					d["title"] = "YouTube"
				}
			} else {
				d["embed"] = "link"
			}
		case "image":
			if len(pics) >= miroMaxImages || str(it.Data, "imageUrl") == "" {
				c.skip(it.Type)
				continue
			}
			p := &miroPic{item: it, id: newObjID(), z: z, box: bx}
			c.ours[it.ID] = p.id
			pics = append(pics, p)
			continue
		default:
			c.skip(it.Type)
			continue
		}
		if it.Geometry != nil && it.Geometry.Rotation != 0 && kind != "frame" {
			d["rot"] = math.Round(it.Geometry.Rotation*10) / 10
		}
		oid := newObjID()
		c.ours[it.ID] = oid
		out = append(out, store.Object{ID: oid, Kind: kind, X: bx.x, Y: bx.y, W: bx.w, H: bx.h, Z: z, Data: mustJSON(d)})
	}
	return out, pics
}

// pictures are the image objects for the pictures that came over.
func (c *miroConv) pictures(pics []*miroPic) []store.Object {
	var out []store.Object
	for _, p := range pics {
		if !p.place {
			delete(c.ours, p.item.ID)
			c.skip("image")
			continue
		}
		bx := p.box
		// keep the picture's own proportions inside Miro's width
		if p.up.Width > 0 && p.up.Height > 0 && (p.item.Geometry == nil || p.item.Geometry.Height <= 0) {
			cx, cy := bx.x+bx.w/2, bx.y+bx.h/2
			bx.h = bx.w * float64(p.up.Height) / float64(p.up.Width)
			bx.x, bx.y = cx-bx.w/2, cy-bx.h/2
			c.boxes[p.item.ID] = bx
		}
		d := map[string]any{"upload": p.up.ID, "src": "/api/v1/uploads/" + itoa(p.up.ID), "name": p.up.Name}
		if p.item.Geometry != nil && p.item.Geometry.Rotation != 0 {
			d["rot"] = math.Round(p.item.Geometry.Rotation*10) / 10
		}
		out = append(out, store.Object{ID: p.id, Kind: "image", X: bx.x, Y: bx.y, W: bx.w, H: bx.h, Z: p.z, Data: mustJSON(d)})
	}
	return out
}

// lines joins the mapped things the way Miro's connectors do.
func (c *miroConv) lines(conns []miroItem, after int) []store.Object {
	var out []store.Object
	for j := range conns {
		cn := &conns[j]
		if cn.StartItem == nil || cn.EndItem == nil {
			c.skip("connector")
			continue
		}
		from, to := c.ours[cn.StartItem.ID], c.ours[cn.EndItem.ID]
		if from == "" || to == "" || from == to {
			c.skip("connector")
			continue
		}
		a, b := c.boxes[cn.StartItem.ID], c.boxes[cn.EndItem.ID]
		x1, y1 := a.x+a.w/2, a.y+a.h/2
		x2, y2 := b.x+b.w/2, b.y+b.h/2
		st := cn.Style
		if st == nil {
			st = map[string]any{}
		}
		d := map[string]any{"from": from, "to": to, "x1": x1, "y1": y1, "x2": x2, "y2": y2, "curve": "curve", "end": miroCap(str(st, "endStrokeCap"), "arrow")}
		switch cn.Shape {
		case "straight":
			d["curve"] = "straight"
		case "elbowed":
			d["curve"] = "elbow"
		}
		if s := miroCap(str(st, "startStrokeCap"), "none"); s != "none" {
			d["start"] = s
		}
		if col := hexColour(str(st, "strokeColor")); col != "" {
			d["color"] = col
		}
		if sw := num(st, "strokeWidth"); sw > 0 {
			d["width"] = math.Max(1, math.Min(sw, 24))
		}
		if ss := str(st, "strokeStyle"); ss == "dashed" || ss == "dotted" {
			d["dash"] = true
		}
		if len(cn.Captions) > 0 {
			if t := clip(strings.ReplaceAll(miroText(cn.Captions[0].Content), "\n", " "), maxLine); t != "" {
				d["text"] = t
			}
		}
		minX, minY := math.Min(x1, x2), math.Min(y1, y2)
		out = append(out, store.Object{ID: newObjID(), Kind: "line", X: minX, Y: minY, W: math.Max(x1, x2) - minX, H: math.Max(y1, y2) - minY, Z: float64(after + j), Data: mustJSON(d)})
	}
	return out
}

// miroCap is a line end: Miro's many arrow heads become an arrow, its
// circles a dot.
func miroCap(v, def string) string {
	switch {
	case v == "":
		return def
	case v == "none":
		return "none"
	case strings.Contains(v, "oval"):
		return "dot"
	}
	return "arrow"
}

// ---- pictures ----

// miroPictures fetches the pictures (a few at a time) and keeps them as
// uploads of the new board.
func (s *Server) miroPictures(ctx context.Context, mc *miroClient, uid, bid int64, pics []*miroPic, c *miroConv) {
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, p := range pics {
		wg.Add(1)
		sem <- struct{}{}
		go func(p *miroPic) {
			defer wg.Done()
			defer func() { <-sem }()
			data, mime, err := mc.picture(ctx, str(p.item.Data, "imageUrl"))
			if err != nil {
				return
			}
			name := firstNonEmpty(str(p.item.Data, "title"), "picture")
			if filepath.Ext(name) == "" {
				name += extOf(mime)
			}
			up := &store.Upload{UserID: uid, BoardID: bid, Mime: mime, Data: data, Name: safeName(name)}
			if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
				up.Width, up.Height = cfg.Width, cfg.Height
			}
			if err := s.store.SaveUpload(ctx, up); err != nil {
				return
			}
			p.up, p.place = up, true
		}(p)
	}
	wg.Wait()
}

func extOf(mime string) string {
	return map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "image/gif": ".gif"}[mime]
}

// picture fetches one picture. Miro's image address answers either with
// the picture itself or with JSON {"url": …} pointing at the file. The
// token only goes to Miro's API, never to where the file is kept.
func (m *miroClient) picture(ctx context.Context, src string) ([]byte, string, error) {
	data, ctype, err := m.fetch(ctx, src)
	if err != nil {
		return nil, "", err
	}
	if strings.Contains(ctype, "json") || bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		var j struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(data, &j) != nil || j.URL == "" {
			return nil, "", errors.New("no picture address")
		}
		if data, _, err = m.fetch(ctx, j.URL); err != nil {
			return nil, "", err
		}
	}
	mime := http.DetectContentType(data)
	if i := strings.Index(mime, ";"); i > 0 {
		mime = mime[:i]
	}
	if !strings.HasPrefix(mime, "image/") || !allowedMime[mime] {
		return nil, "", errors.New("not a picture we keep")
	}
	return data, mime, nil
}

func (m *miroClient) fetch(ctx context.Context, src string) ([]byte, string, error) {
	pu, err := url.Parse(src)
	if err != nil || (pu.Scheme != "https" && pu.Scheme != "http") || pu.Host == "" {
		return nil, "", errors.New("bad picture address")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pu.String(), nil)
	if err != nil {
		return nil, "", err
	}
	if api, err := url.Parse(m.base); err == nil && strings.EqualFold(api.Host, pu.Host) {
		req.Header.Set("Authorization", "Bearer "+m.token)
	}
	res, err := miroHTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, "", fmt.Errorf("picture: %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxUpload+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxUpload {
		return nil, "", errors.New("picture too big")
	}
	return data, res.Header.Get("Content-Type"), nil
}
