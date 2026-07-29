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

// A portal opens somewhere out in the arena: never in the middle of the screen,
// and clear of everything already flying or already promised a spot. The opening
// wave and every respawn go through the same path, so the check runs over a whole
// battle. What is asserted is the point at the moment it is CHOSEN — ships drift
// while the vortex is open, and one wandering under a portal is a hazard of the
// battle, not a placement fault.
func TestPortalsOpenApartAndOffCentre(t *testing.T) {
	cfg := testConfig(11)
	w := New(cfg)
	cx, cy := cfg.Width/2, cfg.Height/2
	holeX, holeY := spawnCenterHole*(cx-ShipRadius), spawnCenterHole*(cy-ShipRadius)

	opened := 0
	for range 3000 {
		w.Step()
		for _, a := range w.Arrivals() {
			opened++
			dx, dy := math.Abs(a.X-cx)/holeX, math.Abs(a.Y-cy)/holeY
			if math.Hypot(dx, dy) < 1 {
				t.Fatalf("tick %d: portal opened at %.1f,%.1f, inside the clear centre", w.Tick, a.X, a.Y)
			}
			for i := range w.Ships {
				o := &w.Ships[i]
				if !o.Alive() || math.Hypot(o.X-a.X, o.Y-a.Y) >= 2*ShipRadius {
					continue
				}
				t.Fatalf("tick %d: portal opened on top of ship %d", w.Tick, o.ID)
			}
		}
	}
	if opened < len(w.Ships) {
		t.Fatalf("only %d portals opened in 3000 ticks, fewer than the %d ships", opened, len(w.Ships))
	}
}

// Best-candidate sampling has to earn the draws it costs: over a busy arena it
// must put portals further from the nearest ship than a single blind draw would.
func TestBestCandidateBeatsASingleDraw(t *testing.T) {
	w := New(testConfig(5))
	for range 400 {
		w.Step() // a populated, moving arena to place into
	}

	const trials = 200
	var chosen, blind float64
	for range trials {
		x, y := w.spawnPoint()
		chosen += w.distToNearestShip(x, y)
		x, y = w.arrivalPoint()
		blind += w.distToNearestShip(x, y)
	}
	if chosen <= blind {
		t.Errorf("best-candidate averaged %.0f px of clearance, a single draw %.0f", chosen/trials, blind/trials)
	}
}

// Deaths is what the overlay hangs an explosion on, so it has to report the spot
// the ship was actually destroyed at, and only for the tick it happened.
func TestDeathsReportWhereAShipDied(t *testing.T) {
	w := New(testConfig(4))
	for range PortalLead + openingStagger + 2 {
		w.Step() // let the opening wave come through its portals
	}
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
	for range 300 {
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
