# linefire-skirmish — the programming game, in ONE executable: no arguments
# opens the garage, any flag goes straight to the battle, and the garage
# launches battles by re-running the same file with -control.
#
# Targets:
#   make build       — build the binary for the current OS/arch into bin/
#   make build-cross — also build for linux/amd64 and windows/amd64
#   make release     — the shipped artefacts: one stamped, compressed file per
#                      platform in dist/, plus SHA256SUMS
#   make icons       — write the application icon out as PNGs to look at
#   make run         — the desktop overlay (the aquarium), i.e. -play
#   make run-window  — the same battle in an ordinary window (what you want while working)
#   make run-ai      — a windowed match with ai/hunter.filo flying faction 1
#   make run-maze    — the same, in a labyrinth
#   make garage      — the garage: configure and watch battles from a real window
#                      (the same as running the binary with no arguments)
#   make sim         — SIM=40 headless battles, scoreboard only
#   make trace       — SIM=40 headless battles + trace + the invariant checker
#   make check       — go fix + go fix -inline + go vet + gofmt + go test
#   make live        — the garage integration test against a real built game
#   make qa          — check + security + cross-build: the full house flow
#   make fmt         — report files that are not gofmt-clean
#   make security    — gosec ./...
#   make tidy        — go mod tidy
#   make clean       — remove bin/ and stray traces
#
# Knobs (any target): SHIPS=6 FACTIONS=4 SIM=40 MAP=maze AI=ai/hunter.filo

export CGO_ENABLED=0
BUILD_FLAGS := -trimpath -ldflags "-s -w"
RUN_FLAGS := -trimpath

CMDS   := $(notdir $(wildcard cmd/*))
GOOS   := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)

VERSION         := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
RELEASE_LDFLAGS := -s -w -X main.version=$(VERSION)
RELEASE_TARGETS := darwin/arm64 darwin/amd64 windows/amd64

# Match knobs. The game's own defaults are deliberately different (2 factions,
# 8 ships); these are the shape a battle is usually watched in while working.
SHIPS    ?= 6
FACTIONS ?= 4
SIM      ?= 40
MAP      ?= arena
AI       ?= ai/hunter.filo
TRACE    ?= battle.jsonl

.PHONY: all build build-cross release dist icons run run-window run-ai run-maze garage sim trace \
        check fix fix-inline vet fmt test live qa security tidy clean

all: build

build:
	@mkdir -p bin
	@for cmd in $(CMDS); do \
	  echo "  -> $$cmd ($(GOOS)-$(GOARCH))"; \
	  go build $(BUILD_FLAGS) -o bin/$$cmd-$(GOOS)-$(GOARCH) ./cmd/$$cmd ; \
	done

build-cross: build
	@for t in linux/amd64 windows/amd64; do \
	  os=$${t%/*}; arch=$${t#*/}; ext=; [ "$$os" = windows ] && ext=.exe; \
	  for cmd in $(CMDS); do \
	    GOOS=$$os GOARCH=$$arch go build $(BUILD_FLAGS) -o bin/$$cmd-$$os-$$arch$$ext ./cmd/$$cmd || exit 1 ; \
	    echo "  -> $$cmd ($$os-$$arch)"; \
	  done ; \
	done

# ONE FILE per platform, and that is the whole distribution: the art, the data,
# the garage's page and the icon all live inside it (go:embed, via the linefire
# module), so installing is copying a file. No app bundle, no assets folder, no
# second binary — the garage launches battles by re-running THIS file.
#
# Linux is missing here on purpose: Ebitengine needs cgo and X11/OpenGL there,
# which a macOS desk cannot cross-compile. It is built natively when the GitHub
# Release is published (.github/workflows/release-linux.yml).
#
# Each binary is compressed on its own rather than tarred with a README: what
# is downloaded should be the program, not an archive to go looking inside.
release: dist
dist:
	@rm -rf dist && mkdir -p dist
	@for t in $(RELEASE_TARGETS); do \
	  os=$${t%/*}; arch=$${t#*/}; ext=; [ "$$os" = windows ] && ext=.exe; \
	  name=linefire-skirmish-$$os-$$arch$$ext; \
	  echo "  -> $$name ($(VERSION))"; \
	  GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(RELEASE_LDFLAGS)" \
	    -o dist/$$name ./cmd/linefire-skirmish || exit 1 ; \
	  gzip -9 dist/$$name || exit 1 ; \
	done
	@cd dist && shasum -a 256 * > SHA256SUMS && cat SHA256SUMS

# The icon, written out as PNGs to look at — the only way to judge one.
icons:
	@mkdir -p dist
	SKIRMISH_ICON_DIR=$$PWD/dist go test -count 1 -run WriteTheIcons ./internal/icon/
	@ls dist/*.png

run:
	go run $(RUN_FLAGS) ./cmd/linefire-skirmish -play

# -window is the one you want while working: the overlay covers the desktop.
run-window:
	go run $(RUN_FLAGS) ./cmd/linefire-skirmish -window -factions $(FACTIONS) -ships $(SHIPS)

run-ai:
	go run $(RUN_FLAGS) ./cmd/linefire-skirmish -window -mode lastfleet \
	  -factions $(FACTIONS) -ships $(SHIPS) -ai $(AI)

run-maze:
	go run $(RUN_FLAGS) ./cmd/linefire-skirmish -window -map maze -mode lastfleet \
	  -factions $(FACTIONS) -ships $(SHIPS) -ai $(AI)

garage:
	go run $(RUN_FLAGS) ./cmd/linefire-skirmish

sim:
	go run $(RUN_FLAGS) ./cmd/linefire-skirmish -sim $(SIM) -map $(MAP) \
	  -factions $(FACTIONS) -ships $(SHIPS) -ai $(AI)

# The deterministic instrument: a battle trace plus the invariant checker. This
# is how a behavior change is judged here — same seeds, count the findings —
# never by watching and forming an impression.
trace:
	go run $(RUN_FLAGS) ./cmd/linefire-skirmish -sim $(SIM) -map $(MAP) \
	  -factions $(FACTIONS) -ships $(SHIPS) -ai $(AI) -trace $(TRACE)

check: fix fix-inline vet fmt test

fix:
	go fix ./...

fix-inline:
	go fix -inline ./...

vet:
	go vet ./...

fmt:
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed:"; gofmt -l .; exit 1)

test:
	go test -timeout 60s -count 1 ./...

# The garage driving the REAL game over the control protocol. Needs a display,
# and skips itself when SKIRMISH_GAME is unset — hence the build first.
live:
	@mkdir -p bin
	go build $(BUILD_FLAGS) -o bin/linefire-skirmish-$(GOOS)-$(GOARCH) ./cmd/linefire-skirmish
	SKIRMISH_GAME=$$PWD/bin/linefire-skirmish-$(GOOS)-$(GOARCH) \
	  go test -timeout 120s -count 1 -run Live ./internal/garage/

qa: check security build-cross

security:
	gosec ./...

tidy:
	go mod tidy

clean:
	rm -rf bin dist
	rm -f $(TRACE)
