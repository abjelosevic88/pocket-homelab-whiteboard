package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/abjelosevic88/pocket-homelab-whiteboard/internal/store"
)

// Organising boards (folders, your own templates), link previews for
// embeds, and cards kept in step with Pocket Tasks.

// ---- folders ----

func (s *Server) handleFolders(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.Folders(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "folders")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": list})
}

func (s *Server) handleSaveFolder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		Name     string `json:"name"`
		Position int    `json:"position"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	name := cleanText(in.Name, 60)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "give the folder a name")
		return
	}
	f := &store.Folder{ID: idParam(r, "fid"), Name: name, Position: in.Position}
	if err := s.store.SaveFolder(ctx, actorID(ctx), f); err != nil {
		s.fail(w, err, "folder")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"folder": f})
}

func (s *Server) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteFolder(r.Context(), actorID(r.Context()), idParam(r, "fid")); err != nil {
		s.fail(w, err, "folder")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleBoardFolder puts a board in one of the person's folders (theirs only).
func (s *Server) handleBoardFolder(w http.ResponseWriter, r *http.Request) {
	a, ok := s.boardParam(w, r)
	if !ok {
		return
	}
	var in struct {
		FolderID int64 `json:"folder_id"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.store.SetFolder(r.Context(), a.u.ID, a.b.ID, in.FolderID); err != nil {
		s.fail(w, err, "folder")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"folder_id": in.FolderID})
}

// ---- your own templates ----

// handleSaveTemplate keeps a board as one of the person's templates. Its
// pictures are copied (with no board), so the template outlives the board.
func (s *Server) handleSaveTemplate(w http.ResponseWriter, r *http.Request) {
	a, ok := s.boardParam(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	var in struct {
		Title string `json:"title"`
		About string `json:"about"`
		Emoji string `json:"emoji"`
	}
	_ = decode(r, &in)
	objs, err := s.store.Objects(ctx, a.b.ID)
	if err != nil {
		s.fail(w, err, "template")
		return
	}
	for i, o := range objs {
		if o.Kind != "image" && o.Kind != "embed" {
			continue
		}
		var d map[string]any
		_ = json.Unmarshal(o.Data, &d)
		old := int64(num(d, "upload"))
		if old == 0 {
			continue
		}
		up, err := s.store.GetUpload(ctx, old)
		if err != nil {
			continue
		}
		cp := *up
		cp.UserID, cp.BoardID = a.u.ID, 0
		if s.store.SaveUpload(ctx, &cp) == nil {
			d["upload"], d["src"] = cp.ID, "/api/v1/uploads/"+itoa(cp.ID)
			objs[i].Data = mustJSON(d)
		}
	}
	emoji, okE := cleanEmoji(in.Emoji)
	if !okE {
		emoji = "📋"
	}
	t := &store.UserTemplate{UserID: a.u.ID, Title: firstNonEmpty(cleanText(in.Title, maxName), a.b.Title), About: cleanText(firstNonEmpty(in.About, a.b.About), maxLine),
		Emoji: emoji, Background: a.b.Background, Preview: mustJSON(preview(objs))}
	if err := s.store.SaveTemplate(ctx, t, objs); err != nil {
		s.fail(w, err, "template")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"template": t, "key": "u:" + itoa(t.ID)})
}

