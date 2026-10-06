package mark

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRelativeLinkDestinationIsDecoded covers the spellings a relative link to
// a document is written in besides the literal one. "my%20other.md" is what VS
// Code and GitHub write for a name with a space in it, and "\_" and "&amp;" are
// CommonMark escapes; each names the same file the <...> form does, and each
// used to be looked up as written and published as a dead relative link.
func TestRelativeLinkDestinationIsDecoded(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Spaced", "page", home.ID)
	server.AddPage("DOCS", "Underscored", "page", home.ID)
	server.AddPage("DOCS", "Ampersand", "page", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "my other.md", "<!-- Space: DOCS -->\n<!-- Title: Spaced -->\n\nx\n")
	writeFile(t, dir, "under_score.md", "<!-- Space: DOCS -->\n<!-- Title: Underscored -->\n\nx\n")
	writeFile(t, dir, "a&b.md", "<!-- Space: DOCS -->\n<!-- Title: Ampersand -->\n\nx\n")

	for name, link := range map[string]string{
		"angle brackets":    "[a](<my other.md>)",
		"percent-encoded":   "[a](my%20other.md)",
		"backslash escape":  `[a](under\_score.md)`,
		"entity reference":  "[a](a&amp;b.md)",
		"with a fragment":   "[a](my%20other.md#x)",
		"numeric reference": "[a](a&#38;b.md)",
	} {
		t.Run(name, func(t *testing.T) {
			file := writeFile(t, dir, "doc.md",
				"<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\nSee "+link+".\n")

			api := confluence.NewAPI(server.URL, "user", "token", false)
			target, err := ProcessFile(file, api, Config{
				BaseURL: server.URL, Username: "user", Password: "token",
				Files: file, Output: io.Discard,
			})
			require.NoError(t, err)

			body := server.Page(target.ID).Body
			assert.Equal(t, 1, strings.Count(body, "/x/"), body)
			assert.NotContains(t, body, ".md", body)
		})
	}
}

// TestEscapedLinkFindsItsDeclaredAttachment: a link to an attachment the
// document declared is matched against it however the path is spelled, as an
// image's already was, rather than left pointing at a file Confluence does not
// have.
func TestEscapedLinkFindsItsDeclaredAttachment(t *testing.T) {
	for name, link := range map[string]string{
		"percent-encoded":  "[r](my%20report.pdf)",
		"backslash escape": `[r](my\_report.pdf)`,
		"entity reference": "[r](my&amp;report.pdf)",
	} {
		t.Run(name, func(t *testing.T) {
			server := confluencetest.New(t)
			home := server.AddPage("DOCS", "Home", "page", "")
			server.SetHomepage("DOCS", home.ID)

			dir := t.TempDir()
			header := ""
			for _, f := range []string{"my report.pdf", "my_report.pdf", "my&report.pdf"} {
				require.NoError(t, os.WriteFile(filepath.Join(dir, f), []byte(f), 0o600))
				header += "<!-- Attachment: " + f + " -->\n"
			}
			file := writeFile(t, dir, "doc.md",
				"<!-- Space: DOCS -->\n<!-- Title: Doc -->\n"+header+"\nSee "+link+".\n")

			api := confluence.NewAPI(server.URL, "user", "token", false)
			target, err := ProcessFile(file, api, Config{
				BaseURL: server.URL, Username: "user", Password: "token",
				Files: file, Output: io.Discard,
			})
			require.NoError(t, err)

			body := server.Page(target.ID).Body
			assert.Contains(t, body, "download/attachments", body)
			assert.NotContains(t, body, `href="my`, body)
		})
	}
}

// TestEscapedLinkedFileIsAttached: --attach-referenced finds the file a link
// names when the link spells it with a CommonMark escape.
func TestEscapedLinkedFileIsAttached(t *testing.T) {
	for name, link := range map[string]string{
		"percent-encoded":  "[r](files/my%20report.pdf)",
		"backslash escape": `[r](files/my\_report.pdf)`,
		"entity reference": "[r](files/my&amp;report.pdf)",
	} {
		t.Run(name, func(t *testing.T) {
			server := confluencetest.New(t)
			home := server.AddPage("DOCS", "Home", "page", "")
			server.SetHomepage("DOCS", home.ID)

			dir := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "files"), 0o755))
			for _, f := range []string{"my report.pdf", "my_report.pdf", "my&report.pdf"} {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "files", f), []byte(f), 0o600))
			}
			file := writeFile(t, dir, "doc.md",
				"<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\nSee "+link+".\n")

			api := confluence.NewAPI(server.URL, "user", "token", false)
			target, err := ProcessFile(file, api, Config{
				BaseURL: server.URL, Username: "user", Password: "token",
				Files: file, AttachReferenced: true, Output: io.Discard,
			})
			require.NoError(t, err)

			require.Len(t, server.Attachments(target.ID), 1)
			assert.Contains(t, server.Page(target.ID).Body, "<ri:attachment")
		})
	}
}
