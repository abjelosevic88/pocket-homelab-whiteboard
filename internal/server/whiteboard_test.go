package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-whiteboard/internal/store"
)

func (a *app) board(auth, title, tpl string) int64 {
	a.t.Helper()
	m := a.expect(201, auth, "POST", "/api/v1/boards", map[string]any{"title": title, "template": tpl})
	return int64(num(obj(m, "board"), "id"))
}

func stickyOp(id, text string, x float64) map[string]any {
	return map[string]any{"op": "put", "obj": map[string]any{"id": id, "kind": "sticky", "x": x, "y": 0, "w": 200, "h": 200, "z": 1, "data": map[string]any{"text": text, "color": "yellow"}}}
}

func (a *app) ops(auth string, b int64, ops ...map[string]any) map[string]any {
	a.t.Helper()
	return a.ok(auth, "POST", fmt.Sprintf("/api/v1/boards/%d/ops", b), map[string]any{"client": "test", "ops": ops})
}

func objects(m map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	l, _ := m["objects"].([]any)
	for _, x := range l {
		o := x.(map[string]any)
		out[str(o, "id")] = o
	}
	return out
}

func (a *app) upload(auth string, b int64) (int, int64) {
	a.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "pic.jpg")
	_, _ = fw.Write(demoPicture(rand.New(rand.NewSource(1)), 1, 40, 30))
	_ = mw.Close()
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/uploads?board=%d", a.url, b), &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+auth)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(res.Body).Decode(&m)
	return res.StatusCode, int64(num(obj(m, "upload"), "id"))
}

func TestBoardsArePerPerson(t *testing.T) {
	a := newApp(t, nil)
	b := a.board(a.alice, "Alice's plans", "blank")
	a.ops(a.alice, b, stickyOp("s1", "secret idea", 0))
	p := fmt.Sprintf("/api/v1/boards/%d", b)
	// Bob (and an admin is no exception: Alice is the admin here, Bob a member) cannot reach it
	a.expect(404, a.bob, "GET", p, nil)
	a.expect(404, a.bob, "PUT", p, map[string]any{"title": "mine"})
	a.expect(404, a.bob, "DELETE", p, nil)
	a.expect(404, a.bob, "POST", p+"/ops", map[string]any{"ops": []any{stickyOp("s2", "x", 0)}})
	a.expect(404, a.bob, "POST", p+"/duplicate", nil)
	a.expect(404, a.bob, "GET", p+"/votes", nil)
	if l := a.ok(a.bob, "GET", "/api/v1/boards", nil)["boards"].([]any); len(l) != 0 {
		t.Fatalf("Bob sees %d boards", len(l))
	}
	if l := a.ok(a.bob, "GET", "/api/v1/search?q=secret", nil)["results"].([]any); len(l) != 0 {
		t.Fatalf("Bob finds %v", l)
	}
	// and Bob's boards stay away from Alice, admin or not
	bb := a.board(a.bob, "Bob's", "blank")
	a.expect(404, a.alice, "GET", fmt.Sprintf("/api/v1/boards/%d", bb), nil)
	if l := a.ok(a.alice, "GET", "/api/v1/search?q=secret", nil)["results"].([]any); len(l) != 1 {
		t.Fatalf("Alice finds %d", len(l))
	}
	// pictures follow the board
	code, up := a.upload(a.alice, b)
	if code != 201 {
		t.Fatalf("upload %d", code)
	}
	a.expect(200, a.alice, "GET", fmt.Sprintf("/api/v1/uploads/%d", up), nil)
	a.expect(404, a.bob, "GET", fmt.Sprintf("/api/v1/uploads/%d", up), nil)
	if code, _ := a.upload(a.bob, b); code != 404 {
		t.Fatalf("Bob uploads to Alice's board: %d", code)
	}
}

