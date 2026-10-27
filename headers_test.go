package mark

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/kovetskiy/mark/v16/manifest"
	"github.com/kovetskiy/mark/v16/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPageHeaderIsPublishedAboveTheDocument(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Generated -->

Body text.
`)
	config := publishConfig(server.URL, file)
	config.PageHeader = writeFile(t, dir, "header.html", `<p>Generated: {{ .Title | xmlesc }}</p>`)

	require.NoError(t, Run(context.Background(), config))

	body := bodyOfPageTitled(t, server, "Generated")
	assert.Contains(t, body, "<p>Generated: Generated</p>")
	assert.Less(t, bytes.Index([]byte(body), []byte("Generated:")), bytes.Index([]byte(body), []byte("Body text.")))
}

// TestPageHeaderSitsInsideTheArticleLayout: outside the layout, Confluence
// would show the header as a section of its own beside the sidebar.
func TestPageHeaderSitsInsideTheArticleLayout(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Article -->
<!-- Layout: article -->

Body text.
`)
	config := publishConfig(server.URL, file)
	config.PageHeader = writeFile(t, dir, "header.html", `<p>Header</p>`)

	require.NoError(t, Run(context.Background(), config))

	assert.Contains(t, bodyOfPageTitled(t, server, "Article"), "<ac:layout-cell><p>Header</p>")
}

func TestPageHeaderIsInCompileOnlyOutput(t *testing.T) {
	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", `<!-- Space: DOCS -->
<!-- Title: Compiled -->

Body text.
`)

	var output bytes.Buffer
	require.NoError(t, Run(context.Background(), Config{
		Files: file, CompileOnly: true,
		Features:   []string{"mention"},
		PageHeader: writeFile(t, dir, "header.md", "**Generated** from {{ .Space }}\n"),
		Output:     &output,
	}))

	assert.Contains(t, output.String(), "<p><strong>Generated</strong> from DOCS</p>")
}

// TestPageHeaderNamesThePageInPageIDMode: --page-id discards the file's
// metadata, so the title has to come from the page being updated.
func TestPageHeaderNamesThePageInPageIDMode(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	existing := server.AddPage("DOCS", "Existing", "page", "")
	file := writeFile(t, dir, "doc.md", "Just a body.\n")

	config := publishConfig(server.URL, file)
	config.PageID = existing.ID
	config.Space = "DOCS"
	config.PageHeader = writeFile(t, dir, "header.html", `<p>{{ .Title | xmlesc }} in {{ .Space | xmlesc }}</p>`)

	require.NoError(t, Run(context.Background(), config))

	assert.Contains(t, bodyOfPageTitled(t, server, "Existing"), "<p>Existing in DOCS</p>")
}

// TestPageHeaderNamesThePageInPageIDDryRun: the dry run already looks the page
// up, so the header it prints matches the one a real run would publish.
func TestPageHeaderNamesThePageInPageIDDryRun(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	existing := server.AddPage("DOCS", "Existing", "page", "")
	file := writeFile(t, dir, "doc.md", "Just a body.\n")

	var output bytes.Buffer

	config := publishConfig(server.URL, file)
	config.PageID = existing.ID
	config.Space = "DOCS"
	config.DryRun = true
	config.Output = &output
	config.PageHeader = writeFile(t, dir, "header.html", `<p>{{ .Title | xmlesc }} in {{ .Space | xmlesc }}</p>`)

	require.NoError(t, Run(context.Background(), config))

	assert.Contains(t, output.String(), "<p>Existing in DOCS</p>")
}

// TestPageHeaderImageThatClashesWithTheDocumentsIsRefused: one filename is one
// attachment on the page, so the second upload would replace the first.
func TestPageHeaderImageThatClashesWithTheDocumentsIsRefused(t *testing.T) {
	server, _ := docsSpace(t)
	docDir := t.TempDir()
	headerDir := t.TempDir()

	writeFile(t, docDir, "logo.png", "document logo")
	writeFile(t, headerDir, "logo.png", "header logo")

	file := writeFile(t, docDir, "doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Clash -->

![logo](logo.png)
`)
	config := publishConfig(server.URL, file)
	config.PageHeader = writeFile(t, headerDir, "header.md", "![logo](logo.png)\n")

	err := Run(context.Background(), config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "page header attachment")
}

