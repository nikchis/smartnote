//go:build integration

package wailsui

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/adapter/searchworker"
	"github.com/nikchis/smartnote/internal/adapter/sqlite"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/sealbox"
	"github.com/nikchis/smartnote/internal/usecase"
)

// benchTimeout bounds every phase of BenchmarkSearchService that waits for
// the worker: the background embedding of the whole set takes minutes.
const benchTimeout = 20 * time.Minute

// benchPause separates the queries while the embeddings are computed, as a
// user typing would.
const benchPause = 10 * time.Millisecond

// benchPureSubstring are queries only the substring search serves: shorter
// than 3 characters, with a character that is not a letter or digit.
var benchPureSubstring = []string{"==", "(", "{}", "<="}

// benchSetQuery is a query of search/testdata/bench/queries.json.
type benchSetQuery struct {
	Q      string `json:"q"`
	Kind   string `json:"kind"`
	Expect *int   `json:"expect"`
}

// benchStatus records the statuses of the search index with the times they
// first held.
type benchStatus struct {
	notes int
	start time.Time

	mu                                  sync.Mutex
	last                                usecase.SearchStatus
	indexed, semanticReady, allEmbedded time.Duration
}

func (s *benchStatus) publish(st usecase.SearchStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.last = st
	at := time.Since(s.start)

	if s.indexed == 0 && st.Notes == s.notes && st.Lexical == usecase.SearchReady {
		s.indexed = at
	}

	if s.semanticReady == 0 && st.Semantic == usecase.SearchReady {
		s.semanticReady = at
	}

	if s.allEmbedded == 0 && st.Notes == s.notes && st.Queued == 0 && st.Semantic == usecase.SearchReady {
		s.allEmbedded = at
	}
}

func (s *benchStatus) get() (last usecase.SearchStatus, indexed, semanticReady, allEmbedded time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.last, s.indexed, s.semanticReady, s.allEmbedded
}

// benchRun is one run of the search over the store: a worker process.
type benchRun struct {
	svc    *SearchService
	status *benchStatus
	engine *searchworker.Engine
	cancel context.CancelFunc
	done   chan struct{}
	mem    *memSampler
}

// startBenchRun starts the search as the application does after unlocking the
// store: a new engine and usecase.Search, then Run.
func startBenchRun(b *testing.B, store *sqlite.Store, notes int) *benchRun {
	b.Helper()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	engine, err := searchworker.New(searchworker.Config{
		Path:     os.Getenv("SMARTNOTE_SEARCH_BIN"),
		OrtLib:   os.Getenv("SMARTNOTE_ORT_LIB"),
		ModelDir: os.Getenv("SMARTNOTE_MODEL_DIR"),
		// As cmd/smartnote: half of the CPUs, at most 4.
		Threads: min(4, max(1, runtime.NumCPU()/2)),
		Log:     log,
	})
	if err != nil {
		b.Fatal(err)
	}

	status := &benchStatus{notes: notes}

	search, err := usecase.NewSearch(usecase.SearchDeps{
		Engine: engine, Notes: store, Embeddings: store, Changes: &usecase.ChangeTracker{},
		OnStatus: status.publish,
		Warn:     func(err error) { log.Error("search", "err", err) },
	})
	if err != nil {
		b.Fatal(err)
	}

	ctx, cancel := context.WithCancel(b.Context())
	run := &benchRun{
		svc:    NewSearchService(Env{Ctx: ctx, Log: log}, search, NewSearchStatusFeed(usecase.SearchStatus{}, nil)),
		status: status,
		engine: engine,
		cancel: cancel,
		done:   make(chan struct{}),
	}

	status.start = time.Now()
	run.mem = newMemSampler(status.start)

	go func() {
		defer close(run.done)

		if err := search.Run(ctx); err != nil {
			b.Errorf("Run: %v", err)
		}
	}()

	go run.mem.run(ctx)

	return run
}

func (r *benchRun) stop(b *testing.B) {
	b.Helper()
	r.cancel()
	<-r.done

	if err := r.engine.Close(); err != nil {
		b.Errorf("Close: %v", err)
	}
}

// search times one call of SearchService.Search.
func (r *benchRun) search(b *testing.B, q string) ([]SearchHitDTO, time.Duration) {
	b.Helper()

	began := time.Now()
	hits, err := r.svc.Search(q)
	took := time.Since(began)

	if err != nil {
		b.Fatalf("Search: %v", err)
	}

	return hits, took
}

