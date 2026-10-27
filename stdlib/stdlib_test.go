package stdlib

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewParsesAllTemplates guards against a template in templates() failing to
// parse, which New reports as an error rather than a panic and which no other
// test in the repository would catch.
func TestNewParsesAllTemplates(t *testing.T) {
	ctx := t.Context()
	lib, err := New(ctx, nil)
	require.NoError(t, err)
	require.NotNil(t, lib)
	require.NotNil(t, lib.Templates)

	// A representative sample across the macro families; if templates() is
	// rewritten these must all still be defined.
	for _, name := range []string{
		"ac:layout",
		"ac:code",
		"ac:status",
		"ac:link:user",
		"ac:image",
		"ac:toc",
		"ac:children",
		"ac:emoticon",
	} {
		assert.NotNil(t, lib.Templates.Lookup(name), "template %q is not defined", name)
	}
}

// TestTemplatesEscapeInterpolatedValues covers the escaping helpers that the
// storage-format output depends on: a quote in a code block title must not be
// able to close the ac:parameter attribute, and a CDATA end marker in the body
// must be split across two sections.
func TestTemplatesEscapeInterpolatedValues(t *testing.T) {
	ctx := t.Context()
	lib, err := New(ctx, nil)
	require.NoError(t, err)

	var out strings.Builder
	err = lib.Templates.ExecuteTemplate(&out, "ac:code", struct {
		Language    string
		Collapse    bool
		Theme       string
		Linenumbers bool
		Firstline   int
		Title       string
		Text        string
	}{
		Language: "go",
		Title:    `a "quoted" <title> & more`,
		Text:     "before ]]> after",
	})
	require.NoError(t, err)

	rendered := out.String()
	assert.NotContains(t, rendered, `"quoted"`, "title quotes must be escaped")
	assert.Contains(t, rendered, "&#34;quoted&#34;")
	assert.Contains(t, rendered, "&lt;title&gt;")
	assert.NotContains(
		t,
		rendered,
		"before ]]> after",
		"a raw ]]> would terminate the CDATA section early",
	)
}

// TestIframeScrollingIsNamedScrolling: the iframe macro's scrolling parameter
// went out named "id", so Confluence never saw it and the frame scrolled or not
// regardless of what the document asked for -- and gained an id it never asked
// for.
func TestIframeScrollingIsNamedScrolling(t *testing.T) {
	ctx := t.Context()
	lib, err := New(ctx, nil)
	require.NoError(t, err)

	var out strings.Builder
	err = lib.Templates.ExecuteTemplate(&out, "ac:iframe", map[string]any{
		"URL":       "https://example.com/?a=1&b=2",
		"Scrolling": "no",
	})
	require.NoError(t, err)

	rendered := out.String()
	assert.Contains(t, rendered, `<ac:parameter ac:name="scrolling">no</ac:parameter>`)
	assert.NotContains(t, rendered, `ac:name="id"`)
	assert.Contains(t, rendered, `ri:value="https://example.com/?a=1&amp;b=2"`)
}

// TestColumnBodyIsRichText: a column's body is storage format, as ac:box's
// and ac:panel's are. Escaping it, which it alone of the rich-text templates
// did, put any markup in it on the page as literal text. The width is a plain
// parameter and stays escaped.
func TestColumnBodyIsRichText(t *testing.T) {
	ctx := t.Context()
	lib, err := New(ctx, nil)
	require.NoError(t, err)

	var out strings.Builder
	err = lib.Templates.ExecuteTemplate(&out, "ac:column", map[string]any{
		"Width": `50%" x="`,
		"Body":  `<p><strong>bold</strong> &amp; <ac:emoticon ac:name="tick"/></p>`,
	})
	require.NoError(t, err)

	rendered := out.String()
	assert.Contains(t, rendered,
		"<ac:rich-text-body>\n\n"+
			`<p><strong>bold</strong> &amp; <ac:emoticon ac:name="tick"/></p>`+
			"\n\n</ac:rich-text-body>")
	assert.NotContains(t, rendered, "&lt;")
	assert.Contains(t, rendered, `<ac:parameter ac:name="width">50%&#34; x=&#34;</ac:parameter>`)
}

