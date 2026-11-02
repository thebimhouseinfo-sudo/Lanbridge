package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestRenderPNG(t *testing.T) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M2 2L22 22" /></svg>`
	data, err := renderPNG(svg, "#34C759", 32)
	if err != nil {
		t.Fatalf("render PNG: %v", err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode PNG: %v", err)
	}
	if config.Width != 32 || config.Height != 32 {
		t.Fatalf("PNG size = %dx%d, want 32x32", config.Width, config.Height)
	}
	imageData, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode rendered PNG: %v", err)
	}
	_, green, _, alpha := imageData.At(16, 16).RGBA()
	if alpha == 0 || green == 0 {
		t.Fatal("rendered stroke is transparent or has no green channel")
	}
}

func TestEncodeICO(t *testing.T) {
	sizes := []int{16, 20, 24, 32, 40, 48}
	images := make(map[int][]byte, len(sizes))
	for _, size := range sizes {
		canvas := image.NewRGBA(image.Rect(0, 0, size, size))
		var data bytes.Buffer
		if err := png.Encode(&data, canvas); err != nil {
			t.Fatalf("encode %d px fixture: %v", size, err)
		}
		images[size] = data.Bytes()
	}

	ico, err := encodeICO(images, sizes)
	if err != nil {
		t.Fatalf("encode ICO: %v", err)
	}
	if binary.LittleEndian.Uint16(ico[2:4]) != 1 {
		t.Fatal("ICO type must be 1")
	}
	if count := binary.LittleEndian.Uint16(ico[4:6]); int(count) != len(sizes) {
		t.Fatalf("ICO entry count = %d, want %d", count, len(sizes))
	}
	for i, size := range sizes {
		entry := 6 + i*16
		if int(ico[entry]) != size || int(ico[entry+1]) != size {
			t.Fatalf("ICO entry %d size = %dx%d, want %dx%d", i, ico[entry], ico[entry+1], size, size)
		}
	}
}

func TestEncodeICNS(t *testing.T) {
	images := make(map[int][]byte, len(icnsSizes))
	for size := range icnsSizes {
		var data bytes.Buffer
		if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, size, size))); err != nil {
			t.Fatalf("encode %d px fixture: %v", size, err)
		}
		images[size] = data.Bytes()
	}

	icns, err := encodeICNS(images)
	if err != nil {
		t.Fatalf("encode ICNS: %v", err)
	}
	if string(icns[:4]) != "icns" {
		t.Fatalf("ICNS signature = %q, want icns", icns[:4])
	}
	if size := binary.BigEndian.Uint32(icns[4:8]); int(size) != len(icns) {
		t.Fatalf("ICNS declared size = %d, actual size = %d", size, len(icns))
	}
}

func TestGenerateWritesTrayAndMacAppIcons(t *testing.T) {
	outputDir := t.TempDir()
	if err := generate("../../internal/ui/icons", outputDir); err != nil {
		t.Fatalf("generate icons: %v", err)
	}

	for _, name := range []string{
		"cable-green.png", "cable-green.ico",
		"cable-blue.png", "cable-blue.ico",
		"cable-orange.png", "cable-orange.ico",
		"unplug-gray.png", "unplug-gray.ico",
		"AppIcon.icns",
	} {
		if _, err := os.Stat(filepath.Join(outputDir, name)); err != nil {
			t.Errorf("%s was not generated: %v", name, err)
		}
	}

	appIcon, err := os.ReadFile(filepath.Join(outputDir, "AppIcon.icns"))
	if err != nil {
		t.Fatalf("read generated AppIcon: %v", err)
	}
	if len(appIcon) < 8 || string(appIcon[:4]) != "icns" {
		t.Fatal("generated AppIcon has an invalid ICNS header")
	}
}
