package main

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Damba017/leitner/internal/library"
)

type practiceResponse struct {
	Items []library.PracticeItem `json:"items"`
}

type dueResponse struct {
	Lists map[string]int `json:"lists"`
}

func TestAPIPractice(t *testing.T) {
	app := newTestApp(t, config{})
	anna := app.session(app.addUser("anna", false))
	bert := app.session(app.addUser("bert", false))
	heading := decode[library.Heading](t, app.doJSON("POST", "/api/headings", `{"name":"Spaans","parentId":null}`, anna)).ID
	list := decode[library.List](t, app.doJSON("POST", "/api/lists",
		fmt.Sprintf(`{"headingId":%d,"name":"Zijn","lang1":"es","lang2":"nl","direction":"both"}`, heading), anna)).ID
	card := decode[library.Card](t, app.doJSON("POST", fmt.Sprintf("/api/lists/%d/cards", list), `{"front":"ser","back":"zijn"}`, anna)).ID

	due := func(scope string) []library.PracticeItem {
		t.Helper()
		rec := app.doJSON("GET", "/api/practice?"+scope+"&mode=due", "", anna)
		wantStatus(t, rec, http.StatusOK, `"items"`)
		return decode[practiceResponse](t, rec).Items
	}
	if items := due(fmt.Sprintf("list=%d", list)); len(items) != 2 || items[0].Front != "ser" || items[0].Box != 1 {
		t.Errorf("due items = %+v, want both directions of ser in box 1", items)
	}
	if items := due(fmt.Sprintf("heading=%d", heading)); len(items) != 2 {
		t.Errorf("due items of heading = %d, want 2", len(items))
	}
	wantStatus(t, app.doJSON("GET", "/api/due", "", anna), http.StatusOK, fmt.Sprintf(`{"lists":{"%d":2}}`, list))

	// A right answer: box 2, due on an Amsterdam midnight, no longer due now.
	rec := app.doJSON("POST", "/api/reviews", fmt.Sprintf(`{"cardId":%d,"direction":"1-2","correct":true}`, card), anna)
	wantStatus(t, rec, http.StatusOK, `"box":2`)
	r := decode[reviewResponse](t, rec)
	if d := r.DueAt.In(amsterdam); d.Hour() != 0 || d.Minute() != 0 || !d.After(time.Now().Add(24*time.Hour)) {
		t.Errorf("dueAt = %v, want midnight in two days (Amsterdam)", d)
	}
	if items := due(fmt.Sprintf("list=%d", list)); len(items) != 1 || items[0].Direction != "2-1" {
		t.Errorf("due after review = %+v, want only 2-1", items)
	}
	wantStatus(t, app.doJSON("GET", "/api/due", "", anna), http.StatusOK, fmt.Sprintf(`{"lists":{"%d":1}}`, list))

	// Free mode ignores the schedule and leaves "both" to the app.
	rec = app.doJSON("GET", fmt.Sprintf("/api/practice?list=%d&mode=free", list), "", anna)
	if items := decode[practiceResponse](t, rec).Items; len(items) != 1 || items[0].Direction != "both" {
		t.Errorf("free items = %+v, want ser once with direction both", items)
	}
	if strings.Contains(rec.Body.String(), `"box"`) {
		t.Errorf("free items show a box: %s", rec.Body)
	}

	tests := []struct {
		name, method, path, body string
		code                     int
		msg                      string
	}{
		{"no scope", "GET", "/api/practice?mode=due", "", 400, "Ongeldige aanvraag"},
		{"two scopes", "GET", fmt.Sprintf("/api/practice?list=%d&heading=%d&mode=due", list, heading), "", 400, "Ongeldige aanvraag"},
		{"bad id", "GET", "/api/practice?list=abc&mode=due", "", 400, "Ongeldige aanvraag"},
		{"zero id", "GET", "/api/practice?list=0&mode=due", "", 400, "Ongeldige aanvraag"},
		{"no mode", "GET", fmt.Sprintf("/api/practice?list=%d", list), "", 400, "Ongeldige aanvraag"},
		{"bad mode", "GET", fmt.Sprintf("/api/practice?list=%d&mode=all", list), "", 400, "Ongeldige aanvraag"},
		{"missing list", "GET", "/api/practice?list=999&mode=due", "", 404, "Niet gevonden"},
		{"missing heading", "GET", "/api/practice?heading=999&mode=free", "", 404, "Niet gevonden"},
		{"review both", "POST", "/api/reviews", fmt.Sprintf(`{"cardId":%d,"direction":"both","correct":true}`, card), 400, "oefenrichting"},
		{"review missing card", "POST", "/api/reviews", `{"cardId":999,"direction":"1-2","correct":true}`, 404, "Niet gevonden"},
		{"review unknown field", "POST", "/api/reviews", fmt.Sprintf(`{"cardId":%d,"direction":"1-2","correct":true,"box":5}`, card), 400, "Ongeldige aanvraag"},
	}
	for _, tt := range tests {
		rec := app.doJSON(tt.method, tt.path, tt.body, anna)
		if rec.Code != tt.code || !strings.Contains(rec.Body.String(), tt.msg) {
			t.Errorf("%s: %d %q, want %d with %q", tt.name, rec.Code, rec.Body, tt.code, tt.msg)
		}
	}

	// Bert sees and changes nothing of anna's.
	for _, path := range []string{
		fmt.Sprintf("/api/practice?list=%d&mode=due", list),
		fmt.Sprintf("/api/practice?heading=%d&mode=free", heading),
	} {
		wantStatus(t, app.doJSON("GET", path, "", bert), http.StatusNotFound, "")
	}
	wantStatus(t, app.doJSON("POST", "/api/reviews", fmt.Sprintf(`{"cardId":%d,"direction":"2-1","correct":true}`, card), bert),
		http.StatusNotFound, "")
	wantStatus(t, app.doJSON("GET", "/api/due", "", bert), http.StatusOK, `{"lists":{}}`)
	if items := due(fmt.Sprintf("list=%d", list)); len(items) != 1 || items[0].Direction != "2-1" {
		t.Errorf("anna's schedule changed by bert: %+v", items)
	}

	// The backup carries the schedule.
	wantStatus(t, app.doJSON("GET", "/api/export.json", "", anna), http.StatusOK, `"box": 2`)

	// Logged out: nothing.
	for _, tt := range []struct{ method, path, body string }{
		{"GET", fmt.Sprintf("/api/practice?list=%d&mode=due", list), ""},
		{"POST", "/api/reviews", fmt.Sprintf(`{"cardId":%d,"direction":"1-2","correct":true}`, card)},
		{"GET", "/api/due", ""},
	} {
		wantStatus(t, app.doJSON(tt.method, tt.path, tt.body, nil), http.StatusUnauthorized, "")
	}
}
