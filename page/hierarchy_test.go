package page

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGlobRoot(t *testing.T) {
	for pattern, expected := range map[string]string{
		"docs/**/*.md":      "docs",
		"docs/*.md":         "docs",
		"docs/guides/*.md":  "docs/guides",
		"docs/README.md":    "docs",
		"*.md":              "",
		"**/*.md":           "",
		"/abs/docs/**/*.md": "/abs/docs",
		"docs/{a,b}/*.md":   "docs",
		"":                  "",
	} {
		assert.Equal(t, expected, GlobRoot(pattern), "pattern %q", pattern)
	}
}

func TestHierarchyParentsAndTitle(t *testing.T) {
	for name, tt := range map[string]struct {
		root, file string
		parents    []string
		title      string
	}{
		"a document in a subdirectory": {
			"docs", "docs/guides/setup.md", []string{"Guides"}, "",
		},
		"nested directories": {
			"docs", "docs/guides/deep/setup.md", []string{"Guides", "Deep"}, "",
		},
		"directory names are titled": {
			"docs", "docs/getting-started/x.md", []string{"Getting Started"}, "",
		},
		"underscores too": {
			"docs", "docs/on_call/x.md", []string{"On Call"}, "",
		},
		"a document at the root": {
			"docs", "docs/setup.md", nil, "",
		},
		"README is its directory's page": {
			"docs", "docs/guides/README.md", nil, "Guides",
		},
		"index is its directory's page": {
			"docs", "docs/guides/index.md", nil, "Guides",
		},
		"index files are matched without regard to case": {
			"docs", "docs/guides/ReadMe.md", nil, "Guides",
		},
		"a nested index sits under the directories above it": {
			"docs", "docs/guides/deep/README.md", []string{"Guides"}, "Deep",
		},
		"README at the root is titled by the root": {
			"docs", "docs/README.md", nil, "Docs",
		},
		"README with no root at all keeps its own title": {
			"", "README.md", nil, "",
		},
		"no root at all": {
			"", "guides/setup.md", []string{"Guides"}, "",
		},
		"a file outside the root says nothing": {
			"docs", "other/setup.md", nil, "",
		},
		"roots and files are compared cleaned": {
			"./docs/", "docs/guides/setup.md", []string{"Guides"}, "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := NewHierarchy(tt.root, []string{tt.file}, nil)

			parents, err := h.Parents(tt.file)
			require.NoError(t, err)
			assert.Equal(t, tt.parents, parents)

			title, err := h.Title(tt.file)
			require.NoError(t, err)
			assert.Equal(t, tt.title, title)
		})
	}
}

// TestHierarchyReportsFilesOutsideTheRoot: a root given by hand that covers
// nothing is a mistake worth hearing about rather than a run that quietly
// derives no parents at all.
func TestHierarchyReportsFilesOutsideTheRoot(t *testing.T) {
	h := NewHierarchy("docs", []string{"docs/a.md", "other/b.md"}, nil)

	assert.Equal(t, []string{"other/b.md"}, h.Outside())
}

// TestDirectoryTitlesAreAskedOnce: a directory's title is worked out by reading
// a file, and a repository has many documents in each directory.
func TestDirectoryTitlesAreAskedOnce(t *testing.T) {
	asked := map[string]int{}
	h := NewHierarchy("docs", []string{"docs/guides/a.md", "docs/guides/b.md", "docs/guides/c.md"},
		func(directory, _ string) (string, error) {
			asked[directory]++

			return "Handbook", nil
		})

	for _, file := range []string{"docs/guides/a.md", "docs/guides/b.md", "docs/guides/c.md"} {
		parents, err := h.Parents(file)
		require.NoError(t, err)
		assert.Equal(t, []string{"Handbook"}, parents)
	}

	assert.Equal(t, 1, asked["docs/guides"])
}

// TestDirectoryTitleFallsBackToTheName: saying nothing leaves the directory
// named after itself.
func TestDirectoryTitleFallsBackToTheName(t *testing.T) {
	h := NewHierarchy("docs", []string{"docs/getting-started/x.md"},
		func(string, string) (string, error) { return "", nil })

	parents, err := h.Parents("docs/getting-started/x.md")
	require.NoError(t, err)
	assert.Equal(t, []string{"Getting Started"}, parents)
}

// TestDirectoryTitleUsedForParentsAndForTheIndex is the agreement that matters:
// the name a directory's own document publishes under, and the name its
// children look for, are the same string.
func TestDirectoryTitleUsedForParentsAndForTheIndex(t *testing.T) {
	files := []string{"docs/guides/README.md", "docs/guides/setup.md"}
	h := NewHierarchy("docs", files, func(directory, indexFile string) (string, error) {
		if directory == "docs/guides" {
			assert.Equal(t, "docs/guides/README.md", indexFile,
				"the resolver is told which document stands for the directory")

			return "Developer Handbook", nil
		}

		return "", nil
	})

	parents, err := h.Parents("docs/guides/setup.md")
	require.NoError(t, err)
	assert.Equal(t, []string{"Developer Handbook"}, parents)

	own, err := h.Title("docs/guides/README.md")
	require.NoError(t, err)
	assert.Equal(t, "Developer Handbook", own)
}

// TestIndexOutsideTheRunDoesNotCount: only a document the run publishes stands
// for its directory. One the pattern left out is not consulted for a title and
// does not stop the directory from being tracked.
func TestIndexOutsideTheRunDoesNotCount(t *testing.T) {
	h := NewHierarchy("docs", []string{"docs/guides/setup.md"},
		func(_, indexFile string) (string, error) {
			assert.Empty(t, indexFile)

			return "", nil
		})

	assert.False(t, h.HasIndex("docs/guides"))
}

func TestDirectories(t *testing.T) {
	files := []string{
		"docs/guides/README.md",
		"docs/guides/deep/setup.md",
		"docs/api/overview.md",
		"docs/top.md",
	}
	h := NewHierarchy("docs", files, nil)

	assert.Equal(t, []string{"docs/guides", "docs/guides/deep"}, h.Directories("docs/guides/deep/setup.md"))
	assert.Equal(t, []string{"docs/api"}, h.Directories("docs/api/overview.md"))
	assert.Empty(t, h.Directories("docs/top.md"))
	assert.Empty(t, h.Directories("docs/guides/README.md"),
		"a document standing for its directory is not under it")
	assert.Empty(t, h.Directories("elsewhere/x.md"))

	// guides has a document of its own; deep does not.
	assert.True(t, h.HasIndex("docs/guides"))
	assert.False(t, h.HasIndex("docs/guides/deep"))
}

// TestClaimIsPerSpaceAndIgnoresCase: Confluence holds one page of a title per
// space, and compares titles without regard to case.
func TestClaimIsPerSpaceAndIgnoresCase(t *testing.T) {
	h := NewHierarchy("", nil, nil)

	_, taken := h.Claim("DOCS", "Overview", "a.md")
	assert.False(t, taken)

	_, taken = h.Claim("DOCS", "Overview", "a.md")
	assert.False(t, taken, "the same document may ask again")

	previous, taken := h.Claim("DOCS", "overview", "b.md")
	assert.True(t, taken)
	assert.Equal(t, "a.md", previous)

	_, taken = h.Claim("OTHER", "Overview", "c.md")
	assert.False(t, taken, "the constraint is per space")

	var none *Hierarchy
	_, taken = none.Claim("DOCS", "Overview", "d.md")
	assert.False(t, taken, "no hierarchy, no claims")
}
