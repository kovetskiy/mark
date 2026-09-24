// Package confluencetest provides an in-memory fake of the Confluence REST API
// for use in tests, in the spirit of net/http/httptest.
//
// Nothing in the repository could previously exercise a code path that talks to
// Confluence, which is why the confluence and page packages had almost no
// coverage. A test creates a Server, points confluence.NewAPI at its URL, and
// drives real API calls against real HTTP.
//
// The package deliberately does not import the confluence package: doing so
// would create an import cycle for in-package tests of confluence itself. The
// types here are therefore standalone and describe only what the fake serves.
package confluencetest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Page is a page, blogpost or folder held by the fake.
type Page struct {
	ID       string
	Title    string
	Type     string
	SpaceKey string
	ParentID string
	Version  int64
	Message  string
	Body     string
	Labels   []string

	// Trashed and Archived record what became of a page, so a test can tell
	// "moved to the trash" from "gone" and from "archived".
	Trashed  bool
	Archived bool
}

// Status is what v1 reports for the page and, more to the point, what it
// filters lookups on. Trashed wins over archived: a page can be archived first
// and trashed afterwards, and the trash is the state it is in.
func (p *Page) Status() string {
	switch {
	case p.Trashed:
		return "trashed"
	case p.Archived:
		return "archived"
	default:
		return "current"
	}
}

// Attachment is a file attached to a page.
type Attachment struct {
	ID       string
	PageID   string
	Filename string
	Comment  string
	// MinorEdit is the minorEdit form field of the latest upload, exactly as
	// sent: empty when the field was missing.
	MinorEdit string
}

// InlineComment is a comment returned by the child/comment endpoint.
type InlineComment struct {
	Location  string
	MarkerRef string
	Selection string
}

// Folder is a Confluence folder. Folders are a Cloud-only, v2-only concept.
type Folder struct {
	ID         string
	Title      string
	SpaceID    string
	SpaceKey   string
	ParentID   string
	ParentType string
}

// Space is a Confluence space. HomepageID may be empty.
type Space struct {
	ID         string
	Key        string
	HomepageID string
}

// User is returned by the user search and current-user endpoints.
type User struct {
	AccountID string
	UserKey   string
	Username  string
	FullName  string
}

// Request is a single recorded inbound request.
type Request struct {
	Method string
	Path   string
	Query  string
}

// FailFunc can fail a request before the fake handles it. Returning handled
// true short-circuits with the given status and body; the request is still
// recorded, so retry behaviour can be asserted by counting requests.
type FailFunc func(r *http.Request) (status int, body string, handled bool)

// Server is an in-memory Confluence exposed over HTTP.
type Server struct {
	*httptest.Server

	mu          sync.Mutex
	pages       map[string]*Page
	spaces      map[string]*Space
	attachments []*Attachment
	properties  []*SpaceProperty
	childOrder  map[string][]string
	folders     []*Folder
	comments    map[string][]InlineComment
	users       []User
	currentUser User
	requests    []Request
	nextID      int

	// PageSizeHint is echoed in paginated responses; tests that exercise
	// pagination set the page count by adding more items than the client's
	// page size rather than by changing this.
	fail FailFunc

	// SiteBase is echoed as `_links.base` on content create/search responses
	// when set. Empty omits the field so tests can still exercise the client's
	// api.BaseURL fallback. Real Confluence Cloud puts the tenant wiki URL
	// here even when the request went through api.atlassian.com.
	SiteBase string
}

// New starts a fake Confluence and registers cleanup with t.
func New(t *testing.T) *Server {
	t.Helper()

	s := &Server{
		pages:    map[string]*Page{},
		spaces:   map[string]*Space{},
		comments: map[string][]InlineComment{},
		nextID:   1000,
		currentUser: User{
			AccountID: "acct-current",
			Username:  "current",
			FullName:  "Current User",
		},
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.Close)
	return s
}

// SetFail installs a failure-injection hook. Pass nil to remove it.
func (s *Server) SetFail(f FailFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = f
}

// Requests returns a copy of every request the fake has received, in order.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Request, len(s.requests))
	copy(out, s.requests)
	return out
}

// CountRequests returns how many recorded requests used the given method and
// had a path containing substr.
func (s *Server) CountRequests(method, substr string) int {
	var n int
	for _, r := range s.Requests() {
		if r.Method == method && strings.Contains(r.Path, substr) {
			n++
		}
	}
	return n
}

// ResetRequests clears the recorded request log.
func (s *Server) ResetRequests() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = nil
}

func (s *Server) newID() string {
	s.nextID++
	return strconv.Itoa(s.nextID)
}

// AddSpace registers a space and returns it.
func (s *Server) AddSpace(key string) *Space {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp := &Space{ID: s.newID(), Key: key}
	s.spaces[key] = sp
	return sp
}

// AddPage adds a page and returns it. Pass an empty parentID for a root page.
func (s *Server) AddPage(spaceKey, title, pageType, parentID string) *Page {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.spaces[spaceKey]; !ok {
		s.spaces[spaceKey] = &Space{ID: s.newID(), Key: spaceKey}
	}
	p := &Page{
		ID:       s.newID(),
		Title:    title,
		Type:     pageType,
		SpaceKey: spaceKey,
		ParentID: parentID,
		Version:  1,
	}
	s.pages[p.ID] = p
	s.placeChild(p.ParentID, p.ID, "")
	return p
}

// AddFolder registers a folder and returns it. Pass an empty parentID for one
// hanging directly off a page anchor.
func (s *Server) AddFolder(spaceKey, title, parentID, parentType string) *Folder {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.spaces[spaceKey]; !ok {
		s.spaces[spaceKey] = &Space{ID: s.newID(), Key: spaceKey}
	}
	f := &Folder{
		ID:         s.newID(),
		Title:      title,
		SpaceID:    s.spaces[spaceKey].ID,
		SpaceKey:   spaceKey,
		ParentID:   parentID,
		ParentType: parentType,
	}
	s.folders = append(s.folders, f)
	return f
}

// Folder returns the stored folder with the given ID, or nil.
func (s *Server) Folder(id string) *Folder {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.folders {
		if f.ID == id {
			cp := *f
			return &cp
		}
	}
	return nil
}

// RenameFolder retitles a folder, as somebody doing it in the Confluence UI
// would -- which is the case mark cannot see coming.
func (s *Server) RenameFolder(id, title string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.folders {
		if f.ID == id {
			f.Title = title
		}
	}
}

// Folders returns every stored folder.
func (s *Server) Folders() []Folder {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Folder, 0, len(s.folders))
	for _, f := range s.folders {
		out = append(out, *f)
	}
	return out
}

// DeletePage removes a page, as someone deleting it in the Confluence UI would.
func (s *Server) DeletePage(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pages, id)
}

