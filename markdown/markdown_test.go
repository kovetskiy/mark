package mark_test

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
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

// TestCompileMarkdownEmoji covers the emoji feature. testdata/emoji.md is also
// picked up by the feature-off tests above, which pin the shortcodes passing
// through as text, so the pair of fixtures records exactly what the feature
// changes -- including which emoji become an ac:emoticon and which are written
// as the character itself.
func TestCompileMarkdownEmoji(t *testing.T) {
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

	const fixture = "testdata/emoji.md"
	markdown, htmlname, html := loadData(t, fixture, "-confluence")

	cfg := types.MarkConfig{
		MermaidScale:  1.0,
		D2Scale:       1.0,
		DropFirstH1:   false,
		StripNewlines: false,
		Features:      []string{"mkdocsadmonitions", "mention", "emoji"},
	}

	actual, _, _ := mark.CompileMarkdown(markdown, lib, fixture, cfg)
	test.EqualValues(strings.TrimSuffix(string(html), "\n"), strings.TrimSuffix(actual, "\n"), fixture+" vs "+htmlname)
}

// TestCompileMarkdownMath covers the math feature, which renders LaTeX to an
// image published as an attachment. testdata/math.md is also picked up by the
// feature-off tests above, which assert the formulas pass through as plain
// text; these two pin what actually reaches the page, in both formats.
//
// The fixtures matter because the rendering is tied to the bundled mathjax-go:
// the pixel dimensions on every ac:image, and the file names, are derived from
// its output, so a version that changes either shows up here rather than as a
// silently resized formula. The rendered bytes are not compared -- Chrome's
// output is not stable across environments -- only the storage format is, and
// that is derived from the formula rather than from the picture.
func TestCompileMarkdownMath(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	dir := path.Join(path.Dir(filename), "..")
	err := os.Chdir(dir)
	if err != nil {
		panic(err)
	}

	tests := []struct {
		name    string
		variant string
		format  string
	}{
		// The default, which needs the same browser mermaid does.
		{name: "png", variant: "-png", format: ""},
		{name: "svg", variant: "-svg", format: "svg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			test := assert.New(t)

			lib, err := stdlib.New(nil)
			if err != nil {
				panic(err)
			}

			const fixture = "testdata/math.md"
			markdown, htmlname, html := loadData(t, fixture, tt.variant)

			cfg := types.MarkConfig{
				MermaidScale:  1.0,
				D2Scale:       1.0,
				MathScale:     2.0,
				MathFormat:    tt.format,
				DropFirstH1:   false,
				StripNewlines: false,
				Features:      []string{"mkdocsadmonitions", "mention", "math"},
			}

			actual, _, _ := mark.CompileMarkdown(markdown, lib, fixture, cfg)
			test.EqualValues(strings.TrimSuffix(string(html), "\n"), strings.TrimSuffix(actual, "\n"), fixture+" vs "+htmlname)
		})
	}
}

// TestCompileMarkdownMathPreservesEscapes guards a property no golden file
// would make obvious: the math parser has to claim the formula before goldmark
// processes backslash escapes. Without that ordering "\\" collapses to "\" and a
// pmatrix loses its row separator -- which is exactly what testdata/math.html
// records happening when the feature is off.
//
// The check is the rendered height. A two-row matrix is taller than a one-row
// matrix; if the separator had been eaten, the two would render alike.
func TestCompileMarkdownMathPreservesEscapes(t *testing.T) {
	render := func(t *testing.T, markdown string) (string, int) {
		t.Helper()

		lib, err := stdlib.New(nil)
		assert.NoError(t, err)

		actual, attachments, err := mark.CompileMarkdown([]byte(markdown), lib, "testdata/math.md",
			types.MarkConfig{Features: []string{"math"}})
		assert.NoError(t, err)

		if !assert.Len(t, attachments, 1) {
			return actual, 0
		}

		pixels, err := strconv.Atoi(attachments[0].Height)
		assert.NoError(t, err)

		return actual, pixels
	}

	_, oneRow := render(t, `$$\begin{pmatrix} a & b \end{pmatrix}$$`)
	body, twoRows := render(t, `$$\begin{pmatrix} a & b \\ c & d \end{pmatrix}$$`)

	assert.Contains(t, body, `\\`, "the row separator must reach MathJax unescaped")
	assert.Greater(t, twoRows, oneRow, "the second row has to be rendered, not swallowed with its separator")
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

// TestDetailsIsNotOptional: <details> is converted whatever --features says.
// The storage format cannot carry the tag, so the alternative was never a
// working page -- and the feature name is still accepted, since nothing
// validates the list.
func TestDetailsIsNotOptional(t *testing.T) {
	lib, err := stdlib.New(nil)
	assert.NoError(t, err)
	markdown := []byte(`<details>
<summary>Summary Text</summary>
Some content
</details>`)

	for _, features := range [][]string{nil, {"mermaid", "mention"}, {"details"}} {
		actual, _, err := mark.CompileMarkdown(
			markdown, lib, "testdata/test.md", types.MarkConfig{Features: features},
		)
		assert.NoError(t, err)
		assert.Contains(t, actual, `<ac:structured-macro ac:name="expand">`, features)
		assert.Contains(t, actual, `<ac:parameter ac:name="title">Summary Text</ac:parameter>`, features)
		assert.Contains(t, actual, `<ac:rich-text-body>`, features)
		assert.NotContains(t, actual, `<details>`, features)
	}
}

// TestHTMLImgTagIsNotOptional: an <img> is a void tag, so leaving it as written
// is not a page without a picture but a body Confluence rejects outright.
func TestHTMLImgTagIsNotOptional(t *testing.T) {
	lib, err := stdlib.New(nil)
	assert.NoError(t, err)
	markdown := []byte(`<img src="https://example.com/image.png" width="300" alt="Test Image">`)

	for _, features := range [][]string{nil, {"mermaid", "mention"}, {"html-img-tag"}} {
		actual, _, err := mark.CompileMarkdown(
			markdown, lib, "testdata/test.md", types.MarkConfig{Features: features},
		)
		assert.NoError(t, err)
		assert.Contains(t, actual, `<ac:image`, features)
		assert.Contains(t, actual, `ri:value="https://example.com/image.png"`, features)
		assert.Contains(t, actual, `ac:width="300"`, features)
	}
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
		ResolveLink: func(target, text string) (string, error) {
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
		ResolveLink: func(target, text string) (string, error) {
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