func TestMembersAndRoles(t *testing.T) {
	a := newApp(t, nil)
	b := a.board(a.alice, "Team", "retro")
	p := fmt.Sprintf("/api/v1/boards/%d", b)
	if n := len(objects(a.ok(a.alice, "GET", p, nil))); n < 4 {
		t.Fatalf("retro template put %d things", n)
	}
	a.expect(400, a.alice, "POST", p+"/members", map[string]any{"email": "nobody@example.com"})
	a.ok(a.alice, "POST", p+"/members", map[string]any{"email": "bob@example.com", "role": "viewer"})
	m := a.ok(a.bob, "GET", p, nil)
	if str(obj(m, "me"), "role") != "viewer" || obj(m, "board")["share_key"] != nil || m["share_url"] != nil {
		t.Fatalf("viewer sees %v", m["me"])
	}
	a.expect(403, a.bob, "POST", p+"/ops", map[string]any{"ops": []any{stickyOp("b1", "hi", 0)}})
	a.expect(403, a.bob, "PUT", p, map[string]any{"title": "x"})
	a.expect(403, a.bob, "POST", p+"/members", map[string]any{"email": "alice@example.com"})
	// an editor changes things, but does not share or delete
	a.ok(a.alice, "POST", p+"/members", map[string]any{"email": "bob@example.com", "role": "editor"})
	a.ops(a.bob, b, stickyOp("b1", "Bob was here", 50))
	a.ok(a.bob, "PUT", p, map[string]any{"title": "Team retro"})
	a.expect(403, a.bob, "PUT", p, map[string]any{"share_mode": "edit"})
	a.expect(403, a.bob, "DELETE", p, nil)
	o := objects(a.ok(a.alice, "GET", p, nil))["b1"]
	if str(o, "author") != "Bob" {
		t.Fatalf("author %v", o)
	}
	// stars are each person's own
	a.ok(a.bob, "PUT", p+"/star", map[string]any{"starred": true})
	if a.ok(a.alice, "GET", p, nil)["starred"] == true {
		t.Fatal("Bob's star shows for Alice")
	}
	// Bob leaves; then it is gone for him
	a.ok(a.bob, "DELETE", fmt.Sprintf("%s/members/%d", p, a.ids["Bob"]), nil)
	a.expect(404, a.bob, "GET", p, nil)
	// a copy is the copier's own, with everything on it
	a.ok(a.alice, "POST", p+"/members", map[string]any{"email": "bob@example.com", "role": "viewer"})
	cp := a.expect(201, a.bob, "POST", p+"/duplicate", nil)
	cid := int64(num(obj(cp, "board"), "id"))
	if str(obj(cp, "board"), "role") != "owner" || len(objects(a.ok(a.bob, "GET", fmt.Sprintf("/api/v1/boards/%d", cid), nil))) == 0 {
		t.Fatalf("copy %v", cp)
	}
	a.expect(404, a.alice, "GET", fmt.Sprintf("/api/v1/boards/%d", cid), nil)
}

func TestOpsAreSavedAndValidated(t *testing.T) {
	a := newApp(t, nil)
	b := a.board(a.alice, "Ops", "blank")
	p := fmt.Sprintf("/api/v1/boards/%d", b)
	v1 := num(a.ops(a.alice, b, stickyOp("a", "one", 0), stickyOp("b", "two", 220)), "version")
	v2 := num(a.ops(a.alice, b, stickyOp("a", "one, changed", 10), map[string]any{"op": "del", "id": "b"}), "version")
	if v2 <= v1 {
		t.Fatalf("version %v → %v", v1, v2)
	}
	objs := objects(a.ok(a.alice, "GET", p, nil))
	if len(objs) != 1 || str(obj(objs["a"], "data"), "text") != "one, changed" || num(objs["a"], "x") != 10 {
		t.Fatalf("objects %v", objs)
	}
	bad := []map[string]any{
		{"op": "put", "obj": map[string]any{"id": "x y", "kind": "sticky"}},
		{"op": "put", "obj": map[string]any{"id": "x", "kind": "spaceship"}},
		{"op": "put", "obj": map[string]any{"id": "x", "kind": "sticky", "w": -5}},
		{"op": "put", "obj": map[string]any{"id": "x", "kind": "sticky", "x": 1e12}},
		{"op": "fly", "id": "x"},
	}
	for _, op := range bad {
		a.expect(400, a.alice, "POST", p+"/ops", map[string]any{"ops": []any{op}})
	}
	// notes from another app land below what is there
	m := a.expect(201, a.alice, "POST", p+"/notes", map[string]any{"notes": []string{"from voice", "", "second"}})
	if num(m, "added") != 2 {
		t.Fatalf("notes %v", m)
	}
}

