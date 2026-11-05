# Modular TUI Music Production Platform — Spec (draft v2)

> Status: design draft. Revised after feedback: standalone apps, system-level
> routing (PipeWire/MIDI), internal tempo, multi-repo via git submodules.
> Evolves from `rig` (single looper TUI).

## 1. Vision

A family of **standalone, lightweight TUI applications** for music production,
each focused on one job (looper, meter, MIDI monitor, sequencer, mixer, ...),
that **interoperate** through system-level routing rather than a monolithic host:

- Audio flows between apps via **PipeWire** (or JACK) — each app is an
  audio client with named ports; the user patches them in `pw-link` /
  `qpwgraph` / Helvum, or via config.
- MIDI flows via the **system MIDI bus** (ALSA MIDI / PipeWire MIDI).
- Tempo/timeline is **internal** to each app that needs it; sync between
  apps is an optional later layer (MIDI clock, then Ableton Link).

Inspired by Ableton Live and modular synths, but broken into Unix-philosophy
pieces: small binaries that do one thing and compose via standard buses.
Tiling/multiplexing is the user's job (tmux, zellij, a terminal
multiplexer) — **out of scope** for this platform.

## 2. Principles

- **Standalone first**: every module runs on its own as a normal binary.
- **Compose via buses, not hosts**: audio/MIDI/sync happen at the OS level
  (PipeWire, ALSA, UDP), not in-process.
- **Shared core, many repos**: a shared `core` library + `components`
  library live in their own repos; each module repo consumes them via git
  submodules (or Go modules) and builds a standalone binary.
- **Internal tempo**: each app owns its clock/timeline; external sync is an
  adapter, not the foundation.
- **Text-native**: TUI first; wireframes are plain text.
- **Incremental**: build on what exists (`rig`'s audio/midi/looper engines
  and the `components/` library).

## 3. Repository layout

```
rig-core/          (shared library repo)
  audio/              audio engine, devices, capture/playback/duplex, meter
  midi/               midi engine (rtmidi/test), monitor, router
  tempo/              internal clock, timeline, beat/bar, (later) sync adapters
  config/             config loading (TOML)
  components/         (or separate repo) TUI primitives + domain components

rig-bus/           optional master-bus app (audio routing matrix, MIDI routing, global gain/pan mixer, meter) — only needed for 2+ apps
rig-looper/        standalone app repo (submodules core + components)
rig-meter/         standalone app repo
rig-midimon/       standalone app repo
rig-seq/           standalone app repo
...
```

- `core` and `components` are consumed via **git submodules** (pinned
  commits) or, alternatively, as Go modules with versioned tags. Submodules
  give offline builds and exact reproducibility; Go modules give easier
  updates. Recommend submodules for the "platform" feel, modules for releases.
- Each app repo is small: `main.go` + module-specific logic + config.

## 4. Standalone module (app) model

Each app is a normal Bubble Tea program:

- Owns its own `tea.Model`, state, config.
- Connects to **PipeWire** for audio (named input/output ports) and to the
  **system MIDI bus** for MIDI.
