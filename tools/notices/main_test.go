package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// cargoMetadata is a cut-down `cargo metadata` document: worker uses alpha (normal) and the
// workspace crate zalloc, which uses beta; gamma is only a dev dependency and delta only a
// build dependency, so neither is shipped.
const metadataFixture = `{
  "packages": [
    {"id": "path+file:///w/worker#0.1.0", "name": "worker", "version": "0.1.0", "source": null, "manifest_path": "/w/worker/Cargo.toml"},
    {"id": "path+file:///w/zalloc#0.1.0", "name": "zalloc", "version": "0.1.0", "source": null, "manifest_path": "/w/zalloc/Cargo.toml"},
    {"id": "reg#alpha@1.0.0", "name": "alpha", "version": "1.0.0", "source": "registry", "license": "MIT", "license_file": null,
     "repository": "https://example.invalid/alpha", "manifest_path": "/r/alpha-1.0.0/Cargo.toml"},
    {"id": "reg#beta@2.0.0", "name": "beta", "version": "2.0.0", "source": "registry", "license": null, "license_file": "COPYING.txt", "repository": null, "manifest_path": "/r/beta-2.0.0/Cargo.toml"},
    {"id": "reg#gamma@3.0.0", "name": "gamma", "version": "3.0.0", "source": "registry", "license": "MIT", "manifest_path": "/r/gamma-3.0.0/Cargo.toml"},
    {"id": "reg#delta@4.0.0", "name": "delta", "version": "4.0.0", "source": "registry", "license": "MIT", "manifest_path": "/r/delta-4.0.0/Cargo.toml"}
  ],
  "workspace_members": ["path+file:///w/worker#0.1.0", "path+file:///w/zalloc#0.1.0"],
  "resolve": {"nodes": [
    {"id": "path+file:///w/worker#0.1.0", "deps": [
      {"pkg": "reg#alpha@1.0.0", "dep_kinds": [{"kind": null, "target": null}]},
      {"pkg": "path+file:///w/zalloc#0.1.0", "dep_kinds": [{"kind": null, "target": null}]},
      {"pkg": "reg#gamma@3.0.0", "dep_kinds": [{"kind": "dev", "target": null}]},
      {"pkg": "reg#delta@4.0.0", "dep_kinds": [{"kind": "build", "target": null}]}
    ]},
    {"id": "path+file:///w/zalloc#0.1.0", "deps": [
      {"pkg": "reg#beta@2.0.0", "dep_kinds": [{"kind": null, "target": null}]}
    ]},
    {"id": "reg#alpha@1.0.0", "deps": [
      {"pkg": "reg#beta@2.0.0", "dep_kinds": [{"kind": "build", "target": null}, {"kind": null, "target": null}]}
    ]},
    {"id": "reg#beta@2.0.0", "deps": []},
    {"id": "reg#gamma@3.0.0", "deps": [{"pkg": "reg#delta@4.0.0", "dep_kinds": [{"kind": null, "target": null}]}]},
    {"id": "reg#delta@4.0.0", "deps": []}
  ]}
}`

