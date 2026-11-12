package export

import (
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

func assertFits(t *testing.T, name string) {
	t.Helper()
	assert.LessOrEqual(t, len(name), nameMax, "%q", name)
	assert.True(t, utf8.ValidString(name), "%q", name)
}

// An attachment's name that a filesystem would refuse is cut before its
// extension, on a character's boundary; one that fits is left whole.
func TestLocalNameIsCapped(t *testing.T) {
	names := newLocalNames()
	assert.Equal(t, "chart.png", names.of("chart.png"))

	for _, filename := range []string{
		strings.Repeat("a", 300) + ".png",
		strings.Repeat("文", 100) + ".png",
		"x" + strings.Repeat("文", 100) + ".png",
		strings.Repeat("é", 200),
	} {
		name := names.of(filename)
		assertFits(t, name)
		assert.Equal(t, filepath.Ext(filename), filepath.Ext(name), "%q keeps its extension", name)
		assert.Greater(t, len(name), nameMax-utf8.UTFMax, "%q is cut no shorter than it has to be", name)
		assert.Equal(t, name, names.of(filename), "an attachment has one name")
	}
}

// Two long names that only differ past the cut keep files of their own, and
// each keeps its file whichever is named first.
func TestCappedNamesStayApart(t *testing.T) {
	first := strings.Repeat("a", 260) + "-one.png"
	second := strings.Repeat("a", 260) + "-two.png"

	names := newLocalNames()
	a, b := names.of(first), names.of(second)
	assertFits(t, a)
	assertFits(t, b)
	assert.NotEqual(t, a, b)

	reversed := newLocalNames()
	assert.Equal(t, b, reversed.of(second))
	assert.Equal(t, a, reversed.of(first))

	// Names that differ only in case share a file on macOS and Windows.
	upper := names.of(strings.ToUpper(first))
	assertFits(t, upper)
	assert.NotEqual(t, strings.ToLower(a), strings.ToLower(upper))
}

// A name that fits until a number tells it apart from another is cut to make
// room for the number.
func TestNumberedNameIsCapped(t *testing.T) {
	stem := strings.Repeat("a", nameMax-len(".png"))
	names := newLocalNames()
	assert.Equal(t, stem+".png", names.of(stem+".png"))

	numbered := names.of(strings.ToUpper(stem) + ".png")
	assertFits(t, numbered)
	assert.True(t, strings.HasSuffix(numbered, "-2.png"), "%q", numbered)
}

// An attachment ending in .md is written as .txt even when its name has to be
// cut.
func TestMarkdownNameIsCapped(t *testing.T) {
	names := newLocalNames()

	name := names.of(strings.Repeat("文", 100) + ".md")
	assertFits(t, name)
	assert.True(t, strings.HasSuffix(name, ".txt"), "%q", name)
}

// An extension too long to leave room for the stem is cut with it.
func TestAbsurdExtensionIsCapped(t *testing.T) {
	names := newLocalNames()
	name := names.of("a." + strings.Repeat("b", 300))
	assertFits(t, name)
	assert.True(t, strings.HasPrefix(name, "a.b"), "%q", name)
}

// An attachment is never written under a name git reads as its own in a
// working tree, in any case, or with the trailing dots and spaces Windows
// drops or the invisible characters HFS+ ignores; other names that start with
// a dot keep theirs.
func TestLocalNamesKeepClearOfGit(t *testing.T) {
	for filename, want := range map[string]string{
		".git":           ".git-2",
		".GIT":           ".GIT-2",
		".Git.":          ".Git-2.",
		".git ":          ".git -2",
		".gitattributes": ".gitattributes-2",
		".gitmodules":    ".gitmodules-2",
		".gitignore":     ".gitignore-2",
		".gitmoduleſ":    ".gitmoduleſ-2",
		".gi\u200ct":     ".gi\u200ct-2",
		".git\ufeff":     ".git\ufeff-2",
		".env":           ".env",
		".github":        ".github",
		"git":            "git",
		"HEAD":           "HEAD-2",
		"head":           "head-2",
		"HEAD.txt":       "HEAD.txt",
		"config":         "config",
	} {
		assert.Equal(t, want, newLocalNames().of(filename), "%q", filename)
	}
}

// A name publishing would read as something other than the one attachment is
// written under one it reads as that file; other names are kept.
func TestLocalName(t *testing.T) {
	for filename, want := range map[string]string{
		"chart.png":        "chart.png",
		"a/b.png":          "a_b.png",
		`a\b.png`:          "a_b.png",
		"..":               "_..",
		"[n]otes.txt":      "_n_otes.txt",
		"report*.txt":      "report_.txt",
		"a?.txt":           "a_.txt",
		"{a,b}.txt":        "_a,b_.txt",
		"report (2024).p!": "report (2024).p!",
		"notes.md":         "notes.md.txt",
		"Notes.MD":         "Notes.MD.txt",
		"notes.md.txt":     "notes.md.txt",
		"notes.mdx":        "notes.mdx",
		".md":              ".md.txt",
		"a/.md":            "a_.md.txt",
		"notes.md.":        "notes.md..txt",
		"notes.MD . ":      "notes.MD . .txt",
		"notes.md.x":       "notes.md.x",
	} {
		assert.Equal(t, want, localName(filename), "%q", filename)
	}

	// Two names written alike still get a file each.
	names := newLocalNames()
	assert.Equal(t, "notes.md.txt", names.of("notes.md"))
	assert.Equal(t, "notes.md-2.txt", names.of("notes.md.txt"))
	assert.Equal(t, "a_.txt", names.of("a?.txt"))
	assert.Equal(t, "a_-2.txt", names.of("a*.txt"))
}

// Names a filesystem takes as one file are numbered; names that only full
// case folding would equate are not.
func TestLocalNamesOfOneFile(t *testing.T) {
	names := newLocalNames()
	assert.Equal(t, "café.png", names.of("café.png"))
	assert.Equal(t, "cafe\u0301-2.png", names.of("cafe\u0301.png"), "one file on macOS")
	assert.Equal(t, "Strasse.png", names.of("Strasse.png"))
	assert.Equal(t, "Straße.png", names.of("Straße.png"), "two files everywhere")
}
