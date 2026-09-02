package mark

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
