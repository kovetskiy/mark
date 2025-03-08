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
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
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

func TestContinueOnError(t *testing.T) {
	const (
		markFileName = "temp-mark"
	)

	var flags = []cli.Flag{
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:      "files",
			Aliases:   []string{"f"},
			Value:     "",
			Usage:     "use specified markdown file(s) for converting to html. Supports file globbing patterns (needs to be quoted).",
			TakesFile: true,
			EnvVars:   []string{"MARK_FILES"},
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    "continue-on-error",
			Value:   false,
			Usage:   "don't exit if an error occurs while processing a file, continue processing remaining files.",
			EnvVars: []string{"MARK_CONTINUE_ON_ERROR"},
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    "compile-only",
			Value:   false,
			Usage:   "show resulting HTML and don't update Confluence page content.",
			EnvVars: []string{"MARK_COMPILE_ONLY"},
		}),
	}

	app := &cli.App{
		Name:                 markFileName,
		Flags:                flags,
		EnableBashCompletion: true,
		HideHelpCommand:      true,
		Action:               RunMark,
	}

	filePath := filepath.Join("testdata", "batch-tests", "*.md")
	argList := []string{
		"--compile-only", "--continue-on-error", "-files", filePath,
	}
	err := app.Run(argList)

	test := assert.New(t)
	test.NoError(err, "App should run without errors when continue-on-error is enabled")
}
