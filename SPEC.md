# rig-looper — Specification

## Purpose

`rig-looper` is a standalone terminal looper application. It records,
overdubs, plays, and clears a single in-memory loop through a full-duplex
audio stream.

## Dependencies

- `rig-core`: audio engine, MIDI engine/monitor
- `rig-components`: dropdowns, lists, lamps, meter

## Architecture

- `cmd/rig`: entrypoint and headless modes (`-watch`, `-audio`, `-loop`)
- `internal/looper`: TUI-agnostic loop state machine and s16 buffer math
- `internal/ui`: Bubble Tea model, view, audio panel, and looper controls
- `core/`: `rig-core` submodule
- `components/`: `rig-components` submodule

## Current features

- MIDI port monitoring
- Audio device selection and live capture meter
- Monitor path (capture to playback)
- Single-channel looper: record, play, overdub, clear
- Headless audio and looper checks

## Roadmap

- MIDI pedal mapping
- Waveform display and position cursor
- Multichannel/track stack
- Tempo-synced loop edges
- Ableton Link clock sync