func TestVoting(t *testing.T) {
	a := newApp(t, nil)
	b := a.board(a.alice, "Vote", "blank")
	p := fmt.Sprintf("/api/v1/boards/%d", b)
	a.ops(a.alice, b, stickyOp("a", "A", 0), stickyOp("b", "B", 220))
	a.ok(a.alice, "POST", p+"/members", map[string]any{"email": "bob@example.com", "role": "viewer"})
	a.expect(400, a.bob, "POST", p+"/vote", map[string]any{"object_id": "a"})
	a.expect(403, a.bob, "PUT", p+"/vote", map[string]any{"action": "start"})
	a.ok(a.alice, "PUT", p+"/vote", map[string]any{"action": "start", "per_person": 2, "hide": true})
	a.ok(a.bob, "POST", p+"/vote", map[string]any{"object_id": "a"})
	v := obj(a.ok(a.bob, "POST", p+"/vote", map[string]any{"object_id": "a"}), "votes")
	if num(obj(v, "mine"), "a") != 2 || num(v, "left") != 0 {
		t.Fatalf("bob's votes %v", v)
	}
	a.expect(400, a.bob, "POST", p+"/vote", map[string]any{"object_id": "b"})
	a.ok(a.alice, "POST", p+"/vote", map[string]any{"object_id": "b"})
	// hidden while open
	if c := obj(obj(a.ok(a.alice, "GET", p+"/votes", nil), "votes"), "counts"); len(c) != 0 {
		t.Fatalf("counts shown while hidden: %v", c)
	}
	a.ok(a.alice, "PUT", p+"/vote", map[string]any{"action": "end"})
	c := obj(obj(a.ok(a.bob, "GET", p+"/votes", nil), "votes"), "counts")
	if num(c, "a") != 2 || num(c, "b") != 1 {
		t.Fatalf("results %v", c)
	}
	a.expect(400, a.bob, "POST", p+"/vote", map[string]any{"object_id": "b"})
}

func TestShareLink(t *testing.T) {
	a := newApp(t, nil)
	b := a.board(a.alice, "Open house", "blank")
	p := fmt.Sprintf("/api/v1/boards/%d", b)
	a.ops(a.alice, b, stickyOp("a", "hello", 0))
	key := str(obj(a.ok(a.alice, "GET", p, nil), "board"), "share_key")
	if key == "" {
		t.Fatal("no share key for the maker")
	}
	pub := "/api/v1/public/b/" + key
	guest := func(method, path string, body any) (int, map[string]any) {
		var rd io.Reader
		if body != nil {
			j, _ := json.Marshal(body)
			rd = bytes.NewReader(j)
		}
		req, _ := http.NewRequest(method, a.url+path, rd)
		req.Header.Set("X-Guest-Id", "guest12345")
		req.Header.Set("X-Guest-Name", "Nona")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var m map[string]any
		_ = json.NewDecoder(res.Body).Decode(&m)
		return res.StatusCode, m
	}
	if code, _ := guest("GET", pub, nil); code != 404 {
		t.Fatalf("link works while off: %d", code)
	}
	a.ok(a.alice, "PUT", p, map[string]any{"share_mode": "view"})
	code, m := guest("GET", pub, nil)
	if code != 200 || len(objects(m)) != 1 || m["members"] != nil || obj(m, "board")["share_key"] != nil {
		t.Fatalf("guest view %d %v", code, m)
	}
	if code, _ := guest("POST", pub+"/ops", map[string]any{"ops": []any{stickyOp("g", "guest", 0)}}); code != 403 {
		t.Fatalf("view link edits: %d", code)
	}
	a.ok(a.alice, "PUT", p, map[string]any{"share_mode": "edit"})
	if code, m := guest("POST", pub+"/ops", map[string]any{"ops": []any{stickyOp("g", "guest note", 0)}}); code != 200 {
		t.Fatalf("edit link: %d %v", code, m)
	}
	if o := objects(a.ok(a.alice, "GET", p, nil))["g"]; str(o, "author") != "Nona" || o["user_id"] != nil {
		t.Fatalf("guest note %v", o)
	}
	// a new link: the old one stops
	a.ok(a.alice, "POST", p+"/share-key", nil)
	if code, _ := guest("GET", pub, nil); code != 404 {
		t.Fatalf("old link still works: %d", code)
	}
}

