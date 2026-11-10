-- ---------- organising: folders, your own templates ----------

-- Each person's own folders on the home page; a board sits in one of them
-- for that person only (marks.folder_id), so a shared board can be in
-- different folders for different people.
CREATE TABLE folders (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  position    INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX folders_user ON folders(user_id, position);
ALTER TABLE marks ADD COLUMN folder_id INTEGER REFERENCES folders(id) ON DELETE SET NULL;

-- A board kept as a template by its person: what was on it, and the pictures (uploads with no board).
CREATE TABLE user_templates (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title       TEXT NOT NULL,
  about       TEXT NOT NULL DEFAULT '',
  emoji       TEXT NOT NULL DEFAULT '📋',
  background  TEXT NOT NULL DEFAULT 'dots',
  objects     BLOB NOT NULL,
  preview     TEXT NOT NULL DEFAULT '[]',
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX user_templates_user ON user_templates(user_id);
