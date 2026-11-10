package confluence_test

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuoteCQL(t *testing.T) {
	tests := []struct {
		name, value, want string
	}{
		{"plain", "Guides", `"Guides"`},
		{"empty", "", `""`},
		{"space", "Release Notes", `"Release Notes"`},
		{"quote", `say "hi"`, `"say \"hi\""`},
		{"backslash", `C:\docs`, `"C:\\docs"`},
		{"both", `a\b "c"`, `"a\\b \"c\""`},
		{"backslash before quote", `a\"b`, `"a\\\"b"`},
		{"trailing backslash", `a\`, `"a\\"`},
		{"non-ASCII", "José Müller 日本", `"José Müller 日本"`},
		{"no-break space", "Jane\u00a0Doe", "\"Jane\u00a0Doe\""},
		{"lucene operators kept", "win* +a -b (c) ~d", `"win* +a -b (c) ~d"`},
		{"tab newline carriage return", "a\tb\nc\rd", "\"a\tb\nc\rd\""},
		{"NUL", "a\x00b", "\"a\uFFFDb\""},
		{"escape", "a\x1b[0m", "\"a\uFFFD[0m\""},
		{"invalid UTF-8", "a\xffb", "\"a\uFFFDb\""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, confluence.QuoteCQL(tt.value))
		})
	}
}

// lastCQL is the cql parameter of the last request to a path containing
// substr.
func lastCQL(t *testing.T, server *confluencetest.Server, substr string) string {
	t.Helper()
	var cql string
	for _, r := range server.Requests() {
		if strings.Contains(r.Path, substr) {
			query, err := url.ParseQuery(r.Query)
			require.NoError(t, err)
			cql = query.Get("cql")
		}
	}
	require.NotEmpty(t, cql, "no CQL search was sent")
	return cql
}

func TestFindFolderQuotesTitleAndSpace(t *testing.T) {
	api, server := newAPI(t)
	title := `Zoë's "notes" \ drafts`
	want := server.AddFolder("DOCS", title, "", "page")

	folder, err := api.FindFolder(context.Background(), "DOCS", title, "")
	require.NoError(t, err)
	require.NotNil(t, folder)
	assert.Equal(t, want.ID, folder.ID)

	assert.Equal(t,
		`type=folder AND title="Zoë's \"notes\" \\ drafts" AND space="DOCS"`,
		lastCQL(t, server, "/rest/api/search"))
}

func TestGetUserByNameQuotesTheName(t *testing.T) {
	api, server := newAPI(t)
	name := "José \"Pepe\" O\\Brien\u00a0Jr"
	server.AddUser(confluencetest.User{AccountID: "acct-1", FullName: name})

	user, err := api.GetUserByName(context.Background(), name)
	require.NoError(t, err)
	assert.Equal(t, "acct-1", user.AccountID)

	// %q would have sent the no-break space as \u00a0.
	assert.Equal(t,
		"user.fullname~\"José \\\"Pepe\\\" O\\\\Brien\u00a0Jr\"",
		lastCQL(t, server, "/rest/api/search"))
}
