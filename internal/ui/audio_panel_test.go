package ui

import (
	"fmt"
	"testing"

	"github.com/isfopo/rig-core/audio"
)

// TestDropdownNavigation drives the dropdown state machine without any audio
// backend: default entry first, navigation wraps, pick records the target
// selection and reports whether it actually moved.
func TestDropdownNavigation(t *testing.T) {
	ap := &audioPanel{}
	ap.ins = []audio.Device{{Name: "A"}, {Name: "B"}}
	ap.outs = []audio.Device{{Name: "Out"}}

	inOpts := make([]string, len(ap.inOptions()))
	for i, d := range ap.inOptions() {
		if i == 0 {
			inOpts[i] = "system default"
		} else {
			inOpts[i] = d.Name
		}
	}
	ap.inDrop = ap.inDrop.SetOptions(inOpts)

	outOpts := make([]string, len(ap.outOptions()))
	for i, d := range ap.outOptions() {
		if i == 0 {
			outOpts[i] = "system default"
		} else {
			outOpts[i] = d.Name
		}
	}
	ap.outDrop = ap.outDrop.SetOptions(outOpts)

	// options list: synthetic "system default" first
	opts := ap.inOptions()
	if len(opts) != 3 || opts[0].Name != "system default" {
		t.Fatalf("inOptions = %+v, want default + 2 entries", opts)
	}
	if deviceIDFor(opts, 0) != nil {
		t.Fatal("default selection must map to nil DeviceID")
	}

	// open in-dropdown: cursor starts on the current selection (default)
	ap.toggleDropdown(dropIn)
	if ap.openWhich() != dropIn || ap.inDrop.Cursor != 0 || ap.dropdownCount() != 3 {
		t.Fatalf("open state: which=%d cursor=%d count=%d", ap.openWhich(), ap.inDrop.Cursor, ap.dropdownCount())
	}

	// wrap-around navigation
	ap.moveCursor(-1)
	if ap.inDrop.Cursor != 2 {
		t.Fatalf("cursor %d, want wrapped to 2", ap.inDrop.Cursor)
	}
	ap.moveCursor(2)
	if ap.inDrop.Cursor != 1 {
		t.Fatalf("cursor %d, want 1", ap.inDrop.Cursor)
	}

	// pick selects and reports the change
	if !ap.pick() {
		t.Fatal("pick should report a changed selection")
	}
	if ap.inDrop.Current != 1 || ap.openWhich() != dropNone {
		t.Fatalf("after pick: inDrop.Current=%d which=%d", ap.inDrop.Current, ap.openWhich())
	}

	// reopening starts on the stored selection; picking it again changes nothing
	ap.toggleDropdown(dropIn)
	if ap.inDrop.Cursor != 1 {
		t.Fatalf("cursor %d, want restored selection 1", ap.inDrop.Cursor)
	}
	if ap.pick() {
		t.Fatal("re-picking the same entry should report no change")
	}

	// channels dropdown cycles stereo -> mono
	chanOpts := make([]string, len(channelCounts))
	for i, c := range channelCounts {
		chanOpts[i] = fmt.Sprintf("%d channels", c)
	}
	ap.chanDrop = ap.chanDrop.SetOptions(chanOpts)
	ap.toggleDropdown(dropChan)
	ap.moveCursor(1)
	if !ap.pick() || ap.chanDrop.Current != chanMono {
		t.Fatalf("channels pick: chans=%d", ap.chanDrop.Current)
	}
	if ap.channels() != 1 {
		t.Fatalf("channels() = %d, want 1", ap.channels())
	}

	// out-dropdown with 2 entries: re-picking default reports no change
	ap.toggleDropdown(dropOut)
	if ap.dropdownCount() != 2 {
		t.Fatalf("out dropdown count = %d, want 2", ap.dropdownCount())
	}
	if ap.pick() {
		t.Fatal("re-picking default out should report no change")
	}

	if audio.Peak([]int16{3, -5, 2}) != 5 {
		t.Fatal("audio.Peak should give the max magnitude")
	}
}


