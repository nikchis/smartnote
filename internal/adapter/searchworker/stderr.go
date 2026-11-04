package searchworker

import (
	"log/slog"
	"path"
	"regexp"
	"strings"
)

// The worker's stderr goes to the log as a closed vocabulary (spec §10.2):
// a line is logged only when it fully matches one of the forms below, which
// are the lines the worker itself writes; any other line is logged as
// "unrecognized" with its length only. So code in the worker process, a
// compromised dependency included, cannot get note text into smartnote.log
// through stderr. A form with free text in it (an OS error message, a
// function name) logs a rendering without that text.
//
// Contract with search/worker/src: every eprintln! of the worker has its
// format listed in the sources of a form here; TestStderrFormsCoverWorker
// fails until a new one is added. Lines written otherwise (the panic hook,
// the allocation failure of std) have forms without sources.

// stderrForm is one kind of line the worker writes to stderr.
type stderrForm struct {
	// sources are the eprintln! formats of the worker that write this form.
	sources []string
	// re matches the whole line.
	re *regexp.Regexp
	// render gives the logged text from the submatches; nil logs the line.
	render func(m []string) string
	// info logs the form at level Info, which the error log drops; the
	// others are Error.
	info bool
	// most is how many lines of the form one process may write; 0 is no
	// limit besides maxStderrLines.
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
	ioErrRe  = `(?:.* \(os error (-?\d{1,11})\)|(parent process has changed|out of range integral type conversion attempted))`
	gapRe    = `(?:no_new_privs|landlock_fs|landlock_net|seccomp_arch|seccomp|threads|unsupported_os)`
	sourceRe = `[A-Za-z0-9_.+/\\-]{1,200}`
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
		// at <file>:<line> <function>"; the log keeps the file name and line.
		sources: []string{"{line}"},
		re:      full(`onnxruntime (error|fatal) at (` + sourceRe + `):(\d{1,9})(?: .*)?`),
		render: func(m []string) string {
			return "onnxruntime " + m[1] + " at " + path.Base(strings.ReplaceAll(m[2], `\`, "/")) + ":" + m[3]
		},
		most: 20,
	},
	{
		// The panic hook (panic.rs) writes one line, and the process aborts.
		re:   full(`panic at (?:<unknown>|` + sourceRe + `\.rs:\d{1,9}:\d{1,9})`),
		most: 1,
	},
	{
		// The allocation failure of std, before it aborts the process.
		re:   full(`memory allocation of \d{1,20} bytes failed`),
		most: 1,
	},
}

func full(re string) *regexp.Regexp { return regexp.MustCompile(`^` + re + `$`) }

// stderrFilter applies the forms to the lines of one process.
type stderrFilter struct {
	forms []stderrForm
	seen  []int // lines logged per form
}

func newStderrFilter(forms []stderrForm) *stderrFilter {
	return &stderrFilter{forms: forms, seen: make([]int, len(forms))}
}

// filter returns the text to log for line and its level; false when the line
// matches no form, or a form already used up for this process.
func (f *stderrFilter) filter(line string) (string, slog.Level, bool) {
	for i, form := range f.forms {
		m := form.re.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		if form.most > 0 && f.seen[i] >= form.most {
			return "", slog.LevelError, false
		}

		f.seen[i]++

		level := slog.LevelError
		if form.info {
			level = slog.LevelInfo
		}

		if form.render == nil {
			return line, level, true
		}

		return form.render(m), level, true
	}

	return "", slog.LevelError, false
}
