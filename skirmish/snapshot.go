package skirmish

// ShipState is one ship in a snapshot.
type ShipState struct {
	ID      int     `json:"id"`
	Faction int     `json:"faction"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Angle   float64 `json:"angle"`
	HP      int     `json:"hp"`
}

// Snapshot is the machine-readable state of the battle at one tick. It is what
// -json emits: enough to follow, replay or score a fight from outside the
// process, without the caller knowing anything about the simulation internals.
type Snapshot struct {
	// Seed and the arena size repeat on every line on purpose: one line is then
	// enough to reproduce the run it came from, so a recording survives being
	// cut, filtered or piped.
	Seed    uint64      `json:"seed"`
	Width   float64     `json:"w"`
	Height  float64     `json:"h"`
	Tick    int         `json:"tick"`
	Alive   []int       `json:"alive"`
	Kills   []int       `json:"kills"`
	Bullets int         `json:"bullets"`
	Ships   []ShipState `json:"ships"`
}

// Snapshot captures the current state.
func (w *World) Snapshot() Snapshot {
	alive := w.Alive()
	kills := w.Kills // copy: a snapshot must not alias live counters
	s := Snapshot{
		Seed:    w.Cfg.Seed,
		Width:   w.Cfg.Width,
		Height:  w.Cfg.Height,
		Tick:    w.Tick,
		Alive:   alive[:],
		Kills:   kills[:],
		Bullets: len(w.Bullets),
		Ships:   make([]ShipState, 0, len(w.Ships)),
	}
	for i := range w.Ships {
		sh := &w.Ships[i]
		s.Ships = append(s.Ships, ShipState{
			ID:      sh.ID,
			Faction: int(sh.Faction),
			X:       sh.X,
			Y:       sh.Y,
			Angle:   sh.Angle,
			HP:      sh.HP,
		})
	}
	return s
}
