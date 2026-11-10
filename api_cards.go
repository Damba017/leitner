package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/Damba017/leitner/internal/library"
)

// maxImportBytes bounds a pasted list or CSV file: 2000 cards of 500
// characters, three fields each, would be more than this.
const maxImportBytes = 1 << 20

func (s *server) apiListCards(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	cards, err := library.ListCards(r.Context(), s.db, me.ID, id)
	if err != nil {
		apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, cards)
}

func (s *server) apiCreateCard(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req library.CardFields
	if !readJSON(w, r, &req) {
		return
	}
	c, err := library.CreateCard(r.Context(), s.db, me.ID, id, req)
	if err != nil {
		apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *server) apiUpdateCard(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req library.CardFields
	if !readJSON(w, r, &req) {
		return
	}
	c, err := library.UpdateCard(r.Context(), s.db, me.ID, id, req)
	if err != nil {
		apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *server) apiDeleteCard(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := library.DeleteCard(r.Context(), s.db, me.ID, id); err != nil {
		apiError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type pasteRequest struct {
	Text string `json:"text"`
	Swap bool   `json:"swap"`
}

// apiPasteCards adds one card per line of `side1;side2;note`.
func (s *server) apiPasteCards(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req pasteRequest
	if !readJSONMax(w, r, &req, maxImportBytes) {
		return
	}
	cards, err := library.ParsePasted(req.Text, req.Swap)
	s.addImported(w, r, me.ID, id, cards, err)
}

// apiImportCSV adds the cards of a CSV file sent as the raw request body.
func (s *server) apiImportCSV(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxImportBytes))
	if err != nil {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "Het bestand is te groot (hoogstens 1 MB).")
		return
	}
	cards, err := library.ParseCSV(bytes.NewReader(data))
	s.addImported(w, r, me.ID, id, cards, err)
}

// addImported stores parsed cards (or answers the parse error) as {"added": n}.
func (s *server) addImported(w http.ResponseWriter, r *http.Request, userID, listID int64, cards []library.CardFields, parseErr error) {
	if parseErr != nil {
		apiError(w, r, parseErr)
		return
	}
	if len(cards) == 0 {
		writeJSONError(w, http.StatusBadRequest, "Er staan geen kaarten in.")
		return
	}
	n, err := library.AddCards(r.Context(), s.db, userID, listID, cards)
	if err != nil {
		apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int{"added": n})
}

// download marks the answer as a file to save, with a name from the user's
// own text (so it is encoded safely by mime.FormatMediaType).
func download(w http.ResponseWriter, contentType, filename string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
}

func (s *server) apiExportCSV(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	list, err := library.GetList(r.Context(), s.db, me.ID, id)
	if err != nil {
		apiError(w, r, err)
		return
	}
	var buf bytes.Buffer
	if err := library.WriteCSV(r.Context(), s.db, me.ID, id, &buf); err != nil {
		apiError(w, r, err)
		return
	}
	download(w, "text/csv; charset=utf-8", strings.ReplaceAll(list.Name, "/", "-")+".csv")
	w.Write(buf.Bytes())
}

func (s *server) apiExportJSON(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	export, err := library.ExportUser(r.Context(), s.db, me.ID)
	if err != nil {
		apiError(w, r, err)
		return
	}
	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		apiError(w, r, errors.New("export: "+err.Error()))
		return
	}
	download(w, "application/json", "leitner-"+time.Now().Format("20060102")+".json")
	w.Write(data)
}

// offlineSnapshot is everything the app needs to open without the server:
// who is logged in, the tree, every card and the schedule.
type offlineSnapshot struct {
	Me struct {
		Username string `json:"username"`
		IsAdmin  bool   `json:"isAdmin"`
	} `json:"me"`
	library.Export
}

// apiOffline answers the snapshot the app keeps on the device for offline use.
func (s *server) apiOffline(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	export, err := library.ExportUser(r.Context(), s.db, me.ID)
	if err != nil {
		apiError(w, r, err)
		return
	}
	snap := offlineSnapshot{Export: export}
	snap.Me.Username = me.Username
	snap.Me.IsAdmin = me.IsAdmin
	writeJSON(w, http.StatusOK, snap)
}
