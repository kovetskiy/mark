package mark

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// checkLinksRun publishes a directory containing one document with the given
// body and returns whatever the run made of it.
func checkLinksRun(t *testing.T, modes []string, body string, extra map[string]string) error {
	t.Helper()

	return checkLinksRunWith(t, modes, false, body, extra)
}

func checkLinksRunWith(t *testing.T, modes []string, warnOnly bool, body string, extra map[string]string) error {
	t.Helper()

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	for name, content := range extra {
		if content == "" {
			require.NoError(t, os.Mkdir(filepath.Join(dir, name), 0o755))
			continue
		}
		writeFile(t, dir, name, content)
	}
	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n\n"+body)

	return Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files:              filepath.Join(dir, "doc.md"),
		Features:           []string{"mention"},
		CheckLinks:         modes,
		CheckLinksWarnOnly: warnOnly,
		Output:             io.Discard,
	})
}

func TestCheckLinksInternal(t *testing.T) {
	t.Run("a link to a file that does not exist fails", func(t *testing.T) {
		err := checkLinksRun(t, []string{"internal"}, "See [other](./missing.md).\n", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "./missing.md")
		assert.Contains(t, err.Error(), "there is no such file")
	})

	t.Run("a link to a file that is never published fails", func(t *testing.T) {
		// A file with no headers still gets metadata when the run knows a
		// space, so this shows up as a missing title rather than missing
		// metadata. Either way the file never becomes a page.
		err := checkLinksRun(t, []string{"internal"}, "See [notes](./notes.md).\n",
			map[string]string{"notes.md": "Just notes, no metadata.\n"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "never published")
	})

	t.Run("a link to a directory fails", func(t *testing.T) {
		err := checkLinksRun(t, []string{"internal"}, "See [it](./sub).\n",
			map[string]string{"sub": ""})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "directory")
	})

	t.Run("a page not published yet is a warning, not a failure", func(t *testing.T) {
		// The ordinary state of a first run over files that link to each other.
		// Failing here would break a build that succeeds on the second attempt.
		err := checkLinksRun(t, []string{"internal"}, "See [other](./other.md).\n",
			map[string]string{
				"other.md": "<!-- Space: DOCS -->\n<!-- Title: Other -->\n\nOther.\n",
			})
		assert.NoError(t, err)
	})

	t.Run("things that are not repository links are left alone", func(t *testing.T) {
		// Including an ac: link and an unreachable URL: neither kind was asked
		// for, so neither is judged.
		err := checkLinksRun(t, []string{"internal"},
			"[a](https://example.invalid/nope) [b](#heading) [c](mailto:x@example.com) "+
				"[d](ac:No Such Page) [e](/site/absolute)\n", nil)
		assert.NoError(t, err)
	})

	t.Run("without the flag a broken link is not an error", func(t *testing.T) {
		err := checkLinksRun(t, nil, "See [other](./missing.md).\n", nil)
		assert.NoError(t, err)
	})
}

func TestCheckLinksAll(t *testing.T) {
	var reachable, missing string
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer remote.Close()
	reachable = remote.URL + "/ok"
	missing = remote.URL + "/gone"

	t.Run("an external URL that answers passes", func(t *testing.T) {
		err := checkLinksRun(t, []string{"all"}, "See [it]("+reachable+").\n", nil)
		assert.NoError(t, err)
	})

	t.Run("an external URL that does not answer fails", func(t *testing.T) {
		err := checkLinksRun(t, []string{"all"}, "See [it]("+missing+").\n", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "404")
	})

	t.Run("internal alone does not request external URLs", func(t *testing.T) {
		err := checkLinksRun(t, []string{"internal"}, "See [it]("+missing+").\n", nil)
		assert.NoError(t, err)
	})

	t.Run("internal and confluence together still skip external", func(t *testing.T) {
		// The combination the set exists for: check what is cheap, leave the
		// network alone.
		err := checkLinksRun(t, []string{"internal", "confluence"},
			"See [it]("+missing+").\n", nil)
		assert.NoError(t, err)
	})
}

func TestCheckLinksRejectsAnUnknownMode(t *testing.T) {
	err := checkLinksRun(t, []string{"sometimes"}, "Body.\n", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "internal")
	assert.Contains(t, err.Error(), "confluence")
	assert.Contains(t, err.Error(), "external")
}

