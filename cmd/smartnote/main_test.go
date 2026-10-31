package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"syscall"
	"testing"
)

func TestLogStartError(t *testing.T) {
	var buf bytes.Buffer

	logStartError(slog.New(slog.NewTextHandler(&buf, nil)), fmt.Errorf("create data directory: %w", syscall.ENOTDIR))

	if got := buf.String(); !strings.Contains(got, "level=ERROR") || !strings.Contains(got, "not a directory") {
		t.Errorf("log = %q, want the start error", got)
	}
}

func TestLogStartErrorNil(t *testing.T) {
	var buf bytes.Buffer

	logStartError(slog.New(slog.NewTextHandler(&buf, nil)), nil)

	if buf.Len() != 0 {
		t.Errorf("log = %q, want nothing after a clean exit", buf.String())
	}
}
