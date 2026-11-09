package library

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestParsePasted(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		swap    bool
		want    []CardFields
		wantErr error
		line    int
	}{
		{"two sides", "hola;hallo", false, []CardFields{{Front: "hola", Back: "hallo"}}, nil, 0},
		{"note, spaces, CRLF, blank lines", " el mes ; de maand ; januari; februari \r\n\r\nla casa;het huis\r\n", false,
			[]CardFields{{Front: "el mes", Back: "de maand", Note: "januari; februari"}, {Front: "la casa", Back: "het huis"}}, nil, 0},
		{"swap keeps the note", "hola;hallo;groet", true, []CardFields{{Front: "hallo", Back: "hola", Note: "groet"}}, nil, 0},
		{"alternatives stay as typed", "el mes / mes;de maand", false, []CardFields{{Front: "el mes / mes", Back: "de maand"}}, nil, 0},
		{"one part", "hola;hallo\nadios", false, nil, ErrMissingSide, 2},
		{"empty side", "hola;", false, nil, ErrMissingSide, 1},
		{"empty side after swap", ";hola", true, nil, ErrMissingSide, 1},
		{"tab inside", "ho\tla;hallo", false, nil, ErrInvalidCard, 1},
		{"too long", strings.Repeat("a", MaxCardText+1) + ";b", false, nil, ErrInvalidCard, 1},
		{"empty text", "", false, nil, nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePasted(tt.text, tt.swap)
			if tt.wantErr != nil {
				var le *LineError
				if !errors.Is(err, tt.wantErr) || !errors.As(err, &le) || le.Line != tt.line {
					t.Fatalf("err = %v, want %v on line %d", err, tt.wantErr, tt.line)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("card %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParsePastedLimit(t *testing.T) {
	text := strings.Repeat("a;b\n", MaxImportCards)
	if got, err := ParsePasted(text, false); err != nil || len(got) != MaxImportCards {
		t.Fatalf("at the limit: %d cards, %v", len(got), err)
	}
	if _, err := ParsePasted(text+"a;b", false); !errors.Is(err, ErrTooManyCards) {
		t.Errorf("over the limit: %v", err)
	}
}

func TestParseCSV(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []CardFields
		wantErr error
		line    int
	}{
		{"basic", "front,back,note\nhola,hallo,groet\nadios,dag\n", []CardFields{{Front: "hola", Back: "hallo", Note: "groet"}, {Front: "adios", Back: "dag"}}, nil, 0},
		{"BOM, header case, quotes", "\ufeffFront,BACK,Note\n\"a, b\",\"say \"\"hi\"\"\",\n", []CardFields{{Front: "a, b", Back: `say "hi"`}}, nil, 0},
		{"two column header", "front,back\nhola,hallo\n", []CardFields{{Front: "hola", Back: "hallo"}}, nil, 0},
		{"CRLF", "front,back,note\r\nhola,hallo,\r\n", []CardFields{{Front: "hola", Back: "hallo"}}, nil, 0},
		{"only a header", "front,back,note\n", nil, nil, 0},
		{"wrong header", "a,b,c\nhola,hallo,\n", nil, ErrBadCSV, 1},
		{"empty file", "", nil, ErrBadCSV, 1},
		{"one column", "front,back,note\nhola\n", nil, ErrMissingSide, 2},
		{"empty back", "front,back,note\nhola,,x\n", nil, ErrMissingSide, 2},
		{"broken quote", "front,back,note\n\"hola,hallo\n", nil, ErrBadCSV, 2},
		{"newline in a field", "front,back,note\n\"ho\nla\",hallo\n", nil, ErrInvalidCard, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCSV(strings.NewReader(tt.in))
			if tt.wantErr != nil {
				var le *LineError
				if !errors.Is(err, tt.wantErr) || !errors.As(err, &le) || le.Line != tt.line {
					t.Fatalf("err = %v, want %v on line %d", err, tt.wantErr, tt.line)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("card %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestCardsCRUD(t *testing.T) {
	db := testDB(t)
	anna, bert := addUser(t, db, "anna"), addUser(t, db, "bert")
	h := mustHeading(t, db, anna, "Spaans", nil)
	list := mustList(t, db, anna, h, "Maanden")

	c, err := CreateCard(ctx, db, anna, list, CardFields{Front: " el mes / mes ", Back: "de maand", Note: "m", Pronunciation: "el mes"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Front != "el mes / mes" || c.ListID != list {
		t.Errorf("card = %+v", c)
	}
	if _, err := CreateCard(ctx, db, anna, list, CardFields{Front: "x"}); !errors.Is(err, ErrInvalidCard) {
		t.Errorf("one side: %v", err)
	}
	if _, err := CreateCard(ctx, db, anna, list, CardFields{Front: "a\nb", Back: "c"}); !errors.Is(err, ErrInvalidCard) {
		t.Errorf("newline: %v", err)
	}
	second, err := CreateCard(ctx, db, anna, list, CardFields{Front: "la casa", Back: "het huis"})
	if err != nil {
		t.Fatal(err)
	}

	got, err := ListCards(ctx, db, anna, list)
	if err != nil || len(got) != 2 || got[0].ID != c.ID || got[1].ID != second.ID {
		t.Fatalf("list = %+v, %v", got, err)
	}
	if got, err := UpdateCard(ctx, db, anna, c.ID, CardFields{Front: "el año", Back: "het jaar"}); err != nil || got.Front != "el año" || got.ListID != list {
		t.Errorf("update = %+v, %v", got, err)
	}

	// Privacy: bert can neither see nor change anna's list or cards.
	if _, err := ListCards(ctx, db, bert, list); !errors.Is(err, ErrNotFound) {
		t.Errorf("bert lists: %v", err)
	}
	if _, err := CreateCard(ctx, db, bert, list, CardFields{Front: "a", Back: "b"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("bert creates: %v", err)
	}
	if _, err := UpdateCard(ctx, db, bert, c.ID, CardFields{Front: "a", Back: "b"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("bert updates: %v", err)
	}
	if err := DeleteCard(ctx, db, bert, c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("bert deletes: %v", err)
	}
	if _, err := AddCards(ctx, db, bert, list, []CardFields{{Front: "a", Back: "b"}}); !errors.Is(err, ErrNotFound) {
		t.Errorf("bert imports: %v", err)
	}

	if err := DeleteCard(ctx, db, anna, c.ID); err != nil {
		t.Fatal(err)
	}
	if err := DeleteCard(ctx, db, anna, c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete twice: %v", err)
	}
	// Deleting the heading takes the list and its cards along.
	if err := DeleteHeading(ctx, db, anna, h); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM cards").Scan(&n); err != nil || n != 0 {
		t.Errorf("cards left = %d, %v", n, err)
	}
}

func TestAddCardsAllOrNothing(t *testing.T) {
	db := testDB(t)
	anna := addUser(t, db, "anna")
	list := mustList(t, db, anna, mustHeading(t, db, anna, "H", nil), "L")

	if _, err := AddCards(ctx, db, anna, list, []CardFields{{Front: "a", Back: "b"}, {Front: "c"}}); !errors.Is(err, ErrInvalidCard) {
		t.Fatalf("err = %v", err)
	}
	if cards, _ := ListCards(ctx, db, anna, list); len(cards) != 0 {
		t.Errorf("a bad import left %d cards", len(cards))
	}
	if n, err := AddCards(ctx, db, anna, list, []CardFields{{Front: "a", Back: "b"}, {Front: "c", Back: "d"}}); err != nil || n != 2 {
		t.Errorf("n = %d, %v", n, err)
	}
}

func TestCSVRoundTrip(t *testing.T) {
	db := testDB(t)
	anna, bert := addUser(t, db, "anna"), addUser(t, db, "bert")
	list := mustList(t, db, anna, mustHeading(t, db, anna, "H", nil), "L")
	want := []CardFields{
		{Front: "a, b", Back: `say "hi"`, Note: "x;y"},
		{Front: "été", Back: "zomer"},
	}
	if _, err := AddCards(ctx, db, anna, list, want); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := WriteCSV(ctx, db, anna, list, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), "front,back,note\n") {
		t.Errorf("csv = %q", buf.String())
	}
	got, err := ParseCSV(&buf)
	if err != nil || len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("round trip = %+v, %v", got, err)
	}
	if err := WriteCSV(ctx, db, bert, list, &buf); !errors.Is(err, ErrNotFound) {
		t.Errorf("bert exports: %v", err)
	}
}

func TestExportUser(t *testing.T) {
	db := testDB(t)
	anna, bert := addUser(t, db, "anna"), addUser(t, db, "bert")
	list := mustList(t, db, anna, mustHeading(t, db, anna, "H", nil), "L")
	if _, err := CreateCard(ctx, db, anna, list, CardFields{Front: "a", Back: "b", Pronunciation: "ey"}); err != nil {
		t.Fatal(err)
	}
	e, err := ExportUser(ctx, db, anna)
	if err != nil || len(e.Headings) != 1 || len(e.Lists) != 1 || len(e.Cards) != 1 || e.Cards[0].Pronunciation != "ey" || e.Version != 1 {
		t.Errorf("anna = %+v, %v", e, err)
	}
	e, err = ExportUser(ctx, db, bert)
	if err != nil || len(e.Headings) != 0 || len(e.Lists) != 0 || e.Cards == nil || len(e.Cards) != 0 {
		t.Errorf("bert = %+v, %v", e, err)
	}
}
