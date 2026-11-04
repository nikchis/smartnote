package searchworker

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/applog"
	"github.com/nikchis/smartnote/internal/usecase"
)

// stderrSource marks the log records of the worker's stderr.
const stderrSource = "search-worker"

const (
	// maxStderrLine is the longest stderr line logged; the rest is cut off.
	maxStderrLine = 4 << 10
	// maxStderrLines is how many stderr lines of one process are logged.
	maxStderrLines = 1000
)

var (
	// errExited means the worker closed its stdout, by exiting or otherwise.
	errExited = errors.New("worker exited")
	// errProtocol means the worker broke the protocol.
	errProtocol = errors.New("protocol violation")
)

// workerError is an error message of the worker without a request: the worker
// failed as a whole and exits.
type workerError struct {
	code, msg string
}

func (e *workerError) Error() string { return fmt.Sprintf("worker error %s: %s", e.code, e.msg) }

// permanent reports whether a restart cannot help: the worker file cannot be
// run, or it speaks another protocol version.
func permanent(err error) bool {
	if we, ok := errors.AsType[*workerError](err); ok {
		return we.code == codeUnsupportedProto
	}

	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission)
}

// workerEnv is the environment of the worker: the parent's without the
// variables of ONNX Runtime and of code injection through the dynamic loader,
// with telemetry of ONNX Runtime off (spec §6.1). LD_LIBRARY_PATH stays: an
// AppImage sets it for its bundled libraries, and the worker and ONNX Runtime
// themselves are given by absolute path, the library with a pinned digest.
func workerEnv(environ []string) []string {
	env := make([]string, 0, len(environ)+1)

	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")

		// Names on Windows ignore case; elsewhere dropping more is harmless.
		upper := strings.ToUpper(name)
		if strings.HasPrefix(upper, "ORT_") || upper == "LD_PRELOAD" || upper == "LD_AUDIT" || strings.HasPrefix(upper, "DYLD_") {
			continue
		}

		env = append(env, kv)
	}

	return append(env, "ORT_DISABLE_TELEMETRY=1")
}

// generation is one worker process and the goroutines that serve it. Its
// replies and events carry its number n; the engine drops those of earlier
// processes.
//
// Goroutines, all waited for by wg:
//   - run starts the process on a locked OS thread and waits for it (spec
//     §8.1: PR_SET_PDEATHSIG follows the thread that started the child);
//   - read reads stdout and never waits for anybody: replies go to the
//     channel of their request (capacity 1), embedded events to the event
//     queue, status to its latest-value cell;
//   - write is the only writer of stdin;
//   - logStderr logs stderr line by line.
type generation struct {
	n           uint64
	log         *slog.Logger
	stderrLevel func(line string) slog.Level
	queue       *eventQueue

	cmd    *exec.Cmd
	writes chan writeReq
	ready  chan *readyMsg // the answer to hello, capacity 1

	mu      sync.Mutex
	pending map[uint64]chan any // the reply channels of the requests, capacity 1
	helloed bool                // ready was received

	dead     chan struct{} // closed at the first failure: the process cannot serve any more
	deadOnce sync.Once
	cause    error // why dead was closed; written once before closing it

	exited  chan struct{} // closed when the process has exited and was waited for
	exitErr error         // the result of Wait; written before closing exited

	stopping chan struct{} // closed to close stdin: the graceful stop
	stopOnce sync.Once

	wg sync.WaitGroup
}

// writeReq is a frame for stdin and the channel for the result of writing
// it (capacity 1).
type writeReq struct {
	frame []byte
	done  chan error
}

