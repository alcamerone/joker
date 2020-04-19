package table

import (
	"errors"
	"fmt"
	"log"
	"sort"

	"github.com/alcamerone/joker/hand"
)

type Status int

const (
	Broken Status = iota
	Dealing
	Done
)

type Round int

const (
	PreFlop Round = iota
	Flop
	Turn
	River
)

type Variant int

const (
	TexasHoldem Variant = iota
	OmahaHi
)

type Limit int

const (
	NoLimit Limit = iota
	PotLimit
)

type Options struct {
	Buyin   int
	Variant Variant
	Stakes  Stakes
	Limit   Limit
	OneShot bool
}

type Stakes struct {
	BigBlind   int
	SmallBlind int
	Ante       int
}

type Table struct {
	options         Options
	seats           []*Player
	dealer          hand.Dealer
	deck            *hand.Deck
	cards           []hand.Card
	active          *Player
	status          Status
	round           Round
	button          int
	cost            int
	lastWinners     []Player
	lastContestants []Player
	lastCards       []hand.Card
}

func New(dealer hand.Dealer, opts Options, playerIDs []string, sittingOut []string) *Table {
	status := Dealing
	if len(playerIDs) < 2 {
		status = Broken
	}
	seats := []*Player{}
	for i, id := range playerIDs {
		p := &Player{
			ID:    id,
			Chips: opts.Buyin,
			Seat:  i,
		}
		for _, s := range sittingOut {
			if s == id {
				p.Defaulting = true
			}
		}
		seats = append(seats, p)
	}
	t := &Table{
		options: opts,
		seats:   seats,
		round:   PreFlop,
		status:  status,
		dealer:  dealer,
	}
	t.setupRound()
	return t
}

func (t *Table) AddPlayer(id string, defaulting bool) {
	p := &Player{
		ID:         id,
		Chips:      t.options.Buyin,
		Seat:       len(t.seats),
		Defaulting: defaulting,
		SittingOut: true,
	}
	t.seats = append(t.seats, p)
}

func (t *Table) BuyPlayerIn(id string) error {
	for _, s := range t.seats {
		if s.ID == id {
			s.Chips = t.options.Buyin
			return nil
		}
	}
	return fmt.Errorf("table: player %s not found", id)
}

func (t *Table) NewRound() State {
	t.status = Dealing
	t.round = PreFlop
	t.lastWinners = nil
	t.lastContestants = nil
	t.lastCards = nil
	return t.setupRound()
}

type Result struct {
	Winners     []Player
	Contestants []Player
	TableCards  []hand.Card
}

type State struct {
	Options    Options
	Seats      []Player `json:",omitempty"`
	Cards      []hand.Card
	Active     Player
	Dealer     *Player
	BigBlind   *Player
	SmallBlind *Player
	Status     Status
	Round      Round
	Button     int
	Cost       int
	Pot        int
	Owed       int
	Result     Result `json:",omitempty"`
}

func (t *Table) State() State {
	seats := []Player{}
	pot := 0
	for _, seat := range t.seats {
		seats = append(seats, *seat)
		pot += seat.ChipsInPot
	}
	s := State{
		Options:    t.options,
		Seats:      seats,
		Cards:      append([]hand.Card(nil), t.cards...),
		Active:     *t.active,
		Dealer:     t.Dealer(),
		BigBlind:   t.BigBlind(),
		SmallBlind: t.SmallBlind(),
		Button:     t.button,
		Cost:       t.cost,
		Round:      t.round,
		Status:     t.status,
		Pot:        pot,
		Owed:       t.owed(),
	}
	if t.lastWinners != nil {
		s.Result = Result{
			Winners:     t.lastWinners,
			Contestants: t.lastContestants,
			TableCards:  t.lastCards,
		}
	}
	return s
}

type Action struct {
	Type  ActionType
	Chips int
}

type ActionType int

const (
	Fold ActionType = iota
	Check
	Call
	Bet
	Raise
	AllIn
)

func (t *Table) Fold() (State, error) {
	return t.Act(Action{Type: Fold})
}

func (t *Table) Check() (State, error) {
	return t.Act(Action{Type: Check})
}

func (t *Table) Call() (State, error) {
	return t.Act(Action{Type: Call})
}

func (t *Table) Bet(chips int) (State, error) {
	return t.Act(Action{Type: Bet, Chips: chips})
}

func (t *Table) Raise(chips int) (State, error) {
	return t.Act(Action{Type: Raise, Chips: chips})
}

func (t *Table) AllIn() (State, error) {
	return t.Act(Action{Type: AllIn})
}

