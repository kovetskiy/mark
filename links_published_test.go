package mark

import (
	"context"
	"io"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A link to a file that is published as a page is a page link, never an
// attachment, even on the first run when the page does not exist yet; a plain
// script is still attached.
func TestAttachReferencedLeavesPublishedNonMarkdownPagesAlone(t *testing.T) {
	for name, tc := range map[string]struct {
		link     string
		attached int
	}{
		"published rst":               {"[r](p.rst)", 0},
		"published rst with anchor":   {"[r](p.rst#sec)", 0},
		"plain script still attached": {"[r](run.sh)", 1},
	} {
		t.Run(name, func(t *testing.T) {
			server := confluencetest.New(t)
			home := server.AddPage("DOCS", "Home", "page", "")
			server.SetHomepage("DOCS", home.ID)

			dir := t.TempDir()
			writeFile(t, dir, "p.rst", "<!-- Space: DOCS -->\n<!-- Title: Rst Page -->\n\nBody.\n")
			writeFile(t, dir, "run.sh", "#!/bin/sh\necho hi\n")
			file := writeFile(t, dir, "doc.md",
				"<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\nSee "+tc.link+".\n")

			api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
			target, err := publishOne(context.Background(), file, api, PublishConfig{
				Rendering: RenderingConfig{AttachReferenced: true},
				Output:    io.Discard,
			})
			require.NoError(t, err)

			assert.Len(t, server.Attachments(target.ID), tc.attached)
			if tc.attached == 0 {
				assert.NotContains(t, server.Page(target.ID).Body, "<ri:attachment")
			} else {
				assert.Contains(t, server.Page(target.ID).Body, "<ri:attachment")
			}
		})
	}
}