func TestPageHeaderImageIdenticalToTheDocumentsIsAttachedOnce(t *testing.T) {
	server, _ := docsSpace(t)
	docDir := t.TempDir()
	headerDir := t.TempDir()

	writeFile(t, docDir, "logo.png", "same logo")
	writeFile(t, headerDir, "logo.png", "same logo")

	file := writeFile(t, docDir, "doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Same -->

![logo](logo.png)
`)
	config := publishConfig(server.URL, file)
	config.PageHeader = writeFile(t, headerDir, "header.md", "![logo](logo.png)\n")

	require.NoError(t, Run(context.Background(), config))
}

// TestPageHeaderImageThatClashesWithADeclaredAttachmentUploadsNothing: the
// clash is known before the first upload, so the document's own file is not
// sent for a page the run is about to fail on.
func TestPageHeaderImageThatClashesWithADeclaredAttachmentUploadsNothing(t *testing.T) {
	server, _ := docsSpace(t)
	docDir := t.TempDir()
	headerDir := t.TempDir()

	writeFile(t, docDir, "logo.png", "document logo")
	writeFile(t, headerDir, "logo.png", "header logo")

	file := writeFile(t, docDir, "doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Declared Clash -->
<!-- Attachment: logo.png -->

Text.
`)
	config := publishConfig(server.URL, file)
	config.PageHeader = writeFile(t, headerDir, "header.md", "![logo](logo.png)\n")

	err := Run(context.Background(), config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "page header attachment")
	assert.Equal(t, 0, server.CountRequests("POST", "/child/attachment"))
}

