package yadisk

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	fakeClientID = "client-1"
	fakeProvider = "yandex_disk"
	fakeAccount  = "alice"
	// fakeDescription is the "description" of every error answer; it must
	// never reach an error text.
	fakeDescription = "SECRET-DESCRIPTION-of-the-answer"

	opStatusInProgress = "in-progress"
	opStatusSuccess    = "success"
	opStatusFailed     = "failed"
)

// Endpoint names for fakeDisk.fail.
const (
	epMeta     = "meta"     // GET /v1/disk/resources?path=<file>
	epList     = "list"     // GET /v1/disk/resources?path=app:/
	epDownload = "download" // GET /v1/disk/resources/download
	epUpload   = "upload"   // GET /v1/disk/resources/upload
	epPut      = "put"      // PUT <upload href>
	epMove     = "move"     // POST /v1/disk/resources/move
	epDelete   = "delete"   // DELETE /v1/disk/resources
)

// fakeFailure makes one endpoint answer with an error.
type fakeFailure struct {
	status int
	code   string
}

type fakeFile struct {
	data    []byte
	md5     string // as the metadata reports it
	size    int64
	created time.Time
	md5Wait int // metadata requests still answered without md5
}

type fakeOp struct {
	status   string
	from, to string
	deferred bool
}

type fakeRequest struct {
	server string // api, href, oauth or plain
	method string
	path   string
	query  url.Values
	auth   string
}

// fakeDisk emulates the Disk API, its upload and download links and the
// OAuth token endpoint, each on its own httptest server.
type fakeDisk struct {
	api, href, oauth, plain *httptest.Server

	mu       sync.Mutex
	now      time.Time
	files    map[string]*fakeFile // by path, "app:/<name>"
	order    []string             // listing order of the files
	links    map[string]string    // download link id -> path
	uploads  map[string]string    // upload link id -> path
	ops      map[string]*fakeOp
	nextID   int
	events   []string // DELETE requests and pending-store calls, in order
	requests []fakeRequest

	// OAuth: the API accepts only access; the token endpoint expects refresh
	// and issues nextAccess and nextRefresh.
	access, refresh         string
	nextAccess, nextRefresh string
	expiresIn               int
	omitRefresh             bool
	oauthError              string // error of every token answer
	oauthStatus             int    // status of the oauthError answer
	reject401               bool   // the API rejects every token
	tokenForms              []url.Values

	// Modes.
	upload202      bool   // PUT answers 202, the md5 appears after md5Delay metadata requests
	md5Delay       int    //
	uploadMD5      string // md5 reported for uploads in place of the real one
	move202        bool   // move answers 202 and is applied at once
	moveFail       bool   // with move202: the operation fails, nothing moves
	moveBadBody    bool   // move is applied and answers 202 with a body that is not JSON
	deferMove      bool   // move answers 202 and waits for runDeferred
	dropPollAfter  int    // the n-th and later operation polls drop the connection; 0 never
	polls          int    //
	deleteFail     bool   // DELETE drops the connection
	delete202      bool   // DELETE answers 202 with a successful operation
	fail           map[string]fakeFailure
	downloadBase   string // base URL of download links; the href server by default
	downloadPrefix string // path prefix of download links
	opBase         string // base URL of operation links; the API server by default
	apiRedirect    bool   // the API redirects every request to the href server
}

func newFakeDisk(t *testing.T, now time.Time) *fakeDisk {
	t.Helper()

	d := &fakeDisk{
		now:     now,
		files:   map[string]*fakeFile{},
		links:   map[string]string{},
		uploads: map[string]string{},
		ops:     map[string]*fakeOp{},
		fail:    map[string]fakeFailure{},

		access: "acc-1", refresh: "ref-1",
		nextAccess: "acc-2", nextRefresh: "ref-2",
		expiresIn:      3600,
		oauthStatus:    http.StatusBadRequest,
		downloadPrefix: "/download/",
	}

	d.api = httptest.NewTLSServer(http.HandlerFunc(d.serveAPI))
	d.href = httptest.NewTLSServer(http.HandlerFunc(d.serveHref))
	d.oauth = httptest.NewTLSServer(http.HandlerFunc(d.serveOAuth))
	d.plain = httptest.NewServer(http.HandlerFunc(d.servePlain))
	d.downloadBase, d.opBase = d.href.URL, d.api.URL

	t.Cleanup(func() {
		d.api.Close()
		d.href.Close()
		d.oauth.Close()
		d.plain.Close()
	})

	return d
}

func (d *fakeDisk) endpoints() Endpoints { return Endpoints{API: d.api.URL, OAuth: d.oauth.URL} }