// startGeneration starts the worker at path with env as generation n. Only
// the three pipes are passed to the child; Go creates them close-on-exec, so
// no other child inherits them.
func startGeneration(n uint64, path string, env []string, cfg *Config, queue *eventQueue) (*generation, error) {
	g := &generation{
		n:           n,
		log:         cfg.Log,
		stderrLevel: cfg.StderrLevel,
		queue:       queue,
		writes:      make(chan writeReq),
		ready:       make(chan *readyMsg, 1),
		pending:     map[uint64]chan any{},
		dead:        make(chan struct{}),
		exited:      make(chan struct{}),
		stopping:    make(chan struct{}),
	}

	pipes, err := newPipes()
	if err != nil {
		return nil, err
	}

	g.cmd = exec.Command(path) //nolint:noctx // ends by closing stdin, then Kill after a grace period
	g.cmd.Env = env
	g.cmd.Stdin = pipes.stdinR
	g.cmd.Stdout = pipes.stdoutW
	g.cmd.Stderr = pipes.stderrW
	g.cmd.SysProcAttr = sysProcAttr()

	started := make(chan error, 1)

	g.wg.Go(func() { g.run(started) })

	err = <-started

	pipes.closeChildEnds()

	if err != nil {
		pipes.closeParentEnds()
		g.wg.Wait()

		return nil, fmt.Errorf("start worker: %w", err)
	}

	g.wg.Go(func() { g.read(pipes.stdoutR) })
	g.wg.Go(func() { g.write(pipes.stdinW) })
	g.wg.Go(func() { g.logStderr(pipes.stderrR) })

	return g, nil
}

// pipes are the three pipes of a worker process.
type pipes struct {
	stdinR, stdinW, stdoutR, stdoutW, stderrR, stderrW *os.File
}