// TestPageHeaderAttachmentClashFailsADryRun: a dry run that reports success
// where the real run refuses the page is not a dry run of it. Both a declared
// attachment and one the document only references are covered.
func TestPageHeaderAttachmentClashFailsADryRun(t *testing.T) {
	for _, mode := range []string{"dry-run", "compile-only"} {
		t.Run(mode, func(t *testing.T) {
			server, _ := docsSpace(t)
			docDir := t.TempDir()
			headerDir := t.TempDir()

			writeFile(t, docDir, "logo.png", "document logo")
			writeFile(t, docDir, "chart.png", "document chart")
			writeFile(t, headerDir, "logo.png", "header logo")
			writeFile(t, headerDir, "chart.png", "header chart")

			file := writeFile(t, docDir, "doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Dry Clash -->
<!-- Attachment: logo.png -->

![chart](chart.png)
`)
			for _, header := range []string{"![logo](logo.png)\n", "![chart](chart.png)\n"} {
				config := publishConfig(server.URL, file)
				config.DryRun = mode == "dry-run"
				config.CompileOnly = mode == "compile-only"
				config.Output = &bytes.Buffer{}
				config.PageHeader = writeFile(t, headerDir, "header.md", header)

				err := Run(context.Background(), config)
				require.Error(t, err, header)
				assert.Contains(t, err.Error(), "page header attachment")
			}
		})
	}
}

// propertiesFixture publishes one document and returns the server, the page id
// and the config used, so a test can republish.
func propertiesFixture(t *testing.T, doc string, config func(*Config)) (*confluencetest.Server, string, Config) {
	t.Helper()

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "doc.md", doc)

	cfg := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files:    filepath.Join(dir, "doc.md"),
		Features: []string{"mention"},
		Output:   io.Discard,
	}
	if config != nil {
		config(&cfg)
	}
	require.NoError(t, Run(context.Background(), cfg))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	page, err := api.FindPage(context.Background(), "DOCS", "Doc", "page")
	require.NoError(t, err)
	require.NotNil(t, page)

	return server, page.ID, cfg
}

const propertyHeaders = "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n"

// TestPropertyHeaderSetsAContentProperty covers the singular, repeatable header
// form, which matches Parent, Label and Attachment.
func TestPropertyHeaderSetsAContentProperty(t *testing.T) {
	server, id, _ := propertiesFixture(t,
		propertyHeaders+
			"<!-- Property: team=platform -->\n"+
			"<!-- Property: reviewed=2026-08 -->\n\nBody.\n", nil)

	require.NotNil(t, server.SpaceProperty(id, "team"))
	assert.JSONEq(t, `"platform"`, string(server.SpaceProperty(id, "team").Value))

	require.NotNil(t, server.SpaceProperty(id, "reviewed"))
	assert.JSONEq(t, `"2026-08"`, string(server.SpaceProperty(id, "reviewed").Value))
}

// TestPropertiesFrontMatterSetsProperties covers the plural front matter form,
// which matches parents, labels and attachments -- and which, unlike the
// header, can carry a value that is not a string.
func TestPropertiesFrontMatterSetsProperties(t *testing.T) {
	server, id, _ := propertiesFixture(t,
		"---\n"+
			"title: Doc\n"+
			"space: DOCS\n"+
			"parents:\n  - Parent\n"+
			"properties:\n"+
			"  team: platform\n"+
			"  reviewers: 3\n"+
			"  tags:\n    - one\n    - two\n"+
			"---\n\nBody.\n",
		func(c *Config) { c.Features = []string{"mention", "frontmatter"} })

	require.NotNil(t, server.SpaceProperty(id, "team"))
	assert.JSONEq(t, `"platform"`, string(server.SpaceProperty(id, "team").Value))

	require.NotNil(t, server.SpaceProperty(id, "reviewers"))
	assert.JSONEq(t, `3`, string(server.SpaceProperty(id, "reviewers").Value))

	require.NotNil(t, server.SpaceProperty(id, "tags"))
	assert.JSONEq(t, `["one","two"]`, string(server.SpaceProperty(id, "tags").Value))
}

func TestGlobalPropertiesApplyToEveryPage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "properties.yaml")
	require.NoError(t, os.WriteFile(path, []byte("owner: docs-team\nsource: git\n"), 0o600))

	server, id, _ := propertiesFixture(t, propertyHeaders+"\nBody.\n",
		func(c *Config) { c.GlobalProperties = path })

	require.NotNil(t, server.SpaceProperty(id, "owner"))
	assert.JSONEq(t, `"docs-team"`, string(server.SpaceProperty(id, "owner").Value))
	require.NotNil(t, server.SpaceProperty(id, "source"))
}

// TestDocumentPropertyWinsOverGlobal: a document naming a property is being
// specific about its own page, which is the more particular statement.
func TestDocumentPropertyWinsOverGlobal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "properties.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"owner": "docs-team"}`), 0o600))

	server, id, _ := propertiesFixture(t,
		propertyHeaders+"<!-- Property: owner=platform-team -->\n\nBody.\n",
		func(c *Config) { c.GlobalProperties = path })

	assert.JSONEq(t, `"platform-team"`, string(server.SpaceProperty(id, "owner").Value))
}

// TestPropertyUnchangedIsNotRewritten: a property holds a version Confluence
// increments on every write, so rewriting an unchanged value would fill its
// history the way republishing an unchanged page fills the page's.
func TestPropertyUnchangedIsNotRewritten(t *testing.T) {
	server, id, config := propertiesFixture(t,
		propertyHeaders+"<!-- Property: team=platform -->\n\nBody.\n", nil)

	first := server.SpaceProperty(id, "team").Version
	require.NoError(t, Run(context.Background(), config))

	assert.Equal(t, first, server.SpaceProperty(id, "team").Version,
		"an unchanged property must not be written again")
}

func TestPropertyChangedIsRewritten(t *testing.T) {
	server, id, config := propertiesFixture(t,
		propertyHeaders+"<!-- Property: team=platform -->\n\nBody.\n", nil)

	first := server.SpaceProperty(id, "team").Version

	writeFile(t, filepath.Dir(config.Files), "doc.md",
		propertyHeaders+"<!-- Property: team=infrastructure -->\n\nBody.\n")
	require.NoError(t, Run(context.Background(), config))

	assert.Greater(t, server.SpaceProperty(id, "team").Version, first)
	assert.JSONEq(t, `"infrastructure"`, string(server.SpaceProperty(id, "team").Value))
}

func TestPropertyHeaderMustBeKeyValue(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Title: Doc -->\n<!-- Property: nonsense -->\n\nBody.\n")

	err := Run(context.Background(), Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "doc.md"), Output: io.Discard,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key=value")
}

func TestGlobalPropertiesFileMustParse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "properties.yaml")
	require.NoError(t, os.WriteFile(path, []byte("owner: [unclosed\n"), 0o600))
	writeFile(t, dir, "doc.md", "<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\nBody.\n")

	err := Run(context.Background(), Config{
		BaseURL: "http://127.0.0.1:1", Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), GlobalProperties: path, Output: io.Discard,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "properties file")
}

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
	require.NoError(t, Run(context.Background(), config))

	id := mustFind(t, server, "Restricted")
	require.Equal(t, []string{"hr"}, server.Page(id).RestrictedGroups["read"])

	writeFile(t, dir, "doc.md", "---\nrestrictions:\n  read: {}\n---\n"+
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Restricted -->\n\npublic\n")
	config.Features = append(config.Features, "frontmatter")
	from := len(server.Requests())
	require.NoError(t, Run(context.Background(), config))

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
	require.NoError(t, Run(context.Background(), config))

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
	require.ErrorContains(t, Run(context.Background(), dry), `"Nobody Known"`)

	require.ErrorContains(t, Run(context.Background(), config), `"Nobody Known"`)
	assert.Zero(t, countPagesTitled(t, server, "Restricted"))
}