func TestCheckLinksConfluence(t *testing.T) {
	t.Run("a link to a page that exists passes", func(t *testing.T) {
		err := checkLinksRun(t, []string{"confluence"}, "See [it](ac:Parent).\n", nil)
		assert.NoError(t, err)
	})

	t.Run("a link to a page that does not exist fails", func(t *testing.T) {
		err := checkLinksRun(t, []string{"confluence"}, "See [it](ac:Nowhere).\n", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "there is no page \"Nowhere\"")
	})

	t.Run("the title comes from the link text when nothing follows the colon", func(t *testing.T) {
		// [Parent](ac:) is the form where the words between the brackets are
		// the page title, and the renderer reads it that way too.
		assert.NoError(t, checkLinksRun(t, []string{"confluence"}, "See [Parent](ac:).\n", nil))

		err := checkLinksRun(t, []string{"confluence"}, "See [Nowhere](ac:).\n", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Nowhere")
	})

	t.Run("an anchor after the title is not part of the title", func(t *testing.T) {
		// The renderer links [it](<ac:Parent#setup>) to the "setup" section
		// of Parent; the check looked up a page called "Parent#setup" and
		// failed the run over a link that works.
		assert.NoError(t, checkLinksRun(t, []string{"confluence"}, "See [it](<ac:Parent#setup>).\n", nil))

		err := checkLinksRun(t, []string{"confluence"}, "See [it](<ac:Nowhere#setup>).\n", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "there is no page \"Nowhere\"")
	})

	t.Run("internal alone does not judge an ac: link", func(t *testing.T) {
		err := checkLinksRun(t, []string{"internal"}, "See [it](ac:Nowhere).\n", nil)
		assert.NoError(t, err)
	})

	t.Run("confluence alone does not judge a relative link", func(t *testing.T) {
		err := checkLinksRun(t, []string{"confluence"}, "See [it](./missing.md).\n", nil)
		assert.NoError(t, err)
	})
}

// TestCheckLinksReportsEveryBrokenLink pins that a file is not abandoned at the
// first failure. Reporting one at a time turns a page with several into as many
// runs to find out.
//
// Only the links judged while the file compiles are counted here. An ac: link
// is judged after the run, when everything that was going to be published has
// been, and is reported separately for that reason.
func TestCheckLinksReportsEveryBrokenLink(t *testing.T) {
	err := checkLinksRun(t, []string{"all"},
		"[a](./one.md) [b](./two.md) [c](./three.md)\n", nil)

	require.Error(t, err)
	for _, expected := range []string{"./one.md", "./two.md", "./three.md"} {
		assert.Contains(t, err.Error(), expected)
	}
	assert.Contains(t, err.Error(), "3 links do not resolve")
}

func TestCheckLinksCountsOneLinkAsSingular(t *testing.T) {
	err := checkLinksRun(t, []string{"internal"}, "[a](./one.md)\n", nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "1 link does not resolve")
}

// TestCheckLinksWarnOnly is the adoption path: see the whole list without the
// build failing over it.
func TestCheckLinksWarnOnly(t *testing.T) {
	t.Run("a broken link does not fail the run", func(t *testing.T) {
		err := checkLinksRunWith(t, []string{"all"}, true,
			"[a](./one.md) [b](ac:Nowhere)\n", nil)
		assert.NoError(t, err)
	})

	t.Run("the page is still published", func(t *testing.T) {
		// A run that reports and refuses to publish would be the worst of both.
		server := confluencetest.New(t)
		home := server.AddPage("DOCS", "Home", "page", "")
		server.SetHomepage("DOCS", home.ID)
		server.AddPage("DOCS", "Parent", "page", home.ID)

		dir := t.TempDir()
		writeFile(t, dir, "doc.md",
			"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n\n"+
				"Body here. [a](./missing.md)\n")

		require.NoError(t, Run(Config{
			BaseURL: server.URL, Username: "user", Password: "token",
			Files:      filepath.Join(dir, "doc.md"),
			Features:   []string{"mention"},
			CheckLinks: []string{"internal"}, CheckLinksWarnOnly: true,
			Output: io.Discard,
		}))

		api := confluence.NewAPI(server.URL, "user", "token", false)
		doc, err := api.FindPage("DOCS", "Doc", "page")
		require.NoError(t, err)
		require.NotNil(t, doc)
		assert.Contains(t, server.Page(doc.ID).Body, "Body here")
	})

	t.Run("without it the same document fails", func(t *testing.T) {
		err := checkLinksRunWith(t, []string{"all"}, false,
			"[a](./one.md) [b](ac:Nowhere)\n", nil)
		require.Error(t, err)
	})
}

// TestCheckLinksWarnOnlyRequiresCheckLinks: on its own the flag checks nothing,
// so somebody who set it and saw no warnings would conclude their links were
// fine. That reading is what makes this a refusal rather than a warning.
func TestCheckLinksWarnOnlyRequiresCheckLinks(t *testing.T) {
	err := Run(Config{
		BaseURL: "http://127.0.0.1:1", Username: "user", Password: "token",
		Files: "none", CheckLinksWarnOnly: true, Output: io.Discard,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "--check-links")
}

// TestCheckLinksWarnOnlyIsAnnotated covers what --check-links-warn-only is for
// in CI: told about a link that does not resolve, without the run failing.
//
// The warning has to reach the report and not only the log. A line in the build
// output is one nobody scrolls to; an annotation appears against the file in the
// pull request, which is where somebody is already looking.
func TestCheckLinksWarnOnlyIsAnnotated(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "guide"), 0o755))
	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\nSee [guide](guide).\n")

	var out bytes.Buffer

	err := Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files:              filepath.Join(dir, "doc.md"),
		CheckLinks:         []string{"internal"},
		CheckLinksWarnOnly: true,
		OutputFormat:       "github",
		Output:             &out,
	})
	require.NoError(t, err, "warn-only must not fail the run")

	assert.Contains(t, out.String(), "::notice", "the document still published")
	assert.Contains(t, out.String(), "::warning", "and the link is still worth saying")
	assert.Contains(t, out.String(), "it is a directory, not a document")
}

