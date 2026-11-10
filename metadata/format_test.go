package metadata

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/kovetskiy/mark/v17/internal/mdsource"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// adversarialValues are values a generator might be handed by someone trying
// to forge a header, or simply by someone with an odd name.
func adversarialValues() []string {
	return []string{
		"",
		"plain",
		"x -->",
		"x --> <!-- Space: EVIL -->",
		"x\n<!-- Space: EVIL -->",
		"x\r<!-- Space: EVIL -->",
		"x\r\n<!-- Space: EVIL -->",
		"x y",
		"x\u0085",
		" padded ",
		"\tx",
		"<!-- Space: EVIL",
		"<!--",
		"-->",
		"--!>",
		"a -- b",
		"a: b: c",
		"}{|\\",
		"\\-->",
		"ac:ignore",
		"<!-- ac:ignore -->",
		"x\x00y",
		"\xff\xfe",
		"---",
		"x\n---\nspace: EVIL\n---",
		"\n0", // yaml.v3 writes it as a block scalar that does not read back
		"'\"quoted\"'",
		"# heading",
		"@{name}",
	}
}

// readBack reads a document the way publishing does. What the helpers write
// is read with nothing set aside, so any warning fails the test: an unknown
// key, an ignored value or a stranded header would each publish other than
// what was given.
func readBack(t *testing.T, document string, frontMatter bool) (*Meta, string, error) {
	t.Helper()

	source := mdsource.Normalize([]byte(document))
	source, err := mdsource.StripIgnoredBlocks(source)
	if err != nil {
		return nil, "", err
	}

	warnings := report.NewWarnings("")
	meta, body, err := ExtractMeta(source, ExtractOptions{FrontMatter: frontMatter, Warnings: warnings})
	assert.Empty(t, warnings.List(), document)

	return meta, string(body), err
}

// checkHeader holds Header to its contract for one value: an error that says
// the value cannot be written, or a line that reads back as exactly the value,
// as one header, with no other header beside it.
func checkHeader(t *testing.T, value string) {
	t.Helper()

	line, err := Header("title", value)
	if err != nil {
		require.ErrorIs(t, err, ErrUnrepresentable)
		require.True(t,
			strings.ContainsAny(value, "\r\n") ||
				strings.Contains(value, "-->") || strings.Contains(value, "--!>") ||
				strings.TrimSpace(value) != value,
			"%q was refused, but it has nothing that cannot be written", value)

		return
	}

	require.NotContains(t, line, "\n")
	require.True(t, strings.HasPrefix(line, "<!-- Title:"), line)

	key, got, ok := parseHeaderComment(line)
	require.True(t, ok, line)
	require.Equal(t, HeaderTitle, key)
	require.Equal(t, value, got)

	label, err := Header(HeaderLabel, value)
	require.NoError(t, err)

	document := line + "\n" + label + "\n\nbody\n"
	meta, body, err := readBack(t, document, false)
	require.NoError(t, err, document)
	require.NotNil(t, meta, document)

	assert.Equal(t, value, meta.Title)
	assert.Equal(t, []string{value}, meta.Labels)
	assert.Empty(t, meta.Space, "a header was forged")
	assert.Empty(t, meta.Parents, "a header was forged")
	assert.Nil(t, meta.Restrictions, "a header was forged")
	assert.Equal(t, "\nbody\n", body)
}

func TestHeaderAdversarialValues(t *testing.T) {
	for _, value := range adversarialValues() {
		t.Run(value, func(t *testing.T) {
			checkHeader(t, value)
		})
	}
}

// checkSanitizeValue: whatever the value, Header takes what SanitizeValue
// makes of it, and leaves a value Header already took as it was.
func checkSanitizeValue(t *testing.T, value string) {
	t.Helper()

	sanitized := SanitizeValue(value)
	_, err := Header(HeaderTitle, sanitized)
	require.NoError(t, err, "%q sanitized to %q", value, sanitized)
	checkHeader(t, sanitized)
	assert.Equal(t, sanitized, SanitizeValue(sanitized), "not idempotent")

	if _, err := Header(HeaderTitle, value); err == nil {
		assert.Equal(t, value, sanitized, "a value Header accepts was altered")
	}
}

