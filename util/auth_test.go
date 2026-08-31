package util

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetCredentialsBaseURLFromTargetURL covers the shortest way to publish:
// paste the URL of an existing page and let mark take the instance and the page
// id out of it.
func TestGetCredentialsBaseURLFromTargetURL(t *testing.T) {
	creds, err := GetCredentials("user", "secret",
		"https://confluence.example.com/pages/viewpage.action?pageId=12345", "", false)
	require.NoError(t, err)

	assert.Equal(t, "https://confluence.example.com", creds.BaseURL)
	assert.Equal(t, "12345", creds.PageID)
	assert.Equal(t, "user", creds.Username)
	assert.Equal(t, "secret", creds.Password)
}

// TestGetCredentialsExplicitBaseURLWins covers -l beside a target URL: the flag
// is the instance, and the URL only supplies the page id.
func TestGetCredentialsExplicitBaseURLWins(t *testing.T) {
	creds, err := GetCredentials("user", "secret",
		"https://old.example.com/pages/viewpage.action?pageId=7", "https://new.example.com/wiki", false)
	require.NoError(t, err)

	assert.Equal(t, "https://new.example.com/wiki", creds.BaseURL)
	assert.Equal(t, "7", creds.PageID)
}

// TestGetCredentialsTrimsTrailingSlashes covers the base URL as people actually
// write it in a config file. Every request path is appended to this value, so a
// trailing slash would produce "//rest/api" and a 404 that says nothing about
// its cause.
func TestGetCredentialsTrimsTrailingSlashes(t *testing.T) {
	creds, err := GetCredentials("user", "secret", "", "https://confluence.example.com/wiki///", false)
	require.NoError(t, err)

	assert.Equal(t, "https://confluence.example.com/wiki", creds.BaseURL)
}

// TestGetCredentialsRequiresAPassword covers the message people see most often.
// It names the flag, because the alternative -- a 401 from Confluence -- sends
// them looking at their account instead of at their invocation.
func TestGetCredentialsRequiresAPassword(t *testing.T) {
	_, err := GetCredentials("user", "", "", "https://confluence.example.com", false)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "-p")
}

// TestGetCredentialsRequiresABaseURL covers the same for the instance: with
// neither a target URL to derive it from nor -l, there is nothing to talk to.
func TestGetCredentialsRequiresABaseURL(t *testing.T) {
	_, err := GetCredentials("user", "secret", "", "", false)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "-l")
}

// TestGetCredentialsCompileOnlyNeedsNothing covers --compile-only, which never
// reaches Confluence. Requiring credentials to render Markdown locally would
// make the flag useless in CI, so both are filled in with placeholders.
func TestGetCredentialsCompileOnlyNeedsNothing(t *testing.T) {
	creds, err := GetCredentials("", "", "", "", true)
	require.NoError(t, err)

	assert.Equal(t, "none", creds.Password)
	assert.Equal(t, "http://localhost", creds.BaseURL)
	assert.Empty(t, creds.PageID)
}

// TestGetCredentialsCompileOnlyKeepsRealValues is the boundary of that rule:
// the placeholders fill gaps, they do not override what was given.
func TestGetCredentialsCompileOnlyKeepsRealValues(t *testing.T) {
	creds, err := GetCredentials("user", "secret", "", "https://confluence.example.com", true)
	require.NoError(t, err)

	assert.Equal(t, "secret", creds.Password)
	assert.Equal(t, "https://confluence.example.com", creds.BaseURL)
}

// TestGetCredentialsPasswordFromStdin covers "-p -", which is how a token gets
// in without appearing in the process list or in shell history. The trailing
// newline a pipe or a heredoc adds has to go: it would be sent as part of the
// token and answered with a 401.
func TestGetCredentialsPasswordFromStdin(t *testing.T) {
	read, write, err := os.Pipe()
	require.NoError(t, err)

	original := os.Stdin
	os.Stdin = read
	defer func() { os.Stdin = original }()

	go func() {
		defer write.Close()
		_, _ = write.WriteString("token-from-stdin\n")
	}()

	creds, err := GetCredentials("user", "-", "", "https://confluence.example.com", false)
	require.NoError(t, err)

	assert.Equal(t, "token-from-stdin", creds.Password)
}

// TestGetCredentialsRejectsAnUnparseableURL covers the target URL that is not
// one at all, which is worth an error naming the value rather than a request to
// an empty host.
func TestGetCredentialsRejectsAnUnparseableURL(t *testing.T) {
	_, err := GetCredentials("user", "secret", "https://exam ple.com/x", "", false)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "as url")
}