func synchronizedServer(t *testing.T) *confluencetest.Server {
	t.Helper()

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	return server
}

// TestSynchronizedFalseHeaderSkipsTheFile covers the header form.
func TestSynchronizedFalseHeaderSkipsTheFile(t *testing.T) {
	server := synchronizedServer(t)

	dir := t.TempDir()
	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Draft -->\n"+
			"<!-- Synchronized: false -->\n\nNot ready.\n")

	require.NoError(t, Run(context.Background(), Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"}, Output: io.Discard,
	}))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	page, err := api.FindPage(context.Background(), "DOCS", "Draft", "page")
	require.NoError(t, err)
	assert.Nil(t, page, "an unsynchronized document must not be published")
}

// TestSynchronizedFalseFrontMatterSkipsTheFile covers the front matter form.
func TestSynchronizedFalseFrontMatterSkipsTheFile(t *testing.T) {
	server := synchronizedServer(t)

	dir := t.TempDir()
	writeFile(t, dir, "doc.md",
		"---\ntitle: Draft\nspace: DOCS\nparents:\n  - Parent\nsynchronized: false\n---\n\nNot ready.\n")

	require.NoError(t, Run(context.Background(), Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention", "frontmatter"},
		Output: io.Discard,
	}))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	page, err := api.FindPage(context.Background(), "DOCS", "Draft", "page")
	require.NoError(t, err)
	assert.Nil(t, page, "an unsynchronized document must not be published")
}

