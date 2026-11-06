// Audio device selection for the TUI (milestone 2): dropdowns for capture
// and playback devices, a channel-count choice, a live capture meter, and an
// optional monitor path (capture → playback) to prove the round trip.
//
// The reusable base lives in rig-components/audiopanel; this file keeps the
// looper-specific pieces (duplex stream, loop buffer) on top of it.
package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	audiopanel "github.com/isfopo/rig-components/audiopanel"

	"github.com/isfopo/rig-core/audio"
	"github.com/isfopo/rig-looper/internal/looper"
)

// Aliases keep the rest of the ui package (and its tests) working unchanged.
const (
	dropNone = audiopanel.DropNone
	dropIn   = audiopanel.DropIn
	dropOut  = audiopanel.DropOut
	dropChan = audiopanel.DropChan

	chanStereo = audiopanel.ChanStereo
	chanMono   = audiopanel.ChanMono
)

var channelCounts = audiopanel.ChannelCounts

type audioListMsg = audiopanel.ListMsg
type audioErrMsg = audiopanel.ErrMsg
type captureStartedMsg = audiopanel.CaptureStartedMsg
type playbackStartedMsg = audiopanel.PlaybackStartedMsg
type audioMeterMsg = audiopanel.MeterMsg

type loopStartedMsg struct {
	Stream *audio.Stream
}

// audioPanel wraps the reusable base panel with looper-specific state.
type audioPanel struct {
	*audiopanel.Panel

	loop       *looper.Looper
	looping    bool
	loopStream *audio.Stream
}

func newAudioPanel() *audioPanel {
	return &audioPanel{Panel: audiopanel.New()}
}

// deviceIDFor resolves a dropdown selection to an Options.DeviceID.
func deviceIDFor(opts []audio.Device, sel int) []byte {
	if sel <= 0 || sel >= len(opts) {
		return nil
	}
	return opts[sel].ID()
}

// ---- looper -----------------------------------------------------------

func (ap *audioPanel) LooperRestartCmd() tea.Cmd {
	if ap.Eng == nil {
		return nil
	}
	seq := ap.Seq.Add(1)
	opts := audio.Options{
		DeviceID:     deviceIDFor(ap.InOptions(), ap.InDrop.Current),
		PlayDeviceID: deviceIDFor(ap.OutOptions(), ap.OutDrop.Current),
		Channels:     ap.Channels(),
	}
	return func() tea.Msg {
		stream, err := ap.Eng.OpenDuplex(opts, ap.loopHandle)
		if err != nil {
			return audioErrMsg{Err: fmt.Errorf("looper: %w", err)}
		}
		if ap.Seq.Load() != seq {
			stream.Stop()
			return nil
		}
		return loopStartedMsg{Stream: stream}
	}
}

func (ap *audioPanel) RestartAfterChange() tea.Cmd {
	if ap.looping {
		return ap.LooperRestartCmd()
	}
	return ap.RestartCmd()
}

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

func (ap *audioPanel) StopSimplex() {
	ap.Panel.StopSimplex()
}

func (ap *audioPanel) Metering() bool {
	if ap.Capturing {
		return true
	}
	return ap.looping && ap.loopStream != nil
}

func (ap *audioPanel) TakeMeter() {
	if ap.looping && ap.loop != nil {
		ap.DispPeak, ap.DispFrames = ap.loop.TakeMeter()
		ap.DispDrops = 0
		return
	}
	ap.Panel.TakeMeter()
}

func (ap *audioPanel) Chip() string {
	live := ap.Capturing
	state, color := "audio:off", "250"
	switch {
	case ap.looping && ap.loopStream != nil:
		state, color = "audio:loop", "205"
		live = ap.loop != nil && ap.loop.State() != looper.Idle
	case ap.Monitor && ap.PlayStream != nil:
		state, color = "audio:monitor", "213"
	case ap.Capturing:
		state, color = "audio:live", "42"
	case ap.Eng == nil:
		state = "audio:err"
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(color)).
		Bold(live).
		Render("[" + state + "]")
}

func (ap *audioPanel) PrepareRestart() {
	if ap.loopStream != nil {
		ap.loopStream.Stop()
		ap.loopStream = nil
	}
	ap.Panel.PrepareRestart()
}

func (ap *audioPanel) Shutdown() {
	if ap.loopStream != nil {
		ap.loopStream.Stop()
		ap.loopStream = nil
	}
	ap.Panel.Shutdown()
}

// meterTickCmd keeps the meter subplot refreshing while capture runs.
func meterTickCmd() tea.Cmd {
	return audiopanel.MeterTickCmd()
}
