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

func TestProcessIncludesInlineCodeTemplateVar(t *testing.T) {
	tests := []struct {
		name    string
		content string
		data    string
	}{
		{
			name:    "simple_var",
			content: "Inline: `{{ .var }}`",
			data:    "var: hello",
		},
		{
			name:    "nil_map_access",
			content: "Inline: `{{ .data.key }}`",
			data:    "",
		},
		{
			name:    "nil_slice_range",
			content: "Inline: `{{ range .items }}{{ . }}{{ end }}`",
			data:    "",
		},
		{
			name:    "nil_slice_index",
			content: "Inline: `{{ index .items 0 }}`",
			data:    "",
		},
		{
			name:    "nil_func_call",
			content: "Inline: `{{ len .items }}`",
			data:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			templatePath := filepath.Join(tempDir, tt.name+".md")
			err := os.WriteFile(templatePath, []byte(tt.content), 0644)
			require.NoError(t, err)

			input := []byte("<!-- Include: " + tt.name + ".md\n" + tt.data + " -->")

			tmpl, output, recurse, err := ProcessIncludes(tempDir, "", input, template.New("test"))
			_ = tmpl
			_ = recurse
			t.Logf("[%s] err: %v, output: %s", tt.name, err, string(output))
		})
	}
}

func TestMultipleIncludesExecute(t *testing.T) {
	tempDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "t1.md"), []byte("Template One: {{ .v1 }}"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "t2.md"), []byte("Template Two: {{ .v2 }}"), 0644))

	input := []byte("<!-- Include: t1.md\nv1: A -->\n<!-- Include: t2.md\nv2: B -->")

	tmpl, output, recurse, err := ProcessIncludes(tempDir, "", input, template.New("test"))
	require.NoError(t, err)
	_ = tmpl
	_ = recurse
	t.Logf("output: %s", string(output))
}

func TestMultipleIncludesWithInlineCode(t *testing.T) {
	tempDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "header.md"), []byte("# Header"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "footer.md"), []byte("# Footer"), 0644))

	input := []byte("<!-- Include: header.md -->\n\nCode snippet: `{{ .unknown.field }}`\n\n<!-- Include: footer.md -->")

	tmpl := template.New("test")
	var recurse bool
	var err error
	for {
		tmpl, input, recurse, err = ProcessIncludes(tempDir, "", input, tmpl)
		require.NoError(t, err)
		if !recurse {
			break
		}
	}

	output := string(input)
	assert.Contains(t, output, "# Header")
	assert.Contains(t, output, "`{{ .unknown.field }}`")
	assert.Contains(t, output, "# Footer")
}
