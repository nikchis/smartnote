// Command notices writes the licence notices of the third-party code linked into the packages:
// the Rust crates of the search worker (from cargo metadata of the worker's build target) and
// the Go modules of the application (from go list of the target system), each with the licence
// files of its source package. The lists follow Cargo.lock and go.sum, so they are rebuilt with
// every package and stay in step with the dependencies.
//
// Usage:
//
//	go run ./tools/notices -rust-target x86_64-unknown-linux-gnu -goos linux -goarch amd64 [-cgo 1] [-out build/notices]
//
// It writes rust-crates.txt and go-modules.txt into the output directory.
//
// A package without licence files is covered by kept texts, extra/<name>/ (copied from the
// package's source repository at the released version: kitoken v0.11.1,
// https://github.com/Systemcluster/kitoken/blob/v0.11.1/LICENSE), or by the licence files of
// a listed package from the same repository (the tantivy crates); otherwise it is reported.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// keptTexts holds extra/<package name>/<licence file> for packages that ship none.
//
//go:embed extra
var keptTexts embed.FS

// component is one third-party package with the directory of its source files.
type component struct {
	Name, Version, License, URL string
	// Dir holds the package's source files, where its licence files are looked up.
	Dir string
	// Extra names further licence files relative to Dir (Cargo's license-file).
	Extra []string
}

type options struct {
	rustTarget, search, crate string
	goos, goarch, cgo         string
	mainPkg, tags, out        string
}

func main() {
	var opts options

	flag.StringVar(&opts.rustTarget, "rust-target", "", "Rust target triple of the worker")
	flag.StringVar(&opts.search, "search", "search", "Cargo workspace of the worker")
	flag.StringVar(&opts.crate, "crate", "worker", "Cargo package of the worker")
	flag.StringVar(&opts.goos, "goos", "", "GOOS of the application")
	flag.StringVar(&opts.goarch, "goarch", "", "GOARCH of the application")
	flag.StringVar(&opts.cgo, "cgo", "1", "CGO_ENABLED of the application build")
	flag.StringVar(&opts.mainPkg, "main", "./cmd/smartnote", "main package of the application")
	flag.StringVar(&opts.tags, "tags", "production", "build tags of the application")
	flag.StringVar(&opts.out, "out", filepath.Join("build", "notices"), "output directory")

	flag.Parse()

	if opts.rustTarget == "" || opts.goos == "" || opts.goarch == "" || flag.NArg() > 0 {
		flag.Usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, opts)

	stop()

	if err != nil {
		fmt.Fprintln(os.Stderr, "notices:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, opts options) error {
	if err := os.MkdirAll(opts.out, 0o750); err != nil {
		return err
	}

	metadata, err := output(ctx, opts.search, nil, "cargo", "metadata", "--locked", "--format-version", "1", "--filter-platform", opts.rustTarget)
	if err != nil {
		return err
	}

	crates, err := crateComponents(bytes.NewReader(metadata), opts.crate)
	if err != nil {
		return err
	}

	title := fmt.Sprintf("Third-party Rust crates linked into smartnote-search (%s), from search/Cargo.lock.\n"+
		"The Rust standard library is under MIT OR Apache-2.0, Copyright (c) The Rust Project Contributors.", opts.rustTarget)
	if err := writeFile(filepath.Join(opts.out, "rust-crates.txt"), title, crates); err != nil {
		return err
	}

	env := []string{"GOOS=" + opts.goos, "GOARCH=" + opts.goarch, "CGO_ENABLED=" + opts.cgo, "GOFLAGS=-mod=readonly"}

	list, err := output(ctx, "", env, "go", "list", "-deps", "-tags", opts.tags, "-f",
		"{{with .Module}}{{if not .Main}}{{.Path}}\t{{.Version}}\t{{.Dir}}{{end}}{{end}}", opts.mainPkg)
	if err != nil {
		return err
	}

	modules, err := moduleComponents(bytes.NewReader(list))
	if err != nil {
		return err
	}

	std, err := goStdComponent(ctx)
	if err != nil {
		return err
	}

	title = fmt.Sprintf("Third-party Go modules linked into smartnote (%s/%s), from go.sum, and the Go standard library.", opts.goos, opts.goarch)

	return writeFile(filepath.Join(opts.out, "go-modules.txt"), title, append([]component{std}, modules...))
}

// output runs a command in dir with env added to the environment and returns its standard
// output; its standard error goes to ours.
func output(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr

	cmd.Env = append(os.Environ(), env...)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", name, args[0], err)
	}

	return out, nil
}

// goStdComponent is the Go standard library and runtime of the toolchain that builds the app.
func goStdComponent(ctx context.Context) (component, error) {
	out, err := output(ctx, "", nil, "go", "env", "GOVERSION", "GOROOT")
	if err != nil {
		return component{}, err
	}

	fields := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(fields) != 2 {
		return component{}, fmt.Errorf("go env: unexpected output %q", out)
	}

	return component{
		Name: "Go standard library and runtime", Version: strings.TrimSpace(fields[0]),
		License: "BSD-3-Clause", URL: "https://go.dev", Dir: strings.TrimSpace(fields[1]),
	}, nil
}

// depKind is the kind of a dependency edge: nil for a normal one, "dev" or "build".
type depKind struct {
	Kind *string `json:"kind"`
}

// cargoMetadata is the part of `cargo metadata` the notices use.
type cargoMetadata struct {
	Packages []struct {
		ID           string  `json:"id"`
		Name         string  `json:"name"`
		Version      string  `json:"version"`
		Source       *string `json:"source"`
		License      *string `json:"license"`
		LicenseFile  *string `json:"license_file"`
		Repository   *string `json:"repository"`
		ManifestPath string  `json:"manifest_path"`
	} `json:"packages"`
	WorkspaceMembers []string `json:"workspace_members"`
	Resolve          struct {
		Nodes []struct {
			ID   string `json:"id"`
			Deps []struct {
				Pkg      string    `json:"pkg"`
				DepKinds []depKind `json:"dep_kinds"`
			} `json:"deps"`
		} `json:"nodes"`
	} `json:"resolve"`
}

// crateComponents returns the crates that workspace member root links: its normal
// dependencies, transitively, without development and build dependencies and without the
// crates of the workspace itself. They are sorted by name and version.
func crateComponents(r io.Reader, root string) ([]component, error) {
	var meta cargoMetadata
	if err := json.NewDecoder(r).Decode(&meta); err != nil {
		return nil, fmt.Errorf("cargo metadata: %w", err)
	}

	normal := func(k depKind) bool { return k.Kind == nil }
	deps := make(map[string][]string, len(meta.Resolve.Nodes))

	for _, node := range meta.Resolve.Nodes {
		for _, dep := range node.Deps {
			if slices.ContainsFunc(dep.DepKinds, normal) {
				deps[node.ID] = append(deps[node.ID], dep.Pkg)
			}
		}
	}

	rootID := ""

	for _, pkg := range meta.Packages {
		if pkg.Name == root && slices.Contains(meta.WorkspaceMembers, pkg.ID) {
			rootID = pkg.ID
		}
	}

	if rootID == "" {
		return nil, fmt.Errorf("cargo metadata: no workspace package %s", root)
	}

	linked := map[string]bool{rootID: true}
	queue := []string{rootID}

	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]

		for _, dep := range deps[id] {
			if !linked[dep] {
				linked[dep] = true
				queue = append(queue, dep)
			}
		}
	}

	var comps []component

	for _, pkg := range meta.Packages {
		if !linked[pkg.ID] || pkg.Source == nil {
			continue
		}

		comp := component{
			Name: pkg.Name, Version: pkg.Version, License: deref(pkg.License), URL: deref(pkg.Repository),
			Dir: filepath.Dir(pkg.ManifestPath),
		}
		if pkg.LicenseFile != nil {
			comp.Extra = []string{*pkg.LicenseFile}
		}

		comps = append(comps, comp)
	}

	sortComponents(comps)

	return comps, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}

