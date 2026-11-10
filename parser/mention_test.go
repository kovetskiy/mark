package parser_test

import (
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/internal/goldmarktest"
	cparser "github.com/kovetskiy/mark/v17/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/extension"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/util"
)

// mentionNames parses source with the mention parser beside the GFM parsers
// publishing uses, and returns the name of every mention in it.
func mentionNames(t *testing.T, source string) []string {
	t.Helper()

	md := goldmarktest.New(goldmarktest.WithParserOptions(
		parser.WithExtensions(
			extension.NewTableParser(),
			extension.NewLinkifyParser(),
			extension.NewStrikethroughParser(),
		),
		parser.WithInlineParsers(util.Prioritized(cparser.NewMentionParser(), 99)),
	))

	src := []byte(source)
	var names []string
	require.NoError(t, ast.Walk(md.Parse(src), func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if m, ok := n.(*cparser.Mention); ok && entering {
			names = append(names, string(m.Name))
		}
		return ast.WalkContinue, nil
	}))

	return names
}

func TestMentionNames(t *testing.T) {
	for _, tt := range []struct {
		source string
		names  []string
	}{
		{"@{name}", []string{"name"}},
		{"@{}", nil},
		{"@{unclosed", nil},
		{`@{a\}b}`, []string{"a}b"}},
		{`@{a\\}b}`, []string{`a\`}},
		{`@{a\|b}`, []string{"a|b"}},
		{`@{DOMAIN\user}`, []string{`DOMAIN\user`}},
		{`@{a\*b}`, []string{`a\*b`}},
		{`@{a\}`, nil},
		{`@{\}}`, []string{"}"}},
		{"| h |\n| - |\n| @{a\\|b} |", []string{"a|b"}},
		{"| h |\n| - |\n| @{a|b} |", nil},
		{"[@{a]b}](http://x)", []string{"a]b"}},
	} {
		t.Run(tt.source, func(t *testing.T) {
			assert.Equal(t, tt.names, mentionNames(t, tt.source+"\n"))
		})
	}
}

// adversarialNames are names a generator might be handed, by someone trying to
// break out of the mention or simply by someone with an odd name. They seed
// FuzzFormatMention, whose seeds go test runs like any other test.
func adversarialNames() []string {
	return []string{
		"name",
		"Jane Doe",
		"}",
		"a}b",
		"a}}",
		"\\",
		"a\\",
		"a\\}",
		"\\\\}",
		"a|b",
		"|",
		"DOMAIN\\user",
		" padded ",
		"@{nested}",
		"a]b[",
		"*a*",
		"`code`",
		"<b>",
		"&amp;",
		"<!-- x -->",
		"user@example.com",
		"https://example.com",
		"x\x00y",
		"\xff",
		"x y",
		"Zoë 😀",
	}
}

// checkMention holds FormatMention to its contract for one name: an error
// that says the name cannot be written, or a mention that reads back as
// exactly the name, alone, in a paragraph and in a table cell.
func checkMention(t *testing.T, name string) {
	t.Helper()

	mention, err := cparser.FormatMention(name)
	if err != nil {
		require.ErrorIs(t, err, cparser.ErrUnrepresentable)
		require.True(t, name == "" || strings.ContainsAny(name, "\r\n") || strings.Contains(name, "<!--"), "%q was refused", name)

		return
	}

	require.NotContains(t, mention, "\n")

	assert.Equal(t, []string{name}, mentionNames(t, "Hello "+mention+" there\n"), "paragraph: %s", mention)
	assert.Equal(t, []string{name}, mentionNames(t, mention+"\n"), "line start: %s", mention)
	assert.Equal(t, []string{name}, mentionNames(t, "| h | i |\n| - | - |\n| "+mention+" | x |\n"), "table: %s", mention)
}

func FuzzFormatMention(f *testing.F) {
	for _, name := range adversarialNames() {
		f.Add(name)
	}

	f.Fuzz(func(t *testing.T, name string) {
		checkMention(t, name)
	})
}

// checkSanitizeMention: whatever the name, FormatMention takes what
// SanitizeMention makes of it unless it is empty, and leaves a name
// FormatMention already took as it was.
func checkSanitizeMention(t *testing.T, name string) {
	t.Helper()

	sanitized := cparser.SanitizeMention(name)
	assert.Equal(t, sanitized, cparser.SanitizeMention(sanitized), "not idempotent")

	if _, err := cparser.FormatMention(name); err == nil {
		assert.Equal(t, name, sanitized, "a name FormatMention accepts was altered")
	}

	if name == "" {
		assert.Empty(t, sanitized)
		return
	}

	_, err := cparser.FormatMention(sanitized)
	require.NoError(t, err, "%q sanitized to %q", name, sanitized)
	checkMention(t, sanitized)
}

func TestSanitizeMention(t *testing.T) {
	assert.Equal(t, "Jane Doe < !-- Include: x -- >", cparser.SanitizeMention("Jane\r\nDoe <!-- Include: x -- >"))
	assert.Equal(t, " ", cparser.SanitizeMention("\n"))
	assert.Equal(t, "", cparser.SanitizeMention(""))
}

func FuzzSanitizeMention(f *testing.F) {
	for _, name := range append(adversarialNames(), "<!--<!--", "<\n!--", "<!\r\n--") {
		f.Add(name)
	}

	f.Fuzz(func(t *testing.T, name string) {
		checkSanitizeMention(t, name)
	})
}

func TestFormatMentionRefusals(t *testing.T) {
	for _, name := range []string{"", "a\nb", "a\rb", "\n", "<!-- Include: secret.md -->", "<!-- Macro: x"} {
		_, err := cparser.FormatMention(name)
		assert.ErrorIs(t, err, cparser.ErrUnrepresentable, "%q", name)
	}
}

func TestFormatMentionEscapes(t *testing.T) {
	mention, err := cparser.FormatMention(`a}b|c\d`)
	require.NoError(t, err)
	assert.Equal(t, `@{a\}b\|c\\d}`, mention)
}
