//go:build linux && cgo && !gtk3

package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteIconThemeWritesEverySize(t *testing.T) {
	dir := t.TempDir()

	if err := writeIconTheme(dir, appIcon); err != nil {
		t.Fatal(err)
	}

	for _, size := range iconSizes {
		img := decodeFile(t, filepath.Join(dir, "hicolor", iconDir(size), "smartnote.png"))
		if b := img.Bounds(); b.Dx() != size || b.Dy() != size {
			t.Errorf("icon %d is %v", size, b)
		}
	}

	index, err := os.ReadFile(filepath.Join(dir, "hicolor", "index.theme"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(index), "Directories=16x16/apps,32x32/apps,64x64/apps,128x128/apps") {
		t.Errorf("index.theme = %q", index)
	}
}

func TestWriteIconThemeKeepsTheSameIcon(t *testing.T) {
	dir := t.TempDir()
	if err := writeIconTheme(dir, appIcon); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "hicolor", iconDir(16), "smartnote.png")

	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	if err := writeIconTheme(dir, appIcon); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if !info.ModTime().Equal(old) {
		t.Errorf("the icon was written again at %v", info.ModTime())
	}
}

func TestWriteIconThemeReplacesAnotherIcon(t *testing.T) {
	dir := t.TempDir()
	if err := writeIconTheme(dir, encodePNG(t, solid(128, color.NRGBA{R: 255, A: 255}))); err != nil {
		t.Fatal(err)
	}

	if err := writeIconTheme(dir, encodePNG(t, solid(128, color.NRGBA{B: 255, A: 255}))); err != nil {
		t.Fatal(err)
	}

	got := decodeFile(t, filepath.Join(dir, "hicolor", iconDir(16), "smartnote.png"))
	if c := color.NRGBAModel.Convert(got.At(0, 0)).(color.NRGBA); c.B != 255 || c.R != 0 {
		t.Errorf("pixel = %v, want the new blue icon", c)
	}
}

func TestWriteIconThemeRejectsAnIconOfAnotherSize(t *testing.T) {
	if err := writeIconTheme(t.TempDir(), encodePNG(t, solid(100, color.NRGBA{A: 255}))); err == nil {
		t.Error("want an error for a 100×100 icon")
	}
}

func TestScaleDownAveragesWithAlpha(t *testing.T) {
	// One opaque white pixel and three transparent black ones become a
	// quarter-opaque white pixel, not grey.
	src := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	src.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255})

	got, err := scaleDown(src, 1)
	if err != nil {
		t.Fatal(err)
	}

	if c := got.NRGBAAt(0, 0); c != (color.NRGBA{R: 255, G: 255, B: 255, A: 64}) {
		t.Errorf("pixel = %v, want white with alpha 64", c)
	}
}

func solid(size int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			img.SetNRGBA(x, y, c)
		}
	}

	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

func decodeFile(t *testing.T, path string) image.Image {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	return img
}