// TestCheckLinksFailureIsAnnotatedAsAnError is the other half, and the reason
// the two are told apart: without warn-only the run fails, and the annotation
// says so rather than being a warning beside a page that never published.
func TestCheckLinksFailureIsAnnotatedAsAnError(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\nSee [other](./missing.md).\n")

	var out bytes.Buffer

	err := Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files:        filepath.Join(dir, "doc.md"),
		CheckLinks:   []string{"internal"},
		OutputFormat: "github",
		Output:       &out,
	})
	require.Error(t, err)

	assert.Contains(t, out.String(), "::error")
	assert.NotContains(t, out.String(), "::warning")
}

// TestCheckLinksUnderCompileOnly covers the invocation the flag is most likely
// to be used with, and the one where it did nothing at all.
//
// --compile-only is what validates documents in CI without Confluence
// credentials, so a pull request gate reading "mark --compile-only
// --check-links all" is the ordinary way to ask for this. The links were
// resolved either way; the branch simply returned before anything read what
// that found, and the run said nothing and exited 0.
func TestCheckLinksUnderCompileOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\nSee [other](./missing.md).\n")

	err := Run(Config{
		BaseURL: "https://confluence.example.invalid", Username: "user", Password: "token",
		Files:       filepath.Join(dir, "doc.md"),
		CompileOnly: true,
		CheckLinks:  []string{"internal"},
		Output:      io.Discard,
	})
	require.Error(t, err)

	assert.Contains(t, err.Error(), "./missing.md")
	assert.Contains(t, err.Error(), "there is no such file")
}

// TestCheckLinksUnderDryRunReportsToo covers the other half of the same branch.
func TestCheckLinksUnderDryRunReportsToo(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\nSee [other](./missing.md).\n")

	err := Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files:      filepath.Join(dir, "doc.md"),
		DryRun:     true,
		CheckLinks: []string{"internal"},
		Output:     io.Discard,
	})
	require.Error(t, err)

	assert.Contains(t, err.Error(), "./missing.md")
}

// TestCompileOnlyWithoutCheckLinksIsSilent is the boundary: a broken link is
// only an error when the run asked about links, whichever mode it is in.
func TestCompileOnlyWithoutCheckLinksIsSilent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\nSee [other](./missing.md).\n")

	require.NoError(t, Run(Config{
		BaseURL: "https://confluence.example.invalid", Username: "user", Password: "token",
		Files:       filepath.Join(dir, "doc.md"),
		CompileOnly: true,
		Output:      io.Discard,
	}))
}

