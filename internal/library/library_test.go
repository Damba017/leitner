package library

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Damba017/leitner/internal/auth"
	"github.com/Damba017/leitner/internal/store"
)

var ctx = context.Background()

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "leitner.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func addUser(t *testing.T, db *sql.DB, name string) int64 {
	t.Helper()
	id, err := auth.CreateUser(ctx, db, auth.NewUser{Username: name, Email: name + "@example.com", Password: "geheim1"})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func ptr(id int64) *int64 { return &id }

func mustHeading(t *testing.T, db *sql.DB, user int64, name string, parent *int64) int64 {
	t.Helper()
	h, err := CreateHeading(ctx, db, user, name, parent)
	if err != nil {
		t.Fatal(err)
	}
	return h.ID
}

func fields(heading int64, name string) ListFields {
	return ListFields{HeadingID: heading, Name: name, Lang1: "es", Lang2: "nl", Direction: DirBoth}
}

func mustList(t *testing.T, db *sql.DB, user, heading int64, name string) int64 {
	t.Helper()
	l, err := CreateList(ctx, db, user, fields(heading, name))
	if err != nil {
		t.Fatal(err)
	}
	return l.ID
}

// children lists the children of parent in order, as "h:name" and "l:name".
func children(t *testing.T, db *sql.DB, user int64, parent *int64) []string {
	t.Helper()
	tree, err := GetTree(ctx, db, user)
	if err != nil {
		t.Fatal(err)
	}
	type child struct {
		pos, id int64
		label   string
	}
	var cs []child
	for _, h := range tree.Headings {
		if eqID(h.ParentID, parent) {
			cs = append(cs, child{h.Position, h.ID, "h:" + h.Name})
		}
	}
	for _, l := range tree.Lists {
		if parent != nil && l.HeadingID == *parent {
			cs = append(cs, child{l.Position, l.ID, "l:" + l.Name})
		}
	}
	slices.SortFunc(cs, func(a, b child) int {
		return cmp.Or(cmp.Compare(a.pos, b.pos), cmp.Compare(a.id, b.id))
	})
	out := []string{}
	for _, c := range cs {
		out = append(out, c.label)
	}
	return out
}

func wantChildren(t *testing.T, db *sql.DB, user int64, parent *int64, want ...string) {
	t.Helper()
	if got := children(t, db, user, parent); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("children = %v, want %v", got, want)
	}
}

func TestCleanName(t *testing.T) {
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"Spaans", "Spaans", true},
		{"  Woordjes maanden  ", "Woordjes maanden", true},
		{"", "", false},
		{"   ", "", false},
		{strings.Repeat("é", MaxNameLen), strings.Repeat("é", MaxNameLen), true},
		{strings.Repeat("é", MaxNameLen+1), "", false},
		{"twee\nregels", "", false},
		{"tab\there", "", false},
		{"\xff", "", false},
	}
	for _, tt := range tests {
		got, err := cleanName(tt.in)
		if (err == nil) != tt.ok || got != tt.want {
			t.Errorf("cleanName(%q) = %q, %v; want %q, ok=%v", tt.in, got, err, tt.want, tt.ok)
		}
	}
}

func TestCreateHeadingAndList(t *testing.T) {
	db := testDB(t)
	u := addUser(t, db, "anna")
	es := mustHeading(t, db, u, "Spaans", nil)
	mustHeading(t, db, u, "Frans", nil)
	mustList(t, db, u, es, "Seizoenen")
	mustHeading(t, db, u, "Werkwoorden", ptr(es))
	mustList(t, db, u, es, "Maanden")

	wantChildren(t, db, u, nil, "h:Spaans", "h:Frans")
	// Lists and sub-headings share one order.
	wantChildren(t, db, u, ptr(es), "l:Seizoenen", "h:Werkwoorden", "l:Maanden")

	tree, err := GetTree(ctx, db, u)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Lists) != 2 || tree.Lists[0].Lang1 != "es" || tree.Lists[0].Direction != DirBoth {
		t.Errorf("lists = %+v", tree.Lists)
	}
}

