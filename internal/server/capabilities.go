package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-whiteboard/internal/store"
)

// The four routes behind `capabilities` (links.go): /api/v1/pocket/search,
// /day/{date}, /widget and /capture. Each runs as the request's person and
// sees the boards they made or are on, as GET /boards does; the SDK's
// handlers check and cap what goes in and out.

func boardPath(id int64) string { return "/b/" + itoa(id) }

// notePath opens a board at one thing on it (the board page reads ?o=).
func notePath(bid int64, oid string) string { return boardPath(bid) + "?o=" + oid }

// hasWord: a query of only signs (??, *) finds nothing.
func hasWord(q string) bool {
	for _, r := range q {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// whenEdited names a stored time the way people say it: "today 14:05",
// "yesterday", "3 Nov", "3 Nov 2025" (the box's zone, the person's clock).
func whenEdited(stamp string, now time.Time, h12 bool) string {
	t, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		return ""
	}
	t = t.In(now.Location())
	day := func(x time.Time) time.Time { return time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, x.Location()) }
	clock := "15:04"
	if h12 {
		clock = "3:04 PM"
	}
	switch d := day(now).Sub(day(t)); {
	case d <= 0:
		return "today " + t.Format(clock)
	case d <= 24*time.Hour:
		return "yesterday"
	case t.Year() == now.Year():
		return t.Format("2 Jan")
	}
	return t.Format("2 Jan 2006")
}

func (s *Server) h12(ctx context.Context, uid int64) bool {
	return s.settingsOf(ctx, uid).TimeFormat == "12h"
}

// ---------- search ----------

// whose says whose board it is and who else is on it, from the board list's
// own numbers: "Your board", "Your board, shared with 2 people", "Shared by Ana".
func whose(b *store.Board) string {
	if b.Role != "owner" {
		return "Shared by " + firstNonEmpty(b.OwnerName, "someone")
	}
	switch {
	case b.Members > 0:
		return "Your board, shared with " + strconv.Itoa(b.Members) + " " + plural(b.Members, "person", "people")
	case b.ShareMode == "view" || b.ShareMode == "edit":
		return "Your board, shared by link"
	}
	return "Your board"
}

