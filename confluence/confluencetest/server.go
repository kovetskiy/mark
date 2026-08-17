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

// Attachment is a file attached to a page.
type Attachment struct {
	ID       string
	PageID   string
	Filename string
	Comment  string
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

	var payload struct {
		Pages []struct {
			ID string `json:"id"`
		} `json:"pages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "bad body"})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, item := range payload.Pages {
		if p, ok := s.pages[item.ID]; ok {
			p.Archived = true
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"id": "archive-task"})
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

// Callers must hold s.mu.
func (s *Server) pageJSON(p *Page) map[string]any {
	ancestors := []map[string]any{}
	for _, a := range s.ancestorsOf(p) {
		ancestors = append(ancestors, map[string]any{"id": a.ID, "title": a.Title})
	}
	return map[string]any{
		"id":        p.ID,
		"title":     p.Title,
		"type":      p.Type,
		"ancestors": ancestors,
		"version": map[string]any{
			"number":  p.Version,
			"message": p.Message,
		},
		"body": map[string]any{
			"storage": map[string]any{"value": p.Body},
		},
		"_links": map[string]any{"webui": "/display/" + p.SpaceKey + "/" + p.ID},
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// paginate slices items by the request's start/limit and reports whether a
// further page exists, mirroring the Confluence _links.next convention the
// client relies on to stop looping.
func paginate[T any](r *http.Request, items []T) (page []T, hasNext bool) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
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
		s.getSpace(w, strings.TrimPrefix(path, "/space/"))

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
			s.updateAttachment(w, r, id)
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
		writeJSON(w, http.StatusOK, map[string]any{"results": results})
	case "/folders":
		s.createFolder(w, r)

	case "/pages":
		s.createPageV2(w, r)

	default:
		if folderID, ok := strings.CutPrefix(path, "/folders/"); ok && r.Method == http.MethodGet {
			s.getFolder(w, folderID)
			return
		}
		// /spaces/{id}/properties and /spaces/{id}/properties/{propertyID}
		if rest, ok := strings.CutPrefix(path, "/spaces/"); ok {
			spaceID, sub, _ := strings.Cut(rest, "/")
			if propertyID, ok := strings.CutPrefix(sub, "properties"); ok {
				s.spaceProperties(w, r, spaceID, strings.TrimPrefix(propertyID, "/"))
				return
			}
		}
		http.NotFound(w, r)
	}
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
			results := []map[string]any{}
			for _, p := range s.properties {
				if p.OwnerID == contentID {
					results = append(results, propertyJSON(p))
				}
			}
			writeJSON(w, http.StatusOK, map[string]any{"results": results})
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

func (s *Server) spaceProperties(w http.ResponseWriter, r *http.Request, ownerID, propertyID string) {
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
		results := []map[string]any{}
		for _, p := range s.properties {
			if p.OwnerID == ownerID && (key == "" || p.Key == key) {
				results = append(results, propertyJSON(p))
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"results": results})

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
		matches = append(matches, p)
	}
	// Deterministic ordering: the client takes results[0].
	sort.Slice(matches, func(i, j int) bool { return matches[i].ID < matches[j].ID })

	results := []map[string]any{}
	for _, p := range matches {
		results = append(results, s.pageJSON(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"results": results,
		"_links":  map[string]any{"base": "/wiki"},
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
	if _, ok := s.spaces[payload.Space.Key]; !ok {
		s.spaces[payload.Space.Key] = &Space{ID: s.newID(), Key: payload.Space.Key}
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
	writeJSON(w, http.StatusOK, s.pageJSON(p))
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
		writeJSON(w, http.StatusOK, s.pageJSON(p))
	case http.MethodPut:
		var payload struct {
			Title   string `json:"title"`
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
		writeJSON(w, http.StatusOK, s.pageJSON(p))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) getSpace(w http.ResponseWriter, key string) {
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
	if sp.HomepageID != "" {
		if hp, ok := s.pages[sp.HomepageID]; ok {
			out["homepage"] = s.pageJSON(hp)
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
					"download": "/download/attachments/" + a.PageID + "/" + a.Filename,
				},
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"results": results,
			"_links":  linksWithNext(hasNext),
		})

	case http.MethodPost:
		filename, comment := parseMultipartAttachment(r)
		s.mu.Lock()
		a := &Attachment{ID: s.newID(), PageID: pageID, Filename: filename, Comment: comment}
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
					"download": "/download/attachments/" + pageID + "/" + a.Filename,
				},
			}},
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) updateAttachment(w http.ResponseWriter, r *http.Request, pageID string) {
	filename, comment := parseMultipartAttachment(r)

	s.mu.Lock()
	var found *Attachment
	for _, a := range s.attachments {
		if a.PageID == pageID && (filename == "" || a.Filename == filename) {
			found = a
			break
		}
	}
	if found != nil {
		found.Comment = comment
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
			"download": "/download/attachments/" + pageID + "/" + found.Filename,
		},
	})
}

func parseMultipartAttachment(r *http.Request) (filename, comment string) {
	// Test fixtures are small; a tight cap keeps a runaway test from buffering
	// to disk. Uploads larger than this are not something the fake supports.
	const maxAttachmentBytes = 8 << 20
	if err := r.ParseMultipartForm(maxAttachmentBytes); err != nil { //nolint:gosec // G120: bounded above, test-only fake
		return "", ""
	}
	comment = r.FormValue("comment")
	if r.MultipartForm != nil {
		for _, headers := range r.MultipartForm.File {
			if len(headers) > 0 {
				filename = headers[0].Filename
				break
			}
		}
	}
	return filename, comment
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

	results := []map[string]any{}
	for _, id := range s.childOrder[parentID] {
		if p, ok := s.pages[id]; ok && p.ParentID == parentID {
			results = append(results, s.pageJSON(p))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
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

// cqlValue pulls a quoted or bare value for a field out of a CQL string. The
// fake only needs to understand the handful of clauses mark actually sends.
func cqlValue(cql, field string) string {
	rest, ok := strings.CutPrefix(cql[max(strings.Index(cql, field+"="), 0):], field+"=")
	if !ok || !strings.Contains(cql, field+"=") {
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
	title := cqlValue(cql, "title")
	spaceKey := cqlValue(cql, "space")
	ancestor := cqlValue(cql, "ancestor")

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

// createPageV2 serves the v2 page create mark uses when the parent is a folder.
func (s *Server) createPageV2(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		SpaceID  string `json:"spaceId"`
		Title    string `json:"title"`
		ParentID string `json:"parentId"`
		Body     struct {
			Storage struct {
				Value string `json:"value"`
			} `json:"storage"`
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

	p := &Page{
		ID:       s.newID(),
		Title:    payload.Title,
		Type:     "page",
		SpaceKey: spaceKey,
		ParentID: payload.ParentID,
		Version:  1,
		Body:     payload.Body.Storage.Value,
	}
	s.pages[p.ID] = p
	s.placeChild(p.ParentID, p.ID, "")
	// The version has to come back. Without it the caller computes the version
	// it is superseding from zero, and its first update is rejected as stale.
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      p.ID,
		"title":   p.Title,
		"type":    "page",
		"version": map[string]any{"number": p.Version},
		"_links":  map[string]any{"webui": "/display/" + spaceKey + "/" + p.ID},
	})
}

func (s *Server) searchUser(w http.ResponseWriter, r *http.Request) {
	cql := r.URL.Query().Get("cql")

	s.mu.Lock()
	defer s.mu.Unlock()

	results := []map[string]any{}
	for _, u := range s.users {
		if u.FullName != "" && !strings.Contains(cql, u.FullName) {
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

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
