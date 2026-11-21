package gdrive

import (
	"cmp"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The emulator answers as the real Drive v3 and Google OAuth did in the
// prototype (spec §13, П1-П8): sizes are JSON strings, createdTime has
// milliseconds, field masks give exactly the asked fields, errors have the
// body {"error": {"code", "message", "errors": [{"reason"}]}}, the token
// endpoint wants the client secret for the exchange and the refresh.

const (
	fakeClientID     = "client-1.apps.example"
	fakeClientSecret = "secret-of-client-1"
	fakeAccess       = "acc-1"
	fakeAuthCode     = "code-1"
	fakeRefresh      = "refresh-1"
	// fakeMessage is the "message" of every error answer; it must never
	// reach an error text.
	fakeMessage = "SECRET-MESSAGE-of-the-answer"

	fakeAPIPath    = "/drive/v3"
	fakeUploadPath = "/upload/drive/v3"
	fakeAppData    = "appDataFolder"
)

// Endpoint names for fakeDrive.fail.
const (
	epList   = "list"   // GET /drive/v3/files
	epMedia  = "media"  // GET /drive/v3/files/<id>?alt=media
	epGet    = "get"    // GET /drive/v3/files/<id> (metadata)
	epDelete = "delete" // DELETE /drive/v3/files/<id>
	epOpen   = "open"   // POST or PATCH of /upload/drive/v3/files?uploadType=resumable
)

// States of an upload session.
const (
	sessOpen      = iota // the body is not yet received
	sessCommitted        // the body is committed; status and DELETE give the file
	sessCancelled        // DELETE cancelled it; status gives 499
)

// fakeSession is a resumable upload session (spec §5.4, П6).
type fakeSession struct {
	id     string
	op     string // "create" or "replace"
	name   string // of a created file
	fileID string // the replaced file, or the created one after the commit
	mask   map[string]bool
	state  int
}

// putAnswer is what the emulator does with the body PUT of a session: the
// zero value commits it and answers 200 with the file.
type putAnswer struct {
	reject bool // the body is not committed
	status int  // the answer status instead of 200; 401 has an error body, others an empty one
	drop   bool // the connection is closed without an answer
}

// Special answers of statusAnswers and deleteAnswers.
const (
	answerServe = 0  // served as the session state says
	answerDrop  = -1 // the request is applied and the connection closed without an answer
)

// fakeFailure makes one endpoint answer with an error body of reason; an
// empty reason gives a body that is not JSON.
type fakeFailure struct {
	status int
	reason string
}

type fakeFile struct {
	id, name string
	data     []byte
	md5      string // as the list reports it
	size     int64  // as the list reports it
	created  time.Time
	trashed  bool
	revision int // number of the head revision
}

type fakeRequest struct {
	server       string // api or oauth
	method       string
	path         string
	query        url.Values
	auth         string
	ifMatch      string
	contentRange string
}

// session reports whether r is a request to the URI of an upload session.
func (r fakeRequest) session() bool {
	return r.server == "api" && r.query.Get("upload_id") != ""
}

// fakeDrive emulates Drive v3 on one https server (www.googleapis.com
// serves both the API and the uploads) and the Google token endpoint on
// another (oauth2.googleapis.com). Handlers run under mu; the routing of
// each server is one switch, so later tasks add endpoints there.
type fakeDrive struct {
	api, oauth *httptest.Server

	mu       sync.Mutex
	files    []*fakeFile // in creation order
	nextID   int
	requests []fakeRequest

	// OAuth: the API accepts only access; the token endpoint expects
	// authCode or refresh with the client secret and issues nextAccess.
	access     string
	authCode   string
	refresh    string
	nextAccess string
	tokenForms []url.Values

	// Modes.
	pageLimit    int    // the largest page the list gives, below the asked pageSize; 0 the asked size
	ignoreOrder  bool   // the list keeps the creation order, whatever orderBy asks
	hideFromList bool   // the list gives no file, as a list behind a commit
	denyAll      bool   // the API answers 401 to every token, as for a revoked grant
	redirectTo   string // the API redirects every request to this URL
	fail         map[string]fakeFailure
	beforeMedia  func(d *fakeDrive) // called under the lock before alt=media is served

	// Uploads.
	sessions    map[string]*fakeSession
	nextSession int
	clock       time.Time // createdTime of the next created file; one second later for each
	// onPut decides the answer to the body PUT of a session; it runs under
	// the lock. nil commits and answers 200.
	onPut func(d *fakeDrive, s *fakeSession) putAnswer
	// beforePut runs without the lock before a body PUT is served, so that
	// concurrent uploads can meet.
	beforePut func()
	// statusAnswers and deleteAnswers are taken one per status request or
	// DELETE of a session: a status code answers with it and an empty body,
	// answerServe serves normally and answerDrop applies the request and
	// drops the answer. Empty serves normally.
	statusAnswers, deleteAnswers []int
	// putMD5 and statusMD5 replace the md5Checksum of the file in the
	// answers of the body PUT and of the status request.
	putMD5, statusMD5 string
	// sessionHost replaces the host of the session URIs given in Location.
	sessionHost string
}

func newFakeDrive(t *testing.T) *fakeDrive {
	t.Helper()

	d := &fakeDrive{
		fail:       map[string]fakeFailure{},
		access:     fakeAccess,
		authCode:   fakeAuthCode,
		refresh:    fakeRefresh,
		nextAccess: "acc-2",
		sessions:   map[string]*fakeSession{},
		clock:      fakeUploadTime,
	}

	d.api = httptest.NewTLSServer(http.HandlerFunc(d.serveAPI))
	d.oauth = httptest.NewTLSServer(http.HandlerFunc(d.serveOAuth))

	t.Cleanup(func() {
		d.api.Close()
		d.oauth.Close()
	})

	return d
}

func (d *fakeDrive) endpoints() Endpoints {
	return Endpoints{API: d.api.URL + fakeAPIPath, Upload: d.api.URL + fakeUploadPath, OAuth: d.oauth.URL}
}

// set changes the emulator's state under its lock.
func (d *fakeDrive) set(f func(d *fakeDrive)) {
	d.mu.Lock()
	defer d.mu.Unlock()

	f(d)
}

// put creates a file of name in appDataFolder and returns its ID.
func (d *fakeDrive) put(name string, data []byte, created time.Time) string {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.nextID++

	return d.add(fmt.Sprintf("file-%03d", d.nextID), name, data, created)
}

// putID is put with the given ID.
func (d *fakeDrive) putID(id, name string, data []byte, created time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.add(id, name, data, created)
}

func (d *fakeDrive) add(id, name string, data []byte, created time.Time) string {
	d.files = append(d.files, &fakeFile{
		id: id, name: name, data: data, md5: md5Of(data), size: int64(len(data)), created: created, revision: 1,
	})

	return id
}

// file returns the file with id, or nil; the caller holds the lock.
func (d *fakeDrive) file(id string) *fakeFile {
	for _, f := range d.files {
		if f.id == id && !f.trashed {
			return f
		}
	}

	return nil
}

// replaceData gives the file id new content, as a replacement by another
// device does; the caller holds the lock.
func (d *fakeDrive) replaceData(id string, data []byte) {
	f := d.file(id)
	f.data, f.md5, f.size = data, md5Of(data), int64(len(data))
	f.revision++
}

// remove deletes the file id for good; the caller holds the lock.
func (d *fakeDrive) remove(id string) {
	d.files = slices.DeleteFunc(d.files, func(f *fakeFile) bool { return f.id == id })
}

func (d *fakeDrive) reqs() []fakeRequest {
	d.mu.Lock()
	defer d.mu.Unlock()

	return slices.Clone(d.requests)
}

// count returns the number of API requests with method to path whose query
// has alt=media when media is set, and has none when it is not.
func (d *fakeDrive) count(method, path string, media bool) int {
	n := 0

	for _, r := range d.reqs() {
		if r.server == "api" && r.method == method && r.path == path && (r.query.Get("alt") == "media") == media {
			n++
		}
	}

	return n
}

func (d *fakeDrive) record(server string, r *http.Request) {
	d.requests = append(d.requests, fakeRequest{
		server: server, method: r.Method, path: r.URL.Path, query: r.URL.Query(), auth: r.Header.Get("Authorization"),
		ifMatch: r.Header.Get("If-Match"), contentRange: r.Header.Get("Content-Range"),
	})
}

func (d *fakeDrive) serveAPI(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	before := d.beforePut
	d.mu.Unlock()

	q := r.URL.Query()
	session := q.Get("upload_id") != ""

	if session && before != nil && r.Method == http.MethodPut && r.Header.Get("Content-Range") == "" {
		before()
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	d.record("api", r)

	if d.redirectTo != "" {
		http.Redirect(w, r, d.redirectTo, http.StatusFound)
		return
	}

	// A session URI is the right to its upload: status, cancel and commit
	// work without Authorization, and a wrong token is ignored (П6 е).
	if session {
		d.serveSession(w, r, q.Get("upload_id"))
		return
	}

	if d.denyAll || r.Header.Get("Authorization") != "Bearer "+d.access {
		writeError(w, http.StatusUnauthorized, "authError")
		return
	}

	filesPath := fakeAPIPath + "/files"
	uploadPath := fakeUploadPath + "/files"
	id, isFile := strings.CutPrefix(r.URL.Path, filesPath+"/")
	uploadID, isUploadFile := strings.CutPrefix(r.URL.Path, uploadPath+"/")

	switch {
	case r.Method == http.MethodGet && r.URL.Path == filesPath:
		d.list(w, q)
	case r.Method == http.MethodGet && isFile && q.Get("alt") == "media":
		d.media(w, id)
	case r.Method == http.MethodGet && isFile:
		d.get(w, id, q)
	case r.Method == http.MethodDelete && isFile:
		d.deleteFile(w, id)
	case r.Method == http.MethodPost && r.URL.Path == uploadPath && q.Get("uploadType") == "resumable":
		d.openSession(w, r, "")
	case r.Method == http.MethodPatch && isUploadFile && q.Get("uploadType") == "resumable":
		d.openSession(w, r, uploadID)
	default:
		// Drive answers an unknown path with a page that is not JSON.
		http.Error(w, "Not Found", http.StatusNotFound)
	}
}

// fakeUploadTime is the createdTime of the first file an upload creates:
// later than the files the tests put.
var fakeUploadTime = time.Date(2026, 11, 21, 13, 0, 0, 0, time.UTC)

// fileOf returns the file with id, trashed or not, or nil; the caller holds
// the lock.
func (d *fakeDrive) fileOf(id string) *fakeFile {
	for _, f := range d.files {
		if f.id == id {
			return f
		}
	}

	return nil
}

// get answers files.get of the metadata with the field mask of q.
func (d *fakeDrive) get(w http.ResponseWriter, id string, q url.Values) {
	if d.failed(w, epGet) {
		return
	}

	f := d.fileOf(id)
	if f == nil {
		writeError(w, http.StatusNotFound, "notFound")
		return
	}

	writeJSON(w, http.StatusOK, resource(f, fieldMask(q.Get("fields"))))
}

// deleteFile answers files.delete: the file is gone for good, 204 (П4).
func (d *fakeDrive) deleteFile(w http.ResponseWriter, id string) {
	if d.failed(w, epDelete) {
		return
	}

	if d.fileOf(id) == nil {
		writeError(w, http.StatusNotFound, "notFound")
		return
	}

	d.remove(id)
	w.WriteHeader(http.StatusNoContent)
}

// fieldMask parses a flat field mask such as "id,md5Checksum".
func fieldMask(fields string) map[string]bool {
	if fields == "" {
		return set("kind", "id", "name", "mimeType")
	}

	return set(strings.Split(fields, ",")...)
}

// openSession opens a resumable session: a creation in appDataFolder when
// fileID is empty, else a replacement of fileID (П4). The answer is 200
// with an empty body and the session URI in Location.
func (d *fakeDrive) openSession(w http.ResponseWriter, r *http.Request, fileID string) {
	if d.failed(w, epOpen) {
		return
	}

	var meta struct {
		Name    string   `json:"name"`
		Parents []string `json:"parents"`
	}

	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&meta); err != nil {
		writeError(w, http.StatusBadRequest, "badRequest")
		return
	}

	op := "replace"

	switch {
	case fileID == "" && (meta.Name == "" || !slices.Equal(meta.Parents, []string{fakeAppData})):
		writeError(w, http.StatusBadRequest, "invalid")
		return
	case fileID == "":
		op = "create"
	case d.file(fileID) == nil:
		writeError(w, http.StatusNotFound, "notFound")
		return
	}

	d.nextSession++
	sid := fmt.Sprintf("UPLOAD-SECRET-%03d", d.nextSession)
	d.sessions[sid] = &fakeSession{
		id: sid, op: op, name: meta.Name, fileID: fileID, mask: fieldMask(r.URL.Query().Get("fields")),
	}

	w.Header().Set("Location", d.sessionURI(sid))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
}

// sessionURI is the URI of the session sid, as Location gives it.
func (d *fakeDrive) sessionURI(sid string) string {
	u, _ := url.Parse(d.api.URL + fakeUploadPath + "/files")
	if d.sessionHost != "" {
		u.Host = d.sessionHost
	}

	u.RawQuery = url.Values{"uploadType": {"resumable"}, "upload_id": {sid}}.Encode()

	return u.String()
}

// newSession opens a session as a store that crashed after it did; it
// returns the session ID and URI.
func (d *fakeDrive) newSession(op, fileID, name string) (sid, uri string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.nextSession++
	sid = fmt.Sprintf("UPLOAD-SECRET-%03d", d.nextSession)
	d.sessions[sid] = &fakeSession{
		id: sid, op: op, name: name, fileID: fileID, mask: fieldMask("id,size,md5Checksum,sha256Checksum,headRevisionId"),
	}

	return sid, d.sessionURI(sid)
}

// commitSession commits data to the open session sid, as its lost PUT did.
func (d *fakeDrive) commitSession(sid string, data []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.commit(d.sessions[sid], data)
}

// sessionState returns the state of the session sid; -1 for an unknown one.
func (d *fakeDrive) sessionState(sid string) int {
	d.mu.Lock()
	defer d.mu.Unlock()

	if s, ok := d.sessions[sid]; ok {
		return s.state
	}

	return -1
}

// forgetSessions makes every session unknown, as after its expiry.
func (d *fakeDrive) forgetSessions() {
	d.mu.Lock()
	defer d.mu.Unlock()

	clear(d.sessions)
}

// commit stores data as the file of s; the caller holds the lock. It is
// false when the replaced file is gone.
func (d *fakeDrive) commit(s *fakeSession, data []byte) bool {
	if s.op == "create" {
		d.nextID++
		s.fileID = d.add(fmt.Sprintf("file-%03d", d.nextID), s.name, data, d.clock)
		d.clock = d.clock.Add(time.Second)
	} else {
		if d.file(s.fileID) == nil {
			return false
		}

		d.replaceData(s.fileID, data)
	}

	s.state = sessCommitted

	return true
}

// serveSession answers the requests to a session URI (П6): the body PUT,
// the status PUT with Content-Range "bytes */<size>" and DELETE.
func (d *fakeDrive) serveSession(w http.ResponseWriter, r *http.Request, sid string) {
	s := d.sessions[sid]

	switch {
	case r.Method == http.MethodDelete:
		d.cancelSession(w, s)
	case r.Method == http.MethodPut && strings.HasPrefix(r.Header.Get("Content-Range"), "bytes */"):
		d.sessionStatus(w, s)
	case r.Method == http.MethodPut:
		d.putBody(w, r, s)
	default:
		http.Error(w, "Not Found", http.StatusNotFound)
	}
}

// takeAnswer takes the next special answer of *queue.
func takeAnswer(queue *[]int) int {
	if len(*queue) == 0 {
		return answerServe
	}

	a := (*queue)[0]
	*queue = (*queue)[1:]

	return a
}

// sessionStatus answers a status request: 308 without Range for an open
// session, 200 with the file for a committed one, 499 clientClosedRequest
// for a cancelled one and the text "Not Found" for an unknown one.
func (d *fakeDrive) sessionStatus(w http.ResponseWriter, s *fakeSession) {
	switch a := takeAnswer(&d.statusAnswers); a {
	case answerServe:
	case answerDrop:
		dropConnection(w)
		return
	default:
		w.WriteHeader(a)
		return
	}

	switch {
	case s == nil:
		http.Error(w, "Not Found", http.StatusNotFound)
	case s.state == sessOpen:
		w.WriteHeader(http.StatusPermanentRedirect)
	case s.state == sessCommitted:
		d.writeSessionFile(w, s, d.statusMD5)
	default:
		writeError(w, 499, "clientClosedRequest")
	}
}

// cancelSession answers DELETE: an open session is cancelled with 499, a
// committed one gives 200 with the file and stays committed.
func (d *fakeDrive) cancelSession(w http.ResponseWriter, s *fakeSession) {
	a := takeAnswer(&d.deleteAnswers)
	if a > 0 {
		w.WriteHeader(a)
		return
	}

	switch {
	case s == nil:
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	case s.state == sessOpen:
		s.state = sessCancelled
	}

	if a == answerDrop {
		dropConnection(w)
		return
	}

	if s.state == sessCommitted {
		d.writeSessionFile(w, s, "")
		return
	}

	writeError(w, 499, "clientClosedRequest")
}

// putBody answers the PUT of the whole body as onPut decides; by default it
// commits and answers 200 with the file of the session's field mask.
func (d *fakeDrive) putBody(w http.ResponseWriter, r *http.Request, s *fakeSession) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return
	}

	switch {
	case s == nil:
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	case s.state == sessCancelled:
		// A PUT completed after the cancellation is not committed (П6 д).
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	case s.state == sessCommitted:
		d.writeSessionFile(w, s, d.putMD5)
		return
	}

	var a putAnswer
	if d.onPut != nil {
		a = d.onPut(d, s)
	}

	if !a.reject && !d.commit(s, data) {
		writeError(w, http.StatusNotFound, "notFound")
		return
	}

	switch {
	case a.drop:
		dropConnection(w)
	case a.status == http.StatusUnauthorized:
		writeError(w, a.status, "authError")
	case a.status != 0:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(a.status)
	default:
		d.writeSessionFile(w, s, d.putMD5)
	}
}

