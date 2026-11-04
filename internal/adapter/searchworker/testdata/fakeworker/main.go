// Command fakeworker stands in for smartnote-search in the tests of package
// searchworker. It speaks the worker protocol with its own message structs and
// misbehaves as FAKE_MODE says:
//
//   - ok: a working worker with a substring index; ready reports semantic
//     "loading", then a status reports it "ready";
//   - crash-on-hello: exits with code 1 after reading hello;
//   - hang-on-upsert: stops reading and answering at the first upsert and
//     never exits on its own, not even at the end of stdin;
//   - flood-events: sends 10 000 status and 10 000 embedded messages before
//     the ok of each upsert;
//   - bad-frame: answers the first request after hello with a frame header
//     declaring 4 GiB;
//   - exit-after-n: exits with code 1, without answering, at request number
//     FAKE_N after hello (default 1);
//   - ignore-search: like ok, but never answers a search.
//
// ready.worker describes the hello it received and the ORT_*, LD_* and DYLD_*
// variables it sees, for the tests to check.
package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/fxamacker/cbor/v2"
)

const floodCount = 10000

type request struct {
	T         string    `cbor:"t"`
	Req       uint64    `cbor:"req"`
	Proto     uint32    `cbor:"proto"`
	ModelDir  string    `cbor:"model_dir"`
	OrtLib    string    `cbor:"ort_lib"`
	Threads   uint16    `cbor:"threads"`
	ParentPID uint32    `cbor:"parent_pid"`
	Notes     []note    `cbor:"notes"`
	Items     []removal `cbor:"items"`
	Query     string    `cbor:"query"`
	Limit     uint16    `cbor:"limit"`
}

type note struct {
	ID   string `cbor:"id"`
	Rev  uint64 `cbor:"rev"`
	Name string `cbor:"name"`
	Info string `cbor:"info"`
	Emb  []byte `cbor:"emb"`
}

type removal struct {
	ID  string `cbor:"id"`
	Rev uint64 `cbor:"rev"`
}

type ready struct {
	T        string `cbor:"t"`
	Worker   string `cbor:"worker"`
	Index    []byte `cbor:"index"`
	Lexical  string `cbor:"lexical"`
	Semantic string `cbor:"semantic"`
	Sandbox  string `cbor:"sandbox"`
}

type ok struct {
	T   string `cbor:"t"`
	Req uint64 `cbor:"req"`
}

type result struct {
	T    string `cbor:"t"`
	Req  uint64 `cbor:"req"`
	Hits []hit  `cbor:"hits"`
}

type hit struct {
	Note    string      `cbor:"note"`
	Line    uint32      `cbor:"line"`
	Start   uint32      `cbor:"start"`
	End     uint32      `cbor:"end"`
	Text    string      `cbor:"text"`
	Snippet string      `cbor:"snippet"`
	Marks   [][2]uint32 `cbor:"marks"`
	Sources []string    `cbor:"sources"`
	Score   float32     `cbor:"score"`
}

type embedded struct {
	T   string `cbor:"t"`
	ID  string `cbor:"id"`
	Rev uint64 `cbor:"rev"`
	Emb []byte `cbor:"emb"`
}

type status struct {
	T        string `cbor:"t"`
	Notes    uint32 `cbor:"notes"`
	Queued   uint32 `cbor:"queued"`
	Lexical  string `cbor:"lexical"`
	Semantic string `cbor:"semantic"`
}

type fake struct {
	mode  string
	in    *bufio.Reader
	out   *bufio.Writer
	enc   cbor.EncMode
	notes map[string]note
}

func main() {
	mode := os.Getenv("FAKE_MODE")
	fmt.Fprintf(os.Stderr, "fake: started mode=%s\n", mode)

	enc, err := cbor.EncOptions{NilContainers: cbor.NilContainerAsEmpty}.EncMode()
	if err != nil {
		fail(err)
	}

	f := &fake{
		mode:  mode,
		in:    bufio.NewReader(os.Stdin),
		out:   bufio.NewWriter(os.Stdout),
		enc:   enc,
		notes: map[string]note{},
	}
	f.run()
}