// TestSynchronizedTrueAndAbsentBothPublish: opting out has to be deliberate,
// and saying nothing is the ordinary case.
func TestSynchronizedTrueAndAbsentBothPublish(t *testing.T) {
	for name, header := range map[string]string{
		"stated true": "<!-- Synchronized: true -->\n",
		"not stated":  "",
	} {
		t.Run(name, func(t *testing.T) {
			server := synchronizedServer(t)

			dir := t.TempDir()
			writeFile(t, dir, "doc.md",
				"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n"+
					header+"\nBody.\n")

			require.NoError(t, Run(context.Background(), Config{
				BaseURL: server.URL, Username: "user", Password: "token",
				Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
				Output: io.Discard,
			}))

			api := confluence.NewAPI(server.URL, "user", "token", false)
			page, err := api.FindPage(context.Background(), "DOCS", "Doc", "page")
			require.NoError(t, err)
			assert.NotNil(t, page)
		})
	}
}

// TestSynchronizedFalseKeepsTheMapping is the case that would hurt. Opting a
// document out must not read as the file being gone: mark drops the page from
// the manifest, and synchronising it again later would no longer know which
// page was already its own -- publishing a second copy beside it.
//
// A second document in the same space is what makes this bite. Skipping every
// file in a space leaves the manifest for that space unread, so nothing is
// judged missing; it takes one page that does publish to load the mapping and
// expose the skipped one as unaccounted for.
func TestSynchronizedFalseKeepsTheMapping(t *testing.T) {
	server := synchronizedServer(t)

	dir := t.TempDir()
	draft := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Draft -->\n"
	writeFile(t, dir, "draft.md", draft+"\nBody.\n")
	writeFile(t, dir, "other.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Other -->\n\nBody.\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		TrackPages: true, Output: io.Discard,
	}
	require.NoError(t, Run(context.Background(), config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	published, err := api.FindPage(context.Background(), "DOCS", "Draft", "page")
	require.NoError(t, err)
	require.NotNil(t, published)

	// Now the draft opts out, while its neighbour keeps publishing.
	writeFile(t, dir, "draft.md", draft+"<!-- Synchronized: false -->\n\nBody.\n")
	require.NoError(t, Run(context.Background(), config))

	// The page is never deleted either way; a forgotten mapping is what causes
	// a duplicate the next time it publishes.
	require.NotNil(t, server.Page(published.ID))

	entry, ok, err := manifest.NewStore(api).Lookup(context.Background(), "DOCS", filepath.Join(dir, "draft.md"))
	require.NoError(t, err)
	require.True(t, ok, "the page mapping must survive a document opting out")
	assert.Equal(t, published.ID, entry.PageID)
}

func TestSynchronizedRejectsANonBoolean(t *testing.T) {
	server := synchronizedServer(t)

	dir := t.TempDir()
	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Title: Doc -->\n<!-- Synchronized: maybe -->\n\nBody.\n")

	err := Run(context.Background(), Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Output: io.Discard,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "true or false")
}

// folderOrderedDocument is a document that declares both a folder and a
// position among its siblings -- the combination that used to get neither.
func folderOrderedDocument(title string, order string) string {
	return `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Folder: Manuals -->
<!-- Title: ` + title + ` -->
<!-- Order: ` + order + ` -->

# ` + title + `

Some content.
`
}

// titlesInOrder names the pages Confluence lists under a parent, in the order
// the tree shows them.
func titlesInOrder(server *confluencetest.Server, parentID string) []string {
	var titles []string
	for _, id := range server.ChildOrder(parentID) {
		if page := server.Page(id); page != nil {
			titles = append(titles, page.Title)
		}
	}

	return titles
}

// TestOrderIsAppliedUnderAFolder pins the class of bug where a placement is
// dropped for want of a parent id. A folder is not a page, so it never appears
// among a page's expanded ancestors -- every folder-parented page therefore
// reported no parent, and OrderChildren discarded its position without a word.
func TestOrderIsAppliedUnderAFolder(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	writeFile(t, dir, "a.md", folderOrderedDocument("Second", "2"))
	writeFile(t, dir, "b.md", folderOrderedDocument("First", "1"))

	require.NoError(t, Run(context.Background(), trackingConfig(server, filepath.Join(dir, "*.md"))))

	folders := server.Folders()
	require.Len(t, folders, 1, "the declared folder should have been created once")

	assert.Equal(t, []string{"First", "Second"}, titlesInOrder(server, folders[0].ID),
		"a page under a folder must still be placed where its Order header asks")
}

// TestOrderIsAppliedUnderAPage guards the ordinary path, which resolves the
// parent from the page's own ancestors, against the folder fallback added
// beside it.
func TestOrderIsAppliedUnderAPage(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	document := func(title, order string) string {
		return `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: ` + title + ` -->
<!-- Order: ` + order + ` -->

# ` + title + `

Some content.
`
	}

	writeFile(t, dir, "a.md", document("Second", "2"))
	writeFile(t, dir, "b.md", document("First", "1"))

	require.NoError(t, Run(context.Background(), trackingConfig(server, filepath.Join(dir, "*.md"))))

	parent, err := api.FindPage(context.Background(), "DOCS", "Parent", "page")
	require.NoError(t, err)
	require.NotNil(t, parent)

	assert.Equal(t, []string{"First", "Second"}, titlesInOrder(server, parent.ID))
}

// TestOrderIsAppliedOnDataCenter: without the content move endpoint the
// Order header is carried out through movepage.action.
func TestOrderIsAppliedOnDataCenter(t *testing.T) {
	server, api := docsSpace(t)
	server.SetDataCenter()
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.Contains(r.URL.Path, "/move/") {
			return http.StatusNotFound, `{"message":"no such endpoint"}`, true
		}
		return 0, "", false
	})
	dir := t.TempDir()

	document := func(title, order string) string {
		return `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: ` + title + ` -->
<!-- Order: ` + order + ` -->

# ` + title + `

Some content.
`
	}

	writeFile(t, dir, "a.md", document("Second", "2"))
	writeFile(t, dir, "b.md", document("First", "1"))

	require.NoError(t, Run(context.Background(), trackingConfig(server, filepath.Join(dir, "*.md"))))

	parent, err := api.FindPage(context.Background(), "DOCS", "Parent", "page")
	require.NoError(t, err)
	require.NotNil(t, parent)

	assert.Equal(t, []string{"First", "Second"}, titlesInOrder(server, parent.ID))
	assert.Positive(t, server.CountRequests(http.MethodPost, "/pages/movepage.action"))
}

