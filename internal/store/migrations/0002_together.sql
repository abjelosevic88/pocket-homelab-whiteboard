-- ---------- working together: private mode, comments, history, reactions ----------

-- Private mode: a note made while it is on is seen only by whoever made it
-- (u<id> or g<guest id>) until someone reveals them all.
ALTER TABLE objects ADD COLUMN hidden_by TEXT NOT NULL DEFAULT '';
ALTER TABLE boards ADD COLUMN private TEXT NOT NULL DEFAULT '{}';   -- {on, by, started_at}

-- Threaded comments, pinned to a spot or to a thing on the board.
CREATE TABLE comments (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  board_id    INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
  parent_id   INTEGER REFERENCES comments(id) ON DELETE CASCADE,   -- NULL: starts a thread
  object_id   TEXT NOT NULL DEFAULT '',                             -- pinned to a thing ('' = a spot)
  x           REAL NOT NULL DEFAULT 0,
  y           REAL NOT NULL DEFAULT 0,
  user_id     INTEGER REFERENCES users(id) ON DELETE SET NULL,      -- the writer (NULL: a guest)
  author      TEXT NOT NULL DEFAULT '',
  voter       TEXT NOT NULL DEFAULT '',                             -- u<id> or g<guest id>
  body        TEXT NOT NULL,
  mentions    TEXT NOT NULL DEFAULT '[]',                           -- user ids
  resolved    INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX comments_board ON comments(board_id, parent_id, id);
CREATE INDEX comments_user ON comments(user_id);

-- The board as it was: a snapshot at most every ten minutes while people work, or when asked.
CREATE TABLE versions (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  board_id    INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
  name        TEXT NOT NULL DEFAULT '',
  who         TEXT NOT NULL DEFAULT '',
  n           INTEGER NOT NULL DEFAULT 0,
  objects     BLOB NOT NULL,                                        -- gzip JSON
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX versions_board ON versions(board_id, created_at);

-- What was deleted, to put back.
CREATE TABLE trash (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  board_id    INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
  object_id   TEXT NOT NULL,
  kind        TEXT NOT NULL,
  text        TEXT NOT NULL DEFAULT '',
  object      TEXT NOT NULL,                                        -- the object as JSON
  deleted_by  TEXT NOT NULL DEFAULT '',
  deleted_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX trash_board ON trash(board_id, deleted_at);

-- Emoji reactions on things.
CREATE TABLE reactions (
  board_id    INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
  object_id   TEXT NOT NULL,
  emoji       TEXT NOT NULL,
  voter       TEXT NOT NULL,
  name        TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (board_id, object_id, emoji, voter)
);