// writeSessionFile answers 200 with the file of s by its field mask; md5,
// when set, replaces its md5Checksum.
func (d *fakeDrive) writeSessionFile(w http.ResponseWriter, s *fakeSession, md5 string) {
	f := d.fileOf(s.fileID)
	if f == nil {
		writeError(w, http.StatusNotFound, "notFound")
		return
	}

	out := resource(f, s.mask)
	if md5 != "" {
		out["md5Checksum"] = md5
	}

	writeJSON(w, http.StatusOK, out)
}

// dropConnection closes the connection without an answer.
func dropConnection(w http.ResponseWriter) {
	conn, _, err := http.NewResponseController(w).Hijack()
	if err == nil {
		_ = conn.Close()
	}
}

// failed answers with the failure set for ep, if any.
func (d *fakeDrive) failed(w http.ResponseWriter, ep string) bool {
	f, ok := d.fail[ep]
	if !ok {
		return false
	}

	if f.reason == "" {
		http.Error(w, "Bad Gateway", f.status)
	} else {
		writeError(w, f.status, f.reason)
	}

	return true
}

// listQuery is the only query the emulator understands, as the store sends it.
var listQuery = regexp.MustCompile(`^name='((?:[^'\\]|\\.)*)' and trashed=false$`)

// list answers files.list over appDataFolder (spec §5.2).
func (d *fakeDrive) list(w http.ResponseWriter, q url.Values) {
	if d.failed(w, epList) {
		return
	}

	m := listQuery.FindStringSubmatch(q.Get("q"))
	if q.Get("spaces") != fakeAppData || m == nil {
		writeError(w, http.StatusBadRequest, "invalid")
		return
	}

	name := strings.NewReplacer(`\'`, `'`, `\\`, `\`).Replace(m[1])

	var found []*fakeFile

	for _, f := range d.files {
		if f.name == name && !f.trashed && !d.hideFromList {
			found = append(found, f)
		}
	}

	if q.Get("orderBy") == "createdTime" && !d.ignoreOrder {
		slices.SortStableFunc(found, func(a, b *fakeFile) int { return a.created.Compare(b.created) })
	}

	size, offset, ok := d.page(q)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid")
		return
	}

	mask, top, ok := parseListFields(q.Get("fields"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalidParameter")
		return
	}

	items := make([]map[string]any, 0, max(0, min(len(found), offset+size)-offset))

	for i := offset; i < len(found) && i < offset+size; i++ {
		items = append(items, resource(found[i], mask))
	}

	out := map[string]any{"files": items}
	if offset+size < len(found) && top["nextPageToken"] {
		out["nextPageToken"] = "page-" + strconv.Itoa(offset+size)
	}

	if top["kind"] {
		out["kind"], out["incompleteSearch"] = "drive#fileList", false
	}

	writeJSON(w, http.StatusOK, out)
}

// page returns the size and the offset of the page that q asks for.
func (d *fakeDrive) page(q url.Values) (size, offset int, ok bool) {
	size = 100

	if s := q.Get("pageSize"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 1000 {
			return 0, 0, false
		}

		size = n
	}

	if d.pageLimit > 0 {
		size = min(size, d.pageLimit)
	}

	if t := q.Get("pageToken"); t != "" {
		n, err := strconv.Atoi(strings.TrimPrefix(t, "page-"))
		if err != nil || !strings.HasPrefix(t, "page-") {
			return 0, 0, false
		}

		offset = n
	}

	return size, offset, true
}

// fileFields are the fields of a file resource the emulator knows.
var fileFields = []string{"kind", "id", "name", "mimeType", "size", "createdTime", "md5Checksum", "sha256Checksum", "headRevisionId", "trashed"}

// parseListFields parses the field mask of files.list, such as
// "files(id,size),nextPageToken". Without a mask Drive gives its default
// fields; "files" without a list gives all of them.
func parseListFields(mask string) (file, top map[string]bool, ok bool) {
	if mask == "" {
		return set("kind", "id", "name", "mimeType"), set("kind", "nextPageToken", "files"), true
	}

	var parts []string

	depth, start := 0, 0

	for i, c := range mask {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, mask[start:i])
				start = i + 1
			}
		}
	}

	parts = append(parts, mask[start:])
	file, top = map[string]bool{}, map[string]bool{}

	for _, p := range parts {
		name, inner, sub := strings.Cut(p, "(")

		switch {
		case sub && (name != "files" || !strings.HasSuffix(inner, ")")):
			return nil, nil, false
		case sub:
			for f := range strings.SplitSeq(strings.TrimSuffix(inner, ")"), ",") {
				file[f] = true
			}
		case name == "files":
			file = set(fileFields...)
		}

		if name != "files" && name != "nextPageToken" && name != "kind" {
			return nil, nil, false
		}

		top[name] = true
	}

	return file, top, true
}

func set(keys ...string) map[string]bool {
	m := map[string]bool{}
	for _, k := range keys {
		m[k] = true
	}

	return m
}

// resource is the file resource f with the fields of mask only.
func resource(f *fakeFile, mask map[string]bool) map[string]any {
	sha := sha256.Sum256(f.data)
	all := map[string]any{
		"kind":           "drive#file",
		"id":             f.id,
		"name":           f.name,
		"mimeType":       "application/octet-stream",
		"size":           strconv.FormatInt(f.size, 10),
		"createdTime":    f.created.UTC().Format("2006-01-02T15:04:05.000Z"),
		"md5Checksum":    f.md5,
		"sha256Checksum": hex.EncodeToString(sha[:]),
		"headRevisionId": f.id + "-rev-" + strconv.Itoa(f.revision),
		"trashed":        f.trashed,
	}

	out := map[string]any{}

	for k, v := range all {
		if mask[k] {
			out[k] = v
		}
	}

	return out
}

// media answers files.get with alt=media: the bytes of the file.
func (d *fakeDrive) media(w http.ResponseWriter, id string) {
	if d.beforeMedia != nil {
		d.beforeMedia(d)
	}

	if d.failed(w, epMedia) {
		return
	}

	f := d.file(id)
	if f == nil {
		writeError(w, http.StatusNotFound, "notFound")
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(f.data)
}

// serveOAuth is the token endpoint of Google: the exchange of a code and
// the refresh, both only with the client secret (П2).
func (d *fakeDrive) serveOAuth(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.record("oauth", r)

	if r.Method != http.MethodPost || r.URL.Path != "/token" {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	f := r.PostForm
	d.tokenForms = append(d.tokenForms, f)

	if f.Get("client_secret") == "" {
		writeOAuthError(w, "invalid_request", "client_secret is missing.")
		return
	}

	if f.Get("client_id") != fakeClientID || f.Get("client_secret") != fakeClientSecret {
		writeOAuthError(w, "invalid_client", fakeMessage)
		return
	}

	out := map[string]any{
		"access_token": d.nextAccess, "expires_in": 3599, "token_type": "Bearer", "scope": scopeAppData,
	}

	switch f.Get("grant_type") {
	case "authorization_code":
		if f.Get("code") != d.authCode || f.Get("code_verifier") == "" || f.Get("redirect_uri") == "" {
			writeOAuthError(w, "invalid_grant", fakeMessage)
			return
		}

		out["refresh_token"] = d.refresh
	case "refresh_token":
		// The refresh gives no new refresh token (П2).
		if f.Get("refresh_token") != d.refresh {
			writeOAuthError(w, "invalid_grant", fakeMessage)
			return
		}
	default:
		writeOAuthError(w, "unsupported_grant_type", fakeMessage)
		return
	}

	d.access = d.nextAccess

	writeJSON(w, http.StatusOK, out)
}

func (d *fakeDrive) forms() []url.Values {
	d.mu.Lock()
	defer d.mu.Unlock()

	return slices.Clone(d.tokenForms)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes an error answer of the Drive API.
func writeError(w http.ResponseWriter, status int, reason string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{
		"code":    status,
		"message": fakeMessage,
		"errors":  []map[string]any{{"message": fakeMessage, "domain": "global", "reason": reason}},
	}})
}

// writeOAuthError writes an error answer of the token endpoint.
func writeOAuthError(w http.ResponseWriter, code, description string) {
	status := cmp.Or(map[string]int{"invalid_client": http.StatusUnauthorized}[code], http.StatusBadRequest)
	writeJSON(w, status, map[string]any{"error": code, "error_description": description})
}

func md5Of(data []byte) string {
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])
}
