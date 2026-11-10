CREATE TABLE users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  email         TEXT NOT NULL UNIQUE,
  name          TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'member',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT
);

CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE api_tokens (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL DEFAULT '',
  token_hash   TEXT NOT NULL UNIQUE,
  prefix       TEXT NOT NULL DEFAULT '',
  last_used_at TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Box-wide settings (title, setup flag); everything else is personal.
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE user_settings (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key     TEXT NOT NULL,
  value   TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, key)
);

CREATE TABLE cache (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL DEFAULT '',
  fetched_at TEXT NOT NULL
);



-- ---------- Pocket Whiteboard ----------
-- A board belongs to the person who made it (boards.user_id). Others reach
-- it only as members (people on this box: editors change it, viewers look)
-- or through its share link (people without an account; view or edit, as
-- the maker chose). Box admins have no way in to a board they are not in.
-- Times are UTC ISO strings; positions are board units (1 = one pixel at 100%).

CREATE TABLE boards (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,  -- the maker
  title       TEXT NOT NULL,
  about       TEXT NOT NULL DEFAULT '',
  template    TEXT NOT NULL DEFAULT 'blank',
  background  TEXT NOT NULL DEFAULT 'dots',      -- dots | grid | plain
  share_key   TEXT NOT NULL UNIQUE,              -- the link for people without an account
  share_mode  TEXT NOT NULL DEFAULT 'off',       -- off | view | edit
  timer_ends  TEXT NOT NULL DEFAULT '',          -- a countdown everyone on the board sees
  timer_secs  INTEGER NOT NULL DEFAULT 0,
  vote        TEXT NOT NULL DEFAULT '{}',        -- the voting round: {round, per_person, open, ...}
  version     INTEGER NOT NULL DEFAULT 0,        -- goes up with every change
  source      TEXT NOT NULL DEFAULT '',          -- the app that made it (link token), '' here
  ext_id      TEXT NOT NULL DEFAULT '',          -- for idempotent imports
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX boards_user ON boards(user_id, updated_at);
CREATE UNIQUE INDEX boards_ext ON boards(user_id, ext_id) WHERE ext_id != '';

-- People on this box on a board.
CREATE TABLE members (
  board_id  INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
  user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role      TEXT NOT NULL DEFAULT 'editor',   -- editor | viewer
  added_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (board_id, user_id)
);
CREATE INDEX members_user ON members(user_id);

-- Each person's own marks on a board: starred, last opened.
CREATE TABLE marks (
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  board_id   INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
  starred    INTEGER NOT NULL DEFAULT 0,
  opened_at  TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, board_id)
);

-- What is on a board: sticky notes, text, shapes, lines, frames, drawings
-- and pictures. The id is made by the browser, so a new note shows at once
-- and the same change sent twice lands once. Kind-specific things (text,
-- colour, line ends, points) live in data as JSON.
CREATE TABLE objects (
  id          TEXT NOT NULL,
  board_id    INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
  user_id     INTEGER REFERENCES users(id) ON DELETE SET NULL,   -- who put it there (NULL: a guest)
  author      TEXT NOT NULL DEFAULT '',
  kind        TEXT NOT NULL,      -- sticky | text | shape | line | frame | pen | image
  x           REAL NOT NULL DEFAULT 0,
  y           REAL NOT NULL DEFAULT 0,
  w           REAL NOT NULL DEFAULT 0,
  h           REAL NOT NULL DEFAULT 0,
  z           REAL NOT NULL DEFAULT 0,
  data        TEXT NOT NULL DEFAULT '{}',
  source      TEXT NOT NULL DEFAULT '',
  updated_by  TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (board_id, id)
);
CREATE INDEX objects_user ON objects(user_id);

-- Dot votes in a board's voting round. voter = u<id> or g<guest id>.
CREATE TABLE votes (
  board_id   INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
  round      INTEGER NOT NULL,
  object_id  TEXT NOT NULL,
  voter      TEXT NOT NULL,
  n          INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY (board_id, round, object_id, voter)
);

-- Pictures put on a board, kept in the database.
CREATE TABLE uploads (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,   -- who put it up (a guest's: the board's maker)
  board_id    INTEGER REFERENCES boards(id) ON DELETE CASCADE,
  name        TEXT NOT NULL DEFAULT '',
  mime        TEXT NOT NULL DEFAULT '',
  bytes       INTEGER NOT NULL DEFAULT 0,
  width       INTEGER NOT NULL DEFAULT 0,
  height      INTEGER NOT NULL DEFAULT 0,
  data        BLOB NOT NULL,
  thumb       BLOB,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX uploads_user ON uploads(user_id);
CREATE INDEX uploads_board ON uploads(board_id);
