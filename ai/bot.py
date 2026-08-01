#!/usr/bin/env python3
"""A first external pilot, and a tour of the IPC protocol.

Run it as faction 1 against the house brain:

    go run ./cmd/linefire-skirmish -ipc "python3 ai/bot.py"

The engine writes one JSON line at ~10 Hz on stdin with the faction's ships
and everything their sensors see (radar plus line of sight — rock hides what
is behind it). This bot answers on stdout with STANDING orders: each holds
until replaced, so the ships keep flying between our answers — which is what
lets a slow driver (even an LLM taking seconds to think) fly ships that act
every tick. The full protocol is documented in linefire's game/ipc.go.
"""
import json
import sys

CORNER_MARGIN = 200
ENGAGE_RANGE = 240

corner_of = {}  # ship id -> which corner of its patrol tour it is heading to


def corner(i, w, h):
    """The four-corner tour: TL, BL, BR, TR."""
    xs = (CORNER_MARGIN, CORNER_MARGIN, w - CORNER_MARGIN, w - CORNER_MARGIN)
    ys = (CORNER_MARGIN, h - CORNER_MARGIN, h - CORNER_MARGIN, CORNER_MARGIN)
    return xs[i % 4], ys[i % 4]


def orders_for(ship, w, h):
    """One ship's standing order: fight the nearest contact, else patrol."""
    sid = ship["id"]
    enemies = ship["enemies"]
    if enemies:
        near = enemies[0]  # the engine sorts contacts nearest first
        tx, ty = near["x"], near["y"]
        if near["dist"] > ENGAGE_RANGE:
            return {"id": sid, "seek": [tx, ty], "fire": [tx, ty]}
        # Close enough: strafe sideways and keep shooting.
        dx, dy = tx - ship["x"], ty - ship["y"]
        return {"id": sid, "move": [-dy, dx], "fire": [tx, ty]}

    # Nobody in sight: walk the corner tour (seek rides the engine's
    # pathfinding, so a maze is walked, not pressed into).
    i = corner_of.setdefault(sid, sid % 4)
    cx, cy = corner(i, w, h)
    if abs(ship["x"] - cx) + abs(ship["y"] - cy) < 150:
        corner_of[sid] = i + 1
        cx, cy = corner(i + 1, w, h)
    return {"id": sid, "seek": [cx, cy]}


for line in sys.stdin:
    state = json.loads(line)
    w, h = state["field"]["w"], state["field"]["h"]
    orders = [orders_for(s, w, h) for s in state["ships"]]
    if orders:
        print(json.dumps({"orders": orders}), flush=True)
