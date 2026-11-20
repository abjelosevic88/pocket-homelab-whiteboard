package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-whiteboard/internal/config"
)

// moveLinks starts Whiteboard behind a fake Core whose pcx_<who>_<export|import|read>
// tokens are Pocket Move links acting for alice@ or bob@ (members on the Core side).
func moveLinks(t *testing.T) *app {
	t.Helper()
	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/auth/introspect" {
			io.WriteString(w, `{}`)
			return
		}
		var in struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		parts := strings.SplitN(strings.TrimPrefix(in.Token, "pcx_"), "_", 2)
		if len(parts) != 2 {
			io.WriteString(w, `{"active":false}`)
			return
		}
		scope := map[string]string{"export": core.ScopeMoveExport, "import": core.ScopeMoveImport, "read": "whiteboard:read"}[parts[1]]
		email := parts[0] + "@example.com"
		fmt.Fprintf(w, `{"active":true,"kind":"delegated","scope":%q,"audience":"whiteboard","user":{"id":1,"sub":%q,"email":%q,"name":%q,"role":"member"},"app":{"id":7,"slug":"move","name":"Pocket Move"}}`, scope, email, email, parts[0])
	}))
	t.Cleanup(coreSrv.Close)
	return newApp(t, func(c *config.Config) { c.CoreURL = coreSrv.URL })
}

func (a *app) moveRaw(tok, method, path, ctype string, body []byte) (int, []byte) {
	a.t.Helper()
	req, _ := http.NewRequest(method, a.url+path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func TestMoveLinkScopes(t *testing.T) {
	a := moveLinks(t)
	exp, imp, read := "pcx_bob_export", "pcx_bob_import", "pcx_bob_read"
	for _, c := range []struct {
		tok, method, path string
		want              int
	}{
		{exp, "GET", core.PortablePath, 200},
		{exp, "GET", "/api/v1/export.json", 200},
		{exp, "GET", "/api/v1/export.csv", 200},
		{exp, "POST", "/api/v1/import", 403},
		{exp, "POST", "/api/v1/import/miro", 403},
		{exp, "GET", "/api/v1/settings", 403},
		{exp, "GET", "/api/v1/boards", 403},
		{exp, "GET", "/api/v1/backup", 403},
		{imp, "GET", core.PortablePath, 200},
		{imp, "GET", "/api/v1/export.json", 403},
		{imp, "POST", "/api/v1/import/miro", 403},
		{imp, "POST", "/api/v1/boards", 403},
		{imp, "PUT", "/api/v1/settings", 403},
		{read, "GET", core.PortablePath, 403},
		{read, "GET", "/api/v1/export.json", 403},
	} {
		if code, body := a.moveRaw(c.tok, c.method, c.path, "application/json", []byte("{}")); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%s)", c.tok, c.method, c.path, code, c.want, body)
		}
	}
	_, body := a.moveRaw(exp, "GET", core.PortablePath, "", nil)
	var p core.Portable
	if err := json.Unmarshal(body, &p); err != nil || len(p.Exports) != 2 || len(p.Imports) != 1 || p.Exports[0].Kind != core.KindPocket {
		t.Fatalf("portable: %s", body)
	}
}

// Move brings Alice's boards to Bob: the import adds once, and a board
// Alice shares with Bob is not in Bob's own export.
func TestMoveImportsAddOnce(t *testing.T) {
	a := moveLinks(t)
	mine := a.board(a.alice, "Alice's plans", "blank")
	a.ops(a.alice, mine, stickyOp("n1", "secret plan", 0))
	shared := a.board(a.alice, "Team retro", "blank")
	a.ops(a.alice, shared, stickyOp("n2", "went well", 0))
	a.ok(a.alice, "POST", fmt.Sprintf("/api/v1/boards/%d/members", shared), map[string]any{"email": "bob@example.com", "role": "editor"})

	code, export := a.moveRaw("pcx_alice_export", "GET", "/api/v1/export.json", "", nil)
	if code != 200 || !strings.Contains(string(export), "secret plan") || !strings.Contains(string(export), "went well") {
		t.Fatalf("alice's export: %d %.200s", code, export)
	}
	_, bobJSON := a.moveRaw("pcx_bob_export", "GET", "/api/v1/export.json", "", nil)
	if strings.Contains(string(bobJSON), "secret plan") || strings.Contains(string(bobJSON), "went well") {
		t.Fatalf("bob's export holds alice's boards: %s", bobJSON)
	}
	_, bobCSV := a.moveRaw("pcx_bob_export", "GET", "/api/v1/export.csv", "", nil)
	if strings.Contains(string(bobCSV), "secret plan") {
		t.Fatalf("bob's csv holds alice's own board: %s", bobCSV)
	}

	imp := func() map[string]any {
		t.Helper()
		code, out := a.moveRaw("pcx_bob_import", "POST", "/api/v1/import", "application/json", export)
		if code != 200 {
			t.Fatalf("import → %d %s", code, out)
		}
		var m map[string]any
		_ = json.Unmarshal(out, &m)
		return m
	}
	if m := imp(); num(m, "added") != 2 {
		t.Fatalf("first import: %v", m)
	}
	if m := imp(); num(m, "added") != 0 || num(m, "skipped") != 2 {
		t.Fatalf("second import: %v", m)
	}
	// Bob: two boards of his own now, plus the one Alice shares.
	if l := a.ok(a.bob, "GET", "/api/v1/boards", nil)["boards"].([]any); len(l) != 3 {
		t.Fatalf("bob has %d boards", len(l))
	}
	if l := a.ok(a.alice, "GET", "/api/v1/boards", nil)["boards"].([]any); len(l) != 2 {
		t.Fatalf("alice has %d boards", len(l))
	}
}