// moduleComponents parses the lines "path\tversion\tdir" of go list into the modules, without
// repetitions, sorted by path.
func moduleComponents(r io.Reader) ([]component, error) {
	seen := map[string]bool{}

	var comps []component

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || seen[line] {
			continue
		}

		seen[line] = true

		fields := strings.Split(line, "\t")
		if len(fields) != 3 || fields[0] == "" || fields[2] == "" {
			return nil, fmt.Errorf("go list: malformed line %q", line)
		}

		comps = append(comps, component{
			Name: fields[0], Version: fields[1], Dir: fields[2],
			URL: "https://pkg.go.dev/" + fields[0] + "@" + fields[1],
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	sortComponents(comps)

	return comps, nil
}

func sortComponents(comps []component) {
	slices.SortFunc(comps, func(a, b component) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}

		return strings.Compare(a.Version, b.Version)
	})
}

// licensePrefixes start the names of the licence and notice files of a package.
var licensePrefixes = []string{"LICENSE", "LICENCE", "COPYING", "COPYRIGHT", "NOTICE", "UNLICENSE", "PATENTS"}

// codeExtensions are extensions of source files that may start with a licence prefix
// (license.rs) and are not licence texts.
var codeExtensions = []string{".rs", ".go", ".c", ".h", ".py", ".sh", ".toml", ".json", ".yml", ".yaml", ".html"}

// licenseFiles returns the names of the licence files of comp: the files in its directory
// whose names start with a licence prefix, and its extra files, sorted.
func licenseFiles(comp component) ([]string, error) {
	entries, err := os.ReadDir(comp.Dir)
	if err != nil {
		return nil, err
	}

	names := slices.Clone(comp.Extra)
	isLicense := func(upper string) func(string) bool {
		return func(prefix string) bool { return strings.HasPrefix(upper, prefix) }
	}

	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || slices.Contains(names, name) ||
			slices.Contains(codeExtensions, strings.ToLower(filepath.Ext(name))) ||
			!slices.ContainsFunc(licensePrefixes, isLicense(strings.ToUpper(name))) {
			continue
		}

		names = append(names, name)
	}

	slices.Sort(names)

	return names, nil
}