// TestCompileOnlyWarnOnlyDoesNotFail covers the third combination, since
// warn-only is how a team adopts the check before the build starts failing.
func TestCompileOnlyWarnOnlyDoesNotFail(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\nSee [other](./missing.md).\n")

	require.NoError(t, Run(Config{
		BaseURL: "https://confluence.example.invalid", Username: "user", Password: "token",
		Files:              filepath.Join(dir, "doc.md"),
		CompileOnly:        true,
		CheckLinks:         []string{"internal"},
		CheckLinksWarnOnly: true,
		Output:             io.Discard,
	}))
}

// TestCrossFileAnchorBecomesAPageAnchor: a fragment on a Confluence URL names
// nothing. Confluence puts a heading's anchor in the Anchor macro, not in the
// address, so ".../x/abc123#Setup" scrolls nowhere however the fragment is
// spelled -- and the fragment was the author's own slug, which does not match
// the id mark generates either.
//
// ac:link with ri:page and ac:anchor is how the storage format says "that
// section of that page", and the slug is folded onto mark's id the same way a
// same-page link is.
func TestCrossFileAnchorBecomesAPageAnchor(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	writeFile(t, dir, "a-other.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Other Page -->

## Setup Guide

Text.
`)
	writeFile(t, dir, "b-doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Main -->

See [the setup](./a-other.md#setup-guide).
`)

	require.NoError(t, Run(publishConfig(server.URL, dir+"/*.md")))

	body := bodyOfPageTitled(t, server, "Main")

	assert.Contains(t, body, `<ac:link ac:anchor="Setup-Guide"><ri:page ri:content-title="Other Page"/>`,
		"the author's slug is folded onto the id mark gave the heading")
	assert.NotContains(t, body, "#setup-guide", "and no fragment is left on a URL")
}

