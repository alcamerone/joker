package table

import (
	"errors"
	"slices"
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

var (
	ErrIllegalAction   = errors.New("table: illegal action attempted")
	ErrInsufficientBet = errors.New("table: bet or raise must be a minimum of the big blind")
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
	options Options
	seats   []*Player
	dealer  hand.Dealer
	deck    *hand.Deck
	cards   []hand.Card
	active  *Player
	status  Status
	round   Round
	button  int
	sb      int
	bb      int
	cost    int
	result  Result
}

// State is designed to be exposed to the front-end to provide enough information to render
type State struct {
	Options       Options
	Seats         []Player
	Deck          []hand.Card `,json:"-"`
	Cards         []hand.Card
	Status        Status
	Round         Round
	ActiveIdx     int
	DealerIdx     int
	SmallBlindIdx int
	BigBlindIdx   int
	Cost          int
	Pot           int
	Owed          int
	Result        Result
}

// Result describes the outcome of a completed hand. It is populated when a
// hand reaches showdown (or ends because all but one player has folded) and is
// exposed via State so callers can display the winner(s) and their cards.
type Result struct {
	// Winners are the players who won all or part of the pot.
	Winners []Player
	// Contestants are the players who were still in the hand when it ended.
	// When only one contestant is present the hand was won on a fold.
	Contestants []Player
	// TableCards are the community cards that were on the table at showdown.
	TableCards []hand.Card
}

func New(dealer hand.Dealer, opts Options, playerIDs []string, sittingOut []string) *Table {
	status := Dealing
	if len(playerIDs) < 2 {
		status = Broken
	}
	seats := []*Player{}
	for _, id := range playerIDs {
		p := &Player{
			ID:         id,
			Chips:      opts.Buyin,
			SittingOut: slices.Contains(sittingOut, id),
		}
		seats = append(seats, p)
	}
	// rand.Shuffle(len(seats), func(i int, j int) {
	// 	seats[i], seats[j] = seats[j], seats[i]
	// })
	for i, seat := range seats {
		seat.Seat = i
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

func NewFromState(dealer hand.Dealer, s State) *Table {
	t := &Table{
		options: s.Options,
		dealer:  dealer,
		cards:   s.Cards,
		status:  s.Status,
		round:   s.Round,
		button:  s.DealerIdx,
		sb:      s.SmallBlindIdx,
		bb:      s.BigBlindIdx,
		cost:    s.Cost,
		result:  s.Result,
	}
	t.seats = make([]*Player, len(s.Seats))
	for i := range s.Seats {
		t.seats[i] = &s.Seats[i]
	}
	t.deck = &hand.Deck{Cards: s.Deck}
	if len(t.seats) > 0 {
		t.active = t.seats[s.ActiveIdx]
	}

	return t
}

func (t *Table) State() State {
	if t == nil {
		return State{}
	}
	seats := []Player{}
	pot := 0
	activeIdx := 0
	for i, seat := range t.seats {
		seats = append(seats, seat.copy())
		pot += seat.ChipsInPot
		if seat == t.active {
			activeIdx = i
		}
	}
	return State{
		Options:       t.options,
		Seats:         seats,
		Deck:          append([]hand.Card(nil), t.deck.Cards...),
		Cards:         append([]hand.Card(nil), t.cards...),
		ActiveIdx:     activeIdx,
		DealerIdx:     t.button,
		SmallBlindIdx: t.sb,
		BigBlindIdx:   t.bb,
		Cost:          t.cost,
		Round:         t.round,
		Status:        t.status,
		Pot:           pot,
		Owed:          t.owed(),
		Result:        t.result,
	}
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
		return State{}, ErrIllegalAction
	}
	// TODO enforce limits, min bets
	switch a.Type {
	case Fold:
		t.active.Folded = true
	case Check:
	case Call:
		t.active.contribute(t.owed())
	case Bet, Raise:
		if a.Chips < t.options.Stakes.BigBlind {
			return State{}, ErrInsufficientBet
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
	t.update()
	return t.State(), nil
}

func (t *Table) Seats() []Player {
	seats := []Player{}
	for _, seat := range t.seats {
		seats = append(seats, *seat)
	}
	return seats
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

func (t *Table) update() {
	seat := t.nextToAct()
	if seat != -1 {
		t.active = t.seats[seat]
		return
	}
	if len(t.contesting()) == 1 || t.round == River {
		t.payout()
		if t.options.OneShot {
			// Stop here and wait for the caller to start the next hand
			// via NewRound
			t.status = Done
			return
		}
		t.round = PreFlop
	} else {
		t.round = (t.round + 1) % (River + 1)
	}
	t.setupRound()
}

func (t *Table) Active() *Player {
	return t.active
}

func (t *Table) NewRound() State {
	t.round = PreFlop
	if t.playingSeats() < 2 {
		t.status = Broken
	} else {
		t.status = Dealing
	}
	t.setupRound()
	return t.State()
}

// SetPlayerDefaulting marks (or unmarks) a seated player as sitting out. A
// player who is sitting out is folded for the current hand and is not dealt
// into subsequent hands until they are unmarked.
func (t *Table) SetPlayerDefaulting(id string, defaulting bool) {
	for _, seat := range t.seats {
		if seat != nil && seat.ID == id {
			seat.SittingOut = defaulting
			return
		}
	}
}

func (t *Table) AddPlayer(id string, sittingOut bool) {
	p := &Player{
		ID:         id,
		Seat:       len(t.seats),
		Chips:      t.options.Buyin,
		SittingOut: sittingOut,
		Folded:     true,
		Acted:      true,
	}
	t.seats = append(t.seats, p)
}

func (t *Table) BuyPlayerIn(id string) error {
	for _, seat := range t.seats {
		if seat != nil && seat.ID == id {
			seat.Chips = t.options.Buyin
			return nil
		}
	}
	return errors.New("table: cannot buy in unknown player")
}

func (t *Table) setupRound() {
	for _, seat := range t.seats {
		if seat != nil {
			seat.Acted = false
		}
	}
	switch t.round {
	case PreFlop:
		// Start of a fresh hand: clear the previous hand's community cards
		// and result, and reset each seat. Players who are sitting out are
		// folded for the hand and are not dealt in.
		t.cards = make([]hand.Card, 0)
		t.result = Result{}
		for _, seat := range t.seats {
			if seat == nil {
				continue
			}
			seat.ChipsInPot = 0
			seat.AllIn = false
			if seat.SittingOut {
				seat.Folded = true
				seat.Acted = true
				seat.Cards = make([]hand.Card, 0)
			} else {
				seat.Folded = false
				seat.Acted = false
			}
		}
		t.button = t.nextPlayingSeat(t.button)
		t.sb = t.nextPlayingSeat(t.button)
		t.bb = t.nextPlayingSeat(t.sb)
		if t.playingSeats() == 2 {
			t.sb = t.button
			t.bb = t.nextPlayingSeat(t.button)
		}
		t.deck = t.dealer.Deck()
		for _, seat := range t.seats {
			if seat != nil && !seat.SittingOut {
				seat.Cards = t.deck.PopMulti(2)
				seat.contribute(t.options.Stakes.Ante)
			}
		}
		t.seats[t.sb].contribute(t.options.Stakes.SmallBlind)
		t.seats[t.bb].contribute(t.options.Stakes.BigBlind)
		action := t.nextPlayingSeat(t.bb)
		t.active = t.seats[action]
		t.cost = t.options.Stakes.BigBlind
	case Flop:
		t.cards = t.deck.PopMulti(3)
		action := t.nextLiveSeat(t.button)
		t.active = t.seats[action]
	case Turn, River:
		t.cards = append(t.cards, t.deck.Pop())
		action := t.nextLiveSeat(t.button)
		t.active = t.seats[action]
	}
}

func (t *Table) payout() {
	contesting := t.contesting()
	hands := map[*Player]*hand.Hand{}
	for _, seat := range contesting {
		hands[seat] = hand.New(append(seat.Cards, t.cards...))
	}
	winnerSet := map[*Player]bool{}
	for _, pot := range t.pots() {
		winners := []*Player{}
		if len(pot.contesting) == 1 {
			// An uncontested pot (everyone else folded) needs no showdown. Hands
			// must not be compared here: before the flop a contestant has only
			// their two hole cards, which is too few for Hand.CompareTo.
			winners = append(winners, pot.contesting[0])
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
		}
		// sort closest to the button for spare chips in split pot
		sort.Slice(winners, func(i, j int) bool {
			iDist := t.distanceFromButton(winners[i])
			jDist := t.distanceFromButton(winners[j])
			return iDist < jDist
		})
		// payout chips
		for i, seat := range winners {
			seat.Chips += pot.chips / len(winners)
			if (pot.chips % len(winners)) > i {
				seat.Chips++
			}
			winnerSet[seat] = true
		}
	}
	t.recordResult(contesting, winnerSet)
}

// recordResult captures the outcome of the hand that has just finished so that
// it can be exposed to callers via State until the next hand begins.
func (t *Table) recordResult(contesting []*Player, winnerSet map[*Player]bool) {
	result := Result{
		Winners:     make([]Player, 0, len(winnerSet)),
		Contestants: make([]Player, 0, len(contesting)),
		TableCards:  append(make([]hand.Card, 0), t.cards...),
	}
	for _, seat := range contesting {
		result.Contestants = append(result.Contestants, *seat)
	}
	// Report winners in seat order for stable output.
	for _, seat := range t.seats {
		if seat != nil && winnerSet[seat] {
			result.Winners = append(result.Winners, *seat)
		}
	}
	t.result = result
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
		if slices.Contains(costs, seat.ChipsInPot) == false {
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
		if p != nil {
			return seat
		}
	}
}

// nextPlayingSeat returns the next seat, after the given one, that is occupied
// by a player who is not sitting out. It is used to assign the button, blinds
// and first-to-act preflop.
func (t *Table) nextPlayingSeat(seat int) int {
	for i := 0; i < len(t.seats); i++ {
		seat = (seat + 1) % len(t.seats)
		p := t.seats[seat]
		if p != nil && !p.SittingOut {
			return seat
		}
	}
	return t.nextSeat(seat)
}

// nextLiveSeat returns the next seat, after the given one, that is occupied by
// a player still in the hand (not folded). Sitting-out players are folded, so
// they are skipped too. It is used to assign first-to-act after the flop.
func (t *Table) nextLiveSeat(seat int) int {
	for i := 0; i < len(t.seats); i++ {
		seat = (seat + 1) % len(t.seats)
		p := t.seats[seat]
		if p != nil && !p.Folded {
			return seat
		}
	}
	return t.nextSeat(seat)
}

func (t *Table) nextToAct() int {
	count := 0
	seat := t.active.Seat
	for {
		seat = t.nextSeat(seat)
		p := t.seats[seat]
		if !p.Acted && !p.AllIn && !p.Folded {
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

func (t *Table) playingSeats() int {
	count := 0
	for _, seat := range t.seats {
		if seat != nil && !seat.SittingOut {
			count++
		}
	}
	return count
}

func (t *Table) owed() int {
	if t.active == nil {
		return 0
	}
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
		if seat.Folded == false {
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

func (p *Player) copy() Player {
	return Player{
		ID:         p.ID,
		Seat:       p.Seat,
		Chips:      p.Chips,
		ChipsInPot: p.ChipsInPot,
		Acted:      p.Acted,
		Folded:     p.Folded,
		AllIn:      p.AllIn,
		SittingOut: p.SittingOut,
		Cards:      append([]hand.Card(nil), p.Cards...),
	}
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