func (t *Table) Act(a Action) (State, error) {
	if includes(t.LegalActions(), a.Type) == false {
		return State{}, fmt.Errorf("table: illegal action %s attempted", a.Type.String())
	}
	log.Printf("%s %ss", t.active.ID, a.Type.String())
	// TODO enforce limits, min bets
	switch a.Type {
	case Fold:
		t.active.Folded = true
	case Check:
	case Call:
		t.active.contribute(t.owed())
	case Bet, Raise:
		if a.Chips < t.options.Stakes.BigBlind {
			return State{},
				errors.New("table: bet or raise must be a minimum of the big blind")
		}
		t.active.contribute(t.owed())
		t.active.contribute(a.Chips)
		t.resetAction()
	case AllIn:
		t.active.contribute(t.owed())
		t.active.contribute(t.active.Chips)
		t.resetAction()
	}
	t.active.Acted = true
	if t.active.ChipsInPot > t.cost {
		t.cost = t.active.ChipsInPot
	}
	return t.update(), nil
}

func (t *Table) Seats() []Player {
	seats := make([]Player, len(t.seats))
	for i, seat := range t.seats {
		seats[i] = *seat
	}
	return seats
}

func (t *Table) SetPlayerDefaulting(playerId string, defaulting bool) error {
	for _, s := range t.seats {
		if s.ID == playerId {
			s.Defaulting = defaulting
			return nil
		}
	}
	return fmt.Errorf("table: %s not found at table", playerId)
}

func (t *Table) LegalActions() []ActionType {
	if t.owed() == 0 {
		return []ActionType{Fold, Check, Bet, AllIn}
	}
	if t.owed() > t.active.Chips {
		return []ActionType{Fold, Call}
	}
	return []ActionType{Fold, Call, Raise, AllIn}
}

func (t *Table) update() State {
	seat := t.nextToAct()
	if seat != -1 {
		t.active = t.seats[seat]
		if t.active.Defaulting && !t.active.SittingOut {
			t.active.SittingOut = true
			state, _ := t.Act(Action{Type: Fold})
			return state
		}
		return t.State()
	}
	if len(t.contesting()) == 1 || t.round == River {
		t.payout()
		if t.options.OneShot {
			t.status = Done
			return t.State()
		}
		t.round = PreFlop
	} else {
		t.round = (t.round + 1) % (River + 1)
	}
	return t.setupRound()
}

func (t *Table) Active() *Player {
	return t.active
}

func (t *Table) Dealer() *Player {
	return t.seats[t.button]
}

func (t *Table) SmallBlind() *Player {
	if t.occupiedSeats()-t.seatsSittingOut() == 2 {
		return t.seats[t.button]
	}
	return t.seats[t.nextSeat(t.button)]
}

func (t *Table) BigBlind() *Player {
	if t.occupiedSeats()-t.seatsSittingOut() == 2 {
		return t.seats[t.nextSeat(t.button)]
	}
	return t.seats[t.nextSeat(t.nextSeat(t.button))]
}

func (t *Table) setupRound() State {
	for _, seat := range t.seats {
		if seat != nil {
			seat.Acted = false
		}
	}
	switch t.round {
	case PreFlop:
		for _, seat := range t.seats {
			if seat != nil {
				seat.SittingOut = seat.Defaulting || seat.Chips == 0 // IF YOU AIN'T GOT NO MONEY TAKE YO BROKE ASS HOME
			}
		}
		t.status = Dealing
		t.cards = nil
		t.button = t.nextSeat(t.button)
		sb := t.nextSeat(t.button)
		bb := t.nextSeat(sb)
		if t.occupiedSeats()-t.seatsSittingOut() == 2 {
			sb = t.button
			bb = t.nextSeat(t.button)
		}
		t.deck = t.dealer.Deck()
		for _, seat := range t.seats {
			if seat != nil {
				seat.Cards = nil
				seat.ChipsInPot = 0
				seat.Acted = false
				seat.Folded = false
				seat.AllIn = false
				if !seat.SittingOut {
					seat.Cards = t.deck.PopMulti(2)
					seat.contribute(t.options.Stakes.Ante)
				}
			}
		}
		t.seats[sb].contribute(t.options.Stakes.SmallBlind)
		t.seats[bb].contribute(t.options.Stakes.BigBlind)
		t.active = t.seats[bb]
		action := t.nextToAct()
		if action == -1 {
			// All players already folded or all in
			return t.update()
		}
		t.active = t.seats[action]
		t.cost = t.options.Stakes.BigBlind
	case Flop:
		t.cards = t.deck.PopMulti(3)
		t.active = t.seats[t.button]
		action := t.nextToAct()
		if action == -1 {
			// All players already folded or all in
			return t.update()
		}
		t.active = t.seats[action]
	case Turn, River:
		t.cards = append(t.cards, t.deck.Pop())
		t.active = t.seats[t.button]
		action := t.nextToAct()
		if action == -1 {
			// All players already folded or all in
			return t.update()
		}
		t.active = t.seats[action]
	}
	if t.active.Defaulting && !t.active.SittingOut {
		t.active.SittingOut = true
		state, _ := t.Act(Action{Type: Fold})
		return state
	}
	return t.State()
}

