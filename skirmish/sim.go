// Package skirmish is the deterministic battle simulation behind Linefire
// Skirmish: hordes of autonomous ships from rival factions fighting in an open
// arena. It knows nothing about rendering, windows or the desktop, so the same
// simulation runs headless behind -json and, later, under ship AI written in
// Filo or driven by an external process over IPC.
//
// Determinism is the contract: same Config, same sequence of Steps, same World.
// Nothing here reads the clock, iterates a map, or touches global state.
package skirmish

import (
	"math"
	"math/rand/v2"
)

// Faction is a side in the battle.
type Faction int

const (
	FactionA Faction = iota
	FactionB
	FactionCount
)

// Tuning. These are the whole balance of the fight; keeping them together makes
// the feel of the battle one screen of numbers instead of a hunt through code.
const (
	ShipRadius   = 15.0 // hit radius in arena pixels
	shipHP       = 3
	maxSpeed     = 3.2   // pixels per tick
	accel        = 0.15  // thrust acceleration
	drag         = 0.985 // velocity kept per tick; ships coast but do not drift forever
	turnRate     = 0.08  // radians per tick
	standoff     = 180.0 // preferred range: ships circle their target instead of ramming
	engageRange  = 460.0 // acquire and shoot inside this
	fireCone     = 0.26  // radians of aim error tolerated before shooting
	fireInterval = 26    // ticks between shots
	bulletSpeed  = 7.0
	bulletLife   = 90
	bulletRadius = 2.0
	edgeMargin   = 100.0 // steer back once this close to the border
	respawnDelay = 120   // ticks a destroyed ship waits before rejoining
)

// Ship is one autonomous fighter. A destroyed ship keeps its slot with HP 0 and
// a respawn countdown, so IDs stay stable and the roster never reallocates.
type Ship struct {
	ID      int
	Faction Faction
	X, Y    float64
	VX, VY  float64
	Angle   float64 // facing, radians, 0 = +X
	HP      int
	cool    int // ticks until the gun is ready
	respawn int // ticks until revival; only meaningful while HP is 0
}

// Alive reports whether the ship is in the fight this tick.
func (s *Ship) Alive() bool { return s.HP > 0 }

// Bullet is a shot in flight. It carries its faction so it only harms the other
// side, which is also what lets a stray shot cross a friendly formation.
type Bullet struct {
	Faction Faction
	X, Y    float64
	VX, VY  float64
	life    int
}

// Config describes the arena and the roster. The zero value is not useful:
// build one with sane sizes, then call New.
type Config struct {
	Width, Height float64
	PerSide       int
	Seed          uint64
}

// World is the whole simulation state.
type World struct {
	Cfg     Config
	Tick    int
	Ships   []Ship
	Bullets []Bullet
	Kills   [FactionCount]int

	rng    *rand.Rand
	orders []Order // reused per tick so stepping allocates nothing
}

// New builds a world with both rosters spawned on opposite sides of the arena.
func New(cfg Config) *World {
	if cfg.PerSide < 1 {
		cfg.PerSide = 1
	}
	w := &World{
		Cfg: cfg,
		// A seeded PCG is the point: the battle must be reproducible from Seed
		// alone, which is exactly what a cryptographic source cannot give.
		rng:    rand.New(rand.NewPCG(cfg.Seed, cfg.Seed^0x9e3779b9)), // #nosec G404
		Ships:  make([]Ship, 0, cfg.PerSide*int(FactionCount)),
		orders: make([]Order, 0, cfg.PerSide*int(FactionCount)),
	}
	for f := range FactionCount {
		for range cfg.PerSide {
			w.Ships = append(w.Ships, w.spawnShip(len(w.Ships), f))
		}
	}
	return w
}

// spawnShip places a fresh ship in its faction's home band: faction A on the
// left third, faction B on the right third, so the first seconds are an
// approach rather than an instant scrum.
func (w *World) spawnShip(id int, f Faction) Ship {
	x := w.rng.Float64() * w.Cfg.Width / 3
	if f == FactionB {
		x = w.Cfg.Width - x
	}
	angle := 0.0
	if f == FactionB {
		angle = math.Pi
	}
	return Ship{
		ID:      id,
		Faction: f,
		X:       x,
		Y:       w.rng.Float64() * w.Cfg.Height,
		Angle:   angle,
		HP:      shipHP,
	}
}

// Step advances the simulation by one tick.
func (w *World) Step() {
	w.Tick++

	// Every ship decides from the same snapshot, so the outcome does not depend
	// on roster order — a ship early in the slice must not fight an arena that
	// has already moved.
	w.orders = w.orders[:0]
	for i := range w.Ships {
		w.orders = append(w.orders, w.decide(i))
	}
	for i := range w.Ships {
		w.apply(i, w.orders[i])
	}

	w.moveBullets()
	w.resolveHits()
	w.reviveDead()
}

