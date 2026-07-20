package metadata

import (
	"os"
	"path"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/text"
)

func TestExtractDocumentLeadingH1(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	dir := path.Join(path.Dir(filename), "..")
	err := os.Chdir(dir)
	if err != nil {
		panic(err)
	}

	filename = "testdata/header.md"

	markdown, err := os.ReadFile(filename)
	if err != nil {
		panic(err)
	}

	reader := text.NewReader(markdown)
	parser := goldmark.DefaultParser()
	doc := parser.Parse(reader)
	actual := ExtractDocumentLeadingH1(doc, markdown)

	assert.Equal(t, "a", actual)
}
func TestSetTitleFromFilename(t *testing.T) {
	t.Run("set title from filename", func(t *testing.T) {
		meta := &Meta{Title: ""}
		setTitleFromFilename(meta, "/path/to/test.md")
		assert.Equal(t, "Test", meta.Title)
	})

	t.Run("replace underscores with spaces", func(t *testing.T) {
		meta := &Meta{Title: ""}
		setTitleFromFilename(meta, "/path/to/test_with_underscores.md")
		assert.Equal(t, "Test With Underscores", meta.Title)
	})

	t.Run("replace dashes with spaces", func(t *testing.T) {
		meta := &Meta{Title: ""}
		setTitleFromFilename(meta, "/path/to/test-with-dashes.md")
		assert.Equal(t, "Test With Dashes", meta.Title)
	})

	t.Run("mixed underscores and dashes", func(t *testing.T) {
		meta := &Meta{Title: ""}
		setTitleFromFilename(meta, "/path/to/test_with-mixed_separators.md")
		assert.Equal(t, "Test With Mixed Separators", meta.Title)
	})

	t.Run("already title cased", func(t *testing.T) {
		meta := &Meta{Title: ""}
		setTitleFromFilename(meta, "/path/to/Already-Title-Cased.md")
		assert.Equal(t, "Already Title Cased", meta.Title)
	})
}

func TestExtractMetaContentAppearance(t *testing.T) {
	t.Run("default fills missing content appearance", func(t *testing.T) {
		data := []byte("<!-- Space: DOC -->\n<!-- Title: Example -->\n\nbody\n")

		meta, _, err := ExtractMeta(data, "", false, false, "", nil, false, FixedContentAppearance, false)
		assert.NoError(t, err)
		assert.NotNil(t, meta)
		assert.Equal(t, FixedContentAppearance, meta.ContentAppearance)
	})

	t.Run("header takes precedence over default", func(t *testing.T) {
		data := []byte("<!-- Space: DOC -->\n<!-- Title: Example -->\n<!-- Content-Appearance: full-width -->\n\nbody\n")

		meta, _, err := ExtractMeta(data, "", false, false, "", nil, false, FixedContentAppearance, false)
		assert.NoError(t, err)
		assert.NotNil(t, meta)
		assert.Equal(t, FullWidthContentAppearance, meta.ContentAppearance)
	})

	t.Run("falls back to full-width when default isn't set", func(t *testing.T) {
		data := []byte("<!-- Space: DOC -->\n<!-- Title: Example -->\n\nbody\n")

		meta, _, err := ExtractMeta(data, "", false, false, "", nil, false, "", false)
		assert.NoError(t, err)
		assert.NotNil(t, meta)
		assert.Equal(t, FullWidthContentAppearance, meta.ContentAppearance)
	})

	t.Run("default appearance via cli flag", func(t *testing.T) {
		data := []byte("<!-- Space: DOC -->\n<!-- Title: Example -->\n\nbody\n")

		meta, _, err := ExtractMeta(data, "", false, false, "", nil, false, DefaultContentAppearance, false)
		assert.NoError(t, err)
		assert.NotNil(t, meta)
		assert.Equal(t, DefaultContentAppearance, meta.ContentAppearance)
	})

	t.Run("default appearance via header", func(t *testing.T) {
		data := []byte("<!-- Space: DOC -->\n<!-- Title: Example -->\n<!-- Content-Appearance: default -->\n\nbody\n")

		meta, _, err := ExtractMeta(data, "", false, false, "", nil, false, "", false)
		assert.NoError(t, err)
		assert.NotNil(t, meta)
		assert.Equal(t, DefaultContentAppearance, meta.ContentAppearance)
	})
}

func TestExtractMetaYAMLFrontMatterDisabled(t *testing.T) {
	markdown := `---
space: DOCS
title: Test Page
---
# Content
`

	meta, body, err := ExtractMeta([]byte(markdown), "", false, false, "", nil, false, "", false)
	assert.NoError(t, err)
	assert.Nil(t, meta)
	assert.Equal(t, markdown, string(body))
}