func (s *Server) handleDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteTemplate(r.Context(), actorID(r.Context()), idParam(r, "tid")); err != nil {
		s.fail(w, err, "template")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- link previews ----

var errPrivate = errors.New("that address is on a private network")

// publicOnly refuses connections to this box, the local network and the
// tailnet: a link preview must not reach inside.
func publicOnly(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() ||
		(ip.To4() != nil && ip.To4()[0] == 100 && ip.To4()[1]&0xc0 == 64) { // 100.64.0.0/10 (Tailscale, CGNAT)
		return errPrivate
	}
	return nil
}

var unfurlClient = &http.Client{
	Timeout: 8 * time.Second,
	Transport: &http.Transport{
		DialContext:         (&net.Dialer{Timeout: 5 * time.Second, Control: publicOnly}).DialContext,
		TLSHandshakeTimeout: 5 * time.Second,
		MaxIdleConns:        4,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 4 {
			return errors.New("too many redirects")
		}
		return nil
	},
}

var (
	reMeta  = regexp.MustCompile(`(?is)<meta\s+[^>]*>`)
	reAttr  = regexp.MustCompile(`(?is)([a-z:_-]+)\s*=\s*("([^"]*)"|'([^']*)')`)
	reTitle = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	reIcon  = regexp.MustCompile(`(?is)<link\s+[^>]*rel\s*=\s*["'][^"']*icon[^"']*["'][^>]*>`)
	reYT    = regexp.MustCompile(`^(?:https?://)?(?:www\.|m\.)?(?:youtube\.com/(?:watch\?(?:.*&)?v=|shorts/|embed/|live/)|youtu\.be/)([A-Za-z0-9_-]{11})`)
)

// Preview is what a link shows on the board.
type Preview struct {
	URL         string `json:"url"`
	Kind        string `json:"kind"` // link | youtube
	Title       string `json:"title"`
	Description string `json:"description"`
	Image       string `json:"image"`
	Site        string `json:"site"`
	Icon        string `json:"icon"`
	Video       string `json:"video,omitempty"` // YouTube id
}

func (s *Server) unfurl(ctx context.Context, raw string) (*Preview, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, &userErr{"that is not a web address (https://…)"}
	}
	p := &Preview{URL: u.String(), Kind: "link", Site: strings.TrimPrefix(u.Hostname(), "www.")}
	if m := reYT.FindStringSubmatch(u.String()); m != nil {
		p.Kind, p.Video, p.Site = "youtube", m[1], "YouTube"
		p.Image = "https://i.ytimg.com/vi/" + m[1] + "/hqdefault.jpg"
	}
	key := "unfurl:" + p.URL
	if v, at, ok := s.store.CacheGet(ctx, key); ok {
		if t, err := time.Parse(time.RFC3339Nano, at); err == nil && time.Since(t) < 24*time.Hour {
			var c Preview
			if json.Unmarshal([]byte(v), &c) == nil {
				return &c, nil
			}
		}
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; PocketWhiteboard/1.0; link preview)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	res, err := unfurlClient.Do(req)
	if err != nil {
		if errors.Is(err, errPrivate) || strings.Contains(err.Error(), errPrivate.Error()) {
			return nil, &userErr{"links to this box or your own network get no preview"}
		}
		if p.Kind == "youtube" {
			return p, nil
		}
		return nil, &userErr{"the page did not answer"}
	}
	defer res.Body.Close()
	if !strings.Contains(res.Header.Get("Content-Type"), "html") {
		p.Title = firstNonEmpty(p.Title, pathTail(u))
		return p, nil
	}
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	page := string(body)
	if i := strings.Index(strings.ToLower(page), "</head>"); i > 0 {
		page = page[:i]
	}
	meta := map[string]string{}
	for _, tag := range reMeta.FindAllString(page, 200) {
		attrs := map[string]string{}
		for _, m := range reAttr.FindAllStringSubmatch(tag, -1) {
			attrs[strings.ToLower(m[1])] = html.UnescapeString(m[3] + m[4])
		}
		k := strings.ToLower(firstNonEmpty(attrs["property"], attrs["name"]))
		if k != "" && attrs["content"] != "" && meta[k] == "" {
			meta[k] = strings.TrimSpace(attrs["content"])
		}
	}
	title := ""
	if m := reTitle.FindStringSubmatch(page); m != nil {
		title = strings.TrimSpace(html.UnescapeString(m[1]))
	}
	p.Title = clip(firstNonEmpty(meta["og:title"], meta["twitter:title"], title, pathTail(u)), 200)
	p.Description = clip(firstNonEmpty(meta["og:description"], meta["twitter:description"], meta["description"]), 300)
	if p.Image == "" {
		p.Image = absURL(res.Request.URL, firstNonEmpty(meta["og:image"], meta["twitter:image"]))
	}
	if p.Kind != "youtube" {
		p.Site = firstNonEmpty(meta["og:site_name"], p.Site)
	}
	if m := reIcon.FindString(page); m != "" {
		for _, a := range reAttr.FindAllStringSubmatch(m, -1) {
			if strings.ToLower(a[1]) == "href" {
				p.Icon = absURL(res.Request.URL, html.UnescapeString(a[3]+a[4]))
			}
		}
	}
	if p.Icon == "" {
		p.Icon = res.Request.URL.Scheme + "://" + res.Request.URL.Host + "/favicon.ico"
	}
	if b, err := json.Marshal(p); err == nil {
		_ = s.store.CachePut(ctx, key, string(b))
	}
	return p, nil
}

