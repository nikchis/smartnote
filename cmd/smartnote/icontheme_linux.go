//go:build linux && cgo && !gtk3

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// iconName is the icon theme name of the logo, the same as in the .desktop file.
const iconName = "smartnote"

// iconSizes are the sizes of the logo in the icon theme; each divides the
// side of appIcon, so that scaling averages whole blocks of pixels.
var iconSizes = []int{16, 32, 64, 128}

// stampFile holds the SHA-256 of the logo the theme was written from.
const stampFile = "smartnote.sha256"

// installIconTheme writes appIcon as an icon theme to the user's cache
// directory and returns the directory to add to the icon search path.
func installIconTheme() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cache directory: %w", err)
	}

	dir := filepath.Join(cache, "smartnote", "icons")

	return dir, writeIconTheme(dir, appIcon)
}

// writeIconTheme writes icon, scaled to iconSizes, as the hicolor theme
// under dir: GTK 4 lists the sizes of an icon by the directories of its
// theme only. Nothing is written when the theme already holds this icon.
func writeIconTheme(dir string, icon []byte) error {
	sum := sha256.Sum256(icon)
	stamp := hex.EncodeToString(sum[:])
	theme := filepath.Join(dir, "hicolor")
	stampPath := filepath.Join(theme, stampFile)

	if old, err := os.ReadFile(stampPath); err == nil && string(old) == stamp {
		return nil
	}

	src, err := png.Decode(bytes.NewReader(icon))
	if err != nil {
		return fmt.Errorf("decode icon: %w", err)
	}

	for _, size := range iconSizes {
		scaled, err := scaleDown(src, size)
		if err != nil {
			return err
		}

		var buf bytes.Buffer
		if err := png.Encode(&buf, scaled); err != nil {
			return fmt.Errorf("encode icon: %w", err)
		}

		if err := writeFileAtomic(filepath.Join(theme, iconDir(size), iconName+".png"), buf.Bytes()); err != nil {
			return err
		}
	}

	// Used only when the system has no hicolor theme: GTK reads the first
	// index.theme of a theme that it finds.
	if err := writeFileAtomic(filepath.Join(theme, "index.theme"), indexTheme()); err != nil {
		return err
	}

	// The stamp goes last: an interrupted write is repeated on the next start.
	return writeFileAtomic(stampPath, []byte(stamp))
}

// iconDir is the theme directory of the icons of size.
func iconDir(size int) string {
	n := strconv.Itoa(size)
	return filepath.Join(n+"x"+n, "apps")
}

// indexTheme describes the directories of iconSizes.
func indexTheme() []byte {
	dirs := make([]string, 0, len(iconSizes))
	for _, size := range iconSizes {
		dirs = append(dirs, filepath.ToSlash(iconDir(size)))
	}

	var b strings.Builder
	b.WriteString("[Icon Theme]\nName=Hicolor\nHidden=true\nDirectories=" + strings.Join(dirs, ",") + "\n")

	for i, size := range iconSizes {
		fmt.Fprintf(&b, "\n[%s]\nSize=%d\nContext=Applications\nType=Threshold\n", dirs[i], size)
	}

	return []byte(b.String())
}

// scaleDown shrinks the square image src to size×size by averaging blocks of
// pixels weighted by alpha, so that transparent pixels do not darken edges.
// The side of src must be a multiple of size.
func scaleDown(src image.Image, size int) (*image.NRGBA, error) {
	b := src.Bounds()
	if b.Dx() != b.Dy() || size <= 0 || b.Dx()%size != 0 {
		return nil, fmt.Errorf("scale icon: %dx%d to %d", b.Dx(), b.Dy(), size)
	}

	in, ok := src.(*image.NRGBA)
	if !ok || b.Min != (image.Point{}) {
		in = image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(in, in.Bounds(), src, b.Min, draw.Src)
	}

	k := b.Dx() / size
	out := image.NewNRGBA(image.Rect(0, 0, size, size))

	for y := range size {
		for x := range size {
			var r, g, bl, a, n uint64

			for sy := y * k; sy < (y+1)*k; sy++ {
				row := in.Pix[sy*in.Stride:]
				for sx := x * k; sx < (x+1)*k; sx++ {
					p := row[sx*4 : sx*4+4]
					pa := uint64(p[3])
					r += uint64(p[0]) * pa
					g += uint64(p[1]) * pa
					bl += uint64(p[2]) * pa
					a += pa
					n++
				}
			}

			o := out.Pix[y*out.Stride+x*4:]
			if a > 0 {
				o[0] = uint8((r + a/2) / a)  //nolint:gosec // an average of bytes
				o[1] = uint8((g + a/2) / a)  //nolint:gosec // an average of bytes
				o[2] = uint8((bl + a/2) / a) //nolint:gosec // an average of bytes
			}

			o[3] = uint8((a + n/2) / n) //nolint:gosec // an average of bytes
		}
	}

	return out, nil
}

// writeFileAtomic replaces path with data through a temporary file, so that
// GTK never reads a half-written icon.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create icon directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("write icon: %w", err)
	}
	defer os.Remove(tmp.Name()) // fails harmlessly once renamed

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write icon: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write icon: %w", err)
	}

	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write icon: %w", err)
	}

	return nil
}
