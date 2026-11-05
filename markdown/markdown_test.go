package markdown_test

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

	"github.com/kovetskiy/mark/v17/markdown"
	"github.com/kovetskiy/mark/v17/metadata"
	"github.com/kovetskiy/mark/v17/stdlib"
	"github.com/stretchr/testify/assert"
)

func loadData(t *testing.T, filename, variant string) ([]byte, string, []byte) {
	t.Helper()
	basename := filepath.Base(filename)
	testname := strings.TrimSuffix(basename, ".md")
	htmlname := filepath.Join(filepath.Dir(filename), testname+variant+".html")

	source, err := os.ReadFile(filename)
	if err != nil {
		panic(err)
	}
	html, err := os.ReadFile(htmlname)
	if err != nil {
		panic(err)
	}

	return source, htmlname, html
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
		filename = filepath.ToSlash(filename) // the cases below compare slash paths
		fmt.Printf("Testing: %v\n", filename)
		lib, err := stdlib.New(context.Background(), nil)
		if err != nil {
			panic(err)
		}
		source, htmlname, html := loadData(t, filename, "")

		var body []byte
		if filename == "testdata/frontmatter.md" {
			var err error
			_, body, err = metadata.ExtractMeta(source, metadata.ExtractOptions{FrontMatter: true})
			if err != nil {
				panic(err)
			}
		} else {
			body = source
		}

		cfg := markdown.Options{
			MermaidScale:    1.0,
			D2Scale:         1.0,
			DropH1:          false,
			StripLinebreaks: false,
			Features:        []string{"mkdocsadmonitions", "mention", "plantuml", "frontmatter"},
		}

		actual, _, _ := markdown.CompileMarkdown(t.Context(), body, lib, filename, cfg)
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
		filename = filepath.ToSlash(filename) // the cases below compare slash paths
		lib, err := stdlib.New(context.Background(), nil)
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
		source, htmlname, html := loadData(t, filename, variant)

		var body []byte
		if filename == "testdata/frontmatter.md" {
			var err error
			_, body, err = metadata.ExtractMeta(source, metadata.ExtractOptions{FrontMatter: true})
			if err != nil {
				panic(err)
			}
		} else {
			body = source
		}

		cfg := markdown.Options{
			MermaidScale:    1.0,
			D2Scale:         1.0,
			DropH1:          true,
			StripLinebreaks: false,
			Features:        []string{"mkdocsadmonitions", "mention", "plantuml", "frontmatter"},
		}

		actual, _, _ := markdown.CompileMarkdown(t.Context(), body, lib, filename, cfg)
		test.EqualValues(strings.TrimSuffix(string(html), "\n"), strings.TrimSuffix(actual, "\n"), filename+" vs "+htmlname)

	}
}

