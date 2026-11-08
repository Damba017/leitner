package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/Damba017/leitner/internal/library"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("write json", "err", err)
	}
}

// writeJSONError sends {"error": msg}; the app shows msg to the user.
func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// readJSON decodes the body into v. Unknown fields and trailing data are
// refused, so a typo in the app shows up instead of being ignored. On
// failure it has answered 400 and returns false.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxFormBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil || dec.More() {
		writeJSONError(w, http.StatusBadRequest, "Ongeldige aanvraag.")
		return false
	}
	return true
}

// pathID is the {id} in the route; anything else than a positive number is
// a page that doesn't exist.
func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSONError(w, http.StatusNotFound, apiErrors[library.ErrNotFound].msg)
		return 0, false
	}
	return id, true
}

type apiErr struct {
	status int
	msg    string
}

// apiErrors are the Dutch messages for the library's errors.
var apiErrors = map[error]apiErr{
	library.ErrNotFound:         {http.StatusNotFound, "Niet gevonden. Misschien is het op een ander apparaat verwijderd; ververs de pagina."},
	library.ErrParentNotFound:   {http.StatusBadRequest, "Die kop bestaat niet (meer). Ververs de pagina."},
	library.ErrInvalidName:      {http.StatusBadRequest, fmt.Sprintf("De naam moet 1 tot %d tekens zijn, op één regel.", library.MaxNameLen)},
	library.ErrCycle:            {http.StatusBadRequest, "Een kop kan niet in zichzelf of in een eigen subkop."},
	library.ErrTooDeep:          {http.StatusBadRequest, fmt.Sprintf("Koppen kunnen hoogstens %d niveaus diep.", library.MaxDepth)},
	library.ErrStaleOrder:       {http.StatusConflict, "De inhoud is intussen veranderd. Ververs de pagina."},
	library.ErrInvalidLang:      {http.StatusBadRequest, "Ongeldige taalcode. Gebruik bijvoorbeeld es, nl of pt-BR."},
	library.ErrInvalidDirection: {http.StatusBadRequest, "Kies een oefenrichting."},
}

// apiError answers err: a known one with its message, anything else with 500.
func apiError(w http.ResponseWriter, r *http.Request, err error) {
	for target, e := range apiErrors {
		if errors.Is(err, target) {
			writeJSONError(w, e.status, e.msg)
			return
		}
	}
	slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	writeJSONError(w, http.StatusInternalServerError, "Er ging iets mis op de server.")
}

func (s *server) apiTree(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	tree, err := library.GetTree(r.Context(), s.db, me.ID)
	if err != nil {
		apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tree)
}

type headingRequest struct {
	Name     string `json:"name"`
	ParentID *int64 `json:"parentId"`
}

func (s *server) apiCreateHeading(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	var req headingRequest
	if !readJSON(w, r, &req) {
		return
	}
	h, err := library.CreateHeading(r.Context(), s.db, me.ID, req.Name, req.ParentID)
	if err != nil {
		apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, h)
}

func (s *server) apiUpdateHeading(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req headingRequest
	if !readJSON(w, r, &req) {
		return
	}
	h, err := library.UpdateHeading(r.Context(), s.db, me.ID, id, req.Name, req.ParentID)
	if err != nil {
		apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, h)
}

func (s *server) apiDeleteHeading(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := library.DeleteHeading(r.Context(), s.db, me.ID, id); err != nil {
		apiError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) apiCreateList(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	var req library.ListFields
	if !readJSON(w, r, &req) {
		return
	}
	l, err := library.CreateList(r.Context(), s.db, me.ID, req)
	if err != nil {
		apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, l)
}

func (s *server) apiUpdateList(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req library.ListFields
	if !readJSON(w, r, &req) {
		return
	}
	l, err := library.UpdateList(r.Context(), s.db, me.ID, id, req)
	if err != nil {
		apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (s *server) apiDeleteList(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := library.DeleteList(r.Context(), s.db, me.ID, id); err != nil {
		apiError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type orderRequest struct {
	ParentID *int64         `json:"parentId"`
	Items    []library.Item `json:"items"`
}

func (s *server) apiOrder(w http.ResponseWriter, r *http.Request) {
	me, _ := currentUser(r)
	var req orderRequest
	if !readJSON(w, r, &req) {
		return
	}
	if err := library.Reorder(r.Context(), s.db, me.ID, req.ParentID, req.Items); err != nil {
		apiError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
