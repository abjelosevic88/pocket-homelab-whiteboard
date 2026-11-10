# API

JSON over HTTP at `/api/v1`. Sign in with the session cookie, a personal token (Settings → API & shortcuts) as `Authorization: Bearer pwb_…`, or, from another Pocket app, a Pocket Core link token. Errors are `{"error": "…"}`.

## Boards

| | |
|---|---|
| `GET /boards` | The boards you made or are on, with a miniature of each (`?plain=1` without). |
| `POST /boards` | `{"title", "template", "background"}` — templates: `blank retro startstop brainstorm kanban swot week mindmap journey` (`GET /templates`). |
| `GET /boards/ID` | The board, everything on it, the people on it, who is here now, the vote. |
| `PUT /boards/ID` | `{"title", "about", "background", "share_mode"}` (`share_mode` off / view / edit: the maker only). |
| `DELETE /boards/ID` | The maker only. |
| `POST /boards/ID/duplicate` | A copy for you, pictures included. |
| `PUT /boards/ID/star` | `{"starred": true}` — your own star. |
| `POST /boards/ID/share-key` | A new share link; the old one stops working. |
| `POST /boards/ID/members` | `{"email" or "user_id", "role": "editor" or "viewer"}` — someone on this box. |
| `DELETE /boards/ID/members/UID` | The maker takes someone off, or you leave. |

## What is on a board

Everything is an object `{id, kind, x, y, w, h, z, data}`; `kind` is `sticky text shape line frame pen image`. The browser makes the id, so the same change sent twice lands once.

| | |
|---|---|
| `POST /boards/ID/ops` | `{"client": "…", "ops": [{"op": "put", "obj": {…}}, {"op": "del", "id": "…"}]}` — up to 2,000 at once. |
| `POST /boards/ID/notes` | `{"notes": ["one", "two"], "color": "pink"}` — sticky notes below what is there (Shortcuts, other apps). |
| `GET /boards/ID/stream?client=…` | Server-sent events: `ops`, `cursor`, `presence`, `board`, `votes`, `deleted`. |
| `POST /boards/ID/cursor` | `{"client", "x", "y", "sel": [ids]}` — your pointer for the others. |
| `PUT /boards/ID/timer` | `{"seconds": 300}`, `{"add": 60}`, `{"seconds": 0}` stops. |
| `PUT /boards/ID/vote` | `{"action": "start", "per_person": 3, "hide": true}`, `end`, `clear`. |
| `POST /boards/ID/vote` | `{"object_id", "delta": 1 or -1}`. `GET /boards/ID/votes` for the state. |
| `POST /uploads?board=ID` | A picture (multipart `file`, optional `thumb`, `width`, `height`). |

## Elsewhere

`GET /summary`, `GET /search?q=`, `GET /day/YYYY-MM-DD` (for Pocket Day, Search and AI), `POST /boards/ID/ai/{group,summary,ideas}`, `POST /boards/ID/tasks` (`{"ids": […]}` → Pocket Tasks), `POST /boards/ID/to-notes`, `POST /boards/ID/chat`, `POST /boards/ID/files` (a PNG → Pocket Files), `GET /export.json`, `GET /export.csv`, `POST /import`.

The share link `/s/KEY` uses `/public/b/KEY` (`GET`, `/stream`, `/ops`, `/cursor`, `/vote`, `/votes`, `/uploads`) with the guest's `X-Guest-Id` and `X-Guest-Name`.