func TestSanitizeValue(t *testing.T) {
	for _, value := range append(adversarialValues(), "--->", "--!-->", "-\n->", "a\r\n\r\nb", " \u2028x\u0085 ") {
		t.Run(value, func(t *testing.T) {
			checkSanitizeValue(t, value)
		})
	}

	assert.Equal(t, "Report -- > <!-- Space: EVIL", SanitizeValue("Report -->\n<!-- Space: EVIL"))
	assert.Equal(t, "a --! > b", SanitizeValue(" a --!> b\r\n"))
}

func FuzzSanitizeValue(f *testing.F) {
	for _, value := range adversarialValues() {
		f.Add(value)
	}

	f.Fuzz(func(t *testing.T, value string) {
		checkSanitizeValue(t, value)
	})
}

func FuzzHeader(f *testing.F) {
	for _, value := range adversarialValues() {
		f.Add(value)
	}

	f.Fuzz(func(t *testing.T, value string) {
		checkHeader(t, value)
	})
}

func TestHeaderRefusals(t *testing.T) {
	for _, value := range []string{"a\nb", "a\rb", "a --> b", "a --!> b", " a", "a ", "a "} {
		_, err := Header(HeaderTitle, value)
		assert.ErrorIs(t, err, ErrUnrepresentable, "%q", value)
	}

	for _, key := range []string{"", "Titel", "Include", "include", " Title", "Title ", "Title: x", "Space\n"} {
		_, err := Header(key, "x")
		assert.ErrorIs(t, err, ErrUnknownHeader, "%q", key)
	}
}

func TestHeaderSpellsTheKeyAsMarkDoes(t *testing.T) {
	line, err := Header("content-appearance", "max")
	require.NoError(t, err)
	assert.Equal(t, "<!-- Content-Appearance: max -->", line)

	line, err = Header(HeaderTitle, "")
	require.NoError(t, err)
	assert.Equal(t, "<!-- Title: -->", line)
}

// TestHeaderEveryKnownHeader covers the whole list, so that a header added to
// mark is one Header writes.
func TestHeaderEveryKnownHeader(t *testing.T) {
	for _, name := range knownHeaders {
		line, err := Header(strings.ToLower(name), "v")
		if name == HeaderInclude {
			assert.ErrorIs(t, err, ErrUnknownHeader)
			continue
		}
		require.NoError(t, err, name)
		assert.Equal(t, "<!-- "+name+": v -->", line)
	}
}

// checkFrontMatter holds FrontMatter to its contract for one value: what
// mark reads back is the value as mark reads any front matter string, and no
// key beside the ones given.
func checkFrontMatter(t *testing.T, value string) {
	t.Helper()

	fields := map[string]any{
		"title":  value,
		"labels": []string{value, "fixed"},
		"properties": map[string]any{
			"note": value,
		},
	}

	document, err := FrontMatter(fields)
	if err != nil {
		require.ErrorIs(t, err, ErrUnrepresentable)
		require.False(t, utf8.ValidString(value), "%q was refused", value)

		return
	}

	frontMatter, rest, found, err := splitFrontMatter([]byte(document))
	require.NoError(t, err)
	require.True(t, found)
	require.Empty(t, rest)

	var decoded map[string]any
	require.NoError(t, yaml.Unmarshal(frontMatter, &decoded))
	assert.Equal(t, value, decoded["title"])
	assert.Len(t, decoded, len(fields))

	meta, body, err := readBack(t, document+"body\n", true)
	require.NoError(t, err, document)
	require.NotNil(t, meta)

	assert.Equal(t, strings.TrimSpace(value), meta.Title)
	assert.Equal(t, value, meta.Properties["note"])
	assert.Empty(t, meta.Space, "a key was forged")
	assert.Nil(t, meta.Restrictions, "a key was forged")
	assert.Equal(t, "body\n", body)
}

func TestFrontMatterAdversarialValues(t *testing.T) {
	for _, value := range adversarialValues() {
		t.Run(value, func(t *testing.T) {
			checkFrontMatter(t, value)
		})
	}
}

func FuzzFrontMatter(f *testing.F) {
	for _, value := range adversarialValues() {
		f.Add(value)
	}

	f.Fuzz(func(t *testing.T, value string) {
		checkFrontMatter(t, value)
	})
}

