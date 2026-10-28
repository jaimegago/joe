package runfindings

import (
	"fmt"
	"math"
)

// The band table. The model classifies the consequence; the band is computed
// here from (type, consequence, reach, weight) through this fixed table and the
// model never picks one. Type bounds the band: an issue may reach P0; a risk is
// at most P1, because nothing is broken now; a graph update is always P3 and is
// not ranked against the other two.
//
// The cells are the build's; the bounds are not (docs/backlog/autojoe-ro.md
// § Ranking). TestBandTable_CellsStayWithinTypeBounds pins the bounds and fails
// if any cell, raised or lowered, crosses them.

// bandCell is one row of the indicative table: the base band and the band a
// finding moves to when reach or weight raises it.
type bandCell struct {
	base   Band
	raised Band
}

var bandTable = map[Type]map[Consequence]bandCell{
	TypeIssue: {
		ConsequenceUnavailable: {base: BandP1, raised: BandP0},
		ConsequenceDegraded:    {base: BandP2, raised: BandP1},
	},
	TypeRisk: {
		ConsequenceWouldBeUnavailable: {base: BandP2, raised: BandP1},
		ConsequenceWouldBeDegraded:    {base: BandP3, raised: BandP3},
	},
	TypeGraphUpdate: {
		ConsequenceNone: {base: BandP3, raised: BandP3},
	},
}

// typeBounds is the most and least severe band a type may carry. These are the
// design's, not the build's.
var typeBounds = map[Type][2]Band{
	TypeIssue:       {BandP0, BandP3},
	TypeRisk:        {BandP1, BandP3},
	TypeGraphUpdate: {BandP3, BandP3},
}

// Bounds returns the most severe and least severe band a type may carry.
func Bounds(t Type) (top, bottom Band, ok bool) {
	b, ok := typeBounds[t]
	if !ok {
		return "", "", false
	}
	return b[0], b[1], true
}

// DefaultWeight is every component's weight until the operator weight label
// exists (docs/backlog/autojoe-ro.md § Out of scope). The computation takes
// weight as an input from the start so the label slots in without a redesign.
const DefaultWeight = 1.0

// Movement values recorded on a derivation.
const (
	MovementNone    = "none"
	MovementRaised  = "raised"
	MovementLowered = "lowered"
)

// ComputeBand computes a finding's band from its type, consequence, reach and
// weight. Reach above zero or weight above DefaultWeight raises the finding to
// its cell's raised band; weight below DefaultWeight with no reach lowers it
// one band; in every case the result is clamped to the type's bounds, so reach
// and weight move a finding within its type's bounds and never past them.
//
// The same inputs yield the same band on every call: nothing here reads the
// clock, the model, or the store.
func ComputeBand(t Type, c Consequence, reach int, weight float64) (band, base Band, movement string, err error) {
	cells, ok := bandTable[t]
	if !ok {
		return "", "", "", fmt.Errorf("compute band: unknown type %q", t)
	}
	cell, ok := cells[c]
	if !ok {
		return "", "", "", fmt.Errorf("compute band: consequence %q is not admissible for type %q", c, t)
	}
	if reach < 0 {
		return "", "", "", fmt.Errorf("compute band: reach %d is negative", reach)
	}
	if math.IsNaN(weight) || math.IsInf(weight, 0) || weight <= 0 {
		return "", "", "", fmt.Errorf("compute band: weight %v is not a positive finite number", weight)
	}
	top, bottom, _ := Bounds(t)
	clamp := func(b Band) Band {
		r := b.Rank()
		if r < top.Rank() {
			r = top.Rank()
		}
		if r > bottom.Rank() {
			r = bottom.Rank()
		}
		return bandFromRank(r)
	}
	base = clamp(cell.base)
	switch {
	case reach > 0 || weight > DefaultWeight:
		band = clamp(cell.raised)
		if band == base {
			movement = MovementNone
		} else {
			movement = MovementRaised
		}
	case weight < DefaultWeight:
		band = clamp(bandFromRank(base.Rank() + 1))
		if band == base {
			movement = MovementNone
		} else {
			movement = MovementLowered
		}
	default:
		band, movement = base, MovementNone
	}
	return band, base, movement, nil
}
