// Audio device selection for the TUI (milestone 2): dropdowns for capture
// and playback devices, a channel-count choice, a live capture meter, and an
// optional monitor path (capture → playback) to prove the round trip.
//
// Value-copy discipline (same lesson as the MIDI monitor in model.go): the
// panel and all its streams are pointer fields created in New; Update works
// on value copies of Model, so anything assigned while handling a copy would
// silently vanish. Every mutation of stream state happens through the *audio
// Panel pointer, exactly like m.monitor.
//
// Audio threading rules respected here:
//
//   - deliver runs on the audio thread: only atomic writes and a non-binding
//     channel send (drops counted, never block).
//   - stream open/close happens in tea.Cmd goroutines (restart sequences get
//     a monotonic token; a late-arriving open for an outdated configuration
//     stops itself instead of being adopted).
//   - meter reads happen on Update; View only reads the plain display fields
//     filled by Update between renders.
package ui

import (
	"fmt"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	dropdown "github.com/isfopo/rig-components/dropdown"

	"github.com/isfopo/rig-core/audio"
	"github.com/isfopo/rig-looper/internal/looper"
)

// dropdown subjects
const (
	dropNone = iota
	dropIn
	dropOut
	dropChan
)

// channel entries (indices into the channels dropdown)
const (
	chanStereo = 0
	chanMono   = 1
)

// channelCounts are the Options.Channels values for the channels dropdown.
var channelCounts = []uint32{2, 1}

// audioPanel owns everything audio for one Model instance.
type audioPanel struct {
	eng     *audio.Engine // nil when the audio backend failed to initialize
	initErr error

	ins, outs []audio.Device // last enumeration (may be nil until listed)

	// dropdown components (one per subject). At most one has Focus==true.
	inDrop   dropdown.Model
	outDrop  dropdown.Model
	chanDrop dropdown.Model

	seq atomic.Int64 // restart token: only the newest async open is adopted

	capStream *audio.Stream
	capturing bool

	// meter (windows read+reset in Update; only atomics touched on the
	// audio thread)
	winPeak    atomic.Int64 // max magnitude since last read
	winFrames  atomic.Int64 // frames since last read
	dispPeak   int16        // snapshot for View
	dispFrames int64        // snapshot for View
	dispDrops  uint64       // snapshots for View

	// monitor path: captured blocks are pushed to the selected output
	monitor    bool
	monOn      atomic.Bool // gates deliver; true only while playStream runs
	buf        chan []int16
	drops      atomic.Int64
	playStream *audio.Stream

	// looper (milestone 3): while attached it owns ONE duplex stream built
	// from the same device selections; the simplex capture/monitor path is
	// stopped for the duration (shared endpoints, no double streams).
	// looping is only touched on the Update thread. The loop content
	// survives device changes and monitor toggles; 'x' (Clear) empties it.
	loop       *looper.Looper
	looping    bool
	loopStream *audio.Stream
}

// newAudioPanel initializes the engine eagerly (like the MIDI monitor is
// created in New, never in a toggle) and starts with the system default
// devices selected but nothing opened until the user picks a device.
func newAudioPanel() *audioPanel {
	ap := &audioPanel{buf: make(chan []int16, 400)}
	ap.eng, ap.initErr = audio.NewEngine()
	if ap.initErr != nil {
		ap.eng = nil
	}
	// channel dropdown options are static
	opts := make([]string, len(channelCounts))
	for i, c := range channelCounts {
		opts[i] = fmt.Sprintf("%d channels", c)
	}
	ap.chanDrop = dropdown.New(opts, 0)
	// device dropdowns start with just the system-default entry; they are
	// rebuilt when enumeration arrives (see audioListMsg handling in model.go)
	ap.inDrop = dropdown.New([]string{"system default"}, 0)
	ap.outDrop = dropdown.New([]string{"system default"}, 0)
	return ap
}

// ---- options ----------------------------------------------------------

// inOptions returns the capture-device dropdown: "system default" always
// first, then every enumerated capture device.
func (ap *audioPanel) inOptions() []audio.Device {
	o := []audio.Device{{Name: "system default", IsDefault: true}}
	return append(o, ap.ins...)
}

// outOptions returns the playback-device dropdown entries.
func (ap *audioPanel) outOptions() []audio.Device {
	o := []audio.Device{{Name: "system default", IsDefault: true}}
	return append(o, ap.outs...)
}