func newPipes() (*pipes, error) {
	var (
		p   pipes
		err error
	)

	if p.stdinR, p.stdinW, err = os.Pipe(); err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	if p.stdoutR, p.stdoutW, err = os.Pipe(); err != nil {
		closeAll(p.stdinR, p.stdinW)
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if p.stderrR, p.stderrW, err = os.Pipe(); err != nil {
		closeAll(p.stdinR, p.stdinW, p.stdoutR, p.stdoutW)
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	return &p, nil
}

// closeChildEnds closes the parent's copies of the child's ends, so the
// parent sees EOF on stdout and stderr when the child exits.
func (p *pipes) closeChildEnds() { closeAll(p.stdinR, p.stdoutW, p.stderrW) }

func (p *pipes) closeParentEnds() { closeAll(p.stdinW, p.stdoutR, p.stderrR) }

func closeAll(files ...*os.File) {
	for _, f := range files {
		_ = f.Close() // a pipe end: nothing to flush
	}
}

// run starts the process and waits for it, on one locked OS thread.
func (g *generation) run(started chan<- error) {
	defer close(g.exited)

	runtime.LockOSThread()

	defer runtime.UnlockOSThread()

	if err := g.cmd.Start(); err != nil {
		g.exitErr = err
		started <- err

		return
	}

	started <- nil

	g.exitErr = g.cmd.Wait()
}

// fail records cause as the reason the generation cannot serve and closes
// dead; only the first cause counts.
func (g *generation) fail(cause error) {
	g.deadOnce.Do(func() {
		g.cause = cause
		close(g.dead)
	})
}

// failure returns the cause; call it only after dead is closed.
func (g *generation) failure() error { return g.cause }

// kill fails the generation with cause and kills the process.
func (g *generation) kill(cause error) {
	g.fail(cause)

	// The process may have exited already; Kill is safe then and reports it.
	if g.cmd.Process != nil {
		_ = g.cmd.Process.Kill()
	}
}

// shutdown stops the process gracefully: it closes stdin and gives the worker
// grace to exit, then kills it (spec §3.3 rule 9). It waits for all
// goroutines of the generation.
func (g *generation) shutdown(grace time.Duration) {
	g.stopOnce.Do(func() { close(g.stopping) })

	timer := time.NewTimer(grace)
	defer timer.Stop()

	select {
	case <-g.exited:
	case <-timer.C:
	}

	g.kill(errExited)
	g.wg.Wait()
}

// teardown kills the process and waits for all goroutines of the generation.
func (g *generation) teardown() {
	g.kill(errExited)
	g.wg.Wait()
}

// read reads and routes the frames of stdout until EOF or a protocol error.
func (g *generation) read(stdout *os.File) {
	defer func() { _ = stdout.Close() }()

	r := bufio.NewReaderSize(stdout, 64<<10)

	for {
		body, err := readFrame(r)
		if errors.Is(err, errStreamEnd) {
			g.fail(errExited)
			return
		}

		if err != nil {
			g.kill(fmt.Errorf("%w: %w", errProtocol, err))
			return
		}

		msg, err := decodeReply(body)
		if err != nil {
			g.kill(fmt.Errorf("%w: %w", errProtocol, err))
			return
		}

		if err := g.route(msg); err != nil {
			g.kill(err)
			return
		}
	}
}

// route passes one message on without waiting for anybody.
func (g *generation) route(msg any) error {
	switch m := msg.(type) {
	case *readyMsg:
		return g.routeReady(m)
	case *okMsg:
		g.deliver(m.Req, m)
	case *resultMsg:
		g.deliver(m.Req, m)
	case *errorMsg:
		if m.Req == nil {
			return &workerError{code: m.Code, msg: m.Msg}
		}

		g.deliver(*m.Req, m)
	case *embeddedMsg:
		id, err := uuid.Parse(m.ID)
		if err != nil {
			return fmt.Errorf("%w: embedded note id", errProtocol)
		}

		g.queue.push(g.n, usecase.SearchEvent{Embedded: &usecase.EmbeddedEvent{ID: id, Rev: m.Rev, Data: m.Emb}})
	case *statusMsg:
		g.queue.setStatus(g.n, usecase.SearchStatus{
			Notes:    int(m.Notes),
			Queued:   int(m.Queued),
			Lexical:  usecase.SearchState(m.Lexical),
			Semantic: usecase.SearchState(m.Semantic),
		})
	default:
		return fmt.Errorf("%w: unexpected %T", errProtocol, msg)
	}

	return nil
}

func (g *generation) routeReady(m *readyMsg) error {
	g.mu.Lock()
	again := g.helloed
	g.helloed = true
	g.mu.Unlock()

	if again {
		return fmt.Errorf("%w: second ready", errProtocol)
	}

	// The status of ready goes in order with the statuses after it; the
	// semantic search may still be loading.
	g.queue.setStatus(g.n, usecase.SearchStatus{Lexical: usecase.SearchState(m.Lexical), Semantic: usecase.SearchState(m.Semantic)})

	g.ready <- m // capacity 1, the only send

	return nil
}

// deliver hands the reply to request req to its waiter. A reply nobody waits
// for any more (the request timed out or was canceled) is dropped.
func (g *generation) deliver(req uint64, reply any) {
	g.mu.Lock()
	ch, ok := g.pending[req]
	delete(g.pending, req)
	g.mu.Unlock()

	if ok {
		ch <- reply // capacity 1, the only send
	}
}

// expect registers request req before it is sent.
func (g *generation) expect(req uint64) chan any {
	ch := make(chan any, 1)

	g.mu.Lock()
	g.pending[req] = ch
	g.mu.Unlock()

	return ch
}

// forget drops the registration of request req.
func (g *generation) forget(req uint64) {
	g.mu.Lock()
	delete(g.pending, req)
	g.mu.Unlock()
}

// write is the only writer of stdin. It closes stdin when the generation
// stops or fails, so a worker still alive reads EOF.
func (g *generation) write(stdin *os.File) {
	defer func() { _ = stdin.Close() }()

	for {
		select {
		case w := <-g.writes:
			_, err := stdin.Write(w.frame)
			w.done <- err // capacity 1

			if err != nil {
				g.kill(fmt.Errorf("write stdin: %w", err))
				return
			}
		case <-g.stopping:
			return
		case <-g.dead:
			return
		}
	}
}

// send passes frame to the writer and waits until it is written, until
// expired fires, ctx ends or the generation fails.
func (g *generation) send(ctx context.Context, expired <-chan time.Time, frame []byte) error {
	w := writeReq{frame: frame, done: make(chan error, 1)}

	select {
	case g.writes <- w:
	case <-g.dead:
		return g.failure()
	case <-expired:
		return errTimeout
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case err := <-w.done:
		if err != nil {
			return fmt.Errorf("write stdin: %w", err)
		}

		return nil
	case <-g.dead:
		return g.failure()
	case <-expired:
		return errTimeout
	case <-ctx.Done():
		return ctx.Err()
	}
}

// hello greets the worker and waits for ready within timeout or until ctx
// ends.
func (g *generation) hello(ctx context.Context, msg *helloMsg, timeout time.Duration) (*readyMsg, error) {
	frame, err := encodeFrame(msg)
	if err != nil {
		return nil, err
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	if err := g.send(ctx, timer.C, frame); err != nil {
		return nil, fmt.Errorf("hello: %w", err)
	}

	select {
	case r := <-g.ready:
		return r, nil
	case <-g.dead:
		select {
		case r := <-g.ready:
			return r, nil
		default:
			return nil, fmt.Errorf("hello: %w", g.failure())
		}
	case <-timer.C:
		return nil, fmt.Errorf("hello: %w", errTimeout)
	case <-ctx.Done():
		return nil, fmt.Errorf("hello: %w", ctx.Err())
	}
}

// call sends msg as request req and waits for its reply within timeout,
// until ctx ends or the generation fails.
func (g *generation) call(ctx context.Context, req uint64, msg any, timeout time.Duration) (any, error) {
	frame, err := encodeFrame(msg)
	if err != nil {
		return nil, err
	}

	reply := g.expect(req)
	defer g.forget(req)

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	if err := g.send(ctx, timer.C, frame); err != nil {
		return nil, err
	}

	select {
	case r := <-reply:
		return r, nil
	case <-g.dead:
		select {
		case r := <-reply: // answered just before failing
			return r, nil
		default:
			return nil, g.failure()
		}
	case <-timer.C:
		return nil, errTimeout
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// logStderr logs the lines of stderr until EOF. The worker writes codes,
// counts and timings there, never note text (spec §6.2).
func (g *generation) logStderr(stderr *os.File) {
	defer func() { _ = stderr.Close() }()

	r := bufio.NewReaderSize(stderr, maxStderrLine)

	for lines := 0; ; lines++ {
		line, err := r.ReadSlice('\n')
		if len(line) > 0 {
			g.logLine(lines, line, errors.Is(err, bufio.ErrBufferFull))
		}

		for errors.Is(err, bufio.ErrBufferFull) { // the rest of a long line
			_, err = r.ReadSlice('\n')
		}

		if err != nil {
			return
		}
	}
}

func (g *generation) logLine(index int, line []byte, cut bool) {
	switch {
	case index < maxStderrLines:
		text := strings.TrimRight(string(line), "\r\n")
		attrs := []slog.Attr{slog.String("source", stderrSource), slog.Uint64("gen", g.n), slog.String("line", text)}

		if cut {
			attrs = append(attrs, slog.Bool("cut", true))
		}

		g.log.LogAttrs(context.Background(), g.stderrLevel(text), "search worker stderr", attrs...)
	case index == maxStderrLines:
		g.log.Error("search worker stderr: further lines dropped", slog.String("source", stderrSource), slog.Uint64("gen", g.n))
	}
}

// exitStatus describes how the process ended; call it after wg.Wait.
func (g *generation) exitStatus() string {
	if g.exitErr == nil {
		return "exit status 0"
	}

	return g.exitErr.Error()
}

// logFailure logs why generation g stopped.
func (g *generation) logFailure() {
	g.log.Error("search worker failed", slog.Uint64("gen", g.n), applog.Err(g.failure()), slog.String("exit", g.exitStatus()))
}

// modelLoadedPrefix starts the one informational stderr line of the worker:
// "model loaded in N ms" (search/worker/src/worker.rs).
const modelLoadedPrefix = "model loaded in "

// defaultStderrLevel is the level of a stderr line when Config.StderrLevel is
// nil: the worker writes there what the error log must keep (panics, sandbox
// below full, open descriptors, model and ONNX Runtime failures), except the
// timing of the model load, which is Info.
func defaultStderrLevel(line string) slog.Level {
	if strings.HasPrefix(line, modelLoadedPrefix) {
		return slog.LevelInfo
	}

	return slog.LevelError
}
