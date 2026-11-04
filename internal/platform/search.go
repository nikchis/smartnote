package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ErrSearchMissing means the search worker file does not exist.
var ErrSearchMissing = errors.New("search worker not found")

// SearchLocation holds the absolute paths of the search worker, the ONNX
// Runtime library and the model directory. The library and the directory may
// be absent: the worker reports that semantic search is unavailable itself.
type SearchLocation struct {
	Worker, OrtLib, ModelDir string
}

// Names of the search files, shared by all systems.
const (
	workerName = "smartnote-search"
	modelName  = "model"
	ortVersion = "1.31.0"
)

// FindSearch locates the search worker, the ONNX Runtime library and the model
// directory relative to the application executable exe (symbolic links are
// resolved first, since a package may link the executable into a bin directory).
// In builds without the production tag SMARTNOTE_SEARCH_DIR overrides the layout.
// It returns ErrSearchMissing when the worker file does not exist.
func FindSearch(exe string) (SearchLocation, error) {
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	return findSearch(runtime.GOOS, exe, searchDirOverride())
}

// findSearch implements FindSearch for the system goos; override is the
// directory from searchDirOverride, empty for the package layout.
func findSearch(goos, exe, override string) (SearchLocation, error) {
	var loc SearchLocation

	if override != "" {
		loc = searchInDir(goos, override)
	} else {
		var err error

		if loc, err = searchInLayout(goos, exe); err != nil {
			return SearchLocation{}, err
		}
	}

	if _, err := os.Stat(loc.Worker); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SearchLocation{}, fmt.Errorf("%w: %s", ErrSearchMissing, loc.Worker)
		}

		return SearchLocation{}, fmt.Errorf("search worker: %w", err)
	}

	return loc, nil
}

// searchInDir is the flat layout of SMARTNOTE_SEARCH_DIR.
func searchInDir(goos, dir string) SearchLocation {
	dir = absClean(dir)

	return SearchLocation{
		Worker:   filepath.Join(dir, workerFile(goos)),
		OrtLib:   filepath.Join(dir, ortFile(goos)),
		ModelDir: filepath.Join(dir, modelName),
	}
}

// searchInLayout is the layout of the packages (specification 8.2).
func searchInLayout(goos, exe string) (SearchLocation, error) {
	if exe == "" {
		return SearchLocation{}, errors.New("empty executable path")
	}

	bin := filepath.Dir(absClean(exe))

	switch goos {
	case goosWindows:
		return searchInDir(goos, bin), nil
	case goosDarwin:
		return SearchLocation{
			Worker:   filepath.Join(bin, workerFile(goos)),
			OrtLib:   filepath.Join(bin, ortFile(goos)),
			ModelDir: filepath.Join(bin, "..", "Resources", modelName),
		}.cleaned(), nil
	default:
		lib := filepath.Join(bin, "..", "lib", appDir)

		return SearchLocation{
			Worker:   filepath.Join(lib, workerFile(goos)),
			OrtLib:   filepath.Join(lib, ortFile(goos)),
			ModelDir: filepath.Join(bin, "..", "share", appDir, modelName),
		}.cleaned(), nil
	}
}

func (l SearchLocation) cleaned() SearchLocation {
	return SearchLocation{
		Worker:   filepath.Clean(l.Worker),
		OrtLib:   filepath.Clean(l.OrtLib),
		ModelDir: filepath.Clean(l.ModelDir),
	}
}

func workerFile(goos string) string {
	if goos == goosWindows {
		return workerName + ".exe"
	}

	return workerName
}

func ortFile(goos string) string {
	switch goos {
	case goosWindows:
		return "onnxruntime.dll"
	case goosDarwin:
		return "libonnxruntime." + ortVersion + ".dylib"
	default:
		return "libonnxruntime.so." + ortVersion
	}
}

// absClean returns the absolute, cleaned form of path; a path that cannot be
// made absolute is only cleaned.
func absClean(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}

	return filepath.Clean(path)
}
