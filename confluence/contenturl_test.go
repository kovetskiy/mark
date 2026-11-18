package confluence

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseContentURL(t *testing.T) {
	for _, test := range []struct {
		name, collection, path, id, container, title string
	}{
		{"page", LabelCollectionPages, "/spaces/RD/pages/42/Title", "42", "", ""},
		{"custom content", LabelCollectionCustomContent, "/display/RD/customcontent/42", "42", "", ""},
		{"preview", LabelCollectionAttachments, "/pages/viewpageattachments.action?pageId=7&preview=%2F7%2F42%2Fa.pdf", "42", "", ""},
		{"page preview", LabelCollectionAttachments, "/spaces/RD/pages/7/Title?preview=/7/42/a.pdf", "42", "", ""},
		{"download", LabelCollectionAttachments, "/download/attachments/7/a%2Bb%20c.pdf?version=2", "", "7", "a+b c.pdf"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ref, err := ParseContentURL("https://example.com/confluence"+test.path, test.collection)
			require.NoError(t, err)
			assert.Equal(t, "https://example.com/confluence", ref.BaseURL)
			assert.Equal(t, test.id, ref.ID)
			assert.Equal(t, test.container, ref.ContainerID)
			assert.Equal(t, test.title, ref.Title)
		})
	}
	for _, path := range []string{
		"/spaces/RD/pages/7/Title", "/pages/viewpageattachments.action?pageId=7",
		"/pages/viewpageattachments.action?pageId=7&preview=/8/42/a.pdf",
		"/pages/viewpageattachments.action?pageId=7&preview=/7/not-an-id/a.pdf",
		"/download/attachments/7/", "/display/RD/customcontent/42",
	} {
		_, err := ParseContentURL("https://example.com/wiki"+path, LabelCollectionAttachments)
		assert.Error(t, err, path)
	}
}

func TestParsePageURL(t *testing.T) {
	for _, test := range []struct {
		url  string
		want PageRef
	}{
		{"https://example.atlassian.net/wiki/spaces/DOCS/pages/123456/Some+Title",
			PageRef{BaseURL: "https://example.atlassian.net/wiki", PageID: "123456", Space: "DOCS"}},
		{"https://example.atlassian.net/wiki/spaces/DOCS/pages/edit-v2/123456",
			PageRef{BaseURL: "https://example.atlassian.net/wiki", PageID: "123456", Space: "DOCS"}},
		{"https://example.atlassian.net/wiki/spaces/~me/blog/2024/01/31/42/Notes",
			PageRef{BaseURL: "https://example.atlassian.net/wiki", PageID: "42", Space: "~me"}},
		{"https://confluence.example.com/pages/viewpage.action?pageId=77",
			PageRef{BaseURL: "https://confluence.example.com", PageID: "77"}},
		{"https://example.com/confluence/pages/viewpage.action?pageId=77",
			PageRef{BaseURL: "https://example.com/confluence", PageID: "77"}},
		{"https://confluence.example.com/display/DOCS/Some+Title%3F",
			PageRef{BaseURL: "https://confluence.example.com", Space: "DOCS", Title: "Some Title?"}},
		{"https://confluence.example.com/display/DOCS/2024/01/31/Notes",
			PageRef{BaseURL: "https://confluence.example.com", Space: "DOCS", Title: "Notes"}},
		// The context path is found as mark publish --url finds it, any *.action
		// included.
		{"https://example.com/confluence/plugins/viewsource/viewpagesrc.action?pageId=77",
			PageRef{BaseURL: "https://example.com/confluence", PageID: "77"}},
		// A short link carries the page id, encoded: see confluence.GenerateTinyLink.
		{"https://example.atlassian.net/wiki/x/AfIFKgE",
			PageRef{BaseURL: "https://example.atlassian.net/wiki", PageID: "5000000001"}},
		{"https://confluence.example.com/x/D4AB/",
			PageRef{BaseURL: "https://confluence.example.com", PageID: "98319"}},
	} {
		got, err := ParsePageURL(test.url)
		require.NoError(t, err, test.url)
		assert.Equal(t, test.want, got, test.url)
	}

	for _, bad := range []string{"not a url", "/wiki/spaces/DOCS/pages/1", "https://example.atlassian.net/wiki/x/A", "https://example.atlassian.net/wiki/x/a.b", "https://example.com/"} {
		_, err := ParsePageURL(bad)
		assert.Error(t, err, bad)
	}
}
