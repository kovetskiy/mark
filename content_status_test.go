package mark

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublishContentStatus(t *testing.T) {
	ctx := context.Background()
	server, _ := docsSpace(t)
	dir := t.TempDir()
	document := func(value, body string) string {
		status := ""
		if value != "" {
			status = "<!-- Content-Status: " + value + " -->\n"
		}
		return "<!-- Space: DOCS -->\n<!-- Title: Guide -->\n" + status + "\n" + body + "\n"
	}
	config := trackingConfig(server, writeFile(t, dir, "guide.md", document("", "One.")))
	config.Tracking.NoOverwrite = true
	config.Update.ChangesOnly = true
	require.NoError(t, Run(ctx, config))
	id := mustFind(t, server, "Guide")
	start := server.Page(id)

	writeFile(t, dir, "guide.md", document("ready for review", "One."))
	preview := decodeReport(t, []byte(previewOf(t, config, "json"))).Pages[0]
	require.Equal(t, report.StatusWouldUpdate, preview.Status)
	assert.Contains(t, preview.Reason, "content status")
	assert.Equal(t, start.Version, server.Page(id).Version)

	assert.Equal(t, report.StatusPublished, runReported(t, config).Status)
	assigned := server.Page(id)
	assert.Equal(t, "Ready for review", assigned.ContentStatus)
	assert.Equal(t, start.Version+1, assigned.Version)
	assert.Equal(t, start.Body, assigned.Body)

	assert.Equal(t, report.StatusUnchanged, runReported(t, config).Status)
	assert.Equal(t, assigned.Version, server.Page(id).Version)
	assert.Equal(t, report.StatusUnchanged, decodeReport(t, []byte(previewOf(t, config, "json"))).Pages[0].Status)

	writeFile(t, dir, "guide.md", document("", "Two."))
	assert.Equal(t, report.StatusPublished, runReported(t, config).Status)
	preserved := server.Page(id)
	assert.Equal(t, "Ready for review", preserved.ContentStatus)
	assert.Contains(t, preserved.Body, "Two.")

	writeFile(t, dir, "guide.md", document("~", "Two."))
	assert.Equal(t, report.StatusPublished, runReported(t, config).Status)
	cleared := server.Page(id)
	assert.Empty(t, cleared.ContentStatus)
	assert.Equal(t, preserved.Version+1, cleared.Version)
	assert.Equal(t, report.StatusUnchanged, runReported(t, config).Status)

	writeFile(t, dir, "guide.md", document("unknown", "Must not publish."))
	require.ErrorContains(t, Run(ctx, config), "is not available")
	assert.Equal(t, cleared.Body, server.Page(id).Body)
}

func TestBlogPostContentStatus(t *testing.T) {
	for _, gateway := range []bool{false, true} {
		name := "site"
		if gateway {
			name = "gateway"
		}
		t.Run(name, func(t *testing.T) {
			server, _ := docsSpace(t)
			dir := t.TempDir()
			write := func(status, body string) string {
				header := ""
				if status != "" {
					header = "<!-- Content-Status: " + status + " -->\n"
				}
				return writeFile(t, dir, "post.md", "<!-- Space: DOCS -->\n<!-- Type: blogpost -->\n<!-- Title: News -->\n"+header+"\n"+body+"\n")
			}
			config := changesOnlyConfig(server, write("Ready for review", "One."))
			if gateway {
				config.Connection.BaseURL += "/ex/confluence/cloud-id"
			}
			require.NoError(t, Run(t.Context(), config))
			api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
			post, err := api.FindPage(t.Context(), "DOCS", "News", confluence.ContentBlogPost)
			require.NoError(t, err)
			require.NotNil(t, post)
			id := post.ID
			assert.Equal(t, "Ready for review", server.Page(id).ContentStatus)
			assert.Equal(t, report.StatusUnchanged, runReported(t, config).Status)

			exported, err := Export(t.Context(), ExportConfig{Connection: config.Connection, PageID: id, IncludeContentStatus: true, NoAttachments: true})
			require.NoError(t, err)
			assert.Contains(t, exported.Markdown, "<!-- Content-Status: Ready for review -->")
			assert.Contains(t, exported.Markdown, "<!-- Type: blogpost -->")

			write("", "Two.")
			require.NoError(t, Run(t.Context(), config))
			assert.Equal(t, "Ready for review", server.Page(id).ContentStatus)
			write("In progress", "Two.")
			require.NoError(t, Run(t.Context(), config))
			assert.Equal(t, "In progress", server.Page(id).ContentStatus)
			assert.Equal(t, report.StatusUnchanged, runReported(t, config).Status)
			write("~", "Two.")
			require.NoError(t, Run(t.Context(), config))
			assert.Empty(t, server.Page(id).ContentStatus)
			assert.Equal(t, report.StatusUnchanged, runReported(t, config).Status)
		})
	}
}