// archiveContent serves the bulk archive endpoint, which takes a list of pages
// rather than addressing one by id.
func (s *Server) archiveContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// The documented schema is a number, and Confluence rejects a quoted one.
	// Decoding into int64 makes the fake as strict as the real thing.
	var payload struct {
		Pages []struct {
			ID int64 `json:"id"`
		} `json:"pages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "bad body"})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, item := range payload.Pages {
		if p, ok := s.pages[strconv.FormatInt(item.ID, 10)]; ok {
			p.Archived = true
		}
	}

	// 202 with a task to watch: the archiving itself happens afterwards.
	writeJSON(w, http.StatusAccepted, map[string]any{
		"id":    "archive-task",
		"links": map[string]any{"status": "/rest/api/longtask/archive-task"},
	})
}

// SetHomepage marks a page as the space homepage.
func (s *Server) SetHomepage(spaceKey, pageID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sp, ok := s.spaces[spaceKey]; ok {
		sp.HomepageID = pageID
	}
}

// AddAttachment attaches a file to a page.
func (s *Server) AddAttachment(pageID, filename, comment string) *Attachment {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := &Attachment{ID: s.newID(), PageID: pageID, Filename: filename, Comment: comment}
	s.attachments = append(s.attachments, a)
	return a
}

// AddComment adds an inline comment to a page.
func (s *Server) AddComment(pageID string, c InlineComment) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.comments[pageID] = append(s.comments[pageID], c)
}

// AddUser registers a user discoverable through the user search endpoint.
func (s *Server) AddUser(u User) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users = append(s.users, u)
}

// Page returns the stored page with the given ID, or nil.
func (s *Server) Page(id string) *Page {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.pages[id]; ok {
		cp := *p
		cp.Labels = append([]string(nil), p.Labels...)
		return &cp
	}
	return nil
}

// RenamePage retitles a page, as somebody doing it in the Confluence UI would
// -- which is the case mark cannot see coming, and the one that strands every
// document declaring the old title as its parent.
func (s *Server) RenamePage(id, title string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if p, ok := s.pages[id]; ok {
		p.Title = title
	}
}

// AddLabel stands in for somebody labelling a page in the Confluence web UI.
func (s *Server) AddLabel(id, label string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if p, ok := s.pages[id]; ok {
		p.Labels = append(p.Labels, label)
	}
}

// EditPage stands in for somebody editing a page in the Confluence web UI: the
// body changes and the version moves on, with no involvement from mark.
func (s *Server) EditPage(id, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if p, ok := s.pages[id]; ok {
		p.Body = body
		p.Version++
		p.Message = "edited by hand"
	}
}

// Attachments returns the attachments stored for a page.
func (s *Server) Attachments(pageID string) []Attachment {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Attachment
	for _, a := range s.attachments {
		if a.PageID == pageID {
			out = append(out, *a)
		}
	}
	return out
}

// ancestorsOf walks ParentID links from the root down to the page's parent.
// Callers must hold s.mu.
func (s *Server) ancestorsOf(p *Page) []*Page {
	var chain []*Page
	seen := map[string]bool{p.ID: true}
	for cur := p; cur.ParentID != ""; {
		parent, ok := s.pages[cur.ParentID]
		if !ok || seen[parent.ID] {
			break
		}
		seen[parent.ID] = true
		chain = append(chain, parent)
		cur = parent
	}
	// chain is leaf-to-root; Confluence returns root-first.
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}

// expansions is v1's expand parameter as a set, with whatever the endpoint
// expands without being asked added to it.
//
// v1 leaves everything else out and names it under _expandable instead. A fake
// that sent the ancestors and the body regardless would certify a client that
// forgot to ask for them -- and that client would find an empty ancestor chain
// or an empty body on a real instance, and act on it.
func expansions(r *http.Request, defaults ...string) map[string]bool {
	set := map[string]bool{}
	for _, name := range defaults {
		set[name] = true
	}
	for _, name := range strings.Split(r.URL.Query().Get("expand"), ",") {
		if name = strings.TrimSpace(name); name != "" {
			set[name] = true
		}
	}
	return set
}

// nestedExpansions is the part of an expand set under one property: the
// ancestors in expand=homepage,homepage.ancestors, say.
func nestedExpansions(expand map[string]bool, property string) map[string]bool {
	set := map[string]bool{}
	for name := range expand {
		if rest, ok := strings.CutPrefix(name, property+"."); ok {
			set[rest] = true
		}
	}
	return set
}

// fullExpansion is what the fake answers a create or an update with. Those
// come back expanded on a real instance too, without being asked.
var fullExpansion = map[string]bool{"ancestors": true, "version": true, "body.storage": true}

// pageJSON renders a page the way v1 does, with only what expand names.
//
// Callers must hold s.mu.
func (s *Server) pageJSON(p *Page, expand map[string]bool) map[string]any {
	out := map[string]any{
		"id":     p.ID,
		"title":  p.Title,
		"type":   p.Type,
		"status": p.Status(),
		"_links": s.pageLinks(p),
	}
	expandable := map[string]any{}
	if expand["ancestors"] {
		ancestors := []map[string]any{}
		for _, a := range s.ancestorsOf(p) {
			ancestors = append(ancestors, map[string]any{"id": a.ID, "title": a.Title})
		}
		out["ancestors"] = ancestors
	} else {
		expandable["ancestors"] = ""
	}
	if expand["version"] {
		out["version"] = map[string]any{
			"number":  p.Version,
			"message": p.Message,
		}
	} else {
		expandable["version"] = ""
	}
	if expand["body.storage"] {
		out["body"] = map[string]any{
			"storage": map[string]any{"value": p.Body},
		}
	} else {
		expandable["body"] = ""
	}
	if len(expandable) > 0 {
		out["_expandable"] = expandable
	}
	return out
}

func (s *Server) pageLinks(p *Page) map[string]any {
	links := map[string]any{"webui": "/display/" + p.SpaceKey + "/" + p.ID}
	if s.SiteBase != "" {
		links["base"] = s.SiteBase
	}
	return links
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// v1MaxLimit and v2MaxLimit are the most items the fake hands out per page,
// whatever limit a request asks for.
//
// Confluence caps limit on its side and answers a larger request with a short
// page and a next link, rather than an error. v2 documents 250 as its maximum.
// v1 caps per endpoint and, on Server and Data Center, per instance through
// its max-results setting; 50 is at the conservative end of what those
// allow, and it sits below the 100 mark asks for, so every v1 listing the
// client pages through is actually paged here. A fake that honoured any limit
// could never tell a client that pages correctly from one that stops on the
// first short page.
const (
	v1MaxLimit = 50
	v2MaxLimit = 250
)

// requestLimit is the request's limit, capped at max; max when absent.
func requestLimit(r *http.Request, maxLimit int) int {
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return min(n, maxLimit)
		}
	}
	return maxLimit
}

// paginate slices items by the request's start/limit and reports whether a
// further page exists, mirroring the Confluence _links.next convention the
// client relies on to stop looping.
func paginate[T any](r *http.Request, items []T) (page []T, hasNext bool) {
	limit := requestLimit(r, v1MaxLimit)
	start := 0
	if v := r.URL.Query().Get("start"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			start = n
		}
	}
	if start >= len(items) {
		return nil, false
	}
	end := min(start+limit, len(items))
	return items[start:end], end < len(items)
}

// cursorPage slices items the way the v2 API paginates: an opaque cursor
// rather than a start offset, handed back inside a whole _links.next URL.
//
// The fake's cursor is the index of the next item. That is opaque enough that a
// client cannot do arithmetic on it -- it has to read the link -- and simple
// enough to be obviously right.
func cursorPage[T any](r *http.Request, items []T) (page []T, next string) {
	limit := requestLimit(r, v2MaxLimit)
	start := 0
	if v := r.URL.Query().Get("cursor"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			start = n
		}
	}
	if start >= len(items) {
		return nil, ""
	}
	end := min(start+limit, len(items))
	if end < len(items) {
		next = strconv.Itoa(end)
	}
	return items[start:end], next
}

func linksWithNext(hasNext bool) map[string]any {
	links := map[string]any{"base": "/wiki", "context": "/wiki"}
	if hasNext {
		links["next"] = "/rest/api/next-page"
	}
	return links
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, Request{
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.RawQuery,
	})
	fail := s.fail
	s.mu.Unlock()

	if fail != nil {
		if status, body, handled := fail(r); handled {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
			return
		}
	}

	path := strings.TrimSuffix(r.URL.Path, "/")

	// The api.atlassian.com gateway mounts a site under /ex/confluence/<id>,
	// and a test that stands in for the gateway uses that base path.
	if rest, ok := strings.CutPrefix(path, "/ex/confluence/"); ok {
		_, rest, _ = strings.Cut(rest, "/")
		path = "/" + rest
	}

	switch {
	case strings.HasPrefix(path, "/rest/api"):
		s.handleV1(w, r, strings.TrimPrefix(path, "/rest/api"))
	case strings.HasPrefix(path, "/api/v2"):
		s.handleV2(w, r, strings.TrimPrefix(path, "/api/v2"))
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleV1(w http.ResponseWriter, r *http.Request, path string) {
	switch {
	case path == "/content":
		switch r.Method {
		case http.MethodGet:
			s.searchContent(w, r)
		case http.MethodPost:
			s.createContent(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}

	case path == "/user/current":
		s.mu.Lock()
		u := s.currentUser
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"accountId": u.AccountID,
			"userKey":   u.UserKey,
			"username":  u.Username,
		})

	case path == "/search/user" || path == "/search":
		// The same endpoint serves user lookup and the CQL folder search; the
		// query says which.
		if strings.Contains(r.URL.Query().Get("cql"), "type=folder") {
			s.searchFolder(w, r)
			return
		}
		s.searchUser(w, r)

	case strings.HasPrefix(path, "/space/"):
		s.getSpace(w, r, strings.TrimPrefix(path, "/space/"))

	case path == "/content/archive":
		s.archiveContent(w, r)

	case strings.HasPrefix(path, "/content/"):
		rest := strings.TrimPrefix(path, "/content/")
		id, sub, _ := strings.Cut(rest, "/")
		switch {
		case sub == "":
			s.contentByID(w, r, id)
		case sub == "child/attachment":
			s.childAttachment(w, r, id)
		case strings.HasPrefix(sub, "child/attachment/"):
			// .../child/attachment/{attachID}/data -- attachment update
			attachID, rest, _ := strings.Cut(strings.TrimPrefix(sub, "child/attachment/"), "/")
			if rest != "data" {
				http.NotFound(w, r)
				return
			}
			s.updateAttachment(w, r, id, attachID)
		case sub == "child/page":
			s.childPages(w, r, id)
		case sub == "child/comment":
			s.childComment(w, r, id)
		case strings.HasPrefix(sub, "move/"):
			rest := strings.TrimPrefix(sub, "move/")
			position, target, _ := strings.Cut(rest, "/")
			s.moveContent(w, r, id, position, target)
		case sub == "label":
			s.label(w, r, id)
		case sub == "property":
			// v1 content properties: POST creates, GET without a key lists.
			s.contentProperties(w, r, id, "")
		case strings.HasPrefix(sub, "property/"):
			s.contentProperties(w, r, id, strings.TrimPrefix(sub, "property/"))
		case sub == "restriction" || strings.HasPrefix(sub, "restriction"):
			writeJSON(w, http.StatusOK, map[string]any{"results": []any{}})
		default:
			http.NotFound(w, r)
		}

	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleV2(w http.ResponseWriter, r *http.Request, path string) {
	switch path {
	case "/spaces":
		key := r.URL.Query().Get("keys")
		s.mu.Lock()
		defer s.mu.Unlock()
		results := []map[string]any{}
		for _, sp := range s.spaces {
			if key == "" || sp.Key == key {
				// v2 reports the homepage as an id, unlike v1 which expands the
				// whole page object. FindHomePage's v2 fallback resolves it with
				// a second call, so the field has to be here for that path to be
				// exercised.
				results = append(results, map[string]any{
					"id":         sp.ID,
					"key":        sp.Key,
					"homepageId": sp.HomepageID,
				})
			}
		}
		sort.Slice(results, func(i, j int) bool {
			return results[i]["key"].(string) < results[j]["key"].(string)
		})
		links := map[string]any{}
		if s.SiteBase != "" {
			links["base"] = s.SiteBase
		}
		writeJSON(w, http.StatusOK, map[string]any{"results": results, "_links": links})
	case "/folders":
		s.createFolder(w, r)

	case "/pages", "/blogposts":
		pageType := "page"
		if path == "/blogposts" {
			pageType = "blogpost"
		}
		switch r.Method {
		case http.MethodGet:
			s.listContentV2(w, r, pageType)
		case http.MethodPost:
			s.createPageV2(w, r, pageType)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}

	default:
		if folderID, ok := strings.CutPrefix(path, "/folders/"); ok && r.Method == http.MethodGet {
			s.getFolder(w, folderID)
			return
		}
		// /pages/{id}/direct-children -- children of every type, which is what
		// separates it from the v1 child/page listing.
		if rest, ok := strings.CutPrefix(path, "/pages/"); ok {
			pageID, sub, _ := strings.Cut(rest, "/")
			if sub == "direct-children" {
				s.directChildren(w, r, pageID)
				return
			}
		}
		for _, collection := range []string{"pages", "blogposts"} {
			rest, ok := strings.CutPrefix(path, "/"+collection+"/")
			if !ok {
				continue
			}
			contentID, sub, _ := strings.Cut(rest, "/")
			pageType := strings.TrimSuffix(collection, "s")
			if sub == "" {
				s.contentV2ByID(w, r, contentID, pageType)
				return
			}
			// As with the object itself, a page's and a blogpost's labels,
			// attachments and properties are only found under their own
			// collection: Confluence answers 404 for a blogpost id under
			// /pages, and so does this.
			if !s.isContentOfType(contentID, pageType) {
				writeJSON(w, http.StatusNotFound, map[string]any{"message": "no content with id " + contentID})
				return
			}
			switch {
			case sub == "labels":
				s.labelsV2(w, r, collection, contentID)
				return
			case sub == "attachments":
				s.attachmentsV2(w, r, collection, contentID)
				return
			case sub == "properties" || strings.HasPrefix(sub, "properties/"):
				s.spaceProperties(w, r, collection, contentID, strings.TrimPrefix(strings.TrimPrefix(sub, "properties"), "/"))
				return
			}
		}
		// /spaces/{id}/properties and /spaces/{id}/properties/{propertyID}
		if rest, ok := strings.CutPrefix(path, "/spaces/"); ok {
			spaceID, sub, _ := strings.Cut(rest, "/")
			if propertyID, ok := strings.CutPrefix(sub, "properties"); ok {
				s.spaceProperties(w, r, "spaces", spaceID, strings.TrimPrefix(propertyID, "/"))
				return
			}
		}
		http.NotFound(w, r)
	}
}

// isContentOfType reports whether id names content of the given type.
func (s *Server) isContentOfType(id, pageType string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pages[id]
	return ok && p.Type == pageType
}

// SpaceProperty is a key/value pair held against a space (v2) or a page (v1).
// OwnerID is the space id or the content id accordingly; the fake stores both
// kinds in one list because nothing distinguishes them but the route.
type SpaceProperty struct {
	ID      string
	OwnerID string
	Key     string
	Value   json.RawMessage
	Version int
}

// SpaceProperty returns the property stored for a space under key, or nil.
func (s *Server) SpaceProperty(ownerID, key string) *SpaceProperty {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.properties {
		if p.OwnerID == ownerID && p.Key == key {
			cp := *p
			return &cp
		}
	}
	return nil
}

// SetSpaceProperty seeds a property without going through HTTP.
func (s *Server) SetSpaceProperty(ownerID, key string, value []byte) *SpaceProperty {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.properties {
		if p.OwnerID == ownerID && p.Key == key {
			p.Value = append(json.RawMessage(nil), value...)
			p.Version++
			cp := *p
			return &cp
		}
	}
	p := &SpaceProperty{
		ID:      s.newID(),
		OwnerID: ownerID,
		Key:     key,
		Value:   append(json.RawMessage(nil), value...),
		Version: 1,
	}
	s.properties = append(s.properties, p)
	cp := *p
	return &cp
}

// contentProperties serves the v1 content property API, which differs from the
// v2 space one in two ways that matter: a read of a single key returns the
// property object directly rather than a collection, and an update addresses it
// by key rather than by property id.
// moveContent reparents a page, which is how mark puts an existing page inside
// a folder.
func (s *Server) moveContent(w http.ResponseWriter, r *http.Request, contentID, position, targetID string) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.pages[contentID]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "no such content"})
		return
	}

	// The target may be a folder or another page; the fake only has to record
	// that the move happened.
	switch position {
	case "append":
		p.ParentID = targetID
		s.placeChild(targetID, p.ID, "")
	case "before", "after":
		sibling, ok := s.pages[targetID]
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "no such sibling"})
			return
		}
		// The target is a sibling here, not the new parent, so the page joins
		// whatever parent the sibling has.
		p.ParentID = sibling.ParentID
		if position == "after" {
			s.placeChild(sibling.ParentID, p.ID, targetID)
		} else {
			s.placeBefore(sibling.ParentID, p.ID, targetID)
		}
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "unknown position " + position})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"id": p.ID})
}

func (s *Server) contentProperties(w http.ResponseWriter, r *http.Request, contentID, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	propertyJSON := func(p *SpaceProperty) map[string]any {
		return map[string]any{
			"id":      p.ID,
			"key":     p.Key,
			"value":   p.Value,
			"version": map[string]any{"number": p.Version},
		}
	}

	find := func(k string) *SpaceProperty {
		for _, p := range s.properties {
			if p.OwnerID == contentID && p.Key == k {
				return p
			}
		}
		return nil
	}

	switch r.Method {
	case http.MethodGet:
		if key == "" {
			// Paginated, because a real homepage carries the properties of
			// every app installed on the instance and not just this one's.
			var owned []*SpaceProperty
			for _, p := range s.properties {
				if p.OwnerID == contentID {
					owned = append(owned, p)
				}
			}
			page, hasNext := paginate(r, owned)
			results := []map[string]any{}
			for _, p := range page {
				results = append(results, propertyJSON(p))
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"results": results,
				"_links":  linksWithNext(hasNext),
			})
			return
		}
		p := find(key)
		if p == nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "no such property"})
			return
		}
		writeJSON(w, http.StatusOK, propertyJSON(p))

	case http.MethodPost:
		var payload struct {
			Key   string          `json:"key"`
			Value json.RawMessage `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"message": "bad payload"})
			return
		}
		if find(payload.Key) != nil {
			writeJSON(w, http.StatusConflict, map[string]any{"message": "property already exists"})
			return
		}
		p := &SpaceProperty{
			ID:      s.newID(),
			OwnerID: contentID,
			Key:     payload.Key,
			Value:   payload.Value,
			Version: 1,
		}
		s.properties = append(s.properties, p)
		writeJSON(w, http.StatusOK, propertyJSON(p))

	case http.MethodPut:
		var payload struct {
			ID      string          `json:"id"`
			Key     string          `json:"key"`
			Value   json.RawMessage `json:"value"`
			Version struct {
				Number int `json:"number"`
			} `json:"version"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"message": "bad payload"})
			return
		}
		// Atlassian documents the property id as required on this endpoint, so
		// the fake insists on it. A fake that is more forgiving than the real
		// thing is worse than no fake: it certifies code that cannot work.
		if payload.ID == "" {
			writeJSON(w, http.StatusBadRequest,
				map[string]any{"message": "id is required when updating a content property"})
			return
		}
		p := find(key)
		if p == nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "no such property"})
			return
		}
		if payload.Version.Number != p.Version+1 {
			writeJSON(w, http.StatusConflict, map[string]any{"message": "version conflict"})
			return
		}
		p.Value = payload.Value
		p.Version = payload.Version.Number
		writeJSON(w, http.StatusOK, propertyJSON(p))

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) spaceProperties(w http.ResponseWriter, r *http.Request, collection, ownerID, propertyID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	propertyJSON := func(p *SpaceProperty) map[string]any {
		return map[string]any{
			"id":      p.ID,
			"key":     p.Key,
			"value":   p.Value,
			"version": map[string]any{"number": p.Version},
		}
	}

	switch r.Method {
	case http.MethodGet:
		key := r.URL.Query().Get("key")
		var owned []*SpaceProperty
		for _, p := range s.properties {
			if p.OwnerID == ownerID && (key == "" || p.Key == key) {
				owned = append(owned, p)
			}
		}
		// v2 pages by cursor, and names the next page as a URL rather than as
		// an offset the client could have worked out for itself.
		page, next := cursorPage(r, owned)
		results := []map[string]any{}
		for _, p := range page {
			results = append(results, propertyJSON(p))
		}
		links := map[string]any{}
		if next != "" {
			links["next"] = fmt.Sprintf(
				"/api/v2/%s/%s/properties?cursor=%s&limit=%s",
				collection, ownerID, next, r.URL.Query().Get("limit"),
			)
		}
		writeJSON(w, http.StatusOK, map[string]any{"results": results, "_links": links})

	case http.MethodPost:
		var payload struct {
			Key   string          `json:"key"`
			Value json.RawMessage `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"message": "bad payload"})
			return
		}
		for _, p := range s.properties {
			if p.OwnerID == ownerID && p.Key == payload.Key {
				writeJSON(w, http.StatusConflict, map[string]any{"message": "property already exists"})
				return
			}
		}
		p := &SpaceProperty{
			ID:      s.newID(),
			OwnerID: ownerID,
			Key:     payload.Key,
			Value:   payload.Value,
			Version: 1,
		}
		s.properties = append(s.properties, p)
		writeJSON(w, http.StatusCreated, propertyJSON(p))

	case http.MethodPut:
		var payload struct {
			Key     string          `json:"key"`
			Value   json.RawMessage `json:"value"`
			Version struct {
				Number int `json:"number"`
			} `json:"version"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"message": "bad payload"})
			return
		}
		for _, p := range s.properties {
			if p.ID != propertyID || p.OwnerID != ownerID {
				continue
			}
			// Confluence versions properties: an update has to name the version
			// it supersedes, and a stale one loses.
			if payload.Version.Number != p.Version+1 {
				writeJSON(w, http.StatusConflict, map[string]any{"message": "version conflict"})
				return
			}
			p.Value = payload.Value
			p.Version = payload.Version.Number
			writeJSON(w, http.StatusOK, propertyJSON(p))
			return
		}
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "no such property"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) searchContent(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	spaceKey, title, pageType := q.Get("spaceKey"), q.Get("title"), q.Get("type")

	// An absent status means current, which is the behaviour that hides an
	// archived page from a lookup while it goes on owning its title. A fake
	// that returned archived pages here would certify a client that can never
	// see the problem. Comma-separated, as v1 accepts.
	wanted := map[string]bool{}
	for _, status := range strings.Split(q.Get("status"), ",") {
		status = strings.TrimSpace(status)
		if status != "" {
			wanted[status] = true
		}
	}
	if len(wanted) == 0 {
		wanted["current"] = true
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var matches []*Page
	for _, p := range s.pages {
		if spaceKey != "" && p.SpaceKey != spaceKey {
			continue
		}
		if pageType != "" && p.Type != pageType {
			continue
		}
		if title != "" && p.Title != title {
			continue
		}
		if !wanted["any"] && !wanted[p.Status()] {
			continue
		}
		matches = append(matches, p)
	}
	// Deterministic ordering: the client takes results[0].
	sort.Slice(matches, func(i, j int) bool { return matches[i].ID < matches[j].ID })

	page, hasNext := paginate(r, matches)

	// The version comes without asking here; ancestors and the body do not.
	expand := expansions(r, "version")
	results := []map[string]any{}
	for _, p := range page {
		results = append(results, s.pageJSON(p, expand))
	}
	links := linksWithNext(hasNext)
	if s.SiteBase != "" {
		links["base"] = s.SiteBase
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"results": results,
		"_links":  links,
	})
}

func (s *Server) createContent(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Type  string `json:"type"`
		Title string `json:"title"`
		Space struct {
			Key string `json:"key"`
		} `json:"space"`
		Ancestors []struct {
			ID string `json:"id"`
		} `json:"ancestors"`
		Body struct {
			Storage struct {
				Value string `json:"value"`
			} `json:"storage"`
		} `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "bad payload", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Creating content does not create the space it goes into. A fake that
	// did would let a misspelt or unresolved space key through as a new page
	// in a space that exists nowhere else.
	if _, ok := s.spaces[payload.Space.Key]; !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"message": "no space with key " + payload.Space.Key,
		})
		return
	}

	// Confluence rejects a duplicate title within a space.
	for _, p := range s.pages {
		if p.SpaceKey == payload.Space.Key && p.Title == payload.Title && p.Type == payload.Type {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"message": fmt.Sprintf("A page with this title already exists: %q", payload.Title),
			})
			return
		}
	}

	var parentID string
	if len(payload.Ancestors) > 0 {
		parentID = payload.Ancestors[0].ID
	}
	p := &Page{
		ID:       s.newID(),
		Title:    payload.Title,
		Type:     payload.Type,
		SpaceKey: payload.Space.Key,
		ParentID: parentID,
		Version:  1,
		Body:     payload.Body.Storage.Value,
	}
	s.pages[p.ID] = p
	s.placeChild(p.ParentID, p.ID, "")
	writeJSON(w, http.StatusOK, s.pageJSON(p, fullExpansion))
}