func TestCreateErrors(t *testing.T) {
	db := testDB(t)
	u := addUser(t, db, "anna")
	other := addUser(t, db, "bob")
	h := mustHeading(t, db, u, "Spaans", nil)
	bobs := mustHeading(t, db, other, "Duits", nil)

	if _, err := CreateHeading(ctx, db, u, " ", nil); !errors.Is(err, ErrInvalidName) {
		t.Errorf("empty name: %v", err)
	}
	if _, err := CreateHeading(ctx, db, u, "X", ptr(999)); !errors.Is(err, ErrParentNotFound) {
		t.Errorf("missing parent: %v", err)
	}
	if _, err := CreateHeading(ctx, db, u, "X", ptr(bobs)); !errors.Is(err, ErrParentNotFound) {
		t.Errorf("other user's parent: %v", err)
	}
	if _, err := CreateList(ctx, db, u, fields(bobs, "X")); !errors.Is(err, ErrParentNotFound) {
		t.Errorf("list in other user's heading: %v", err)
	}
	if _, err := CreateList(ctx, db, u, fields(0, "X")); !errors.Is(err, ErrParentNotFound) {
		t.Errorf("list without heading: %v", err)
	}

	tests := []struct {
		name string
		edit func(*ListFields)
		want error
	}{
		{"region tag", func(f *ListFields) { f.Lang1 = "pt-BR" }, nil},
		{"script tag", func(f *ListFields) { f.Lang2 = "zh-Hant" }, nil},
		{"spaces trimmed", func(f *ListFields) { f.Lang1 = " fr " }, nil},
		{"empty lang", func(f *ListFields) { f.Lang1 = "" }, ErrInvalidLang},
		{"one letter", func(f *ListFields) { f.Lang1 = "e" }, ErrInvalidLang},
		{"underscore", func(f *ListFields) { f.Lang2 = "pt_BR" }, ErrInvalidLang},
		{"too long", func(f *ListFields) { f.Lang2 = "en" + strings.Repeat("-abcd", 8) }, ErrInvalidLang},
		{"bad direction", func(f *ListFields) { f.Direction = "random" }, ErrInvalidDirection},
		{"empty direction", func(f *ListFields) { f.Direction = "" }, ErrInvalidDirection},
		{"1-2", func(f *ListFields) { f.Direction = Dir12 }, nil},
		{"2-1", func(f *ListFields) { f.Direction = Dir21 }, nil},
		{"bad name", func(f *ListFields) { f.Name = "" }, ErrInvalidName},
	}
	for _, tt := range tests {
		f := fields(h, "Lijst")
		tt.edit(&f)
		_, err := CreateList(ctx, db, u, f)
		if !errors.Is(err, tt.want) {
			t.Errorf("%s: err = %v, want %v", tt.name, err, tt.want)
		}
	}
}

func TestMaxDepth(t *testing.T) {
	db := testDB(t)
	u := addUser(t, db, "anna")
	var parent *int64
	for range MaxDepth {
		parent = ptr(mustHeading(t, db, u, "Kop", parent))
	}
	if _, err := CreateHeading(ctx, db, u, "Te diep", parent); !errors.Is(err, ErrTooDeep) {
		t.Errorf("err = %v, want ErrTooDeep", err)
	}
	// A list at the deepest level is fine: only headings count.
	mustList(t, db, u, *parent, "Lijst")
}

func TestUpdateHeading(t *testing.T) {
	db := testDB(t)
	u := addUser(t, db, "anna")
	other := addUser(t, db, "bob")
	es := mustHeading(t, db, u, "Spaans", nil)
	fr := mustHeading(t, db, u, "Frans", nil)
	verbs := mustHeading(t, db, u, "Werkwoorden", ptr(es))
	irr := mustHeading(t, db, u, "Onregelmatig", ptr(verbs))
	mustList(t, db, u, fr, "Kleuren")
	bobs := mustHeading(t, db, other, "Duits", nil)

	// Rename only: the position stays.
	h, err := UpdateHeading(ctx, db, u, es, "Español", nil)
	if err != nil || h.Name != "Español" {
		t.Fatalf("rename: %+v, %v", h, err)
	}
	wantChildren(t, db, u, nil, "h:Español", "h:Frans")

	// Move to another parent: at the end there.
	if _, err := UpdateHeading(ctx, db, u, verbs, "Werkwoorden", ptr(fr)); err != nil {
		t.Fatal(err)
	}
	wantChildren(t, db, u, ptr(fr), "l:Kleuren", "h:Werkwoorden")
	wantChildren(t, db, u, ptr(es))
	wantChildren(t, db, u, ptr(verbs), "h:Onregelmatig")

	// Move to the top level.
	if _, err := UpdateHeading(ctx, db, u, verbs, "Werkwoorden", nil); err != nil {
		t.Fatal(err)
	}
	wantChildren(t, db, u, nil, "h:Español", "h:Frans", "h:Werkwoorden")

	tests := []struct {
		name   string
		id     int64
		parent *int64
		want   error
	}{
		{"into itself", verbs, ptr(verbs), ErrCycle},
		{"into own child", verbs, ptr(irr), ErrCycle},
		{"missing parent", verbs, ptr(999), ErrParentNotFound},
		{"other user's parent", verbs, ptr(bobs), ErrParentNotFound},
		{"missing heading", 999, nil, ErrNotFound},
		{"other user's heading", bobs, nil, ErrNotFound},
	}
	for _, tt := range tests {
		if _, err := UpdateHeading(ctx, db, u, tt.id, "X", tt.parent); !errors.Is(err, tt.want) {
			t.Errorf("%s: err = %v, want %v", tt.name, err, tt.want)
		}
	}
	if _, err := UpdateHeading(ctx, db, u, es, "", nil); !errors.Is(err, ErrInvalidName) {
		t.Errorf("empty name: %v", err)
	}
}

