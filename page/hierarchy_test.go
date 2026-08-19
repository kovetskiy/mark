package page

import (
	"testing"

	"github.com/stretchr/testify/assert"
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

func TestPathHierarchy(t *testing.T) {
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
		"a nested index sits under the directories above it": {
			"docs", "docs/guides/deep/README.md", []string{"Guides"}, "Deep",
		},
		"README at the root has no directory to be named after": {
			"docs", "docs/README.md", nil, "",
		},
		"no root at all": {
			"", "guides/setup.md", []string{"Guides"}, "",
		},
		"a file outside the root says nothing": {
			"docs", "other/setup.md", nil, "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			parents, title, err := PathHierarchy(tt.root, tt.file, nil)
			assert.NoError(t, err)
			assert.Equal(t, tt.parents, parents)
			assert.Equal(t, tt.title, title)
		})
	}
}

// TestDirectoryTitlesAreAskedOnce: a directory's title is worked out by reading
// a file, and a repository has many documents in each directory.
func TestDirectoryTitlesAreAskedOnce(t *testing.T) {
	asked := map[string]int{}
	titles := NewDirectoryTitles(func(directory string) (string, error) {
		asked[directory]++

		return "Handbook", nil
	})

	for range 3 {
		title, err := titles.Title("docs/guides")
		assert.NoError(t, err)
		assert.Equal(t, "Handbook", title)
	}

	assert.Equal(t, 1, asked["docs/guides"])
}

// TestDirectoryTitleFallsBackToTheName: saying nothing leaves the directory
// named after itself.
func TestDirectoryTitleFallsBackToTheName(t *testing.T) {
	titles := NewDirectoryTitles(func(string) (string, error) { return "", nil })

	title, err := titles.Title("docs/getting-started")
	assert.NoError(t, err)
	assert.Equal(t, "Getting Started", title)
}

// TestDirectoryTitleUsedForParentsAndForTheIndex is the agreement that matters:
// the name a directory's own document publishes under, and the name its
// children look for, are the same string.
func TestDirectoryTitleUsedForParentsAndForTheIndex(t *testing.T) {
	titles := NewDirectoryTitles(func(directory string) (string, error) {
		if directory == "docs/guides" {
			return "Developer Handbook", nil
		}

		return "", nil
	})

	parents, _, err := PathHierarchy("docs", "docs/guides/setup.md", titles)
	assert.NoError(t, err)
	assert.Equal(t, []string{"Developer Handbook"}, parents)

	_, own, err := PathHierarchy("docs", "docs/guides/README.md", titles)
	assert.NoError(t, err)
	assert.Equal(t, "Developer Handbook", own)
}
