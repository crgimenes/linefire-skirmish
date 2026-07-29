package skirmish

import (
	"math"
	"reflect"
	"testing"
)

func testConfig(seed uint64) Config {
	return Config{Width: 1280, Height: 800, PerSide: 6, Seed: seed}
}

// The sim is the shared contract between the overlay, -json and (later) user
// scripts: two runs of the same seed must agree tick for tick, or none of those
// can be compared or replayed.
func TestSameSeedSameBattle(t *testing.T) {
	a, b := New(testConfig(42)), New(testConfig(42))
	for range 600 {
		a.Step()
		b.Step()
	}
	if !reflect.DeepEqual(a.Snapshot(), b.Snapshot()) {
		t.Fatal("same seed diverged")
	}
	if reflect.DeepEqual(a.Snapshot(), New(testConfig(43)).Snapshot()) {
		t.Fatal("different seeds produced the same battle")
	}
}

func TestShipsStayInTheArena(t *testing.T) {
	w := New(testConfig(7))
	for range 3000 {
		w.Step()
		for j := range w.Ships {
			s := &w.Ships[j]
			if s.X < 0 || s.X > w.Cfg.Width || s.Y < 0 || s.Y > w.Cfg.Height {
				t.Fatalf("tick %d: ship %d escaped at %.1f,%.1f", w.Tick, s.ID, s.X, s.Y)
			}
		}
	}
}

// The overlay runs for hours, so the fight has to be self-sustaining: both sides
// keep scoring and neither roster is wiped out for good.
func TestBattleSustainsItself(t *testing.T) {
	w := New(testConfig(1))
	for range 6000 {
		w.Step()
	}
	for f := range FactionCount {
		if w.Kills[f] == 0 {
			t.Errorf("faction %d never scored in 6000 ticks", f)
		}
	}
	alive := w.Alive()
	for f := range FactionCount {
		if alive[f] == 0 {
			t.Errorf("faction %d has no ships left", f)
		}
	}
	if len(w.Bullets) > w.Cfg.PerSide*int(FactionCount)*bulletLife {
		t.Errorf("bullets accumulating: %d in flight", len(w.Bullets))
	}
}

// A portal drops its ship somewhere out in the arena: never in the middle of the
// screen, and away from whatever is already flying. Both hold for the opening
// roster and for every respawn afterwards, so the check runs over a whole battle.
func TestPortalsDropShipsApartAndOffCentre(t *testing.T) {
	cfg := testConfig(11)
	w := New(cfg)
	cx, cy := cfg.Width/2, cfg.Height/2
	holeX, holeY := spawnCenterHole*(cx-ShipRadius), spawnCenterHole*(cy-ShipRadius)

	checkArrival := func(s *Ship) {
		t.Helper()
		dx, dy := math.Abs(s.X-cx)/holeX, math.Abs(s.Y-cy)/holeY
		if math.Hypot(dx, dy) < 1 {
			t.Errorf("tick %d: ship %d arrived at %.1f,%.1f, inside the clear centre", w.Tick, s.ID, s.X, s.Y)
		}
		for i := range w.Ships {
			o := &w.Ships[i]
			if o.ID == s.ID || !o.Alive() {
				continue
			}
			if d := math.Hypot(o.X-s.X, o.Y-s.Y); d < 2*ShipRadius {
				t.Errorf("tick %d: ship %d arrived %.1f from ship %d", w.Tick, s.ID, d, o.ID)
			}
		}
	}

	for i := range w.Ships {
		checkArrival(&w.Ships[i])
	}

	// Watch respawns: a ship that was dead last tick and is alive now just arrived.
	wasDead := make([]bool, len(w.Ships))
	for range 3000 {
		w.Step()
		for i := range w.Ships {
			s := &w.Ships[i]
			if wasDead[i] && s.Alive() {
				checkArrival(s)
			}
			wasDead[i] = !s.Alive()
		}
	}
}

// The whole point of the scatter is that arrivals are not bunched: measure it
// against what plain uniform placement would give.
func TestArrivalsBeatUniformScatter(t *testing.T) {
	cfg := testConfig(5)
	spread := func(w *World) float64 {
		worst := math.Inf(1)
		for i := range w.Ships {
			for j := i + 1; j < len(w.Ships); j++ {
				d := math.Hypot(w.Ships[i].X-w.Ships[j].X, w.Ships[i].Y-w.Ships[j].Y)
				worst = min(worst, d)
			}
		}
		return worst
	}

	w := New(cfg)
	uniform := New(cfg)
	for i := range uniform.Ships {
		uniform.Ships[i].X = uniform.rng.Float64() * cfg.Width
		uniform.Ships[i].Y = uniform.rng.Float64() * cfg.Height
	}
	if spread(w) <= spread(uniform) {
		t.Errorf("closest pair %.1f apart, no better than uniform placement at %.1f", spread(w), spread(uniform))
	}
}

