package server

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeMiro answers like Miro's REST API for one board, uXjVTest=.
type fakeMiro struct {
	api, files *httptest.Server
	pages      atomic.Int32 // item pages asked for
	fileAuth   atomic.Bool  // the file store was sent the token
}

func newFakeMiro(t *testing.T) *fakeMiro {
	t.Helper()
	f := &fakeMiro{}
	pic := demoPicture(rand.New(rand.NewSource(2)), 1, 40, 30)
	f.files = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			f.fileAuth.Store(true)
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(pic)
	}))
	t.Cleanup(f.files.Close)
	const board = "/v2/boards/uXjVTest="
	f.api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good-token" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"status":401,"message":"bad token"}`))
			return
		}
		send := func(v any) { w.Header().Set("Content-Type", "application/json"); _ = json.NewEncoder(w).Encode(v) }
		switch {
		case r.URL.Path == board:
			send(map[string]any{"id": "uXjVTest=", "name": "Team plan", "description": "From the offsite"})
		case r.URL.Path == board+"/items":
			if r.URL.Query().Get("limit") != "50" {
				t.Errorf("limit = %q", r.URL.Query().Get("limit"))
			}
			f.pages.Add(1)
			if r.URL.Query().Get("cursor") == "" {
				send(map[string]any{"cursor": "page2", "data": []any{
					map[string]any{"id": "F1", "type": "frame", "data": map[string]any{"title": "Ideas", "format": "custom"}, "style": map[string]any{"fillColor": "#ffffffff"},
						"position": map[string]any{"x": 0, "y": 0, "origin": "center", "relativeTo": "canvas_center"}, "geometry": map[string]any{"width": 1000, "height": 800}},
					map[string]any{"id": "S1", "type": "sticky_note", "data": map[string]any{"content": "<p>Hello <strong>there</strong></p>", "shape": "square"}, "style": map[string]any{"fillColor": "dark_blue", "textAlign": "center"},
						"position": map[string]any{"x": 150, "y": 200, "origin": "center", "relativeTo": "parent_top_left"}, "geometry": map[string]any{"width": 200, "height": 228}, "parent": map[string]any{"id": "F1"}},
					map[string]any{"id": "S2", "type": "sticky_note", "data": map[string]any{"content": "<p>Second &amp; last</p>"}, "style": map[string]any{"fillColor": "light_green"},
						"position": map[string]any{"x": 800, "y": 0, "origin": "center", "relativeTo": "canvas_center"}, "geometry": map[string]any{"width": 200, "height": 228}},
				}})
				return
			}
			send(map[string]any{"cursor": "", "data": []any{
				map[string]any{"id": "SH1", "type": "shape", "data": map[string]any{"content": "<p>Database</p>", "shape": "can"},
					"style":    map[string]any{"fillColor": "#FF0000", "fillOpacity": "1.0", "borderColor": "#00ff00", "borderWidth": "3.0", "borderStyle": "dashed", "borderOpacity": "1.0"},
					"position": map[string]any{"x": 0, "y": 600, "relativeTo": "canvas_center"}, "geometry": map[string]any{"width": 200, "height": 100, "rotation": 15}},
				map[string]any{"id": "T1", "type": "text", "data": map[string]any{"content": "<p>Big title</p>"}, "style": map[string]any{"fontSize": "36", "color": "#1a1a1a", "textAlign": "left"},
					"position": map[string]any{"x": 0, "y": -600, "relativeTo": "canvas_center"}, "geometry": map[string]any{"width": 400}},
				map[string]any{"id": "I1", "type": "image", "data": map[string]any{"title": "Whiteboard photo", "imageUrl": f.api.URL + board + "/resources/images/I1?format=preview&redirect=false"},
					"position": map[string]any{"x": -900, "y": 0, "relativeTo": "canvas_center"}, "geometry": map[string]any{"width": 400, "height": 300}},
				map[string]any{"id": "C1", "type": "card", "data": map[string]any{"title": "<p>Ship it</p>", "description": "Before Friday", "dueDate": "2026-12-01T10:00:00Z"}, "style": map[string]any{"cardTheme": "#2d9bf0"},
					"position": map[string]any{"x": 0, "y": 900, "relativeTo": "canvas_center"}, "geometry": map[string]any{"width": 320, "height": 94}},
				map[string]any{"id": "S3", "type": "sticky_note", "data": map[string]any{"content": "plain"}, "style": map[string]any{"fillColor": "mystery"},
					"position": map[string]any{"x": 1200, "y": 0, "relativeTo": "canvas_center"}, "geometry": map[string]any{"width": 200, "height": 228}},
				map[string]any{"id": "A1", "type": "app_card", "data": map[string]any{"title": "Jira"}, "position": map[string]any{"x": 0, "y": 0, "relativeTo": "canvas_center"}},
			}})
		case r.URL.Path == board+"/connectors":
			send(map[string]any{"data": []any{
				map[string]any{"id": "L1", "type": "connector", "shape": "elbowed", "startItem": map[string]any{"id": "S1"}, "endItem": map[string]any{"id": "S2"},
					"style":    map[string]any{"endStrokeCap": "stealth", "startStrokeCap": "none", "strokeColor": "#333333", "strokeWidth": "2.0", "strokeStyle": "normal"},
					"captions": []any{map[string]any{"content": "<p>leads to</p>"}}},
				map[string]any{"id": "L2", "type": "connector", "shape": "straight", "startItem": map[string]any{"id": "S1"}, "endItem": map[string]any{"id": "A1"}},
			}})
		case r.URL.Path == board+"/resources/images/I1":
			send(map[string]any{"type": "image", "url": f.files.URL + "/files/photo"})
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"status":404}`))
		}
	}))
	t.Cleanup(f.api.Close)
	old := miroAPI
	miroAPI = f.api.URL
	t.Cleanup(func() { miroAPI = old })
	return f
}

func TestMiroBoardID(t *testing.T) {
	for in, want := range map[string]string{
		"https://miro.com/app/board/uXjVKabc12=/":                       "uXjVKabc12=",
		"https://miro.com/app/board/uXjVKabc12%3D/?share_link_id=12345": "uXjVKabc12=",
		"miro.com/app/board/o9J_kqs1234=":                               "o9J_kqs1234=",
		"  uXjVKabc12=  ":                                               "uXjVKabc12=",
	} {
		if got, ok := miroBoardID(in); !ok || got != want {
			t.Errorf("miroBoardID(%q) = %q %v, want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "https://miro.com/app/dashboard/", "not a board <x>"} {
		if _, ok := miroBoardID(bad); ok {
			t.Errorf("miroBoardID(%q) accepted", bad)
		}
	}
}

func TestImportFromMiro(t *testing.T) {
	a := newApp(t, nil)
	f := newFakeMiro(t)

	m := a.expect(201, a.alice, "POST", "/api/v1/import/miro", map[string]any{"board": "https://miro.com/app/board/uXjVTest%3D/?share_link_id=1", "token": "good-token"})
	if f.pages.Load() != 2 {
		t.Fatalf("item pages read = %d, want 2", f.pages.Load())
	}
	if f.fileAuth.Load() {
		t.Fatal("the token went to the file store")
	}
	b := obj(m, "board")
	if str(b, "title") != "Team plan" || str(b, "about") != "From the offsite" {
		t.Fatalf("board = %v", b)
	}
	// frame, 3 notes, shape, text, picture, card, one line (the other joins the skipped app card)
	if num(m, "imported") != 9 {
		t.Fatalf("imported = %v (%v)", m["imported"], m["skipped"])
	}
	sk := obj(m, "skipped")
	if num(sk, "app_card") != 1 || num(sk, "connector") != 1 {
		t.Fatalf("skipped = %v", sk)
	}

	bid := int64(num(b, "id"))
	doc := a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/boards/%d", bid), nil)
	byText := map[string]map[string]any{}
	var frame, line, img, card, shape map[string]any
	for _, o := range objects(doc) {
		d := obj(o, "data")
		switch str(o, "kind") {
		case "frame":
			frame = o
		case "line":
			line = o
		case "image":
			img = o
		case "card":
			card = o
		case "shape":
			shape = o
		}
		if t := str(d, "text"); t != "" {
			byText[t] = o
		}
	}
	if frame == nil || str(obj(frame, "data"), "title") != "Ideas" || num(frame, "x") != -500 || num(frame, "y") != -400 || num(frame, "w") != 1000 || str(obj(frame, "data"), "fill") != "#ffffff" {
		t.Fatalf("frame = %v", frame)
	}
	if num(frame, "z") >= 0 {
		t.Fatalf("the frame is not at the back: z = %v", frame["z"])
	}

	// a note in the frame: its centre was 150,200 from the frame's corner (-500,-400)
	s1 := byText["Hello there"]
	if s1 == nil {
		t.Fatalf("no first note: %v", byText)
	}
	if num(s1, "x") != -450 || num(s1, "y") != -314 || num(s1, "w") != 200 || num(s1, "h") != 228 {
		t.Fatalf("note in the frame at %v,%v %vx%v", s1["x"], s1["y"], s1["w"], s1["h"])
	}
	if d := obj(s1, "data"); str(d, "color") != "dark-blue" || !strings.Contains(str(d, "html"), "<strong>") {
		t.Fatalf("first note data = %v", d)
	}
	s2 := byText["Second & last"]
	if s2 == nil || str(obj(s2, "data"), "color") != "light-green" || num(s2, "x") != 700 || num(s2, "y") != -114 {
		t.Fatalf("second note = %v", s2)
	}
	if s3 := byText["plain"]; s3 == nil || str(obj(s3, "data"), "color") != "light-yellow" {
		t.Fatalf("a note in an unknown colour = %v", s3)
	}

	if line == nil {
		t.Fatal("no line")
	}
	ld := obj(line, "data")
	if str(ld, "from") != str(s1, "id") || str(ld, "to") != str(s2, "id") || str(ld, "text") != "leads to" || str(ld, "curve") != "elbow" || str(ld, "end") != "arrow" || str(ld, "color") != "#333333" {
		t.Fatalf("line data = %v", ld)
	}

	if shape == nil {
		t.Fatal("no shape")
	}
	sd := obj(shape, "data")
	if str(sd, "shape") != "cylinder" || str(sd, "fill") != "#ff0000" || str(sd, "stroke") != "#00ff00" || num(sd, "width") != 3 || sd["dash"] != true || num(sd, "rot") != 15 || str(sd, "text") != "Database" {
		t.Fatalf("shape data = %v", sd)
	}
	if tx := byText["Big title"]; tx == nil || num(obj(tx, "data"), "size") != 36 || str(obj(tx, "data"), "align") != "left" {
		t.Fatalf("text = %v", tx)
	}
	if card == nil || str(obj(card, "data"), "text") != "Ship it\nBefore Friday" || str(obj(card, "data"), "due") != "2026-12-01" || str(obj(card, "data"), "color") != "#2d9bf0" {
		t.Fatalf("card = %v", card)
	}

	// the picture was fetched and kept on the new board
	if img == nil {
		t.Fatal("no picture")
	}
	idata := obj(img, "data")
	up := int64(num(idata, "upload"))
	if up == 0 || str(idata, "src") != fmt.Sprintf("/api/v1/uploads/%d", up) || num(img, "x") != -1100 || num(img, "w") != 400 {
		t.Fatalf("picture = %v", img)
	}
	stored, err := a.st.GetUpload(t.Context(), up)
	if err != nil || stored.BoardID != bid || stored.Mime != "image/jpeg" || stored.Width != 40 || stored.Height != 30 {
		t.Fatalf("upload = %+v, %v", stored, err)
	}
	a.expect(200, a.alice, "GET", fmt.Sprintf("/api/v1/uploads/%d", up), nil)

	// the new board is Alice's alone
	a.expect(404, a.bob, "GET", fmt.Sprintf("/api/v1/boards/%d", bid), nil)
	a.expect(404, a.bob, "GET", fmt.Sprintf("/api/v1/uploads/%d", up), nil)
	if l := a.ok(a.bob, "GET", "/api/v1/boards", nil)["boards"].([]any); len(l) != 0 {
		t.Fatalf("Bob sees %d boards", len(l))
	}
}

func TestImportFromMiroErrors(t *testing.T) {
	a := newApp(t, nil)
	newFakeMiro(t)
	p := "/api/v1/import/miro"
	m := a.expect(400, a.alice, "POST", p, map[string]any{"board": "uXjVTest=", "token": "wrong"})
	if !strings.Contains(str(m, "error"), "did not accept that token") {
		t.Fatalf("bad token: %v", m)
	}
	m = a.expect(404, a.alice, "POST", p, map[string]any{"board": "uXjVNope=", "token": "good-token"})
	if !strings.Contains(str(m, "error"), "could not find that board") {
		t.Fatalf("unknown board: %v", m)
	}
	a.expect(400, a.alice, "POST", p, map[string]any{"board": "https://miro.com/app/dashboard/", "token": "good-token"})
	a.expect(400, a.alice, "POST", p, map[string]any{"board": "uXjVTest=", "token": ""})
	a.expect(401, "", "POST", p, map[string]any{"board": "uXjVTest=", "token": "good-token"})
	// nothing was made along the way
	if l := a.ok(a.alice, "GET", "/api/v1/boards", nil)["boards"].([]any); len(l) != 0 {
		t.Fatalf("Alice has %d boards after failed imports", len(l))
	}
}
