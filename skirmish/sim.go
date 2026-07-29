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

	// portalLead is how long the vortex is open before the ship drops out of it,
	// and openingStagger spreads the first wave so the battle does not begin with
	// every portal firing on the same frame.
	portalLead     = 36
	openingStagger = 90

	// Separation, borrowed from flocking: a short-range push away from whatever is
	// nearby, so ships circling the same target stop flying through each other.
	// Only separation — cohesion and alignment would make the battle a flock.
	separationRange = 4 * ShipRadius // start pushing apart inside this
	separationGain  = 1.5            // strength of the push against the tactical heading

	// How much of the computed intercept a ship actually aims at. Deliberately
	// short of 1: a gunner that never misses turns the dogfight into an execution,
	// while no lead at all leaves both sides orbiting until someone is hit by
	// accident. The gap is the fight.
	leadAccuracy = 0.6

	// A portal is not precise: it drops a ship somewhere out in the arena rather
	// than on a marked spot. spawnCenterHole is the share of the way out from the
	// centre that stays clear, so arrivals never land in the middle of the screen;
	// spawnCandidates is how many points are drawn before keeping the one furthest
	// from every living ship, which is what spreads arrivals apart without a
	// retry loop that could fail to place anything.
	spawnCenterHole = 0.35
	spawnCandidates = 12
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

	// Thrust and Burn are what the engines did last tick: how hard, and in which
	// direction. They are kept on the ship, not just in the Order, because the
	// exhaust is drawn from them a frame later.
	Thrust float64 // 0..1, zero while coasting
	Burn   float64 // radians

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

// Arrival is a portal opening: a ship is on its way in, and this is where it
// will come out. It is announced portalLead ticks before the ship exists, so
// whatever draws the portal has time to open it before anything flies out.
type Arrival struct {
	X, Y    float64
	Faction Faction
}

// Death is a ship destroyed during the last Step. The sim has no idea what a
// renderer will do with it — play an explosion, log a kill, tell another process
// over IPC — it just reports that it happened and where.
type Death struct {
	X, Y    float64
	Faction Faction
}

// World is the whole simulation state.
type World struct {
	Cfg     Config
	Tick    int
	Ships   []Ship
	Bullets []Bullet
	Kills   [FactionCount]int

	rng      *rand.Rand
	orders   []Order   // reused per tick so stepping allocates nothing
	deaths   []Death   // this tick's kills; reused for the same reason
	arrivals []Arrival // this tick's portals, likewise
}

// New builds a world whose rosters have not arrived yet: every ship starts
// destroyed with a short, staggered timer, so the opening is portals dropping a
// horde in rather than ships blinking into place, and the first arrival goes
// through exactly the same path as the thousandth.
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
			id := len(w.Ships)
			w.Ships = append(w.Ships, Ship{
				ID:      id,
				Faction: f,
				respawn: portalLead + 1 + w.rng.IntN(openingStagger),
			})
		}
	}
	return w
}

// newShip is a ship coming out of a portal at (x, y). Facing is random: a portal
// does not aim what it throws.
func (w *World) newShip(id int, f Faction, x, y float64) Ship {
	return Ship{
		ID:      id,
		Faction: f,
		X:       x,
		Y:       y,
		Angle:   wrapAngle(w.rng.Float64() * 2 * math.Pi),
		HP:      shipHP,
	}
}

// spawnPoint draws spawnCandidates arrival points and keeps the one furthest from
// any living ship. Best-candidate sampling rather than reject-and-retry: it always
// returns a point, costs a fixed number of draws, and stays deterministic.
func (w *World) spawnPoint() (float64, float64) {
	bestX, bestY, bestDist := 0.0, 0.0, -1.0
	for range spawnCandidates {
		x, y := w.arrivalPoint()
		d := w.distToNearestShip(x, y)
		if d <= bestDist {
			continue
		}
		bestX, bestY, bestDist = x, y, d
	}
	return bestX, bestY
}

// arrivalPoint picks one point in the arena outside the central keep-out, drawn
// in polar form around the centre so every direction is equally likely and the
// clear middle costs no rejections.
func (w *World) arrivalPoint() (float64, float64) {
	angle := w.rng.Float64() * 2 * math.Pi
	reach := spawnCenterHole + w.rng.Float64()*(1-spawnCenterHole)
	cx, cy := w.Cfg.Width/2, w.Cfg.Height/2
	x := cx + math.Cos(angle)*reach*(cx-ShipRadius)
	y := cy + math.Sin(angle)*reach*(cy-ShipRadius)
	return clamp(x, ShipRadius, w.Cfg.Width-ShipRadius), clamp(y, ShipRadius, w.Cfg.Height-ShipRadius)
}

// distToNearestShip measures how clear a candidate point is. Ships already
// flying count, and so do the ones whose portal is open but who have not dropped
// out of it yet — otherwise two portals opening within a second of each other
// would happily pick the same spot. A wreck with no portal yet has no position
// to crowd anything with.
func (w *World) distToNearestShip(x, y float64) float64 {
	best := math.Inf(1)
	for i := range w.Ships {
		s := &w.Ships[i]
		if !s.Alive() && s.respawn > portalLead {
			continue
		}
		d := math.Hypot(s.X-x, s.Y-y)
		if d >= best {
			continue
		}
		best = d
	}
	return best
}

// Deaths lists the ships destroyed during the last Step. The slice is the
// world's own and is emptied by the next one.
func (w *World) Deaths() []Death { return w.deaths }

// Arrivals lists the portals that opened during the last Step, on the same terms
// as Deaths.
func (w *World) Arrivals() []Arrival { return w.arrivals }

// Step advances the simulation by one tick.
func (w *World) Step() {
	w.Tick++
	w.deaths = w.deaths[:0]
	w.arrivals = w.arrivals[:0]

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

	s.Thrust, s.Burn = 0, o.Move
	if o.Thrust > 0 {
		s.Thrust = min(o.Thrust, 1)
		s.VX += math.Cos(o.Move) * accel * s.Thrust
		s.VY += math.Sin(o.Move) * accel * s.Thrust
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
				w.deaths = append(w.deaths, Death{X: s.X, Y: s.Y, Faction: s.Faction})
			}
			break
		}
		if !hit {
			kept = append(kept, b)
		}
	}
	w.Bullets = kept
}

// reviveDead runs the arrival of every ship waiting to fly, so the battle never
// runs out of combatants. A portal opens portalLead ticks ahead of the ship it
// delivers: the point is chosen then, announced, and parked on the dead ship —
// X and Y are free while it has no hull — so the vortex and the ship agree on
// where it comes out.
func (w *World) reviveDead() {
	for i := range w.Ships {
		s := &w.Ships[i]
		if s.Alive() {
			continue
		}
		s.respawn--
		switch {
		case s.respawn == portalLead:
			s.X, s.Y = w.spawnPoint()
			w.arrivals = append(w.arrivals, Arrival{X: s.X, Y: s.Y, Faction: s.Faction})
		case s.respawn <= 0:
			*s = w.newShip(s.ID, s.Faction, s.X, s.Y)
		}
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
