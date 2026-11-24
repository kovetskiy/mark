package metadata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContentStatusMetadata(t *testing.T) {
	for _, tt := range []struct {
		name     string
		document string
		status   *string
		invalid  bool
	}{
		{name: "absent", document: "<!-- Title: Guide -->\n\nBody"},
		{name: "comment name", document: "<!-- Content-Status: Ready for review -->\n\nBody", status: new("Ready for review")},
		{name: "comment clear", document: "<!-- Content-Status: ~ -->\n\nBody", status: new("")},
		{name: "yaml name", document: "---\ncontent-status: Ready for review\n---\n\nBody", status: new("Ready for review")},
		{name: "yaml clear", document: "---\ncontent-status: ~\n---\n\nBody", status: new("")},
		{name: "yaml null", document: "---\ncontent-status: null\n---\n\nBody", status: new("")},
		{name: "yaml literal tilde", document: "---\ncontent-status: \"~\"\n---\n\nBody", status: new("~")},
		{name: "comment empty", document: "<!-- Content-Status: -->\n\nBody", invalid: true},
		{name: "yaml empty", document: "---\ncontent-status:\n---\n\nBody", invalid: true},
		{name: "yaml list", document: "---\ncontent-status: [Ready, Done]\n---\n\nBody", invalid: true},
		{name: "duplicate", document: "---\ncontent-status: Ready\n---\n\n<!-- Content-Status: Done -->\n\nBody", invalid: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			meta, _, err := ExtractMeta([]byte(tt.document), ExtractOptions{FrontMatter: true})
			if tt.invalid {
				require.ErrorContains(t, err, "status")
				return
			}
			require.NoError(t, err)
			require.NotNil(t, meta)
			assert.Equal(t, tt.status, meta.ContentStatus)
		})
	}
}

func TestFormatContentStatus(t *testing.T) {
	markdown, err := FrontMatter(map[string]any{"content-status": nil})
	require.NoError(t, err)

	meta, _, err := ExtractMeta([]byte(markdown), ExtractOptions{FrontMatter: true})
	require.NoError(t, err)
	require.NotNil(t, meta)
	assert.Equal(t, new(""), meta.ContentStatus)

	_, err = FrontMatter(map[string]any{"content-status": ""})
	require.ErrorIs(t, err, ErrUnrepresentable)
}