// deviceID resolves a dropdown selection to an Options.DeviceID (nil keeps
// the system default: do not use the first enumerated device blindly).
func deviceIDFor(opts []audio.Device, sel int) []byte {
	if sel <= 0 || sel >= len(opts) {
		return nil
	}
	return opts[sel].ID()
}

func (ap *audioPanel) inName() string {
	sel := ap.inDrop.Current
	if sel == 0 {
		return "system default"
	}
	if sel <= len(ap.ins) {
		return ap.ins[sel-1].Name
	}
	return "?"
}

func (ap *audioPanel) outName() string {
	sel := ap.outDrop.Current
	if sel == 0 {
		return "system default"
	}
	if sel <= len(ap.outs) {
		return ap.outs[sel-1].Name
	}
	return "?"
}

func (ap *audioPanel) channels() uint32 {
	return channelCounts[ap.chanDrop.Current]
}

// ---- messages ---------------------------------------------------------

type audioListMsg struct {
	ins  []audio.Device
	outs []audio.Device
}

type audioErrMsg struct{ err error }

type captureStartedMsg struct {
	stream *audio.Stream
}

type playbackStartedMsg struct {
	stream *audio.Stream
}

type audioMeterMsg struct{}

// loopStartedMsg adopts a freshly opened looper duplex stream.
type loopStartedMsg struct {
	stream *audio.Stream
}

// ---- commands ---------------------------------------------------------

// listCmd enumerates capture/playback devices (async: WASAPI/ALSA
// enumeration can block for a noticeable while).
func (ap *audioPanel) listCmd() tea.Cmd {
	return func() tea.Msg {
		if ap.eng == nil {
			return audioErrMsg{ap.initErr}
		}
		ins, err := ap.eng.Inputs()
		if err != nil {
			return audioErrMsg{err}
		}
		outs, err := ap.eng.Outputs()
		if err != nil {
			return audioErrMsg{err}
		}
		return audioListMsg{ins: ins, outs: outs}
	}
}

// restartCmd re-opens capture (and playback when the monitor is on) with the
// current dropdown state. Old streams are stopped by prepareRestart before
// this is called. One seq token covers both opens so a newer restart
// invalidates both in-flight opens at once.
func (ap *audioPanel) restartCmd() tea.Cmd {
	if ap.eng == nil {
		return nil
	}
	// snapshot all selection state on the Update thread
	seq := ap.seq.Add(1)
	opts := audio.Options{
		DeviceID: deviceIDFor(ap.inOptions(), ap.inDrop.Current),
		Channels: ap.channels(),
	}
	outID := deviceIDFor(ap.outOptions(), ap.outDrop.Current)
	mon := ap.monitor

	captureCmd := func() tea.Msg {
		stream, err := ap.eng.OpenCapture(opts, ap.deliver)
		if err != nil {
			return audioErrMsg{err}
		}
		if ap.seq.Load() != seq { // a newer restart won: abort
			stream.Stop()
			return nil
		}
		return captureStartedMsg{stream: stream}
	}

	if !mon {
		return captureCmd
	}

	playbackCmd := func() tea.Msg {
		stream, err := ap.eng.OpenPlayback(audio.Options{
			DeviceID: outID,
			Channels: opts.Channels,
		}, ap.pull)
		if err != nil {
			return audioErrMsg{err}
		}
		if ap.seq.Load() != seq {
			stream.Stop()
			return nil
		}
		return playbackStartedMsg{stream: stream}
	}
	return tea.Batch(captureCmd, playbackCmd)
}

// ---- looper -----------------------------------------------------------

// looperRestartCmd opens the duplex stream the looper runs on, using the
// current device selections (in the same seq-token pattern as restartCmd:
// a newer restart invalidates in-flight opens). prepareRestart must have
// stopped every other stream first — shared endpoints stay single-tenant.
func (ap *audioPanel) looperRestartCmd() tea.Cmd {
	if ap.eng == nil {
		return nil
	}
	seq := ap.seq.Add(1)
	opts := audio.Options{
		DeviceID:     deviceIDFor(ap.inOptions(), ap.inDrop.Current),
		PlayDeviceID: deviceIDFor(ap.outOptions(), ap.outDrop.Current),
		Channels:     ap.channels(),
	}
	return func() tea.Msg {
		stream, err := ap.eng.OpenDuplex(opts, ap.loopHandle)
		if err != nil {
			return audioErrMsg{fmt.Errorf("looper: %w", err)}
		}
		if ap.seq.Load() != seq { // a newer restart won: abort
			stream.Stop()
			return nil
		}
		return loopStartedMsg{stream: stream}
	}
}

