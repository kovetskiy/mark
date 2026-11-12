package metadata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kovetskiy/mark/v17/report"
)

// TestFrontMatterSingleValueKeysReadAScalarAsWritten: YAML reads an unquoted
// 2024, 1.0, true or 2024-01-31 as a number, a boolean or a date, but under a
// key that holds text each is the text the author wrote, as it is in a
// comment header.
func TestFrontMatterSingleValueKeysReadAScalarAsWritten(t *testing.T) {
	for _, test := range []struct{ written, want string }{
		{"2024", "2024"},
		{"1.0", "1.0"},
		{"1e3", "1e3"},
		{"0x1F", "0x1F"},
		{"007", "007"},
		{"-5", "-5"},
		{"true", "true"},
		{"2024-01-31", "2024-01-31"},
		{".inf", ".inf"},
		{`"2024"`, "2024"},
		{"'1.0'", "1.0"},
		{"yes", "yes"},
		{"Release 2024", "Release 2024"},
		{"|\n  2024\n", "2024"},
	} {
		document := "---\nspace: DOCS\ntitle: " + test.written + "\n---\n\nBody.\n"

		warnings := report.NewWarnings("doc.md")
		meta, _, err := ExtractMeta([]byte(document), ExtractOptions{FrontMatter: true, Warnings: warnings})
		require.NoError(t, err, test.written)
		assert.Equal(t, test.want, meta.Title, test.written)
		assert.Empty(t, warnings.List(), test.written)
	}
}

// TestFrontMatterEverySingleValueKeyReadsANumber: the keys that hold one text
// value all read a number alike.
func TestFrontMatterEverySingleValueKeyReadsANumber(t *testing.T) {
	document := "---\nspace: 42\ntitle: 2024\ntype: 1\nlayout: 2\nemoji: 2705\nimage-align: 3\n---\n\nBody.\n"

	warnings := report.NewWarnings("doc.md")
	meta, _, err := ExtractMeta([]byte(document), ExtractOptions{FrontMatter: true, Warnings: warnings})
	require.NoError(t, err)
	assert.Equal(t, "42", meta.Space)
	assert.Equal(t, "2024", meta.Title)
	assert.Equal(t, "1", meta.Type)
	assert.Equal(t, "2", meta.Layout)
	assert.Equal(t, "2705", meta.Emoji)
	assert.Equal(t, "3", meta.ImageAlign)
	assert.Empty(t, warnings.List())

	meta, _, err = ExtractMeta([]byte("---\nsidebar: 7\n---\n\nBody.\n"), ExtractOptions{FrontMatter: true})
	require.NoError(t, err)
	assert.Equal(t, "7", meta.Sidebar)
}

// TestFrontMatterSingleValueKeysReadThroughAnchors: a value an alias or a
// merge brings in is read as it was written where it was anchored.
func TestFrontMatterSingleValueKeysReadThroughAnchors(t *testing.T) {
	document := "---\nproperties: {year: &year 1.0, base: &base {space: 42}}\ntitle: *year\n<<: *base\n---\n\nBody.\n"

	warnings := report.NewWarnings("doc.md")
	meta, _, err := ExtractMeta([]byte(document), ExtractOptions{FrontMatter: true, Warnings: warnings})
	require.NoError(t, err)
	assert.Equal(t, "1.0", meta.Title)
	assert.Equal(t, "42", meta.Space)
	assert.Empty(t, warnings.List())
}

// TestFrontMatterSingleValueKeysLeftEmpty: a key with no value, or null, sets
// nothing and is not worth a warning.
func TestFrontMatterSingleValueKeysLeftEmpty(t *testing.T) {
	for _, written := range []string{"", "~", "null", `""`} {
		document := "---\nspace: DOCS\ntitle: " + written + "\n---\n\nBody.\n"

		warnings := report.NewWarnings("doc.md")
		meta, _, err := ExtractMeta([]byte(document), ExtractOptions{FrontMatter: true, Warnings: warnings})
		require.NoError(t, err, written)
		assert.Empty(t, meta.Title, written)
		assert.Empty(t, warnings.List(), written)
	}
}

// TestFrontMatterSingleValueKeysWarnAboutWhatTheyDrop: a list or a mapping is
// not one value, and is reported against its key as a list key reports what
// it cannot hold.
func TestFrontMatterSingleValueKeysWarnAboutWhatTheyDrop(t *testing.T) {
	for _, written := range []string{"[Guide]", "{name: Guide}"} {
		document := "---\nspace: DOCS\ntitle: " + written + "\n---\n\nBody.\n"

		warnings := report.NewWarnings("doc.md")
		meta, _, err := ExtractMeta([]byte(document), ExtractOptions{FrontMatter: true, Warnings: warnings})
		require.NoError(t, err, written)
		assert.Empty(t, meta.Title, written)

		list := warnings.List()
		require.Len(t, list, 1, written)
		assert.Equal(t, report.WarningIgnoredValue, list[0].Kind)
		assert.Equal(t, 3, list[0].Line)
		assert.Contains(t, list[0].Message, `front matter key "title" takes a single value`)
	}
}