func TestLiveStream(t *testing.T) {
	a := newApp(t, nil)
	b := a.board(a.alice, "Live", "blank")
	p := fmt.Sprintf("/api/v1/boards/%d", b)
	a.ok(a.alice, "POST", p+"/members", map[string]any{"email": "bob@example.com", "role": "editor"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", a.url+p+"/stream?client=bobtab", nil)
	req.Header.Set("Authorization", "Bearer "+a.bob)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	wait := func(want string) string {
		for sc.Scan() {
			if line := sc.Text(); strings.HasPrefix(line, "data:") && strings.Contains(line, want) {
				return line
			}
		}
		t.Fatalf("no %q on the stream", want)
		return ""
	}
	wait(`"version"`) // hello
	go a.ops(a.alice, b, stickyOp("live1", "it moves", 0))
	if line := wait(`"type":"ops"`); !strings.Contains(line, "live1") || !strings.Contains(line, `"by":"Alice"`) {
		t.Fatalf("ops event %s", line)
	}
	go a.ok(a.alice, "POST", p+"/cursor", map[string]any{"client": "alicetab", "x": 10, "y": 20})
	if line := wait(`"type":"cursor"`); !strings.Contains(line, `"name":"Alice"`) {
		t.Fatalf("cursor event %s", line)
	}
}

func TestLinkScopes(t *testing.T) {
	r := func(method, path string) *http.Request { return httptest.NewRequest(method, path, nil) }
	read, write := []string{"whiteboard:read"}, []string{"whiteboard:write"}
	cases := []struct {
		scopes []string
		req    *http.Request
		want   bool
	}{
		{read, r("GET", "/api/v1/auth/me"), true},
		{read, r("GET", "/api/v1/boards"), true},
		{read, r("GET", "/api/v1/boards/3"), true},
		{read, r("GET", "/api/v1/search"), true},
		{read, r("GET", "/api/v1/day/2026-11-10"), true},
		{read, r("POST", "/api/v1/boards"), false},
		{write, r("POST", "/api/v1/boards"), true},
		{write, r("POST", "/api/v1/boards/3/notes"), true},
		{write, r("POST", "/api/v1/boards/3/ops"), true},
		{write, r("PUT", "/api/v1/boards/3"), false},
		{append(read, write...), r("DELETE", "/api/v1/boards/3"), false},
		{append(read, write...), r("POST", "/api/v1/boards/3/members"), false},
		{append(read, write...), r("GET", "/api/v1/export.json"), false},
		{append(read, write...), r("GET", "/api/v1/settings"), false},
	}
	for _, c := range cases {
		if got := linkAllows(c.scopes, c.req); got != c.want {
			t.Errorf("%v %s %s = %v, want %v", c.scopes, c.req.Method, c.req.URL.Path, got, c.want)
		}
	}
	ctx := withLinkApp(context.Background(), "voice")
	if !mayChange(ctx, "voice") || mayChange(ctx, "") || mayChange(ctx, "photos") {
		t.Fatal("mayChange")
	}
}

func TestExportImportIsIdempotent(t *testing.T) {
	a := newApp(t, nil)
	b := a.board(a.alice, "Keep me", "kanban")
	a.ops(a.alice, b, stickyOp("k1", "kept", 0))
	_, up := a.upload(a.alice, b)
	a.ops(a.alice, b, map[string]any{"op": "put", "obj": map[string]any{"id": "img", "kind": "image", "x": 0, "y": 300, "w": 40, "h": 30, "data": map[string]any{"upload": up, "src": fmt.Sprintf("/api/v1/uploads/%d", up)}}})
	a.board(a.alice, "Second", "blank")
	doc, err := a.s.export(context.Background(), &store.User{ID: a.ids["Alice"]})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(doc)
	imp := func() map[string]any {
		req, _ := http.NewRequest("POST", a.url+"/api/v1/import", bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+a.bob)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		if res.StatusCode != 200 {
			t.Fatalf("import %d %s", res.StatusCode, body)
		}
		return m
	}
	if m := imp(); num(m, "added") != 2 {
		t.Fatalf("first import %v", m)
	}
	if m := imp(); num(m, "added") != 0 || num(m, "skipped") != 2 {
		t.Fatalf("second import %v", m)
	}
	list := a.ok(a.bob, "GET", "/api/v1/boards", nil)["boards"].([]any)
	if len(list) != 2 {
		t.Fatalf("Bob has %d boards", len(list))
	}
	for _, x := range list {
		bm := x.(map[string]any)
		if str(bm, "title") != "Keep me" {
			continue
		}
		objs := objects(a.ok(a.bob, "GET", fmt.Sprintf("/api/v1/boards/%d", int64(num(bm, "id"))), nil))
		img := obj(objs["img"], "data")
		nu := int64(num(img, "upload"))
		if nu == 0 || nu == up {
			t.Fatalf("picture not copied: %v", img)
		}
		a.expect(200, a.bob, "GET", fmt.Sprintf("/api/v1/uploads/%d", nu), nil)
	}
}

func TestSettingsArePersonal(t *testing.T) {
	a := newApp(t, nil)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"background": "grid", "sticky_color": "pink", "snap": true, "notes_folder": "Boards"})
	m := a.ok(a.alice, "GET", "/api/v1/settings", nil)
	if m["background"] != "grid" || m["sticky_color"] != "pink" || m["snap"] != true || m["notes_folder"] != "Boards" {
		t.Fatalf("alice %v", m)
	}
	m = a.ok(a.bob, "GET", "/api/v1/settings", nil)
	if m["background"] != "dots" || m["sticky_color"] != "light-yellow" || m["snap"] == true {
		t.Fatalf("bob got alice's settings: %v", m)
	}
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"background": "lava"})
	if a.ok(a.alice, "GET", "/api/v1/settings", nil)["background"] != "grid" {
		t.Fatal("bad background saved")
	}
	// a new board takes the person's background
	b := a.board(a.alice, "Mine", "blank")
	if str(obj(a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/boards/%d", b), nil), "board"), "background") != "grid" {
		t.Fatal("new board background")
	}
}

func TestDemoData(t *testing.T) {
	a := newApp(t, nil)
	if err := a.s.seedDemo(context.Background(), a.ids["Alice"]); err != nil {
		t.Fatal(err)
	}
	l := a.ok(a.alice, "GET", "/api/v1/boards", nil)["boards"].([]any)
	if len(l) != 5 {
		t.Fatalf("demo boards %d", len(l))
	}
	for _, x := range l {
		bm := x.(map[string]any)
		if num(bm, "objects_n") == 0 {
			t.Fatalf("empty demo board %v", bm["title"])
		}
	}
	// twice changes nothing
	_ = a.s.seedDemo(context.Background(), a.ids["Alice"])
	if l2 := a.ok(a.alice, "GET", "/api/v1/boards", nil)["boards"].([]any); len(l2) != 5 {
		t.Fatalf("demo seeded twice: %d", len(l2))
	}
}
