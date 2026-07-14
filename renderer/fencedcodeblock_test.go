package renderer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark"
	goldmarkRenderer "github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// newGoldmarkWithRenderer creates a goldmark instance with the given fenced code block renderer
func newGoldmarkWithRenderer(fcbRenderer goldmarkRenderer.NodeRenderer) goldmark.Markdown {
	m := goldmark.New(
		goldmark.WithRendererOptions(html.WithUnsafe(), html.WithXHTML()),
	)
	m.Renderer().AddOptions(goldmarkRenderer.WithNodeRenderers(
		util.Prioritized(fcbRenderer, 100),
	))
	return m
}

// mockAttacher collects attachments for testing
type mockAttacher struct {
	attachments []attachment.Attachment
}

func (m *mockAttacher) Attach(a attachment.Attachment) {
	m.attachments = append(m.attachments, a)
}

func TestMermaidNative_WithTitle(t *testing.T) {
	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	attacher := &mockAttacher{}
	cfg := types.MarkConfig{
		Features: []string{"mermaid-cloud"},
	}

	renderer := NewConfluenceFencedCodeBlockRenderer(lib, attacher, cfg)

	// Simulate a fenced code block with mermaid and title
	source := []byte("```mermaid title My Diagram\ngraph TD;\n    A-->B;\n```\n")
	// Use goldmark to parse and render
	md := newGoldmarkWithRenderer(renderer)
	var buf bytes.Buffer
	err = md.Convert(source, &buf)
	require.NoError(t, err)

	output := buf.String()

	// Verify mermaid-cloud macro is generated
	assert.Contains(t, output, `<ac:structured-macro ac:name="mermaid-cloud"`)
	assert.Contains(t, output, `<ac:parameter ac:name="filename">My Diagram.md</ac:parameter>`)
	assert.Contains(t, output, `<ac:parameter ac:name="format">svg</ac:parameter>`)
	assert.Contains(t, output, `<ac:parameter ac:name="zoom">fit</ac:parameter>`)
	assert.Contains(t, output, `<ac:parameter ac:name="toolbar">bottom</ac:parameter>`)

	// Verify attachment was created
	require.Len(t, attacher.attachments, 1)
	assert.Equal(t, "My Diagram", attacher.attachments[0].Name)
	assert.Equal(t, "My Diagram.md", attacher.attachments[0].Filename)
	assert.Equal(t, "graph TD;\n    A-->B;\n", string(attacher.attachments[0].FileBytes))
}

func TestMermaidNative_WithoutTitle(t *testing.T) {
	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	attacher := &mockAttacher{}
	cfg := types.MarkConfig{
		Features: []string{"mermaid-cloud"},
	}

	r := NewConfluenceFencedCodeBlockRenderer(lib, attacher, cfg)

	diagramContent := []byte("graph TD;\n    C-->D;\n")
	// Compute expected hash
	hash := sha256.Sum256(diagramContent)
	expectedName := "mermaid-" + hex.EncodeToString(hash[:])[:8]
	expectedFilename := expectedName + ".md"

	source := []byte("```mermaid\ngraph TD;\n    C-->D;\n```\n")
	md := newGoldmarkWithRenderer(r)
	var buf bytes.Buffer
	err = md.Convert(source, &buf)
	require.NoError(t, err)

	output := buf.String()

	// Verify mermaid-cloud macro uses hash-based filename
	assert.Contains(t, output, `<ac:structured-macro ac:name="mermaid-cloud"`)
	assert.Contains(t, output, `<ac:parameter ac:name="filename">`+expectedFilename+`</ac:parameter>`)

	// Verify attachment was created with hash-based name
	require.Len(t, attacher.attachments, 1)
	assert.Equal(t, expectedName, attacher.attachments[0].Name)
	assert.Equal(t, expectedFilename, attacher.attachments[0].Filename)
}

func TestMermaidNative_MultipleUntitled(t *testing.T) {
	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	attacher := &mockAttacher{}
	cfg := types.MarkConfig{
		Features: []string{"mermaid-cloud"},
	}

	r := NewConfluenceFencedCodeBlockRenderer(lib, attacher, cfg)

	// Two different untitled diagrams
	source := []byte("```mermaid\ngraph TD;\n    A-->B;\n```\n\n```mermaid\ngraph TD;\n    C-->D;\n```\n")
	md := newGoldmarkWithRenderer(r)
	var buf bytes.Buffer
	err = md.Convert(source, &buf)
	require.NoError(t, err)

	// Verify two attachments were created with different names
	require.Len(t, attacher.attachments, 2)
	assert.NotEqual(t, attacher.attachments[0].Name, attacher.attachments[1].Name,
		"Multiple untitled mermaid diagrams should have unique hash-based names")
	assert.NotEqual(t, attacher.attachments[0].Filename, attacher.attachments[1].Filename)

	// Verify both are .md files
	assert.True(t, strings.HasSuffix(attacher.attachments[0].Filename, ".md"))
	assert.True(t, strings.HasSuffix(attacher.attachments[1].Filename, ".md"))
}

func TestMermaidNative_SameContentSameHash(t *testing.T) {
	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	attacher := &mockAttacher{}
	cfg := types.MarkConfig{
		Features: []string{"mermaid-cloud"},
	}

	r := NewConfluenceFencedCodeBlockRenderer(lib, attacher, cfg)

	// Two identical untitled diagrams should produce the same hash
	source := []byte("```mermaid\ngraph TD;\n    A-->B;\n```\n\n```mermaid\ngraph TD;\n    A-->B;\n```\n")
	md := newGoldmarkWithRenderer(r)
	var buf bytes.Buffer
	err = md.Convert(source, &buf)
	require.NoError(t, err)

	// Same content = same hash = same name (content-addressable)
	require.Len(t, attacher.attachments, 2)
	assert.Equal(t, attacher.attachments[0].Name, attacher.attachments[1].Name,
		"Identical untitled mermaid diagrams should produce the same hash-based name")
}

func TestMermaidNative_NotActiveWhenFeatureDisabled(t *testing.T) {
	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	attacher := &mockAttacher{}
	// Only "mermaid" feature, NOT "mermaid-cloud"
	cfg := types.MarkConfig{
		Features: []string{"mermaid"},
	}

	r := NewConfluenceFencedCodeBlockRenderer(lib, attacher, cfg)

	source := []byte("```mermaid\ngraph TD;\n    A-->B;\n```\n")
	md := newGoldmarkWithRenderer(r)
	var buf bytes.Buffer
	// This will fail because mermaid.ProcessMermaidLocally requires chrome,
	// but the key point is that it does NOT produce mermaid-cloud macro
	err = md.Convert(source, &buf)

	// If chrome is not available, the mermaid branch will error.
	// But we should NOT see mermaid-cloud in the output
	if err == nil {
		assert.NotContains(t, buf.String(), `ac:name="mermaid-cloud"`,
			"mermaid-cloud macro should NOT be generated when mermaid-cloud feature is disabled")
	}
}