func TestUpdateHeadingTooDeep(t *testing.T) {
	db := testDB(t)
	u := addUser(t, db, "anna")
	// A chain of MaxDepth-1 headings, and a separate subtree two levels high.
	var deep *int64
	for range MaxDepth - 1 {
		deep = ptr(mustHeading(t, db, u, "Kop", deep))
	}
	top := mustHeading(t, db, u, "Top", nil)
	mustHeading(t, db, u, "Kind", ptr(top))

	if _, err := UpdateHeading(ctx, db, u, top, "Top", deep); !errors.Is(err, ErrTooDeep) {
		t.Errorf("err = %v, want ErrTooDeep", err)
	}
}

func TestUpdateList(t *testing.T) {
	db := testDB(t)
	u := addUser(t, db, "anna")
	other := addUser(t, db, "bob")
	es := mustHeading(t, db, u, "Spaans", nil)
	fr := mustHeading(t, db, u, "Frans", nil)
	a := mustList(t, db, u, es, "A")
	mustList(t, db, u, es, "B")
	mustList(t, db, u, fr, "C")
	bobs := mustHeading(t, db, other, "Duits", nil)
	bobList := mustList(t, db, other, bobs, "D")

	f := ListFields{HeadingID: es, Name: "A2", Lang1: "es-ES", Lang2: "nl", Direction: Dir21,
		StrictCase: true, StrictPunct: false, StrictAccents: true}
	l, err := UpdateList(ctx, db, u, a, f)
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != "A2" || l.Position != 1 {
		t.Errorf("list = %+v", l)
	}
	tree, _ := GetTree(ctx, db, u)
	got := tree.Lists[0]
	if got.ListFields != f {
		t.Errorf("stored = %+v, want %+v", got.ListFields, f)
	}

	f.HeadingID = fr
	if _, err := UpdateList(ctx, db, u, a, f); err != nil {
		t.Fatal(err)
	}
	wantChildren(t, db, u, ptr(fr), "l:C", "l:A2")
	wantChildren(t, db, u, ptr(es), "l:B")

	if _, err := UpdateList(ctx, db, u, bobList, f); !errors.Is(err, ErrNotFound) {
		t.Errorf("other user's list: %v", err)
	}
	f.HeadingID = bobs
	if _, err := UpdateList(ctx, db, u, a, f); !errors.Is(err, ErrParentNotFound) {
		t.Errorf("into other user's heading: %v", err)
	}
	f.HeadingID, f.Direction = fr, "x"
	if _, err := UpdateList(ctx, db, u, a, f); !errors.Is(err, ErrInvalidDirection) {
		t.Errorf("bad direction: %v", err)
	}
}