func pathTail(u *url.URL) string {
	t := strings.Trim(u.Path, "/")
	if i := strings.LastIndex(t, "/"); i >= 0 {
		t = t[i+1:]
	}
	if t == "" {
		return u.Hostname()
	}
	if v, err := url.PathUnescape(t); err == nil {
		return v
	}
	return t
}

func absURL(base *url.URL, ref string) string {
	if ref == "" {
		return ""
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	out := base.ResolveReference(r)
	if out.Scheme != "http" && out.Scheme != "https" {
		return ""
	}
	return out.String()
}

func (s *Server) handleUnfurl(w http.ResponseWriter, r *http.Request) {
	p, err := s.unfurl(r.Context(), r.URL.Query().Get("url"))
	if err != nil {
		s.userFail(w, err, "preview")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"preview": p})
}

// ---- cards in step with Pocket Tasks ----

type cardData struct {
	Text     string `json:"text"`
	Column   string `json:"column"`
	Due      string `json:"due"`
	Done     bool   `json:"done"`
	Assignee *struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"assignee"`
	Task *struct {
		ID  int64  `json:"id"`
		URL string `json:"url"`
	} `json:"task"`
}

// handleCardTask makes a card a task in Pocket Tasks (or sends the card's
// title, due date and done to the task it already has).
func (s *Server) handleCardTask(w http.ResponseWriter, r *http.Request) {
	a, ok := s.editorParam(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	o, err := s.store.GetObject(ctx, a.b.ID, chiParam(r, "oid"))
	if err != nil || o.Kind != "card" || (o.HiddenBy != "" && o.HiddenBy != a.voter()) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	var d cardData
	_ = json.Unmarshal(o.Data, &d)
	title := clip(firstNonEmpty(firstLine(d.Text), "Card"), 200)
	if !validDate(d.Due) {
		d.Due = ""
	}
	sib, ok := s.sib(w, r, appTasks, scopeTasksWrite)
	if !ok {
		return
	}
	link := s.boardURL(a.b) + "?o=" + url.QueryEscape(o.ID)
	if d.Task == nil || d.Task.ID == 0 {
		notes := "A card on the board “" + a.b.Title + "”."
		if d.Assignee != nil && d.Assignee.Name != "" {
			notes += " For " + d.Assignee.Name + "."
		}
		t, err := sib.client.CreateTask(ctx, title, notes, d.Due, link)
		if err != nil {
			s.appFail(w, appTasks, err)
			return
		}
		var m map[string]any
		_ = json.Unmarshal(o.Data, &m)
		m["task"] = map[string]any{"id": t.ID, "url": linkTo(sib, "/tasks/"+strconv.FormatInt(t.ID, 10))}
		o.Data = mustJSON(m)
		if d.Done {
			_ = sib.client.Do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/tasks/%d/complete", t.ID), nil, map[string]any{"done": true}, nil)
		}
		s.taskColumnTag(ctx, sib, t.ID, d.Column)
		ops := []store.Op{{Op: "put", ID: o.ID, Obj: o}}
		v, err := s.store.ApplyOps(ctx, a.b.ID, ops, a.uid(), a.name, "")
		if err != nil {
			s.fail(w, err, "card")
			return
		}
		s.hub.send(a.b.ID, "", map[string]any{"type": "ops", "by": a.name, "ops": ops, "version": v})
		writeJSON(w, http.StatusOK, map[string]any{"object": o, "text": "Now a task in Pocket Tasks", "url": linkTo(sib, "/tasks/"+strconv.FormatInt(t.ID, 10))})
		return
	}
	body := map[string]any{"title": title, "url": link}
	if d.Due != "" {
		body["due"] = d.Due
	}
	if err := sib.client.Do(ctx, http.MethodPut, fmt.Sprintf("/api/v1/tasks/%d", d.Task.ID), nil, body, nil); err != nil {
		s.appFail(w, appTasks, err)
		return
	}
	if err := sib.client.Do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/tasks/%d/complete", d.Task.ID), nil, map[string]any{"done": d.Done}, nil); err != nil {
		s.appFail(w, appTasks, err)
		return
	}
	s.taskColumnTag(ctx, sib, d.Task.ID, d.Column)
	writeJSON(w, http.StatusOK, map[string]any{"object": o, "text": "Pocket Tasks is up to date", "url": d.Task.URL})
}

// taskColumnTag marks a task "doing" while its card sits in a Kanban
// "Doing" column (Pocket Tasks has only open and done).
func (s *Server) taskColumnTag(ctx context.Context, sib *siblingConn, id int64, column string) {
	if column == "" {
		return
	}
	var raw json.RawMessage
	if err := sib.client.Do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/tasks/%d", id), nil, nil, &raw); err != nil {
		return
	}
	var t struct {
		Tags []string        `json:"tags"`
		Task json.RawMessage `json:"task"`
	}
	_ = json.Unmarshal(raw, &t)
	if t.Tags == nil && len(t.Task) > 0 {
		_ = json.Unmarshal(t.Task, &t)
	}
	tags, has := []string{}, false
	for _, tg := range t.Tags {
		if strings.EqualFold(tg, "doing") {
			has = true
			continue
		}
		tags = append(tags, tg)
	}
	want := column == "doing"
	if want == has {
		return
	}
	if want {
		tags = append(tags, "doing")
	}
	_ = sib.client.Do(ctx, http.MethodPut, fmt.Sprintf("/api/v1/tasks/%d", id), nil, map[string]any{"tags": tags}, nil)
}

// handleCardsPull brings what changed in Pocket Tasks back onto the board's
// cards (done, title, due date).
func (s *Server) handleCardsPull(w http.ResponseWriter, r *http.Request) {
	a, ok := s.editorParam(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	objs, err := s.store.ObjectsFor(ctx, a.b.ID, a.voter())
	if err != nil {
		s.fail(w, err, "cards")
		return
	}
	var linked []store.Object
	for _, o := range objs {
		if o.Kind == "card" && strings.Contains(string(o.Data), `"task"`) {
			linked = append(linked, o)
		}
	}
	if len(linked) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"changed": 0})
		return
	}
	sib, err := s.clients(ctx, actorID(ctx)).conn(ctx, appTasks, scopeTasksRead)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"changed": 0, "why": plainErr(appTasks, err)})
		return
	}
	ops := []store.Op{}
	for i, o := range linked {
		if i >= 60 {
			break
		}
		var d cardData
		_ = json.Unmarshal(o.Data, &d)
		if d.Task == nil || d.Task.ID == 0 {
			continue
		}
		var t struct {
			Title  string  `json:"title"`
			Status string  `json:"status"`
			Due    *string `json:"due"`
		}
		var raw json.RawMessage
		if err := sib.client.Do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/tasks/%d", d.Task.ID), nil, nil, &raw); err != nil {
			continue
		}
		if json.Unmarshal(raw, &t) != nil || t.Title == "" {
			var wrap struct {
				Task json.RawMessage `json:"task"`
			}
			if json.Unmarshal(raw, &wrap) != nil || json.Unmarshal(wrap.Task, &t) != nil {
				continue
			}
		}
		done := t.Status == "done"
		due := ""
		if t.Due != nil {
			due = day10(*t.Due)
		}
		if done == d.Done && due == d.Due && (t.Title == "" || t.Title == firstLine(d.Text)) {
			continue
		}
		var m map[string]any
		_ = json.Unmarshal(o.Data, &m)
		m["done"] = done
		if due != "" {
			m["due"] = due
		} else {
			delete(m, "due")
		}
		if t.Title != "" && t.Title != firstLine(d.Text) {
			m["text"] = t.Title
		}
		oo := o
		oo.Data = mustJSON(m)
		ops = append(ops, store.Op{Op: "put", ID: o.ID, Obj: &oo})
	}
	if len(ops) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"changed": 0})
		return
	}
	v, err := s.store.ApplyOps(ctx, a.b.ID, ops, a.uid(), "Pocket Tasks", "")
	if err != nil {
		s.fail(w, err, "cards")
		return
	}
	s.hub.send(a.b.ID, "", map[string]any{"type": "ops", "by": "Pocket Tasks", "ops": ops, "version": v})
	writeJSON(w, http.StatusOK, map[string]any{"changed": len(ops), "version": v})
}