// orderedDoc is a document asking for a position among its siblings.
func orderedDoc(title string, order int) string {
	return fmt.Sprintf(`<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: %s -->
<!-- Order: %d -->

Body.
`, title, order)
}

// orderSpace builds a space and returns the parent the documents hang from.
func orderSpace(t *testing.T) (*confluencetest.Server, *confluencetest.Page) {
	t.Helper()
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	return server, server.AddPage("DOCS", "Parent", "page", home.ID)
}

func titlesOf(t *testing.T, server *confluencetest.Server, parentID string) []string {
	t.Helper()
	var titles []string
	for _, id := range server.ChildOrder(parentID) {
		titles = append(titles, server.Page(id).Title)
	}
	return titles
}

// TestOrderHeaderArrangesSiblings is the feature: documents are published in
// whatever order the glob produced them, and end up in the order they asked
// for.
func TestOrderHeaderArrangesSiblings(t *testing.T) {
	server, parent := orderSpace(t)
	dir := t.TempDir()

	// Written so the filenames sort opposite to the requested order, which is
	// what shows the ordering is doing the work.
	writeFile(t, dir, "a.md", orderedDoc("Third", 30))
	writeFile(t, dir, "b.md", orderedDoc("First", 10))
	writeFile(t, dir, "c.md", orderedDoc("Second", 20))

	require.NoError(t, Run(context.Background(), Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"}, Output: io.Discard,
	}))

	assert.Equal(t, []string{"First", "Second", "Third"}, titlesOf(t, server, parent.ID))
}

