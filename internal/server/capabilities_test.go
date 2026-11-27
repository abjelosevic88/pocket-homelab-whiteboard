package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-whiteboard/internal/config"
)

// fakeCapCore is Pocket Core for the capability tests. It answers
// introspection for link tokens (pcx_<scope>_<name> acts for
// <name>@example.com with that one scope, sent by Pocket Hub) and keeps the
// manifest the app registers with.
type fakeCapCore struct {
	*httptest.Server
	mu       sync.Mutex
	manifest map[string]any
}

var capScopes = map[string]string{
	"search": core.ScopeSearch, "day": core.ScopeDay, "widget": core.ScopeWidget, "capture": core.ScopeCapture,
	"read": "whiteboard:read", "write": "whiteboard:write",
}

func newFakeCapCore(t *testing.T) *fakeCapCore {
	f := &fakeCapCore{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/apps/register":
			f.manifest = body
			_ = json.NewEncoder(w).Encode(map[string]any{"app": map[string]any{}})
		case "/api/v1/auth/introspect":
			tok, _ := body["token"].(string)
			parts := strings.Split(tok, "_") // pcx, scope, name
			if len(parts) != 3 || capScopes[parts[1]] == "" {
				_ = json.NewEncoder(w).Encode(map[string]any{"active": false})
				return
			}
			email := parts[2] + "@example.com"
			_ = json.NewEncoder(w).Encode(map[string]any{
				"active": true, "kind": "delegated", "scope": capScopes[parts[1]], "audience": "whiteboard",
				"exp":  time.Now().Add(time.Hour).Unix(),
				"user": map[string]any{"id": 1, "sub": email, "email": email, "name": strings.ToUpper(parts[2][:1]) + parts[2][1:], "role": "member"},
				"app":  map[string]any{"id": 7, "slug": "hub", "name": "Pocket Hub"},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func capApp(t *testing.T) *app {
	t.Helper()
	f := newFakeCapCore(t)
	return newApp(t, func(c *config.Config) { c.CoreURL = f.URL })
}

// capGet decodes a capability answer as the person with this token.
func (a *app) capGet(tok, path string, out any) int {
	a.t.Helper()
	code, body := a.moveRaw(tok, "GET", path, "", nil)
	if out != nil && code < 300 {
		if err := json.Unmarshal(body, out); err != nil {
			a.t.Fatalf("GET %s: %v (%s)", path, err, body)
		}
	}
	return code
}

func (a *app) capture(tok, rid, text string) (int, core.CaptureResult, string) {
	a.t.Helper()
	body, _ := json.Marshal(map[string]any{"request_id": rid, "kind": "note", "text": text})
	code, raw := a.moveRaw(tok, "POST", core.CapturePath, "application/json", body)
	var res core.CaptureResult
	var e struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &res)
	_ = json.Unmarshal(raw, &e)
	return code, res, e.Error
}

func cardOp(id, text string) map[string]any {
	return map[string]any{"op": "put", "obj": map[string]any{"id": id, "kind": "card", "x": 0, "y": 400, "w": 240, "h": 120, "z": 2, "data": map[string]any{"text": text}}}
}

func (a *app) share(b int64, email, role string) {
	a.t.Helper()
	a.ok(a.alice, "POST", fmt.Sprintf("/api/v1/boards/%d/members", b), map[string]any{"email": email, "role": role})
}

// Pocket Search finds boards by name and the words on notes and cards, on the
// boards the person made or is on and nowhere else.
func TestPocketSearch(t *testing.T) {
	a := capApp(t)
	retro := a.board(a.alice, "Team retro", "blank")
	a.ops(a.alice, retro, stickyOp("n1", "Bread on Fridays\nand coffee", 0))
	a.share(retro, "bob@example.com", "editor")
	plans := a.board(a.alice, "Alice's plans", "blank")
	a.ops(a.alice, plans, stickyOp("n2", "secret bread recipe", 0), cardOp("c1", "Bake the bread"), stickyOp("n3", "a_c sign", 0), stickyOp("n4", "private bread", 0))
	if _, err := a.st.DB.Exec(`UPDATE objects SET hidden_by = 'u99' WHERE id = 'n4'`); err != nil {
		t.Fatal(err)
	}
	bobs := a.board(a.bob, "Bob's board", "blank")
	a.ops(a.bob, bobs, stickyOp("n5", "bob's bread", 0))

	var res core.SearchResults
	if code := a.capGet(a.alice, "/api/v1/pocket/search?q=bread", &res); code != 200 || len(res.Items) != 3 {
		t.Fatalf("alice bread: %d %+v", code, res)
	}
	seen := map[string]core.SearchItem{}
	for _, it := range res.Items {
		seen[it.ID] = it
		if strings.Contains(it.Title, "bob") || strings.Contains(it.Title, "private") {
			t.Errorf("found what is not hers to see: %+v", it)
		}
	}
	n1 := seen[fmt.Sprintf("note:%d:n1", retro)]
	if n1.Kind != "note" || n1.Title != "Bread on Fridays" || n1.Snippet != "Team retro · Bread on Fridays and coffee" || n1.URL != fmt.Sprintf("/b/%d?o=n1", retro) || n1.Date == "" {
		t.Errorf("note: %+v", n1)
	}
	if c := seen[fmt.Sprintf("note:%d:c1", plans)]; c.Title != "Bake the bread" {
		t.Errorf("card not found: %+v", res.Items)
	}

	// a board by name: whose it is and when it changed
	if a.capGet(a.alice, "/api/v1/pocket/search?q=retro", &res); len(res.Items) != 1 {
		t.Fatalf("alice retro: %+v", res.Items)
	}
	b := res.Items[0]
	if b.ID != fmt.Sprintf("board:%d", retro) || b.Kind != "board" || b.URL != fmt.Sprintf("/b/%d", retro) || !strings.HasPrefix(b.Snippet, "Your board, shared with 1 person · edited today ") {
		t.Errorf("board: %+v", b)
	}
	if a.capGet(a.bob, "/api/v1/pocket/search?q=retro", &res); len(res.Items) != 1 || !strings.HasPrefix(res.Items[0].Snippet, "Shared by Alice · edited today") {
		t.Errorf("bob retro: %+v", res.Items)
	}
	// a board only she sees
	if a.capGet(a.alice, "/api/v1/pocket/search?q=plans", &res); len(res.Items) != 1 || !strings.HasPrefix(res.Items[0].Snippet, "Your board · edited today ") {
		t.Errorf("plans: %+v", res.Items)
	}
	// Bob is on the retro, not on Alice's plans
	if a.capGet(a.bob, "/api/v1/pocket/search?q=secret", &res); len(res.Items) != 0 {
		t.Errorf("bob sees alice's plans: %+v", res.Items)
	}
	if a.capGet(a.bob, "/api/v1/pocket/search?q=fridays", &res); len(res.Items) != 1 {
		t.Errorf("bob misses the shared board's note: %+v", res.Items)
	}
	for _, q := range []string{"%3F%3F", "*", "%25", "_"} {
		if a.capGet(a.alice, "/api/v1/pocket/search?q="+q, &res); len(res.Items) != 0 {
			t.Errorf("q=%s found %+v", q, res.Items)
		}
	}
	// the words as they are: _ is not a wildcard
	if a.capGet(a.alice, "/api/v1/pocket/search?q=a_c", &res); len(res.Items) != 1 {
		t.Errorf("a_c: %+v", res.Items)
	}
	if a.capGet(a.alice, "/api/v1/pocket/search?q=abc", &res); len(res.Items) != 0 {
		t.Errorf("abc matched a_c: %+v", res.Items)
	}
	if a.capGet(a.alice, "/api/v1/pocket/search?q=bread&limit=1", &res); len(res.Items) != 1 {
		t.Errorf("limit: %+v", res.Items)
	}
	// the app's own search still answers as before
	if l := a.ok(a.alice, "GET", "/api/v1/search?q=bread", nil)["results"].([]any); len(l) != 2 {
		t.Errorf("GET /search: %v", l)
	}
}

// Pocket Day: one line per board the person worked on that day, in the
// caller's time zone.
func TestPocketDay(t *testing.T) {
	a := capApp(t)
	b := a.board(a.alice, "Plans", "blank")
	a.ops(a.alice, b, stickyOp("n1", "one", 0), stickyOp("n2", "two", 220))
	old := a.board(a.alice, "Old", "blank")
	a.ops(a.alice, old, stickyOp("o1", "made long ago", 0))
	// made two days ago, changed yesterday just after midnight (UTC)
	y := time.Now().UTC().AddDate(0, 0, -1)
	yday := y.Format("2006-01-02")
	if _, err := a.st.DB.Exec(`UPDATE objects SET created_at = ?, updated_at = ? WHERE id = 'o1'`,
		y.AddDate(0, 0, -1).Format(time.RFC3339Nano), yday+"T00:00:00.5Z"); err != nil {
		t.Fatal(err)
	}

	var d core.Day
	today := time.Now().UTC().Format("2006-01-02")
	if code := a.capGet(a.alice, "/api/v1/pocket/day/"+today+"?tz=UTC", &d); code != 200 || len(d.Items) != 1 {
		t.Fatalf("today: %d %+v", code, d)
	}
	it := d.Items[0]
	if d.Summary != "2 things added on 1 board" || it.Title != "Plans" || it.Sub != "2 things added" || it.URL != fmt.Sprintf("/b/%d", b) || len(it.Time) != 5 {
		t.Errorf("today: %+v", d)
	}
	if a.capGet(a.alice, "/api/v1/pocket/day/"+yday+"?tz=UTC", &d); d.Summary != "1 thing changed on 1 board" || len(d.Items) != 1 || d.Items[0].Time != "00:00" {
		t.Errorf("yesterday: %+v", d)
	}
	// Bob did nothing: an empty day
	d = core.Day{}
	if a.capGet(a.bob, "/api/v1/pocket/day/"+today+"?tz=UTC", &d); d.Summary != "" || len(d.Items) != 0 {
		t.Errorf("bob: %+v", d)
	}
	if code := a.capGet(a.alice, "/api/v1/pocket/day/2026-13-01", nil); code != 400 {
		t.Errorf("bad day → %d", code)
	}
}

// Pocket Hub's widget: the boards changed last, when, and who else is on them.
func TestPocketWidget(t *testing.T) {
	a := capApp(t)
	var w core.WidgetData
	if code := a.capGet(a.bob, "/api/v1/pocket/widget", &w); code != 200 || len(w.Items) != 0 || w.Line != "" {
		t.Fatalf("no boards: %d %+v", code, w)
	}
	alone := a.board(a.alice, "Alone", "blank")
	retro := a.board(a.alice, "Retro", "blank")
	a.share(retro, "bob@example.com", "viewer")
	if a.capGet(a.alice, "/api/v1/pocket/widget", &w); len(w.Items) != 2 || w.Line != "" || w.URL != "/" {
		t.Fatalf("alice: %+v", w)
	}
	// Retro changed last (Bob was added to it)
	if it := w.Items[0]; it.Title != "Retro" || it.URL != fmt.Sprintf("/b/%d", retro) || !strings.HasPrefix(it.Detail, "today ") || !strings.HasSuffix(it.Detail, " · shared with Bob") {
		t.Errorf("retro: %+v", it)
	}
	if it := w.Items[1]; it.ID != itoa(alone) || strings.Contains(it.Detail, "shared") || !strings.HasPrefix(it.Detail, "today ") {
		t.Errorf("alone: %+v", it)
	}
	if a.capGet(a.bob, "/api/v1/pocket/widget", &w); len(w.Items) != 1 || !strings.HasSuffix(w.Items[0].Detail, " · shared by Alice") {
		t.Errorf("bob: %+v", w)
	}
}

func TestWhenEdited(t *testing.T) {
	loc := time.FixedZone("x", 3600)
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, loc)
	for stamp, want := range map[string]string{
		"2026-11-27T08:05:00Z": "today 09:05",
		"2026-11-26T22:30:00Z": "yesterday",
		"2026-11-26T23:30:00Z": "today 00:30",
		"2026-11-03T12:00:00Z": "3 Nov",
		"2025-11-03T12:00:00Z": "3 Nov 2025",
		"nonsense":             "",
	} {
		if got := whenEdited(stamp, now, false); got != want {
			t.Errorf("%s → %q, want %q", stamp, got, want)
		}
	}
	if got := whenEdited("2026-11-27T13:05:00Z", now, true); got != "today 2:05 PM" {
		t.Errorf("12h: %q", got)
	}
}

// A quick line becomes a sticky note on the board of the person's own they
// opened last, or on the board named before a colon; once per request id.
func TestPocketCapture(t *testing.T) {
	a := capApp(t)
	if code, _, msg := a.capture(a.alice, "req-000000000000001", "an idea"); code != 400 || !strings.Contains(msg, "make a board") {
		t.Fatalf("no board: %d %q", code, msg)
	}
	retro := a.board(a.alice, "Retro", "blank")
	inbox := a.board(a.alice, "Inbox", "blank")
	shared := a.board(a.alice, "Shared", "blank")
	a.share(shared, "bob@example.com", "viewer")
	a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/boards/%d", retro), nil)
	time.Sleep(5 * time.Millisecond)
	a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/boards/%d", inbox), nil)

	code, kept, msg := a.capture(a.alice, "req-000000000000001", "call the plumber")
	if code != 201 || !strings.HasPrefix(kept.ID, itoa(inbox)+":") || kept.Title != "call the plumber" || !strings.HasPrefix(kept.URL, fmt.Sprintf("/b/%d?o=", inbox)) {
		t.Fatalf("capture: %d %+v %q", code, kept, msg)
	}
	if _, again, _ := a.capture(a.alice, "req-000000000000001", "call the plumber"); again != kept {
		t.Errorf("again: %+v, want %+v", again, kept)
	}
	if code, _, msg := a.capture(a.alice, "req-000000000000001", "other words"); code != 400 || !strings.Contains(msg, "other words") {
		t.Errorf("same id, other words: %d %q", code, msg)
	}
	objs := objects(a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/boards/%d", inbox), nil))
	if len(objs) != 1 {
		t.Fatalf("inbox holds %d things", len(objs))
	}
	oid := strings.TrimPrefix(kept.ID, itoa(inbox)+":")
	if o := objs[oid]; o == nil || str(o, "kind") != "sticky" || str(obj(o, "data"), "text") != "call the plumber" {
		t.Errorf("note: %v", objs)
	}

	// a board by name before a colon
	if code, res, _ := a.capture(a.alice, "req-000000000000002", "retro:  more coffee"); code != 201 || !strings.HasPrefix(res.ID, itoa(retro)+":") || res.Title != "more coffee" {
		t.Errorf("retro: %d %+v", code, res)
	}
	// a colon that names no board stays in the note
	if _, res, _ := a.capture(a.alice, "req-000000000000003", "Note: buy milk"); !strings.HasPrefix(res.ID, itoa(inbox)+":") || res.Title != "Note: buy milk" {
		t.Errorf("no such board: %+v", res)
	}
	if code, _, msg := a.capture(a.alice, "req-000000000000004", "Retro:"); code != 400 || !strings.Contains(msg, "after the board's name") {
		t.Errorf("nothing after the name: %d %q", code, msg)
	}

	// Bob may look at Shared, not add to it; and he has no board of his own
	if code, _, msg := a.capture(a.bob, "req-000000000000005", "Shared: hello"); code != 400 || !strings.Contains(msg, "look at") {
		t.Errorf("viewer: %d %q", code, msg)
	}
	if code, _, msg := a.capture(a.bob, "req-000000000000006", "hello"); code != 400 || !strings.Contains(msg, "make a board") {
		t.Errorf("bob's default: %d %q", code, msg)
	}

	// through Pocket Hub's link: the note is Alice's own, not the hub's
	code, linked, _ := a.capture("pcx_capture_alice", "req-000000000000001", "call the plumber")
	if code != 201 || linked.ID == kept.ID {
		t.Fatalf("link: %d %+v (ids are kept per app)", code, linked)
	}
	if _, again, _ := a.capture("pcx_capture_alice", "req-000000000000001", "call the plumber"); again != linked {
		t.Errorf("link again: %+v", again)
	}
	var source, author string
	if err := a.st.DB.QueryRow(`SELECT source, author FROM objects WHERE board_id = ? AND id = ?`, inbox, strings.TrimPrefix(linked.ID, itoa(inbox)+":")).Scan(&source, &author); err != nil || source != "" || author != "Alice" {
		t.Errorf("linked note: source %q author %q %v", source, author, err)
	}
	var n int
	_ = a.st.DB.QueryRow(`SELECT COUNT(1) FROM objects WHERE board_id = ?`, inbox).Scan(&n)
	if n != 3 {
		t.Errorf("inbox holds %d notes, want 3", n)
	}
}

// Each pocket:* scope opens its one route (and /auth/me); nothing else, and
// the app's own read and write scopes do not open them.
func TestPocketLinkScopes(t *testing.T) {
	a := capApp(t)
	a.board(a.alice, "Plans", "blank")
	day := core.DayPath + time.Now().Format("2006-01-02")
	routes := map[string][2]string{
		"search":  {"GET", core.SearchPath + "?q=plans"},
		"day":     {"GET", day},
		"widget":  {"GET", core.WidgetPath},
		"capture": {"POST", core.CapturePath},
	}
	body := []byte(`{"request_id":"req-scope-0000000001","text":"from the hub"}`)
	for scope := range capScopes {
		tok := "pcx_" + scope + "_alice"
		if code, _ := a.moveRaw(tok, "GET", "/api/v1/auth/me", "", nil); code != 200 {
			t.Errorf("%s /auth/me → %d", scope, code)
		}
		for name, rt := range routes {
			want := 403
			if name == scope {
				want = 200
				if rt[0] == "POST" {
					want = 201
				}
			}
			if code, out := a.moveRaw(tok, rt[0], rt[1], "application/json", body); code != want {
				t.Errorf("%s %s %s → %d, want %d (%s)", scope, rt[0], rt[1], code, want, out)
			}
		}
	}
	for _, c := range []struct{ tok, method, path string }{
		{"pcx_search_alice", "GET", "/api/v1/boards"},
		{"pcx_search_alice", "GET", "/api/v1/search?q=plans"},
		{"pcx_day_alice", "GET", "/api/v1/day/2026-11-27"},
		{"pcx_widget_alice", "GET", "/api/v1/home"},
		{"pcx_capture_alice", "GET", core.CapturePath},
		{"pcx_capture_alice", "POST", "/api/v1/boards"},
		{"pcx_search_alice", "POST", core.SearchPath},
	} {
		// the wrong method on a capability route: the router answers 405 before the link is looked at
		if code, _ := a.moveRaw(c.tok, c.method, c.path, "application/json", []byte("{}")); code != 403 && code != 405 {
			t.Errorf("%s %s %s → %d, want 403 or 405", c.tok, c.method, c.path, code)
		}
	}
}

// What Core is told: the shelf, the four capabilities and their offers next
// to the app's own.
func TestManifestCapabilities(t *testing.T) {
	f := newFakeCapCore(t)
	a := newApp(t, func(c *config.Config) {
		c.CoreURL, c.CoreRegistrationKey, c.CoreAppURL = f.URL, "pcr_test", "http://whiteboard:8080"
	})
	if err := a.s.registerWithCore(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	raw, _ := json.Marshal(f.manifest)
	f.mu.Unlock()
	var m core.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.Slug != "whiteboard" || m.Category != "Work" {
		t.Errorf("manifest %q on shelf %q", m.Slug, m.Category)
	}
	if m.Search == nil || strings.Join(m.Search.Bangs, ",") != "whiteboard,board" || strings.Join(m.Search.Kinds, ",") != "board,note" || m.Search.What == "" {
		t.Errorf("search: %+v", m.Search)
	}
	if m.Day == nil || m.Day.One != "board" || m.Day.Many != "boards" || m.Day.What == "" {
		t.Errorf("day: %+v", m.Day)
	}
	if m.Widget == nil || m.Widget.Title != "Boards" || m.Widget.What == "" {
		t.Errorf("widget: %+v", m.Widget)
	}
	if m.Capture == nil || len(m.Capture.Kinds) != 1 || m.Capture.Kinds[0].Kind != "note" || m.Capture.Kinds[0].What == "" {
		t.Errorf("capture: %+v", m.Capture)
	}
	offers := map[string]bool{}
	for _, o := range m.Offers {
		if o.Name == "" || o.Description == "" {
			t.Errorf("offer %q has no words", o.Scope)
		}
		offers[o.Scope] = true
	}
	for _, scope := range []string{core.ScopeSearch, core.ScopeDay, core.ScopeWidget, core.ScopeCapture, "whiteboard:read", "whiteboard:write", core.ScopeMoveExport, core.ScopeMoveImport} {
		if !offers[scope] {
			t.Errorf("offer %s is missing", scope)
		}
	}
}

// Without Pocket Core the routes answer the person signed in, as before.
func TestPocketRoutesWithoutCore(t *testing.T) {
	a := newApp(t, nil)
	b := a.board(a.alice, "Plans", "blank")
	var w core.WidgetData
	if code := a.capGet(a.alice, core.WidgetPath, &w); code != 200 || len(w.Items) != 1 || w.Items[0].ID != itoa(b) {
		t.Errorf("widget: %d %+v", code, w)
	}
	if code, res, _ := a.capture(a.alice, "req-standalone-00001", "an idea"); code != 201 || !strings.HasPrefix(res.ID, itoa(b)+":") {
		t.Errorf("capture: %d %+v", code, res)
	}
}
