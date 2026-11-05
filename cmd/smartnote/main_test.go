package main

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"syscall"
	"testing"

	"github.com/nikchis/smartnote/internal/adapter/sqlite"
	"github.com/nikchis/smartnote/internal/adapter/wailsui"
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

func TestWithoutURLHidesTheURL(t *testing.T) {
	const url = "https://oauth.yandex.ru/authorize?state=SECRET"

	err := withoutURL(func(u string) error { return fmt.Errorf("exec xdg-open %s: failed", u) })(url)
	if !errors.Is(err, errOpenBrowser) || strings.Contains(err.Error(), "SECRET") {
		t.Errorf("err = %v, want the fixed error without the URL", err)
	}

	if err := withoutURL(func(string) error { return nil })(url); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}

func TestNewServicesWires(t *testing.T) {
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	logger := slog.New(slog.DiscardHandler)
	engine, status := newSearchEngine(logger, findSearch)

	services, err := newServices(wailsui.Env{Ctx: t.Context(), Log: logger}, store, wailsui.NewLifecycle(func() {}, func() {}), searchParts{
		engine: engine, status: status, run: newSearchRunner(t.Context(), logger),
	})
	if err != nil || len(services) == 0 {
		t.Fatalf("newServices = %d services, %v", len(services), err)
	}

	t.Cleanup(func() { _ = engine.Close() })
}