// restartAfterChange re-opens whatever the panel was running before a
// device/channel change: the looper's duplex stream stays the owner.
func (ap *audioPanel) restartAfterChange() tea.Cmd {
	if ap.looping {
		return ap.looperRestartCmd()
	}
	return ap.restartCmd()
}

// loopHandle is the duplex callback (audio thread): playback follows the
// looper, capture feeds it and the meter pass-through. Playback aliases the
// looper's scratch buffer, so the copy into the device buffer is required.
func (ap *audioPanel) loopHandle(out, in []int16) {
	if out != nil {
		if data := ap.loop.Playback(len(out)); data != nil {
			copy(out, data)
		}
	}
	if in != nil {
		ap.loop.Capture(in)
	}
}

// stopSimplex halts the panel's capture/monitor streams (the looper takes
// over the endpoints; meter numbers come from the looper's pass-through).
func (ap *audioPanel) stopSimplex() {
	if ap.capStream != nil {
		ap.capStream.Stop()
		ap.capStream = nil
	}
	if ap.playStream != nil {
		ap.playStream.Stop()
		ap.playStream = nil
	}
	ap.monOn.Store(false)
	ap.capturing = false
	ap.drainBuf()
}

// metering reports whether a live meter exists (simplex capture or looper).
func (ap *audioPanel) metering() bool {
	if ap.capturing {
		return true
	}
	return ap.looping && ap.loopStream != nil
}

// ---- audio thread side -------------------------------------------------

// deliver is the capture callback: meters the block, and forwards a copy to
// the monitor buffer when the monitor path is live.
func (ap *audioPanel) deliver(in []int16) {
	n := len(in) / 2
	ap.winFrames.Add(int64(n))
	peak := audio.Peak(in)
	for {
		old := ap.winPeak.Load()
		if int64(peak) <= old || ap.winPeak.CompareAndSwap(old, int64(peak)) {
			break
		}
	}

	if !ap.monOn.Load() {
		return
	}
	block := make([]int16, len(in)) // fresh copy: capture buffer is reused
	copy(block, in)
	select {
	case ap.buf <- block:
	default:
		ap.drops.Add(1)
	}
}

// pull is the playback callback (drains the monitor buffer).
func (ap *audioPanel) pull(n int) []int16 {
	select {
	case b := <-ap.buf:
		return b
	default:
		return nil // capture not warmed up (or too slow): silence
	}
}

// shutdown stops streams and closes the engine (Model.Shutdown calls this).
func (ap *audioPanel) shutdown() {
	if ap.capStream != nil {
		ap.capStream.Stop()
		ap.capStream = nil
	}
	if ap.playStream != nil {
		ap.playStream.Stop()
		ap.playStream = nil
	}
	if ap.loopStream != nil {
		ap.loopStream.Stop()
		ap.loopStream = nil
	}
	ap.monOn.Store(false)
	if ap.eng != nil {
		ap.eng.Close()
	}
}

// chip renders the compact audio state for the status bar.
func (ap *audioPanel) chip() string {
	live := ap.capturing
	state, color := "audio:off", "250"
	switch {
	case ap.looping && ap.loopStream != nil:
		state, color = "audio:loop", "205"
		live = ap.loop != nil && ap.loop.State() != looper.Idle
	case ap.monitor && ap.playStream != nil:
		state, color = "audio:monitor", "213"
	case ap.capturing:
		state, color = "audio:live", "42"
	case ap.eng == nil:
		state = "audio:err"
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(color)).
		Bold(live).
		Render("[" + state + "]")
}

// ---- dropdown state helpers -------------------------------------------

// prepareRestart stops running streams and flushes the monitor buffer so
// restartCmd comes after a clean slate. The looper's stream is stopped with
// everything else; restartAfterChange re-opens it attached (loop content
// survives: only the *audio.Stream* churns).
func (ap *audioPanel) prepareRestart() {
	if ap.capStream != nil {
		ap.capStream.Stop()
		ap.capStream = nil
	}
	if ap.playStream != nil {
		ap.playStream.Stop()
		ap.playStream = nil
	}
	if ap.loopStream != nil {
		ap.loopStream.Stop()
		ap.loopStream = nil
	}
	ap.monOn.Store(false)
	ap.drainBuf()
}

