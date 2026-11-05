package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	meter "github.com/isfopo/rig-components/meter"

	"github.com/isfopo/rig-looper/internal/looper"
)

// view styles. Kept deliberately plain: faint the chrome, bright the events.
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			MarginRight(2)

	headingStyle = lipgloss.NewStyle().
			Faint(true)

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("212"))

	openMarkStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42"))

	errStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)

	okStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("250"))

	helpStyle = lipgloss.NewStyle().
			Faint(true)
)

func (m Model) View() string {
	var b strings.Builder

	// header -----------------------------------------------------------
	if m.width > 0 {
		title := titleStyle.Render("◉ rig")
		sub := headingStyle.Render("midi + audio monitor")
		left := title + sub
		lamps := m.lampView()
		gap := m.width - lipgloss.Width(left) - lipgloss.Width(lamps)
		if gap < 1 {
			gap = 1
		}
		b.WriteString(left + strings.Repeat(" ", gap) + lamps + "\n\n")
	}

	// ports --------------------------------------------------------------
	b.WriteString(headingStyle.Render("midi (↑/↓ select, ⏎ open/close, r refresh)") + "\n")

	if len(m.ports) == 0 {
		b.WriteString("  " + okStyle.Render("(no input ports — connect your audio interface / pedal)") + "\n")
	} else {
		b.WriteString(m.portList.View())
	}

	// audio panel ---------------------------------------------------------
	m.audioView(&b)

	// fill the remainder so the status bar stays on the bottom row
	padding := m.height - m.reserved()
	if padding > 0 {
		b.WriteString(strings.Repeat("\n", padding))
	}

	b.WriteString(m.statusBar())
	return b.String()
}

// audioLinesOf counts the lines the audio panel occupies, matching exactly
// what audioView renders — reserved must never drift from the drawn output
// or the status bar slides around.
func audioLinesOf(m Model) int {
	if m.ap == nil {
		return 0
	}
	if m.ap.eng == nil {
		return 2 // heading + error line
	}
	n := 1 + 3 // heading + in/out/chan rows
	if m.ap.looping && m.ap.loop != nil {
		n++ // looper status row
	}
	if m.ap.anyOpen() {
		n += m.ap.dropdownCount() + 1 // options + controls hint
	}
	return n
}

// audioView renders the audio panel into b. Rows for device selection are
// the dropdowns proper: pressing the key in front of a row expands the
// option list right below it.
func (m Model) audioView(b *strings.Builder) {
	if m.ap == nil {
		return
	}
	write := func(s string) { b.WriteString(s + "\n") }

	heading := "audio"
	if m.ap.eng == nil {
		write(headingStyle.Render(heading))
		write("  " + errStyle.Render("(unavailable: "+m.ap.initErr.Error()+")"))
		return
	}

	write(headingStyle.Render(heading + " (i in • o out • c channels • m monitor • l looper • x clear)"))

	// in row: selection + live meter
	inRow := "  in : " + m.ap.inName() + "  " + meter.Bar(m.ap.dispPeak, m.ap.dispFrames, m.ap.dispDrops)
	write(m.markRow(dropIn, inRow, m.ap.capturing))

	outRow := "  out: " + m.ap.outName()
	if m.ap.monitor {
		outRow += "   [monitor → on]"
	}
	write(m.markRow(dropOut, outRow, m.ap.playStream != nil))

	chanName := map[int]string{chanStereo: "stereo (2)", chanMono: "mono (1)"}[m.ap.chanDrop.Current]
	write(m.markRow(dropChan, "  chan: "+chanName, false))

	if m.ap.looping && m.ap.loop != nil {
		write(m.loopRow())
	}

	// expanded dropdown: the option list of the focused row
	switch m.ap.openWhich() {
	case dropIn:
		b.WriteString(m.ap.inDrop.View())
	case dropOut:
		b.WriteString(m.ap.outDrop.View())
	case dropChan:
		b.WriteString(m.ap.chanDrop.View())
	default:
		return
	}
	write(helpStyle.Render("  (↑/↓ j/k move • ⏎ select • esc cancel)"))
}

// loopRow renders the looper status line: state (bright while it makes
// sound) plus the take/loop length.
func (m Model) loopRow() string {
	st := m.ap.loop.State()
	stateTxt := lipgloss.NewStyle().
		Foreground(lipgloss.Color("205")).
		Bold(st != looper.Idle).
		Render(st.String())
	return fmt.Sprintf("  loop: %s %5.1fs", stateTxt, m.ap.loop.Seconds())
}

// markRow renders an audio row with its dropdown affordance and activity
// mark. It highlights the row whose dropdown is currently open.
func (m Model) markRow(dropdown int, row string, active bool) string {
	name := row
	if m.ap.openWhich() == dropdown {
		name = "  > " + selectedStyle.Render(strings.TrimPrefix(row, "  "))
	} else if active {
		name += " " + openMarkStyle.Render("•")
	}
	return name
}

func (m Model) dropdownViewStrings(b *strings.Builder, opts []string, cursor int) {
	for i, o := range opts {
		if i == cursor {
			b.WriteString("    > " + selectedStyle.Render(o) + "\n")
			continue
		}
		b.WriteString("      " + o + "\n")
	}
}

// lampView renders the two MIDI activity dots, right-aligned by the caller:
// dark = dark-gray hollow circle (idle), lit = bold filled dot in its color
// for a short window after traffic (in green, out magenta). Plain color
// attributes only — no faint/blink escapes, the least supported on Windows
// consoles.
func (m Model) lampView() string {
	if len(m.lamps.Names()) == 0 {
		return ""
	}
	dark := lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render("○")
	// keep the original leading-space + spacing layout
	inLit := m.lamps.Lit("in")
	outLit := m.lamps.Lit("out")
	render := func(lit bool, color string) string {
		if lit {
			return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Bold(true).Render("●")
		}
		return dark
	}
	return " " + render(inLit, "42") + " " + render(outLit, "205")
}

// reserved computes the number of lines occupied above the status bar:
// title block + midi heading + ports + audio panel + status bar (2 lines).
func (m Model) reserved() int {
	return 2 + 1 + len(m.ports) + audioLinesOf(m) + 2
}

func (m Model) statusBar() string {
	var b strings.Builder

	if m.monitor != nil {
		state, stateStyle := "idle", "250"
		if m.monitor.Active() {
			state = "live"
		}
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(stateStyle)).Bold(state == "live")
		b.WriteString(style.Render("["+state+"]") + " ")
	}

	b.WriteString(okStyle.Render(fmt.Sprintf("events=%d clocks=%d dropped=%d", m.events, m.clocks, m.dropped)))
	b.WriteString("  ")
	if m.ap != nil {
		b.WriteString(m.ap.chip())
	}
	b.WriteString("  ")
	if m.isErr {
		b.WriteString(errStyle.Render(m.status))
	} else {
		b.WriteString(okStyle.Render(m.status))
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("q quit • ↑/k, ↓/j move • enter/space open/select • i,o,c audio dropdowns • m monitor • l/x looper rec/clear • r refresh • esc cancels dropdown • ● midi in/out lamps top right"))

	// NOTE: no trailing newline. The frame must occupy exactly m.height
	// \n-separated rows; a trailing "\n" adds an empty row that overflows
	// the terminal and makes bubbletea's renderer drop rows from the TOP
	// (which silently ate the header line and lamps in every terminal).
	return b.String()
}
