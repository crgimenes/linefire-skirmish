# linefire-skirmish

A tiny autonomous desktop battle simulator built with Linefire ships.

Hordes of ships from two factions fight on a transparent, click-through overlay
over your desktop. There is nothing to play: the ships fly themselves, and the
only interface is a small toolbar to pause, inspect or close the battle. The
desktop underneath stays usable — the overlay only grabs the mouse while the
cursor is over the toolbar.

The point of the project is what comes next: the ship AI will be a script you
write, in [Filo](https://github.com/crgimenes/filo), so two people can pit their
programs against each other the way Core Wars does.

## Status

Early. What works today:

- transparent full-monitor overlay with the Linefire vector look (glow, no CRT)
- two factions of autonomous ships, respawning so the fight never ends
- a deterministic simulation: the same `-seed` always produces the same battle
- `-json` and `-ticks` for running and recording a battle headless

Not there yet: Filo scripting, sound, maps, IPC control.

## Running

```bash
go run ./cmd/linefire-skirmish
```

| Flag | What it does |
| --- | --- |
| `-debug` | log to stderr and show tick/FPS/score on screen |
| `-json` | write one JSON snapshot per second to stdout |
| `-ticks N` | run N ticks headless, with no window, and exit |
| `-seed N` | pick the battle; `0` (the default) chooses one at random |
| `-ships N` | ships per faction (default 8) |
| `-version` | print the version and exit |

Every `-json` line carries the seed and the arena size, so a single line is
enough to reproduce the run it came from:

```bash
go run ./cmd/linefire-skirmish -ticks 3600 -json -seed 7 | tail -1
```

## Layout

- `skirmish/` — the simulation. Deterministic, allocation-free per tick, and
  free of any dependency on rendering or the desktop, so it runs headless and
  will later be driven by user scripts or by another process over IPC.
- `cmd/linefire-skirmish/` — the overlay: Ebitengine window, glow rendering and
  the [minigui](https://github.com/crgimenes/minigui) toolbar.

Ship art, the vector renderer and the Filo asset format come from
[linefire](https://github.com/crgimenes/linefire) as imported packages, so
improvements on either side are shared instead of copied.

## License

MIT — see [LICENSE](LICENSE).
