# linefire-skirmish

[Linefire](https://github.com/crgimenes/linefire) playing itself over your
desktop.

An indestructible autopilot ship fights an endless horde in a procedurally
generated arena — a fresh one every twenty seconds or so — on a transparent,
undecorated, click-through window covering the monitor. There is nothing to
play: the mouse and keyboard pass straight through to whatever you are actually
doing. It is the game's attract mode, as entertainment.

The game is Linefire's, whole. This binary is only the window: all the code, art
and data come from the linefire module, so what appears over the desktop is
pixel for pixel the game.

The plan from here is to grow the battle itself — factions with assignable
colours, and ship AI written by users in
[Filo](https://github.com/crgimenes/filo), Core Wars style — inside Linefire's
own packages, so both projects stay one game.

## Running

```bash
go run ./cmd/linefire-skirmish
```

| Flag | What it does |
| --- | --- |
| `-sound` | play the arcade attract-mode audio (silent by default) |
| `-debug` | start with the debug HUD up |
| `-version` | print the version and exit |

Quit with Ctrl+C in the terminal that launched it — the window takes no input.

## License

MIT — see [LICENSE](LICENSE).