func TestCompileMarkdownStripLinebreaks(t *testing.T) {
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
		filename = filepath.ToSlash(filename) // the cases below compare slash paths
		lib, err := stdlib.New(context.Background(), nil)
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

		source, htmlname, html := loadData(t, filename, variant)

		var body []byte
		if filename == "testdata/frontmatter.md" {
			var err error
			_, body, err = metadata.ExtractMeta(source, metadata.ExtractOptions{FrontMatter: true})
			if err != nil {
				panic(err)
			}
		} else {
			body = source
		}

		cfg := markdown.Options{
			MermaidScale:    1.0,
			D2Scale:         1.0,
			DropH1:          false,
			StripLinebreaks: true,
			Features:        []string{"mkdocsadmonitions", "mention", "plantuml", "frontmatter"},
		}

		actual, _, _ := markdown.CompileMarkdown(t.Context(), body, lib, filename, cfg)
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

	lib, err := stdlib.New(context.Background(), nil)
	if err != nil {
		panic(err)
	}

	source, _, html := loadData(t, "testdata/plantuml.md", "-nofeature")

	cfg := markdown.Options{
		MermaidScale:    1.0,
		D2Scale:         1.0,
		DropH1:          false,
		StripLinebreaks: false,
		Features:        []string{"mkdocsadmonitions", "mention"},
	}

	actual, _, _ := markdown.CompileMarkdown(t.Context(), source, lib, "testdata/plantuml.md", cfg)
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

	lib, err := stdlib.New(context.Background(), nil)
	if err != nil {
		panic(err)
	}

	const fixture = "testdata/inline-link-card.md"
	source, htmlname, html := loadData(t, fixture, "-inlinecard")

	cfg := markdown.Options{
		MermaidScale:    1.0,
		D2Scale:         1.0,
		DropH1:          false,
		StripLinebreaks: false,
		Features:        []string{"mkdocsadmonitions", "mention", "inline-link-card"},
	}

	actual, _, _ := markdown.CompileMarkdown(t.Context(), source, lib, fixture, cfg)
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

	lib, err := stdlib.New(context.Background(), nil)
	if err != nil {
		panic(err)
	}

	const fixture = "testdata/emoji.md"
	source, htmlname, html := loadData(t, fixture, "-confluence")

	cfg := markdown.Options{
		MermaidScale:    1.0,
		D2Scale:         1.0,
		DropH1:          false,
		StripLinebreaks: false,
		Features:        []string{"mkdocsadmonitions", "mention", "emoji"},
	}

	actual, _, _ := markdown.CompileMarkdown(t.Context(), source, lib, fixture, cfg)
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

			lib, err := stdlib.New(context.Background(), nil)
			if err != nil {
				panic(err)
			}

			const fixture = "testdata/math.md"
			source, htmlname, html := loadData(t, fixture, tt.variant)

			cfg := markdown.Options{
				MermaidScale:    1.0,
				D2Scale:         1.0,
				MathScale:       2.0,
				MathFormat:      tt.format,
				DropH1:          false,
				StripLinebreaks: false,
				Browser:         markdown.TestBrowser,
				Features:        []string{"mkdocsadmonitions", "mention", "math"},
			}

			actual, _, _ := markdown.CompileMarkdown(t.Context(), source, lib, fixture, cfg)
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
	render := func(t *testing.T, source string) (string, int) {
		t.Helper()

		lib, err := stdlib.New(context.Background(), nil)
		assert.NoError(t, err)

		actual, attachments, err := markdown.CompileMarkdown(t.Context(), []byte(source), lib, "testdata/math.md",
			markdown.Options{Browser: markdown.TestBrowser, Features: []string{"math"}})
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

// TestDetailsIsNotOptional: <details> is converted whatever --features says.
// The storage format cannot carry the tag, so the alternative was never a
// working page -- and the feature name is still accepted, since nothing
// validates the list.
func TestDetailsIsNotOptional(t *testing.T) {
	lib, err := stdlib.New(context.Background(), nil)
	assert.NoError(t, err)
	source := []byte(`<details>
<summary>Summary Text</summary>
Some content
</details>`)

	for _, features := range [][]string{nil, {"mermaid", "mention"}, {"details"}} {
		actual, _, err := markdown.CompileMarkdown(
			t.Context(),
			source, lib, "testdata/test.md", markdown.Options{Features: features},
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
	lib, err := stdlib.New(context.Background(), nil)
	assert.NoError(t, err)
	source := []byte(`<img src="https://example.com/image.png" width="300" alt="Test Image">`)

	for _, features := range [][]string{nil, {"mermaid", "mention"}, {"html-img-tag"}} {
		actual, _, err := markdown.CompileMarkdown(
			t.Context(),
			source, lib, "testdata/test.md", markdown.Options{Features: features},
		)
		assert.NoError(t, err)
		assert.Contains(t, actual, `<ac:image`, features)
		assert.Contains(t, actual, `ri:value="https://example.com/image.png"`, features)
		assert.Contains(t, actual, `ac:width="300"`, features)
	}
}

func TestDateFeature(t *testing.T) {
	lib, err := stdlib.New(context.Background(), nil)
	assert.NoError(t, err)
	source := []byte(`Release on @date(2026-07-27) or <time datetime="2026-12-31">Dec 31</time>.`)

	// 1. With date feature enabled
	cfgEnabled := markdown.Options{
		Features: []string{"date"},
	}
	actualEnabled, _, err := markdown.CompileMarkdown(t.Context(), source, lib, "testdata/test.md", cfgEnabled)
	assert.NoError(t, err)
	assert.Contains(t, actualEnabled, `<time datetime="2026-07-27" />`)
	assert.Contains(t, actualEnabled, `<time datetime="2026-12-31" />`)

	// 2. Without date feature enabled
	cfgDisabled := markdown.Options{
		Features: []string{"mermaid", "mention"},
	}
	actualDisabled, _, err := markdown.CompileMarkdown(t.Context(), source, lib, "testdata/test.md", cfgDisabled)
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

	lib, err := stdlib.New(context.Background(), nil)
	if err != nil {
		panic(err)
	}

	cfg := markdown.Options{
		MermaidScale: 1.0,
		D2Scale:      1.0,
		ResolveLink: func(target, text string) (string, error) {
			return "", fmt.Errorf("resolve link %q: nope", target)
		},
	}

	_, _, err = markdown.CompileMarkdown(
		t.Context(),
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

	lib, err := stdlib.New(context.Background(), nil)
	if err != nil {
		panic(err)
	}

	var asked []string
	cfg := markdown.Options{
		MermaidScale: 1.0,
		D2Scale:      1.0,
		ResolveLink: func(target, text string) (string, error) {
			asked = append(asked, target)
			return "", nil
		},
	}

	source := []byte("" +
		"[real](./real.md)\n\n" +
		"`[code span](./span.md)`\n\n" +
		"```\n[fenced](./fenced.md)\n```\n\n" +
		"    [indented](./indented.md)\n",
	)

	_, _, err = markdown.CompileMarkdown(t.Context(), source, lib, "testdata/x.md", cfg)
	test.NoError(err)
	test.Equal([]string{"./real.md"}, asked)
}

// TestCompileMarkdownResolveLinkDecodesDestination: the resolver is asked
// about the file a destination names, not the Markdown that spells it, just as
// an image's destination is read. An ac: link is passed as written, since the
// renderer takes its title from the destination as written.
func TestCompileMarkdownResolveLinkDecodesDestination(t *testing.T) {
	lib, err := stdlib.New(context.Background(), nil)
	if err != nil {
		panic(err)
	}

	var asked []string
	cfg := markdown.Options{
		MermaidScale: 1.0,
		D2Scale:      1.0,
		ResolveLink: func(target, text string) (string, error) {
			asked = append(asked, target)
			return "", nil
		},
	}

	source := []byte("[a](other%20page.md) [b](other\\_page.md) " +
		"[c](<other page.md>) [d](a&amp;b.md) [e](ac:AT&amp;T)\n")

	_, _, err = markdown.CompileMarkdown(t.Context(), source, lib, "testdata/x.md", cfg)
	assert.NoError(t, err)
	assert.Equal(t, []string{
		"other%20page.md", "other_page.md", "other page.md", "a&b.md", "ac:AT&amp;T",
	}, asked)
}
