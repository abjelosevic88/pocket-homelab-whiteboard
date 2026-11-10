package server

import (
	"context"
	"fmt"
	"testing"
)

func TestPrivateMode(t *testing.T) {
	a := newApp(t, nil)
	b := a.board(a.alice, "Retro", "blank")
	p := fmt.Sprintf("/api/v1/boards/%d", b)
	a.ok(a.alice, "POST", p+"/members", map[string]any{"email": "bob@example.com", "role": "editor"})
	a.ops(a.alice, b, stickyOp("open", "everyone sees this", 0))
	a.ok(a.alice, "PUT", p+"/private", map[string]any{"on": true})
	a.ops(a.alice, b, stickyOp("secret", "only Alice for now", 300))
	a.ops(a.bob, b, stickyOp("bobs", "only Bob for now", 600))
	alice, bob := objects(a.ok(a.alice, "GET", p, nil)), objects(a.ok(a.bob, "GET", p, nil))
	if alice["secret"] == nil || alice["bobs"] != nil || alice["secret"]["hidden"] != true {
		t.Fatalf("alice sees %v", alice)
	}
	if bob["secret"] != nil || bob["bobs"] == nil || bob["open"] == nil {
		t.Fatalf("bob sees %v", bob)
	}
	// someone else's private note cannot be changed or deleted
	a.expect(404, a.bob, "POST", p+"/ops", map[string]any{"ops": []any{stickyOp("secret", "hijack", 0)}})
	a.expect(404, a.bob, "POST", p+"/ops", map[string]any{"ops": []any{map[string]any{"op": "del", "id": "secret"}}})
	a.expect(404, a.bob, "POST", p+"/react", map[string]any{"object_id": "secret", "emoji": "👍"})
	// nor found by search or listed in counts with its text
	if l := a.ok(a.bob, "GET", "/api/v1/search?q=Alice", nil)["results"].([]any); len(l) != 0 {
		t.Fatalf("bob finds %v", l)
	}
	counts := a.ok(a.bob, "GET", p, nil)["private_counts"].([]any)
	if len(counts) != 2 {
		t.Fatalf("counts %v", counts)
	}
	// reveal: everyone sees everything
	a.ok(a.bob, "PUT", p+"/private", map[string]any{"on": false})
	bob = objects(a.ok(a.bob, "GET", p, nil))
	if bob["secret"] == nil || bob["secret"]["hidden"] != nil {
		t.Fatalf("after reveal bob sees %v", bob)
	}
}

func TestComments(t *testing.T) {
	a := newApp(t, nil)
	b := a.board(a.alice, "Talk", "blank")
	p := fmt.Sprintf("/api/v1/boards/%d", b)
	a.ops(a.alice, b, stickyOp("n1", "a note", 0))
	a.expect(404, a.bob, "POST", p+"/comments", map[string]any{"body": "hi"})
	a.ok(a.alice, "POST", p+"/members", map[string]any{"email": "bob@example.com", "role": "viewer"})
	// a viewer may comment; mentions keep only people on the board
	root := obj(a.expect(201, a.bob, "POST", p+"/comments", map[string]any{"object_id": "n1", "x": 10, "y": 20, "body": "Is this done? @Alice", "mentions": []int64{a.ids["Alice"], 999}}), "comment")
	if m := root["mentions"].([]any); len(m) != 1 {
		t.Fatalf("mentions %v", m)
	}
	rid := int64(num(root, "id"))
	reply := obj(a.expect(201, a.alice, "POST", p+"/comments", map[string]any{"parent_id": rid, "body": "Yes"}), "comment")
	if str(reply, "object_id") != "n1" {
		t.Fatalf("reply pin %v", reply)
	}
	a.expect(400, a.alice, "POST", p+"/comments", map[string]any{"body": "   "})
	// only the writer changes the text; the maker resolves and deletes
	a.expect(403, a.alice, "PUT", fmt.Sprintf("%s/comments/%d", p, rid), map[string]any{"body": "changed"})
	a.ok(a.bob, "PUT", fmt.Sprintf("%s/comments/%d", p, rid), map[string]any{"body": "Is this really done?"})
	a.ok(a.alice, "PUT", fmt.Sprintf("%s/comments/%d", p, rid), map[string]any{"resolved": true})
	a.expect(403, a.bob, "DELETE", fmt.Sprintf("%s/comments/%d", p, int64(num(reply, "id"))), nil)
	l := a.ok(a.bob, "GET", p+"/comments", nil)["comments"].([]any)
	if len(l) != 2 || l[0].(map[string]any)["resolved"] != true || str(l[0].(map[string]any), "body") != "Is this really done?" {
		t.Fatalf("comments %v", l)
	}
	a.expect(204, a.alice, "DELETE", fmt.Sprintf("%s/comments/%d", p, rid), nil)
	if l := a.ok(a.alice, "GET", p+"/comments", nil)["comments"].([]any); len(l) != 0 {
		t.Fatalf("thread not gone: %v", l)
	}
}

