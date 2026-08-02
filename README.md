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

## The garage

The workshop, in a real window, for everyone who will never open a terminal:
choose the mode, hand each fleet its program, deploy, and watch the scoreboard.

```bash
go run ./cmd/skirmish-garage
```

It launches the battle in its own process and talks to it over the control
protocol, so a battle can be reconfigured or restaged without closing anything.

## The battle

```bash
go run ./cmd/linefire-skirmish
```

| Flag | What it does |
| --- | --- |
| `-mode` | `endless` (the aquarium, default), `lastfleet` (annihilation) or `timed` |
| `-ships` | fleet size per faction in a real match (default 8) |
| `-duration` | deadline in seconds for `timed` (default 120) |
| `-factions` | teams sharing the arena, 2 to 8 (default 2) |
| `-map` | `arena` (open field) or `maze` (cave labyrinth) |
| `-ai` | comma-separated `.filo` programs, one per faction in order |
| `-ipc` | comma-separated commands; each child process pilots its faction |
| `-window` | an ordinary window instead of the desktop overlay |
| `-sound` | play the arcade attract-mode audio (silent by default) |
| `-debug` | start with the debug HUD up |

The overlay reads no keys — every keystroke belongs to whatever you are doing
underneath. The one clickable thing is a small floating panel with Quit;
Ctrl+C in the launching terminal works too.

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