// apply turns one order into movement and, if the gun is ready, a shot.
func (w *World) apply(i int, o Order) {
	s := &w.Ships[i]
	if !s.Alive() {
		return
	}

	s.Angle = turnToward(s.Angle, o.Face, turnRate)

	if o.Thrust > 0 {
		t := min(o.Thrust, 1)
		s.VX += math.Cos(o.Move) * accel * t
		s.VY += math.Sin(o.Move) * accel * t
	}
	s.VX *= drag
	s.VY *= drag
	speed := math.Hypot(s.VX, s.VY)
	if speed > maxSpeed {
		s.VX *= maxSpeed / speed
		s.VY *= maxSpeed / speed
	}
	s.X += s.VX
	s.Y += s.VY

	// The border is a hard wall: the AI steers away from it, and anything that
	// still reaches it bounces instead of leaving the screen.
	if s.X < ShipRadius || s.X > w.Cfg.Width-ShipRadius {
		s.X = clamp(s.X, ShipRadius, w.Cfg.Width-ShipRadius)
		s.VX = -s.VX
	}
	if s.Y < ShipRadius || s.Y > w.Cfg.Height-ShipRadius {
		s.Y = clamp(s.Y, ShipRadius, w.Cfg.Height-ShipRadius)
		s.VY = -s.VY
	}

	if s.cool > 0 {
		s.cool--
	}
	if !o.Fire || s.cool > 0 {
		return
	}
	s.cool = fireInterval
	w.Bullets = append(w.Bullets, Bullet{
		Faction: s.Faction,
		X:       s.X + math.Cos(s.Angle)*ShipRadius,
		Y:       s.Y + math.Sin(s.Angle)*ShipRadius,
		VX:      s.VX + math.Cos(s.Angle)*bulletSpeed,
		VY:      s.VY + math.Sin(s.Angle)*bulletSpeed,
		life:    bulletLife,
	})
}

// moveBullets advances shots and drops the ones that expired or left the arena,
// compacting in place so the slice keeps its capacity.
func (w *World) moveBullets() {
	kept := w.Bullets[:0]
	for _, b := range w.Bullets {
		b.X += b.VX
		b.Y += b.VY
		b.life--
		if b.life <= 0 {
			continue
		}
		if b.X < 0 || b.Y < 0 || b.X > w.Cfg.Width || b.Y > w.Cfg.Height {
			continue
		}
		kept = append(kept, b)
	}
	w.Bullets = kept
}

// resolveHits damages ships hit by enemy fire. A bullet is spent on the first
// ship it touches.
func (w *World) resolveHits() {
	kept := w.Bullets[:0]
	for _, b := range w.Bullets {
		hit := false
		for i := range w.Ships {
			s := &w.Ships[i]
			if !s.Alive() || s.Faction == b.Faction {
				continue
			}
			if math.Hypot(s.X-b.X, s.Y-b.Y) > ShipRadius+bulletRadius {
				continue
			}
			s.HP--
			hit = true
			if s.HP <= 0 {
				s.HP = 0
				s.respawn = respawnDelay
				w.Kills[b.Faction]++
			}
			break
		}
		if !hit {
			kept = append(kept, b)
		}
	}
	w.Bullets = kept
}

// reviveDead brings destroyed ships back at their faction's home band, so the
// battle never runs out of combatants.
func (w *World) reviveDead() {
	for i := range w.Ships {
		s := &w.Ships[i]
		if s.Alive() {
			continue
		}
		s.respawn--
		if s.respawn > 0 {
			continue
		}
		*s = w.spawnShip(s.ID, s.Faction)
	}
}

// Alive counts the living ships of each faction.
func (w *World) Alive() [FactionCount]int {
	var n [FactionCount]int
	for i := range w.Ships {
		if w.Ships[i].Alive() {
			n[w.Ships[i].Faction]++
		}
	}
	return n
}

// Resize adapts the arena to a new screen size, keeping every ship inside it.
// The overlay calls this when the display or window size changes.
func (w *World) Resize(width, height float64) {
	if width <= 0 || height <= 0 {
		return
	}
	w.Cfg.Width, w.Cfg.Height = width, height
	for i := range w.Ships {
		w.Ships[i].X = clamp(w.Ships[i].X, ShipRadius, width-ShipRadius)
		w.Ships[i].Y = clamp(w.Ships[i].Y, ShipRadius, height-ShipRadius)
	}
}

// turnToward rotates from toward to by at most rate, taking the short way round.
func turnToward(from, to, rate float64) float64 {
	d := wrapAngle(to - from)
	if math.Abs(d) <= rate {
		return wrapAngle(to)
	}
	return wrapAngle(from + math.Copysign(rate, d))
}

// wrapAngle normalizes an angle to (-pi, pi].
func wrapAngle(a float64) float64 {
	for a > math.Pi {
		a -= 2 * math.Pi
	}
	for a <= -math.Pi {
		a += 2 * math.Pi
	}
	return a
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
