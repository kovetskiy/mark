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

// A linked document handed over from memory is read as the resolver reads it,
// whatever a file of its name on disk holds: the link waits for the page and
// the file on disk is not attached.
func TestAttachReferencedReadsALinkedDocumentFromTheRun(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "p.rst", "Source text, with no headers.\n")

	require.NoError(t, Run(context.Background(), Config{
		PublishConfig: PublishConfig{Output: io.Discard, Rendering: RenderingConfig{AttachReferenced: true}},
		Documents: []Document{
			{Path: filepath.Join(dir, "doc.md"), Content: []byte("<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\nSee [r](p.rst).\n")},
			{Path: filepath.Join(dir, "p.rst"), Content: []byte("<!-- Space: DOCS -->\n<!-- Title: Rst Page -->\n\nBody.\n")},
		},
		Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
	}))

	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
	doc, err := api.FindPage(context.Background(), "DOCS", "Doc", confluence.ContentPage)
	require.NoError(t, err)
	require.NotNil(t, doc)

	assert.Empty(t, server.Attachments(doc.ID))
	assert.Contains(t, server.Page(doc.ID).Body, `/x/`)
}

// The start of a file is all that is read of it beside Documents too, from
// memory, from the run's files and from the root alike.
func TestDocumentFilesReadOnlyTheStartOfAFile(t *testing.T) {
	dir := t.TempDir()
	file := writeFile(t, dir, "run.rst", "0123456789")
	other := writeFile(t, dir, "other.log", "abcdefghij")
	memory := filepath.Join(dir, "memory.rst")

	inputs, err := indexInputs([]string{file}, []Document{{Path: memory, Content: []byte("ABCDEFGHIJ")}})
	require.NoError(t, err)

	files := documentFiles{inputs: inputs}

	for name, want := range map[string]string{file: "0123", other: "abcd", memory: "ABCD"} {
		head, err := files.ReadHead(name, 4)
		require.NoError(t, err)
		assert.Equal(t, want, string(head), name)
	}

	head, err := files.ReadHead(memory, 64)
	require.NoError(t, err)
	assert.Equal(t, "ABCDEFGHIJ", string(head))
}

// A file the run publishes is a document whatever its name, so one that takes
// its title from its heading is linked as a page and not attached, and a file
// the run does not publish needs a title of its own to be one.
func TestLinkToAPublishedFileWithADerivedTitleIsAPageLink(t *testing.T) {
	for name, tc := range map[string]struct {
		glob     string
		attached []string
		body     string
	}{
		"published":     {glob: "*", body: `/x/`},
		"not published": {glob: "*.md", attached: []string{"h.rst"}, body: `<ri:attachment ri:filename="h.rst"/>`},
	} {
		t.Run(name, func(t *testing.T) {
			server := confluencetest.New(t)
			home := server.AddPage("DOCS", "Home", "page", "")
			server.SetHomepage("DOCS", home.ID)

			dir := t.TempDir()
			writeFile(t, dir, "h.rst", "# Heading\n\nBody.\n")
			writeFile(t, dir, "doc.md", "# Doc\n\nSee [r](h.rst).\n")

			require.NoError(t, Run(context.Background(), Config{
				PublishConfig: PublishConfig{
					Output:    io.Discard,
					Page:      PageConfig{Space: "DOCS", TitleFromH1: true},
					Rendering: RenderingConfig{AttachReferenced: true},
				},
				Files:      filepath.Join(dir, tc.glob),
				Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
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
			assert.Contains(t, server.Page(doc.ID).Body, tc.body)
		})
	}
}
