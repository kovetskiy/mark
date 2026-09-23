package confluencetest

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// These pin the places where the fake is as strict as Confluence. Each one was
// once more forgiving, and a forgiving fake certifies a client that cannot work
// against the real thing.

func do(t *testing.T, req *http.Request) (int, map[string]any) {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

func get(t *testing.T, rawURL string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	return do(t, req)
}

func postJSON(t *testing.T, rawURL string, payload any) (int, map[string]any) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, rawURL, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	return do(t, req)
}

// upload posts a multipart attachment. An empty filename leaves the file part
// out altogether.
func upload(t *testing.T, rawURL, filename, comment string, xsrf bool) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if filename != "" {
		part, err := w.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write([]byte("content"))
	}
	if err := w.WriteField("comment", comment); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, rawURL, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if xsrf {
		req.Header.Set("X-Atlassian-Token", "no-check")
	}
	return do(t, req)
}

func TestUpdateAttachmentMatchesByID(t *testing.T) {
	s := New(t)
	page := s.AddPage("DOCS", "Page", "page", "")
	first := s.AddAttachment(page.ID, "a.png", "old")
	second := s.AddAttachment(page.ID, "b.png", "old")

	// The upload names the first file; the path names the second. The path
	// is what Confluence goes by.
	status, _ := upload(t,
		s.URL+"/rest/api/content/"+page.ID+"/child/attachment/"+second.ID+"/data",
		"a.png", "new", true)
	if status != http.StatusOK {
		t.Fatalf("update: status %d", status)
	}

	for _, a := range s.Attachments(page.ID) {
		want := map[string]string{first.ID: "old", second.ID: "new"}[a.ID]
		if a.Comment != want {
			t.Errorf("attachment %s (%s): comment %q, want %q", a.ID, a.Filename, a.Comment, want)
		}
	}
}

func TestUpdateAttachmentRequiresAFile(t *testing.T) {
	s := New(t)
	page := s.AddPage("DOCS", "Page", "page", "")
	a := s.AddAttachment(page.ID, "a.png", "old")

	status, _ := upload(t,
		s.URL+"/rest/api/content/"+page.ID+"/child/attachment/"+a.ID+"/data",
		"", "new", true)
	if status != http.StatusBadRequest {
		t.Fatalf("status %d, want 400 for an upload with no file", status)
	}
	if got := s.Attachments(page.ID)[0].Comment; got != "old" {
		t.Errorf("comment %q changed by a rejected upload", got)
	}
}

func TestUpdateAttachmentUnknownID(t *testing.T) {
	s := New(t)
	page := s.AddPage("DOCS", "Page", "page", "")
	s.AddAttachment(page.ID, "a.png", "old")

	status, _ := upload(t,
		s.URL+"/rest/api/content/"+page.ID+"/child/attachment/999999/data",
		"a.png", "new", true)
	if status != http.StatusNotFound {
		t.Fatalf("status %d, want 404 for an attachment id that does not exist", status)
	}
}

func TestAttachmentUploadsRequireTheXSRFHeader(t *testing.T) {
	s := New(t)
	page := s.AddPage("DOCS", "Page", "page", "")
	a := s.AddAttachment(page.ID, "a.png", "old")

	status, _ := upload(t, s.URL+"/rest/api/content/"+page.ID+"/child/attachment", "b.png", "c", false)
	if status != http.StatusForbidden {
		t.Errorf("create without X-Atlassian-Token: status %d, want 403", status)
	}

	status, _ = upload(t,
		s.URL+"/rest/api/content/"+page.ID+"/child/attachment/"+a.ID+"/data", "a.png", "c", false)
	if status != http.StatusForbidden {
		t.Errorf("update without X-Atlassian-Token: status %d, want 403", status)
	}

	if n := len(s.Attachments(page.ID)); n != 1 {
		t.Errorf("%d attachments after refused uploads, want 1", n)
	}
}

func TestCreateAttachmentRefusesADuplicateName(t *testing.T) {
	s := New(t)
	page := s.AddPage("DOCS", "Page", "page", "")
	s.AddAttachment(page.ID, "a.png", "old")

	status, _ := upload(t, s.URL+"/rest/api/content/"+page.ID+"/child/attachment", "a.png", "new", true)
	if status != http.StatusBadRequest {
		t.Fatalf("status %d, want 400 for a second attachment of the same name", status)
	}

	// The same name on another page is a different attachment.
	other := s.AddPage("DOCS", "Other", "page", "")
	status, _ = upload(t, s.URL+"/rest/api/content/"+other.ID+"/child/attachment", "a.png", "new", true)
	if status != http.StatusOK {
		t.Fatalf("status %d, want 200 for the name on another page", status)
	}
}

