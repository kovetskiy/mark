package mark

import (
	"net/http"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/kovetskiy/mark/v16/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// restrictedDoc is a document under Parent whose headers restrict reading to
// group, with body below them.
func restrictedDoc(group, body string) string {
	return "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Restricted -->\n" +
		"<!-- Restriction: read.group=" + group + " -->\n\n" + body + "\n"
}

// writesAfter reports, of the requests the server took from the index from
// on, the position of the last restriction PUT and of the content PUT.
func writesAfter(server *confluencetest.Server, from int, pageID string) (restriction, content int) {
	restriction, content = -1, -1
	for i, request := range server.Requests()[from:] {
		if request.Method != http.MethodPut {
			continue
		}
		switch {
		case strings.HasSuffix(request.Path, "/restriction"):
			restriction = i
		case strings.HasSuffix(request.Path, "/content/"+pageID):
			content = i
		}
	}
	return restriction, content
}

// TestRestrictionsLoosenAfterTheContent: lifting a restriction waits until
// the content that needed it has been replaced, so a run that fails between
// the two never shows the old content to the people it was kept from.
func TestRestrictionsLoosenAfterTheContent(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", restrictedDoc("hr", "secret"))
	config := trackingConfig(server, file)
	require.NoError(t, Run(config))

	id := mustFind(t, server, "Restricted")
	require.Equal(t, []string{"hr"}, server.Page(id).RestrictedGroups["read"])

	writeFile(t, dir, "doc.md", "---\nrestrictions:\n  read: {}\n---\n"+
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Restricted -->\n\npublic\n")
	config.Features = append(config.Features, "frontmatter")
	from := len(server.Requests())
	require.NoError(t, Run(config))

	restriction, content := writesAfter(server, from, id)
	require.NotEqual(t, -1, restriction, "the restriction is lifted")
	assert.Less(t, content, restriction, "and only after the content is replaced")
	assert.Empty(t, server.Page(id).RestrictedGroups["read"])
}

// TestRestrictionsChangeIsAChange: a document whose only change is to its
// restrictions is previewed as would-update and reported as published, and
// one with none left to make sends nothing.
func TestRestrictionsChangeIsAChange(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", restrictedDoc("hr", "same"))
	config := trackingConfig(server, file)
	config.ChangesOnly = true
	require.NoError(t, Run(config))

	writeFile(t, dir, "doc.md", restrictedDoc("eng", "same"))
	assert.Contains(t, previewOf(t, config, "json"), `"status": "would-update"`)

	assert.Equal(t, report.StatusPublished, runReported(t, config).Status)
	id := mustFind(t, server, "Restricted")
	assert.Equal(t, []string{"eng"}, server.Page(id).RestrictedGroups["read"])

	puts := server.CountRequests(http.MethodPut, "/restriction")
	assert.Equal(t, report.StatusUnchanged, runReported(t, config).Status)
	assert.Equal(t, puts, server.CountRequests(http.MethodPut, "/restriction"), "nothing left to change sends nothing")
}

// TestRestrictionsUnknownUserCreatesNothing: on Cloud, a restricted user that
// resolves to nobody fails a dry run, and fails a real run before it creates
// a page that would then stand empty and unrestricted.
func TestRestrictionsUnknownUserCreatesNothing(t *testing.T) {
	server, _ := docsSpace(t)
	server.SetCurrentUser(confluencetest.User{AccountID: "acct-me", FullName: "Mark Bot"})
	file := writeFile(t, t.TempDir(), "doc.md", "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n"+
		"<!-- Title: Restricted -->\n<!-- Restriction: read.user=Nobody Known -->\n\nbody\n")
	config := trackingConfig(server, file)

	dry := config
	dry.DryRun = true
	require.ErrorContains(t, Run(dry), `"Nobody Known"`)

	require.ErrorContains(t, Run(config), `"Nobody Known"`)
	assert.Zero(t, countPagesTitled(t, server, "Restricted"))
}
