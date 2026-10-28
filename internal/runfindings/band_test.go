package runfindings

import "testing"

// TestBandTable_CellsStayWithinTypeBounds pins the design's bounds — an issue
// may reach P0, a risk is at most P1, a graph update is always P3 — and fails
// if any cell of the table, raised by reach or weight or lowered by weight,
// crosses them. The cells may change; this test does not care which band a
// cell lands on, only that it stays inside its type's bounds.
func TestBandTable_CellsStayWithinTypeBounds(t *testing.T) {
	reaches := []int{0, 1, 5, 500}
	weights := []float64{0.5, 1, 2, 10}
	for typ, cells := range bandTable {
		top, bottom, ok := Bounds(typ)
		if !ok {
			t.Fatalf("type %s has cells but no bounds", typ)
		}
		for cons := range cells {
			for _, reach := range reaches {
				for _, weight := range weights {
					band, base, _, err := ComputeBand(typ, cons, reach, weight)
					if err != nil {
						t.Fatalf("ComputeBand(%s,%s,%d,%g): %v", typ, cons, reach, weight, err)
					}
					for _, b := range []Band{band, base} {
						if b.Rank() < top.Rank() || b.Rank() > bottom.Rank() {
							t.Errorf("ComputeBand(%s,%s,%d,%g) = %s crosses bounds [%s..%s]", typ, cons, reach, weight, b, top, bottom)
						}
					}
				}
			}
		}
	}
	// Every type with bounds has cells, so no type can be ranked without a table row.
	for typ := range typeBounds {
		if _, ok := bandTable[typ]; !ok {
			t.Errorf("type %s has bounds but no cells", typ)
		}
	}
}

// TestComputeBand_RiskNeverReachesP0 is the one bound worth naming: nothing is
// broken now, so a risk cannot outrank an unavailable issue however wide its
// reach or heavy its weight.
func TestComputeBand_RiskNeverReachesP0(t *testing.T) {
	for _, cons := range []Consequence{ConsequenceWouldBeUnavailable, ConsequenceWouldBeDegraded} {
		band, _, _, err := ComputeBand(TypeRisk, cons, 1000, 100)
		if err != nil {
			t.Fatal(err)
		}
		if band == BandP0 {
			t.Errorf("risk/%s reached P0", cons)
		}
	}
	band, _, _, err := ComputeBand(TypeGraphUpdate, ConsequenceNone, 1000, 100)
	if err != nil {
		t.Fatal(err)
	}
	if band != BandP3 {
		t.Errorf("graph update ranked %s, want P3", band)
	}
}

func TestComputeBand_Table(t *testing.T) {
	tests := []struct {
		name     string
		typ      Type
		cons     Consequence
		reach    int
		weight   float64
		want     Band
		wantBase Band
		wantMove string
	}{
		{"issue unavailable, no reach", TypeIssue, ConsequenceUnavailable, 0, 1, BandP1, BandP1, MovementNone},
		{"issue unavailable, reach raises to P0", TypeIssue, ConsequenceUnavailable, 2, 1, BandP0, BandP1, MovementRaised},
		{"issue unavailable, weight raises to P0", TypeIssue, ConsequenceUnavailable, 0, 2, BandP0, BandP1, MovementRaised},
		{"issue degraded, no reach", TypeIssue, ConsequenceDegraded, 0, 1, BandP2, BandP2, MovementNone},
		{"issue degraded, reach raises", TypeIssue, ConsequenceDegraded, 1, 1, BandP1, BandP2, MovementRaised},
		{"issue degraded, light weight lowers", TypeIssue, ConsequenceDegraded, 0, 0.5, BandP3, BandP2, MovementLowered},
		{"risk would be unavailable", TypeRisk, ConsequenceWouldBeUnavailable, 0, 1, BandP2, BandP2, MovementNone},
		{"risk would be unavailable, reach raises to P1", TypeRisk, ConsequenceWouldBeUnavailable, 3, 1, BandP1, BandP2, MovementRaised},
		{"risk would be degraded stays P3 under reach", TypeRisk, ConsequenceWouldBeDegraded, 9, 1, BandP3, BandP3, MovementNone},
		{"graph update is P3", TypeGraphUpdate, ConsequenceNone, 0, 1, BandP3, BandP3, MovementNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			band, base, move, err := ComputeBand(tt.typ, tt.cons, tt.reach, tt.weight)
			if err != nil {
				t.Fatal(err)
			}
			if band != tt.want || base != tt.wantBase || move != tt.wantMove {
				t.Errorf("got (%s, base %s, %s), want (%s, base %s, %s)", band, base, move, tt.want, tt.wantBase, tt.wantMove)
			}
		})
	}
}

func TestComputeBand_RejectsInadmissibleInputs(t *testing.T) {
	cases := []struct {
		typ    Type
		cons   Consequence
		reach  int
		weight float64
	}{
		{TypeIssue, ConsequenceWouldBeDegraded, 0, 1},
		{TypeRisk, ConsequenceUnavailable, 0, 1},
		{TypeGraphUpdate, ConsequenceDegraded, 0, 1},
		{Type("severity"), ConsequenceDegraded, 0, 1},
		{TypeIssue, ConsequenceDegraded, -1, 1},
		{TypeIssue, ConsequenceDegraded, 0, 0},
		{TypeIssue, ConsequenceDegraded, 0, -3},
	}
	for _, c := range cases {
		if _, _, _, err := ComputeBand(c.typ, c.cons, c.reach, c.weight); err == nil {
			t.Errorf("ComputeBand(%s,%q,%d,%g) accepted, want error", c.typ, c.cons, c.reach, c.weight)
		}
	}
}

// TestComputeBand_Deterministic: the same evidence yields the same band on
// every run.
func TestComputeBand_Deterministic(t *testing.T) {
	first, _, _, _ := ComputeBand(TypeIssue, ConsequenceDegraded, 4, 1)
	for i := 0; i < 50; i++ {
		b, _, _, _ := ComputeBand(TypeIssue, ConsequenceDegraded, 4, 1)
		if b != first {
			t.Fatalf("call %d yielded %s, first yielded %s", i, b, first)
		}
	}
}
