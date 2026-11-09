package srs

import "time"

// leitnerDays is the interval of each box (box 1 first), in calendar days.
var leitnerDays = [...]int{1, 2, 4, 8, 16}

// Boxes is the number of Leitner boxes.
const Boxes = len(leitnerDays)

// Leitner moves a card one box up when it is right (box 5 stays at 16 days)
// and back to box 1 when it is wrong. A card is due from local midnight in
// Loc, so it comes back on a calendar day, whatever the hour of the review.
type Leitner struct {
	Loc *time.Location
}

func (Leitner) New() State {
	return State{Box: 1}
}

func (l Leitner) Review(s State, correct bool, now time.Time) State {
	box := 1
	if correct {
		box = min(max(s.Box, 1)+1, Boxes)
	}
	y, m, d := now.In(l.Loc).Date()
	// time.Date normalises the day overflow and picks the right UTC offset
	// on both sides of a DST change.
	due := time.Date(y, m, d+leitnerDays[box-1], 0, 0, 0, 0, l.Loc)
	return State{Box: box, Due: due.UTC()}
}