// set changes the emulator's state under its lock.
func (d *fakeDisk) set(f func(d *fakeDisk)) {
	d.mu.Lock()
	defer d.mu.Unlock()

	f(d)
}

// put stores a file in the app folder and returns its md5.
func (d *fakeDisk) put(name string, data []byte, created time.Time) string {
	d.mu.Lock()
	defer d.mu.Unlock()

	sum := md5Of(data)
	d.addFile("app:/"+name, &fakeFile{data: data, md5: sum, size: int64(len(data)), created: created})

	return sum
}

// file returns a copy of the file at path, or nil.
func (d *fakeDisk) file(name string) *fakeFile {
	d.mu.Lock()
	defer d.mu.Unlock()

	f, ok := d.files["app:/"+name]
	if !ok {
		return nil
	}

	c := *f

	return &c
}

// names lists the files of the app folder in listing order.
func (d *fakeDisk) names() []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	out := make([]string, 0, len(d.order))
	for _, p := range d.order {
		out = append(out, strings.TrimPrefix(p, "app:/"))
	}

	return out
}

// temps lists the temporary files of the container.
func (d *fakeDisk) temps() []string {
	var out []string

	for _, n := range d.names() {
		if strings.Contains(n, ".tmp-") {
			out = append(out, n)
		}
	}

	return out
}

func (d *fakeDisk) reqs() []fakeRequest {
	d.mu.Lock()
	defer d.mu.Unlock()

	return slices.Clone(d.requests)
}

// count returns the number of requests to server with method and path.
func (d *fakeDisk) count(server, method, path string) int {
	n := 0

	for _, r := range d.reqs() {
		if r.server == server && r.method == method && r.path == path {
			n++
		}
	}

	return n
}

func (d *fakeDisk) eventLog() []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	return slices.Clone(d.events)
}

func (d *fakeDisk) event(e string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.events = append(d.events, e)
}

// runDeferred finishes the deferred moves: a move whose source is gone fails.
func (d *fakeDisk) runDeferred() {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, op := range d.ops {
		if !op.deferred || op.status != opStatusInProgress {
			continue
		}

		if _, ok := d.files[op.from]; !ok {
			op.status = opStatusFailed
			continue
		}

		d.moveFile(op.from, op.to)
		op.status = opStatusSuccess
	}
}

// opStatuses lists the statuses of all operations.
func (d *fakeDisk) opStatuses() []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	out := make([]string, 0, len(d.ops))
	for _, op := range d.ops {
		out = append(out, op.status)
	}

	return out
}

func (d *fakeDisk) addFile(p string, f *fakeFile) {
	if _, ok := d.files[p]; !ok {
		d.order = append(d.order, p)
	}

	d.files[p] = f
}

func (d *fakeDisk) removeFile(p string) {
	delete(d.files, p)
	d.order = slices.DeleteFunc(d.order, func(s string) bool { return s == p })
}

func (d *fakeDisk) moveFile(from, to string) {
	f := d.files[from]
	d.removeFile(from)
	d.addFile(to, f)
}

func (d *fakeDisk) id() string {
	d.nextID++
	return strconv.Itoa(d.nextID)
}

func (d *fakeDisk) record(server string, r *http.Request) {
	d.requests = append(d.requests, fakeRequest{
		server: server, method: r.Method, path: r.URL.Path, query: r.URL.Query(), auth: r.Header.Get("Authorization"),
	})
}

func (d *fakeDisk) serveAPI(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.record("api", r)

	if d.apiRedirect {
		http.Redirect(w, r, d.href.URL+"/probe", http.StatusFound)
		return
	}

	if d.reject401 || r.Header.Get("Authorization") != "OAuth "+d.access {
		writeError(w, http.StatusUnauthorized, "UnauthorizedError")
		return
	}

	q := r.URL.Query()

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/disk/resources" && q.Get("path") == "app:/":
		d.list(w, q)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/disk/resources":
		d.meta(w, q)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/disk/resources/download":
		d.downloadLink(w, q)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/disk/resources/upload":
		d.uploadLink(w, q)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/disk/resources/move":
		d.move(w, q)
	case r.Method == http.MethodDelete && r.URL.Path == "/v1/disk/resources":
		d.delete(w, q)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/disk/operations/"):
		d.operation(w, strings.TrimPrefix(r.URL.Path, "/v1/disk/operations/"))
	default:
		writeError(w, http.StatusNotFound, "NotFound")
	}
}

// failed answers with the failure set for ep, if any.
func (d *fakeDisk) failed(w http.ResponseWriter, ep string) bool {
	f, ok := d.fail[ep]
	if ok {
		writeError(w, f.status, f.code)
	}

	return ok
}

