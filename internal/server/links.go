package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-whiteboard/internal/store"
)

// Pocket Core app links (docs/links.md in pocket-homelab-core), Whiteboard as
// the target: another Pocket app the person allowed in Core may act here for
// them with a pcx_ token, only within these scopes. Every route not listed
// in linkRoutes stays closed to it (403).

var linkOffers = []core.Offer{
	{Scope: "whiteboard:read", Name: "See your whiteboards", Description: "See the boards you made or are on and what is on them — sticky notes, text, frames and who wrote what — and search the words on them: for your journal, search and Pocket AI."},
	{Scope: "whiteboard:write", Name: "Add to your whiteboards", Description: "Start a board for you and put sticky notes on it, from Pocket Voice, Notes or Pocket AI when you ask. Changes or removes only what that app put there itself."},
}

// portable is what Pocket Move may copy out and bring in for a person
// (docs/move.md in pocket-homelab-core); Register adds move:export/import.
// The JSON is only the boards the person made; the CSV is the notes on the
// boards they made or are on, as they see them (never others' private notes).
var portable = &core.Portable{
	Exports: []core.PortableExport{
		{Path: "/api/v1/export.json", File: "whiteboard.json", Kind: core.KindPocket, Title: "Every board you made, with everything on it and its pictures"},
		{Path: "/api/v1/export.csv", File: "whiteboard-notes.csv", Kind: core.KindCSV, Title: "The notes on your boards as a table"},
	},
	Imports: []core.PortableImport{
		{Path: "/api/v1/import", Kind: core.KindPocket, ContentType: "application/json", MaxBytes: 1 << 30, Title: "Adds the boards from another box; boards brought in before are skipped"},
	},
}

// capabilities are what Pocket Search, Day and Hub find here on their own
// (docs/capabilities.md in pocket-homelab-core); Register adds the pocket:*
// offers, capabilities.go serves the routes.
var capabilities = core.Capabilities{
	Search: &core.Searchable{What: "Your boards by name, and the words on their sticky notes and cards", Kinds: []string{"board", "note"}, Bangs: []string{"whiteboard", "board"}},
	Day:    &core.DayFeed{Label: "Whiteboard", One: "board", Many: "boards", Unit: "count", What: "The boards you added to or changed that day"},
	Widget: &core.Widget{Title: "Boards", What: "The boards changed last, and who they are shared with"},
	Capture: &core.QuickCapture{Kinds: []core.CaptureKind{{Kind: "note", Label: "Sticky note",
		What: "A sticky note on the board of yours you opened last. Start with a board's name and a colon to put it on that board."}}},
}

type linkRoute struct {
	methods string
	path    *regexp.Regexp
	scope   string // "" = any linked app
}

var linkRoutes = []linkRoute{
	{"GET", regexp.MustCompile(`^/api/v1/auth/me$`), ""},
	{"GET", regexp.MustCompile(`^/api/v1/(home|summary|search|boards|templates|day/\d{4}-\d{2}-\d{2})$`), "whiteboard:read"},
	{"GET", regexp.MustCompile(`^/api/v1/boards/\d+$`), "whiteboard:read"},
	{"GET", regexp.MustCompile(`^/api/v1/uploads/\d+$`), "whiteboard:read"},
	{"POST", regexp.MustCompile(`^/api/v1/boards$`), "whiteboard:write"},
	{"POST", regexp.MustCompile(`^/api/v1/boards/\d+/notes$`), "whiteboard:write"},
	{"POST", regexp.MustCompile(`^/api/v1/boards/\d+/ops$`), "whiteboard:write"},
}

var errLinkScope = errors.New("outside the link's scope")

// linkAllows reports whether a link token with these scopes may make this request.
func linkAllows(scopes []string, r *http.Request) bool {
	if portable.LinkAllows(scopes, r) || capabilities.LinkAllows(scopes, r) {
		return true
	}
	for _, lr := range linkRoutes {
		if !strings.Contains(" "+lr.methods+" ", " "+r.Method+" ") || !lr.path.MatchString(r.URL.Path) {
			continue
		}
		if lr.scope == "" {
			return true
		}
		for _, sc := range scopes {
			if sc == lr.scope {
				return true
			}
		}
	}
	return false
}

// linkUser validates a pcx_ token, checks the route against its scopes and
// maps it to the person it acts for. The second value is the acting app's slug.
func (s *Server) linkUser(ctx context.Context, tok string, r *http.Request) (*store.User, string, error) {
	in, err := s.core.Introspect(ctx, tok)
	if err != nil {
		return nil, "", err
	}
	if !in.Active || !in.Delegated() || in.Audience != "whiteboard" || in.User == nil || in.User.Email == "" || in.App == nil || in.App.Slug == "" {
		return nil, "", http.ErrNoCookie
	}
	if !linkAllows(strings.Fields(in.Scope), r) {
		return nil, "", errLinkScope
	}
	u, err := s.coreIdentityUser(ctx, in)
	return u, in.App.Slug, err
}

const linkAppKey ctxKey = 2

// linkApp is the acting app's slug when the request came in on a link token.
func linkApp(ctx context.Context) string {
	a, _ := ctx.Value(linkAppKey).(string)
	return a
}

// withLinkApp marks a context as acting for another app (tests, and the auth middleware).
func withLinkApp(ctx context.Context, slug string) context.Context {
	return context.WithValue(ctx, linkAppKey, slug)
}
