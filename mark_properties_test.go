package mark

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	require.NoError(t, Run(cfg))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	page, err := api.FindPage("DOCS", "Doc", "page")
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
	require.NoError(t, Run(config))

	assert.Equal(t, first, server.SpaceProperty(id, "team").Version,
		"an unchanged property must not be written again")
}

func TestPropertyChangedIsRewritten(t *testing.T) {
	server, id, config := propertiesFixture(t,
		propertyHeaders+"<!-- Property: team=platform -->\n\nBody.\n", nil)

	first := server.SpaceProperty(id, "team").Version

	writeFile(t, filepath.Dir(config.Files), "doc.md",
		propertyHeaders+"<!-- Property: team=infrastructure -->\n\nBody.\n")
	require.NoError(t, Run(config))

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

	err := Run(Config{
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

	err := Run(Config{
		BaseURL: "http://127.0.0.1:1", Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), GlobalProperties: path, Output: io.Discard,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "properties file")
}
