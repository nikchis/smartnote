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
	cmdPrefix  = modulePath + "/cmd"
)

// rule restricts the project imports of one package. A package that does not
// exist yet is skipped, so a rule for a future package applies once it appears.
type rule struct {
	pkg string
	// onlyProject, when set, is the exact list of allowed project imports.
	onlyProject []string
	// forbidden lists project imports the package must not have.
	forbidden []string
}

var rules = []rule{
	{pkg: internal + "domain", onlyProject: []string{}},
	{pkg: internal + "usecase", onlyProject: []string{internal + "domain"}},
	{pkg: internal + "i18n", onlyProject: []string{}},
	{pkg: adapter + "yadisk", forbidden: []string{adapter + "gdrive", adapter + "wailsui"}},
	{pkg: adapter + "gdrive", forbidden: []string{adapter + "yadisk", adapter + "wailsui"}},
	{pkg: adapter + "wailsui", forbidden: []string{adapter + "yadisk", adapter + "gdrive", adapter + "cloudauth"}},
}

// i18nImporters are the only packages that may import i18n.
var i18nImporters = []string{
	adapter + "wailsui",
	adapter + "cloudauth",
	modulePath + "/cmd/smartnote",
}

func isProject(path string) bool {
	return path == modulePath || strings.HasPrefix(path, modulePath+"/")
}

// packages returns the import path of every Go package under dir (relative to
// the module root) that has non-test Go files.
func packages(t *testing.T, root, dir string) map[string]*build.Package {
	t.Helper()

	found := map[string]*build.Package{}

	err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() {
			return nil
		}

		pkg, err := build.Default.ImportDir(p, 0)
		if err != nil {
			var noGo *build.NoGoError
			if errors.As(err, &noGo) {
				return nil
			}

			return err
		}

		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}

		found[modulePath+"/"+filepath.ToSlash(rel)] = pkg

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

func projectImports(pkg *build.Package) []string {
	var out []string

	for _, imp := range pkg.Imports {
		if isProject(imp) {
			out = append(out, imp)
		}
	}

	return out
}

func TestLayers(t *testing.T) {
	root := moduleRoot(t)

	all := packages(t, root, "internal")
	for path, pkg := range packages(t, root, "cmd") {
		all[path] = pkg
	}

	if _, ok := all[internal+"domain"]; !ok {
		t.Fatal("package domain not found: the test does not see the module")
	}

	for _, r := range rules {
		pkg, ok := all[r.pkg]
		if !ok {
			continue
		}

		for _, imp := range projectImports(pkg) {
			if r.onlyProject != nil && !slices.Contains(r.onlyProject, imp) {
				t.Errorf("package %s imports %s: only %v is allowed", r.pkg, imp, r.onlyProject)
			}

			if slices.Contains(r.forbidden, imp) {
				t.Errorf("package %s imports %s: forbidden", r.pkg, imp)
			}
		}
	}

	for path, pkg := range all {
		for _, imp := range projectImports(pkg) {
			if strings.HasPrefix(path, internal) && strings.HasPrefix(imp, cmdPrefix) {
				t.Errorf("package %s imports %s: internal must not import cmd", path, imp)
			}

			if imp == internal+"i18n" && !slices.Contains(i18nImporters, path) {
				t.Errorf("package %s imports %s: only %v may import i18n", path, imp, i18nImporters)
			}
		}
	}
}
