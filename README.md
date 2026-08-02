# linefire-skirmish

A programming game: you write the AI, your fleet does the fighting.

Two or more factions of autonomous ships battle each other in a procedurally
generated arena — over your desktop on a transparent, click-through window, or
in an ordinary one. You never pilot. You write a program, hand it to a faction,
and watch it hold the field or lose it. Robocode, Crobots and RealTimeBattle
are the lineage; [Filo](https://github.com/crgimenes/filo) is the language.

The game is [Linefire](https://github.com/crgimenes/linefire)'s, whole: all the
code, art and data come from the linefire module, so what appears over the
desktop is pixel for pixel the game. This repository is the shell — the window,
the garage and the command line around it.

Everything below is **one executable**. Installing it is copying one file:
there is nothing beside it to find and no assets to ship along. Run it with no
arguments and you get the garage; any flag goes straight to the battle.

## The garage

The workshop, in a real window, for everyone who will never open a terminal:
choose the mode, hand each fleet its program, deploy, and watch the scoreboard.

```bash
go run ./cmd/linefire-skirmish
```

It launches the battle in its own process — the same executable, re-run with
`-control` — and talks to it over the control protocol, so a battle can be
reconfigured or restaged without closing anything.

Sound is a checkbox there, off unless you ask: the overlay is something you
leave running over your desktop while doing something else, and a battle that
starts making noise on its own is a battle you close. Turning it on relaunches
the battle, because a process already flying cannot grow ears. There is no
soundtrack here — the songs belong to Linefire's arcade cabinet; what you get
is the fighting.

A real match (`lastfleet` or `timed`) ENDS: the winning faction is named on
screen in its own colour, and the battle FREEZES where it finished. Another one
is a Restage or a new Deploy away.

Deploy takes about a second to put a battle on screen — a window has to be
created and the view has to settle — so the garage says `launching…` and holds
the row until the battle reports in. There is nothing to click twice.

The garage has a real menu bar, and on macOS that is not decoration: a WebView
has no route to the editing commands without one, so `Cmd+C` and `Cmd+V` would
do nothing in every text field on the page.

### Factions and their code

The fleet list is the battle's cast: **+** adds a faction (its colour is its
identity, exactly as on the battlefield), **×** removes it, and each row
carries its program — *choose* an existing `.filo`, or *edit* to write one in
place. Code against code is the point, and this is the short path to it: add
two factions, edit both, deploy.

### The editor

Each faction's *edit* opens the garage's second room on **that faction's
program**: a real code editor (line numbers, Filo syntax highlighting,
autocompletion over the ship contract, `Cmd+S`), one buffer per faction, with
unsaved work marked with a dot on the fleet list. **Validate** compiles with
the battle's own engine and flies one dry tick — the failing line is marked in
place. **Quick test** fights 20 headless battles against the house brain and
reports the score, in seconds, in-process. The component is
[`glaze/editor`](https://github.com/crgimenes/glaze), shared with other tools.

## The battle

```bash
go run ./cmd/linefire-skirmish -play
```

| Flag | What it does |
| --- | --- |
| `-play` | go straight to the battle (what "no flags" used to mean) |
| `-garage` | open the garage explicitly, e.g. to pass it `-game` |
| `-mode` | `endless` (the aquarium, default), `lastfleet` (annihilation) or `timed` |
| `-ships` | fleet size per faction in a real match (default 8) |
| `-duration` | deadline in seconds for `timed` (default 120) |
| `-factions` | teams sharing the arena, 2 to 8 (default 2) |
| `-map` | `arena` (open field) or `maze` (cave labyrinth) |
| `-ai` | comma-separated `.filo` programs, one per faction in order |
| `-ipc` | comma-separated commands; each child process pilots its faction |
| `-window` | an ordinary window instead of the desktop overlay |
| `-sound` | play the battle's sound effects (silent by default) |
| `-debug` | start with the debug HUD up |

The overlay is inert: it reads no keys and takes no clicks — every keystroke
and every click belongs to whatever you are doing underneath. Starting and
stopping battles is the garage's job; Ctrl+C in the launching terminal quits
one started from there.

## Writing a fleet's AI

One program per faction, instantiated per ship: every hull runs its own copy
with its own memory. [`ai/hunter.filo`](ai/hunter.filo) is a commented tour of
the contract, and the contract itself is documented in linefire's
[`game/pilot.go`](https://github.com/crgimenes/linefire/blob/trunk/game/pilot.go).

```bash
go run ./cmd/linefire-skirmish -ai ai/hunter.filo
```

A program reads its ship's instruments — position, hull, speed, radar,
`fire-ready`, and the contacts its sensors can actually see (inside the radar
and not behind rock) — and answers with orders: `(seek x y)` to navigate around
walls, `(move dx dy)` to steer, `(face deg)` to aim, `(fire x y)` to shoot.
Physics stays the engine's: a program pilots a ship, it does not teleport one.

The battle leaves things on the field, and a hull salvages what it flies over:
repairs, shields, combat mods, and weapons. Programs see them too — `loot`
lists the pickups in sight under the same radar-and-line-of-sight rules as
ships — so a canister is something two fleets can race for, which is where
strategy starts.

Weapons change what `(fire)` means, and `self-weapon` says which one you are
holding. The missile explodes and its blast hurts your own fleet; the laser
burns everything on the firing line; the mine is *laid* where you stand and
waits for anybody but your fleet; the devourer is dropped at your own feet and
drags in every hull nearby, yours included. Salvaging is automatic, so a
wounded ship diving for a repair can come up carrying a black hole — reading
`self-weapon` before you shoot is a real decision, not a formality.

A program that errors is reported once and its ship falls back to the built-in
brain for that tick — a crash makes a dumb ship, never a dead one.

### From another language, or an agent

Any process that reads and writes lines can fly a faction over stdio JSONL —
[`ai/bot.py`](ai/bot.py) is a stdlib-Python example, and the protocol is
linefire's [`game/ipc.go`](https://github.com/crgimenes/linefire/blob/trunk/game/ipc.go).
Orders are standing (they hold until replaced), so a driver that thinks for
seconds — an LLM agent, say — still flies ships that act every tick.

```bash
go run ./cmd/linefire-skirmish -ipc "python3 ai/bot.py"
```

The garage offers the same thing without a terminal: **agent** on a fleet row
turns that faction over to a command you type, next to the factions flying
`.filo` files — one fleet list, two kinds of pilot, battling each other. The
command runs under your shell, so quoting and paths with spaces work; commas
do not, because commas are what separate one faction's command from the next
(put it in a script and call the script). When an agent crashes or closes its
stream its ships fall back to the built-in brain, and the row says so — a
fleet quietly flying differently is exactly what you would otherwise waste an
afternoon debugging.

## Developing an AI

Battles run without a window at full speed, which is how a program is actually
judged: not by watching one fight, but by measuring a hundred.

```bash
go run ./cmd/linefire-skirmish -ai ai/hunter.filo -sim 100
```

```
faction 1 (ai/hunter.filo): 94 wins
faction 2 (house brain): 0 wins
draws: 6
battles: 100, avg 3072 ticks (51.2s of game time each)
```

Battles are deterministic from their seed, and `-trace` records them as JSONL:
every spawn, shot, hit and death, plus a per-second snapshot of each ship.
`-check` audits a recorded trace against the game's invariants — ships inside
the arena, hulls only ever falling, nothing happening to the dead, survivors
reconciling with the scoreboard — and reports ships that stalled with enemies
still alive.

```bash
go run ./cmd/linefire-skirmish -ai ai/hunter.filo -sim 100 -trace battle.jsonl
```

## License

MIT — see [LICENSE](LICENSE).