// waitReady polls a word and a substring query until each finds its note
// and the worker reports every note indexed; it returns the times from the
// start of Run.
func (r *benchRun) waitReady(b *testing.B, word, substring, wordNote, substringNote string) (wordAt, substringAt, indexedAt time.Duration) {
	b.Helper()

	deadline := time.Now().Add(benchTimeout)

	for time.Now().Before(deadline) {
		if wordAt == 0 && containsNote(r.svc, word, wordNote) {
			wordAt = time.Since(r.status.start)
		}

		if substringAt == 0 && containsNote(r.svc, substring, substringNote) {
			substringAt = time.Since(r.status.start)
		}

		_, indexed, _, _ := r.status.get()
		if wordAt != 0 && substringAt != 0 && indexed != 0 {
			return wordAt, substringAt, indexed
		}

		time.Sleep(2 * time.Millisecond)
	}

	b.Fatal("the word and substring search did not become ready")

	return 0, 0, 0
}

func containsNote(svc *SearchService, q, id string) bool {
	hits, err := svc.Search(q)
	if err != nil {
		return false
	}

	return slices.ContainsFunc(hits, func(h SearchHitDTO) bool { return h.NoteID == id })
}

// BenchmarkSearchService measures criterion 4 of the search (spec §1, §12.4)
// with the real worker and model on the benchmark set, timed from the call of
// SearchService.Search:
//
//  1. a store with the 1000 notes and no embeddings; the search starts as
//     after unlocking: the time until a word and a substring query find their
//     notes and the worker reports every note indexed;
//  2. the latency of the queries of the set while the worker computes the
//     embeddings in the background;
//  3. the latency after that (the timed loop), the RSS of the worker and its
//     peak, and the latency of queries only the substring search serves;
//  4. a restart over the stored embeddings, as the next start of the
//     application: readiness again and the RSS.
//
// It needs SMARTNOTE_SEARCH_BIN (a release build of smartnote-search),
// SMARTNOTE_MODEL_DIR, SMARTNOTE_ORT_LIB and SMARTNOTE_BENCH_CORPUS, the notes
// from `cargo run -p worker --example bench_corpus -- OUT.json`; `make
// search-bench` sets them. The numbers are reported, not checked: they depend
// on the machine.
func BenchmarkSearchService(b *testing.B) {
	for _, v := range []string{"SMARTNOTE_SEARCH_BIN", "SMARTNOTE_MODEL_DIR", "SMARTNOTE_ORT_LIB", "SMARTNOTE_BENCH_CORPUS"} {
		if os.Getenv(v) == "" {
			b.Skipf("%s is not set", v)
		}
	}

	if runtime.GOOS != "linux" {
		b.Skip("the memory of the worker is read from /proc")
	}

	ids, store := benchStore(b)
	queries := benchQueries(b)

	// 1. Readiness after unlocking, without stored embeddings.
	run := startBenchRun(b, store, len(ids))
	wordAt, substringAt, indexedAt := run.waitReady(b, "retryWithBackoff", "== 11", ids[17], ids[37])
	b.Logf("cold start: word search ready %v, substring %v, all %d notes indexed %v", wordAt, substringAt, len(ids), indexedAt)

	// 2. Queries while the embeddings are computed.
	during, queued := run.queryDuringEmbedding(b, queries)
	_, _, semanticAt, embeddedAt := run.status.get()
	b.Logf("semantic search ready %v; all embeddings computed %v; %d queries timed during the embedding (queue %d→%d)",
		semanticAt, embeddedAt, len(during), queued[0], queued[1])

	// 3. Queries after the embedding.
	rss, peak := run.mem.current()
	sampled, sampledAt := run.mem.maxRSS()
	b.Logf("worker RSS after indexing %.0f MB, peak %.0f MB (sampled peak %.0f MB at %v); RSS when semantic search was ready %.0f MB, when every note was indexed %.0f MB",
		mb(rss), mb(peak), mb(sampled), sampledAt, mb(run.mem.rssAt(semanticAt)), mb(run.mem.rssAt(indexedAt)))

	after := make([]time.Duration, 0, len(queries))
	i := 0

	for b.Loop() {
		_, took := run.search(b, queries[i%len(queries)].Q)
		after = append(after, took)
		i++
	}

	b.StopTimer()

	substring := make([]time.Duration, 0, 50*len(benchPureSubstring))

	for range 50 {
		for _, q := range benchPureSubstring {
			_, took := run.search(b, q)
			substring = append(substring, took)
		}
	}

	rssAfter, peakAfter := run.mem.current()
	run.stop(b)

	// 4. The next start: the embeddings come from the store.
	warm := startBenchRun(b, store, len(ids))
	wWord, wSubstring, wIndexed := warm.waitReady(b, "retryWithBackoff", "== 11", ids[17], ids[37])
	warmAll := warm.waitAllEmbedded(b)
	warmAfter := make([]time.Duration, 0, 3*len(queries))

	for range 3 {
		for _, q := range queries {
			_, took := warm.search(b, q.Q)
			warmAfter = append(warmAfter, took)
		}
	}

	warmRSS, warmPeak := warm.mem.current()
	warm.stop(b)

	b.Logf("warm start: word search ready %v, substring %v, all indexed %v, semantic over stored embeddings %v; RSS %.0f MB, peak %.0f MB",
		wWord, wSubstring, wIndexed, warmAll, mb(warmRSS), mb(warmPeak))

	report := func(name string, d []time.Duration) {
		p50, p95, maxD := percentiles(d)
		b.Logf("%-32s n=%5d p50 %6.2f ms  p95 %6.2f ms  max %7.2f ms", name, len(d), ms(p50), ms(p95), ms(maxD))
	}
	report("during the embedding", during)
	report("after the embedding", after)
	report("after a warm start", warmAfter)
	report("substring-only queries", substring)

	_, p95During, _ := percentiles(during)
	_, p95After, _ := percentiles(after)
	_, p95Substring, _ := percentiles(substring)

	b.ReportMetric(ms(max(wordAt, substringAt, indexedAt)), "ready-ms")
	b.ReportMetric(ms(max(wWord, wSubstring, wIndexed)), "warm-ready-ms")
	b.ReportMetric(ms(p95During), "p95-during-ms")
	b.ReportMetric(ms(p95After), "p95-after-ms")
	b.ReportMetric(ms(p95Substring), "p95-substring-ms")
	b.ReportMetric(mb(rssAfter), "rss-MB")
	b.ReportMetric(mb(max(peakAfter, warmPeak)), "peak-MB")
	b.ReportMetric(mb(warmRSS), "warm-rss-MB")
}

