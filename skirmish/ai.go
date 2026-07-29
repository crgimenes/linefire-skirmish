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

	target := w.nearestEnemy(i)
	if target < 0 {
		// Nobody left to fight: drift back to the middle and wait for respawns.
		face := math.Atan2(w.Cfg.Height/2-s.Y, w.Cfg.Width/2-s.X)
		return Order{Face: face, Move: face, Thrust: 0.5}
	}

	t := &w.Ships[target]
	dx, dy := t.X-s.X, t.Y-s.Y
	dist := math.Hypot(dx, dy)
	face := math.Atan2(dy, dx)

	// Closing, holding or backing off, depending on the range band. Inside the
	// band the ship strafes sideways, which keeps it a moving target and makes
	// the swarm read as a dogfight instead of two lines meeting head-on.
	move := face // closing in
	switch {
	case dist < standoff*0.7:
		move = face + math.Pi // too close: back off, guns still on the target
	case dist < standoff*1.15:
		move = face + math.Pi/2 // in the band: strafe, alternating side by ID
		if s.ID%2 == 0 {
			move = face - math.Pi/2
		}
	}
	move = w.avoidEdges(s, move)

	aimed := math.Abs(wrapAngle(face-s.Angle)) < fireCone
	return Order{
		Face:   face,
		Move:   move,
		Thrust: 1,
		Fire:   aimed && dist < engageRange,
	}
}

// nearestEnemy returns the index of the closest living ship of another faction,
// or -1 when there is none.
func (w *World) nearestEnemy(i int) int {
	s := &w.Ships[i]
	best, bestDist := -1, math.Inf(1)
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
