# rig-looper — Notes

## Build and run

Requires Go >= 1.24 and a C compiler for CGO (RtMidi/miniaudio).

    make build
    make test
    make vet

The binary is `rig-looper` (`rig-looper.exe` on Windows).

## Container development

Day-to-day TUI development uses the Docker/PipeWire rig:

    docker compose build
    docker compose run --rm dev
    docker compose run --rm rig -engine test -demo

See `scripts/container-entrypoint.sh` and `scripts/pipewire/` for the
PipeWire bootstrap and virtual sink configuration.

## Current status

Single-channel looper is implemented and covered by the headless `-loop`
check. Tempo-synced edges and multichannel support are future work.
