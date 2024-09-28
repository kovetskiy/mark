package mark

import (
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/stdlib"
	"github.com/stretchr/testify/assert"
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
		actual, _ := CompileMarkdown(markdown, lib, filename, "", 1.0, false, false)
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
		actual, _ := CompileMarkdown(markdown, lib, filename, "", 1.0, true, false)
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
		actual, _ := CompileMarkdown(markdown, lib, filename, "", 1.0, false, true)
		test.EqualValues(string(html), actual, filename+" vs "+htmlname)
	}
}