// Deaths is what the overlay hangs an explosion on, so it has to report the spot
// the ship was actually destroyed at, and only for the tick it happened.
func TestDeathsReportWhereAShipDied(t *testing.T) {
	w := New(testConfig(4))
	victim := &w.Ships[0]
	victim.HP = 1
	x, y := victim.X, victim.Y
	w.Bullets = append(w.Bullets, Bullet{
		Faction: FactionB,
		X:       x, Y: y,
		life: bulletLife,
	})

	w.Step()
	deaths := w.Deaths()
	if len(deaths) != 1 {
		t.Fatalf("one ship died, Deaths reported %d", len(deaths))
	}
	if deaths[0].Faction != FactionA {
		t.Errorf("death reported for faction %v, want %v", deaths[0].Faction, FactionA)
	}
	if math.Hypot(deaths[0].X-x, deaths[0].Y-y) > maxSpeed {
		t.Errorf("death at %.1f,%.1f, nowhere near the ship at %.1f,%.1f", deaths[0].X, deaths[0].Y, x, y)
	}

	w.Step()
	if len(w.Deaths()) != 0 {
		t.Errorf("deaths from an earlier tick are still being reported: %d", len(w.Deaths()))
	}
}

func TestDestroyedShipRespawns(t *testing.T) {
	w := New(testConfig(3))
	s := &w.Ships[0]
	s.HP = 0
	s.respawn = respawnDelay
	for range respawnDelay {
		w.Step()
	}
	if !w.Ships[0].Alive() {
		t.Fatalf("ship still dead %d ticks after being destroyed", respawnDelay)
	}
	if w.Ships[0].ID != 0 || w.Ships[0].Faction != FactionA {
		t.Fatal("respawn changed the ship's identity")
	}
}

func TestResizeKeepsShipsInside(t *testing.T) {
	w := New(testConfig(9))
	for range 120 {
		w.Step()
	}
	w.Resize(400, 300)
	for i := range w.Ships {
		s := &w.Ships[i]
		if s.X < 0 || s.X > 400 || s.Y < 0 || s.Y > 300 {
			t.Fatalf("ship %d outside the resized arena at %.1f,%.1f", s.ID, s.X, s.Y)
		}
	}
}

// Leading is the difference between a dogfight and two groups orbiting each
// other: a crossing target has to be aimed ahead of, a still one straight at.
func TestAimLeadsAMovingTarget(t *testing.T) {
	shooter := Ship{X: 0, Y: 0}
	still := Ship{X: 100, Y: 0}
	if got := aimAt(&shooter, &still); math.Abs(got) > 1e-9 {
		t.Errorf("aim at a still target = %v, want straight ahead", got)
	}

	crossing := Ship{X: 100, Y: 0, VY: 2}
	lead := aimAt(&shooter, &crossing)
	if lead <= 0 {
		t.Errorf("aim at a target crossing to +Y = %v, want ahead of it", lead)
	}
	// The shot inherits the shooter's velocity, so matching the target's drift
	// leaves nothing to lead: only relative motion counts.
	matched := shooter
	matched.VY = crossing.VY
	if got := aimAt(&matched, &crossing); math.Abs(got) > 1e-9 {
		t.Errorf("aim while drifting with the target = %v, want straight ahead", got)
	}

	// An uncatchable target (outrunning the shot, opening away) still gets a
	// heading rather than a solver failure.
	fleeing := Ship{X: 100, Y: 0, VX: bulletSpeed * 2}
	if got := aimAt(&shooter, &fleeing); math.IsNaN(got) {
		t.Error("aim at an uncatchable target returned NaN")
	}
}

// Ships crowding the same spot have to be pushed apart, whatever side they fly
// for — overlapping hulls read as a rendering fault, not as tactics.
func TestSeparationPushesCrowdedShipsApart(t *testing.T) {
	w := New(testConfig(2))
	w.Ships[0] = Ship{ID: 0, Faction: FactionA, X: 500, Y: 500, HP: shipHP}
	w.Ships[1] = Ship{ID: 1, Faction: FactionB, X: 500 + ShipRadius, Y: 500, HP: shipHP}
	for i := 2; i < len(w.Ships); i++ {
		w.Ships[i].HP = 0 // clear the arena: only the crowded pair matters here
	}

	west := math.Pi // straight at the neighbour, who sits to the east
	got := w.separate(&w.Ships[0], west-math.Pi)
	if math.Abs(wrapAngle(got-west)) > math.Pi/2 {
		t.Errorf("heading into a neighbour bent to %v, still not away from it", got)
	}

	alone := w.Ships[0]
	alone.X, alone.Y = 100, 100
	w.Ships[1].HP = 0
	if got := w.separate(&alone, 1.0); got != 1.0 {
		t.Errorf("separation moved a ship with nobody near it: %v, want 1", got)
	}
}

func TestTurnTowardTakesTheShortWay(t *testing.T) {
	tests := []struct {
		from, to, want float64
	}{
		{0, 0.01, 0.01},                             // closer than one step: snap
		{0, math.Pi / 2, turnRate},                  // counter-clockwise
		{0, -math.Pi / 2, -turnRate},                // clockwise
		{3, -3, wrapAngle(3 + turnRate)},            // across the +pi seam
		{-3, 3, wrapAngle(-3 - turnRate)},           // and back
		{math.Pi, math.Pi, wrapAngle(math.Pi)},      // already there
		{0, math.Pi + 0.5, wrapAngle(0 - turnRate)}, // to is normalized first
	}
	for _, tt := range tests {
		got := turnToward(tt.from, tt.to, turnRate)
		if math.Abs(got-tt.want) > 1e-9 {
			t.Errorf("turnToward(%v, %v) = %v, want %v", tt.from, tt.to, got, tt.want)
		}
	}
}
