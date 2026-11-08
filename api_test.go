package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Damba017/leitner/internal/library"
)

// doJSON sends body (a JSON string, may be empty) like the app does.
func (a *testApp) doJSON(method, path, body string, c *http.Cookie) *httptest.ResponseRecorder {
	r := a.request(method, path, nil, c)
	if body != "" {
		r.Body = httpBody(body)
		r.ContentLength = int64(len(body))
		r.Header.Set("Content-Type", "application/json")
	}
	return a.serve(r)
}

func httpBody(s string) io.ReadCloser { return io.NopCloser(strings.NewReader(s)) }

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body, err)
	}
	return v
}

func TestAPIHeadingsAndLists(t *testing.T) {
	app := newTestApp(t, config{})
	anna := app.session(app.addUser("anna", false))

	rec := app.doJSON("POST", "/api/headings", `{"name":" Spaans ","parentId":null}`, anna)
	wantStatus(t, rec, http.StatusCreated, `"name":"Spaans"`)
	es := decode[library.Heading](t, rec).ID

	rec = app.doJSON("POST", "/api/headings", fmt.Sprintf(`{"name":"Werkwoorden","parentId":%d}`, es), anna)
	wantStatus(t, rec, http.StatusCreated, fmt.Sprintf(`"parentId":%d`, es))
	verbs := decode[library.Heading](t, rec).ID

	list := fmt.Sprintf(`{"headingId":%d,"name":"Maanden","lang1":"es","lang2":"nl","direction":"both",`+
		`"strictCase":false,"strictPunct":true,"strictAccents":false}`, es)
	rec = app.doJSON("POST", "/api/lists", list, anna)
	wantStatus(t, rec, http.StatusCreated, `"strictPunct":true`)
	months := decode[library.List](t, rec).ID

	rec = app.doJSON("GET", "/api/tree", "", anna)
	wantStatus(t, rec, http.StatusOK, `"Maanden"`)
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}
	tree := decode[library.Tree](t, rec)
	if len(tree.Headings) != 2 || len(tree.Lists) != 1 {
		t.Errorf("tree = %+v", tree)
	}

	// Reorder: the list before the sub-heading.
	order := fmt.Sprintf(`{"parentId":%d,"items":[{"kind":"list","id":%d},{"kind":"heading","id":%d}]}`, es, months, verbs)
	wantStatus(t, app.doJSON("PUT", "/api/order", order, anna), http.StatusNoContent, "")
	stale := fmt.Sprintf(`{"parentId":%d,"items":[{"kind":"list","id":%d}]}`, es, months)
	wantStatus(t, app.doJSON("PUT", "/api/order", stale, anna), http.StatusConflict, "Ververs de pagina")

	// Update and move.
	wantStatus(t, app.doJSON("PUT", fmt.Sprintf("/api/headings/%d", verbs), `{"name":"Verbos","parentId":null}`, anna),
		http.StatusOK, `"parentId":null`)
	moved := strings.Replace(list, fmt.Sprintf(`"headingId":%d`, es), fmt.Sprintf(`"headingId":%d`, verbs), 1)
	wantStatus(t, app.doJSON("PUT", fmt.Sprintf("/api/lists/%d", months), moved, anna),
		http.StatusOK, fmt.Sprintf(`"headingId":%d`, verbs))

	// Errors come back as JSON with a Dutch message.
	tests := []struct {
		name, method, path, body string
		code                     int
		msg                      string
	}{
		{"empty name", "POST", "/api/headings", `{"name":"","parentId":null}`, 400, "1 tot 100 tekens"},
		{"unknown field", "POST", "/api/headings", `{"name":"X","parent":1}`, 400, "Ongeldige aanvraag"},
		{"not json", "POST", "/api/headings", `name=X`, 400, "Ongeldige aanvraag"},
		{"two objects", "POST", "/api/headings", `{"name":"X"}{"name":"Y"}`, 400, "Ongeldige aanvraag"},
		{"missing parent", "POST", "/api/headings", `{"name":"X","parentId":999}`, 400, "bestaat niet"},
		{"cycle", "PUT", fmt.Sprintf("/api/headings/%d", es), fmt.Sprintf(`{"name":"X","parentId":%d}`, es), 400, "in zichzelf"},
		{"bad lang", "POST", "/api/lists", strings.Replace(list, `"lang1":"es"`, `"lang1":"spaans!"`, 1), 400, "taalcode"},
		{"bad direction", "POST", "/api/lists", strings.Replace(list, `"both"`, `"x"`, 1), 400, "oefenrichting"},
		{"missing heading", "PUT", "/api/headings/999", `{"name":"X","parentId":null}`, 404, "Niet gevonden"},
		{"bad id", "PUT", "/api/headings/abc", `{"name":"X","parentId":null}`, 404, "Niet gevonden"},
		{"zero id", "DELETE", "/api/lists/0", "", 404, "Niet gevonden"},
	}
	for _, tt := range tests {
		rec := app.doJSON(tt.method, tt.path, tt.body, anna)
		if rec.Code != tt.code || !strings.Contains(rec.Body.String(), tt.msg) ||
			rec.Header().Get("Content-Type") != "application/json" {
			t.Errorf("%s: %d %q (%s), want %d with %q", tt.name, rec.Code, rec.Body, rec.Header().Get("Content-Type"), tt.code, tt.msg)
		}
	}

	// Delete: the list, then the heading with its contents.
	wantStatus(t, app.doJSON("DELETE", fmt.Sprintf("/api/lists/%d", months), "", anna), http.StatusNoContent, "")
	wantStatus(t, app.doJSON("DELETE", fmt.Sprintf("/api/lists/%d", months), "", anna), http.StatusNotFound, "")
	wantStatus(t, app.doJSON("DELETE", fmt.Sprintf("/api/headings/%d", es), "", anna), http.StatusNoContent, "")
	tree = decode[library.Tree](t, app.doJSON("GET", "/api/tree", "", anna))
	if len(tree.Headings) != 1 || tree.Headings[0].Name != "Verbos" || len(tree.Lists) != 0 {
		t.Errorf("tree after delete = %+v", tree)
	}
}