func (ap *audioPanel) drainBuf() {
	for {
		select {
		case <-ap.buf:
		default:
			return
		}
	}
}

// openWhich returns dropIn/dropOut/dropChan for the currently open dropdown,
// or dropNone when none is open.
func (ap *audioPanel) openWhich() int {
	switch {
	case ap.inDrop.Focus:
		return dropIn
	case ap.outDrop.Focus:
		return dropOut
	case ap.chanDrop.Focus:
		return dropChan
	}
	return dropNone
}

// anyOpen reports whether any dropdown is currently open.
func (ap *audioPanel) anyOpen() bool { return ap.openWhich() != dropNone }

// closeAll closes every dropdown.
func (ap *audioPanel) closeAll() {
	ap.inDrop = ap.inDrop.Close()
	ap.outDrop = ap.outDrop.Close()
	ap.chanDrop = ap.chanDrop.Close()
}

// toggleDropdown opens the focused dropdown (or closes it if already open).
func (ap *audioPanel) toggleDropdown(which int) {
	if ap.openWhich() == which {
		ap.closeAll()
		return
	}
	ap.closeAll()
	switch which {
	case dropIn:
		opts := make([]string, len(ap.inOptions()))
		for i, d := range ap.inOptions() {
			if i == 0 {
				opts[i] = "system default"
			} else {
				opts[i] = d.Name
			}
		}
		ap.inDrop = ap.inDrop.SetOptions(opts)
		ap.inDrop = ap.inDrop.Open()
	case dropOut:
		opts := make([]string, len(ap.outOptions()))
		for i, d := range ap.outOptions() {
			if i == 0 {
				opts[i] = "system default"
			} else {
				opts[i] = d.Name
			}
		}
		ap.outDrop = ap.outDrop.SetOptions(opts)
		ap.outDrop = ap.outDrop.Open()
	case dropChan:
		ap.chanDrop = ap.chanDrop.Open()
	}
}

// moveCursor moves the cursor inside the open dropdown with wrap-around.
func (ap *audioPanel) moveCursor(delta int) {
	switch ap.openWhich() {
	case dropIn:
		ap.inDrop = ap.inDrop.Move(delta)
	case dropOut:
		ap.outDrop = ap.outDrop.Move(delta)
	case dropChan:
		ap.chanDrop = ap.chanDrop.Move(delta)
	}
}

// dropdownCount is the number of entries of the currently open dropdown.
func (ap *audioPanel) dropdownCount() int {
	switch ap.openWhich() {
	case dropIn:
		return ap.inDrop.Count()
	case dropOut:
		return ap.outDrop.Count()
	case dropChan:
		return ap.chanDrop.Count()
	}
	return 0
}

// pick selects the entry under the cursor; the caller restarts the streams.
// Returns true when the selection actually moved (restart only then).
func (ap *audioPanel) pick() bool {
	which := ap.openWhich()
	if which == dropNone {
		return false
	}
	var changed bool
	switch which {
	case dropIn:
		nm, ch := ap.inDrop.Pick()
		ap.inDrop = nm
		changed = ch
	case dropOut:
		nm, ch := ap.outDrop.Pick()
		ap.outDrop = nm
		changed = ch
	case dropChan:
		nm, ch := ap.chanDrop.Pick()
		ap.chanDrop = nm
		changed = ch
	}
	return changed
}

// takeMeter snapshots the metering window into the plain display fields that
// View reads (Update runs between renders, so this stays race-free). While
// the looper owns capture, its pass-through is the source instead.
func (ap *audioPanel) takeMeter() {
	if ap.looping && ap.loop != nil {
		ap.dispPeak, ap.dispFrames = ap.loop.TakeMeter()
		ap.dispDrops = 0
		return
	}
	ap.dispPeak = int16(ap.winPeak.Swap(0))
	ap.dispFrames = ap.winFrames.Swap(0)
	ap.dispDrops = uint64(ap.drops.Load())
}

// meterTickCmd keeps the meter subplot refreshing while capture runs.
func meterTickCmd() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg {
		return audioMeterMsg{}
	})
}
