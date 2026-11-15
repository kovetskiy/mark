package util

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJoinPatterns(t *testing.T) {
	for _, test := range []struct {
		name     string
		patterns []string
		want     string
	}{
		{"none", nil, ""},
		{"empty ones are none", []string{"", ""}, ""},
		{"one is left as written", []string{"docs/{a,b}/*.md"}, "docs/{a,b}/*.md"},
		{"two are alternatives", []string{"README.md", "docs/**/*.md"}, "{README.md,docs/**/*.md}"},
		{"a comma in a class stays", []string{"a[1,2].md", "c.md"}, "{a[1,2].md,c.md}"},
		{"a leading ./ goes", []string{"./a.md", "./docs/*.md"}, "{a.md,docs/*.md}"},
		{"a repeated one counts once", []string{"a.md", "b.md", "a.md"}, "{a.md,b.md}"},
		{"braces of their own stay", []string{"x/{a,b}.md", "c.md"}, "{x/{a,b}.md,c.md}"},
		{"a literal comma is escaped", []string{"a,b.md", "c.md"}, "{a[,]b.md,c.md}"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := joinPatterns(test.patterns)
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestJoinedPatternsMatchEachFileOnce(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"README.md", "notes,1.md", "n1.md", "docs/a.md", "docs/sub/b.md", "other/c.md"} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, nil, 0o600))
	}
	t.Chdir(dir)

	pattern, err := joinPatterns([]string{
		"README.md",
		"docs/**/*.md",
		"docs/a.md",
		"notes,1.md",
		"{other,nothing}/*.md",
		"n[1,2].md",
	})

	require.NoError(t, err)
	got, err := doublestar.FilepathGlob(pattern)
	require.NoError(t, err)

	names := make([]string, len(got))
	for i, match := range got {
		names[i] = filepath.ToSlash(match)
	}
	assert.ElementsMatch(t, []string{"README.md", "notes,1.md", "n1.md", "docs/a.md", "docs/sub/b.md", "other/c.md"}, names)
}

func TestFilesFlagRepeats(t *testing.T) {
	noDefaultConfig(t)

	got, _, err := dispatch(t, "publish", "-f", "README.md", "--files", "docs/**/*.md")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "{README.md,docs/**/*.md}", *got)

	got, _, err = dispatch(t, "publish", "-f", "docs/{a,b}/*.md")
	require.NoError(t, err)
	assert.Equal(t, "docs/{a,b}/*.md", *got)
}

func TestFilesFromConfigAndEnvironment(t *testing.T) {
	noDefaultConfig(t)
	config := writeConfig(t, t.TempDir(), "mark.toml", "files = [\"README.md\", \"docs/{a,b}/*.md\"]\n")

	resolved := resolveFlags(t, nil, []string{"files"}, "publish", "--config", config)
	assert.Equal(t, "[README.md docs/{a,b}/*.md]", resolved["files"])

	resolved = resolveFlags(t, map[string]string{"MARK_FILES": "one.md\ntwo,{x,y}.md"}, []string{"files"}, "publish")
	assert.Equal(t, "[one.md two,{x,y}.md]", resolved["files"])
}

func TestJoinPatternsRefusesADrive(t *testing.T) {
	if filepath.VolumeName("C:/x") == "" {
		t.Skip("only Windows has drives")
	}
	_, err := joinPatterns([]string{"C:/x/*.md", "y.md"})
	assert.ErrorContains(t, err, "names a drive")
}

func TestJoinPatternsReadsBackslashesAsSeparatorsOnWindows(t *testing.T) {
	if filepath.Separator != '\\' {
		t.Skip("a backslash is an escape here")
	}
	got, err := joinPatterns([]string{"a" + string(filepath.Separator) + "{b,c}.md", "d.md"})
	require.NoError(t, err)
	assert.Equal(t, "{a/{b,c}.md,d.md}", got)
}