func TestPublishContentStatusFailureKeepsTrackedBodyVersion(t *testing.T) {
	ctx := context.Background()
	server, _ := docsSpace(t)
	dir := t.TempDir()
	file := func(status, body string) string {
		return writeFile(t, dir, "guide.md", "<!-- Space: DOCS -->\n<!-- Title: Guide -->\n<!-- Content-Status: "+status+" -->\n\n"+body+"\n")
	}
	config := trackingConfig(server, file("Ready for review", "One."))
	config.Update.ChangesOnly = true
	config.Tracking.NoOverwrite = true
	require.NoError(t, Run(ctx, config))
	id := mustFind(t, server, "Guide")

	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/content/"+id+"/state") {
			return http.StatusForbidden, `{"message":"status forbidden"}`, true
		}
		return 0, "", false
	})
	file("In progress", "Two.")
	require.ErrorIs(t, Run(ctx, config), confluence.ErrForbidden)
	assert.Contains(t, server.Page(id).Body, "Two.")

	server.SetFail(nil)
	assert.Equal(t, report.StatusPublished, runReported(t, config).Status)
	assert.Equal(t, "In progress", server.Page(id).ContentStatus)
}

func TestPublishWithoutContentStatusWhenOptionalLookupFails(t *testing.T) {
	for _, tt := range []struct {
		name        string
		endpoint    string
		changesOnly bool
	}{
		{name: "current status", endpoint: "/state"},
		{name: "saved fingerprint", endpoint: "/property/mark.content-status-fingerprint", changesOnly: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server, _ := docsSpace(t)
			guide := server.AddPage("DOCS", "Guide", "page", "")
			server.WritePage(guide.ID, "<p>Old body.</p>")
			server.SetFail(func(r *http.Request) (int, string, bool) {
				if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/content/"+guide.ID+tt.endpoint) {
					return http.StatusInternalServerError, `{"message":"optional lookup failed"}`, true
				}
				return 0, "", false
			})
			file := writeFile(t, t.TempDir(), "guide.md", "<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\nNew body.\n")
			config := changesOnlyConfig(server, file)
			config.Update.ChangesOnly = tt.changesOnly

			require.NoError(t, Run(t.Context(), config))
			assert.Contains(t, server.Page(guide.ID).Body, "New body.")
		})
	}
}

func TestPublishContentStatusWhenFingerprintCannotBeSaved(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	file := writeFile(t, dir, "guide.md", "<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\nBody.\n")
	config := changesOnlyConfig(server, file)
	require.NoError(t, Run(t.Context(), config))
	id := mustFind(t, server, "Guide")
	start := server.Page(id)
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/content/"+id+"/property") {
			return http.StatusForbidden, `{"message":"property write forbidden"}`, true
		}
		return 0, "", false
	})
	writeFile(t, dir, "guide.md", "<!-- Space: DOCS -->\n<!-- Title: Guide -->\n<!-- Content-Status: In progress -->\n\nBody.\n")

	assert.Equal(t, report.StatusPublished, runReported(t, config).Status)
	page := server.Page(id)
	assert.Equal(t, "In progress", page.ContentStatus)
	assert.Equal(t, start.Version+1, page.Version)
}

func TestPublishContentStatusWithoutReadingBody(t *testing.T) {
	server, _ := docsSpace(t)
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method == http.MethodGet && r.URL.Query().Get("expand") == "version,body.storage" {
			return http.StatusForbidden, `{"message":"page body cannot be read"}`, true
		}
		return 0, "", false
	})
	file := writeFile(t, t.TempDir(), "guide.md", "<!-- Space: DOCS -->\n<!-- Title: Guide -->\n<!-- Content-Status: Ready for review -->\n\nBody.\n")
	config := changesOnlyConfig(server, file)
	config.Update.ChangesOnly = false

	require.NoError(t, Run(t.Context(), config))
	id := mustFind(t, server, "Guide")
	assert.Equal(t, "Ready for review", server.Page(id).ContentStatus)
}

func TestContentStatusIsCloudOnly(t *testing.T) {
	server, _ := docsSpace(t)
	server.SetCurrentUser(confluencetest.User{Username: "current", UserKey: "key-current"})
	file := writeFile(t, t.TempDir(), "guide.md", "<!-- Space: DOCS -->\n<!-- Title: Guide -->\n<!-- Content-Status: ~ -->\n\nBody.\n")
	config := changesOnlyConfig(server, file)
	require.ErrorContains(t, Run(context.Background(), config), "requires Confluence Cloud")
}

func TestContentStatusThroughGateway(t *testing.T) {
	server, _ := docsSpace(t)
	file := writeFile(t, t.TempDir(), "guide.md", "<!-- Space: DOCS -->\n<!-- Title: Guide -->\n<!-- Content-Status: Ready for review -->\n\nBody.\n")
	config := changesOnlyConfig(server, file)
	config.Connection.BaseURL += "/ex/confluence/cloud-id"

	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.Contains(r.URL.Path, "/ex/confluence/cloud-id/rest/api/") && !strings.Contains(r.URL.Path, "/state") {
			return http.StatusUnauthorized, `{"message":"scope does not match"}`, true
		}
		return 0, "", false
	})

	assert.Equal(t, report.StatusPublished, runReported(t, config).Status)
	id := mustFind(t, server, "Guide")
	page := server.Page(id)
	require.Equal(t, "Ready for review", page.ContentStatus)
	assert.Equal(t, report.StatusUnchanged, runReported(t, config).Status)
	assert.Equal(t, page.Version, server.Page(id).Version)
}
