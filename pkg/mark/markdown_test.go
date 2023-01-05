package mark

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/pkg/mark/stdlib"
	"github.com/stretchr/testify/assert"
)

const (
	NL = "\n"
)

func TestCompileMarkdown(t *testing.T) {
	test := assert.New(t)

	testcases, err := filepath.Glob("testdata/*.md")
	if err != nil {
		panic(err)
	}

	for _, filename := range testcases {
		basename := filepath.Base(filename)
		testname := strings.TrimSuffix(basename, ".md")
		htmlname := filepath.Join(filepath.Dir(filename), testname+".html")

		markdown, err := os.ReadFile(filename)
		if err != nil {
			panic(err)
		}
		html, err := os.ReadFile(htmlname)
		if err != nil {
			panic(err)
		}

		lib, err := stdlib.New(nil)
		if err != nil {
			panic(err)
		}
		actual := CompileMarkdown(markdown, lib)
		test.EqualValues(string(html), actual, filename+" vs "+htmlname)
	}
}

func TestExtractDocumentLeadingH1(t *testing.T) {
	filename := "testdata/header.md"

	markdown, err := os.ReadFile(filename)
	if err != nil {
		panic(err)
	}

	actual := ExtractDocumentLeadingH1(markdown)

	assert.Equal(t, "a", actual)
}