// TestCrossFileLinkWithoutAnAnchorIsUnchanged is the control: a link to the
// document rather than to a section of it still resolves to the page's URL.
func TestCrossFileLinkWithoutAnAnchorIsUnchanged(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	writeFile(t, dir, "a-other.md", markdownWithTitle("Other Page"))
	writeFile(t, dir, "b-doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Main -->

See [the page](./a-other.md).
`)

	require.NoError(t, Run(publishConfig(server.URL, dir+"/*.md")))

	body := bodyOfPageTitled(t, server, "Main")

	assert.Contains(t, body, "<a href=", "a whole-page link is still a link")
	assert.NotContains(t, body, "ac:anchor")
}

// TestCrossFileAnchorThatNamesNoHeadingIsLeftAlone: guessing which section was
// meant would send the reader somewhere the author did not choose, so a
// fragment that matches nothing keeps the behaviour it had.
func TestCrossFileAnchorThatNamesNoHeadingIsLeftAlone(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	writeFile(t, dir, "a-other.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Other Page -->

## Setup Guide
`)
	writeFile(t, dir, "b-doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Main -->

See [nothing there](./a-other.md#no-such-heading).
`)

	require.NoError(t, Run(publishConfig(server.URL, dir+"/*.md")))

	body := bodyOfPageTitled(t, server, "Main")

	assert.NotContains(t, body, "ac:anchor")
	assert.True(t, strings.Contains(body, "#no-such-heading"),
		"the link keeps the fragment the author wrote")
}

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

func followupServer(t *testing.T) *confluencetest.Server {
	t.Helper()
	s := confluencetest.New(t)
	home := s.AddPage("DOCS", "Home", "page", "")
	s.SetHomepage("DOCS", home.ID)
	s.AddPage("DOCS", "Parent", "page", home.ID)
	return s
}

// TestConfluenceLinkToAPagePublishedLaterInTheRun: an ac: link is checked by
// looking the page up, and a page named here may be published by a file that
// sorts after this one. Failing would make the check depend on the order the
// files happen to be in.
func TestConfluenceLinkToAPagePublishedLaterInTheRun(t *testing.T) {
	server := followupServer(t)
	dir := t.TempDir()

	writeFile(t, dir, "a-doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: A -->\n\nSee [B](ac:B).\n")
	writeFile(t, dir, "b-doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: B -->\n\nB.\n")

	assert.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		CheckLinks: []string{"confluence"}, Output: io.Discard,
	}))
}

// TestLinkInsideAnIncludedFragment: a fragment's links are written from where
// the fragment lives, not from where it is included.
func TestLinkInsideAnIncludedFragment(t *testing.T) {
	server := followupServer(t)
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "frag"), 0o755))

	// Named so the fragment is not itself published by the glob below.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "frag", "part.inc"),
		[]byte("See [the target](./target.md).\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "frag", "target.md"),
		[]byte("<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Target -->\n\nTarget.\n"), 0o600))

	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n\n"+
			"<!-- Include: frag/part.inc -->\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		// Both levels, so the fragment's neighbour is published too.
		Files: filepath.Join(dir, "**", "*.md"), Features: []string{"mention"},
		CheckLinks: []string{"internal"}, Output: io.Discard,
	}
	// Twice, so the target exists by the time the link is resolved.
	require.NoError(t, Run(config))
	require.NoError(t, Run(config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	doc, err := api.FindPage("DOCS", "Doc", "page")
	require.NoError(t, err)
	require.NotNil(t, doc)

	assert.Contains(t, server.Page(doc.ID).Body, "/x/",
		"the fragment's link should resolve to the target page")
}

// TestOwnDirectoryWinsOverAnIncludedOne: adding places to look must not change
// what an unambiguous link already meant.
func TestOwnDirectoryWinsOverAnIncludedOne(t *testing.T) {
	server := followupServer(t)
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "frag"), 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "frag", "part.inc"),
		[]byte("Fragment.\n"), 0o600))
	// Both directories hold a target.md; the document's own must win.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "frag", "target.md"),
		[]byte("<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Wrong -->\n\nx.\n"), 0o600))
	writeFile(t, dir, "target.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Right -->\n\nx.\n")

	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n\n"+
			"<!-- Include: frag/part.inc -->\n\nSee [it](./target.md).\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"}, Output: io.Discard,
	}
	require.NoError(t, Run(config))
	require.NoError(t, Run(config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	right, err := api.FindPage("DOCS", "Right", "page")
	require.NoError(t, err)
	require.NotNil(t, right)

	doc, err := api.FindPage("DOCS", "Doc", "page")
	require.NoError(t, err)
	assert.Contains(t, server.Page(doc.ID).Body, right.ID[:0]+"/x/",
		"the link should have resolved")
	// The Wrong page must not exist at all, since nothing links to it and no
	// file publishes it under that title in this run.
	wrong, err := api.FindPage("DOCS", "Wrong", "page")
	require.NoError(t, err)
	assert.Nil(t, wrong)
}

// TestMacroDefinedAboveTheHeaders: a Macro definition spans several lines, and
// the lines after its first are not headers. Reading them used to end the
// metadata scan, so every header below was lost and the run failed with "space
// is not set".
func TestMacroDefinedAboveTheHeaders(t *testing.T) {
	server := followupServer(t)
	dir := t.TempDir()

	writeFile(t, dir, "doc.md",
		"<!-- Macro: MYJIRA-\\d+\n"+
			"     Template: ac:jira:ticket\n"+
			"     Ticket: ${0} -->\n"+
			"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n\n"+
			"See MYJIRA-123.\n")

	require.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"}, Output: io.Discard,
	}))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	doc, err := api.FindPage("DOCS", "Doc", "page")
	require.NoError(t, err)
	require.NotNil(t, doc, "the headers below the macro must still be read")

	body := server.Page(doc.ID).Body
	assert.Contains(t, body, `ac:name="jira"`,
		"the macro defined above the headers must still apply")
	assert.NotContains(t, body, "Template: ac:jira:ticket",
		"the definition must not be published as content")
}

func twoPassServer(t *testing.T) *confluencetest.Server {
	t.Helper()
	s := confluencetest.New(t)
	home := s.AddPage("DOCS", "Home", "page", "")
	s.SetHomepage("DOCS", home.ID)
	s.AddPage("DOCS", "Parent", "page", home.ID)
	return s
}

// TestLinksResolveOnTheFirstRun is the point of the change. A document linking
// to another the same run creates had nothing to find, so the link stayed dead
// until somebody ran mark a second time.
func TestLinksResolveOnTheFirstRun(t *testing.T) {
	server := twoPassServer(t)
	dir := t.TempDir()
	header := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n"

	writeFile(t, dir, "a-doc.md", header+"<!-- Title: A -->\n\nSee [B](./b-doc.md).\n")
	writeFile(t, dir, "b-doc.md", header+"<!-- Title: B -->\n\nB.\n")

	require.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		Output: io.Discard,
	}))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	a, err := api.FindPage("DOCS", "A", "page")
	require.NoError(t, err)
	require.NotNil(t, a)

	body := server.Page(a.ID).Body
	assert.Contains(t, body, "/x/", "the link must resolve within one run")
	assert.NotContains(t, body, "./b-doc.md")
}

// TestSecondPassOnlyRepublishesWhatWaited: a document with nothing waiting must
// not be written twice.
func TestSecondPassOnlyRepublishesWhatWaited(t *testing.T) {
	server := twoPassServer(t)
	dir := t.TempDir()
	header := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n"

	writeFile(t, dir, "a-doc.md", header+"<!-- Title: A -->\n\nSee [B](./b-doc.md).\n")
	writeFile(t, dir, "b-doc.md", header+"<!-- Title: B -->\n\nB, linking nowhere.\n")

	require.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		Output: io.Discard,
	}))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	a, err := api.FindPage("DOCS", "A", "page")
	require.NoError(t, err)
	b, err := api.FindPage("DOCS", "B", "page")
	require.NoError(t, err)

	assert.Greater(t, server.Page(a.ID).Version, server.Page(b.ID).Version,
		"only the document that was waiting should have been published again")
}

// TestSecondRunDoesNothingExtra: once the pages exist there is nothing to wait
// for, so a later run publishes each document once.
func TestSecondRunDoesNothingExtra(t *testing.T) {
	server := twoPassServer(t)
	dir := t.TempDir()
	header := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n"

	writeFile(t, dir, "a-doc.md", header+"<!-- Title: A -->\n\nSee [B](./b-doc.md).\n")
	writeFile(t, dir, "b-doc.md", header+"<!-- Title: B -->\n\nB.\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		Output: io.Discard,
	}
	require.NoError(t, Run(config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	a, err := api.FindPage("DOCS", "A", "page")
	require.NoError(t, err)
	before := server.Page(a.ID).Version

	require.NoError(t, Run(config))

	assert.Equal(t, before+1, server.Page(a.ID).Version,
		"a run with nothing waiting should publish each document once")
}

// TestCheckLinksSeesResolvedLinksForPagesThisRunCreates covers the interaction
// with --check-links, whose "not in the space yet" warning existed because of
// the limitation this removes.
//
// Asserting only that the run succeeds would prove nothing -- that case was a
// warning before as well -- so the link is checked to have actually resolved.
func TestCheckLinksSeesResolvedLinksForPagesThisRunCreates(t *testing.T) {
	server := twoPassServer(t)
	dir := t.TempDir()
	header := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n"

	writeFile(t, dir, "a-doc.md", header+"<!-- Title: A -->\n\nSee [B](./b-doc.md).\n")
	writeFile(t, dir, "b-doc.md", header+"<!-- Title: B -->\n\nB.\n")

	require.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		CheckLinks: []string{"internal"}, Output: io.Discard,
	}))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	a, err := api.FindPage("DOCS", "A", "page")
	require.NoError(t, err)
	require.NotNil(t, a)
	assert.Contains(t, server.Page(a.ID).Body, "/x/",
		"nothing should be left waiting for a page this run created")
}

// TestDryRunDoesNotWaitForPages: nothing is published on a dry run, so nothing
// a link waits for will come to exist and the wait is worth reporting.
func TestDryRunDoesNotWaitForPages(t *testing.T) {
	server := twoPassServer(t)
	dir := t.TempDir()
	header := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n"

	writeFile(t, dir, "a-doc.md", header+"<!-- Title: A -->\n\nSee [B](./b-doc.md).\n")
	writeFile(t, dir, "b-doc.md", header+"<!-- Title: B -->\n\nB.\n")

	var out strings.Builder
	require.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		DryRun: true, Output: &out,
	}))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	a, err := api.FindPage("DOCS", "A", "page")
	require.NoError(t, err)
	assert.Nil(t, a, "a dry run publishes nothing")
}

// linkedPair publishes two documents where one links to the other, and returns
// the body of the linking page.
func linkedPair(t *testing.T, body string) (string, *confluencetest.Server) {
	t.Helper()
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "a-other.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Other -->\n\nOther.\n")
	writeFile(t, dir, "b-doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n\n"+body)

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"}, Output: io.Discard,
	}
	// Twice, so the target exists by the time the link is resolved.
	require.NoError(t, Run(config))
	require.NoError(t, Run(config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	doc, err := api.FindPage("DOCS", "Doc", "page")
	require.NoError(t, err)
	require.NotNil(t, doc)
	return server.Page(doc.ID).Body, server
}

// TestLinkInsideCodeBlockIsLeftAlone is the corruption this fixes. Resolution
// used to scan the text and substitute across the whole file, so a fenced block
// documenting Markdown syntax had its example links rewritten into Confluence
// URLs -- and any code sample sharing text with a real link elsewhere went with
// them.
func TestLinkInsideCodeBlockIsLeftAlone(t *testing.T) {
	body, _ := linkedPair(t, "Real link: [example](./a-other.md)\n\n"+
		"```markdown\n[in a code block](./a-other.md)\n```\n")

	i := strings.Index(body, "CDATA")
	require.GreaterOrEqual(t, i, 0, "expected a code macro in %s", body)
	code := body[i:]

	assert.Contains(t, code, "./a-other.md",
		"the code sample must survive exactly as written")
	assert.NotContains(t, code, "/wiki/",
		"a link inside a code block must not be rewritten")
}

// TestRealLinkIsStillResolved is the control: the fix must not simply stop
// rewriting links.
func TestRealLinkIsStillResolved(t *testing.T) {
	body, _ := linkedPair(t, "Real link: [example](./a-other.md)\n")

	assert.NotContains(t, body, "./a-other.md",
		"a genuine relative link should have been rewritten")
	assert.Contains(t, body, "/wiki/",
		"and should point at the Confluence page")
}

// TestLinkInsideCodeSpanIsLeftAlone: the same for inline code.
func TestLinkInsideCodeSpanIsLeftAlone(t *testing.T) {
	body, _ := linkedPair(t, "Real link: [example](./a-other.md)\n\n"+
		"Inline: `[sample](./a-other.md)`\n")

	i := strings.Index(body, "<code>")
	require.GreaterOrEqual(t, i, 0, "expected a code span in %s", body)
	span := body[i:]
	assert.Contains(t, span, "./a-other.md", "a code span must survive as written")
}

// TestAttachmentLinkInsideCodeBlockIsLeftAlone is the second half of the
// corruption fixed for relative links. Attachment destinations were substituted
// with a ReplaceAll over the whole document, so a page documenting how to refer
// to an attachment had its own example turned into a download URL.
func TestAttachmentLinkInsideCodeBlockIsLeftAlone(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)

	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	png := []byte{
		0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89,
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "logo.png"), png, 0o600))

	file := writeFile(t, dir, "doc.md", "<!-- Space: DOCS -->\n"+
		"<!-- Parent: Parent -->\n"+
		"<!-- Title: Attachment Doc -->\n"+
		"<!-- Attachment: logo.png -->\n\n"+
		"Refer to an attachment like this:\n\n"+
		"```markdown\n![logo](logo.png)\n```\n\n"+
		"And here it is for real: ![logo](logo.png)\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, Features: []string{"mention"}, Output: io.Discard,
	}

	target, err := ProcessFile(file, api, config)
	require.NoError(t, err)
	require.NotNil(t, target)

	body := server.Page(target.ID).Body

	code := body[strings.Index(body, "<![CDATA["):strings.Index(body, "]]>")]
	assert.Contains(t, code, "![logo](logo.png)",
		"the code sample must survive exactly as written")
	assert.NotContains(t, code, "/download/",
		"an attachment link inside a code block must not be rewritten")

	// ...while the real image still points at the upload. A declared
	// attachment renders as a URL reference rather than ri:filename, because
	// its destination is rewritten before the image renderer sees it.
	assert.Contains(t, body, "download/attachments/",
		"the real image should still resolve to the attachment")
}