- Optionally exposes a **tempo** (if it's a clock master) or follows one
  (if a clock slave) via the sync layer.
- Declares its **audio/MIDI port names** so users can patch them.

There is no host, no module interface, no registry. Composition is by
patching buses at the OS level (and by config files that record the patch).

## 5. Shared services (in `core`)

### 5.1 Audio (`core/audio`) — exists in `internal/audio`
- Engine, device enumeration, capture/playback/duplex streams, meter helper.
- Each app opens its own streams with chosen device + channels.
- **Named ports**: when running under PipeWire, name streams so they show up
  as patchable nodes (`rig-looper:out`, `rig-meter:in`).
- Per-app gain/mute is the app's own concern (or a tiny mixer utility).

### 5.2 MIDI (`core/midi`) — exists in `internal/midi`
- Engine (rtmidi/test), monitor.
- **MIDI router helper**: subscribe to incoming events by port/filter; publish
  outgoing events to a virtual out port.
- **MIDI mapping table** (config-driven): `(device, ch, note/CC)` ->
  `(action, value)`. Reusable by any app that wants MIDI control.

### 5.3 Tempo (`core/tempo`) — new
- **Internal clock**: BPM, beat, bar, phase, start/stop/continue. Every app
  that needs timing owns its own clock; tempo is never required.
- **Timeline**: optional bar/beat grid for sequencers/loopers; apps can snap
  edges to the grid.
- **Sync adapters** (later, optional):
  - MIDI clock (24 ppqn) in/out for hardware.
  - Ableton Link (UDP) for wireless sync with Live and other Link peers.
- **Master bus** (optional hub app): when two or more apps run together, a
  `rig-bus` app can act as clock master and route audio/MIDI between them.
  Apps run standalone fine without it; sync is opt-in.
- Apps that don't need tempo simply ignore it. Tempo is never required.

### 5.4 Config (`core/config`) — new
- Shared TOML/JSON loading, path resolution (`~/.config/rig/<app>.toml`),
  validation helpers. Each app defines its own schema on top.

### 5.5 Components (`components/`) — exists
- Generic TUI primitives (dropdown, list, lamps, meter, demo harness, gallery).
- Domain components (audio panel, etc.) that any app can embed.

## 6. Interoperability mechanisms

| Concern   | Mechanism                                      | Status     |
|-----------|------------------------------------------------|------------|
| Audio     | PipeWire/JACK named ports + user patching       | use now    |
| MIDI      | System MIDI bus (ALSA/PipeWire MIDI)            | use now    |
| Tempo     | Internal clock in each app                     | build now  |
| Sync      | MIDI clock in/out, then Ableton Link           | later      |
| Config    | Shared `core/config` + per-app schema          | build now  |

No in-process graph, no host, no IPC protocol beyond what the OS buses
already provide. This keeps apps truly standalone and debuggable.

## 7. Wireframes (standalone apps)

### 7.1 Looper (standalone, with tempo)

```
+--------------------------------------------------+
| rig-looper   [tempo 120.0 master] [●in ●out]  |
+--------------------------------------------------+
|  in : system default  [####----] -12dBFS 1.2kf/s |
|  out: system default                             |
+--------------------------------------------------+
|  loop: play  4.2s   (snapped to beat 3)          |
|  [l rec/dub] [x clear] [m monitor]               |
+--------------------------------------------------+
| status: ready   audio:loop   q quit ...          |
+--------------------------------------------------+
```

### 7.2 Meter (standalone)

```
+--------------------------------------------------+
| rig-meter                                     |
+--------------------------------------------------+
|  in : system default                             |
|  [########--] -6dBFS   2.1kf/s                  |
+--------------------------------------------------+
|  q quit                                          |
+--------------------------------------------------+
```

### 7.3 MIDI monitor (standalone)

```
+--------------------------------------------------+
| rig-midimon  events=12 clocks=4 dropped=0     |
+--------------------------------------------------+
|  midi in ports:                                  |
|  > Launchpad  •                                  |
|    ...                                           |
+--------------------------------------------------+
|  q quit ...                                      |
+--------------------------------------------------+
```

### 7.4 Master bus (`rig-bus`) — optional, only for 2+ apps

```
+--------------------------------------------------+
| rig-bus   [tempo 120.0 master] [●in ●out]       |
+--------------------------------------------------+
|  AUDIO ROUTING MATRIX                            |
|        src: looper  meter  seq                   |
|  dst                                               |
|  out1   [x]     [ ]    [ ]                       |
|  out2   [ ]     [x]    [x]                       |
|  meter  [x]     [x]    [x]                       |
+--------------------------------------------------+
|  MIDI ROUTING                                    |
|  Launchpad -> looper:record, mixer:volume         |
+--------------------------------------------------+
|  MIXER (gain/pan per channel)   [master gain]    |
+--------------------------------------------------+
|  q quit ...                                      |
+--------------------------------------------------+
```

### 7.5 Patching two apps together (user's tiler)

```
Terminal window 1:  rig-looper   (audio out -> PipeWire)
Terminal window 2:  rig-meter    (audio in  <- PipeWire, patched to looper:out)
Terminal window 3:  rig-midimon  (midi in <- Launchpad)
```

The user arranges these in tmux/zellij however they like. No in-app tiling.

## 8. Configuration

Per-app config, e.g. `~/.config/rig/looper.toml`:

```toml
[audio]
capture = "system default"
playback = "system default"
channels = 2

[midi]
in_ports = ["Launchpad"]
mappings = [
  { device = "Launchpad", channel = 1, type = "note", number = 36, action = "record" },
]

[tempo]
role = "master"   # master | slave | none
bpm = 120.0
```

## 9. Roadmap (phased)

1. **Promote `internal/*` to `core/`** (audio, midi, looper) as a reusable
   library; keep `rig` working. (S)
2. **Tempo package** (`core/tempo`): internal clock + timeline + beat snap. (M)
3. **Config package** (`core/config`): shared TOML loading. (S)
4. **Named audio/MIDI ports** so apps are patchable in PipeWire. (S)
5. **MIDI mapping table** (config-driven, reusable). (M)
6. **Split into repos**: `rig-core`, `rig-components`, `rig-looper`,
   `rig-meter`, `rig-midimon` with git submodules. (M)
7. **New apps**: sequencer, simple synth, mixer, effect. (L each)
8. **Sync adapters**: MIDI clock in/out, then Ableton Link. (M-L)

Effort: S/M/L.

## 10. Open questions

1. **Submodules vs. Go modules** for sharing core/components: submodules for
   dev reproducibility, Go modules for releases. Recommend starting with
   submodules and tagging releases later.
2. **PipeWire naming**: confirm stream names show up as patchable nodes in
   the container rig (test with `pw-link -l`).
3. **Tempo ownership**: no dedicated clock app (breaks standalone goal).
   Instead a `rig-bus` master-bus app handles inter-app routing + can be clock
   master; apps run solo with internal tempo.
4. **Config format**: TOML (lean yes).
5. **First app to split out**: meter (simplest, proves the infra + DX). Then
   looper (most complex).

## 11. What exists today (reuse)

- `components/` — dropdown, list, lamps, meter, demo harness, gallery, golden test
- `internal/audio` — engine, devices, capture/playback/duplex, meter
- `internal/midi` — engine (rtmidi/test), monitor
- `internal/looper` — state machine + buffer math
- `internal/ui` — Model/view (to be decomposed into standalone apps)
- Containerized dev workflow (PipeWire rig) — keep for audio testing
- `Makefile`, `SPEC.md`
