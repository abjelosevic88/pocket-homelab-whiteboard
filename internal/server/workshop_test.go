package server

import (
	"fmt"
	"testing"
)

func TestAnonymousBoard(t *testing.T) {
	a := newApp(t, nil)
	b := a.board(a.alice, "Retro", "blank")
	p := fmt.Sprintf("/api/v1/boards/%d", b)
	a.ok(a.alice, "POST", p+"/members", map[string]any{"email": "bob@example.com", "role": "editor"})
	a.ops(a.alice, b, stickyOp("before", "written before", 0))
	a.expect(404, a.bob, "PUT", fmt.Sprintf("/api/v1/boards/%d/anonymous", b+99), map[string]any{"on": true})
	if m := a.ok(a.bob, "PUT", p+"/anonymous", map[string]any{"on": true}); obj(m, "board")["anonymous"] != true {
		t.Fatalf("not anonymous: %v", m)
	}
	a.ops(a.bob, b, stickyOp("during", "who wrote this?", 300))
	for _, who := range []string{a.alice, a.bob} {
		for id, o := range objects(a.ok(who, "GET", p, nil)) {
			if str(o, "author") != "" || str(o, "updated_by") != "" || o["user_id"] != nil {
				t.Fatalf("%s shows who: %v", id, o)
			}
		}
	}
	// off again: what was written while it was on stays nameless
	a.ok(a.alice, "PUT", p+"/anonymous", map[string]any{"on": false})
	objs := objects(a.ok(a.alice, "GET", p, nil))
	if str(objs["before"], "author") != "Alice" || str(objs["during"], "author") != someone {
		t.Fatalf("after: %v / %v", objs["before"], objs["during"])
	}
}

func TestPlanningPoker(t *testing.T) {
	a := newApp(t, nil)
	b := a.board(a.alice, "Sprint", "blank")
	p := fmt.Sprintf("/api/v1/boards/%d", b)
	a.ok(a.alice, "POST", p+"/members", map[string]any{"email": "bob@example.com", "role": "viewer"})
	a.ops(a.alice, b, stickyOp("c1", "Login page", 0))
	a.expect(404, a.alice, "POST", p+"/poker", map[string]any{"object": "nope"})
	a.expect(403, a.bob, "POST", p+"/poker", map[string]any{"object": "c1"})
	a.ok(a.alice, "POST", p+"/poker", map[string]any{"object": "c1"})
	a.ok(a.alice, "POST", p+"/poker/pick", map[string]any{"value": "5"})
	a.expect(400, a.bob, "POST", p+"/poker/pick", map[string]any{"value": "7"})
	a.ok(a.bob, "POST", p+"/poker/pick", map[string]any{"value": "8"})
	// before showing: who picked, not what
	pk := obj(obj(a.ok(a.bob, "GET", p, nil), "board"), "poker")
	if pk["picks"] != nil || len(pk["picked"].([]any)) != 2 || str(pk, "mine") != "8" {
		t.Fatalf("hidden round %v", pk)
	}
	a.expect(403, a.bob, "PUT", p+"/poker/reveal", map[string]any{"revealed": true})
	pk = obj(obj(a.ok(a.alice, "PUT", p+"/poker/reveal", map[string]any{"revealed": true}), "board"), "poker")
	if len(pk["picks"].([]any)) != 2 || num(pk, "average") != 6.5 {
		t.Fatalf("shown round %v", pk)
	}
	a.expect(400, a.bob, "POST", p+"/poker/pick", map[string]any{"value": "3"})
	// again: same card, picks cleared
	pk = obj(obj(a.ok(a.alice, "PUT", p+"/poker/reveal", map[string]any{"revealed": false}), "board"), "poker")
	if len(pk["picked"].([]any)) != 0 || str(pk, "object") != "c1" {
		t.Fatalf("again %v", pk)
	}
	a.ok(a.alice, "DELETE", p+"/poker", nil)
	if obj(a.ok(a.alice, "GET", p, nil), "board")["poker"] != nil {
		t.Fatal("round still on")
	}
}

func TestPastedPictureComesAlong(t *testing.T) {
	a := newApp(t, nil)
	src := a.board(a.alice, "From", "blank")
	dst := a.board(a.alice, "To", "blank")
	other := a.board(a.bob, "Bob's", "blank")
	a.ok(a.alice, "POST", fmt.Sprintf("/api/v1/boards/%d/members", dst), map[string]any{"email": "bob@example.com", "role": "viewer"})
	_, up := a.upload(a.alice, src)
	_, bobs := a.upload(a.bob, other)
	m := obj(a.ok(a.alice, "POST", fmt.Sprintf("/api/v1/boards/%d/uploads/adopt", dst), map[string]any{"ids": []int64{up, bobs}}), "ids")
	nu := int64(num(m, fmt.Sprint(up)))
	if nu == 0 || nu == up || m[fmt.Sprint(bobs)] != nil {
		t.Fatalf("adopt %v", m)
	}
	// Bob sees the copy on the board he is on, not the original
	a.expect(200, a.bob, "GET", fmt.Sprintf("/api/v1/uploads/%d", nu), nil)
	a.expect(404, a.bob, "GET", fmt.Sprintf("/api/v1/uploads/%d", up), nil)
}
