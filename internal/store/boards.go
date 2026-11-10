package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

const boardCols = `b.id, b.user_id, b.title, b.about, b.template, b.background, b.share_key, b.share_mode, b.timer_ends, b.timer_secs, b.vote, b.version, b.source, b.ext_id, b.created_at, b.updated_at, b.private, b.anonymous, b.poker,
	COALESCE(NULLIF(o.name, ''), o.email, '')`

func scanBoard(row interface{ Scan(...any) error }, extra ...any) (*Board, error) {
	var b Board
	var vote, priv, poker string
	dst := []any{&b.ID, &b.UserID, &b.Title, &b.About, &b.Template, &b.Background, &b.ShareKey, &b.ShareMode, &b.TimerEnds, &b.TimerSecs, &vote, &b.Version, &b.Source, &b.ExtID, &b.CreatedAt, &b.UpdatedAt, &priv, &b.Anonymous, &poker, &b.OwnerName}
	if err := row.Scan(append(dst, extra...)...); err != nil {
		return nil, notFound(err)
	}
	if !json.Valid([]byte(vote)) {
		vote = "{}"
	}
	b.Vote = json.RawMessage(vote)
	if !json.Valid([]byte(priv)) {
		priv = "{}"
	}
	b.Private = json.RawMessage(priv)
	if !json.Valid([]byte(poker)) {
		poker = "{}"
	}
	b.Poker = json.RawMessage(poker)
	return &b, nil
}

// VoteOf decodes a board's voting round.
func (b *Board) VoteOf() Vote {
	var v Vote
	_ = json.Unmarshal(b.Vote, &v)
	if v.PerPerson <= 0 {
		v.PerPerson = 3
	}
	return v
}

// Poker is a round of planning poker on one thing: everyone picks a number,
// nobody sees the others' picks until it is shown.
type Poker struct {
	Object   string            `json:"object,omitempty"`
	By       string            `json:"by,omitempty"`
	Started  string            `json:"started_at,omitempty"`
	Revealed bool              `json:"revealed,omitempty"`
	Picks    map[string]string `json:"picks,omitempty"` // voter → value
	Names    map[string]string `json:"names,omitempty"` // voter → name
}

// PokerOf decodes a board's planning poker round.
func (b *Board) PokerOf() Poker {
	var p Poker
	_ = json.Unmarshal(b.Poker, &p)
	if p.Picks == nil {
		p.Picks = map[string]string{}
	}
	if p.Names == nil {
		p.Names = map[string]string{}
	}
	return p
}

// PrivateOf decodes a board's private mode.
func (b *Board) PrivateOf() Private {
	var p Private
	_ = json.Unmarshal(b.Private, &p)
	return p
}