func (d *fakeDisk) meta(w http.ResponseWriter, q url.Values) {
	if d.failed(w, epMeta) {
		return
	}

	p := q.Get("path")

	f, ok := d.files[p]
	if !ok {
		writeError(w, http.StatusNotFound, "DiskNotFoundError")
		return
	}

	out := map[string]any{"name": strings.TrimPrefix(p, "app:/"), "path": p, "type": "file", "size": f.size, "created": stamp(f.created)}
	if f.md5Wait > 0 {
		f.md5Wait--
	} else {
		out["md5"] = f.md5
	}

	writeJSON(w, http.StatusOK, out)
}

func (d *fakeDisk) list(w http.ResponseWriter, q url.Values) {
	if d.failed(w, epList) {
		return
	}

	offset, _ := strconv.Atoi(q.Get("offset"))
	limit, _ := strconv.Atoi(q.Get("limit"))

	if limit == 0 {
		limit = 20
	}

	items := make([]map[string]any, 0, limit)

	for i := offset; i < len(d.order) && i < offset+limit; i++ {
		p := d.order[i]
		items = append(items, map[string]any{"name": strings.TrimPrefix(p, "app:/"), "created": stamp(d.files[p].created), "type": "file"})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"_embedded": map[string]any{"items": items, "total": len(d.order), "limit": limit, "offset": offset},
	})
}

func (d *fakeDisk) downloadLink(w http.ResponseWriter, q url.Values) {
	if d.failed(w, epDownload) {
		return
	}

	p := q.Get("path")
	if _, ok := d.files[p]; !ok {
		writeError(w, http.StatusNotFound, "DiskNotFoundError")
		return
	}

	id := d.id()
	d.links[id] = p
	writeJSON(w, http.StatusOK, map[string]any{"href": d.downloadBase + d.downloadPrefix + id, "method": "GET", "templated": false})
}

func (d *fakeDisk) uploadLink(w http.ResponseWriter, q url.Values) {
	if d.failed(w, epUpload) {
		return
	}

	p := q.Get("path")
	if _, ok := d.files[p]; ok && q.Get("overwrite") != "true" {
		writeError(w, http.StatusConflict, "DiskResourceAlreadyExistsError")
		return
	}

	id := d.id()
	d.uploads[id] = p
	writeJSON(w, http.StatusOK, map[string]any{"href": d.href.URL + "/upload/" + id, "method": "PUT", "operation_id": "up-" + id})
}

func (d *fakeDisk) move(w http.ResponseWriter, q url.Values) {
	if d.failed(w, epMove) {
		return
	}

	from, to := q.Get("from"), q.Get("path")

	if _, ok := d.files[from]; !ok {
		writeError(w, http.StatusNotFound, "DiskNotFoundError")
		return
	}

	if _, ok := d.files[to]; ok && q.Get("overwrite") != "true" {
		writeError(w, http.StatusConflict, "DiskResourceAlreadyExistsError")
		return
	}

	switch {
	case d.deferMove:
		d.accept(w, &fakeOp{status: opStatusInProgress, from: from, to: to, deferred: true})
	case d.moveBadBody:
		d.moveFile(from, to)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("accepted, not JSON"))
	case d.move202 && d.moveFail:
		d.accept(w, &fakeOp{status: opStatusFailed, from: from, to: to})
	case d.move202:
		d.moveFile(from, to)
		d.accept(w, &fakeOp{status: opStatusSuccess, from: from, to: to})
	default:
		d.moveFile(from, to)
		writeJSON(w, http.StatusCreated, map[string]any{"href": d.api.URL + "/v1/disk/resources?path=" + url.QueryEscape(to)})
	}
}

// accept registers op and answers 202 with its link.
func (d *fakeDisk) accept(w http.ResponseWriter, op *fakeOp) {
	id := d.id()
	d.ops[id] = op
	writeJSON(w, http.StatusAccepted, map[string]any{"href": d.opBase + "/v1/disk/operations/" + id, "method": "GET"})
}

