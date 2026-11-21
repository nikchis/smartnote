// Package prefs keeps the preferences of the app that are needed before the
// storage is unlocked and are not secret: the language of the interface
// (spec §16.4). They live in plain JSON in prefs.json in the data directory.
package prefs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// fileName is the name of the preferences file in the data directory.
const fileName = "prefs.json"

// Prefs are the saved preferences. Language is what the user chose, as
// written; the reader checks it.
type Prefs struct {
	Language string `json:"language"`
}

// Load reads the preferences from dir. found is false without an error when
// the file is missing or is not valid JSON of an object: the app then starts
// with the defaults. Other failures to read the file are errors.
func Load(dir string) (p Prefs, found bool, err error) {
	b, err := os.ReadFile(filepath.Join(dir, fileName))
	if errors.Is(err, fs.ErrNotExist) {
		return Prefs{}, false, nil
	}

	if err != nil {
		return Prefs{}, false, fmt.Errorf("read preferences: %w", err)
	}

	if err := json.Unmarshal(b, &p); err != nil {
		return Prefs{}, false, nil //nolint:nilerr // a damaged file means the defaults, not a failed start
	}

	return p, true, nil
}

// Save writes p to dir: to a temporary file of mode 0600 first, then renamed
// over prefs.json, so that a crash leaves either the old or the new file.
func Save(dir string, p Prefs) (err error) {
	b, err := json.Marshal(p)
	if err != nil { // a struct of strings always marshals
		return fmt.Errorf("encode preferences: %w", err)
	}

	f, err := os.CreateTemp(dir, ".prefs-*.tmp") // CreateTemp makes it 0600
	if err != nil {
		return fmt.Errorf("save preferences: %w", err)
	}

	tmp := f.Name()

	defer func() {
		if err != nil {
			_ = os.Remove(tmp) // best effort: the file holds no secret
		}
	}()

	if _, err := f.Write(b); err != nil {
		_ = f.Close() // the write error is the one to report

		return fmt.Errorf("save preferences: %w", err)
	}

	if err := f.Sync(); err != nil {
		_ = f.Close() // the sync error is the one to report

		return fmt.Errorf("save preferences: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("save preferences: %w", err)
	}

	if err := os.Rename(tmp, filepath.Join(dir, fileName)); err != nil {
		return fmt.Errorf("save preferences: %w", err)
	}

	return nil
}
