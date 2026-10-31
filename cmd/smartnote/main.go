// Command smartnote starts the Smartnote desktop application.
package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/nikchis/smartnote/frontend"
	"github.com/nikchis/smartnote/internal/adapter/sqlite"
	"github.com/nikchis/smartnote/internal/adapter/ssh"
	"github.com/nikchis/smartnote/internal/adapter/wailsui"
	"github.com/nikchis/smartnote/internal/applog"
	"github.com/nikchis/smartnote/internal/platform"
	"github.com/nikchis/smartnote/internal/usecase"
)

const (
	// appName is the application name and the window title.
	appName = "Smartnote"
	// instanceID identifies Smartnote to Wails' protection against a second instance.
	instanceID = "com.github.nikchis.smartnote"
)

func main() {
	logger, closeLog := openLog()
	err := run(logger)
	logStartError(logger, err)
	closeLog()

	if err != nil {
		log.Fatal(err)
	}
}

func run(logger *slog.Logger) error {
	dataDir, err := platform.DataDir()
	if err != nil {
		return fmt.Errorf("data directory: %w", err)
	}

	assets, err := fs.Sub(frontend.Dist, "dist")
	if err != nil {
		return fmt.Errorf("frontend assets: %w", err)
	}

	lifecycle := wailsui.NewLifecycle(
		func() { emit(wailsui.EventCloseRequested) },
		func() { application.Get().Quit() },
	)

	// Wails' protection against a second instance comes first: a second
	// instance brings the running window forward and ends here. Taking the
	// data lock first would let a losing process claim Wails' name before the
	// winner and end both.
	app := newApp(lifecycle, assets)

	// The lock comes before anything touches the storage files. It still
	// fails if the protection above is unavailable or the previous instance
	// is still closing.
	release, err := platform.AcquireLock(dataDir)
	if err != nil {
		return err
	}

	defer func() { logClose(logger, "release lock", release()) }()

	store, err := sqlite.Open(dataDir)
	if err != nil {
		return fmt.Errorf("open local storage: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// finish is the one shutdown function of every quit path (spec §7.3): it
	// cancels running operations and closes the storage, which wipes its key.
	finish := sync.OnceFunc(func() {
		cancel()
		logClose(logger, "close local storage", store.Close())
	})
	defer finish()

	app.OnShutdown(finish)

	services, err := newServices(wailsui.Env{Ctx: ctx, Log: logger}, store, lifecycle)
	if err != nil {
		return err
	}

	// Services and the window come alive in Run, after this.
	for _, s := range services {
		app.RegisterService(s)
	}

	stopSignals := quitOnSignal(ctx, lifecycle)
	defer stopSignals()

	if err := app.Run(); err != nil {
		return fmt.Errorf("run application: %w", err)
	}

	return nil
}

// newApp creates the application with its window; services are registered
// later, once the storage is open.
func newApp(lifecycle *wailsui.Lifecycle, assets fs.FS) *application.App {
	var window *application.WebviewWindow

	app := application.New(application.Options{
		Name:        appName,
		Description: "Encrypted notes with remote backup",
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		// The end of a Windows session quits without the close question.
		Windows: application.WindowsOptions{WndProcInterceptor: lifecycle.OnWindowsMessage},
		// SIGTERM and SIGINT quit without asking; see quitOnSignal.
		DisableDefaultSignalHandler: true,
		// The end of a macOS session quits without the close question.
		ShouldQuit: func() bool { return lifecycle.OnAppQuit(sessionEnding()) },
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: instanceID,
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if window != nil {
					window.Restore()
					window.Show()
					window.Focus()
				}
			},
		},
	})

	window = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  appName,
		Width:  1000,
		Height: 640,
		URL:    "/",
	})

	// Closing the window runs the close scenario in the interface first.
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if !lifecycle.OnQuitRequested() {
			e.Cancel()
		}
	})

	return app
}

// newServices wires the use cases over store into the Wails services.
func newServices(env wailsui.Env, store *sqlite.Store, lifecycle *wailsui.Lifecycle) ([]application.Service, error) {
	// The local storage keeps milliseconds; a finer clock would make a saved
	// note differ from the one returned to the interface.
	clock := func() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }
	changes := &usecase.ChangeTracker{}
	gate := &usecase.Gate{}
	offer := &usecase.HostKeyOffer{}

	vault, err := usecase.NewVault(store)
	if err != nil {
		return nil, err
	}

	notes, err := usecase.NewNotes(store, clock, uuid.New, changes)
	if err != nil {
		return nil, err
	}

	connector, err := ssh.NewConnector(store)
	if err != nil {
		return nil, err
	}

	syncer, err := usecase.NewSync(usecase.SyncDeps{
		Local: store, Settings: store, Remote: connector, Changes: changes, IDs: uuid.New, Gate: gate, Offer: offer,
		OnState: func(s usecase.State) { emit(wailsui.EventSyncState, wailsui.ToStateDTO(s)) },
		Warn:    func(err error) { wailsui.LogError(env.Log, "sync", err) },
	})
	if err != nil {
		return nil, err
	}

	connection, err := usecase.NewConnection(store, connector, gate, offer, syncer.ResetConnection)
	if err != nil {
		return nil, err
	}

	keys, err := usecase.NewKeys(store, ssh.KeyTool{})
	if err != nil {
		return nil, err
	}

	return []application.Service{
		application.NewService(wailsui.NewVaultService(env, vault)),
		application.NewService(wailsui.NewNotesService(env, notes)),
		application.NewService(wailsui.NewSyncService(env, syncer)),
		application.NewService(wailsui.NewConnectionService(env, connection)),
		application.NewService(wailsui.NewKeyService(env, keys, wailsui.WailsFiles{})),
		application.NewService(wailsui.NewAppService(lifecycle)),
	}, nil
}

// emit sends an event to the interface of the running application.
func emit(name string, data ...any) {
	if app := application.Get(); app != nil {
		app.Event.Emit(name, data...)
	}
}

// quitOnSignal quits without asking on SIGTERM and SIGINT until ctx ends.
func quitOnSignal(ctx context.Context, lifecycle *wailsui.Lifecycle) (stop func()) {
	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)

	go func() {
		select {
		case <-sigCtx.Done():
			lifecycle.ForceQuit()
		case <-ctx.Done():
		}
	}()

	return stop
}

// openLog opens the error log in the state directory. Without it the app
// still runs and logs to stderr.
func openLog() (logger *slog.Logger, closeLog func()) {
	stderr := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	dir, err := platform.StateDir()
	if err != nil {
		stderr.Error("state directory", applog.Err(err))
		return stderr, func() {}
	}

	file, closer, err := applog.Open(dir)
	if err != nil {
		stderr.Error("open log", applog.Err(err))
		return stderr, func() {}
	}

	return file, func() { closeQuietly(closer) }
}

// logStartError writes a failed start to the error log: started from the
// desktop menu, stderr goes nowhere.
func logStartError(logger *slog.Logger, err error) {
	if err != nil {
		logger.Error("start", applog.Err(err))
	}
}

// logClose logs err, if any, from a step of shutting down.
func logClose(logger *slog.Logger, msg string, err error) {
	if err != nil {
		logger.Error(msg, applog.Err(err))
	}
}

func closeQuietly(c io.Closer) {
	if err := c.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "close log: %v\n", err)
	}
}