// CreateBoard adds a board.
func (s *Store) CreateBoard(ctx context.Context, b *Board) error {
	if b.ShareKey == "" {
		b.ShareKey = RandomToken(12)
	}
	if b.ShareMode == "" {
		b.ShareMode = "off"
	}
	if b.Background == "" {
		b.Background = "dots"
	}
	if b.Template == "" {
		b.Template = "blank"
	}
	if len(b.Vote) == 0 {
		b.Vote = json.RawMessage(`{}`)
	}
	now := Now()
	res, err := s.DB.ExecContext(ctx, `INSERT INTO boards (user_id, title, about, template, background, share_key, share_mode, vote, source, ext_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.UserID, b.Title, b.About, b.Template, b.Background, b.ShareKey, b.ShareMode, string(b.Vote), b.Source, b.ExtID, now, now)
	if err != nil {
		return conflict(err)
	}
	b.ID, _ = res.LastInsertId()
	b.CreatedAt, b.UpdatedAt = now, now
	return nil
}

// GetBoard loads a board (no access check).
func (s *Store) GetBoard(ctx context.Context, id int64) (*Board, error) {
	return scanBoard(s.DB.QueryRowContext(ctx, `SELECT `+boardCols+` FROM boards b JOIN users o ON o.id = b.user_id WHERE b.id = ?`, id))
}

// BoardByShareKey loads a board by its share link.
func (s *Store) BoardByShareKey(ctx context.Context, key string) (*Board, error) {
	if key == "" {
		return nil, ErrNotFound
	}
	return scanBoard(s.DB.QueryRowContext(ctx, `SELECT `+boardCols+` FROM boards b JOIN users o ON o.id = b.user_id WHERE b.share_key = ?`, key))
}

// BoardByExt finds a person's board by its import id.
func (s *Store) BoardByExt(ctx context.Context, uid int64, ext string) (*Board, error) {
	return scanBoard(s.DB.QueryRowContext(ctx, `SELECT `+boardCols+` FROM boards b JOIN users o ON o.id = b.user_id WHERE b.user_id = ? AND b.ext_id = ? AND b.ext_id != ''`, uid, ext))
}

// BoardsFor lists the boards a person made or is on, newest change first.
func (s *Store) BoardsFor(ctx context.Context, uid int64) ([]Board, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+boardCols+`,
		CASE WHEN b.user_id = ? THEN 'owner' ELSE COALESCE(m.role, '') END,
		COALESCE(k.starred, 0), COALESCE(k.opened_at, ''), COALESCE(k.folder_id, 0),
		(SELECT COUNT(1) FROM objects x WHERE x.board_id = b.id AND x.hidden_by = ''),
		(SELECT COUNT(1) FROM members y WHERE y.board_id = b.id)
		FROM boards b JOIN users o ON o.id = b.user_id
		LEFT JOIN members m ON m.board_id = b.id AND m.user_id = ?
		LEFT JOIN marks k ON k.board_id = b.id AND k.user_id = ?
		WHERE b.user_id = ? OR m.user_id IS NOT NULL
		ORDER BY b.updated_at DESC, b.id DESC`, uid, uid, uid, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Board
	for rows.Next() {
		var role string
		var starred int
		var b *Board
		var opened string
		var objs, mem int
		var folder int64
		if b, err = scanBoard(rows, &role, &starred, &opened, &folder, &objs, &mem); err != nil {
			return nil, err
		}
		b.Role, b.Starred, b.OpenedAt, b.Objects, b.Members, b.FolderID = role, starred == 1, opened, objs, mem, folder
		out = append(out, *b)
	}
	return out, rows.Err()
}

// OwnBoards lists the boards a person made (exports).
func (s *Store) OwnBoards(ctx context.Context, uid int64) ([]Board, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+boardCols+` FROM boards b JOIN users o ON o.id = b.user_id WHERE b.user_id = ? ORDER BY b.id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Board
	for rows.Next() {
		b, err := scanBoard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

// UpdateBoard saves the editable fields.
func (s *Store) UpdateBoard(ctx context.Context, b *Board) error {
	b.UpdatedAt = Now()
	if len(b.Private) == 0 {
		b.Private = json.RawMessage(`{}`)
	}
	if len(b.Poker) == 0 {
		b.Poker = json.RawMessage(`{}`)
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE boards SET title = ?, about = ?, background = ?, share_mode = ?, share_key = ?, timer_ends = ?, timer_secs = ?, vote = ?, private = ?, anonymous = ?, poker = ?, version = version + 1, updated_at = ? WHERE id = ?`,
		b.Title, b.About, b.Background, b.ShareMode, b.ShareKey, b.TimerEnds, b.TimerSecs, string(b.Vote), string(b.Private), b.Anonymous, string(b.Poker), b.UpdatedAt, b.ID)
	return err
}

// DeleteBoard removes a board and everything on it.
func (s *Store) DeleteBoard(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM boards WHERE id = ?`, id)
	return err
}

// Mark sets a person's star / last-opened on a board.
func (s *Store) Mark(ctx context.Context, uid, bid int64, starred *bool, opened bool) error {
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO marks (user_id, board_id) VALUES (?, ?) ON CONFLICT DO NOTHING`, uid, bid); err != nil {
		return err
	}
	if starred != nil {
		if _, err := s.DB.ExecContext(ctx, `UPDATE marks SET starred = ? WHERE user_id = ? AND board_id = ?`, boolInt(*starred), uid, bid); err != nil {
			return err
		}
	}
	if opened {
		if _, err := s.DB.ExecContext(ctx, `UPDATE marks SET opened_at = ? WHERE user_id = ? AND board_id = ?`, Now(), uid, bid); err != nil {
			return err
		}
	}
	return nil
}

// Starred says whether a person starred a board.
func (s *Store) Starred(ctx context.Context, uid, bid int64) bool {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT starred FROM marks WHERE user_id = ? AND board_id = ?`, uid, bid).Scan(&n)
	return n == 1
}

// ---- members ----

// Membership is a person's role on a board ("" = not on it).
func (s *Store) Membership(ctx context.Context, bid, uid int64) string {
	var role string
	_ = s.DB.QueryRowContext(ctx, `SELECT role FROM members WHERE board_id = ? AND user_id = ?`, bid, uid).Scan(&role)
	return role
}

// Members lists the people on a board.
func (s *Store) Members(ctx context.Context, bid int64) ([]Member, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT m.board_id, m.user_id, m.role, u.name, u.email, m.added_at FROM members m JOIN users u ON u.id = m.user_id WHERE m.board_id = ? ORDER BY m.added_at, m.user_id`, bid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.BoardID, &m.UserID, &m.Role, &m.Name, &m.Email, &m.AddedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetMember adds a person to a board or changes their role.
func (s *Store) SetMember(ctx context.Context, bid, uid int64, role string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO members (board_id, user_id, role) VALUES (?, ?, ?) ON CONFLICT(board_id, user_id) DO UPDATE SET role = excluded.role`, bid, uid, role)
	return err
}

// RemoveMember takes a person off a board.
func (s *Store) RemoveMember(ctx context.Context, bid, uid int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM members WHERE board_id = ? AND user_id = ?`, bid, uid)
	return err
}

// ---- objects ----

const objCols = `id, board_id, user_id, author, kind, x, y, w, h, z, data, source, updated_by, created_at, updated_at, hidden_by`

func scanObject(row interface{ Scan(...any) error }) (*Object, error) {
	var o Object
	var data string
	var uid sql.NullInt64
	if err := row.Scan(&o.ID, &o.BoardID, &uid, &o.Author, &o.Kind, &o.X, &o.Y, &o.W, &o.H, &o.Z, &data, &o.Source, &o.UpdatedBy, &o.CreatedAt, &o.UpdatedAt, &o.HiddenBy); err != nil {
		return nil, notFound(err)
	}
	if uid.Valid {
		v := uid.Int64
		o.UserID = &v
	}
	if !json.Valid([]byte(data)) {
		data = "{}"
	}
	o.Data = json.RawMessage(data)
	o.Hidden = o.HiddenBy != ""
	return &o, nil
}

func (s *Store) objects(ctx context.Context, q string, args ...any) ([]Object, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+objCols+` FROM objects WHERE `+q+` ORDER BY z, created_at, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Object{}
	for rows.Next() {
		o, err := scanObject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *o)
	}
	return out, rows.Err()
}

