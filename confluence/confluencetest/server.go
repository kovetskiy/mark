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
	return p
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
		s.searchUser(w, r)

	case strings.HasPrefix(path, "/space/"):
		s.getSpace(w, strings.TrimPrefix(path, "/space/"))

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
		case sub == "child/comment":
			s.childComment(w, r, id)
		case sub == "label":
			s.label(w, r, id)
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
	default:
		http.NotFound(w, r)
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