func (s *Server) contentByID(w http.ResponseWriter, r *http.Request, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.pages[id]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "no content with id " + id})
		return
	}

	switch r.Method {
	case http.MethodDelete:
		// Confluence moves a page to the trash; purging is a separate call with
		// status=trashed, which mark deliberately never makes.
		if r.URL.Query().Get("status") == "trashed" {
			delete(s.pages, id)
		} else {
			p.Trashed = true
		}
		w.WriteHeader(http.StatusNoContent)

	case http.MethodGet:
		// A trashed page is not found by id unless the trash is asked for.
		// Answering it anyway would certify a client that goes on treating a
		// page somebody deleted as the one it recorded.
		if p.Trashed {
			wanted := map[string]bool{}
			for _, status := range strings.Split(r.URL.Query().Get("status"), ",") {
				wanted[strings.TrimSpace(status)] = true
			}
			if !wanted["trashed"] && !wanted["any"] {
				writeJSON(w, http.StatusNotFound, map[string]any{"message": "no content with id " + id})
				return
			}
		}
		writeJSON(w, http.StatusOK, s.pageJSON(p, expansions(r, "version")))
	case http.MethodPut:
		var payload struct {
			Title string `json:"title"`
			// A pointer, so that an absent key and an empty list are different
			// things. Confluence documents ancestors on an update as the way to
			// move a page, which makes "ancestors": [] a request rather than a
			// no-op, and that distinction is the whole question for a page
			// whose parent is a folder.
			Ancestors *[]struct {
				ID string `json:"id"`
			} `json:"ancestors"`
			Version struct {
				Number  int64  `json:"number"`
				Message string `json:"message"`
			} `json:"version"`
			Body struct {
				Storage struct {
					Value string `json:"value"`
				} `json:"storage"`
			} `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "bad payload", http.StatusBadRequest)
			return
		}
		if payload.Version.Number != p.Version+1 {
			writeJSON(w, http.StatusConflict, map[string]any{
				"message": fmt.Sprintf(
					"version conflict: expected %d, got %d", p.Version+1, payload.Version.Number,
				),
			})
			return
		}
		p.Version = payload.Version.Number
		p.Message = payload.Version.Message
		p.Body = payload.Body.Storage.Value
		if payload.Title != "" {
			p.Title = payload.Title
		}
		// An ancestors key that is present moves the page: to the last id it
		// names, or -- when the list is empty -- to the root of the space.
		//
		// Naming the parent a page already has is not a move, and must not
		// disturb where the page sits among its siblings; only a real change of
		// parent re-places it.
		if payload.Ancestors != nil {
			parentID := ""
			if n := len(*payload.Ancestors); n > 0 {
				parentID = (*payload.Ancestors)[n-1].ID
			}
			if parentID != p.ParentID {
				p.ParentID = parentID
				s.placeChild(parentID, p.ID, "")
			}
		}
		writeJSON(w, http.StatusOK, s.pageJSON(p, fullExpansion))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) getSpace(w http.ResponseWriter, r *http.Request, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sp, ok := s.spaces[key]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "no space " + key})
		return
	}
	// Confluence v1 returns the space id as a JSON *number* (v2 returns a
	// string). The fake matches v1 here so that callers decoding it are
	// exercised against the real shape.
	var id any = sp.ID
	if n, err := strconv.Atoi(sp.ID); err == nil {
		id = n
	}
	out := map[string]any{"id": id, "key": sp.Key, "name": sp.Key}
	// The homepage is there only when expanded, and then with only what
	// homepage.<property> asks of it.
	expand := expansions(r)
	if sp.HomepageID != "" && expand["homepage"] {
		if hp, ok := s.pages[sp.HomepageID]; ok {
			out["homepage"] = s.pageJSON(hp, nestedExpansions(expand, "homepage"))
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) childAttachment(w http.ResponseWriter, r *http.Request, pageID string) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		var all []*Attachment
		for _, a := range s.attachments {
			if a.PageID == pageID {
				all = append(all, a)
			}
		}
		s.mu.Unlock()

		sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
		page, hasNext := paginate(r, all)

		results := []map[string]any{}
		for _, a := range page {
			results = append(results, map[string]any{
				"id":       a.ID,
				"title":    a.Filename,
				"metadata": map[string]any{"comment": a.Comment},
				"_links": map[string]any{
					"context":  "/wiki",
					"download": downloadLink(a.PageID, a.Filename),
				},
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"results": results,
			"_links":  linksWithNext(hasNext),
		})

	case http.MethodPost:
		if !checkXSRF(w, r) {
			return
		}
		filename, comment, minorEdit, ok := parseMultipartAttachment(w, r)
		if !ok {
			return
		}
		s.mu.Lock()
		// A second attachment of the same name is refused, not added beside
		// the first: a new version of a file goes through the update endpoint.
		for _, a := range s.attachments {
			if a.PageID == pageID && a.Filename == filename {
				s.mu.Unlock()
				writeJSON(w, http.StatusBadRequest, map[string]any{
					"message": "Cannot add a new attachment with same file name as an existing attachment: " + filename,
				})
				return
			}
		}
		a := &Attachment{
			ID: s.newID(), PageID: pageID, Filename: filename, Comment: comment, MinorEdit: minorEdit,
		}
		s.attachments = append(s.attachments, a)
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"_links": map[string]any{"context": "/wiki"},
			"results": []map[string]any{{
				"id":       a.ID,
				"title":    a.Filename,
				"metadata": map[string]any{"comment": a.Comment},
				"_links": map[string]any{
					"context":  "/wiki",
					"download": downloadLink(pageID, a.Filename),
				},
			}},
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// updateAttachment serves a new version of an attachment, which the path names
// by id. The filename in the upload is not what finds it: matching on that
// would certify a client that sends the wrong id.
func (s *Server) updateAttachment(w http.ResponseWriter, r *http.Request, pageID, attachID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !checkXSRF(w, r) {
		return
	}
	_, comment, minorEdit, ok := parseMultipartAttachment(w, r)
	if !ok {
		return
	}

	s.mu.Lock()
	var found *Attachment
	for _, a := range s.attachments {
		if a.PageID == pageID && a.ID == attachID {
			found = a
			break
		}
	}
	if found != nil {
		found.Comment = comment
		found.MinorEdit = minorEdit
	}
	s.mu.Unlock()

	if found == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "no such attachment"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":    found.ID,
		"title": found.Filename,
		"_links": map[string]any{
			"context":  "/wiki",
			"download": downloadLink(pageID, found.Filename),
		},
	})
}

// checkXSRF refuses a multipart upload that does not carry the header
// Confluence requires on one, which is how it tells an API call from a form
// posted by some other site. Without it the answer is 403, and the fake says
// so rather than accepting what a real instance would not.
func checkXSRF(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("X-Atlassian-Token") != "no-check" {
		writeJSON(w, http.StatusForbidden, map[string]any{"message": "XSRF check failed"})
		return false
	}
	return true
}

// parseMultipartAttachment reads the file part and the comment of an upload,
// and answers 400 itself when there is no file in it.
func parseMultipartAttachment(w http.ResponseWriter, r *http.Request) (filename, comment, minorEdit string, ok bool) {
	// Test fixtures are small; a tight cap keeps a runaway test from buffering
	// to disk. Uploads larger than this are not something the fake supports.
	const maxAttachmentBytes = 8 << 20
	if err := r.ParseMultipartForm(maxAttachmentBytes); err == nil { //nolint:gosec // G120: bounded above, test-only fake
		comment = r.FormValue("comment")
		minorEdit = r.FormValue("minorEdit")
		if headers := r.MultipartForm.File["file"]; len(headers) > 0 {
			filename = headers[0].Filename
		}
	}
	if filename == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "no file in the upload"})
		return "", "", "", false
	}
	return filename, comment, minorEdit, true
}

// childPages lists a page's children in the order the tree shows them, which
// for the fake is simply the order they were created or last moved into.
func (s *Server) childPages(w http.ResponseWriter, r *http.Request, parentID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var children []*Page
	for _, id := range s.childOrder[parentID] {
		// Trashing a page takes it out of its parent's children.
		if p, ok := s.pages[id]; ok && p.ParentID == parentID && !p.Trashed {
			children = append(children, p)
		}
	}

	page, hasNext := paginate(r, children)
	expand := expansions(r)
	results := []map[string]any{}
	for _, p := range page {
		results = append(results, s.pageJSON(p, expand))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"results": results,
		"_links":  linksWithNext(hasNext),
	})
}

// directChildren answers the v2 listing of a page's children of every type.
// Only pages and folders exist in this fake; the type field is what a caller
// telling them apart relies on.
func (s *Server) directChildren(w http.ResponseWriter, r *http.Request, parentID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	children := []map[string]any{}
	for _, id := range s.childOrder[parentID] {
		if p, ok := s.pages[id]; ok && p.ParentID == parentID {
			children = append(children, map[string]any{
				"id": p.ID, "type": "page", "title": p.Title,
			})
		}
	}
	for _, f := range s.folders {
		if f.ParentID == parentID {
			children = append(children, map[string]any{
				"id": f.ID, "type": "folder", "title": f.Title,
			})
		}
	}

	results, next := cursorPage(r, children)
	if results == nil {
		results = []map[string]any{}
	}
	links := map[string]any{}
	if next != "" {
		links["next"] = fmt.Sprintf("/api/v2/pages/%s/direct-children?cursor=%s", parentID, next)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"results": results,
		"_links":  links,
	})
}

// ChildOrder returns the ids of a page's children, in order.
func (s *Server) ChildOrder(parentID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []string{}
	for _, id := range s.childOrder[parentID] {
		if p, ok := s.pages[id]; ok && p.ParentID == parentID {
			out = append(out, id)
		}
	}
	return out
}

// placeBefore inserts a child directly before a sibling. Callers must hold the
// lock.
func (s *Server) placeBefore(parentID, childID, before string) {
	s.placeChild(parentID, childID, "")
	ids := s.childOrder[parentID]
	// Lift it back out and reinsert ahead of the sibling.
	for i, id := range ids {
		if id == childID {
			ids = append(ids[:i:i], ids[i+1:]...)
			break
		}
	}
	for i, id := range ids {
		if id == before {
			rest := append([]string{childID}, ids[i:]...)
			s.childOrder[parentID] = append(ids[:i:i], rest...)
			return
		}
	}
	s.childOrder[parentID] = append(ids, childID)
}

// placeChild records a child's position under a parent. Callers must hold the
// lock. Appends when after is empty, otherwise inserts directly after it.
func (s *Server) placeChild(parentID, childID, after string) {
	if s.childOrder == nil {
		s.childOrder = map[string][]string{}
	}

	// Remove it from wherever it currently sits, including another parent.
	for parent, ids := range s.childOrder {
		for i, id := range ids {
			if id == childID {
				s.childOrder[parent] = append(ids[:i:i], ids[i+1:]...)
				break
			}
		}
	}

	if parentID == "" {
		return
	}

	if after == "" {
		s.childOrder[parentID] = append(s.childOrder[parentID], childID)
		return
	}

	for i, id := range s.childOrder[parentID] {
		if id == after {
			rest := append([]string{childID}, s.childOrder[parentID][i+1:]...)
			s.childOrder[parentID] = append(s.childOrder[parentID][:i+1:i+1], rest...)
			return
		}
	}

	s.childOrder[parentID] = append(s.childOrder[parentID], childID)
}

func (s *Server) childComment(w http.ResponseWriter, r *http.Request, pageID string) {
	s.mu.Lock()
	all := append([]InlineComment(nil), s.comments[pageID]...)
	s.mu.Unlock()

	page, hasNext := paginate(r, all)
	results := []map[string]any{}
	for _, c := range page {
		results = append(results, map[string]any{
			"extensions": map[string]any{
				"location": c.Location,
				"inlineProperties": map[string]any{
					"markerRef":         c.MarkerRef,
					"originalSelection": c.Selection,
				},
			},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"results": results,
		"_links":  linksWithNext(hasNext),
	})
}

func (s *Server) label(w http.ResponseWriter, r *http.Request, pageID string) {
	s.mu.Lock()
	p, ok := s.pages[pageID]
	if !ok {
		s.mu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "no content " + pageID})
		return
	}

	switch r.Method {
	case http.MethodPost:
		var payload []struct {
			Prefix string `json:"prefix"`
			Name   string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.mu.Unlock()
			http.Error(w, "bad payload", http.StatusBadRequest)
			return
		}
		for _, l := range payload {
			if !contains(p.Labels, l.Name) {
				p.Labels = append(p.Labels, l.Name)
			}
		}

	case http.MethodDelete:
		name := r.URL.Query().Get("name")
		kept := p.Labels[:0]
		for _, l := range p.Labels {
			if l != name {
				kept = append(kept, l)
			}
		}
		p.Labels = kept
	}

	labels := append([]string(nil), p.Labels...)
	s.mu.Unlock()

	sort.Strings(labels)
	items := make([]map[string]any, 0, len(labels))
	for i, l := range labels {
		items = append(items, map[string]any{
			"id":     strconv.Itoa(i + 1),
			"prefix": "global",
			"name":   l,
		})
	}
	page, hasNext := paginate(r, items)
	writeJSON(w, http.StatusOK, map[string]any{
		"results": page,
		"number":  len(page),
		"_links":  linksWithNext(hasNext),
	})
}

// cqlValue pulls a quoted or bare value for a field compared with op out of a
// CQL string. The fake only needs to understand the handful of clauses mark
// actually sends.
func cqlValue(cql, field, op string) string {
	clause := field + op
	rest, ok := strings.CutPrefix(cql[max(strings.Index(cql, clause), 0):], clause)
	if !ok || !strings.Contains(cql, clause) {
		return ""
	}
	if quoted, ok := strings.CutPrefix(rest, `"`); ok {
		if end := strings.Index(quoted, `"`); end >= 0 {
			return strings.ReplaceAll(quoted[:end], `\"`, `"`)
		}
		return ""
	}
	if end := strings.IndexAny(rest, " )"); end >= 0 {
		return rest[:end]
	}
	return rest
}

