package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-whiteboard/internal/store"
)

const maxUpload = 25 << 20

var allowedMime = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true, "image/gif": true, "image/svg+xml": false,
	"application/pdf": true, "text/plain": true, "text/csv": true, "application/zip": true,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": true,
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": true, "application/vnd.oasis.opendocument.text": true,
	"application/vnd.oasis.opendocument.spreadsheet": true, "application/msword": true, "application/vnd.ms-excel": true}

// handleUpload stores a picture (shrunk by the browser) for a board the
// person may change.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, _ := strconv.ParseInt(r.URL.Query().Get("board"), 10, 64)
	if id == 0 {
		id, _ = strconv.ParseInt(r.Header.Get("X-Board"), 10, 64)
	}
	a, err := s.boardAccess(ctx, userFrom(ctx), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if !a.canEdit() {
		writeErr(w, http.StatusForbidden, "you can look at this board, not change it")
		return
	}
	s.saveUpload(w, r, a)
}

func (s *Server) saveUpload(w http.ResponseWriter, r *http.Request, a *actor) {
	ctx := r.Context()
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+(1<<20))
	if err := r.ParseMultipartForm(maxUpload); err != nil {
		writeErr(w, http.StatusBadRequest, "a picture can be up to 25 MB")
		return
	}
	up := &store.Upload{UserID: a.b.UserID, BoardID: a.b.ID}
	if a.u != nil {
		up.UserID = a.u.ID
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "no file")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxUpload+1))
	if err != nil || len(data) > maxUpload {
		writeErr(w, http.StatusBadRequest, "a picture can be up to 25 MB")
		return
	}
	mime := http.DetectContentType(data)
	if i := strings.Index(mime, ";"); i > 0 {
		mime = mime[:i]
	}
	if mime == "application/octet-stream" || mime == "text/plain" || mime == "application/zip" {
		if guess := mimeByExt(hdr.Filename); guess != "" {
			mime = guess
		}
	}
	if !allowedMime[mime] {
		writeErr(w, http.StatusBadRequest, "pictures, PDFs and documents (Word, Excel, PowerPoint, text, CSV) only")
		return
	}
	up.Mime, up.Data, up.Name = mime, data, safeName(hdr.Filename)
	up.Width, _ = strconv.Atoi(r.FormValue("width"))
	up.Height, _ = strconv.Atoi(r.FormValue("height"))
	if tf, _, err := r.FormFile("thumb"); err == nil {
		up.Thumb, _ = io.ReadAll(io.LimitReader(tf, 1<<20))
		tf.Close()
	}
	if err := s.store.SaveUpload(ctx, up); err != nil {
		s.fail(w, err, "upload")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"upload": up, "url": fmt.Sprintf("/api/v1/uploads/%d", up.ID)})
}

func mimeByExt(name string) string {
	return map[string]string{".pdf": "application/pdf", ".txt": "text/plain", ".md": "text/plain", ".csv": "text/csv", ".zip": "application/zip", ".mp3": "audio/mpeg",
		".m4a": "audio/mp4", ".mp4": "video/mp4", ".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", ".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation", ".ods": "application/vnd.oasis.opendocument.spreadsheet", ".odt": "application/vnd.oasis.opendocument.text", ".doc": "application/msword", ".xls": "application/vnd.ms-excel"}[strings.ToLower(filepath.Ext(name))]
}

func safeName(n string) string {
	n = filepath.Base(strings.ReplaceAll(n, "\\", "/"))
	n = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*[]()`, r) {
			return '_'
		}
		return r
	}, n)
	return firstNonEmpty(clip(strings.TrimSpace(n), 120), "file")
}

// mayGetUpload: someone on its board.
func (s *Server) mayGetUpload(ctx context.Context, u *store.User, up *store.Upload) bool {
	if u == nil {
		return false
	}
	if up.BoardID == 0 {
		return up.UserID == u.ID
	}
	_, err := s.boardAccess(ctx, u, up.BoardID)
	return err == nil
}

func (s *Server) serveUpload(w http.ResponseWriter, r *http.Request, up *store.Upload) {
	data := up.Data
	if queryBool(r, "thumb") && len(up.Thumb) > 0 {
		data = up.Thumb
	}
	w.Header().Set("Content-Type", up.Mime)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	disp := "inline"
	if !strings.HasPrefix(up.Mime, "image/") && up.Mime != "application/pdf" && !strings.HasPrefix(up.Mime, "audio/") && !strings.HasPrefix(up.Mime, "video/") {
		disp = "attachment"
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disp, strings.ReplaceAll(up.Name, `"`, "")))
	if up.Mime == "application/pdf" {
		// the browser's own PDF viewer (shown on the board) does not open in a sandbox
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'self'")
	} else {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	}
	http.ServeContent(w, r, up.Name, time.Time{}, bytes.NewReader(data))
}

func (s *Server) handleGetUpload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	up, err := s.store.GetUpload(ctx, idParam(r, "id"))
	if err != nil || !s.mayGetUpload(ctx, userFrom(ctx), up) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	s.serveUpload(w, r, up)
}