func (f *fake) run() {
	hello, err := f.read()
	if err != nil || hello.T != "hello" {
		fail(fmt.Errorf("expected hello: %w", err))
	}

	if f.mode == "crash-on-hello" {
		fail(errors.New("crash"))
	}

	worker := fmt.Sprintf("fake proto=%d threads=%d model=%s ort=%s parent=%t env=%s",
		hello.Proto, hello.Threads, hello.ModelDir, hello.OrtLib, int(hello.ParentPID) == os.Getppid(), loaderEnv())
	f.send(ready{T: "ready", Worker: worker, Index: make([]byte, 32), Lexical: "ready", Semantic: "loading", Sandbox: "none"})
	f.send(status{T: "status", Lexical: "ready", Semantic: "ready"})

	exitAt := 1
	if n, err := strconv.Atoi(os.Getenv("FAKE_N")); err == nil {
		exitAt = n
	}

	for count := 1; ; count++ {
		req, err := f.read()
		if errors.Is(err, io.EOF) {
			os.Exit(0)
		}

		if err != nil {
			fail(err)
		}

		if f.mode == "exit-after-n" && count == exitAt {
			fail(errors.New("exit after n"))
		}

		if f.mode == "bad-frame" {
			f.writeRaw(binary.LittleEndian.AppendUint32(nil, 0xffffffff))
			continue
		}

		f.handle(req)
	}
}

func (f *fake) handle(req request) {
	switch req.T {
	case "reset":
		clear(f.notes)
		f.send(ok{T: "ok", Req: req.Req})
	case "upsert":
		f.upsert(req)
	case "remove":
		for _, item := range req.Items {
			delete(f.notes, item.ID)
		}

		f.send(ok{T: "ok", Req: req.Req})
	case "search":
		if f.mode != "ignore-search" {
			f.send(result{T: "result", Req: req.Req, Hits: f.search(req.Query, int(req.Limit))})
		}
	default:
		fail(fmt.Errorf("unexpected %q", req.T))
	}
}

func (f *fake) upsert(req request) {
	switch f.mode {
	case "hang-on-upsert":
		fmt.Fprintln(os.Stderr, "fake: hanging")

		for {
			time.Sleep(time.Hour)
		}
	case "flood-events":
		for i := range floodCount {
			f.send(status{T: "status", Notes: uint32(i), Lexical: "ready", Semantic: "ready"}) //nolint:gosec // i < floodCount
			f.send(embedded{T: "embedded", ID: req.Notes[0].ID, Rev: uint64(i), Emb: []byte("flood")})
		}
	}

	for _, n := range req.Notes {
		f.notes[n.ID] = n
	}

	f.send(ok{T: "ok", Req: req.Req})
	f.send(status{T: "status", Notes: uint32(len(f.notes)), Lexical: "ready", Semantic: "ready"}) //nolint:gosec // a few notes

	for _, n := range req.Notes {
		if n.Emb == nil {
			f.send(embedded{T: "embedded", ID: n.ID, Rev: n.Rev, Emb: []byte("emb:" + n.ID)})
		}
	}
}

// search finds query in the text of the notes, at most limit hits in the
// order of the IDs.
func (f *fake) search(query string, limit int) []hit {
	ids := make([]string, 0, len(f.notes))
	for id := range f.notes {
		ids = append(ids, id)
	}

	slices.Sort(ids)

	hits := []hit{}

	for _, id := range ids {
		n := f.notes[id]

		at := strings.Index(n.Info, query)
		if at < 0 || len(hits) == limit {
			continue
		}

		start, end := uint32(at), uint32(at+len(query)) //nolint:gosec // short test texts
		hits = append(hits, hit{
			Note: id, Line: 1, Start: start, End: end, Text: query, Snippet: n.Info,
			Marks: [][2]uint32{{start, end}}, Sources: []string{"substring"}, Score: 0.5,
		})
	}

	return hits
}

func (f *fake) read() (request, error) {
	var header [4]byte
	if _, err := io.ReadFull(f.in, header[:]); err != nil {
		return request{}, err
	}

	body := make([]byte, binary.LittleEndian.Uint32(header[:]))
	if _, err := io.ReadFull(f.in, body); err != nil {
		return request{}, err
	}

	var req request
	if err := cbor.Unmarshal(body, &req); err != nil {
		return request{}, err
	}

	return req, nil
}

func (f *fake) send(msg any) {
	body, err := f.enc.Marshal(msg)
	if err != nil {
		fail(err)
	}

	f.writeRaw(append(binary.LittleEndian.AppendUint32(nil, uint32(len(body))), body...)) //nolint:gosec // small messages
}

func (f *fake) writeRaw(b []byte) {
	if _, err := f.out.Write(b); err != nil {
		fail(err)
	}

	if err := f.out.Flush(); err != nil {
		fail(err)
	}
}

// loaderEnv lists the variables the worker must not inherit, and the one it
// must, as the fake sees them.
func loaderEnv() string {
	var seen []string

	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")

		upper := strings.ToUpper(name)
		if strings.HasPrefix(upper, "ORT_") || strings.HasPrefix(upper, "LD_") || strings.HasPrefix(upper, "DYLD_") {
			seen = append(seen, kv)
		}
	}

	slices.Sort(seen)

	return strings.Join(seen, ",")
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "fake: %v\n", err)
	os.Exit(1)
}
