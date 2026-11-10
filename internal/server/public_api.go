package server

import (
	"net/http"
	"time"
)

// The share link: people without an account open /s/<key> and see the
// board (and change it, when the maker chose "can edit"). Never the people
// on it or anything else on this box. Changes from one address are limited.

const guestPerMinute = 600

// guestLimit says whether this address may make another change now.
func (s *Server) guestLimit(r *http.Request) bool {
	ip := r.RemoteAddr
	s.guestMu.Lock()
	defer s.guestMu.Unlock()
	cut := time.Now().Add(-time.Minute)
	hits := s.guestHits[ip][:0]
	for _, t := range s.guestHits[ip] {
		if t.After(cut) {
			hits = append(hits, t)
		}
	}
	if len(hits) >= guestPerMinute {
		s.guestHits[ip] = hits
		return false
	}
	s.guestHits[ip] = append(hits, time.Now())
	return true
}

func (s *Server) handlePublicBoard(w http.ResponseWriter, r *http.Request) {
	a, ok := s.guestParam(w, r)
	if !ok {
		return
	}
	doc, err := s.boardDoc(r.Context(), a)
	if err != nil {
		s.fail(w, err, "board")
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) handlePublicStream(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.guestParam(w, r); ok {
		s.stream(w, r, a)
	}
}

func (s *Server) guestEditor(w http.ResponseWriter, r *http.Request) (*actor, bool) {
	a, ok := s.guestParam(w, r)
	if !ok {
		return nil, false
	}
	if !a.canEdit() {
		writeErr(w, http.StatusForbidden, "this link is for looking at the board")
		return nil, false
	}
	if !s.guestLimit(r) {
		writeErr(w, http.StatusTooManyRequests, "slow down a little — too many changes in a minute")
		return nil, false
	}
	return a, true
}

func (s *Server) handlePublicOps(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.guestEditor(w, r); ok {
		s.applyOps(w, r, a)
	}
}

func (s *Server) handlePublicCursor(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.guestParam(w, r); ok {
		s.cursor(w, r, a)
	}
}

func (s *Server) handlePublicVote(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.guestParam(w, r); ok {
		if !s.guestLimit(r) {
			writeErr(w, http.StatusTooManyRequests, "slow down a little")
			return
		}
		s.castVote(w, r, a)
	}
}

func (s *Server) handlePublicVotes(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.guestParam(w, r); ok {
		writeJSON(w, http.StatusOK, map[string]any{"votes": s.voteState(r.Context(), a)})
	}
}

func (s *Server) handlePublicUpload(w http.ResponseWriter, r *http.Request) {
	a, ok := s.guestParam(w, r)
	if !ok {
		return
	}
	up, err := s.store.GetUpload(r.Context(), idParam(r, "id"))
	if err != nil || up.BoardID != a.b.ID {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	s.serveUpload(w, r, up)
}

func (s *Server) handlePublicNewUpload(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.guestEditor(w, r); ok {
		s.saveUpload(w, r, a)
	}
}
