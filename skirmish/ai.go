package skirmish

import "math"

// Order is one ship's decision for one tick. It is deliberately the smallest
// useful command set, because this is the contract a Filo script — and later an
// external program speaking the ship protocol over IPC — will emit: where to
// aim, where to push, how hard, and whether to shoot. Aim and movement are
// separate so a ship can keep its guns on a target while backing off.
type Order struct {
	Face   float64 // desired facing, radians
	Move   float64 // desired thrust direction, radians
	Thrust float64 // 0..1
	Fire   bool
}

// decide is the built-in AI: acquire the nearest enemy, hold a firing standoff
// while circling it, and shoot when the nose lines up. It is the reference
// opponent and the default script — a Filo program replaces this per faction,
// so keep it readable rather than clever.
func (w *World) decide(i int) Order {
	s := &w.Ships[i]
	if !s.Alive() {
		return Order{}
	}

	target := w.acquire(i)
	if target < 0 {
		return w.patrol(s)
	}

	t := &w.Ships[target]
	dx, dy := t.X-s.X, t.Y-s.Y
	dist := math.Hypot(dx, dy)
	bearing := math.Atan2(dy, dx) // where the target is; movement is built on this
	face := aimAt(s, t)           // where to shoot; ahead of it
	standoff := s.Standoff()      // the range this hull likes to hold

	// Closing, holding or backing off, depending on the range band. Inside the
	// band the ship strafes sideways, which keeps it a moving target and makes
	// the swarm read as a dogfight instead of two lines meeting head-on.
	move := bearing // closing in
	switch {
	case dist < standoff*0.7:
		move = bearing + math.Pi // too close: back off, guns still on the target
	case dist < standoff*1.15:
		move = bearing + math.Pi/2 // in the band: strafe, alternating side by ID
		if s.ID%2 == 0 {
			move = bearing - math.Pi/2
		}
	}
	move = w.separate(s, move)
	move = w.avoidEdges(s, move)

	aimed := math.Abs(wrapAngle(face-s.Angle)) < fireCone
	return Order{
		Face:   face,
		Move:   move,
		Thrust: 1,
		Fire:   aimed && dist < s.radar,
	}
}

// acquire is the sensor: it returns the nearest enemy the ship can actually see,
// or -1. A ship engages only inside its radar, and once engaged it holds the
// contact for a grace period after it slips back out — so a fast target cannot
// shake a pursuer by stepping over the line — with the radar widened for as long
// as it is hunting. Losing the contact for good resets the reach and sends the
// ship back to patrolling.
func (w *World) acquire(i int) int {
	s := &w.Ships[i]
	target := w.nearestEnemyWithin(i, s.radar)

	if s.combat {
		if target >= 0 {
			s.grace = combatGrace
			return target
		}
		s.grace--
		if s.grace > 0 {
			return -1 // still hunting, but with nothing in reach to aim at
		}
		s.combat = false
		s.radar = s.EngageRange()
		return -1
	}

	if target < 0 {
		return -1
	}
	s.combat = true
	s.grace = combatGrace
	s.radar = max(s.radar, s.EngageRange()*radarCombatMul)
	return target
}

// patrol cruises an idle ship along a heading it commits to for a while, so it
// reads as searching the arena instead of jittering on the spot. It flies slower
// than it fights, and takes a fresh heading when the timer runs out.
func (w *World) patrol(s *Ship) Order {
	s.wanderCD--
	if s.wanderCD <= 0 {
		s.wanderHead = wrapAngle(s.wanderHead + (w.rng.Float64()*2-1)*patrolTurnRange)
		s.wanderCD = patrolHold + w.rng.IntN(patrolHold)
	}
	move := w.separate(s, s.wanderHead)
	move = w.avoidEdges(s, move)
	return Order{Face: move, Move: move, Thrust: patrolThrust}
}