func TestExtractMetaYAMLFrontMatterDoesNotEnableTOML(t *testing.T) {
	markdown := `+++
space = "DOCS"
title = "Test Page"
+++
# Content
`

	meta, body, err := ExtractMeta([]byte(markdown), "", false, false, "", nil, false, "", true)
	assert.NoError(t, err)
	assert.Nil(t, meta)
	assert.Equal(t, markdown, string(body))
}

func TestExtractMetaYAMLFrontMatter(t *testing.T) {
	markdown := `---
space: DOCS
parents:
  - Parent 1
  - Parent 2
folders:
  - Folder 1
  - Folder 2
title: Test Page
type: page
layout: article
sidebar: <p>Side</p>
emoji: rocket
attachments:
  - image.png
labels:
  - alpha
  - beta
content-appearance: default
image-align: center
---
# Content
`

	meta, body, err := ExtractMeta([]byte(markdown), "", false, false, "", nil, false, "", true)
	assert.NoError(t, err)
	assert.Equal(t, &Meta{
		Parents:           []string{"Parent 1", "Parent 2"},
		Folders:           []string{"Folder 1", "Folder 2"},
		Space:             "DOCS",
		Type:              "page",
		Title:             "Test Page",
		Layout:            "article",
		Sidebar:           "<p>Side</p>",
		Emoji:             "rocket",
		Attachments:       []string{"image.png"},
		Labels:            []string{"alpha", "beta"},
		ContentAppearance: DefaultContentAppearance,
		ImageAlign:        "center",
	}, meta)
	assert.Equal(t, "# Content\n", string(body))
}

func TestExtractMetaYAMLFrontMatterScalarAliases(t *testing.T) {
	markdown := `---
space: DOCS
parent: Parent
folder: Folder
title: Test Page
attachment: image.png
label: alpha
content_appearance: fixed
image_align: right
---
# Content
`

	meta, body, err := ExtractMeta([]byte(markdown), "", false, false, "", nil, false, "", true)
	assert.NoError(t, err)
	assert.Equal(t, []string{"Parent"}, meta.Parents)
	assert.Equal(t, []string{"Folder"}, meta.Folders)
	assert.Equal(t, []string{"image.png"}, meta.Attachments)
	assert.Equal(t, []string{"alpha"}, meta.Labels)
	assert.Equal(t, FixedContentAppearance, meta.ContentAppearance)
	assert.Equal(t, "right", meta.ImageAlign)
	assert.Equal(t, "# Content\n", string(body))
}

func TestExtractMetaYAMLFrontMatterSidebarForcesArticleLayout(t *testing.T) {
	markdown := `---
space: DOCS
title: Test Page
layout: plain
sidebar: <p>Side</p>
---
# Content
`

	meta, _, err := ExtractMeta([]byte(markdown), "", false, false, "", nil, false, "", true)
	assert.NoError(t, err)
	assert.Equal(t, "article", meta.Layout)
	assert.Equal(t, "<p>Side</p>", meta.Sidebar)
}

func TestExtractMetaYAMLFrontMatterAllowsExtraKeysAndFallbacks(t *testing.T) {
	markdown := `---
product:
  - uks
doc_type: explanation
status: draft
owner: []
labels:
  - k8s-docs
---
# Title From H1
`

	meta, body, err := ExtractMeta([]byte(markdown), "DOCS", true, false, "", nil, false, "", true)
	assert.NoError(t, err)
	assert.Equal(t, "DOCS", meta.Space)
	assert.Equal(t, "Title From H1", meta.Title)
	assert.Equal(t, "page", meta.Type)
	assert.Equal(t, []string{"k8s-docs"}, meta.Labels)
	assert.Equal(t, FullWidthContentAppearance, meta.ContentAppearance)
	assert.Equal(t, "# Title From H1\n", string(body))
}

func TestExtractMetaYAMLFrontMatterDoesNotCloseOnIndentedDelimiter(t *testing.T) {
	markdown := `---
space: DOCS
title: Test Page
description: |
  First paragraph.
  ---
  Second paragraph.
labels:
  - expected-label
---
# Content
`

	meta, body, err := ExtractMeta([]byte(markdown), "", false, false, "", nil, false, "", true)
	assert.NoError(t, err)
	assert.Equal(t, []string{"expected-label"}, meta.Labels)
	assert.Equal(t, "# Content\n", string(body))
}

