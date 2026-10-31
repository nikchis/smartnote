// Command smartnote starts the Smartnote desktop application.
package main

import (
	"context"
	"errors"
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
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	logger, closeLog := openLog()
	defer closeLog()

	dataDir, err := platform.DataDir()
	if err != nil {
		return fmt.Errorf("data directory: %w", err)
	}

	// The lock comes before anything touches the storage files.
	release, err := platform.AcquireLock(dataDir)
	if errors.Is(err, platform.ErrLocked) {
		focusRunningInstance()
		return err
	}

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

	app, lifecycle, err := newApp(wailsui.Env{Ctx: ctx, Log: logger}, store, finish)
	if err != nil {
		return err
	}

	stopSignals := quitOnSignal(ctx, lifecycle)
	defer stopSignals()

	if err := app.Run(); err != nil {
		return fmt.Errorf("run application: %w", err)
	}

	return nil
}

// newApp builds the use cases over store, the Wails services and the window.
func newApp(env wailsui.Env, store *sqlite.Store, finish func()) (*application.App, *wailsui.Lifecycle, error) {
	assets, err := fs.Sub(frontend.Dist, "dist")
	if err != nil {
		return nil, nil, fmt.Errorf("frontend assets: %w", err)
	}

	services, lifecycle, err := newServices(env, store)
	if err != nil {
		return nil, nil, err
	}

	var window *application.WebviewWindow

	app := application.New(application.Options{
		Name:        appName,
		Description: "Encrypted notes with remote backup",
		Services:    services,
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		// SIGTERM and SIGINT quit without asking; see quitOnSignal.
		DisableDefaultSignalHandler: true,
		ShouldQuit:                  lifecycle.OnQuitRequested,
		OnShutdown:                  finish,
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

	return app, lifecycle, nil
}

// newServices wires the use cases over store into the Wails services.
func newServices(env wailsui.Env, store *sqlite.Store) ([]application.Service, *wailsui.Lifecycle, error) {
	// The local storage keeps milliseconds; a finer clock would make a saved
	// note differ from the one returned to the interface.
	clock := func() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }
	changes := &usecase.ChangeTracker{}
	gate := &usecase.Gate{}
	offer := &usecase.HostKeyOffer{}

	vault, err := usecase.NewVault(store)
	if err != nil {
		return nil, nil, err
	}

	notes, err := usecase.NewNotes(store, clock, uuid.New, changes)
	if err != nil {
		return nil, nil, err
	}

	connector, err := ssh.NewConnector(store)
	if err != nil {
		return nil, nil, err
	}

	syncer, err := usecase.NewSync(usecase.SyncDeps{
		Local: store, Settings: store, Remote: connector, Changes: changes, IDs: uuid.New, Gate: gate, Offer: offer,
		OnState: func(s usecase.State) { emit(wailsui.EventSyncState, wailsui.ToStateDTO(s)) },
		Warn:    func(err error) { wailsui.LogError(env.Log, "sync", err) },
	})
	if err != nil {
		return nil, nil, err
	}

	connection, err := usecase.NewConnection(store, connector, gate, offer, syncer.ResetConnection)
	if err != nil {
		return nil, nil, err
	}

	keys, err := usecase.NewKeys(store, ssh.KeyTool{})
	if err != nil {
		return nil, nil, err
	}

	lifecycle := wailsui.NewLifecycle(
		func() { emit(wailsui.EventCloseRequested) },
		func() { application.Get().Quit() },
	)

	return []application.Service{
		application.NewService(wailsui.NewVaultService(env, vault)),
		application.NewService(wailsui.NewNotesService(env, notes)),
		application.NewService(wailsui.NewSyncService(env, syncer)),
		application.NewService(wailsui.NewConnectionService(env, connection)),
		application.NewService(wailsui.NewKeyService(env, keys, wailsui.WailsFiles{})),
		application.NewService(wailsui.NewAppService(lifecycle)),
	}, lifecycle, nil
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

// focusRunningInstance hands over to the instance that holds the data
// directory: Wails' protection against a second instance brings its window
// forward and ends this process. It returns only if that instance has not
// taken Wails' lock yet.
func focusRunningInstance() {
	application.New(application.Options{
		Name:                        appName,
		DisableDefaultSignalHandler: true,
		SingleInstance:              &application.SingleInstanceOptions{UniqueID: instanceID},
	})
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
