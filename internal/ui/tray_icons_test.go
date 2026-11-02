package ui

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"testing"
)

var requiredIconSizes = []int{16, 20, 24, 32, 40, 48}

func TestTrayIconsMatchPlatformAndState(t *testing.T) {
	tests := []struct {
		state TrayState
		name  string
	}{
		{TrayStateDisconnected, "unplug-gray"},
		{TrayStateReady, "cable-green"},
		{TrayStateBusy, "cable-blue"},
		{TrayStateWaiting, "cable-orange"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pngData, ok := iconForState(tt.state, "darwin")
			if !ok {
				t.Fatal("missing PNG tray icon")
			}
			config, err := png.DecodeConfig(bytes.NewReader(pngData))
			if err != nil {
				t.Fatalf("decode PNG tray icon: %v", err)
			}
			if config.Width != 32 || config.Height != 32 {
				t.Fatalf("PNG size = %dx%d, want 32x32", config.Width, config.Height)
			}

			icoData, ok := iconForState(tt.state, "windows")
			if !ok {
				t.Fatal("missing ICO tray icon")
			}
			if len(icoData) < 6 || binary.LittleEndian.Uint16(icoData[2:4]) != 1 {
				t.Fatal("invalid ICO header")
			}
			if count := binary.LittleEndian.Uint16(icoData[4:6]); int(count) != len(requiredIconSizes) {
				t.Fatalf("ICO image count = %d, want %d", count, len(requiredIconSizes))
			}
			for i, size := range requiredIconSizes {
				entry := 6 + i*16
				if int(icoData[entry]) != size || int(icoData[entry+1]) != size {
					t.Fatalf("ICO entry %d size = %dx%d, want %dx%d", i, icoData[entry], icoData[entry+1], size, size)
				}
				dataSize := int(binary.LittleEndian.Uint32(icoData[entry+8 : entry+12]))
				offset := int(binary.LittleEndian.Uint32(icoData[entry+12 : entry+16]))
				if offset < 6+len(requiredIconSizes)*16 || dataSize <= 0 || offset+dataSize > len(icoData) {
					t.Fatalf("ICO entry %d has invalid data bounds", i)
				}
				config, err := png.DecodeConfig(bytes.NewReader(icoData[offset : offset+dataSize]))
				if err != nil {
					t.Fatalf("decode ICO entry %d: %v", i, err)
				}
				if config.Width != size || config.Height != size {
					t.Fatalf("ICO entry %d image = %dx%d, want %dx%d", i, config.Width, config.Height, size, size)
				}
			}
		})
	}
}

func TestIconForStateRejectsUnknownState(t *testing.T) {
	if _, ok := iconForState(TrayState(255), "windows"); ok {
		t.Fatal("unknown tray state returned an icon")
	}
}

func TestSetStateBeforeTrayReady(t *testing.T) {
	manager := NewTrayManager(nil, nil)
	manager.SetState(TrayStateBusy, "busy")

	if manager.currentState != TrayStateBusy {
		t.Fatalf("current state = %d, want %d", manager.currentState, TrayStateBusy)
	}
	if manager.tooltip != "busy" {
		t.Fatalf("tooltip = %q, want %q", manager.tooltip, "busy")
	}
	if manager.ready {
		t.Fatal("tray should not be ready before onReady")
	}
}