// A control character has no escape in XML, so xmlesc and cdata replace it:
// an ANSI colour code in a code block made the page unpublishable.
func TestTemplatesReplaceIllegalXMLCharacters(t *testing.T) {
	ctx := t.Context()
	lib, err := New(ctx, nil)
	require.NoError(t, err)

	var out strings.Builder
	err = lib.Templates.ExecuteTemplate(&out, "ac:code", struct {
		Language    string
		Collapse    bool
		Theme       string
		Linenumbers bool
		Firstline   int
		Title       string
		Text        string
	}{
		Language: "go",
		Title:    "a\x1bb & c",
		Text:     "\x1b[31mred\x1b[0m\f ]]> \U0001F600",
	})
	require.NoError(t, err)

	rendered := out.String()
	assert.NotContains(t, rendered, "\x1b")
	assert.NotContains(t, rendered, "\f")
	assert.Contains(t, rendered, "a�b &amp; c")
	assert.Contains(t, rendered, "�[31mred�[0m� ]]><![CDATA[]]]]><![CDATA[> \U0001F600")

	assert.Equal(t, "a_b�.png", mustExec(t, lib, `{{ convertAttachment "a/b\x00.png" }}`))
	assert.Equal(t, "a_b.png", mustExec(t, lib, `{{ convertAttachment "./a//b.png" }}`))
	// A diagram's name is flattened from its title before it gets here.
	assert.Equal(t, "Q1_._Q2.png", mustExec(t, lib, `{{ convertAttachment "Q1_._Q2.png" }}`))
}

func mustExec(t *testing.T, lib *Lib, text string) string {
	t.Helper()

	tmpl, err := lib.Templates.Clone()
	require.NoError(t, err)

	tmpl, err = tmpl.New("test").Parse(text)
	require.NoError(t, err)

	var out strings.Builder
	require.NoError(t, tmpl.Execute(&out, nil))

	return out.String()
}

func TestReplaceIllegalXMLChars(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		replaced int
	}{
		{"", "", 0},
		{"tab\tlf\ncr\r", "tab\tlf\ncr\r", 0},
		{"\x00\x08\x0b\x0c\x0e\x1f", "������", 6},
		{"del \x7f and c1 \u0085 stay", "del \x7f and c1 \u0085 stay", 0},
		{"￾￿�", "���", 2},
		{"lone surrogate \xed\xa0\x80", "lone surrogate ���", 3},
		{"\U0010FFFF", "\U0010FFFF", 0},
	} {
		got, replaced := ReplaceIllegalXMLChars(tc.in)
		assert.Equal(t, tc.want, got, "%q", tc.in)
		assert.Equal(t, tc.replaced, replaced, "%q", tc.in)
	}
}

// BenchmarkNew measures the cost of building the standard library. mark calls
// New once per processed file, so this is the per-file floor for template
// parsing.
func BenchmarkNew(b *testing.B) {
	ctx := b.Context()
	for b.Loop() {
		if _, err := New(ctx, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// fakeUser has the fields a custom template may read, beyond the two the
// built-in ones do.
type fakeUser struct {
	AccountID, UserKey, DisplayName string
}

type fakeUsers map[string]*fakeUser

func (f fakeUsers) LookupUser(_ context.Context, name string) (any, error) {
	u, ok := f[name]
	if !ok {
		return nil, errors.New("user not found")
	}
	return u, nil
}

// TestUserTemplatesUseLookup drives the "user" template func through a
// UserLookup that is not the Confluence client.
func TestUserTemplatesUseLookup(t *testing.T) {
	lib, err := New(t.Context(), fakeUsers{
		"cloud": {AccountID: `a"1`, DisplayName: "Ada"},
		"dc":    {UserKey: "k1"},
	})
	require.NoError(t, err)

	render := func(tpl, name string) string {
		var out strings.Builder
		require.NoError(t, lib.Templates.ExecuteTemplate(&out, tpl, struct{ Name string }{name}))
		return out.String()
	}

	assert.Equal(t, `<ac:link><ri:user ri:account-id="a&#34;1" /></ac:link>`, render("ac:link:user", "cloud"))
	assert.Equal(t, `<ac:link><ri:user ri:userkey="k1" /></ac:link>`, render("ac:link:user", "dc"))
	assert.Equal(t, `nobody`, render("ac:link:user", "nobody"))
	assert.Contains(t, render("ac:profile", "dc"), `<ri:user ri:userkey="k1" />`)
	assert.Empty(t, render("ac:profile", "nobody"))

	// A template of the user's own reads the user as the lookup returned it.
	custom, err := lib.Templates.New("custom").Parse(`{{ with user .Name }}{{ .DisplayName }}{{ end }}`)
	require.NoError(t, err)
	var out strings.Builder
	require.NoError(t, custom.Execute(&out, struct{ Name string }{"cloud"}))
	assert.Equal(t, "Ada", out.String())
}

// TestNilConfluenceAPIIsNoLookup: a nil *confluence.API in the interface must
// behave as no lookup at all rather than panic.
func TestNilConfluenceAPIIsNoLookup(t *testing.T) {
	var api *confluence.API
	lib, err := New(t.Context(), api)
	require.NoError(t, err)

	var out strings.Builder
	require.NoError(t, lib.Templates.ExecuteTemplate(&out, "ac:link:user", struct{ Name string }{"x"}))
	assert.Equal(t, "x", out.String())
}
