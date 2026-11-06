// Package ui implements the rig terminal UI: a Bubble Tea based monitor
// for MIDI and audio devices. It lists MIDI input ports (open one and watch
// events scroll by) and audio endpoints (device/channel dropdowns, capture
// meter, monitor path). Keyboard only, no mouse, no config files yet.
//
// Structure notes (Elm architecture, as used by Bubble Tea):
//
//	Model   - all app state, plain struct
//	Init()  - commands to run at startup
//	Update() - (model, incoming message) -> (model, followup commands)
//	View()  - render current state to a string
//
// Driver details live in internal/midi and internal/audio; this package
// only knows about ports/devices, events, and its own state.
package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	lamps "github.com/isfopo/rig-components/lamps"
	list "github.com/isfopo/rig-components/list"

	"github.com/isfopo/rig-core/midi"
	"github.com/isfopo/rig-looper/internal/looper"
)

// Looper defaults: buffer format follows the attached duplex stream; the
// capacity ceiling silently auto-closes long takes.
const (
	loopSampleRate = 48000 // audio.Options' default rate
	loopMaxSeconds = 30.0  // ≈5.8 MB of s16 stereo
)

// Model holds all state of the monitor.
type Model struct {
	width, height int

	eng      *midi.Engine // owns the MIDi
	ports    []string     // input port names; index == port number
	cursor   int          // selected row in ports (mirrors portList.Cursor)
	portList list.Model   // component for the port list

	monitor *midi.Monitor
	openNum int // port number of the active listener, -1 when closed

	demo    bool // -demo mode: synthetic events on the test engine
	demoCtl *DemoSender

	ap *audioPanel // audio device dropdowns / meter / monitor (always non-nil after New)

	// MIDI activity lamps: receive/send stamps (unix nanos) written via
	// atomics from whichever goroutine observes traffic; View decides bright
	// vs. dark from the elapsed time since the stamp.
	lamps lamps.Model

	events  uint64 // total received
	clocks  uint64 // timing clock / tick, counted separately (often spammy)
	dropped uint64 // dropped by the monitor buffer
	status  string // last status line
	isErr   bool
}

// New returns a fresh monitor model. eng chooses the MIDI driver to work
// with; demo enables the synthetic event sender (test engine only).
//
// The monitor is created here, not in togglePort: Update and togglePort work
// on value copies of Model, so an assignment inside togglePort would never
// reach the model the TUI keeps rendering (a nil-monitor panic on the first
// portOpenedMsg).
func New(eng *midi.Engine, demo bool) Model {
	m := Model{
		eng:      eng,
		monitor:  midi.NewMonitor(eng),
		demo:     demo,
		ap:       newAudioPanel(),
		lamps:    lamps.New([]lamps.Spec{{Name: "in", Color: "42"}, {Name: "out", Color: "205"}}, 0),
		portList: list.New(nil),
		openNum:  -1,
		status:   "ready",
		height:   24,
		width:    80,
	}
	if demo {
		// created here (not in togglePort): togglePort works on value
		// copies, so the sender would never reach the rendered model
		m.demoCtl = NewDemoSender(eng)
		m.demoCtl.OnSend(func() { m.lamps.Light("out") })
	}
	return m
}

// Shutdown stops the demo sender (if any) and the listener, and returns the
// engine for final close by main. Called after Program.Run returns (the
// returned final model is us).
func (m Model) Shutdown() *midi.Engine {
	if m.demoCtl != nil {
		<-m.demoCtl.Stop()
		m.demoCtl = nil
	}
	if m.monitor != nil {
		m.monitor.Stop()
	}
	if m.ap != nil {
		m.ap.Shutdown()
	}
	return m.eng
}

// ---- messages ---------------------------------------------------------

type portsMsg struct {
	ports []string
	err   error
}

type portOpenedMsg struct{ num int }

type portOpenErrMsg struct {
	num int
	err error
}

type portClosedMsg struct{ num int }

type eventMsg struct{ Event midi.Event }

