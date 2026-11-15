package mark

import (
	"context"
	"io"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A link to a file that the run publishes as a page is a page link, never an
// attachment, even on the first run when the page does not exist yet. A file
// the run does not publish is attached while it has no page, whether it reads
// as a page or is a plain script, and linked as a page once it has one.
func TestAttachReferencedLeavesPublishedNonMarkdownPagesAlone(t *testing.T) {
	const rst = "<!-- Space: DOCS -->\n<!-- Title: Rst Page -->\n\n# Sec\n\nBody.\n"

	for name, tc := range map[string]struct {
		link     string
		glob     string
		existing bool
		attached []string
		body     string
	}{
		"published rst":                {link: "[r](p.rst)", glob: "*", body: `/x/`},
		"published rst with anchor":    {link: "[r](p.rst#sec)", glob: "*", body: `<ri:page ri:content-title="Rst Page"/>`},
		"rst the run does not publish": {link: "[r](p.rst)", glob: "*.md", attached: []string{"p.rst"}, body: `<ri:attachment ri:filename="p.rst"/>`},
		"rst published before":         {link: "[r](p.rst)", glob: "*.md", existing: true, body: `/x/`},
		"plain script still attached":  {link: "[r](run.sh)", glob: "*.md", attached: []string{"run.sh"}, body: `<ri:attachment ri:filename="run.sh"/>`},
	} {
		t.Run(name, func(t *testing.T) {
			server := confluencetest.New(t)
			home := server.AddPage("DOCS", "Home", "page", "")
			server.SetHomepage("DOCS", home.ID)
			if tc.existing {
				server.AddPage("DOCS", "Rst Page", "page", home.ID)
			}

			dir := t.TempDir()
			writeFile(t, dir, "p.rst", rst)
			if tc.link == "[r](run.sh)" {
				writeFile(t, dir, "run.sh", "#!/bin/sh\necho hi\n")
			}
			writeFile(t, dir, "doc.md", "<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\nSee "+tc.link+".\n")

			require.NoError(t, Run(context.Background(), Config{
				PublishConfig: PublishConfig{Output: io.Discard, Rendering: RenderingConfig{AttachReferenced: true}},
				Files:         filepath.Join(dir, tc.glob),
				Connection:    ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
			}))

			api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
			doc, err := api.FindPage(context.Background(), "DOCS", "Doc", confluence.ContentPage)
			require.NoError(t, err)
			require.NotNil(t, doc)

			var attached []string
			for _, a := range server.Attachments(doc.ID) {
				attached = append(attached, a.Filename)
			}
			assert.Equal(t, tc.attached, attached)

			body := server.Page(doc.ID).Body
			assert.Contains(t, body, tc.body)
			if len(tc.attached) == 0 {
				assert.NotContains(t, body, "<ri:attachment")
			}
		})
	}
}