// TestOrderIsIdempotent is what decides whether this can run in CI. Ordering
// happens on every publish, so a run that changes nothing must move nothing --
// otherwise every run fills page histories with churn.
func TestOrderIsIdempotent(t *testing.T) {
	server, parent := orderSpace(t)
	dir := t.TempDir()

	writeFile(t, dir, "a.md", orderedDoc("Third", 30))
	writeFile(t, dir, "b.md", orderedDoc("First", 10))
	writeFile(t, dir, "c.md", orderedDoc("Second", 20))
	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"}, Output: io.Discard,
	}
	require.NoError(t, Run(context.Background(), config))
	require.Equal(t, []string{"First", "Second", "Third"}, titlesOf(t, server, parent.ID))

	server.ResetRequests()
	require.NoError(t, Run(context.Background(), config))

	assert.Equal(t, 0, countMoves(server), "a second run must not move anything")
	assert.Equal(t, []string{"First", "Second", "Third"}, titlesOf(t, server, parent.ID))
}

func countMoves(server *confluencetest.Server) int {
	n := 0
	for _, r := range server.Requests() {
		if strings.Contains(r.Path, "/move/") {
			n++
		}
	}
	return n
}

// TestOrderMovesOnlyWhatMustMove: one page changing position must not
// reshuffle the rest.
func TestOrderMovesOnlyWhatMustMove(t *testing.T) {
	server, parent := orderSpace(t)
	dir := t.TempDir()

	for i, title := range []string{"One", "Two", "Three", "Four"} {
		writeFile(t, dir, fmt.Sprintf("%d.md", i), orderedDoc(title, (i+1)*10))
	}
	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"}, Output: io.Discard,
	}
	require.NoError(t, Run(context.Background(), config))
	require.Equal(t, []string{"One", "Two", "Three", "Four"}, titlesOf(t, server, parent.ID))

	// "Four" is asked to lead. Everything else keeps its relative order, so
	// only one page needs to move.
	writeFile(t, dir, "3.md", orderedDoc("Four", 5))
	server.ResetRequests()
	require.NoError(t, Run(context.Background(), config))

	assert.Equal(t, []string{"Four", "One", "Two", "Three"}, titlesOf(t, server, parent.ID))
	assert.Equal(t, 1, countMoves(server), "only the page that changed position should move")
}

// TestOrderLeavesUnorderedPagesAlone: a document that says nothing about order
// must not be dragged about by the ones that do.
func TestOrderLeavesUnorderedPagesAlone(t *testing.T) {
	server, parent := orderSpace(t)
	dir := t.TempDir()

	writeFile(t, dir, "plain.md", markdownWithTitle("Plain"))
	writeFile(t, dir, "a.md", orderedDoc("Second", 20))
	writeFile(t, dir, "b.md", orderedDoc("First", 10))

	require.NoError(t, Run(context.Background(), Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"}, Output: io.Discard,
	}))

	order := titlesOf(t, server, parent.ID)
	assert.Contains(t, order, "Plain")
	first, second := indexOf(order, "First"), indexOf(order, "Second")
	assert.Less(t, first, second, "the ordered pages should be in the order they asked for")
}

func indexOf(values []string, want string) int {
	for i, v := range values {
		if v == want {
			return i
		}
	}
	return -1
}

// TestOrderIsNotAppliedOnDryRun: a dry run reports and rearranges nothing.
func TestOrderIsNotAppliedOnDryRun(t *testing.T) {
	server, parent := orderSpace(t)
	dir := t.TempDir()

	writeFile(t, dir, "a.md", orderedDoc("Third", 30))
	writeFile(t, dir, "b.md", orderedDoc("First", 10))
	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"}, Output: io.Discard,
	}
	require.NoError(t, Run(context.Background(), config))

	// Flip the requested order, then dry run.
	writeFile(t, dir, "a.md", orderedDoc("Third", 5))
	before := titlesOf(t, server, parent.ID)
	config.DryRun = true
	require.NoError(t, Run(context.Background(), config))

	assert.Equal(t, before, titlesOf(t, server, parent.ID),
		"a dry run must leave the order exactly as it found it")
}
