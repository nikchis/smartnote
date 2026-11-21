package archtest

import (
	"errors"
	"go/build"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	modulePath = "github.com/nikchis/smartnote"
	internal   = modulePath + "/internal/"
	adapter    = internal + "adapter/"
	cmdPath    = modulePath + "/cmd"
)

// rule restricts the project imports of one package and its subpackages.
// Every package must exist except the ones marked optional (not written yet),
// so a rename or move cannot silently switch a rule off.
type rule struct {
	pkg      string
	optional bool
	// onlyProject, when set, is the exact list of allowed project imports
	// (imports inside the package's own tree are always allowed).
	onlyProject []string
	// forbidden lists project imports (and their subpackages) the package
	// must not have.
	forbidden []string
}

var rules = []rule{
	{pkg: internal + "domain", onlyProject: []string{}},
	{pkg: internal + "usecase", onlyProject: []string{internal + "domain"}},
	{pkg: internal + "i18n", onlyProject: []string{}},
	{pkg: adapter + "yadisk", forbidden: []string{adapter + "gdrive", adapter + "wailsui"}},
	{pkg: adapter + "gdrive", optional: true, forbidden: []string{adapter + "yadisk", adapter + "wailsui"}},
	{pkg: adapter + "wailsui", forbidden: []string{adapter + "yadisk", adapter + "gdrive", adapter + "cloudauth"}},
}

// i18nImporters are the only packages (with subpackages) that may import i18n.
// All of them must exist.
var i18nImporters = []string{
	adapter + "wailsui",
	adapter + "cloudauth",
	modulePath + "/cmd/smartnote",
}

// platforms are the operating systems whose build-tagged files are checked.
var platforms = []string{"linux", "windows", "darwin"}

// within reports whether path is pkg or one of its subpackages.
func within(path, pkg string) bool {
	return path == pkg || strings.HasPrefix(path, pkg+"/")
}

func withinAny(path string, pkgs []string) bool {
	return slices.ContainsFunc(pkgs, func(p string) bool { return within(path, p) })
}

func isProject(path string) bool {
	return path == modulePath || strings.HasPrefix(path, modulePath+"/")
}

// packages returns the sorted project imports of every Go package under dir
// (relative to the module root) that has non-test Go files, keyed by import
// path. Imports are the union over the platforms. UseAllFiles is not used:
// it breaks on files that are excluded on purpose (build ignore).
func packages(t *testing.T, root, dir string) map[string][]string {
	t.Helper()

	found := map[string][]string{}

	err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() {
			return nil
		}

		var imports []string

		exists := false

		for _, goos := range platforms {
			ctx := build.Default
			ctx.GOOS = goos
			ctx.CgoEnabled = true

			pkg, err := ctx.ImportDir(p, 0)
			if err != nil {
				var noGo *build.NoGoError
				if errors.As(err, &noGo) {
					continue
				}

				return err
			}

			exists = true

			for _, imp := range pkg.Imports {
				if isProject(imp) && !slices.Contains(imports, imp) {
					imports = append(imports, imp)
				}
			}
		}

		if !exists {
			return nil
		}

		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}

		slices.Sort(imports)
		found[modulePath+"/"+filepath.ToSlash(rel)] = imports

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}

	return found
}

func moduleRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	return filepath.Join(wd, "..", "..")
}

func TestLayers(t *testing.T) {
	root := moduleRoot(t)

	all := packages(t, root, "internal")
	for path, imports := range packages(t, root, "cmd") {
		all[path] = imports
	}

	for _, r := range rules {
		if !r.optional {
			if _, ok := all[r.pkg]; !ok {
				t.Errorf("required package %s not found: renamed or moved? update the rules", r.pkg)
			}
		}
	}

	for _, p := range i18nImporters {
		if _, ok := all[p]; !ok {
			t.Errorf("i18n importer %s not found: renamed or moved? update the rules", p)
		}
	}

	for _, r := range rules {
		for path, imports := range all {
			if !within(path, r.pkg) {
				continue
			}

			for _, imp := range imports {
				if r.onlyProject != nil && !within(imp, r.pkg) && !withinAny(imp, r.onlyProject) {
					t.Errorf("package %s imports %s: only %v is allowed", path, imp, r.onlyProject)
				}

				if withinAny(imp, r.forbidden) {
					t.Errorf("package %s imports %s: forbidden", path, imp)
				}
			}
		}
	}

	for path, imports := range all {
		for _, imp := range imports {
			if within(path, internal[:len(internal)-1]) && within(imp, cmdPath) {
				t.Errorf("package %s imports %s: internal must not import cmd", path, imp)
			}

			if within(imp, internal+"i18n") && !within(path, internal+"i18n") && !withinAny(path, i18nImporters) {
				t.Errorf("package %s imports %s: only %v may import i18n", path, imp, i18nImporters)
			}
		}
	}
}
