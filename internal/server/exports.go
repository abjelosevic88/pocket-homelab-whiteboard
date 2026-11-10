package server

import (
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/abjelosevic88/pocket-homelab-whiteboard/internal/store"
)

// The export is every board a person made, with everything on it, the
// pictures and who else was on it. Importing it again changes nothing: a
// board imported before (same origin) is skipped.

type exportFile struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Mime  string `json:"mime"`
	W     int    `json:"width,omitempty"`
	H     int    `json:"height,omitempty"`
	Data  string `json:"data"`
	Thumb string `json:"thumb,omitempty"`
}

type exportBoard struct {
	Board   store.Board    `json:"board"`
	Objects []store.Object `json:"objects"`
	Files   []exportFile   `json:"files"`
	Members []string       `json:"members"`
}

type exportDoc struct {
	App        string        `json:"app"`
	Version    string        `json:"version"`
	ExportedAt string        `json:"exported_at"`
	Boards     []exportBoard `json:"boards"`
}

func (s *Server) export(ctx context.Context, u *store.User) (*exportDoc, error) {
	list, err := s.store.OwnBoards(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	doc := &exportDoc{App: "pocket-whiteboard", Version: Version, ExportedAt: store.Now(), Boards: []exportBoard{}}
	for _, b := range list {
		objs, _ := s.store.Objects(ctx, b.ID)
		eb := exportBoard{Board: b, Objects: objs, Files: []exportFile{}, Members: []string{}}
		seen := map[int64]bool{}
		for _, o := range objs {
			if o.Kind != "image" {
				continue
			}
			var d map[string]any
			_ = json.Unmarshal(o.Data, &d)
			id := int64(num(d, "upload"))
			if id == 0 || seen[id] {
				continue
			}
			seen[id] = true
			up, err := s.store.GetUpload(ctx, id)
			if err != nil || up.BoardID != b.ID {
				continue
			}
			f := exportFile{ID: up.ID, Name: up.Name, Mime: up.Mime, W: up.Width, H: up.Height, Data: base64.StdEncoding.EncodeToString(up.Data)}
			if len(up.Thumb) > 0 {
				f.Thumb = base64.StdEncoding.EncodeToString(up.Thumb)
			}
			eb.Files = append(eb.Files, f)
		}
		members, _ := s.store.Members(ctx, b.ID)
		for _, m := range members {
			eb.Members = append(eb.Members, m.Email+" "+m.Role)
		}
		doc.Boards = append(doc.Boards, eb)
	}
	return doc, nil
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	doc, err := s.export(r.Context(), userFrom(r.Context()))
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="pocket-whiteboard-%s.json"`, time.Now().Format("2006-01-02")))
	_ = json.NewEncoder(w).Encode(doc)
}

// handleExportCSV is every note on the person's boards (or one board): for
// spreadsheets.
func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	list, err := s.store.BoardsFor(ctx, u.ID)
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	only := int64(queryInt(r, "board"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="pocket-whiteboard-%s.csv"`, time.Now().Format("2006-01-02")))
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"board", "frame", "kind", "text", "colour", "votes", "author", "x", "y", "updated_at"})
	for i := range list {
		b := &list[i]
		if only > 0 && b.ID != only {
			continue
		}
		a := &actor{b: b, u: u, role: b.Role}
		objs, _ := s.store.Objects(ctx, b.ID)
		v := b.VoteOf()
		votes := map[string]int{}
		if v.Round > 0 && (v.Done || !v.Hide) {
			votes, _, _, _ = s.store.Votes(ctx, b.ID, v.Round, "")
		}
		byID := map[string]store.Object{}
		for _, o := range objs {
			byID[o.ID] = o
		}
		for _, n := range notesOf(objs, votes) {
			o := byID[n.ID]
			_ = cw.Write([]string{a.b.Title, n.Frame, n.Kind, n.Text, n.Color, fmt.Sprint(n.Votes), firstNonEmpty(o.Author, o.UpdatedBy), fmt.Sprintf("%.0f", o.X), fmt.Sprintf("%.0f", o.Y), o.UpdatedAt})
		}
	}
	cw.Flush()
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var doc exportDoc
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<30)).Decode(&doc); err != nil || doc.App != "pocket-whiteboard" {
		writeErr(w, http.StatusBadRequest, "that is not a Pocket Whiteboard export")
		return
	}
	res, err := s.importDoc(ctx, userFrom(ctx), &doc)
	if err != nil {
		s.userFail(w, err, "import")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// importDoc adds what is not here yet.
func (s *Server) importDoc(ctx context.Context, u *store.User, doc *exportDoc) (map[string]any, error) {
	added, skipped, things := 0, 0, 0
	for _, eb := range doc.Boards {
		src := eb.Board
		ext := firstNonEmpty(src.ExtID, fmt.Sprintf("pwb:%d:%s", src.ID, src.CreatedAt))
		if _, err := s.store.BoardByExt(ctx, u.ID, ext); err == nil {
			skipped++
			continue
		}
		b := &store.Board{UserID: u.ID, Title: firstNonEmpty(cleanText(src.Title, maxName), "Imported board"), About: cleanText(src.About, maxLine), Template: firstNonEmpty(src.Template, "blank"),
			Background: src.Background, ExtID: ext}
		if !oneOf(b.Background, backgrounds) {
			b.Background = "dots"
		}
		if err := s.store.CreateBoard(ctx, b); err != nil {
			return nil, err
		}
		files := map[int64]int64{}
		for _, f := range eb.Files {
			data, err := base64.StdEncoding.DecodeString(f.Data)
			if err != nil || !allowedMime[f.Mime] {
				continue
			}
			up := &store.Upload{UserID: u.ID, BoardID: b.ID, Name: safeName(f.Name), Mime: f.Mime, Width: f.W, Height: f.H, Data: data}
			if f.Thumb != "" {
				up.Thumb, _ = base64.StdEncoding.DecodeString(f.Thumb)
			}
			if err := s.store.SaveUpload(ctx, up); err == nil {
				files[f.ID] = up.ID
			}
		}
		objs := []store.Object{}
		for _, o := range eb.Objects {
			op := store.Op{Op: "put", Obj: &o}
			if cleanOp(&op) != "" {
				continue
			}
			if o.Kind == "image" {
				var d map[string]any
				_ = json.Unmarshal(o.Data, &d)
				if nid := files[int64(num(d, "upload"))]; nid > 0 {
					d["upload"], d["src"] = nid, "/api/v1/uploads/"+itoa(nid)
					o.Data = mustJSON(d)
				} else {
					continue
				}
			}
			o.Source = ""
			objs = append(objs, o)
		}
		if len(objs) > 0 {
			ops := make([]store.Op, len(objs))
			for i := range objs {
				ops[i] = store.Op{Op: "put", ID: objs[i].ID, Obj: &objs[i]}
			}
			if _, err := s.store.ApplyOps(ctx, b.ID, ops, &u.ID, s.actorName(u)); err != nil {
				return nil, err
			}
		}
		things += len(objs)
		added++
	}
	msg := fmt.Sprintf("%d %s added", added, plural(added, "board", "boards"))
	if skipped > 0 {
		msg += fmt.Sprintf(", %d already here", skipped)
	}
	return map[string]any{"added": added, "skipped": skipped, "objects": things, "text": msg}, nil
}
