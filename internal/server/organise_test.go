package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFoldersArePersonal(t *testing.T) {
	a := newApp(t, nil)
	b := a.board(a.alice, "Shared", "blank")
	p := fmt.Sprintf("/api/v1/boards/%d", b)
	a.ok(a.alice, "POST", p+"/members", map[string]any{"email": "bob@example.com", "role": "editor"})
	fa := int64(num(obj(a.ok(a.alice, "POST", "/api/v1/folders", map[string]any{"name": "Work"}), "folder"), "id"))
	fb := int64(num(obj(a.ok(a.bob, "POST", "/api/v1/folders", map[string]any{"name": "Fun"}), "folder"), "id"))
	a.expect(400, a.alice, "POST", "/api/v1/folders", map[string]any{"name": " "})
	a.ok(a.alice, "PUT", p+"/folder", map[string]any{"folder_id": fa})
	a.expect(404, a.alice, "PUT", p+"/folder", map[string]any{"folder_id": fb}) // not hers
	a.ok(a.bob, "PUT", p+"/folder", map[string]any{"folder_id": fb})
	row := func(auth string) map[string]any {
		return a.ok(auth, "GET", "/api/v1/boards?plain=1", nil)["boards"].([]any)[0].(map[string]any)
	}
	if int64(num(row(a.alice), "folder_id")) != fa || int64(num(row(a.bob), "folder_id")) != fb {
		t.Fatalf("folders %v / %v", row(a.alice)["folder_id"], row(a.bob)["folder_id"])
	}
	if l := a.ok(a.bob, "GET", "/api/v1/folders", nil)["folders"].([]any); len(l) != 1 || str(l[0].(map[string]any), "name") != "Fun" {
		t.Fatalf("bob's folders %v", l)
	}
	a.expect(404, a.bob, "DELETE", fmt.Sprintf("/api/v1/folders/%d", fa), nil)
	a.expect(204, a.alice, "DELETE", fmt.Sprintf("/api/v1/folders/%d", fa), nil)
	if int64(num(row(a.alice), "folder_id")) != 0 {
		t.Fatal("board still in a deleted folder")
	}
}

func TestOwnTemplates(t *testing.T) {
	a := newApp(t, nil)
	b := a.board(a.alice, "Our retro", "retro")
	a.ops(a.alice, b, stickyOp("x1", "keep this note", 0))
	m := a.expect(201, a.alice, "POST", fmt.Sprintf("/api/v1/boards/%d/template", b), map[string]any{"title": "Team retro", "emoji": "🔁"})
	key := str(m, "key")
	mine := a.ok(a.alice, "GET", "/api/v1/templates", nil)["mine"].([]any)
	if len(mine) != 1 || str(mine[0].(map[string]any), "title") != "Team retro" {
		t.Fatalf("mine %v", mine)
	}
	if l := a.ok(a.bob, "GET", "/api/v1/templates", nil)["mine"].([]any); len(l) != 0 {
		t.Fatalf("bob sees alice's templates %v", l)
	}
	nb := a.expect(201, a.alice, "POST", "/api/v1/boards", map[string]any{"template": key})
	objs := objects(a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/boards/%d", int64(num(obj(nb, "board"), "id"))), nil))
	if objs["x1"] == nil || len(objs) < 5 {
		t.Fatalf("board from template has %d things", len(objs))
	}
	a.expect(400, a.bob, "POST", "/api/v1/boards", map[string]any{"template": key})
	tid := int64(num(mine[0].(map[string]any), "id"))
	a.expect(404, a.bob, "DELETE", fmt.Sprintf("/api/v1/templates/mine/%d", tid), nil)
	a.expect(204, a.alice, "DELETE", fmt.Sprintf("/api/v1/templates/mine/%d", tid), nil)
}

func TestUnfurl(t *testing.T) {
	a := newApp(t, nil)
	// a page on this machine is never fetched
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`<title>secret</title>`)) }))
	defer local.Close()
	m := a.expect(400, a.alice, "GET", "/api/v1/unfurl?url="+local.URL, nil)
	if !strings.Contains(str(m, "error"), "own network") {
		t.Fatalf("local fetch: %v", m)
	}
	a.expect(400, a.alice, "GET", "/api/v1/unfurl?url=ftp://example.com", nil)
	a.expect(400, a.alice, "GET", "/api/v1/unfurl?url=http://192.168.1.1/admin", nil)
	// YouTube links are known without fetching
	p := obj(a.ok(a.alice, "GET", "/api/v1/unfurl?url=https://youtu.be/dQw4w9WgXcQ", nil), "preview")
	if str(p, "kind") != "youtube" || str(p, "video") != "dQw4w9WgXcQ" {
		t.Fatalf("youtube %v", p)
	}
}

func TestCardsAndDocuments(t *testing.T) {
	a := newApp(t, nil)
	b := a.board(a.alice, "Cards", "blank")
	p := fmt.Sprintf("/api/v1/boards/%d", b)
	a.ops(a.alice, b, map[string]any{"op": "put", "obj": map[string]any{"id": "c1", "kind": "card", "x": 0, "y": 0, "w": 260, "h": 140, "data": map[string]any{"text": "Ship it", "due": "2026-12-01"}}})
	// without Pocket Tasks the button says what to do
	m := a.expect(400, a.alice, "POST", p+"/cards/c1/task", nil)
	if !strings.Contains(strings.ToLower(str(m, "error")), "tasks") {
		t.Fatalf("no tasks: %v", m)
	}
	a.expect(404, a.alice, "POST", p+"/cards/nope/task", nil)
	if num(a.ok(a.alice, "POST", p+"/cards/pull", nil), "changed") != 0 {
		t.Fatal("pull")
	}
	// a PDF goes up; a script does not
	up := func(name string, data []byte) int {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("file", name)
		fw.Write(data)
		mw.Close()
		req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/uploads?board=%d", a.url, b), &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+a.alice)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var j map[string]any
		_ = json.NewDecoder(res.Body).Decode(&j)
		return res.StatusCode
	}
	if c := up("plan.pdf", []byte("%PDF-1.4\n%fake\n")); c != 201 {
		t.Fatalf("pdf upload %d", c)
	}
	if c := up("x.html", []byte("<html><script>alert(1)</script></html>")); c != 400 {
		t.Fatalf("html upload %d", c)
	}
}
