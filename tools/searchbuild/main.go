// Command searchbuild builds the search worker smartnote-search in release mode for one Rust
// target and copies the executable into a directory. Cargo runs every time, so a stale worker
// is never packaged; it finds the executable through its JSON messages, so CARGO_TARGET_DIR
// and the other cargo settings of the environment (RUSTUP_TOOLCHAIN too) are honoured.
//
// Usage: go run ./tools/searchbuild -target x86_64-unknown-linux-gnu [-search search] [-out bin]
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
)

// workerBin is the cargo binary target of the worker (package worker).
const workerBin = "smartnote-search"

func main() {
	target := flag.String("target", "", "Rust target triple, e.g. x86_64-unknown-linux-gnu")
	search := flag.String("search", "search", "Cargo workspace of the worker")
	out := flag.String("out", "bin", "directory to copy the executable to")

	flag.Parse()

	if *target == "" || flag.NArg() > 0 {
		flag.Usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	dst, err := run(ctx, *search, *target, *out)

	stop()

	if err != nil {
		fmt.Fprintln(os.Stderr, "searchbuild:", err)
		os.Exit(1)
	}

	fmt.Println("built", dst)
}

// run builds the worker for target in the workspace search and copies it into out.
func run(ctx context.Context, search, target, out string) (string, error) {
	cmd := exec.CommandContext(ctx, "cargo", "build", "--locked", "--release",
		"-p", "worker", "--bin", workerBin, "--target", target,
		"--message-format=json-render-diagnostics")
	cmd.Dir = search
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}

	if err := cmd.Start(); err != nil {
		return "", err
	}

	src, parseErr := workerArtifact(stdout)
	// Drain what is left so cargo never blocks on a full pipe.
	_, _ = io.Copy(io.Discard, stdout)

	if err := cmd.Wait(); err != nil {
		return "", fmt.Errorf("cargo build: %w", err)
	}

	if parseErr != nil {
		return "", parseErr
	}

	return copyExecutable(src, out)
}

// message is the part of a cargo JSON message that names a built executable.
type message struct {
	Reason string `json:"reason"`
	Target struct {
		Name string `json:"name"`
	} `json:"target"`
	Executable *string `json:"executable"`
}

// workerArtifact returns the path of the worker executable from cargo's JSON messages.
func workerArtifact(r io.Reader) (string, error) {
	var path string

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), 16<<20)

	for scanner.Scan() {
		var msg message
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			return "", fmt.Errorf("cargo message: %w", err)
		}

		if msg.Reason == "compiler-artifact" && msg.Target.Name == workerBin && msg.Executable != nil {
			path = *msg.Executable
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	if path == "" {
		return "", errors.New("cargo reported no " + workerBin + " executable")
	}

	return path, nil
}

// copyExecutable copies src into the directory out under its own name, replacing an older
// copy only once the new one is complete, and returns the new path.
func copyExecutable(src, out string) (dst string, err error) {
	// Readable by all like a directory go build creates: other users read bin (the AppImage smoke).
	if err := os.MkdirAll(out, 0o755); err != nil {
		return "", err
	}

	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()

	dst = filepath.Join(out, filepath.Base(src))
	partial := dst + ".part"

	file, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return "", err
	}

	defer func() {
		if err != nil {
			_ = os.Remove(partial)
		}
	}()

	_, err = io.Copy(file, in)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}

	if err == nil {
		// The mode of OpenFile is subject to the umask; the worker must be executable by all.
		err = os.Chmod(partial, 0o755)
	}

	if err == nil {
		err = os.Rename(partial, dst)
	}

	if err != nil {
		return "", err
	}

	return dst, nil
}