// pocketSearch is the search behind GET /search: the boards whose name or
// description has the words, then the sticky notes, text, shapes and cards
// that do (one query, newest change first; others' private notes never).
func (s *Server) pocketSearch(r *http.Request, q string, limit int) ([]core.SearchItem, error) {
	if !hasWord(q) {
		return nil, nil
	}
	ctx := r.Context()
	u := userFrom(ctx)
	list, err := s.store.BoardsFor(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	now, h12 := time.Now(), s.h12(ctx, u.ID)
	lq := strings.ToLower(q)
	out := []core.SearchItem{}
	for i := range list {
		b := &list[i]
		score := 0.0
		switch {
		case strings.Contains(strings.ToLower(b.Title), lq):
			score = 3
		case strings.Contains(strings.ToLower(b.About), lq):
			score = 2
		default:
			continue
		}
		snippet := whose(b)
		if w := whenEdited(b.UpdatedAt, now, h12); w != "" {
			snippet += " · edited " + w
		}
		out = append(out, core.SearchItem{ID: "board:" + itoa(b.ID), Title: b.Title, Snippet: snippet, Kind: "board", Date: b.UpdatedAt, URL: boardPath(b.ID), Score: score})
		if len(out) == limit {
			return out, nil
		}
	}
	objs, titles, err := s.store.FindNotes(ctx, u.ID, q, limit-len(out))
	if err != nil {
		return nil, err
	}
	for _, o := range objs {
		t, _ := objText(o)
		out = append(out, core.SearchItem{ID: "note:" + itoa(o.BoardID) + ":" + o.ID, Title: clip(firstLine(t), 120), Snippet: titles[o.BoardID] + " · " + t,
			Kind: "note", Date: o.UpdatedAt, URL: notePath(o.BoardID, o.ID), Score: 1})
	}
	return out, nil
}

// ---------- day ----------

// changes says what a person did on boards in a few words: "4 things added,
// 1 changed", "2 things changed".
func changes(added, changed int) string {
	switch {
	case added > 0 && changed > 0:
		return strconv.Itoa(added) + " " + plural(added, "thing", "things") + " added, " + strconv.Itoa(changed) + " changed"
	case added > 0:
		return strconv.Itoa(added) + " " + plural(added, "thing", "things") + " added"
	}
	return strconv.Itoa(changed) + " " + plural(changed, "thing", "things") + " changed"
}

// pocketDay is GET /day/{date} in the shape of a day: one line per board the
// person added things to or changed things on, at the time of their last
// change there. The day is the caller's (tz), else the box's.
func (s *Server) pocketDay(r *http.Request, date string, tz *time.Location) (*core.Day, error) {
	ctx := r.Context()
	u := userFrom(ctx)
	loc := time.Local
	if tz != nil {
		loc = tz
	}
	day, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return nil, nil
	}
	// stored times are RFC 3339 in UTC; bounds without the zone letter sort
	// before every stamp of their second ("…:00Z" and "…:00.5Z" alike)
	const layout = "2006-01-02T15:04:05"
	from, to := day.UTC().Format(layout), day.AddDate(0, 0, 1).UTC().Format(layout)
	rows, err := s.store.DB.QueryContext(ctx, `SELECT b.id, b.title, COUNT(o.id), SUM(CASE WHEN o.created_at >= ? THEN 1 ELSE 0 END), MAX(o.updated_at)
		FROM objects o JOIN boards b ON b.id = o.board_id LEFT JOIN members m ON m.board_id = b.id AND m.user_id = ?
		WHERE (b.user_id = ? OR m.user_id IS NOT NULL) AND o.updated_at >= ? AND o.updated_at < ? AND (o.user_id = ? OR o.updated_by = ?)
		GROUP BY b.id ORDER BY MAX(o.updated_at) DESC LIMIT 200`, from, u.ID, u.ID, from, to, u.ID, s.actorName(u))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &core.Day{Date: date}
	added, changed := 0, 0
	for rows.Next() {
		var id int64
		var title, last string
		var n, a int
		if err := rows.Scan(&id, &title, &n, &a, &last); err != nil {
			return nil, err
		}
		at := ""
		if t, err := time.Parse(time.RFC3339Nano, last); err == nil {
			at = t.In(loc).Format("15:04")
		}
		added, changed = added+a, changed+n-a
		out.Items = append(out.Items, core.DayItem{ID: itoa(id), Time: at, Title: title, Sub: changes(a, n-a), Kind: "worked", URL: boardPath(id)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out.Items) == 0 {
		return nil, nil
	}
	boards := len(out.Items)
	out.Summary = changes(added, changed) + " on " + strconv.Itoa(boards) + " " + plural(boards, "board", "boards")
	out.Metrics = append(out.Metrics, core.Metric{Label: "Boards", Value: strconv.Itoa(boards)})
	if added > 0 {
		out.Metrics = append(out.Metrics, core.Metric{Label: "Added", Value: strconv.Itoa(added)})
	}
	if changed > 0 {
		out.Metrics = append(out.Metrics, core.Metric{Label: "Changed", Value: strconv.Itoa(changed)})
	}
	return out, nil
}

// ---------- widget ----------

// names joins up to two names and counts the rest: "Ana", "Ana and Ben",
// "Ana, Ben and 2 others".
func names(l []string) string {
	switch len(l) {
	case 0:
		return ""
	case 1:
		return l[0]
	case 2:
		return l[0] + " and " + l[1]
	}
	more := len(l) - 2
	return l[0] + ", " + l[1] + " and " + strconv.Itoa(more) + " " + plural(more, "other", "others")
}

// sharedLine says who else is on a board: "shared with Ana and Ben",
// "shared by link", "shared by Ana"; nothing for a board only the person sees.
func (s *Server) sharedLine(ctx context.Context, b *store.Board) string {
	if b.Role != "owner" {
		return "shared by " + firstNonEmpty(b.OwnerName, "someone")
	}
	if b.Members > 0 {
		ms, err := s.store.Members(ctx, b.ID)
		if err == nil && len(ms) > 0 {
			l := make([]string, 0, len(ms))
			for _, m := range ms {
				l = append(l, firstNonEmpty(m.Name, m.Email))
			}
			return "shared with " + names(l)
		}
	}
	if b.ShareMode == "view" || b.ShareMode == "edit" {
		return "shared by link"
	}
	return ""
}

// pocketWidget is the boards changed last, as the side bar of the board list
// shows them, each with when and who else is on it.
func (s *Server) pocketWidget(r *http.Request) (*core.WidgetData, error) {
	ctx := r.Context()
	u := userFrom(ctx)
	list, err := s.store.RecentBoards(ctx, u.ID, 8)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	now, h12 := time.Now(), s.h12(ctx, u.ID)
	out := &core.WidgetData{URL: "/"}
	for i := range list {
		b := &list[i]
		detail := []string{}
		for _, part := range []string{whenEdited(b.UpdatedAt, now, h12), s.sharedLine(ctx, b)} {
			if part != "" {
				detail = append(detail, part)
			}
		}
		out.Items = append(out.Items, core.WidgetItem{ID: itoa(b.ID), Title: b.Title, Detail: strings.Join(detail, " · "), URL: boardPath(b.ID)})
	}
	return out, nil
}

// ---------- capture ----------

// captureBoard finds where a quick line goes: the board named before a
// colon ("Retro: more coffee") when the person may add to one by that name,
// else the board of their own they opened last. It returns the line without
// the board's name.
func (s *Server) captureBoard(ctx context.Context, u *store.User, line string) (*actor, string, error) {
	if name, rest, ok := strings.Cut(line, ":"); ok {
		name = strings.TrimSpace(name)
		if name != "" && len([]rune(name)) <= maxName && !strings.Contains(name, "\n") {
			list, err := s.store.BoardsFor(ctx, u.ID) // newest change first
			if err != nil {
				return nil, "", err
			}
			var lookOnly *store.Board
			for i := range list {
				b := &list[i]
				if !strings.EqualFold(strings.TrimSpace(b.Title), name) {
					continue
				}
				if b.Role == "owner" || b.Role == "editor" {
					a, err := s.boardAccess(ctx, u, b.ID)
					return a, strings.TrimSpace(rest), err
				}
				if lookOnly == nil {
					lookOnly = b
				}
			}
			if lookOnly != nil {
				return nil, "", fmt.Errorf("%w: you can look at “%s”, not add to it", core.ErrCapture, lookOnly.Title)
			}
		}
	}
	id, err := s.store.LastOpenedBoard(ctx, u.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, "", fmt.Errorf("%w: make a board in Pocket Whiteboard first; quick notes go on the board of yours you opened last", core.ErrCapture)
	}
	if err != nil {
		return nil, "", err
	}
	a, err := s.boardAccess(ctx, u, id)
	return a, line, err
}

// pocketCapture puts a quick line on a board as a sticky note, the way
// POST /boards/{id}/notes does: below what is there, in the person's first
// sticky colour, seen live by everyone on the board. The note is the
// person's own (no source): the app that passed the line on may not change
// it later. The same request_id again answers the same note and adds
// nothing: the receipt is written in the transaction that adds the note.
func (s *Server) pocketCapture(r *http.Request, in core.CaptureInput) (*core.CaptureResult, error) {
	ctx := r.Context()
	u := userFrom(ctx)
	op, err := core.NewOperation(fmt.Sprintf("whiteboard:user:%d:%s:%s", u.ID, linkApp(ctx), core.CapturePath), in.RequestID, in)
	if err != nil {
		return nil, err
	}
	var res core.CaptureResult
	replay := func() (*core.CaptureResult, error) {
		found, err := op.Replay(ctx, s.store.DB, &res)
		switch {
		case errors.Is(err, core.ErrOperationConflict):
			return nil, fmt.Errorf("%w: this was already kept with other words", core.ErrCapture)
		case err != nil || !found:
			return nil, err
		}
		return &res, nil
	}
	if kept, err := replay(); kept != nil || err != nil {
		return kept, err
	}

	line := in.Text
	if in.Title != "" {
		line = strings.TrimSpace(in.Title + "\n" + in.Text)
	}
	a, line, err := s.captureBoard(ctx, u, line)
	if err != nil {
		return nil, err
	}
	ops := s.notesBelow(ctx, a, []string{line}, "", "")
	if len(ops) == 0 {
		return nil, fmt.Errorf("%w: write the note after the board's name", core.ErrCapture)
	}
	if s.objectCount(ctx, a.b.ID)+1 > maxObjects {
		return nil, fmt.Errorf("%w: “%s” is full; start another board", core.ErrCapture, a.b.Title)
	}
	o := ops[0].Obj
	text, _ := objText(*o)
	res = core.CaptureResult{ID: itoa(a.b.ID) + ":" + o.ID, Title: clip(firstLine(text), 120), URL: notePath(a.b.ID, o.ID)}
	v, err := s.store.ApplyOps(ctx, a.b.ID, ops, a.uid(), a.name, "", func(tx *sql.Tx) error { return op.Save(ctx, tx, res) })
	if err != nil {
		// the same line sent twice at once: the other one kept it
		if kept, rerr := replay(); kept != nil || rerr != nil {
			return kept, rerr
		}
		return nil, err
	}
	s.hub.send(a.b.ID, "", map[string]any{"type": "ops", "by": a.name, "ops": ops, "version": v})
	return &res, nil
}