func TestReorder(t *testing.T) {
	db := testDB(t)
	u := addUser(t, db, "anna")
	other := addUser(t, db, "bob")
	es := mustHeading(t, db, u, "Spaans", nil)
	fr := mustHeading(t, db, u, "Frans", nil)
	s := mustList(t, db, u, es, "Seizoenen")
	w := mustHeading(t, db, u, "Werkwoorden", ptr(es))
	m := mustList(t, db, u, es, "Maanden")
	bobs := mustHeading(t, db, other, "Duits", nil)

	// Mixed order inside a heading.
	order := []Item{{KindList, m}, {KindList, s}, {KindHeading, w}}
	if err := Reorder(ctx, db, u, ptr(es), order); err != nil {
		t.Fatal(err)
	}
	wantChildren(t, db, u, ptr(es), "l:Maanden", "l:Seizoenen", "h:Werkwoorden")

	// Top level; the other user's heading isn't part of it.
	if err := Reorder(ctx, db, u, nil, []Item{{KindHeading, fr}, {KindHeading, es}}); err != nil {
		t.Fatal(err)
	}
	wantChildren(t, db, u, nil, "h:Frans", "h:Spaans")

	stale := []struct {
		name  string
		items []Item
	}{
		{"missing item", []Item{{KindList, m}, {KindList, s}}},
		{"extra item", []Item{{KindList, m}, {KindList, s}, {KindHeading, w}, {KindHeading, fr}}},
		{"duplicate", []Item{{KindList, m}, {KindList, m}, {KindHeading, w}}},
		{"wrong kind", []Item{{KindHeading, m}, {KindList, s}, {KindHeading, w}}},
		{"unknown kind", []Item{{"card", m}, {KindList, s}, {KindHeading, w}}},
		{"other user's item", []Item{{KindList, m}, {KindList, s}, {KindHeading, bobs}}},
		{"empty", nil},
	}
	for _, tt := range stale {
		if err := Reorder(ctx, db, u, ptr(es), tt.items); !errors.Is(err, ErrStaleOrder) {
			t.Errorf("%s: err = %v, want ErrStaleOrder", tt.name, err)
		}
	}
	wantChildren(t, db, u, ptr(es), "l:Maanden", "l:Seizoenen", "h:Werkwoorden")

	if err := Reorder(ctx, db, u, ptr(bobs), nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("other user's heading: %v", err)
	}
	// An empty heading has an empty order.
	if err := Reorder(ctx, db, u, ptr(w), []Item{}); err != nil {
		t.Errorf("empty heading: %v", err)
	}
}

func count(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestDeleteCascades(t *testing.T) {
	db := testDB(t)
	u := addUser(t, db, "anna")
	admin, err := auth.CreateUser(ctx, db, auth.NewUser{Username: "admin", Email: "admin@example.com", Password: "geheim1", IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	es := mustHeading(t, db, u, "Spaans", nil)
	verbs := mustHeading(t, db, u, "Werkwoorden", ptr(es))
	irr := mustHeading(t, db, u, "Onregelmatig", ptr(verbs))
	mustList(t, db, u, es, "A")
	mustList(t, db, u, irr, "B")
	fr := mustHeading(t, db, u, "Frans", nil)
	fl := mustList(t, db, u, fr, "C")
	adminHeading := mustHeading(t, db, admin, "Admin", nil)
	mustList(t, db, admin, adminHeading, "D")

	if err := DeleteHeading(ctx, db, admin, es); !errors.Is(err, ErrNotFound) {
		t.Errorf("other user's heading: %v", err)
	}
	if err := DeleteList(ctx, db, admin, fl); !errors.Is(err, ErrNotFound) {
		t.Errorf("other user's list: %v", err)
	}

	// Deleting a heading takes its whole subtree, at every level.
	if err := DeleteHeading(ctx, db, u, es); err != nil {
		t.Fatal(err)
	}
	if h, l := count(t, db, "headings"), count(t, db, "lists"); h != 2 || l != 2 {
		t.Errorf("after heading delete: %d headings, %d lists; want 2, 2", h, l)
	}
	if err := DeleteHeading(ctx, db, u, es); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}

	if err := DeleteList(ctx, db, u, fl); err != nil {
		t.Fatal(err)
	}
	if err := DeleteList(ctx, db, u, fl); !errors.Is(err, ErrNotFound) {
		t.Errorf("second list delete: %v", err)
	}

	// Deleting a user takes everything of theirs.
	mustList(t, db, u, fr, "E")
	if err := auth.DeleteUser(ctx, db, u); err != nil {
		t.Fatal(err)
	}
	if h, l := count(t, db, "headings"), count(t, db, "lists"); h != 1 || l != 1 {
		t.Errorf("after user delete: %d headings, %d lists; want 1, 1 (the admin's)", h, l)
	}
}