// Objects lists what everyone sees on a board, back to front (notes still
// private to someone are left out).
func (s *Store) Objects(ctx context.Context, bid int64) ([]Object, error) {
	return s.objects(ctx, `board_id = ? AND hidden_by = ''`, bid)
}

// ObjectsFor lists what one person sees: everyone's, and their own private notes.
func (s *Store) ObjectsFor(ctx context.Context, bid int64, voter string) ([]Object, error) {
	return s.objects(ctx, `board_id = ? AND (hidden_by = '' OR hidden_by = ?)`, bid, voter)
}

// GetObject loads one thing on a board (private or not).
func (s *Store) GetObject(ctx context.Context, bid int64, id string) (*Object, error) {
	return scanObject(s.DB.QueryRowContext(ctx, `SELECT `+objCols+` FROM objects WHERE board_id = ? AND id = ?`, bid, id))
}

// Op is one change to a board: put (add or replace) an object, or delete it.
type Op struct {
	Op  string  `json:"op"` // put | del
	ID  string  `json:"id,omitempty"`
	Obj *Object `json:"obj,omitempty"`
}

// versionEvery is how often, at most, the board is kept as it was while people work on it.
const versionEvery = 10 * time.Minute

// ApplyOps saves a batch of changes in one transaction and moves the
// board's version on. A put keeps who first made the object; a new one
// made while private mode is on is hidden for everyone but hideAs.
// Deleted things go to the trash; before the first change after a quiet
// while, the board as it was is kept as a version.
func (s *Store) ApplyOps(ctx context.Context, bid int64, ops []Op, uid *int64, who, hideAs string) (int64, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := Now()
	if who != "" {
		if err := s.autoVersion(ctx, tx, bid, who); err != nil {
			return 0, err
		}
	}
	for _, op := range ops {
		switch op.Op {
		case "put":
			o := op.Obj
			data := string(o.Data)
			if data == "" || !json.Valid([]byte(data)) {
				data = "{}"
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO objects (id, board_id, user_id, author, kind, x, y, w, h, z, data, source, updated_by, created_at, updated_at, hidden_by)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(board_id, id) DO UPDATE SET kind = excluded.kind, x = excluded.x, y = excluded.y, w = excluded.w, h = excluded.h, z = excluded.z,
				data = excluded.data, updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
				o.ID, bid, nullI(uid), who, o.Kind, o.X, o.Y, o.W, o.H, o.Z, data, o.Source, who, now, now, hideAs); err != nil {
				return 0, err
			}
		case "del":
			var kind, data, hidden string
			err := tx.QueryRowContext(ctx, `SELECT kind, data, hidden_by FROM objects WHERE board_id = ? AND id = ?`, bid, op.ID).Scan(&kind, &data, &hidden)
			if err != nil {
				continue
			}
			if hidden == "" {
				o, _ := scanObject(tx.QueryRowContext(ctx, `SELECT `+objCols+` FROM objects WHERE board_id = ? AND id = ?`, bid, op.ID))
				if o != nil {
					b, _ := json.Marshal(o)
					var d struct {
						Text  string `json:"text"`
						Title string `json:"title"`
					}
					_ = json.Unmarshal([]byte(data), &d)
					text := d.Text
					if text == "" {
						text = d.Title
					}
					if _, err := tx.ExecContext(ctx, `INSERT INTO trash (board_id, object_id, kind, text, object, deleted_by, deleted_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
						bid, op.ID, kind, truncate(text, 300), string(b), who, now); err != nil {
						return 0, err
					}
				}
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM objects WHERE board_id = ? AND id = ?`, bid, op.ID); err != nil {
				return 0, err
			}
			_, _ = tx.ExecContext(ctx, `DELETE FROM reactions WHERE board_id = ? AND object_id = ?`, bid, op.ID)
		}
	}
	var v int64
	if err := tx.QueryRowContext(ctx, `UPDATE boards SET version = version + 1, updated_at = ? WHERE id = ? RETURNING version`, now, bid).Scan(&v); err != nil {
		return 0, err
	}
	return v, tx.Commit()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// SearchObjects finds the words on a person's boards.
func (s *Store) SearchObjects(ctx context.Context, uid int64, q string, limit int) ([]Object, map[int64]string, error) {
	like := "%" + strings.ToLower(q) + "%"
	rows, err := s.DB.QueryContext(ctx, `SELECT o.id, o.board_id, o.user_id, o.author, o.kind, o.x, o.y, o.w, o.h, o.z, o.data, o.source, o.updated_by, o.created_at, o.updated_at, b.title
		FROM objects o JOIN boards b ON b.id = o.board_id
		LEFT JOIN members m ON m.board_id = b.id AND m.user_id = ?
		WHERE (b.user_id = ? OR m.user_id IS NOT NULL) AND o.hidden_by = '' AND o.kind IN ('sticky','text','shape','frame') AND LOWER(json_extract(o.data, '$.text')) LIKE ?
		ORDER BY o.updated_at DESC LIMIT ?`, uid, uid, like, limit)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	out := []Object{}
	titles := map[int64]string{}
	for rows.Next() {
		var o Object
		var data, title string
		var u sql.NullInt64
		if err := rows.Scan(&o.ID, &o.BoardID, &u, &o.Author, &o.Kind, &o.X, &o.Y, &o.W, &o.H, &o.Z, &data, &o.Source, &o.UpdatedBy, &o.CreatedAt, &o.UpdatedAt, &title); err != nil {
			return nil, nil, err
		}
		o.Data = json.RawMessage(data)
		titles[o.BoardID] = title
		out = append(out, o)
	}
	return out, titles, rows.Err()
}

// ---- votes ----

// Votes counts the votes per object in a round, and the voter's own.
func (s *Store) Votes(ctx context.Context, bid int64, round int, voter string) (map[string]int, map[string]int, int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT object_id, voter, n FROM votes WHERE board_id = ? AND round = ?`, bid, round)
	if err != nil {
		return nil, nil, 0, err
	}
	defer rows.Close()
	all, mine := map[string]int{}, map[string]int{}
	voters := map[string]bool{}
	for rows.Next() {
		var id, v string
		var n int
		if err := rows.Scan(&id, &v, &n); err != nil {
			return nil, nil, 0, err
		}
		all[id] += n
		voters[v] = true
		if v == voter {
			mine[id] += n
		}
	}
	return all, mine, len(voters), rows.Err()
}

// CastVote adds (delta 1) or takes back (delta -1) one vote.
func (s *Store) CastVote(ctx context.Context, bid int64, round int, obj, voter string, delta int) error {
	if delta > 0 {
		_, err := s.DB.ExecContext(ctx, `INSERT INTO votes (board_id, round, object_id, voter, n) VALUES (?, ?, ?, ?, 1) ON CONFLICT DO UPDATE SET n = n + 1`, bid, round, obj, voter)
		return err
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE votes SET n = n - 1 WHERE board_id = ? AND round = ? AND object_id = ? AND voter = ?`, bid, round, obj, voter); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM votes WHERE board_id = ? AND round = ? AND n <= 0`, bid, round)
	return err
}

// ---- uploads ----

// SaveUpload stores a picture.
func (s *Store) SaveUpload(ctx context.Context, up *Upload) error {
	up.Bytes = len(up.Data)
	var thumb any
	if len(up.Thumb) > 0 {
		thumb = up.Thumb
	}
	var board any
	if up.BoardID > 0 {
		board = up.BoardID
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO uploads (user_id, board_id, name, mime, bytes, width, height, data, thumb) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		up.UserID, board, up.Name, up.Mime, up.Bytes, up.Width, up.Height, up.Data, thumb)
	if err != nil {
		return err
	}
	up.ID, _ = res.LastInsertId()
	up.CreatedAt = Now()
	return nil
}

// GetUpload loads a picture with its bytes.
func (s *Store) GetUpload(ctx context.Context, id int64) (*Upload, error) {
	var up Upload
	var board *int64
	err := s.DB.QueryRowContext(ctx, `SELECT id, user_id, board_id, name, mime, bytes, width, height, data, COALESCE(thumb, X''), created_at FROM uploads WHERE id = ?`, id).
		Scan(&up.ID, &up.UserID, &board, &up.Name, &up.Mime, &up.Bytes, &up.Width, &up.Height, &up.Data, &up.Thumb, &up.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	if board != nil {
		up.BoardID = *board
	}
	return &up, nil
}

// PurgeUploads removes pictures no object on their board shows any more
// that are older than before.
func (s *Store) PurgeUploads(ctx context.Context, before string) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM uploads WHERE created_at < ? AND board_id IS NOT NULL AND NOT EXISTS (
		SELECT 1 FROM objects o WHERE o.board_id = uploads.board_id AND o.kind = 'image' AND json_extract(o.data, '$.upload') = uploads.id)`, before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
