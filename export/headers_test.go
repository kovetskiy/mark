package export

import (
	"testing"

	"github.com/kovetskiy/mark/v16/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMetadata(t *testing.T) {
	for value, want := range map[string]string{
		"":              MetadataComments,
		"comments":      MetadataComments,
		"front-matter":  MetadataFrontMatter,
		" Frontmatter ": MetadataFrontMatter,
	} {
		got, err := ParseMetadata(value)
		require.NoError(t, err, value)
		assert.Equal(t, want, got, value)
	}

	_, err := ParseMetadata("toml")
	assert.ErrorContains(t, err, `unknown export metadata "toml"`)
}

func TestFrontMatterWritesEmojiAsThemselves(t *testing.T) {
	h := pageHeaders{Space: "DOCS", Title: "🚀 Launch", Emoji: "🚀"}
	frontMatter, err := h.frontMatter()
	require.NoError(t, err)
	assert.Equal(t, "---\nspace: DOCS\ntitle: \"🚀 Launch\"\nemoji: \"🚀\"\n---\n", frontMatter)
}

// TestFrontMatterKeepsLineBreaks: a value comments cannot hold is still
// written as front matter, on one line, and read back as it was.
func TestFrontMatterKeepsLineBreaks(t *testing.T) {
	sidebar := "<h2>Test</h2>\n<p>\"🚀\" \\U0001F680</p>"
	h := pageHeaders{Space: "DOCS", Title: "Page", Sidebar: sidebar}

	_, err := h.comments()
	require.Error(t, err)

	frontMatter, err := h.frontMatter()
	require.NoError(t, err)
	assert.Contains(t, frontMatter, `sidebar: "<h2>Test</h2>\n<p>\"🚀\" \\U0001F680</p>"`+"\n")

	meta, _, err := metadata.ExtractMeta([]byte(frontMatter+"\nbody\n"), "", false, false, "page.md", nil, false, "", true)
	require.NoError(t, err)
	assert.Equal(t, sidebar, meta.Sidebar)
}

func TestParseOptionsRefusesFrontMatterForOKF(t *testing.T) {
	_, _, err := parseOptions(Config{Format: FormatOKF, Metadata: MetadataFrontMatter})
	assert.ErrorContains(t, err, "front matter of its own")

	format, spelling, err := parseOptions(Config{Format: FormatOKF, Metadata: MetadataComments})
	require.NoError(t, err, "the default spelling is not a choice OKF has to refuse")
	assert.Equal(t, FormatOKF, format)
	assert.Equal(t, MetadataComments, spelling)
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

			read := func(header string) *metadata.Meta {
				meta, _, err := metadata.ExtractMeta([]byte(header+"\nbody\n"), "", false, false, "page.md", nil, false, "", true)
				require.NoError(t, err, header)
				require.NotNil(t, meta, header)
				return meta
			}

			fromComments, fromFrontMatter := read(comments), read(frontMatter)
			assert.Equal(t, fromComments, fromFrontMatter, "front matter:\n%s\ncomments:\n%s", frontMatter, comments)
			assert.Equal(t, h.Title, fromFrontMatter.Title)
			assert.Equal(t, h.Parents, fromFrontMatter.Parents)
		})
	}
}