// queryDuringEmbedding runs the queries in turn while semantic search is
// ready and embeddings are queued, and returns the times of those that
// started then and the queue at the first and the last of them.
func (r *benchRun) queryDuringEmbedding(b *testing.B, queries []benchSetQuery) (during []time.Duration, queued [2]int) {
	b.Helper()

	deadline := time.Now().Add(benchTimeout)
	queued[0] = -1

	for i := 0; time.Now().Before(deadline); i++ {
		st, _, _, embedded := r.status.get()
		if embedded != 0 {
			return during, queued
		}

		if st.Semantic != usecase.SearchReady || st.Queued == 0 {
			time.Sleep(benchPause)
			continue
		}

		if queued[0] < 0 {
			queued[0] = st.Queued
		}

		queued[1] = st.Queued
		_, took := r.search(b, queries[i%len(queries)].Q)
		during = append(during, took)

		time.Sleep(benchPause)
	}

	b.Fatal("the embeddings were not computed in time")

	return nil, queued
}

// waitAllEmbedded waits until every note has its embeddings and returns the
// time from the start of Run.
func (r *benchRun) waitAllEmbedded(b *testing.B) time.Duration {
	b.Helper()

	deadline := time.Now().Add(benchTimeout)

	for time.Now().Before(deadline) {
		if _, _, _, at := r.status.get(); at != 0 {
			return at
		}

		time.Sleep(2 * time.Millisecond)
	}

	b.Fatal("the embeddings were not loaded in time")

	return 0
}

