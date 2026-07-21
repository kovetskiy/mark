package mark_test

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	mark "github.com/kovetskiy/mark/v16/markdown"
	"github.com/kovetskiy/mark/v16/metadata"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/kovetskiy/mark/v16/util"
	"github.com/stretchr/testify/assert"
	"github.com/urfave/cli/v3"
)

func loadData(t *testing.T, filename, variant string) ([]byte, string, []byte) {
	t.Helper()
	basename := filepath.Base(filename)
	testname := strings.TrimSuffix(basename, ".md")
	htmlname := filepath.Join(filepath.Dir(filename), testname+variant+".html")

	markdown, err := os.ReadFile(filename)
	if err != nil {
		panic(err)
	}
	html, err := os.ReadFile(htmlname)
	if err != nil {
		panic(err)
	}

	return markdown, htmlname, html
}

func TestCompileMarkdown(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	dir := path.Join(path.Dir(filename), "..")
	err := os.Chdir(dir)
	if err != nil {
		panic(err)
	}

	test := assert.New(t)

	testcases, err := filepath.Glob("testdata/*.md")
	if err != nil {
		panic(err)
	}

	for _, filename := range testcases {
		fmt.Printf("Testing: %v\n", filename)
		lib, err := stdlib.New(nil)
		if err != nil {
			panic(err)
		}
		markdown, htmlname, html := loadData(t, filename, "")

		var body []byte
		if filename == "testdata/frontmatter.md" {
			var err error
			_, body, err = metadata.ExtractMeta(markdown, "", false, false, "", nil, false, "", true)
			if err != nil {
				panic(err)
			}
		} else {
			body = markdown
		}

		cfg := types.MarkConfig{
			MermaidScale:  1.0,
			D2Scale:       1.0,
			DropFirstH1:   false,
			StripNewlines: false,
			Features:      []string{"mkdocsadmonitions", "mention", "plantuml", "frontmatter"},
		}

		actual, _, _ := mark.CompileMarkdown(body, lib, filename, cfg)
		test.EqualValues(strings.TrimSuffix(string(html), "\n"), strings.TrimSuffix(actual, "\n"), filename+" vs "+htmlname)
	}
}

func TestCompileMarkdownDropH1(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	dir := path.Join(path.Dir(filename), "..")
	err := os.Chdir(dir)
	if err != nil {
		panic(err)
	}

	test := assert.New(t)

	testcases, err := filepath.Glob("testdata/*.md")
	if err != nil {
		panic(err)
	}

	for _, filename := range testcases {
		lib, err := stdlib.New(nil)
		if err != nil {
			panic(err)
		}
		var variant string
		switch filename {
		case "testdata/quotes.md", "testdata/header.md", "testdata/admonitions.md", "testdata/plantuml.md", "testdata/codeblock-comments.md", "testdata/frontmatter.md":
			variant = "-droph1"
		default:
			variant = ""
		}
		markdown, htmlname, html := loadData(t, filename, variant)

		var body []byte
		if filename == "testdata/frontmatter.md" {
			var err error
			_, body, err = metadata.ExtractMeta(markdown, "", false, false, "", nil, false, "", true)
			if err != nil {
				panic(err)
			}
		} else {
			body = markdown
		}

		cfg := types.MarkConfig{
			MermaidScale:  1.0,
			D2Scale:       1.0,
			DropFirstH1:   true,
			StripNewlines: false,
			Features:      []string{"mkdocsadmonitions", "mention", "plantuml", "frontmatter"},
		}

		actual, _, _ := mark.CompileMarkdown(body, lib, filename, cfg)
		test.EqualValues(strings.TrimSuffix(string(html), "\n"), strings.TrimSuffix(actual, "\n"), filename+" vs "+htmlname)

	}
}

