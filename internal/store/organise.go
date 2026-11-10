package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

// Folder is one of a person's folders on the home page.
type Folder struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
	Boards   int    `json:"boards"`
}

// Folders lists a person's folders with how many boards each holds.
func (s *Store) Folders(ctx context.Context, uid int64) ([]Folder, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT f.id, f.name, f.position, (SELECT COUNT(1) FROM marks k WHERE k.user_id = f.user_id AND k.folder_id = f.id)
		FROM folders f WHERE f.user_id = ? ORDER BY f.position, f.id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Folder{}
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.ID, &f.Name, &f.Position, &f.Boards); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// SaveFolder adds (id 0) or renames a person's folder.
func (s *Store) SaveFolder(ctx context.Context, uid int64, f *Folder) error {
	if f.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO folders (user_id, name, position) VALUES (?, ?, (SELECT COALESCE(MAX(position), 0) + 1 FROM folders WHERE user_id = ?))`, uid, f.Name, uid)
		if err != nil {
			return err
		}
		f.ID, _ = res.LastInsertId()
		return nil
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE folders SET name = ?, position = ? WHERE id = ? AND user_id = ?`, f.Name, f.Position, f.ID, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteFolder removes a person's folder (its boards stay, outside any folder).
func (s *Store) DeleteFolder(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM folders WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetFolder puts a board in one of the person's folders (0 = none).
func (s *Store) SetFolder(ctx context.Context, uid, bid, fid int64) error {
	if fid > 0 {
		var n int
		if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM folders WHERE id = ? AND user_id = ?`, fid, uid).Scan(&n); err != nil || n == 0 {
			return ErrNotFound
		}
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO marks (user_id, board_id) VALUES (?, ?) ON CONFLICT DO NOTHING`, uid, bid); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE marks SET folder_id = ? WHERE user_id = ? AND board_id = ?`, nullI(&fid), uid, bid)
	return err
}

// FolderOf is the folder a person keeps a board in (0 = none).
func (s *Store) FolderOf(ctx context.Context, uid, bid int64) int64 {
	var f sql.NullInt64
	_ = s.DB.QueryRowContext(ctx, `SELECT folder_id FROM marks WHERE user_id = ? AND board_id = ?`, uid, bid).Scan(&f)
	return f.Int64
}

// ---- a person's own templates ----

// UserTemplate is a board kept to start new ones from.
type UserTemplate struct {
	ID         int64           `json:"id"`
	UserID     int64           `json:"-"`
	Title      string          `json:"title"`
	About      string          `json:"about"`
	Emoji      string          `json:"emoji"`
	Background string          `json:"background"`
	Preview    json.RawMessage `json:"preview"`
	CreatedAt  string          `json:"created_at"`
}

// SaveTemplate keeps a person's template.
func (s *Store) SaveTemplate(ctx context.Context, t *UserTemplate, objs []Object) error {
	if len(t.Preview) == 0 {
		t.Preview = json.RawMessage(`[]`)
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO user_templates (user_id, title, about, emoji, background, objects, preview) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		t.UserID, t.Title, t.About, t.Emoji, t.Background, packObjects(objs), string(t.Preview))
	if err != nil {
		return err
	}
	t.ID, _ = res.LastInsertId()
	t.CreatedAt = Now()
	return nil
}

// Templates lists a person's own templates, newest first.
func (s *Store) Templates(ctx context.Context, uid int64) ([]UserTemplate, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, user_id, title, about, emoji, background, preview, created_at FROM user_templates WHERE user_id = ? ORDER BY id DESC`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UserTemplate{}
	for rows.Next() {
		var t UserTemplate
		var p string
		if err := rows.Scan(&t.ID, &t.UserID, &t.Title, &t.About, &t.Emoji, &t.Background, &p, &t.CreatedAt); err != nil {
			return nil, err
		}
		if !json.Valid([]byte(p)) {
			p = "[]"
		}
		t.Preview = json.RawMessage(p)
		out = append(out, t)
	}
	return out, rows.Err()
}

// Template loads one of a person's templates with what is on it.
func (s *Store) Template(ctx context.Context, uid, id int64) (*UserTemplate, []Object, error) {
	var t UserTemplate
	var raw []byte
	var p string
	err := s.DB.QueryRowContext(ctx, `SELECT id, user_id, title, about, emoji, background, preview, created_at, objects FROM user_templates WHERE id = ? AND user_id = ?`, id, uid).
		Scan(&t.ID, &t.UserID, &t.Title, &t.About, &t.Emoji, &t.Background, &p, &t.CreatedAt, &raw)
	if err != nil {
		return nil, nil, notFound(err)
	}
	t.Preview = json.RawMessage(p)
	objs, err := unpackObjects(raw)
	return &t, objs, err
}

// DeleteTemplate removes one of a person's templates.
func (s *Store) DeleteTemplate(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM user_templates WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
