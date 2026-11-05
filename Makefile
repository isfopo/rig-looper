# rig build targets
#
# Requires: Go >= 1.24, a C compiler (gcc) for CGO (RtMidi).
# CGO is required by the rtmidi driver. On Linux install build-essential;
# in the dev container it is already present.

BINARY := rig-looper
GO     := go

.PHONY: all build test vet fmt gallery golden-update clean run dev

all: build

## build: compile the rig binary
build:
	CGO_ENABLED=1 $(GO) build -o $(BINARY) ./cmd/rig

## test: run all tests (root + components module)
test:
	$(GO) test ./...
	cd core && $(GO) test ./...
	cd components && $(GO) test ./...

## vet: run go vet
vet:
	$(GO) vet ./...
	cd core && $(GO) vet ./...
	cd components && $(GO) vet ./...

## fmt: format all Go sources
fmt:
	gofmt -w .

## gallery: run the component gallery TUI
gallery:
	cd components && $(GO) run ./examples/gallery

## golden-update: regenerate golden snapshot files
golden-update:
	cd components && $(GO) test ./dropdown/ -run TestDropdownGolden -update

## run: build and run the TUI on real MIDI ports
run: build
	./$(BINARY)

## dev: run the TUI on the test engine with synthetic events
dev:
	$(GO) run ./cmd/rig -engine test -demo

## clean: remove build artifacts
clean:
	rm -f $(BINARY) rig.exe
	cd components && rm -f gallery