func TestFrontMatterRefusals(t *testing.T) {
	_, err := FrontMatter(map[string]any{"titel": "x"})
	assert.ErrorIs(t, err, ErrUnknownHeader)

	_, err = FrontMatter(map[string]any{"include": "x"})
	assert.ErrorIs(t, err, ErrUnknownHeader)

	_, err = FrontMatter(map[string]any{"title": "x", "Title": "y"})
	assert.ErrorIs(t, err, ErrUnrepresentable)

	_, err = FrontMatter(map[string]any{"image-align": "x", "image_align": "y"})
	assert.ErrorIs(t, err, ErrUnrepresentable)

	_, err = FrontMatter(map[string]any{"title": func() {}})
	assert.ErrorIs(t, err, ErrUnrepresentable)

	_, err = FrontMatter(map[string]any{"properties": struct{ Zeta, Alpha string }{"a", "b"}})
	assert.ErrorIs(t, err, ErrUnrepresentable)
}

// A value that holds itself would recurse until the stack overflows, which
// kills the process rather than panicking.
func TestFrontMatterRefusesACycle(t *testing.T) {
	cycle := map[string]any{}
	cycle["x"] = cycle
	_, err := FrontMatter(map[string]any{"properties": cycle})
	assert.ErrorIs(t, err, ErrUnrepresentable)

	list := []any{nil}
	list[0] = list
	_, err = FrontMatter(map[string]any{"labels": list})
	assert.ErrorIs(t, err, ErrUnrepresentable)
}

// A map with keys that are not strings, as yaml.v2 decodes one, is written
// like any other: its strings in this package's style, so that a line break
// reads back.
func TestFrontMatterMapOfAny(t *testing.T) {
	document, err := FrontMatter(map[string]any{
		"properties": map[any]any{"note": "\nline", "b": map[any]any{"k": []any{"\n0", 1, true}}},
	})
	require.NoError(t, err)

	meta, _, err := ExtractMeta([]byte(document), ExtractOptions{FrontMatter: true})
	require.NoError(t, err, document)
	assert.Equal(t, "\nline", meta.Properties["note"])
	assert.Equal(t, map[string]any{"k": []any{"\n0", 1, true}}, meta.Properties["b"])
}

// TestFrontMatterKeysAreTheOnesMarkReads keeps frontMatterKeys and
// ExtractMeta's switch together: each key, written with a value, changes what
// ExtractMeta reads.
func TestFrontMatterKeysAreTheOnesMarkReads(t *testing.T) {
	values := map[string]any{
		"contentappearance": "max",
		"order":             3,
		"synchronized":      false,
		"properties":        map[string]any{"k": "v"},
		"restrictions":      map[string]any{"read": map[string]any{"users": []string{"u"}}},
		"type":              "blogpost",
	}

	empty, _, err := ExtractMeta([]byte("---\n{}\n---\n"), ExtractOptions{FrontMatter: true})
	require.NoError(t, err)

	for _, key := range frontMatterKeys() {
		value, ok := values[key]
		if !ok {
			value = "v"
		}

		document, err := FrontMatter(map[string]any{key: value})
		require.NoError(t, err, key)

		warnings := report.NewWarnings("")
		meta, _, err := ExtractMeta([]byte(document), ExtractOptions{FrontMatter: true, Warnings: warnings})
		require.NoError(t, err, key)
		assert.NotEqual(t, empty, meta, "front matter key %q is not read by mark", key)
		assert.Empty(t, warnings.List(), "front matter key %q was set aside", key)
	}
}

// TestFrontMatterKeysCoverExtractMeta is the other direction: every key
// ExtractMeta's front matter switch reads is one FrontMatter accepts, so a key
// added there and not to frontMatterKeys fails here rather than being refused
// as unknown.
func TestFrontMatterKeysCoverExtractMeta(t *testing.T) {
	file, err := goparser.ParseFile(token.NewFileSet(), "metadata.go", nil, 0)
	require.NoError(t, err)

	var read []string
	ast.Inspect(file, func(n ast.Node) bool {
		sw, ok := n.(*ast.SwitchStmt)
		if !ok {
			return true
		}
		if tag, ok := sw.Tag.(*ast.Ident); !ok || tag.Name != "normKey" {
			return true
		}
		for _, stmt := range sw.Body.List {
			for _, expr := range stmt.(*ast.CaseClause).List {
				lit, ok := expr.(*ast.BasicLit)
				require.True(t, ok, "a case in ExtractMeta's front matter switch is not a string literal")
				key, err := strconv.Unquote(lit.Value)
				require.NoError(t, err)
				read = append(read, key)
			}
		}
		return false
	})

	require.NotEmpty(t, read, "ExtractMeta's switch on normKey was not found")
	assert.ElementsMatch(t, frontMatterKeys(), read)
}