func TestExtractMetaYAMLFrontMatterWithHTMLHeaders(t *testing.T) {
	markdown := `---
space: DOCS
title: YAML Title
labels:
  - yaml
---
<!-- Space: LEGACY -->
<!-- Title: HTML Title -->
<!-- Parent: Parent Page -->
<!-- Label: html -->
# Content
`

	meta, body, err := ExtractMeta([]byte(markdown), "", false, false, "", nil, false, "", true)
	assert.NoError(t, err)
	assert.Equal(t, "LEGACY", meta.Space)
	assert.Equal(t, "HTML Title", meta.Title)
	assert.Equal(t, []string{"Parent Page"}, meta.Parents)
	assert.Equal(t, []string{"yaml", "html"}, meta.Labels)
	assert.Equal(t, "# Content\n", string(body))
}

func TestExtractMetaYAMLFrontMatterWithoutTrailingNewline(t *testing.T) {
	markdown := "---\nspace: DOCS\ntitle: Test Page\n---"

	meta, body, err := ExtractMeta([]byte(markdown), "", false, false, "", nil, false, "", true)
	assert.NoError(t, err)
	assert.Equal(t, "DOCS", meta.Space)
	assert.Equal(t, "Test Page", meta.Title)
	assert.Equal(t, "", string(body))
}

func TestExtractMeta_FolderHeaders(t *testing.T) {
	tests := []struct {
		name     string
		markdown string
		expected *Meta
	}{
		{
			name: "single folder header",
			markdown: `<!-- Space: DOCS -->
<!-- Folder: API Documentation -->
<!-- Title: Authentication -->

# Content`,
			expected: &Meta{
				Space:             "DOCS",
				Folders:           []string{"API Documentation"},
				Title:             "Authentication",
				Type:              "page",
				ContentAppearance: "full-width",
			},
		},
		{
			name: "multiple folder headers",
			markdown: `<!-- Space: DOCS -->
<!-- Folder: Backend -->
<!-- Folder: Services -->
<!-- Folder: Authentication -->
<!-- Title: Password Reset -->

# Content`,
			expected: &Meta{
				Space:             "DOCS",
				Folders:           []string{"Backend", "Services", "Authentication"},
				Title:             "Password Reset",
				Type:              "page",
				ContentAppearance: "full-width",
			},
		},
		{
			name: "folder and parent headers mixed",
			markdown: `<!-- Space: DOCS -->
<!-- Folder: Backend -->
<!-- Folder: Services -->
<!-- Parent: User Management -->
<!-- Title: Password Reset -->

# Content`,
			expected: &Meta{
				Space:             "DOCS",
				Folders:           []string{"Backend", "Services"},
				Parents:           []string{"User Management"},
				Title:             "Password Reset",
				Type:              "page",
				ContentAppearance: "full-width",
			},
		},
		{
			name: "no folder headers",
			markdown: `<!-- Space: DOCS -->
<!-- Parent: User Management -->
<!-- Title: Password Reset -->

# Content`,
			expected: &Meta{
				Space:             "DOCS",
				Parents:           []string{"User Management"},
				Title:             "Password Reset",
				Type:              "page",
				ContentAppearance: "full-width",
			},
		},
		{
			name: "folder headers with spaces and special characters",
			markdown: `<!-- Space: DOCS -->
<!-- Folder: API Documentation & Examples -->
<!-- Folder: User's Guide -->
<!-- Title: Getting Started -->

# Content`,
			expected: &Meta{
				Space:             "DOCS",
				Folders:           []string{"API Documentation & Examples", "User's Guide"},
				Title:             "Getting Started",
				Type:              "page",
				ContentAppearance: "full-width",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, _, err := ExtractMeta([]byte(tt.markdown), "", false, false, "", nil, false, "", false)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, meta)
		})
	}
}

func TestExtractMeta_FolderHeadersOrder(t *testing.T) {
	markdown := `<!-- Space: DOCS -->
<!-- Folder: First -->
<!-- Folder: Second -->
<!-- Folder: Third -->
<!-- Title: Test Page -->

# Content`

	meta, _, err := ExtractMeta([]byte(markdown), "", false, false, "", nil, false, "", false)
	assert.NoError(t, err)
	assert.Equal(t, []string{"First", "Second", "Third"}, meta.Folders)
}

func TestExtractMeta_FolderHeadersWithCliParents(t *testing.T) {
	markdown := `<!-- Space: DOCS -->
<!-- Folder: API -->
<!-- Title: Test Page -->

# Content`

	cliParents := []string{"CLI Parent 1", "CLI Parent 2"}
	meta, _, err := ExtractMeta([]byte(markdown), "", false, false, "", cliParents, false, "", false)
	assert.NoError(t, err)

	// CLI parents should be prepended to any parents in the markdown, not folders
	assert.Equal(t, cliParents, meta.Parents)
	assert.Equal(t, []string{"API"}, meta.Folders)
}