func (s *Server) searchFolder(w http.ResponseWriter, r *http.Request) {
	cql := r.URL.Query().Get("cql")
	title := cqlValue(cql, "title", "=")
	spaceKey := cqlValue(cql, "space", "=")
	ancestor := cqlValue(cql, "ancestor", "=")

	s.mu.Lock()
	defer s.mu.Unlock()

	results := []map[string]any{}
	for _, f := range s.folders {
		if f.Title != title || (spaceKey != "" && f.SpaceKey != spaceKey) {
			continue
		}
		if ancestor != "" && f.ParentID != ancestor {
			continue
		}
		results = append(results, map[string]any{
			"content": map[string]any{"id": f.ID, "type": "folder", "title": f.Title},
		})
		break
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (s *Server) folderJSON(f *Folder) map[string]any {
	return map[string]any{
		"id":         f.ID,
		"type":       "folder",
		"status":     "current",
		"title":      f.Title,
		"spaceId":    f.SpaceID,
		"parentId":   f.ParentID,
		"parentType": f.ParentType,
	}
}

func (s *Server) getFolder(w http.ResponseWriter, folderID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.folders {
		if f.ID == folderID {
			writeJSON(w, http.StatusOK, s.folderJSON(f))
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"message": "no such folder"})
}

func (s *Server) createFolder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		SpaceID    string `json:"spaceId"`
		Title      string `json:"title"`
		ParentID   string `json:"parentId"`
		ParentType string `json:"parentType"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "bad payload"})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	spaceKey := ""
	for _, sp := range s.spaces {
		if sp.ID == payload.SpaceID {
			spaceKey = sp.Key
		}
	}

	// Confluence rejects a second folder with the same title under one parent,
	// which is what makes a stranded reference visible rather than silent.
	for _, f := range s.folders {
		if f.Title == payload.Title && f.ParentID == payload.ParentID && f.SpaceID == payload.SpaceID {
			writeJSON(w, http.StatusBadRequest,
				map[string]any{"message": "A folder exists with the same title"})
			return
		}
	}

	f := &Folder{
		ID:         s.newID(),
		Title:      payload.Title,
		SpaceID:    payload.SpaceID,
		SpaceKey:   spaceKey,
		ParentID:   payload.ParentID,
		ParentType: payload.ParentType,
	}
	s.folders = append(s.folders, f)
	writeJSON(w, http.StatusOK, s.folderJSON(f))
}

// contentJSONV2 renders a page or blogpost the way v2 does.
//
// The differences from the v1 shape are the ones a client has to cope with: the
// space is an id, the parent is a single id with its type beside it rather than
// an ancestor chain, and the body arrives only when a format was asked for.
//
// Callers must hold s.mu.
func (s *Server) contentJSONV2(p *Page, withBody bool) map[string]any {
	spaceID := ""
	if sp, ok := s.spaces[p.SpaceKey]; ok {
		spaceID = sp.ID
	}

	parentType := ""
	if p.ParentID != "" {
		parentType = "page"
		for _, f := range s.folders {
			if f.ID == p.ParentID {
				parentType = "folder"
			}
		}
	}

	out := map[string]any{
		"id":         p.ID,
		"status":     p.Status(),
		"title":      p.Title,
		"spaceId":    spaceID,
		"parentId":   p.ParentID,
		"parentType": parentType,
		"version": map[string]any{
			"number":  p.Version,
			"message": p.Message,
		},
		"_links": s.pageLinks(p),
	}

	if withBody {
		out["body"] = map[string]any{"storage": map[string]any{"value": p.Body}}
	}

	return out
}

// labelsV2 serves the v2 label listing, which pages by cursor and hands the
// label id over as a number where v1 makes it a string.
func (s *Server) labelsV2(w http.ResponseWriter, r *http.Request, collection, pageID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.pages[pageID]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "no content with id " + pageID})
		return
	}

	page, next := cursorPage(r, p.Labels)

	results := []map[string]any{}
	for i, label := range page {
		results = append(results, map[string]any{
			"id":     i + 1,
			"name":   label,
			"prefix": "global",
		})
	}

	links := map[string]any{}
	if next != "" {
		links["next"] = fmt.Sprintf("/api/v2/%s/%s/labels?cursor=%s", collection, pageID, next)
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results, "_links": links})
}

// attachmentsV2 serves the v2 attachment listing, which names the comment and
// the download link at the top level where v1 nests them.
func (s *Server) attachmentsV2(w http.ResponseWriter, r *http.Request, collection, pageID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var owned []*Attachment
	for _, a := range s.attachments {
		if a.PageID == pageID {
			owned = append(owned, a)
		}
	}

	page, next := cursorPage(r, owned)

	results := []map[string]any{}
	for _, a := range page {
		results = append(results, map[string]any{
			"id":           a.ID,
			"title":        a.Filename,
			"comment":      a.Comment,
			"downloadLink": downloadLink(a.PageID, a.Filename),
		})
	}

	links := map[string]any{}
	if next != "" {
		links["next"] = fmt.Sprintf("/api/v2/%s/%s/attachments?cursor=%s", collection, pageID, next)
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results, "_links": links})
}

// listContentV2 serves GET /pages and GET /blogposts: a space, a title and a
// status, where v1 takes a space key and a type.
func (s *Server) listContentV2(w http.ResponseWriter, r *http.Request, pageType string) {
	q := r.URL.Query()
	spaceID, title := q.Get("space-id"), q.Get("title")

	wanted := map[string]bool{}
	for _, status := range strings.Split(q.Get("status"), ",") {
		status = strings.TrimSpace(status)
		if status != "" {
			wanted[status] = true
		}
	}
	if len(wanted) == 0 {
		wanted["current"] = true
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var matches []*Page
	for _, p := range s.pages {
		if p.Type != pageType {
			continue
		}
		if spaceID != "" {
			sp, ok := s.spaces[p.SpaceKey]
			if !ok || sp.ID != spaceID {
				continue
			}
		}
		if title != "" && p.Title != title {
			continue
		}
		if !wanted[p.Status()] {
			continue
		}
		matches = append(matches, p)
	}
	// Deterministic ordering: the client takes results[0].
	sort.Slice(matches, func(i, j int) bool { return matches[i].ID < matches[j].ID })

	page, next := cursorPage(r, matches)

	results := []map[string]any{}
	for _, p := range page {
		results = append(results, s.contentJSONV2(p, false))
	}

	links := map[string]any{}
	if next != "" {
		links["next"] = fmt.Sprintf("/api/v2/%ss?cursor=%s", pageType, next)
	}
	if s.SiteBase != "" {
		links["base"] = s.SiteBase
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results, "_links": links})
}

// contentV2ByID serves GET and PUT of a single page or blogpost.
//
// A page id and a blogpost id come from the same sequence here, as they do in
// Confluence, so the type is checked rather than assumed: asking /blogposts for
// a page id has to answer 404, which is what makes a client's fall through from
// one collection to the other worth having.
func (s *Server) contentV2ByID(w http.ResponseWriter, r *http.Request, id, pageType string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.pages[id]
	if !ok || p.Type != pageType {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "no content with id " + id})
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.contentJSONV2(p, r.URL.Query().Get("body-format") != ""))

	case http.MethodPut:
		var payload struct {
			Title    string `json:"title"`
			Status   string `json:"status"`
			ParentID string `json:"parentId"`
			Version  struct {
				Number  int64  `json:"number"`
				Message string `json:"message"`
			} `json:"version"`
			Body struct {
				Value string `json:"value"`
			} `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"message": "bad payload"})
			return
		}
		if payload.Version.Number != p.Version+1 {
			writeJSON(w, http.StatusConflict, map[string]any{
				"message": fmt.Sprintf(
					"version conflict: expected %d, got %d", p.Version+1, payload.Version.Number,
				),
			})
			return
		}
		p.Version = payload.Version.Number
		p.Message = payload.Version.Message
		p.Body = payload.Body.Value
		if payload.Title != "" {
			p.Title = payload.Title
		}
		// v2 names the parent directly, and only a real change of parent
		// re-places the page among its siblings.
		if payload.ParentID != "" && payload.ParentID != p.ParentID {
			p.ParentID = payload.ParentID
			s.placeChild(payload.ParentID, p.ID, "")
		}
		writeJSON(w, http.StatusOK, s.contentJSONV2(p, false))

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// createPageV2 serves the v2 page create mark uses when the parent is a folder.
func (s *Server) createPageV2(w http.ResponseWriter, r *http.Request, pageType string) {
	var payload struct {
		SpaceID  string `json:"spaceId"`
		Title    string `json:"title"`
		ParentID string `json:"parentId"`
		Body     struct {
			Value string `json:"value"`
		} `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "bad payload"})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	spaceKey := ""
	for _, sp := range s.spaces {
		if sp.ID == payload.SpaceID {
			spaceKey = sp.Key
		}
	}
	if spaceKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "no space with id " + payload.SpaceID})
		return
	}

	// v2 refuses a duplicate title within a space with 400, as v1 does. The
	// check is the same one createContent makes, so a create through either
	// API reaches the client's explainCreateFailure the same way.
	for _, p := range s.pages {
		if p.SpaceKey == spaceKey && p.Title == payload.Title && p.Type == pageType {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"message": fmt.Sprintf("A page with this title already exists: %q", payload.Title),
			})
			return
		}
	}

	p := &Page{
		ID:       s.newID(),
		Title:    payload.Title,
		Type:     pageType,
		SpaceKey: spaceKey,
		ParentID: payload.ParentID,
		Version:  1,
		Body:     payload.Body.Value,
	}
	s.pages[p.ID] = p
	s.placeChild(p.ParentID, p.ID, "")
	// The version has to come back. Without it the caller computes the version
	// it is superseding from zero, and its first update is rejected as stale.
	writeJSON(w, http.StatusOK, s.contentJSONV2(p, false))
}

func (s *Server) searchUser(w http.ResponseWriter, r *http.Request) {
	// The client searches with user.fullname~"<name>", a contains match. An
	// empty or absent name matches nobody: it used to match every user without
	// a full name, which answered a search that named nothing.
	name := strings.ToLower(cqlValue(r.URL.Query().Get("cql"), "user.fullname", "~"))

	s.mu.Lock()
	defer s.mu.Unlock()

	results := []map[string]any{}
	for _, u := range s.users {
		if name == "" || !strings.Contains(strings.ToLower(u.FullName), name) {
			continue
		}
		results = append(results, map[string]any{
			"user": map[string]any{
				"accountId": u.AccountID,
				"userKey":   u.UserKey,
				"username":  u.Username,
			},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// downloadLink is an attachment's download path as Confluence writes it, with
// the filename percent-encoded: a#b.png is served as a%23b.png.
func downloadLink(pageID, filename string) string {
	return "/download/attachments/" + pageID + "/" + url.PathEscape(filename)
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
