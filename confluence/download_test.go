package confluence

import (
	"bytes"
	"context"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAttachmentURL(t *testing.T) {
	attachment := func(context, download string) AttachmentInfo {
		info := AttachmentInfo{Filename: "a.png"}
		info.Links.Context = context
		info.Links.Download = download
		return info
	}

	for _, test := range []struct {
		name, base string
		info       AttachmentInfo
		want       string
	}{
		{"a base URL that ends in the context", "https://example.atlassian.net/wiki",
			attachment("/wiki", "/download/attachments/1/a.png"), "https://example.atlassian.net/wiki/download/attachments/1/a.png"},
		{"a base URL without it", "https://example.com",
			attachment("/confluence", "/download/attachments/1/a.png"), "https://example.com/confluence/download/attachments/1/a.png"},
		{"no context", "https://example.com/",
			attachment("", "/download/attachments/1/a.png?version=2"), "https://example.com/download/attachments/1/a.png?version=2"},
		{"an absolute link on the same host", "https://example.com",
			attachment("", "https://example.com/download/attachments/1/a.png"), "https://example.com/download/attachments/1/a.png"},
		{"the default port written out", "https://example.com",
			attachment("", "https://example.com:443/download/attachments/1/a.png"), "https://example.com:443/download/attachments/1/a.png"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := NewAPI(test.base, "u", "p", false).AttachmentURL("1", test.info)
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}

	for name, link := range map[string]string{
		"another host": "https://elsewhere.example/a.png",
		"plain http":   "http://example.com/download/attachments/1/a.png",
		"another port": "https://example.com:8443/download/attachments/1/a.png",
	} {
		t.Run("an absolute link to "+name+" is refused", func(t *testing.T) {
			_, err := NewAPI("https://example.com", "u", "p", false).
				AttachmentURL("1", attachment("", link))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "refusing to send credentials")
		})
	}

	t.Run("through the gateway, the REST download endpoint", func(t *testing.T) {
		info := attachment("/wiki", "/download/attachments/1/a.png")
		info.ID = "att2"
		got, err := NewAPI("https://api.atlassian.com/ex/confluence/cloud", "", "token", false).AttachmentURL("1", info)
		require.NoError(t, err)
		assert.Equal(t, "https://api.atlassian.com/ex/confluence/cloud/rest/api/content/1/child/attachment/att2/download", got)
	})
}

func TestDownloadAttachment(t *testing.T) {
	server := confluencetest.New(t)
	page := server.AddPage("DOCS", "Page", "page", "")
	server.AddAttachmentData(page.ID, "a b#1.png", []byte("content"))

	api := NewAPI(server.URL, "user", "token", false)
	attachments, err := api.GetAttachments(context.Background(), page.ID)
	require.NoError(t, err)
	require.Len(t, attachments, 1)

	var got bytes.Buffer
	require.NoError(t, api.DownloadAttachment(context.Background(), page.ID, attachments[0], &got))
	assert.Equal(t, "content", got.String())

	missing := attachments[0]
	missing.Links.Download = "/download/attachments/" + page.ID + "/gone.png"
	err = api.DownloadAttachment(context.Background(), page.ID, missing, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}
