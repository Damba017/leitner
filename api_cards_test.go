package main

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/Damba017/leitner/internal/library"
)

// newTestList makes a heading and a list for the user and returns the list id.
func newTestList(t *testing.T, app *testApp, c *http.Cookie) int64 {
	t.Helper()
	h := decode[library.Heading](t, app.doJSON("POST", "/api/headings", `{"name":"H","parentId":null}`, c)).ID
	return decode[library.List](t, app.doJSON("POST", "/api/lists",
		fmt.Sprintf(`{"headingId":%d,"name":"Maanden / zomer","lang1":"es","lang2":"nl","direction":"1-2"}`, h), c)).ID
}

func TestAPICards(t *testing.T) {
	app := newTestApp(t, config{})
	anna := app.session(app.addUser("anna", false))
	list := newTestList(t, app, anna)
	cards := fmt.Sprintf("/api/lists/%d/cards", list)

	rec := app.doJSON("POST", cards, `{"front":" el mes ","back":"de maand","note":"","pronunciation":"el mes"}`, anna)
	wantStatus(t, rec, http.StatusCreated, `"front":"el mes"`)
	id := decode[library.Card](t, rec).ID

	wantStatus(t, app.doJSON("GET", cards, "", anna), http.StatusOK, `"pronunciation":"el mes"`)
	wantStatus(t, app.doJSON("PUT", fmt.Sprintf("/api/cards/%d", id), `{"front":"el año","back":"het jaar","note":"n","pronunciation":""}`, anna),
		http.StatusOK, `"front":"el año"`)

	tests := []struct {
		name, method, path, body string
		code                     int
		msg                      string
	}{
		{"empty back", "POST", cards, `{"front":"a","back":" "}`, 400, "verplicht"},
		{"unknown field", "POST", cards, `{"front":"a","back":"b","listId":2}`, 400, "Ongeldige aanvraag"},
		{"missing list", "POST", "/api/lists/999/cards", `{"front":"a","back":"b"}`, 404, "Niet gevonden"},
		{"missing card", "PUT", "/api/cards/999", `{"front":"a","back":"b"}`, 404, "Niet gevonden"},
		{"bad id", "DELETE", "/api/cards/abc", "", 404, "Niet gevonden"},
		{"paste no cards", "POST", fmt.Sprintf("/api/lists/%d/paste", list), `{"text":" \n","swap":false}`, 400, "geen kaarten"},
		{"paste bad line", "POST", fmt.Sprintf("/api/lists/%d/paste", list), `{"text":"a;b\nc","swap":false}`, 400, "Regel 2: een kaart heeft twee kanten"},
		{"csv wrong header", "POST", fmt.Sprintf("/api/lists/%d/import", list), "a,b\n1,2\n", 400, "Regel 1: geen geldig CSV-bestand"},
	}
	for _, tt := range tests {
		rec := app.doJSON(tt.method, tt.path, tt.body, anna)
		if rec.Code != tt.code || !strings.Contains(rec.Body.String(), tt.msg) {
			t.Errorf("%s: %d %q, want %d with %q", tt.name, rec.Code, rec.Body, tt.code, tt.msg)
		}
	}
	// A refused paste adds nothing, not even the good lines.
	if got := decode[[]library.Card](t, app.doJSON("GET", cards, "", anna)); len(got) != 1 {
		t.Errorf("cards after refused imports = %d, want 1", len(got))
	}

	wantStatus(t, app.doJSON("POST", fmt.Sprintf("/api/lists/%d/paste", list), `{"text":"hola;hallo;groet\nadios;dag","swap":true}`, anna),
		http.StatusCreated, `"added":2`)
	got := decode[[]library.Card](t, app.doJSON("GET", cards, "", anna))
	if len(got) != 3 || got[1].Front != "hallo" || got[1].Back != "hola" || got[1].Note != "groet" {
		t.Errorf("after paste = %+v", got)
	}

	wantStatus(t, app.doJSON("DELETE", fmt.Sprintf("/api/cards/%d", id), "", anna), http.StatusNoContent, "")
	wantStatus(t, app.doJSON("DELETE", fmt.Sprintf("/api/cards/%d", id), "", anna), http.StatusNotFound, "")
}