func TestCompileMarkdownStripNewlines(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	dir := path.Join(path.Dir(filename), "..")
	err := os.Chdir(dir)
	if err != nil {
		panic(err)
	}

	test := assert.New(t)

	testcases, err := filepath.Glob("testdata/*.md")
	if err != nil {
		panic(err)
	}

	for _, filename := range testcases {
		lib, err := stdlib.New(nil)
		if err != nil {
			panic(err)
		}
		var variant string
		switch filename {
		case "testdata/quotes.md", "testdata/codes.md", "testdata/newlines.md", "testdata/macro-include.md", "testdata/admonitions.md", "testdata/mention.md", "testdata/codeblock-comments.md":
			variant = "-stripnewlines"
		default:
			variant = ""
		}

		markdown, htmlname, html := loadData(t, filename, variant)

		var body []byte
		if filename == "testdata/frontmatter.md" {
			var err error
			_, body, err = metadata.ExtractMeta(markdown, "", false, false, "", nil, false, "", true)
			if err != nil {
				panic(err)
			}
		} else {
			body = markdown
		}

		cfg := types.MarkConfig{
			MermaidScale:  1.0,
			D2Scale:       1.0,
			DropFirstH1:   false,
			StripNewlines: true,
			Features:      []string{"mkdocsadmonitions", "mention", "plantuml", "frontmatter"},
		}

		actual, _, _ := mark.CompileMarkdown(body, lib, filename, cfg)
		test.EqualValues(strings.TrimSuffix(string(html), "\n"), strings.TrimSuffix(actual, "\n"), filename+" vs "+htmlname)

	}
}

func TestCompileMarkdownPlantumlOptIn(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	dir := path.Join(path.Dir(filename), "..")
	err := os.Chdir(dir)
	if err != nil {
		panic(err)
	}

	test := assert.New(t)

	lib, err := stdlib.New(nil)
	if err != nil {
		panic(err)
	}

	markdown, _, html := loadData(t, "testdata/plantuml.md", "-nofeature")

	cfg := types.MarkConfig{
		MermaidScale:  1.0,
		D2Scale:       1.0,
		DropFirstH1:   false,
		StripNewlines: false,
		Features:      []string{"mkdocsadmonitions", "mention"},
	}

	actual, _, _ := mark.CompileMarkdown(markdown, lib, "testdata/plantuml.md", cfg)
	test.EqualValues(strings.TrimSuffix(string(html), "\n"), strings.TrimSuffix(actual, "\n"), "plantuml without feature should render as regular code block")
}

func TestContinueOnError(t *testing.T) {
	cmd := &cli.Command{
		Name:                  "temp-mark",
		Usage:                 "test usage",
		Description:           "mark unit tests",
		Version:               "TEST-VERSION",
		Flags:                 util.Flags,
		EnableShellCompletion: true,
		HideHelpCommand:       true,
		Action:                util.RunMark,
	}

	filePath := filepath.Join("testdata", "batch-tests", "*.md")
	argList := []string{
		"",
		"--log-level", "INFO",
		"--compile-only",
		"--continue-on-error",
		"--files", filePath,
	}

	err := cmd.Run(context.TODO(), argList)
	// --continue-on-error processes all files even when some fail, but still
	// returns an error to allow callers/CI to detect partial failures.
	assert.Error(t, err, "App should report partial failure when continue-on-error is enabled and some files fail")
	assert.ErrorContains(t, err, "one or more files failed to process")
}

func TestDetailsFeature(t *testing.T) {
	lib, err := stdlib.New(nil)
	assert.NoError(t, err)
	markdown := []byte(`<details>
<summary>Summary Text</summary>
Some content
</details>`)

	// 1. With details feature enabled
	cfgEnabled := types.MarkConfig{
		Features: []string{"details"},
	}
	actualEnabled, _, err := mark.CompileMarkdown(markdown, lib, "testdata/test.md", cfgEnabled)
	assert.NoError(t, err)
	assert.Contains(t, actualEnabled, `<ac:structured-macro ac:name="expand">`)
	assert.Contains(t, actualEnabled, `<ac:parameter ac:name="title">Summary Text</ac:parameter>`)
	assert.Contains(t, actualEnabled, `<ac:rich-text-body>`)

	// 2. Without details feature enabled
	cfgDisabled := types.MarkConfig{
		Features: []string{"mermaid", "mention"},
	}
	actualDisabled, _, err := mark.CompileMarkdown(markdown, lib, "testdata/test.md", cfgDisabled)
	assert.NoError(t, err)
	assert.NotContains(t, actualDisabled, `<ac:structured-macro ac:name="expand">`)
	assert.Contains(t, actualDisabled, `<details>`)
}