// eventsStoppedMsg signals that the monitor event channel closed
// (stop or reopen).
type eventsStoppedMsg struct{}

// ---- commands ---------------------------------------------------------

func (m Model) portsCmd() tea.Cmd {
	return func() tea.Msg {
		ports, err := m.eng.InputPorts()
		return portsMsg{ports: ports, err: err}
	}
}

func openPortCmd(m *midi.Monitor, num int) tea.Cmd {
	return func() tea.Msg {
		if err := m.Open(num); err != nil {
			return portOpenErrMsg{num: num, err: err}
		}
		return portOpenedMsg{num: num}
	}
}

func closePortCmd(m *midi.Monitor, num int) tea.Cmd {
	return func() tea.Msg {
		m.Stop()
		return portClosedMsg{num: num}
	}
}

// waitForEventCmd keeps the event subscription alive: Update re-issues it
// after every eventMsg received, so exactly one receive sits on the channel
// between renders. When the channel closes (reopen/stop) it reports that.
func waitForEventCmd(ch <-chan midi.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return eventsStoppedMsg{}
		}
		return eventMsg{Event: ev}
	}
}

// ---- Init / Update ----------------------------------------------------

func (m Model) Init() tea.Cmd {
	return m.portsCmd()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {

		case "q", "ctrl+c":
			return m, tea.Quit

		case "esc":
			// esc first cancels an open dropdown, and only quits otherwise
			if m.ap != nil && m.ap.AnyOpen() {
				m.ap.CloseAll()
				return m, nil
			}
			return m, tea.Quit

		case "up", "k":
			if m.ap != nil && m.ap.AnyOpen() {
				m.ap.MoveCursor(-1)
				return m, nil
			}
			if len(m.ports) > 0 {
				m.portList = m.portList.Move(-1)
				m.cursor = m.portList.Cursor
			}
			return m, nil

		case "down", "j":
			if m.ap != nil && m.ap.AnyOpen() {
				m.ap.MoveCursor(1)
				return m, nil
			}
			if len(m.ports) > 0 {
				m.portList = m.portList.Move(1)
				m.cursor = m.portList.Cursor
			}
			return m, nil

		case "enter", "space":
			if m.ap != nil && m.ap.AnyOpen() {
				which := m.ap.OpenWhich()
				changed := m.ap.Pick()
				if changed {
					if which == dropChan && m.ap.looping {
						// the loop buffer's channel layout is fixed at
						// attach time; a change re-creates it (loop lost,
						// and the status line says so)
						m.ap.loop = looper.New(int(m.ap.Channels()),
							loopSampleRate, loopMaxSeconds)
						m.ok("looper re-created (%d ch; loop cleared)",
							int(m.ap.Channels()))
					}
					m.ap.PrepareRestart()
					return m, m.ap.RestartAfterChange()
				}
				return m, nil
			}
			return m, m.togglePort()

		case "i", "o", "c":
			if m.ap == nil {
				return m, nil
			}
			if m.ap.Eng == nil {
				m.fail("audio unavailable: %v", m.ap.InitErr)
				return m, nil
			}
			m.ap.ToggleDropdown(map[string]int{"i": dropIn, "o": dropOut, "c": dropChan}[msg.String()])
			if m.ap.Ins == nil && m.ap.Outs == nil {
				// first audio interaction: enumerate in the background;
				// the dropdown opens immediately and fills on arrival
				return m, m.ap.ListCmd()
			}
			return m, nil

		case "m":
			if m.ap != nil && m.ap.looping {
				m.fail("looper attached (x clears) — monitor needs the free path")
				return m, nil
			}
			return m.toggleMonitor()

		case "l":
			if m.ap == nil {
				return m, nil
			}
			if m.ap.Eng == nil {
				m.fail("audio unavailable: %v", m.ap.InitErr)
				return m, nil
			}
			if !m.ap.looping {
				// attach: the looper takes over the selected endpoints (the
				// monitor path goes off; it needs both ends for itself)
				if m.ap.Monitor {
					m.ap.Monitor = false
					m.ok("audio monitor off (looper takes over)")
				}
				m.ap.StopSimplex()
				if m.ap.loop == nil {
					m.ap.loop = looper.New(int(m.ap.Channels()),
						loopSampleRate, loopMaxSeconds)
				}
				return m, m.ap.LooperRestartCmd()
			}
			switch st := m.ap.loop.Toggle(); st.String() {
			case "rec":
				m.ok("looper: recording (l closes the take)")
			case "play":
				m.ok("looper: playing %.1fs", m.ap.loop.Seconds())
			case "overdub":
				m.ok("looper: overdubbing %.1fs", m.ap.loop.Seconds())
			default:
				m.ok("looper: idle")
			}
			return m, nil

		case "x":
			if m.ap == nil || !m.ap.looping || m.ap.loop == nil {
				return m, nil
			}
			m.ap.loop.Clear()
			m.ok("looper: cleared")
			return m, nil

		case "r":
			cmds := []tea.Cmd{m.portsCmd()}
			if m.ap != nil {
				cmds = append(cmds, m.ap.ListCmd())
			}
			return m, tea.Batch(cmds...)
		}

	case portsMsg:
		if msg.err != nil {
			m.fail("list ports: %v", msg.err)
			return m, nil
		}
		m.ports = msg.ports
		m.portList = m.portList.SetItems(msg.ports)
		if len(m.ports) == 0 || m.portList.Cursor >= len(m.ports) {
			m.portList.Cursor = 0
		}
		m.cursor = m.portList.Cursor
		return m, nil

	case portOpenedMsg:
		m.openNum = msg.num
		m.portList = m.portList.SetActive(msg.num)
		m.ok("listening: %s", m.ports[msg.num])
		cmds := []tea.Cmd{waitForEventCmd(m.monitor.Events())}
		if m.demo && m.demoCtl != nil {
			m.demoCtl.Start()
		}
		return m, tea.Batch(cmds...)

	case portOpenErrMsg:
		m.fail("open port %d: %v", msg.num, msg.err)
		return m, nil

	case portClosedMsg:
		m.openNum = -1
		m.portList = m.portList.SetActive(-1)
		if m.demoCtl != nil {
			<-m.demoCtl.Stop()
			m.demoCtl = nil
		}
		m.ok("closed port %d", msg.num)
		return m, nil

	case eventsStoppedMsg:
		// listener stopped (reopen or close); nothing to re-subscribe until
		// a port is opened again
		return m, nil

	case eventMsg:
		return m.updateEvent(msg.Event)

	case lamps.LampOffMsg:
		upd, _ := m.lamps.Update(msg)
		m.lamps = upd.(lamps.Model)
		return m, nil // repaint: any lamp whose blink window expired goes dark

	// ---- audio panel -----------------------------------------------------

	case audioListMsg:
		if m.ap == nil {
			return m, nil
		}
		m.ap.Ins = msg.Ins
		m.ap.Outs = msg.Outs
		// re-clamp stored selections against the new lists
		if m.ap.InDrop.Current > len(msg.Ins) {
			m.ap.InDrop.Current = 0
		}
		if m.ap.OutDrop.Current > len(msg.Outs) {
			m.ap.OutDrop.Current = 0
		}
		// rebuild device dropdown option lists now that enumeration arrived
		inOpts := make([]string, len(m.ap.InOptions()))
		for i, d := range m.ap.InOptions() {
			if i == 0 {
				inOpts[i] = "system default"
			} else {
				inOpts[i] = d.Name
			}
		}
		m.ap.InDrop = m.ap.InDrop.SetOptions(inOpts)
		outOpts := make([]string, len(m.ap.OutOptions()))
		for i, d := range m.ap.OutOptions() {
			if i == 0 {
				outOpts[i] = "system default"
			} else {
				outOpts[i] = d.Name
			}
		}
		m.ap.OutDrop = m.ap.OutDrop.SetOptions(outOpts)
		m.ok("audio: %d capture / %d playback devices", len(msg.Ins), len(msg.Outs))
		return m, nil

	case audioErrMsg:
		if m.ap == nil {
			return m, nil
		}
		m.fail("audio: %v", msg.Err)
		return m, nil

	case captureStartedMsg:
		if m.ap == nil {
			return m, nil
		}
		m.ap.CapStream = msg.Stream
		m.ap.Capturing = true
		// the monitor gate opens fully only once playback is up too
		m.ap.MonOn.Store(m.ap.Monitor && m.ap.PlayStream != nil)
		m.ok("audio capture: %s (%d ch)", m.ap.InName(), m.ap.Channels())
		return m, meterTickCmd()

	case playbackStartedMsg:
		if m.ap == nil {
			return m, nil
		}
		m.ap.PlayStream = msg.Stream
		m.ap.MonOn.Store(m.ap.Monitor)
		m.ok("audio monitor: %s → %s", m.ap.InName(), m.ap.OutName())
		return m, nil

	case audioMeterMsg:
		if m.ap == nil {
			return m, nil
		}
		m.ap.TakeMeter()
		if m.ap.Metering() {
			return m, meterTickCmd()
		}
		return m, nil

	case loopStartedMsg:
		if m.ap == nil {
			return m, nil
		}
		m.ap.loopStream = msg.Stream
		m.ap.looping = true
		m.ap.Capturing = false
		m.ap.MonOn.Store(false)
		m.ok("looper attached: %s → %s (l rec, x clear)", m.ap.InName(), m.ap.OutName())
		return m, meterTickCmd()
	}

	return m, nil
}

