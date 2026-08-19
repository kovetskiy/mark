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
			parents, title := PathHierarchy(tt.root, tt.file)
			assert.Equal(t, tt.parents, parents)
			assert.Equal(t, tt.title, title)
		})
	}
}