func TestCrateComponentsKeepsTheShippedRegistryCrates(t *testing.T) {
	got, err := crateComponents(strings.NewReader(metadataFixture), "worker")
	if err != nil {
		t.Fatal(err)
	}

	want := []component{
		{Name: "alpha", Version: "1.0.0", License: "MIT", URL: "https://example.invalid/alpha", Dir: "/r/alpha-1.0.0"},
		{Name: "beta", Version: "2.0.0", Dir: "/r/beta-2.0.0", Extra: []string{"COPYING.txt"}},
	}

	if len(got) != len(want) {
		t.Fatalf("crateComponents() = %+v, want %+v", got, want)
	}

	for i := range want {
		if got[i].Name != want[i].Name || got[i].Version != want[i].Version || got[i].License != want[i].License ||
			got[i].URL != want[i].URL || got[i].Dir != want[i].Dir || strings.Join(got[i].Extra, ",") != strings.Join(want[i].Extra, ",") {
			t.Errorf("component %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestCrateComponentsRejectsUnknownRoot(t *testing.T) {
	_, err := crateComponents(strings.NewReader(metadataFixture), "nope")
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("crateComponents() error = %v, want an unknown package", err)
	}
}

func TestModuleComponentsParsesGoList(t *testing.T) {
	list := "golang.org/x/sys\tv0.49.0\t/m/sys@v0.49.0\n\n" +
		"github.com/google/uuid\tv1.6.0\t/m/uuid@v1.6.0\n" +
		"golang.org/x/sys\tv0.49.0\t/m/sys@v0.49.0\n"

	got, err := moduleComponents(strings.NewReader(list))
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 || got[0].Name != "github.com/google/uuid" || got[0].Version != "v1.6.0" || got[0].Dir != "/m/uuid@v1.6.0" ||
		got[1].Name != "golang.org/x/sys" || got[1].URL != "https://pkg.go.dev/golang.org/x/sys@v0.49.0" {
		t.Errorf("moduleComponents() = %+v", got)
	}
}

func TestModuleComponentsRejectsMalformedLine(t *testing.T) {
	_, err := moduleComponents(strings.NewReader("golang.org/x/sys v0.49.0\n"))
	if err == nil {
		t.Fatal("moduleComponents() error = nil, want a malformed line")
	}
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLicenseFilesFindsLicenceNoticeAndExtraFiles(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"LICENSE-MIT": "mit", "LICENSE-APACHE": "apache", "NOTICE": "notice", "COPYING.txt": "copying",
		"README.md": "readme", "license.rs": "code", "Cargo.toml": "toml",
	})

	got, err := licenseFiles(component{Dir: dir, Extra: []string{"COPYING.txt"}})
	if err != nil {
		t.Fatal(err)
	}

	if strings.Join(got, ",") != "COPYING.txt,LICENSE-APACHE,LICENSE-MIT,NOTICE" {
		t.Errorf("licenseFiles() = %v", got)
	}
}

func TestWriteNoticesListsComponentsAndSharesIdenticalTexts(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, filepath.Join(root, "a"), map[string]string{"LICENSE": "Same text\r\n"})
	writeFiles(t, filepath.Join(root, "b"), map[string]string{"LICENSE": "Same text\n", "NOTICE": "b notice"})
	writeFiles(t, filepath.Join(root, "c"), map[string]string{"README": "no licence here"})

	comps := []component{
		{Name: "a", Version: "1", License: "MIT", URL: "https://a.invalid", Dir: filepath.Join(root, "a")},
		{Name: "b", Version: "2", Dir: filepath.Join(root, "b")},
		{Name: "c", Version: "3", License: "Apache-2.0", Dir: filepath.Join(root, "c")},
	}

	var buf bytes.Buffer

	missing, err := writeNotices(&buf, "Title line", comps, fstest.MapFS{})
	if err != nil {
		t.Fatal(err)
	}

	if strings.Join(missing, ",") != "c 3" {
		t.Errorf("missing = %v, want [c 3]", missing)
	}

	text := buf.String()
	for _, want := range []string{
		"Title line\n",
		"a 1 — MIT — https://a.invalid\n",
		"b 2\n",
		"c 3 — Apache-2.0\n",
		"LICENSE:\n\nSame text\n",
		"LICENSE: the same text as a 1, LICENSE.\n",
		"NOTICE:\n\nb notice\n",
		"No licence file in the package; its licence by the package metadata: Apache-2.0.\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("notices lack %q:\n%s", want, text)
		}
	}

	if strings.Count(text, "Same text") != 1 {
		t.Errorf("identical text written more than once:\n%s", text)
	}

	if strings.Contains(text, "\r") {
		t.Error("notices contain CR")
	}
}

func TestWriteNoticesUsesKeptTextsAndTheFilesOfTheSameRepository(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, filepath.Join(root, "tantivy"), map[string]string{"LICENSE": "tantivy licence"})
	writeFiles(t, filepath.Join(root, "tantivy-common"), map[string]string{"README.md": "readme"})
	writeFiles(t, filepath.Join(root, "kitoken"), map[string]string{"README.md": "readme"})

	repo := "https://example.invalid/tantivy"
	comps := []component{
		{Name: "kitoken", Version: "0.11.1", License: "BSD-2-Clause", Dir: filepath.Join(root, "kitoken")},
		{Name: "tantivy", Version: "0.27.0", License: "MIT", URL: repo, Dir: filepath.Join(root, "tantivy")},
		{Name: "tantivy-common", Version: "0.27.0", License: "MIT", URL: repo, Dir: filepath.Join(root, "tantivy-common")},
	}
	kept := fstest.MapFS{"kitoken/LICENSE": {Data: []byte("kitoken licence\n")}}

	var buf bytes.Buffer

	missing, err := writeNotices(&buf, "Title", comps, kept)
	if err != nil {
		t.Fatal(err)
	}

	if len(missing) != 0 {
		t.Errorf("missing = %v, want none", missing)
	}

	text := buf.String()
	for _, want := range []string{
		"The package has no licence file; the texts below come from its source repository.\n\nLICENSE:\n\nkitoken licence\n",
		"The package has no licence file; the licence files of tantivy 0.27.0, from the same repository " + repo + ", cover it.\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("notices lack %q:\n%s", want, text)
		}
	}
}
