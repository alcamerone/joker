package table_test

import (
	"math/rand/v2"
	"testing"

	"github.com/alcamerone/joker/hand"
	"github.com/alcamerone/joker/table"
)

type testCase struct {
	start       *table.Table
	actions     []table.Action
	condition   func(table.State) bool
	description string
}

var (
	testCases = []testCase{
		{
			start:   threePerson100Buyin(),
			actions: nil,
			condition: func(s table.State) bool {
				return s.Seats[0].Chips == 98 && s.Seats[1].Chips == 100 && s.Seats[2].Chips == 99 && s.ActiveIdx == 1
			},
			description: "initial blinds",
		},
		{
			start: threePerson100Buyin(),
			actions: []table.Action{
				{table.Raise, 5},
			},
			condition: func(s table.State) bool {
				return s.Seats[0].Chips == 98 && s.Seats[1].Chips == 93 && s.Seats[2].Chips == 99 && s.ActiveIdx == 2 && s.Cost == 7
			},
			description: "preflop raise",
		},
		{
			start: threePerson100Buyin(),
			actions: []table.Action{
				{table.Raise, 5},
				{table.Call, 0},
				{table.Fold, 0},
				{table.Check, 0},
				{table.Bet, 5},
				{table.Fold, 0},
			},
			condition: func(s table.State) bool {
				return s.Seats[0].Chips == 97 && s.Seats[1].Chips == 107 && s.Seats[2].Chips == 93 && s.ActiveIdx == 2 && s.DealerIdx == 2
			},
			description: "full hand 1",
		},
	}
)

func TestTable(t *testing.T) {
	for _, tc := range testCases {
		tbl := tc.start
		for _, a := range tc.actions {
			if _, err := tbl.Act(a); err != nil {
				t.Fatal(err)
			}
		}
		if tc.condition(tbl.State()) == false {
			t.Fatal(tc.description)
		}
	}
}

func threePerson100Buyin() *table.Table {
	dealer := hand.NewDealer(rand.New(rand.NewPCG(0, 42)))
	opts := table.Options{
		Variant: table.TexasHoldem,
		Limit:   table.NoLimit,
		Stakes:  table.Stakes{SmallBlind: 1, BigBlind: 2},
		Buyin:   100,
	}
	ids := []string{"a", "b", "c"}
	return table.New(dealer, opts, ids, nil)
}

// A hand that ends before the flop has no community cards, so contestants hold
// only two cards and cannot be compared. These must be paid out without a showdown.
func TestPreflopFoldOutPaysSoleContestant(t *testing.T) {
	headsUp := func() *table.Table {
		dealer := hand.NewDealer(rand.New(rand.NewPCG(0, 42)))
		opts := table.Options{
			Variant: table.TexasHoldem,
			Limit:   table.NoLimit,
			Stakes:  table.Stakes{SmallBlind: 1, BigBlind: 2},
			Buyin:   100,
			OneShot: true,
		}
		return table.New(dealer, opts, []string{"a", "b"}, nil)
	}
	tests := []struct {
		name    string
		actions []table.ActionType
		winner  string
		chips   map[string]int
	}{
		// Heads-up, "b" is the small blind and acts first; "a" is the big blind.
		{"small blind calls, big blind folds", []table.ActionType{table.Call, table.Fold}, "b", map[string]int{"a": 98, "b": 102}},
		{"small blind folds, big blind checks", []table.ActionType{table.Fold, table.Check}, "a", map[string]int{"a": 101, "b": 99}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tbl := headsUp()
			var s table.State
			for _, a := range tt.actions {
				var err error
				if s, err = tbl.Act(table.Action{Type: a}); err != nil {
					t.Fatal(err)
				}
			}
			if s.Status != table.Done {
				t.Fatalf("status = %v, want Done", s.Status)
			}
			if len(s.Result.Winners) != 1 || s.Result.Winners[0].ID != tt.winner {
				t.Fatalf("winners = %+v, want only %s", s.Result.Winners, tt.winner)
			}
			for _, seat := range s.Seats {
				if seat.Chips != tt.chips[seat.ID] {
					t.Fatalf("%s has %d chips, want %d", seat.ID, seat.Chips, tt.chips[seat.ID])
				}
			}
		})
	}
}