// benchStore creates an unlocked store with the notes of the benchmark set
// and returns the IDs of the notes by their number in the set.
func benchStore(b *testing.B) ([]string, *sqlite.Store) {
	b.Helper()

	raw, err := os.ReadFile(os.Getenv("SMARTNOTE_BENCH_CORPUS"))
	if err != nil {
		b.Fatal(err)
	}

	var corpus struct {
		Notes []struct{ ID, Name, Info string } `json:"notes"`
	}

	if err := json.Unmarshal(raw, &corpus); err != nil {
		b.Fatal(err)
	}

	store, err := sqlite.OpenWithKDF(b.TempDir(), sealbox.KDFParams{Time: 1, MemoryKiB: 19456, Threads: 1})
	if err != nil {
		b.Fatal(err)
	}

	b.Cleanup(func() { _ = store.Close() })

	ctx := b.Context()
	if err := store.Create(ctx, "bench-password"); err != nil {
		b.Fatal(err)
	}

	updated := time.Now().UTC().Truncate(time.Millisecond)
	ids := make([]string, 0, len(corpus.Notes))

	for i, n := range corpus.Notes {
		if n.ID != fmt.Sprintf("n%04d", i) {
			b.Fatalf("note %d has id %s", i, n.ID)
		}

		id := uuid.NewSHA1(uuid.NameSpaceOID, []byte("smartnote-bench/"+n.ID))
		if err := store.InsertNote(ctx, domain.Note{ID: id, Name: n.Name, Info: n.Info, Updated: updated}); err != nil {
			b.Fatal(err)
		}

		ids = append(ids, id.String())
	}

	return ids, store
}

func benchQueries(b *testing.B) []benchSetQuery {
	b.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "search", "testdata", "bench", "queries.json"))
	if err != nil {
		b.Fatal(err)
	}

	var set struct {
		Queries []benchSetQuery `json:"queries"`
	}

	if err := json.Unmarshal(raw, &set); err != nil {
		b.Fatal(err)
	}

	return set.Queries
}

// memSampler follows the RSS of the worker, a child process of the test.
type memSampler struct {
	start time.Time

	mu        sync.Mutex
	rss, peak int64 // the last VmRSS and VmHWM, bytes
	sampled   int64 // the largest VmRSS sampled
	sampledAt time.Duration
	series    []memSample
}

// memSample is the VmRSS of the worker at a time from the start of the run.
type memSample struct {
	at  time.Duration
	rss int64
}

func newMemSampler(start time.Time) *memSampler { return &memSampler{start: start} }

func (m *memSampler) run(ctx context.Context) {
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}

		pid := workerPID()
		if pid == 0 {
			continue
		}

		rss, peak, ok := procMemory(pid)
		if !ok {
			continue
		}

		at := time.Since(m.start)

		m.mu.Lock()
		m.rss, m.peak = rss, peak
		m.series = append(m.series, memSample{at: at, rss: rss})

		if rss > m.sampled {
			m.sampled, m.sampledAt = rss, at
		}
		m.mu.Unlock()
	}
}

func (m *memSampler) current() (rss, peak int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.rss, m.peak
}

// maxRSS returns the largest VmRSS sampled and when.
func (m *memSampler) maxRSS() (int64, time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.sampled, m.sampledAt
}

// rssAt returns the first VmRSS sampled at or after at.
func (m *memSampler) rssAt(at time.Duration) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, s := range m.series {
		if s.at >= at {
			return s.rss
		}
	}

	return 0
}

// workerPID returns the PID of a running smartnote-search child of the test
// process, or 0.
func workerPID() int {
	tasks, _ := filepath.Glob("/proc/self/task/*/children")
	for _, t := range tasks {
		raw, err := os.ReadFile(t)
		if err != nil {
			continue
		}

		for _, f := range strings.Fields(string(raw)) {
			pid, err := strconv.Atoi(f)
			if err != nil {
				continue
			}

			comm, err := os.ReadFile("/proc/" + f + "/comm")
			if err == nil && strings.HasPrefix(strings.TrimSpace(string(comm)), "smartnote-searc") {
				return pid
			}
		}
	}

	return 0
}

// procMemory returns VmRSS and VmHWM of pid in bytes.
func procMemory(pid int) (rss, peak int64, ok bool) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return 0, 0, false
	}

	for line := range strings.SplitSeq(string(raw), "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}

		fields := strings.Fields(value)
		if len(fields) == 0 {
			continue
		}

		kib, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			continue
		}

		switch key {
		case "VmRSS":
			rss = kib * 1024
		case "VmHWM":
			peak = kib * 1024
		}
	}

	return rss, peak, rss > 0
}

func percentiles(d []time.Duration) (p50, p95, maxD time.Duration) {
	if len(d) == 0 {
		return 0, 0, 0
	}

	s := slices.Clone(d)
	slices.Sort(s)

	at := func(q float64) time.Duration { return s[int(float64(len(s)-1)*q+0.5)] }

	return at(0.5), at(0.95), s[len(s)-1]
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

func mb(bytes int64) float64 { return float64(bytes) / (1 << 20) }