func TestCreateAttachmentRequiresAFile(t *testing.T) {
	s := New(t)
	page := s.AddPage("DOCS", "Page", "page", "")

	status, _ := upload(t, s.URL+"/rest/api/content/"+page.ID+"/child/attachment", "", "c", true)
	if status != http.StatusBadRequest {
		t.Fatalf("status %d, want 400 for an upload with no file", status)
	}
}

func TestCreateContentRefusesAnUnknownSpace(t *testing.T) {
	s := New(t)
	s.AddSpace("DOCS")

	status, _ := postJSON(t, s.URL+"/rest/api/content", map[string]any{
		"type": "page", "title": "Page", "space": map[string]any{"key": "NOPE"},
	})
	if status != http.StatusNotFound {
		t.Fatalf("status %d, want 404 for a space that does not exist", status)
	}

	status, _ = postJSON(t, s.URL+"/rest/api/content", map[string]any{
		"type": "page", "title": "Page", "space": map[string]any{"key": "DOCS"},
	})
	if status != http.StatusOK {
		t.Fatalf("status %d, want 200 for a registered space", status)
	}
}

func TestContentByIDHonoursExpand(t *testing.T) {
	s := New(t)
	root := s.AddPage("DOCS", "Root", "page", "")
	child := s.AddPage("DOCS", "Child", "page", root.ID)
	s.EditPage(child.ID, "<p>body</p>")

	_, bare := get(t, s.URL+"/rest/api/content/"+child.ID)
	for _, key := range []string{"ancestors", "body"} {
		if _, ok := bare[key]; ok {
			t.Errorf("%s returned without being expanded", key)
		}
	}
	if _, ok := bare["version"]; !ok {
		t.Error("version is expanded by default and is missing")
	}

	_, full := get(t, s.URL+"/rest/api/content/"+child.ID+"?expand=ancestors,body.storage")
	ancestors, _ := full["ancestors"].([]any)
	if len(ancestors) != 1 {
		t.Errorf("ancestors %v, want the root", full["ancestors"])
	}
	body, _ := full["body"].(map[string]any)
	storage, _ := body["storage"].(map[string]any)
	if storage["value"] != "<p>body</p>" {
		t.Errorf("body %v, want the stored body", full["body"])
	}
}

func TestSearchContentHonoursExpand(t *testing.T) {
	s := New(t)
	root := s.AddPage("DOCS", "Root", "page", "")
	s.AddPage("DOCS", "Child", "page", root.ID)

	q := url.Values{"spaceKey": {"DOCS"}, "title": {"Child"}}
	_, bare := get(t, s.URL+"/rest/api/content?"+q.Encode())
	results, _ := bare["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results %v", bare["results"])
	}
	if _, ok := results[0].(map[string]any)["ancestors"]; ok {
		t.Error("ancestors returned without being expanded")
	}

	q.Set("expand", "ancestors,version")
	_, full := get(t, s.URL+"/rest/api/content?"+q.Encode())
	results, _ = full["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results %v", full["results"])
	}
	if ancestors, _ := results[0].(map[string]any)["ancestors"].([]any); len(ancestors) != 1 {
		t.Errorf("ancestors %v, want the root", results[0])
	}
}

func TestSpaceHomepageOnlyWhenExpanded(t *testing.T) {
	s := New(t)
	home := s.AddPage("DOCS", "Home", "page", "")
	s.SetHomepage("DOCS", home.ID)

	_, bare := get(t, s.URL+"/rest/api/space/DOCS")
	if _, ok := bare["homepage"]; ok {
		t.Error("homepage returned without being expanded")
	}

	_, expanded := get(t, s.URL+"/rest/api/space/DOCS?expand=homepage")
	homepage, _ := expanded["homepage"].(map[string]any)
	if homepage["id"] != home.ID {
		t.Fatalf("homepage %v, want %s", expanded["homepage"], home.ID)
	}
	if _, ok := homepage["version"]; ok {
		t.Error("homepage version returned without homepage.version")
	}
}

