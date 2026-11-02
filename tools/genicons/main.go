package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

type iconVariant struct {
	name   string
	source string
	color  string
}

var variants = []iconVariant{
	{name: "cable-green", source: "cable.svg", color: "#34C759"},
	{name: "cable-blue", source: "cable.svg", color: "#0A84FF"},
	{name: "cable-orange", source: "cable.svg", color: "#FF9F0A"},
	{name: "unplug-gray", source: "unplug.svg", color: "#8E8E93"},
}

var icoSizes = []int{16, 20, 24, 32, 40, 48}

var icnsSizes = map[int]string{
	16:   "icp4",
	32:   "icp5",
	64:   "icp6",
	128:  "ic07",
	256:  "ic08",
	512:  "ic09",
	1024: "ic10",
}

func main() {
	sourceDir := flag.String("source", "../../internal/ui/icons", "directory containing source SVG icons")
	outputDir := flag.String("out", "../../internal/ui/icons", "directory for generated icon files")
	flag.Parse()

	if err := generate(*sourceDir, *outputDir); err != nil {
		fmt.Fprintln(os.Stderr, "generate icons:", err)
		os.Exit(1)
	}
}

func generate(sourceDir, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	for _, variant := range variants {
		source, err := os.ReadFile(filepath.Join(sourceDir, variant.source))
		if err != nil {
			return fmt.Errorf("read %s: %w", variant.source, err)
		}

		images := make(map[int][]byte, len(icoSizes))
		for _, size := range icoSizes {
			data, err := renderPNG(string(source), variant.color, size)
			if err != nil {
				return fmt.Errorf("render %s at %d px: %w", variant.name, size, err)
			}
			images[size] = data
			if size == 32 {
				if err := writeFile(outputDir, variant.name+".png", data); err != nil {
					return err
				}
			}
		}

		ico, err := encodeICO(images, icoSizes)
		if err != nil {
			return fmt.Errorf("encode %s ICO: %w", variant.name, err)
		}
		if err := writeFile(outputDir, variant.name+".ico", ico); err != nil {
			return err
		}

		if variant.name == "cable-green" {
			icnsImages := make(map[int][]byte, len(icnsSizes))
			for size := range icnsSizes {
				data, err := renderPNG(string(source), variant.color, size)
				if err != nil {
					return fmt.Errorf("render AppIcon at %d px: %w", size, err)
				}
				icnsImages[size] = data
			}
			icns, err := encodeICNS(icnsImages)
			if err != nil {
				return fmt.Errorf("encode AppIcon: %w", err)
			}
			if err := writeFile(outputDir, "AppIcon.icns", icns); err != nil {
				return err
			}
		}
	}
	return nil
}

func renderPNG(svg, stroke string, size int) ([]byte, error) {
	if size <= 0 {
		return nil, errors.New("image size must be positive")
	}
	if !strings.Contains(svg, "currentColor") {
		return nil, errors.New("SVG must use currentColor for its stroke")
	}
	svg = strings.ReplaceAll(svg, "currentColor", stroke)
	icon, err := oksvg.ReadIconStream(strings.NewReader(svg))
	if err != nil {
		return nil, fmt.Errorf("parse SVG: %w", err)
	}
	icon.SetTarget(0, 0, float64(size), float64(size))
	canvas := image.NewRGBA(image.Rect(0, 0, size, size))
	scanner := rasterx.NewScannerGV(size, size, canvas, canvas.Bounds())
	icon.Draw(rasterx.NewDasher(size, size, scanner), 1)

	var data bytes.Buffer
	if err := png.Encode(&data, canvas); err != nil {
		return nil, fmt.Errorf("encode PNG: %w", err)
	}
	return data.Bytes(), nil
}

func encodeICO(images map[int][]byte, sizes []int) ([]byte, error) {
	if len(sizes) == 0 {
		return nil, errors.New("ICO needs at least one image")
	}
	sizes = append([]int(nil), sizes...)
	sort.Ints(sizes)

	headerSize := 6 + len(sizes)*16
	result := make([]byte, headerSize)
	binary.LittleEndian.PutUint16(result[2:4], 1)
	binary.LittleEndian.PutUint16(result[4:6], uint16(len(sizes)))
	offset := headerSize
	for i, size := range sizes {
		pngData, ok := images[size]
		if !ok {
			return nil, fmt.Errorf("missing %d px ICO image", size)
		}
		if len(pngData) == 0 {
			return nil, fmt.Errorf("empty %d px ICO image", size)
		}
		entry := 6 + i*16
		if size < 256 {
			result[entry] = byte(size)
			result[entry+1] = byte(size)
		}
		binary.LittleEndian.PutUint16(result[entry+4:entry+6], 1)
		binary.LittleEndian.PutUint16(result[entry+6:entry+8], 32)
		binary.LittleEndian.PutUint32(result[entry+8:entry+12], uint32(len(pngData)))
		binary.LittleEndian.PutUint32(result[entry+12:entry+16], uint32(offset))
		offset += len(pngData)
		result = append(result, pngData...)
	}
	return result, nil
}

func encodeICNS(images map[int][]byte) ([]byte, error) {
	sizes := make([]int, 0, len(icnsSizes))
	for size := range icnsSizes {
		sizes = append(sizes, size)
	}
	sort.Ints(sizes)

	chunks := make([][]byte, 0, len(sizes))
	totalSize := 8
	for _, size := range sizes {
		pngData, ok := images[size]
		if !ok {
			return nil, fmt.Errorf("missing %d px ICNS image", size)
		}
		if len(pngData) == 0 {
			return nil, fmt.Errorf("empty %d px ICNS image", size)
		}
		chunkSize := 8 + len(pngData)
		chunk := make([]byte, chunkSize)
		copy(chunk[:4], icnsSizes[size])
		binary.BigEndian.PutUint32(chunk[4:8], uint32(chunkSize))
		copy(chunk[8:], pngData)
		chunks = append(chunks, chunk)
		totalSize += chunkSize
	}

	result := make([]byte, 8, totalSize)
	copy(result[:4], "icns")
	binary.BigEndian.PutUint32(result[4:8], uint32(totalSize))
	for _, chunk := range chunks {
		result = append(result, chunk...)
	}
	return result, nil
}

func writeFile(outputDir, name string, data []byte) error {
	if err := os.WriteFile(filepath.Join(outputDir, name), data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}