func (t *Table) payout() {
	hands := map[*Player]*hand.Hand{}
	for _, seat := range t.seats {
		hands[seat] = hand.New(append(seat.Cards, t.cards...))
	}
	for _, pot := range t.pots() {
		winners := []*Player{}
		if len(pot.contesting) == 1 {
			winners = []*Player{pot.contesting[0]}
		} else {
			// sort by best hand first
			sort.Slice(pot.contesting, func(i, j int) bool {
				iHand := hands[pot.contesting[i]]
				jHand := hands[pot.contesting[j]]
				return iHand.CompareTo(jHand) > 0
			})
			// select winners who split pot if more than one
			h1 := hands[pot.contesting[0]]
			for _, seat := range pot.contesting {
				h2 := hands[seat]
				if h1.CompareTo(h2) != 0 {
					break
				}
				winners = append(winners, seat)
			}
			// sort closest to the button for spare chips in split pot
			sort.Slice(winners, func(i, j int) bool {
				iDist := t.distanceFromButton(winners[i])
				jDist := t.distanceFromButton(winners[j])
				return iDist < jDist
			})
		}
		// payout chips
		for i, seat := range winners {
			seat.Chips += pot.chips / len(winners)
			if (pot.chips % len(winners)) > i {
				seat.Chips++
			}
		}
		// store for reporting
		t.lastWinners = make([]Player, len(winners))
		for i, w := range winners {
			t.lastWinners[i] = *w
		}
		t.lastContestants = make([]Player, len(pot.contesting))
		for i, c := range pot.contesting {
			t.lastContestants[i] = *c
		}
		t.lastCards = t.cards
	}
}

type sidePot struct {
	contesting []*Player
	chips      int
}

func (t *Table) pots() []*sidePot {
	contesting := t.contesting()
	sort.Slice(contesting, func(i, j int) bool {
		return contesting[i].ChipsInPot < contesting[j].ChipsInPot
	})
	costs := []int{}
	for _, seat := range contesting {
		if contains(costs, seat.ChipsInPot) == false {
			costs = append(costs, seat.ChipsInPot)
		}
	}
	pots := []*sidePot{}
	for i, cost := range costs {
		pot := &sidePot{}
		min := 0
		if i != 0 {
			min = costs[i-1]
		}
		for _, seat := range t.seats {
			pot.chips += max(seat.ChipsInPot-min, 0)
		}
		for _, seat := range contesting {
			if seat.ChipsInPot >= cost {
				pot.contesting = append(pot.contesting, seat)
			}
		}
		pots = append(pots, pot)
	}
	return pots
}

func (t *Table) resetAction() {
	for _, seat := range t.seats {
		if seat != nil {
			seat.Acted = false
		}
	}
}

func (t *Table) nextSeat(seat int) int {
	for {
		seat = (seat + 1) % len(t.seats)
		p := t.seats[seat]
		if p != nil && !p.SittingOut {
			return seat
		}
	}
}

func (t *Table) nextToAct() int {
	count := 0
	seat := t.active.Seat
	for {
		seat = t.nextSeat(seat)
		p := t.seats[seat]
		if !p.Acted && !p.AllIn && !p.Folded && !p.SittingOut {
			return p.Seat
		}
		count++
		if count == t.occupiedSeats()-1 {
			return -1
		}
	}
}

func (t *Table) occupiedSeats() int {
	count := 0
	for _, seat := range t.seats {
		if seat != nil {
			count++
		}
	}
	return count
}
func (t *Table) seatsSittingOut() int {
	count := 0
	for _, seat := range t.seats {
		if seat != nil && seat.SittingOut {
			count++
		}
	}
	return count
}

func (t *Table) owed() int {
	return t.cost - t.active.ChipsInPot
}

func (t *Table) distanceFromButton(p *Player) int {
	seat := t.button
	dist := 0
	for {
		seat = t.nextSeat(seat)
		dist++
		if p.Seat == seat {
			return dist
		}
	}
}

func (t *Table) contesting() []*Player {
	contesting := []*Player{}
	for _, seat := range t.seats {
		if !seat.Folded && !seat.SittingOut {
			contesting = append(contesting, seat)
		}
	}
	return contesting
}

type Player struct {
	ID         string
	Seat       int
	Chips      int
	ChipsInPot int
	Acted      bool
	Folded     bool
	AllIn      bool
	SittingOut bool
	Defaulting bool
	Cards      []hand.Card
}

func (p *Player) contribute(chips int) {
	amount := chips
	if p.Chips <= amount {
		amount = p.Chips
		p.AllIn = true
	}
	p.ChipsInPot += amount
	p.Chips -= amount
}

func includes(actions []ActionType, include ...ActionType) bool {
	for _, a1 := range include {
		found := false
		for _, a2 := range actions {
			found = found || a1 == a2
		}
		if !found {
			return false
		}
	}
	return true
}

func max(i, j int) int {
	if i > j {
		return i
	}
	return j
}

func contains(a []int, i int) bool {
	for _, v := range a {
		if v == i {
			return true
		}
	}
	return false
}
