package searchworker

import (
	"context"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// What the worker processes write, their stderr lines and the msg of their
// error replies, reaches smartnote.log as a closed vocabulary within a budget
// per Engine, that is per run of the application (spec §10.2):
//
//   - a stderr line is logged only when it fully matches one of the forms
//     below, the lines the worker itself writes; the logged text is fixed
//     but for numbers (OS error numbers, descriptor counts, protocol
//     versions, sizes, timings, the line and column of a panic) and choices
//     among fixed words (sandbox gaps, model errors, the source file of a
//     panic from workerSourceFiles). Free text in a line (an OS error
//     message, an ONNX Runtime location, a file outside the worker) is never
//     logged. Any other line is logged as "unrecognized" with its length;
//   - the msg of an error reply is shown only when it is one of
//     knownErrorTexts (protocol.go), else the code alone;
//   - all of it uses one budget per run: stderrBudget stderr records and
//     errorTextBudget error texts, plus per-form limits; restarts do not
//     renew it. Past the budget one line says so and the rest is counted;
//     Close logs the counts.
//
// What is guaranteed: no text the worker chooses reaches the log, and what it
// chooses (numbers, choices among fixed texts, line lengths) is at most about
// 0.6 KB per run: at most about 72 bits per stderr record (the densest forms
// carry two numbers of up to 10 digits, about 67 bits, plus the choice of the
// form) times 64, about 580 bytes; under 3 bits per error text times 64, 24
// bytes; the two counts at Close, under 16 bytes. Not covered: the code of an error reply (one of four,
// shown with every failed command), the exit status and the timing of a
// failed process, which the restart policy bounds per 10 minutes, not per run.
//
// Contract with search/worker/src: every eprintln! of the worker has its
// format listed in the sources of a form here, and workerSourceFiles lists
// its source files; TestStderrFormsCoverWorker fails until a new one is
// added. Lines written otherwise (the panic hook, the logger of ONNX Runtime,
// the allocation failure of std) have forms without sources.

// Budgets of the worker's output per run of the application.
const (
	stderrBudget    = 64 // stderr records
	errorTextBudget = 64 // texts of error replies
	// panicsPerRun is how many panic lines a run logs; a process writes one.
	panicsPerRun = 8
	// ortLinesPerRun is how many lines of the ONNX Runtime logger a run logs.
	ortLinesPerRun = 8
)

// stderrForm is one kind of line the worker writes to stderr.
type stderrForm struct {
	// sources are the eprintln! formats of the worker that write this form.
	sources []string
	// re matches the whole line.
	re *regexp.Regexp
	// render gives the logged text from the submatches; nil logs the line,
	// which then must hold no free text.
	render func(m []string) string
	// info logs the form at level Info, which the error log drops; the
	// others are Error.
	info bool
	// most is how many lines of the form a run logs; 0 is no limit besides
	// stderrBudget.
	most int
}

// Fixed texts of the worker's errors: the Display of ModelError
// (search/worker/src/embed.rs) and of the io::Error values of harden.rs.
const (
	modelErrRe = `(?:reading a model file failed` +
		`|SHA-256 of (?:model\.onnx|tokenizer\.kit|manifest\.json|onnxruntime) differs from the pinned digest` +
		`|manifest\.json is invalid|the ONNX Runtime library path is not absolute` +
		`|no ONNX Runtime library is pinned for this target|loading the ONNX Runtime library failed` +
		`|ORT_DISABLE_TELEMETRY is not 1|the ONNX Runtime library is not loaded|tokenizer failed` +
		`|ONNX Runtime failed|unexpected model inputs or outputs)`
	// ioErrRe is an io::Error: an OS error, whose message the log drops,
	// or one of the worker's own texts.
	ioErrRe = `(?:.* \(os error (-?\d{1,11})\)|(parent process has changed|out of range integral type conversion attempted))`
	gapRe   = `(?:no_new_privs|landlock_fs|landlock_net|seccomp_arch|seccomp|threads|unsupported_os)`
)

// renderIOErr logs prefix with the number of an OS error or the worker's own
// text.
func renderIOErr(prefix string) func(m []string) string {
	return func(m []string) string {
		if m[1] != "" {
			return prefix + "os error " + m[1]
		}

		return prefix + m[2]
	}
}

// workerStderrForms are the lines of search/worker/src (see above).
var workerStderrForms = []stderrForm{
	{
		sources: []string{"closing inherited descriptors failed: {err}"},
		re:      full(`closing inherited descriptors failed: ` + ioErrRe),
		render:  renderIOErr("closing inherited descriptors failed: "),
	},
	{
		sources: []string{"harden failed: {err}"},
		re:      full(`harden failed: ` + ioErrRe),
		render:  renderIOErr("harden failed: "),
	},
	{
		sources: []string{"unsupported protocol version {proto}, expected {PROTO}"},
		re:      full(`unsupported protocol version \d{1,10}, expected \d{1,10}`),
	},
	{
		sources: []string{"bad request: {}"},
		re:      full(`bad request: (?:malformed frame|malformed message|expected hello|unexpected hello)`),
	},
	{sources: []string{"stdin read failed"}, re: full(`stdin read failed`)},
	{sources: []string{"stdout write failed"}, re: full(`stdout write failed`)},
	{
		sources: []string{"starting the {name} thread failed"},
		re:      full(`starting the (?:writer|embed|index) thread failed`),
	},
	{
		sources: []string{
			"semantic search unavailable: model files: {err}",
			"semantic search unavailable: onnxruntime: {err}",
			"semantic search unavailable: {err}",
			"semantic search unavailable: model: {err}",
		},
		re: full(`semantic search unavailable: (?:(?:model files|onnxruntime|model): )?` + modelErrRe),
	},
	{
		sources: []string{"semantic search unavailable: the CPU has no AVX2"},
		re:      full(`semantic search unavailable: the CPU has no AVX2`),
	},
	{
		sources: []string{"sandbox {level}: {}"},
		re:      full(`sandbox (?:partial|none): (?:` + gapRe + `(?: ` + gapRe + `){0,6})?`),
	},
	{
		sources: []string{"sandbox: {} descriptors besides 0, 1, 2 are open, {} writable"},
		re:      full(`sandbox: \d{1,10} descriptors besides 0, 1, 2 are open, \d{1,10} writable`),
	},
	{
		sources: []string{"sandbox: open descriptors cannot be listed"},
		re:      full(`sandbox: open descriptors cannot be listed`),
	},
	{
		sources: []string{"query vector timed out after {QUERY_VECTOR_WAIT:?}"},
		re:      full(`query vector timed out after \d{1,6}(?:\.\d{1,9})?(?:s|ms|µs|ns)`),
	},
	{
		// The timing of the model load: not a failure, the error log drops it.
		sources: []string{"model loaded in {} ms"},
		re:      full(`model loaded in \d{1,10} ms`),
		info:    true,
	},
	{
		sources: []string{"query embedding failed: {err}", "note embedding failed: {err}"},
		re:      full(`(?:query|note) embedding failed: ` + modelErrRe),
	},
	{
		// The logger of ONNX Runtime (embed/model.rs): "onnxruntime <level>
		// at <location>"; the location is left out.
		sources: []string{"{line}"},
		re:      full(`onnxruntime (error|fatal) at .+`),
		render:  func(m []string) string { return "onnxruntime " + m[1] },
		most:    ortLinesPerRun,
	},
	{
		// The panic hook (panic.rs) writes one line, and the process aborts.
		re:     full(`panic at (?:(<unknown>)|(.+):(\d{1,9}):(\d{1,9}))`),
		render: renderPanic,
		most:   panicsPerRun,
	},
	{
		// The allocation failure of std, before it aborts the process.
		re:   full(`memory allocation of \d{1,20} bytes failed`),
		most: panicsPerRun,
	},
}

// workerSourceFiles are the source files of the worker, relative to
// search/worker/src: the files a panic place may name.
var workerSourceFiles = []string{
	"embed.rs", "embed/chunk.rs", "embed/files.rs", "embed/model.rs", "embed/record.rs",
	"engine.rs", "harden.rs", "index.rs", "index/lexical.rs", "index/substring.rs",
	"lib.rs", "main.rs", "panic.rs", "protocol.rs", "protocol/frame.rs", "protocol/messages.rs",
	"rank.rs", "sandbox.rs", "text.rs", "text/form.rs", "text/query.rs", "worker.rs",
}

// renderPanic logs the place of a panic: the source file when it is one of
// the worker's, as Rust writes it from the workspace (worker/src/…), or from
// src/, with / or \ separators and any directory before; else <other>.
func renderPanic(m []string) string {
	if m[1] != "" {
		return "panic at <unknown>"
	}

	file := "<other>"

	p := strings.ReplaceAll(m[2], `\`, "/")
	if i := strings.LastIndex(p, "worker/src/"); i >= 0 && (i == 0 || p[i-1] == '/') {
		p = p[i+len("worker/src/"):]
	} else {
		p = strings.TrimPrefix(p, "src/")
	}

	if slices.Contains(workerSourceFiles, p) {
		file = "worker/src/" + p
	}

	return "panic at " + file + ":" + m[3] + ":" + m[4]
}

func full(re string) *regexp.Regexp { return regexp.MustCompile(`^` + re + `$`) }

// renderStderr returns the form of line, the text to log and its level;
// false when the line matches no form.
func renderStderr(forms []stderrForm, line string) (form int, text string, level slog.Level, ok bool) {
	for i, f := range forms {
		m := f.re.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		level = slog.LevelError
		if f.info {
			level = slog.LevelInfo
		}

		if f.render == nil {
			return i, line, level, true
		}

		return i, f.render(m), level, true
	}

	return 0, "", slog.LevelError, false
}

// workerOutput logs the output of the worker processes of one Engine within
// the budgets of a run (see the top of the file). It is safe for concurrent
// use.
type workerOutput struct {
	log   *slog.Logger
	forms []stderrForm

	mu           sync.Mutex
	seen         []int // stderr records per form
	lines, texts int   // stderr records and error texts logged
	droppedLines int
	droppedTexts int
	noticed      bool // the line about dropped output is logged
}

func newWorkerOutput(log *slog.Logger, forms []stderrForm) *workerOutput {
	return &workerOutput{log: log, forms: forms, seen: make([]int, len(forms))}
}

// line logs a stderr line of generation gen, cut when it was longer than the
// longest line read, or counts it once the budget is spent.
func (o *workerOutput) line(gen uint64, raw string, cut bool) {
	o.mu.Lock()
	defer o.mu.Unlock()

	attrs := []slog.Attr{slog.String("source", stderrSource), slog.Uint64("gen", gen)}

	form, text, level, ok := renderStderr(o.forms, raw)
	ok = ok && !cut

	switch {
	case o.lines >= stderrBudget, ok && o.forms[form].most > 0 && o.seen[form] >= o.forms[form].most:
		o.droppedLines++
		o.noticeLocked()

		return
	case !ok:
		attrs = append(attrs, slog.Int("len", len(raw)))
		if cut {
			attrs = append(attrs, slog.Bool("cut", true))
		}

		o.log.LogAttrs(context.Background(), slog.LevelError, "search worker stderr: unrecognized line", attrs...)
	default:
		o.seen[form]++
		o.log.LogAttrs(context.Background(), level, "search worker stderr", append(attrs, slog.String("line", text))...)
	}

	o.lines++
}

// errorText is the error reply m as errors and the log show it: the code with
// the text while the text is one of the worker's and the budget lasts, else
// the code alone.
func (o *workerOutput) errorText(m *errorMsg) string {
	if !knownErrorTexts[m.Msg] {
		return m.Code
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	if o.texts >= errorTextBudget {
		o.droppedTexts++
		o.noticeLocked()

		return m.Code
	}

	o.texts++

	return m.Code + ": " + m.Msg
}

// noticeLocked logs once that the worker's output is dropped from now on.
func (o *workerOutput) noticeLocked() {
	if o.noticed {
		return
	}

	o.noticed = true
	o.log.Error("search worker: further worker output dropped")
}

// close logs how much output was dropped, if any.
func (o *workerOutput) close() {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.droppedLines > 0 || o.droppedTexts > 0 {
		o.log.Error("search worker: worker output dropped", slog.Int("lines", o.droppedLines), slog.Int("texts", o.droppedTexts))
	}
}
