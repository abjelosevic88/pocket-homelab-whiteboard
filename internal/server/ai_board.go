package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-whiteboard/internal/ai"
)

// Pocket AI (or the person's own OpenAI-compatible key) on a board: group
// notes by theme, sum up a board, suggest more notes. It only proposes —
// the page lays out what comes back as ordinary changes, so Undo takes it
// back like anything else.

func (s *Server) aiClient(w http.ResponseWriter, r *http.Request) (*ai.Client, bool) {
	ctx := r.Context()
	st := s.settingsOf(ctx, actorID(ctx))
	if !st.AIHelp {
		writeErr(w, http.StatusBadRequest, "AI help is switched off in Settings → AI")
		return nil, false
	}
	cl, _, why := s.aiFor(ctx, actorID(ctx), st)
	if cl == nil {
		writeErr(w, http.StatusBadRequest, firstNonEmpty(why, "connect Pocket AI or an AI key in Settings → AI"))
		return nil, false
	}
	select {
	case s.aiSem <- struct{}{}:
	default:
		writeErr(w, http.StatusTooManyRequests, "the AI is busy: try again in a moment")
		return nil, false
	}
	return cl, true
}

func (s *Server) aiDone() { <-s.aiSem }

// aiNotes are the notes the AI works on: the ones picked, else everything
// written on the board.
func (s *Server) aiNotes(r *http.Request, a *actor, ids []string) []note {
	objs, _ := s.store.Objects(r.Context(), a.b.ID)
	all := notesOf(objs, nil)
	if len(ids) == 0 {
		return all
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	out := []note{}
	for _, n := range all {
		if want[n.ID] {
			out = append(out, n)
		}
	}
	return out
}

// handleAIGroup sorts notes into themes: [{title, ids}].
func (s *Server) handleAIGroup(w http.ResponseWriter, r *http.Request) {
	a, ok := s.editorParam(w, r)
	if !ok {
		return
	}
	var in struct {
		IDs    []string `json:"ids"`
		Groups int      `json:"groups"`
	}
	_ = decode(r, &in)
	notes := s.aiNotes(r, a, in.IDs)
	if len(notes) < 3 {
		writeErr(w, http.StatusBadRequest, "pick at least three notes with something written on them")
		return
	}
	if len(notes) > 300 {
		notes = notes[:300]
	}
	cl, ok := s.aiClient(w, r)
	if !ok {
		return
	}
	defer s.aiDone()
	var b strings.Builder
	for _, n := range notes {
		fmt.Fprintf(&b, "%s: %s\n", n.ID, clip(strings.ReplaceAll(n.Text, "\n", " "), 300))
	}
	want := "between 2 and 8 groups"
	if in.Groups >= 2 && in.Groups <= 12 {
		want = fmt.Sprintf("exactly %d groups", in.Groups)
	}
	sys := "You sort sticky notes from a whiteboard into themes. Reply with JSON only: {\"groups\":[{\"title\":\"short theme name, 1-4 words\",\"ids\":[\"note id\", ...]}]}. " +
		"Use " + want + ". Every note id goes in exactly one group. Theme names are plain words in the language of the notes, no numbering, no emoji."
	var out struct {
		Groups []struct {
			Title string   `json:"title"`
			IDs   []string `json:"ids"`
		} `json:"groups"`
	}
	if err := cl.JSON(r.Context(), sys, "Board: "+a.b.Title+"\n\nNotes (id: text):\n"+b.String(), &out); err != nil {
		writeErr(w, http.StatusBadGateway, "the AI: "+err.Error())
		return
	}
	known := map[string]bool{}
	for _, n := range notes {
		known[n.ID] = true
	}
	used := map[string]bool{}
	groups := []map[string]any{}
	for _, g := range out.Groups {
		ids := []string{}
		for _, id := range g.IDs {
			if known[id] && !used[id] {
				used[id] = true
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			groups = append(groups, map[string]any{"title": clip(firstNonEmpty(strings.TrimSpace(g.Title), "Group"), 60), "ids": ids})
		}
	}
	rest := []string{}
	for _, n := range notes {
		if !used[n.ID] {
			rest = append(rest, n.ID)
		}
	}
	if len(rest) > 0 {
		groups = append(groups, map[string]any{"title": "Other", "ids": rest})
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

// handleAISummary sums up a board (or the notes picked) in a few lines.
func (s *Server) handleAISummary(w http.ResponseWriter, r *http.Request) {
	a, ok := s.boardParam(w, r)
	if !ok {
		return
	}
	var in struct {
		IDs []string `json:"ids"`
	}
	_ = decode(r, &in)
	notes := s.aiNotes(r, a, in.IDs)
	if len(notes) == 0 {
		writeErr(w, http.StatusBadRequest, "there is nothing written on the board yet")
		return
	}
	cl, ok := s.aiClient(w, r)
	if !ok {
		return
	}
	defer s.aiDone()
	v := a.b.VoteOf()
	votes := map[string]int{}
	if v.Round > 0 && (v.Done || !v.Hide) {
		votes, _, _, _ = s.store.Votes(r.Context(), a.b.ID, v.Round, "")
	}
	var b strings.Builder
	cur := "\x00"
	for _, n := range notes {
		if n.Frame != cur {
			cur = n.Frame
			b.WriteString("\n[" + firstNonEmpty(n.Frame, "no frame") + "]\n")
		}
		line := "- " + clip(strings.ReplaceAll(n.Text, "\n", " "), 300)
		if votes[n.ID] > 0 {
			line += fmt.Sprintf(" (%d votes)", votes[n.ID])
		}
		b.WriteString(line + "\n")
	}
	sys := "You sum up a whiteboard for the people who worked on it. Write 3 to 6 short bullet points with the main points and decisions, then a line 'Next:' with the actions if there are any. " +
		"Plain words, the language of the notes, no heading, no preamble. Notes with votes matter more."
	out, err := cl.Complete(r.Context(), sys, clip("Board: "+a.b.Title+"\n"+b.String(), 30000), false)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "the AI: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"text": strings.TrimSpace(out)})
}

// handleAIIdeas suggests more notes on a topic (or the board's own).
func (s *Server) handleAIIdeas(w http.ResponseWriter, r *http.Request) {
	a, ok := s.editorParam(w, r)
	if !ok {
		return
	}
	var in struct {
		Topic string `json:"topic"`
		N     int    `json:"n"`
	}
	_ = decode(r, &in)
	n := clampInt(firstPos(in.N, 8), 1, 30)
	cl, ok := s.aiClient(w, r)
	if !ok {
		return
	}
	defer s.aiDone()
	have := []string{}
	for _, x := range s.aiNotes(r, a, nil) {
		if len(have) < 80 {
			have = append(have, "- "+clip(strings.ReplaceAll(x.Text, "\n", " "), 160))
		}
	}
	topic := firstNonEmpty(cleanText(in.Topic, 300), a.b.Title)
	sys := fmt.Sprintf("You add ideas to a whiteboard brainstorm. Reply with JSON only: {\"ideas\":[\"...\"]} with %d new ideas, each a short sticky note (at most 12 words), different from the notes already there, in their language.", n)
	var out struct {
		Ideas []string `json:"ideas"`
	}
	if err := cl.JSON(r.Context(), sys, "Topic: "+topic+"\n\nAlready on the board:\n"+strings.Join(have, "\n"), &out); err != nil {
		writeErr(w, http.StatusBadGateway, "the AI: "+err.Error())
		return
	}
	ideas := []string{}
	for _, t := range out.Ideas {
		if t = cleanText(t, 300); t != "" && len(ideas) < n {
			ideas = append(ideas, t)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ideas": ideas})
}
