package server

import (
	"net/http"
	"strings"
	"time"
)

// For the person and for the rest of the box (Pocket Day, Search, AI):
// what is going on on their boards.

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	list, err := s.store.BoardsFor(ctx, u.ID)
	if err != nil {
		s.fail(w, err, "summary")
		return
	}
	mine, shared, starred, things, live := 0, 0, 0, 0, 0
	recent := []map[string]any{}
	for i := range list {
		b := &list[i]
		if b.Role == "owner" {
			mine++
		} else {
			shared++
		}
		if b.Starred {
			starred++
		}
		things += b.Objects
		n := s.hub.count(b.ID)
		if n > 0 {
			live++
		}
		if len(recent) < 5 {
			recent = append(recent, map[string]any{"id": b.ID, "title": b.Title, "updated_at": b.UpdatedAt, "objects": b.Objects, "role": b.Role, "live": n, "url": s.boardURL(b)})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"boards": len(list), "mine": mine, "shared_with_me": shared, "starred": starred, "things": things, "live_now": live, "recent": recent})
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) { s.handleSummary(w, r) }

// handleSearch finds boards by name and the words written on them.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(q)) < 2 {
		writeJSON(w, http.StatusOK, map[string]any{"results": []any{}})
		return
	}
	out := []map[string]any{}
	list, _ := s.store.BoardsFor(ctx, u.ID)
	lq := strings.ToLower(q)
	for i := range list {
		b := &list[i]
		if strings.Contains(strings.ToLower(b.Title+" "+b.About), lq) {
			out = append(out, map[string]any{"kind": "board", "board_id": b.ID, "title": b.Title, "snippet": firstNonEmpty(b.About, b.OwnerName), "url": s.boardURL(b), "path": "/b/" + itoa(b.ID), "updated_at": b.UpdatedAt})
		}
	}
	objs, titles, err := s.store.SearchObjects(ctx, u.ID, q, 40)
	if err != nil {
		s.fail(w, err, "search")
		return
	}
	for _, o := range objs {
		t, _ := objText(o)
		path := "/b/" + itoa(o.BoardID) + "?o=" + o.ID
		out = append(out, map[string]any{"kind": o.Kind, "board_id": o.BoardID, "object_id": o.ID, "title": clip(firstLine(t), 120), "snippet": titles[o.BoardID] + " · " + firstNonEmpty(o.Author, o.UpdatedBy),
			"url": s.link(path), "path": path, "updated_at": o.UpdatedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": out})
}

// handleDay is what happened on the person's boards on one day (Pocket Day).
func (s *Server) handleDay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	d := chiParam(r, "date")
	day, err := time.ParseInLocation("2006-01-02", d, time.Local)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "the date is YYYY-MM-DD")
		return
	}
	from, to := day.UTC().Format(time.RFC3339Nano), day.AddDate(0, 0, 1).UTC().Format(time.RFC3339Nano)
	rows, err := s.store.DB.QueryContext(ctx, `SELECT b.id, b.title, COUNT(o.id), SUM(CASE WHEN o.created_at >= ? THEN 1 ELSE 0 END)
		FROM objects o JOIN boards b ON b.id = o.board_id LEFT JOIN members m ON m.board_id = b.id AND m.user_id = ?
		WHERE (b.user_id = ? OR m.user_id IS NOT NULL) AND o.updated_at >= ? AND o.updated_at < ? AND (o.user_id = ? OR o.updated_by = ?)
		GROUP BY b.id ORDER BY MAX(o.updated_at) DESC`, from, u.ID, u.ID, from, to, u.ID, s.actorName(u))
	if err != nil {
		s.fail(w, err, "day")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var title string
		var changed, added int
		if err := rows.Scan(&id, &title, &changed, &added); err != nil {
			s.fail(w, err, "day")
			return
		}
		out = append(out, map[string]any{"board_id": id, "title": title, "changed": changed, "added": added, "url": s.link("/b/" + itoa(id))})
	}
	writeJSON(w, http.StatusOK, map[string]any{"date": d, "boards": out})
}