func TestAPICardImportExport(t *testing.T) {
	app := newTestApp(t, config{})
	anna := app.session(app.addUser("anna", false))
	list := newTestList(t, app, anna)

	rec := app.doJSON("POST", fmt.Sprintf("/api/lists/%d/import", list), "front,back,note\nhola,hallo,groet\n\"a, b\",c,\n", anna)
	wantStatus(t, rec, http.StatusCreated, `"added":2`)

	rec = app.doJSON("GET", fmt.Sprintf("/api/lists/%d/export.csv", list), "", anna)
	wantStatus(t, rec, http.StatusOK, "front,back,note\nhola,hallo,groet\n\"a, b\",c,\n")
	if ct := rec.Header().Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	// The slash in the list name must not end up in the file name.
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") || !strings.Contains(cd, "Maanden - zomer.csv") {
		t.Errorf("Content-Disposition = %q", cd)
	}

	rec = app.doJSON("GET", "/api/export.json", "", anna)
	wantStatus(t, rec, http.StatusOK, `"front": "hola"`)
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "leitner-") || !strings.Contains(cd, ".json") {
		t.Errorf("Content-Disposition = %q", cd)
	}
	e := decode[library.Export](t, rec)
	if len(e.Cards) != 2 || len(e.Lists) != 1 || len(e.Headings) != 1 {
		t.Errorf("export = %+v", e)
	}

	big := "front,back,note\n" + strings.Repeat("a,b,c\n", 2001)
	wantStatus(t, app.doJSON("POST", fmt.Sprintf("/api/lists/%d/import", list), big, anna), http.StatusBadRequest, "hoogstens 2000")
	huge := "front,back,note\n" + strings.Repeat("a", maxImportBytes)
	wantStatus(t, app.doJSON("POST", fmt.Sprintf("/api/lists/%d/import", list), huge, anna), http.StatusRequestEntityTooLarge, "te groot")
}

func TestAPICardsPrivate(t *testing.T) {
	app := newTestApp(t, config{})
	anna := app.session(app.addUser("anna", false))
	bert := app.session(app.addUser("bert", true))
	list := newTestList(t, app, anna)
	id := decode[library.Card](t, app.doJSON("POST", fmt.Sprintf("/api/lists/%d/cards", list), `{"front":"a","back":"b"}`, anna)).ID

	tests := []struct {
		method, path, body string
	}{
		{"GET", fmt.Sprintf("/api/lists/%d/cards", list), ""},
		{"POST", fmt.Sprintf("/api/lists/%d/cards", list), `{"front":"a","back":"b"}`},
		{"POST", fmt.Sprintf("/api/lists/%d/paste", list), `{"text":"a;b","swap":false}`},
		{"POST", fmt.Sprintf("/api/lists/%d/import", list), "front,back\na,b\n"},
		{"GET", fmt.Sprintf("/api/lists/%d/export.csv", list), ""},
		{"PUT", fmt.Sprintf("/api/cards/%d", id), `{"front":"x","back":"y"}`},
		{"DELETE", fmt.Sprintf("/api/cards/%d", id), ""},
	}
	for _, tt := range tests {
		if rec := app.doJSON(tt.method, tt.path, tt.body, bert); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: %d, want 404", tt.method, tt.path, rec.Code)
		}
	}
	rec := app.doJSON("GET", "/api/export.json", "", bert)
	wantStatus(t, rec, http.StatusOK, `"cards": []`)
	if strings.Contains(rec.Body.String(), `"front"`) {
		t.Error("bert's export contains anna's cards")
	}
	// Logged out: no access to the exports either.
	wantStatus(t, app.doJSON("GET", "/api/export.json", "", nil), http.StatusUnauthorized, "")
	if got := decode[[]library.Card](t, app.doJSON("GET", fmt.Sprintf("/api/lists/%d/cards", list), "", anna)); len(got) != 1 || got[0].Front != "a" {
		t.Errorf("anna's cards changed: %+v", got)
	}
}

func TestAPIOffline(t *testing.T) {
	app := newTestApp(t, config{})
	anna := app.session(app.addUser("anna", false))
	bert := app.session(app.addUser("bert", true))
	list := newTestList(t, app, anna)
	card := decode[library.Card](t, app.doJSON("POST", fmt.Sprintf("/api/lists/%d/cards", list), `{"front":"enero","back":"januari"}`, anna)).ID
	wantStatus(t, app.doJSON("POST", "/api/reviews", fmt.Sprintf(`{"cardId":%d,"direction":"1-2","correct":true}`, card), anna), http.StatusOK, "")

	rec := app.doJSON("GET", "/api/offline", "", anna)
	wantStatus(t, rec, http.StatusOK, "")
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}
	type snapshot struct {
		Me struct {
			Username string `json:"username"`
			IsAdmin  bool   `json:"isAdmin"`
		} `json:"me"`
		library.Export
	}
	got := decode[snapshot](t, rec)
	if got.Me.Username != "anna" || got.Me.IsAdmin {
		t.Errorf("me = %+v", got.Me)
	}
	if len(got.Headings) != 1 || len(got.Lists) != 1 || len(got.Cards) != 1 || got.Cards[0].Front != "enero" {
		t.Errorf("snapshot = %+v", got.Export)
	}
	if len(got.Schedule) != 1 || got.Schedule[0].CardID != card || got.Schedule[0].Box != 2 {
		t.Errorf("schedule = %+v", got.Schedule)
	}

	// Only the user's own data, and only with a session.
	other := decode[snapshot](t, app.doJSON("GET", "/api/offline", "", bert))
	if other.Me.Username != "bert" || !other.Me.IsAdmin || len(other.Lists)+len(other.Cards)+len(other.Schedule) != 0 {
		t.Errorf("bert's snapshot = %+v", other)
	}
	wantStatus(t, app.doJSON("GET", "/api/offline", "", nil), http.StatusUnauthorized, "")
}
