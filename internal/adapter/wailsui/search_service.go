package wailsui

import (
	"log/slog"
	"sync"

	"github.com/nikchis/smartnote/internal/applog"
	"github.com/nikchis/smartnote/internal/usecase"
)

// searchLimit is the most hits one search returns.
const searchLimit = 10

// searchWarningKind marks the log records of search problems: the search
// never fails a note operation, so they are warnings, but the error log keeps
// only records of level Error and above and spec §11 asks for them there.
const searchWarningKind = "search-warning"

// LogSearchWarning writes err, a problem of the search that the application
// works around, to log at level Error with kind=search-warning, so the error
// log keeps it apart from internal errors. The errors of the search carry no
// note text.
func LogSearchWarning(log *slog.Logger, msg string, err error) {
	log.Error(msg, slog.String("kind", searchWarningKind), applog.Err(err))
}

// SearchStatusFeed keeps the last status of the search index and sends each
// new one to the interface as EventSearchStatus. It is a type of its own, not
// a method of SearchService, because Wails would let the interface call every
// exported method of a service.
type SearchStatusFeed struct {
	emit func(name string, data ...any)

	mu   sync.Mutex
	last SearchStatusDTO
}

// NewSearchStatusFeed returns a feed whose last status is initial; emit sends
// an event to the interface, nil sends nothing.
func NewSearchStatusFeed(initial usecase.SearchStatus, emit func(name string, data ...any)) *SearchStatusFeed {
	if emit == nil {
		emit = func(string, ...any) {}
	}

	return &SearchStatusFeed{emit: emit, last: toSearchStatusDTO(initial)}
}

// Publish keeps st as the last status and emits it. It is usecase.SearchDeps
// OnStatus, which is called from one goroutine, so the events keep the order
// of the statuses.
func (f *SearchStatusFeed) Publish(st usecase.SearchStatus) {
	dto := toSearchStatusDTO(st)

	f.mu.Lock()
	f.last = dto
	f.mu.Unlock()

	f.emit(EventSearchStatus, dto)
}

// Last returns the last status.
func (f *SearchStatusFeed) Last() SearchStatusDTO {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.last
}

// SearchService searches the notes and tells the state of the search index.
type SearchService struct {
	env    Env
	search *usecase.Search
	status *SearchStatusFeed
}

// NewSearchService returns a SearchService over s; status is the feed that
// usecase.SearchDeps OnStatus publishes to.
func NewSearchService(env Env, s *usecase.Search, status *SearchStatusFeed) *SearchService {
	return &SearchService{env: env, search: s, status: status}
}

// Search returns at most 10 places that match query, best first; none is an
// empty list. The search bypasses the queue of index changes (spec §3.3 rule 7).
func (s *SearchService) Search(query string) ([]SearchHitDTO, error) {
	hits, err := s.search.Query(s.env.Ctx, query, searchLimit)
	if err != nil {
		return nil, s.env.fail("search", err)
	}

	out := make([]SearchHitDTO, 0, len(hits))
	for _, h := range hits {
		out = append(out, toSearchHitDTO(h))
	}

	return out, nil
}

// Status returns the last known status of the search index; the interface
// reads it once and then follows EventSearchStatus.
func (s *SearchService) Status() SearchStatusDTO { return s.status.Last() }
