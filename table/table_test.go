package table_test

import (
	"math/rand"
	"testing"

	"github.com/notnil/joker/hand"
	"github.com/notnil/joker/table"
	"github.com/stretchr/testify/require"
)

type testCase struct {
	start       *table.Table
	actions     []table.Action
	condition   func(*testing.T, table.State)
	description string
}

var (
	testCases = []testCase{
		{
			start:   threePerson100Buyin(),
			actions: nil,
			condition: func(t *testing.T, s table.State) {
				require.Equal(t, 98, s.Seats[0].Chips)
				require.Equal(t, 100, s.Seats[1].Chips)
				require.Equal(t, 99, s.Seats[2].Chips)
				require.Equal(t, 1, s.Active.Seat)
			},
			description: "initial blinds",
		},
		{
			start: threePerson100Buyin(),
			actions: []table.Action{
				{table.Raise, 5},
			},
			condition: func(t *testing.T, s table.State) {
				require.Equal(t, 98, s.Seats[0].Chips)
				require.Equal(t, 93, s.Seats[1].Chips)
				require.Equal(t, 99, s.Seats[2].Chips)
				require.Equal(t, 2, s.Active.Seat)
				require.Equal(t, 7, s.Cost)
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
			condition: func(t *testing.T, s table.State) {
				require.Equal(t, 97, s.Seats[0].Chips)
				require.Equal(t, 107, s.Seats[1].Chips)
				require.Equal(t, 93, s.Seats[2].Chips)
				require.Equal(t, 2, s.Active.Seat)
				require.Equal(t, 2, s.Button)
			},
			description: "full hand 1",
		},
		{
			start: threePerson100Buyin(),
			actions: []table.Action{
				{table.Raise, 5},
				{Type: table.Fold},
				{Type: table.Call},
			},
			condition: func(t *testing.T, s table.State) {
				require.Equal(t, table.Flop, s.Round)
				require.Equal(t, 0, s.Active.Seat) // action is on 0 as 2 folded
			},
			description: "sb fold",
		},
		{
			start: threePerson100Buyin(),
			actions: []table.Action{
				{table.Raise, 5},
				{Type: table.Call},
				{Type: table.Call},
				{table.Bet, 5},
				{Type: table.Fold},
				{Type: table.Fold},
			},
			condition: func(t *testing.T, s table.State) {
				require.Equal(t, table.PreFlop, s.Round)
				require.Equal(t, 92, s.Seats[0].Chips)  // -7 last round, -1 small blind
				require.Equal(t, 91, s.Seats[1].Chips)  // -7 last round, -2 big blind
				require.Equal(t, 114, s.Seats[2].Chips) // +14 last round
			},
			description: "post-flop folds",
		},
	}
)

func TestTable(t *testing.T) {
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			tbl := tc.start
			for _, a := range tc.actions {
				if err := tbl.Act(a); err != nil {
					t.Fatal(err)
				}
			}
			tc.condition(t, tbl.State())
		})
	}
}

func threePerson100Buyin() *table.Table {
	src := rand.NewSource(42)
	r := rand.New(src)
	dealer := hand.NewDealer(r)
	opts := table.Options{
		Variant: table.TexasHoldem,
		Limit:   table.NoLimit,
		Stakes:  table.Stakes{SmallBlind: 1, BigBlind: 2},
		Buyin:   100,
	}
	ids := []string{"a", "b", "c"}
	return table.New(dealer, opts, ids)
}
