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
