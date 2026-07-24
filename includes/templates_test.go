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
