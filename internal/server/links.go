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
