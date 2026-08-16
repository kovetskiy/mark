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
		case "testdata/quotes.md", "testdata/header.md", "testdata/admonitions.md", "testdata/plantuml.md", "testdata/codeblock-comments.md", "testdata/frontmatter.md", "testdata/heading-anchor-links.md", "testdata/heading-anchor-custom-ids.md":
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
		case "testdata/quotes.md", "testdata/codes.md", "testdata/newlines.md", "testdata/macro-include.md", "testdata/admonitions.md", "testdata/mention.md", "testdata/codeblock-comments.md", "testdata/heading-anchor-links.md", "testdata/heading-anchor-custom-ids.md":
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

func TestCompileMarkdownInlineLinkCard(t *testing.T) {
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

	const fixture = "testdata/inline-link-card.md"
	markdown, htmlname, html := loadData(t, fixture, "-inlinecard")

	cfg := types.MarkConfig{
		MermaidScale:  1.0,
		D2Scale:       1.0,
		DropFirstH1:   false,
		StripNewlines: false,
		Features:      []string{"mkdocsadmonitions", "mention", "inline-link-card"},
	}

	actual, _, _ := mark.CompileMarkdown(markdown, lib, fixture, cfg)
	test.EqualValues(strings.TrimSuffix(string(html), "\n"), strings.TrimSuffix(actual, "\n"), fixture+" vs "+htmlname)
}

// TestCompileMarkdownMath covers the math feature, which renders LaTeX to
// KaTeX markup at compile time. testdata/math.md is also picked up by the
// feature-off tests above, which assert the formulas pass through as plain
// text; this one pins what KaTeX actually emits.
//
// The fixture matters because the emitted markup is tied to the bundled KaTeX
// version: the v0.18 upgrade renamed presentational classes (base ->
// katex-base, strut -> katex-strut, sizing -> katex-sizing) and, with no
// fixture, the suite stayed green through the change.
func TestCompileMarkdownMath(t *testing.T) {
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

	const fixture = "testdata/math.md"
	markdown, htmlname, html := loadData(t, fixture, "-katex")

	cfg := types.MarkConfig{
		MermaidScale:  1.0,
		D2Scale:       1.0,
		DropFirstH1:   false,
		StripNewlines: false,
		Features:      []string{"mkdocsadmonitions", "mention", "math"},
	}

	actual, _, _ := mark.CompileMarkdown(markdown, lib, fixture, cfg)
	test.EqualValues(strings.TrimSuffix(string(html), "\n"), strings.TrimSuffix(actual, "\n"), fixture+" vs "+htmlname)
}

