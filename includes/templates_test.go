package includes

import (
	"os"
	"path/filepath"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessIncludesDirect(t *testing.T) {
	tempDir := t.TempDir()
	templatePath := filepath.Join(tempDir, "header.md")
	err := os.WriteFile(templatePath, []byte("# Header from Include\n\nHello {{ .name }}"), 0644)
	require.NoError(t, err)

	input := []byte("<!-- Include: header.md\nname: World -->")

	tmpl, output, recurse, err := ProcessIncludes(tempDir, "", input, template.New("test"))
	require.NoError(t, err)
	_ = tmpl
	_ = recurse
	assert.Contains(t, string(output), "Header from Include")
	assert.Contains(t, string(output), "Hello World")
}

func TestProcessIncludesCircularLoop(t *testing.T) {
	tempDir := t.TempDir()

	// a.md includes b.md
	aPath := filepath.Join(tempDir, "a.md")
	require.NoError(t, os.WriteFile(aPath, []byte("<!-- Include: b.md -->"), 0644))

	// b.md includes a.md
	bPath := filepath.Join(tempDir, "b.md")
	require.NoError(t, os.WriteFile(bPath, []byte("<!-- Include: a.md -->"), 0644))

	input := []byte("<!-- Include: a.md -->")

	_, _, _, err := ProcessIncludes(tempDir, "", input, template.New("test"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular include detected")
}