func (d *fakeDisk) operation(w http.ResponseWriter, id string) {
	d.polls++
	if d.dropPollAfter > 0 && d.polls >= d.dropPollAfter {
		dropConnection(w)
		return
	}

	op, ok := d.ops[id]
	if !ok {
		writeError(w, http.StatusNotFound, "DiskNotFoundError")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": op.status})
}

func (d *fakeDisk) delete(w http.ResponseWriter, q url.Values) {
	p := q.Get("path")
	d.events = append(d.events, "DELETE "+p+" permanently="+q.Get("permanently"))

	if d.deleteFail {
		dropConnection(w)
		return
	}

	if d.failed(w, epDelete) {
		return
	}

	if _, ok := d.files[p]; !ok {
		writeError(w, http.StatusNotFound, "DiskNotFoundError")
		return
	}

	d.removeFile(p)

	if d.delete202 {
		d.accept(w, &fakeOp{status: opStatusSuccess, from: p})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (d *fakeDisk) serveHref(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.record("href", r)

	dir, id, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")

	switch {
	case r.Method == http.MethodPut && dir == "upload":
		d.receive(w, r, id)
	case r.Method == http.MethodGet && dir == "download":
		d.send(w, id)
	case r.Method == http.MethodGet && dir == "redirect":
		http.Redirect(w, r, d.href.URL+"/download/"+id, http.StatusFound)
	case r.Method == http.MethodGet && dir == "redirect-http":
		http.Redirect(w, r, d.plain.URL+"/download/"+id, http.StatusFound)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (d *fakeDisk) receive(w http.ResponseWriter, r *http.Request, id string) {
	if f, ok := d.fail[epPut]; ok {
		http.Error(w, "upload refused: "+fakeDescription, f.status)
		return
	}

	p, ok := d.uploads[id]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	f := &fakeFile{data: data, md5: md5Of(data), size: int64(len(data)), created: d.now}
	if d.uploadMD5 != "" {
		f.md5 = d.uploadMD5
	}

	status := http.StatusCreated

	if d.upload202 {
		f.md5Wait = d.md5Delay
		status = http.StatusAccepted
	}

	d.addFile(p, f)
	w.WriteHeader(status)
}

func (d *fakeDisk) send(w http.ResponseWriter, id string) {
	f, ok := d.files[d.links[id]]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	_, _ = w.Write(f.data)
}

// servePlain is a plain-http server that must never be reached.
func (d *fakeDisk) servePlain(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.record("plain", r)
	d.send(w, strings.TrimPrefix(r.URL.Path, "/download/"))
}

func (d *fakeDisk) serveOAuth(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.record("oauth", r)

	if r.Method != http.MethodPost || r.URL.Path != "/token" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	d.tokenForms = append(d.tokenForms, r.PostForm)

	if d.oauthError != "" {
		writeJSON(w, d.oauthStatus, map[string]any{"error": d.oauthError, "error_description": fakeDescription})
		return
	}

	f := r.PostForm
	if f.Get("grant_type") != "refresh_token" || f.Get("refresh_token") != d.refresh || f.Get("client_id") != fakeClientID {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant", "error_description": fakeDescription})
		return
	}

	d.access = d.nextAccess
	out := map[string]any{"access_token": d.nextAccess, "expires_in": d.expiresIn, "token_type": "bearer"}

	if !d.omitRefresh {
		out["refresh_token"] = d.nextRefresh
		d.refresh = d.nextRefresh
	}

	writeJSON(w, http.StatusOK, out)
}

func (d *fakeDisk) forms() []url.Values {
	d.mu.Lock()
	defer d.mu.Unlock()

	return slices.Clone(d.tokenForms)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]any{"error": code, "description": fakeDescription, "message": fakeDescription})
}

// dropConnection closes the connection without an answer.
func dropConnection(w http.ResponseWriter) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		panic("fake disk: response writer cannot hijack")
	}

	conn, _, err := hj.Hijack()
	if err == nil {
		_ = conn.Close()
	}
}

func stamp(t time.Time) string { return t.Format("2006-01-02T15:04:05-07:00") }

func md5Of(data []byte) string {
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])
}

// fakePending is an in-memory PendingStore that writes its calls to the
// emulator's event log.
type fakePending struct {
	disk *fakeDisk

	mu                       sync.Mutex
	name                     string
	found                    bool
	saveErr, loadErr, clrErr error
	saves, loads, clears     int
}

func (p *fakePending) LoadPendingTemp(_ context.Context) (name string, found bool, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.loads++

	return p.name, p.found, p.loadErr
}

func (p *fakePending) SavePendingTemp(_ context.Context, name string) error {
	p.disk.event("save " + name)

	p.mu.Lock()
	defer p.mu.Unlock()

	p.saves++
	if p.saveErr != nil {
		return p.saveErr
	}

	p.name, p.found = name, true

	return nil
}

func (p *fakePending) ClearPendingTemp(_ context.Context) error {
	p.disk.event("clear")

	p.mu.Lock()
	defer p.mu.Unlock()

	p.clears++
	if p.clrErr != nil {
		return p.clrErr
	}

	p.name, p.found = "", false

	return nil
}

// state returns the pending name and whether there is one.
func (p *fakePending) state() (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.name, p.found
}
