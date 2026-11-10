# Backup

Everything lives in `DATA_DIR`: the SQLite file `pocket-whiteboard.db` (WAL mode; every picture on a board is inside it). Take a consistent copy with `pocket-whiteboard backup` (writes `DATA_DIR/backups/pocket-whiteboard-<time>.db` with `VACUUM INTO`) or Settings → Backup & data → Download database (admin). Back up that copy, not the live file. `DATA_DIR/core.token*` is the Pocket Core registration.

Restore: stop the app, replace `pocket-whiteboard.db` (remove the `-wal`/`-shm` files), start.

Every person can also export the boards they made, whole (everything on them with the pictures and who else was on them), as JSON (`/api/v1/export.json`, back in with Settings → Backup & data — twice is the same as once). Every note on your boards as a table is at `/api/v1/export.csv` (`?board=ID` for one board).
