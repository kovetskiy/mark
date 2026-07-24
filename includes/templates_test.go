package includes

import (
	"bytes"
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

func TestLoadTemplateScopedBySubdirectory(t *testing.T) {
	tempDir := t.TempDir()

	dirA := filepath.Join(tempDir, "dirA")
	dirB := filepath.Join(tempDir, "dirB")
	require.NoError(t, os.MkdirAll(dirA, 0755))
	require.NoError(t, os.MkdirAll(dirB, 0755))

	require.NoError(t, os.WriteFile(filepath.Join(dirA, "header.md"), []byte("Header A"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dirB, "header.md"), []byte("Header B"), 0644))

	tmpl := template.New("stdlib")

	tmpl, err := LoadTemplate(tempDir, "", "dirA/header.md", "", "", tmpl)
	require.NoError(t, err)

	tmpl, err = LoadTemplate(tempDir, "", "dirB/header.md", "", "", tmpl)
	require.NoError(t, err)

	assert.NotNil(t, tmpl.Lookup("dirA/header"))
	assert.NotNil(t, tmpl.Lookup("dirB/header"))

	var bufA, bufB bytes.Buffer
	require.NoError(t, tmpl.Lookup("dirA/header").Execute(&bufA, nil))
	require.NoError(t, tmpl.Lookup("dirB/header").Execute(&bufB, nil))

	assert.Equal(t, "Header A", bufA.String())
	assert.Equal(t, "Header B", bufB.String())
}
