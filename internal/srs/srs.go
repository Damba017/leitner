// Package srs holds the spaced-repetition schedulers. They are pure: the
// caller loads and stores the State. Leitner is the only one for now; SM-2 or
// FSRS can be added later behind the same interface.
package srs

import "time"

// State is where one direction of a card stands. A zero Due means due now.
type State struct {
	Box int
	Due time.Time
}

// Scheduler decides when a card comes back.
type Scheduler interface {
	// New is the state of a card that was never reviewed.
	New() State
	// Review is the state after answering at now.
	Review(s State, correct bool, now time.Time) State
}