func TestTrashedContentIsNotFoundByID(t *testing.T) {
	s := New(t)
	page := s.AddPage("DOCS", "Page", "page", "")

	req, err := http.NewRequest(http.MethodDelete, s.URL+"/rest/api/content/"+page.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := do(t, req); status != http.StatusNoContent {
		t.Fatalf("delete: status %d", status)
	}

	if status, _ := get(t, s.URL+"/rest/api/content/"+page.ID); status != http.StatusNotFound {
		t.Errorf("status %d, want 404 for trashed content", status)
	}
	for _, status := range []string{"trashed", "any", "current,trashed"} {
		if got, _ := get(t, s.URL+"/rest/api/content/"+page.ID+"?status="+status); got != http.StatusOK {
			t.Errorf("status=%s: status %d, want 200", status, got)
		}
	}
}

func TestSearchUserMatchesTheNameAsked(t *testing.T) {
	s := New(t)
	s.AddUser(User{AccountID: "nameless"})
	s.AddUser(User{AccountID: "ada", FullName: "Ada Lovelace"})

	q := url.Values{"cql": {`user.fullname~"Grace Hopper"`}}
	_, body := get(t, s.URL+"/rest/api/search/user?"+q.Encode())
	if results, _ := body["results"].([]any); len(results) != 0 {
		t.Errorf("results %v for a name nobody has", results)
	}

	q.Set("cql", `user.fullname~"lovelace"`)
	_, body = get(t, s.URL+"/rest/api/search/user?"+q.Encode())
	results, _ := body["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results %v, want Ada alone", results)
	}
	user, _ := results[0].(map[string]any)["user"].(map[string]any)
	if user["accountId"] != "ada" {
		t.Errorf("matched %v, want ada", user)
	}
}

func TestV1ListingsCapTheLimit(t *testing.T) {
	s := New(t)
	page := s.AddPage("DOCS", "Page", "page", "")
	for i := range v1MaxLimit + 10 {
		s.AddAttachment(page.ID, "file-"+strconv.Itoa(i)+".png", "")
		s.AddPage("DOCS", "Child "+strconv.Itoa(i), "page", page.ID)
	}

	for _, path := range []string{"/child/attachment", "/child/page"} {
		_, body := get(t, s.URL+"/rest/api/content/"+page.ID+path+"?limit=1000")
		results, _ := body["results"].([]any)
		if len(results) != v1MaxLimit {
			t.Errorf("%s: %d results for limit=1000, want the cap of %d", path, len(results), v1MaxLimit)
		}
		links, _ := body["_links"].(map[string]any)
		if links["next"] == nil {
			t.Errorf("%s: a capped page has to say there is more", path)
		}

		_, body = get(t, s.URL+"/rest/api/content/"+page.ID+path+"?limit=1000&start="+strconv.Itoa(v1MaxLimit))
		if results, _ := body["results"].([]any); len(results) != 10 {
			t.Errorf("%s: %d results on the second page, want 10", path, len(results))
		}
	}
}

func TestV2ListingsCapTheLimit(t *testing.T) {
	s := New(t)
	page := s.AddPage("DOCS", "Page", "page", "")
	for i := range v2MaxLimit + 10 {
		s.AddFolder("DOCS", "Folder "+strconv.Itoa(i), page.ID, "page")
	}

	_, body := get(t, s.URL+"/api/v2/pages/"+page.ID+"/direct-children?limit=1000")
	results, _ := body["results"].([]any)
	if len(results) != v2MaxLimit {
		t.Fatalf("%d results for limit=1000, want the cap of %d", len(results), v2MaxLimit)
	}
	links, _ := body["_links"].(map[string]any)
	next, _ := links["next"].(string)
	if !strings.Contains(next, "cursor=") {
		t.Fatalf("next %q, want a cursor to the rest", next)
	}

	_, body = get(t, s.URL+next)
	if results, _ := body["results"].([]any); len(results) != 10 {
		t.Errorf("%d results after the cursor, want 10", len(results))
	}
}

func TestFoldersOnlyCreateOnPost(t *testing.T) {
	s := New(t)
	s.AddSpace("DOCS")

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		req, err := http.NewRequest(method, s.URL+"/api/v2/folders", strings.NewReader(`{"title":"F"}`))
		if err != nil {
			t.Fatal(err)
		}
		if status, _ := do(t, req); status != http.StatusMethodNotAllowed {
			t.Errorf("%s /folders: status %d, want 405", method, status)
		}
	}
	if n := len(s.Folders()); n != 0 {
		t.Errorf("%d folders created by requests that were not a POST", n)
	}
}

func TestCreatePageV2RefusesADuplicateTitle(t *testing.T) {
	s := New(t)
	space := s.AddSpace("DOCS")
	s.AddPage("DOCS", "Taken", "page", "")

	status, _ := postJSON(t, s.URL+"/api/v2/pages", map[string]any{
		"spaceId": space.ID, "title": "Taken",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("status %d, want 400 for a title already in the space", status)
	}

	status, _ = postJSON(t, s.URL+"/api/v2/pages", map[string]any{
		"spaceId": "999999", "title": "Free",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("status %d, want 400 for a space that does not exist", status)
	}

	status, _ = postJSON(t, s.URL+"/api/v2/pages", map[string]any{
		"spaceId": space.ID, "title": "Free",
	})
	if status != http.StatusOK {
		t.Fatalf("status %d, want 200 for a free title", status)
	}
}