// aimAt returns the heading that puts a shot where the target is going, rather
// than where it is now. Without this two groups orbit each other for a long time
// and only connect by accident, because every shot is aimed at a position the
// target has already left.
//
// The intercept is solved in the shooter's frame — a shot inherits the ship's
// velocity, so only relative motion matters — and only leadAccuracy of it is
// applied. A target that cannot be caught (faster than the shot, or closing time
// that does not solve) falls back to firing straight at it.
func aimAt(s, t *Ship) float64 {
	rx, ry := t.X-s.X, t.Y-s.Y
	vx, vy := t.VX-s.VX, t.VY-s.VY

	// |r + v*time| = shot speed * time, as a quadratic in time.
	shot := s.ShotSpeed()
	a := vx*vx + vy*vy - shot*shot
	b := 2 * (rx*vx + ry*vy)
	c := rx*rx + ry*ry
	time, ok := smallestPositiveRoot(a, b, c)
	if !ok {
		return math.Atan2(ry, rx)
	}
	return math.Atan2(ry+vy*time*leadAccuracy, rx+vx*time*leadAccuracy)
}

// smallestPositiveRoot solves a*x^2 + b*x + c for its smallest positive root,
// degrading to the linear case when a is zero (target and shot equally fast).
func smallestPositiveRoot(a, b, c float64) (float64, bool) {
	if math.Abs(a) < 1e-9 {
		if math.Abs(b) < 1e-9 {
			return 0, false
		}
		x := -c / b
		return x, x > 0
	}
	disc := b*b - 4*a*c
	if disc < 0 {
		return 0, false
	}
	root := math.Sqrt(disc)
	x1, x2 := (-b-root)/(2*a), (-b+root)/(2*a)
	if x1 > x2 {
		x1, x2 = x2, x1
	}
	if x1 > 0 {
		return x1, true
	}
	return x2, x2 > 0
}

// separate blends a push away from nearby ships into the tactical heading, so
// two ships working the same target stop occupying the same spot. Enemies count
// as well as allies: overlapping anything reads as a rendering fault.
func (w *World) separate(s *Ship, move float64) float64 {
	ax, ay := math.Cos(move), math.Sin(move)
	for i := range w.Ships {
		o := &w.Ships[i]
		if o.ID == s.ID || !o.Alive() {
			continue
		}
		dx, dy := s.X-o.X, s.Y-o.Y
		d := math.Hypot(dx, dy)
		if d >= separationRange || d == 0 {
			continue
		}
		push := (separationRange - d) / separationRange // full at contact, nothing at the rim
		ax += dx / d * push * separationGain
		ay += dy / d * push * separationGain
	}
	if ax == 0 && ay == 0 {
		return move
	}
	return math.Atan2(ay, ax)
}

// nearestEnemyWithin returns the index of the closest living ship of another
// faction inside reach, or -1 when there is none.
func (w *World) nearestEnemyWithin(i int, reach float64) int {
	s := &w.Ships[i]
	best, bestDist := -1, reach
	for j := range w.Ships {
		o := &w.Ships[j]
		if !o.Alive() || o.Faction == s.Faction {
			continue
		}
		d := math.Hypot(o.X-s.X, o.Y-s.Y)
		if d >= bestDist {
			continue
		}
		best, bestDist = j, d
	}
	return best
}

// avoidEdges blends the desired thrust direction with a push away from whichever
// borders are close, so ships turn away instead of grinding along the screen.
func (w *World) avoidEdges(s *Ship, move float64) float64 {
	ax, ay := math.Cos(move), math.Sin(move)
	if s.X < edgeMargin {
		ax += (edgeMargin - s.X) / edgeMargin
	}
	if s.X > w.Cfg.Width-edgeMargin {
		ax -= (edgeMargin - (w.Cfg.Width - s.X)) / edgeMargin
	}
	if s.Y < edgeMargin {
		ay += (edgeMargin - s.Y) / edgeMargin
	}
	if s.Y > w.Cfg.Height-edgeMargin {
		ay -= (edgeMargin - (w.Cfg.Height - s.Y)) / edgeMargin
	}
	if ax == 0 && ay == 0 {
		return move
	}
	return math.Atan2(ay, ax)
}
