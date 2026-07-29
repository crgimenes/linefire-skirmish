package main

import (
	"fmt"
	"image"
	"image/color"
	"io/fs"
	"math"
	"math/rand/v2"

	"github.com/crgimenes/linefire-skirmish/skirmish"
	"github.com/crgimenes/linefire/asset"
	"github.com/crgimenes/linefire/effects"
	"github.com/crgimenes/linefire/filoio"
	"github.com/crgimenes/linefire/loot"
	"github.com/crgimenes/linefire/render"
	"github.com/crgimenes/linefire/ship"
	"github.com/crgimenes/minigui"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// artScale is linefire's own world-to-pixel factor. Everything drawn here goes
// through it rather than through a factor derived on this side: that is what
// keeps a hull, a ring and a portal the size they are in the game, and it is why
// the sim's ShipRadius is stated in the same terms.
const artScale = render.CamScale

// palette is the stroke and fill one side flies in.
type palette struct{ stroke, fill color.RGBA }

// factionPalette gives every ship its side's colours. Both pairs are linefire's
// own — the player hull's cool pair and the enemy hull's warm one — so the fleet
// keeps the game's look while the colour answers the question that matters in a
// battle: whose side is that one on.
//
// Colouring by TYPE was tried first and read badly. Each hull carries its own
// hue in the art, which is identity in a game where every enemy is an enemy, but
// here it meant a screen of five colours per side and no way to tell the sides
// apart. The type still reads without colour: the silhouettes differ, and so do
// their stroke widths.
var factionPalette = [skirmish.FactionCount]palette{
	{stroke: color.RGBA{0x80, 0xff, 0xff, 0xff}, fill: color.RGBA{0x00, 0x18, 0x20, 0xff}},
	{stroke: color.RGBA{0xff, 0x5a, 0x4a, 0xff}, fill: color.RGBA{0x24, 0x0a, 0x08, 0xff}},
}

// art is one asset drawn: the solid geometry, the emissive copy that feeds the
// bloom, and the two numbers the asset itself carries. Both are read off the
// asset rather than assumed — the hulls are 64x64 around 32,32 but a power-up is
// 48x48 around 24,24, and a hard-coded centre puts it visibly off its own ring.
type art struct {
	body   *render.Mesh
	glow   *render.Mesh
	origin asset.Point
	radius float64 // the asset's collision circle, what anything drawn around it is sized from
}

// draw places the art at a world point, rotated, at linefire's scale.
func (a art) draw(dst *ebiten.Image, x, y, angle float64, emissive bool) {
	mesh := a.body
	if emissive {
		mesh = a.glow
	}
	var geo ebiten.GeoM
	geo.Translate(-a.origin.X, -a.origin.Y)
	geo.Scale(artScale, artScale)
	geo.Rotate(angle)
	geo.Translate(x, y)
	mesh.Draw(dst, geo, true)
}

const (
	// The vortex lives exactly as long as the sim's lead, so it finishes and
	// vanishes on the frame the ship appears. Nothing overlaps: the motes fall in,
	// the portal goes, and the hull is there — which is what materialising looks
	// like, rather than a ship sliding out of a hole that is still open.
	portalFrames    = skirmish.PortalLead
	portalOpenSpeed = 4.0 // how much of its life it spends widening

	// portalRadius starts from linefire's — a 24-unit collision circle with the
	// vortex drawn beyond it — at linefire's scale, opened out further so the
	// convergence has room to be seen.
	portalRadius = (24 + 24) * artScale

	// portalMoteSpeed makes the motes stream in hard: several full trips from rim
	// to centre inside the portal's half second, so the arrival reads as energy
	// being pulled into a point rather than dust drifting inward. One trip was
	// enough to arrive and still looked slack; the package default would not even
	// have finished one.
	portalMoteSpeed = 6.0

	// portalMoteSize is two thirds of linefire's: finer motes on a rim this wide,
	// so the convergence reads as a stream of sparks rather than a ring of dots.
	portalMoteSize = effects.DefaultMoteSize * 2 / 3
)

// pickupRingColor is the accent on the ring around loot — linefire's pickup
// green, so a drop reads as something to collect rather than something to shoot.
var pickupRingColor = color.RGBA{0x80, 0xff, 0xb0, 0xff}

// portal is one vortex the overlay is drawing. It is presentation only: the sim
// announced where a ship is coming out and has no further interest.
type portal struct {
	x, y    float64
	faction skirmish.Faction
	left    int // frames before it closes
}

// overlay is the Ebitengine game: a transparent, click-through window covering
// the monitor, on which the simulation is drawn. It owns nothing about the
// battle itself — that lives in the skirmish package.
type overlay struct {
	world   *skirmish.World
	hulls   [skirmish.FactionCount]map[string]art
	pickups map[string]art
	fx      *effects.Pool
	portals []portal
	glow    *render.Glow
	gui     minigui.Context
	win     minigui.Window
	panel   image.Rectangle
	report  func(*skirmish.World)

	width, height int
	paused        bool
	debug         bool
	passthrough   bool
	quit          bool
}

// errQuit unwinds RunGame when the user closes the overlay from the toolbar.
type errQuit struct{}

func (errQuit) Error() string { return "quit" }

// newOverlay meshes the hulls and prepares the toolbar. content holds the
// embedded gameassets tree.
func newOverlay(content fs.FS, w *skirmish.World, width, height int, debug bool, report func(*skirmish.World)) (*overlay, error) {
	// The effects run off the battle's own seed. They feed nothing back into the
	// simulation, so this is not what makes a run reproducible — it just means the
	// picture replays along with the fight rather than only the fight.
	fxRandom := rand.New(rand.NewPCG(w.Cfg.Seed, ^w.Cfg.Seed)) // #nosec G404 -- cosmetic, and seeded on purpose
	o := &overlay{
		world:  w,
		fx:     effects.New(effects.DefaultMax, fxRandom.Float64),
		glow:   render.NewGlow(),
		width:  width,
		height: height,
		debug:  debug,
		report: report,
		win:    minigui.Window{Title: "Skirmish", X: 8, Y: 8, Open: true, NoClose: true},
	}
	for f := range o.hulls {
		o.hulls[f] = map[string]art{}
	}
	for _, kind := range ship.Kinds() {
		a, err := filoio.LoadAssetFS(content, "gameassets", hullAsset(kind))
		if err != nil {
			return nil, fmt.Errorf("loading hull %q: %w", kind, err)
		}
		for f := range o.hulls {
			layers := paint(a.Layers, factionPalette[f])
			o.hulls[f][kind] = art{
				body:   render.BuildLayersMesh(layers),
				glow:   render.BuildGlowMesh(layers),
				origin: a.Origin,
				radius: a.Radius(),
			}
		}
	}
	// Loot keeps its authored colours: on a ship the colour answers "whose side",
	// but on the ground it answers "what is it", which is what the art already
	// says. Only the refs the drop table can actually produce are loaded.
	o.pickups = map[string]art{}
	for _, ref := range loot.Refs() {
		a, err := filoio.LoadAssetFS(content, "gameassets", ref)
		if err != nil {
			return nil, fmt.Errorf("loading pickup %q: %w", ref, err)
		}
		o.pickups[ref] = art{
			body:   render.BuildLayersMesh(a.Layers),
			glow:   render.BuildGlowMesh(a.Layers),
			origin: a.Origin,
			radius: a.Radius(),
		}
	}

	face, err := minigui.SystemFace(14)
	if err == nil {
		o.gui.SetFace(face)
	}
	return o, nil
}

func (o *overlay) Layout(outsideWidth, outsideHeight int) (int, int) {
	return o.width, o.height
}

func (o *overlay) Update() error {
	if o.quit {
		return errQuit{}
	}

	o.buildToolbar()

	if !o.paused {
		o.world.Step()
		for _, d := range o.world.Deaths() {
			o.fx.Explosion(d.X, d.Y)
		}
		for _, a := range o.world.Arrivals() {
			o.portals = append(o.portals, portal{x: a.X, y: a.Y, faction: a.Faction, left: portalFrames})
		}
		o.stepPortals()
		o.emitExhaust()
		o.fx.Step()
		o.report(o.world)
	}

	// Dynamic passthrough on a window-wide flag: the overlay only grabs the
	// mouse while the cursor is over the toolbar or it is being dragged, so the
	// desktop underneath stays usable at all other times.
	x, y := ebiten.CursorPosition()
	capture := image.Pt(x, y).In(o.panel) || o.gui.Dragging()
	o.setPassthrough(!capture)
	return nil
}

// buildToolbar lays out the always-visible control window for one frame and
// records its rectangle for the cursor-over-panel test in Update.
func (o *overlay) buildToolbar() {
	o.gui.Begin(minigui.InputFromEbiten(), 0, 0)

	if o.gui.BeginWindow(&o.win) {
		o.gui.SetItemWidth(96)
		alive := o.world.Alive()
		o.gui.Label(fmt.Sprintf("%d - %d  kills", o.world.Kills[0], o.world.Kills[1]))
		o.gui.Label(fmt.Sprintf("%d vs %d  alive", alive[0], alive[1]))
		if o.gui.Toggle("pause", "Pause", o.paused) {
			o.paused = !o.paused
		}
		if o.gui.Toggle("debug", "Debug", o.debug) {
			o.debug = !o.debug
		}
		if o.gui.Button("quit", "Quit") {
			o.quit = true
		}
		o.panel = o.gui.EndWindow()
	}

	o.gui.End()
}

// setPassthrough flips the window's click-through state, skipping the call when
// nothing changed: Update runs this every frame and the window API is not free.
func (o *overlay) setPassthrough(enabled bool) {
	if enabled == o.passthrough {
		return
	}
	o.passthrough = enabled
	ebiten.SetWindowMousePassthrough(enabled)
}

// fxView places the particle pool on screen. The arena is the screen, one world
// unit to one pixel, so the camera is the identity and both scales are 1.
func fxView(glow bool) effects.View {
	return effects.View{DPR: 1, PixelScale: 1, Glow: glow}
}

// exhaustCone is how far off the nose a burn can be and still show a plume.
// Unlike linefire's ship, these thrust in a direction independent of where they
// point, and lighting the engines while sliding sideways or backing off looks
// nothing like the game — so only a burn that is roughly forward is drawn, and
// it leaves through the tail.
const exhaustCone = math.Pi / 3

// emitExhaust trails engine exhaust from the tail of every ship accelerating
// forward this tick.
func (o *overlay) emitExhaust() {
	for i := range o.world.Ships {
		s := &o.world.Ships[i]
		if !s.Alive() || s.Thrust <= 0 {
			continue
		}
		if math.Abs(shortestAngle(s.Burn-s.Angle)) > exhaustCone {
			continue
		}
		fx, fy := math.Cos(s.Angle), math.Sin(s.Angle)
		o.fx.Thruster(s.X-fx*skirmish.ShipRadius, s.Y-fy*skirmish.ShipRadius, fx, fy)
	}
}

// shortestAngle normalizes to (-pi, pi], so a difference across the seam is
// small rather than nearly a full turn.
func shortestAngle(a float64) float64 {
	a = math.Mod(a, 2*math.Pi)
	if a > math.Pi {
		return a - 2*math.Pi
	}
	if a <= -math.Pi {
		return a + 2*math.Pi
	}
	return a
}

// stepPortals ages the open vortices and drops the closed ones.
func (o *overlay) stepPortals() {
	kept := o.portals[:0]
	for _, p := range o.portals {
		p.left--
		if p.left <= 0 {
			continue
		}
		kept = append(kept, p)
	}
	o.portals = kept
}

// drawPortals renders each open vortex in its faction's colour, growing as it
// opens and fading as it closes, so a ship is thrown out of something that was
// already there rather than appearing beside a flash.
func (o *overlay) drawPortals(dst *ebiten.Image) {
	seconds := float64(o.world.Tick) / float64(ebiten.TPS())
	for _, p := range o.portals {
		age := 1 - float64(p.left)/portalFrames // 0 when it opens, 1 as it closes
		effects.DrawPortal(dst, effects.Portal{
			X: p.x, Y: p.y,
			Radius:   portalRadius * min(age*portalOpenSpeed, 1),
			Col:      factionPalette[p.faction].stroke,
			Seconds:  seconds,
			Speed:    portalMoteSpeed,
			MoteSize: portalMoteSize,
			HideRing: true,        // a ship materialises; nothing here is a hole to fly into
			Fade:     1 - age*age, // holds open, then goes quickly
			DPR:      1,
			Scale:    1,
		})
	}
}

// drawDrops renders the loot lying in the arena, each under the breathing ring
// linefire uses to say something is worth flying to.
func (o *overlay) drawDrops(dst *ebiten.Image, emissive bool) {
	seconds := float64(o.world.Tick) / float64(ebiten.TPS())
	for i := range o.world.Drops {
		d := &o.world.Drops[i]
		a, ok := o.pickups[d.Ref]
		if !ok {
			continue
		}
		a.draw(dst, d.X, d.Y, 0, emissive)
		if !emissive {
			// The ring is sized off the pickup's own collision circle and swells in
			// world units, exactly as linefire's does.
			effects.DrawPulseRing(dst, d.X, d.Y, a.radius*artScale, pickupRingColor, seconds, artScale, 1)
		}
	}
}

func (o *overlay) Draw(screen *ebiten.Image) {
	// Order is the effect: portals under everything because ships come out of
	// them, then shots and exhaust, then the hulls on top. A muzzle flash or a
	// plume drawn OVER its ship shows where it starts, which reads as a mark
	// stuck to the hull; drawn under, the ship covers the origin and what is left
	// is the part that should be seen.
	o.drawPortals(screen)
	o.drawDrops(screen, false)
	o.drawBullets(screen, false)
	o.fx.Draw(screen, fxView(false))
	o.drawShips(screen, false)

	// The bloom is the whole look: the crisp geometry above, plus an additive
	// halo built from the same shapes drawn into the emissive buffer.
	o.glow.Bloom(screen, screen.Bounds(), render.GlowOptions{
		Intensity:  1.1,
		Spread:     2.5,
		Iterations: 2,
		AntiAlias:  true,
	}, func(emissive *ebiten.Image) {
		o.drawDrops(emissive, true)
		o.drawBullets(emissive, true)
		o.fx.Draw(emissive, fxView(true))
		o.drawShips(emissive, true)
	})

	o.gui.Render(screen)
	if o.debug {
		o.drawDebug(screen)
	}
}

// drawShips renders every living ship, either as solid geometry or as the
// emissive copy that feeds the bloom.
func (o *overlay) drawShips(dst *ebiten.Image, emissive bool) {
	for i := range o.world.Ships {
		s := &o.world.Ships[i]
		if !s.Alive() {
			continue
		}
		// The art points up and angle 0 is +X, so the hull turns a quarter more.
		o.hulls[s.Faction][s.Kind].draw(dst, s.X, s.Y, s.Angle+math.Pi/2, emissive)
	}
}

// hullAsset maps a profile kind to the asset that draws it. The grunt's profile
// is named for the role it plays in linefire, where it is the enemy.
func hullAsset(kind string) string {
	if kind == "enemy" {
		return "enemy"
	}
	return kind
}

// paint returns a copy of the art in one faction's colours. Everything else the
// art carries — stroke width, glow, the paths themselves — is left alone, so the
// hulls are still drawn exactly the way linefire draws them.
func paint(layers []asset.Layer, p palette) []asset.Layer {
	out := make([]asset.Layer, len(layers))
	copy(out, layers)
	for i := range out {
		out[i].Stroke = hex(p.stroke)
		out[i].Fill = hex(p.fill)
	}
	return out
}

func hex(c color.RGBA) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

// drawBullets draws shots as short tracers along their velocity, which reads
// better than a dot at these speeds.
func (o *overlay) drawBullets(dst *ebiten.Image, emissive bool) {
	const tracer = 2.5 // how many ticks of travel the streak represents
	for i := range o.world.Bullets {
		b := &o.world.Bullets[i]
		col := factionPalette[b.Faction].stroke
		width := float32(2)
		if emissive {
			width = 3
		}
		vector.StrokeLine(dst,
			float32(b.X), float32(b.Y),
			float32(b.X-b.VX*tracer), float32(b.Y-b.VY*tracer),
			width, col, true)
	}
}

// drawDebug prints the numbers needed to reproduce and judge a run.
func (o *overlay) drawDebug(screen *ebiten.Image) {
	alive := o.world.Alive()
	msg := fmt.Sprintf("tick %d  fps %.0f  tps %.0f\nalive %v  kills %v  bullets %d\nseed %d  arena %dx%d",
		o.world.Tick, ebiten.ActualFPS(), ebiten.ActualTPS(),
		alive, o.world.Kills, len(o.world.Bullets),
		o.world.Cfg.Seed, o.width, o.height)
	ebitenutil.DebugPrintAt(screen, msg, 8, o.height-56)
}
