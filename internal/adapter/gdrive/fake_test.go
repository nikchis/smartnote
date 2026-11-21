package gdrive

import (
	"cmp"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	epList  = "list"  // GET /drive/v3/files
	epMedia = "media" // GET /drive/v3/files/<id>?alt=media
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
	server string // api or oauth
	method string
	path   string
	query  url.Values
	auth   string
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
	pageLimit   int    // the largest page the list gives, below the asked pageSize; 0 the asked size
	ignoreOrder bool   // the list keeps the creation order, whatever orderBy asks
	redirectTo  string // the API redirects every request to this URL
	fail        map[string]fakeFailure
	beforeMedia func(d *fakeDrive) // called under the lock before alt=media is served
}

func newFakeDrive(t *testing.T) *fakeDrive {
	t.Helper()

	d := &fakeDrive{
		fail:       map[string]fakeFailure{},
		access:     fakeAccess,
		authCode:   fakeAuthCode,
		refresh:    fakeRefresh,
		nextAccess: "acc-2",
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
	})
}

func (d *fakeDrive) serveAPI(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.record("api", r)

	if d.redirectTo != "" {
		http.Redirect(w, r, d.redirectTo, http.StatusFound)
		return
	}

	if r.Header.Get("Authorization") != "Bearer "+d.access {
		writeError(w, http.StatusUnauthorized, "authError")
		return
	}

	filesPath := fakeAPIPath + "/files"
	q := r.URL.Query()

	switch {
	case r.Method == http.MethodGet && r.URL.Path == filesPath:
		d.list(w, q)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, filesPath+"/") && q.Get("alt") == "media":
		d.media(w, strings.TrimPrefix(r.URL.Path, filesPath+"/"))
	default:
		// Drive answers an unknown path with a page that is not JSON.
		http.Error(w, "Not Found", http.StatusNotFound)
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
		if f.name == name && !f.trashed {
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
