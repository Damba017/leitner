package main

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Damba017/leitner/internal/library"
)

// practiceScope reads ?list={id} or ?heading={id} (exactly one) and
// ?mode=due|free. On failure it has answered 400 and returns false.
func practiceScope(w http.ResponseWriter, r *http.Request) (library.Scope, bool, bool) {
	q := r.URL.Query()
	parse := func(name string) int64 {
		if !q.Has(name) {
			return 0
		}
		id, err := strconv.ParseInt(q.Get(name), 10, 64)
		if err != nil || id <= 0 {
			return -1
		}
		return id
	}
	s := library.Scope{ListID: parse("list"), HeadingID: parse("heading")}
	mode := q.Get("mode")
	if s.ListID < 0 || s.HeadingID < 0 || (s.ListID == 0) == (s.HeadingID == 0) || (mode != "due" && mode != "free") {
		writeJSONError(w, http.StatusBadRequest, "Ongeldige aanvraag.")
		return library.Scope{}, false, false
	}
	return s, mode == "due", true
}

// apiPractice answers the questions of a practice session, unshuffled.
func (s *server) apiPractice(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	scope, due, ok := practiceScope(w, r)
	if !ok {
		return
	}
	items, err := library.PracticeItems(r.Context(), s.db, me.ID, scope, time.Now(), due)
	if err != nil {
		apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type reviewRequest struct {
	CardID    int64  `json:"cardId"`
	Direction string `json:"direction"`
	Correct   bool   `json:"correct"`
}

type reviewResponse struct {
	Box   int       `json:"box"`
	DueAt time.Time `json:"dueAt"`
}

// apiReview stores one answer of a due session.
func (s *server) apiReview(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	var req reviewRequest
	if !readJSON(w, r, &req) {
		return
	}
	st, err := library.RecordReview(r.Context(), s.db, s.sched, me.ID, req.CardID, req.Direction, req.Correct, time.Now())
	if err != nil {
		apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, reviewResponse{Box: st.Box, DueAt: st.Due})
}

// apiDue answers {"lists": {"<id>": n}}: due questions per list.
func (s *server) apiDue(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	counts, err := library.DueCounts(r.Context(), s.db, me.ID, time.Now())
	if err != nil {
		apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lists": counts})
}