// updateEvent keeps the counters fresh, keeps the subscription alive, and
// lights the in-lamp. (A scrolling event log used to live here; traffic now
// shows as counters plus the activity lamps instead.)
func (m Model) updateEvent(ev midi.Event) (tea.Model, tea.Cmd) {
	m.events++
	m.lamps.Light("in")

	line := ev.String()
	// Clock spam gets its own counter to keep the log useful
	if line == "TimingClock" {
		m.clocks++
		return m, m.lamps.WithLampOff(waitForEventCmd(m.monitor.Events()))
	}
	m.dropped = m.monitor.Dropped()
	return m, m.lamps.WithLampOff(waitForEventCmd(m.monitor.Events()))
}

// togglePort opens the selected port, or closes the active one.
func (m Model) togglePort() tea.Cmd {
	if len(m.ports) == 0 {
		m.fail("no input ports available")
		return nil
	}

	if m.monitor == nil {
		m.monitor = midi.NewMonitor(m.eng)
	}

	num := m.cursor
	if m.openNum == num {
		return closePortCmd(m.monitor, num)
	}
	return openPortCmd(m.monitor, num)
}

// toggleMonitor switches the capture→playback monitor path. Turning it on
// re-opens both ends (device consistency); turning it off stops playback only
// so capture keeps metering.
func (m Model) toggleMonitor() (tea.Model, tea.Cmd) {
	if m.ap == nil {
		return m, nil
	}
	if m.ap.Eng == nil {
		m.fail("audio unavailable: %v", m.ap.InitErr)
		return m, nil
	}
	m.ap.Monitor = !m.ap.Monitor
	if m.ap.Monitor {
		m.ap.MonOn.Store(false)
		m.ap.PrepareRestart()
		m.ok("audio monitor on (beware feedback on real hardware)")
		return m, m.ap.RestartCmd()
	}
	if m.ap.PlayStream != nil {
		m.ap.PlayStream.Stop()
		m.ap.PlayStream = nil
	}
	m.ap.MonOn.Store(false)
	m.ok("audio: monitor off")
	return m, nil
}

// ---- update helpers ---------------------------------------------------

func (m *Model) ok(format string, args ...any) {
	m.status = fmt.Sprintf(format, args...)
	m.isErr = false
}

func (m *Model) fail(format string, args ...any) {
	m.status = fmt.Sprintf(format, args...)
	m.isErr = true
}

// compile-time interface check: Model must implement tea.Model.
var _ tea.Model = Model{}
