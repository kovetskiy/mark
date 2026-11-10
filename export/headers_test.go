package export

import (
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v17/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMetadata(t *testing.T) {
	for value, want := range map[string]Metadata{
		"":              MetadataComments,
		"comments":      MetadataComments,
		" Frontmatter ": MetadataFrontMatter,
		"none":          MetadataNone,
	} {
		got, err := ParseMetadata(value)
		require.NoError(t, err, value)
		assert.Equal(t, want, got, value)
	}

	for _, value := range []string{"toml", "front-matter"} {
		_, err := ParseMetadata(value)
		assert.ErrorContains(t, err, `unknown export metadata "`+value+`"`)
	}
}

func TestFrontMatterWritesEmojiAsThemselves(t *testing.T) {
	h := pageHeaders{Space: "DOCS", Title: "🚀 Launch", Emoji: "🚀"}
	frontMatter, err := h.frontMatter()
	require.NoError(t, err)
	assert.Equal(t, "---\nspace: DOCS\ntitle: \"🚀 Launch\"\nemoji: \"🚀\"\n---\n", frontMatter)
}

// TestFrontMatterQuotesWhatYAMLWouldMisread: what YAML cannot hold as it is
// is escaped on one line, what it can -- emoji joined into one, flags -- is
// written as it is, and all of it reads back as it was.
func TestFrontMatterQuotesWhatYAMLWouldMisread(t *testing.T) {
	for _, test := range []struct{ value, want string }{
		{"👨\u200d💻", "\"👨\u200d💻\""},
		{"🏴\U000E0067\U000E0062\U000E0073\U000E0063\U000E0074\U000E007F", "\"🏴\U000E0067\U000E0062\U000E0073\U000E0063\U000E0074\U000E007F\""},
		{"a\nb\r\tc", `"a\nb\r\tc"`},
		{"next\u0085line\u2028sep\u2029par", `"next\u0085line\u2028sep\u2029par"`},
		{"bell\x07 bom\ufeff", `"bell\u0007 bom\uFEFF"`},
		{`"quoted" \U0001F680`, `'"quoted" \U0001F680'`},
		{"--> -->", "--> -->"},
		{"yes", `"yes"`},
	} {
		got, err := yamlScalar(test.value)
		require.NoError(t, err, test.value)
		assert.Equal(t, test.want, got, test.value)

		h := pageHeaders{Space: "DOCS", Title: "Page", Labels: []string{"x" + test.value}}
		frontMatter, err := h.frontMatter()
		require.NoError(t, err)
		assert.Equal(t, []string{"x" + test.value}, readMeta(t, frontMatter).Labels, frontMatter)
	}
}

// TestFrontMatterHoldsTheSidebarCommentsCannot: a sidebar with "-->" in it
// stays in the body under comment headers, and goes to the sidebar key in
// front matter.
func TestFrontMatterHoldsTheSidebarCommentsCannot(t *testing.T) {
	storage := `<ac:layout><ac:layout-section ac:type="two_right_sidebar">` +
		`<ac:layout-cell><p>Body</p></ac:layout-cell>` +
		`<ac:layout-cell><ac:structured-macro ac:name="code"><ac:plain-text-body><![CDATA[A-->B]]></ac:plain-text-body></ac:structured-macro></ac:layout-cell>` +
		`</ac:layout-section></ac:layout>`

	doc, err := Convert(storage, Options{})
	require.NoError(t, err)
	assert.Empty(t, doc.Sidebar)
	assert.Contains(t, doc.Markdown, "ac:layout")

	doc, err = Convert(storage, Options{FrontMatter: true})
	require.NoError(t, err)
	assert.Equal(t, "article", doc.Layout)
	assert.Contains(t, doc.Sidebar, "A-->B")
	assert.Equal(t, "Body\n", doc.Markdown)

	h := pageHeaders{Space: "DOCS", Title: "Page", Sidebar: doc.Sidebar}
	frontMatter, err := h.frontMatter()
	require.NoError(t, err)
	assert.Equal(t, doc.Sidebar, readMeta(t, frontMatter).Sidebar)
}

// TestCommentsLeaveInTheBodyASidebarNoHeaderHolds: a sidebar with "--!>",
// which ends a comment as "-->" does, stays in the body under comment
// headers rather than failing the export.
func TestCommentsLeaveInTheBodyASidebarNoHeaderHolds(t *testing.T) {
	storage := `<ac:layout><ac:layout-section ac:type="two_right_sidebar">` +
		`<ac:layout-cell><p>Body</p></ac:layout-cell>` +
		`<ac:layout-cell><ac:structured-macro ac:name="code"><ac:plain-text-body><![CDATA[A--!>B]]></ac:plain-text-body></ac:structured-macro></ac:layout-cell>` +
		`</ac:layout-section></ac:layout>`

	doc, err := Convert(storage, Options{})
	require.NoError(t, err)
	assert.Empty(t, doc.Sidebar)
	assert.Contains(t, doc.Markdown, "ac:layout")

	h := pageHeaders{Space: "DOCS", Title: "Page", Layout: doc.Layout, Sidebar: doc.Sidebar}
	_, err = h.comments()
	require.NoError(t, err)
}

// TestNoMetadataKeepsTheSidebarInTheBody: with no metadata to carry it, an
// article layout's sidebar stays in the body rather than being dropped.
func TestNoMetadataKeepsTheSidebarInTheBody(t *testing.T) {
	storage := `<ac:layout><ac:layout-section ac:type="two_right_sidebar">` +
		`<ac:layout-cell><p>Body</p></ac:layout-cell>` +
		`<ac:layout-cell><p>Aside</p></ac:layout-cell>` +
		`</ac:layout-section></ac:layout>`

	doc, err := Convert(storage, Options{NoMetadata: true})
	require.NoError(t, err)
	assert.Empty(t, doc.Layout)
	assert.Empty(t, doc.Sidebar)
	assert.Contains(t, doc.Markdown, "Aside")
	assert.Equal(t, canonical(t, storage), canonical(t, compile(t, doc.Markdown, filepath.Join(t.TempDir(), "page.md"))),
		"the body publishes back to the layout it came from:\n%s", doc.Markdown)
}

func TestParseOptionsLeavesOKFItsOwnFrontMatter(t *testing.T) {
	format, spelling, err := parseOptions(Config{Format: FormatOKF, Metadata: MetadataFrontMatter})
	require.NoError(t, err, "a configuration file setting front matter for mark's documents does not stop an OKF export")
	assert.Equal(t, FormatOKF, format)
	assert.Equal(t, MetadataFrontMatter, spelling)

	_, _, err = parseOptions(Config{Format: FormatOKF, Metadata: "toml"})
	assert.ErrorContains(t, err, "unknown export metadata")
}

// TestHeadersReadTheSameBothWays: mark reads the front matter as it reads the
// comments, values YAML would take for something other than a string
// included.
func TestHeadersReadTheSameBothWays(t *testing.T) {
	for name, h := range map[string]pageHeaders{
		"page": {
			Space:             "DOCS",
			Parents:           []string{"123", "yes"},
			Folders:           []string{"Team: Platform"},
			Title:             "🚀 Release: 1.0 #notes",
			Labels:            []string{"how-to", "null"},
			Layout:            "plain",
			ContentAppearance: metadata.FullWidthContentAppearance,
			Emoji:             "🚀",
			Attachments:       []string{"files/chart.png", "- dash.png"},
		},
		"blog post with a sidebar": {
			Space:   "DOCS",
			Type:    "blogpost",
			Title:   "'quoted' & \"double\" \\U0001F680 \\\\U0001F680",
			Sidebar: "<h2>Test</h2> <p>\"🚀\" \\U0001F680</p>",
		},
	} {
		t.Run(name, func(t *testing.T) {
			comments, err := h.comments()
			require.NoError(t, err)
			frontMatter, err := h.frontMatter()
			require.NoError(t, err)

			fromComments, fromFrontMatter := readMeta(t, comments), readMeta(t, frontMatter)
			assert.Equal(t, fromComments, fromFrontMatter, "front matter:\n%s\ncomments:\n%s", frontMatter, comments)
			assert.Equal(t, h.Title, fromFrontMatter.Title)
			assert.Equal(t, h.Parents, fromFrontMatter.Parents)
		})
	}
}

// readMeta is the metadata mark reads from header, front matter enabled.
func readMeta(t *testing.T, header string) *metadata.Meta {
	t.Helper()
	meta, _, err := metadata.ExtractMeta([]byte(header+"\nbody\n"), metadata.ExtractOptions{Filename: "page.md", FrontMatter: true})
	require.NoError(t, err, header)
	require.NotNil(t, meta, header)
	return meta
}