// TestCompileMarkdownMathPreservesEscapes guards a property the golden file
// alone would not make obvious: the math parser has to claim the formula
// before Goldmark processes backslash escapes. Without that ordering "\\"
// collapses to "\" and a pmatrix loses its row separator, which is exactly
// what testdata/math.html records happening when the feature is off.
func TestCompileMarkdownMathPreservesEscapes(t *testing.T) {
	lib, err := stdlib.New(nil)
	assert.NoError(t, err)

	markdown := []byte(`$$\begin{pmatrix} a & b \\ c & d \end{pmatrix}$$`)

	cfg := types.MarkConfig{Features: []string{"math"}}
	actual, _, err := mark.CompileMarkdown(markdown, lib, "testdata/math.md", cfg)
	assert.NoError(t, err)

	assert.Contains(t, actual, `\\`, "the row separator must reach KaTeX unescaped")
	assert.Equal(t, 2, strings.Count(actual, "<mtr>"), "the matrix should render two rows")
	assert.NotContains(t, actual, "katex-error", "the formula should parse cleanly")
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

	// The fixtures live in the repo root's testdata, not this package's
	// directory. Deriving the path from this file rather than the working
	// directory keeps the test independent of whether another test in the
	// package has already chdir'd: run on its own, a relative glob matched
	// nothing and the command failed with "no files matched" instead of the
	// partial-failure error being asserted.
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := path.Join(path.Dir(thisFile), "..")
	filePath := filepath.Join(repoRoot, "testdata", "batch-tests", "*.md")
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

func TestHTMLImgTagFeature(t *testing.T) {
	lib, err := stdlib.New(nil)
	assert.NoError(t, err)
	markdown := []byte(`<img src="https://example.com/image.png" width="300" alt="Test Image">`)

	// 1. With html-img-tag feature enabled
	cfgEnabled := types.MarkConfig{
		Features: []string{"html-img-tag"},
	}
	actualEnabled, _, err := mark.CompileMarkdown(markdown, lib, "testdata/test.md", cfgEnabled)
	assert.NoError(t, err)
	assert.Contains(t, actualEnabled, `<ac:image`)
	assert.Contains(t, actualEnabled, `ri:value="https://example.com/image.png"`)
	assert.Contains(t, actualEnabled, `ac:width="300"`)

	// 2. Without html-img-tag feature enabled
	cfgDisabled := types.MarkConfig{
		Features: []string{"mermaid", "mention"},
	}
	actualDisabled, _, err := mark.CompileMarkdown(markdown, lib, "testdata/test.md", cfgDisabled)
	assert.NoError(t, err)
	assert.NotContains(t, actualDisabled, `<ac:image`)
}

func TestDateFeature(t *testing.T) {
	lib, err := stdlib.New(nil)
	assert.NoError(t, err)
	markdown := []byte(`Release on @date(2026-07-27) or <time datetime="2026-12-31">Dec 31</time>.`)

	// 1. With date feature enabled
	cfgEnabled := types.MarkConfig{
		Features: []string{"date"},
	}
	actualEnabled, _, err := mark.CompileMarkdown(markdown, lib, "testdata/test.md", cfgEnabled)
	assert.NoError(t, err)
	assert.Contains(t, actualEnabled, `<time datetime="2026-07-27" />`)
	assert.Contains(t, actualEnabled, `<time datetime="2026-12-31" />`)

	// 2. Without date feature enabled
	cfgDisabled := types.MarkConfig{
		Features: []string{"mermaid", "mention"},
	}
	actualDisabled, _, err := mark.CompileMarkdown(markdown, lib, "testdata/test.md", cfgDisabled)
	assert.NoError(t, err)
	assert.NotContains(t, actualDisabled, `<time datetime="2026-07-27" />`)
}

// TestCompileMarkdownResolveLinkError checks that a failure while resolving a
// link stops the run.
//
// An AST transformer cannot return an error, so this one is stashed and picked
// up after rendering. Without that collection step the failure would be
// dropped and the page published with the link left as written -- which is
// exactly what a broken link target looks like, so nobody would notice.
func TestCompileMarkdownResolveLinkError(t *testing.T) {
	test := assert.New(t)

	lib, err := stdlib.New(nil)
	if err != nil {
		panic(err)
	}

	cfg := types.MarkConfig{
		MermaidScale: 1.0,
		D2Scale:      1.0,
		ResolveLink: func(target string) (string, error) {
			return "", fmt.Errorf("resolve link %q: nope", target)
		},
	}

	_, _, err = mark.CompileMarkdown(
		[]byte("[a link](./other.md)\n"), lib, "testdata/x.md", cfg,
	)
	test.Error(err)
	test.Contains(err.Error(), "./other.md")
}

// TestCompileMarkdownResolveLinkSkipsCode is the unit-level counterpart to the
// end-to-end tests in mark_process_test.go: the resolver must never be asked
// about text that only looks like a link.
func TestCompileMarkdownResolveLinkSkipsCode(t *testing.T) {
	test := assert.New(t)

	lib, err := stdlib.New(nil)
	if err != nil {
		panic(err)
	}

	var asked []string
	cfg := types.MarkConfig{
		MermaidScale: 1.0,
		D2Scale:      1.0,
		ResolveLink: func(target string) (string, error) {
			asked = append(asked, target)
			return "", nil
		},
	}

	markdown := []byte("" +
		"[real](./real.md)\n\n" +
		"`[code span](./span.md)`\n\n" +
		"```\n[fenced](./fenced.md)\n```\n\n" +
		"    [indented](./indented.md)\n",
	)

	_, _, err = mark.CompileMarkdown(markdown, lib, "testdata/x.md", cfg)
	test.NoError(err)
	test.Equal([]string{"./real.md"}, asked)
}
