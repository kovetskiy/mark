package mark_test

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	mark "github.com/kovetskiy/mark/markdown"
	"github.com/kovetskiy/mark/stdlib"
	"github.com/kovetskiy/mark/util"
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
		lib, err := stdlib.New(nil)
		if err != nil {
			panic(err)
		}
		markdown, htmlname, html := loadData(t, filename, "")
		actual, _ := mark.CompileMarkdown(markdown, lib, filename, "", 1.0, false, false)
		test.EqualValues(string(html), actual, filename+" vs "+htmlname)
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
		case "testdata/quotes.md", "testdata/header.md":
			variant = "-droph1"
		default:
			variant = ""
		}
		markdown, htmlname, html := loadData(t, filename, variant)
		actual, _ := mark.CompileMarkdown(markdown, lib, filename, "", 1.0, true, false)
		test.EqualValues(string(html), actual, filename+" vs "+htmlname)
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
		case "testdata/quotes.md", "testdata/codes.md", "testdata/newlines.md", "testdata/macro-include.md":
			variant = "-stripnewlines"
		default:
			variant = ""
		}

		markdown, htmlname, html := loadData(t, filename, variant)
		actual, _ := mark.CompileMarkdown(markdown, lib, filename, "", 1.0, false, true)
		test.EqualValues(string(html), actual, filename+" vs "+htmlname)
	}
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
	assert.NoError(t, err, "App should run without errors when continue-on-error is enabled")
}
