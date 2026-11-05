# rig

A looper with a terminal UI (Bubble Tea), intended to run against a DAW
(Ableton) via Ableton Link and MIDI. Currently past **Milestone 1: MIDI
monitor**, **Milestone 2: audio I/O** (device dropdowns, meter, monitor, and
**Milestone 3's single-channel looper** (`l` record/dub, `x` clear, `-loop`
headless check).

## Containerized development (Linux, recommended for TUI work)

Day-to-day development of the TUI happens in a Linux container — Go lives
where it is happiest, and no per-machine toolchain setup is needed. Each
container run starts a private **PipeWire session** (audio + MIDI) and a
virtual sound rig: playback goes to a null sink and capture records that
sink's monitor, so the audio path has a deterministic loopback with zero host
hardware:

| Command | What it does |
|---|---|
| `docker compose build` | build the `rig-dev` image (Go + gcc + ALSA headers + PipeWire) |
| `docker compose run --rm dev` | interactive shell in the container |
| `docker compose run --rm rig -engine test -demo` | TUI on loopback MIDI ports + synthetic events |
| `docker compose run --rm rig -engine test -demo -watch 3s` | headless MIDI smoke test |
| `docker compose run --rm rig -audio 3s` | audio devices + capture meter (silent) |
| `docker compose run --rm rig -audio 3s -loopback` | capture→playback round trip |
| `docker compose run --rm dev go test ./internal/...` | run the tests |
| `docker compose run --rm dev go vet ./...` | vet |

Notes:

- The repo is bind-mounted at `/workspace`; Go's build and module caches
  live in named volumes (`rig_go-build`, `rig_go-mod`), so a warm
  `docker compose run` is only a few seconds.
- The container has no MIDI hardware: use the `test` engine there (this is
  also why `-demo` exists). `rtmidi` compiles fine and simply lists zero
  ports.
- The PipeWire bootstrap (`scripts/container-entrypoint.sh`) starts
  pipewire, wireplumber and a private D-Bus session, and routes every
  alsa-pipewire plugin client to the virtual rig (`scripts/pipewire/`);
  backend logs land in `/tmp/pipewire.log` inside the container.
- On a **Linux host** with real MIDI/audio hardware, the `midi` profile
  exposes ALSA and leaves capture routing alone:
  `docker compose --profile midi run --rm rig-alsa -engine rtmidi`.
- Containers on Windows/macOS hosts cannot receive MIDI/audio hardware:
  WinMM/CoreMIDI/WASAPI endpoints are not exposed by Docker Desktop.
  Real-hardware testing stays on a Linux host with ALSA.

## Build (host)

Requires Go >= 1.24 and a C compiler (gcc) for CGO (RtMidi):

    make build       # build ./rig
    make test        # run all tests (root + components module)
    make vet         # go vet
    make gallery     # run the component gallery TUI
    make clean       # remove build artifacts

Prefer the containerized workflow below for TUI iteration.

## Run

| Command | What it does |
|---|---|
| `.\rig.exe` | TUI on real MIDI ports (RtMidi via CGO: WinMM/CoreMIDI/ALSA) |
| `.\rig.exe -inline` | TUI inline (no alternate screen; above the prompt) |
| `.\rig.exe -engine test` | TUI on fake loopback ports (no hardware or CGO needed) |
| `.\rig.exe -engine test -demo` | Fake ports plus a synthetic event sender |
| `.\rig.exe -watch 5s` | Headless: print events for 5 seconds (smoke test/debugging) |
| `.\rig.exe -audio 5s` | Headless audio: enumerate WASAPI devices, meter default capture |
| `.\rig.exe -audio 5s -loopback` | + feed capture into playback (feedback risk: mic into speakers!) |
| `.\rig.exe -loop 2s` | Headless looper check: record a round trip, play the loop back, verify levels |

Flags:

- `-engine rtmidi|test` — MIDI driver. `rtmidi` is the default (real ports);
  `test` uses gomidi `testdrv` loopback ports.
- `-demo` — inject synthetic events (test engine only).
- `-watch <duration>` — headless event printing instead of the TUI.
- `-audio <duration>` — headless audio check (milestone 2): lists devices,
  meters the default capture (peak in dBFS). `-loopback` additionally plays
  captured audio straight out of the default output — opt-in because on real
  hardware that is a live monitor/feedback loop.
- `-loop <duration>` — headless looper check (milestone 3): records `<duration>`
  of a synthesized test tone round trip (in the container rig the capture side
  hears the null-sink monitor; on real hardware the mic hears the speakers),
  closes the take, plays the loop back, then verifies the recorded content
  level. Fails on an empty or silent loop.

- `-inline` — run the TUI inline instead of in the alternate screen; handy
  in terminals with altscreen quirks (e.g. Zed's integrated terminal
  bottom-aligns altscreen apps, so the UI floats above the prompt).

## TUI keys

- `q` / `ctrl+c` — quit (`esc` first cancels an open audio dropdown)
- `↑`/`k`, `↓`/`j` — move selection (inside an open dropdown: its entries)
- `enter` / `space` — open/close the selected MIDI port, or confirm the open
  audio dropdown
- `i` / `o` / `c` — audio dropdowns: capture device, playback device,
  channel count (stereo/mono). Selecting re-opens the streams.
- `m` — toggle the monitor path (capture → playback). In the container the
  rig makes this safe; on real hardware mic→speakers is a live feedback loop.
- `r` — refresh lists (ports + audio devices)
- `l` — looper: attach a duplex stream to the selected audio devices, then
  cycle record → play → overdub → play (press again to close a take).
  `x` clears the loop.
- `x` — clear the looper (back to idle; the duplex stream stays attached)

Audio captures start only after you pick a device; the `in` row shows a live
peak meter (dBFS + frames/s) once capture runs.

Incoming events are not scrolled as a log anymore (the `-watch` headless
mode still prints events); in the TUI traffic shows as the counters in the
status line (`events=… clocks=…`) plus the header activity dots.

## Project layout

    cmd/rig/          entrypoint, flags, headless watch/audio/loop modes
    internal/midi/       MIDI Engine (driver owner), Monitor (event stream)
    internal/audio/      audio Engine over miniaudio (capture/playback/duplex/Devices)
    internal/looper/     loop engine: state machine + s16 buffer math (TUI-agnostic)
    internal/ui/         terminal UI: model.go (state/logic), view.go (render),
                         demo.go, audio_panel.go (device dropdowns/meter/monitor/looper)
    scripts/             container entrypoint (PipeWire rig) + its pipewire config
    Dockerfile,          containerized Linux dev environment (see README)
    docker-compose.yml

## Roadmap

1. ✅ MIDI monitor (this milestone)
2. ✅ Audio I/O headless core: enumeration, capture meter, capture→playback round trip
3. ✅ Device/channel selection in the TUI (audio dropdowns + meter, monitor path)
4. ✅ Single-channel looper core: record/overdub/play/clear (duplex stream, TUI
   keys `l`/`x`, headless `-loop` check). Tempo-synced edges land with Ableton
   Link (milestone 8).
5. MIDI pedal mapping (config file: device, note/CC -> action)
6. Waveform display + position cursor on the TUI
7. Multichannel/track stack, loop-in-loop
8. Ableton Link clock sync (UDP protocol, Go binding or port)
9. Release packaging (prebuilt binaries, `-engine rtmidi` embedding)

See `NOTES.md` for build/packaging notes.
