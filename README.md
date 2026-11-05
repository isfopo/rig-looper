# rig-looper

Standalone terminal looper for the rig platform.

`rig-looper` is an independent application. It consumes `rig-core` and
`rig-components` as git submodules and owns its looper-specific logic in
`internal/looper`.

## Submodules

- `core/` -> `https://github.com/isfopo/rig-core.git`
- `components/` -> `https://github.com/isfopo/rig-components.git`

Clone with submodules:

    git clone --recurse-submodules https://github.com/isfopo/rig-looper.git

## Build (host)

Requires Go >= 1.24 and a C compiler for CGO (RtMidi):

    make build       # build ./rig-looper
    make test        # run all tests (root + submodules)
    make vet         # go vet
    make clean       # remove build artifacts

## Containerized development (Linux)

    docker compose build
    docker compose run --rm dev
    docker compose run --rm rig -engine test -demo

## Run

| Command | What it does |
|---|---|
| `.\rig-looper.exe` | TUI on real MIDI ports (RtMidi via CGO) |
| `.\rig-looper.exe -engine test` | TUI on fake loopback ports |
| `.\rig-looper.exe -engine test -demo` | Fake ports plus synthetic events |
| `.\rig-looper.exe -watch 5s` | Headless MIDI monitoring |
| `.\rig-looper.exe -audio 5s` | Headless audio device check |
| `.\rig-looper.exe -audio 5s -loopback` | Capture to playback round trip |
| `.\rig-looper.exe -loop 2s` | Headless looper round-trip check |

## Layout

    cmd/rig/          entrypoint and headless modes
    internal/looper/  loop engine and buffer math
    internal/ui/      terminal UI
    core/             rig-core submodule
    components/       rig-components submodule
    scripts/          container entrypoint and PipeWire configuration

See `NOTES.md` for build and packaging notes.
