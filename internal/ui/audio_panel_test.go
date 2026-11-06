package ui

import (
	"fmt"
	"testing"

	audiopanel "github.com/isfopo/rig-components/audiopanel"

	"github.com/isfopo/rig-core/audio"
)

// TestDropdownNavigation drives the dropdown state machine without any audio
// backend: default entry first, navigation wraps, pick records the target
// selection and reports whether it actually moved.
func TestDropdownNavigation(t *testing.T) {
	ap := &audioPanel{Panel: &audiopanel.Panel{}}
	ap.Ins = []audio.Device{{Name: "A"}, {Name: "B"}}
	ap.Outs = []audio.Device{{Name: "Out"}}

	inOpts := make([]string, len(ap.InOptions()))
	for i, d := range ap.InOptions() {
		if i == 0 {
			inOpts[i] = "system default"
		} else {
			inOpts[i] = d.Name
		}
	}
	ap.InDrop = ap.InDrop.SetOptions(inOpts)

	outOpts := make([]string, len(ap.OutOptions()))
	for i, d := range ap.OutOptions() {
		if i == 0 {
			outOpts[i] = "system default"
		} else {
			outOpts[i] = d.Name
		}
	}
	ap.OutDrop = ap.OutDrop.SetOptions(outOpts)

	// options list: synthetic "system default" first
	opts := ap.InOptions()
	if len(opts) != 3 || opts[0].Name != "system default" {
		t.Fatalf("inOptions = %+v, want default + 2 entries", opts)
	}
	if deviceIDFor(opts, 0) != nil {
		t.Fatal("default selection must map to nil DeviceID")
	}

	// open in-dropdown: cursor starts on the current selection (default)
	ap.ToggleDropdown(dropIn)
	if ap.OpenWhich() != dropIn || ap.InDrop.Cursor != 0 || ap.DropdownCount() != 3 {
		t.Fatalf("open state: which=%d cursor=%d count=%d", ap.OpenWhich(), ap.InDrop.Cursor, ap.DropdownCount())
	}

	// wrap-around navigation
	ap.MoveCursor(-1)
	if ap.InDrop.Cursor != 2 {
		t.Fatalf("cursor %d, want wrapped to 2", ap.InDrop.Cursor)
	}
	ap.MoveCursor(2)
	if ap.InDrop.Cursor != 1 {
		t.Fatalf("cursor %d, want 1", ap.InDrop.Cursor)
	}

	// pick selects and reports the change
	if !ap.Pick() {
		t.Fatal("pick should report a changed selection")
	}
	if ap.InDrop.Current != 1 || ap.OpenWhich() != dropNone {
		t.Fatalf("after pick: inDrop.Current=%d which=%d", ap.InDrop.Current, ap.OpenWhich())
	}

	// reopening starts on the stored selection; picking it again changes nothing
	ap.ToggleDropdown(dropIn)
	if ap.InDrop.Cursor != 1 {
		t.Fatalf("cursor %d, want restored selection 1", ap.InDrop.Cursor)
	}
	if ap.Pick() {
		t.Fatal("re-picking the same entry should report no change")
	}

	// channels dropdown cycles stereo -> mono
	chanOpts := make([]string, len(channelCounts))
	for i, c := range channelCounts {
		chanOpts[i] = fmt.Sprintf("%d channels", c)
	}
	ap.ChanDrop = ap.ChanDrop.SetOptions(chanOpts)
	ap.ToggleDropdown(dropChan)
	ap.MoveCursor(1)
	if !ap.Pick() || ap.ChanDrop.Current != chanMono {
		t.Fatalf("channels pick: chans=%d", ap.ChanDrop.Current)
	}
	if ap.Channels() != 1 {
		t.Fatalf("channels() = %d, want 1", ap.Channels())
	}

	// out-dropdown with 2 entries: re-picking default reports no change
	ap.ToggleDropdown(dropOut)
	if ap.DropdownCount() != 2 {
		t.Fatalf("out dropdown count = %d, want 2", ap.DropdownCount())
	}
	if ap.Pick() {
		t.Fatal("re-picking default out should report no change")
	}

	if audio.Peak([]int16{3, -5, 2}) != 5 {
		t.Fatal("audio.Peak should give the max magnitude")
	}
}