// TestAPIPrivate checks that one user can't see or touch another's tree.
func TestAPIPrivate(t *testing.T) {
	app := newTestApp(t, config{})
	anna := app.session(app.addUser("anna", false))
	bert := app.session(app.addUser("bert", true)) // admins don't get to see others' lists either

	h := decode[library.Heading](t, app.doJSON("POST", "/api/headings", `{"name":"Geheim","parentId":null}`, anna)).ID
	l := decode[library.List](t, app.doJSON("POST", "/api/lists",
		fmt.Sprintf(`{"headingId":%d,"name":"L","lang1":"es","lang2":"nl","direction":"1-2"}`, h), anna)).ID

	wantStatus(t, app.doJSON("GET", "/api/tree", "", bert), http.StatusOK, `{"headings":[],"lists":[]}`)
	tests := []struct {
		method, path, body string
		code               int
	}{
		{"PUT", fmt.Sprintf("/api/headings/%d", h), `{"name":"Van mij","parentId":null}`, 404},
		{"DELETE", fmt.Sprintf("/api/headings/%d", h), "", 404},
		{"PUT", fmt.Sprintf("/api/lists/%d", l), fmt.Sprintf(`{"headingId":%d,"name":"L","lang1":"es","lang2":"nl","direction":"1-2"}`, h), 404},
		{"DELETE", fmt.Sprintf("/api/lists/%d", l), "", 404},
		{"POST", "/api/headings", fmt.Sprintf(`{"name":"In","parentId":%d}`, h), 400},
		{"POST", "/api/lists", fmt.Sprintf(`{"headingId":%d,"name":"In","lang1":"es","lang2":"nl","direction":"1-2"}`, h), 400},
		{"PUT", "/api/order", fmt.Sprintf(`{"parentId":%d,"items":[{"kind":"list","id":%d}]}`, h, l), 404},
	}
	for _, tt := range tests {
		if rec := app.doJSON(tt.method, tt.path, tt.body, bert); rec.Code != tt.code {
			t.Errorf("%s %s: %d, want %d", tt.method, tt.path, rec.Code, tt.code)
		}
	}
	tree := decode[library.Tree](t, app.doJSON("GET", "/api/tree", "", anna))
	if len(tree.Headings) != 1 || tree.Headings[0].Name != "Geheim" || len(tree.Lists) != 1 {
		t.Errorf("anna's tree changed: %+v", tree)
	}
}

func TestAPINeedsSessionAndSameOrigin(t *testing.T) {
	app := newTestApp(t, config{})
	anna := app.session(app.addUser("anna", false))
	for _, route := range []string{"GET /api/tree", "POST /api/headings", "PUT /api/headings/1", "DELETE /api/headings/1",
		"POST /api/lists", "PUT /api/lists/1", "DELETE /api/lists/1", "PUT /api/order"} {
		method, path, _ := strings.Cut(route, " ")
		if rec := app.doJSON(method, path, `{}`, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without session: %d, want 401", route, rec.Code)
		}
	}

	r := app.request("POST", "/api/headings", nil, anna)
	r.Body = httpBody(`{"name":"X","parentId":null}`)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	if rec := app.serve(r); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site POST: %d, want 403", rec.Code)
	}
	r = app.request("DELETE", "/api/headings/1", nil, anna)
	r.Header.Set("Sec-Fetch-Site", "same-site") // nestje on the same host
	if rec := app.serve(r); rec.Code != http.StatusForbidden {
		t.Errorf("same-site DELETE: %d, want 403", rec.Code)
	}
}
