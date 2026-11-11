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

// An OKF attachment ending in .md is written as .txt even when its name has
// to be cut.
func TestOKFNameIsCapped(t *testing.T) {
	names := newLocalNames()
	names.okf = true

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
// drops; other names that start with a dot keep theirs.
func TestLocalNamesKeepClearOfGit(t *testing.T) {
	for filename, want := range map[string]string{
		".git":           ".git-2",
		".GIT":           ".GIT-2",
		".Git.":          ".Git-2.",
		".git ":          ".git -2",
		".gitattributes": ".gitattributes-2",
		".gitmodules":    ".gitmodules-2",
		".gitignore":     ".gitignore-2",
		".env":           ".env",
		".github":        ".github",
		"git":            "git",
	} {
		assert.Equal(t, want, newLocalNames().of(filename), "%q", filename)
	}

	okf := newLocalNames()
	okf.okf = true
	assert.Equal(t, ".git-2", okf.of(".git"))
}