// writeFile writes the notices of comps to path and warns about components without a licence file.
func writeFile(path, title string, comps []component) error {
	kept, err := fs.Sub(keptTexts, "extra")
	if err != nil {
		return err
	}

	var buf bytes.Buffer

	missing, err := writeNotices(&buf, title, comps, kept)
	if err != nil {
		return err
	}

	for _, id := range missing {
		fmt.Fprintf(os.Stderr, "notices: warning: %s has no licence file\n", id)
	}

	return os.WriteFile(path, buf.Bytes(), 0o644) //nolint:gosec // shipped in the packages, readable by all
}

// licenseText is one licence file of a component.
type licenseText struct{ Name, Text string }

// componentTexts returns the licence files of comp. When its package has none, it returns the
// texts kept for it in kept/<name>/ with a note, or else only a note naming a component of comps
// from the same repository that has licence files; no texts and no note mean none were found.
func componentTexts(comp component, comps []component, kept fs.FS) ([]licenseText, string, error) {
	files, err := licenseFiles(comp)
	if err != nil {
		return nil, "", err
	}

	if len(files) > 0 {
		texts, err := readTexts(os.DirFS(comp.Dir), ".", files)

		return texts, "", err
	}

	if entries, err := fs.ReadDir(kept, comp.Name); err == nil && len(entries) > 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}

		texts, err := readTexts(kept, comp.Name, names)

		return texts, "The package has no licence file; the texts below come from its source repository.", err
	}

	for _, other := range comps {
		if other.URL == "" || other.URL != comp.URL || other.Dir == comp.Dir {
			continue
		}

		if files, err := licenseFiles(other); err == nil && len(files) > 0 {
			return nil, fmt.Sprintf("The package has no licence file; the licence files of %s %s, from the same repository %s, cover it.",
				other.Name, other.Version, other.URL), nil
		}
	}

	return nil, "", nil
}

// readTexts reads the files names of dir in fsys, with CRLF line ends made LF and trailing
// blanks removed.
func readTexts(fsys fs.FS, dir string, names []string) ([]licenseText, error) {
	texts := make([]licenseText, 0, len(names))

	for _, name := range names {
		data, err := fs.ReadFile(fsys, path.Join(dir, filepath.ToSlash(name)))
		if err != nil {
			return nil, err
		}

		text := strings.TrimRight(strings.ReplaceAll(string(data), "\r\n", "\n"), " \t\n") + "\n"
		texts = append(texts, licenseText{Name: name, Text: text})
	}

	return texts, nil
}

// writeNotices writes title, the list of comps and then the licence files of each component;
// a text identical to an earlier one is named instead of repeated. Licence files are looked up
// as componentTexts does, with kept texts from kept. It returns the components that have no
// licence file.
func writeNotices(w io.Writer, title string, comps []component, kept fs.FS) ([]string, error) {
	var buf bytes.Buffer

	buf.WriteString(title + "\n\nComponents:\n\n")

	for _, comp := range comps {
		buf.WriteString("  " + comp.Name + " " + comp.Version)

		for _, s := range []string{comp.License, comp.URL} {
			if s != "" {
				buf.WriteString(" — " + s)
			}
		}

		buf.WriteString("\n")
	}

	buf.WriteString("\nThe licence files of the components follow in the order of the list; " +
		"a text identical to an earlier one is not repeated.\n")

	var missing []string

	seen := map[[sha256.Size]byte]string{}

	for _, comp := range comps {
		id := comp.Name + " " + comp.Version
		fmt.Fprintf(&buf, "\n%s\n%s\n%s\n", strings.Repeat("=", 80), id, strings.Repeat("-", 80))

		texts, note, err := componentTexts(comp, comps, kept)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", id, err)
		}

		if note != "" {
			buf.WriteString(note + "\n")
		}

		if len(texts) == 0 && note == "" {
			missing = append(missing, id)

			license := comp.License
			if license == "" {
				license = "unknown"
			}

			fmt.Fprintf(&buf, "No licence file in the package; its licence by the package metadata: %s.\n", license)

			continue
		}

		for _, text := range texts {
			key := sha256.Sum256([]byte(text.Text))
			if first, ok := seen[key]; ok {
				fmt.Fprintf(&buf, "\n%s: the same text as %s.\n", text.Name, first)

				continue
			}

			seen[key] = id + ", " + text.Name
			fmt.Fprintf(&buf, "\n%s:\n\n%s", text.Name, text.Text)
		}
	}

	if _, err := w.Write(buf.Bytes()); err != nil {
		return nil, err
	}

	return missing, nil
}