func TestHistoryAndTrash(t *testing.T) {
	a := newApp(t, nil)
	b := a.board(a.alice, "History", "blank")
	p := fmt.Sprintf("/api/v1/boards/%d", b)
	a.ops(a.alice, b, stickyOp("k1", "keep me", 0), stickyOp("k2", "and me", 220))
	vid := int64(num(a.expect(201, a.alice, "POST", p+"/versions", map[string]any{"name": "Before the workshop"}), "id"))
	a.ops(a.alice, b, map[string]any{"op": "del", "id": "k1"}, stickyOp("k3", "new", 440))
	h := a.ok(a.alice, "GET", p+"/history", nil)
	trash := h["trash"].([]any)
	if len(trash) != 1 || str(trash[0].(map[string]any), "text") != "keep me" {
		t.Fatalf("trash %v", trash)
	}
	tid := int64(num(trash[0].(map[string]any), "id"))
	a.ok(a.alice, "POST", fmt.Sprintf("%s/trash/%d/restore", p, tid), nil)
	if objects(a.ok(a.alice, "GET", p, nil))["k1"] == nil {
		t.Fatal("not put back")
	}
	// back to the version: k3 goes, k1 and k2 stay; the state before is kept too
	a.ok(a.alice, "POST", fmt.Sprintf("%s/versions/%d/restore", p, vid), nil)
	objs := objects(a.ok(a.alice, "GET", p, nil))
	if len(objs) != 2 || objs["k3"] != nil {
		t.Fatalf("after restore %v", objs)
	}
	if v := a.ok(a.alice, "GET", p+"/history", nil)["versions"].([]any); len(v) < 2 {
		t.Fatalf("versions %v", v)
	}
	// Bob is not on it
	a.expect(404, a.bob, "GET", p+"/history", nil)
	a.ok(a.alice, "POST", p+"/members", map[string]any{"email": "bob@example.com", "role": "viewer"})
	a.expect(403, a.bob, "POST", fmt.Sprintf("%s/versions/%d/restore", p, vid), nil)
}

func TestReactions(t *testing.T) {
	a := newApp(t, nil)
	b := a.board(a.alice, "React", "blank")
	p := fmt.Sprintf("/api/v1/boards/%d", b)
	a.ops(a.alice, b, stickyOp("n", "nice", 0))
	a.ok(a.alice, "POST", p+"/members", map[string]any{"email": "bob@example.com", "role": "viewer"})
	a.ok(a.alice, "POST", p+"/react", map[string]any{"object_id": "n", "emoji": "👍"})
	m := a.ok(a.bob, "POST", p+"/react", map[string]any{"object_id": "n", "emoji": "👍"})
	if r := m["reactions"].([]any); len(r) != 1 || len(r[0].(map[string]any)["names"].([]any)) != 2 {
		t.Fatalf("reactions %v", m)
	}
	m = a.ok(a.bob, "POST", p+"/react", map[string]any{"object_id": "n", "emoji": "👍"})
	if r := m["reactions"].([]any); len(r[0].(map[string]any)["names"].([]any)) != 1 {
		t.Fatalf("toggle %v", m)
	}
	a.expect(400, a.alice, "POST", p+"/react", map[string]any{"object_id": "n", "emoji": "<script>"})
	a.expect(400, a.alice, "POST", p+"/float", map[string]any{"emoji": "hello"})
	a.expect(204, a.bob, "POST", p+"/float", map[string]any{"emoji": "🎉"})
	if r := a.ok(a.alice, "GET", p, nil)["reactions"].(map[string]any); r["n"] == nil {
		t.Fatalf("board reactions %v", r)
	}
	_ = context.Background()
}
