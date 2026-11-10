package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"time"
)

// ---- private mode ----

// Reveal shows every private note on a board to everyone.
func (s *Store) Reveal(ctx context.Context, bid int64) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE objects SET hidden_by = '' WHERE board_id = ? AND hidden_by != ''`, bid)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PrivateCount is how many private notes one person has on a board.
type PrivateCount struct {
	Voter string `json:"voter"`
	Name  string `json:"name"`
	N     int    `json:"n"`
}

// PrivateCounts says who has how many private notes (never what is in them).
func (s *Store) PrivateCounts(ctx context.Context, bid int64) ([]PrivateCount, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT hidden_by, MAX(author), COUNT(1) FROM objects WHERE board_id = ? AND hidden_by != '' AND kind != 'line' GROUP BY hidden_by ORDER BY MIN(created_at)`, bid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PrivateCount{}
	for rows.Next() {
		var c PrivateCount
		if err := rows.Scan(&c.Voter, &c.Name, &c.N); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---- history ----

func packObjects(objs []Object) []byte {
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	_ = json.NewEncoder(zw).Encode(objs)
	_ = zw.Close()
	return b.Bytes()
}

func unpackObjects(raw []byte) ([]Object, error) {
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	var out []Object
	err = json.NewDecoder(io.LimitReader(zr, 1<<30)).Decode(&out)
	return out, err
}

type querier interface {
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
}

func objectsIn(ctx context.Context, q querier, bid int64) ([]Object, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+objCols+` FROM objects WHERE board_id = ? AND hidden_by = '' ORDER BY z, created_at, id`, bid)
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

func saveVersion(ctx context.Context, q querier, bid int64, name, who string) (int64, error) {
	objs, err := objectsIn(ctx, q, bid)
	if err != nil {
		return 0, err
	}
	res, err := q.ExecContext(ctx, `INSERT INTO versions (board_id, name, who, n, objects, created_at) VALUES (?, ?, ?, ?, ?, ?)`, bid, name, who, len(objs), packObjects(objs), Now())
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	// keep the last 200 per board
	_, _ = q.ExecContext(ctx, `DELETE FROM versions WHERE board_id = ? AND id NOT IN (SELECT id FROM versions WHERE board_id = ? ORDER BY id DESC LIMIT 200)`, bid, bid)
	return id, nil
}

// autoVersion keeps the board as it was before the first change after a
// quiet while (nothing kept in versionEvery), when there is anything on it.
func (s *Store) autoVersion(ctx context.Context, tx *sql.Tx, bid int64, who string) error {
	var last string
	_ = tx.QueryRowContext(ctx, `SELECT MAX(created_at) FROM versions WHERE board_id = ?`, bid).Scan(&last)
	if last != "" {
		if t, err := time.Parse(time.RFC3339Nano, last); err == nil && time.Since(t) < versionEvery {
			return nil
		}
	}
	var n int
	_ = tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM objects WHERE board_id = ? AND hidden_by = ''`, bid).Scan(&n)
	if n == 0 {
		return nil
	}
	_, err := saveVersion(ctx, tx, bid, "", who)
	return err
}

// SaveVersion keeps the board as it is now, with a name.
func (s *Store) SaveVersion(ctx context.Context, bid int64, name, who string) (int64, error) {
	return saveVersion(ctx, s.DB, bid, name, who)
}

// Versions lists a board's kept versions, newest first.
func (s *Store) Versions(ctx context.Context, bid int64) ([]Version, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, board_id, name, who, n, created_at FROM versions WHERE board_id = ? ORDER BY id DESC`, bid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Version{}
	for rows.Next() {
		var v Version
		if err := rows.Scan(&v.ID, &v.BoardID, &v.Name, &v.Who, &v.N, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// VersionObjects loads what was on the board in a version.
func (s *Store) VersionObjects(ctx context.Context, bid, vid int64) (*Version, []Object, error) {
	var v Version
	var raw []byte
	err := s.DB.QueryRowContext(ctx, `SELECT id, board_id, name, who, n, created_at, objects FROM versions WHERE board_id = ? AND id = ?`, bid, vid).
		Scan(&v.ID, &v.BoardID, &v.Name, &v.Who, &v.N, &v.CreatedAt, &raw)
	if err != nil {
		return nil, nil, notFound(err)
	}
	objs, err := unpackObjects(raw)
	return &v, objs, err
}

// RestoreVersion puts the board back as it was in a version (after keeping
// how it is now as another version). Private notes stay as they are.
func (s *Store) RestoreVersion(ctx context.Context, bid, vid int64, who string) (int64, error) {
	_, objs, err := s.VersionObjects(ctx, bid, vid)
	if err != nil {
		return 0, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := saveVersion(ctx, tx, bid, "Before going back", who); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM objects WHERE board_id = ? AND hidden_by = ''`, bid); err != nil {
		return 0, err
	}
	now := Now()
	for _, o := range objs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO objects (id, board_id, user_id, author, kind, x, y, w, h, z, data, source, updated_by, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(board_id, id) DO NOTHING`,
			o.ID, bid, nullI(o.UserID), o.Author, o.Kind, o.X, o.Y, o.W, o.H, o.Z, string(o.Data), o.Source, who, firstNonEmptyS(o.CreatedAt, now), now); err != nil {
			return 0, err
		}
	}
	var v int64
	if err := tx.QueryRowContext(ctx, `UPDATE boards SET version = version + 1, updated_at = ? WHERE id = ? RETURNING version`, now, bid).Scan(&v); err != nil {
		return 0, err
	}
	return v, tx.Commit()
}

func firstNonEmptyS(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// Trash lists what was deleted from a board, newest first (the last 300).
func (s *Store) Trash(ctx context.Context, bid int64) ([]Trashed, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, object_id, kind, text, deleted_by, deleted_at FROM trash WHERE board_id = ? ORDER BY id DESC LIMIT 300`, bid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Trashed{}
	for rows.Next() {
		var t Trashed
		if err := rows.Scan(&t.ID, &t.ObjectID, &t.Kind, &t.Text, &t.DeletedBy, &t.DeletedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TrashedObject loads something from the trash.
func (s *Store) TrashedObject(ctx context.Context, bid, tid int64) (*Object, error) {
	var raw string
	if err := s.DB.QueryRowContext(ctx, `SELECT object FROM trash WHERE board_id = ? AND id = ?`, bid, tid).Scan(&raw); err != nil {
		return nil, notFound(err)
	}
	var o Object
	if err := json.Unmarshal([]byte(raw), &o); err != nil {
		return nil, err
	}
	return &o, nil
}

// DropTrashed takes something out of the trash (it was put back).
func (s *Store) DropTrashed(ctx context.Context, bid, tid int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM trash WHERE board_id = ? AND id = ?`, bid, tid)
	return err
}

// PurgeHistory forgets deleted things and unnamed versions older than before.
func (s *Store) PurgeHistory(ctx context.Context, before string) {
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM trash WHERE deleted_at < ?`, before)
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM versions WHERE name = '' AND created_at < ?`, before)
}

// ---- comments ----

const commentCols = `id, board_id, parent_id, object_id, x, y, user_id, author, voter, body, mentions, resolved, created_at, updated_at`

func scanComment(row interface{ Scan(...any) error }) (*Comment, error) {
	var c Comment
	var parent, uid sql.NullInt64
	var mentions string
	var resolved int
	if err := row.Scan(&c.ID, &c.BoardID, &parent, &c.ObjectID, &c.X, &c.Y, &uid, &c.Author, &c.Voter, &c.Body, &mentions, &resolved, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	if parent.Valid {
		v := parent.Int64
		c.ParentID = &v
	}
	if uid.Valid {
		v := uid.Int64
		c.UserID = &v
	}
	_ = json.Unmarshal([]byte(mentions), &c.Mentions)
	if c.Mentions == nil {
		c.Mentions = []int64{}
	}
	c.Resolved = resolved == 1
	return &c, nil
}

// Comments lists every comment on a board, oldest first.
func (s *Store) Comments(ctx context.Context, bid int64) ([]Comment, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+commentCols+` FROM comments WHERE board_id = ? ORDER BY id`, bid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Comment{}
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// GetComment loads one comment.
func (s *Store) GetComment(ctx context.Context, bid, id int64) (*Comment, error) {
	return scanComment(s.DB.QueryRowContext(ctx, `SELECT `+commentCols+` FROM comments WHERE board_id = ? AND id = ?`, bid, id))
}

// AddComment saves a comment.
func (s *Store) AddComment(ctx context.Context, c *Comment) error {
	m, _ := json.Marshal(c.Mentions)
	now := Now()
	res, err := s.DB.ExecContext(ctx, `INSERT INTO comments (board_id, parent_id, object_id, x, y, user_id, author, voter, body, mentions, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.BoardID, nullI(c.ParentID), c.ObjectID, c.X, c.Y, nullI(c.UserID), c.Author, c.Voter, c.Body, string(m), now, now)
	if err != nil {
		return err
	}
	c.ID, _ = res.LastInsertId()
	c.CreatedAt, c.UpdatedAt = now, now
	return nil
}

// UpdateComment saves the text, the pin and whether the thread is resolved.
func (s *Store) UpdateComment(ctx context.Context, c *Comment) error {
	m, _ := json.Marshal(c.Mentions)
	c.UpdatedAt = Now()
	_, err := s.DB.ExecContext(ctx, `UPDATE comments SET body = ?, mentions = ?, resolved = ?, x = ?, y = ?, object_id = ?, updated_at = ? WHERE id = ?`,
		c.Body, string(m), boolInt(c.Resolved), c.X, c.Y, c.ObjectID, c.UpdatedAt, c.ID)
	return err
}

// DeleteComment removes a comment (a thread with its replies).
func (s *Store) DeleteComment(ctx context.Context, bid, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM comments WHERE board_id = ? AND id = ?`, bid, id)
	return err
}

// ---- reactions ----

// Reaction is one emoji on a thing, with who gave it.
type Reaction struct {
	Emoji  string   `json:"emoji"`
	Names  []string `json:"names"`
	Voters []string `json:"voters"`
}

// ToggleReaction gives an emoji to a thing, or takes it back.
func (s *Store) ToggleReaction(ctx context.Context, bid int64, obj, emoji, voter, name string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM reactions WHERE board_id = ? AND object_id = ? AND emoji = ? AND voter = ?`, bid, obj, emoji, voter)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO reactions (board_id, object_id, emoji, voter, name) VALUES (?, ?, ?, ?, ?)`, bid, obj, emoji, voter, name)
	return err
}

// Reactions lists the reactions on a board by thing (one thing when obj != "").
func (s *Store) Reactions(ctx context.Context, bid int64, obj string) (map[string][]Reaction, error) {
	q := `SELECT object_id, emoji, voter, name FROM reactions WHERE board_id = ?`
	args := []any{bid}
	if obj != "" {
		q += ` AND object_id = ?`
		args = append(args, obj)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]Reaction{}
	for rows.Next() {
		var id, emoji, voter, name string
		if err := rows.Scan(&id, &emoji, &voter, &name); err != nil {
			return nil, err
		}
		list := out[id]
		found := false
		for i := range list {
			if list[i].Emoji == emoji {
				list[i].Names = append(list[i].Names, name)
				list[i].Voters = append(list[i].Voters, voter)
				found = true
			}
		}
		if !found {
			list = append(list, Reaction{Emoji: emoji, Names: []string{name}, Voters: []string{voter}})
		}
		out[id] = list
	}
	return out, rows.Err()
}
